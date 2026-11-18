package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func unboundCheckout(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "forbidden-board"))
	cfg := config.DefaultConfig()
	cfg.Server.URL = "http://127.0.0.1:1"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// A value the contract enumerates, and a required or exclusive option, is an argument error:
// text on stderr and exit 2, and the usage code with --json, in a bound checkout or not.
func TestContractEnumerationsAndRequirementsAreArgumentErrors(t *testing.T) {
	unboundCheckout(t)
	for _, args := range [][]string{
		{"list", "wokring"},
		{"list", "--json", "wokring"},
		{"new", "nonsense", "slug", "title"},
		{"request", sampleTask, "nonsense", "text"},
		{"request", "answer", sampleTask, "request-1", "nonsense"},
		{"notify", sampleTask},
		{"notify", sampleTask, "--message", "a", "--message-file", "/abs/b.md"},
		{"resume", sampleTask},
		{"resume", sampleTask, "--message", "a", "--message-file", "/abs/b.md"},
		{"orchestrate", sampleTask},
		{"orchestrate", sampleTask, "--message", "a", "--message-file", "/abs/b.md"},
		{"review", "/abs", "base", "head", "Nobody", "goal"},
		{"review", "codex", "/abs", "base", "head", "Nobody", "goal"},
	} {
		result := runKander(t, args...)
		if result.code != 2 {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
		if strings.Contains(result.stderr, config.Text("remote.not_configured")) {
			t.Errorf("%v: the binding was checked before the arguments: %s", args, result.stderr)
		}
	}
	// With --json the error object carries the usage code.
	for _, args := range [][]string{
		{"--json", "list", "wokring"},
		{"--json", "new", "nonsense", "slug", "title"},
		{"--json", "notify", sampleTask},
		{"--json", "review", "/abs", "base", "head", "Nobody", "goal"},
	} {
		result := runKander(t, args...)
		var body struct {
			Error struct{ Code string }
		}
		if result.code != 2 || json.Unmarshal([]byte(result.stderr), &body) != nil || body.Error.Code != "usage" {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
}

// The project binding is checked after the arguments and before anything the command does: a
// missing option wins over the missing binding, and a missing input file does not.
func TestProjectGateStandsBehindArgumentsAndBeforeTheWork(t *testing.T) {
	unboundCheckout(t)
	missing := runKander(t, "issue", "result", "42")
	if missing.code != 2 || !strings.Contains(missing.stderr, "--card") {
		t.Fatalf("issue result without --card: exit=%d stderr=%s", missing.code, missing.stderr)
	}
	for _, args := range [][]string{
		{"update", sampleTask, "--document", "spec.md", "--file", "/nonexistent/input.md", "--expect-revision", "1"},
		{"review", "plan", "--schema=false", "/abs", "/abs/plan.json"},
		{"guard-write", "somewhere"},
		{"pick", sampleTask},
		{"start", sampleTask},
	} {
		result := runKander(t, args...)
		if result.code != 1 || !strings.Contains(result.stderr, config.Text("remote.not_configured")) {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
	// A schema is static text: it needs no binding, and an explicit false does not lift it.
	if result := runKander(t, "review", "plan", "--schema"); result.code != 0 {
		t.Errorf("review plan --schema: exit=%d stderr=%s", result.code, result.stderr)
	}
	// Choosing from a list needs a terminal, with or without a binding.
	for _, args := range [][]string{{"--json", "pick"}, {"--json", "start"}} {
		if result := runKander(t, args...); result.code != 2 {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
}
