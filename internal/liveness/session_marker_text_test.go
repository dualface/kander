package liveness

import (
	"context"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/i18n"
	"github.com/dualface/kander/v2/internal/terminal"
)

const missingSessionIdentityKey = "liveness.session_identity_was_not_reported_direct_delivery_is_unavailable"

// The text points at the session marker and at the worker version, and no
// longer sends the user to reopen the container or promises that notify resumes.
func TestMissingSessionIdentityTextPointsAtTheMarkerAndTheWorker(t *testing.T) {
	for _, tc := range []struct {
		lang   string
		absent []string
	}{
		{"en", []string{"reopen", "resume"}},
		{"cn", []string{"重开容器", "恢复通道", "notify"}},
		{"ja", []string{"開き直", "resume", "notify"}},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			text := i18n.Text(tc.lang, missingSessionIdentityKey)
			for _, want := range []string{"kander.session", "kander doctor", "worker", "herdr-core"} {
				if !strings.Contains(text, want) {
					t.Errorf("the text lacks %q: %s", want, text)
				}
			}
			for _, bad := range tc.absent {
				if strings.Contains(text, bad) {
					t.Errorf("the text must not contain %q: %s", bad, text)
				}
			}
		})
	}
}

// An agent pane that reports no session identity is classified alive with the
// new text, whatever worker runs.
func TestClassifyAgentFactsReportsTheMissingSessionIdentityWithTheMarkerText(t *testing.T) {
	resetLang(t)
	pane := terminal.PaneFacts{Agent: "claude", AgentStatus: "idle"}
	report := classifyAgentFacts(context.Background(), board.Entry{TaskID: "no-identity"}, TaskSession{Agent: "claude", Reference: "wanted"},
		&resolvedLookupBackend{}, terminal.Address{Container: "t1", Pane: "p1"}, "herdr", pane, staleSearch{})
	if report.Status != Alive || report.Detail != i18n.Text("cn", missingSessionIdentityKey) {
		t.Fatalf("report=%+v", report)
	}
	if !strings.Contains(report.Detail, "kander.session") || !strings.Contains(report.Detail, "kander doctor") {
		t.Fatalf("the detail names the marker and the doctor command: %s", report.Detail)
	}
}
