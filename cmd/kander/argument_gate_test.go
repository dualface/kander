package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
)

func unboundCheckout(t *testing.T) { checkout(t, "") }

// boundCheckout is a checkout bound to a project of a service that is not there: a command line
// that is accepted ends in a connection failure, so a usage error is only the argument check.
func boundCheckout(t *testing.T) { checkout(t, "demo") }

func checkout(t *testing.T, project string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "forbidden-board"))
	cfg := config.DefaultConfig()
	cfg.Server.URL = "http://127.0.0.1:1"
	cfg.Project.Name = project
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
		{"decision"},
		{"decision", "record", sampleTask, "--kind", "human"},
		{"decision", "record", sampleTask, "--kind", "unknown", "--file", "input.json"},
		{"decision", "mode", sampleTask, "unknown"},
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

// An empty --message or --message-file, or a blank inline message, is refused with the arguments
// of notify, resume and orchestrate: usage and exit 2 whether or not the checkout is bound,
// where the command body would only reject it after the binding or the service.
func TestEmptyMessageOptionsAreArgumentErrors(t *testing.T) {
	for name, setup := range map[string]func(*testing.T){"unbound": unboundCheckout, "bound": boundCheckout} {
		t.Run(name, func(t *testing.T) {
			setup(t)
			for _, command := range []string{"notify", "resume", "orchestrate"} {
				for _, option := range [][]string{
					{"--message-file="},
					{"--message-file", ""},
					{"--message="},
					{"--message", "   "},
				} {
					args := append([]string{"--json", command, sampleTask}, option...)
					result := runKander(t, args...)
					var body struct {
						Error struct{ Code string }
					}
					if result.code != 2 || json.Unmarshal([]byte(result.stderr), &body) != nil || body.Error.Code != "usage" {
						t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
					}
				}
			}
		})
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
		{"guard-write", "somewhere"},
		{"pick", sampleTask},
		{"start", sampleTask},
	} {
		result := runKander(t, args...)
		if result.code != 1 || !strings.Contains(result.stderr, config.Text("remote.not_configured")) {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
	// A schema is static text: it needs no binding, and an explicit false does not lift it. Every
	// review subcommand that has a schema is read from the tree, so a new one is covered too and
	// the exemption of any of them cannot be dropped unnoticed.
	var withSchema []string
	arguments := map[string]int{}
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		if strings.HasPrefix(leaf.path, "review ") && leaf.Flags().Lookup("schema") != nil {
			name := strings.TrimPrefix(leaf.path, "review ")
			withSchema = append(withSchema, name)
			arguments[name] = strings.Count(leaf.Use, "<")
		}
	}
	if !slices.Contains(withSchema, "abandon") || !slices.Contains(withSchema, "plan") {
		t.Fatalf("review subcommands with a schema: %v", withSchema)
	}
	for _, name := range withSchema {
		result := runKander(t, "review", name, "--schema", "--json")
		var body map[string]any
		if result.code != 0 || result.stderr != "" || json.Unmarshal([]byte(result.stdout), &body) != nil || body["command"] != name || body["schema"] == nil {
			t.Errorf("review %s --schema --json: exit=%d stderr=%q stdout=%q", name, result.code, result.stderr, result.stdout)
		}
		args := append([]string{"review", name, "--schema=false"}, slices.Repeat([]string{"/abs"}, arguments[name])...)
		if result := runKander(t, args...); result.code != 1 || !strings.Contains(result.stderr, config.Text("remote.not_configured")) {
			t.Errorf("review %s --schema=false: exit=%d stderr=%s", name, result.code, result.stderr)
		}
	}
	// Choosing from a list needs a terminal, with or without a binding.
	for _, args := range [][]string{{"--json", "pick"}, {"--json", "start"}} {
		if result := runKander(t, args...); result.code != 2 {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
}
