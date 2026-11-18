package notify

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func resetLang(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})
}

func setupBoard(t *testing.T) (root, fakeBin string) {
	t.Helper()
	resetLang(t)
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root = t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(root, "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "cn"
	cfg.AgentLanguage = "zh-CN"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	fakeBin = filepath.Join(root, "fake-bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeHerdr(t, filepath.Join(fakeBin, "herdr"), filepath.Join(root, "herdr.log"))
	writeFakeTmux(t, filepath.Join(fakeBin, "tmux"), filepath.Join(root, "tmux.log"))
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KANBAN_HERDR_LOG", filepath.Join(root, "herdr.log"))
	t.Setenv("KANBAN_TMUX_LOG", filepath.Join(root, "tmux.log"))
	return root, fakeBin
}

func writeFakeHerdr(t *testing.T, path, log string) {
	t.Helper()
	script := `#!/bin/sh
log="${KANBAN_HERDR_LOG:-` + log + `}"
if [ "$1" = "tab" ] && [ "$2" = "create" ]; then
  printf '%s\n' "$@" >> "$log.order"
  printf '%s\n' '{"id":"cli:tab:create","result":{"type":"tab_created","tab":{"tab_id":"w1:t9"},"root_pane":{"pane_id":"w1:p9","tab_id":"w1:t9"}}}'
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "wait-output" ]; then
  printf '%s\n' "$@" >> "$log.wait"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "run" ]; then
  printf '%s\n' "$@" >> "$log.run"
  printf '%s\n' "tab create" >> "$log.order"
  exit 0
fi
if [ "$1" = "agent" ] && [ "$2" = "prompt" ]; then
  if [ "${KANBAN_HERDR_PROMPT_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr prompt failure' >&2
    exit 1
  fi
  printf '%s\n' "$@" > "$log.prompt"
  printf '%s\n' "$@" >> "$log.order"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "get" ]; then
  if [ "${KANBAN_HERDR_GET_FAIL:-}" = "1" ]; then
    printf '%s\n' 'fake herdr get failure' >&2
    exit 1
  fi
  if [ -n "${KANBAN_HERDR_STALE_PANE:-}" ] && [ "$3" = "$KANBAN_HERDR_STALE_PANE" ]; then
    printf '%s\n' '{"error":{"code":"pane_not_found","message":"gone"}}' >&2
    exit 1
  fi
  if [ "${KANBAN_HERDR_BUSY_ONCE:-}" = "1" ] && [ ! -f "$log.busy" ]; then
    printf '%s\n' busy > "$log.busy"
    status=blocked
  else
    status="${KANBAN_HERDR_STATUS:-idle}"
  fi
  agent="${KANBAN_HERDR_AGENT:-claude}"
  tab="${KANBAN_HERDR_TAB_ID:-w1:t9}"
  session="${KANBAN_HERDR_SESSION:-session-1}"
  if [ -n "${KANBAN_HERDR_SESSION_FILE:-}" ]; then
    session=""
    [ ! -f "$KANBAN_HERDR_SESSION_FILE" ] || session=$(cat "$KANBAN_HERDR_SESSION_FILE")
  fi
  kind="${KANBAN_HERDR_SESSION_KIND:-}"
  if [ -f "$log.prompt" ]; then
    printf '%s\n' "{\"id\":\"cli:pane:get\",\"result\":{\"pane\":{\"pane_id\":\"$3\",\"tab_id\":\"$tab\"}}}"
    exit 0
  fi
  if [ -n "$kind" ]; then
    printf '%s\n' "{\"id\":\"cli:pane:get\",\"result\":{\"pane\":{\"pane_id\":\"$3\",\"tab_id\":\"$tab\",\"agent\":\"$agent\",\"agent_status\":\"$status\",\"agent_session\":{\"kind\":\"$kind\",\"value\":\"$session\"}}}}"
  else
    printf '%s\n' "{\"id\":\"cli:pane:get\",\"result\":{\"pane\":{\"pane_id\":\"$3\",\"tab_id\":\"$tab\",\"agent\":\"$agent\",\"agent_status\":\"$status\",\"agent_session\":{\"value\":\"$session\"}}}}"
  fi
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "process-info" ]; then
  printf '%s\n' "$KANBAN_HERDR_PROCESS_JSON"
  exit 0
fi
if [ "$1" = "pane" ] && [ "$2" = "list" ]; then
  if [ -n "${KANBAN_HERDR_LIST_JSON:-}" ]; then
    printf '%s\n' "$KANBAN_HERDR_LIST_JSON"
    exit 0
  fi
  session="${KANBAN_HERDR_SESSION:-session-1}"
  agent="${KANBAN_HERDR_AGENT:-claude}"
  printf '%s\n' "{\"id\":\"cli:pane:list\",\"result\":{\"type\":\"pane_list\",\"panes\":[{\"pane_id\":\"w1:p9\",\"tab_id\":\"w1:t9\",\"agent\":\"$agent\",\"agent_status\":\"idle\",\"agent_session\":{\"value\":\"$session\"}}]}}"
  exit 0
fi
if [ "$1" = "tab" ] && [ "$2" = "close" ]; then
  printf '%s\n' "$@" > "$log.close"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFakeTmux(t *testing.T, path, log string) {
	t.Helper()
	script := `#!/bin/sh
log="${KANBAN_TMUX_LOG:-` + log + `}"
if [ "$1" = "display-message" ]; then
  target=""
  i=1
  while [ "$i" -le "$#" ]; do
    eval "arg=\${$i}"
    if [ "$arg" = "-t" ]; then
      i=$((i+1))
      eval "target=\${$i}"
    fi
    i=$((i+1))
  done
  if [ -n "${KANBAN_TMUX_STALE_PANE:-}" ] && [ "$target" = "$KANBAN_TMUX_STALE_PANE" ]; then
    printf '%s\n' "can't find pane $target" >&2
    exit 1
  fi
  case "${5:-$6}" in
    *pane_current_command*pane_in_mode*pane_dead*)
      dead="${KANBAN_TMUX_DEAD:-0}"
      [ -f "$log.instruction" ] && dead=1
      printf '%s\t%s\t%s\n' "${KANBAN_TMUX_CURRENT_COMMAND:-claude}" "${KANBAN_TMUX_IN_MODE:-0}" "$dead"
      exit 0
      ;;
    *session_id*session_name*window_id*window_panes*)
      printf '%s\t%s\t%s\t%s\n' "${KANBAN_TMUX_TARGET_SESSION:-\$42}" "${KANBAN_TMUX_TARGET_SESSION_NAME:-fake-session}" "${KANBAN_TMUX_TARGET_WINDOW:-@9}" "${KANBAN_TMUX_WINDOW_PANES:-1}"
      exit 0
      ;;
    '#{window_id}')
      printf '%s\n' "$target"
      exit 0
      ;;
  esac
  printf '%s\n' '$42'
  exit 0
fi
if [ "$1" = "show-options" ]; then
  printf '%s\n' "${KANBAN_TMUX_PANE_SESSION:-session-1}"
  exit 0
fi
if [ "$1" = "list-panes" ]; then
  printf '%s\n' "$KANBAN_TMUX_LIST_PANES"
  exit 0
fi
if [ "$1" = "send-keys" ]; then
  printf '%s\n' "$@" >> "$log.send"
  printf '%s\n' "$5" >> "$log.instruction"
  exit 0
fi
if [ "$1" = "capture-pane" ]; then
  if [ ! -f "$log.send" ]; then
    printf '%s\n' "$@" >> "$log.capture-before-send"
  fi
  # The pane echoes every delivered key, submitted or not.
  cat "$log.send" 2>/dev/null
  printf '\n'
  # KANBAN_TMUX_AGENT_ACK simulates an agent that read the message file and output its marker.
  if [ "${KANBAN_TMUX_AGENT_ACK:-}" = "1" ] && [ -f "$log.instruction" ]; then
    message=$(grep -o '/[^ ,]*/message\.txt' "$log.instruction" | tail -n 1)
    [ -z "$message" ] || grep -o 'KANDER-NOTIFY-ACK:[0-9a-f]*' "$message"
  fi
  exit 0
fi
if [ "$1" = "kill-window" ]; then
  printf '%s\n' "$@" > "$log.kill"
  exit 0
fi
printf '%s\t%s\n' '@9' '%9'
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
