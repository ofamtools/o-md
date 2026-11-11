package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

// openRecorder is a catalog runtime whose doc.open records what it would
// open instead of starting an editor.
func openRecorder(t *testing.T, data string) (*catalogApp, *[]openTarget) {
	t.Helper()
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	c.data = data
	var got []openTarget
	c.open = func(target openTarget) error {
		got = append(got, target)
		return nil
	}
	return c, &got
}

// doc.open names its buffer by path, id or the row it ran on; a path with
// no file yet is a named buffer that opening does not create, standard
// input and nothing named are scratch buffers, and nothing is refused.
func TestDocOpenTargets(t *testing.T) {
	root, _ := docEnv(t)
	data := t.TempDir()
	c, got := openRecorder(t, data)
	open := func(q ochazuke.Request) (openTarget, map[string]any) {
		t.Helper()
		q.Noun, q.Verb = docNoun, "open"
		v, err := c.r.Execute(q)
		if err != nil {
			t.Fatalf("doc open %+v: %v", q, err)
		}
		record, _ := v.(map[string]any)
		return (*got)[len(*got)-1], record
	}
	plan := filepath.Join(root, "plan.md")
	if target, record := open(ochazuke.Request{Args: ochazuke.Arguments{"path": plan}}); target != (openTarget{Path: plan}) || record["saved"] != true {
		t.Fatalf("an existing path: %+v %v", target, record)
	}
	missing := filepath.Join(root, "new", "later.md")
	if target, record := open(ochazuke.Request{Args: ochazuke.Arguments{"path": missing}}); target != (openTarget{Path: missing}) || record["saved"] != false || record["name"] != "later.md" || exists(missing) || exists(filepath.Dir(missing)) {
		t.Fatalf("a path with no file yet: %+v %v exists %v", target, record, exists(missing))
	}
	id := docRecord(t, executeDoc(t, c, "get", plan, nil))["id"].(string)
	if target, _ := open(ochazuke.Request{Args: ochazuke.Arguments{"id": id}}); target.Path != plan || target.Scratch {
		t.Fatalf("a stable id: %+v", target)
	}
	notes := filepath.Join(root, "deep", "notes.md")
	if err := c.reveal(notes); err != nil {
		t.Fatal(err)
	}
	if target, _ := open(ochazuke.Request{Target: notes}); target.Path != notes || target.Scratch {
		t.Fatalf("the row it ran on (--target): %+v", target)
	}
	scratch, record := open(ochazuke.Request{})
	if !scratch.Scratch || filepath.Dir(scratch.Path) != scratchDir(data) || exists(scratchDir(data)) || record["scratch"] != true {
		t.Fatalf("nothing named is a new scratch buffer with nothing on disk: %+v %v", scratch, record)
	}
	c.stdin = strings.NewReader("# From a pipe\n\nbody\n")
	piped, record := open(ochazuke.Request{Args: ochazuke.Arguments{"path": "-"}})
	if !piped.Scratch || piped.Origin != "stdin" || record["origin"] != "stdin" || read(t, piped.Path) != "# From a pipe\n\nbody\n" {
		t.Fatalf("standard input: %+v %v", piped, record)
	}
	c.stdin = nil
	if empty, _ := open(ochazuke.Request{Args: ochazuke.Arguments{"path": "-"}}); !empty.Scratch || exists(empty.Path) {
		t.Fatalf("no input is an empty scratch buffer, not a refusal: %+v", empty)
	}
}

// doc.reopen opens the newest scratch buffer (a new one when there is
// none), and doc list --scratch lists them newest first.
func TestDocReopenAndScratchList(t *testing.T) {
	docEnv(t)
	data := t.TempDir()
	c, got := openRecorder(t, data)
	reopen := func() openTarget {
		t.Helper()
		if _, err := c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "reopen"}); err != nil {
			t.Fatal(err)
		}
		return (*got)[len(*got)-1]
	}
	if first := reopen(); !first.Scratch || filepath.Dir(first.Path) != scratchDir(data) || exists(first.Path) {
		t.Fatalf("no scratch buffers: reopen must open a new one, not refuse: %+v", first)
	}
	older, newer := filepath.Join(scratchDir(data), "older.md"), filepath.Join(scratchDir(data), "newer.md")
	writeFile(t, older, "# Older\n")
	writeFile(t, newer, "- [ ] newer item\n")
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}
	if target := reopen(); target != (openTarget{Path: newer, Scratch: true}) {
		t.Fatalf("reopen: %+v", target)
	}
	v, err := c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "list", Args: ochazuke.Arguments{"scratch": true}})
	rows, _ := v.([]docRow)
	if err != nil || len(rows) != 2 || rows[0].Path != newer || rows[1].Path != older || rows[1].Title != "Older" || rows[0].OpenTasks != 1 || !strings.HasPrefix(rows[0].ID, "md-") {
		t.Fatalf("doc list --scratch: %+v %v", rows, err)
	}
	if v, err := c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "list"}); err != nil || len(v.([]docRow)) == 0 || strings.Contains(mustJSON(t, v), scratchDir(data)) {
		t.Fatalf("the normal list is unchanged and leaves scratch buffers out: %v %v", v, err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The bare command line is the verbs: o-md [FILE], o-md -, o-md -r and
// o-md scratch run doc open, doc reopen and doc list --scratch.
func TestBareFormsRunTheVerbs(t *testing.T) {
	root, _ := docEnv(t)
	data := t.TempDir()
	bare := func(stdin string, argv ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := run(append(argv, "--data", data), strings.NewReader(stdin), &out); err != nil {
			t.Fatalf("o-md %v: %v", argv, err)
		}
		return out.String()
	}
	// -r with no scratch buffer opens a new one: never a refusal.
	if out := bare("", "-r", "--smoke"); !strings.Contains(out, "[scratch]") {
		t.Fatalf("-r with nothing to reopen: %q", out)
	}
	missing := filepath.Join(root, "later.md")
	if out := bare("", "--smoke", missing); !strings.Contains(out, "later.md") || strings.Contains(out, "[scratch]") || exists(missing) {
		t.Fatalf("a FILE with no file yet is a named, unsaved buffer: exists %v\n%s", exists(missing), out)
	}
	if out := bare("", "--smoke", filepath.Join(root, "plan.md")); !strings.Contains(out, "Inbox page") {
		t.Fatalf("an existing FILE: %q", out)
	}
	if out := bare("# Piped\n\n- [ ] from stdin\n", "--smoke", "-"); !strings.Contains(out, "Piped") || !strings.Contains(out, "[scratch]") {
		t.Fatalf("o-md - reads standard input: %q", out)
	}
	if out := bare("", "-r", "--smoke"); !strings.Contains(out, "from stdin") {
		t.Fatalf("-r reopens what standard input wrote: %q", out)
	}
	var listed scratchFile
	if err := json.Unmarshal([]byte(bare("", "scratch", "--json")), &listed); err != nil || listed.First != "# Piped" || listed.Size == 0 || filepath.Dir(listed.Path) != scratchDir(data) {
		t.Fatalf("o-md scratch --json: %+v %v", listed, err)
	}
	if out := bare("", "scratch"); !strings.Contains(out, "# Piped") {
		t.Fatalf("o-md scratch: %q", out)
	}
	// With no terminal to draw on, the bare form and the noun verb form both
	// answer the buffer's record and start nothing.
	var record map[string]any
	if err := json.Unmarshal([]byte(bare("", missing)), &record); err != nil || record["path"] != missing || record["saved"] != false || exists(missing) {
		t.Fatalf("o-md PATH without a terminal: %v %v", record, err)
	}
	if err := json.Unmarshal([]byte(mustShell(t, "--data", data, "doc", "open", missing)), &record); err != nil || record["path"] != missing || record["saved"] != false || exists(missing) {
		t.Fatalf("o-md doc open PATH: %v %v", record, err)
	}
	if err := json.Unmarshal([]byte(mustShell(t, "--data", data, "doc", "reopen")), &record); err != nil || record["path"] != listed.Path || record["scratch"] != true {
		t.Fatalf("o-md doc reopen: %v %v", record, err)
	}
	var rows []docRow
	if err := json.Unmarshal([]byte(mustShell(t, "--data", data, "doc", "list", "--scratch")), &rows); err != nil || len(rows) != 1 || rows[0].Path != listed.Path {
		t.Fatalf("o-md doc list --scratch: %+v %v", rows, err)
	}
}

// Inside the editor the palette's doc open and doc reopen do what the
// command line's do: a named buffer written on its first change, a new
// scratch buffer when nothing is named, the newest scratch buffer again.
func TestEditorOpenAndReopen(t *testing.T) {
	root, _ := docEnv(t)
	data := t.TempDir()
	a, err := newApp(filepath.Join(root, "plan.md"), data, false)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(verb string, args ochazuke.Arguments) {
		t.Helper()
		if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: verb, Args: args}); err != nil {
			t.Fatalf("doc %s %v: %v", verb, args, err)
		}
	}
	missing := filepath.Join(root, "drafts", "idea.md")
	execute("open", ochazuke.Arguments{"path": missing})
	if a.path != missing || a.scratch || exists(missing) || !strings.Contains(a.m.Title, "idea.md") || a.doc.Text() != "" {
		t.Fatalf("a new named buffer: path %s scratch %v exists %v title %q", a.path, a.scratch, exists(missing), a.m.Title)
	}
	// The unsaved buffer is already a document to the store.
	if v, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "get", Args: ochazuke.Arguments{"prop": "path"}}); err != nil || v != missing {
		t.Fatalf("get on an unsaved named buffer: %v %v", v, err)
	}
	press(t, a, "o")
	typeText(t, a, "- [ ] an idea")
	press(t, a, "esc")
	if read(t, missing) != "- [ ] an idea\n" {
		t.Fatal("the first change writes the named file and its directory")
	}
	execute("open", nil)
	scratch := a.path
	if !a.scratch || filepath.Dir(scratch) != scratchDir(data) || exists(scratch) || !strings.Contains(a.m.Title, "[scratch]") {
		t.Fatalf("open with nothing named is a new scratch buffer: %s scratch %v title %q", a.path, a.scratch, a.m.Title)
	}
	press(t, a, "o")
	typeText(t, a, "scratch words")
	press(t, a, "esc")
	execute("open", ochazuke.Arguments{"path": filepath.Join(root, "plan.md")})
	if a.scratch || a.doc.Text() != plan {
		t.Fatal("open PATH switches to the file")
	}
	execute("reopen", nil)
	if a.path != scratch || !a.scratch || a.doc.Text() != "scratch words\n" {
		t.Fatalf("reopen: %s scratch %v text %q", a.path, a.scratch, a.doc.Text())
	}
}

// The catalog publishes the entry points with typed parameters: a host
// reads open's positional file path, and mounts reopen and list --scratch.
func TestCatalogEntryPoints(t *testing.T) {
	docEnv(t)
	var doc ocli.CatalogDoc
	if err := json.Unmarshal([]byte(mustShell(t, "catalog")), &doc); err != nil {
		t.Fatal(err)
	}
	verbs := map[string]ocli.CatalogVerb{}
	for _, n := range doc.AppNouns() {
		if n.Name == docNoun {
			for _, v := range n.Verbs {
				verbs[v.Name] = v
			}
		}
	}
	open, path := verbs["open"], verbs["open"].Params["path"]
	if open.OpensPage != docsPageID || strings.Join(open.Positional, ",") != "path" || path.Type != ochazuke.PropPath || path.Path != "file" || !path.New || path.Required || open.Params["id"].Focus {
		t.Fatalf("open: %+v", open)
	}
	if reopen, ok := verbs["reopen"]; !ok || reopen.OpensPage != "" || reopen.Subject != ochazuke.SubjectCollection {
		t.Fatalf("reopen: %+v", reopen)
	}
	if p := verbs["list"].Params["scratch"]; p.Type != "boolean" {
		t.Fatalf("list --scratch: %+v", verbs["list"].Params)
	}
}
