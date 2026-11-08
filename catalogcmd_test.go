package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The catalog command wins over a file named catalog in the working
// directory, and prints the catalog JSON with no terminal (mtm extension
// refresh runs it so).
func TestCatalogCommandBeatsAFileNamedCatalog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("XDG_DATA_HOME", dir+"/data")
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.WriteFile("catalog", []byte("# a doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"catalog"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc["name"] != "o-md" {
		t.Fatalf("catalog answered %v: %.200s", err, out.String())
	}
}
