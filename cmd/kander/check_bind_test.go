package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
)

// This file intentionally does not import internal/liveness. The production
// package graph (blank imports plus notify/takeover) must register check with
// liveness probing; rebinding to board.RunCheck or leaving it unimplemented fails.

func TestCheckCommandUsesLivenessInFullBinary(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})

	got := cli.Commands["check"]
	if got == nil {
		t.Fatal("check is not registered")
	}
	if reflect.ValueOf(got).Pointer() == reflect.ValueOf(board.RunCheck).Pointer() {
		t.Fatal("check bound to board.RunCheck; full binary must use liveness.RunCheck")
	}

	root := t.TempDir()
	for _, state := range board.States {
		if err := os.Mkdir(filepath.Join(root, state), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(board.EnvBoardDir, root)

	slug := "full-binary-check"
	created, err := board.NewTask(root, "chore", slug, "binding", false)
	if err != nil {
		t.Fatal(err)
	}
	path := created
	id := strings.TrimSuffix(filepath.Base(path), ".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, replacement := range []string{"实现目标", "产生可验证结果", "满足验收", "无额外范围"} {
		text = strings.Replace(text, "<填写>", replacement, 1)
	}
	text = strings.Replace(text, "## 讨论与决策\n", "## 讨论与决策\n\n自审: 通过\n卡审: 通过\n", 1)
	text = strings.Replace(text, "- 任务分支:\n", "- 任务分支: task/"+slug+"\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	moved, err := board.MoveEntry(board.Entry{
		TaskID: id, State: "backlog", Path: path, Document: path, Kind: "small",
	}, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	moved, err = board.MoveEntry(moved, root, "working")
	if err != nil {
		t.Fatal(err)
	}
	_ = moved

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutW, stderrW
	code := got(nil)
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	outBytes, _ := io.ReadAll(stdoutR)
	errBytes, _ := io.ReadAll(stderrR)
	_ = stdoutR.Close()
	_ = stderrR.Close()
	out := string(outBytes)
	if code != 0 {
		t.Fatalf("check exit=%d stderr=%s stdout=%s", code, errBytes, out)
	}
	if !strings.Contains(out, "存活: "+id) {
		t.Fatalf("full binary check must emit liveness output; got %q", out)
	}
}
