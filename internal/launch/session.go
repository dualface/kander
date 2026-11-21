package launch

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

var sessionReferenceRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func randomUUID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return hex.EncodeToString(buf[:])
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	h := hex.EncodeToString(buf[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// newAgentSession gives a new launch its session: the id the provider allocates
// before the agent starts, or an empty reference the launch fills in later.
func newAgentSession(agent string, program *process.AgentProgram, configs ...*config.Config) (AgentSession, error) {
	var cfg *config.Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	definition := config.AgentFor(cfg, agent)
	provider, err := sessionProviderFor(cfg, agent, definition.Session)
	if err != nil {
		return AgentSession{}, err
	}
	id, err := allocateSession(provider, sessionLaunch{Program: program, Definition: definition.Session})
	return AgentSession{Agent: agent, Reference: id}, err
}

func parseTaskSession(text string) *AgentSession {
	return parseSessionValue(metadataFrom(text, sessionField))
}

// parseSessionValue parses a SESSION value; it returns nil when the value is
// empty or malformed.
func parseSessionValue(value string) *AgentSession {
	if value == "" {
		return nil
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return nil
	}
	agent := parts[0]
	reference := ""
	if len(parts) > 1 {
		reference = parts[1]
	}
	if !config.ValidAgentName(agent) || len(parts) > 2 || (reference != "" && !sessionReferenceRe.MatchString(reference)) {
		return nil
	}
	return &AgentSession{Agent: agent, Reference: reference}
}

func sessionFrom(text string) (AgentSession, error) {
	value := metadataFrom(text, sessionField)
	if value == "" {
		return AgentSession{}, launchError(
			"launch.task_has_no_metadata_only_tasks_launched_by_kander", sessionField,
		)
	}
	session := parseTaskSession(text)
	if session == nil {
		return AgentSession{}, launchError("launch.task_session_metadata_is_invalid", value)
	}
	return *session, nil
}

func agentArguments(agent string, model map[string]string, kind string, session AgentSession, resume bool, configs ...*config.Config) ([]string, error) {
	var cfg *config.Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	scale := "small"
	if kind == "large" {
		scale = "large"
	}
	// The model is picked per task scale; an empty scale model falls back to the shared "model" key of legacy configs.
	modelID := config.KanbanModelFor(model, scale)
	return expandAgentInvocation(agent, modelID, model[scale+"_effort"], session, resume, cfg)
}

func expandAgentInvocation(agent, modelID, effort string, session AgentSession, resume bool, cfg *config.Config) ([]string, error) {
	definition := config.AgentFor(cfg, agent)
	provider, err := sessionProviderFor(cfg, agent, definition.Session)
	if err != nil {
		return nil, err
	}
	if resume && !provider.Resumable() {
		return nil, config.AgentResumeError(agent)
	}
	if definition.Args == nil {
		return nil, launchError("launch.unsupported_agent", agent)
	}
	template := definition.Args.Start
	if resume {
		template = definition.Args.Resume
	}
	reference := session.Reference
	if !provider.Resumable() {
		reference = ""
		template = config.RewriteKeepSession(template, true)
	}
	return config.ExpandAgentArgs(template, modelID, effort, reference), nil
}

func requireAgentProgram(agentName string, configs ...*config.Config) (*process.AgentProgram, error) {
	var cfg *config.Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	if cfg != nil && !config.HasAgent(cfg, agentName) {
		return nil, unavailableAgentError(cfg, agentName)
	}
	executable := config.AgentPath(cfg, agentName)
	program := resolveAgent(executable)
	if program != nil {
		return program, nil
	}
	return nil, launchError("launch.agent_is_not_in_path", executable)
}

func itoa(n int) string { return strconv.Itoa(n) }
