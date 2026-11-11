package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func fakeTrash(t *testing.T, dir string) {
	t.Helper()
	old := trashRun
	t.Cleanup(func() { trashRun = old })
	trashRun = func(_ string, args ...string) error {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		path := args[len(args)-1]
		return os.Rename(path, filepath.Join(dir, filepath.Base(path)))
	}
}

func executeDoc(t *testing.T, c *catalogApp, verb, id string, args ochazuke.Arguments) any {
	t.Helper()
	if args == nil {
		args = ochazuke.Arguments{}
	}
	args["id"] = id
	out, err := c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: verb, Args: args, Confirmed: true})
	if err != nil {
		t.Fatalf("%s: %v", verb, err)
	}
	return out
}
func docRecord(t *testing.T, value any) ochazuke.Thing {
	t.Helper()
	th, err := ochazuke.ThingOf(value)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func TestDocumentScaffoldLifecycle(t *testing.T) {
	root, _ := docEnv(t)
	path := filepath.Join(root, "plan.md")
	before := read(t, path)
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	original := docRecord(t, executeDoc(t, c, "get", path, nil))
	id := original["id"].(string)
	if id == path || !strings.HasPrefix(id, "md-") {
		t.Fatalf("identity %q", id)
	}
	executeDoc(t, c, "relate", id, ochazuke.Arguments{"to": "project:p1", "kind": "document"})
	moved := filepath.Join(root, "sub", "renamed.md")
	executeDoc(t, c, "move", id, ochazuke.Arguments{"to": moved})
	if exists(path) || read(t, moved) != before {
		t.Fatal("move did not preserve bytes")
	}
	after := docRecord(t, executeDoc(t, c, "get", id, nil))
	if after["id"] != id || after["path"] != moved {
		t.Fatalf("move identity: %v", after)
	}
	if _, err := c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "get", Args: ochazuke.Arguments{"id": path}}); err == nil {
		t.Fatal("stale path remains a selector")
	}
	if _, err := c.r.Execute(ochazuke.Request{Noun: "ochazuke.history-entry", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if read(t, path) != before || exists(moved) {
		t.Fatal("move undo did not restore the old location")
	}
	copy := docRecord(t, executeDoc(t, c, "duplicate", id, ochazuke.Arguments{"name": "copy"}))
	if copy["id"] == id || copy["path"] == path || read(t, copy["path"].(string)) != before {
		t.Fatalf("duplicate: %v", copy)
	}
	executeDoc(t, c, "delete", id, ochazuke.Arguments{"archive-first": true})
	archived := filepath.Join(root, ".archive", "plan.md")
	if exists(path) || read(t, archived) != before {
		t.Fatal("first delete must archive")
	}
	executeDoc(t, c, "restore", id, nil)
	if read(t, path) != before || exists(archived) {
		t.Fatal("restore must move back")
	}
	executeDoc(t, c, "delete", id, ochazuke.Arguments{"archive-first": false})
	if exists(path) {
		t.Fatal("trash left source in place")
	}
	if _, err := c.r.Execute(ochazuke.Request{Noun: "ochazuke.history-entry", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if read(t, path) != before {
		t.Fatal("trash undo lost content")
	}
	restored := docRecord(t, executeDoc(t, c, "get", id, nil))
	if restored["id"] != id {
		t.Fatal("trash undo changed identity")
	}
	next, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	got := docRecord(t, executeDoc(t, next, "get", id, nil))
	if got["path"] != path {
		t.Fatalf("restart: %v", got)
	}
	relations, err := next.state.Relations(ochazuke.RelationQuery{Thing: ochazuke.AnyRef(docNoun, id)})
	if err != nil || len(relations) != 1 || relations[0].To != "project:p1" {
		t.Fatalf("relations survived: %v %v", relations, err)
	}
}

func TestDocumentCLIHistory(t *testing.T) {
	root, _ := docEnv(t)
	path := filepath.Join(root, "plan.md")
	var record struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "get", "--id", path)), &record); err != nil {
		t.Fatal(err)
	}
	mustShell(t, "doc", "archive", "--id", record.ID)
	var entries []ochazuke.HistoryEntry
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "history", "--id", record.ID)), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Request.Verb != "archive" {
		t.Fatalf("history across CLI sessions = %+v", entries)
	}
}

func TestDocumentTUIUsesSameStore(t *testing.T) {
	root, _ := docEnv(t)
	path := filepath.Join(root, "plan.md")
	a, err := newApp(path, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	value, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "get", Target: pageID})
	if err != nil {
		t.Fatal(err)
	}
	id := docRecord(t, value)["id"].(string)
	other := filepath.Join(root, "other.md")
	if err := os.WriteFile(other, []byte("# Other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "open", Args: ochazuke.Arguments{"id": other}}); err != nil {
		t.Fatal(err)
	}
	if a.path != other || a.doc.Text() != "# Other\n" {
		t.Fatal("native open did not switch the rendered editor")
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "open", Args: ochazuke.Arguments{"id": id}}); err != nil {
		t.Fatal(err)
	}
	if a.path != path || a.doc.Text() != plan {
		t.Fatal("native open did not resolve the stable ID")
	}
	moved := filepath.Join(root, "moved.md")
	if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "move", Args: ochazuke.Arguments{"id": id, "to": moved}}); err != nil {
		t.Fatal(err)
	}
	if a.path != moved || exists(path) || a.doc.Text() != plan {
		t.Fatalf("buffer did not follow move: %s", a.path)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "set", Args: ochazuke.Arguments{"id": id, "props": map[string]any{"content": "# Written\n"}}}); err != nil {
		t.Fatal(err)
	}
	if a.doc.Text() != "# Written\n" || read(t, moved) != "# Written\n" {
		t.Fatal("store writes and editor diverged")
	}
	calls := []string{}
	a.r.SetOpener(ochazuke.Opener{Start: func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...)...)
		return nil
	}})
	for _, verb := range []string{"reveal", "open-with"} {
		args := ochazuke.Arguments{"id": id}
		if verb == "open-with" {
			args["with"] = "TextEdit"
		}
		if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: verb, Args: args, Confirmed: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(strings.Join(calls, " "), moved) {
		t.Fatalf("path verbs did not use moved file: %v", calls)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "delete", Args: ochazuke.Arguments{"id": id, "archive-first": false}, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.doc.Edit(func(string) (string, int, error) { return "# Must not recreate\n", 0, nil }); err == nil {
		t.Fatal("trashed buffer accepted a write")
	}
	if exists(moved) {
		t.Fatal("editor resurrected a trashed file")
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "ochazuke.history-entry", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if read(t, moved) != "# Written\n" || a.doc.Text() != "# Written\n" {
		t.Fatal("trash undo did not restore the editor and file")
	}
	if err := a.doc.Edit(func(string) (string, int, error) { return "# Restored and editable\n", 0, nil }); err != nil {
		t.Fatal(err)
	}
	if read(t, moved) != "# Restored and editable\n" {
		t.Fatal("trash undo left the editor read-only")
	}
}

func TestTrashCommandsAreInjectedAndPreserveArguments(t *testing.T) {
	path := "/tmp/a 'quote'; $(no).md"
	cases := []struct {
		platform, name string
		args           []string
	}{
		{"darwin", "osascript", []string{"-e", "on run argv\n tell application \"Finder\" to delete POSIX file (item 1 of argv)\nend run", path}},
		{"linux", "gio", []string{"trash", "--", path}},
	}
	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			sentinel := errors.New("runner error")
			err := trashFile(tc.platform, path, func(name string, args ...string) error {
				if name != tc.name || !reflect.DeepEqual(args, tc.args) {
					t.Fatalf("argv: %s %q", name, args)
				}
				return sentinel
			})
			if !errors.Is(err, sentinel) {
				t.Fatalf("runner failure lost: %v", err)
			}
		})
	}
}

func TestDocumentHostActions(t *testing.T) {
	root, _ := docEnv(t)
	path := filepath.Join(root, "plan.md")
	log := filepath.Join(t.TempDir(), "calls.jsonl")
	bin := filepath.Join(t.TempDir(), "host")
	script := "#!/usr/bin/env python3\nimport json,sys\na=json.loads(sys.argv[-1])\nwith open(" + strconvQuote(log) + ",'a') as f: f.write(json.dumps([sys.argv[1],sys.argv[2],a])+'\\n')\nprint(json.dumps({'id':'n1','text':'before'}) if sys.argv[1:3]==['note','get'] else '{}')\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]string{bin})
	t.Setenv("OCHAZUKE_HOST", string(raw))
	t.Setenv("OCHAZUKE_EXTENSION", "md")
	t.Setenv("VISUAL", `"/tmp/Editor App" --wait`)
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	executeDoc(t, c, "edit-in-pane", path, ochazuke.Arguments{"session": "s"})
	executeDoc(t, c, "send-to-pane", path, ochazuke.Arguments{"pane": "p1", "session": "s"})
	executeDoc(t, c, "send-to-note", path, nil)
	executeDoc(t, c, "send-to-note", path, ochazuke.Arguments{"note": "n1"})
	executeDoc(t, c, "add-to-project", path, ochazuke.Arguments{"project": "p1"})
	lines := strings.Split(strings.TrimSpace(read(t, log)), "\n")
	if len(lines) != 6 {
		t.Fatalf("host calls: %s", read(t, log))
	}
	var call []json.RawMessage
	json.Unmarshal([]byte(lines[0]), &call)
	var args map[string]any
	json.Unmarshal(call[2], &args)
	if !reflect.DeepEqual(args["command"], []any{"/tmp/Editor App", "--wait", path}) {
		t.Fatalf("editor argv: %v", args)
	}
	if !strings.Contains(lines[1], `"submit": false`) || !strings.Contains(lines[1], `"window": "p1"`) {
		t.Fatal(lines[1])
	}
	if !strings.Contains(lines[2], "md.doc:md-") || !strings.Contains(lines[4], "before\\n\\n# Plan") {
		t.Fatalf("note calls %v", lines)
	}
	if !strings.Contains(lines[5], `"project", "relate"`) {
		t.Fatalf("project doesn't share relate: %s", lines[5])
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
