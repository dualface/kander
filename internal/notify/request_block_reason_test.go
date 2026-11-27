package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/i18n"
	"github.com/dualface/kander/v2/internal/worker"
)

// recordTerminalCalls replaces the fake herdr with one that only records its
// argv, so a test can prove the resolution never called the terminal.
func recordTerminalCalls(t *testing.T, root, fakeBin string) string {
	t.Helper()
	calls := filepath.Join(root, "herdr.calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + calls + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "herdr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return calls
}

func requireNoTerminalCall(t *testing.T, calls string) {
	t.Helper()
	if data, err := os.ReadFile(calls); !os.IsNotExist(err) {
		t.Fatalf("the resolution called the terminal: %q %v", data, err)
	}
}

func markerHint() string { return config.Text("notify.request_session_marker_hint", "") }

func requireNoNewReason(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("the resolution must fail")
	}
	bare := config.Text("notify.request_session_without_id", "claude")
	if strings.Contains(err.Error(), bare) || strings.Contains(err.Error(), markerHint()) {
		t.Fatalf("a failure unrelated to the session identity got a session reason: %v", err)
	}
}

func TestResolveRequestExplainsABareSessionWithoutProbing(t *testing.T) {
	root, fakeBin := setupBoard(t)
	calls := recordTerminalCalls(t, root, fakeBin)
	target, err := resolveRequest(worker.AgentAddress{Window: "herdr:w1:t9:w1:p9", Session: "codex"})
	if target != nil || err == nil {
		t.Fatalf("a bare SESSION must fail: %v", err)
	}
	if want := config.Text("notify.request_session_without_id", "codex"); err.Error() != want {
		t.Fatalf("reason = %q, want %q", err, want)
	}
	requireNoTerminalCall(t, calls)
}

func TestResolveRequestExplainsAnAgentThatRecordsNoSessionID(t *testing.T) {
	root, fakeBin := setupBoard(t)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Agents = map[string]config.AgentDefinition{"codex": {Session: &config.AgentSessionDefinition{Mode: "none"}}}
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	calls := recordTerminalCalls(t, root, fakeBin)
	_, err := resolveRequest(worker.AgentAddress{Window: "herdr:w1:t9:w1:p9", Session: "codex"})
	if want := config.Text("notify.request_session_mode_none", "codex"); err == nil || err.Error() != want {
		t.Fatalf("reason = %v, want %q", err, want)
	}
	requireNoTerminalCall(t, calls)
}

// A WINDOW that is no terminal address keeps its reason, bare SESSION or not.
func TestResolveRequestKeepsTheReasonOfAForegroundWindow(t *testing.T) {
	_, _ = setupBoard(t)
	_, err := resolveRequest(worker.AgentAddress{Window: "foreground", Session: "codex"})
	if want := config.Text("notify.no_direct_notification_address", "foreground"); err == nil || err.Error() != want {
		t.Fatalf("reason = %v, want %q", err, want)
	}
}

func TestResolveRequestHintsAtAnUnverifiedAgentPaneSession(t *testing.T) {
	_, _ = setupBoard(t)
	t.Setenv("KANBAN_HERDR_SESSION_FILE", filepath.Join(t.TempDir(), "absent"))
	_, err := resolveRequest(worker.AgentAddress{Window: "herdr:w1:t9:w1:p9", Session: "claude id-1"})
	probe := config.Text("notify.session_uncertain_task_pane", config.Text("terminal.session_identity_missing"))
	if want := config.Text("notify.request_session_marker_hint", probe); err == nil || err.Error() != want {
		t.Fatalf("reason = %v, want %q", err, want)
	}
}

func TestResolveRequestHintsAtADifferentAgentPaneSession(t *testing.T) {
	_, _ = setupBoard(t)
	t.Setenv("KANBAN_HERDR_SESSION", "id-2")
	_, err := resolveRequest(worker.AgentAddress{Window: "herdr:w1:t9:w1:p9", Session: "claude id-1"})
	if err == nil || !strings.Contains(err.Error(), markerHint()) {
		t.Fatalf("reason = %v, want the marker hint", err)
	}
}

func TestResolveRequestHintsAtAMissingProcessPaneMarker(t *testing.T) {
	_, _ = setupBoard(t)
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "")
	_, err := resolveRequest(worker.AgentAddress{Window: "tmux:$1:@1:%1", Session: "claude id-1"})
	probe := config.Text("launch.tmux_pane_has_no_session_marker", "%1")
	if want := config.Text("notify.request_session_marker_hint", probe); err == nil || err.Error() != want {
		t.Fatalf("reason = %v, want %q", err, want)
	}
}

func TestResolveRequestHintsAtADifferentProcessPaneMarker(t *testing.T) {
	_, _ = setupBoard(t)
	t.Setenv("KANBAN_TMUX_PANE_SESSION", "id-2")
	t.Setenv("KANBAN_TMUX_LIST_PANES", "")
	_, err := resolveRequest(worker.AgentAddress{Window: "tmux:$1:@1:%1", Session: "claude id-1"})
	if err == nil || !strings.Contains(err.Error(), markerHint()) {
		t.Fatalf("reason = %v, want the marker hint", err)
	}
}

func TestResolveRequestAddsNoSessionReasonToAMissingPane(t *testing.T) {
	_, _ = setupBoard(t)
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p9")
	_, err := resolveRequest(worker.AgentAddress{Window: "herdr:w1:t9:w1:p9", Session: "claude id-1"})
	requireNoNewReason(t, err)
}

func TestResolveRequestAddsNoSessionReasonToADifferentAgent(t *testing.T) {
	_, _ = setupBoard(t)
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	_, err := resolveRequest(worker.AgentAddress{Window: "herdr:w1:t9:w1:p9", Session: "claude id-1"})
	requireNoNewReason(t, err)
}

// The worker truncates a reason above 512 bytes; the new reasons fit in every
// language. None of them names the session ID.
func TestRequestBlockReasonsFitTheWorkerReasonLimit(t *testing.T) {
	for _, lang := range []string{"en", "cn", "ja"} {
		marker := func(detail string) string {
			return i18n.Text(lang, "notify.request_session_marker_hint", detail)
		}
		reasons := []string{
			i18n.Text(lang, "notify.request_session_without_id", "codex"),
			i18n.Text(lang, "notify.request_session_mode_none", "codex"),
			marker(i18n.Text(lang, "notify.session_uncertain_task_pane", i18n.Text(lang, "terminal.session_identity_missing"))),
			marker(i18n.Text(lang, "launch.tmux_pane_has_no_session_marker", "t1nx")),
		}
		for _, reason := range reasons {
			if len(reason) > requestReasonLimit {
				t.Errorf("%s reason has %d bytes: %s", lang, len(reason), reason)
			}
			if strings.Contains(reason, "notify.") || strings.Contains(reason, "launch.") {
				t.Errorf("%s reason renders a message ID: %s", lang, reason)
			}
		}
	}
}

// A session mismatch whose reverse lookup finds no pane composes the probe and
// the lookup diagnostics; the hint still ends the reason whole and within the
// worker limit, in every language, on both kinds of backend.
func TestResolveRequestKeepsTheMarkerHintWholeAfterAFailedLookup(t *testing.T) {
	const card, pane = "0d6a5f3e-5b8c-4c1e-9a7d-2f4b8e1c6a90", "7c1b9e2a-3d4f-4a6b-8c9d-0e1f2a3b4c5d"
	for _, lang := range []string{"en", "cn", "ja"} {
		for _, window := range []string{"herdr:w1:t9:w1:p9", "tmux:$1:@1:%1"} {
			t.Run(lang+"/"+window, func(t *testing.T) {
				_, _ = setupBoard(t)
				t.Setenv(config.EnvLang, lang)
				config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
				t.Setenv("KANBAN_HERDR_SESSION", pane)
				t.Setenv("KANBAN_TMUX_PANE_SESSION", pane)
				t.Setenv("KANBAN_TMUX_LIST_PANES", "")
				_, err := resolveRequest(worker.AgentAddress{Window: window, Session: "claude " + card})
				if err == nil {
					t.Fatal("the resolution must fail")
				}
				if len(err.Error()) > requestReasonLimit || !strings.HasSuffix(err.Error(), markerHint()) {
					t.Fatalf("reason has %d bytes or lost its hint: %s", len(err.Error()), err)
				}
			})
		}
	}
}

func TestSessionMarkerReasonShortensOnlyTheFailureText(t *testing.T) {
	_, _ = setupBoard(t)
	short := "pane gone"
	if got, want := sessionMarkerReason(short), config.Text("notify.request_session_marker_hint", short); got != want {
		t.Fatalf("a short failure text changed: %q", got)
	}
	got := sessionMarkerReason(strings.Repeat("界", requestReasonLimit))
	if len(got) > requestReasonLimit || !strings.HasSuffix(got, markerHint()) || !utf8.ValidString(got) {
		t.Fatalf("reason has %d bytes, lost its hint, or split a character: %q", len(got), got)
	}
}
