package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

func writeSessionTestConfig(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
}

func writeSessionHeader(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := `{"type":"session","version":3,"id":"` + id + `","timestamp":"2026-09-19T00:00:00Z","cwd":"/tmp"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func listConn(stdout string) terminal.Conn {
	return terminal.Conn{Program: "herdr", Run: func(context.Context, string, []string) (probe.Result, error) {
		return probe.Result{Stdout: stdout}, nil
	}}
}

// The embedded herdr definition resolves a reported path-kind session
// identity through the agent's declared session file before matching.
func TestHerdrSessionAwareReverseLookup(t *testing.T) {
	resetLang(t)
	writeSessionTestConfig(t)
	backend := herdrBackendForTest()
	identity := terminal.Identity{Agent: "pi", Reference: "s1"}
	good := writeSessionHeader(t, "s1")
	other := writeSessionHeader(t, "s2")
	missing := filepath.Join(t.TempDir(), "gone.jsonl")
	row := func(tab, pane, agent, session string) string {
		return `{"pane_id":"` + pane + `","tab_id":"` + tab + `","agent":"` + agent + `","agent_session":` + session + `}`
	}
	list := func(rows ...string) string {
		return `{"result":{"panes":[` + joinRows(rows) + `]}}`
	}
	for name, tc := range map[string]struct {
		list    string
		address terminal.Address
		matches int
		err     bool
	}{
		"path resolves to same session": {
			list:    list(row("w9:t9", "w9:p9", "pi", `{"kind":"path","value":"`+good+`"}`)),
			address: terminal.Address{Container: "w9:t9", Pane: "w9:p9"},
		},
		"id still matches directly": {
			list:    list(row("w1:t1", "w1:p1", "pi", `{"kind":"id","value":"s1"}`)),
			address: terminal.Address{Container: "w1:t1", Pane: "w1:p1"},
		},
		"legacy value without kind": {
			list:    list(row("w1:t1", "w1:p1", "pi", `{"value":"s1"}`)),
			address: terminal.Address{Container: "w1:t1", Pane: "w1:p1"},
		},
		"path to another session is decided": {
			list:    list(row("w1:t1", "w1:p1", "pi", `{"kind":"path","value":"`+other+`"}`)),
			matches: 0,
		},
		"unresolvable candidate stays incomplete": {
			list: list(row("w1:t1", "w1:p1", "pi", `{"kind":"path","value":"`+missing+`"}`)),
			err:  true,
		},
		"match plus unresolvable stays incomplete": {
			list: list(
				row("w1:t1", "w1:p1", "pi", `{"kind":"id","value":"s1"}`),
				row("w1:t2", "w1:p2", "pi", `{"kind":"path","value":"`+missing+`"}`),
			),
			err: true,
		},
		"empty same-agent session stays incomplete": {
			list: list(row("w1:t1", "w1:p1", "pi", `null`)),
			err:  true,
		},
		"ambiguous decided matches": {
			list: list(
				row("w1:t1", "w1:p1", "pi", `{"kind":"id","value":"s1"}`),
				row("w2:t2", "w2:p2", "pi", `{"kind":"path","value":"`+good+`"}`),
			),
			matches: 2,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := backend.ReverseLookup(context.Background(), listConn(tc.list), identity)
			if tc.err {
				var incomplete *terminal.IncompleteLookupError
				if !errors.As(err, &incomplete) {
					t.Fatalf("err=%v is not an incomplete lookup", err)
				}
				return
			}
			if tc.address.Pane != "" {
				if err != nil || got != tc.address {
					t.Fatalf("address=%+v err=%v", got, err)
				}
				return
			}
			var matchErr *terminal.MatchError
			if !errors.As(err, &matchErr) || matchErr.Matches != tc.matches {
				t.Fatalf("err=%v matches=%v", err, tc.matches)
			}
		})
	}
}

func joinRows(rows []string) string {
	out := ""
	for index, row := range rows {
		if index > 0 {
			out += ","
		}
		out += row
	}
	return out
}

// pane_facts carries the reported kind so consumers share the same identity.
func TestHerdrPaneFactsCarriesSessionKind(t *testing.T) {
	resetLang(t)
	r := &recorder{reply: func([]string) probe.Result {
		return probe.Result{Stdout: `{"result":{"pane":{"pane_id":"w1:p1","tab_id":"w1:t1","agent":"pi","agent_status":"idle","agent_session":{"kind":"path","value":"/tmp/s.jsonl"}}}}`}
	}}
	facts, err := herdrBackendForTest().PaneFacts(context.Background(), r.conn(), "w1:p1")
	if err != nil {
		t.Fatal(err)
	}
	if facts.AgentSession != "/tmp/s.jsonl" || facts.AgentSessionKind != "path" {
		t.Fatalf("facts=%+v", facts)
	}
}
