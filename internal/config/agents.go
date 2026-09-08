package config

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// AgentDefinition overrides an execution agent only; reviewers retain their own read-only adapters.
// The map key in Config.Agents is the stable agent name stored on task cards.
type AgentDefinition struct {
	Path        string                  `json:"path,omitempty"`
	ProcessName string                  `json:"process_name,omitempty"`
	Dialect     string                  `json:"dialect,omitempty"`
	Args        *AgentArgs              `json:"args,omitempty"`
	Session     *AgentSessionDefinition `json:"session,omitempty"`
}

type AgentArgs struct {
	Start  []string `json:"start"`
	Resume []string `json:"resume"`
}

// Allocate is an argv array including the executable. Output is a plain ID or a
// JSON object with the named top-level field. Both forms validate the resulting ID.
type AgentSessionDefinition struct {
	Mode      string   `json:"mode"`
	Allocate  []string `json:"allocate,omitempty"`
	JSONField string   `json:"json_field,omitempty"`
}

var agentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func ValidAgentName(name string) bool { return agentNamePattern.MatchString(name) }

func AgentNames(cfg *Config) []string {
	names := append([]string{}, ExecutionAgents...)
	var custom []string
	if cfg != nil {
		for name := range cfg.Agents {
			if !contains(names, name) {
				custom = append(custom, name)
			}
		}
	}
	sort.Strings(custom)
	return append(names, custom...)
}

func HasAgent(cfg *Config, name string) bool { return contains(AgentNames(cfg), name) }

// AgentFor returns a detached definition with defaults resolved. The discovered
// mode is internal to the Codex dialect and cannot be declared in JSON.
func AgentFor(cfg *Config, name string) AgentDefinition {
	var d AgentDefinition
	if cfg != nil {
		d = cloneAgent(cfg.Agents[name])
	}
	if d.Path == "" {
		d.Path = AgentExecutableName(name)
	}
	if d.ProcessName == "" {
		d.ProcessName = filepath.Base(d.Path)
	}
	if d.Dialect == "" && contains(ExecutionAgents, name) {
		d.Dialect = name
	}
	if d.Session == nil {
		mode := "generated"
		switch d.Dialect {
		case "codex", "kimi":
			mode = "discovered"
		case "cursor":
			mode = "allocated"
		}
		d.Session = &AgentSessionDefinition{Mode: mode}
		if d.Dialect == "cursor" {
			d.Session.Allocate = []string{d.Path, "create-chat"}
		}
	}
	return d
}

func AgentPath(cfg *Config, name string) string        { return AgentFor(cfg, name).Path }
func AgentProcessName(cfg *Config, name string) string { return AgentFor(cfg, name).ProcessName }

// LoadAgent resolves current runtime settings without caching machine-local state.
func LoadAgent(name string) (AgentDefinition, error) {
	cfg, err := Effective(nil)
	if err != nil {
		return AgentDefinition{}, err
	}
	if !HasAgent(cfg, name) {
		return AgentDefinition{}, choiceError("agent", strings.Join(AgentNames(cfg), ", "))
	}
	return AgentFor(cfg, name), nil
}

func cloneAgent(d AgentDefinition) AgentDefinition {
	if d.Args != nil {
		a := *d.Args
		a.Start = append([]string{}, a.Start...)
		a.Resume = append([]string{}, a.Resume...)
		d.Args = &a
	}
	if d.Session != nil {
		s := *d.Session
		s.Allocate = append([]string(nil), s.Allocate...)
		d.Session = &s
	}
	return d
}

func CloneAgents(src map[string]AgentDefinition) map[string]AgentDefinition {
	if src == nil {
		return nil
	}
	out := make(map[string]AgentDefinition, len(src))
	for name, d := range src {
		out[name] = cloneAgent(d)
	}
	return out
}

func agentDefinitionError(name, detail string) error {
	return configErrorf("config.agent_definition_invalid", name, detail)
}
func validAgentText(s string) bool {
	return strings.TrimSpace(s) != "" && strings.IndexFunc(s, unicode.IsControl) < 0
}

func validateAgentProgram(path string) bool {
	if !validAgentText(path) {
		return false
	}
	if strings.ContainsAny(path, `/\`) && !filepath.IsAbs(path) {
		return false
	}
	_, err := exec.LookPath(path)
	return err == nil
}

func validateAgentDefinitions(raw any) (map[string]AgentDefinition, error) {
	obj, ok := asObject(raw)
	if !ok {
		return nil, agentDefinitionError("agents", Text("config.agent_object"))
	}
	out := map[string]AgentDefinition{}
	for name, value := range obj {
		if !ValidAgentName(name) {
			return nil, agentDefinitionError(name, Text("config.agent_name"))
		}
		fields, ok := asObject(value)
		if !ok {
			return nil, agentDefinitionError(name, Text("config.agent_object"))
		}
		data, _ := json.Marshal(fields)
		var d AgentDefinition
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&d); err != nil {
			return nil, agentDefinitionError(name, err.Error())
		}
		for _, key := range []string{"path", "process_name", "dialect"} {
			if v, exists := fields[key]; exists {
				s, ok := v.(string)
				if !ok || !validAgentText(s) {
					return nil, agentDefinitionError(name, Text("config.agent_text", key))
				}
			}
		}
		for _, key := range []string{"args", "session"} {
			if v, exists := fields[key]; exists {
				if _, ok := asObject(v); !ok {
					return nil, agentDefinitionError(name, Text("config.agent_object"))
				}
			}
		}
		if d.Path != "" && !validateAgentProgram(d.Path) {
			return nil, agentDefinitionError(name, Text("config.agent_path", d.Path))
		}
		if d.Dialect != "" && !contains(ExecutionAgents, d.Dialect) {
			return nil, agentDefinitionError(name, Text("config.agent_dialect"))
		}
		if d.Dialect == "" && d.Args == nil && !contains(ExecutionAgents, name) {
			return nil, agentDefinitionError(name, Text("config.agent_adapter"))
		}
		if d.Args != nil {
			if d.Args.Start == nil {
				return nil, agentDefinitionError(name, Text("config.agent_start"))
			}
			for _, args := range [][]string{d.Args.Start, d.Args.Resume} {
				for _, arg := range args {
					if !validAgentText(arg) || invalidPlaceholder(arg) {
						return nil, agentDefinitionError(name, Text("config.agent_template"))
					}
				}
			}
		}
		if d.Session != nil {
			s := d.Session
			if !contains([]string{"generated", "allocated", "none"}, s.Mode) {
				return nil, agentDefinitionError(name, Text("config.agent_session"))
			}
			if s.Mode == "allocated" {
				if len(s.Allocate) == 0 || !validateAgentProgram(s.Allocate[0]) {
					return nil, agentDefinitionError(name, Text("config.agent_allocate"))
				}
				for _, arg := range s.Allocate {
					if !validAgentText(arg) {
						return nil, agentDefinitionError(name, Text("config.agent_allocate"))
					}
				}
				if s.JSONField != "" && !validAgentText(s.JSONField) {
					return nil, agentDefinitionError(name, Text("config.agent_allocate"))
				}
			} else if len(s.Allocate) != 0 || s.JSONField != "" {
				return nil, agentDefinitionError(name, Text("config.agent_allocate_only"))
			}
		}
		resolved := AgentFor(&Config{Agents: map[string]AgentDefinition{name: d}}, name)
		if d.Args == nil && d.Session != nil && d.Session.Mode != "none" &&
			(resolved.Dialect == "codex" || resolved.Dialect == "kimi" ||
				resolved.Dialect == "cursor" && d.Session.Mode != "allocated") {
			return nil, agentDefinitionError(name, Text("config.agent_session_dialect", resolved.Dialect, d.Session.Mode))
		}
		if d.Args != nil && d.Dialect == "" && d.Session == nil && !contains(ExecutionAgents, name) {
			return nil, agentDefinitionError(name, Text("config.agent_session_required"))
		}
		if d.Args != nil && resolved.Session.Mode != "none" && d.Args.Resume == nil {
			return nil, agentDefinitionError(name, Text("config.agent_resume"))
		}
		out[name] = d
	}
	return out, nil
}

func invalidPlaceholder(arg string) bool {
	rest := strings.NewReplacer("{model}", "", "{effort}", "", "{session}", "").Replace(arg)
	return strings.ContainsAny(rest, "{}")
}

// ExpandAgentArgs replaces text within individual argv elements, never shell text.
// An empty placeholder removes its element and its immediately preceding literal flag.
func ExpandAgentArgs(template []string, model, effort, session string) []string {
	values := map[string]string{"{model}": model, "{effort}": effort, "{session}": session}
	var out []string
	for i, arg := range template {
		omit := false
		for key, value := range values {
			if value == "" && strings.Contains(arg, key) {
				omit = true
			}
		}
		if omit {
			if i > 0 && strings.HasPrefix(template[i-1], "-") && !strings.ContainsAny(template[i-1], "{}=") && len(out) > 0 && out[len(out)-1] == template[i-1] {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, strings.NewReplacer("{model}", model, "{effort}", effort, "{session}", session).Replace(arg))
	}
	return out
}

// AgentWarnings makes intentional loss of resume/direct-notify support visible.
func AgentWarnings(cfg *Config) []string {
	var out []string
	for _, name := range AgentNames(cfg) {
		if AgentFor(cfg, name).Session.Mode == "none" {
			out = append(out, Text("config.agent_no_resume", name))
		}
	}
	return out
}

// AgentHasEffort reports whether a built-in agent's CLI takes a reasoning effort. The kanban
// defaults are that agent's configuration schema, so the presence of the effort keys is the
// single source of truth: cursor and kimi have no effort switch and therefore no keys.
func AgentHasEffort(agent string) bool {
	_, ok := kanbanModelDefaults[agent]["large_effort"]
	return ok
}

// ReviewAgentHasEffort is the reviewer-side counterpart of AgentHasEffort.
func ReviewAgentHasEffort(agent string) bool {
	_, ok := reviewModelDefaults[agent]["effort"]
	return ok
}

func customModelDefaults(definitions map[string]AgentDefinition, models *Models) {
	for name, d := range definitions {
		if contains(ExecutionAgents, name) {
			// An effortless built-in regains the generic effort fields once it is driven by a
			// template or another dialect, because it is no longer its own CLI that runs.
			if !AgentHasEffort(name) && (d.Args != nil || d.Dialect != "" && d.Dialect != name) {
				for _, key := range []string{"model", "large_effort", "small_effort"} {
					value := ""
					if key != "model" {
						value = "medium"
					}
					models.Kanban[name][key] = value
				}
			}
			continue
		}
		fields := map[string]string{"model": "", "large_model": "", "small_model": "", "large_effort": "medium", "small_effort": "medium"}
		if defaults := kanbanModelDefaults[d.Dialect]; defaults != nil && d.Args == nil {
			fields = cloneStringMap(defaults)
		}
		models.Kanban[name] = fields
	}
}

// AgentResumeError explains why a non-resumable agent cannot accept resume.
func AgentResumeError(name string) error { return configErrorf("config.agent_no_resume", name) }
