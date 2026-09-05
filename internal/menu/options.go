package menu

import (
	"errors"
	"strconv"

	"github.com/dualface/kander/internal/config"
)

// Choice 是选项面板里的一个可选值.
type Choice struct {
	Value string
	Label string
}

// ModelField 是「模型与推理档位」里的一个可编辑字段.
type ModelField struct {
	// Label 是行式菜单用的完整标签, 例如「kanban Codex 模型」.
	Label string
	// Short 是嵌在 Agent 选项下方时用的短标签, 例如「Codex 模型」.
	Short string
	// Agent 是这个字段所属的对象 (执行侧是 Agent, 审核侧是角色),
	// 与 field 一起构成去重用的键.
	Agent  string
	Prompt string
	entry  map[string]string
	field  string
}

// Key 唯一标识一个配置项. 两个任务规模或两个审核角色选了同一个 Agent 时,
// 它们指向的其实是同一份配置, 界面上只应出现一次.
func (f ModelField) Key() string { return f.Agent + "." + f.field }

// Value 返回该字段当前值; 空串表示沿用 CLI 默认.
func (f ModelField) Value() string { return f.entry[f.field] }

// Set 写入该字段.
func (f ModelField) Set(value string) { f.entry[f.field] = value }

// Session 承载选项面板的可编辑配置和一次性环境探测结果.
type Session struct {
	// Config 是本次编辑中的配置, 保存前不落盘.
	Config *config.Config
	// Warnings 是构造期间的环境告警, 由调用方决定如何展示.
	Warnings []ReportLine

	existing *config.Config
	agents   map[string]agentState
	exec     []Choice
	review   []Choice
}

// NewSession 探测 Agent 与启动方式, 并以 existing 为基准准备可编辑配置.
// configValid 表示磁盘上已有一份通过 schema 校验的配置.
func NewSession(existing *config.Config, configValid bool) (*Session, error) {
	session := &Session{existing: existing}
	var err error
	session.Warnings = CaptureReport(func() {
		err = session.prepare(configValid)
	})
	// 出错时也返回 session, 让调用方仍能展示已收集到的环境告警.
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
	case isWindowsOS() && cfg.Launcher != "console" && cfg.Launcher != "foreground":
		cfg.Launcher = "console"
		warning(config.Text(
			"menu.native_windows_does_not_support_auto_tmux_or_herdr",
		))
	case (cfg.Launcher == "tmux" || cfg.Launcher == "tmux-session") && lookPath("tmux") == "":
		cfg.Launcher = "foreground"
		warning(config.Text(
			"menu.tmux_is_not_installed_using_foreground_the_launcher_menu",
		))
	case cfg.Launcher == "herdr" && lookPath("herdr") == "":
		if lookPath("tmux") != "" {
			cfg.Launcher = "tmux"
		} else {
			cfg.Launcher = "foreground"
		}
		warning(config.Text(
			"menu.herdr_is_not_installed_using", cfg.Launcher,
		))
	}
}

// ExecutionChoices 返回当前可用的执行 Agent.
func (s *Session) ExecutionChoices() []Choice {
	return append([]Choice{}, s.exec...)
}

// ExecutionChoicesFor 在可用列表前补上当前值, 避免已配置但不可用的 Agent 被静默改掉.
func (s *Session) ExecutionChoicesFor(current string) []Choice {
	return choicesWithCurrent(s.exec, current)
}

// ReviewerChoicesFor 返回某个审核角色的候选 Reviewer.
func (s *Session) ReviewerChoicesFor(current string) []Choice {
	return choicesWithCurrent(s.review, current)
}

// SetExecutionAgent 设置某个任务规模 (large/small) 的执行 Agent.
func (s *Session) SetExecutionAgent(scale, agent string) {
	s.Config.KanbanAgents[scale] = agent
	s.Config.KanbanAgent = s.Config.KanbanAgents["large"]
}

// SetReviewer 设置某个审核角色的 Reviewer.
func (s *Session) SetReviewer(role, agent string) {
	s.Config.Reviewers[role] = agent
}

// SetReviewStage 设置某个审核角色的默认环节策略.
func (s *Session) SetReviewStage(role, mode string) {
	s.Config.ReviewStages[role] = mode
}

// SetLanguage 设置默认输出语言并立即生效.
func (s *Session) SetLanguage(language string) {
	s.Config.Language = language
	config.BindConfigLanguage(s.Config)
}

// SetLauncher 设置启动方式.
func (s *Session) SetLauncher(launcher string) {
	s.Config.Launcher = launcher
}

// LauncherInstallValue 是启动方式列表里「安装 tmux」这一项的取值.
const LauncherInstallValue = "install-tmux"

// LauncherChoices 返回当前平台可选的启动方式; tmux 缺失时附带安装项.
func (s *Session) LauncherChoices() []Choice {
	if isWindowsOS() {
		return windowsLauncherChoices()
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

// TmuxModeChoices 返回安装 tmux 之后可选的两种 tmux 启动方式.
func (s *Session) TmuxModeChoices() []Choice {
	return tmuxLauncherChoices()
}

// InstallTmux 调用系统包管理器安装 tmux. 它会占用当前终端, TUI 必须先挂起屏幕.
func (s *Session) InstallTmux() ([]ReportLine, bool) {
	installed := false
	lines := CaptureReport(func() {
		installed = installTmux()
	})
	return lines, installed
}

// ReviewStageChoices 返回审核环节策略的候选值.
func (s *Session) ReviewStageChoices() []Choice {
	return reviewStageChoices()
}

// LanguageChoices 返回默认语言的候选值.
func (s *Session) LanguageChoices() []Choice {
	return languageChoices()
}

// ModelFields 按当前实际在用的执行 Agent 与 Reviewer 生成可编辑的模型字段.
func (s *Session) ModelFields() []ModelField {
	return append(s.ExecutionModelFields(), s.ReviewModelFields()...)
}

// kanbanModelField 构造一个执行侧的模型字段.
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

// ExecutionModelFieldsFor 返回某个任务规模 (large/small) 当前所选 Agent 的模型字段.
// 模型与推理档位都按规模独立存放, 所以大小任务即便选同一个 Agent,
// 也各有一套可以分别设置的取值.
func (s *Session) ExecutionModelFieldsFor(scale string) []ModelField {
	agent := s.Config.KanbanAgents[scale]
	if agent == "" {
		return nil
	}
	// 旧配置只有共享的 model 键时, 把规模模型补成同样的具体值,
	// 界面上就不会出现「空着但其实有值」的情况.
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

// ExecutionModelFields 是各任务规模字段按顺序去重后的平铺结果, 供行式菜单使用.
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

// ReviewModelFieldsFor 返回某个审核角色的模型字段.
// 每个角色都存自己的一份具体取值, 界面上看到什么就是什么, 没有隐藏的继承层;
// 角色还没有取值时, 用它所选 Reviewer 的默认值填上.
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

// seedReviewRole 保证角色有一份自己的取值: 缺哪一项就用该 Reviewer 的默认值补上.
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

// ResetReviewRoleModel 把某个角色的模型与推理档位重置成新 Reviewer 的默认值.
// 角色换了 Reviewer 时调用: 旧取值是照着旧 Reviewer 配的, 留着会张冠李戴.
func (s *Session) ResetReviewRoleModel(role string) {
	s.Config.Models.ReviewRoles[role] = map[string]string{}
	s.seedReviewRole(role, s.Config.Reviewers[role])
}

// ReviewModelFields 是各审核角色字段按顺序去重后的平铺结果, 供行式菜单使用.
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

// Finish 做保存前的收尾: 报告规则接入情况, 标记初始化完成.
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

// Save 落盘当前配置, 返回配置文件路径.
func (s *Session) Save() (string, error) {
	path, err := config.SaveIfUnchanged(s.Config, s.existing)
	if err == nil {
		s.existing = config.Clone(s.Config)
	}
	return path, err
}

// Summary 返回「当前配置」概览, 供菜单标题与面板顶部显示.
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

// NewSessionForTest 构造一个跳过环境探测的会话, 只供测试使用.
// 候选 Agent 直接取 schema 允许的全部取值, 不调用任何 Agent 可执行文件.
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
