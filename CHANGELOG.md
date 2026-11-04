# Changelog

## Unreleased

- Added: document archive and restore move files through `<root>/.archive/<relative path>` without overwriting files. `doc list --archived` lists archived files; `docs.hide-archived`, default true, controls their inclusion in the normal list.
- Changed: document deletion declares destructive metadata so agent-aware hosts refuse it and direct agents to archive instead.
