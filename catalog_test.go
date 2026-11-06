package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

// shell runs one catalog command and answers its stdout.
func shell(t *testing.T, argv ...string) (string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	err := runShell(argv, strings.NewReader(""), &out, &errb)
	if err != nil && errb.Len() > 0 {
		t.Logf("stderr: %s", errb.String())
	}
	return out.String(), err
}

func mustShell(t *testing.T, argv ...string) string {
	t.Helper()
	out, err := shell(t, argv...)
	if err != nil {
		t.Fatalf("o-md %s: %v", strings.Join(argv, " "), err)
	}
	return out
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// docEnv points the catalog at temp roots and config: never the owner's.
func docEnv(t *testing.T) (root, proj string) {
	t.Helper()
	base := t.TempDir()
	root, proj = filepath.Join(base, "plans"), filepath.Join(base, "proj")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("O_MD_ROOTS", root)
	t.Setenv("MTM_PROJECT_DIRS", proj)
	setEnv(t, "", "josh")
	writeFile(t, filepath.Join(root, "plan.md"), plan)
	writeFile(t, filepath.Join(root, "deep", "notes.md"), "no heading here\n")
	writeFile(t, filepath.Join(root, ".worktrees", "lane", "skip.md"), "# skip\n")
	writeFile(t, filepath.Join(root, "node_modules", "skip.md"), "# skip\n")
	writeFile(t, filepath.Join(proj, "README.md"), "# Readme\n")
	writeFile(t, filepath.Join(proj, "docs", "guide", "guide.md"), "# Guide\n")
	writeFile(t, filepath.Join(proj, "src", "inner.md"), "# not listed\n")
	return root, proj
}

func listRows(t *testing.T) map[string]docRow {
	t.Helper()
	var rows []docRow
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "list", "--json", "{}")), &rows); err != nil {
		t.Fatal(err)
	}
	out := map[string]docRow{}
	for _, r := range rows {
		out[filepath.Base(r.Path)] = r
	}
	return out
}

// doc list walks the roots (recursively, skipping .worktrees,
// node_modules and .git) and each project directory's top-level *.md and
// docs/**/*.md; a row has its title (first heading, else the file name),
// counts and cells.
func TestDocList(t *testing.T) {
	root, _ := docEnv(t)
	rows := listRows(t)
	for _, want := range []string{"plan.md", "notes.md", "README.md", "guide.md"} {
		if _, ok := rows[want]; !ok {
			t.Fatalf("%s not listed: %v", want, rows)
		}
	}
	for _, not := range []string{"skip.md", "inner.md"} {
		if _, ok := rows[not]; ok {
			t.Fatalf("%s listed", not)
		}
	}
	p := rows["plan.md"]
	if p.Title != "Plan" || p.OpenTasks != 4 || p.OpenNotes != 0 || p.ID != p.Path || len(p.Cells) != 5 || p.Root != tilde(root) {
		t.Fatalf("plan row %+v", p)
	}
	if rows["notes.md"].Title != "notes" {
		t.Fatalf("title fallback %q", rows["notes.md"].Title)
	}
	// docs.roots from the settings when $O_MD_ROOTS is unset; --root adds.
	os.Unsetenv("O_MD_ROOTS")
	t.Setenv("MTM_PROJECT_DIRS", "")
	mustShell(t, "setting", "set", "--scope", "file", rootsSetting, `["`+filepath.Join(root, "deep")+`"]`, "--yes")
	rows = listRows(t)
	if len(rows) != 1 || rows["notes.md"].Path == "" {
		t.Fatalf("settings roots: %v", rows)
	}
}

// The verbs act on --target PATH: note, notes, check, uncheck, new,
// rename, show, delete (confirmed), copy-path.
func TestDocVerbs(t *testing.T) {
	root, _ := docEnv(t)
	path := filepath.Join(root, "plan.md")
	var added map[string]string
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "note", "--target", path, "--json", `{"item":"archive","text":"keep a trash view"}`)), &added); err != nil || added["id"] == "" {
		t.Fatalf("note: %v %v", added, err)
	}
	mustShell(t, "doc", "note", "--target", path, "--json", `{"reply":"`+added["id"]+`","text":"agreed"}`)
	var notes []noteRecord
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "notes", "--target", path)), &notes); err != nil || len(notes) != 2 || notes[0].Author != "josh" {
		t.Fatalf("notes %v %v", notes, err)
	}
	prompt := mustShell(t, "doc", "notes", "--target", path, "--format", "prompt")
	if !strings.Contains(prompt, "keep a trash view") || !strings.Contains(prompt, "Notes on ") {
		t.Fatalf("prompt %s", prompt)
	}
	mustShell(t, "doc", "note", "--target", path, "--json", `{"resolve":"`+added["id"]+`"}`)
	if !strings.Contains(read(t, path), "keep a trash view") {
		t.Fatal("a resolved note stays in the file")
	}
	mustShell(t, "doc", "check", "archive", "--target", path)
	if !strings.Contains(read(t, path), "- [x] Archive and restore") {
		t.Fatal("check")
	}
	mustShell(t, "doc", "uncheck", "archive", "--target", path)
	if !strings.Contains(read(t, path), "- [ ] Archive and restore") {
		t.Fatal("uncheck")
	}
	var row docRow
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "new", "Road map", "--root", root)), &row); err != nil || filepath.Base(row.Path) != "Road map.md" || read(t, row.Path) != "# Road map\n" {
		t.Fatalf("new %+v %v", row, err)
	}
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "new", "Road map", "--root", root)), &row); err != nil || filepath.Base(row.Path) != "Road map-2.md" {
		t.Fatalf("new taken name %+v %v", row, err)
	}
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "new")), &row); err != nil || !strings.HasPrefix(filepath.Base(row.Path), "untitled-") || filepath.Dir(row.Path) != root {
		t.Fatalf("new untitled %+v %v", row, err)
	}
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "rename", "roadmap", "--target", filepath.Join(root, "Road map.md"))), &row); err != nil || row.Path != filepath.Join(root, "roadmap.md") {
		t.Fatalf("rename %+v %v", row, err)
	}
	if _, err := shell(t, "doc", "rename", "roadmap", "--target", filepath.Join(root, "Road map-2.md")); err == nil {
		t.Fatal("rename over an existing file")
	}
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "show", "--target", row.Path)), &row); err != nil || row.Title != "Road map" {
		t.Fatalf("show %+v %v", row, err)
	}
	if _, err := shell(t, "doc", "delete", "--target", row.Path); err == nil || exists(row.Path) == false {
		t.Fatalf("delete without --yes: %v", err)
	}
	mustShell(t, "doc", "delete", "--target", row.Path, "--yes")
	if exists(row.Path) {
		t.Fatal("not deleted")
	}
	t.Setenv("O_MD_CLIPBOARD", "true")
	var copied map[string]any
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "copy-path", "--target", path)), &copied); err != nil || copied["path"] != path || copied["copied"] != true {
		t.Fatalf("copy-path %v %v", copied, err)
	}
}

func TestDocArchiveRoundTrip(t *testing.T) {
	root, proj := docEnv(t)
	for _, path := range []string{filepath.Join(root, "deep", "notes.md"), filepath.Join(proj, "docs", "guide", "guide.md")} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			before := read(t, path)
			base := root
			if strings.HasPrefix(path, proj+string(filepath.Separator)) {
				base = proj
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(base, ".archive", rel)
			var row docRow
			if err := json.Unmarshal([]byte(mustShell(t, "doc", "archive", "--target", path)), &row); err != nil || row.Path != dest {
				t.Fatalf("archive: %+v %v", row, err)
			}
			if exists(path) || read(t, dest) != before {
				t.Fatal("archive did not preserve the file")
			}
			if _, ok := listRows(t)[filepath.Base(path)]; ok {
				t.Fatal("archived document in the default list")
			}
			var rows []docRow
			if err := json.Unmarshal([]byte(mustShell(t, "doc", "list", "--archived")), &rows); err != nil || len(rows) != 1 || rows[0].Path != dest {
				t.Fatalf("archived list: %+v %v", rows, err)
			}
			mustShell(t, "setting", "set", "--scope", "file", "docs.hide-archived", "false", "--yes")
			if got := listRows(t)[filepath.Base(path)].Path; got != dest {
				t.Fatalf("archive hiding opt-out: %s", got)
			}
			mustShell(t, "setting", "set", "--scope", "file", "docs.hide-archived", "true", "--yes")
			// Retrying archive is harmless; restoring must not replace a new file.
			mustShell(t, "doc", "archive", "--target", dest)
			writeFile(t, path, "# Replacement\n")
			if _, err := shell(t, "doc", "restore", "--target", dest); err == nil || read(t, path) != "# Replacement\n" || read(t, dest) != before {
				t.Fatalf("restore overwrote a file: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			mustShell(t, "doc", "restore", "--target", dest)
			if exists(dest) || read(t, path) != before || listRows(t)[filepath.Base(path)].Path != path {
				t.Fatal("restore did not recover the document")
			}
		})
	}
}

func TestDocArchiveCollision(t *testing.T) {
	root, _ := docEnv(t)
	path := filepath.Join(root, "plan.md")
	dest := filepath.Join(root, ".archive", "plan.md")
	before := read(t, path)
	writeFile(t, dest, "# Earlier archive\n")
	if _, err := shell(t, "doc", "archive", "--target", path); err == nil || read(t, dest) != "# Earlier archive\n" || read(t, path) != before {
		t.Fatalf("archive overwrote a file: %v", err)
	}
	if _, err := shell(t, "doc", "restore", "--target", path); err == nil || read(t, path) != before {
		t.Fatalf("restored a live document: %v", err)
	}
}

func TestDocArchiveAttribution(t *testing.T) {
	root, _ := docEnv(t)
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	actor := ochazuke.Actor{Kind: ochazuke.ActorAgent, Name: "archive-test", Harness: "codex", Session: "fixture"}
	path := filepath.Join(root, "plan.md")
	for _, step := range []struct{ verb, path string }{
		{"archive", path},
		{"restore", filepath.Join(root, ".archive", "plan.md")},
	} {
		if err := c.reveal(step.path); err != nil {
			t.Fatal(err)
		}
		if _, err := c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: step.verb, Target: step.path, Actor: &actor}); err != nil {
			t.Fatal(err)
		}
		entry := c.r.History(1)[0]
		if entry.Context.Actor != actor || entry.Request.Verb != step.verb || !entry.Result.OK {
			t.Fatalf("archive history: %+v", entry)
		}
	}
	if read(t, path) != plan {
		t.Fatal("agent round trip changed document content")
	}
}

// send-notes queues the open, unsent notes for the pane through tuios
// and marks them sent; without a pane it says so.
func TestDocSendNotes(t *testing.T) {
	root, _ := docEnv(t)
	path := filepath.Join(root, "plan.md")
	mustShell(t, "doc", "note", "--target", path, "--json", `{"item":"saved prompts","text":"per task"}`)
	if _, err := shell(t, "doc", "send-notes", "--target", path); err == nil || !strings.Contains(err.Error(), "needs a pane") {
		t.Fatalf("no pane: %v", err)
	}
	logf := fakeTuios(t, "p1")
	var sent map[string]any
	if err := json.Unmarshal([]byte(mustShell(t, "doc", "send-notes", "p1", "--target", path)), &sent); err != nil || sent["sent"] != float64(1) {
		t.Fatalf("send %v %v", sent, err)
	}
	if log := read(t, logf); !strings.HasPrefix(log, "queue -w p1 -s s -- Notes on") || !strings.Contains(log, "per task") {
		t.Fatalf("queued %q", log)
	}
	if !strings.Contains(read(t, path), "· sent: per task") {
		t.Fatalf("not marked sent:\n%s", read(t, path))
	}
}

// The catalog publishes the doc noun with its row source and columns,
// send-notes' pane parameter naming the host's pane noun, and the Docs
// page: a split with a markdown detail over the path field.
func TestCatalog(t *testing.T) {
	docEnv(t)
	var doc ocli.CatalogDoc
	if err := json.Unmarshal([]byte(mustShell(t, "catalog")), &doc); err != nil {
		t.Fatal(err)
	}
	if verb, rows := doc.RowSource(docNoun); verb != "list" || rows != docNoun {
		t.Fatalf("row source %s %s", verb, rows)
	}
	var verbs []string
	for _, n := range doc.AppNouns() {
		if n.Name != docNoun {
			continue
		}
		for _, v := range n.Verbs {
			verbs = append(verbs, v.Name)
			if v.Name == "send-notes" && v.Params["pane"].Noun != "pane" {
				t.Fatalf("send-notes pane = %+v", v.Params["pane"])
			}
			if v.Name == "delete" && v.Confirm == "" {
				t.Fatal("delete has no confirm")
			}
		}
	}
	for _, want := range []string{"check", "copy-path", "delete", "list", "new", "note", "notes", "rename", "send-notes", "show", "uncheck"} {
		if !strings.Contains(" "+strings.Join(verbs, " ")+" ", " "+want+" ") {
			t.Fatalf("verb %s missing: %v", want, verbs)
		}
	}
	if len(doc.Pages) != 1 {
		t.Fatalf("pages %+v", doc.Pages)
	}
	p := doc.Pages[0]
	if p.ID != docsPageID || p.Layout != ocli.LayoutSplit || p.Detail == nil || p.Detail.Kind != "markdown" || p.Detail.PathField != "path" || len(p.Lists) != 1 || p.Lists[0].Noun != docNoun {
		t.Fatalf("page %+v detail %+v", p, p.Detail)
	}
	if len(doc.ListColumns(docNoun)) != 5 {
		t.Fatalf("columns %+v", doc.ListColumns(docNoun))
	}
}

// A file named like a command still opens (o-md doc is the noun only when
// no such file exists).
func TestShellCommandOrFile(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if !isShellCommand([]string{"doc", "list"}) || isShellCommand([]string{"plan.md"}) || isShellCommand(nil) {
		t.Fatal("dispatch")
	}
	writeFile(t, filepath.Join(dir, "doc"), "# a file called doc\n")
	if isShellCommand([]string{"doc"}) {
		t.Fatal("an existing file named doc is a file")
	}
}
