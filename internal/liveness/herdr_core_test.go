//go:build unix

package liveness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// writeFakeHCByPane installs an hc whose get answers with <dir>/<terminal>.json,
// or not_found when that file is missing. connect returns a batch RPC fixture.
func writeFakeHCByPane(t *testing.T) (program, dir string) {
	t.Helper()
	dir = t.TempDir()
	program = filepath.Join(dir, "hc")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + dir + `/calls"
if [ "$1" = connect ] && [ -f "` + dir + `/list.json" ]; then cat "` + dir + `/list.json"; exit 0; fi
if [ "$1" = get ] && [ -f "` + dir + `/$3.json" ]; then cat "` + dir + `/$3.json"; exit 0; fi
printf '%s\n' '{"error":{"kind":"not_found","message":"no terminal"}}' >&2
exit 3
`
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return program, dir
}

// One table holds the runtime rules of a herdr-core card: the terminal state
// decides first (finished or lost is lost whatever the status says); a live
// terminal passes Kander's state words through, reports error as blocked with
// the reason, and is unknown while its status is null.
func TestHerdrCoreRuntimeStateRules(t *testing.T) {
	resetLang(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	writeCompleteConfig(t)
	program, dir := writeFakeHCByPane(t)
	original := lookPath
	lookPath = func(string) (string, error) { return program, nil }
	t.Cleanup(func() { lookPath = original })
	cases := []struct {
		pane, state, status string
		want, runtime       string
		detail              string
	}{
		{"t1", "finished", `{"app":"claude-code","state":"idle"}`, Stopped, Unknown, ""},
		{"t2", "lost", `{"app":"claude-code","state":"working"}`, Stopped, Unknown, ""},
		{"t3", "live", `{"app":"claude-code","state":"working"}`, Alive, "working", ""},
		{"t4", "live", `{"app":"claude-code","state":"blocked"}`, Alive, "blocked", ""},
		{"t5", "live", `{"app":"claude-code","state":"idle"}`, Alive, "idle", ""},
		{"t6", "live", `{"app":"claude-code","state":"done"}`, Alive, "done", ""},
		{"t7", "live", `{"app":"claude-code","state":"error"}`, Alive, "blocked", "error"},
		{"t8", "live", "null", Unknown, Unknown, config.Text("program_status.unreported")},
		{"t9", "live", `{"app":"codex","state":"idle"}`, Stopped, Unknown, ""},
	}
	var inputs []AgentStatusInput
	var rows []json.RawMessage
	for _, tc := range cases {
		body := `{"address":"` + tc.pane + `","labels":{"kander-session":"s1"},"state":"` + tc.state + `","status":` + tc.status + `}`
		if err := os.WriteFile(filepath.Join(dir, tc.pane+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, AgentStatusInput{TaskID: tc.pane, Session: "claude s1", Window: "herdr-core:" + tc.pane})
		if tc.state == "live" {
			rows = append(rows, json.RawMessage(body))
		}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"terminals": rows}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "list.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	inputs = append(inputs, AgentStatusInput{TaskID: "stale-session", Session: "claude other", Window: "herdr-core:t5"})
	reports := ObserveAgentStatus(t.Context(), inputs, BatchOptions{})
	for index, tc := range cases {
		rep := reports[index]
		if rep.Status != tc.want || rep.RuntimeState != tc.runtime || !strings.Contains(rep.Detail, tc.detail) {
			t.Fatalf("%s %s %s: %+v; want %s/%s with %q", tc.pane, tc.state, tc.status, rep, tc.want, tc.runtime, tc.detail)
		}
	}
	if reports[len(cases)].Status != Stopped {
		t.Fatalf("batch lost session mismatch: %+v", reports[len(cases)])
	}
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), "connect --json") != 1 || strings.Count(string(calls), "get --json") != 2 {
		t.Fatalf("expected one list and confirmation only for the two missing panes: %s", calls)
	}
	if err := os.WriteFile(filepath.Join(dir, "list.json"), []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"unavailable"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, rep := range ObserveAgentStatus(t.Context(), inputs, BatchOptions{}) {
		if rep.Status != Unknown || rep.ObservationValid {
			t.Fatalf("failed batch reported presence: %+v", rep)
		}
	}
}
