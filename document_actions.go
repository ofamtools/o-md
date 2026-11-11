package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func (c *catalogApp) documentActions() {
	r := c.r
	r.Define(docNoun).
		Verb("rename", c.changeLocation("name"), "Rename the actual file; a name stays in its directory and a path moves it").Positional("to").History(ochazuke.HistoryChange).
		Verb("move", c.changeLocation("path"), "Move the actual file to another path, refusing to overwrite").Positional("to").History(ochazuke.HistoryChange).
		Verb("edit-in-pane", c.on(c.editInPane), "Open this file in a new host pane using $VISUAL, then $EDITOR, then vi").
		Verb("send-to-pane", c.on(c.sendToPane), "Paste the document into a host pane without submitting it").Positional("pane").Reciprocal("pane", "receive-md-doc", "doc").
		Verb("send-to-note", c.on(c.sendToNote), "Append the document to a host note, or create an associated note when none is named").Positional("note").Reciprocal("note", "receive-md-doc", "doc").
		Verb("add-to-project", c.on(c.addToProject), "Associate the document with a host project through the shared relation handler").Positional("project").Reciprocal("project", "add-md-doc", "doc")
	params := map[string]map[string]ochazuke.Parameter{
		"rename":         {"to": {Type: "string", Required: true, Description: "New filename or path"}},
		"move":           {"to": {Type: ochazuke.PropPath, Path: "file", New: true, Required: true}},
		"edit-in-pane":   {"session": {Type: "string", HostNoun: "session", Noun: "session", Free: true}},
		"send-to-pane":   {"pane": {Type: "string", HostNoun: "pane", Noun: "pane", Free: true, Required: true}, "session": {Type: "string", HostNoun: "session", Noun: "session", Free: true}},
		"send-to-note":   {"note": {Type: "string", HostNoun: "note", Noun: "note", Free: true}, "project": {Type: "string", HostNoun: "project", Noun: "project", Free: true}},
		"add-to-project": {"project": {Type: "string", HostNoun: "project", Noun: "project", Free: true, Required: true}},
	}
	for verb, parameters := range params {
		parameters["id"] = docSubject()
		op := r.Operations.At(ochazuke.Key(docNoun + "." + verb))
		op.Parameters = parameters
		r.Operations.Set(op)
	}
}

func docSubject() ochazuke.Parameter {
	return ochazuke.Parameter{Type: "string", Noun: docNoun, Focus: true, Free: true, Description: "The document's stable ID or current path"}
}
func (c *catalogApp) changeLocation(prop string) ochazuke.AppHandler {
	return func(ctx ochazuke.Ctx) ochazuke.Result {
		path, err := c.targetPath(ctx.Instance)
		if id := ochazuke.StringArg(ctx.Args, "id", ""); id != "" {
			path, err = c.state.path(id)
		}
		if err != nil {
			return ochazuke.Failed(err)
		}
		value, err := c.r.Execute(ochazuke.Request{Noun: docNoun, Verb: "set", Args: ochazuke.Arguments{"id": path, "props": map[string]any{prop: ochazuke.StringArg(ctx.Args, "to", "")}}})
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(value)
	}
}

func hostCall(noun, verb string, args ochazuke.Arguments) (any, error) {
	host, ok, err := ocli.HostFromEnv()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("this action needs a configured ochazuke host (OCHAZUKE_HOST)")
	}
	raw, err := host.Call(context.Background(), noun, verb, args)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
func hostSession(args ochazuke.Arguments) string {
	return ochazuke.StringArg(args, "session", paneEnv(os.Getenv, "SESSION"))
}
func (c *catalogApp) editInPane(path string, args ochazuke.Arguments) (any, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	argv, err := editorWords(editor)
	if err != nil {
		return nil, err
	}
	argv = append(argv, path)
	return hostCall("pane", "open", ochazuke.Arguments{"session": hostSession(args), "cwd": filepath.Dir(path), "command": argv, "name": filepath.Base(path)})
}
func (c *catalogApp) sendToPane(path string, args ochazuke.Arguments) (any, error) {
	text, err := readText(path)
	if err != nil {
		return nil, err
	}
	return hostCall("pane", "send-text", ochazuke.Arguments{"session": hostSession(args), "window": ochazuke.StringArg(args, "pane", ""), "text": text, "paste": true, "submit": false})
}
func (c *catalogApp) sendToNote(path string, args ochazuke.Arguments) (any, error) {
	text, err := readText(path)
	if err != nil {
		return nil, err
	}
	row, err := c.rowOf(c.rootOf(path), path)
	if err != nil {
		return nil, err
	}
	if note := ochazuke.StringArg(args, "note", ""); note != "" {
		value, err := hostCall("note", "get", ochazuke.Arguments{"id": note})
		if err != nil {
			return nil, err
		}
		th, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("host note get returned %T, expected a record", value)
		}
		old, ok := th["text"].(string)
		if !ok {
			return nil, errors.New("host note has no text")
		}
		return hostCall("note", "set", ochazuke.Arguments{"id": note, "props": map[string]any{"text": old + "\n\n" + text}})
	}
	return hostCall("note", "new", ochazuke.Arguments{"text": text, "about": ochazuke.AnyRef("md.doc", row.ID), "project": ochazuke.StringArg(args, "project", "")})
}
func (c *catalogApp) addToProject(path string, args ochazuke.Arguments) (any, error) {
	row, err := c.rowOf(c.rootOf(path), path)
	if err != nil {
		return nil, err
	}
	return hostCall("project", "relate", ochazuke.Arguments{"id": ochazuke.StringArg(args, "project", ""), "to": ochazuke.AnyRef("md.doc", row.ID), "kind": "document"})
}

// Editor commands are argv, never shell programs: quotes and escapes group words,
// while dollar signs, substitutions and operators remain literal text.
func editorWords(text string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote rune
	escaped, started := false, false
	for _, ch := range text {
		switch {
		case escaped:
			b.WriteRune(ch)
			escaped = false
			started = true
		case ch == '\\' && quote != '\'':
			escaped = true
			started = true
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				b.WriteRune(ch)
			}
			started = true
		case ch == '\'' || ch == '"':
			quote = ch
			started = true
		case unicode.IsSpace(ch):
			if started {
				out = append(out, b.String())
				b.Reset()
				started = false
			}
		default:
			b.WriteRune(ch)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, errors.New("unterminated quote or escape in editor command")
	}
	if started {
		out = append(out, b.String())
	}
	if len(out) == 0 || out[0] == "" {
		return nil, errors.New("empty editor command")
	}
	return out, nil
}
