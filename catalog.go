package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
	"ofam.dev/ochazuke/markdown"
	"ofam.dev/oden"
)

// The extension declares the file-backed document store once. The runtime
// derives the standard actions; documentActions adds domain and host operations.

const (
	paneNoun   = "pane"
	docsPageID = "docs"
	docsListID = "docs/docs"
	// rootsSetting names the directories doc list walks.
	rootsSetting = "docs.roots"
	// maxDocs caps one listing.
	maxDocs = 2000
)

// Version is the catalog's version.
const Version = "0.2.0"

// defaultRoots are docs.roots' default.
var defaultRoots = []string{"~/hub/o-plans", "~/claude/docs"}

// skipDirs are never walked.
var skipDirs = []string{".git", ".worktrees", "node_modules"}

// docColumns are the doc list's table.
var docColumns = []ochazuke.Column{
	{Title: "Title", Min: 12},
	{Title: "Root", Max: 24, Priority: 2},
	{Title: "Modified", Width: 16, Priority: 1},
	{Title: "Tasks", Align: ochazuke.AlignRight, Priority: 3},
	{Title: "Notes", Align: ochazuke.AlignRight, Priority: 3},
}

// docsPage is the Docs page as the catalog declares it.
func docsPage() ocli.CatalogPage {
	return ocli.CatalogPage{
		ID: docsPageID, Title: "Docs", Layout: ocli.LayoutSplit,
		Lists:  []ocli.CatalogList{{ID: docsListID, Noun: docNoun, Title: "Docs"}},
		Detail: &ocli.CatalogDetail{Kind: "markdown", PathField: "path"},
	}
}

// docRow is one doc list row.
type docRow struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Path      string   `json:"path"`
	Root      string   `json:"root"`
	Modified  string   `json:"modified"`
	OpenTasks int      `json:"open_tasks"`
	OpenNotes int      `json:"open_notes"`
	Cells     []string `json:"cells"`
}

// catalogApp is the runtime the command line runs the doc verbs on.
type catalogApp struct {
	r            *ochazuke.Runtime
	list         *ochazuke.Collection
	now          func() time.Time
	trash        func(string) error
	open         func(string) error
	moved        func(string, string) error
	beforeChange func(string) error
	changed      func(string) error
	deleted      func(string) error
	sendNotesUI  func(ochazuke.Ctx) ochazuke.Result
	pending      func(string) (string, bool)
	state        *documentState
	data         string
}

func newCatalogApp() (*catalogApp, error) {
	r := ochazuke.NewRuntime()
	r.Config().App = "o-md"
	c := &catalogApp{r: r, now: time.Now, trash: systemTrash}
	c.state = &documentState{dir: func() string {
		if c.data != "" {
			return c.data
		}
		return dataDir("")
	}}
	c.open = func(path string) error { return run([]string{"--data", c.state.dir(), path}, os.Stdin, os.Stdout) }
	r.SetRelationStore(c.state)
	if err := r.DeclareSetting(ochazuke.Setting{Name: rootsSetting, Type: ochazuke.SettingList, Default: slices.Clone(defaultRoots),
		Description: "The directories doc list walks for Markdown files (~ is your home); $O_MD_ROOTS (" + string(os.PathListSeparator) + " between) overrides it"}); err != nil {
		return nil, err
	}
	if err := r.DeclareSetting(ochazuke.Setting{Name: "docs.hide-archived", Type: ochazuke.SettingBoolean, Default: true,
		Description: "Hide archived documents from doc list; --archived lists only archived documents"}); err != nil {
		return nil, err
	}
	r.Instance(docsPageID, ochazuke.Kind(docNoun+"-page"))
	var err error
	if c.list, err = r.AddList(docsListID, nil, ochazuke.Parent(docsPageID), ochazuke.Columns(docColumns...)); err != nil {
		return nil, err
	}
	c.define()
	if err := ocli.DeclarePage(r, docsPage()); err != nil {
		return nil, err
	}
	if err := declarePickers(r); err != nil {
		return nil, err
	}
	return c, nil
}

// define registers the doc noun's verbs and their parameters.
func (c *catalogApp) define() {
	r := c.r
	// pane names the host's panes (send-notes' parameter): o-md owns the
	// short name, so the catalog writes "pane", not the framework's
	// ochazuke.pane, and mtm's pane picker answers it. It has no verbs
	// here, so a host mounts nothing for it.
	r.DefineOwn(paneNoun)
	r.Define(docNoun).Columns(docColumns...).Props(docProps()...).Store(c.docStore()).
		Verb("list", c.listDocs, "List the Markdown files under the docs roots (docs.roots), --root and the host's project directories ($MTM_PROJECT_DIRS: each one's top-level *.md and docs/**/*.md)").Rows(docNoun).Fetch().Quiet().
		Verb("open", c.on(c.openDoc), "Open the document in o-md's rendered Markdown editor, not an OS application").OpensPage(docsPageID).
		Verb("new", c.newDoc, "Create a doc: NAME.md under ROOT (default the first docs root), a heading of its name; no name is an untitled doc").Positional("name", "root").Creates().
		Verb("notes", c.on(c.notes), "The doc's notes: JSON records, or with --format prompt one message for an agent").Fetch().
		Verb("annotate", c.on(c.note), "Add a note under the first item holding ITEM, reply to one, or edit, resolve, unresolve or delete one by id").Positional("text").
		Verb("check", c.on(c.check(true)), "Check the first checklist item holding ITEM").Positional("item").
		Verb("uncheck", c.on(c.check(false)), "Uncheck the first checklist item holding ITEM").Positional("item").
		Verb("send-notes", c.sendNotesAction, "Send the open, unsent notes to an agent pane as one prompt (queued by tuios until the agent is at rest) and mark them sent").Positional("pane").
		// The catalog's key row: a host mounting the Docs page answers g s
		// on the doc list and in the markdown detail, and asks for the pane
		// (a noun-typed parameter) with its own picker.
		Mode("normal").
		Bind("g s", ochazuke.Do(docNoun+".send-notes"))
	params := map[string]map[string]ochazuke.Parameter{
		"open": {"id": docSubject()},
		"list": {"root": {Type: ochazuke.PropPath, Path: "dir", Description: "An additional root to walk"},
			"archived": {Type: "boolean", Description: "List only archived documents"}},
		"new": {"name": {Type: "string", Description: "The doc's name (its file NAME.md); none is untitled-TIME"},
			"root": {Type: ochazuke.PropPath, Path: "dir", New: true, Description: "The directory it goes in; default the first docs root"}},
		"notes": {"id": docSubject(), "format": {Type: "string", Enum: []string{"jsonl", "prompt"}, Description: "jsonl (records, the default) or prompt"},
			"item": {Type: "string", Description: "Only the notes under the first item holding this"}, "unresolved": {Type: "boolean"}, "unsent": {Type: "boolean"}},
		"annotate": {"id": docSubject(), "item": {Type: "string", Description: "Add a note under the first item holding this"}, "reply": {Type: "string", Description: "Reply to note ID"},
			"edit": {Type: "string", Description: "Replace note ID's text"}, "resolve": {Type: "string"}, "unresolve": {Type: "string"}, "delete": {Type: "string"},
			"text": {Type: "string", Description: "The note's text (add, reply, edit)"}, "author": {Type: "string", Description: "Sign as this name (self-declared); an agent may not sign as the owner"}},
		"check":   {"id": docSubject(), "item": {Type: "string", Required: true}},
		"uncheck": {"id": docSubject(), "item": {Type: "string", Required: true}},
		"send-notes": {"id": docSubject(), "pane": {Type: "string", Noun: paneNoun, HostNoun: paneNoun, Free: true, Description: "The tuios pane (window id or name); a host chooses one when it is missing"},
			"now": {Type: "boolean", Description: "Refuse a pane that is not at rest instead of queueing"}, "item": {Type: "string", Description: "Only the notes under the first item holding this"}},
	}
	for verb, p := range params {
		op := r.Operations.At(ochazuke.Key(docNoun + "." + verb))
		op.Parameters = p
		r.Operations.Set(op)
	}
	c.documentActions()
}

func (c *catalogApp) sendNotesAction(ctx ochazuke.Ctx) ochazuke.Result {
	if c.sendNotesUI != nil {
		return c.sendNotesUI(ctx)
	}
	return c.on(c.sendNotes)(ctx)
}

func (c *catalogApp) openDoc(path string, _ ochazuke.Arguments) (any, error) {
	if err := c.open(path); err != nil {
		return nil, err
	}
	return c.docThing(path)
}

// on runs a verb on the doc its instance names (--target PATH).
func (c *catalogApp) on(run func(path string, args ochazuke.Arguments) (any, error)) ochazuke.AppHandler {
	return func(ctx ochazuke.Ctx) ochazuke.Result {
		path, err := c.targetPath(ctx.Instance)
		if id := ochazuke.StringArg(ctx.Args, "id", ""); id != "" {
			path, err = c.state.path(id)
		}
		if err != nil {
			return ochazuke.Failed(err)
		}
		if path == "" {
			return ochazuke.Failed(errors.New("name a doc: --target PATH"))
		}
		v, err := run(path, ctx.Args)
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(v)
	}
}

// targetPath is the file a doc instance names: its Data (a row's own id)
// or its id, when it is a path.
func (c *catalogApp) targetPath(inst *ochazuke.Instance) (string, error) {
	if inst == nil {
		return "", nil
	}
	id, _ := inst.Data.(string)
	if id == "" {
		id = inst.ID
	}
	return c.state.path(id)
}

// reveal makes a doc row exist for a --target PATH.
func (c *catalogApp) reveal(target string) error {
	path, err := c.state.path(target)
	if err != nil {
		return err
	}
	return c.list.SetItems([]ochazuke.ListItem{{ID: target, Label: filepath.Base(path), Noun: docNoun, Data: target}})
}

// roots are the directories doc list walks: $O_MD_ROOTS, else docs.roots;
// then --root.
func (c *catalogApp) roots(extra string) []string {
	var raw []string
	if env, ok := os.LookupEnv("O_MD_ROOTS"); ok {
		raw = splitList(env)
	} else {
		raw = c.r.Settings().Strings(rootsSetting)
	}
	raw = append(raw, splitList(extra)...)
	var out []string
	for _, r := range raw {
		if p, err := expand(r); err == nil && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// projectDirs are the host's project directories ($MTM_PROJECT_DIRS).
func projectDirs() []string {
	var out []string
	for _, d := range splitList(os.Getenv("MTM_PROJECT_DIRS")) {
		if p, err := expand(d); err == nil && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == os.PathListSeparator || r == ',' }) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// listDocs is doc list: every root's Markdown files (recursively), every
// project directory's top-level *.md and docs/**/*.md, newest first.
func (c *catalogApp) listDocs(ctx ochazuke.Ctx) ochazuke.Result {
	archived, _ := ctx.Args["archived"].(bool)
	withArchived := archived || !c.r.Settings().Bool("docs.hide-archived")
	rows, err := c.docRows(ochazuke.StringArg(ctx.Args, "root", ""), archived, withArchived)
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(rows)
}

// docRows are the doc rows doc list answers: every root's Markdown files
// (recursively; extraRoots adds more), every project directory's
// top-level *.md and docs/**/*.md, newest first. archivedOnly lists only
// the archived ones, withArchived includes them.
func (c *catalogApp) docRows(extraRoots string, archivedOnly, withArchived bool) ([]docRow, error) {
	seen := map[string]bool{}
	var rows []docRow
	add := func(root, path string) {
		if seen[path] || len(rows) >= maxDocs {
			return
		}
		seen[path] = true
		if row, err := c.rawRow(root, path); err == nil {
			rows = append(rows, row)
		}
	}
	archived := archivedOnly
	for _, root := range c.roots(extraRoots) {
		if !archived {
			walkMarkdown(root, func(p string) { add(root, p) })
		}
		if withArchived {
			walkMarkdown(filepath.Join(root, ".archive"), func(p string) { add(root, p) })
		}
	}
	for _, dir := range projectDirs() {
		if !archived {
			matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
			for _, p := range matches {
				add(dir, p)
			}
			walkMarkdown(filepath.Join(dir, "docs"), func(p string) { add(dir, p) })
		}
		if withArchived {
			walkMarkdown(filepath.Join(dir, ".archive"), func(p string) { add(dir, p) })
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Modified != rows[j].Modified {
			return rows[i].Modified > rows[j].Modified
		}
		return rows[i].Path < rows[j].Path
	})
	if rows == nil {
		rows = []docRow{}
	}
	return rows, c.state.identify(rows)
}

// walkMarkdown calls fn with every *.md file under root, skipping
// skipDirs and hidden directories; a missing root is no files.
func walkMarkdown(root string, fn func(string)) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return fs.SkipAll
			}
			return nil
		}
		if d.IsDir() {
			if p != root && (slices.Contains(skipDirs, d.Name()) || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") && !strings.HasPrefix(d.Name(), ".") {
			fn(p)
		}
		return nil
	})
}

// rowOf is the doc row of the file at path under root.
func (c *catalogApp) rawRow(root, path string) (docRow, error) {
	src, err := c.readDocument(path)
	if err != nil {
		return docRow{}, err
	}
	modified := ""
	if info, err := os.Stat(path); err == nil {
		modified = info.ModTime().UTC().Format(time.RFC3339Nano)
	} else if !errors.Is(err, os.ErrNotExist) {
		return docRow{}, err
	}
	row := docRow{ID: path, Title: docTitle(src, path), Path: path, Root: root, Modified: modified}
	for _, t := range markdown.Tasks(src) {
		if !t.Checked {
			row.OpenTasks++
		}
	}
	row.OpenNotes = len(openNotes(markdown.Notes(src), false))
	row.Cells = []string{row.Title, row.Root, row.Modified, fmt.Sprint(row.OpenTasks), fmt.Sprint(row.OpenNotes)}
	return row, nil
}

func (c *catalogApp) rowOf(root, path string) (docRow, error) {
	row, err := c.rawRow(root, path)
	if err != nil {
		return row, err
	}
	rows := []docRow{row}
	if err := c.state.identify(rows); err != nil {
		return docRow{}, err
	}
	return rows[0], nil
}

func (c *catalogApp) readDocument(path string) (string, error) {
	if c.pending != nil {
		if text, ok := c.pending(path); ok {
			return text, nil
		}
	}
	return readText(path)
}

// docTitle is the first heading's text, else the file name without .md.
func docTitle(src, path string) string {
	for _, line := range strings.Split(src, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "#") {
			if h := strings.TrimSpace(strings.TrimLeft(t, "#")); h != "" {
				return h
			}
		}
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// tilde writes a path under the home directory with ~.
func tilde(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			return "~/" + rest
		}
	}
	return path
}

// rootOf is the root a path lies under, else its directory.
func (c *catalogApp) rootOf(path string) string {
	for _, r := range append(c.roots(""), projectDirs()...) {
		if strings.HasPrefix(path, r+string(filepath.Separator)) {
			return r
		}
	}
	return filepath.Dir(path)
}

// newDoc is doc new (and the standard create): record first, the file is
// made at once (a name taken gets -2, -3, ...).
func (c *catalogApp) newDoc(ctx ochazuke.Ctx) ochazuke.Result {
	row, err := c.makeDoc(ochazuke.StringArg(ctx.Args, "name", ""), ochazuke.StringArg(ctx.Args, "root", ""))
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(row)
}

func (c *catalogApp) makeDoc(name, root string) (docRow, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "untitled-" + c.now().Format("20060102-150405")
	}
	if strings.ContainsRune(name, filepath.Separator) {
		return docRow{}, fmt.Errorf("name %q: a name, not a path (pass root for the directory)", name)
	}
	if root == "" {
		roots := c.roots("")
		if len(roots) == 0 {
			return docRow{}, errors.New("no docs root: pass root or set " + rootsSetting)
		}
		root = roots[0]
	}
	dir, err := expand(root)
	if err != nil {
		return docRow{}, err
	}
	base := strings.TrimSuffix(name, ".md")
	path := filepath.Join(dir, base+".md")
	for n := 2; exists(path); n++ {
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.md", base, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return docRow{}, err
	}
	if err := markdown.WriteFile(path, "# "+base+"\n"); err != nil {
		return docRow{}, err
	}
	return c.rowOf(dir, path)
}

func (c *catalogApp) renameDoc(path string, args ochazuke.Arguments) (any, error) {
	to := strings.TrimSpace(ochazuke.StringArg(args, "to", ""))
	if to == "" {
		return nil, errors.New("rename needs to")
	}
	var dest string
	if strings.ContainsRune(to, filepath.Separator) || strings.HasPrefix(to, "~") {
		p, err := expand(to)
		if err != nil {
			return nil, err
		}
		dest = p
	} else {
		dest = filepath.Join(filepath.Dir(path), to)
	}
	if filepath.Ext(dest) == "" {
		dest += ".md"
	}
	if dest == path {
		return c.rowOf(c.rootOf(path), path)
	}
	if exists(dest) {
		return nil, fmt.Errorf("%s exists; not renamed", tilde(dest))
	}
	if !exists(path) {
		return nil, fmt.Errorf("%s: no such file", tilde(path))
	}
	if c.beforeChange != nil {
		if err := c.beforeChange(path); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(path, dest); err != nil {
		return nil, err
	}
	if err := c.state.move(path, dest); err != nil {
		return nil, errors.Join(err, os.Rename(dest, path))
	}
	if c.moved != nil {
		if err := c.moved(path, dest); err != nil {
			return nil, err
		}
	}
	return c.rowOf(c.rootOf(dest), dest)
}

// moveToArchive moves a doc to ROOT/.archive (keeping its relative path),
// or, with restore, back; a move that is already done answers the row.
func (c *catalogApp) moveToArchive(path string, restore bool) (any, error) {
	root, inArchive := c.archiveRoot(path)
	if restore && !inArchive {
		return nil, errors.New("restore needs a document under .archive")
	}
	if !restore && inArchive {
		return c.rowOf(root, path)
	}
	from, to := root, filepath.Join(root, ".archive")
	if restore {
		from, to = to, from
	}
	rel, err := filepath.Rel(from, path)
	if err != nil {
		return nil, err
	}
	return c.renameDoc(path, ochazuke.Arguments{"to": filepath.Join(to, rel)})
}

func (c *catalogApp) deleteDoc(path string, _ ochazuke.Arguments) (any, error) {
	if c.beforeChange != nil {
		if err := c.beforeChange(path); err != nil {
			return nil, err
		}
	}
	if err := c.trash(path); err != nil {
		return nil, err
	}
	if err := c.state.markTrashed(path); err != nil {
		return nil, err
	}
	if c.deleted != nil {
		if err := c.deleted(path); err != nil {
			return nil, err
		}
	}
	return map[string]string{"trashed": path}, nil
}

// argsOf is a verb's arguments as the legacy commands' flags.
func argsOf(a ochazuke.Arguments, names ...string) args {
	out := args{flags: map[string]string{}}
	for _, n := range names {
		switch v := a[n].(type) {
		case string:
			if v != "" {
				out.flags[n] = v
			}
		case bool:
			if v {
				out.flags[n] = "true"
			}
		}
	}
	return out
}

func (c *catalogApp) notes(path string, args ochazuke.Arguments) (any, error) {
	a := argsOf(args, "item", "unresolved", "unsent")
	src, err := readText(path)
	if err != nil {
		return nil, err
	}
	notes, err := selectNotes(src, a)
	if err != nil {
		return nil, err
	}
	switch ochazuke.StringArg(args, "format", "jsonl") {
	case "prompt":
		from, _, err := identity("")
		if err != nil {
			return nil, err
		}
		return compose(path, src, from, openNotes(notes, false)), nil
	case "jsonl", "json", "":
		out := make([]noteRecord, 0, len(notes))
		for _, n := range notes {
			out = append(out, noteRecord{Note: n, SelfDeclared: selfDeclared(n.Author)})
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown format %q (jsonl or prompt)", ochazuke.StringArg(args, "format", ""))
}

func (c *catalogApp) note(path string, args ochazuke.Arguments) (any, error) {
	s := func(n string) string { return ochazuke.StringArg(args, n, "") }
	op := noteOp{Item: s("item"), Reply: s("reply"), Edit: s("edit"), Resolve: s("resolve"), Unresolve: s("unresolve"), Delete: s("delete"), Text: s("text"), Author: s("author")}
	if (op.Item != "" || op.Reply != "" || op.Edit != "") && strings.TrimSpace(op.Text) == "" {
		return nil, errors.New("no note text")
	}
	id, err := changeNote(path, op)
	if err != nil {
		return nil, err
	}
	return map[string]string{"path": path, "id": id}, nil
}

func (c *catalogApp) check(on bool) func(string, ochazuke.Arguments) (any, error) {
	return func(path string, args ochazuke.Arguments) (any, error) {
		item := ochazuke.StringArg(args, "item", "")
		if err := checkItem(path, item, on); err != nil {
			return nil, err
		}
		return map[string]any{"path": path, "item": item, "checked": on}, nil
	}
}

func (c *catalogApp) sendNotes(path string, args ochazuke.Arguments) (any, error) {
	pane := ochazuke.StringArg(args, "pane", "")
	if pane == "" {
		return nil, errors.New("send-notes needs a pane (o-md doc send-notes PANE --target PATH)")
	}
	now, _ := args["now"].(bool)
	n, err := sendNotes(path, pane, now, argsOf(args, "item"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "pane": pane, "sent": n}, nil
}

// isShellCommand reports whether argv is for the catalog's command line
// (catalog, a noun of it) rather than a file to open. A command word wins
// over a file of the same name in the directory; ./NAME opens the file. The
// nouns come from the catalog runtime, not from a list of words.
func isShellCommand(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	if argv[0] == "catalog" {
		return true
	}
	c, err := newCatalogApp()
	return err == nil && c.r.Noun(argv[0]) != nil
}

// runShell runs one catalog command (o-md catalog, o-md doc VERB ...).
func runShell(argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	c, err := newCatalogApp()
	if err != nil {
		return err
	}
	argv, c.data, err = splitDataArgs(argv)
	if err != nil {
		return err
	}
	sh := ocli.New(c.r, "o-md", stdin, stdout, stderr)
	sh.Version = Version
	sh.Reveal = c.reveal
	app := oden.New("o-md", argv, oden.WithBuiltins(), oden.WithStdio(stdin, stdout, stderr))
	ocli.Mount(app, sh)
	return app.Run(context.Background())
}
