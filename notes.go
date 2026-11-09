package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"ofam.dev/ochazuke/markdown"
)

// identity is who runs an agent command: the --author given (self
// declared), else the agent the command runs in ($MTM_PANE_ID (else $TUIOS_PANE_ID),
// when mtm agent scan lists an agent in that pane), else the owner
// ($O_MD_OWNER, else $USER). A pane that is not an agent's (a shell, mtm)
// is the owner's. An agent may not declare itself the owner.
func identity(flagAuthor string) (author string, declared bool, err error) {
	agent := callerAgent()
	if flagAuthor != "" {
		if agent != "" && isOwner(flagAuthor) {
			return "", false, fmt.Errorf("an agent (pane %s) may not sign as the owner %q", agent, flagAuthor)
		}
		return flagAuthor, true, nil
	}
	if agent != "" {
		return agent, false, nil
	}
	return owner(), false, nil
}

// callerAgent is $MTM_PANE_ID (else $TUIOS_PANE_ID) when mtm lists an agent in that pane,
// else "" (no pane, a pane without an agent, or mtm not answering).
func callerAgent() string {
	pane := paneEnv(os.Getenv, "PANE_ID")
	if pane == "" {
		return ""
	}
	list, err := agents()
	if err != nil {
		return ""
	}
	for _, a := range list {
		if a.ID == pane {
			return pane
		}
	}
	return ""
}

// owner is the owner's name: $O_MD_OWNER, else $USER, else "owner".
func owner() string {
	if o := os.Getenv("O_MD_OWNER"); o != "" {
		return o
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "owner"
}

// isOwner reports whether a note author is the owner: "owner" (the TUI's
// default signature) or the owner's name.
func isOwner(name string) bool { return name == "owner" || name == owner() }

var paneID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// selfDeclared reports whether a note's author is a name the system did
// not set: neither the owner nor a pane id.
func selfDeclared(author string) bool { return !isOwner(author) && !paneID.MatchString(author) }

// mayChange is the permission rule: the owner may change any note, anyone
// else only the notes they wrote.
func mayChange(caller string, n markdown.Note) error {
	if isOwner(caller) || n.Author == caller {
		return nil
	}
	return fmt.Errorf("note %s is %s's: only its author or the owner may change it", n.ID, n.Author)
}

// noteRecord is one line of o-md notes.
type noteRecord struct {
	markdown.Note
	SelfDeclared bool `json:"self_declared"`
}

func today() string { return time.Now().Format("2006-01-02") }

// heading is the nearest heading above element e, or "".
func heading(src string, els []markdown.Element, e int) string {
	for k := e; k >= 0; k-- {
		if els[k].Kind == markdown.KindHeading {
			line, _, _ := strings.Cut(src[els[k].Start:els[k].End], "\n")
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return ""
}

// location is where a note's item stands: its heading and its ancestor
// items, " › " between.
func location(src string, els []markdown.Element, n markdown.Note) string {
	item := els[n.Element].Item
	var ancestors []string
	for k := els[item].Item; k >= 0; k = els[k].Item {
		ancestors = append([]string{markdown.ItemText(src, els[k])}, ancestors...)
	}
	var parts []string
	if h := heading(src, els, item); h != "" {
		parts = append(parts, h)
	}
	return strings.Join(append(parts, ancestors...), " › ")
}

// oneLine is s on one line, at most limit runes (… when cut).
func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}

// compose is the notes as one numbered message an agent can be sent
// (modelled on the review compose of the older pane tool): per note its place, the item's
// text and the note's id and line, who wrote it when that is not the
// sender, then its text indented; one closing line asking for replies.
func compose(file, src, from string, notes []markdown.Note) string {
	els := markdown.Elements(src)
	var b strings.Builder
	fmt.Fprintf(&b, "Notes on %s", file)
	if from != "" {
		fmt.Fprintf(&b, ", from %s", oneLine(from, 80))
	}
	b.WriteString(":\n\n")
	for i, n := range notes {
		place := location(src, els, n)
		if place != "" {
			place += " "
		}
		fmt.Fprintf(&b, "%d. %s%q (note %s, %s:%d)\n", i+1, place, oneLine(n.Item, 120), n.ID, file, n.Line)
		if n.Author != "" && n.Author != from {
			fmt.Fprintf(&b, "   (written by %s", oneLine(n.Author, 80))
			if selfDeclared(n.Author) {
				b.WriteString(", self-declared")
			}
			b.WriteString(")\n")
		}
		for _, line := range strings.Split(n.Text, "\n") {
			b.WriteString("   " + line + "\n")
		}
	}
	fmt.Fprintf(&b, "\nAnswer each note with: o-md note %s --reply ID TEXT", file)
	return b.String()
}

// openNotes are the notes neither resolved nor (when unsent) sent.
func openNotes(notes []markdown.Note, unsent bool) []markdown.Note {
	var out []markdown.Note
	for _, n := range notes {
		if n.Resolved || unsent && n.Sent {
			continue
		}
		out = append(out, n)
	}
	return out
}

// markSent is src with the notes of ids marked sent.
func markSent(src string, ids []string) (string, error) {
	for _, id := range ids {
		e := markdown.NoteElement(src, id)
		if e < 0 {
			return "", fmt.Errorf("no note %s", id)
		}
		next, err := markdown.SetSent(src, markdown.Elements(src)[e], true)
		if err != nil {
			return "", err
		}
		src = next
	}
	return src, nil
}

// noteByID is the note with id, or an error naming the ids there are.
func noteByID(src, id string) (markdown.Note, error) {
	notes := markdown.Notes(src)
	for _, n := range notes {
		if n.ID == id {
			return n, nil
		}
	}
	var ids []string
	for _, n := range notes {
		ids = append(ids, n.ID)
	}
	if len(ids) == 0 {
		return markdown.Note{}, fmt.Errorf("no note %s: the file has no notes", id)
	}
	return markdown.Note{}, fmt.Errorf("no note %s; the notes are %s", id, strings.Join(ids, ", "))
}

// deleteNote is src without note n's lines.
func deleteNote(src string, n markdown.Note) string {
	e := markdown.Elements(src)[n.Element]
	start, end := e.Start, e.End
	if end < len(src) {
		end++
	} else if start > 0 {
		start--
	}
	return src[:start] + src[end:]
}

// editNote is src with note n's text replaced (its head kept, its sent
// marker cleared: the text is no longer what was sent).
func editNote(src string, n markdown.Note, text string) (string, error) {
	clean, err := markdown.CleanNote(text)
	if err != nil {
		return "", err
	}
	els := markdown.Elements(src)
	e := els[n.Element]
	src, err = markdown.SetSent(src, e, false)
	if err != nil {
		return "", err
	}
	e = markdown.Elements(src)[n.Element]
	raw := src[e.Start:e.End]
	first, _, _ := strings.Cut(raw, "\n")
	k := strings.IndexByte(first, '>')
	headEnd := k + 1
	if headEnd < len(first) && first[headEnd] == ' ' {
		headEnd++
	}
	body := first[headEnd:]
	if c := strings.Index(body, ":"); c >= 0 {
		headEnd += c + 1
		if headEnd < len(first) && first[headEnd] == ' ' {
			headEnd++
		}
	}
	pad := strings.Repeat(" ", e.Indent)
	lines := strings.Split(clean, "\n")
	out := first[:headEnd] + lines[0]
	for _, l := range lines[1:] {
		out += "\n" + pad + strings.TrimRight("> "+l, " ")
	}
	return src[:e.Start] + out + src[e.End:], nil
}

var errUsage = errors.New("usage")
