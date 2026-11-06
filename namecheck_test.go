package main

import (
	"slices"
	"testing"
	"time"

	"ofam.dev/ochazuke/namecheck"
)

// TestNoNewNameBranches holds the non-test Go to namecheck: no new
// comparison, case label or lookup table keyed by a noun, verb, mode,
// motion or parameter name the runtime declares, and no function grows
// or passes complexity 25. The accepted uses are testdata/namecheck.txt,
// which only shrinks.
func TestNoNewNameBranches(t *testing.T) {
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
	namecheck.Check(t, namecheck.Config{
		Vocab:    mergeVocab(cat.r.NameVocab(), a.r.NameVocab()),
		Baseline: "testdata/namecheck.txt",
		Dirs:     []string{"."},
	})
}

func mergeVocab(vs ...map[string][]string) map[string][]string {
	out := map[string][]string{}
	for _, v := range vs {
		for name, cats := range v {
			for _, c := range cats {
				if !slices.Contains(out[name], c) {
					out[name] = append(out[name], c)
				}
			}
		}
	}
	return out
}
