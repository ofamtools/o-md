package main

import (
	"encoding/json"
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func TestCatalogDeclaresSingularNamespace(t *testing.T) {
	var doc ocli.CatalogDoc
	if err := json.Unmarshal([]byte(mustShell(t, "catalog")), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Namespace != "md" || ochazuke.DefaultNamespace != "md" {
		t.Fatalf("catalog namespace %q, default %q; want md", doc.Namespace, ochazuke.DefaultNamespace)
	}
	if got, e := ocli.CatalogNamespace(doc, "o-md"); e != nil || got != "md" {
		t.Fatal(got, e)
	}
}
