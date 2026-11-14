# Changelog

## Unreleased

- Changed: requires ochazuke v0.137.0.
- Added: the catalog declares the host serve protocol (`"serve": 1`, `cli.Serves()`), so mtm may keep o-md running and send it calls over a pipe. Every environment read is per call (the owner identity, `$O_MD_ROOTS`, `$MTM_PROJECT_DIRS`, the data directory, and the state directory now set before each command), and `o-md ochazuke host serve` is a command word, not a file to open.
- Changed: `doc list` is a table for a person at a terminal (`.Table()`); a pipe still gets JSON.
- Fixed: `doc show nosuch` answers `no doc "nosuch"; doc list shows ids`, not a raw OS error with a path.
- Added: `TestNoNewRefusals` (ochazuke's refusecheck) with today's baseline, `testdata/refusecheck.txt`.

## v0.7.0

- Changed: requires ochazuke v0.124.1 (a key on a focused row acts on it again instead of asking for its subject; the path ask's source keys are restored).
- Changed: requires ochazuke v0.124.0 and oden v0.5.1. Uses the current standard document scaffold, typed path/time fields and generated noun actions.
- Added: the same complete file-backed document store in the catalog CLI and editor: native open, property forms, content edits, rename/move, byte-preserving duplicate, archive/restore, history, yank, persistent relations and standard undo.
- Changed: documents have stable IDs in the tool data directory; current paths remain human selectors and old paths stop resolving after a move. Path/root props declare file/directory kinds, and modification times use RFC 3339 UTC.
- Changed: document delete moves the actual file to the system trash with an explicit confirmation, never permanent removal. The OS command runner is injectable; tests use temp trash fixtures.
- Added: noun-typed editor-in-pane, send-to-pane, send-to-note and project-association actions through host callbacks, with declarative reciprocal actions on receivers.
- Changed: `doc show` uses the runtime's standard form; `doc open` means o-md's editor and `doc open-with` means a named OS application. Removed the custom clipboard implementation in favor of the runtime's registers.
- Added: generated settings/bindings/standard-verb reference (`make reference`) and document lifecycle, transport and TUI tests.
- Fixed: one-shot document commands now opt into the shared runtime history and flush it before exit; CLI and TUI history share the tool data directory. The runtime's existing retention and value-privacy rules still apply.
- Fixed: concurrent creation and duplication claim filenames exclusively; moves of regular documents use same-filesystem no-replace link/unlink. Failed create, move and undo-restore metadata commits roll their file changes back before releasing the metadata lock.
- Fixed: duplicating an archived source creates a visible, unarchived full-content copy. Multi-prop edits compute their final path/name/archive destination before moving, so a destination collision leaves the original file and identity unchanged.
- Fixed: if an atomic content write fails after a combined move/edit, restore the original location and identity before returning the write/rollback errors.
- Changed: `doc new` is an alias of the runtime-generated `doc create`; both use Store.Create and preserve positional name/root and named flags. Removed the redundant application creation handler.
- Added: the command line's entry points are verbs, reachable in noun verb form, from the editor's palette and mounted in a host. `doc open [PATH]` takes a typed file path (positional) or `--id`; `doc reopen` opens the newest scratch buffer; `doc list --scratch` lists the scratch buffers. The bare `o-md [FILE]`, `o-md -r` and `o-md scratch` stay and only run these verbs.
- Changed: opening never refuses. A path with no file yet opens as a new, named buffer and the file is written on its first change; before, `o-md FILE` created an empty file on opening. With nothing named `doc open` is a new scratch buffer (also from the palette, where it used to need a document). `o-md -r` and `doc reopen` with no scratch buffer open a new one instead of failing.
- Added: `doc open -` and `o-md -` read standard input into a new scratch buffer, written at once. The handler reads through one `io.Reader` seam (`docInput`) that the library's shared input-source parameter can replace; `!COMMAND` and output sinks are not built here.
- Changed: without a terminal (a host's job, a pipe) `doc open` and `doc reopen` start no editor and answer the buffer's record (`path`, `name`, `scratch`, `saved`, `origin`); `doc open` no longer answers the whole document.
- Known gaps, in the library and host, not worked around here: a host's native open of a path with no file yet fails at its `doc get` (and its Markdown detail opens files with `markdown.File`, which needs the file); a mounted verb that opens a page cannot name its own subject, so `md.doc open` alone only opens the Docs page and `md.doc reopen` answers the record without opening it; a host does not pass standard input to a mounted verb.
- Fixed: the README said delete was archive-first by default; `delete.archive` is off by default, so `doc delete` asks and then moves the file to the system trash.

## v0.6.0

- Changed: requires ochazuke v0.110.1 (esc only leaves insert in modals and `q` closes them; label-first pickers).
- Changed: the pane variables are read as `MTM_X` first, then `TUIOS_X` (mtm sets both for one release): `ENV`, `SESSION`, `PANE_ID`, `HARNESS`, `AGENT_NAME`, through one `paneEnv` helper.

## v0.4.0 (2026-11-08)

- Changed, breaking (lexicon, ochazuke v0.105.0, one shot, no aliases; every rename is in `docs/lexicon-renames-v0.105.tsv`): `doc note` is `doc annotate`, `doc copy-path` is `doc yank-path`, the editor's `doc path` verb is the `path` prop of the doc (`doc get --prop path`), `doc` is the main noun. The `o md note PATH` command is unchanged. `TestLexicon` runs with an empty baseline over both the catalog and the editor runtime.
- Changed: requires ochazuke v0.102.0 and oden v0.3.0 (the Operations registry and three-part keys; `cmd/opname` moved the code). o-md declares its namespace, `md` (`ochazuke.DeclareNamespace`), so its keys are `md.<noun>.<verb>` and the catalog carries `namespace: md`. CLI spellings are unchanged.
- Changed: doc is a thing noun with a store over the Markdown files (key: the path; label: the file name; archive role: lying under .archive). `doc delete` and `doc archive` are the standard verbs over it, `doc set` and `doc edit` write the name and the archive flag; `list`, `show` and `new` stay.
- Removed: `doc rename` (use `doc set --props '{"@label": NEW}'`) and `doc restore` (use `doc set --props '{"@archive": false}'`); no aliases.
- Added: document archive and restore move files through `<root>/.archive/<relative path>` without overwriting files. `doc list --archived` lists archived files; `docs.hide-archived`, default true, controls their inclusion in the normal list.
- Changed: document deletion declares destructive metadata so agent-aware hosts refuse it and direct agents to archive instead.
