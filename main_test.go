package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/markdown"
)

const plan = "# Plan\n\n## Inbox\n\n- [ ] Inbox page\n  - [ ] Read/unread per turn\n- [ ] Archive and restore\n\n## Prompts\n\n- [ ] Saved prompts\n"

func writeTemp(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func cli(t *testing.T, argv ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(argv, strings.NewReader(""), &out)
	return out.String(), err
}

func press(t *testing.T, a *App, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, err := a.r.Dispatch(ochazuke.MacroKey{Key: k}); err != nil {
			t.Fatalf("key %q: %v", k, err)
		}
	}
}

func typeText(t *testing.T, a *App, s string) {
	t.Helper()
	for _, c := range s {
		k := string(c)
		if c == ' ' {
			k = "space"
		}
		press(t, a, k)
	}
}

// setEnv runs as pane (an agent tuios lists, through a fake tuios; ""
// none) for user.
func setEnv(t *testing.T, pane, user string) {
	t.Setenv("TUIOS_PANE_ID", pane)
	t.Setenv("USER", user)
	t.Setenv("O_MD_OWNER", "")
	fakeTuios(t, pane)
}

// fakeTuios is a tuios that lists an agent in each pane given and logs
// every other command to the file it answers.
func fakeTuios(t *testing.T, panes ...string) string {
	t.Helper()
	dir := t.TempDir()
	logf := filepath.Join(dir, "log")
	var agents []string
	for _, p := range panes {
		if p != "" {
			agents = append(agents, `{"window_id":"`+p+`","name":"agent","state":"idle","ready":true,"session":"s"}`)
		}
	}
	script := "#!/bin/sh\nif [ \"$1\" = list-agents ]; then echo '{\"agents\":[" + strings.Join(agents, ",") + "]}'; exit 0; fi\necho \"$@\" >> " + logf + "\n"
	fake := filepath.Join(dir, "tuios")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("O_MD_TUIOS", fake)
	return logf
}

// A pane tuios lists no agent in is the owner's (mtm, a shell); an agent
// may not sign as the owner.
func TestIdentityNeedsAnAgent(t *testing.T) {
	path := writeTemp(t, plan)
	t.Setenv("TUIOS_PANE_ID", "aaaaaaaa-1111-2222-3333-444444444444")
	t.Setenv("USER", "josh")
	t.Setenv("O_MD_OWNER", "")
	fakeTuios(t) // no agents
	if _, err := cli(t, "note", path, "--item", "inbox page", "from a shell pane"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), "> **josh** ") {
		t.Fatalf("a pane without an agent is not the owner:\n%s", read(t, path))
	}
	agent := "bbbbbbbb-1111-2222-3333-444444444444"
	setEnv(t, agent, "josh")
	if _, err := cli(t, "note", path, "--item", "archive", "--author", "josh", "pretending"); err == nil || !strings.Contains(err.Error(), "may not sign as the owner") {
		t.Fatalf("an agent signed as the owner: %v", err)
	}
	if _, err := cli(t, "note", path, "--item", "archive", "--author", "owner", "pretending"); err == nil {
		t.Fatal("an agent signed as owner")
	}
	if _, err := cli(t, "note", path, "--item", "archive", "mine"); err != nil || !strings.Contains(read(t, path), "> **"+agent+"** ") {
		t.Fatalf("agent note: %v\n%s", err, read(t, path))
	}
}

// An agent adds a note, replies, and reads them back; its identity is
// its pane; it may resolve its own note but not the owner's.
func TestNoteCommands(t *testing.T) {
	path := writeTemp(t, plan)
	setEnv(t, "", "josh")
	if _, err := cli(t, "note", path, "--item", "inbox page", "start", "from", "matomeru-old"); err != nil {
		t.Fatal(err)
	}
	pane := "90605ff3-d3ae-4258-b6a7-7ca49ff9d8f5"
	setEnv(t, pane, "josh")
	id, err := cli(t, "note", path, "--reply", "1.1", "on it")
	if err != nil || strings.TrimSpace(id) != "1.2" {
		t.Fatalf("reply: %q %v", id, err)
	}
	got := read(t, path)
	want := "- [ ] Inbox page\n  > **josh** " + today() + ": start from matomeru-old\n  > **" + pane + "** " + today() + ": on it\n  - [ ] Read/unread"
	if !strings.Contains(got, want) {
		t.Fatalf("file:\n%s", got)
	}
	out, err := cli(t, "notes", path)
	if err != nil {
		t.Fatal(err)
	}
	var recs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		recs = append(recs, m)
	}
	if len(recs) != 2 || recs[0]["item"] != "Inbox page" || recs[0]["author"] != "josh" || recs[1]["self_declared"] != false || recs[0]["checked"] != false {
		t.Fatalf("notes %v", recs)
	}
	if _, err := cli(t, "note", path, "--resolve", "1.1"); err == nil || !strings.Contains(err.Error(), "only its author or the owner") {
		t.Fatalf("an agent resolving the owner's note: %v", err)
	}
	if _, err := cli(t, "note", path, "--resolve", "1.2"); err != nil {
		t.Fatal(err)
	}
	if out, _ := cli(t, "notes", path, "--unresolved"); strings.Count(out, "\n") != 1 || !strings.Contains(out, "matomeru-old") {
		t.Fatalf("unresolved: %q", out)
	}
	if _, err := cli(t, "note", path, "--item", "inbox", "--author", "bob", "hello"); err != nil {
		t.Fatal(err)
	}
	if out, _ := cli(t, "notes", path, "--item", "inbox"); !strings.Contains(out, `"author":"bob"`) || !strings.Contains(out, `"self_declared":true`) {
		t.Fatalf("self declared: %s", out)
	}
	if _, err := cli(t, "note", path, "--item", "nothing like it", "x"); err == nil || !strings.Contains(err.Error(), "no list item matches") {
		t.Fatalf("no match: %v", err)
	}
	setEnv(t, "", "josh")
	if _, err := cli(t, "note", path, "--resolve", "1.1"); err != nil {
		t.Fatalf("the owner resolves any note: %v", err)
	}
	if _, err := cli(t, "note", path, "--item", "inbox", strings.Repeat("x", markdown.NoteMax+1)); err == nil {
		t.Fatal("a note over 4 KiB must be refused")
	}
}

func TestCheckAndPrompt(t *testing.T) {
	path := writeTemp(t, plan)
	setEnv(t, "", "josh")
	if _, err := cli(t, "check", path, "--item", "archive"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), "- [x] Archive and restore") {
		t.Fatal("check")
	}
	if _, err := cli(t, "uncheck", path, "--item", "archive"); err != nil || !strings.Contains(read(t, path), "- [ ] Archive and restore") {
		t.Fatalf("uncheck %v", err)
	}
	_, _ = cli(t, "note", path, "--item", "read/unread", "header count too")
	_, _ = cli(t, "note", path, "--item", "saved prompts", "per task\nor per project")
	out, err := cli(t, "notes", path, "--format", "prompt")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Notes on " + path, `1. Inbox › Inbox page "Read/unread per turn" (note 2.1`, `2. Prompts "Saved prompts"`, "   per task\n   or per project", "o-md note " + path + " --reply ID TEXT"} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, out)
		}
	}
	dry, err := cli(t, "send", path, "--dry-run")
	if err != nil || !strings.Contains(dry, "header count too") {
		t.Fatalf("dry run %q %v", dry, err)
	}
	if strings.Contains(read(t, path), "· sent") {
		t.Fatal("a dry run marks nothing")
	}
}

// o-md send queues through tuios (a fake here) and marks the notes sent;
// --now refuses a busy pane.
func TestSend(t *testing.T) {
	path := writeTemp(t, plan)
	setEnv(t, "", "josh")
	_, _ = cli(t, "note", path, "--item", "archive", "keep a trash view")
	t.Setenv("TUIOS_PANE_ID", "")
	dir := t.TempDir()
	logf := filepath.Join(dir, "log")
	fake := filepath.Join(dir, "tuios")
	script := "#!/bin/sh\nif [ \"$1\" = list-agents ]; then echo '{\"agents\":[{\"window_id\":\"p1\",\"name\":\"worker\",\"state\":\"working\",\"ready\":false,\"session\":\"s\"}]}'; exit 0; fi\necho \"$@\" >> " + logf + "\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("O_MD_TUIOS", fake)
	if _, err := cli(t, "send", path, "--pane", "p1", "--now"); err == nil || !strings.Contains(err.Error(), "not at rest") {
		t.Fatalf("--now on a busy pane: %v", err)
	}
	if _, err := cli(t, "send", path, "--pane", "p1"); err != nil {
		t.Fatal(err)
	}
	if log := read(t, logf); !strings.HasPrefix(log, "queue -w p1 -s s -- Notes on") || !strings.Contains(log, "keep a trash view") {
		t.Fatalf("queued %q", log)
	}
	if !strings.Contains(read(t, path), "· sent: keep a trash view") {
		t.Fatalf("not marked sent:\n%s", read(t, path))
	}
	if _, err := cli(t, "send", path, "--pane", "p1"); err == nil {
		t.Fatal("nothing unsent left to send")
	}
}

// A bare o-md is a scratch buffer: nothing on disk until the first edit,
// [scratch] in the title, :save-as moves it; -r reopens the newest.
func TestScratch(t *testing.T) {
	data := t.TempDir()
	path := newScratchPath(data, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	a, err := newApp(path, data, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.m.Title, "[scratch]") || exists(path) {
		t.Fatalf("title %q, exists %v", a.m.Title, exists(path))
	}
	press(t, a, "o")
	typeText(t, a, "- [ ] buy milk")
	press(t, a, "esc")
	if got := read(t, path); got != "- [ ] buy milk\n" {
		t.Fatalf("scratch %q", got)
	}
	files, _ := scratchFiles(data)
	if len(files) != 1 || files[0].First != "- [ ] buy milk" {
		t.Fatalf("scratch list %+v", files)
	}
	out, err := cli(t, "-r", "--data", data, "--smoke")
	if err != nil || !strings.Contains(out, "buy milk") {
		t.Fatalf("-r: %q %v", out, err)
	}
	named := filepath.Join(t.TempDir(), "deep", "list.md")
	if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "save-as", Args: ochazuke.Arguments{"path": named}}); err != nil {
		t.Fatal(err)
	}
	if exists(path) || read(t, named) != "- [ ] buy milk\n" || strings.Contains(a.m.Title, "[scratch]") {
		t.Fatalf("save-as: scratch %v, title %q", exists(path), a.m.Title)
	}
	press(t, a, "x")
	if read(t, named) != "- [x] buy milk\n" {
		t.Fatal("edits continue in the named file")
	}
	// A named file saved as another is a copy; an existing target asks.
	other := filepath.Join(t.TempDir(), "copy.md")
	if err := os.WriteFile(other, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "save-as", Args: ochazuke.Arguments{"path": other}}); err != nil {
		t.Fatal(err)
	}
	if a.r.TopModal() == "" || read(t, other) != "old\n" {
		t.Fatal("an existing file must be confirmed before it is overwritten")
	}
	press(t, a, "enter")
	if read(t, other) != "- [x] buy milk\n" || read(t, named) != "- [x] buy milk\n" {
		t.Fatal("overwrite after confirming; the old file stays")
	}
	// An untouched scratch buffer leaves nothing behind.
	data2 := t.TempDir()
	if _, err := cli(t, "--data", data2, "--smoke"); err != nil {
		t.Fatal(err)
	}
	if files, _ := scratchFiles(data2); len(files) != 0 {
		t.Fatalf("untouched scratch left %v", files)
	}
}

// The TUI over the checklist: an item and a note added, an item toggled,
// q quits; the bytes outside the edits are the file's own.
func TestEditChecklist(t *testing.T) {
	path := writeTemp(t, plan)
	a, err := newApp(path, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	a.doc.View(80, 20, *a.m.Appearance)
	a.doc.Now = func() time.Time { return time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC) }
	press(t, a, "]", "x", "x", "]", "x", "]", "x", "o")
	typeText(t, a, "Restore from trash")
	press(t, a, "esc", "n")
	typeText(t, a, "ask about retention")
	press(t, a, "esc")
	want := "# Plan\n\n## Inbox\n\n- [x] Inbox page\n  - [ ] Read/unread per turn\n- [ ] Archive and restore\n- [ ] Restore from trash\n  > **owner** 2026-10-27: ask about retention\n\n## Prompts\n\n- [ ] Saved prompts\n"
	if got := read(t, path); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := a.r.Dispatch(ochazuke.MacroKey{Key: "q"}); err != nil {
		t.Fatal(err)
	}
	if !a.r.Quit {
		t.Fatal("q quits outside insert mode")
	}
}

// Every row o-md and the markdown package add is atomic.
func TestBindingsAreAtomic(t *testing.T) {
	a, err := newApp(writeTemp(t, plan), t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if warnings, err := a.r.ValidateBindings(); err != nil || len(warnings) != 0 {
		t.Fatalf("%v %v", warnings, err)
	}
}

// :w PATH and :save-as PATH from the palette name the buffer.
func TestPaletteSaveAs(t *testing.T) {
	data := t.TempDir()
	a, err := newApp(newScratchPath(data, time.Now()), data, true)
	if err != nil {
		t.Fatal(err)
	}
	press(t, a, "o")
	typeText(t, a, "hello")
	press(t, a, "esc")
	target := filepath.Join(t.TempDir(), "named.md")
	press(t, a, ":")
	typeText(t, a, "w "+target)
	press(t, a, "enter")
	if !exists(target) || read(t, target) != "hello\n" || a.scratch {
		t.Fatalf("named %v scratch %v status %q", exists(target), a.scratch, a.m.Status)
	}
}

// g s sends the notes in standalone o-md: it opens the agent picker and
// enter queues the notes through tuios (a fake here). g s is the hinted
// key; S stays bound, and ? help lists both.
func TestGSSendsNotes(t *testing.T) {
	path := writeTemp(t, plan)
	setEnv(t, "", "josh")
	logf := fakeTuios(t, "p1")
	a, err := newApp(path, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	a.doc.View(80, 20, *a.m.Appearance)
	press(t, a, "]", "x", "n")
	typeText(t, a, "keep a trash view")
	press(t, a, "esc")
	hints := ochazuke.BindingHints(a.r, a.r.FocusID())
	if !strings.Contains(hints, "g s doc.send-notes") || strings.Contains(hints, "S doc.send-notes") {
		t.Fatalf("hints %q: want g s hinted, S not", hints)
	}
	var keys []string
	for _, row := range ochazuke.BindingHelp(a.r, a.r.FocusID()) {
		if row.Operation == "doc.send-notes" {
			keys = append(keys, row.Key)
		}
	}
	if !slices.Contains(keys, "g s") || !slices.Contains(keys, "S") {
		t.Fatalf("? help send-notes keys %v, want g s and S", keys)
	}
	press(t, a, "g", "s")
	settle(t, a, a.r.PendingCommands())
	top := a.r.Instances[a.r.TopModal()]
	if top == nil {
		t.Fatalf("g s opened no picker; status %q", a.m.Status)
	}
	press(t, a, "enter")
	settle(t, a, a.r.PendingCommands())
	if log := read(t, logf); !strings.HasPrefix(log, "queue -w p1 -s s -- Notes on") || !strings.Contains(log, "keep a trash view") {
		t.Fatalf("queued %q", log)
	}
	if !strings.Contains(read(t, path), "· sent: keep a trash view") {
		t.Fatalf("not marked sent:\n%s", read(t, path))
	}
}

// settle runs a command and the completions it answers, through the model.
func settle(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(5 * time.Second):
		return // a timer
	}
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			settle(t, a, c)
		}
	case ochazuke.Completion:
		_, next := a.m.Update(msg)
		settle(t, a, next)
		settle(t, a, a.r.PendingCommands())
	}
}
