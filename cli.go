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

	"ofam.dev/ochazuke/markdown"
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

// noteOp is one change to a file's notes: exactly one of Item (add a
// note under the first item holding it), Reply, Edit, Resolve, Unresolve
// or Delete (a note id); Text for add, reply and edit. Author is the
// --author given ("" for the caller's own identity).
type noteOp struct {
	Item, Reply, Edit, Resolve, Unresolve, Delete string
	Text, Author                                  string
}

// changeNote applies op to the file at path and answers the id of a note
// it added ("" for the other changes).
func changeNote(path string, op noteOp) (string, error) {
	src, err := readText(path)
	if err != nil {
		return "", err
	}
	caller, _, err := identity(op.Author)
	if err != nil {
		return "", err
	}
	var next string
	var at int
	switch {
	case op.Resolve != "", op.Unresolve != "":
		n, err := noteByID(src, op.Resolve+op.Unresolve)
		if err != nil {
			return "", err
		}
		if err := mayChange(caller, n); err != nil {
			return "", err
		}
		next, err = markdown.SetResolved(src, markdown.Elements(src)[n.Element], op.Resolve != "")
		if err != nil {
			return "", err
		}
	case op.Delete != "":
		n, err := noteByID(src, op.Delete)
		if err != nil {
			return "", err
		}
		if err := mayChange(caller, n); err != nil {
			return "", err
		}
		next = deleteNote(src, n)
	case op.Edit != "":
		n, err := noteByID(src, op.Edit)
		if err != nil {
			return "", err
		}
		if err := mayChange(caller, n); err != nil {
			return "", err
		}
		if next, err = editNote(src, n, op.Text); err != nil {
			return "", err
		}
	case op.Reply != "":
		n, err := noteByID(src, op.Reply)
		if err != nil {
			return "", err
		}
		if next, at, err = markdown.ReplyNote(src, n.Element, caller, today(), op.Text); err != nil {
			return "", err
		}
	case op.Item != "":
		item, err := markdown.FindItem(src, op.Item)
		if err != nil {
			return "", err
		}
		if next, at, err = markdown.AddNote(src, item, caller, today(), op.Text); err != nil {
			return "", err
		}
	default:
		return "", errors.New("a note change needs item, reply, edit, resolve, unresolve or delete")
	}
	if err := markdown.WriteFile(path, next); err != nil {
		return "", err
	}
	if at > 0 {
		for _, n := range markdown.Notes(next) {
			if e := markdown.Elements(next)[n.Element]; e.Start == at {
				return n.ID, nil
			}
		}
	}
	return "", nil
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
	op := noteOp{Item: a.flags["item"], Reply: a.flags["reply"], Edit: a.flags["edit"], Resolve: a.flags["resolve"], Unresolve: a.flags["unresolve"], Delete: a.flags["delete"], Author: a.flags["author"]}
	if !a.has("item") && !a.has("reply") && !a.has("edit") && !a.has("resolve") && !a.has("unresolve") && !a.has("delete") {
		return errors.New("o-md note needs --item MATCH, --reply ID, --edit ID, --resolve ID, --unresolve ID or --delete ID")
	}
	if op.Item != "" || op.Reply != "" || op.Edit != "" {
		if op.Text, err = a.text(stdin); err != nil {
			return err
		}
	}
	id, err := changeNote(path, op)
	if err != nil {
		return err
	}
	if id != "" {
		fmt.Fprintln(stdout, id)
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
		from, _, err := identity(a.flags["author"])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, compose(path, src, from, openNotes(notes, false)))
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
	return checkItem(path, a.flags["item"], check)
}

// checkItem checks or unchecks the first checklist item holding match.
func checkItem(path, match string, check bool) error {
	src, err := readText(path)
	if err != nil {
		return err
	}
	item, err := markdown.FindItem(src, match)
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

// mtm is the mtm command ($O_MD_MTM overrides it, for tests).
func mtm() string {
	if t := os.Getenv("O_MD_MTM"); t != "" {
		return t
	}
	return "mtm"
}

// agent is one agent pane as mtm agent scan reports it.
type agent struct {
	ID      string `json:"window_id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Ready   bool   `json:"ready"`
	Harness string `json:"harness_id"`
	Session string `json:"session"`
	Cwd     string `json:"cwd"`
}

// agents lists the agent panes of every session (mtm agent scan).
func agents() ([]agent, error) {
	out, err := exec.Command(mtm(), "agent", "scan", "--all_sessions", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("mtm agent scan: %w", err)
	}
	var doc struct {
		Agents []agent `json:"agents"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("mtm agent scan: %w", err)
	}
	return doc.Agents, nil
}

// deliver types prompt into the agent's pane with mtm pane send-text (a
// paste, then enter). The daemon refuses a pane blocked on a prompt, so it is
// never typed over a question the agent waits on; unlike the older queue it
// does not wait for the agent to come to rest. With now, a pane that is not at
// rest refuses before anything is sent.
func deliver(pane, prompt string, now bool) error {
	session, window := "", pane
	list, err := agents()
	if err != nil && now {
		return err
	}
	for _, a := range list {
		if a.ID == pane || a.Name == pane {
			session, window = a.Session, a.ID
			if now && !a.Ready {
				return fmt.Errorf("pane %s is not at rest (%s); not sent (without --now it is queued)", pane, a.State)
			}
		}
	}
	argv := []string{"pane", "send-text", "--window", window}
	if session != "" {
		argv = append(argv, "--session", session)
	}
	argv = append(argv, "--paste", "--submit", "--text", prompt)
	cmd := exec.Command(mtm(), argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mtm pane send-text: %v: %s", err, strings.TrimSpace(stderr.String()))
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
	if a.has("dry-run") {
		prompt, _, err := sendPrompt(path, a)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, prompt)
		return err
	}
	pane := a.flags["pane"]
	if pane == "" {
		return errors.New("o-md send needs --pane ID (or --dry-run)")
	}
	_, err = sendNotes(path, pane, a.has("now"), a)
	return err
}

// sendPrompt is the prompt of the open, unsent notes the args select
// (--item) and their ids.
func sendPrompt(path string, a args) (string, []string, error) {
	src, err := readText(path)
	if err != nil {
		return "", nil, err
	}
	notes, err := selectNotes(src, a)
	if err != nil {
		return "", nil, err
	}
	notes = openNotes(notes, true)
	if len(notes) == 0 {
		return "", nil, errors.New("no open, unsent notes to send")
	}
	from, _, err := identity(a.flags["author"])
	if err != nil {
		return "", nil, err
	}
	ids := make([]string, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	return compose(path, src, from, notes), ids, nil
}

// sendNotes sends the open, unsent notes to pane through mtm and
// marks them sent; it answers how many it sent.
func sendNotes(path, pane string, now bool, a args) (int, error) {
	prompt, ids, err := sendPrompt(path, a)
	if err != nil {
		return 0, err
	}
	if err := deliver(pane, prompt, now); err != nil {
		return 0, err
	}
	// The file as it is now, so an edit made meanwhile is kept.
	src, err := readText(path)
	if err != nil {
		return 0, err
	}
	next, err := markSent(src, ids)
	if err != nil {
		return 0, err
	}
	return len(ids), markdown.WriteFile(path, next)
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
