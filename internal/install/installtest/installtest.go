// Package installtest isolates tests from the caller's agent environment.
package installtest

import (
	"slices"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// AgentConfigEnvNames lists every environment variable that relocates the
// global config directory of a built-in agent, sorted and without duplicates.
func AgentConfigEnvNames() []string {
	var names []string
	for _, agent := range config.ExecutionAgents {
		spec, _ := config.RulesSpec(agent)
		for _, env := range spec.ConfigEnv {
			names = append(names, env.Name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// IsolateAgentConfigEnv clears those variables for the test, so a sandboxed
// HOME is not bypassed by a value inherited from the developer's shell.
func IsolateAgentConfigEnv(t testing.TB) {
	t.Helper()
	for _, name := range AgentConfigEnvNames() {
		t.Setenv(name, "")
	}
}
