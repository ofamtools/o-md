package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"

	"ofam.dev/ochazuke"
)

// Adapted from o-json's documentState: identities and relations are metadata,
// never edits to the source Markdown. Transactions reload under an advisory lock.
type documentState struct {
	dir func() string
	mu  sync.Mutex
}
type documentRecords struct {
	Docs      map[string]documentRecord `json:"docs"`
	changed   bool
	Relations []ochazuke.Relation `json:"relations"`
}
type documentRecord struct {
	Path    string `json:"path"`
	Trashed bool   `json:"trashed,omitempty"`
}

func (s *documentState) transaction(write bool, fn func(*documentRecords) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "documents.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	state := documentRecords{}
	path := filepath.Join(dir, "documents.json")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &state); err != nil {
			return err
		}
	}
	if state.Docs == nil {
		state.Docs = map[string]documentRecord{}
	}
	if err := fn(&state); err != nil {
		return err
	}
	if !write || !state.changed {
		return nil
	}
	b, err = json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".documents-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
func (s *documentState) identify(rows []docRow) error {
	if len(rows) == 0 {
		return nil
	}
	return s.transaction(true, func(state *documentRecords) error {
		byPath := make(map[string]string, len(state.Docs))
		for id, doc := range state.Docs {
			if !doc.Trashed {
				byPath[doc.Path] = id
			}
		}
		for i := range rows {
			id := byPath[rows[i].Path]
			if id == "" {
				var buf [16]byte
				if _, err := rand.Read(buf[:]); err != nil {
					return err
				}
				id = "md-" + hex.EncodeToString(buf[:])
				state.Docs[id] = documentRecord{Path: rows[i].Path}
				state.changed = true
				byPath[rows[i].Path] = id
			}
			rows[i].ID = id
		}
		return nil
	})
}
func (s *documentState) path(id string) (string, error) {
	var path string
	err := s.transaction(false, func(state *documentRecords) error {
		doc, ok := state.Docs[id]
		if ok && doc.Trashed {
			return fmt.Errorf("document %s is in the system trash", id)
		}
		path = doc.Path
		return nil
	})
	if err != nil {
		return "", err
	}
	if path != "" {
		return path, nil
	}
	if len(id) == 35 && id[:3] == "md-" {
		return "", fmt.Errorf("unknown document %s", id)
	}
	return expand(id)
}
func (s *documentState) move(from, to string) error {
	return s.transaction(true, func(state *documentRecords) error {
		for id, doc := range state.Docs {
			if doc.Path == from && !doc.Trashed {
				doc.Path = to
				state.Docs[id] = doc
				state.changed = true
			}
		}
		return nil
	})
}
func (s *documentState) Relations(q ochazuke.RelationQuery) ([]ochazuke.Relation, error) {
	out := []ochazuke.Relation{}
	err := s.transaction(false, func(state *documentRecords) error {
		for _, x := range state.Relations {
			if q.Match(x) {
				out = append(out, x)
			}
		}
		return nil
	})
	slices.SortStableFunc(out, func(a, b ochazuke.Relation) int { return a.Position - b.Position })
	return out, err
}
func (s *documentState) Put(x ochazuke.Relation) error {
	return s.transaction(true, func(state *documentRecords) error {
		i := slices.IndexFunc(state.Relations, func(old ochazuke.Relation) bool { return old.ID() == x.ID() })
		if i < 0 {
			state.Relations = append(state.Relations, x)
		} else {
			state.Relations[i] = x
		}
		state.changed = true
		return nil
	})
}
func (s *documentState) Remove(x ochazuke.Relation) error {
	return s.transaction(true, func(state *documentRecords) error {
		state.Relations = slices.DeleteFunc(state.Relations, func(old ochazuke.Relation) bool { return old.ID() == x.ID() })
		state.changed = true
		return nil
	})
}

func (s *documentState) markTrashed(path string) error {
	return s.transaction(true, func(state *documentRecords) error {
		for id, doc := range state.Docs {
			if doc.Path == path && !doc.Trashed {
				doc.Trashed = true
				state.Docs[id] = doc
				state.changed = true
			}
		}
		return nil
	})
}
func (s *documentState) restorePath(id string) (string, error) {
	var path string
	err := s.transaction(false, func(state *documentRecords) error { path = state.Docs[id].Path; return nil })
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("unknown document %s", id)
	}
	return path, nil
}
func (s *documentState) restored(id string) error {
	return s.transaction(true, func(state *documentRecords) error {
		doc, ok := state.Docs[id]
		if !ok {
			return fmt.Errorf("unknown document %s", id)
		}
		doc.Trashed = false
		state.Docs[id] = doc
		state.changed = true
		return nil
	})
}
