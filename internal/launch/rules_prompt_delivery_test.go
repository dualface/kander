package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestKanbanRulesKeepArgvPromisesAndDocumentPane(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "rules", "KANDER-KANBAN-RULES.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, keep := range []string{
		"Templates replace `{model}`, `{effort}`, `{session}` within argv elements; Kander appends the prompt last. No shell interpolation is used.",
		"The agent command line receives only one instruction containing that absolute path.",
		"The temporary task file of `start` contains only the task ID and fixed requirements; the agent command line receives only one instruction to read that file.",
		"herdr counts as started once `pane run` succeeds; the subsequent session identity report and read-back are best-effort, failures only warn and do not enter the `LaunchFailure` tab close and card rollback path.",
		"tmux/tmux-session count as started only after session discovery and pane marker write succeed.",
	} {
		if !strings.Contains(text, keep) {
			t.Fatalf("missing argv promise: %s", keep)
		}
	}
	for _, added := range []string{
		"When `prompt_delivery.mode` is `pane`, the prompt is not appended to argv",
		"An agent whose `prompt_delivery.mode` is `pane` is rejected before claiming when the resolved launcher is `foreground` or `console`",
		"When `prompt_delivery.mode` is `pane`, herdr counts as started once `pane run` succeeds and prompt delivery succeeds.",
		"When `prompt_delivery.mode` is `pane`, tmux/tmux-session count as started only after prompt delivery succeeds and session discovery and pane marker write succeed.",
		"`blocked` match, prompt-delivery rejection, or ready timeout",
	} {
		if !strings.Contains(text, added) {
			t.Fatalf("missing pane clause: %s", added)
		}
	}
}
