package launch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/terminal"
)

// A program-status backend identifies the live agent independently of the
// session marker. Wrap the tmux fake's marker storage to model that distinction.
type adoptionProgramBackend struct{ capsBackend }

func (b adoptionProgramBackend) PaneFacts(ctx context.Context, conn terminal.Conn, pane string) (terminal.PaneFacts, error) {
	facts, err := b.Backend.PaneFacts(ctx, conn, pane)
	facts.ProgramStatus = true
	if agent, err := config.LoadAgent(facts.Command); err == nil {
		facts.AgentApp, _ = config.ProgramStatusApp(agent)
	}
	facts.AgentStatus = "working"
	facts.AgentSession = facts.SessionMarker
	return facts, err
}

func registerAdoptionProgramBackend(t *testing.T) string {
	t.Helper()
	const name = "adopt-program-status"
	tmux, ok := terminal.Lookup("tmux")
	if !ok {
		t.Fatal("missing tmux backend")
	}
	terminal.Register(adoptionProgramBackend{capsBackend{Backend: tmux, name: name, caps: herdrCoreCaps}})
	return name
}

type fastCodexDiscovery struct{ codexSessionProvider }

func (fastCodexDiscovery) Wait(sessionLaunch) time.Duration     { return 40 * time.Millisecond }
func (fastCodexDiscovery) Interval(sessionLaunch) time.Duration { return 10 * time.Millisecond }

func TestRemoteAdoptsCodexAfterRolloutAppears(t *testing.T) {
	skipOnWindows(t)
	launcher := registerAdoptionProgramBackend(t)
	t.Setenv(remote.EnvExecutionID, "")
	updates := observeExecutionUpdates(t, nil)
	_, session, task, _, fakeRoot := remoteLifecycleFixture(t, "late-codex-adopt")
	const mode = "hook:codex-rollout"
	previous := sessionProviders[mode]
	sessionProviders[mode] = fastCodexDiscovery{}
	t.Cleanup(func() { sessionProviders[mode] = previous })
	t.Setenv("KANBAN_TMUX_SKIP_CODEX_ROLLOUT", "1")
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", "codex")
	_, err := StartRemote("codex", launcher, task.TaskID)
	requireUnresolved(t, err)
	if !strings.HasPrefix(err.Error(), "started-unresolved: "+task.TaskID+" ") {
		t.Fatalf("unexpected launch result: %v", err)
	}
	before, err := session.Project.ExecutionRecord(t.Context(), task.TaskID)
	if err != nil || before.Status != board.ExecutionUnresolved {
		t.Fatalf("execution before adoption = %q, error = %v", before.Status, err)
	}
	prompt := codexPromptPrefixes(task.TaskID)[0]
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeLaunchRollout(t, sessionLaunch{CWD: currentDir, Prompt: prompt})
	code, out, diagnostics := runTestAdoption(task.TaskID)
	if code != 0 {
		t.Fatalf("adoption failed (%d): %s%s", code, out, diagnostics)
	}
	markerLine := fmt.Sprintf("session-marker-written: %s window=%s session=%s", task.TaskID, before.Window, launchedCodexSession)
	markerAt, adoptAt := strings.Index(out, markerLine), strings.Index(out, "adopted:")
	if markerAt < 0 || adoptAt <= markerAt {
		t.Fatalf("expected marker before adoption, got %q", out)
	}
	if marker := paneMarker(t, fakeRoot); marker != launchedCodexSession {
		t.Fatalf("pane marker = %q", marker)
	}
	state, credential := adoptionState(t, session, task.TaskID)
	requireRepairedAdoption(t, session, state, credential, launchedCodexSession)
	updates.mu.Lock()
	defer updates.mu.Unlock()
	for _, u := range updates.updates {
		if u.Action == api.ExecutionAdopt {
			if u.ExpectedRevision != state.Task.Revision {
				t.Fatalf("adopt was bound to revision %d, want repaired revision %d", u.ExpectedRevision, state.Task.Revision)
			}
			break
		}
	}
	backend, address, _ := terminal.ParseWindow(credential.Window)
	program, err := lookPath(backend.Executable())
	if err != nil {
		t.Fatal(err)
	}
	facts, err := backend.PaneFacts(t.Context(), terminal.Conn{Program: program, Run: terminal.ProbeRunner}, address.Pane)
	if err != nil {
		t.Fatal(err)
	}
	match, detail := terminal.MatchAgentSession(t.Context(), "codex", facts.AgentSessionKind, facts.AgentSession, parseSessionValue(credential.Session).Reference)
	if match != terminal.SessionMatches {
		t.Fatalf("delivery session verdict = %v (%s)", match, detail)
	}
}

func runTestAdoption(task string, options ...string) (int, string, string) {
	return commandtest.Capture(func() int {
		return RunResumeRemote(append([]string{task, "--adopt", "--decision", "confirmed local executor"}, options...))
	})
}

func adoptionState(t *testing.T, service *remote.Session, task string) (api.ExecutionState, remote.Credential) {
	t.Helper()
	credential, found, err := service.Credential(task)
	if !found || err != nil {
		t.Fatalf("credential available = %v, error = %v", found, err)
	}
	state, err := service.Project.ExecutionState(t.Context(), task, *credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	return state, credential
}

func requireRepairedAdoption(t *testing.T, service *remote.Session, state api.ExecutionState, credential remote.Credential, reference string) {
	t.Helper()
	want := "codex " + reference
	card := board.MetadataFrom(state.Task.Text, board.FieldSession)
	if card != want || state.Record.Session != want || credential.Session != want || state.Record.Status != board.ExecutionRunning {
		t.Fatalf("after adoption: card = %q, record = %q, credential = %q, status = %q", card, state.Record.Session, credential.Session, state.Record.Status)
	}
	transitions := adoptionTransitions(t, service, state.Task.TaskID)
	var repairs, repairSeq, adoptSeq uint64
	for _, transition := range transitions {
		switch transition.Kind {
		case board.TransitionSessionRepair:
			repairs++
			repairSeq = transition.Seq
			if transition.CardRevision != state.Task.Revision {
				t.Fatalf("repair revision = %d, want %d", transition.CardRevision, state.Task.Revision)
			}
		case board.TransitionAdopt:
			adoptSeq = transition.Seq
		}
	}
	if repairs != 1 || repairSeq >= adoptSeq {
		t.Fatalf("repairs = %d, transition sequence repair = %d, adopt = %d", repairs, repairSeq, adoptSeq)
	}
}

// Read transition originals from this test's in-process service, not from the
// caller's checkout or client board path.
func adoptionTransitions(t *testing.T, service *remote.Session, task string) []board.ExecutionTransition {
	t.Helper()
	dir := filepath.Join(service.Info.BoardPath, ".kander", "groups", "00000000-start-group", task, "transitions")
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	transitions := make([]board.ExecutionTransition, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var transition board.ExecutionTransition
		if err := json.Unmarshal(data, &transition); err != nil {
			t.Fatal(err)
		}
		transitions = append(transitions, transition)
	}
	return transitions
}
