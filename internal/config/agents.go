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

// AgentDefinition is the user overlay of an execution agent. Built-in review
// isolation stays on the embedded path for built-in names; custom agents may
// declare args.review and review.* to act as reviewers. The map key in
// Config.Agents is the stable agent name stored on task cards.
type AgentDefinition struct {
	SchemaVersion  int                     `json:"schema_version,omitempty"`
	Path           string                  `json:"path,omitempty"`
	ProcessName    string                  `json:"process_name,omitempty"`
	Dialect        string                  `json:"dialect,omitempty"`
	Args           *AgentArgs              `json:"args,omitempty"`
	Session        *AgentSessionDefinition `json:"session,omitempty"`
	PromptDelivery *PromptDelivery         `json:"prompt_delivery,omitempty"`
	Review         *AgentReview            `json:"review,omitempty"`
	ExitCommand    *string                 `json:"exit_command,omitempty"`
}

type AgentArgs struct {
	Start  []string `json:"start"`
	Resume []string `json:"resume"`
	Review []string `json:"review,omitempty"`
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

// AgentFor returns a detached definition with defaults resolved. Session
// mode and exit_command come from the named overlay, then the dialect's
// embedded definition, then generated / unset.
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
	overlay := d
	allowReviewInherit := contains(ExecutionAgents, name) || userDeclaredReviewTemplate(overlay)
	if d.Args == nil {
		if emb, ok := embeddedByName(d.Dialect); ok {
			d.Args = cloneArgs(&emb.Args)
			if !allowReviewInherit && d.Args != nil {
				d.Args.Review = nil
			}
		}
	} else if d.Args.Review == nil && allowReviewInherit {
		if emb, ok := embeddedByName(d.Dialect); ok && emb.Args.Review != nil {
			d.Args.Review = append([]string{}, emb.Args.Review...)
		}
	}
	if d.Review == nil && allowReviewInherit {
		if emb, ok := embeddedByName(d.Dialect); ok {
			d.Review = cloneReview(&emb.Review)
		}
	}
	if d.PromptDelivery == nil {
		if emb, ok := embeddedByName(d.Dialect); ok {
			pd := emb.PromptDelivery
			d.PromptDelivery = clonePromptDelivery(&pd)
		} else {
			d.PromptDelivery = &PromptDelivery{Mode: "argv"}
		}
	}
	if d.Session == nil {
		if emb, ok := embeddedByName(d.Dialect); ok {
			d.Session = cloneSession(&emb.Session)
		} else {
			d.Session = &AgentSessionDefinition{Mode: "generated"}
		}
	}
	if d.ExitCommand == nil {
		if emb, ok := embeddedByName(d.Dialect); ok {
			cmd := emb.ExitCommand
			d.ExitCommand = &cmd
		}
	}
	return d
}

func AgentPath(cfg *Config, name string) string        { return AgentFor(cfg, name).Path }
func AgentProcessName(cfg *Config, name string) string { return AgentFor(cfg, name).ProcessName }

// LoadAgent resolves current runtime settings without caching machine-local state.
// A missing or invalid config is an error; it does not fall back to Effective defaults.
func LoadAgent(name string) (AgentDefinition, error) {
	cfg, err := Load(false)
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
		if a.Review != nil {
			a.Review = append([]string{}, a.Review...)
		}
		d.Args = &a
	}
	d.Review = cloneReview(d.Review)
	d.Session = cloneSession(d.Session)
	d.PromptDelivery = clonePromptDelivery(d.PromptDelivery)
	d.ExitCommand = cloneExitCommand(d.ExitCommand)
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

func validExitCommandText(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) < 0
}

func cloneSession(src *AgentSessionDefinition) *AgentSessionDefinition {
	if src == nil {
		return nil
	}
	out := *src
	out.Allocate = append([]string(nil), src.Allocate...)
	return &out
}

func cloneExitCommand(src *string) *string {
	if src == nil {
		return nil
	}
	v := *src
	return &v
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
		for _, key := range []string{"args", "session", "prompt_delivery", "review"} {
			if v, exists := fields[key]; exists {
				if _, ok := asObject(v); !ok {
					return nil, agentDefinitionError(name, Text("config.agent_object"))
				}
			}
		}
		if d.SchemaVersion != 0 && d.SchemaVersion != embeddedAgentSchemaVersion {
			return nil, agentDefinitionError(name, Text("config.agent_schema_version"))
		}
		if err := validatePromptDelivery(name, d.PromptDelivery); err != nil {
			return nil, err
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
		if v, exists := fields["exit_command"]; exists {
			s, ok := v.(string)
			if !ok || !validExitCommandText(s) {
				return nil, agentDefinitionError(name, Text("config.agent_exit_command"))
			}
		}
		if d.Session != nil {
			s := d.Session
			if hookName, isHook := ParseSessionHook(s.Mode); isHook {
				if _, ok := LookupSessionHook(s.Mode); !ok {
					return nil, agentDefinitionError(name, Text("config.agent_session_hook", name, hookName))
				}
			} else if !contains([]string{"generated", "allocated", "none"}, s.Mode) {
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
		if d.Args == nil && d.Session != nil && d.Session.Mode != "none" {
			inherited := ""
			if emb, ok := embeddedByName(resolved.Dialect); ok {
				inherited = emb.Session.Mode
			}
			// An allocation hook and an explicit allocator both supply the ID
			// consumed by the inherited resume arguments.
			compatibleAllocator := d.Session.Mode == "allocated" && SessionAllocatesBeforeStart(inherited)
			if inherited != "" && d.Session.Mode != inherited && !compatibleAllocator {
				if _, isHook := LookupSessionHook(inherited); isHook {
					return nil, agentDefinitionError(name, Text("config.agent_session_dialect", resolved.Dialect, d.Session.Mode))
				}
			}
		}
		if d.Args != nil && d.Dialect == "" && d.Session == nil && !contains(ExecutionAgents, name) {
			return nil, agentDefinitionError(name, Text("config.agent_session_required"))
		}
		if d.Args != nil && resolved.Session.Mode != "none" && d.Args.Resume == nil {
			return nil, agentDefinitionError(name, Text("config.agent_resume"))
		}
		if err := validateReviewDefinition(name, d); err != nil {
			return nil, err
		}
		out[name] = d
	}
	return out, nil
}

func invalidPlaceholder(arg string) bool {
	rest := strings.NewReplacer("{model}", "", "{effort}", "", "{session=}", "", "{session}", "").Replace(arg)
	return strings.ContainsAny(rest, "{}")
}

// ExpandAgentArgs replaces text within individual argv elements, never shell text.
// An empty {model}, {effort}, or {session} removes its element and its immediately
// preceding literal flag. {session=} always substitutes, including an empty value,
// so a session flag can stay present when the id is not yet known.
func ExpandAgentArgs(template []string, model, effort, session string) []string {
	values := map[string]string{"{model}": model, "{effort}": effort, "{session}": session}
	var out []string
	for i, arg := range template {
		omit := false
		keepEmptySession := strings.Contains(arg, "{session=}")
		for key, value := range values {
			if value == "" && strings.Contains(arg, key) {
				if key == "{session}" && keepEmptySession {
					continue
				}
				omit = true
			}
		}
		if omit {
			if i > 0 && strings.HasPrefix(template[i-1], "-") && !strings.ContainsAny(template[i-1], "{}=") && len(out) > 0 && out[len(out)-1] == template[i-1] {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, strings.NewReplacer("{session=}", session, "{model}", model, "{effort}", effort, "{session}", session).Replace(arg))
	}
	return out
}

func RewriteKeepSession(template []string, dropEmpty bool) []string {
	if !dropEmpty {
		return template
	}
	out := append([]string{}, template...)
	for i, arg := range out {
		out[i] = strings.ReplaceAll(arg, "{session=}", "{session}")
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

func customModelDefaults(definitions map[string]AgentDefinition, models *Models) {
	for name, d := range definitions {
		if contains(ExecutionAgents, name) {
			if !embeddedSupportsEffort(name) && (d.Args != nil || d.Dialect != "" && d.Dialect != name) {
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
		if emb, ok := embeddedByName(d.Dialect); ok && d.Args == nil {
			fields = cloneStringMap(emb.kanbanFields())
		}
		models.Kanban[name] = fields
		if HasReviewTemplate(&Config{Agents: definitions}, name) {
			if _, ok := models.Review[name]; !ok {
				reviewFields := map[string]string{"model": "", "effort": "high"}
				if emb, ok := embeddedByName(d.Dialect); ok {
					reviewFields = cloneStringMap(emb.reviewFields())
				}
				models.Review[name] = reviewFields
			}
		}
	}
}

// AgentResumeError explains why a non-resumable agent cannot accept resume.
func AgentResumeError(name string) error { return configErrorf("config.agent_no_resume", name) }
