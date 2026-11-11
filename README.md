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
in `$MTM_PANE_ID` (else `$TUIOS_PANE_ID`), else the owner (`$O_MD_OWNER`, else `$USER`); an agent may
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
o md doc show|get|edit|delete|archive|restore|duplicate|yank|history --target ID_OR_PATH
o md doc rename NEW_NAME --target ID_OR_PATH
o md doc move DESTINATION.md --target ID_OR_PATH
o md doc set --target ID_OR_PATH --props '{"content": "# Updated\n"}'
o md doc reveal|yank-path|open --target ID_OR_PATH
o md doc open-with --with "Visual Studio Code" --target ID_OR_PATH
o md doc edit-in-pane --target ID_OR_PATH
o md doc send-to-pane PANE --target ID_OR_PATH
o md doc send-to-note [NOTE] --target ID_OR_PATH
o md doc add-to-project PROJECT --target ID_OR_PATH
o md doc relate --to project:PROJECT --target ID_OR_PATH
o md doc notes|annotate|check|uncheck|send-notes --target ID_OR_PATH ...
o md doc --help                  # every verb and its parameters
```

`doc new` is an alias of the runtime-generated `doc create`, not a separate
creation handler. Both use the declared store and accept positional name/root
or `--name` and `--root`.

`doc list` walks the folders configured in `docs.roots`
(`$O_MD_ROOTS` overrides; set it with
`o md setting set docs.roots '["~/notes"]'`) and the
host's project directories (`$MTM_PROJECT_DIRS`: each one's top-level `*.md`
and `docs/**/*.md`). Rows show title, root, modified time, open tasks and open
notes. `doc archive` moves a file to `<root>/.archive/<relative path>` and
`doc restore` moves it back. Both refuse to overwrite an existing file.
Creation and duplication claim filenames exclusively. Moves use same-filesystem
no-replace links for regular files and fail across filesystems without copying.
Failed create, move and undo-restore metadata commits roll their file changes
back before another app transaction can use that path.
The default list skips `.archive`; `--archived` lists only archived files.
Set `docs.hide-archived` to `false` to include them in the normal list.
The standard `show` opens the document's property form in a TUI; `open` opens
its rendered Markdown editor. `open-with` is deliberately different: it uses
the named OS application through `--with`.
The same typed path supplies `reveal` and `yank-path` in both standalone and
mounted runtimes (`paths.free-verbs`, default on).

Deletion is archive-first by default (`delete.archive`). To go straight to
the system trash, use `doc delete --archive-first=false --target ID --yes`.
The confirmation explicitly names the system trash. macOS uses Finder;
Linux requires `gio`. Failure to contact the trash service leaves the file
in place; there is no permanent-delete fallback. Agents are directed to
archive rather than destructive deletion.

Document IDs are durable metadata in `documents.json` under the data
directory. Existing files are imported without modifying their Markdown.
Current paths still select documents; old paths stop resolving after a move.
Moving, renaming, archiving and restoring keep the ID, relations and history.
Standard duplicate copies the complete Markdown text, including inline notes,
to a new file and ID. Runtime undo (`ochazuke.history-entry.undo`) restores
standard store changes in the current session; undoing trash recreates the
original file and keeps the trash copy. Keep the data directory when moving
between machines.
Upgraded hosts normalize earlier path-key references through the declared
human path selector before a rename; extensions do not rewrite host data.
CLI commands and the TUI share persisted runtime history under `DATA/state`.
The inherited `history-limit` setting controls retention; `0` disables it.
The runtime persists only its permitted simple values, not full text snapshots,
so in-session text/delete undo is not promised after a restart.

Host operations use `OCHAZUKE_HOST` and fail explicitly when no host is
configured. `edit-in-pane` starts `$VISUAL`, then `$EDITOR`, else `vi` as an
argv command in a new pane. `send-to-pane` pastes the complete document
without pressing Enter. `send-to-note` appends to an existing note or creates
a new note about this document. `add-to-project` delegates to the host's
shared project relation handler. Parameters are noun-typed; mounted hosts
derive `pane.receive-md-doc`, `note.receive-md-doc` and `project.add-md-doc`
from reciprocal declarations, using the same original handler.
`notes` and `send-notes` continue to select, compose and mark inline notes.

The standalone editor and catalog CLI share this store. Generated record
keys can be disabled with `keys.noun-defaults=false`; the standard form
detail with `detail.show-form=false`; the inherited Nouns tab with
`nouns.page=false`. No application-specific key switches are added.

### Compared with Vim

| Vim form | Help tag | o-md |
|---|---|---|
| `:edit FILE` | `:help :edit` | Similar: `doc open` opens the rendered file; missing files opened directly are recorded immediately. |
| `:file NAME` | `:help :file` | Different: `doc rename` renames the actual file, preserving its document ID. |
| `:undo` | `:help undo` | Same intent: editor undo changes text; runtime history undo reverses document-store changes. |
| `:!$EDITOR FILE` | `:help :!` | Different: `edit-in-pane` uses argv in a host pane, never shell evaluation. |

Generated settings, bindings and standard verbs are in
[`docs/reference/`](docs/reference/README.md); regenerate with `make reference`.

## Data

The data directory is `$O_MD_DATA`, else `$XDG_DATA_HOME/o-md`, else
`~/.local/share/o-md`; `--data DIR` overrides it. It contains scratch files,
stable document IDs and document relations. Named files stay at their
selected paths. `$O_MD_MTM` names another `mtm` bin for the notes transport.

## Build

```sh
go build -o ~/bin/o-md .
```
