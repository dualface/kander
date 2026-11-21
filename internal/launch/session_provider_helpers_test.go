package launch

import (
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

// The helpers below drive the session provider entry points the way the
// discovery tests set a launch up, so each test states only what it varies.

func mustSessionProvider(def *config.AgentSessionDefinition) sessionProvider {
	provider, err := sessionProviderFor(nil, "test", def)
	if err != nil {
		panic(err)
	}
	return provider
}

func fixedBudget(wait time.Duration) func(time.Duration) time.Duration {
	return func(time.Duration) time.Duration { return wait }
}

// sessionDiscoverSnapshot records the sessions before a launch.
func sessionDiscoverSnapshot(def *config.AgentSessionDefinition, task string, discover bool, program *process.AgentProgram, cwd string, budget func(time.Duration) time.Duration) (map[string]struct{}, error) {
	l := sessionLaunch{TaskID: task, CWD: cwd, Program: program, Definition: def, Budget: budget}
	return snapshotSessions(mustSessionProvider(def), l, discover)
}

// runSessionDiscoverHook finds the session a launch of def created.
func runSessionDiscoverHook(def *config.AgentSessionDefinition, task string, previous map[string]struct{}, start time.Time, program *process.AgentProgram, cwd string, budget func(time.Duration) time.Duration) (string, error) {
	l := sessionLaunch{TaskID: task, CWD: cwd, Start: start, Program: program, Definition: def, Budget: budget}
	return discoverLaunchSession(mustSessionProvider(def), l, previous)
}

// discoverNewAgentSession runs discovered-mode discovery with the given wait.
func discoverNewAgentSession(disc *config.SessionDiscovery, wait time.Duration, task string, previous map[string]struct{}, program *process.AgentProgram, cwd string) (string, error) {
	def := &config.AgentSessionDefinition{Mode: "discovered", Discovery: disc}
	return runSessionDiscoverHook(def, task, previous, time.Time{}, program, cwd, fixedBudget(wait))
}

// discoverNewCodexSession runs Codex rollout discovery with the given wait.
func discoverNewCodexSession(task string, start time.Time, cwd string, wait time.Duration) (string, error) {
	def := &config.AgentSessionDefinition{Mode: "hook:codex-rollout"}
	return runSessionDiscoverHook(def, task, nil, start, nil, cwd, fixedBudget(wait))
}

// discoverNewDroidSession runs Droid session discovery with the given wait.
func discoverNewDroidSession(previous map[string]struct{}, cwd string, wait time.Duration) (string, error) {
	def := &config.AgentSessionDefinition{Mode: "hook:droid-sessions"}
	return runSessionDiscoverHook(def, "task", previous, time.Time{}, nil, cwd, fixedBudget(wait))
}
