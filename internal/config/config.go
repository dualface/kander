// Package config resolves the Kander install scope and reads/writes the schema-validated config.json.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
)

const (
	SchemaVersion            = 1
	ProjectInstallDirname    = ".kander"
	ProjectGitExcludePattern = "/.kander/"
	EnvConfig                = "KANDER_CONFIG"
	EnvLang                  = "KANDER_LANG"
	EnvLangCLI               = "KANDER_LANG_CLI"
)

type Mode string

const (
	ModeGlobal  Mode = "global"
	ModeProject Mode = "project"
)

var (
	ExecutionAgents  = []string{"codex", "claude", "grok", "cursor"}
	TaskScales       = []string{"large", "small"}
	ReviewAgents     = []string{"codex", "claude", "grok", "cursor"}
	ReviewRoles      = []string{"PM", "CSA", "Hacker", "QA"}
	ReviewStageModes = []string{"auto", "skip", "required"}
	Launchers        = []string{"auto", "tmux", "tmux-session", "herdr", "foreground", "console"}
	Languages        = []string{"cn", "en"}
	TUIThemes        = []string{"auto", "light", "dark"}
)

const (
	DefaultTUIColumns        = 5
	MinTUIColumns            = 1
	MaxTUIColumns            = 7
	DefaultTUIMinColumnWidth = 40
	MinTUIMinColumnWidth     = 20
	MaxTUIMinColumnWidth     = 60
	DefaultTUIRefresh        = 30
	MinTUIRefresh            = 1
	MaxTUIRefresh            = 3600
)

// AgentExecutables maps agent names used in the config and on the command line to executable names on PATH.
var AgentExecutables = map[string]string{
	"codex":  "codex",
	"claude": "claude",
	"grok":   "grok",
	"cursor": "cursor-agent",
}

var modelIDFields = map[string]struct{}{
	"model":       {},
	"large_model": {},
	"small_model": {},
}

// Every agent in kanbanModelDefaults carries one model per task scale:
// large and small tasks can use different models and reasoning efforts even when they pick the same agent.
// The retained "model" key is for legacy configs: a scale model falls back to it when empty, and new configs no longer write it.
var kanbanModelDefaults = map[string]map[string]string{
	"codex": {
		"model":        "",
		"large_model":  "gpt-5.6-sol",
		"small_model":  "gpt-5.6-sol",
		"large_effort": "high",
		"small_effort": "medium",
	},
	"claude": {
		"model":        "",
		"large_model":  "opus",
		"small_model":  "opus",
		"large_effort": "high",
		"small_effort": "medium",
	},
	"grok": {
		"model":        "",
		"large_model":  "",
		"small_model":  "",
		"large_effort": "xhigh",
		"small_effort": "high",
	},
	"cursor": {
		"large_model": "cursor-grok-4.6-xhigh",
		"small_model": "cursor-grok-4.6-high",
	},
}

var reviewModelDefaults = map[string]map[string]string{
	"codex":  {"model": "gpt-5.6-sol", "effort": "high"},
	"claude": {"model": "opus", "effort": "high"},
	"grok":   {"model": "", "effort": "high"},
	"cursor": {"model": "cursor-grok-4.6-xhigh"},
}

var languageLabels = map[string]string{
	"cn": "config.languageLabels.cn",
	"en": "config.languageLabels.en",
}

// Error means the config is unreadable or violates the schema.
type Error struct {
	Msg string
	Err error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func configErrorf(id string, args ...any) *Error {
	return &Error{Msg: Text(id, args...)}
}

func configErrorfWrap(err error, id string, args ...any) *Error {
	return &Error{Msg: Text(id, args...), Err: err}
}

func IsError(err error) bool {
	var target *Error
	return errors.As(err, &target)
}

// InstallPaths holds the shared paths of the current install scope.
//
// global maps to the layout under the user's HOME; project maps to .kander/ in the Git main worktree.
// Running from the source tree counts as global, even when the repository root holds both cmd/ and rules/.
type InstallPaths struct {
	Mode        Mode
	ConfigPath  string
	RulesDir    string
	BinDir      string
	ShareDir    string
	ProjectRoot string
	InstallRoot string
}

// Models matches the models section of the onevoke schema.
// ReviewRoles holds per-role overrides: an empty value means the role inherits the value of its selected reviewer,
// so all four roles can use different models and reasoning efforts even when they select the same reviewer.
type Models struct {
	Kanban      map[string]map[string]string `json:"kanban"`
	Review      map[string]map[string]string `json:"review"`
	ReviewRoles map[string]map[string]string `json:"review_roles"`
}

// TUI holds the persistent terminal UI preferences. Command-line flags only affect the current run and never change these values.
type TUI struct {
	Columns        int    `json:"columns"`
	MinColumnWidth int    `json:"min_column_width"`
	Refresh        int    `json:"refresh"`
	Single         bool   `json:"single"`
	Theme          string `json:"theme"`
}

// Config is the schema-validated configuration.
type Config struct {
	SchemaVersion   int               `json:"schema_version"`
	WelcomeComplete bool              `json:"welcome_complete"`
	KanbanAgent     string            `json:"kanban_agent"`
	KanbanAgents    map[string]string `json:"kanban_agents"`
	Launcher        string            `json:"launcher"`
	Reviewers       map[string]string `json:"reviewers"`
	ReviewStages    map[string]string `json:"review_stages"`
	Rules           Rules             `json:"rules"`
	Models          Models            `json:"models"`
	TUI             TUI               `json:"tui"`
	Language        string            `json:"language"`
	AgentLanguage   string            `json:"agent_language"`
}

// Clone deep-copies the config so a long-lived editing session can keep its baseline.
func Clone(src *Config) *Config {
	if src == nil {
		return nil
	}
	out := *src
	out.KanbanAgents = cloneStringMap(src.KanbanAgents)
	out.Reviewers = cloneStringMap(src.Reviewers)
	out.ReviewStages = cloneStringMap(src.ReviewStages)
	out.Rules = src.Rules.Clone()
	out.Models = Models{
		Kanban:      cloneNested(src.Models.Kanban),
		Review:      cloneNested(src.Models.Review),
		ReviewRoles: cloneNested(src.Models.ReviewRoles),
	}
	return &out
}

func cloneStringMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneNested(src map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(src))
	for k, v := range src {
		out[k] = cloneStringMap(v)
	}
	return out
}

func defaultReviewRoles() map[string]map[string]string {
	out := make(map[string]map[string]string, len(ReviewRoles))
	for _, role := range ReviewRoles {
		// Empty by default: the role inherits the value of its reviewer.
		out[role] = map[string]string{"model": "", "effort": ""}
	}
	return out
}

func DefaultModels() Models {
	return Models{
		Kanban:      cloneNested(kanbanModelDefaults),
		Review:      cloneNested(reviewModelDefaults),
		ReviewRoles: defaultReviewRoles(),
	}
}

func DefaultTUI() TUI {
	return TUI{
		Columns:        DefaultTUIColumns,
		MinColumnWidth: DefaultTUIMinColumnWidth,
		Refresh:        DefaultTUIRefresh,
		Theme:          "auto",
	}
}

// ReviewModelFor returns the model and reasoning effort actually in force for a review role.
// An empty role override falls back to the agent's own value; the agent comes from the caller because
// kander review may name a reviewer explicitly, which need not match the reviewer configured for that role.
func ReviewModelFor(cfg *Config, agent, role string) (model, effort string) {
	if cfg == nil {
		return "", ""
	}
	agentEntry := cfg.Models.Review[agent]
	roleEntry := cfg.Models.ReviewRoles[role]
	model = roleEntry["model"]
	if model == "" {
		model = agentEntry["model"]
	}
	effort = roleEntry["effort"]
	if effort == "" {
		effort = agentEntry["effort"]
	}
	return model, effort
}

func DefaultReviewStages() map[string]string {
	out := make(map[string]string, len(ReviewRoles))
	for _, role := range ReviewRoles {
		out[role] = "auto"
	}
	return out
}

func DefaultLauncher() string {
	if runtime.GOOS == "windows" {
		return "console"
	}
	return "auto"
}

func DefaultConfig() *Config {
	agents := make(map[string]string, len(TaskScales))
	for _, scale := range TaskScales {
		agents[scale] = "codex"
	}
	reviewers := make(map[string]string, len(ReviewRoles))
	for _, role := range ReviewRoles {
		reviewers[role] = "codex"
	}
	return &Config{
		SchemaVersion:   SchemaVersion,
		WelcomeComplete: false,
		KanbanAgent:     "codex",
		KanbanAgents:    agents,
		Launcher:        DefaultLauncher(),
		Reviewers:       reviewers,
		ReviewStages:    DefaultReviewStages(),
		Rules:           DefaultRules(true),
		Models:          DefaultModels(),
		TUI:             DefaultTUI(),
		Language:        "cn",
		AgentLanguage:   DefaultAgentLanguage("cn"),
	}
}

// agentLanguageDefaults maps an interface language to the agent communication language a config
// without agent_language falls back to.
var agentLanguageDefaults = map[string]string{
	"cn": "zh-CN",
	"en": "en",
}

// DefaultAgentLanguage returns the agent communication language derived from an interface language.
func DefaultAgentLanguage(language string) string {
	if value, ok := agentLanguageDefaults[language]; ok {
		return value
	}
	return agentLanguageDefaults["en"]
}

// validateAgentLanguage accepts any short, single-line, non-empty language name such as en, zh-CN, or ja.
func validateAgentLanguage(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", configErrorf("config.agent_language_invalid")
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 64 {
		return "", configErrorf("config.agent_language_invalid")
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return "", configErrorf("config.agent_language_invalid")
		}
	}
	return text, nil
}

func AgentExecutableName(agent string) string {
	if name, ok := AgentExecutables[agent]; ok {
		return name
	}
	return agent
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func choiceError(name, expected string) *Error {
	return configErrorf(
		"config.must_be_one_of", name, expected,
	)
}

func validateChoice(value any, choices []string, name string) (string, error) {
	text, ok := value.(string)
	if !ok || !contains(choices, text) {
		return "", choiceError(name, strings.Join(choices, ", "))
	}
	return text, nil
}

// launcherPlatformError now covers a single platform conflict: console is
// Windows-only. tmux's platform limit is reported by launch at start time, and
// herdr/auto work on both sides.
func launcherPlatformError() *Error {
	return configErrorf("config.console_is_windows_only")
}

func validateLauncher(value any) (string, error) {
	launcher, err := validateChoice(value, Launchers, "launcher")
	if err != nil {
		return "", err
	}
	if err := CheckLauncherPlatform(launcher); err != nil {
		return "", err
	}
	return launcher, nil
}

// CheckLauncherPlatform validates the launcher against the current OS.
// herdr has shipped a native Windows build since 0.8, so auto/herdr are no
// longer POSIX-only; tmux still needs POSIX, but launch reports that at start.
func CheckLauncherPlatform(launcher string) error {
	if launcher == "console" && runtime.GOOS != "windows" {
		return launcherPlatformError()
	}
	return nil
}

func validateKanbanAgents(raw any, defaultAgent string) (map[string]string, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.kanban_agents_must_be_a_json_object")
	}
	allowed := map[string]struct{}{}
	for _, scale := range TaskScales {
		allowed[scale] = struct{}{}
	}
	var unknown []string
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return nil, configErrorf(
			"config.kanban_agents_has_unknown_scales_only_are_allowed", strings.Join(sorted(unknown), ", "), strings.Join(TaskScales, ", "),
		)
	}
	agents := make(map[string]string, len(TaskScales))
	for _, scale := range TaskScales {
		agents[scale] = defaultAgent
	}
	for _, scale := range TaskScales {
		if _, exists := obj[scale]; !exists {
			continue
		}
		agent, err := validateChoice(obj[scale], ExecutionAgents, "kanban_agents."+scale)
		if err != nil {
			return nil, err
		}
		agents[scale] = agent
	}
	return agents, nil
}

// KanbanAgentFor picks the execution agent for a task scale; an unknown scale is rejected.
func KanbanAgentFor(cfg *Config, kind string) (string, error) {
	if cfg == nil || cfg.KanbanAgents == nil {
		return "", configErrorf("config.unknown_task_scale", kind)
	}
	if !contains(TaskScales, kind) {
		return "", configErrorf("config.unknown_task_scale", kind)
	}
	return cfg.KanbanAgents[kind], nil
}

// ExecutionAgentsInUse lists the execution agents that will actually be launched, deduplicated, large task first.
func ExecutionAgentsInUse(cfg *Config) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, scale := range TaskScales {
		agent := cfg.KanbanAgents[scale]
		if _, ok := seen[agent]; ok {
			continue
		}
		seen[agent] = struct{}{}
		out = append(out, agent)
	}
	return out
}

func validateReviewStages(raw any) (map[string]string, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.review_stages_must_be_a_json_object")
	}
	stages := DefaultReviewStages()
	allowed := map[string]struct{}{}
	for _, role := range ReviewRoles {
		allowed[role] = struct{}{}
	}
	var unknown []string
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return nil, configErrorf(
			"config.review_stages_has_unknown_roles", strings.Join(sorted(unknown), ", "),
		)
	}
	for _, role := range ReviewRoles {
		if _, exists := obj[role]; !exists {
			continue
		}
		mode, err := validateChoice(obj[role], ReviewStageModes, "review_stages."+role)
		if err != nil {
			return nil, err
		}
		stages[role] = mode
	}
	return stages, nil
}

func validateModels(raw any) (Models, error) {
	models := DefaultModels()
	obj, ok := raw.(map[string]any)
	if !ok {
		return Models{}, configErrorf("config.models_must_be_a_json_object")
	}
	var unknown []string
	for key := range obj {
		if key != "kanban" && key != "review" && key != "review_roles" {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return Models{}, configErrorf(
			"config.models_has_unknown_keys", strings.Join(sorted(unknown), ", "),
		)
	}
	sections := []struct {
		name string
		// agents lists the keys allowed in this section: the first two sections use agent names, review_roles uses review role names.
		agents []string
		dest   map[string]map[string]string
		// When allowEmpty is true the values of this section may be empty strings; review_roles uses an empty string to mean inherit.
		allowEmpty bool
	}{
		{"kanban", ExecutionAgents, models.Kanban, false},
		{"review", ReviewAgents, models.Review, false},
		{"review_roles", ReviewRoles, models.ReviewRoles, true},
	}
	for _, section := range sections {
		providedRaw, exists := obj[section.name]
		if !exists {
			continue
		}
		provided, ok := providedRaw.(map[string]any)
		if !ok {
			return Models{}, configErrorf(
				"config.models_must_be_a_json_object_2", section.name,
			)
		}
		allowed := map[string]struct{}{}
		for _, agent := range section.agents {
			allowed[agent] = struct{}{}
		}
		var unknownAgents []string
		for agent := range provided {
			if _, ok := allowed[agent]; !ok {
				unknownAgents = append(unknownAgents, agent)
			}
		}
		if len(unknownAgents) > 0 {
			return Models{}, configErrorf(
				"config.models_has_unknown_agents", section.name, strings.Join(sorted(unknownAgents), ", "),
			)
		}
		for agent, entryRaw := range provided {
			entry, ok := entryRaw.(map[string]any)
			if !ok {
				return Models{}, configErrorf(
					"config.models_must_be_a_json_object_3", section.name, agent,
				)
			}
			fields := section.dest[agent]
			var unknownFields []string
			for field := range entry {
				if _, ok := fields[field]; !ok {
					unknownFields = append(unknownFields, field)
				}
			}
			if len(unknownFields) > 0 {
				return Models{}, configErrorf(
					"config.models_has_unknown_fields", section.name, agent, strings.Join(sorted(unknownFields), ", "),
				)
			}
			for field, value := range entry {
				text, ok := value.(string)
				_, modelID := modelIDFields[field]
				if section.allowEmpty {
					modelID = true
				}
				if !ok || (!modelID && strings.TrimSpace(text) == "") {
					kind := Text("config.non_empty_string")
					if modelID {
						kind = Text("config.string")
					}
					return Models{}, configErrorf(
						"config.models_must_be_a", section.name, agent, field, kind,
					)
				}
				if strings.ContainsAny(text, "\n\r\x00") {
					return Models{}, configErrorf(
						"config.models_must_not_contain_line_breaks_or_nul", section.name, agent, field,
					)
				}
				fields[field] = text
			}
		}
	}
	return models, nil
}

func validateInteger(value any, name string, min, max int) (int, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, configErrorf("config.must_be_an_integer", name)
	}
	parsed, err := number.Int64()
	if err != nil || parsed < int64(min) || parsed > int64(max) {
		return 0, configErrorf(
			"config.must_be_an_integer_from_to", name, min, max,
		)
	}
	return int(parsed), nil
}

func validateTUI(raw any) (TUI, error) {
	obj, ok := asObject(raw)
	if !ok {
		return TUI{}, configErrorf("config.tui_must_be_a_json_object")
	}
	allowed := map[string]struct{}{
		"columns": {}, "min_column_width": {}, "refresh": {}, "single": {}, "theme": {},
	}
	var unknown []string
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		return TUI{}, configErrorf(
			"config.tui_has_unknown_fields", strings.Join(sorted(unknown), ", "),
		)
	}
	columns, err := validateInteger(obj["columns"], "tui.columns", MinTUIColumns, MaxTUIColumns)
	if err != nil {
		return TUI{}, err
	}
	width, err := validateInteger(obj["min_column_width"], "tui.min_column_width", MinTUIMinColumnWidth, MaxTUIMinColumnWidth)
	if err != nil {
		return TUI{}, err
	}
	refresh, err := validateInteger(obj["refresh"], "tui.refresh", MinTUIRefresh, MaxTUIRefresh)
	if err != nil {
		return TUI{}, err
	}
	single, ok := obj["single"].(bool)
	if !ok {
		return TUI{}, configErrorf("config.tui_single_must_be_a_boolean")
	}
	theme, err := validateChoice(obj["theme"], TUIThemes, "tui.theme")
	if err != nil {
		return TUI{}, err
	}
	return TUI{Columns: columns, MinColumnWidth: width, Refresh: refresh, Single: single, Theme: theme}, nil
}

func asObject(raw any) (map[string]any, bool) {
	obj, ok := raw.(map[string]any)
	return obj, ok
}

func schemaVersionOf(raw any) any {
	switch v := raw.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
		return v.String()
	default:
		return v
	}
}

// Validate checks and completes the schema; an unknown scale or an illegal agent is rejected.
func Validate(raw any) (*Config, error) {
	obj, ok := asObject(raw)
	if !ok {
		return nil, configErrorf("config.config_root_must_be_a_json_object")
	}
	if schemaVersionOf(obj["schema_version"]) != SchemaVersion {
		return nil, configErrorf(
			"config.unsupported_schema_version_only_is_supported", obj["schema_version"], SchemaVersion,
		)
	}
	welcome, ok := obj["welcome_complete"].(bool)
	if !ok {
		return nil, configErrorf("config.welcome_complete_must_be_a_boolean")
	}
	kanbanAgent, err := validateChoice(obj["kanban_agent"], ExecutionAgents, "kanban_agent")
	if err != nil {
		return nil, err
	}
	var kanbanAgents map[string]string
	if _, exists := obj["kanban_agents"]; exists {
		kanbanAgents, err = validateKanbanAgents(obj["kanban_agents"], kanbanAgent)
		if err != nil {
			return nil, err
		}
	} else {
		kanbanAgents = make(map[string]string, len(TaskScales))
		for _, scale := range TaskScales {
			kanbanAgents[scale] = kanbanAgent
		}
	}
	launcher, err := validateLauncher(obj["launcher"])
	if err != nil {
		return nil, err
	}
	reviewersRaw, ok := asObject(obj["reviewers"])
	if !ok {
		return nil, configErrorf("config.reviewers_must_be_a_json_object")
	}
	reviewers := make(map[string]string, len(ReviewRoles))
	for _, role := range ReviewRoles {
		agent, err := validateChoice(reviewersRaw[role], ReviewAgents, "reviewers."+role)
		if err != nil {
			return nil, err
		}
		reviewers[role] = agent
	}
	var stages map[string]string
	if _, exists := obj["review_stages"]; exists {
		stages, err = validateReviewStages(obj["review_stages"])
		if err != nil {
			return nil, err
		}
	} else {
		stages = DefaultReviewStages()
	}
	rules := legacyRules()
	if rawRules, exists := obj["rules"]; exists {
		rules, err = validateRules(rawRules)
		if err != nil {
			return nil, err
		}
	}
	var models Models
	if _, exists := obj["models"]; exists {
		models, err = validateModels(obj["models"])
		if err != nil {
			return nil, err
		}
	} else {
		models = DefaultModels()
	}
	tui := DefaultTUI()
	if _, exists := obj["tui"]; exists {
		tui, err = validateTUI(obj["tui"])
		if err != nil {
			return nil, err
		}
	}
	languageRaw, hasLanguage := obj["language"]
	if !hasLanguage {
		languageRaw = "cn"
	}
	language, err := validateChoice(languageRaw, Languages, "language")
	if err != nil {
		return nil, err
	}
	agentLanguageRaw, hasAgentLanguage := obj["agent_language"]
	if !hasAgentLanguage {
		agentLanguageRaw = DefaultAgentLanguage(language)
	}
	agentLanguage, err := validateAgentLanguage(agentLanguageRaw)
	if err != nil {
		return nil, err
	}
	return &Config{
		SchemaVersion:   SchemaVersion,
		WelcomeComplete: welcome,
		KanbanAgent:     kanbanAgent,
		KanbanAgents:    kanbanAgents,
		Launcher:        launcher,
		Reviewers:       reviewers,
		ReviewStages:    stages,
		Rules:           rules,
		Models:          models,
		TUI:             tui,
		Language:        language,
		AgentLanguage:   agentLanguage,
	}, nil
}

func ValidateJSON(data []byte) (*Config, error) {
	raw, err := decodeJSON(data)
	if err != nil {
		return nil, err
	}
	return Validate(raw)
}

func decodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, configErrorf("config.config_contains_trailing_json")
	}
	return raw, nil
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return home + path[1:]
	}
	return path
}
