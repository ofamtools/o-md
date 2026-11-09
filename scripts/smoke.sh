#!/bin/sh
# Smoke test of the real binary: the TUI in a pseudo-terminal over a copy
# of the matomeru checklist (never the original), then the agent commands
# against that copy, then a scratch buffer. Usage: scripts/smoke.sh [BIN]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
bin=${1:-$here/../o-md}
src=${O_MD_SMOKE_SOURCE:-$HOME/hub/o-plans/matomeru-features.md}
work=$(mktemp -d "${TMPDIR:-/tmp}/o-md-smoke.XXXXXX")
trap 'rm -rf "$work"' EXIT
export O_MD_DATA="$work/data" USER=owner MTM_PANE_ID= TUIOS_PANE_ID=
copy="$work/features.md"
cp "$src" "$copy"
keys="$here/pty_keys.py"
fail() { echo "smoke: FAIL: $*" >&2; exit 1; }

# 1. TUI: toggle the first item, add an item below the second, a note on it, quit.
python3 "$keys" '["]x", "x", "]x", "o", "Smoke item from o-md", {"key":"esc"}, "n", "a smoke note", {"key":"esc"}, {"sleep":0.5}, "q"]' -- "$bin" "$copy"
diff -u "$src" "$copy" > "$work/tui.diff" || true
cat "$work/tui.diff"
grep -q '^-- \[ \] One queue across all panes' "$work/tui.diff" || fail "first item not toggled"
grep -q '^+- \[x\] One queue across all panes' "$work/tui.diff" || fail "first item not checked"
grep -q '^+- \[ \] Smoke item from o-md$' "$work/tui.diff" || fail "item not added"
grep -q '^+  > \*\*owner\*\* [0-9-]*: a smoke note$' "$work/tui.diff" || fail "note not added"
[ "$(grep -v '^\(---\|+++\) ' "$work/tui.diff" | grep -c '^[-+]')" -eq 4 ] || fail "more lines changed than the edits"

# 2. Agent commands: an agent (pane id) notes, the owner's note is not its to resolve.
# A pane is an agent only when mtm lists one there: a fake mtm lists it.
agent=11111111-2222-3333-4444-555555555555
printf '#!/bin/sh\nif [ "$1 $2" = "agent scan" ]; then echo '"'"'{"agents":[{"window_id":"%s","name":"smoke","state":"idle","ready":true,"session":"s"}]}'"'"'; fi\n' "$agent" > "$work/mtm"
chmod +x "$work/mtm"
export O_MD_MTM="$work/mtm"
id=$(MTM_PANE_ID=$agent "$bin" note "$copy" --item "Archive and restore" "started on the trash view")
echo "agent note: $id"
MTM_PANE_ID=$agent "$bin" notes "$copy" --item "Archive and restore" | tee "$work/notes.jsonl"
grep -q "\"author\":\"$agent\"" "$work/notes.jsonl" || fail "agent identity"
grep -q '"self_declared":false' "$work/notes.jsonl" || fail "pane identity is not self-declared"
owner_id=$("$bin" notes "$copy" | grep '"author":"owner"' | head -1 | sed 's/.*"id":"\([^"]*\)".*/\1/')
if MTM_PANE_ID=$agent "$bin" note "$copy" --resolve "$owner_id" 2> "$work/refused"; then fail "agent resolved the owner's note"; fi
cat "$work/refused"
grep -q "only its author or the owner" "$work/refused" || fail "refusal message"
reply=$("$bin" note "$copy" --reply "$id" "thanks, go on")
echo "reply: $reply"
MTM_PANE_ID=$agent "$bin" note "$copy" --resolve "$id"
"$bin" notes "$copy" --unresolved | grep -q "started on the trash view" && fail "resolved note listed as open"
grep -q "started on the trash view" "$copy" || fail "resolved note must stay in the file"
"$bin" notes "$copy" --format prompt | tee "$work/prompt.txt"
grep -q '^Notes on ' "$work/prompt.txt" || fail "prompt header"
grep -q 'thanks, go on' "$work/prompt.txt" || fail "prompt holds the reply"
grep -q -- '--reply ID TEXT$' "$work/prompt.txt" || fail "prompt closing line"
"$bin" send "$copy" --dry-run > "$work/send.txt"
grep -q 'a smoke note' "$work/send.txt" || fail "send --dry-run"
"$bin" check "$copy" --item "Archive and restore"
grep -q '^- \[x\] Archive and restore' "$copy" || fail "check"
if MTM_PANE_ID=$agent "$bin" note "$copy" --item "Archive and restore" --author owner "not me" 2> "$work/refused2"; then fail "agent signed as the owner"; fi
grep -q "may not sign as the owner" "$work/refused2" || fail "owner refusal message"
MTM_PANE_ID=99999999-2222-3333-4444-555555555555 "$bin" note "$copy" --item "Archive and restore" "from a shell pane" > /dev/null
grep -q '> \*\*owner\*\* [0-9-]*: from a shell pane' "$copy" || fail "a pane without an agent is the owner"

# 2b. The extension: catalog, doc list over a temp root, a verb on a target.
export O_MD_ROOTS="$work" XDG_CONFIG_HOME="$work/config" MTM_PROJECT_DIRS=
"$bin" catalog > "$work/catalog.json"
grep -q '"kind": "markdown"' "$work/catalog.json" || fail "catalog: markdown detail"
"$bin" doc list --json '{}' > "$work/docs.json"
grep -q '"path": *"'"$copy"'"' "$work/docs.json" || grep -q "features.md" "$work/docs.json" || fail "doc list"
"$bin" doc uncheck "Archive and restore" --target "$copy" > /dev/null
grep -q '^- \[ \] Archive and restore' "$copy" || fail "doc uncheck"

# 3. A bare o-md is a scratch buffer: type a checklist, quit, reopen with -r.
python3 "$keys" '["o", "- [ ] scratch task", {"key":"esc"}, {"sleep":0.5}, "q"]' -- "$bin"
"$bin" scratch --json | tee "$work/scratch.jsonl"
grep -q '"first":"- \[ \] scratch task"' "$work/scratch.jsonl" || fail "scratch file"
"$bin" -r --smoke | grep -q 'scratch task' || fail "-r reopens the scratch buffer"
python3 "$keys" '["q"]' -- "$bin" -r
echo "smoke: ok"
