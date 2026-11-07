package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/markdown"
)

const linkDoc = "# Links\n\nSee [the guide](https://example.com/guide) and [the spec](https://example.com/spec).\n"

// The doc view draws a link as its text only; the focused link's URL is
// a "→ URL" line under the document (ochazuke markdown.link-urls).
func TestDocViewLinkURLs(t *testing.T) {
	a, err := newApp(writeTemp(t, linkDoc), t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	frame := func() string { return ansi.Strip(a.doc.View(80, 20, *a.m.Appearance)) }
	f := frame()
	if !strings.Contains(f, "the guide") || strings.Contains(f, "example.com") {
		t.Fatalf("unfocused doc shows link text only:\n%s", f)
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: markdown.DocNoun, Verb: "next-link"}); err != nil {
		t.Fatal(err)
	}
	f = frame()
	if !strings.Contains(f, "→ https://example.com/guide") {
		t.Fatalf("focused link's URL line missing:\n%s", f)
	}
	if strings.Contains(f, "example.com/spec") {
		t.Fatalf("only the focused link's URL shows:\n%s", f)
	}
}
