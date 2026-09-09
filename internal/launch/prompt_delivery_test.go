package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

func paneAgentDef(path string) config.AgentDefinition {
	return config.AgentDefinition{
		Path: path,
		Args: &config.AgentArgs{
			Start:  []string{"--interactive"},
			Resume: []string{"--interactive", "--resume", "{session=}"},
		},
		Session: &config.AgentSessionDefinition{Mode: "generated"},
		PromptDelivery: &config.PromptDelivery{
			Mode:  "pane",
			Ready: &config.PromptReady{Match: "TUI_READY", TimeoutMS: 2000},
			Blocked: []config.PromptBlocked{{
				Match:  "TRUST_DIALOG",
				Reason: "trust confirmation",
			}},
		},
	}
}

func usePaneAgent(t *testing.T, root, bin, launcher string) *config.Config {
	t.Helper()
	cfg := envConfig("panecli", launcher, nil)
	cfg.Agents = map[string]config.AgentDefinition{"panecli": paneAgentDef(filepath.Join(bin, "claude"))}
	loadEffective = func() (*config.Config, error) { return cfg, nil }
	return cfg
}

func TestRejectPaneDeliveryOnDirectLaunchers(t *testing.T) {
	for _, launcher := range []string{"foreground", "console"} {
		err := rejectPaneDirectLauncher(LaunchPlan{Launcher: launcher, PromptDelivery: config.PromptDelivery{Mode: "pane"}})
		if err == nil || !strings.Contains(err.Error(), "tmux") || !strings.Contains(err.Error(), "herdr") {
			t.Fatalf("%s: %v", launcher, err)
		}
	}
	if err := rejectPaneDirectLauncher(LaunchPlan{Launcher: "tmux", PromptDelivery: config.PromptDelivery{Mode: "pane"}}); err != nil {
		t.Fatal(err)
	}
	if err := rejectPaneDirectLauncher(LaunchPlan{Launcher: "foreground", PromptDelivery: config.PromptDelivery{Mode: "argv"}}); err != nil {
		t.Fatal(err)
	}
}

func TestPaneDeliveryRejectedBeforeForegroundClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, bin := setupBoard(t)
	stdinIsTTY, stdoutIsTTY, stderrIsTTY = func() bool { return true }, func() bool { return true }, func() bool { return true }
	t.Cleanup(func() {
		stdinIsTTY = func() bool { return fileIsTTY(os.Stdin) }
		stdoutIsTTY = func() bool { return fileIsTTY(os.Stdout) }
		stderrIsTTY = func() bool { return fileIsTTY(os.Stderr) }
	})
	usePaneAgent(t, root, bin, "foreground")
	taskID, path := makeTodo(t, root, "pane-fg")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, startErr := capture(t, func() error { return commandStart(root, "panecli", "foreground", taskID) })
	if startErr == nil || !strings.Contains(startErr.Error(), "tmux") || !strings.Contains(startErr.Error(), "herdr") {
		t.Fatalf("err=%v", startErr)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(original) {
		t.Fatal("card rewritten before claim")
	}
	if _, stat := os.Stat(path); stat != nil {
		t.Fatal("card left todo")
	}
	after, err := board.ReadSnapshot(root, taskID)
	if err != nil || after.Revision != before.Revision {
		t.Fatalf("revision changed before claim: %d -> %d, %v", before.Revision, after.Revision, err)
	}
}

func TestPaneDeliveryHerdrAndTmux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, bin := setupBoard(t)
	var captured []string
	previous := newShellInvocation
	newShellInvocation = func(p process.AgentProgram, args []string, env map[string]string) (process.ProcessInvocation, error) {
		captured = append([]string{}, args...)
		return previous(p, args, env)
	}
	t.Cleanup(func() { newShellInvocation = previous })

	t.Run("tmux", func(t *testing.T) {
		captured = nil
		usePaneAgent(t, root, bin, "tmux")
		t.Setenv("KANBAN_TMUX_PANE_OUTPUT", "prefix TUI_READY")
		id, _ := makeTodo(t, root, "pane-tmux")
		_, _, err := capture(t, func() error { return commandStart(root, "panecli", "tmux", id) })
		if err != nil {
			t.Fatal(err)
		}
		if len(captured) == 0 || strings.Contains(captured[len(captured)-1], "UTF-8") {
			t.Fatalf("prompt leaked into argv: %q", captured)
		}
		sent, err := os.ReadFile(filepath.Join(root, "tmux.log.send-keys"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(sent)
		if !strings.Contains(text, "send-keys") || !strings.Contains(text, "-l") || !strings.Contains(text, "UTF-8") || !strings.Contains(text, "Enter") {
			t.Fatalf("send-keys=%s", text)
		}
		setopt, _ := os.ReadFile(filepath.Join(root, "tmux.log.pane-setopt"))
		if !strings.Contains(string(setopt), "@kander_session") {
			t.Fatalf("session marker missing: %s", setopt)
		}
	})

	t.Run("herdr", func(t *testing.T) {
		captured = nil
		usePaneAgent(t, root, bin, "herdr")
		log := filepath.Join(root, "herdr-pane.log")
		t.Setenv("KANBAN_HERDR_LOG", log)
		t.Setenv("HERDR_ENV", "1")
		t.Setenv("HERDR_WORKSPACE_ID", "w1")
		t.Setenv("KANBAN_HERDR_AGENT_OUTPUT", "prefix TUI_READY")
		writeFakeHerdr(t, filepath.Join(bin, "herdr"), log)
		id, _ := makeTodo(t, root, "pane-herdr")
		_, _, err := capture(t, func() error { return commandStart(root, "panecli", "herdr", id) })
		if err != nil {
			t.Fatal(err)
		}
		if len(captured) == 0 || strings.Contains(captured[len(captured)-1], "UTF-8") {
			t.Fatalf("prompt leaked into argv: %q", captured)
		}
		order, _ := os.ReadFile(log + ".order")
		if !strings.Contains(string(order), "pane run") || !strings.Contains(string(order), "agent prompt") {
			t.Fatalf("order=%s", order)
		}
		if idxRun, idxPrompt := strings.Index(string(order), "pane run"), strings.Index(string(order), "agent prompt"); idxRun < 0 || idxPrompt < idxRun {
			t.Fatalf("prompt before run: %s", order)
		}
		prompt, _ := os.ReadFile(log + ".prompt")
		if !strings.Contains(string(prompt), "UTF-8") {
			t.Fatalf("prompt=%s", prompt)
		}
	})
}

func TestPaneDeliveryTwoStageReady(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, bin := setupBoard(t)
	usePaneAgent(t, root, bin, "herdr")
	log := filepath.Join(root, "herdr-stage.log")
	t.Setenv("KANBAN_HERDR_LOG", log)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	t.Setenv("KANBAN_HERDR_READY_AFTER", "2")
	t.Setenv("KANBAN_HERDR_PRE_READY_OUTPUT", "starting")
	t.Setenv("KANBAN_HERDR_AGENT_OUTPUT", "TUI_READY")
	writeFakeHerdr(t, filepath.Join(bin, "herdr"), log)
	freezeClock(t)
	advance := sleepFn
	waited := false
	sleepFn = func(d time.Duration) {
		if strings.TrimSpace(mustRead(t, log+".ready-count")) == "1" {
			waited = true
			if _, err := os.Stat(log + ".prompt"); !os.IsNotExist(err) {
				t.Fatalf("prompt delivered before TUI readiness: %v", err)
			}
		}
		advance(d)
	}
	id, _ := makeTodo(t, root, "pane-stage")
	_, _, err := capture(t, func() error { return commandStart(root, "panecli", "herdr", id) })
	if err != nil {
		t.Fatal(err)
	}
	if !waited {
		t.Fatal("second-stage readiness was skipped")
	}
	order, _ := os.ReadFile(log + ".order")
	if strings.Index(string(order), "pane run") > strings.Index(string(order), "agent prompt") {
		t.Fatalf("order=%s", order)
	}
	if _, err := os.Stat(log + ".prompt"); err != nil {
		t.Fatal("prompt not delivered after ready")
	}
}

func TestPaneDeliveryBlockedWinsBeforeTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, bin := setupBoard(t)
	cfg := usePaneAgent(t, root, bin, "herdr")
	def := cfg.Agents["panecli"]
	def.PromptDelivery.Ready.TimeoutMS = 5000
	cfg.Agents["panecli"] = def
	log := filepath.Join(root, "herdr-block.log")
	t.Setenv("KANBAN_HERDR_LOG", log)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	t.Setenv("KANBAN_HERDR_BLOCKED_OUTPUT", "TUI_READY TRUST_DIALOG")
	writeFakeHerdr(t, filepath.Join(bin, "herdr"), log)
	id, path := makeTodo(t, root, "pane-block")
	original, _ := os.ReadFile(path)
	started := time.Now()
	_, _, err := capture(t, func() error { return commandStart(root, "panecli", "herdr", id) })
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "手动跑一次该 CLI 回答对话框后重试 `kander start`") {
		t.Fatalf("err=%v", err)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("blocked waited too long: %s", elapsed)
	}
	assertPaneStartRolledBack(t, root, path, original, log)
	if _, err := os.Stat(log + ".prompt"); !os.IsNotExist(err) {
		t.Fatalf("blocked dialog received prompt: %v", err)
	}
}

func TestPaneDeliveryHerdrPromptRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, bin := setupBoard(t)
	usePaneAgent(t, root, bin, "herdr")
	log := filepath.Join(root, "herdr-reject.log")
	t.Setenv("KANBAN_HERDR_LOG", log)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	t.Setenv("KANBAN_HERDR_AGENT_OUTPUT", "TUI_READY")
	t.Setenv("KANBAN_HERDR_PROMPT_FAIL", "1")
	writeFakeHerdr(t, filepath.Join(bin, "herdr"), log)
	id, path := makeTodo(t, root, "pane-reject")
	original, _ := os.ReadFile(path)
	_, _, err := capture(t, func() error { return commandStart(root, "panecli", "herdr", id) })
	if err == nil || !strings.Contains(err.Error(), "手动跑一次该 CLI 回答对话框后重试 `kander start`") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "TUI_READY") {
		t.Fatalf("missing pane output: %v", err)
	}
	assertPaneStartRolledBack(t, root, path, original, log)
}

func TestPaneDeliveryReadyTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, bin := setupBoard(t)
	cfg := usePaneAgent(t, root, bin, "herdr")
	def := cfg.Agents["panecli"]
	def.PromptDelivery.Ready.TimeoutMS = 250
	cfg.Agents["panecli"] = def
	log := filepath.Join(root, "herdr-timeout.log")
	t.Setenv("KANBAN_HERDR_LOG", log)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	t.Setenv("KANBAN_HERDR_AGENT_OUTPUT", "still starting")
	writeFakeHerdr(t, filepath.Join(bin, "herdr"), log)
	freezeClock(t)
	id, path := makeTodo(t, root, "pane-timeout")
	original, _ := os.ReadFile(path)
	_, _, err := capture(t, func() error { return commandStart(root, "panecli", "herdr", id) })
	if err == nil || strings.Contains(err.Error(), "手动跑一次该 CLI 回答对话框后重试") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "still starting") {
		t.Fatalf("missing pane output: %v", err)
	}
	assertPaneStartRolledBack(t, root, path, original, log)
}

func TestPaneDeliveryDurableBlockedClosesTab(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, bin := setupBoard(t)
	cfg := usePaneAgent(t, root, bin, "herdr")
	log := filepath.Join(root, "herdr-durable-block.log")
	t.Setenv("KANBAN_HERDR_LOG", log)
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_WORKSPACE_ID", "w1")
	t.Setenv("KANBAN_HERDR_BLOCKED_OUTPUT", "TRUST_DIALOG")
	writeFakeHerdr(t, filepath.Join(bin, "herdr"), log)
	plan, err := prepareLaunch("herdr", filepath.Dir(root), "start")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyAgentDelivery(&plan, cfg, "panecli"); err != nil {
		t.Fatal(err)
	}
	program, err := requireAgentProgram("panecli", cfg)
	if err != nil {
		t.Fatal(err)
	}
	session, err := newAgentSession("panecli", program, cfg)
	if err != nil {
		t.Fatal(err)
	}
	args, err := agentArguments("panecli", nil, "small", session, false, cfg)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := launchInvocation(plan, *program, attachPrompt(&plan, args, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = launchAgent(plan, root, "pane-durable", inv, nil, nil, &session, true)
	failure := asLaunchFailure(err)
	if err == nil || failure.DeliveryUnknown {
		t.Fatalf("err=%v unknown=%v", err, failure.DeliveryUnknown)
	}
	if !strings.Contains(err.Error(), "手动跑一次该 CLI 回答对话框后重试") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(log + ".close"); statErr != nil {
		t.Fatal("tab not closed")
	}
}

func TestPaneDeliveryResumeTakeover(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fakes")
	}
	root, _, bin := setupBoard(t)
	usePaneAgent(t, root, bin, "tmux")
	t.Setenv("KANBAN_TMUX_PANE_OUTPUT", "TUI_READY")
	id, path := makeTodo(t, root, "pane-takeover")
	_, _, err := capture(t, func() error { return commandStart(root, "claude", "tmux", id) })
	if err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "working", filepath.Base(filepath.Dir(path)), "spec.md")
	setBranch(t, working)
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, id)
	if _, err := board.MoveEntry(entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	agent := "panecli"
	_, _, err = capture(t, func() error {
		return commandResume(root, &agent, "tmux", id, "take over", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := os.ReadFile(filepath.Join(root, "tmux.log.send-keys"))
	if err != nil || !strings.Contains(string(sent), "-l") || !strings.Contains(string(sent), "Enter") {
		t.Fatalf("send-keys=%s err=%v", sent, err)
	}
}

func assertPaneStartRolledBack(t *testing.T, root, todoPath string, original []byte, herdrLog string) {
	t.Helper()
	if _, err := os.Stat(todoPath); err != nil {
		t.Fatal("card not in todo")
	}
	got, _ := os.ReadFile(todoPath)
	if string(got) != string(original) {
		t.Fatalf("card changed:\n%s", got)
	}
	if strings.Contains(string(got), "WINDOW:") && metadataFrom(string(got), windowField) != "" {
		t.Fatal("WINDOW residual")
	}
	if metadataFrom(string(got), sessionField) != "" {
		t.Fatal("SESSION residual")
	}
	if herdrLog != "" {
		if _, err := os.Stat(herdrLog + ".close"); err != nil {
			t.Fatal("tab not closed")
		}
	}
	matches, _ := filepath.Glob(filepath.Join(root, "working", "*", "spec.md"))
	if len(matches) != 0 {
		t.Fatalf("working leftovers %v", matches)
	}
}
