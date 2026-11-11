package main

import (
	"testing"
	"time"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/lexiconcheck"
)

// o-md's registered operations are held to the lexicon's naming rules
// (lexicon plan): one word has one part of speech, nouns are singular, a
// namespace is not a noun or action word, a key has at most three parts.
// testdata/lexicon-words.tsv is a copy of ochazuke's word list.
func TestLexicon(t *testing.T) {
	data := t.TempDir()
	t.Setenv("HOME", data)
	t.Setenv("XDG_CONFIG_HOME", data+"/config")
	t.Setenv("XDG_DATA_HOME", data+"/data")
	cat, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	a, err := newApp(newScratchPath(data, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)), data, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, rt := range map[string]*ochazuke.Runtime{"catalog": cat.r, "editor": a.r} {
		t.Run(name, func(t *testing.T) {
			lexiconcheck.Check(t, lexiconcheck.Config{
				Operations: &rt.Operations,
				MainNouns:  map[string]string{"md": "doc"},
				Words:      "testdata/lexicon-words.tsv",
				Baseline:   "testdata/lexiconcheck.txt",
			})
		})
	}
}

// The file a buffer is written to is the doc's path prop, not a verb.
func TestEditorPathIsAProp(t *testing.T) {
	data := t.TempDir()
	t.Setenv("HOME", data)
	t.Setenv("XDG_CONFIG_HOME", data+"/config")
	t.Setenv("XDG_DATA_HOME", data+"/data")
	scratch := newScratchPath(data, time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	a, err := newApp(scratch, data, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "get", Args: ochazuke.Arguments{"prop": "path"}})
	if err != nil || got != scratch {
		t.Fatalf("doc get --prop path = %v, %v; want %s", got, err, scratch)
	}
	if exists(scratch) {
		t.Fatal("reading a pending scratch record created a file")
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "set", Args: ochazuke.Arguments{"props": map[string]any{"content": "# Scratch\n"}}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, scratch); got != "# Scratch\n" || a.doc.Text() != got {
		t.Fatal("a scratch record's content did not write through to its file and editor")
	}
	if _, ok := a.r.Operations.Find(ochazuke.Key("md.doc.path")); ok {
		t.Fatal("doc path is still a verb")
	}
}
