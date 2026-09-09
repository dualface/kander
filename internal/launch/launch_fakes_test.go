package launch

import (
	"os"
	"testing"
)

func writeFakeTmux(t *testing.T, path, log string) {
	t.Helper()
	script := `#!/bin/sh
log="${KANBAN_TMUX_LOG:-` + log + `}"
printf '%s\n' "$1" >> "$log.order"
if [ "$1" = "display-message" ]; then
  case "${5:-}" in
    *pane_current_command*pane_in_mode*pane_dead*)
      current="${KANBAN_TMUX_CURRENT_COMMAND:-}"
      [ -n "$current" ] || { [ -f "$log.current" ] && current=$(sed -n '1p' "$log.current"); }
      printf '%s\t%s\t%s\n' "${current:-codex}" "${KANBAN_TMUX_IN_MODE:-0}" "${KANBAN_TMUX_DEAD:-0}"
      exit 0
      ;;
    *pane_current_command*pane_dead*)
      printf '%s\t0\n' "${KANBAN_TMUX_CURRENT_COMMAND:-codex}"
      exit 0
      ;;
    '#{session_id}')
      printf '%s\n' '$42'
      exit 0
      ;;
  esac
  printf '%s\n' '$42'
  exit 0
fi
if [ "$1" = "has-session" ]; then
  for name in ${KANBAN_TMUX_SESSIONS:-}; do
    [ "$name" = "${3#=}" ] && exit 0
  done
  exit 1
fi
if [ "$1" = "show-options" ]; then
  if [ "${2:-}" = "-p" ]; then
    if [ -n "${KANBAN_TMUX_PANE_SESSION:-}" ]; then
      printf '%s\n' "$KANBAN_TMUX_PANE_SESSION"
    elif [ -f "$log.pane-session" ]; then
      sed -n '1p' "$log.pane-session"
    else
      exit 1
    fi
    exit 0
  fi
  [ -n "${KANBAN_TMUX_PROJECT:-}" ] || exit 1
  printf '%s\n' "$KANBAN_TMUX_PROJECT"
  exit 0
fi
if [ "$1" = "set-option" ] && [ "$2" = "-p" ]; then
  printf '%s\n' "$@" > "$log.pane-setopt"
  if [ "${KANBAN_TMUX_PANE_SETOPT_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake pane setopt failure' >&2
    exit 1
  fi
  printf '%s\n' "$6" > "$log.pane-session"
  exit 0
fi
if [ "$1" = "set-option" ]; then
  printf '%s\n' "$@" > "$log.setopt"
  exit 0
fi
if [ "$1" = "kill-window" ]; then
  printf '%s\n' "$@" > "$log.kill"
  exit 0
fi
if [ "$1" = "send-keys" ]; then
  printf '%s\n' "$@" >> "$log.send-keys"
  exit 0
fi
if [ "$1" = "respawn-pane" ]; then
  printf '%s\n' "$5" > "$log.command"
  command=${5#exec }; current=${command%% *}
  printf '%s\n' "${current##*/}" > "$log.current"
  if [ "${current##*/}" = "codex" ]; then
    task=$(printf '%s\n' "$5" | grep -Eo '[0-9]{8}-[a-z0-9-]+-task' | head -n 1)
    if [ -n "$task" ]; then
      session_dir="${CODEX_HOME:-$HOME/.codex}/sessions/fake"
      mkdir -p "$session_dir"
      printf '%s\n' '{"type":"session_meta","payload":{"id":"fake-codex-session"}}' > "$session_dir/rollout-$task.jsonl"
      printf '{"type":"event_msg","payload":{"type":"user_message","message":"执行 Kanban 任务 %s; full instructions are in the UTF-8 task file at /tmp/task.md; read the complete file first and follow it exactly."}}\n' "$task" >> "$session_dir/rollout-$task.jsonl"
    fi
  fi
  if [ -n "${KANBAN_TMUX_MUTATE_CARD:-}" ]; then
    printf '%s\n' '# agent mutation' >> "$KANBAN_TMUX_MUTATE_CARD"
  fi
  if [ "${KANBAN_TMUX_RESPAWN_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake tmux respawn failure' >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "capture-pane" ]; then
  printf '%s\n' "$@" >> "$log.capture"
  if [ -n "${KANBAN_TMUX_PANE_OUTPUT:-}" ] || [ -n "${KANBAN_TMUX_READY_AFTER:-}" ] || [ -n "${KANBAN_TMUX_BLOCKED_OUTPUT:-}" ] || [ -f "$log.pane-output" ]; then
    count=0
    [ -f "$log.ready-count" ] && count=$(cat "$log.ready-count")
    count=$((count+1))
    echo "$count" > "$log.ready-count"
    after="${KANBAN_TMUX_READY_AFTER:-0}"
    if [ "$after" -gt 0 ] 2>/dev/null && [ "$count" -lt "$after" ]; then
      printf '%s\n' "${KANBAN_TMUX_PRE_READY_OUTPUT:-starting}"
      exit 0
    fi
    if [ -n "${KANBAN_TMUX_BLOCKED_OUTPUT:-}" ]; then
      printf '%s\n' "$KANBAN_TMUX_BLOCKED_OUTPUT"
      exit 0
    fi
    if [ -f "$log.pane-output" ]; then
      cat "$log.pane-output"
      exit 0
    fi
    printf '%s\n' "${KANBAN_TMUX_PANE_OUTPUT:-}"
    exit 0
  fi
  exit 1
fi
printf '%s\n' "$@" > "$log"
if [ "${KANBAN_TMUX_FAIL:-}" = "1" ]; then
  printf '%s\n' 'fake tmux failure' >&2
  exit 1
fi
printf '%s\t%s\n' '@9' '%9'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakeAgent(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
if [ "$1" = "create-chat" ]; then
  if [ "${KANBAN_CURSOR_CHAT_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake create-chat failure' >&2
    exit 1
  fi
  printf '%s\n' "${KANBAN_CURSOR_CHAT_ID:-chat-fake-0001}"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakeHerdr(t *testing.T, path, log string) {
	t.Helper()
	script := `#!/bin/sh
log="${KANBAN_HERDR_LOG}"
if [ "$1" = "tab" ] && [ "$2" = "create" ]; then
  printf '%s\n' "tab create" >> "$log.order"
  printf '%s\n' "$@" > "$log.create"
  if [ "${KANBAN_HERDR_CREATE_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr create failure' >&2
    exit 1
  fi
  printf '%s\n' '{"id":"cli:tab:create","result":{"type":"tab_created","tab":{"tab_id":"w1:t9"},"root_pane":{"pane_id":"w1:p9","tab_id":"w1:t9"}}}'
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "wait-output" ]; then
  printf '%s\n' "pane wait-output" >> "$log.order"
  printf '%s\n' "$@" > "$log.wait"
  printf '%s\n' "$@" >> "$log.wait-all"
  recent=0
  prev=""
  for a in "$@"; do
    if [ "$prev" = "--source" ] && [ "$a" = "recent" ]; then
      recent=1
    fi
    prev="$a"
  done
  if [ "$recent" = "1" ]; then
    count=0
    [ -f "$log.ready-count" ] && count=$(cat "$log.ready-count")
    count=$((count+1))
    echo "$count" > "$log.ready-count"
    after="${KANBAN_HERDR_READY_AFTER:-0}"
    if [ "$after" -gt 0 ] 2>/dev/null && [ "$count" -lt "$after" ]; then
      exit 1
    fi
  fi
  if [ "${KANBAN_HERDR_WAIT_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr wait failure' >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "run" ]; then
  printf '%s\n' "pane run" >> "$log.order"
  printf '%s\n' "$@" > "$log.run"
  if [ "${KANBAN_HERDR_RUN_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr run failure' >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "agent" ] && [ "$2" = "prompt" ]; then
  printf '%s\n' "agent prompt" >> "$log.order"
  printf '%s\n' "$@" >> "$log.prompt"
  if [ "${KANBAN_HERDR_PROMPT_FAIL:-}" = "1" ]; then
    printf '%s\n' 'approval required' >&2
    exit 1
  fi
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "get" ]; then
  if [ -n "${KANBAN_HERDR_PANE_JSON:-}" ]; then
    printf '%s\n' "$KANBAN_HERDR_PANE_JSON"
    exit 0
  fi
  agent="${KANBAN_HERDR_AGENT:-claude}"
  status="${KANBAN_HERDR_STATUS:-idle}"
  printf '%s\n' "{\"id\":\"cli:pane:get\",\"result\":{\"pane\":{\"pane_id\":\"$3\",\"tab_id\":\"w1:t9\",\"agent\":\"$agent\",\"agent_status\":\"$status\",\"agent_session\":{\"value\":\"${KANBAN_HERDR_SESSION:-}\"}}}}"
  exit 0
fi
if [ "$1" = "tab" ] && [ "$2" = "close" ]; then
  printf '%s\n' "$@" > "$log.close"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "read" ]; then
  count=0
  [ -f "$log.ready-count" ] && count=$(cat "$log.ready-count")
  after="${KANBAN_HERDR_READY_AFTER:-0}"
  if [ -n "${KANBAN_HERDR_BLOCKED_OUTPUT:-}" ]; then
    printf '%s\n' "$KANBAN_HERDR_BLOCKED_OUTPUT"
    exit 0
  fi
  if [ "$after" -gt 0 ] 2>/dev/null && [ "$count" -lt "$after" ]; then
    printf '%s\n' "${KANBAN_HERDR_PRE_READY_OUTPUT:-starting}"
    exit 0
  fi
  if [ -n "${KANBAN_HERDR_AGENT_OUTPUT:-}" ]; then
    printf '%s\n' "$KANBAN_HERDR_AGENT_OUTPUT"
    exit 0
  fi
  printf '%s\n' "${KANBAN_HERDR_OUTPUT:-}"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
