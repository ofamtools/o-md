# Changelog

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
