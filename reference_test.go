package main

import (
	"os"
	"path/filepath"
	"testing"

	"ofam.dev/ochazuke/refdoc"
)

func TestReference(t *testing.T) {
	docEnv(t)
	c, err := newCatalogApp()
	if err != nil {
		t.Fatal(err)
	}
	c.r.Config().Disable()
	model := c.r.ReferenceModel()
	opts := refdoc.Options{Stamp: refdoc.Stamp{Tool: "o-md", Version: Version, Ochazuke: "v0.124.0"}, Regenerate: "make reference", Check: "go test -run '^TestReference$' ."}
	pages := refdoc.Render(model, opts)
	for name, text := range pages {
		path := filepath.Join("docs", "reference", name)
		if os.Getenv("UPDATE_REFERENCE") == "1" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if refdoc.Body(string(data)) != refdoc.Body(text) {
			t.Errorf("%s is stale; regenerate with make reference", path)
		}
	}
}
