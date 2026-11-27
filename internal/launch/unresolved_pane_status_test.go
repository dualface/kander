package launch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/probe"
	"github.com/dualface/kander/v2/internal/terminal"
)

// paneStatusBackend answers the pane reads that follow session discovery with
// a fixed result and counts them; earlier reads reach the fake tmux.
type paneStatusBackend struct {
	capsBackend
	state *paneStatusState
}

type paneStatusState struct {
	mu         sync.Mutex
	discovered bool
	reads      int
	facts      terminal.PaneFacts
	err        error
	// outlastDeadline answers only after the read context ended.
	outlastDeadline bool
}

func (b paneStatusBackend) PaneFacts(ctx context.Context, conn terminal.Conn, pane string) (terminal.PaneFacts, error) {
	s := b.state
	s.mu.Lock()
	if !s.discovered {
		s.mu.Unlock()
		return b.capsBackend.PaneFacts(ctx, conn, pane)
	}
	s.reads++
	facts, err, outlast := s.facts, s.err, s.outlastDeadline
	s.mu.Unlock()
	if outlast {
		// Like a declarative step that absorbs its timeout: wait out the
		// bound, then answer the earlier facts without an error.
		<-ctx.Done()
	}
	return facts, err
}

func (s *paneStatusState) afterDiscovery() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

// paneStatusLaunchers makes every registration name unique: the registry
// keeps the first backend of a name, so a repeated run (go test -count=2)
// would otherwise reach the backend and state of an earlier run.
var paneStatusLaunchers atomic.Int64

// registerPaneStatusLauncher registers a launcher named after prefix over the
// fake tmux with caps and pane reads that answer facts and err once session
// discovery has run. It returns the registered name and the read state.
func registerPaneStatusLauncher(t *testing.T, prefix string, caps terminal.Capabilities, facts terminal.PaneFacts, err error) (string, *paneStatusState) {
	t.Helper()
	tmux, ok := terminal.Lookup("tmux")
	if !ok {
		t.Fatal("tmux backend is not registered")
	}
	name := fmt.Sprintf("%s-%d", prefix, paneStatusLaunchers.Add(1))
	state := &paneStatusState{facts: facts, err: err}
	terminal.Register(paneStatusBackend{capsBackend: capsBackend{Backend: tmux, name: name, caps: caps}, state: state})
	return name, state
}

// missingCodexRollout stands in for a Codex that never writes its rollout:
// discovery finds nothing within a short budget and marks the end of the
// discovery on state.
type missingCodexRollout struct {
	sessionDiscoverer
	state *paneStatusState
}

func (m missingCodexRollout) Scan(sessionLaunch, map[string]struct{}) (sessionScan, error) {
	return m, nil
}

func (m missingCodexRollout) Candidates(context.Context) ([]string, error) {
	m.state.mu.Lock()
	m.state.discovered = true
	m.state.mu.Unlock()
	return nil, nil
}

func (missingCodexRollout) Wait(sessionLaunch) time.Duration     { return 10 * time.Millisecond }
func (missingCodexRollout) Interval(sessionLaunch) time.Duration { return time.Millisecond }

func failCodexDiscovery(t *testing.T, state *paneStatusState) {
	t.Helper()
	const mode = "hook:codex-rollout"
	wrapped, ok := sessionProviders[mode].(sessionDiscoverer)
	if !ok {
		t.Fatalf("%s is not a discovering provider", mode)
	}
	sessionProviders[mode] = missingCodexRollout{sessionDiscoverer: wrapped, state: state}
	t.Cleanup(func() { sessionProviders[mode] = wrapped })
}

// startUnresolvedCodex starts Codex on launcher until discovery fails and
// returns the unresolved reason and the reason the server recorded.
func startUnresolvedCodex(t *testing.T, launcher string, state *paneStatusState, slug string) (reason, recorded string) {
	t.Helper()
	_, session, task, _, _ := remoteLifecycleFixture(t, slug)
	failCodexDiscovery(t, state)
	_, err := StartRemote("codex", launcher, task.TaskID)
	unresolved := requireUnresolved(t, err)
	if !strings.HasPrefix(err.Error(), "started-unresolved: "+task.TaskID+" ") || !strings.HasSuffix(err.Error(), " reason="+unresolved.Reason) {
		t.Fatalf("err=%v", err)
	}
	record, err := session.Project.ExecutionRecord(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != board.ExecutionUnresolved {
		t.Fatalf("execution record status=%q, want %q", record.Status, board.ExecutionUnresolved)
	}
	return unresolved.Reason, record.Failure
}

func noRolloutReason() string {
	return config.Text("launch.the_newly_started_codex_session_has_not_appeared_yet")
}

func TestRemoteUnresolvedStartNamesABlockedPane(t *testing.T) {
	skipOnWindows(t)
	launcher, state := registerPaneStatusLauncher(t, "status-blocked", herdrCoreCaps, terminal.PaneFacts{ProgramStatus: true, AgentApp: "codex", AgentStatus: "blocked"}, nil)
	reason, recorded := startUnresolvedCodex(t, launcher, state, "status-blocked")
	suffix := config.Text("launch.unresolved_pane_status", "blocked") + config.Text("launch.unresolved_pane_blocked_hint")
	if reason != noRolloutReason()+suffix {
		t.Fatalf("reason=%q, want %q", reason, noRolloutReason()+suffix)
	}
	if recorded != reason {
		t.Fatalf("recorded reason=%q, want %q", recorded, reason)
	}
}

func TestRemoteUnresolvedStartNamesAWorkingPaneWithoutTheHint(t *testing.T) {
	skipOnWindows(t)
	launcher, state := registerPaneStatusLauncher(t, "status-working", herdrCoreCaps, terminal.PaneFacts{ProgramStatus: true, AgentApp: "codex", AgentStatus: "working"}, nil)
	reason, recorded := startUnresolvedCodex(t, launcher, state, "status-working")
	if want := noRolloutReason() + config.Text("launch.unresolved_pane_status", "working"); reason != want || recorded != want {
		t.Fatalf("reason=%q recorded=%q, want %q", reason, recorded, want)
	}
	if strings.Contains(reason, config.Text("launch.unresolved_pane_blocked_hint")) {
		t.Fatalf("reason=%q carries the blocked hint", reason)
	}
}

// A pane read that fails, finds the pane gone, or reports no status leaves
// the reason as it was without the read.
func TestRemoteUnresolvedStartKeepsTheReasonWithoutAStatus(t *testing.T) {
	skipOnWindows(t)
	for _, tc := range []struct {
		name  string
		facts terminal.PaneFacts
		err   error
	}{
		{name: "error", err: errors.New("hc is not answering")},
		{name: "gone", facts: terminal.PaneFacts{Gone: true, AgentStatus: "blocked"}},
		{name: "empty", facts: terminal.PaneFacts{ProgramStatus: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launcher, state := registerPaneStatusLauncher(t, "status-"+tc.name, herdrCoreCaps, tc.facts, tc.err)
			reason, recorded := startUnresolvedCodex(t, launcher, state, "status-"+tc.name)
			if reason != noRolloutReason() || recorded != noRolloutReason() {
				t.Fatalf("reason=%q recorded=%q, want %q", reason, recorded, noRolloutReason())
			}
			if state.afterDiscovery() == 0 {
				t.Fatal("the failure branch did not read the pane")
			}
		})
	}
}

// A pane read that returns a status only after its bound ran out leaves the
// reason as it was.
func TestRemoteUnresolvedStartIgnoresAStatusReadPastItsBound(t *testing.T) {
	skipOnWindows(t)
	previous := unresolvedPaneStatusTimeout
	unresolvedPaneStatusTimeout = 20 * time.Millisecond
	t.Cleanup(func() { unresolvedPaneStatusTimeout = previous })
	launcher, state := registerPaneStatusLauncher(t, "status-expired", herdrCoreCaps, terminal.PaneFacts{ProgramStatus: true, AgentApp: "codex", AgentStatus: "blocked"}, nil)
	state.outlastDeadline = true
	reason, recorded := startUnresolvedCodex(t, launcher, state, "status-expired")
	if reason != noRolloutReason() || recorded != noRolloutReason() {
		t.Fatalf("reason=%q recorded=%q, want %q", reason, recorded, noRolloutReason())
	}
	if state.afterDiscovery() == 0 {
		t.Fatal("the failure branch did not read the pane")
	}
}

// A launcher without program status is never asked for the pane status.
func TestRemoteUnresolvedStartWithoutProgramStatusReadsNoStatus(t *testing.T) {
	skipOnWindows(t)
	caps := herdrCoreCaps
	caps.ProgramStatus = false
	launcher, state := registerPaneStatusLauncher(t, "status-unsupported", caps, terminal.PaneFacts{AgentStatus: "blocked"}, nil)
	reason, recorded := startUnresolvedCodex(t, launcher, state, "status-unsupported")
	if reason != noRolloutReason() || recorded != noRolloutReason() {
		t.Fatalf("reason=%q recorded=%q, want %q", reason, recorded, noRolloutReason())
	}
	if reads := state.afterDiscovery(); reads != 0 {
		t.Fatalf("pane reads after discovery=%d, want 0", reads)
	}
}

func TestUnresolvedReasonKeepsTheWholeSuffix(t *testing.T) {
	suffix := config.Text("launch.unresolved_pane_status", "blocked") + config.Text("launch.unresolved_pane_blocked_hint")
	long := strings.Repeat("界", 200)
	reason := unresolvedReason(long, suffix)
	if !strings.HasSuffix(reason, unresolvedReasonEllipsis+suffix) || !utf8.ValidString(reason) {
		t.Fatalf("reason=%q", reason)
	}
	if len(reason) > unresolvedReasonMaxBytes+len(unresolvedReasonEllipsis) {
		t.Fatalf("reason has %d bytes", len(reason))
	}
}

func TestUnresolvedPaneStatusCleansTheReportedValue(t *testing.T) {
	skipOnWindows(t)
	raw := "\x1b[31mblo\x07cked\x00" + strings.Repeat("状态", 20)
	launcher, state := registerPaneStatusLauncher(t, "status-dirty", herdrCoreCaps, terminal.PaneFacts{ProgramStatus: true, AgentStatus: raw}, nil)
	state.discovered = true
	suffix := unresolvedPaneStatus(LaunchPlan{Launcher: launcher}, "%1")
	prefix, _, _ := strings.Cut(config.Text("launch.unresolved_pane_status", "\x00"), "\x00")
	status, ok := strings.CutPrefix(suffix, prefix)
	if !ok || !strings.HasPrefix(status, "blocked状态") || len(status) > unresolvedPaneStatusMaxBytes || !utf8.ValidString(status) {
		t.Fatalf("suffix=%q status=%q", suffix, status)
	}
	for _, r := range status {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("status=%q keeps a control character", status)
		}
	}
	if strings.Contains(suffix, config.Text("launch.unresolved_pane_blocked_hint")) {
		t.Fatalf("suffix=%q carries the blocked hint for another status", suffix)
	}
}

func TestUnresolvedPaneStatusTimeoutStaysWithinTheProbeBudget(t *testing.T) {
	if unresolvedPaneStatusTimeout <= 0 || unresolvedPaneStatusTimeout > probe.DefaultCommandTimeout {
		t.Fatalf("timeout=%v, probe budget=%v", unresolvedPaneStatusTimeout, probe.DefaultCommandTimeout)
	}
}
