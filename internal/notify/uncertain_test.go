package notify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dualface/kander/v2/internal/liveness"
	"github.com/dualface/kander/v2/internal/terminal"
)

func writeNotifySessionFile(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-09-19T00:00:00Z","cwd":"/tmp"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// requireReady maps an uncertain probe to the non-recovery error.
func TestRequireReadyUncertain(t *testing.T) {
	if err := requireReady(TargetProbe{State: "uncertain", Detail: "reason"}); err == nil {
		t.Fatal("uncertain must fail")
	} else {
		var uncertain *UncertainError
		if !errors.As(err, &uncertain) {
			t.Fatalf("err=%v", err)
		}
	}
	if err := requireReady(TargetProbe{State: "ready"}); err != nil {
		t.Fatal(err)
	}
}

// isUncertain covers both the probe verdict and the backend's unfinished
// reverse lookup, so neither becomes recovery evidence.
func TestIsUncertainCoversIncompleteLookup(t *testing.T) {
	incomplete := &terminal.IncompleteLookupError{Candidates: 1, Cause: errors.New("x")}
	if !isUncertain(incomplete) {
		t.Fatal("incomplete lookup must be uncertain")
	}
	if isUncertain(errors.New("plain")) || isUncertain(&BusyError{}) {
		t.Fatal("ordinary and busy errors are not uncertain")
	}
}

// The forward probe classifies the tri-state verdict per state.
func TestAgentNotifyProbeStates(t *testing.T) {
	root, _ := setupBoard(t)
	file := writeNotifySessionFile(t, "wanted")
	_ = root
	for name, tc := range map[string]struct {
		facts terminal.PaneFacts
		want  string
	}{
		"path match ready": {
			facts: terminal.PaneFacts{Agent: "pi", AgentStatus: "idle", AgentSession: file, AgentSessionKind: "path"},
			want:  "ready",
		},
		"id mismatch stale": {
			facts: terminal.PaneFacts{Agent: "pi", AgentStatus: "idle", AgentSession: "other", AgentSessionKind: "id"},
			want:  "stale",
		},
		"path unresolvable uncertain": {
			facts: terminal.PaneFacts{Agent: "pi", AgentStatus: "idle", AgentSession: filepath.Join(t.TempDir(), "none.jsonl"), AgentSessionKind: "path"},
			want:  "uncertain",
		},
		"unknown kind uncertain": {
			facts: terminal.PaneFacts{Agent: "pi", AgentStatus: "idle", AgentSession: "x", AgentSessionKind: "url"},
			want:  "uncertain",
		},
	} {
		t.Run(name, func(t *testing.T) {
			backend := &factsBackend{facts: tc.facts}
			probe := AgentNotifyProbe(backend, "herdr", "w1:p1", liveness.TaskSession{Agent: "pi", Reference: "wanted"}, 5*time.Second)
			if probe.State != tc.want {
				t.Fatalf("state=%s detail=%s want=%s", probe.State, probe.Detail, tc.want)
			}
		})
	}
}

// factsBackend stubs the agent-identity backend for probe classification.
type factsBackend struct {
	terminal.Backend
	facts terminal.PaneFacts
}

func (b *factsBackend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{Container: true, AgentIdentity: true}
}

func (b *factsBackend) PaneFacts(context.Context, terminal.Conn, string) (terminal.PaneFacts, error) {
	return b.facts, nil
}
