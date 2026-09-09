package review

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

func TestBuiltinReviewDefinitionsMatchPrevious(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	for _, key := range []string{
		"CODEX_HOME", "CLAUDE_CONFIG_DIR", "GROK_HOME", "CURSOR_CONFIG_DIR",
		"CODEX_REVIEW_BIN", "CLAUDE_REVIEW_BIN", "GROK_REVIEW_BIN", "CURSOR_REVIEW_BIN",
		"CODEX_REVIEW_MODEL", "CLAUDE_REVIEW_MODEL", "GROK_REVIEW_MODEL", "CURSOR_REVIEW_MODEL",
	} {
		t.Setenv(key, "")
	}
	type expect struct {
		cwd, output, inspection, homeEnv string
		helpers, snapshot                bool
		env                              []string
	}
	cases := map[string]expect{
		"codex": {
			cwd: "root", output: "output.txt", homeEnv: "CODEX_HOME",
			inspection: "Use only read-only filesystem and shell operations needed to inspect code.",
			env:        []string{"CODEX_HOME"},
		},
		"claude": {
			cwd: "runtime", output: "output.json", homeEnv: "CLAUDE_CONFIG_DIR", snapshot: true,
			inspection: "Use only read-only inspection: read, search, and shell commands that do not write. Never create, modify, or delete any file; the review gate fails if HEAD moves or the worktree is dirty.",
			env:        []string{"CLAUDE_CONFIG_DIR"},
		},
		"cursor": {
			cwd: "runtime", output: "output.json", homeEnv: "CURSOR_CONFIG_DIR", helpers: true, snapshot: true,
			inspection: "Prefer read-only inspection. Do not modify the target worktree; the review gate fails if HEAD moves or the worktree is dirty.",
			env:        []string{"CURSOR_CONFIG_DIR", "CURSOR_DATA_DIR"},
		},
		"grok": {
			cwd: "root", output: "output.json", homeEnv: "GROK_HOME",
			inspection: "Use only read_file, grep, and list_dir to inspect code.",
			env:        []string{"GROK_HOME"},
		},
	}
	for agent, want := range cases {
		t.Run(agent, func(t *testing.T) {
			settings, err := agentSettingsFor(agent, "PM")
			if err != nil {
				t.Fatal(err)
			}
			if settings.cwd != want.cwd || settings.outputName != want.output || settings.inspectionRules != want.inspection {
				t.Fatalf("settings %+v", settings)
			}
			if settings.spawnsHelperProcesses != want.helpers || settings.snapshotSpec != want.snapshot {
				t.Fatalf("flags helpers=%v snapshot=%v", settings.spawnsHelperProcesses, settings.snapshotSpec)
			}
			if settings.stdin != config.ReviewStdinInstruction || len(settings.promptFiles) != 0 {
				t.Fatal("builtin stdin/prompt_files")
			}
			if settings.homePolicy == "" {
				t.Fatal("home policy")
			}
			for _, key := range want.env {
				if _, ok := settings.env[key]; !ok {
					t.Fatalf("missing env %s in %v", key, settings.env)
				}
			}
			ctx := reviewContext{
				agent: agent, settings: settings, root: "/work",
				program:     process.AgentProgram{Path: "/bin/reviewer"},
				instruction: process.TaskFileInstruction("Perform the PM review.", "/rt/prompt.txt"),
			}
			inv, cwd, err := reviewerArguments(ctx, "/rt", "/rt/out", "/rt/prompt.txt")
			if err != nil {
				t.Fatal(err)
			}
			if want.cwd == "root" && cwd != "/work" || want.cwd == "runtime" && cwd != "/rt" {
				t.Fatalf("cwd %s", cwd)
			}
			got := inv.Argv[1:]
			wantArgv := previousBuiltinArgv(agent, settings.model, settings.effort, "/work", "/rt", "/rt/out", "/rt/prompt.txt")
			if !reflect.DeepEqual(got, wantArgv) {
				t.Fatalf("argv\n got %q\nwant %q", got, wantArgv)
			}
			for _, key := range want.env {
				if inv.Env[key] == "" {
					t.Fatalf("env %s empty: %v", key, inv.Env)
				}
			}
			if inv.Env["GIT_OPTIONAL_LOCKS"] != "0" {
				t.Fatal("GIT_OPTIONAL_LOCKS")
			}
			if agent == "cursor" {
				if inv.Env["CURSOR_CONFIG_DIR"] != "/rt" || inv.Env["CURSOR_DATA_DIR"] != "/rt" {
					t.Fatalf("cursor env %v", inv.Env)
				}
			}
		})
	}
}

func previousBuiltinArgv(agent, model, effort, root, runtime, output, prompt string) []string {
	var modelArgs []string
	if model != "" {
		modelArgs = []string{"--model", model}
	}
	switch agent {
	case "codex":
		return append(append([]string{"exec", "--cd", root}, modelArgs...),
			"--sandbox", "read-only",
			"--ephemeral",
			"--config", `model_reasoning_effort="`+effort+`"`,
			"--config", `web_search="live"`,
			"--config", "allow_login_shell=false",
			"--output-last-message", output,
			"-",
		)
	case "claude":
		return append(append([]string{
			"--print", "--output-format", "json",
			"--permission-mode", "bypassPermissions",
			"--disallowedTools", "Edit,Write",
		}, modelArgs...), "--effort", effort,
			"--append-system-prompt", lastMessageOutputContract)
	case "cursor":
		return append([]string{
			"--print", "--output-format", "json", "--trust",
			"--add-dir", root,
		}, modelArgs...)
	case "grok":
		return append(append([]string{"--cwd", runtime}, modelArgs...),
			"--effort", effort,
			"--output-format", "json",
			"--permission-mode", "dontAsk",
			"--allow", "Read",
			"--allow", "Grep",
			"--tools", "read_file,grep,list_dir",
			"--disallowed-tools", "Agent,run_terminal_command,search_tool,use_tool,web_search,web_fetch,search_replace,todo_write,scheduler_create,scheduler_delete,scheduler_list,monitor,workflow,enter_plan_mode,exit_plan_mode,ask_user_question,image_gen,image_edit,image_to_video,reference_to_video,write",
			"--deny", "Edit",
			"--deny", "Write",
			"--deny", "MCPTool(*)",
			"--sandbox", "read-only",
			"--disable-web-search",
			"--no-memory",
			"--no-subagents",
			"--no-plan",
			"--verbatim",
			"--prompt-file", prompt,
		)
	default:
		return nil
	}
}

func TestParseReviewOutputSuccessBeforeExtract(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "missing.json"))
	t.Setenv(config.EnvLang, "en")
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	runtimeDir := t.TempDir()
	stdout := filepath.Join(runtimeDir, "stdout.log")
	if err := os.WriteFile(stdout, []byte("ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	type row struct {
		agent string
		body  string
		ok    bool
	}
	rows := []row{
		{"codex", "report text\n", true},
		{"codex", "", false},
		{"grok", `{"stopReason":"end_turn","text":"ok"}`, true},
		{"grok", `{"stopReason":"error","text":"ok"}`, false},
		{"claude", `{"type":"result","subtype":"success","is_error":false,"result":"ok"}`, true},
		{"claude", `{"type":"result","subtype":"error","is_error":true,"result":"ok"}`, false},
		{"cursor", `{"type":"result","subtype":"success","is_error":false,"result":"ok"}`, true},
		{"cursor", `{"type":"result","subtype":"success","is_error":true,"result":"ok"}`, false},
	}
	for _, item := range rows {
		t.Run(item.agent+"/"+item.body[:min(12, len(item.body))], func(t *testing.T) {
			settings, err := agentSettingsFor(item.agent, "QA")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(runtimeDir, settings.outputName)
			if err := os.WriteFile(path, []byte(item.body), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx := reviewContext{agent: item.agent, settings: settings}
			err = parseReviewOutput(ctx, runtimeDir, path, stdout)
			if item.ok && err != nil {
				t.Fatal(err)
			}
			if !item.ok && err == nil {
				t.Fatal("expected failure")
			}
			if !item.ok && (!strings.Contains(err.Error(), settings.name) || !strings.Contains(err.Error(), "did not complete with review text")) {
				t.Fatal(err)
			}
		})
	}
}

func TestBuiltinReviewStdinMatchesPreviousInstruction(t *testing.T) {
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "missing.json"))
	promptFile := "/rt/prompt.txt"
	want := process.TaskFileInstruction("Perform the QA review.", promptFile)
	if !strings.Contains(want, "task file at "+promptFile) {
		t.Fatalf("instruction %q", want)
	}
	for _, agent := range []string{"codex", "claude", "cursor", "grok"} {
		settings, err := agentSettingsFor(agent, "QA")
		if err != nil {
			t.Fatal(err)
		}
		if settings.stdin != config.ReviewStdinInstruction || len(settings.promptFiles) != 0 {
			t.Fatalf("%s stdin=%q files=%v", agent, settings.stdin, settings.promptFiles)
		}
		got := process.TaskFileInstruction("Perform the QA review.", promptFile)
		if got != want {
			t.Fatalf("%s instruction %q", agent, got)
		}
	}
}

func TestReviewOutputParsingUsesProcessEntry(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "process.ParseReviewOutput") {
			found = true
		}
		if name == "args.go" && (strings.Contains(text, "json.Unmarshal") || strings.Contains(text, "encoding/json")) {
			t.Fatal("review package reimplemented output parsing")
		}
		for _, banned := range []string{"stopReason", "json_field:result", `obj["result"]`, `obj["text"]`} {
			if strings.Contains(text, banned) {
				t.Fatalf("%s still branches on %s", name, banned)
			}
		}
	}
	if !found {
		t.Fatal("review parsing must use the review-subset entry")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
