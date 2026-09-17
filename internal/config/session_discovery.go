package config

import (
	"regexp"
	"strings"
	"time"
)

// SessionDiscovery declares how an agent CLI enumerates its own sessions, so a
// launch can identify the session it created without a Go hook. The enumerate
// command is the agent program plus Args, spawned directly with no shell.
type SessionDiscovery struct {
	Args       []string         `json:"args"`
	Format     string           `json:"format"`
	IDField    string           `json:"id_field,omitempty"`
	Match      []DiscoveryMatch `json:"match,omitempty"`
	TimeoutMS  int              `json:"timeout_ms,omitempty"`
	IntervalMS int              `json:"interval_ms,omitempty"`
	MaxBytes   int64            `json:"max_bytes,omitempty"`
}

// DiscoveryMatch filters enumerated records: Field must be a string equal to
// Equals after placeholder expansion. Supported placeholders are {cwd} for the
// launch project directory and {task_id} for the card's task ID.
type DiscoveryMatch struct {
	Field  string `json:"field"`
	Equals string `json:"equals"`
}

const (
	sessionDiscoveryDefaultTimeoutMS  = 10000
	sessionDiscoveryDefaultIntervalMS = 100
	sessionDiscoveryDefaultMaxBytes   = 1 << 20

	sessionDiscoveryMinTimeoutMS  = 1000
	sessionDiscoveryMaxTimeoutMS  = 120000
	sessionDiscoveryMinIntervalMS = 20
	sessionDiscoveryMaxIntervalMS = 10000
	sessionDiscoveryMinMaxBytes   = 4096
	sessionDiscoveryMaxMaxBytes   = 16 << 20
)

var sessionFieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]{0,63}$`)

// Timeout returns the total discovery budget after a launch.
func (d *SessionDiscovery) Timeout() time.Duration {
	if d.TimeoutMS <= 0 {
		return time.Duration(sessionDiscoveryDefaultTimeoutMS) * time.Millisecond
	}
	return time.Duration(d.TimeoutMS) * time.Millisecond
}

// Interval returns the pause between two enumerations.
func (d *SessionDiscovery) Interval() time.Duration {
	if d.IntervalMS <= 0 {
		return time.Duration(sessionDiscoveryDefaultIntervalMS) * time.Millisecond
	}
	return time.Duration(d.IntervalMS) * time.Millisecond
}

// OutputLimit returns the maximum accepted enumerate output in bytes.
func (d *SessionDiscovery) OutputLimit() int64 {
	if d.MaxBytes <= 0 {
		return sessionDiscoveryDefaultMaxBytes
	}
	return d.MaxBytes
}

func validDiscoveryPlaceholder(arg string) bool {
	rest := strings.NewReplacer("{cwd}", "", "{task_id}", "").Replace(arg)
	return !strings.ContainsAny(rest, "{}")
}

// validateSessionDiscovery checks the session mode/discovery combination of one
// agent definition, shared by embedded files and user overlays.
func validateSessionDiscovery(name string, s *AgentSessionDefinition) error {
	if s == nil {
		return nil
	}
	if s.Mode != "discovered" {
		if s.Discovery != nil {
			return agentDefinitionError(name, Text("config.agent_discovery_only"))
		}
		return nil
	}
	d := s.Discovery
	if d == nil {
		return agentDefinitionError(name, Text("config.agent_discovery"))
	}
	if len(d.Args) == 0 {
		return agentDefinitionError(name, Text("config.agent_discovery"))
	}
	// Args are argv elements for the enumerate command; no placeholder is
	// expanded there, so braces are rejected outright.
	for _, arg := range d.Args {
		if !validAgentText(arg) || strings.ContainsAny(arg, "{}") {
			return agentDefinitionError(name, Text("config.agent_discovery"))
		}
	}
	switch d.Format {
	case "json", "jsonl":
		if !sessionFieldName.MatchString(d.IDField) {
			return agentDefinitionError(name, Text("config.agent_discovery"))
		}
	case "lines":
		if d.IDField != "" || len(d.Match) != 0 {
			return agentDefinitionError(name, Text("config.agent_discovery"))
		}
	default:
		return agentDefinitionError(name, Text("config.agent_discovery"))
	}
	for _, m := range d.Match {
		if !sessionFieldName.MatchString(m.Field) || !validAgentText(m.Equals) || !validDiscoveryPlaceholder(m.Equals) {
			return agentDefinitionError(name, Text("config.agent_discovery"))
		}
	}
	if d.TimeoutMS != 0 && (d.TimeoutMS < sessionDiscoveryMinTimeoutMS || d.TimeoutMS > sessionDiscoveryMaxTimeoutMS) {
		return agentDefinitionError(name, Text("config.agent_discovery"))
	}
	if d.IntervalMS != 0 && (d.IntervalMS < sessionDiscoveryMinIntervalMS || d.IntervalMS > sessionDiscoveryMaxIntervalMS || d.IntervalMS >= int(d.Timeout()/time.Millisecond)) {
		return agentDefinitionError(name, Text("config.agent_discovery"))
	}
	if d.MaxBytes != 0 && (d.MaxBytes < sessionDiscoveryMinMaxBytes || d.MaxBytes > sessionDiscoveryMaxMaxBytes) {
		return agentDefinitionError(name, Text("config.agent_discovery"))
	}
	return nil
}

func cloneDiscovery(src *SessionDiscovery) *SessionDiscovery {
	if src == nil {
		return nil
	}
	out := *src
	out.Args = append([]string(nil), src.Args...)
	out.Match = append([]DiscoveryMatch(nil), src.Match...)
	return &out
}
