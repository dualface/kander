package liveness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/terminal"
)

// resolvedLookupBackend answers a reverse lookup and records the reference it got.
type resolvedLookupBackend struct {
	terminal.Backend
	address   terminal.Address
	err       error
	reference string
}

func (b *resolvedLookupBackend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{}
}
func (b *resolvedLookupBackend) ReverseLookup(_ context.Context, _ terminal.Conn, identity terminal.Identity) (terminal.Address, error) {
	b.reference = identity.Reference
	return b.address, b.err
}
func (b *resolvedLookupBackend) Name() string { return "tmux" }
func (b *resolvedLookupBackend) FormatAddress(a terminal.Address) string {
	return "tmux:" + a.Session + ":" + a.Container + ":" + a.Pane
}

func staleCodex(ctx context.Context, backend terminal.Backend, resolve ReferenceResolver) Report {
	return staleReport(ctx, board.Entry{TaskID: "codex-card"}, TaskSession{Agent: "codex"}, "tmux", "s:@1", "stale", "tmux", backend, staleSearch{reverse: true, resolve: resolve})
}

func TestResolvedReferenceFeedsTheReverseLookup(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, false)
	backend := &resolvedLookupBackend{address: terminal.Address{Session: "$9", Container: "@9", Pane: "%9"}}
	report := staleCodex(context.Background(), backend, func(context.Context) (string, error) { return "rollout-id", nil })
	if report.Status != Drifted || backend.reference != "rollout-id" || report.Reference != "rollout-id" {
		t.Fatalf("report=%+v reference=%q", report, backend.reference)
	}
	backend.err = &terminal.MatchError{Matches: 0, Cause: errors.New("none")}
	if report = staleCodex(context.Background(), backend, func(context.Context) (string, error) { return "rollout-id", nil }); report.Status != Stopped {
		t.Fatalf("report=%+v", report)
	}
}

func TestFailedResolutionIsAProbeFailure(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, false)
	report := staleCodex(context.Background(), &resolvedLookupBackend{}, func(context.Context) (string, error) { return "", errors.New("no rollout") })
	if report.Status != Unknown || report.Evidence != nil || !strings.Contains(report.Detail, "no rollout") {
		t.Fatalf("report=%+v", report)
	}
}

// Without a resolver the classification of check and adoption stays as it was.
func TestStaleCodexWithoutAResolverStaysStopped(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, false)
	if report := staleCodex(context.Background(), &resolvedLookupBackend{}, nil); report.Status != Stopped {
		t.Fatalf("report=%+v", report)
	}
}

// The resolver gets the deadline of the classification, not one of its own.
func TestResolutionInheritsTheClassificationDeadline(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	want, _ := ctx.Deadline()
	var got time.Time
	staleCodex(ctx, &resolvedLookupBackend{}, func(ctx context.Context) (string, error) {
		got, _ = ctx.Deadline()
		return "", ctx.Err()
	})
	if !got.Equal(want) {
		t.Fatalf("deadline=%s want %s", got, want)
	}
}

func TestSeveralMatchesAreEvidenceWithTheirCount(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, false)
	backend := &resolvedLookupBackend{err: &terminal.MatchError{Matches: 3, Cause: errors.New("several")}}
	report := staleCodex(context.Background(), backend, func(context.Context) (string, error) { return "id", nil })
	if report.Status != Unknown || report.Evidence == nil || report.Evidence.Matches != 3 {
		t.Fatalf("report=%+v", report)
	}
}

// An uncertain identity at the end of the deadline says nothing about the pane, so
// it stays a probe failure. A finished probe that reads the same pane is evidence.
func TestUncertainIdentityIsEvidenceOnlyBeforeTheDeadline(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, false)
	pane := terminal.PaneFacts{Agent: "pi", AgentStatus: "idle", AgentSessionKind: "path", AgentSession: "/no/such/session"}
	classify := func(ctx context.Context) Report {
		return classifyAgentFacts(ctx, board.Entry{TaskID: "uncertain"}, TaskSession{Agent: "pi", Reference: "wanted"}, &resolvedLookupBackend{}, terminal.Address{Container: "t1", Pane: "p1"}, "herdr", pane, staleSearch{reverse: true})
	}
	if report := classify(context.Background()); report.Status != Unknown || report.Evidence == nil {
		t.Fatalf("report=%+v", report)
	}
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if report := classify(expired); report.Status != Unknown || report.Evidence != nil {
		t.Fatalf("report=%+v", report)
	}
}

// Evidence only serves recover. kander check and subscribe show an unknown as before.
func TestEvidenceStaysOutOfTheCheckOutput(t *testing.T) {
	resetLang(t)
	plain := Report{TaskID: "task", Status: Unknown, Detail: "reason"}
	marked := plain
	marked.Evidence, marked.Reference = &ExecutorEvidence{Window: "tmux:$1:@1:%1", Matches: 2}, "id"
	if formatReport(plain) != formatReport(marked) {
		t.Fatalf("%q != %q", formatReport(plain), formatReport(marked))
	}
	before, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(marked)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("%s != %s", before, after)
	}
}
