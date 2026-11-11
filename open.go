package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"ofam.dev/ochazuke"
)

// The entry points are verbs: everything the bare command line does (open a
// file, a new scratch buffer, the newest scratch buffer, standard input) is
// doc.open or doc.reopen, and the bare forms in main.go only call them.

// openTarget is the buffer doc.open opens: a file that may not exist yet (a
// new, named, unsaved buffer, written on its first change) or a scratch
// buffer, unnamed until save-as.
type openTarget struct {
	Path    string
	Scratch bool
	// Origin names where a buffer's first text came from when it was not
	// its own file ("stdin").
	Origin string
}

// record is what doc.open answers: enough to name the buffer again, never
// the text.
func (t openTarget) record() map[string]any {
	out := map[string]any{"path": t.Path, "name": filepath.Base(t.Path), "scratch": t.Scratch, "saved": exists(t.Path)}
	if t.Origin != "" {
		out["origin"] = t.Origin
	}
	return out
}

// docInput is where a new buffer's text is read from. It is the one place
// the library's shared input-source parameter (a file, -, !COMMAND, a
// register) replaces: openInput only needs a name and a Reader.
type docInput struct {
	Origin string
	Reader io.Reader
}

// stdinWord is the path that means standard input.
const stdinWord = "-"

// inputOf is the input a path word names, when it names one.
func (c *catalogApp) inputOf(word string) (docInput, bool) {
	if word != stdinWord {
		return docInput{}, false
	}
	return docInput{Origin: "stdin", Reader: c.stdin}, true
}

// newScratch is a new scratch buffer's target: nothing on disk until its
// first change.
func (c *catalogApp) newScratch() openTarget {
	return openTarget{Path: newScratchPath(c.state.dir(), c.now()), Scratch: true}
}

// openInput reads in into a new scratch buffer. Text that was read is
// written at once, so input is never lost; no input is an empty buffer.
func (c *catalogApp) openInput(in docInput) (openTarget, error) {
	t := c.newScratch()
	t.Origin = in.Origin
	if in.Reader == nil {
		return t, nil
	}
	b, err := io.ReadAll(in.Reader)
	if err != nil {
		return t, fmt.Errorf("read %s: %w", in.Origin, err)
	}
	if len(b) == 0 {
		return t, nil
	}
	return t, lazySource(t.Path).Save(string(b))
}

// openTargetOf is what a doc.open request names: its path (an input word
// reads that input), else its id, else the doc row it was run on, else a
// new scratch buffer. The buffer that is already open is not a subject, so
// open with nothing named is a new scratch buffer everywhere.
func (c *catalogApp) openTargetOf(ctx ochazuke.Ctx) (openTarget, error) {
	if word := ochazuke.StringArg(ctx.Args, "path", ""); word != "" {
		if in, ok := c.inputOf(word); ok {
			return c.openInput(in)
		}
		path, err := expand(word)
		return openTarget{Path: path}, err
	}
	if id := ochazuke.StringArg(ctx.Args, "id", ""); id != "" {
		path, err := c.state.path(id)
		return openTarget{Path: path}, err
	}
	if ctx.Instance != nil && ctx.Instance.Parent == docsListID {
		path, err := c.targetPath(ctx.Instance)
		return openTarget{Path: path}, err
	}
	return c.newScratch(), nil
}

func (c *catalogApp) openAction(ctx ochazuke.Ctx) ochazuke.Result {
	t, err := c.openTargetOf(ctx)
	if err != nil {
		return ochazuke.Failed(err)
	}
	return c.opened(t)
}

// reopenAction opens the newest scratch buffer; with none it opens a new
// one rather than refusing.
func (c *catalogApp) reopenAction(ochazuke.Ctx) ochazuke.Result {
	files, err := scratchFiles(c.state.dir())
	if err != nil {
		return ochazuke.Failed(err)
	}
	if len(files) == 0 {
		return c.opened(c.newScratch())
	}
	return c.opened(openTarget{Path: files[0].Path, Scratch: true})
}

// opened opens t and answers its record. The editor shows the buffer
// itself, so there the answer is nothing (c.shown).
func (c *catalogApp) opened(t openTarget) ochazuke.Result {
	if err := c.open(t); err != nil {
		return ochazuke.Failed(err)
	}
	if c.shown {
		return ochazuke.Done(nil)
	}
	return ochazuke.Done(t.record())
}

// scratchRows are the scratch buffers as doc rows, newest first.
func (c *catalogApp) scratchRows() ([]docRow, error) {
	dir := c.state.dir()
	files, err := scratchFiles(dir)
	if err != nil {
		return nil, err
	}
	rows := make([]docRow, 0, len(files))
	for _, f := range files {
		if row, err := c.rawRow(scratchDir(dir), f.Path); err == nil {
			rows = append(rows, row)
		}
	}
	return rows, c.state.identify(rows)
}

// editorLaunch is how the command line starts the editor for doc.open.
type editorLaunch struct {
	// Smoke prints one drawn frame and ends (a test's view of the editor).
	Smoke  bool
	Stdout io.Writer
}

// launch is the command line's open: the editor on the terminal. Without a
// terminal to draw on (a host's job, a pipe) nothing starts and doc.open
// answers the buffer's record.
func (c *catalogApp) launch(l editorLaunch) func(openTarget) error {
	return func(t openTarget) error {
		if !l.Smoke && !isTerminal(l.Stdout) {
			return nil
		}
		app, err := newApp(t.Path, c.state.dir(), t.Scratch)
		if err != nil {
			return err
		}
		if l.Smoke {
			app.m.Width, app.m.Height = 80, 24
			_, err := fmt.Fprintln(l.Stdout, ansi.Strip(app.m.View().Content))
			return err
		}
		_, err = tea.NewProgram(app.m).Run()
		if lerr := app.doc.Leave(); err == nil {
			err = lerr
		}
		return err
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}
