# Changelog

## Unreleased

- Changed: doc is a thing noun with a store over the Markdown files (key: the path; label: the file name; archive role: lying under .archive). `doc delete` and `doc archive` are the standard verbs over it, `doc set` and `doc edit` write the name and the archive flag; `list`, `show` and `new` stay.
- Removed: `doc rename` (use `doc set --props '{"@label": NEW}'`) and `doc restore` (use `doc set --props '{"@archive": false}'`); no aliases.
- Added: document archive and restore move files through `<root>/.archive/<relative path>` without overwriting files. `doc list --archived` lists archived files; `docs.hide-archived`, default true, controls their inclusion in the normal list.
- Changed: document deletion declares destructive metadata so agent-aware hosts refuse it and direct agents to archive instead.
