# Code map

- `main.go`, `cli.go`: standalone Markdown editor and notes/checklist commands; dispatch into the catalog CLI, including the app-owned `--data` flag.
- `catalog.go`: Docs page declaration, rooted Markdown listing, document records and filesystem create/rename/archive operations. Native `doc.open` opens the tool editor; mounted hosts use its `opensPage` declaration.
- `doc new` aliases runtime-generated `doc create` with name/root positional metadata. The custom `doc list` adapter preserves the existing raw-row array, `--archived` meaning only archived, `docs.hide-archived`, and merged docs-root/host-project traversal contract; Store.List remains the shared listing capability.
- `open.go`: the entry points as verbs. `doc.open [PATH]` (a path with no file yet is a named buffer written on its first change; `-` is standard input through `docInput`, the one seam for the library's shared input source; nothing named is a new scratch buffer), `doc.reopen` (the newest scratch buffer) and `doc list --scratch`. `catalogApp.launch` starts the editor on a terminal and otherwise lets the verb answer the buffer's record. The bare `o-md [FILE]`, `o-md -r` and `o-md scratch` in `main.go`/`cli.go` only run these verbs.
- `doc_store.go`: the document's typed props and filesystem store. The shared runtime generates get/list/show/create/edit/set/duplicate/archive/restore/delete/yank/history/relate from capabilities. Writable content preserves bytes in duplicate and undo.
- `relations.go`: durable document IDs and general relations, with process-shared file locking and atomic metadata replacement. Files retain their identity across path changes; only current paths resolve.
- `document_actions.go`: registered disk move/rename actions routed through standard set, editor-in-pane, send-to-pane, send-to-note and project association via sanctioned host callbacks. Reciprocal metadata lets the host register the other relationship ends without naming a tool.
- `trash.go`: system-trash process boundary. macOS uses Finder through argument-safe AppleScript; Linux uses `gio trash`. Missing commands and unsupported platforms fail without permanent deletion. Tests inject the runner.
- `file_ops.go`: exclusive creation and same-filesystem no-replace moves of regular documents. Metadata transactions undo these changes before unlocking when persistence fails.
- `tui.go`, `scratch.go`: rendered Markdown editing and lazy scratch files. The TUI uses the same document store as the CLI; an open buffer follows a moved file and cannot recreate a trashed file.
- `notes.go`: inline note identity, ownership, formatting and edits. `doc.notes`, `doc.annotate` and `doc.send-notes` retain their existing file-backed behavior.
- `testdata/lexicon-words.tsv`, `testdata/namecheck.txt`: app vocabulary and reviewed parser-boundary declarations used by the existing naming gates.
- `reference_test.go`, `Makefile`: canonical ochazuke reference generation and stale-reference checking for `docs/reference/`.
- `document_actions_test.go`, `catalog_test.go`, `main_test.go`: document lifecycle, host transport, fake trash/openers, catalog routes and editor behavior.
- `file_ops_test.go`: competing create/duplicate/move operations, real-file rollback when metadata persistence fails, archived-source duplication, failed multi-prop destination edits and atomic-content-write failure after a move.
