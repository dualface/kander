package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func defaultRaw(t *testing.T) map[string]any {
	t.Helper()
	data, err := json.Marshal(DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := decodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw.(map[string]any)
}

func TestInstallRulesMissingKeysMeanOn(t *testing.T) {
	raw := defaultRaw(t)
	delete(raw, "install_rules")
	cfg, err := Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	switches := InstallRulesOf(cfg)
	for _, agent := range ExecutionAgents {
		if !switches.Enabled(agent) {
			t.Fatalf("%s off in a config without install_rules", agent)
		}
	}
}

func TestInstallRulesValidation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"global string", func(raw map[string]any) { raw["install_rules"] = "no" }},
		{"agent string", func(raw map[string]any) {
			raw["agents"] = map[string]any{"codex": map[string]any{"install_rules": "no"}}
		}},
		{"custom agent", func(raw map[string]any) {
			raw["agents"] = map[string]any{"wrapper": map[string]any{"path": "sh", "dialect": "codex", "install_rules": false}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := defaultRaw(t)
			tt.mutate(raw)
			if _, err := Validate(raw); err == nil || !strings.Contains(err.Error(), "install_rules") {
				t.Fatalf("err=%v", err)
			}
		})
	}
	raw := defaultRaw(t)
	raw["agents"] = map[string]any{"codex": map[string]any{"install_rules": false}}
	cfg, err := Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v := cfg.Agents["codex"].InstallRules; v == nil || *v {
		t.Fatalf("codex install_rules=%v", v)
	}
}

func TestInstallRulesAgentValueOverridesGlobal(t *testing.T) {
	on, off := true, false
	for _, tt := range []struct {
		global bool
		agent  *bool
		want   bool
	}{
		{true, nil, true},
		{true, &on, true},
		{true, &off, false},
		{false, nil, false},
		{false, &on, true},
		{false, &off, false},
	} {
		cfg := DefaultConfig()
		cfg.InstallRules = tt.global
		SetAgentInstallRules(cfg, "codex", tt.agent)
		if got := InstallRulesOf(cfg).Enabled("codex"); got != tt.want {
			t.Fatalf("global=%v agent=%v got %v want %v", tt.global, tt.agent, got, tt.want)
		}
	}
}

func TestClearAgentInstallRulesKeepsOtherFields(t *testing.T) {
	off := false
	cfg := DefaultConfig()
	cfg.Agents = map[string]AgentDefinition{"pi": {ConfigDir: "~/pi"}}
	SetAgentInstallRules(cfg, "pi", &off)
	SetAgentInstallRules(cfg, "codex", &off)
	ClearAgentInstallRules(cfg)
	if _, ok := cfg.Agents["codex"]; ok {
		t.Fatal("agent entry left empty by the removal stays")
	}
	if def := cfg.Agents["pi"]; def.ConfigDir != "~/pi" || def.InstallRules != nil {
		t.Fatalf("pi=%+v", def)
	}
}

func TestOverlayInstallRulesIgnored(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	scope := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, scope)
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	if _, err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(work, OverlayFilename)
	data := `{"install_rules":false,"agents":{"codex":{"install_rules":false},"wrapper":{"install_rules":true}}}`
	if err := os.WriteFile(overlay, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	file, keys, err := OverlayInstallRules()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"agents.codex.install_rules", "agents.wrapper.install_rules", "install_rules"}
	if !sameFile(t, file, overlay) || !slices.Equal(keys, want) {
		t.Fatalf("file=%q keys=%v", file, keys)
	}
	loaded, err := Load(false)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.InstallRules {
		t.Fatal("overlay turned the global switch off")
	}
	if _, ok := loaded.Agents["codex"]; ok {
		t.Fatal("overlay agent switch merged")
	}
	encoded, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"install_rules":true`) || strings.Contains(string(encoded), `"install_rules":false`) {
		t.Fatalf("config json=%s", encoded)
	}
	switches, err := ScopeInstallRules(InstallPaths{})
	if err != nil || !switches.Enabled("codex") {
		t.Fatalf("scope switches=%+v err=%v", switches, err)
	}
}

func TestConfigLinesShowInstallRules(t *testing.T) {
	t.Chdir(t.TempDir())
	off := false
	cfg := DefaultConfig()
	cfg.WelcomeComplete = true
	SetAgentInstallRules(cfg, "codex", &off)
	lines, err := FormatConfigLines(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	global := Text("config.install_rules") + ": " + FormatSwitch(true)
	agent := Text("config.install_rules") + " codex: " + FormatSwitch(false)
	if !strings.Contains(joined, global) || !strings.Contains(joined, agent) {
		t.Fatalf("lines=%s", joined)
	}
}

func TestUpdateAtCreatesMissingConfig(t *testing.T) {
	file := filepath.Join(t.TempDir(), "kander", "config.json")
	if _, err := UpdateAt(file, func(cfg *Config) error {
		cfg.InstallRules = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadValidated(file, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InstallRules || cfg.WelcomeComplete {
		t.Fatalf("install_rules=%v welcome=%v", cfg.InstallRules, cfg.WelcomeComplete)
	}
}

func TestRepairKeepsInstallRules(t *testing.T) {
	raw := defaultRaw(t)
	raw["install_rules"] = false
	raw["agents"] = map[string]any{"codex": map[string]any{"install_rules": true}}
	delete(raw, "launcher")
	cfg, err := repairValues(raw)
	if err != nil {
		t.Fatal(err)
	}
	switches := InstallRulesOf(cfg)
	if switches.Global || !switches.Enabled("codex") || switches.Enabled("claude") {
		t.Fatalf("repair lost the switches: %+v", switches)
	}
}
