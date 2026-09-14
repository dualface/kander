package terminalcheck

import (
	"fmt"
	"strings"

	"github.com/dualface/kander/internal/terminal"
)

func (s step) execute(c *checker) (string, error) {
	supported, err := supports(c.backend.Capabilities(), s.capability)
	if err != nil {
		return "", err
	}
	return s.check(c, supported)
}

// supports evaluates the inventory's OR-of-AND capability requirements.
// An empty requirement is unconditional; an unknown name is a checker error.
func supports(c terminal.Capabilities, expression string) (bool, error) {
	if expression == "" {
		return true, nil
	}
	flags := map[string]bool{
		"Container": c.Container, "Focus": c.Focus,
		"PaneMetadata": c.PaneMetadata, "ForegroundProcess": c.ForegroundProcess,
		"AgentIdentity": c.AgentIdentity, "SessionReport": c.SessionReport,
		"WaitOutput": c.WaitOutput,
	}
	any := false
	for _, alternative := range strings.Split(expression, "|") {
		all := true
		for _, name := range strings.Split(alternative, "&") {
			enabled, ok := flags[name]
			if !ok {
				return false, fmt.Errorf("unknown conformance capability %q", name)
			}
			all = all && enabled
		}
		any = any || all
	}
	return any, nil
}
