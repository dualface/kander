//go:build unix

package menu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestConfigLegacyRolesWarnOnceOutsideJSONAndDoctorRepairs(t *testing.T) {
	h := newHarness(t)
	h.fakeCommand("codex", "")
	h.fakeCommand("cursor", "")
	h.writeConfig(defaultPayload(map[string]any{
		"language":      "en",
		"launcher":      "foreground",
		"reviewers":     map[string]any{"large": map[string]any{"CSA": "cursor", "Hacker": "codex", "PM": "codex"}},
		"review_stages": map[string]any{"large": map[string]any{"CSA": "required", "Hacker": "skip"}, "small": map[string]any{"PM": "auto"}},
	}))
	for _, args := range [][]string{{"config", "--json"}, {"config"}} {
		code, out, stderr := h.run(args...)
		if code != 0 || strings.Count(stderr, "Legacy review keys") != 1 {
			t.Fatalf("%d %s %s", code, out, stderr)
		}
		if len(args) == 2 {
			var cfg config.Config
			if err := json.Unmarshal([]byte(out), &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.Reviewers["large"]["Security"] != "cursor" || cfg.ReviewStages["large"]["Security"] != "required" {
				t.Fatalf("%+v", cfg)
			}
			if strings.Contains(out, `"PM"`) || strings.Contains(out, `"CSA"`) || strings.Contains(out, `"Hacker"`) {
				t.Fatal(out)
			}
		}
	}
	_, out, stderr := h.run("doctor")
	if !strings.Contains(out+stderr, "Legacy review keys") || !strings.Contains(out+stderr, "Saved new review keys") {
		t.Fatalf("%s %s", out, stderr)
	}
	cfg := readDoctorConfig(t, h)
	if len(cfg.LegacyReviewKeys()) != 0 || len(cfg.Reviewers["large"]) != 2 {
		t.Fatalf("%+v", cfg)
	}
	_, _, stderr = h.run("config", "--json")
	if strings.Contains(stderr, "Legacy review keys") {
		t.Fatal(stderr)
	}
}
