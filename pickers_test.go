package main

import (
	"testing"
)

// The tool's noun pickers are declared, so its catalog lists them.
func TestDeclaredPickers(t *testing.T) {
	c, e := newCatalogApp()
	if e != nil {
		t.Fatal(e)
	}
	got := map[string]bool{}
	for _, d := range c.r.CatalogPickerDefs() {
		got[d.Name] = d.Noun != "" && d.About != ""
	}
	for _, name := range []string{"doc"} {
		if !got[name] {
			t.Errorf("catalog does not list the %s picker: %v", name, got)
		}
	}
}
