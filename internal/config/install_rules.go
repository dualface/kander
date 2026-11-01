package config

import (
	"maps"
	"os"
	"slices"
)

// InstallRules is the rules-injection switch set of one scope: whether Kander
// may write each agent's rules files and rules extension. An agent value
// overrides the global value; agents without one follow it.
type InstallRules struct {
	Global bool
	Agents map[string]bool
}

// Enabled reports the effective switch of one agent.
func (r InstallRules) Enabled(agent string) bool {
	if value, ok := r.Agents[agent]; ok {
		return value
	}
	return r.Global
}

// InstallRulesOf returns the switches a config carries. Only builtin agents
// can carry their own value; validation rejects it on a custom agent.
func InstallRulesOf(cfg *Config) InstallRules {
	out := InstallRules{Global: true, Agents: map[string]bool{}}
	if cfg == nil {
		return out
	}
	out.Global = cfg.InstallRules
	for name, def := range cfg.Agents {
		if def.InstallRules != nil {
			out.Agents[name] = *def.InstallRules
		}
	}
	return out
}

// ScopeInstallRules returns the switches from the scope config file of paths
// (KANDER_CONFIG when set). A project overlay is never read: it ships with a
// repository and must not turn injection into the user's global agent files on
// or off. A missing config file means every switch is on.
func ScopeInstallRules(paths InstallPaths) (InstallRules, error) {
	file := ScopeConfigFile(paths)
	if file == "" {
		return InstallRulesOf(nil), nil
	}
	cfg, _, err := loadScopeRawAt(file, true)
	if err != nil {
		return InstallRules{}, err
	}
	return InstallRulesOf(cfg), nil
}

// ScopeConfigFile is the config file that holds the scope-only keys of paths:
// KANDER_CONFIG when set, otherwise the scope's config.json.
func ScopeConfigFile(paths InstallPaths) string {
	if override := os.Getenv(EnvConfig); override != "" {
		return expandUser(override)
	}
	return paths.ConfigPath
}

// SetAgentInstallRules stores one agent's own switch; nil removes it so the
// agent follows the global value again. An agent entry left empty goes too.
func SetAgentInstallRules(cfg *Config, agent string, value *bool) {
	def, exists := cfg.Agents[agent]
	if value == nil {
		if !exists {
			return
		}
		def.InstallRules = nil
		if isZeroAgentDefinition(def) {
			delete(cfg.Agents, agent)
			return
		}
		cfg.Agents[agent] = def
		return
	}
	if cfg.Agents == nil {
		cfg.Agents = map[string]AgentDefinition{}
	}
	v := *value
	def.InstallRules = &v
	cfg.Agents[agent] = def
}

// ApplyAgentInstallRules makes cfg carry exactly the agent values of switches.
func ApplyAgentInstallRules(cfg *Config, switches InstallRules) {
	ClearAgentInstallRules(cfg)
	for agent, value := range switches.Agents {
		SetAgentInstallRules(cfg, agent, &value)
	}
}

// ClearAgentInstallRules removes every agent's own switch, so all agents
// follow the global value.
func ClearAgentInstallRules(cfg *Config) {
	for _, name := range slices.Collect(maps.Keys(cfg.Agents)) {
		SetAgentInstallRules(cfg, name, nil)
	}
}

func isZeroAgentDefinition(def AgentDefinition) bool {
	return def == AgentDefinition{}
}

// validateInstallRules accepts a missing key as on, so configs written before
// the key existed keep injecting the rules references.
func validateInstallRules(obj map[string]any) (bool, error) {
	raw, exists := obj["install_rules"]
	if !exists {
		return true, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, configErrorf("config.install_rules_must_be_a_boolean")
	}
	return value, nil
}

// dropOverlayInstallRules removes install_rules and agents.<name>.install_rules
// from an overlay copy before the merge, the same way config_dir is dropped.
func dropOverlayInstallRules(overlay map[string]any) {
	delete(overlay, "install_rules")
	dropOverlayAgentField(overlay, "install_rules")
}

// OverlayInstallRules returns the project overlay path and the install_rules
// keys it declares, as dotted paths; ScopeInstallRules ignores those values.
func OverlayInstallRules() (string, []string, error) {
	file, raw, err := readOverlay("")
	if err != nil || raw == nil {
		return "", nil, err
	}
	var keys []string
	if _, ok := raw["install_rules"]; ok {
		keys = append(keys, "install_rules")
	}
	agents, _ := asObject(raw["agents"])
	for name, value := range agents {
		fields, _ := asObject(value)
		if _, ok := fields["install_rules"]; ok {
			keys = append(keys, "agents."+name+".install_rules")
		}
	}
	slices.Sort(keys)
	return file, keys, nil
}

// formatInstallRulesLines lists the global switch and every agent's own value.
// It reads cfg rather than the effective defaults, so a config that is not
// initialized yet still shows a switch turned off by uninstall.
func formatInstallRulesLines(cfg *Config) []string {
	switches := InstallRulesOf(cfg)
	lines := []string{Text("config.install_rules") + ": " + FormatSwitch(switches.Global)}
	for _, agent := range slices.Sorted(maps.Keys(switches.Agents)) {
		lines = append(lines, Text("config.install_rules")+" "+agent+": "+FormatSwitch(switches.Agents[agent]))
	}
	return lines
}
