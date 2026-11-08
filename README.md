<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-animated-dark.svg">
    <img alt="o-md" src="assets/logo-animated-light.svg" width="360">
  </picture>
  <br>
  <a href="https://github.com/ofamtools/o-md/tags"><img alt="Latest tag" src="https://img.shields.io/github/v/tag/ofamtools/o-md"></a>
  <a href="https://pkg.go.dev/ofam.dev/o-md"><img alt="Go Reference" src="https://pkg.go.dev/badge/ofam.dev/o-md.svg"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/ofamtools/o-md"></a>
</p>

<p align="center">Markdown documents with item notes, a family extension for mtm.</p>

`o-md` opens a Markdown file in ochazuke's inline editor: the document stays
rendered, the cursor moves by drawn line and by block, and the block under the
cursor is edited as Markdown in place. Every change is written as it is made;
there is no save key, and `u` / `ctrl+r` undo and redo.

Notes are short threaded comments stored in the file itself, as a quote under
the list item they are about. People and agents add, answer and resolve them
from the editor or the command line, and `o md send` hands the open notes to
an agent's pane as one prompt.

## Install

```sh
go install ofam.dev/oden/cmd/o@latest   # oden's o: o md runs o-md, no dash needed
go install ofam.dev/o-md@latest
mtm extension register md        # mounts the Docs page and the doc.* verbs in mtm
```

## Usage

```sh
o md plan.md                     # open plan.md (created empty when missing)
o md                             # a new scratch buffer; :save-as PATH names it
o md -r                          # reopen the newest scratch buffer
o md scratch [--json]            # list scratch buffers, newest first
```

Notes and checklists from the command line:

```sh
$ o md note plan.md --item README "say which badges"
1.1
$ o md notes plan.md
{"id":"1.1","item":"write the README","item_line":3,"line":4,"author":"me","text":"say which badges","resolved":false,"sent":false,...}
$ o md check plan.md --item logo          # tick the first checklist item holding "logo"
$ cat plan.md
# Plan

- [ ] write the README
  > **me** 2026-11-01: say which badges
- [x] add the logo
```

```sh
o md note FILE --reply ID TEXT                     # answer note ID
o md note FILE --edit ID TEXT | --resolve ID | --unresolve ID | --delete ID
o md notes FILE [--item MATCH] [--unresolved] [--unsent] [--format jsonl|prompt]
o md send FILE --pane ID [--now] [--dry-run]       # send the open notes to an agent's pane
```

TEXT `-` reads stdin. A note's author is `--author`, else the agent mtm lists
in `$TUIOS_PANE_ID`, else the owner (`$O_MD_OWNER`, else `$USER`); an agent may
not sign as the owner, and only a note's author or the owner may edit, resolve
or delete it. `send` uses `mtm agent enqueue`: the prompt is typed when the agent is at rest and waits while it
works, never over a prompt it waits on; with `--now` a pane that is not at rest refuses too. `--dry-run` prints the
prompt without sending. Sent notes are marked so they are not sent twice.

### Editor keys

| Keys | Action |
|---|---|
| `j` / `k`, `}` / `{` | drawn lines, components |
| `]]` / `[[` | next / previous block of the same kind |
| `]h ]t ]x ]l ]c ]q ]n` | next heading, table, checklist item, list, code, quote, note |
| `ctrl+d/u/f/b`, `gO` | pages, headings outline |
| `i a I A`, `o O` | edit the block, open a block below / above |
| `dd yy p`, `x` | delete, yank, put; toggle a checklist item |
| `n`, `za`, `S` | add a note, fold notes, send notes |
| `u`, `ctrl+r` | undo, redo |
| `?`, `q` | help, quit |

## The Docs page and `doc` verbs

`o md catalog` is the extension's catalog. Registered in mtm, it adds a Docs
page (a list with a preview) and the `doc` noun, also reachable through mtm's
MCP gateway as `md.doc.*`:

```sh
o md doc list [--root DIR] [--archived]
o md doc new [--name NAME] [--root DIR]
o md doc show|get|delete|archive|yank-path --target PATH
o md doc set --target PATH --props '{"@label": "NEW"}'       # rename the file
o md doc set --target PATH --props '{"@archive": false}'    # restore it from .archive
o md doc notes|annotate|check|uncheck|send-notes --target PATH ...
o md doc --help                  # every verb and its parameters
```

`doc list` walks the folders configured in `docs.roots`
(`$O_MD_ROOTS` overrides; set it with
`o md settings set docs.roots '["~/notes"]'`) and the
host's project directories (`$MTM_PROJECT_DIRS`: each one's top-level `*.md`
and `docs/**/*.md`). Rows show title, root, modified time, open tasks and open
notes. `doc archive` moves a file to `<root>/.archive/<relative path>` and
`set` with `@archive` false moves it back (what `doc restore` was). Both refuse to overwrite an existing file.
The default list skips `.archive`; `--archived` lists only archived files.
Set `docs.hide-archived` to `false` to include them in the normal list.
`doc delete` remains available to the owner with confirmation and carries
the destructive flag for agent-aware hosts. Hosts using ochazuke v0.85.0
or newer refuse it for agents and name `doc.archive` instead.

## Data

Scratch buffers live in `$O_MD_DATA`, else `$XDG_DATA_HOME/o-md`, else
`~/.local/share/o-md`; `--data DIR` overrides it. Named files are edited where
they are. `$O_MD_MTM` names another `mtm` bin (tests).

## Build

```sh
go build -o ~/bin/o-md .
```
