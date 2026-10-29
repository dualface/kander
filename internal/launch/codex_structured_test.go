package launch

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
)

func structuredConfig(launcher string) *config.Config {
	cfg := envConfig("codex", launcher, nil)
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Transport: "structured"}}
	return cfg
}

func writeStructuredCodex(t *testing.T, path, argsLog, inputLog, threadID string) {
	t.Helper()
	body := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" >> " + posixQuote(argsLog) + "\n" +
		"printf '%s\\n' -- >> " + posixQuote(argsLog) + "\n" +
		"cat > " + posixQuote(inputLog) + "\n" +
		"printf '%s\\n' " + posixQuote(`{"type":"thread.started","thread_id":"`+threadID+`"}`) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestCodexStructuredTransportBuildsExecArguments(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Transport: "structured"}}

	got, err := agentArguments("codex", cfg.Models.Kanban["codex"], "small", AgentSession{Agent: "codex"}, false, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"exec", "--json", "--model", "gpt-5.6-sol", "--config", `model_reasoning_effort="medium"`,
		"--dangerously-bypass-approvals-and-sandbox", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("structured start args = %q, want %q", got, want)
	}
}

func TestCodexStructuredTransportBuildsResumeArguments(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Transport: "structured"}}

	got, err := agentArguments("codex", cfg.Models.Kanban["codex"], "large", AgentSession{Agent: "codex", Reference: "session-123"}, true, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"exec", "resume", "--json", "--model", "gpt-5.6-sol", "--config", `model_reasoning_effort="high"`,
		"--dangerously-bypass-approvals-and-sandbox", "session-123", "-",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("structured resume args = %q, want %q", got, want)
	}
}

func TestCodexDefaultTransportKeepsInteractiveArguments(t *testing.T) {
	cfg := config.DefaultConfig()
	got, err := agentArguments("codex", cfg.Models.Kanban["codex"], "small", AgentSession{Agent: "codex", Reference: "session-123"}, true, cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"resume", "--model", "gpt-5.6-sol", "--config", `model_reasoning_effort="medium"`,
		"--dangerously-bypass-approvals-and-sandbox", "session-123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default resume args = %q, want %q", got, want)
	}
}

func TestStructuredStartUsesDirectProcessOnceAndPersistsThread(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	argsLog := filepath.Join(root, "structured-args")
	inputLog := filepath.Join(root, "structured-input")
	writeStructuredCodex(t, filepath.Join(fakeBin, "codex"), argsLog, inputLog, "thread-start")
	loadEffective = func() (*config.Config, error) { return structuredConfig("tmux"), nil }
	taskID, taskPath := makeTodo(t, root, "structured-start")

	result, err := Start(root, "", "", taskID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Launcher != "foreground" || !result.Plan.Structured {
		t.Fatalf("plan = %+v", result.Plan)
	}
	if code, waitErr := result.Outcome.Wait(); waitErr != nil || code != 0 {
		t.Fatalf("wait = %d, %v", code, waitErr)
	}
	args := strings.Split(strings.TrimSpace(mustRead(t, argsLog)), "\n")
	markerCount := 0
	for _, arg := range args {
		if arg == "-" {
			markerCount++
		}
	}
	if markerCount != 1 {
		t.Fatalf("argv = %q; stdin marker count = %d", args, markerCount)
	}
	if strings.Contains(strings.Join(args, "\n"), "Read the complete") {
		t.Fatalf("prompt was duplicated on argv: %s", strings.Join(args, "\n"))
	}
	if strings.TrimSpace(mustRead(t, inputLog)) == "" {
		t.Fatal("prompt was not delivered on stdin")
	}
	working := filepath.Join(root, "working", filepath.Base(filepath.Dir(taskPath)), "spec.md")
	if text := mustRead(t, working); !strings.Contains(text, "- SESSION: codex thread-start\n") {
		t.Fatalf("thread id was not persisted: %s", text)
	}
}

func TestStructuredResumeUsesDirectProcessAndAcceptsQuickSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	argsLog := filepath.Join(root, "structured-args")
	inputLog := filepath.Join(root, "structured-input")
	writeStructuredCodex(t, filepath.Join(fakeBin, "codex"), argsLog, inputLog, "thread-resume")
	loadEffective = func() (*config.Config, error) { return structuredConfig("tmux"), nil }
	taskID, _ := makeTodo(t, root, "structured-resume")
	started, err := Start(root, "", "", taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := started.Outcome.Wait(); err != nil {
		t.Fatal(err)
	}
	loaded, err := board.LoadBoard(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := board.Locate(loaded, taskID)
	if err != nil {
		t.Fatal(err)
	}
	text := mustRead(t, entry.Document)
	if _, err := NotifyViaResume(root, entry, text, "continue", 61); err != nil {
		t.Fatal(err)
	}
	log := mustRead(t, argsLog)
	if !strings.Contains(log, "exec\nresume\n--json") || !strings.Contains(log, "thread-resume\n-\n--\n") {
		t.Fatalf("resume argv = %s", log)
	}
}

func TestStructuredStartAndResumeRejectIncompatibleComposedConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX")
	}
	makeConfigLoader := func(t *testing.T) func() (*config.Config, error) {
		t.Helper()
		scope := envConfig("codex", "tmux", nil)
		scope.WelcomeComplete = true
		raw, err := config.DocumentFromConfig(scope)
		if err != nil {
			t.Fatal(err)
		}
		raw["agents"] = map[string]any{
			"codex": map[string]any{"transport": "structured"},
		}
		overlay := map[string]any{
			"agents": map[string]any{
				"codex": map[string]any{
					"args": map[string]any{
						"start":  []string{"ignored"},
						"resume": []string{"ignored", "{session}"},
					},
					"session": map[string]any{"mode": "generated"},
				},
			},
		}
		return func() (*config.Config, error) {
			return config.MergeOverlayOnRaw(raw, overlay)
		}
	}

	t.Run("start", func(t *testing.T) {
		root, _, fakeBin := setupBoard(t)
		argsLog := filepath.Join(root, "structured-args")
		writeStructuredCodex(t, filepath.Join(fakeBin, "codex"), argsLog, filepath.Join(root, "structured-input"), "thread-start")
		loadEffective = makeConfigLoader(t)
		taskID, taskPath := makeTodo(t, root, "structured-invalid-composed-start")

		if _, err := Start(root, "", "", taskID); err == nil || !strings.Contains(strings.ToLower(err.Error()), "structured transport") {
			t.Fatalf("start error = %v, want structured transport rejection", err)
		}
		if _, err := os.Stat(taskPath); err != nil {
			t.Fatalf("start claimed the card before rejecting config: %v", err)
		}
		if _, err := os.Stat(argsLog); !os.IsNotExist(err) {
			t.Fatalf("structured process started before config rejection: %v", err)
		}
	})

	t.Run("resume", func(t *testing.T) {
		root, _, fakeBin := setupBoard(t)
		argsLog := filepath.Join(root, "structured-args")
		writeStructuredCodex(t, filepath.Join(fakeBin, "codex"), argsLog, filepath.Join(root, "structured-input"), "thread-resume")
		loadEffective = func() (*config.Config, error) { return structuredConfig("tmux"), nil }
		taskID, _ := makeTodo(t, root, "structured-invalid-composed-resume")
		started, err := Start(root, "", "", taskID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := started.Outcome.Wait(); err != nil {
			t.Fatal(err)
		}
		loaded, err := board.LoadBoard(root)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := board.Locate(loaded, taskID)
		if err != nil {
			t.Fatal(err)
		}
		text := mustRead(t, entry.Document)
		if !strings.Contains(text, "- SESSION: codex thread-resume\n") {
			t.Fatalf("start did not persist the Codex thread: %s", text)
		}

		before := mustRead(t, argsLog)
		loadEffective = makeConfigLoader(t)
		if _, err := NotifyViaResume(root, entry, text, "continue", 61); err == nil || !strings.Contains(strings.ToLower(err.Error()), "structured transport") {
			t.Fatalf("resume error = %v, want structured transport rejection", err)
		}
		if after := mustRead(t, argsLog); after != before {
			t.Fatalf("resume launched before config rejection:\nbefore=%s\nafter=%s", before, after)
		}
	})
}

func TestStructuredTakeoverCapturesAndPersistsReplacementThread(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	argsLog := filepath.Join(root, "structured-takeover-args")
	inputLog := filepath.Join(root, "structured-takeover-input")
	writeStructuredCodex(t, filepath.Join(fakeBin, "codex"), argsLog, inputLog, "thread-old")
	loadEffective = func() (*config.Config, error) { return structuredConfig("tmux"), nil }
	taskID, _ := makeTodo(t, root, "structured-takeover")
	started, err := Start(root, "", "", taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := started.Outcome.Wait(); err != nil {
		t.Fatal(err)
	}
	writeStructuredCodex(t, filepath.Join(fakeBin, "codex"), argsLog, inputLog, "thread-new")
	agent := "codex"
	if err := commandResumeLegacy(root, &agent, "", taskID, "take over", "", true, 61); err != nil {
		t.Fatal(err)
	}
	loaded, err := board.LoadBoard(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := board.Locate(loaded, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if text := mustRead(t, entry.Document); !strings.Contains(text, "- SESSION: codex thread-new\n") {
		t.Fatalf("replacement thread was not persisted: %s", text)
	}
}

func TestPreviewStartReportsStructuredDirectLauncher(t *testing.T) {
	root, _, _ := setupBoard(t)
	loadEffective = func() (*config.Config, error) { return structuredConfig("tmux"), nil }
	taskID, _ := makeTodo(t, root, "structured-preview")
	preview, err := PreviewStart(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Launcher != "foreground" {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestStructuredExplicitContainerLauncherIsRejectedBeforeClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fixture is POSIX")
	}
	root, _, _ := setupBoard(t)
	loadEffective = func() (*config.Config, error) { return structuredConfig("tmux"), nil }
	taskID, taskPath := makeTodo(t, root, "structured-container")
	if _, err := Start(root, "", "tmux", taskID); err == nil || !strings.Contains(strings.ToLower(err.Error()), "structured") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(taskPath); err != nil {
		t.Fatalf("task was claimed despite preflight rejection: %v", err)
	}
}

func TestStructuredStartRejectsInvalidThreadIDsAndRollsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX")
	}
	for name, threadID := range map[string]string{"spaces": "bad id", "oversize": strings.Repeat("x", 129)} {
		t.Run(name, func(t *testing.T) {
			root, _, fakeBin := setupBoard(t)
			writeStructuredCodex(t, filepath.Join(fakeBin, "codex"), filepath.Join(root, "args"), filepath.Join(root, "input"), threadID)
			loadEffective = func() (*config.Config, error) { return structuredConfig("tmux"), nil }
			taskID, taskPath := makeTodo(t, root, "structured-invalid-thread")
			if _, err := Start(root, "", "", taskID); err == nil {
				t.Fatal("invalid thread id was accepted")
			}
			if _, err := os.Stat(taskPath); err != nil {
				t.Fatalf("task was not rolled back: %v", err)
			}
		})
	}
}

func TestStructuredTransportRunsContainerDefaultSessionEntrypointsDirectly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX")
	}
	for _, name := range []string{"chat", "triage", "result", "orchestrate"} {
		t.Run(name, func(t *testing.T) {
			root, _, fakeBin := setupBoard(t)
			argsLog := filepath.Join(root, name+"-args")
			inputLog := filepath.Join(root, name+"-input")
			writeStructuredCodex(t, filepath.Join(fakeBin, "codex"), argsLog, inputLog, "thread-"+name)
			cfg := structuredConfig("tmux")
			cfg.ChatAgent = "codex"
			loadEffective = func() (*config.Config, error) { return cfg, nil }

			switch name {
			case "chat":
				result, err := StartChat(ChatRequest{Root: root, Message: "hello"})
				if err != nil || result.Launcher != "foreground" {
					t.Fatalf("result = %+v, err = %v", result, err)
				}
			case "triage":
				request := triageRequest(t, root)
				result, err := StartTriage(request)
				if err != nil || result.Launcher != "foreground" {
					t.Fatalf("result = %+v, err = %v", result, err)
				}
			case "result":
				request := triageRequest(t, root)
				data, err := os.ReadFile(request.JSONPath)
				if err != nil {
					t.Fatal(err)
				}
				request.CardID = "20261029-structured-result-task"
				dir := filepath.Join(root, "done", request.CardID)
				if err := os.MkdirAll(filepath.Join(dir, "source"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, issue.SourceFileName), data, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("# Done\n\n- SIZE: small\n- LANGUAGE: en\n\n## SUMMARY\n\nDone.\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				request.ResultSync = true
				result, err := StartTriage(request)
				if err != nil || result.Launcher != "foreground" {
					t.Fatalf("result = %+v, err = %v", result, err)
				}
			case "orchestrate":
				taskID, _ := makeTodo(t, root, "structured-orchestrate")
				result, err := StartOrchestrator(OrchestrateRequest{Root: root, References: []string{taskID}, Handover: "run it"})
				if err != nil || result.Launcher != "foreground" {
					t.Fatalf("result = %+v, err = %v", result, err)
				}
			}
			deadline := time.Now().Add(time.Second)
			for {
				input, inputErr := os.ReadFile(inputLog)
				args, argsErr := os.ReadFile(argsLog)
				if inputErr == nil && argsErr == nil && strings.TrimSpace(string(input)) != "" {
					if !strings.Contains(string(args), "exec\n--json") {
						t.Fatalf("argv = %s", args)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("structured prompt was not delivered: input=%v args=%v", inputErr, argsErr)
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}

func TestStructuredChatReturnsAssistantResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	script := filepath.Join(fakeBin, "codex")
	body := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"structured reply\"}}'\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := structuredConfig("tmux")
	cfg.ChatAgent = "codex"
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	result, err := StartChat(ChatRequest{Root: root, Message: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != "structured reply" || result.Address != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestStructuredChatReportsJSONLParserFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX")
	}
	root, _, fakeBin := setupBoard(t)
	script := filepath.Join(fakeBin, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{not-json}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := structuredConfig("tmux")
	cfg.ChatAgent = "codex"
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	if _, err := StartChat(ChatRequest{Root: root, Message: "hello"}); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("err = %v", err)
	}
}
