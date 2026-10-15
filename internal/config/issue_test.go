package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIssueResultOnDoneDefaultsToAuto(t *testing.T) {
	cfg, err := Validate(minimalPayload(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Issue.ResultOnDone != IssueResultAuto {
		t.Fatalf("missing section: %q", cfg.Issue.ResultOnDone)
	}
	cfg, err = Validate(minimalPayload(map[string]any{"issue": map[string]any{}}))
	if err != nil || cfg.Issue.ResultOnDone != IssueResultAuto {
		t.Fatalf("missing key: %q err=%v", cfg.Issue.ResultOnDone, err)
	}
}

func TestIssueResultOnDoneValidation(t *testing.T) {
	for _, mode := range IssueResultModes {
		cfg, err := Validate(minimalPayload(map[string]any{"issue": map[string]any{"result_on_done": mode}}))
		if err != nil || cfg.Issue.ResultOnDone != mode {
			t.Fatalf("%s: %q err=%v", mode, cfg.Issue.ResultOnDone, err)
		}
	}
	for _, value := range []any{"always", "", true, 1} {
		if _, err := Validate(minimalPayload(map[string]any{"issue": map[string]any{"result_on_done": value}})); err == nil {
			t.Fatalf("accepted %v", value)
		}
	}
	if _, err := Validate(minimalPayload(map[string]any{"issue": "auto"})); err == nil {
		t.Fatal("accepted a non-object issue section")
	}
}

func TestRepairWritesIssueResultOnDone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)
	if err := os.WriteFile(path, []byte(`{"language":"en","issue":{"result_on_done":"never"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Repair(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Issue.ResultOnDone != IssueResultAuto {
		t.Fatalf("repaired value = %q", cfg.Issue.ResultOnDone)
	}
	saved, err := Load(false)
	if err != nil || saved.Issue.ResultOnDone != IssueResultAuto {
		t.Fatalf("saved value = %q err=%v", saved.Issue.ResultOnDone, err)
	}
}

func TestOverlayOverridesIssueResultOnDone(t *testing.T) {
	scope := DefaultConfig()
	scope.WelcomeComplete = true
	merged, err := ApplyOverlay(scope, map[string]any{"issue": map[string]any{"result_on_done": "off"}})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Issue.ResultOnDone != IssueResultOff || scope.Issue.ResultOnDone != IssueResultAuto {
		t.Fatalf("merged=%q scope=%q", merged.Issue.ResultOnDone, scope.Issue.ResultOnDone)
	}
}
