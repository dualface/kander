package menu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigPrintsIssueResultOnDone(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"issue": map[string]any{"result_on_done": "ask"}}))
	code, out, err := h.run("config")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(out, ": ask") || !strings.Contains(out, "issue") {
		t.Fatalf("human output misses issue.result_on_done:\n%s", out)
	}
	code, out, err = h.run("config", "--json")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	var cfg struct {
		Issue struct {
			ResultOnDone string `json:"result_on_done"`
		} `json:"issue"`
	}
	if e := json.Unmarshal([]byte(out), &cfg); e != nil {
		t.Fatal(e)
	}
	if cfg.Issue.ResultOnDone != "ask" {
		t.Fatalf("json issue.result_on_done = %q", cfg.Issue.ResultOnDone)
	}
}
