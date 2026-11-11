package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"os"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/markdown"
)

// The document store owns filesystem effects; the runtime supplies the
// standard verbs, forms, history and archive-first deletion from this declaration.

func docProps() []ochazuke.PropSpec {
	text := func(name, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: desc}}
	}
	return []ochazuke.PropSpec{
		{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "Stable document identity; path selectors are also accepted"}},
		{Name: "name", Role: ochazuke.RoleLabel, Parameter: ochazuke.Parameter{Type: "string", Description: "The file's name; writing it renames the file (a bare name stays in the directory, a path moves it; .md is added)", Free: true}},
		{Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean, Description: "Whether the file lies under ROOT/.archive; writing it moves the file there or back, never over an existing file"}},
		text("title", "The first heading, else the file name"),
		{Name: "path", Human: true, Parameter: ochazuke.Parameter{Type: ochazuke.PropPath, Path: "file", New: true, Description: "The document file; changing it moves the actual file without overwriting"}},
		{Name: "root", CreateOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.PropPath, Path: "dir", New: true, Description: "The docs root or project directory"}},
		{Name: "modified", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.PropTime, Description: "When the file was last changed"}},
		{Name: "content", Multiline: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The complete Markdown text; each edit writes the file"}},
		{Name: "open_tasks", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingInteger, Description: "Unchecked checklist items"}},
		{Name: "open_notes", ReadOnly: true, Parameter: ochazuke.Parameter{Type: ochazuke.SettingInteger, Description: "Unresolved notes"}},
	}
}

// archiveRoot is the root of a path under .archive (inArchive) or the root
// the path lies under.
func (c *catalogApp) archiveRoot(path string) (root string, inArchive bool) {
	for dir := filepath.Dir(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if filepath.Base(dir) == ".archive" {
			return filepath.Dir(dir), true
		}
	}
	return c.rootOf(path), false
}

func (c *catalogApp) docThing(path string) (ochazuke.Thing, error) {
	root, in := c.archiveRoot(path)
	if !in {
		root = c.rootOf(path)
	}
	row, err := c.rowOf(root, path)
	if err != nil {
		return nil, err
	}
	th, err := ochazuke.ThingOf(row)
	if err != nil {
		return nil, err
	}
	delete(th, "cells")
	th["name"] = filepath.Base(path)
	th["archived"] = in
	src, err := c.readDocument(path)
	if err != nil {
		return nil, err
	}
	th["content"] = src
	return th, nil
}

func (c *catalogApp) docStore() ochazuke.Store {
	return ochazuke.Store{
		Kind: ochazuke.KindCollection,
		Undo: true, AcceptsKey: true,
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			path, err := c.state.path(id)
			if err != nil {
				return nil, err
			}
			return c.docThing(path)
		},
		List: func(_ ochazuke.Ctx, q ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			root, _ := q.Args["root"].(string)
			rows, err := c.docRows(root, false, true)
			if err != nil {
				return nil, err
			}
			out := make([]ochazuke.Thing, 0, len(rows))
			for _, row := range rows {
				th, err := ochazuke.ThingOf(row)
				if err != nil {
					return nil, err
				}
				delete(th, "cells")
				th["name"] = filepath.Base(row.Path)
				_, th["archived"] = c.archiveRoot(row.Path)
				out = append(out, th)
			}
			return out, nil
		},
		ListParams: map[string]ochazuke.Parameter{
			"root": {Type: ochazuke.PropPath, Path: "dir", Description: "Another root to walk"},
		},
		Create: func(_ ochazuke.Ctx, t ochazuke.Thing) (ochazuke.Thing, error) {
			return c.createThing(t)
		},
		Patch: c.patchThing,
		Delete: func(_ ochazuke.Ctx, id string) error {
			path, err := c.state.path(id)
			if err != nil {
				return err
			}
			_, err = c.deleteDoc(path, nil)
			return err
		},
		Confirm: map[string]string{"delete": "Move {{label}} to the system trash? The actual document file leaves its current location."},
	}
}

func (c *catalogApp) patchThing(_ ochazuke.Ctx, id string, t ochazuke.Thing) (ochazuke.Thing, error) {
	path, err := c.state.path(id)
	if err != nil {
		return nil, err
	}
	pending := false
	if c.pending != nil {
		_, pending = c.pending(path)
	}
	onDisk := exists(path)
	if !onDisk && !pending {
		return nil, fmt.Errorf("%s: no such file", tilde(path))
	}
	if c.beforeChange != nil {
		if err := c.beforeChange(path); err != nil {
			return nil, err
		}
	}
	destination, err := c.patchDestination(path, t)
	if err != nil {
		return nil, err
	}
	if destination != path {
		if _, err := c.renameDoc(path, ochazuke.Arguments{"to": destination}); err != nil {
			return nil, err
		}
		path = destination
	}
	if v, ok := t["content"]; ok {
		src, _ := v.(string)
		if !onDisk {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
		}
		if err := markdown.WriteFile(path, src); err != nil {
			return nil, err
		}
	}
	if c.changed != nil {
		if err := c.changed(path); err != nil {
			return nil, err
		}
	}
	return c.docThing(path)
}

func (c *catalogApp) createThing(t ochazuke.Thing) (ochazuke.Thing, error) {
	name, _ := t["name"].(string)
	root, _ := t["root"].(string)
	source, hasContent := t["content"].(string)
	var content *string
	if hasContent {
		content = &source
	}
	var path string
	if id, _ := t["id"].(string); id != "" {
		var err error
		path, err = c.state.restore(id, source)
		if err != nil {
			return nil, err
		}
	} else {
		if dest, _ := t["path"].(string); dest != "" {
			path, err := expand(dest)
			if err != nil {
				return nil, err
			}
			root = filepath.Dir(path)
			if name == "" {
				name = filepath.Base(path)
			}
		}
		archived, _ := t["archived"].(bool)
		var err error
		root, err = c.creationDirectory(root, archived)
		if err != nil {
			return nil, err
		}
		row, err := c.makeDoc(name, root, content)
		if err != nil {
			return nil, err
		}
		path = row.Path
	}
	if c.changed != nil {
		if err := c.changed(path); err != nil {
			return nil, err
		}
	}
	return c.docThing(path)
}

func (c *catalogApp) patchDestination(path string, props ochazuke.Thing) (string, error) {
	dest := path
	for _, field := range []string{"path", "name"} {
		if raw, ok := props[field]; ok {
			value, _ := raw.(string)
			value = strings.TrimSpace(value)
			if value == "" {
				return "", fmt.Errorf("a doc needs a %s", field)
			}
			dest = c.renamedPath(dest, value)
		}
	}
	if raw, ok := props["archived"]; ok {
		archived, _ := raw.(bool)
		if _, in := c.archiveRoot(dest); in != archived {
			dest = c.archivedPath(dest, !archived)
		}
	}
	return dest, nil
}

func (c *catalogApp) creationDirectory(root string, archived bool) (string, error) {
	if root == "" {
		roots := c.roots("")
		if len(roots) == 0 {
			return "", errors.New("no docs root: pass root or set " + rootsSetting)
		}
		root = roots[0]
	}
	dir, err := expand(root)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "document.md")
	if _, in := c.archiveRoot(path); in != archived {
		path = c.archivedPath(path, !archived)
	}
	return filepath.Dir(path), nil
}

// renamedPath is where renameDoc puts path for to.
func (c *catalogApp) renamedPath(path, to string) string {
	dest := filepath.Join(filepath.Dir(path), to)
	if p, err := expand(to); err == nil && (filepath.IsAbs(to) || to[0] == '~' || containsSep(to)) {
		dest = p
	}
	if filepath.Ext(dest) == "" {
		dest += ".md"
	}
	return dest
}

func containsSep(s string) bool {
	for _, r := range s {
		if r == filepath.Separator {
			return true
		}
	}
	return false
}

// archivedPath is where moveToArchive puts path.
func (c *catalogApp) archivedPath(path string, restore bool) string {
	root, in := c.archiveRoot(path)
	from, to := root, filepath.Join(root, ".archive")
	if restore && in {
		from, to = to, from
	}
	rel, _ := filepath.Rel(from, path)
	return filepath.Join(to, rel)
}
