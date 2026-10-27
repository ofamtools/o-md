package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/unixsuperhero/ochazuke"
	ocli "github.com/unixsuperhero/ochazuke/cli"
	"github.com/unixsuperhero/ochazuke/markdown"
	"github.com/unixsuperhero/oden"
)

// The catalog: o-md as a family extension. Noun doc lists the Markdown
// files under the docs roots (rows: title, path, root, modified,
// open_tasks, open_notes) and acts on one (--target PATH): new, rename,
// delete, copy-path, show, notes, note, check, uncheck and send-notes. The
// Docs page (docsPage) is a split of the docs list beside a markdown
// detail (detail kind markdown, pathField path): a host that registers
// the kind (mtm, markdown/cliview) draws the selected file as an editable
// document. Every verb runs in this process over the files; a host runs
// `o-md doc VERB --target PATH --json ARGS --yes`.

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
	r    *ochazuke.Runtime
	list *ochazuke.List
	now  func() time.Time
}

func newCatalogApp() (*catalogApp, error) {
	r := ochazuke.NewRuntime()
	r.Config().App = "o-md"
	c := &catalogApp{r: r, now: time.Now}
	if err := r.DeclareSetting(ochazuke.Setting{Name: rootsSetting, Type: ochazuke.SettingList, Default: slices.Clone(defaultRoots),
		Description: "The directories doc list walks for Markdown files (~ is your home); $O_MD_ROOTS (" + string(os.PathListSeparator) + " between) overrides it"}); err != nil {
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
	return c, nil
}

// define registers the doc noun's verbs and their parameters.
func (c *catalogApp) define() {
	r := c.r
	// pane names the host's panes (send-notes' parameter): o-md owns the
	// short name, so the catalog writes "pane", not the framework's
	// ochazuke.pane, and mtm's pane chooser answers it. It has no verbs
	// here, so a host mounts nothing for it.
	r.DefineOwn(paneNoun)
	r.Define(docNoun).Columns(docColumns...).
		Verb("list", c.listDocs, "List the Markdown files under the docs roots (docs.roots), --root and the host's project directories ($MTM_PROJECT_DIRS: each one's top-level *.md and docs/**/*.md)").Rows(docNoun).Fetch().Quiet().
		Verb("show", c.on(c.showDoc), "Show one doc's row").Fetch().
		Verb("new", c.newDoc, "Create a doc: NAME.md under ROOT (default the first docs root), a heading of its name; no name is an untitled doc").Positional("name", "root").
		Verb("rename", c.on(c.renameDoc), "Rename a doc's file: TO is a new name in the same directory, or a path").Positional("to").
		Verb("delete", c.on(c.deleteDoc), "Delete a doc's file").Confirm("Delete {{label}}? The file is removed from disk.").
		Verb("copy-path", c.on(c.copyPath), "Copy the doc's path to the clipboard and answer it").
		Verb("notes", c.on(c.notes), "The doc's notes: JSON records, or with --format prompt one message for an agent").Fetch().
		Verb("note", c.on(c.note), "Add a note under the first item holding ITEM, reply to one, or edit, resolve, unresolve or delete one by id").Positional("text").
		Verb("check", c.on(c.check(true)), "Check the first checklist item holding ITEM").Positional("item").
		Verb("uncheck", c.on(c.check(false)), "Uncheck the first checklist item holding ITEM").Positional("item").
		Verb("send-notes", c.on(c.sendNotes), "Send the open, unsent notes to an agent pane as one prompt (queued by tuios until the agent is at rest) and mark them sent").Positional("pane").
		// The catalog's key row: a host mounting the Docs page answers g s
		// on the doc list and in the markdown detail, and asks for the pane
		// (a noun-typed parameter) with its own chooser.
		Mode("normal").
		Bind("g s", ochazuke.Do(docNoun+".send-notes"))
	params := map[string]map[string]ochazuke.Parameter{
		"list": {"root": {Type: "string", Description: "More roots to walk (" + string(os.PathListSeparator) + " or , between)"}},
		"new": {"name": {Type: "string", Description: "The doc's name (its file NAME.md); none is untitled-TIME"},
			"root": {Type: "string", Description: "The directory it goes in; default the first docs root"}},
		"rename": {"to": {Type: "string", Required: true, Description: "The new name (same directory) or path"}},
		"notes": {"format": {Type: "string", Enum: []string{"jsonl", "prompt"}, Description: "jsonl (records, the default) or prompt"},
			"item": {Type: "string", Description: "Only the notes under the first item holding this"}, "unresolved": {Type: "boolean"}, "unsent": {Type: "boolean"}},
		"note": {"item": {Type: "string", Description: "Add a note under the first item holding this"}, "reply": {Type: "string", Description: "Reply to note ID"},
			"edit": {Type: "string", Description: "Replace note ID's text"}, "resolve": {Type: "string"}, "unresolve": {Type: "string"}, "delete": {Type: "string"},
			"text": {Type: "string", Description: "The note's text (add, reply, edit)"}, "author": {Type: "string", Description: "Sign as this name (self-declared); an agent may not sign as the owner"}},
		"check":   {"item": {Type: "string", Required: true}},
		"uncheck": {"item": {Type: "string", Required: true}},
		"send-notes": {"pane": {Type: "string", Noun: paneNoun, Free: true, Description: "The tuios pane (window id or name); a host chooses one when it is missing"},
			"now": {Type: "boolean", Description: "Refuse a pane that is not at rest instead of queueing"}, "item": {Type: "string", Description: "Only the notes under the first item holding this"}},
	}
	for verb, p := range params {
		op := r.Operations[docNoun+"."+verb]
		op.Parameters = p
		r.Operations[op.Key()] = op
	}
}

// on runs a verb on the doc its instance names (--target PATH).
func (c *catalogApp) on(run func(path string, args ochazuke.Arguments) (any, error)) ochazuke.AppHandler {
	return func(ctx ochazuke.Ctx) ochazuke.Result {
		path := targetPath(ctx.Instance)
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
func targetPath(inst *ochazuke.Instance) string {
	if inst == nil || inst.Noun != docNoun {
		return ""
	}
	id, _ := inst.Data.(string)
	if id == "" {
		id = inst.ID
	}
	if p, err := expand(id); err == nil {
		return p
	}
	return ""
}

// reveal makes a doc row exist for a --target PATH.
func (c *catalogApp) reveal(target string) error {
	path, err := expand(target)
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
	seen := map[string]bool{}
	var rows []docRow
	add := func(root, path string) {
		if seen[path] || len(rows) >= maxDocs {
			return
		}
		seen[path] = true
		if row, err := c.rowOf(root, path); err == nil {
			rows = append(rows, row)
		}
	}
	for _, root := range c.roots(ochazuke.StringArg(ctx.Args, "root", "")) {
		walkMarkdown(root, func(p string) { add(root, p) })
	}
	for _, dir := range projectDirs() {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		for _, p := range matches {
			add(dir, p)
		}
		walkMarkdown(filepath.Join(dir, "docs"), func(p string) { add(dir, p) })
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
	return ochazuke.Done(rows)
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
func (c *catalogApp) rowOf(root, path string) (docRow, error) {
	info, err := os.Stat(path)
	if err != nil {
		return docRow{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return docRow{}, err
	}
	src := string(b)
	row := docRow{ID: path, Title: docTitle(src, path), Path: path, Root: tilde(root), Modified: info.ModTime().Local().Format("2006-01-02 15:04")}
	for _, t := range markdown.Tasks(src) {
		if !t.Checked {
			row.OpenTasks++
		}
	}
	row.OpenNotes = len(openNotes(markdown.Notes(src), false))
	row.Cells = []string{row.Title, row.Root, row.Modified, fmt.Sprint(row.OpenTasks), fmt.Sprint(row.OpenNotes)}
	return row, nil
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

func (c *catalogApp) showDoc(path string, _ ochazuke.Arguments) (any, error) {
	return c.rowOf(c.rootOf(path), path)
}

// newDoc is doc new: record first, the file is made at once (a name
// taken gets -2, -3, ...).
func (c *catalogApp) newDoc(ctx ochazuke.Ctx) ochazuke.Result {
	name := strings.TrimSpace(ochazuke.StringArg(ctx.Args, "name", ""))
	if name == "" {
		name = "untitled-" + c.now().Format("20060102-150405")
	}
	if strings.ContainsRune(name, filepath.Separator) {
		return ochazuke.Failed(fmt.Errorf("name %q: a name, not a path (pass root for the directory)", name))
	}
	root := ochazuke.StringArg(ctx.Args, "root", "")
	if root == "" {
		roots := c.roots("")
		if len(roots) == 0 {
			return ochazuke.Failed(errors.New("no docs root: pass root or set " + rootsSetting))
		}
		root = roots[0]
	}
	dir, err := expand(root)
	if err != nil {
		return ochazuke.Failed(err)
	}
	base := strings.TrimSuffix(name, ".md")
	path := filepath.Join(dir, base+".md")
	for n := 2; exists(path); n++ {
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.md", base, n))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ochazuke.Failed(err)
	}
	if err := markdown.WriteFile(path, "# "+base+"\n"); err != nil {
		return ochazuke.Failed(err)
	}
	row, err := c.rowOf(dir, path)
	if err != nil {
		return ochazuke.Failed(err)
	}
	return ochazuke.Done(row)
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
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(path, dest); err != nil {
		return nil, err
	}
	return c.rowOf(c.rootOf(dest), dest)
}

func (c *catalogApp) deleteDoc(path string, _ ochazuke.Arguments) (any, error) {
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	return map[string]string{"deleted": path}, nil
}

// copyPath copies the path with the platform's clipboard command when
// there is one and answers it either way.
func (c *catalogApp) copyPath(path string, _ ochazuke.Arguments) (any, error) {
	return map[string]any{"path": path, "copied": copyToClipboard(path) == nil}, nil
}

// copyToClipboard writes text to the system clipboard ($O_MD_CLIPBOARD,
// else pbcopy, wl-copy, xclip or xsel).
func copyToClipboard(text string) error {
	var argv []string
	if cmd := os.Getenv("O_MD_CLIPBOARD"); cmd != "" {
		argv = strings.Fields(cmd)
	} else {
		candidates := [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
		if runtime.GOOS == "darwin" {
			candidates = append([][]string{{"pbcopy"}}, candidates...)
		}
		for _, cand := range candidates {
			if _, err := exec.LookPath(cand[0]); err == nil {
				argv = cand
				break
			}
		}
	}
	if len(argv) == 0 {
		return errors.New("no clipboard command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
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
// (catalog, a noun of it, help of one) rather than a file to open.
func isShellCommand(argv []string) bool {
	if len(argv) == 0 || exists(argv[0]) {
		return false
	}
	switch argv[0] {
	case "catalog", docNoun, "settings", "ochazuke", "bindings", "operations", "history":
		return true
	}
	return false
}

// runShell runs one catalog command (o-md catalog, o-md doc VERB ...).
func runShell(argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	c, err := newCatalogApp()
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
