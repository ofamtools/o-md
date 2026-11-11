package main

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"ofam.dev/ochazuke"
)

func TestDocumentConcurrentCreateAndDuplicate(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		name := "create"
		if duplicate {
			name = "duplicate"
		}
		t.Run(name, func(t *testing.T) {
			root, _ := docEnv(t)
			apps := make([]*catalogApp, 2)
			texts := []string{"# First\n\nfirst original bytes\n", "# Second\n\nsecond original bytes\n"}
			sources := []string{filepath.Join(root, "first.md"), filepath.Join(root, "second.md")}
			for i := range apps {
				var err error
				apps[i], err = newCatalogApp()
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, sources[i], texts[i])
			}
			start := make(chan struct{})
			type outcome struct {
				row ochazuke.Thing
				err error
			}
			out := make(chan outcome, 2)
			for i := range apps {
				go func(i int) {
					<-start
					if duplicate {
						value, err := apps[i].r.Execute(ochazuke.Request{Noun: docNoun, Verb: "duplicate", Args: ochazuke.Arguments{"id": sources[i], "name": "contended"}})
						if err != nil {
							out <- outcome{err: err}
							return
						}
						row, err := ochazuke.ThingOf(value)
						out <- outcome{row, err}
					} else {
						row, err := apps[i].makeDoc("contended", root, &texts[i])
						thing, _ := ochazuke.ThingOf(row)
						out <- outcome{thing, err}
					}
				}(i)
			}
			close(start)
			paths := map[string]bool{}
			ids := map[string]bool{}
			contents := map[string]bool{}
			for range apps {
				result := <-out
				if result.err != nil {
					t.Fatal(result.err)
				}
				path := result.row["path"].(string)
				id := result.row["id"].(string)
				paths[path] = true
				ids[id] = true
				contents[read(t, path)] = true
			}
			if len(paths) != 2 || len(ids) != 2 || !contents[texts[0]] || !contents[texts[1]] {
				t.Fatalf("contending documents lost bytes or identity: paths=%v ids=%v content=%v", paths, ids, contents)
			}
			for i := range sources {
				if read(t, sources[i]) != texts[i] {
					t.Fatal("an original source was changed")
				}
			}
		})
	}
}

func TestDocumentConcurrentMovesDoNotReplace(t *testing.T) {
	root, _ := docEnv(t)
	sources := []string{filepath.Join(root, "first.md"), filepath.Join(root, "second.md")}
	texts := []string{"# First original\n", "# Second original\n"}
	destination := filepath.Join(root, "claimed.md")
	apps := make([]*catalogApp, 2)
	ids := make([]string, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	for i := range apps {
		writeFile(t, sources[i], texts[i])
		var err error
		apps[i], err = newCatalogApp()
		if err != nil {
			t.Fatal(err)
		}
		thing, err := apps[i].docThing(sources[i])
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = thing["id"].(string)
		apps[i].beforeChange = func(string) error { ready.Done(); <-start; return nil }
	}
	type outcome struct {
		index int
		err   error
	}
	out := make(chan outcome, 2)
	for i := range apps {
		go func(i int) {
			_, err := apps[i].renameDoc(sources[i], ochazuke.Arguments{"to": destination})
			out <- outcome{i, err}
		}(i)
	}
	ready.Wait()
	close(start)
	winner, loser := -1, -1
	for range apps {
		result := <-out
		if result.err == nil {
			if winner != -1 {
				t.Fatal("both moves claimed one destination")
			}
			winner = result.index
		} else {
			loser = result.index
		}
	}
	if winner < 0 || loser < 0 {
		t.Fatalf("winner=%d loser=%d", winner, loser)
	}
	if exists(sources[winner]) || read(t, destination) != texts[winner] || read(t, sources[loser]) != texts[loser] {
		t.Fatal("contending moves lost or replaced source bytes")
	}
	for i := range apps {
		got, err := apps[i].state.path(ids[i])
		want := sources[i]
		if i == winner {
			want = destination
		}
		if err != nil || got != want {
			t.Fatalf("identity %s points to %s, want %s: %v", ids[i], got, want, err)
		}
	}
}

func TestDocumentMetadataFailureRollsBackFilesAndRelations(t *testing.T) {
	root, _ := docEnv(t)
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "plan.md")
	original := read(t, source)
	thing, err := c.docThing(source)
	if err != nil {
		t.Fatal(err)
	}
	id := thing["id"].(string)
	executeDoc(t, c, "relate", id, ochazuke.Arguments{"to": "project:p1", "kind": "document"})
	before, err := os.ReadFile(filepath.Join(c.state.dir(), "documents.json"))
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected metadata replace failure")
	c.state.replace = func(string, string) error { return failure }
	destination := filepath.Join(root, "not-committed.md")
	if _, err := c.renameDoc(source, ochazuke.Arguments{"to": destination}); !errors.Is(err, failure) {
		t.Fatalf("move: %v", err)
	}
	if read(t, source) != original || exists(destination) {
		t.Fatal("failed metadata commit did not roll back file move")
	}
	text := "# New must roll back\n"
	if _, err := c.makeDoc("not-committed", root, &text); !errors.Is(err, failure) {
		t.Fatalf("create: %v", err)
	}
	if exists(destination) {
		t.Fatal("failed metadata commit left a new file behind")
	}
	after, err := os.ReadFile(filepath.Join(c.state.dir(), "documents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed filesystem mutation changed document IDs or relations")
	}
	c.state.replace = nil
	got, err := c.state.path(id)
	if err != nil || got != source {
		t.Fatalf("identity after rollback: %s %v", got, err)
	}
	relations, err := c.state.Relations(ochazuke.RelationQuery{Thing: ochazuke.AnyRef(docNoun, id)})
	if err != nil || len(relations) != 1 || relations[0].To != "project:p1" {
		t.Fatalf("relations after rollback: %v %v", relations, err)
	}
	executeDoc(t, c, "delete", id, ochazuke.Arguments{"archive-first": false})
	c.state.replace = func(string, string) error { return failure }
	if _, err := c.createThing(thing); !errors.Is(err, failure) {
		t.Fatalf("restore: %v", err)
	}
	if exists(source) {
		t.Fatal("failed restore metadata commit left an unregistered file")
	}
	c.state.replace = nil
	if _, err := c.state.path(id); err == nil {
		t.Fatal("failed restore removed the tombstone")
	}
}

func TestDocumentArchivedDuplicateIsLive(t *testing.T) {
	root, _ := docEnv(t)
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "plan.md")
	original := read(t, source)
	thing, err := c.docThing(source)
	if err != nil {
		t.Fatal(err)
	}
	id := thing["id"].(string)
	executeDoc(t, c, "archive", id, nil)
	duplicate := docRecord(t, executeDoc(t, c, "duplicate", id, ochazuke.Arguments{"name": "live-copy"}))
	if duplicate["archived"] != false || duplicate["path"] != filepath.Join(root, "live-copy.md") || duplicate["id"] == id {
		t.Fatalf("archived duplicate did not become a new live document: %v", duplicate)
	}
	if read(t, duplicate["path"].(string)) != original || read(t, filepath.Join(root, ".archive", "plan.md")) != original {
		t.Fatal("archived duplicate lost source or copied bytes")
	}
	rows, err := c.docRows("", false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == duplicate["id"] {
			return
		}
	}
	t.Fatal("live duplicate is hidden from the normal list")
}

func TestDocumentFailedMultiPropPatchLeavesOriginal(t *testing.T) {
	root, _ := docEnv(t)
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "plan.md")
	original := read(t, source)
	thing, err := c.docThing(source)
	if err != nil {
		t.Fatal(err)
	}
	id := thing["id"].(string)
	directory := filepath.Join(root, "other")
	collision := filepath.Join(directory, "taken.md")
	writeFile(t, collision, "# Existing destination\n")
	intermediate := filepath.Join(directory, "intermediate.md")
	_, err = c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "set", Args: ochazuke.Arguments{
		"id": id, "props": map[string]any{"path": intermediate, "name": "taken", "content": "# Must not be applied\n"},
	}})
	if err == nil {
		t.Fatal("a colliding final destination was accepted")
	}
	if exists(intermediate) || read(t, source) != original || read(t, collision) != "# Existing destination\n" {
		t.Fatal("failed multi-prop patch changed a document's bytes or location")
	}
	current := docRecord(t, executeDoc(t, c, "get", id, nil))
	if current["id"] != id || current["path"] != source {
		t.Fatalf("failed multi-prop patch changed identity: %v", current)
	}
}

func TestDocumentContentWriteFailureRollsBackMove(t *testing.T) {
	root, _ := docEnv(t)
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "plan.md")
	destination := filepath.Join(root, "failed-write.md")
	original := read(t, source)
	thing, err := c.docThing(source)
	if err != nil {
		t.Fatal(err)
	}
	id := thing["id"].(string)
	executeDoc(t, c, "relate", id, ochazuke.Arguments{"to": "project:p1", "kind": "document"})
	failure := errors.New("injected atomic content-write failure")
	c.writeContent = func(string, string) error { return failure }
	_, err = c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "set", Args: ochazuke.Arguments{
		"id": id, "props": map[string]any{"path": destination, "content": "# Must not replace original\n"},
	}})
	if !errors.Is(err, failure) {
		t.Fatalf("content write: %v", err)
	}
	if exists(destination) || read(t, source) != original {
		t.Fatal("a failed content write left the document moved or changed")
	}
	current := docRecord(t, executeDoc(t, c, "get", id, nil))
	if current["id"] != id || current["path"] != source {
		t.Fatalf("a failed content write left the identity moved: %v", current)
	}
	relations, err := c.state.Relations(ochazuke.RelationQuery{Thing: ochazuke.AnyRef(docNoun, id)})
	if err != nil || len(relations) != 1 || relations[0].To != "project:p1" {
		t.Fatalf("relations after failed content write: %v %v", relations, err)
	}
	for _, entry := range c.r.History(0) {
		if entry.Request.Verb == "set" && entry.Result.OK {
			t.Fatal("a failed content write recorded a successful set")
		}
	}
}
