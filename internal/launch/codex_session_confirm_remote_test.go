package launch

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/terminal"
)

// capsBackend drives the fake tmux under its own launcher name with another
// capability set, so a remote start can run on the capabilities of a launcher
// the tests have no fake for.
type capsBackend struct {
	terminal.Backend
	name string
	caps terminal.Capabilities
}

func (b capsBackend) Name() string                        { return b.name }
func (b capsBackend) Capabilities() terminal.Capabilities { return b.caps }
func (b capsBackend) AutoDetect(func(string) string) bool { return false }

// ParseAddress claims only this backend's own addresses: the registry is
// global, so later tests probe every registered backend.
func (b capsBackend) ParseAddress(value string) (terminal.Address, bool) {
	rest, ok := strings.CutPrefix(value, b.name+":")
	if !ok {
		return terminal.Address{}, false
	}
	return b.Backend.ParseAddress("tmux:" + rest)
}

// Capability sets of the launchers under test.
var (
	herdrCoreCaps = terminal.Capabilities{Container: true, PaneMetadata: true, AgentIdentity: true, ProgramStatus: true}
	herdrCaps     = terminal.Capabilities{Container: true, AgentIdentity: true, SessionReport: true}
	unmarkedCaps  = terminal.Capabilities{Container: true}
)

// registerCapsLauncher registers name over the fake tmux with caps and returns it.
func registerCapsLauncher(t *testing.T, name string, caps terminal.Capabilities) string {
	t.Helper()
	tmux, ok := terminal.Lookup("tmux")
	if !ok {
		t.Fatal("tmux backend is not registered")
	}
	terminal.Register(capsBackend{Backend: tmux, name: name, caps: caps})
	if backend, _ := terminal.Lookup(name); backend.Capabilities() != caps {
		t.Fatalf("launcher %s is registered with other capabilities", name)
	}
	return name
}

// executionUpdates records the execution updates the fixture server receives.
// refuse, when set, answers an update with a conflict instead of serving it.
type executionUpdates struct {
	mu      sync.Mutex
	updates []api.ExecutionUpdate
	refuse  func(api.ExecutionUpdate) bool
}

func observeExecutionUpdates(t *testing.T, refuse func(api.ExecutionUpdate) bool) *executionUpdates {
	t.Helper()
	observed := &executionUpdates{refuse: refuse}
	remoteFixtureMiddleware = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/execution") {
				next.ServeHTTP(w, r)
				return
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var update api.ExecutionUpdate
			_ = json.Unmarshal(data, &update)
			observed.mu.Lock()
			observed.updates = append(observed.updates, update)
			observed.mu.Unlock()
			if observed.refuse != nil && observed.refuse(update) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeRevisionConflict, Message: "confirmation refused by the test"})
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
			next.ServeHTTP(w, r)
		})
	}
	t.Cleanup(func() { remoteFixtureMiddleware = nil })
	return observed
}

func (o *executionUpdates) confirms() []api.ExecutionUpdate {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []api.ExecutionUpdate
	for _, update := range o.updates {
		if update.Action == api.ExecutionConfirm {
			out = append(out, update)
		}
	}
	return out
}

// codexRolloutWriter stands in for Codex: the first discovery scan of a launch
// finds the rollout Codex writes for the launch prompt, in the launch
// directory. It also counts the scans.
type codexRolloutWriter struct {
	sessionDiscoverer
	t     *testing.T
	scans *int
}

// launchedCodexSession is the id of the rollout codexRolloutWriter writes.
const launchedCodexSession = "codex-rollout-of-this-launch"

func (w codexRolloutWriter) Scan(l sessionLaunch, previous map[string]struct{}) (sessionScan, error) {
	*w.scans++
	if *w.scans == 1 {
		writeLaunchRollout(w.t, l)
	}
	return w.sessionDiscoverer.Scan(l, previous)
}

func writeLaunchRollout(t *testing.T, l sessionLaunch) {
	t.Helper()
	created := time.Now().UTC()
	root := filepath.Join(codexSessionsRoot(), filepath.FromSlash(created.Format("2006/01/02")))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	for _, record := range []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": launchedCodexSession, "cwd": l.CWD, "timestamp": created.Format(time.RFC3339Nano)}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": l.Prompt}}}},
	} {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "rollout-"+launchedCodexSession+".jsonl"), data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeCodexRollouts makes the next Codex launch create its rollout and returns
// the count of discovery scans.
func fakeCodexRollouts(t *testing.T) *int {
	t.Helper()
	const mode = "hook:codex-rollout"
	wrapped, ok := sessionProviders[mode].(sessionDiscoverer)
	if !ok {
		t.Fatalf("%s is not a discovering provider", mode)
	}
	scans := new(int)
	sessionProviders[mode] = codexRolloutWriter{sessionDiscoverer: wrapped, t: t, scans: scans}
	t.Cleanup(func() { sessionProviders[mode] = wrapped })
	return scans
}

// remoteSessionFacts is every place a remote start records the session.
type remoteSessionFacts struct {
	card, record, credential, journal string
}

func readRemoteSessionFacts(t *testing.T, session *remote.Session, taskID string) remoteSessionFacts {
	t.Helper()
	var facts remoteSessionFacts
	task, err := session.Project.Show(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	facts.card = board.MetadataFrom(task.Text, board.FieldSession)
	record, err := session.Project.ExecutionRecord(t.Context(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	facts.record = record.Session
	credentials, err := remote.Credentials(session.Home)
	if err != nil || len(credentials) != 1 {
		t.Fatalf("credentials=%+v err=%v", credentials, err)
	}
	facts.credential = credentials[0].Session
	facts.journal = launchJournalSession(t, session)
	return facts
}

// launchJournalSession reads the session of the one task-launch receipt.
func launchJournalSession(t *testing.T, session *remote.Session) string {
	t.Helper()
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "launch-actions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var receipt remote.LocalAction
		if err := json.Unmarshal(data, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Kind == "task-launch" {
			found = append(found, receipt.Session)
		}
	}
	if len(found) != 1 {
		t.Fatalf("task-launch receipts=%q", found)
	}
	return found[0]
}

// paneMarker is the session marker the fake tmux pane carries.
func paneMarker(t *testing.T, fakeRoot string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fakeRoot, "tmux.log.pane-session"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// A Codex start on a launcher with pane metadata, such as herdr-core, records
// the discovered session on the server card, the server execution record, the
// credential, and the journal alike, and confirms the replacement.
func TestRemoteCodexStartOnPaneMetadataConfirmsTheDiscoveredSession(t *testing.T) {
	skipOnWindows(t)
	launcher := registerCapsLauncher(t, "caps-herdr-core", herdrCoreCaps)
	updates := observeExecutionUpdates(t, nil)
	_, session, task, _, fakeRoot := remoteLifecycleFixture(t, "codex-confirm-herdr-core")
	scans := fakeCodexRollouts(t)
	if _, err := StartRemote("codex", launcher, task.TaskID); err != nil {
		t.Fatal(err)
	}
	if *scans == 0 {
		t.Fatal("the start did not discover the Codex session")
	}
	facts := readRemoteSessionFacts(t, session, task.TaskID)
	reference, ok := strings.CutPrefix(facts.credential, "codex ")
	if !ok || reference == "" {
		t.Fatalf("credential session=%q, want codex <id>", facts.credential)
	}
	want := remoteSessionFacts{card: facts.credential, record: facts.credential, credential: facts.credential, journal: facts.credential}
	if facts != want {
		t.Fatalf("sessions=%+v, want all %q", facts, facts.credential)
	}
	if marker := paneMarker(t, fakeRoot); marker != reference {
		t.Fatalf("pane marker=%q, want %q", marker, reference)
	}
	confirms := updates.confirms()
	if len(confirms) != 1 || confirms[0].SessionBefore != "codex" || confirms[0].SessionAfter != "codex "+reference {
		t.Fatalf("confirms=%+v, want one codex -> codex %s", confirms, reference)
	}
}

// A refused confirmation of the discovered session leaves the pane that got
// the prompt running and records the execution unresolved.
func TestRemoteCodexStartKeepsThePaneWhenTheSessionConfirmationFails(t *testing.T) {
	skipOnWindows(t)
	launcher := registerCapsLauncher(t, "caps-herdr-core", herdrCoreCaps)
	observeExecutionUpdates(t, func(u api.ExecutionUpdate) bool {
		return u.Action == api.ExecutionConfirm && u.SessionAfter != ""
	})
	_, session, task, _, fakeRoot := remoteLifecycleFixture(t, "codex-confirm-refused")
	fakeCodexRollouts(t)
	_, err := StartRemote("codex", launcher, task.TaskID)
	unresolved := requireUnresolved(t, err)
	if !strings.HasPrefix(err.Error(), "started-unresolved: "+task.TaskID+" ") || unresolved.Unmarked {
		t.Fatalf("err=%v unresolved=%+v", err, unresolved)
	}
	if kill, statErr := os.ReadFile(filepath.Join(fakeRoot, "tmux.log.kill")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the pane was closed: %s", kill)
	}
	record, err := session.Project.ExecutionRecord(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != board.ExecutionUnresolved {
		t.Fatalf("execution record status=%q, want %q", record.Status, board.ExecutionUnresolved)
	}
	current, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil || current.State != "working" {
		t.Fatalf("the claim was rolled back: state=%q err=%v", current.State, err)
	}
}

// Claude allocates its session before the launch, so pane metadata changes
// nothing: the card keeps the allocated session and the start confirms
// without a replacement.
func TestRemoteClaudeStartOnPaneMetadataKeepsTheAllocatedSession(t *testing.T) {
	skipOnWindows(t)
	launcher := registerCapsLauncher(t, "caps-herdr-core", herdrCoreCaps)
	updates := observeExecutionUpdates(t, nil)
	_, session, task, _, _ := remoteLifecycleFixture(t, "claude-herdr-core")
	if _, err := StartRemote("claude", launcher, task.TaskID); err != nil {
		t.Fatal(err)
	}
	facts := readRemoteSessionFacts(t, session, task.TaskID)
	if reference, ok := strings.CutPrefix(facts.card, "claude "); !ok || reference == "" || facts.credential != facts.card || facts.record != facts.card {
		t.Fatalf("sessions=%+v, want one claude <id>", facts)
	}
	confirms := updates.confirms()
	if len(confirms) != 1 || confirms[0].SessionBefore != "" || confirms[0].SessionAfter != "" {
		t.Fatalf("confirms=%+v, want one without a session replacement", confirms)
	}
}

// Without pane metadata or a session report there is nothing to mark, so a
// Codex start neither discovers its session nor records more than the agent.
func TestRemoteCodexStartWithoutPaneMetadataKeepsTheAgentSession(t *testing.T) {
	skipOnWindows(t)
	launcher := registerCapsLauncher(t, "caps-unmarked", unmarkedCaps)
	updates := observeExecutionUpdates(t, nil)
	_, session, task, _, _ := remoteLifecycleFixture(t, "codex-unmarked")
	scans := fakeCodexRollouts(t)
	if _, err := StartRemote("codex", launcher, task.TaskID); err != nil {
		t.Fatal(err)
	}
	if *scans != 0 {
		t.Fatalf("discovery scans=%d, want none", *scans)
	}
	facts := readRemoteSessionFacts(t, session, task.TaskID)
	if facts.card != "codex" || facts.record != "codex" || facts.credential != "codex" {
		t.Fatalf("sessions=%+v, want codex", facts)
	}
	confirms := updates.confirms()
	if len(confirms) != 1 || confirms[0].SessionAfter != "" {
		t.Fatalf("confirms=%+v, want one without a session replacement", confirms)
	}
}

// A session-reporting launcher such as herdr already recorded the discovered
// Codex session before this change; it still does.
func TestRemoteCodexStartOnSessionReportRecordsTheDiscoveredSession(t *testing.T) {
	skipOnWindows(t)
	launcher := registerCapsLauncher(t, "caps-herdr", herdrCaps)
	_, session, task, _, _ := remoteLifecycleFixture(t, "codex-session-report")
	scans := fakeCodexRollouts(t)
	if _, err := StartRemote("codex", launcher, task.TaskID); err != nil {
		t.Fatal(err)
	}
	if *scans == 0 {
		t.Fatal("the start did not discover the Codex session")
	}
	facts := readRemoteSessionFacts(t, session, task.TaskID)
	if reference, ok := strings.CutPrefix(facts.card, "codex "); !ok || reference == "" || facts.record != facts.card || facts.credential != facts.card || facts.journal != facts.card {
		t.Fatalf("sessions=%+v, want all codex <id>", facts)
	}
}

// A Codex execution started on a pane metadata launcher records the same
// session on the card and the credential, so a native resume of the stopped
// executor passes the identity check and resumes that Codex session.
func TestRemoteNativeResumeContinuesTheConfirmedCodexSession(t *testing.T) {
	skipOnWindows(t)
	launcher := registerCapsLauncher(t, "caps-herdr-core", herdrCoreCaps)
	session, task, _ := remoteGitFixture(t, "codex-native-resume")
	root, err := session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g1"))
	fakeCodexRollouts(t)
	if _, err := StartRemote("codex", launcher, task.TaskID); err != nil {
		t.Fatal(err)
	}
	old := markRemoteExecutorStopped(t, session, task.TaskID)
	if old.Session != "codex "+launchedCodexSession {
		t.Fatalf("credential session=%q", old.Session)
	}
	t.Setenv("KANBAN_LAUNCH_CWD", filepath.Join(root, "worktrees", task.TaskID, "g2"))
	code, stdout, stderr := commandtest.Capture(func() int {
		return RunResumeRemote([]string{task.TaskID, "--launcher", launcher, "--message", "Continue the Codex session"})
	})
	if code != 0 {
		t.Fatalf("resume exit %d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	next, found, err := session.Credential(task.TaskID)
	if err != nil || !found || next.Generation != old.Generation+1 || next.Session != old.Session {
		t.Fatalf("continuation credential: %+v %v", next, err)
	}
	command := lastCommand(t, filepath.Dir(os.Getenv("KANBAN_TMUX_LOG")))
	if !strings.Contains(command, "/codex resume ") || !strings.Contains(command, " "+launchedCodexSession+" ") && !strings.Contains(command, " "+launchedCodexSession+"'") {
		t.Fatalf("resume command does not continue the Codex session: %s", command)
	}
}
