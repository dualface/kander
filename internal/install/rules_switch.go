package install

import (
	"path/filepath"

	"github.com/dualface/kander/v2/internal/config"
)

// DisabledReaders lists the embedded agents that read the project rules file target below the
// main worktree root and have install_rules off. A project AGENTS.md is read by several
// agents, and a rules file may be a symlink to another agent's, so turning one reader off
// keeps Kander from writing the shared file for all of them.
func DisabledReaders(target, root string, switches config.InstallRules) []string {
	var out []string
	for _, agent := range config.ExecutionAgents {
		if switches.Enabled(agent) {
			continue
		}
		candidate := ProjectRulesTarget(agent, root)
		if candidate == "" {
			continue
		}
		if rulesFileKey(candidate) == rulesFileKey(target) {
			out = append(out, agent)
		}
	}
	return out
}

// rulesFileKey identifies the file a rules target names: its path with symlinks resolved when
// it exists, otherwise the path itself.
func rulesFileKey(target string) string {
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		return resolved
	}
	return target
}
