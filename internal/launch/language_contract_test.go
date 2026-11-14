package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// assertTaskLanguage checks that the language contract travels in the task file,
// while the delivered command ends at the task-file instruction.
func assertTaskLanguage(t *testing.T, command, taskFile, language string) {
	t.Helper()
	if strings.Contains(command, "follow it exactly. ") {
		t.Fatalf("delivered line carries a suffix: %s", command)
	}
	body := mustRead(t, taskFile)
	if !strings.Contains(body, `Hard requirement for every conversation related to this task: communicate with the user in "`+language+`".`) {
		t.Fatalf("task file lacks the %q language contract:\n%s", language, body)
	}
}

// configuredAgentLanguage is the agent_language the prompt builders read from the test scope.
func configuredAgentLanguage(t *testing.T) string {
	t.Helper()
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.AgentLanguage
}

func TestHeadedTaskFileInstructionEndsAtFileProtocol(t *testing.T) {
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(t.TempDir(), "rules")}
	for _, kind := range []string{"start", "resume", "takeover", "orchestrate", "triage", "refine", "result", "chat"} {
		line, err := headedTaskFileInstruction(paths, kind, "20260101-a-task", "dualface/kander#42", 2, "/tmp/task.md")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(line, "follow it exactly.") || strings.ContainsAny(line, "\r\n") {
			t.Fatalf("%s line=%q", kind, line)
		}
	}
}

func TestCardLaunchesCarryTheCardLanguageContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "reminder-card")
	replaceInFile(t, path, "- LANGUAGE: en\n", "- LANGUAGE: ko\n")
	if configuredAgentLanguage(t) == "ko" {
		t.Fatal("the card language must differ from the configured one")
	}
	if _, _, err := capture(t, func() error { return commandStart(root, "claude", "", taskID) }); err != nil {
		t.Fatal(err)
	}
	cmd := lastCommand(t, root)
	assertTaskLanguage(t, cmd, taskFileFromCommand(t, cmd), "ko")

	working := filepath.Join(root, "working", filepath.Base(filepath.Dir(path)), "spec.md")
	setBranch(t, working)
	loaded, _ := board.LoadBoard(root)
	entry, _ := board.Locate(loaded, taskID)
	if _, err := board.MoveEntry(entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := capture(t, func() error { return commandResume(root, nil, "", taskID, "继续", "", true, 61) }); err != nil {
		t.Fatal(err)
	}
	cmd = lastCommand(t, root)
	assertTaskLanguage(t, cmd, taskFileFromCommand(t, cmd), "ko")

	reviewed := filepath.Join(root, "review", filepath.Base(filepath.Dir(path)), "spec.md")
	snapshot, err := board.ReadSnapshot(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NotifyViaResume(root, snapshot.Entry, mustRead(t, reviewed), "继续", 61); err != nil {
		t.Fatal(err)
	}
	cmd = lastCommand(t, root)
	assertTaskLanguage(t, cmd, taskFileFromCommand(t, cmd), "ko")

	agent := "grok"
	if _, _, err := capture(t, func() error { return commandResume(root, &agent, "", taskID, "继续", "", true, 61) }); err != nil {
		t.Fatal(err)
	}
	cmd = lastCommand(t, root)
	assertTaskLanguage(t, cmd, taskFileFromCommand(t, cmd), "ko")
}

func TestInvalidCardLanguageDeliversNoReminder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	taskID, path := makeTodo(t, root, "reminder-invalid")
	replaceInFile(t, path, "- LANGUAGE: en\n", "- LANGUAGE: ja\x1b[0m\n")
	if _, _, err := capture(t, func() error { return commandStart(root, "claude", "", taskID) }); err != nil {
		t.Fatal(err)
	}
	if cmd := lastCommand(t, root); strings.Contains(cmd, "follow it exactly. ") || strings.Contains(cmd, "\x1b") {
		t.Fatalf("an invalid card LANGUAGE reached the delivered line: %q", cmd)
	}
}

func TestOrchestrateLanguageFallsBackToConfigWhenCardsDisagree(t *testing.T) {
	setupBoard(t)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(t.TempDir(), "rules")}
	if got, err := orchestrateLanguage([]string{"- LANGUAGE: ja\n", "- LANGUAGE: ja\n"}, paths); err != nil || got != "ja" {
		t.Fatalf("shared language=%q err=%v", got, err)
	}
	if got, err := orchestrateLanguage([]string{"- LANGUAGE: ja\n", "- LANGUAGE: en\n"}, paths); err != nil || got != configuredAgentLanguage(t) {
		t.Fatalf("mixed languages=%q err=%v", got, err)
	}
}

func TestStartOrchestratorCarriesConfiguredLanguageForMixedCards(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	firstID, _ := makeTodo(t, root, "reminder-first")
	secondID, secondPath := makeBacklog(t, root, "reminder-second", true)
	replaceInFile(t, secondPath, "- LANGUAGE: en\n", "- LANGUAGE: ja\n")
	if _, err := StartOrchestrator(OrchestrateRequest{Root: root, References: []string{firstID, secondID}, Handover: "plan", Agent: "claude", Launcher: "tmux"}); err != nil {
		t.Fatal(err)
	}
	cmd := lastCommand(t, root)
	assertTaskLanguage(t, cmd, taskFileFromCommand(t, cmd), configuredAgentLanguage(t))
}

func TestStartChatCarriesTheConfiguredLanguageContract(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	oldLoad := loadEffective
	loadEffective = func() (*config.Config, error) {
		cfg := envConfig("codex", "tmux", map[string]string{"large": "claude", "small": "codex"})
		cfg.ChatAgent, cfg.AgentLanguage = "claude", "fr"
		return cfg, nil
	}
	t.Cleanup(func() { loadEffective = oldLoad })
	if _, err := StartChat(ChatRequest{Root: root, Message: "bonjour"}); err != nil {
		t.Fatal(err)
	}
	cmd := lastCommand(t, root)
	taskFile := taskFileFromCommand(t, cmd)
	t.Cleanup(func() { _ = os.Remove(taskFile) })
	assertTaskLanguage(t, cmd, taskFile, "fr")
}
