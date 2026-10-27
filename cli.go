package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/unixsuperhero/ochazuke/markdown"
)

// args are a command's words: flags anywhere (--name value, --name=value,
// a boolean alone), the rest positional; "--" ends the flags.
type args struct {
	flags map[string]string
	pos   []string
}

func parseArgs(in []string, bools ...string) (args, error) {
	a := args{flags: map[string]string{}}
	isBool := map[string]bool{}
	for _, b := range bools {
		isBool[b] = true
	}
	for i := 0; i < len(in); i++ {
		w := in[i]
		if w == "--" {
			a.pos = append(a.pos, in[i+1:]...)
			break
		}
		if !strings.HasPrefix(w, "--") && !(strings.HasPrefix(w, "-") && len(w) == 2) {
			a.pos = append(a.pos, w)
			continue
		}
		name := strings.TrimLeft(w, "-")
		if k, v, ok := strings.Cut(name, "="); ok {
			a.flags[k] = v
			continue
		}
		if isBool[name] {
			a.flags[name] = "true"
			continue
		}
		if i+1 >= len(in) {
			return a, fmt.Errorf("--%s needs a value", name)
		}
		a.flags[name] = in[i+1]
		i++
	}
	return a, nil
}

func (a args) has(name string) bool { _, ok := a.flags[name]; return ok }

// file is the command's FILE, required.
func (a args) file(cmd string) (string, error) {
	if len(a.pos) == 0 {
		return "", fmt.Errorf("o-md %s needs a FILE", cmd)
	}
	return a.pos[0], nil
}

// text is the words after FILE joined by spaces; "-" reads stdin.
func (a args) text(stdin io.Reader) (string, error) {
	words := a.pos[1:]
	if len(words) == 1 && words[0] == "-" {
		b, err := io.ReadAll(stdin)
		return string(b), err
	}
	if len(words) == 0 {
		return "", errors.New("no note text")
	}
	return strings.Join(words, " "), nil
}

func readText(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// cmdNote adds, replies to, edits, resolves, reopens or deletes a note.
func cmdNote(in []string, stdin io.Reader, stdout io.Writer) error {
	a, err := parseArgs(in)
	if err != nil {
		return err
	}
	path, err := a.file("note")
	if err != nil {
		return err
	}
	src, err := readText(path)
	if err != nil {
		return err
	}
	caller, _ := identity(a.flags["author"])
	var next string
	var at int
	switch {
	case a.has("resolve"), a.has("unresolve"):
		id := a.flags["resolve"] + a.flags["unresolve"]
		n, err := noteByID(src, id)
		if err != nil {
			return err
		}
		if err := mayChange(caller, n); err != nil {
			return err
		}
		next, err = markdown.SetResolved(src, markdown.Elements(src)[n.Element], a.has("resolve"))
		if err != nil {
			return err
		}
	case a.has("delete"):
		n, err := noteByID(src, a.flags["delete"])
		if err != nil {
			return err
		}
		if err := mayChange(caller, n); err != nil {
			return err
		}
		next = deleteNote(src, n)
	case a.has("edit"):
		n, err := noteByID(src, a.flags["edit"])
		if err != nil {
			return err
		}
		if err := mayChange(caller, n); err != nil {
			return err
		}
		text, err := a.text(stdin)
		if err != nil {
			return err
		}
		if next, err = editNote(src, n, text); err != nil {
			return err
		}
	case a.has("reply"):
		n, err := noteByID(src, a.flags["reply"])
		if err != nil {
			return err
		}
		text, err := a.text(stdin)
		if err != nil {
			return err
		}
		if next, at, err = markdown.ReplyNote(src, n.Element, caller, today(), text); err != nil {
			return err
		}
	case a.has("item"):
		item, err := markdown.FindItem(src, a.flags["item"])
		if err != nil {
			return err
		}
		text, err := a.text(stdin)
		if err != nil {
			return err
		}
		if next, at, err = markdown.AddNote(src, item, caller, today(), text); err != nil {
			return err
		}
	default:
		return errors.New("o-md note needs --item MATCH, --reply ID, --edit ID, --resolve ID, --unresolve ID or --delete ID")
	}
	if err := markdown.WriteFile(path, next); err != nil {
		return err
	}
	if at > 0 {
		for _, n := range markdown.Notes(next) {
			if e := markdown.Elements(next)[n.Element]; e.Start == at {
				fmt.Fprintln(stdout, n.ID)
			}
		}
	}
	return nil
}

// cmdNotes prints the notes as JSON Lines, or as one prompt.
func cmdNotes(in []string, stdout io.Writer) error {
	a, err := parseArgs(in, "unresolved", "unsent")
	if err != nil {
		return err
	}
	path, err := a.file("notes")
	if err != nil {
		return err
	}
	src, err := readText(path)
	if err != nil {
		return err
	}
	notes, err := selectNotes(src, a)
	if err != nil {
		return err
	}
	switch a.flags["format"] {
	case "", "jsonl", "json":
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		for _, n := range notes {
			if err := enc.Encode(noteRecord{Note: n, SelfDeclared: selfDeclared(n.Author)}); err != nil {
				return err
			}
		}
		return nil
	case "prompt":
		from, _ := identity(a.flags["author"])
		_, err := fmt.Fprintln(stdout, compose(path, src, from, openNotes(notes, false)))
		return err
	}
	return fmt.Errorf("unknown --format %q (jsonl or prompt)", a.flags["format"])
}

// selectNotes are the notes --item, --unresolved and --unsent leave.
func selectNotes(src string, a args) ([]markdown.Note, error) {
	notes := markdown.Notes(src)
	if match, ok := a.flags["item"]; ok {
		item, err := markdown.FindItem(src, match)
		if err != nil {
			return nil, err
		}
		els := markdown.Elements(src)
		var keep []markdown.Note
		for _, n := range notes {
			if els[n.Element].Item == item {
				keep = append(keep, n)
			}
		}
		notes = keep
	}
	if a.has("unresolved") || a.has("unsent") {
		notes = openNotes(notes, a.has("unsent"))
	}
	return notes, nil
}

// cmdCheck checks or unchecks the first item matching --item.
func cmdCheck(in []string, check bool) error {
	a, err := parseArgs(in)
	if err != nil {
		return err
	}
	cmd := "check"
	if !check {
		cmd = "uncheck"
	}
	path, err := a.file(cmd)
	if err != nil {
		return err
	}
	if !a.has("item") {
		return fmt.Errorf("o-md %s needs --item MATCH", cmd)
	}
	src, err := readText(path)
	if err != nil {
		return err
	}
	item, err := markdown.FindItem(src, a.flags["item"])
	if err != nil {
		return err
	}
	e := markdown.Elements(src)[item]
	if e.Kind != markdown.KindTask || e.Box < 0 {
		return fmt.Errorf("%q is not a checklist item", markdown.ItemText(src, e))
	}
	want := byte(' ')
	if check {
		want = 'x'
	}
	if (src[e.Box] != ' ') == check {
		return nil
	}
	return markdown.WriteFile(path, src[:e.Box]+string(want)+src[e.Box+1:])
}

// tuios is the tuios command ($O_MD_TUIOS overrides it, for tests).
func tuios() string {
	if t := os.Getenv("O_MD_TUIOS"); t != "" {
		return t
	}
	return "tuios"
}

// agent is one tuios agent pane as list-agents reports it.
type agent struct {
	ID      string `json:"window_id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Ready   bool   `json:"ready"`
	Harness string `json:"harness_id"`
	Session string `json:"session"`
	Cwd     string `json:"cwd"`
}

// agents lists the tuios agent panes of every session.
func agents() ([]agent, error) {
	out, err := exec.Command(tuios(), "list-agents", "--all-sessions", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("tuios list-agents: %w", err)
	}
	var doc struct {
		Agents []agent `json:"agents"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("tuios list-agents: %w", err)
	}
	return doc.Agents, nil
}

// deliver queues prompt for the agent in pane with tuios queue: typed
// when the agent is at rest, never over a prompt it waits on. With now,
// a pane that is not at rest refuses instead of queueing.
func deliver(pane, prompt string, now bool) error {
	session := ""
	list, err := agents()
	if err != nil && now {
		return err
	}
	for _, a := range list {
		if a.ID == pane || a.Name == pane {
			session = a.Session
			if now && !a.Ready {
				return fmt.Errorf("pane %s is not at rest (%s); not sent (without --now it is queued)", pane, a.State)
			}
		}
	}
	argv := []string{"queue", "-w", pane}
	if session != "" {
		argv = append(argv, "-s", session)
	}
	argv = append(argv, "--", prompt)
	cmd := exec.Command(tuios(), argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tuios queue: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// cmdSend sends the open, unsent notes to an agent pane as one prompt and
// marks them sent; --dry-run prints the prompt and changes nothing.
func cmdSend(in []string, stdout io.Writer) error {
	a, err := parseArgs(in, "now", "dry-run")
	if err != nil {
		return err
	}
	path, err := a.file("send")
	if err != nil {
		return err
	}
	src, err := readText(path)
	if err != nil {
		return err
	}
	notes, err := selectNotes(src, a)
	if err != nil {
		return err
	}
	notes = openNotes(notes, true)
	if len(notes) == 0 {
		return errors.New("no open, unsent notes to send")
	}
	from, _ := identity(a.flags["author"])
	prompt := compose(path, src, from, notes)
	if a.has("dry-run") {
		_, err := fmt.Fprintln(stdout, prompt)
		return err
	}
	pane := a.flags["pane"]
	if pane == "" {
		return errors.New("o-md send needs --pane ID (or --dry-run)")
	}
	if err := deliver(pane, prompt, a.has("now")); err != nil {
		return err
	}
	ids := make([]string, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	// The file as it is now, so an edit made meanwhile is kept.
	if src, err = readText(path); err != nil {
		return err
	}
	next, err := markSent(src, ids)
	if err != nil {
		return err
	}
	return markdown.WriteFile(path, next)
}

// cmdScratch lists the scratch buffers, newest first.
func cmdScratch(in []string, data string, stdout io.Writer) error {
	a, err := parseArgs(in, "json")
	if err != nil {
		return err
	}
	files, err := scratchFiles(data)
	if err != nil {
		return err
	}
	if a.has("json") {
		enc := json.NewEncoder(stdout)
		for _, f := range files {
			if err := enc.Encode(f); err != nil {
				return err
			}
		}
		return nil
	}
	for _, f := range files {
		fmt.Fprintf(stdout, "%s  %s  %s\n", f.Modified.Format("2006-01-02 15:04"), filepath.Base(f.Path), oneLine(f.First, 60))
	}
	return nil
}
