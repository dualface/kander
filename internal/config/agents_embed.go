package config

import (
	"bytes"
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

//go:embed agents/*.json agents/extensions/*.ts
var embeddedAgentFS embed.FS

const (
	embeddedAgentSchemaVersion = 1
	rulesIntegrationClaude     = "claude-import"
	rulesIntegrationMarkdown   = "markdown-reference"
)

// ExecutionAgents is the load order of embedded definition files in agents/index.json.
var ExecutionAgents []string

// AgentRulesSpec is the installer target and recognition strategy of one embedded agent.
type AgentRulesSpec struct {
	Global      string
	Project     string
	Integration string
}

// AgentExtensionSpec is the optional rules-extension declaration of one embedded agent.
// Only embedded definitions may declare it; a user agents.<name> overlay cannot add,
// change, or remove it. Source names a file embedded under agents/extensions/;
// Global and Project are the install targets relative to the home directory and the
// Git main worktree.
type AgentExtensionSpec struct {
	Source  string
	Global  string
	Project string
}

type PromptDelivery struct {
	Mode    string          `json:"mode"`
	Ready   *PromptReady    `json:"ready,omitempty"`
	Blocked []PromptBlocked `json:"blocked,omitempty"`
}

type PromptReady struct {
	Match     string `json:"match"`
	TimeoutMS int    `json:"timeout_ms"`
}

type PromptBlocked struct {
	Match  string `json:"match"`
	Reason string `json:"reason"`
}

type agentRulesTarget struct {
	Global  string `json:"global"`
	Project string `json:"project"`
}

type agentRulesExtension struct {
	Source  string `json:"source"`
	Global  string `json:"global"`
	Project string `json:"project"`
}

type embeddedAgent struct {
	SchemaVersion    int                    `json:"schema_version"`
	Path             string                 `json:"path"`
	ProcessName      string                 `json:"process_name"`
	Args             AgentArgs              `json:"args"`
	Session          AgentSessionDefinition `json:"session"`
	DisplayName      string                 `json:"display_name"`
	SupportsEffort   bool                   `json:"supports_effort"`
	PromptDelivery   PromptDelivery         `json:"prompt_delivery"`
	ExitCommand      string                 `json:"exit_command"`
	Review           AgentReview            `json:"review"`
	RulesTarget      agentRulesTarget       `json:"rules_target"`
	RulesIntegration string                 `json:"rules_integration"`
	RulesExtension   *agentRulesExtension   `json:"rules_extension"`
	extensionData    []byte
	LargeModel       string `json:"large_model"`
	SmallModel       string `json:"small_model"`
	LargeEffort      string `json:"large_effort"`
	SmallEffort      string `json:"small_effort"`
	Model            string `json:"model"`
	Effort           string `json:"effort"`
}

var embeddedAgents = map[string]embeddedAgent{}

func init() {
	if err := loadEmbeddedAgents(); err != nil {
		panic(err)
	}
}

func loadEmbeddedAgents() error {
	names, agents, err := loadEmbeddedAgentsFrom(embeddedAgentFS)
	if err != nil {
		return err
	}
	ExecutionAgents = names
	embeddedAgents = agents
	return nil
}

func loadEmbeddedAgentsFrom(fsys fs.FS) ([]string, map[string]embeddedAgent, error) {
	indexData, err := fs.ReadFile(fsys, "agents/index.json")
	if err != nil {
		// Tests pass a rooted FS whose index lives at the root.
		indexData, err = fs.ReadFile(fsys, "index.json")
		if err != nil {
			return nil, nil, embedAgentError("index.json", err.Error())
		}
	}
	var names []string
	dec := json.NewDecoder(bytes.NewReader(indexData))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&names); err != nil {
		return nil, nil, embedAgentError("index.json", err.Error())
	}
	if len(names) == 0 {
		return nil, nil, embedAgentError("index.json", "names")
	}
	out := make(map[string]embeddedAgent, len(names))
	for _, name := range names {
		if !ValidAgentName(name) {
			return nil, nil, embedAgentError(name+".json", "name")
		}
		fileName := name + ".json"
		data, err := fs.ReadFile(fsys, path.Join("agents", fileName))
		if err != nil {
			data, err = fs.ReadFile(fsys, fileName)
			if err != nil {
				return nil, nil, embedAgentError(fileName, err.Error())
			}
		}
		agent, err := parseEmbeddedAgentFile(fileName, data)
		if err != nil {
			return nil, nil, err
		}
		if agent.RulesExtension != nil {
			payload, err := readExtensionSource(fsys, agent.RulesExtension.Source)
			if err != nil {
				return nil, nil, err
			}
			agent.extensionData = payload
		}
		out[name] = agent
	}
	return append([]string{}, names...), out, nil
}

// readExtensionSource loads the embedded extension payload named by an agent's
// rules_extension.source, using the same rooted-FS fallback as the agent files.
func readExtensionSource(fsys fs.FS, source string) ([]byte, error) {
	data, err := fs.ReadFile(fsys, path.Join("agents", "extensions", source))
	if err != nil {
		data, err = fs.ReadFile(fsys, path.Join("extensions", source))
		if err != nil {
			return nil, embedAgentError("extensions/"+source, err.Error())
		}
	}
	return data, nil
}

func parseEmbeddedAgentFile(fileName string, data []byte) (embeddedAgent, error) {
	var agent embeddedAgent
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&agent); err != nil {
		return embeddedAgent{}, embedAgentError(fileName, err.Error())
	}
	if agent.SchemaVersion != embeddedAgentSchemaVersion {
		return embeddedAgent{}, embedAgentError(fileName, "schema_version")
	}
	if !validAgentText(agent.Path) {
		return embeddedAgent{}, embedAgentError(fileName, "path")
	}
	if !validAgentText(agent.ProcessName) {
		return embeddedAgent{}, embedAgentError(fileName, "process_name")
	}
	if agent.Args.Start == nil {
		return embeddedAgent{}, embedAgentError(fileName, "args.start")
	}
	for _, args := range [][]string{agent.Args.Start, agent.Args.Resume} {
		for _, arg := range args {
			if !validAgentText(arg) || invalidPlaceholder(arg) {
				return embeddedAgent{}, embedAgentError(fileName, "args")
			}
		}
	}
	if !validDeclaredSessionMode(agent.Session.Mode) {
		return embeddedAgent{}, embedAgentError(fileName, "session.mode: "+agent.Session.Mode)
	}
	if err := validateSessionDiscovery(fileName, &agent.Session); err != nil {
		return embeddedAgent{}, err
	}
	if err := validateSessionFile(fileName, &agent.Session); err != nil {
		return embeddedAgent{}, err
	}
	if !validExitCommandText(agent.ExitCommand) {
		return embeddedAgent{}, embedAgentError(fileName, "exit_command")
	}
	if !validAgentText(agent.DisplayName) {
		return embeddedAgent{}, embedAgentError(fileName, "display_name")
	}
	if err := validatePromptDelivery(fileName, &agent.PromptDelivery); err != nil {
		return embeddedAgent{}, err
	}
	if agent.RulesTarget.Global != "" && !validAgentText(agent.RulesTarget.Global) {
		return embeddedAgent{}, embedAgentError(fileName, "rules_target.global")
	}
	if agent.RulesTarget.Project != "" && !validAgentText(agent.RulesTarget.Project) {
		return embeddedAgent{}, embedAgentError(fileName, "rules_target.project")
	}
	if !contains([]string{rulesIntegrationClaude, rulesIntegrationMarkdown}, agent.RulesIntegration) {
		return embeddedAgent{}, embedAgentError(fileName, "rules_integration")
	}
	if ext := agent.RulesExtension; ext != nil {
		if !validAgentText(ext.Source) || strings.ContainsAny(ext.Source, `/\\`) {
			return embeddedAgent{}, embedAgentError(fileName, "rules_extension.source")
		}
		if !validAgentText(ext.Global) {
			return embeddedAgent{}, embedAgentError(fileName, "rules_extension.global")
		}
		if !validAgentText(ext.Project) {
			return embeddedAgent{}, embedAgentError(fileName, "rules_extension.project")
		}
	}
	if strings.ContainsAny(agent.LargeModel+agent.SmallModel+agent.LargeEffort+agent.SmallEffort+agent.Model+agent.Effort, "\n\r\x00") {
		return embeddedAgent{}, embedAgentError(fileName, "model")
	}
	if err := validateReviewDefinition(fileName, AgentDefinition{Args: &agent.Args, Review: &agent.Review}); err != nil {
		return embeddedAgent{}, err
	}
	return agent, nil
}

func embedAgentError(fileName, field string) error {
	return configErrorf("config.agent_embed_invalid", fileName, field)
}

func embeddedByName(name string) (embeddedAgent, bool) {
	agent, ok := embeddedAgents[name]
	return agent, ok
}

func defaultAgentName() string {
	if len(ExecutionAgents) == 0 {
		return ""
	}
	return ExecutionAgents[0]
}

func AgentExecutableName(agent string) string {
	if emb, ok := embeddedByName(agent); ok && emb.Path != "" {
		return emb.Path
	}
	return agent
}

func AgentDisplayName(name string) string {
	if emb, ok := embeddedByName(name); ok && emb.DisplayName != "" {
		return emb.DisplayName
	}
	return name
}

func embeddedSupportsEffort(name string) bool {
	emb, ok := embeddedByName(name)
	return ok && emb.SupportsEffort
}

// AgentSupportsEffort reports whether the options panel and summaries show effort.
// User-supplied argv templates always accept effort; otherwise the named or dialect
// embedded definition decides, matching the previous Cursor-only hide rule.
func AgentSupportsEffort(cfg *Config, name string) bool {
	dialect := name
	userArgs := false
	if cfg != nil {
		if user, ok := cfg.Agents[name]; ok {
			userArgs = user.Args != nil
			if user.Dialect != "" {
				dialect = user.Dialect
			}
		}
	}
	if userArgs {
		return true
	}
	if emb, ok := embeddedByName(dialect); ok {
		return emb.SupportsEffort
	}
	if emb, ok := embeddedByName(name); ok {
		return emb.SupportsEffort
	}
	return true
}

// ReviewModelSupportsEffort reports whether models.review.<agent> stores an
// effort key. User argv overlays must not invent a review effort field that
// configuredModel would discard.
func ReviewModelSupportsEffort(cfg *Config, name string) bool {
	if cfg != nil {
		if entry, ok := cfg.Models.Review[name]; ok {
			_, has := entry["effort"]
			return has
		}
	}
	if emb, ok := embeddedByName(name); ok {
		return emb.SupportsEffort
	}
	return false
}

func RulesSpec(name string) (AgentRulesSpec, bool) {
	emb, ok := embeddedByName(name)
	if !ok {
		return AgentRulesSpec{}, false
	}
	return AgentRulesSpec{
		Global:      emb.RulesTarget.Global,
		Project:     emb.RulesTarget.Project,
		Integration: emb.RulesIntegration,
	}, true
}

// AgentExtension returns the embedded rules-extension spec and payload of one
// agent. The second return value is false when the agent is unknown or declares
// no rules_extension; user overlays never change the answer.
func AgentExtension(name string) (AgentExtensionSpec, []byte, bool) {
	emb, ok := embeddedByName(name)
	if !ok || emb.RulesExtension == nil {
		return AgentExtensionSpec{}, nil, false
	}
	return AgentExtensionSpec{
		Source:  emb.RulesExtension.Source,
		Global:  emb.RulesExtension.Global,
		Project: emb.RulesExtension.Project,
	}, emb.extensionData, true
}

func (e embeddedAgent) kanbanFields() map[string]string {
	if e.SupportsEffort {
		return map[string]string{
			"model":        "",
			"large_model":  e.LargeModel,
			"small_model":  e.SmallModel,
			"large_effort": e.LargeEffort,
			"small_effort": e.SmallEffort,
		}
	}
	return map[string]string{
		"large_model": e.LargeModel,
		"small_model": e.SmallModel,
	}
}

func (e embeddedAgent) reviewFields() map[string]string {
	if e.SupportsEffort {
		return map[string]string{"model": e.Model, "effort": e.Effort}
	}
	return map[string]string{"model": e.Model}
}

func kanbanModelDefaults() map[string]map[string]string {
	out := make(map[string]map[string]string, len(ExecutionAgents))
	for _, name := range ExecutionAgents {
		if emb, ok := embeddedByName(name); ok {
			out[name] = emb.kanbanFields()
		}
	}
	return out
}

func reviewModelDefaults() map[string]map[string]string {
	out := make(map[string]map[string]string, len(ExecutionAgents))
	for _, name := range ExecutionAgents {
		if emb, ok := embeddedByName(name); ok {
			out[name] = emb.reviewFields()
		}
	}
	return out
}

func cloneArgs(src *AgentArgs) *AgentArgs {
	if src == nil {
		return nil
	}
	out := *src
	out.Start = slices.Clone(src.Start)
	out.Resume = slices.Clone(src.Resume)
	out.Review = slices.Clone(src.Review)
	return &out
}

func clonePromptDelivery(src *PromptDelivery) *PromptDelivery {
	if src == nil {
		return nil
	}
	out := *src
	if src.Ready != nil {
		ready := *src.Ready
		out.Ready = &ready
	}
	if src.Blocked != nil {
		out.Blocked = append([]PromptBlocked{}, src.Blocked...)
	}
	return &out
}

func validatePromptDelivery(name string, d *PromptDelivery) error {
	if d == nil {
		return nil
	}
	switch d.Mode {
	case "", "argv":
		if d.Mode == "" {
			d.Mode = "argv"
		}
		if d.Ready != nil || len(d.Blocked) > 0 {
			return agentDefinitionError(name, Text("config.agent_prompt_delivery", "ready"))
		}
	case "pane":
		if d.Ready == nil {
			return agentDefinitionError(name, Text("config.agent_prompt_delivery", "ready"))
		}
		if !validAgentText(d.Ready.Match) {
			return agentDefinitionError(name, Text("config.agent_prompt_delivery", "ready.match"))
		}
		if d.Ready.TimeoutMS <= 0 {
			return agentDefinitionError(name, Text("config.agent_prompt_delivery", "ready.timeout_ms"))
		}
		if err := compileDeliveryPattern(d.Ready.Match); err != nil {
			return agentDefinitionError(name, Text("config.agent_prompt_delivery", "ready.match"))
		}
		for _, blocked := range d.Blocked {
			if !validAgentText(blocked.Match) {
				return agentDefinitionError(name, Text("config.agent_prompt_delivery", "blocked.match"))
			}
			if strings.TrimSpace(blocked.Reason) == "" || strings.ContainsAny(blocked.Reason, "\n\r") {
				return agentDefinitionError(name, Text("config.agent_prompt_delivery", "blocked.reason"))
			}
			if err := compileDeliveryPattern(blocked.Match); err != nil {
				return agentDefinitionError(name, Text("config.agent_prompt_delivery", "blocked.match"))
			}
		}
	default:
		return agentDefinitionError(name, Text("config.agent_prompt_delivery", "mode"))
	}
	return nil
}

func compileDeliveryPattern(match string) error {
	if strings.HasPrefix(match, "regex:") {
		_, err := regexp.Compile(strings.TrimPrefix(match, "regex:"))
		return err
	}
	return nil
}
