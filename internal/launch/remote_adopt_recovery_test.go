package launch

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

type paneAdoptionFixture struct {
	service    *remote.Session
	task       string
	cwd        string
	prompt     string
	markerPath string
	setOptPath string
	credential remote.Credential
}

func setupPaneAdoption(t *testing.T, launcher, sessionValue string, worktree bool) paneAdoptionFixture {
	t.Helper()
	skipOnWindows(t)
	t.Setenv(remote.EnvExecutionID, "")
	if launcher == "" {
		launcher = registerAdoptionProgramBackend(t)
	}
	cfg, service, task, _, fakeRoot := remoteLifecycleFixture(t, "repair-pane")
	agent, _, _ := strings.Cut(sessionValue, " ")
	t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", agent)
	if agent == "opencode" {
		cfg.Agents = map[string]config.AgentDefinition{
			agent: {
				Session:       &config.AgentSessionDefinition{Mode: "none"},
				ProgramStatus: &config.ProgramStatus{Reports: true, App: agent},
			},
		}
		if _, err := config.Save(cfg); err != nil {
			t.Fatal(err)
		}
	}
	window := launcher + ":$42:@9:%9"
	grant, err := service.Project.Claim(t.Context(), client.NewKey(), task.TaskID, api.ClaimRequest{
		ClientID: service.Client.ClientID(), ExecutionID: "pane-adoption", Owner: agent,
		Start: &api.StartInput{Session: sessionValue, Window: window},
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{
		ServerURL: service.ServerURL, ClientID: service.Client.ClientID(), ProjectID: service.Info.ID,
		TaskID: task.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation,
		Token: grant.Token, Session: sessionValue, Window: window,
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if worktree {
		cwd = t.TempDir()
		credential.Worktree = cwd
	}
	if err := service.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Project.Execution(t.Context(), client.NewKey(), task.TaskID, api.ExecutionUpdate{
		Action: api.ExecutionUnresolve, Window: window, Reason: "session discovery expired",
	}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	prompt := codexPromptPrefixes(task.TaskID)[0]
	if !worktree && sessionValue == "codex" {
		receipt, _, err := service.BeginAction("task-launch", task.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		receipt.Value.TaskID, receipt.Value.ExecutionID = task.TaskID, credential.ExecutionID
		receipt.Value.Window, receipt.Value.TaskFile = window, filepath.Join(t.TempDir(), "this-execution.md")
		err = errors.Join(receipt.Save(), receipt.Close())
		if err != nil {
			t.Fatal(err)
		}
		prompt = testAdoptionLaunchPrompt(t, service, credential, "start")
	}
	return paneAdoptionFixture{service: service, task: task.TaskID, cwd: cwd, prompt: prompt, credential: credential,
		markerPath: filepath.Join(fakeRoot, "tmux.log.pane-session"), setOptPath: filepath.Join(fakeRoot, "tmux.log.pane-setopt")}
}

func (f paneAdoptionFixture) rollout(t *testing.T, id string) {
	t.Helper()
	writeTestRollout(t, id, f.cwd, f.prompt, time.Now().UTC())
}

func (f paneAdoptionFixture) assertMarker(t *testing.T, want string, wrote bool) {
	t.Helper()
	data, err := os.ReadFile(f.markerPath)
	if want == "" {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected no marker, got %q, error = %v", data, err)
		}
	} else if err != nil || strings.TrimSpace(string(data)) != want {
		t.Fatalf("marker = %q, error = %v, want %q", data, err, want)
	}
	_, err = os.Stat(f.setOptPath)
	if (wrote && err != nil) || (!wrote && !errors.Is(err, os.ErrNotExist)) {
		t.Fatalf("expected marker write = %t, set-option log error = %v", wrote, err)
	}
}

func (f paneAdoptionFixture) assertNoAdoptionWrites(t *testing.T, before api.ExecutionState) {
	t.Helper()
	after, credential := adoptionState(t, f.service, f.task)
	if before.Task.Revision != after.Task.Revision || before.Task.Text != after.Task.Text ||
		!reflect.DeepEqual(before.Record, after.Record) || !reflect.DeepEqual(credential, f.credential) {
		t.Fatal("refused adoption changed card, execution, or credential")
	}
}

func TestRemoteAdoptsUnmarkedForegroundProcessPane(t *testing.T) {
	f := setupPaneAdoption(t, "tmux", "codex", true)
	f.rollout(t, "tmux-codex")
	code, out, diagnostics := runTestAdoption(f.task, "--json")
	want := `{"action":"adopt","task_id":"` + f.task + `"}` + "\n"
	if code != 0 || out != want || diagnostics != "" {
		t.Fatalf("adopt did not keep its JSON result while writing the marker: (%d) %q %q", code, out, diagnostics)
	}
	f.assertMarker(t, "tmux-codex", true)
	state, credential := adoptionState(t, f.service, f.task)
	requireRepairedAdoption(t, f.service, state, credential, "tmux-codex")
}

func TestRemoteAdoptionRequiresOneMatchingRollout(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, paneAdoptionFixture, api.ExecutionState)
		key    string
		detail string
	}{
		{name: "missing", key: "launch.adopt_session_unproven", detail: "0"},
		{name: "shared launch directory has two candidates", change: func(t *testing.T, f paneAdoptionFixture, _ api.ExecutionState) {
			f.rollout(t, "first")
			f.rollout(t, "second")
		}, key: "launch.adopt_session_unproven", detail: "2"},
		{name: "other directory", change: func(t *testing.T, f paneAdoptionFixture, _ api.ExecutionState) {
			writeTestRollout(t, "other-cwd", t.TempDir(), f.prompt, time.Now().UTC())
		}, key: "launch.adopt_session_unproven", detail: "0"},
		{name: "other task", change: func(t *testing.T, f paneAdoptionFixture, _ api.ExecutionState) {
			path := writeTestRollout(t, "other-task", f.cwd, "A different task prompt", time.Now().UTC())
			appendRolloutPrompt(t, path, f.prompt)
		}, key: "launch.adopt_session_unproven", detail: "0"},
		{name: "before floor", change: func(t *testing.T, f paneAdoptionFixture, state api.ExecutionState) {
			floor, err := remoteAdoptSessionFloor(board.MetadataFrom(state.Task.Text, board.FieldStartedAt))
			if err != nil {
				t.Fatal(err)
			}
			writeTestRollout(t, "old", f.cwd, f.prompt, floor.Add(-time.Minute))
		}, key: "launch.adopt_session_unproven", detail: "0"},
		{name: "read failure", change: func(t *testing.T, _ paneAdoptionFixture, _ api.ExecutionState) {
			dir := filepath.Join(codexSessionsRoot(), filepath.FromSlash(time.Now().UTC().Format("2006/01/02")))
			if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dir, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, key: "launch.adopt_session_scan_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := setupPaneAdoption(t, "", "codex", false)
			before, _ := adoptionState(t, f.service, f.task)
			if test.change != nil {
				test.change(t, f, before)
			}
			code, _, diagnostics := runTestAdoption(f.task)
			stop := tTextStop(f)
			if code == 0 || !strings.Contains(diagnostics, stop) {
				t.Fatalf("expected refusal to stop first, got (%d) %q", code, diagnostics)
			}
			if test.detail != "" {
				want := config.Text(test.key, f.task, test.detail, stop)
				if !strings.Contains(diagnostics, want) {
					t.Fatalf("expected %q, got %q", want, diagnostics)
				}
			}
			f.assertMarker(t, "", false)
			f.assertNoAdoptionWrites(t, before)
		})
	}
}

func tTextStop(f paneAdoptionFixture) string {
	return t("launch.unmarked_executor_stop_first", f.credential.Window)
}

func TestRemoteAdoptSessionFloor(t *testing.T) {
	for _, bad := range []string{"", "not a local timestamp"} {
		if _, err := remoteAdoptSessionFloor(bad); err == nil {
			t.Fatalf("accepted invalid STARTED_AT %q", bad)
		}
	}
	floor, err := remoteAdoptSessionFloor("2026-11-27 00:15")
	want := time.Date(2026, 11, 25, 22, 15, 0, 0, time.Local)
	if err != nil || !floor.Equal(want) {
		t.Fatalf("floor = %v, error = %v, want %v", floor, err, want)
	}
}

func TestRemoteAdoptionWithKnownOrAbsentSessionID(t *testing.T) {
	tests := []struct {
		name, session, existing, marker string
		writes, reject                  bool
	}{
		{name: "Claude reference", session: "claude 00000000-0000-4000-8000-000000000001", marker: "00000000-0000-4000-8000-000000000001", writes: true},
		{name: "already marked", session: "codex recorded", existing: "recorded", marker: "recorded"},
		{name: "no session mode", session: "opencode"},
		{name: "undiscoverable after launch", session: "devin", reject: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updates := observeExecutionUpdates(t, nil)
			f := setupPaneAdoption(t, "", test.session, false)
			if test.existing != "" {
				if err := os.WriteFile(f.markerPath, []byte(test.existing), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := adoptionState(t, f.service, f.task)
			code, _, diagnostics := runTestAdoption(f.task)
			if test.reject {
				if code == 0 || !strings.Contains(diagnostics, tTextStop(f)) {
					t.Fatalf("expected refusal to stop first, got (%d) %q", code, diagnostics)
				}
				f.assertNoAdoptionWrites(t, before)
			} else {
				after, credential := adoptionState(t, f.service, f.task)
				if code != 0 || after.Record.Status != board.ExecutionRunning || after.Task.Revision != before.Task.Revision ||
					after.Record.Session != test.session || credential.Session != test.session {
					t.Fatalf("unexpected adoption (%d) %q, status = %s, revision = %d", code, diagnostics, after.Record.Status, after.Task.Revision)
				}
			}
			updates.mu.Lock()
			defer updates.mu.Unlock()
			for _, update := range updates.updates {
				if update.Action == api.ExecutionSession {
					t.Fatal("unexpected session update")
				}
			}
			f.assertMarker(t, test.marker, test.writes)
		})
	}
}

func TestRemoteAdoptionRefusesMarkerWriteFailureOrConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		name := "write failure"
		if conflict {
			name = "conflict"
		}
		t.Run(name, func(t *testing.T) {
			f := setupPaneAdoption(t, "", "codex", false)
			f.rollout(t, "proven-session")
			marker := ""
			if conflict {
				marker = "another-session"
				if err := os.WriteFile(f.markerPath, []byte(marker), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("KANBAN_TMUX_PANE_SETOPT_FAIL", "1")
			}
			before, _ := adoptionState(t, f.service, f.task)
			code, _, diagnostics := runTestAdoption(f.task)
			if code == 0 || !strings.Contains(diagnostics, tTextStop(f)) {
				t.Fatalf("expected refusal to stop first, got (%d) %q", code, diagnostics)
			}
			f.assertMarker(t, marker, !conflict)
			f.assertNoAdoptionWrites(t, before)
			if !conflict {
				t.Setenv("KANBAN_TMUX_PANE_SETOPT_FAIL", "")
				code, _, diagnostics := runTestAdoption(f.task)
				if code != 0 {
					t.Fatalf("retry after marker write failure: (%d) %q", code, diagnostics)
				}
				f.assertMarker(t, "proven-session", true)
				after, credential := adoptionState(t, f.service, f.task)
				requireRepairedAdoption(t, f.service, after, credential, "proven-session")
			}
		})
	}
}

func TestRemoteAdoptionRechecksSessionAfterRolloutScan(t *testing.T) {
	var armed atomic.Bool
	var reads atomic.Int32
	var f paneAdoptionFixture
	changeResult := make(chan error, 1)
	remoteFixtureMiddleware = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if armed.Load() && r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/execution") && reads.Add(1) == 2 {
				_, err := f.service.Project.Execution(r.Context(), client.NewKey(), f.task, api.ExecutionUpdate{
					Action: api.ExecutionSession, SessionBefore: "codex", SessionAfter: "codex concurrent-change",
				}, *f.credential.Claim())
				changeResult <- err
			}
			next.ServeHTTP(w, r)
		})
	}
	t.Cleanup(func() { remoteFixtureMiddleware = nil })
	f = setupPaneAdoption(t, "", "codex", false)
	f.rollout(t, "proven-session")
	armed.Store(true)
	code, _, diagnostics := runTestAdoption(f.task)
	select {
	case err := <-changeResult:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("adoption did not recheck the execution")
	}
	if code == 0 || !strings.Contains(diagnostics, config.Text("launch.adopt_changed_before_marker", f.task)) {
		t.Fatalf("expected changed-before-marker refusal, got (%d) %q", code, diagnostics)
	}
	f.assertMarker(t, "", false)
	state, credential := adoptionState(t, f.service, f.task)
	if state.Record.Session != "codex concurrent-change" || state.Record.Status != board.ExecutionUnresolved || credential.Session != "codex" {
		t.Fatal("refused adoption did not preserve the concurrent update")
	}
}

func TestRemoteAdoptionKeepsCheckHintOnTakeoverResponse(t *testing.T) {
	var armed atomic.Bool
	var reads atomic.Int32
	remoteFixtureMiddleware = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if armed.Load() && r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/execution") && reads.Add(1) == 2 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeTakenOver, Message: "another generation owns the execution"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	t.Cleanup(func() { remoteFixtureMiddleware = nil })
	f := setupPaneAdoption(t, "", "codex", false)
	before, _ := adoptionState(t, f.service, f.task)
	f.rollout(t, "proven-session")
	armed.Store(true)
	code, out, diagnostics := runTestAdoption(f.task, "--json")
	var result struct{ Error api.Error }
	if err := json.Unmarshal([]byte(diagnostics), &result); err != nil {
		t.Fatal(err)
	}
	if code != 1 || out != "" || result.Error.Code != api.CodeTakenOver ||
		!strings.Contains(result.Error.Message, config.Text("launch.adopt_changed_before_marker", f.task)) {
		t.Fatalf("adopt lost the check hint or changed the error code: (%d) %q %q", code, out, diagnostics)
	}
	f.assertMarker(t, "", false)
	f.assertNoAdoptionWrites(t, before)
}

func TestRemoteAdoptionBindsSharedDirectoryTakeoverToItsTaskFile(t *testing.T) {
	skipOnWindows(t)
	t.Setenv(remote.EnvExecutionID, "")
	launcher := registerAdoptionProgramBackend(t)
	_, service, task, _, fakeRoot := remoteLifecycleFixture(t, "shared-directory-takeover")
	simulateLateCodexRollout(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, err = StartRemote("codex", launcher, task.TaskID)
	requireUnresolved(t, err)
	old := markRemoteExecutorStopped(t, service, task.TaskID)
	oldPrompt := testAdoptionLaunchPrompt(t, service, old, "start")
	oldCreated := time.Now().UTC()
	code, out, diagnostics := commandtest.Capture(func() int {
		return RunResumeRemote([]string{task.TaskID, "--agent", "codex", "--launcher", launcher, "--decision", "old executor stopped", "--message", "Take over after the stop"})
	})
	if code != 3 || !strings.Contains(out+diagnostics, "started-unresolved:") {
		t.Fatalf("takeover did not reach an unresolved start: (%d) %q %q", code, out, diagnostics)
	}
	before, current := adoptionState(t, service, task.TaskID)
	currentPrompt := testAdoptionLaunchPrompt(t, service, current, "takeover")
	if current.Generation != old.Generation+1 || current.Worktree != "" {
		t.Fatal("the fixture did not start the next generation in the shared directory")
	}
	writeTestRollout(t, "old-rollout", cwd, oldPrompt, oldCreated)
	code, _, diagnostics = runTestAdoption(task.TaskID)
	if code == 0 || !strings.Contains(diagnostics, config.Text("launch.unmarked_executor_stop_first", current.Window)) {
		t.Fatalf("the old rollout was not refused: (%d) %q", code, diagnostics)
	}
	f := paneAdoptionFixture{service: service, task: task.TaskID, cwd: cwd, credential: current,
		markerPath: filepath.Join(fakeRoot, "tmux.log.pane-session"), setOptPath: filepath.Join(fakeRoot, "tmux.log.pane-setopt")}
	f.assertMarker(t, "", false)
	f.assertNoAdoptionWrites(t, before)
	writeTestRollout(t, "current-rollout", cwd, currentPrompt, time.Now().UTC())
	code, _, diagnostics = runTestAdoption(task.TaskID)
	if code != 0 {
		t.Fatalf("current rollout was not adopted: (%d) %q", code, diagnostics)
	}
	f.assertMarker(t, "current-rollout", true)
	after, credential := adoptionState(t, service, task.TaskID)
	requireRepairedAdoption(t, service, after, credential, "current-rollout")
}

func TestRemoteAdoptionCannotUseAnotherProjectsRollout(t *testing.T) {
	skipOnWindows(t)
	t.Setenv(remote.EnvExecutionID, "")
	const slug = "same-task-across-projects"
	launcher := registerAdoptionProgramBackend(t)
	cfg, first, task, _, fakeRoot := remoteLifecycleFixture(t, slug)
	simulateLateCodexRollout(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, err = StartRemote("codex", launcher, task.TaskID)
	requireUnresolved(t, err)
	_, old := adoptionState(t, first, task.TaskID)
	oldPrompt := testAdoptionLaunchPrompt(t, first, old, "start")
	oldCreated := time.Now().UTC()

	cfg.Project.Name = "rebound-project"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	second, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	next := remoteLifecycleTodo(t, second, slug)
	if task.TaskID != next.TaskID || first.Info.ID == second.Info.ID {
		t.Fatal("expected the same task ID in two projects")
	}
	_, err = StartRemote("codex", launcher, next.TaskID)
	requireUnresolved(t, err)
	before, current := adoptionState(t, second, next.TaskID)
	currentPrompt := testAdoptionLaunchPrompt(t, second, current, "start")
	if current.Generation != 1 || current.Worktree != "" || currentPrompt == oldPrompt {
		t.Fatal("expected the first execution on the rebound project to have its own task file")
	}
	f := paneAdoptionFixture{service: second, task: next.TaskID, cwd: cwd, prompt: currentPrompt, credential: current,
		markerPath: filepath.Join(fakeRoot, "tmux.log.pane-session"), setOptPath: filepath.Join(fakeRoot, "tmux.log.pane-setopt")}
	writeTestRollout(t, "other-project-rollout", cwd, oldPrompt, oldCreated)
	code, _, diagnostics := runTestAdoption(next.TaskID)
	if code == 0 || !strings.Contains(diagnostics, config.Text("launch.unmarked_executor_stop_first", current.Window)) {
		t.Fatalf("another project's rollout was not refused: (%d) %q", code, diagnostics)
	}
	f.assertMarker(t, "", false)
	f.assertNoAdoptionWrites(t, before)
	f.rollout(t, "rebound-project-rollout")
	code, _, diagnostics = runTestAdoption(next.TaskID)
	if code != 0 {
		t.Fatalf("current project's rollout was not adopted: (%d) %q", code, diagnostics)
	}
	f.assertMarker(t, "rebound-project-rollout", true)
	after, credential := adoptionState(t, second, next.TaskID)
	requireRepairedAdoption(t, second, after, credential, "rebound-project-rollout")
}

func testAdoptionLaunchPrompt(t *testing.T, service *remote.Session, credential remote.Credential, kind string) string {
	t.Helper()
	file, err := service.TaskLaunchFile(t.Context(), credential)
	if err != nil || file == "" {
		t.Fatalf("launch receipt found = %v, error = %v", file != "", err)
	}
	paths, err := currentInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	head, err := promptHead(paths, kind, credential.TaskID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	instruction, err := taskFileInstruction(paths, head, file)
	if err != nil {
		t.Fatal(err)
	}
	return instruction
}

func TestRemoteAdoptionRetriesWithoutRepeatingCompletedWrites(t *testing.T) {
	t.Run("server session write failed", func(t *testing.T) {
		var refuse atomic.Bool
		observeExecutionUpdates(t, func(u api.ExecutionUpdate) bool {
			return u.Action == api.ExecutionSession && refuse.CompareAndSwap(true, false)
		})
		f := setupPaneAdoption(t, "", "codex", false)
		f.rollout(t, "retry-server")
		before, _ := adoptionState(t, f.service, f.task)
		refuse.Store(true)
		code, _, _ := runTestAdoption(f.task)
		if code == 0 {
			t.Fatal("adopt succeeded when the server refused session repair")
		}
		f.assertMarker(t, "retry-server", true)
		f.assertNoAdoptionWrites(t, before)
		requireRetryAdoption(t, f, "retry-server")
	})
	t.Run("local credential write failed", func(t *testing.T) {
		f := setupPaneAdoption(t, "", "codex", false)
		f.rollout(t, "retry-local")
		lock := findAdoptionCredentialLock(t, f)
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(lock, 0o700); err != nil {
			t.Fatal(err)
		}
		code, _, _ := runTestAdoption(f.task)
		if code == 0 {
			t.Fatal("adopt succeeded when the credential write failed")
		}
		state, credential := adoptionState(t, f.service, f.task)
		if state.Record.Session != "codex retry-local" || state.Record.Status != board.ExecutionUnresolved || credential.Session != "codex" {
			t.Fatal("expected server repair without local repair or adoption")
		}
		f.assertMarker(t, "retry-local", true)
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
		requireRetryAdoption(t, f, "retry-local")
	})
	t.Run("adopt update refused", func(t *testing.T) {
		var refuse atomic.Bool
		observeExecutionUpdates(t, func(u api.ExecutionUpdate) bool {
			return u.Action == api.ExecutionAdopt && refuse.CompareAndSwap(true, false)
		})
		f := setupPaneAdoption(t, "", "codex", false)
		f.rollout(t, "retry-adopt")
		refuse.Store(true)
		code, _, _ := runTestAdoption(f.task)
		if code == 0 {
			t.Fatal("adopt succeeded even though the server refused it")
		}
		state, credential := adoptionState(t, f.service, f.task)
		if state.Record.Session != "codex retry-adopt" || state.Record.Status != board.ExecutionUnresolved || credential.Session != "codex retry-adopt" {
			t.Fatal("expected completed session repair without adoption")
		}
		f.assertMarker(t, "retry-adopt", true)
		requireRetryAdoption(t, f, "retry-adopt")
	})
}

func requireRetryAdoption(t *testing.T, f paneAdoptionFixture, reference string) {
	t.Helper()
	if err := os.Remove(f.setOptPath); err != nil {
		t.Fatal(err)
	}
	code, out, diagnostics := runTestAdoption(f.task, "--json")
	want := `{"action":"adopt","task_id":"` + f.task + `"}` + "\n"
	if code != 0 || out != want || diagnostics != "" {
		t.Fatalf("retry failed or changed JSON output: (%d) stdout=%q stderr=%q", code, out, diagnostics)
	}
	f.assertMarker(t, reference, false)
	state, credential := adoptionState(t, f.service, f.task)
	requireRepairedAdoption(t, f.service, state, credential, reference)
}

func findAdoptionCredentialLock(t *testing.T, f paneAdoptionFixture) string {
	t.Helper()
	var lock string
	err := filepath.WalkDir(f.service.Home, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.Name() == f.credential.ExecutionID+".lock" {
			lock = path
			return filepath.SkipAll
		}
		return err
	})
	if err != nil || lock == "" {
		t.Fatalf("missing execution lock in the fixture: %v", err)
	}
	return lock
}
