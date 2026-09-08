package launch

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

func allocateAgentSession(definition *config.AgentSessionDefinition) (string, error) {
	program := resolveAgent(definition.Allocate[0])
	if program == nil {
		return "", launchError("launch.agent_is_not_in_path", definition.Allocate[0])
	}
	inv, err := launchInvocation(LaunchPlan{Launcher: "foreground"}, *program, definition.Allocate[1:])
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := probe.CaptureWithEnv(ctx, inv.Argv[0], inv.Argv[1:], envSlice(inv.Env))
	if err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", launchError("launch.agent_allocate_failed", result.Code)
	}
	id := strings.TrimSpace(result.Stdout)
	if definition.JSONField != "" {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(id), &obj); err != nil {
			return "", launchError("launch.agent_allocate_invalid")
		}
		if err := json.Unmarshal(obj[definition.JSONField], &id); err != nil {
			return "", launchError("launch.agent_allocate_invalid")
		}
	}
	if !sessionReferenceRe.MatchString(id) {
		return "", launchError("launch.agent_allocate_invalid")
	}
	return id, nil
}
