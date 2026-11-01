package menu

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
)

// InstallRules returns the rules-injection switches of the scope buffer.
func (s *Session) InstallRules() config.InstallRules {
	return config.InstallRulesOf(s.scopeBuffer())
}

// InstallRulesAgents lists the agents the options page shows a switch for: the agents whose
// rules file or rules extension the scope covers, in the embedded agent order.
func (s *Session) InstallRulesAgents() []string {
	paths, err := currentPaths()
	if err != nil {
		return nil
	}
	covered := append(install.IntegrationAgents(paths), install.ExtensionAgents(paths)...)
	var out []string
	for _, agent := range config.ExecutionAgents {
		if slices.Contains(covered, agent) {
			out = append(out, agent)
		}
	}
	return out
}

// SetInstallRules turns the global rules-injection switch on or off. Only the scope config
// file holds it, so the Project tab never edits it.
func (s *Session) SetInstallRules(on bool) error {
	if s.EditingOverlay() {
		return errors.New(config.Text("menu.install_rules_scope_only"))
	}
	if s.Config.InstallRules == on {
		return nil
	}
	s.Config.InstallRules = on
	s.ScopeDirty = true
	s.syncScopeRaw([]string{"install_rules"}, on)
	return nil
}

// SetAgentInstallRules sets one built-in agent's switch. A value equal to the global switch
// removes the agent's own value, so the agent follows later global changes again.
func (s *Session) SetAgentInstallRules(agent string, on bool) error {
	if s.EditingOverlay() {
		return errors.New(config.Text("menu.install_rules_scope_only"))
	}
	if !slices.Contains(config.ExecutionAgents, agent) {
		return fmt.Errorf("%s", config.Text("config.agent_install_rules_builtin_only"))
	}
	if s.InstallRules().Enabled(agent) == on {
		return nil
	}
	path := []string{"agents", agent, "install_rules"}
	if on == s.Config.InstallRules {
		config.SetAgentInstallRules(s.Config, agent, nil)
		config.OverlayDelete(s.scopeRaw, path...)
	} else {
		config.SetAgentInstallRules(s.Config, agent, &on)
		s.syncScopeRaw(path, on)
	}
	s.ScopeDirty = true
	return nil
}
