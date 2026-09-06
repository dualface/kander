package menu

import (
	"errors"
	"strconv"

	"github.com/dualface/kander/internal/config"
	"strings"
)

// Choice is one selectable value in the options panel.
type Choice struct {
	Value string
	Label string
}

// ModelField is one editable field of "model and reasoning effort".
type ModelField struct {
	// Label is the full label used by the line-based menu, e.g. "kanban Codex model".
	Label string
	// Short is the short label used when nested under an agent option, e.g. "Codex model".
	Short string
	// Agent is the object this field belongs to (an agent on the execution side, a role on the review side),
	// and together with field it forms the deduplication key.
	Agent  string
	Prompt string
	entry  map[string]string
	field  string
}

// Key uniquely identifies one config item. When two task scales or two review roles pick the same agent,
// they point at the very same config entry, which must appear only once in the UI.
func (f ModelField) Key() string { return f.Agent + "." + f.field }

// Value returns the current value of the field; an empty string means the CLI default is used.
func (f ModelField) Value() string { return f.entry[f.field] }

// Set writes the field.
func (f ModelField) Set(value string) { f.entry[f.field] = value }

// Session carries the editable config of the options panel plus the one-shot environment probe results.
type Session struct {
	// Config is the config being edited; it does not reach disk before saving.
	Config *config.Config
	// Warnings are environment warnings raised while constructing the session; the caller decides how to display them.
	Warnings []ReportLine

	existing *config.Config
	agents   map[string]agentState
	exec     []Choice
	review   []Choice
}

// NewSession probes agents and launchers, and prepares the editable config from existing.
// configValid means a schema-validated config already exists on disk.
func NewSession(existing *config.Config, configValid bool) (*Session, error) {
	session := &Session{existing: existing}
	var err error
	session.Warnings = CaptureReport(func() {
		err = session.prepare(configValid)
	})
	// A session is returned even on error, so the caller can still display the environment warnings collected so far.
	return session, err
}

func (s *Session) prepare(configValid bool) error {
	s.agents = findAgents()
	labels := agentLabels()
	for name, state := range s.agents {
		if state.Path != "" && !agentUsable(state) {
			warning(config.Text(
				"menu.version_failed_unavailable_as_a_new_choice", labels[name], state.Path,
			))
		}
	}
	for _, name := range config.ExecutionAgents {
		state := s.agents[name]
		if agentUsable(state) {
			s.exec = append(s.exec, Choice{Value: name, Label: labels[name] + " (" + state.Version + ")"})
		}
	}
	if len(s.exec) == 0 && !s.existing.WelcomeComplete {
		return errors.New(config.Text(
			"menu.no_usable_agent_found_version_must_succeed_install_codex",
		))
	}
	for _, name := range config.ReviewAgents {
		if agentUsable(s.agents[name]) {
			s.review = append(s.review, Choice{Value: name, Label: labels[name] + " (" + s.agents[name].Version + ")"})
		}
	}
	if len(s.review) == 0 && !s.existing.WelcomeComplete {
		return errors.New(config.Text(
			"menu.no_usable_reviewer_found_install_codex_claude_grok_or",
		))
	}

	cfg := config.DefaultConfig()
	cfg.Models = copyModels(s.existing.Models)
	cfg.KanbanAgent = s.existing.KanbanAgent
	cfg.AgentLanguage = s.existing.AgentLanguage
	cfg.KanbanAgents = map[string]string{}
	for k, v := range s.existing.KanbanAgents {
		cfg.KanbanAgents[k] = v
	}
	if !s.existing.WelcomeComplete {
		if !agentUsable(s.agents[cfg.KanbanAgent]) {
			cfg.KanbanAgent = s.exec[0].Value
		}
		for _, scale := range config.TaskScales {
			if !agentUsable(s.agents[cfg.KanbanAgents[scale]]) {
				cfg.KanbanAgents[scale] = cfg.KanbanAgent
			}
		}
	}
	cfg.Reviewers = map[string]string{}
	for _, role := range config.ReviewRoles {
		previous := s.existing.Reviewers[role]
		cfg.Reviewers[role] = previous
		if !s.existing.WelcomeComplete && !agentUsable(s.agents[previous]) {
			cfg.Reviewers[role] = s.review[0].Value
		}
	}
	cfg.Launcher = s.existing.Launcher
	s.normalizeLauncher(cfg)
	cfg.TUI = s.existing.TUI
	cfg.Rules = s.existing.Rules.Clone()
	cfg.ReviewStages = map[string]string{}
	for k, v := range s.existing.ReviewStages {
		cfg.ReviewStages[k] = v
	}
	if configValid {
		if stored := config.ConfiguredLanguage(); stored != "" {
			cfg.Language = stored
		} else {
			cfg.Language = config.ResolveLanguage()
		}
	} else {
		cfg.Language = config.ResolveLanguage()
	}
	config.BindConfigLanguage(cfg)
	s.Config = cfg
	return nil
}

func (s *Session) normalizeLauncher(cfg *config.Config) {
	if s.existing.WelcomeComplete {
		return
	}
	switch {
	case isWindowsOS() && (cfg.Launcher == "tmux" || cfg.Launcher == "tmux-session"):
		cfg.Launcher = "console"
		warning(config.Text(
			"menu.windows_does_not_support_tmux_using_console",
		))
	case (cfg.Launcher == "tmux" || cfg.Launcher == "tmux-session") && lookPath("tmux") == "":
		cfg.Launcher = "foreground"
		warning(config.Text(
			"menu.tmux_is_not_installed_using_foreground_the_launcher_menu",
		))
	// On POSIX auto holds as long as tmux exists, so a missing herdr must not
	// rewrite it; on Windows auto can only land on herdr, so a missing herdr does.
	case (cfg.Launcher == "herdr" || (cfg.Launcher == "auto" && isWindowsOS())) && lookPath("herdr") == "":
		switch {
		case isWindowsOS():
			cfg.Launcher = "console"
		case lookPath("tmux") != "":
			cfg.Launcher = "tmux"
		default:
			cfg.Launcher = "foreground"
		}
		warning(config.Text(
			"menu.herdr_is_not_installed_using", cfg.Launcher,
		))
	}
}

// ExecutionChoices returns the execution agents currently available.
func (s *Session) ExecutionChoices() []Choice {
	return append([]Choice{}, s.exec...)
}

// ExecutionChoicesFor prepends the current value to the available list, so a configured but unavailable agent is not silently replaced.
func (s *Session) ExecutionChoicesFor(current string) []Choice {
	return choicesWithCurrent(s.exec, current)
}

// ReviewerChoicesFor returns the candidate reviewers for one review role.
func (s *Session) ReviewerChoicesFor(current string) []Choice {
	return choicesWithCurrent(s.review, current)
}

// SetExecutionAgent sets the execution agent of one task scale (large/small).
func (s *Session) SetExecutionAgent(scale, agent string) {
	s.Config.KanbanAgents[scale] = agent
	s.Config.KanbanAgent = s.Config.KanbanAgents["large"]
}

// SetReviewer sets the reviewer of one review role.
func (s *Session) SetReviewer(role, agent string) {
	s.Config.Reviewers[role] = agent
}

// SetReviewStage sets the default stage policy of one review role.
func (s *Session) SetReviewStage(role, mode string) {
	s.Config.ReviewStages[role] = mode
}

// SetLanguage sets the default output language and applies it immediately.
func (s *Session) SetLanguage(language string) {
	s.Config.Language = language
	config.BindConfigLanguage(s.Config)
}

// SetAgentLanguage sets the language the agent uses when talking to the user.
func (s *Session) SetAgentLanguage(language string) {
	s.Config.AgentLanguage = strings.TrimSpace(language)
}

// SetLauncher sets the launcher.
func (s *Session) SetLauncher(launcher string) {
	s.Config.Launcher = launcher
}

// LauncherInstallValue is the value of the "install tmux" item in the launcher list.
const LauncherInstallValue = "install-tmux"

// LauncherChoices returns the launchers available on the current platform, plus the install item when tmux is missing.
func (s *Session) LauncherChoices() []Choice {
	if isWindowsOS() {
		return windowsLauncherChoices(s.Config)
	}
	foreground := Choice{Value: "foreground", Label: config.Text("menu.foreground_in_this_terminal")}
	choices := []Choice{autoLauncherChoice()}
	if lookPath("tmux") != "" {
		choices = append(choices, tmuxLauncherChoices()...)
		choices = append(choices, herdrLauncherChoices(s.Config)...)
		return append(choices, foreground)
	}
	unavailable := config.Text("menu.not_currently_installed")
	for _, item := range tmuxLauncherChoices() {
		if s.Config.Launcher == item.Value {
			choices = append(choices, Choice{Value: item.Value, Label: item.Label + unavailable})
		}
	}
	choices = append(choices, herdrLauncherChoices(s.Config)...)
	return append(choices, foreground, Choice{
		Value: LauncherInstallValue,
		Label: config.Text("menu.install_tmux_and_use_a_new_window"),
	})
}

// TmuxModeChoices returns the two tmux launchers available once tmux is installed.
func (s *Session) TmuxModeChoices() []Choice {
	return tmuxLauncherChoices()
}

// InstallTmux installs tmux through the system package manager. It takes over the current terminal, so the TUI must suspend the screen first.
func (s *Session) InstallTmux() ([]ReportLine, bool) {
	installed := false
	lines := CaptureReport(func() {
		installed = installTmux()
	})
	return lines, installed
}

// ReviewStageChoices returns the candidate review stage policies.
func (s *Session) ReviewStageChoices() []Choice {
	return reviewStageChoices()
}

// LanguageChoices returns the candidate default languages.
func (s *Session) LanguageChoices() []Choice {
	return languageChoices()
}

// ModelFields builds the editable model fields for the execution agents and reviewers actually in use.
func (s *Session) ModelFields() []ModelField {
	return append(s.ExecutionModelFields(), s.ReviewModelFields()...)
}

// kanbanModelField builds one model field on the execution side.
func (s *Session) kanbanModelField(agent, field, label, short, prompt string) ModelField {
	return ModelField{
		Label:  label,
		Short:  short,
		Agent:  agent,
		Prompt: prompt,
		entry:  s.Config.Models.Kanban[agent],
		field:  field,
	}
}

// ExecutionModelFieldsFor returns the model fields of the agent currently selected for one task scale (large/small).
// Model and reasoning effort are stored per scale, so large and small tasks each keep their own
// independently editable values even when they select the same agent.
func (s *Session) ExecutionModelFieldsFor(scale string) []ModelField {
	agent := s.Config.KanbanAgents[scale]
	if agent == "" {
		return nil
	}
	// When a legacy config only has the shared model key, the scale models are filled in with the same concrete value,
	// so the UI never shows a field that looks empty but actually has a value.
	if entry := s.Config.Models.Kanban[agent]; entry != nil {
		if entry[scale+"_model"] == "" && entry["model"] != "" {
			entry[scale+"_model"] = entry["model"]
		}
	}
	label := agentLabels()[agent]
	scaleLabel := config.Text("menu.large_task")
	if scale == "small" {
		scaleLabel = config.Text("menu.small_task")
	}
	fields := []ModelField{s.kanbanModelField(agent, scale+"_model",
		config.Text("menu.kanban_model", label, scaleLabel),
		config.Text("menu.model", label, scaleLabel),
		config.Text("menu.full_model_id_for_s", scaleLabel))}
	if agent == "cursor" {
		return fields
	}
	return append(fields, s.kanbanModelField(agent, scale+"_effort",
		config.Text("menu.kanban_reasoning_effort", label, scaleLabel),
		config.Text("menu.effort", label, scaleLabel),
		config.Text("menu.reasoning_effort_for_s", scaleLabel)))
}

// ExecutionModelFields is the flattened, order-preserving deduplication of the per-scale fields, for the line-based menu.
func (s *Session) ExecutionModelFields() []ModelField {
	var out []ModelField
	seen := map[string]struct{}{}
	for _, scale := range config.TaskScales {
		for _, field := range s.ExecutionModelFieldsFor(scale) {
			if _, ok := seen[field.Key()]; ok {
				continue
			}
			seen[field.Key()] = struct{}{}
			out = append(out, field)
		}
	}
	return out
}

// ReviewModelFieldsFor returns the model fields of one review role.
// Every role stores its own concrete values, so what the UI shows is what is used, with no hidden inheritance layer;
// while a role has no value yet, it is filled in from the default of its selected reviewer.
func (s *Session) ReviewModelFieldsFor(role string) []ModelField {
	reviewer := s.Config.Reviewers[role]
	if reviewer == "" {
		return nil
	}
	entry := s.seedReviewRole(role, reviewer)
	fields := []ModelField{{
		Label:  config.Text("menu.review_model", role),
		Short:  config.Text("menu.model_2", role),
		Agent:  role,
		Prompt: config.Text("menu.which_model_should_use", role),
		entry:  entry,
		field:  "model",
	}}
	if reviewer == "cursor" {
		return fields
	}
	return append(fields, ModelField{
		Label:  config.Text("menu.review_reasoning_effort", role),
		Short:  config.Text("menu.effort_2", role),
		Agent:  role,
		Prompt: config.Text("menu.reasoning_effort_for", role),
		entry:  entry,
		field:  "effort",
	})
}

// seedReviewRole makes sure the role owns its values: whichever item is missing is filled in from that reviewer's default.
func (s *Session) seedReviewRole(role, reviewer string) map[string]string {
	entry := s.Config.Models.ReviewRoles[role]
	if entry == nil {
		entry = map[string]string{}
		s.Config.Models.ReviewRoles[role] = entry
	}
	agentEntry := s.Config.Models.Review[reviewer]
	if entry["model"] == "" {
		entry["model"] = agentEntry["model"]
	}
	if _, ok := agentEntry["effort"]; ok && entry["effort"] == "" {
		entry["effort"] = agentEntry["effort"]
	}
	return entry
}

// ResetReviewRoleModel resets the model and reasoning effort of one role to the defaults of its new reviewer.
// Call it when a role changes reviewer: the old values were configured for the old reviewer and would otherwise be misattributed.
func (s *Session) ResetReviewRoleModel(role string) {
	s.Config.Models.ReviewRoles[role] = map[string]string{}
	s.seedReviewRole(role, s.Config.Reviewers[role])
}

// ReviewModelFields is the flattened, order-preserving deduplication of the per-role fields, for the line-based menu.
func (s *Session) ReviewModelFields() []ModelField {
	var out []ModelField
	seen := map[string]struct{}{}
	for _, role := range config.ReviewRoles {
		for _, field := range s.ReviewModelFieldsFor(role) {
			if _, ok := seen[field.Key()]; ok {
				continue
			}
			seen[field.Key()] = struct{}{}
			out = append(out, field)
		}
	}
	return out
}

// Finish wraps up before saving: it reports how the rules were installed and marks initialization complete.
func (s *Session) Finish() ([]ReportLine, error) {
	var err error
	lines := CaptureReport(func() {
		err = s.finish()
	})
	return lines, err
}

func (s *Session) finish() error {
	if err := config.ValidateRules(s.Config.Rules); err != nil {
		return err
	}
	labels := agentLabels()
	paths, err := currentPaths()
	if err != nil {
		return err
	}
	entry := rulesEntry(paths)
	for _, selected := range config.ExecutionAgentsInUse(s.Config) {
		integrated, detail := rulesIntegration(selected, paths)
		if integrated {
			success(config.Text("menu.is_connected_to_kander_rules", labels[selected], detail))
		} else {
			warning(config.Text("menu.is_not_connected_to_kander_rules", labels[selected], detail))
		}
		hint(config.Text(
			"menu.follow_the_readme_integration_section_and_point_the_rules", entry,
		))
	}
	note(config.Text(
		"menu.note_kanban_start_uses_the_agent_s_no_confirmation",
	))
	note(RulesSessionNotice())
	s.Config.WelcomeComplete = true
	return nil
}

// Save writes the current config to disk and returns the config file path.
func (s *Session) Save() (string, error) {
	path, err := config.SaveIfUnchanged(s.Config, s.existing)
	if err == nil {
		s.existing = config.Clone(s.Config)
	}
	return path, err
}

// Summary returns the "current configuration" overview shown in the menu title and at the top of the panel.
func (s *Session) Summary() []string {
	cfg := s.Config
	lines := []string{config.Text("menu.current_configuration")}
	lines = append(lines, config.Text("rules.modules")+": "+config.FormatRulesSummary(cfg.Rules))
	for _, agent := range config.ExecutionAgentsInUse(cfg) {
		entry := cfg.Models.Kanban[agent]
		lines = append(lines, "  kanban "+agent+": "+config.FormatKanbanModelSummary(agent, entry))
	}
	seen := map[string]struct{}{}
	for _, role := range config.ReviewRoles {
		reviewer := cfg.Reviewers[role]
		if _, ok := seen[reviewer]; ok {
			continue
		}
		seen[reviewer] = struct{}{}
		entry := cfg.Models.Review[reviewer]
		lines = append(lines, "  review "+reviewer+": "+config.FormatReviewModelSummary(entry))
	}
	return lines
}

func modelFieldValueLabel(value string) string {
	if value == "" {
		return config.Text("config.cli_default")
	}
	return value
}

func choiceLabelFor(choices []Choice, value string) string {
	for _, item := range choices {
		if item.Value == value {
			return item.Label
		}
	}
	return value
}

func indexLabel(index int) string {
	return strconv.Itoa(index + 1)
}

// NewSessionForTest builds a session that skips environment probing; it is for tests only.
// The candidate agents are simply every value the schema allows, and no agent executable is invoked.
func NewSessionForTest(existing *config.Config) (*Session, error) {
	session := &Session{existing: existing}
	labels := agentLabels()
	for _, name := range config.ExecutionAgents {
		session.exec = append(session.exec, Choice{Value: name, Label: labels[name]})
	}
	for _, name := range config.ReviewAgents {
		session.review = append(session.review, Choice{Value: name, Label: labels[name]})
	}
	cfg := config.DefaultConfig()
	cfg.Models = copyModels(existing.Models)
	cfg.KanbanAgent = existing.KanbanAgent
	cfg.KanbanAgents = cloneStrings(existing.KanbanAgents)
	cfg.Reviewers = cloneStrings(existing.Reviewers)
	cfg.ReviewStages = cloneStrings(existing.ReviewStages)
	cfg.Rules = existing.Rules.Clone()
	cfg.Launcher = existing.Launcher
	cfg.TUI = existing.TUI
	cfg.Language = existing.Language
	cfg.AgentLanguage = existing.AgentLanguage
	session.Config = cfg
	return session, nil
}

func cloneStrings(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
