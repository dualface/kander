package terminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/terminal/terminaltest"
	"golang.org/x/sys/unix"
)

func writeTestConfig(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

// writeSessionJSONL writes a pi-style session file: the first line is a
// session header and later lines are arbitrary records.
func writeSessionJSONL(t *testing.T, id string, firstLine string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if firstLine == "" {
		firstLine = `{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-09-19T00:00:00Z","cwd":"/tmp"}`
	}
	content := firstLine + "\n{\"type\":\"message\",\"id\":\"m1\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMatchAgentSessionDirectKinds(t *testing.T) {
	resetLanguage(t)
	writeTestConfig(t)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		agent, kind, value, reference string
		want                          SessionMatch
	}{
		"id match":          {"pi", "id", "s1", "s1", SessionMatches},
		"legacy no kind":    {"pi", "", "s1", "s1", SessionMatches},
		"id differ":         {"pi", "id", "s1", "s2", SessionDiffers},
		"non-uuid id match": {"pi", "id", "chat-42", "chat-42", SessionMatches},
		"empty value":       {"pi", "id", "", "s1", SessionUncertain},
		"empty both":        {"pi", "", "", "s1", SessionUncertain},
		"unknown kind":      {"pi", "url", "x", "s1", SessionUncertain},
		"kind alone":        {"pi", "path", "", "s1", SessionUncertain},
	} {
		t.Run(name, func(t *testing.T) {
			got, _ := MatchAgentSession(ctx, tc.agent, tc.kind, tc.value, tc.reference)
			if got != tc.want {
				t.Fatalf("verdict=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestMatchAgentSessionPathResolution(t *testing.T) {
	resetLanguage(t)
	writeTestConfig(t)
	ctx := context.Background()
	good := writeSessionJSONL(t, "s1", "")
	for name, tc := range map[string]struct {
		agent, value, reference string
		want                    SessionMatch
	}{
		"path match":           {"pi", good, "s1", SessionMatches},
		"path differ":          {"pi", good, "s2", SessionDiffers},
		"missing file":         {"pi", filepath.Join(t.TempDir(), "none.jsonl"), "s1", SessionUncertain},
		"directory not file":   {"pi", t.TempDir(), "s1", SessionUncertain},
		"relative path":        {"pi", "relative/session.jsonl", "s1", SessionUncertain},
		"agent without format": {"codex", good, "s1", SessionUncertain},
		"unknown agent":        {"not-an-agent", good, "s1", SessionUncertain},
	} {
		t.Run(name, func(t *testing.T) {
			got, detail := MatchAgentSession(ctx, tc.agent, "path", tc.value, tc.reference)
			if got != tc.want {
				t.Fatalf("verdict=%v detail=%s want=%v", got, detail, tc.want)
			}
			if got == SessionUncertain && detail == "" {
				t.Fatal("uncertain verdict must carry a diagnosable detail")
			}
		})
	}
}

func TestMatchAgentSessionHeaderValidation(t *testing.T) {
	resetLanguage(t)
	writeTestConfig(t)
	ctx := context.Background()
	longHeader := `{"type":"session","version":3,"id":"` + strings.Repeat("x", 9000) + `"}`
	for name, tc := range map[string]struct {
		firstLine string
		want      SessionMatch
	}{
		"header exceeds bound": {firstLine: longHeader, want: SessionUncertain},
		"header not json":      {firstLine: "not json", want: SessionUncertain},
		"header wrong type":    {firstLine: `{"type":"message","id":"s1"}`, want: SessionUncertain},
		"header missing id":    {firstLine: `{"type":"session","version":3}`, want: SessionUncertain},
		"header id not string": {firstLine: `{"type":"session","id":42}`, want: SessionUncertain},
		"header array":         {firstLine: `[1,2]`, want: SessionUncertain},
		"header empty line":    {firstLine: ` `, want: SessionUncertain},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeSessionJSONL(t, "s1", tc.firstLine)
			got, detail := MatchAgentSession(ctx, "pi", "path", path, "s1")
			if got != tc.want || (tc.want == SessionUncertain && detail == "") {
				t.Fatalf("verdict=%v detail=%s want=%v", got, detail, tc.want)
			}
		})
	}
	// A single header line without a trailing newline is a complete header.
	path := filepath.Join(t.TempDir(), "nonl.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session","id":"s1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := MatchAgentSession(ctx, "pi", "path", path, "s1"); got != SessionMatches {
		t.Fatalf("verdict=%v", got)
	}
}

func TestMatchAgentSessionSpecialFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fifo/socket are POSIX concepts")
	}
	resetLanguage(t)
	writeTestConfig(t)
	ctx := context.Background()
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan SessionMatch, 1)
	go func() {
		got, _ := MatchAgentSession(ctx, "pi", "path", fifo, "s1")
		done <- got
	}()
	select {
	case got := <-done:
		if got != SessionUncertain {
			t.Fatalf("verdict=%v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FIFO open blocked past the bound")
	}
}

func TestMatchAgentSessionCancelled(t *testing.T) {
	resetLanguage(t)
	writeTestConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := writeSessionJSONL(t, "s1", "")
	if got, detail := MatchAgentSession(ctx, "pi", "path", path, "s1"); got != SessionUncertain || detail == "" {
		t.Fatalf("verdict=%v detail=%s", got, detail)
	}
}

// sessionLookupDef mirrors the session-aware reverse_lookup rows of the
// embedded herdr definition, with a template-fake list step.
const sessionLookupDef = `{
	"steps": [{"store": "list", "argv": ["pane", "list"], "output": {"source": "stdout", "parse": "json_field:result"}}],
	"rows": {
		"from": "list",
		"split": "json_array:panes",
		"fields": {
			"tab": "json_field:tab_id",
			"pane": "json_field:pane_id",
			"agent": "json_field:agent",
			"kind": "json_field:agent_session.kind",
			"reference": "json_field:agent_session.value",
			"bad_agent": "regex:\"agent\":([^\"n])",
			"bad_session": "regex:\"agent_session\":([^{n])",
			"bad_kind": "regex:\"kind\":([^\"n])",
			"bad_reference": "regex:\"value\":([^\"n])",
			"valid_tab": "regex:\"tab_id\":\"([^:\\s\"\\\\]+:[^:\\s\"\\\\]+)\"",
			"valid_pane": "regex:\"pane_id\":\"([^:\\s\"\\\\]+:[^:\\s\"\\\\]+)\""
		},
		"expect": [["!field_missing:row.valid_tab", "!field_missing:row.valid_pane", "field_missing:row.bad_agent", "field_missing:row.bad_session", "field_missing:row.bad_kind", "field_missing:row.bad_reference"]],
		"match": [["field:row.agent={agent}"]],
		"session": {"kind": "kind", "value": "reference"},
		"result": {"container": "{row.tab}", "pane": "{row.pane}"},
		"messages": {"missing": "pane list has no panes", "invalid": "pane list contains an invalid pane", "incomplete": "lookup could not decide: {detail}"}
	}
}`

func sessionLookupBackend(t *testing.T) *DeclarativeBackend {
	t.Helper()
	return declarativeOp(t, OpReverseLookup, sessionLookupDef, nil)
}

func paneRow(tab, pane, agent, session string) string {
	return `{"pane_id":"` + pane + `","tab_id":"` + tab + `","agent":"` + agent + `","agent_session":` + session + `}`
}

func TestSessionRowLookupOutcomes(t *testing.T) {
	resetLanguage(t)
	writeTestConfig(t)
	backend := sessionLookupBackend(t)
	identity := Identity{Agent: "pi", Reference: "s1"}
	good := writeSessionJSONL(t, "s1", "")
	other := writeSessionJSONL(t, "s2", "")
	missing := filepath.Join(t.TempDir(), "gone.jsonl")
	for name, tc := range map[string]struct {
		list    string
		address Address
		matches int
		err     bool
	}{
		"id match": {
			list:    `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "pi", `{"kind":"id","value":"s1"}`) + `]}}`,
			address: Address{Container: "w1:t1", Pane: "w1:p1"},
		},
		"path match": {
			list:    `{"result":{"panes":[` + paneRow("w1:t2", "w1:p2", "pi", `{"kind":"path","value":"`+good+`"}`) + `]}}`,
			address: Address{Container: "w1:t2", Pane: "w1:p2"},
		},
		"cross-workspace path match": {
			list:    `{"result":{"panes":[` + paneRow("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+good+`"}`) + `]}}`,
			address: Address{Container: "w9:t9", Pane: "w9:p9"},
		},
		"decided zero match": {
			list: `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "pi", `{"kind":"id","value":"s2"}`) + `,` +
				paneRow("w1:t2", "w1:p2", "pi", `{"kind":"path","value":"`+other+`"}`) + `]}}`,
			matches: 0,
		},
		"decided ambiguous": {
			list: `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "pi", `{"kind":"id","value":"s1"}`) + `,` +
				paneRow("w1:t2", "w1:p2", "pi", `{"kind":"path","value":"`+good+`"}`) + `]}}`,
			matches: 2,
		},
		"unrelated agent never blocks": {
			list: `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "claude", `{"kind":"path","value":"`+missing+`"}`) + `,` +
				paneRow("w1:t2", "w1:p2", "pi", `{"kind":"id","value":"s1"}`) + `]}}`,
			address: Address{Container: "w1:t2", Pane: "w1:p2"},
		},
		"null session is undecidable": {
			list: `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "pi", `null`) + `]}}`,
			err:  true,
		},
		"match plus undecidable is incomplete": {
			list: `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "pi", `{"kind":"id","value":"s1"}`) + `,` +
				paneRow("w1:t2", "w1:p2", "pi", `{"kind":"path","value":"`+missing+`"}`) + `]}}`,
			err: true,
		},
		"undecidable path is incomplete": {
			list: `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "pi", `{"kind":"path","value":"`+missing+`"}`) + `]}}`,
			err:  true,
		},
		"unknown kind is incomplete": {
			list: `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "pi", `{"kind":"url","value":"https://x"}`) + `]}}`,
			err:  true,
		},
		"other agent path never resolves": {
			list:    `{"result":{"panes":[` + paneRow("w1:t1", "w1:p1", "codex", `{"kind":"path","value":"`+good+`"}`) + `]}}`,
			matches: 0,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := terminaltest.New(t, reply(tc.list, "pane", "list"))
			got, err := backend.ReverseLookup(context.Background(), fakeConn(fake), identity)
			if tc.err {
				var incomplete *IncompleteLookupError
				if !errors.As(err, &incomplete) {
					t.Fatalf("err=%v is not an incomplete lookup", err)
				}
				if incomplete.Candidates == 0 || err.Error() == "" {
					t.Fatalf("incomplete=%+v", incomplete)
				}
				return
			}
			if tc.address.Pane != "" {
				if err != nil || got != tc.address {
					t.Fatalf("address=%+v err=%v", got, err)
				}
				return
			}
			var matchErr *MatchError
			if !errors.As(err, &matchErr) || matchErr.Matches != tc.matches {
				t.Fatalf("err=%v matches=%v", err, tc.matches)
			}
		})
	}
}
