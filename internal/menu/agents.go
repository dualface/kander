package menu

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

type agentState struct {
	Path      string
	Version   string
	Batch     bool
	Execution bool
	Review    bool
}

func versionFailedText() string {
	return config.Text("menu.version_check_failed")
}

func agentLabels() map[string]string {
	return map[string]string{
		"codex":  "Codex",
		"claude": "Claude",
		"grok":   "Grok",
		"cursor": "Cursor",
	}
}

func currentPaths() (config.InstallPaths, error) {
	return config.CurrentInstallPaths()
}

func rulesEntry(paths config.InstallPaths) string {
	return filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
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

func findCommands(paths config.InstallPaths) map[string]string {
	found := map[string]string{}
	if paths.Mode == config.ModeProject {
		command := projectCommandPath(paths.BinDir, "kander")
		found["kander"] = command
		return found
	}
	found["kander"] = lookPath("kander")
	return found
}

func projectCommandPath(binDir, name string) string {
	candidates := []string{name}
	if isWindowsOS() {
		candidates = []string{name + ".exe", name}
	}
	for _, candidate := range candidates {
		path := filepath.Join(binDir, candidate)
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func commandMissingMessage(name string, paths config.InstallPaths) string {
	if paths.Mode == config.ModeProject {
		expected := filepath.Join(paths.BinDir, name)
		if isWindowsOS() && name == "kander" {
			expected = filepath.Join(paths.BinDir, name+".exe")
		}
		return config.Text(
			"menu.is_not_in_the_project_command_root", name, expected,
		)
	}
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
	cmd := exec.CommandContext(ctx, inv.Argv[0], inv.Argv[1:]...)
	if inv.Env != nil {
		env := make([]string, 0, len(inv.Env))
		for k, v := range inv.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return versionFailedText()
	}
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

func findAgents() map[string]agentState {
	agents := map[string]agentState{}
	reviewSet := map[string]struct{}{}
	for _, name := range config.ReviewAgents {
		reviewSet[name] = struct{}{}
	}
	for _, name := range config.ExecutionAgents {
		executable := config.AgentExecutableName(name)
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
		agents[name] = state
	}
	return agents
}

func reviewGatePresent(paths config.InstallPaths) (bool, string) {
	if paths.Mode == config.ModeProject {
		command := projectCommandPath(paths.BinDir, "kander")
		if command != "" {
			return true, command
		}
		expected := filepath.Join(paths.BinDir, "kander")
		if isWindowsOS() {
			expected = filepath.Join(paths.BinDir, "kander.exe")
		}
		return false, config.Text("menu.review_entrypoint_is_missing", expected)
	}
	if found := lookPath("kander"); found != "" {
		return true, found
	}
	return false, config.Text(
		"menu.kander_is_not_in_path_add_local_bin_to",
	)
}
