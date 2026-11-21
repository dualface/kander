package config

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
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
	ProgramStatus  *ProgramStatus          `json:"program_status,omitempty"`
	// ConfigDir relocates a built-in agent's global config directory for rules
	// installation; only the scope config file's value takes effect.
	ConfigDir string `json:"config_dir,omitempty"`
	// InstallRules overrides the global install_rules switch for one built-in
	// agent; nil follows it. Only the scope config file's value takes effect.
	InstallRules *bool `json:"install_rules,omitempty"`
}

type AgentArgs struct {
	Start  []string `json:"start"`
	Resume []string `json:"resume"`
	Review []string `json:"review,omitempty"`
}

// Allocate is an argv array including the executable. Output is a plain ID or a
// JSON object with the named top-level field. Both forms validate the resulting ID.
// Discovery is valid only for mode "discovered" and declares how the agent CLI
// enumerates its sessions after launch.
// File optionally declares the on-disk session file format, so a path-kind
// session identity reported by a terminal resolves to the session id inside
// the file. Agents without it keep path identities undecidable.
type AgentSessionDefinition struct {
	Mode      string            `json:"mode"`
	Allocate  []string          `json:"allocate,omitempty"`
	JSONField string            `json:"json_field,omitempty"`
	Discovery *SessionDiscovery `json:"discovery,omitempty"`
	File      *SessionFile      `json:"file,omitempty"`
}

// SessionFile declares the format of the on-disk session file an agent
// writes, letting a reported path-kind session identity resolve to the real
// session id inside it.
type SessionFile struct {
	// Format is the file layout; "jsonl_header" reads the first JSON line of
	// the file as the session header.
	Format string `json:"format"`
	// IDField is the top-level header field whose string value is the
	// session id.
	IDField string `json:"id_field"`
	// TypeField and TypeValue pin an optional header discriminator; a header
	// whose field does not equal the value is not a session record. Both
	// must be set or neither.
	TypeField string `json:"type_field,omitempty"`
	TypeValue string `json:"type_value,omitempty"`
	// MaxBytes bounds the header read; 0 means DefaultSessionFileMaxBytes.
	MaxBytes int `json:"max_bytes,omitempty"`
}

const (
	// DefaultSessionFileMaxBytes bounds a session header read when the
	// declaration omits max_bytes.
	DefaultSessionFileMaxBytes = 8 * 1024
	// MaxSessionFileMaxBytes caps the declared bound of a session header read.
	MaxSessionFileMaxBytes = 1024 * 1024
)

// SessionFileFormats are the file layouts a session.file declaration may
// name; "jsonl_header" reads the first JSON line as the session header.
var SessionFileFormats = []string{"jsonl_header"}

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
		// An unusable entry also takes its built-in namesake out of service: running
		// the embedded definition would silently ignore what the user declared.
		names = slices.DeleteFunc(names, func(name string) bool { _, bad := cfg.UnusableAgents[name]; return bad })
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
			d.Review = cloneReview(&emb.Review.AgentReview)
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
	} else if d.Session.File == nil {
		// An overlay session that keeps the embedded mode also inherits the
		// embedded file declaration; a different mode opts out, and an
		// explicit file always wins. Inheritance follows the dialect's
		// embedded definition so wrappers driving the same CLI resolve the
		// same on-disk session format.
		if emb, ok := embeddedByName(d.Dialect); ok && emb.Session.Mode == d.Session.Mode && emb.Session.File != nil {
			file := *emb.Session.File
			d.Session.File = &file
		}
	}
	if d.ExitCommand == nil {
		if emb, ok := embeddedByName(d.Dialect); ok {
			cmd := emb.ExitCommand
			d.ExitCommand = &cmd
		}
	}
	if d.ProgramStatus == nil {
		if emb, ok := embeddedByName(d.Dialect); ok {
			d.ProgramStatus = cloneProgramStatus(emb.ProgramStatus)
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
		return AgentDefinition{}, UnavailableAgentError(cfg, name)
	}
	return AgentFor(cfg, name), nil
}

func cloneAgent(d AgentDefinition) AgentDefinition {
	if d.Args != nil {
		a := *d.Args
		a.Start = slices.Clone(a.Start)
		a.Resume = slices.Clone(a.Resume)
		a.Review = slices.Clone(a.Review)
		d.Args = &a
	}
	d.Review = cloneReview(d.Review)
	d.Session = cloneSession(d.Session)
	d.PromptDelivery = clonePromptDelivery(d.PromptDelivery)
	d.ExitCommand = cloneExitCommand(d.ExitCommand)
	d.ProgramStatus = cloneProgramStatus(d.ProgramStatus)
	if d.InstallRules != nil {
		v := *d.InstallRules
		d.InstallRules = &v
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

func validExitCommandText(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) < 0
}

func cloneSession(src *AgentSessionDefinition) *AgentSessionDefinition {
	if src == nil {
		return nil
	}
	out := *src
	out.Allocate = append([]string(nil), src.Allocate...)
	out.Discovery = cloneDiscovery(src.Discovery)
	if src.File != nil {
		file := *src.File
		out.File = &file
	}
	return &out
}

func cloneExitCommand(src *string) *string {
	if src == nil {
		return nil
	}
	v := *src
	return &v
}

// validateAgentProgram checks only the form of a program value. It never
// probes the file system: a program that does not resolve leaves the
// configuration valid and is reported by the step that runs it, as a missing
// built-in CLI is.
func validateAgentProgram(path string) bool {
	if !validAgentText(path) {
		return false
	}
	return !strings.ContainsAny(path, `/\`) || filepath.IsAbs(path)
}

// validateAgentDefinitions validates the agents section. An entry this version
// cannot turn into a usable agent is not an error; see agentEntries.
func validateAgentDefinitions(raw any) (map[string]AgentDefinition, error) {
	defs, _, err := agentEntries(raw)
	return defs, err
}

// agentEntries validates every agents.<name> entry. Entries that are valid become
// definitions. Entries that only a newer version can make sense of (a built-in-only
// key on a custom name, an unknown dialect, a missing adapter, an unregistered
// session.mode) are returned as unusable and never fail the configuration. Any
// other problem is a hard error; the first one in name order is returned, and the
// valid entries are still returned for tolerant callers.
func agentEntries(raw any) (map[string]AgentDefinition, map[string]unusableAgent, error) {
	obj, ok := asObject(raw)
	if !ok {
		return nil, nil, agentDefinitionError("agents", Text("config.agent_object"))
	}
	out := map[string]AgentDefinition{}
	unusable := map[string]unusableAgent{}
	var firstErr error
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		d, reason, err := validateAgentEntry(name, obj[name])
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
		case reason != "":
			entry := unusableAgent{Reason: reason, Value: obj[name]}
			if _, builtin := embeddedByName(name); builtin {
				// The install switches are scope policy, not part of what makes the
				// agent unusable, so they keep applying; see InstallRulesOf.
				entry.Policy = AgentDefinition{InstallRules: d.InstallRules, ConfigDir: d.ConfigDir}
			}
			unusable[name] = entry
		default:
			out[name] = d
		}
	}
	return out, unusable, firstErr
}

// validateAgentEntry checks one agents.<name> entry. A non-empty reason means the
// entry is unusable in this version without being invalid. Every check of a known
// field that does not depend on the reason runs first, so a soft reason never hides
// an invalid value; checks that need the dialect or the session mode run only for
// an entry that is otherwise usable.
func validateAgentEntry(name string, value any) (d AgentDefinition, reason string, err error) {
	if !ValidAgentName(name) {
		return d, "", agentDefinitionError(name, Text("config.agent_name"))
	}
	fields, ok := asObject(value)
	if !ok {
		return d, "", agentDefinitionError(name, Text("config.agent_object"))
	}
	_, builtin := embeddedByName(name)
	if v, exists := fields["install_rules"]; exists {
		if _, ok := v.(bool); !ok {
			return d, "", agentDefinitionError(name, Text("config.agent_install_rules"))
		}
	}
	// Decode only the keys the definition declares, matched exactly: the standard
	// decoder also matches keys that differ in case, which would let a key the scanner
	// reports as ignored change the definition.
	data, _ := json.Marshal(pruneToStruct(fields, reflect.TypeOf(AgentDefinition{})))
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&d); err != nil {
		return d, "", agentDefinitionError(name, err.Error())
	}
	for _, key := range []string{"path", "process_name", "dialect"} {
		if v, exists := fields[key]; exists {
			s, ok := v.(string)
			if !ok || !validAgentText(s) {
				return d, "", agentDefinitionError(name, Text("config.agent_text", key))
			}
		}
	}
	if v, exists := fields["config_dir"]; exists {
		s, ok := v.(string)
		if !ok || !validConfigDirValue(s) {
			return d, "", agentDefinitionError(name, Text("config.agent_config_dir"))
		}
		if emb, ok := embeddedByName(name); ok && emb.ConfigHome.Fixed {
			return d, "", agentDefinitionError(name, Text("config.agent_config_dir_fixed"))
		}
	}
	for _, key := range []string{"args", "session", "prompt_delivery", "review", "program_status"} {
		if v, exists := fields[key]; exists {
			if _, ok := asObject(v); !ok {
				return d, "", agentDefinitionError(name, Text("config.agent_object"))
			}
		}
	}
	if d.SchemaVersion != 0 && d.SchemaVersion != embeddedAgentSchemaVersion {
		return d, "", agentDefinitionError(name, Text("config.agent_schema_version"))
	}
	if err := validatePromptDelivery(name, d.PromptDelivery); err != nil {
		return d, "", err
	}
	if !validProgramStatus(d.ProgramStatus) {
		return d, "", agentDefinitionError(name, Text("program_status.agent_definition_invalid"))
	}
	if d.Path != "" && !validateAgentProgram(d.Path) {
		return d, "", agentDefinitionError(name, Text("config.agent_path", d.Path))
	}
	if d.Args != nil {
		if d.Args.Start == nil {
			return d, "", agentDefinitionError(name, Text("config.agent_start"))
		}
		for _, args := range [][]string{d.Args.Start, d.Args.Resume} {
			for _, arg := range args {
				if !validAgentText(arg) || invalidPlaceholder(arg) {
					return d, "", agentDefinitionError(name, Text("config.agent_template"))
				}
			}
		}
	}
	if v, exists := fields["exit_command"]; exists {
		s, ok := v.(string)
		if !ok || !validExitCommandText(s) {
			return d, "", agentDefinitionError(name, Text("config.agent_exit_command"))
		}
	}
	if d.Session != nil && d.Session.Mode == "" {
		return d, "", agentDefinitionError(name, Text("config.agent_session"))
	}
	if err := validateSessionFile(name, d.Session); err != nil {
		return d, "", err
	}
	if err := validateReviewDefinition(name, d); err != nil {
		return d, "", err
	}
	// Entries a newer version wrote for an agent it ships as built-in: the keys
	// below exist only for built-in names, so on a custom name they mark an agent
	// this version does not have.
	switch {
	case !builtin && fields["install_rules"] != nil:
		return d, Text("config.agent_install_rules_builtin_only"), nil
	case !builtin && fields["config_dir"] != nil:
		return d, Text("config.agent_config_dir_builtin_only"), nil
	case d.Dialect != "" && !contains(ExecutionAgents, d.Dialect):
		return d, Text("config.agent_dialect"), nil
	case d.Dialect == "" && d.Args == nil && !contains(ExecutionAgents, name):
		return d, Text("config.agent_adapter"), nil
	case d.Session != nil && !validDeclaredSessionMode(d.Session.Mode):
		return d, Text("config.agent_unusable_session_mode", d.Session.Mode), nil
	}
	if d.Session != nil {
		s := d.Session
		if err := validateSessionDiscovery(name, s); err != nil {
			return d, "", err
		}
		if s.Mode == "allocated" {
			if len(s.Allocate) == 0 || !validateAgentProgram(s.Allocate[0]) {
				return d, "", agentDefinitionError(name, Text("config.agent_allocate"))
			}
			for _, arg := range s.Allocate {
				if !validAgentText(arg) {
					return d, "", agentDefinitionError(name, Text("config.agent_allocate"))
				}
			}
			if s.JSONField != "" && !validAgentText(s.JSONField) {
				return d, "", agentDefinitionError(name, Text("config.agent_allocate"))
			}
		} else if len(s.Allocate) != 0 || s.JSONField != "" {
			return d, "", agentDefinitionError(name, Text("config.agent_allocate_only"))
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
		compatibleAllocator := d.Session.Mode == "allocated" && SessionAllocatesBeforeStart(&AgentSessionDefinition{Mode: inherited})
		if inherited != "" && d.Session.Mode != inherited && !compatibleAllocator {
			// Hook and discovery modes produce an agent-native id; a foreign
			// mode would feed an incompatible value into the inherited argv.
			if _, isHook := LookupSessionHook(inherited); isHook || inherited == "discovered" {
				return d, "", agentDefinitionError(name, Text("config.agent_session_dialect", resolved.Dialect, d.Session.Mode))
			}
		}
	}
	if d.Args != nil && d.Dialect == "" && d.Session == nil && !contains(ExecutionAgents, name) {
		return d, "", agentDefinitionError(name, Text("config.agent_session_required"))
	}
	if d.Args != nil && resolved.Session.Mode != "none" && d.Args.Resume == nil {
		return d, "", agentDefinitionError(name, Text("config.agent_resume"))
	}
	return d, "", nil
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
