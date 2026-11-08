// Command o-md opens a Markdown file in ochazuke's inline editor: the
// document stays rendered, the cursor moves by drawn line and by block,
// and the block under the cursor is edited as Markdown in place. Every
// change is written as it is made; there is no save key.
//
//	o-md [FILE]            open FILE (created when missing), or a scratch buffer
//	o-md -r                reopen the newest scratch buffer
//	o-md scratch [--json]  list the scratch buffers, newest first
//	o-md note FILE ...     add, reply to, edit, resolve or delete a note
//	o-md notes FILE ...    print the notes (JSON Lines, or --format prompt)
//	o-md check FILE --item MATCH / o-md uncheck FILE --item MATCH
//	o-md send FILE --pane ID [--now] [--dry-run]
//	o-md doc VERB [--target PATH] ...   the family extension's verbs (catalog.go)
//	o-md catalog                        the catalog a host (mtm) mounts
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const usage = `Usage:
  o-md [--data DIR] [FILE]       open FILE (created empty when missing); no FILE: a new scratch buffer
  o-md -r [--data DIR]           reopen the newest scratch buffer
  o-md scratch [--json]          list scratch buffers, newest first
  o-md note FILE --item MATCH [--author NAME] TEXT     add a note under the first item holding MATCH
  o-md note FILE --reply ID [--author NAME] TEXT       add a note right after note ID
  o-md note FILE --edit ID TEXT | --resolve ID | --unresolve ID | --delete ID
  o-md notes FILE [--item MATCH] [--unresolved] [--unsent] [--format jsonl|prompt]
  o-md check FILE --item MATCH | o-md uncheck FILE --item MATCH
  o-md send FILE --pane ID [--now] [--dry-run]
  o-md doc list|show|get|new|set|edit|delete|archive|copy-path|notes|note|check|uncheck|send-notes [--target PATH] ...
  o-md catalog                   the extension's catalog (mtm extension register md mounts its Docs page)

doc list walks docs.roots (o-md settings set docs.roots ...; $O_MD_ROOTS overrides) and
the host's project directories ($MTM_PROJECT_DIRS: top-level *.md and docs/**/*.md).
TEXT "-" reads stdin. A note's author is --author, else $TUIOS_PANE_ID when mtm lists
an agent in that pane, else the owner ($O_MD_OWNER, else $USER); an agent may not sign
as the owner. Only a note's author or the owner may edit, resolve or delete it.
Scratch buffers live in $O_MD_DATA, else $XDG_DATA_HOME/o-md, else ~/.local/share/o-md.

Keys: j/k lines, }/{ components, ]]/[[ same kind, ]h ]t ]x ]l ]c ]q ]n by kind,
ctrl+d/u/f/b pages, gO headings; i a I A edit, o O open, dd yy p, x toggle,
n note, za fold notes, S send notes, u ctrl+r undo/redo; :save-as PATH names
the buffer; ? help; q quits.`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprintln(os.Stderr, usage)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "o-md:", err)
		os.Exit(1)
	}
}

func run(argv []string, stdin io.Reader, stdout io.Writer) error {
	if isShellCommand(argv) {
		return runShell(argv, stdin, stdout, os.Stderr)
	}
	if len(argv) > 0 {
		switch argv[0] {
		case "note":
			return cmdNote(argv[1:], stdin, stdout)
		case "notes":
			return cmdNotes(argv[1:], stdout)
		case "check", "uncheck":
			return cmdCheck(argv[1:], argv[0] == "check")
		case "send":
			return cmdSend(argv[1:], stdout)
		case "scratch":
			a, err := parseArgs(argv[1:], "json")
			if err != nil {
				return err
			}
			return cmdScratch(argv[1:], dataDir(a.flags["data"]), stdout)
		case "-h", "--help", "help":
			fmt.Fprintln(stdout, usage)
			return nil
		}
	}
	a, err := parseArgs(argv, "r", "smoke")
	if err != nil {
		return err
	}
	if len(a.pos) > 1 {
		return errUsage
	}
	data := dataDir(a.flags["data"])
	path, scratch := "", false
	switch {
	case a.has("r"):
		files, err := scratchFiles(data)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return errors.New("no scratch buffers to reopen")
		}
		path, scratch = files[0].Path, true
	case len(a.pos) == 1:
		if path, err = expand(a.pos[0]); err != nil {
			return err
		}
	default:
		path, scratch = newScratchPath(data, time.Now()), true
	}
	app, err := newApp(path, data, scratch)
	if err != nil {
		return err
	}
	if a.has("smoke") {
		app.m.Width, app.m.Height = 80, 24
		fmt.Fprintln(stdout, ansi.Strip(app.m.View().Content))
		return nil
	}
	_, err = tea.NewProgram(app.m).Run()
	if lerr := app.doc.Leave(); err == nil {
		err = lerr
	}
	return err
}
