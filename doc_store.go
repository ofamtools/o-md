package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"ofam.dev/ochazuke"
)

// The doc noun is a thing noun (DESIGN-001) over the Markdown files: the
// key is the file's path; name (the label) is the file name, and writing it
// renames the file (set @label=NEW, what doc rename was: a name in the same
// directory, or a path); archived (the archive role) is whether the file
// lies under ROOT/.archive, and writing it moves the file there or back
// (archive and set archived=false, what doc archive and doc restore were).
// Both moves refuse to overwrite a file. The standard create is doc new,
// delete removes the file (asking first). list, show and new stay named
// verbs: list takes roots and --archived, show answers the row.

func docProps() []ochazuke.PropSpec {
	text := func(name, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: desc}}
	}
	return []ochazuke.PropSpec{
		{Name: "id", Key: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The file's path (~ is your home)"}},
		{Name: "name", Role: ochazuke.RoleLabel, Human: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The file's name; writing it renames the file (a bare name stays in the directory, a path moves it; .md is added)", Free: true}},
		{Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean, Description: "Whether the file lies under ROOT/.archive; writing it moves the file there or back, never over an existing file"}},
		text("title", "The first heading, else the file name"),
		text("path", "The file's path"),
		text("root", "The docs root or project directory it lies under"),
		text("modified", "When the file was last changed"),
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
	th["id"] = path
	th["name"] = filepath.Base(path)
	th["archived"] = in
	return th, nil
}

func (c *catalogApp) docStore() ochazuke.Store {
	return ochazuke.Store{
		Kind: ochazuke.Collection,
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			path, err := expand(id)
			if err != nil {
				return nil, err
			}
			if !exists(path) {
				return nil, fmt.Errorf("%s: no such file", tilde(path))
			}
			return c.docThing(path)
		},
		List: func(_ ochazuke.Ctx, q ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			root, _ := q.Args["root"].(string)
			rows := c.docRows(root, false, true)
			out := make([]ochazuke.Thing, 0, len(rows))
			for _, row := range rows {
				th, err := c.docThing(row.Path)
				if err != nil {
					continue
				}
				out = append(out, th)
			}
			return out, nil
		},
		ListParams: map[string]ochazuke.Parameter{
			"root": {Type: "string", Description: "More roots to walk"},
		},
		Create: func(_ ochazuke.Ctx, t ochazuke.Thing) (ochazuke.Thing, error) {
			name, _ := t["name"].(string)
			root, _ := t["root"].(string)
			row, err := c.makeDoc(name, root)
			if err != nil {
				return nil, err
			}
			return c.docThing(row.Path)
		},
		Patch: func(_ ochazuke.Ctx, id string, t ochazuke.Thing) (ochazuke.Thing, error) {
			path, err := expand(id)
			if err != nil {
				return nil, err
			}
			if !exists(path) {
				return nil, fmt.Errorf("%s: no such file", tilde(path))
			}
			// Archive first: the name is then the file in its new place.
			if v, ok := t["archived"]; ok {
				want, _ := v.(bool)
				if _, in := c.archiveRoot(path); in != want {
					if _, err := c.moveToArchive(path, !want); err != nil {
						return nil, err
					}
					path = c.archivedPath(path, !want)
				}
			}
			if v, ok := t["name"]; ok {
				to, _ := v.(string)
				if to == "" {
					return nil, errors.New("a doc needs a name")
				}
				if _, err := c.renameDoc(path, ochazuke.Arguments{"to": to}); err != nil {
					return nil, err
				}
				path = c.renamedPath(path, to)
			}
			return c.docThing(path)
		},
		Delete: func(_ ochazuke.Ctx, id string) error {
			path, err := expand(id)
			if err != nil {
				return err
			}
			_, err = c.deleteDoc(path, nil)
			return err
		},
		Confirm: map[string]string{"delete": "Delete {{label}}? The file is removed from disk."},
	}
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
