package install

import (
	"github.com/dualface/kander/internal/config"
)

// DisabledReaders lists the embedded agents that read the rules file target and have
// install_rules off. A project AGENTS.md is read by several agents, so turning one of them off
// keeps Kander from writing the shared file for all of them. An agent whose target cannot be
// resolved reads no known file and never blocks the write.
func DisabledReaders(target string, paths config.InstallPaths, switches config.InstallRules) []string {
	var out []string
	for _, agent := range config.ExecutionAgents {
		if switches.Enabled(agent) {
			continue
		}
		candidate, err := AgentRulesTarget(agent, paths)
		if err != nil || candidate == "" {
			continue
		}
		if candidate == target {
			out = append(out, agent)
		}
	}
	return out
}

// targetInjectionAllowed reports whether the scope switches let Kander write the rules file
// target; a switch that cannot be read allows nothing.
func targetInjectionAllowed(target string, paths config.InstallPaths) bool {
	switches, err := config.ScopeInstallRules(paths)
	if err != nil {
		return false
	}
	return len(DisabledReaders(target, paths, switches)) == 0
}
