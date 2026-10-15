package config

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// AgentConfigEnv is one environment variable an agent itself reads to relocate
// its global config directory.
type AgentConfigEnv struct {
	Name string `json:"name"`
	// Join is the subdirectory below the variable's directory, such as
	// "opencode" under XDG_CONFIG_HOME; empty means the variable names the
	// config directory itself.
	Join string `json:"join,omitempty"`
	// AbsoluteOnly ignores relative values, as the XDG base directory
	// specification requires.
	AbsoluteOnly bool `json:"absolute_only,omitempty"`
}

// agentConfigHome is the embedded config_home declaration: the default global
// config directory relative to the home directory and the environment sources
// that relocate it, highest precedence first.
type agentConfigHome struct {
	Default string           `json:"default"`
	Env     []AgentConfigEnv `json:"env"`
}

var envNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// validateConfigHome requires every global install target to live below the
// declared default directory, so relocating that directory moves the rules file
// and the extension together.
func validateConfigHome(fileName string, agent embeddedAgent) error {
	home := agent.ConfigHome.Default
	if !cleanRelativeSlashPath(home) {
		return embedAgentError(fileName, "config_home.default")
	}
	for _, env := range agent.ConfigHome.Env {
		if !envNamePattern.MatchString(env.Name) {
			return embedAgentError(fileName, "config_home.env.name")
		}
		if env.Join != "" && !cleanRelativeSlashPath(env.Join) {
			return embedAgentError(fileName, "config_home.env.join")
		}
	}
	if global := agent.RulesTarget.Global; global != "" && !strings.HasPrefix(global, home+"/") {
		return embedAgentError(fileName, "rules_target.global")
	}
	if ext := agent.RulesExtension; ext != nil && !strings.HasPrefix(ext.Global, home+"/") {
		return embedAgentError(fileName, "rules_extension.global")
	}
	return nil
}

func cleanRelativeSlashPath(p string) bool {
	return validAgentText(p) && !strings.Contains(p, `\`) && !path.IsAbs(p) &&
		path.Clean(p) == p && p != "." && p != ".." && !strings.HasPrefix(p, "../")
}

// validConfigDirValue accepts an absolute path or a ~ / ~/ path; a relative
// value would depend on the directory kander happens to run from.
func validConfigDirValue(value string) bool {
	if !validAgentText(value) {
		return false
	}
	return value == "~" || strings.HasPrefix(value, "~/") || filepath.IsAbs(value)
}

// ExpandConfigDir expands a leading ~ or ~/ to the home directory and resolves
// any other relative value from the current working directory.
func ExpandConfigDir(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", configErrorfWrap(err, "config.cannot_resolve_home_directory")
		}
		value = home + value[1:]
	}
	return filepath.Abs(value)
}

// ScopeAgentConfigDir returns agents.<name>.config_dir from the scope config
// file of paths (KANDER_CONFIG when set). A project overlay is never read: it
// ships with a repository and must not redirect writes to the user's global
// agent files. Paths without a config file yield no override.
func ScopeAgentConfigDir(paths InstallPaths, name string) (string, error) {
	file := paths.ConfigPath
	if override := os.Getenv(EnvConfig); override != "" {
		file = expandUser(override)
	}
	if file == "" {
		return "", nil
	}
	cfg, _, err := loadScopeRawAt(file, true)
	if err != nil {
		return "", err
	}
	return cfg.Agents[name].ConfigDir, nil
}

// OverlayAgentConfigDirs returns the project overlay path and the agents whose
// config_dir it declares; ScopeAgentConfigDir ignores those values.
func OverlayAgentConfigDirs() (string, []string, error) {
	file, raw, err := readOverlay("")
	if err != nil || raw == nil {
		return "", nil, err
	}
	agents, _ := asObject(raw["agents"])
	var names []string
	for name, value := range agents {
		fields, _ := asObject(value)
		if _, ok := fields["config_dir"]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return file, names, nil
}
