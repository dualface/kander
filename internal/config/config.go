// Package config resolves the Kander install scope, merges an optional project overlay,
// and reads/writes the schema-validated scope config.json plus sparse overlay edits.
package config

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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
	TaskScales       = []string{"large", "small"}
	ReviewRoles      = []string{"PMQA", "Security"}
	ReviewStageModes = []string{"auto", "skip", "required"}
	Languages        = []string{"cn", "en", "ja"}
	TUIThemes        = []string{"auto", "light", "light-warm", "light-contrast", "dark", "dark-soft", "dark-contrast", "tide", "dusk", "slate-dark", "slate-light", "matcha-zen", "bamboo-multiplex"}
)

const (
	DefaultTUIColumns        = 5
	MinTUIColumns            = 1
	MaxTUIColumns            = 7
	DefaultTUIMinColumnWidth = 40
	MinTUIMinColumnWidth     = 20
	MaxTUIMinColumnWidth     = 60
	DefaultTUIRefresh        = 30
	DefaultTUIThemeLight     = "light"
	DefaultTUIThemeDark      = "dark"
	MinTUIRefresh            = 1
	MaxTUIRefresh            = 3600
)

var modelIDFields = map[string]struct{}{
	"model":       {},
	"large_model": {},
	"small_model": {},
}

var languageLabels = map[string]string{
	"cn": "config.languageLabels.cn",
	"en": "config.languageLabels.en",
	"ja": "config.languageLabels.ja",
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
// ReviewRoles holds per-role overrides. Empty values inherit the selected reviewer's
// models.review entry. Prefer large_*/small_* when set; otherwise fall back to the
// shared model/effort keys for legacy entries. A large_agent/small_agent binding
// restricts that scale's overrides to its reviewer and disables legacy fallback.
type Models struct {
	Kanban      map[string]map[string]string `json:"kanban"`
	Review      map[string]map[string]string `json:"review"`
	ReviewRoles map[string]map[string]string `json:"review_roles"`
	Chat        map[string]map[string]string `json:"chat"`
}

// TUI holds the persistent terminal UI preferences. Command-line flags only affect the current run and never change these values.
type TUI struct {
	Compact        bool   `json:"compact"`
	Columns        int    `json:"columns"`
	MinColumnWidth int    `json:"min_column_width"`
	Refresh        int    `json:"refresh"`
	Single         bool   `json:"single"`
	Theme          string `json:"theme"`
	// ThemeLight and ThemeDark are the named themes "auto" picks for a light
	// or dark terminal background. They never take "auto" themselves.
	ThemeLight string `json:"theme_light"`
	ThemeDark  string `json:"theme_dark"`
}

// Config is the schema-validated configuration.
type Config struct {
	SchemaVersion   int                          `json:"schema_version"`
	WelcomeComplete bool                         `json:"welcome_complete"`
	KanbanAgent     string                       `json:"kanban_agent"`
	KanbanAgents    map[string]string            `json:"kanban_agents"`
	ChatAgent       string                       `json:"chat_agent"`
	Launcher        string                       `json:"launcher"`
	Reviewers       map[string]map[string]string `json:"reviewers"`
	ReviewStages    map[string]map[string]string `json:"review_stages"`
	Rules           Rules                        `json:"rules"`
	Models          Models                       `json:"models"`
	TUI             TUI                          `json:"tui"`
	Issue           Issue                        `json:"issue"`
	Server          Server                       `json:"server,omitzero"`
	Project         Project                      `json:"project,omitzero"`
	Git             Git                          `json:"git"`
	Language        string                       `json:"language"`
	AgentLanguage   string                       `json:"agent_language"`
	Agents          map[string]AgentDefinition   `json:"agents,omitempty"`

	// ExtendedLaunchTimeouts raises the launch-time waits to
	// ExtendedLaunchTimeout; see LaunchTimeout.
	ExtendedLaunchTimeouts bool `json:"extended_launch_timeouts"`

	// InstallRules is the global rules-injection switch; agents.<name>.install_rules
	// overrides it per agent. Only the scope config file sets it; see ScopeInstallRules.
	InstallRules bool `json:"install_rules"`

	// Ignored lists what this version skipped while loading: keys it does not
	// define, models entries of unknown agents, and unusable agents entries.
	// Save writes them back unchanged. It is never serialized from the struct.
	Ignored []Ignored `json:"-"`
	// UnusableAgents maps the name of an agents entry that cannot become an agent
	// in this version to the reason. Such an agent is absent from AgentNames.
	UnusableAgents map[string]string `json:"-"`
	// UnusablePolicy holds the install_rules and config_dir values of unusable
	// built-in entries. They are scope policy and keep applying; see InstallRulesOf.
	UnusablePolicy map[string]AgentDefinition `json:"-"`
	// Policy says where the project policy values (git, review_stages, rules,
	// agent_language, issue) come from; Load replaces them with the cached
	// server policy of the bound project. It is never serialized from the struct.
	Policy PolicyState `json:"-"`
	// policyLocal are the file values a policy replaced; nil while none did.
	policyLocal *PolicyValues
	// layered marks a configuration that holds more than the scope file: a project
	// overlay or a project policy merged by Load, or the display of kander config.
	// Save refuses it, so neither ever lands in config.json.
	layered bool
}

// Clone deep-copies the config so a long-lived editing session can keep its baseline.
func Clone(src *Config) *Config {
	if src == nil {
		return nil
	}
	out := *src
	out.Agents = CloneAgents(src.Agents)
	out.Ignored = slices.Clone(src.Ignored)
	if src.UnusableAgents != nil {
		out.UnusableAgents = cloneStringMap(src.UnusableAgents)
	}
	out.UnusablePolicy = CloneAgents(src.UnusablePolicy)
	out.KanbanAgents = cloneStringMap(src.KanbanAgents)
	out.ChatAgent = src.ChatAgent
	out.Reviewers = cloneNested(src.Reviewers)
	out.ReviewStages = cloneNested(src.ReviewStages)
	out.Rules = src.Rules.Clone()
	out.Models = Models{
		Kanban:      cloneNested(src.Models.Kanban),
		Review:      cloneNested(src.Models.Review),
		ReviewRoles: cloneNested(src.Models.ReviewRoles),
		Chat:        cloneNested(src.Models.Chat),
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
		out[role] = map[string]string{
			"model": "", "effort": "",
			"large_model": "", "small_model": "",
			"large_effort": "", "small_effort": "",
			"large_agent": "", "small_agent": "",
		}
	}
	return out
}

func DefaultModels() Models {
	return Models{
		Kanban:      cloneNested(kanbanModelDefaults()),
		Review:      cloneNested(reviewModelDefaults()),
		ReviewRoles: defaultReviewRoles(),
		Chat:        cloneNested(chatModelDefaults()),
	}
}

func DefaultTUI() TUI {
	return TUI{
		Columns:        DefaultTUIColumns,
		MinColumnWidth: DefaultTUIMinColumnWidth,
		Refresh:        DefaultTUIRefresh,
		Theme:          "auto",
		ThemeLight:     DefaultTUIThemeLight,
		ThemeDark:      DefaultTUIThemeDark,
	}
}

// ReviewModelFor resolves overrides only for their owning reviewer. Bound entries
// inherit empty values directly from that agent, never from legacy role keys.
// Unbound legacy entries belong to the configured reviewer at the requested scale.
func ReviewModelFor(cfg *Config, agent, role, scale string) (model, effort string) {
	if cfg == nil {
		return "", ""
	}
	agentEntry := cfg.Models.Review[agent]
	roleEntry := cfg.Models.ReviewRoles[role]
	owner := roleEntry[scale+"_agent"]
	if owner != "" && owner != agent || owner == "" && ReviewerFor(cfg, scale, role) != agent {
		return agentEntry["model"], agentEntry["effort"]
	}
	model = roleEntry[scale+"_model"]
	if model == "" && owner == "" {
		model = roleEntry["model"]
	}
	if model == "" {
		model = agentEntry["model"]
	}
	effort = roleEntry[scale+"_effort"]
	if effort == "" && owner == "" {
		effort = roleEntry["effort"]
	}
	if effort == "" {
		effort = agentEntry["effort"]
	}
	return model, effort
}

func DefaultLauncher() string {
	if runtime.GOOS == "windows" {
		return "console"
	}
	return "auto"
}

func DefaultConfig() *Config {
	agent := defaultAgentName()
	agents := make(map[string]string, len(TaskScales))
	for _, scale := range TaskScales {
		agents[scale] = agent
	}
	return &Config{
		SchemaVersion:   SchemaVersion,
		WelcomeComplete: false,
		KanbanAgent:     agent,
		KanbanAgents:    agents,
		ChatAgent:       agent,
		Launcher:        DefaultLauncher(),
		Reviewers:       DefaultReviewers(),
		ReviewStages:    DefaultReviewStages(),
		Rules:           DefaultRules(true),
		Models:          DefaultModels(),
		TUI:             DefaultTUI(),
		Issue:           DefaultIssue(),
		Git:             DefaultGit(),
		Language:        "en",
		AgentLanguage:   DefaultAgentLanguage("en"),
		InstallRules:    true,
	}
}

// agentLanguageDefaults maps an interface language to the agent communication language a config
// without agent_language falls back to.
var agentLanguageDefaults = map[string]string{
	"cn": "zh-CN",
	"en": "en",
	"ja": "ja",
}

// DefaultAgentLanguage returns the agent communication language derived from an interface language.
func DefaultAgentLanguage(language string) string {
	if value, ok := agentLanguageDefaults[language]; ok {
		return value
	}
	return agentLanguageDefaults["en"]
}

// agentLanguageMaxRunes bounds agent_language so it stays a language name rather than a prompt.
const agentLanguageMaxRunes = 64

// ValidateAgentLanguage normalizes an agent communication language given outside config.json, such as the
// LANGUAGE of a new task card, under the same rule as the agent_language key.
func ValidateAgentLanguage(value string) (string, error) {
	return validateAgentLanguage(value)
}

// validateAgentLanguage accepts any non-empty single-line language name of at most 64 characters, such as en, zh-CN, or ja.
func validateAgentLanguage(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", configErrorf("config.agent_language_invalid")
	}
	// Scan before trimming: TrimSpace would silently swallow a line separator at either end.
	for _, r := range text {
		// Control characters cover CR, LF, NEL and DEL; the two Unicode separators are category Z, so they are named explicitly.
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", configErrorf("config.agent_language_invalid")
		}
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > agentLanguageMaxRunes {
		return "", configErrorf("config.agent_language_invalid")
	}
	return text, nil
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

// validateAgentRef accepts a role's agent name: a known agent, or a well-formed name
// this version does not know, which a newer version may have written. The unknown
// name stays in the configuration and only fails when the role is used.
func validateAgentRef(value any, names []string, field string) (string, error) {
	text, ok := value.(string)
	if !ok || (!contains(names, text) && !ValidAgentName(text)) {
		return "", choiceError(field, strings.Join(names, ", "))
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
	return validateLauncherIn(value, LauncherNames)
}

// validateLauncherIn is validateLauncher against the launcher names that
// names returns.
func validateLauncherIn(value any, names func() []string) (string, error) {
	launcher, err := validateChoice(value, names(), "launcher")
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

func validateKanbanAgents(raw any, defaultAgent string, choices ...[]string) (map[string]string, error) {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, configErrorf("config.kanban_agents_must_be_a_json_object")
	}
	agents := make(map[string]string, len(TaskScales))
	for _, scale := range TaskScales {
		agents[scale] = defaultAgent
	}
	for _, scale := range TaskScales {
		if _, exists := obj[scale]; !exists {
			continue
		}
		names := ExecutionAgents
		if len(choices) > 0 {
			names = choices[0]
		}
		agent, err := validateAgentRef(obj[scale], names, "kanban_agents."+scale)
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

// KanbanAgentsInUse lists the execution agents bound to task scales,
// deduplicated, large first then small. kander config kanban-model lines and
// the Options summary use this set so a Chat-only agent is not labeled as a
// kanban model.
func KanbanAgentsInUse(cfg *Config) []string {
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

// ExecutionAgentsInUse lists the execution agents that will actually be launched,
// deduplicated, large task first, then small, then the Chat Agent when it is
// not already in that set. Doctor uses this inventory only for configured-agent
// availability checks; the rules integration coverage set lives in
// internal/install (IntegrationAgents).
func ExecutionAgentsInUse(cfg *Config) []string {
	out := KanbanAgentsInUse(cfg)
	seen := map[string]struct{}{}
	for _, agent := range out {
		seen[agent] = struct{}{}
	}
	if chat := ChatAgentFor(cfg); chat != "" {
		if _, ok := seen[chat]; !ok {
			out = append(out, chat)
		}
	}
	return out
}

func validateModels(raw any, probe *Config) (Models, error) {
	models := DefaultModels()
	customModelDefaults(probe.Agents, &models)
	names := AgentNames(probe)
	reviewNames := ReviewAgentNames(probe)
	obj, ok := raw.(map[string]any)
	if !ok {
		return Models{}, configErrorf("config.models_must_be_a_json_object")
	}
	models.Chat = map[string]map[string]string{}
	sections := []struct {
		name string
		// agents lists the keys allowed in this section: the first two sections use agent names, review_roles uses review role names.
		agents []string
		dest   map[string]map[string]string
		// When allowEmpty is true the values of this section may be empty strings; review_roles uses an empty string to mean inherit.
		allowEmpty bool
	}{
		{"kanban", names, models.Kanban, false},
		{"review", reviewNames, models.Review, false},
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
		if section.name == "review_roles" {
			folded, err := validateAndFoldReviewRoleModels(provided)
			if err != nil {
				return Models{}, err
			}
			for role, entry := range folded {
				fields := section.dest[role]
				for field, text := range entry {
					fields[field] = text
				}
			}
			continue
		}
		allowed := map[string]struct{}{}
		for _, agent := range section.agents {
			allowed[agent] = struct{}{}
		}
		for agent, entryRaw := range provided {
			// An agent this version does not know keeps its entry in the file; see scanIgnored.
			if _, ok := allowed[agent]; !ok {
				continue
			}
			entry, ok := entryRaw.(map[string]any)
			if !ok {
				return Models{}, configErrorf(
					"config.models_must_be_a_json_object_3", section.name, agent,
				)
			}
			fields := section.dest[agent]
			for field, value := range entry {
				if _, known := fields[field]; !known {
					continue
				}
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
				if section.name == "review_roles" && strings.HasSuffix(field, "_agent") && text != "" && !ValidAgentName(text) {
					return Models{}, agentDefinitionError("models.review_roles."+agent+"."+field, Text("config.agent_name"))
				}
				fields[field] = text
			}
		}
	}
	if providedRaw, exists := obj["chat"]; exists {
		provided, ok := providedRaw.(map[string]any)
		if !ok {
			return Models{}, configErrorf("config.models_must_be_a_json_object_2", "chat")
		}
		chat, err := validateChatModels(provided, names, probe)
		if err != nil {
			return Models{}, err
		}
		models.Chat = chat
	}
	return models, nil
}

// pruneUnusableModels drops the default entries of built-in agents an unusable
// agents entry took out of service. The file keeps the user's own values for them
// (see scanIgnored), and a default left here would shadow those on save.
func pruneUnusableModels(models *Models, probe *Config) {
	for name := range probe.UnusableAgents {
		delete(models.Kanban, name)
		delete(models.Review, name)
		delete(models.Chat, name)
	}
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
	// A section that holds only keys this version does not know, such as a project
	// overlay that sets a newer tui key over a scope file without a tui section, is
	// the same as no section: it must not fail on the missing required keys.
	if known := jsonKeys(reflect.TypeOf(TUI{})); len(obj) > 0 && !slices.ContainsFunc(slices.Collect(maps.Keys(obj)), func(key string) bool { return containsKey(known, key) }) {
		return DefaultTUI(), nil
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
	compact := false
	if raw, exists := obj["compact"]; exists {
		var ok bool
		compact, ok = raw.(bool)
		if !ok {
			return TUI{}, configErrorf("config.tui_compact_must_be_a_boolean")
		}
	}
	theme, err := validateChoice(obj["theme"], TUIThemes, "tui.theme")
	if err != nil {
		return TUI{}, err
	}
	themeLight, err := validateAutoVariantTheme(obj, "theme_light", DefaultTUIThemeLight)
	if err != nil {
		return TUI{}, err
	}
	themeDark, err := validateAutoVariantTheme(obj, "theme_dark", DefaultTUIThemeDark)
	if err != nil {
		return TUI{}, err
	}
	return TUI{
		Compact: compact, Columns: columns, MinColumnWidth: width, Refresh: refresh, Single: single,
		Theme: theme, ThemeLight: themeLight, ThemeDark: themeDark,
	}, nil
}

// validateAutoVariantTheme reads an optional "auto" variant key: a missing key
// keeps the built-in default so older configs stay valid, while a present key
// must name a concrete theme. The light/dark family is not checked here, so a
// hand-edited cross-family choice is honored.
func validateAutoVariantTheme(obj map[string]any, key, fallback string) (string, error) {
	raw, exists := obj[key]
	if !exists {
		return fallback, nil
	}
	return validateChoice(raw, NamedTUIThemes(), "tui."+key)
}

// NamedTUIThemes lists the concrete themes, that is TUIThemes without "auto".
func NamedTUIThemes() []string {
	return slices.DeleteFunc(slices.Clone(TUIThemes), func(name string) bool { return name == "auto" })
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
	return validateWithLaunchers(raw, LauncherNames)
}

// validateWithLaunchers is Validate with the launcher checked against the
// names that launchers returns.
func validateWithLaunchers(raw any, launchers func() []string) (*Config, error) {
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
	var definitions map[string]AgentDefinition
	var unusable map[string]unusableAgent
	var err error
	if raw, exists := obj["agents"]; exists {
		definitions, unusable, err = agentEntries(raw)
		if err != nil {
			return nil, err
		}
	}
	probe := &Config{Agents: definitions, UnusableAgents: unusableReasons(unusable), UnusablePolicy: unusablePolicies(unusable)}
	names := AgentNames(probe)
	kanbanAgent, err := validateAgentRef(obj["kanban_agent"], names, "kanban_agent")
	if err != nil {
		return nil, err
	}
	var kanbanAgents map[string]string
	if _, exists := obj["kanban_agents"]; exists {
		kanbanAgents, err = validateKanbanAgents(obj["kanban_agents"], kanbanAgent, names)
		if err != nil {
			return nil, err
		}
	} else {
		kanbanAgents = make(map[string]string, len(TaskScales))
		for _, scale := range TaskScales {
			kanbanAgents[scale] = kanbanAgent
		}
	}
	chatAgent := ""
	if _, exists := obj["chat_agent"]; exists {
		chatAgent, err = validateAgentRef(obj["chat_agent"], names, "chat_agent")
		if err != nil {
			return nil, err
		}
	}
	launcher, err := validateLauncherIn(obj["launcher"], launchers)
	if err != nil {
		return nil, err
	}
	extendedTimeouts, err := validateExtendedLaunchTimeouts(obj)
	if err != nil {
		return nil, err
	}
	installRules, err := validateInstallRules(obj)
	if err != nil {
		return nil, err
	}
	reviewersRaw, ok := asObject(obj["reviewers"])
	if !ok {
		return nil, configErrorf("config.reviewers_must_be_a_json_object")
	}
	reviewNames := ReviewAgentNames(probe)
	reviewers, err := validateReviewers(reviewersRaw, probe, reviewNames)
	if err != nil {
		return nil, err
	}
	var stages map[string]map[string]string
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
		models, err = validateModels(obj["models"], probe)
		if err != nil {
			return nil, err
		}
	} else {
		models = DefaultModels()
		customModelDefaults(definitions, &models)
	}
	tui := DefaultTUI()
	if _, exists := obj["tui"]; exists {
		tui, err = validateTUI(obj["tui"])
		if err != nil {
			return nil, err
		}
	}
	issue := DefaultIssue()
	if _, exists := obj["issue"]; exists {
		issue, err = validateIssue(obj["issue"])
		if err != nil {
			return nil, err
		}
	}
	var server Server
	if _, exists := obj["server"]; exists {
		if server, err = validateServer(obj["server"]); err != nil {
			return nil, err
		}
	}
	var project Project
	if _, exists := obj["project"]; exists {
		if project, err = validateProject(obj["project"]); err != nil {
			return nil, err
		}
	}
	git := DefaultGit()
	if _, exists := obj["git"]; exists {
		git, err = validateGit(obj["git"])
		if err != nil {
			return nil, err
		}
	}
	languageRaw, hasLanguage := obj["language"]
	if !hasLanguage {
		languageRaw = "en"
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
	pruneUnusableModels(&models, probe)
	cfg := &Config{
		SchemaVersion:   SchemaVersion,
		WelcomeComplete: welcome,
		KanbanAgent:     kanbanAgent,
		KanbanAgents:    kanbanAgents,
		ChatAgent:       chatAgent,
		Launcher:        launcher,
		Reviewers:       reviewers,
		ReviewStages:    stages,
		Rules:           rules,
		Models:          models,
		TUI:             tui,
		Issue:           issue,
		Server:          server,
		Project:         project,
		Git:             git,
		Language:        language,
		AgentLanguage:   agentLanguage,
		Agents:          definitions,

		ExtendedLaunchTimeouts: extendedTimeouts,
		InstallRules:           installRules,

		UnusableAgents: probe.UnusableAgents,
		UnusablePolicy: probe.UnusablePolicy,
	}
	cfg.Ignored = scanIgnored(obj, definitions, unusable)
	applyChatFallbacks(cfg, obj)
	return cfg, nil
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
