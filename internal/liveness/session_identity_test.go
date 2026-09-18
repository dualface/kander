package liveness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

// writePiSessionFile writes a pi-style session JSONL: a session header first
// line, then an arbitrary record.
func writePiSessionFile(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-09-19T00:00:00Z","cwd":"/tmp"}` + "\n{\"type\":\"message\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func classifyText(session, window string) string {
	return "- SESSION: " + session + "\n- WINDOW: " + window + "\n"
}

func listJSON(rows ...string) string {
	return `{"result":{"panes":[` + strings.Join(rows, ",") + `]}}`
}

func herdrRow(tab, pane, agent, session string) string {
	return `{"pane_id":"` + pane + `","tab_id":"` + tab + `","agent":"` + agent + `","agent_status":"idle","agent_session":` + session + `}`
}

// The reported path-kind identity resolves through the agent session file:
// the forward check proves the same session instead of calling it stale.
func TestHerdrPathIdentityForwardAlive(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	file := writePiSessionFile(t, "wanted")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION", file)
	report := ClassifyTask(board.Entry{TaskID: "fwd-path"}, classifyText("pi wanted", "herdr:w1:t1:w1:p1"))
	if report.Status != Alive {
		t.Fatalf("report=%+v", report)
	}
}

// A path-kind pane resolving to a different session is a proven mismatch:
// the reverse lookup finds the real pane in another workspace.
func TestHerdrPathIdentityForwardMismatchDrifts(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	other := writePiSessionFile(t, "other")
	file := writePiSessionFile(t, "wanted")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION", other)
	// The re-probe of the drifted pane resolves the same path-kind identity.
	t.Setenv("KANBAN_HERDR_PANE2", "w9:p9")
	t.Setenv("KANBAN_HERDR_AGENT2", "pi")
	t.Setenv("KANBAN_HERDR_TAB_ID2", "w9:t9")
	t.Setenv("KANBAN_HERDR_SESSION_KIND2", "path")
	t.Setenv("KANBAN_HERDR_SESSION2", file)
	t.Setenv("KANBAN_HERDR_LIST_JSON", listJSON(
		herdrRow("w1:t1", "w1:p1", "pi", `{"kind":"path","value":"`+other+`"}`),
		herdrRow("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+file+`"}`),
	))
	report := ClassifyTask(board.Entry{TaskID: "fwd-mismatch"}, classifyText("pi wanted", "herdr:w1:t1:w1:p1"))
	if report.Status != Drifted || report.NewWindow != "herdr:w9:t9:w9:p9" {
		t.Fatalf("report=%+v", report)
	}
}

// An unresolvable reported identity is undecidable, never stale evidence.
func TestHerdrPathIdentityForwardUnresolvableIsUnknown(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	missing := filepath.Join(t.TempDir(), "gone.jsonl")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION", missing)
	report := ClassifyTask(board.Entry{TaskID: "fwd-uncertain"}, classifyText("pi wanted", "herdr:w1:t1:w1:p1"))
	if report.Status != Unknown || report.Detail == "" {
		t.Fatalf("report=%+v", report)
	}
}

// Reverse lookup across the whole pane list resolves the path-kind identity
// of the only matching pane.
func TestHerdrPathIdentityReverseLookupDrifts(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	file := writePiSessionFile(t, "wanted")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p1")
	t.Setenv("KANBAN_HERDR_SESSION_KIND", "path")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_SESSION", file)
	t.Setenv("KANBAN_HERDR_LIST_JSON", listJSON(
		herdrRow("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+file+`"}`),
		herdrRow("w8:t8", "w8:p8", "claude", `{"kind":"id","value":"wanted"}`),
	))
	report := ClassifyTask(board.Entry{TaskID: "rev-path"}, classifyText("pi wanted", "herdr:w1:t1:w1:p1"))
	if report.Status != Drifted || report.NewWindow != "herdr:w9:t9:w9:p9" {
		t.Fatalf("report=%+v", report)
	}
}

// An unresolvable same-agent candidate makes the lookup incomplete: it is
// unknown, never a proven absence and never a unique match.
func TestHerdrPathIdentityLookupIncomplete(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	file := writePiSessionFile(t, "wanted")
	missing := filepath.Join(t.TempDir(), "gone.jsonl")
	for name, list := range map[string]string{
		"only unresolvable": listJSON(
			herdrRow("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+missing+`"}`),
		),
		"match plus unresolvable": listJSON(
			herdrRow("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+file+`"}`),
			herdrRow("w8:t8", "w8:p8", "pi", `{"kind":"path","value":"`+missing+`"}`),
		),
		"empty session candidate": listJSON(
			herdrRow("w9:t9", "w9:p9", "pi", `null`),
		),
		"unknown kind candidate": listJSON(
			herdrRow("w9:t9", "w9:p9", "pi", `{"kind":"url","value":"https://x"}`),
		),
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p1")
			t.Setenv("KANBAN_HERDR_LIST_JSON", list)
			report := ClassifyTask(board.Entry{TaskID: "rev-incomplete"}, classifyText("pi wanted", "herdr:w1:t1:w1:p1"))
			if report.Status != Unknown {
				t.Fatalf("report=%+v", report)
			}
		})
	}
}

// A complete lookup that decided every same-agent candidate keeps stopped.
func TestHerdrPathIdentityLookupDecidedZeroStaysStopped(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	other := writePiSessionFile(t, "other")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p1")
	t.Setenv("KANBAN_HERDR_LIST_JSON", listJSON(
		herdrRow("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+other+`"}`),
		herdrRow("w8:t8", "w8:p8", "claude", `{"kind":"id","value":"wanted"}`),
	))
	report := ClassifyTask(board.Entry{TaskID: "rev-zero"}, classifyText("pi wanted", "herdr:w1:t1:w1:p1"))
	if report.Status != Stopped {
		t.Fatalf("report=%+v", report)
	}
}

// Revalidation re-resolves the drifted pane's identity: an identity that
// changed between lookup and revalidation is unknown, never drift evidence.
func TestHerdrPathIdentityRevalidationChanged(t *testing.T) {
	resetLang(t)
	installPOSIXFakes(t, true)
	file := writePiSessionFile(t, "wanted")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p1")
	t.Setenv("KANBAN_HERDR_AGENT", "pi")
	t.Setenv("KANBAN_HERDR_LIST_JSON", listJSON(
		herdrRow("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+file+`"}`),
	))
	for name, tc := range map[string]struct {
		kind, value, want string
	}{
		"re-probe different id":  {"id", "other", Stopped},
		"re-probe unresolvable":  {"path", filepath.Join(t.TempDir(), "gone.jsonl"), Unknown},
		"re-probe same resolved": {"path", file, Drifted},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("KANBAN_HERDR_SESSION_KIND", tc.kind)
			t.Setenv("KANBAN_HERDR_SESSION", tc.value)
			report := ClassifyTask(board.Entry{TaskID: "rev-revalidate"}, classifyText("pi wanted", "herdr:w1:t1:w1:p1"))
			if report.Status != tc.want {
				t.Fatalf("report=%+v want=%s", report, tc.want)
			}
		})
	}
}
