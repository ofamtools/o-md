# Changelog

## Unreleased

- Changed: requires ochazuke v0.122.1 and oden v0.5.0. Uses the current standard document scaffold, typed path/time fields and generated noun actions.
- Added: the same complete file-backed document store in the catalog CLI and editor: native open, property forms, content edits, rename/move, byte-preserving duplicate, archive/restore, history, yank, persistent relations and standard undo.
- Changed: documents have stable IDs in the tool data directory; current paths remain human selectors and old paths stop resolving after a move. Path/root props declare file/directory kinds, and modification times use RFC 3339 UTC.
- Changed: document delete moves the actual file to the system trash with an explicit confirmation, never permanent removal. The OS command runner is injectable; tests use temp trash fixtures.
- Added: noun-typed editor-in-pane, send-to-pane, send-to-note and project-association actions through host callbacks, with declarative reciprocal actions on receivers.
- Changed: `doc show` uses the runtime's standard form; `doc open` means o-md's editor and `doc open-with` means a named OS application. Removed the custom clipboard implementation in favor of the runtime's registers.
- Added: generated settings/bindings/standard-verb reference (`make reference`) and document lifecycle, transport and TUI tests.
- Fixed: one-shot document commands now opt into the shared runtime history and flush it before exit; CLI and TUI history share the tool data directory. The runtime's existing retention and value-privacy rules still apply.

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
