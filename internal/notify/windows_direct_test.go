package notify

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// fakeHerdrNotifySource answers only the three subcommands direct delivery uses.
// It is compiled to a binary rather than written as a /bin/sh script so the
// herdr direct-delivery path is covered on Windows too.
const fakeHerdrNotifySource = `package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) >= 3 && args[0] == "pane" && args[1] == "get" {
		payload, _ := json.Marshal(map[string]any{
			"result": map[string]any{
				"type": "pane_info",
				"pane": map[string]any{
					"pane_id":       args[2],
					"tab_id":        "w1:t1",
					"agent":         "codex",
					"agent_status":  "idle",
					"agent_session": map[string]any{"value": "session-x"},
				},
			},
		})
		fmt.Println(string(payload))
		return
	}
	if len(args) >= 2 && args[0] == "agent" && args[1] == "prompt" {
		return
	}
	if len(args) >= 2 && args[0] == "pane" && args[1] == "wait-output" {
		return
	}
	fmt.Fprintln(os.Stderr, "unexpected herdr args")
	os.Exit(1)
}
`

func buildFakeHerdrNotify(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte(fakeHerdrNotifySource), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "herdr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, name), source)
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake herdr: %v %s", err, out)
	}
	return dir
}

// herdr has a native Windows build, so direct delivery is no longer rejected by
// platform: with a usable target it must really deliver instead of silently
// falling back to resume. This only creates directories and a card file, no
// POSIX fakes, so the case runs on Windows too.
func TestNotifyDeliversThroughHerdrOnEveryPlatform(t *testing.T) {
	resetLang(t)
	root := t.TempDir()
	t.Setenv(config.EnvConfig, filepath.Join(root, "config.json"))
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)
	t.Setenv("PATH", buildFakeHerdrNotify(t))

	taskID := "20260906-notify-gate-task"
	card := "# 冒烟" + "\n" + "\n" + "- 类型: Chore" + "\n" + "- 会话: codex session-x" + "\n" + "- 窗口: herdr:w1:t1:w1:p1" + "\n"
	if err := os.WriteFile(filepath.Join(root, "working", taskID+".md"), []byte(card), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatalf("直投失败: %v", err)
	}
	if !strings.Contains(out, "herdr-direct") {
		t.Fatalf("没有走 herdr 直投: %s", out)
	}
}
