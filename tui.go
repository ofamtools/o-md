package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/markdown"
)

// Nouns: the buffer (o-md's page) and an agent pane notes go to.
const (
	docNoun   = "doc"
	agentNoun = "agent"
	pageID    = "doc"
	mdID      = "doc/md"
)

// App is o-md's TUI: one page holding one markdown Doc over a file, or
// over a scratch buffer until it is named.
type App struct {
	r       *ochazuke.Runtime
	m       *ochazuke.Model
	doc     *markdown.Doc
	path    string
	scratch bool
	data    string
	notice  ochazuke.NoticeID
	noticed bool
}

// newApp opens path (a missing file is created empty: record first), or
// with path "" a new scratch buffer under data, created on its first
// edit.
func newApp(path, data string, scratch bool) (*App, error) {
	c, err := newCatalogApp()
	if err != nil {
		return nil, err
	}
	c.data = data
	r := c.r
	a := &App{r: r, path: path, scratch: scratch, data: data}
	a.m = ochazuke.NewModel(r)
	var src markdown.Source
	if scratch {
		src = lazySource(path)
	} else {
		if !exists(path) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
			if err := markdown.WriteFile(path, ""); err != nil {
				return nil, err
			}
		}
		src = markdown.File(path)
	}
	r.Instance(pageID, ochazuke.Kind(docNoun)).Data = path
	doc, err := markdown.New(r, mdID, pageID, src, markdown.PreserveNewlines())
	if err != nil {
		return nil, err
	}
	a.doc = doc
	c.pending = func(path string) (string, bool) {
		if a.scratch && path == a.path {
			return a.doc.Text(), true
		}
		return "", false
	}
	c.sendNotesUI = func(ctx ochazuke.Ctx) ochazuke.Result {
		return a.sendNotes(ochazuke.StringArg(ctx.Args, "pane", ""))
	}
	c.beforeChange = func(path string) error {
		if path == a.path {
			return a.doc.Leave()
		}
		return nil
	}
	c.open = func(path string) error {
		if err := a.doc.Leave(); err != nil {
			return err
		}
		if err := a.doc.SetSource(markdown.File(path)); err != nil {
			return err
		}
		a.path, a.scratch = path, false
		a.r.Instance(pageID).Data = path
		a.retitle()
		return nil
	}
	c.moved = func(from, to string) error {
		if a.path != from {
			return nil
		}
		return c.open(to)
	}
	c.changed = func(path string) error {
		if path == a.path {
			return a.doc.SetSource(markdown.File(path))
		}
		return nil
	}
	c.deleted = func(path string) error {
		if path != a.path {
			return nil
		}
		text := a.doc.Text()
		a.scratch = false
		return a.doc.SetSource(markdown.Source{
			Load: func() (string, error) { return text, nil },
		})
	}
	a.register()
	if _, err := r.AddPage(pageID, a.name(), a.render, ochazuke.Focus(doc.FocusTarget())); err != nil {
		return nil, err
	}
	if err := r.OpenPage(pageID); err != nil {
		return nil, err
	}
	a.retitle()
	return a, nil
}

// name is what the title calls the buffer.
func (a *App) name() string {
	if a.scratch {
		return "[scratch]"
	}
	return filepath.Base(a.path)
}

// retitle shows the buffer's name in the title and, for a scratch
// buffer, a footer notice until it is named.
func (a *App) retitle() {
	a.m.Title = "o-md " + a.name()
	if page := a.r.PageOf(pageID); page != nil {
		page.Title = a.name()
	}
	if a.noticed {
		a.r.UI().ClearNotice(a.notice)
		a.noticed = false
	}
	if a.scratch {
		a.notice, a.noticed = a.r.UI().Notify("[scratch] name it with :save-as PATH"), true
	}
}

func (a *App) render(_ *ochazuke.Runtime, width, height int) ochazuke.Block {
	return a.doc.Block(width, height, *a.m.Appearance)
}

func (a *App) register() {
	saveAs := func(c ochazuke.Ctx) ochazuke.Result {
		path := ochazuke.StringArg(c.Args, "path", "")
		force, _ := c.Args["force"].(bool)
		return result(a.saveAs(path, force))
	}
	a.r.Define(docNoun).
		Verb("save-as", saveAs, "Name the buffer: write it to PATH (parent directories made) and keep editing there; a scratch buffer's file moves, a named file stays where it was. An existing PATH asks first").Positional("path").
		Verb("w", saveAs, "Save as (vim's :w PATH)").Positional("path").
		Mode("normal").
		// g s is the hinted key (the footer shows a request's first row);
		// S stays bound, and ? and the palette list both.
		Bind("g s", ochazuke.Do(docNoun+".send-notes")).
		Bind("S", ochazuke.Do(docNoun+".send-notes")).
		Bind("q", ochazuke.Do("app.quit"))
	for verb, params := range map[string]map[string]ochazuke.Parameter{
		"save-as": {"path": {Type: ochazuke.PropPath, Path: "file", New: true, Required: true, Description: "Where to write the buffer (~ is your home)"}, "force": {Type: "boolean", Description: "Overwrite an existing file"}},
		"w":       {"path": {Type: ochazuke.PropPath, Path: "file", New: true, Required: true}, "force": {Type: "boolean"}},
	} {
		op := a.r.Operations.At(ochazuke.Key(docNoun + "." + verb))
		op.Parameters = params
		a.r.Operations.Set(op)
	}
	a.r.Define(agentNoun)
}

func result(err error) ochazuke.Result {
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(nil)
}

func expand(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	return filepath.Abs(path)
}

// saveAs names the buffer path: an existing file is overwritten only
// once confirmed (a picker asks).
func (a *App) saveAs(path string, force bool) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("save-as needs a PATH")
	}
	abs, err := expand(path)
	if err != nil {
		return err
	}
	if abs == a.path {
		return nil
	}
	if exists(abs) && !force {
		_, err := a.r.OpenPicker(ochazuke.PickerSpec{Kind: "o-md-overwrite", Title: abs + " exists: overwrite it?", Rows: []ochazuke.PickerRow{
			{ID: "overwrite", Label: "overwrite " + filepath.Base(abs)},
			{ID: "cancel", Label: "cancel"},
		}, Choose: func(row ochazuke.PickerRow) ochazuke.Result {
			if row.ID != "overwrite" {
				return ochazuke.Done(nil)
			}
			return result(a.saveAs(abs, true))
		}})
		return err
	}
	if err := a.doc.Leave(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if err := markdown.WriteFile(abs, a.doc.Text()); err != nil {
		return err
	}
	old, wasScratch := a.path, a.scratch
	if err := a.doc.SetSource(markdown.File(abs)); err != nil {
		return err
	}
	a.path, a.scratch = abs, false
	a.r.Instance(pageID).Data = abs
	if wasScratch && exists(old) {
		_ = os.Remove(old) // a scratch buffer moves; a named file stays
	}
	a.retitle()
	return nil
}

// sendNotes sends the open, unsent notes to pane; without one it lists
// the agent panes (off the loop) and asks.
func (a *App) sendNotes(pane string) ochazuke.Result {
	if err := a.doc.Leave(); err != nil {
		return ochazuke.Failed(err)
	}
	notes := openNotes(markdown.Notes(a.doc.Text()), true)
	if len(notes) == 0 {
		return ochazuke.Failed(errors.New("no open, unsent notes to send"))
	}
	if pane != "" {
		return a.send(pane)
	}
	return ochazuke.Pending(func(context.Context) (any, error) { return agents() }).Label("agents").Then(func(c ochazuke.Ctx, v any, err error) {
		if err != nil {
			c.Runtime.UI().Status(err.Error())
			return
		}
		list, _ := v.([]agent)
		if len(list) == 0 {
			c.Runtime.UI().Status("no agent panes")
			return
		}
		rows := make([]ochazuke.PickerRow, len(list))
		for i, ag := range list {
			rows[i] = ochazuke.PickerRow{ID: ag.ID, Label: ag.Name + " " + ag.Harness + " " + ag.State, Data: ag.ID,
				Cells: []string{ag.Name, ag.Harness, ag.State, ag.Session, ag.Cwd}}
		}
		if _, err := c.Runtime.OpenPicker(ochazuke.PickerSpec{Kind: "o-md-agents", Title: "Send notes to", Rows: rows, Choose: func(row ochazuke.PickerRow) ochazuke.Result {
			id, _ := row.Data.(string)
			return a.send(id)
		}}); err != nil {
			c.Runtime.UI().Status(err.Error())
		}
	})
}

// send composes the open, unsent notes, queues them for pane off the
// loop and marks them sent through the Doc (one undo step).
func (a *App) send(pane string) ochazuke.Result {
	src := a.doc.Text()
	notes := openNotes(markdown.Notes(src), true)
	if len(notes) == 0 {
		return ochazuke.Failed(errors.New("no open, unsent notes to send"))
	}
	from := "owner"
	prompt := compose(a.path, src, from, notes)
	ids := make([]string, len(notes))
	for i, n := range notes {
		ids[i] = n.ID
	}
	return ochazuke.Pending(func(context.Context) (any, error) { return nil, deliver(pane, prompt, false) }).Label(pane).Then(func(c ochazuke.Ctx, _ any, err error) {
		if err != nil {
			c.Runtime.UI().Status(err.Error())
			return
		}
		if err := a.doc.Edit(func(text string) (string, int, error) {
			next, err := markSent(text, ids)
			return next, 0, err
		}); err != nil {
			c.Runtime.UI().Status(err.Error())
			return
		}
		c.Runtime.UI().Status(fmt.Sprintf("sent %d notes to %s", len(ids), pane))
	})
}
