package liveness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestReverseLookupOutcomes(t *testing.T) {
	const herdrMatch = `{"pane_id":"w9:p9","tab_id":"w9:t9","agent":"codex","agent_session":{"value":"wanted"}}`
	const tmuxMatch = "%9\t$9\tnine\t@9\tcodex\t0\twanted\t"
	for _, test := range []struct {
		name, channel, output, status, window string
		matches                               int
	}{
		{"herdr-zero", "herdr", `{"result":{"panes":[]}}`, Stopped, "", 0},
		{"herdr-one", "herdr", `{"result":{"panes":[` + herdrMatch + `]}}`, Drifted, "herdr:w9:t9:w9:p9", 1},
		{"herdr-many", "herdr", `{"result":{"panes":[` + herdrMatch + `,` + herdrMatch + `]}}`, Unknown, "", 2},
		{"herdr-json", "herdr", "not json", Unknown, "", -1},
		{"herdr-missing-list", "herdr", `{"result":{}}`, Unknown, "", -1},
		{"herdr-invalid-row", "herdr", `{"result":{"panes":[null]}}`, Unknown, "", -1},
		{"herdr-invalid-id", "herdr", `{"result":{"panes":[{"agent":"codex","agent_session":{"value":"wanted"}}]}}`, Unknown, "", -1},
		{"herdr-invalid-session", "herdr", `{"result":{"panes":[` + strings.Replace(herdrMatch, `"wanted"`, `42`, 1) + `]}}`, Unknown, "", -1},
		{"herdr-partial-list", "herdr", `{"result":{"panes":[` + herdrMatch + `,null]}}`, Unknown, "", -1},
		{"tmux-zero", "tmux", strings.Replace(tmuxMatch, "wanted", "other", 1), Stopped, "", 0},
		{"tmux-one", "tmux", tmuxMatch, Drifted, "tmux:$9:@9:%9", 1},
		{"tmux-session-one", "tmux-session", tmuxMatch, Drifted, "tmux-session:nine:@9:%9", 1},
		{"tmux-many", "tmux", tmuxMatch + "\n" + tmuxMatch, Unknown, "", 2},
		{"tmux-empty", "tmux", "", Unknown, "", -1},
		{"tmux-invalid-row", "tmux", "bad output", Unknown, "", -1},
		{"tmux-invalid-id", "tmux", strings.Replace(tmuxMatch, "%9", "", 1), Unknown, "", -1},
		{"tmux-invalid-dead", "tmux", strings.Replace(tmuxMatch, "\t0\t", "\tbad\t", 1), Unknown, "", -1},
		{"tmux-partial-list", "tmux", tmuxMatch + "\nbad output", Unknown, "", -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetLang(t)
			installPOSIXFakes(t, true)
			session := TaskSession{Agent: "codex", Reference: "wanted"}
			var lookupErr error
			window := test.channel + ":$1:@1:%1"
			if test.channel == "herdr" {
				window = "herdr:w1:t1:w1:p1"
				t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p1")
				t.Setenv("KANBAN_HERDR_SESSION", "wanted")
				t.Setenv("KANBAN_HERDR_LIST_JSON", test.output)
				_, _, lookupErr = HerdrReverseLookup("herdr", session)
			} else {
				t.Setenv("KANBAN_TMUX_STALE_PANE", "%1")
				t.Setenv("KANBAN_TMUX_LIST_PANES", test.output)
				_, lookupErr = TmuxReverseLookup("tmux", session)
			}
			var matchErr *lookupMatchError
			switch test.matches {
			case 1:
				if lookupErr != nil {
					t.Fatal(lookupErr)
				}
			case -1:
				if lookupErr == nil || errors.As(lookupErr, &matchErr) {
					t.Fatalf("invalid output is a collection failure: %v", lookupErr)
				}
			default:
				if !errors.As(lookupErr, &matchErr) || matchErr.matches != test.matches {
					t.Fatalf("matches=%d error=%v", test.matches, lookupErr)
				}
			}
			report := ClassifyTask(board.Entry{TaskID: "lookup"}, "- SESSION: codex wanted\n- WINDOW: "+window+"\n")
			if report.Status != test.status || report.NewWindow != test.window {
				t.Fatalf("report=%+v", report)
			}
			if lookupErr != nil && (!strings.Contains(report.Detail, lookupErr.Error()) || !strings.Contains(report.Detail, "反查:")) {
				t.Fatalf("lookup cause or stage lost: %+v", report)
			}
		})
	}
}

// The audit reproduced this path as stopped; failed lookup is not absence.
func TestAuditReverseLookupErrorBecomesUnknown(t *testing.T) {
	for _, channel := range []string{"herdr", "tmux"} {
		t.Run(channel, func(t *testing.T) {
			tempBoard(t)
			installPOSIXFakes(t, true)
			id, path := makeWorking(t, "audit-lookup-error", "反查错误")
			window := "tmux:$1:@1:%1"
			if channel == "herdr" {
				window = "herdr:w1:t1:w1:p1"
				script := "#!/bin/sh\nif [ \"$2\" = get ]; then\n echo '{\"error\":{\"code\":\"pane_not_found\"}}' >&2\nelse\n echo 'audit lookup failure' >&2\nfi\nexit 1\n"
				if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("KANBAN_TMUX_STALE_PANE", "%1")
				t.Setenv("KANBAN_TMUX_LIST_PANES_FAIL", "1")
			}
			setLocation(t, path, "codex wanted", window)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			code, out, _ := capture(t, func() int { return RunCheck([]string{id}) })
			if code != 0 || !strings.Contains(out, "状态=unknown") || !strings.Contains(out, "failure") || !strings.Contains(out, "反查:") {
				t.Fatalf("code=%d output=%s", code, out)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatalf("check mutated card: %v", err)
			}
		})
	}
}

func TestReverseLookupTimeoutIsUnknown(t *testing.T) {
	for _, channel := range []string{"herdr", "tmux"} {
		t.Run(channel, func(t *testing.T) {
			resetLang(t)
			installPOSIXFakes(t, true)
			// exec leaves no child holding the output pipe after timeout kills it.
			if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), channel), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			report := staleReport(ctx, board.Entry{TaskID: "timeout"}, TaskSession{Agent: "codex", Reference: "wanted"}, channel, "old", "old pane gone", channel, channel, true)
			if report.Status != Unknown || report.NewWindow != "" || !strings.Contains(report.Detail, "context deadline exceeded") || !strings.Contains(report.Detail, "old pane gone") || !strings.Contains(report.Detail, "反查:") {
				t.Fatalf("report=%+v", report)
			}
		})
	}
}

func TestLivenessIdentityAndReadinessSemantics(t *testing.T) {
	for _, test := range []struct {
		name, channel, session, agent, reference, state, status string
		allowLookup                                             bool
	}{
		{"herdr-agent-mismatch", "herdr", "codex wanted", "claude", "wanted", "idle", Stopped, true},
		{"herdr-session-mismatch", "herdr", "codex wanted", "codex", "other", "idle", Stopped, true},
		{"herdr-working", "herdr", "codex wanted", "codex", "wanted", "working", Alive, true},
		{"herdr-blocked", "herdr", "codex wanted", "codex", "wanted", "blocked", Alive, true},
		{"herdr-codex-empty", "herdr", "codex", "claude", "", "idle", Stopped, true},
		{"herdr-no-lookup", "herdr", "codex wanted", "claude", "wanted", "idle", Stopped, false},
		{"tmux-process-mismatch", "tmux", "codex wanted", "claude", "wanted", "0", Stopped, true},
		{"tmux-session-mismatch", "tmux", "codex wanted", "codex", "other", "0", Stopped, true},
		{"tmux-copy-mode", "tmux", "codex wanted", "codex", "wanted", "1", Alive, true},
		{"tmux-codex-empty", "tmux", "codex", "claude", "", "0", Stopped, true},
		{"tmux-no-lookup", "tmux", "codex wanted", "claude", "wanted", "0", Stopped, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetLang(t)
			installPOSIXFakes(t, true)
			window := "tmux:$1:@1:%1"
			if test.channel == "herdr" {
				window = "herdr:w1:t1:w1:p1"
				t.Setenv("KANBAN_HERDR_AGENT", test.agent)
				t.Setenv("KANBAN_HERDR_SESSION", test.reference)
				t.Setenv("KANBAN_HERDR_STATUS", test.state)
				t.Setenv("KANBAN_HERDR_LIST_JSON", `{"result":{"panes":[]}}`)
				if !test.allowLookup || test.session == "codex" {
					t.Setenv("KANBAN_HERDR_LIST_JSON", "unexpected lookup")
				}
			} else {
				t.Setenv("KANBAN_TMUX_CURRENT_COMMAND", test.agent)
				t.Setenv("KANBAN_TMUX_PANE_SESSION", test.reference)
				t.Setenv("KANBAN_TMUX_IN_MODE", test.state)
				t.Setenv("KANBAN_TMUX_LIST_PANES", "%9\t$9\tnine\t@9\tcodex\t0\tother")
				if !test.allowLookup || test.session == "codex" {
					t.Setenv("KANBAN_TMUX_LIST_PANES_FAIL", "1")
				}
			}
			report := ClassifyTaskLookup(board.Entry{TaskID: "identity"}, "- SESSION: "+test.session+"\n- WINDOW: "+window+"\n", test.allowLookup)
			if report.Status != test.status || report.NewWindow != "" {
				t.Fatalf("report=%+v", report)
			}
		})
	}
}
