package launch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

func TestCustomDialectArguments(t *testing.T) {
	session := AgentSession{Agent: "alias", Reference: "session-id"}
	for _, test := range []struct {
		dialect       string
		start, resume []string
	}{
		{"codex", []string{"--model", "model", "--config", `model_reasoning_effort="high"`, "--dangerously-bypass-approvals-and-sandbox"}, []string{"resume", "--model", "model", "--config", `model_reasoning_effort="high"`, "--dangerously-bypass-approvals-and-sandbox", "session-id"}},
		{"claude", []string{"--model", "model", "--effort", "high", "--dangerously-skip-permissions", "--session-id", "session-id"}, []string{"--model", "model", "--effort", "high", "--dangerously-skip-permissions", "--resume", "session-id"}},
		{"grok", []string{"--model", "model", "--effort", "high", "--permission-mode", "bypassPermissions", "--session-id", "session-id"}, []string{"--model", "model", "--effort", "high", "--permission-mode", "bypassPermissions", "--resume", "session-id"}},
		{"cursor", []string{"--model", "model", "--trust", "--force", "--resume", "session-id"}, []string{"--model", "model", "--trust", "--force", "--resume", "session-id"}},
	} {
		t.Run(test.dialect, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Agents = map[string]config.AgentDefinition{"alias": {Dialect: test.dialect}}
			for _, resume := range []bool{false, true} {
				want := test.start
				if resume {
					want = test.resume
				}
				got, err := agentArguments("alias", map[string]string{"small_model": "model", "small_effort": "high"}, "small", session, resume, cfg)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("%q != %q: %v", got, want, err)
				}
				builtin, err := agentArguments(test.dialect, map[string]string{"small_model": "model", "small_effort": "high"}, "small", session, resume)
				if err != nil || !reflect.DeepEqual(got, builtin) {
					t.Fatalf("default drift %q %q", got, builtin)
				}
			}
		})
	}
}

func TestAgentTemplatePrecedenceAndSessions(t *testing.T) {
	_, _, bin := setupBoard(t)
	cfg := envConfig("custom", "tmux", nil)
	cfg.Agents = map[string]config.AgentDefinition{"custom": {Path: filepath.Join(bin, "claude"), Dialect: "claude", Args: &config.AgentArgs{Start: []string{"start", "--model", "{model}", "--id", "{session}"}, Resume: []string{"again", "{session}"}}, Session: &config.AgentSessionDefinition{Mode: "generated"}}}
	program, err := requireAgentProgram("custom", cfg)
	if err != nil || program.Path != filepath.Join(bin, "claude") {
		t.Fatalf("%+v %v", program, err)
	}
	session, err := newAgentSession("custom", program, cfg)
	if err != nil || len(session.Reference) != 36 {
		t.Fatalf("%+v %v", session, err)
	}
	args, err := agentArguments("custom", nil, "small", session, false, cfg)
	if err != nil || !reflect.DeepEqual(args, []string{"start", "--id", session.Reference}) {
		t.Fatalf("%q %v", args, err)
	}
	args, err = agentArguments("custom", nil, "small", session, true, cfg)
	if err != nil || !reflect.DeepEqual(args, []string{"again", session.Reference}) {
		t.Fatalf("%q %v", args, err)
	}
	d := cfg.Agents["custom"]
	d.Session.Mode = "none"
	cfg.Agents["custom"] = d
	if _, err := resolvedTaskSession("task", "- SESSION: custom id\n", cfg); err == nil || !strings.Contains(err.Error(), "none") {
		t.Fatal(err)
	}
	args, err = agentArguments("custom", nil, "small", session, false, cfg)
	if err != nil || !reflect.DeepEqual(args, []string{"start"}) {
		t.Fatalf("%q %v", args, err)
	}
	if _, err := agentArguments("custom", nil, "small", session, true, cfg); err == nil {
		t.Fatal("none resumed")
	}
}

func TestAllocatedAgentSession(t *testing.T) {
	_, _, bin := setupBoard(t)
	allocator := filepath.Join(bin, "allocator")
	for _, test := range []struct {
		output, field string
		exit          int
		bad           bool
	}{
		{"allocated-id", "", 0, false}, {`{"id":"json-id"}`, "id", 0, false}, {`{"id":42}`, "id", 0, true}, {"two\nlines", "", 0, true}, {"ok", "", 1, true},
	} {
		script := "#!/bin/sh\nprintf '%s' '" + test.output + "'\nexit " + itoa(test.exit) + "\n"
		if err := os.WriteFile(allocator, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		id, err := allocateAgentSession(&config.AgentSessionDefinition{Mode: "allocated", Allocate: []string{allocator, "literal $(no-shell)"}, JSONField: test.field})
		if (err != nil) != test.bad || (!test.bad && id == "") {
			t.Fatalf("id=%q err=%v", id, err)
		}
	}
}

func TestTemplateStartAppendsPromptAndUsesConfiguredProgram(t *testing.T) {
	root, _, bin := setupBoard(t)
	cfg := envConfig("custom", "tmux", nil)
	cfg.Agents = map[string]config.AgentDefinition{"custom": {Path: filepath.Join(bin, "claude"), ProcessName: "node", Args: &config.AgentArgs{Start: []string{"--model", "{model}", "--session-id", "{session}"}, Resume: []string{"--resume", "{session}"}}, Session: &config.AgentSessionDefinition{Mode: "generated"}}}
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	var got []string
	previous := newShellInvocation
	newShellInvocation = func(p process.AgentProgram, args []string, env map[string]string) (process.ProcessInvocation, error) {
		got = append([]string{p.Path}, args...)
		return previous(p, args, env)
	}
	t.Cleanup(func() { newShellInvocation = previous })
	id, _ := makeTodo(t, root, "custom-template")
	_, _, err := capture(t, func() error { return commandStart(root, "custom", "tmux", id) })
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != filepath.Join(bin, "claude") || got[1] != "--session-id" || !strings.Contains(got[len(got)-1], "UTF-8") {
		t.Fatalf("%q", got)
	}
}

func TestNoneDialectArgumentsDoNotReuseTerminalIdentity(t *testing.T) {
	for _, agent := range config.ExecutionAgents {
		t.Run(agent, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Agents = map[string]config.AgentDefinition{agent: {Session: &config.AgentSessionDefinition{Mode: "none"}}}
			args, err := agentArguments(agent, nil, "small", AgentSession{Agent: agent, Reference: "terminal-marker"}, false, cfg)
			if err != nil {
				t.Fatal(err)
			}
			for _, arg := range args {
				if arg == "--resume" || arg == "--session-id" || arg == "terminal-marker" {
					t.Fatalf("none passed session identity: %q", args)
				}
			}
		})
	}
}
