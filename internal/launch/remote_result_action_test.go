package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

func TestRemoteResultLaunchUsesFrozenCardAndPreservesUnknownTaskFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX terminal fixture")
	}
	root, _, _ := setupBoard(t)
	request := triageRequest(t, root)
	request.ResultSync, request.CardID, request.Agent, request.Launcher = true, "20261112-result-source-task", "claude", "tmux"
	request.CardSnapshot = &board.TaskSummary{TaskID: request.CardID, State: "done", Document: "# Result evidence\n\n- LANGUAGE: ja\n\n## SUMMARY\n\nCompleted.\n"}
	session := &remote.Session{Home: t.TempDir(), ServerURL: "http://127.0.0.1:1", Info: api.Project{ID: "p-result-action"}}
	journal, _, err := session.BeginAction("issue-result-start", "frozen-result-source")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	request.Action = journal
	t.Setenv("KANBAN_TMUX_RESPAWN_FAIL", "1")
	t.Setenv(remote.EnvExecutionID, "predecessor-execution")
	t.Setenv(remote.EnvCoordinatorExecutionID, "predecessor-coordinator")
	if _, err := StartTriage(request); err == nil {
		t.Fatal("terminal failure was not reported")
	}
	if journal.Value.Window == "" {
		t.Fatal("unknown result launch lost its created scene address")
	}
	if journal.Value.TaskFile == "" {
		t.Fatal("no retained task file identity")
	}
	t.Cleanup(func() { _ = os.Remove(journal.Value.TaskFile) })
	body, err := os.ReadFile(journal.Value.TaskFile)
	if err != nil {
		t.Fatal("unknown result launch removed its task file")
	}
	if !strings.Contains(string(body), "ja") {
		t.Fatal("frozen card language was not used")
	}
	before, err := os.ReadFile(os.Getenv("KANBAN_TMUX_LOG"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartTriage(request); err == nil {
		t.Fatal("unknown result launch was repeated")
	}
	after, err := os.ReadFile(os.Getenv("KANBAN_TMUX_LOG"))
	if err != nil || string(after) != string(before) {
		t.Fatal("recovery attempted another terminal operation")
	}
	command := lastCommand(t, root)
	assertTaskLanguage(t, command, journal.Value.TaskFile, "ja")
	if !strings.Contains(command, remote.EnvExecutionID+"="+journal.Value.ExecutionID) {
		t.Fatal("result session inherited task authority")
	}
}

func TestRemoteResultForegroundRetainsProcessIdentityAndClearsInheritedRole(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX agent script")
	}
	root, _, bin := setupBoard(t)
	request := triageRequest(t, root)
	request.ResultSync, request.CardID, request.Agent, request.Launcher = true, "20261112-foreground-result-task", "claude", "foreground"
	request.CardSnapshot = &board.TaskSummary{TaskID: request.CardID, State: "done", Document: "# Completed\n\n- LANGUAGE: en\n\n## SUMMARY\n\nCompleted.\n"}
	envFile := filepath.Join(t.TempDir(), "result-environment")
	t.Setenv("KANDER_TEST_RESULT_ENV", envFile)
	t.Setenv(remote.EnvExecutionID, "old-task-execution")
	t.Setenv(remote.EnvCoordinatorExecutionID, "old-coordinator")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf 'claude fixture\n'; exit 0; fi
printf '%s|%s' "$KANDER_EXECUTION_ID" "$KANDER_COORDINATOR_EXECUTION_ID" > "$KANDER_TEST_RESULT_ENV"
sleep 0.3
`
	foregroundClaude(t, bin, script)
	session := &remote.Session{Home: t.TempDir(), ServerURL: "http://127.0.0.1:1", Info: api.Project{ID: "p-result-process"}}
	journal, _, err := session.BeginAction("issue-result-start", "native-result-source")
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	request.Action = journal
	if _, err := StartTriage(request); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(journal.Value.TaskFile) })
	if journal.Value.AgentPID <= 0 || journal.Value.AgentStart == "" || journal.Value.BootID == "" {
		t.Fatalf("missing native process identity: %+v", journal.Value)
	}
	data, err := os.ReadFile(envFile)
	if err != nil || string(data) != journal.Value.ExecutionID+"|" {
		t.Fatalf("inherited execution authority: %q %v", data, err)
	}
}
