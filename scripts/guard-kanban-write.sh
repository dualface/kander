#!/bin/sh
# 看板写入护栏 hook 样例: 从 stdin 读取 Claude Code PreToolUse JSON,
# 把 tool_input.file_path 交给 kander guard-write 判定.
# 退出码 0 放行; 2 阻止本次工具调用 (stderr 会回显给 Agent).
# 依赖 jq; 缺 jq、无 payload 或无 file_path 时放行, 不阻塞非看板写入.
# 用法见 docs/kanban-write-guard.md. KANDER_BIN 可覆盖 kander 命令路径.

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
