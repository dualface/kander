#!/bin/sh
# Example board write guard hook: read the Claude Code PreToolUse JSON from stdin
# and hand tool_input.file_path to kander guard-write for a verdict.
# Exit code 0 allows; 2 blocks this tool call (stderr is echoed back to the agent).
# Requires jq; a missing jq, payload or file_path allows the write, so non-board writes are never blocked.
# See docs/kanban-write-guard.md for usage. KANDER_BIN overrides the path to the kander command.

set -u

KANDER_BIN="${KANDER_BIN:-kander}"

command -v jq >/dev/null 2>&1 || exit 0
command -v "$KANDER_BIN" >/dev/null 2>&1 || exit 0

payload=$(cat) || exit 0
path=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // empty' 2>/dev/null) || exit 0
[ -n "$path" ] || exit 0

if ! "$KANDER_BIN" guard-write "$path" 1>&2; then
    exit 2
fi
exit 0
