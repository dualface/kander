package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// agentConfigRoot resolves the global config directory of one embedded agent:
// the scope config's agents.<name>.config_dir first, then the environment
// variables the agent itself reads, then the embedded default below the home
// directory. relocated reports that the result did not come from that default,
// so callers cannot trust the home directory as the anchor of its path chain.
func agentConfigRoot(agent string, paths config.InstallPaths) (root string, relocated bool, err error) {
	spec, ok := config.RulesSpec(agent)
	if !ok {
		return "", false, nil
	}
	configured, err := config.ScopeAgentConfigDir(paths, agent)
	if err != nil {
		return "", false, err
	}
	if configured != "" {
		root, err := config.ExpandConfigDir(configured)
		return root, true, err
	}
	for _, env := range spec.ConfigEnv {
		value := os.Getenv(env.Name)
		if value == "" || (env.AbsoluteOnly && !filepath.IsAbs(value)) {
			continue
		}
		root, err := config.ExpandConfigDir(value)
		if err != nil {
			return "", false, err
		}
		return filepath.Join(root, filepath.FromSlash(env.Join)), true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("%s", config.Text("config.cannot_resolve_home_directory"))
	}
	return filepath.Join(home, filepath.FromSlash(spec.ConfigHome)), false, nil
}

// globalAgentTarget maps an embedded home-relative target, which always lives
// below the agent's default config directory, onto the resolved config
// directory, so the rules file and the extension move together.
func globalAgentTarget(agent, relative string, paths config.InstallPaths) (string, error) {
	spec, _ := config.RulesSpec(agent)
	root, _, err := agentConfigRoot(agent, paths)
	if err != nil || root == "" {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(relative, spec.ConfigHome+"/"))), nil
}
