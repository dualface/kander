package launch

import (
	"github.com/dualface/kander/internal/config"
)

// codexRolloutHook is the session mode whose sessions adoption can find again:
// a rollout names its task, its launch directory and its creation time.
const codexRolloutHook = "hook:codex-rollout"

// markerRepairable reports from static facts alone whether adoption can write
// the session marker a launch left out: the card names the session, or the
// agent's sessions can be found again. It runs no discovery and writes nothing,
// so recovery hints may call it. A configuration that cannot be read keeps the
// refusal.
func markerRepairable(session string) bool {
	parsed := parseSessionValue(session)
	if parsed == nil {
		return false
	}
	if parsed.Reference != "" {
		return true
	}
	cfg, err := loadEffective()
	if err != nil {
		return false
	}
	definition := config.AgentFor(cfg, parsed.Agent)
	return definition.Session != nil && definition.Session.Mode == codexRolloutHook
}
