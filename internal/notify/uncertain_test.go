package notify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/liveness"
	"github.com/dualface/kander/internal/terminal"
)

func setSessionField(t *testing.T, path, session string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := regexp.MustCompile(`(?m)^- SESSION:.*$`).ReplaceAllLiteralString(string(data), "- SESSION: "+session)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeNotifySessionFile(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-09-19T00:00:00Z","cwd":"/tmp"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func herdrLogExists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, "herdr.log."+name))
	return err == nil
}

// A path-kind reported identity resolving to the card session is a proven
// match: direct delivery works instead of degrading to a resume.
func TestNotifyPathIdentityDeliversDirectly(t *testing.T) {
	root, _ := setupBoard(t)
	file := writeNotifySessionFile(t, "wanted")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_SESSION", file)
	taskID, path := makeReview(t, root, "notify-path-deliver")
	setSessionField(t, path, "pi wanted")
	out, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "herdr-direct") {
		t.Fatalf("out=%s", out)
	}
	if !herdrLogExists(root, "prompt") {
		t.Fatal("no prompt was delivered")
	}
	if herdrLogExists(root, "run") {
		t.Fatal("a fresh process must not be launched for a live session")
	}
}

// An undecidable reported identity is never delivery proof and never resume
// evidence: the command fails without delivering or recovering a process.
func TestNotifyUncertainIdentityDoesNotDeliverOrResume(t *testing.T) {
	root, _ := setupBoard(t)
	missing := filepath.Join(t.TempDir(), "gone.jsonl")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_SESSION", missing)
	taskID, path := makeReview(t, root, "notify-uncertain")
	setSessionField(t, path, "pi wanted")
	_, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err == nil {
		t.Fatal("uncertain identity must fail")
	}
	var uncertain *UncertainError
	if !errors.As(err, &uncertain) {
		t.Fatalf("err=%v is not uncertain", err)
	}
	if herdrLogExists(root, "prompt") || herdrLogExists(root, "run") || herdrLogExists(root, "order") {
		t.Fatal("uncertain identity must not deliver or recover a process")
	}
}

// A stale address with an unfinished reverse lookup is equally undecidable:
// the command reports the uncertain evidence and never reaches the resume
// recovery that a decided stopped observation would take.
func TestNotifyStaleIncompleteLookupDoesNotResume(t *testing.T) {
	root, _ := setupBoard(t)
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p9")
	t.Setenv("KANBAN_HERDR_LIST_JSON", `{"result":{"panes":[{"pane_id":"w9:p9","tab_id":"w9:t9","agent":"pi","agent_status":"idle","agent_session":{"kind":"path","value":"/nonexistent/gone.jsonl"}}]}}`)
	taskID, path := makeReview(t, root, "notify-stale-incomplete")
	setSessionField(t, path, "pi wanted")
	_, _, err := capture(t, func() error {
		return commandNotify(root, taskID, "x", "", "", true, 61)
	})
	if err == nil {
		t.Fatal("incomplete lookup must fail")
	}
	if !isUncertain(err) {
		t.Fatalf("err=%v is not uncertain", err)
	}
	if strings.Contains(err.Error(), "恢复=") {
		t.Fatalf("uncertain evidence reached recovery: %v", err)
	}
	if herdrLogExists(root, "order") || herdrLogExists(root, "prompt") || herdrLogExists(root, "run") {
		t.Fatal("incomplete lookup must not deliver or recover")
	}
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
