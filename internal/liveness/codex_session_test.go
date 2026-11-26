//go:build unix

package liveness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// A Codex card whose SESSION carries the discovered id, as a remote start on
// herdr-core now records it, is alive in the pane whose marker is that id and
// stopped in a pane that carries another session.
func TestHerdrCoreCodexSessionIsJudgedByThePaneMarker(t *testing.T) {
	resetLang(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	writeCompleteConfig(t)
	program, dir := writeFakeHCByPane(t)
	original := lookPath
	lookPath = func(string) (string, error) { return program, nil }
	t.Cleanup(func() { lookPath = original })
	const id = "01a20fe4-5f0f-7a42-a040-cd6e31cc675c"
	panes := map[string]string{"t1": id, "t2": "01a20d36-0000-7000-8000-000000000000"}
	var rows []json.RawMessage
	for pane, marker := range panes {
		body := `{"address":"` + pane + `","labels":{"kander.session":"` + marker + `"},"state":"live","status":{"app":"codex","state":"idle"}}`
		if err := os.WriteFile(filepath.Join(dir, pane+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, json.RawMessage(body))
	}
	list, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"terminals": rows}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "list.json"), list, 0o600); err != nil {
		t.Fatal(err)
	}
	reports := ObserveAgentStatus(t.Context(), []AgentStatusInput{
		{TaskID: "marked", Session: "codex " + id, Window: "herdr-core:t1"},
		{TaskID: "other-session", Session: "codex " + id, Window: "herdr-core:t2"},
	}, BatchOptions{})
	if reports[0].Status != Alive || reports[0].RuntimeState != "idle" {
		t.Fatalf("pane with the recorded marker: %+v", reports[0])
	}
	if reports[1].Status != Stopped {
		t.Fatalf("pane with another marker: %+v", reports[1])
	}
}
