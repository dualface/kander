package menu

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/probe"
	"github.com/dualface/kander/v2/internal/process"
)

type agentState struct {
	Path      string
	Version   string
	Batch     bool
	Execution bool
	Review    bool
	reviewer  *agentState
}

func versionFailedText() string {
	return config.Text("menu.version_check_failed")
}

func agentLabels() map[string]string {
	labels := make(map[string]string, len(config.ExecutionAgents))
	for _, name := range config.ExecutionAgents {
		labels[name] = config.AgentDisplayName(name)
	}
	return labels
}

func currentPaths() (config.InstallPaths, error) {
	return config.CurrentInstallPaths()
}

func rulesEntry(paths config.InstallPaths) string {
	return install.RulesEntry(paths)
}

func lookPath(name string) string {
	found, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return found
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func findCommands() map[string]string {
	return map[string]string{"kander": lookPath("kander")}
}

func commandMissingMessage(name string) string {
	return config.Text(
		"menu.is_not_in_path_add_local_bin_to_path", name,
	)
}

func agentVersion(program *process.AgentProgram) string {
	if program == nil {
		return versionFailedText()
	}
	inv, err := process.NewProcessInvocation(*program, []string{"--version"}, nil)
	if err != nil {
		return versionFailedText()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var env []string
	if inv.Env != nil {
		for k, v := range inv.Env {
			env = append(env, k+"="+v)
		}
	}
	result, err := probe.CaptureWithEnv(ctx, inv.Argv[0], inv.Argv[1:], env)
	if err != nil || result.Code != 0 {
		return versionFailedText()
	}
	out := result.Stdout + result.Stderr
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return versionFailedText()
	}
	return lines[0]
}

func agentUsable(state agentState) bool {
	failed := versionFailedText()
	return state.Path != "" && state.Version != "" && state.Version != failed
}

func findAgents(configs ...*config.Config) map[string]agentState {
	var cfg *config.Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	reviewSet := map[string]struct{}{}
	for _, name := range config.ReviewAgentNames(cfg) {
		reviewSet[name] = struct{}{}
	}
	// Each probe runs a `--version` subprocess with its own timeout, so the
	// agents are probed concurrently: one slow CLI no longer serializes the
	// wait. The fan-out is bounded by config.AgentNames, and each goroutine
	// writes only its own slot, so no mutex is needed.
	names := config.AgentNames(cfg)
	states := make([]agentState, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			states[i] = probeAgent(cfg, name, reviewSet)
		})
	}
	wg.Wait()
	agents := make(map[string]agentState, len(names))
	for i, name := range names {
		agents[name] = states[i]
	}
	return agents
}

// probeAgent resolves one agent's executables and versions it. It only reads
// cfg, so callers may run it concurrently.
func probeAgent(cfg *config.Config, name string, reviewSet map[string]struct{}) agentState {
	executable := config.AgentPath(cfg, name)
	program := process.ResolveAgentProgram(executable)
	state := agentState{
		Execution: true,
		Review:    false,
	}
	if _, ok := reviewSet[name]; ok {
		state.Review = true
	}
	if program != nil {
		state.Path = program.Path
		state.Version = agentVersion(program)
		state.Batch = program.Batch
	}
	if state.Review {
		reviewPath := config.ReviewExecutable(cfg, name)
		if reviewPath != executable {
			reviewer := agentState{Review: true}
			if program := process.ResolveAgentProgram(reviewPath); program != nil {
				reviewer.Path = program.Path
				reviewer.Version = agentVersion(program)
				reviewer.Batch = program.Batch
			}
			state.reviewer = &reviewer
		}
	}
	return state
}

func reviewGatePresent() (bool, string) {
	if found := lookPath("kander"); found != "" {
		return true, found
	}
	return false, config.Text(
		"menu.kander_is_not_in_path_add_local_bin_to",
	)
}

// reviewerState keeps execution wrappers out of reviewer availability decisions.
func reviewerState(state agentState) agentState {
	if state.reviewer != nil {
		return *state.reviewer
	}
	return state
}
func reviewerUsable(state agentState) bool { return state.Review && agentUsable(reviewerState(state)) }
