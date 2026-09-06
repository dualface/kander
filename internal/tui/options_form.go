package tui

import (
	"strconv"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

// formBinding 是 Huh 表单绑定的可变取值. Huh 需要稳定指针,
// 因此这里按分区一次性铺平, 表单关闭时再写回 App 与配置会话.
type formBinding struct {
	theme    string
	columns  int
	minWidth int
	refresh  int
	single   bool
	archived bool

	large      string
	small      string
	launcher   string
	prevLaunch string

	reviewers map[string]*string
	stages    map[string]*string

	language       string
	rules          map[string]*bool
	rulePreset     string
	prevRulePreset string

	modelFields []menu.ModelField
	// modelValues 存每个输入框自己的字符串指针. 不能绑切片元素地址:
	// 后续 append 扩容会换底层数组, 先建字段的指针就会悬空.
	modelValues []*string
	modelSeen   map[string]struct{}
	// fieldIndex 记录各选择器在本次表单里排第几个可聚焦字段, 供重建后恢复焦点.
	fieldIndex map[string]int
	// formFields 是本次表单的全部字段 (含用作空行的 Note),
	// 鼠标命中时按它们的实际渲染高度换算行号.
	formFields []huh.Field
	// focusable 是已加入的可聚焦字段数; 空行不计入, 因为 NextField 会跳过它们.
	focusable int
}

// addField 追加一个可聚焦字段.
func (b *formBinding) addField(field huh.Field) {
	b.formFields = append(b.formFields, field)
	b.focusable++
}

// addSpacer 追加一个只占一行的空白字段, 用来把不同分组隔开.
// Note 默认是 skip 的, 不会被 ↑↓ 或 NextField 选中.
func (b *formBinding) addSpacer() {
	b.formFields = append(b.formFields, huh.NewNote())
}

// reset 清掉上一次构建留下的模型字段, 供重建表单时复用同一个绑定结构.
func (b *formBinding) reset() {
	b.modelFields = nil
	b.modelValues = nil
	b.modelSeen = map[string]struct{}{}
	b.fieldIndex = map[string]int{}
	b.formFields = nil
	b.focusable = 0
}

func huhTheme(p palette) *huh.Theme {
	theme := huh.ThemeBase()
	applyHuhPalette(theme, p)
	return theme
}

// applyHuhPalette 把浅色/深色画布写进已有的 Huh 主题.
// Huh 字段只在第一次 WithTheme 时接上这个指针, 之后再 WithTheme 会被忽略,
// 原地修改只更新样式, Huh 的视口缓存仍需在主题切换后通过重建表单刷新.
func applyHuhPalette(theme *huh.Theme, p palette) {
	if theme == nil {
		return
	}
	fresh := huh.ThemeBase()
	// 标签与取值分主次: 标签一律暗色不加粗, 取值加粗, 选中的还上强调色.
	// 否则一个选项的两行同缩进同样式, 第一眼分不出哪行是标签哪行是取值.
	theme.Focused.Title = p.paint(fresh.Focused.Title.Foreground(p.Dim).Bold(false))
	theme.Blurred.Title = p.paint(fresh.Blurred.Title.Foreground(p.Dim).Bold(false))
	theme.Focused.Description = p.paint(fresh.Focused.Description.Foreground(p.Separator))
	theme.Blurred.Description = p.paint(fresh.Blurred.Description.Foreground(p.Separator))
	theme.Focused.SelectSelector = p.paint(fresh.Focused.SelectSelector.Foreground(p.Accent).SetString(focusMarker + " "))
	theme.Focused.SelectedOption = p.paint(fresh.Focused.SelectedOption.Foreground(p.Accent).Bold(true))
	theme.Blurred.SelectedOption = p.paint(fresh.Blurred.SelectedOption.Foreground(p.Base).Bold(true))
	theme.Focused.Option = p.paint(fresh.Focused.Option.Foreground(p.Base))
	theme.Blurred.Option = p.paint(fresh.Blurred.Option.Foreground(p.Base))
	theme.Focused.UnselectedOption = p.paint(fresh.Focused.UnselectedOption.Foreground(p.Base))
	theme.Blurred.UnselectedOption = p.paint(fresh.Blurred.UnselectedOption.Foreground(p.Base))
	// 左右箭头只留给聚焦的字段: 它画在字段最左侧, 会顶到缩进外面去,
	// 同时给每一行都加反而更乱. 未聚焦的行靠「标签暗, 取值加粗」区分.
	theme.Focused.PrevIndicator = p.paint(fresh.Focused.PrevIndicator.Foreground(p.Accent))
	theme.Focused.NextIndicator = p.paint(fresh.Focused.NextIndicator.Foreground(p.Accent))
	theme.Focused.TextInput.Text = p.paint(fresh.Focused.TextInput.Text.Foreground(p.Accent).Bold(true))
	theme.Blurred.TextInput.Text = p.paint(fresh.Blurred.TextInput.Text.Foreground(p.Base).Bold(true))
	theme.Focused.TextInput.Placeholder = p.paint(fresh.Focused.TextInput.Placeholder.Foreground(p.Dim))
	theme.Blurred.TextInput.Placeholder = p.paint(fresh.Blurred.TextInput.Placeholder.Foreground(p.Dim))
	theme.Focused.Base = p.paint(fresh.Focused.Base.Foreground(p.Base)).BorderForeground(p.PopupEdge).BorderBackground(p.Bg)
	theme.Blurred.Base = p.paint(fresh.Blurred.Base.Foreground(p.Base)).BorderForeground(p.Dim).BorderBackground(p.Bg)
	theme.Help.ShortKey = p.paint(fresh.Help.ShortKey.Foreground(p.Accent))
	theme.Help.ShortDesc = p.paint(fresh.Help.ShortDesc.Foreground(p.Dim))

	// Huh 默认主题的是非按钮是「黑字浅灰底 / 浅灰字黑底」: 在浅色终端上,
	// 未选中的那个反而变成一整块黑, 看起来像被选中, 三个是非项因此全是反的.
	// 这里改成与顶栏同一套强调色: 选中是实心色块, 未选中只有暗色文字.
	// 字段之间默认隔一个空行, 改成只换行: 空行由本包按分组自己插,
	// 这样同一个 Agent/角色的几项贴在一起, 只有不同分组之间才空一行.
	theme.FieldSeparator = lipgloss.NewStyle().SetString("\n")

	selected, unselected := confirmButtonStyles(p)
	theme.Focused.FocusedButton = selected
	theme.Focused.BlurredButton = unselected
	theme.Blurred.FocusedButton = selected
	theme.Blurred.BlurredButton = unselected
}

func (p *optionsPanel) syncFormTheme(palette palette) {
	if p.formTheme == nil {
		return
	}
	applyHuhPalette(p.formTheme, palette)
	p.spinner.Style = lipgloss.NewStyle().Foreground(palette.Accent).Background(palette.Bg)
}

// pillBorder 是是非按钮的轮廓. 用方括号而不是半块字符:
// 半块要同时依赖字体有这个字形和终端把它的颜色画出来, 两样有一样不成立就什么也看不到;
// 方括号在任何字体, 任何配色, 甚至完全没有颜色时都能看出哪一个被选中.
var pillBorder = lipgloss.Border{Left: "[", Right: "]"}

// confirmButtonStyles 返回是非按钮的选中态与未选中态样式.
// 选中态有方括号轮廓加填色, 未选中态用等宽的隐藏边框占位, 切换时不抖动.
func confirmButtonStyles(p palette) (selected, unselected lipgloss.Style) {
	selected = lipgloss.NewStyle().
		MarginLeft(1).
		Border(pillBorder, false, true).
		BorderForeground(p.Accent).
		Padding(0, 1).
		Foreground(p.ChromeFg).Background(p.ChromeBg).Bold(true)
	unselected = lipgloss.NewStyle().
		MarginLeft(1).
		Border(lipgloss.HiddenBorder(), false, true).
		Padding(0, 1).
		Foreground(p.Dim).Background(p.Bg)
	return selected, unselected
}

// disableFilter 关掉候选项过滤: 候选都很短, 过滤只会吃掉 q 之类的按键.
func disableFilter(keys *huh.KeyMap) {
	keys.Select.Filter = key.NewBinding(key.WithDisabled())
	keys.Select.SetFilter = key.NewBinding(key.WithDisabled())
	keys.Select.ClearFilter = key.NewBinding(key.WithDisabled())
}

// rootKeyMap 用于根菜单. 根菜单是一个条目列表, ↑↓ 在条目间移动, Enter 进入.
func rootKeyMap() *huh.KeyMap {
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", t("tui.close")))
	disableFilter(keys)
	return keys
}

// sectionKeyMap 用于分区表单. 分区是设置列表而不是向导:
// ↑↓ 在字段间移动, ←→ 就地改值, 不必一路 Enter 走到底.
func sectionKeyMap() *huh.KeyMap {
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", t("tui.back")))
	disableFilter(keys)

	// Enter 一律是「提交本节」, 不再是「跳到下一个字段」; 换字段用 ↑↓ 或 Tab.
	next := key.NewBinding(key.WithKeys("down", "tab"), key.WithHelp("↑↓", t("tui.move")))
	prev := key.NewBinding(key.WithKeys("up", "shift+tab"))
	submit := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", t("tui.submit")))
	change := key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("←→", t("tui.change")))
	changeBack := key.NewBinding(key.WithKeys("left", "h"))

	keys.Select.Next = next
	keys.Select.Prev = prev
	keys.Select.Submit = submit
	// ↑↓ 让给字段导航, 取值改用 ←→; Inline 的 Select 才会启用 Left/Right.
	keys.Select.Up = key.NewBinding(key.WithDisabled())
	keys.Select.Down = key.NewBinding(key.WithDisabled())
	keys.Select.Left = changeBack
	keys.Select.Right = change

	keys.Confirm.Next = next
	keys.Confirm.Prev = prev
	keys.Confirm.Submit = submit
	keys.Confirm.Toggle = key.NewBinding(key.WithKeys("left", "right", "h", "l"), key.WithHelp("←→", t("tui.toggle")))

	// 文本输入里 ←→ 仍然移动光标, 只把上下键让给字段导航.
	keys.Input.Next = next
	keys.Input.Prev = prev
	keys.Input.Submit = submit
	return keys
}

func (p *optionsPanel) newForm(keys *huh.KeyMap, groups ...*huh.Group) *huh.Form {
	p.formTheme = huhTheme(themePalette(p.app.Theme))
	return huh.NewForm(groups...).
		WithTheme(p.formTheme).
		WithKeyMap(keys).
		// 提示行由本包自绘: Huh 的 help 会按内部宽度截断, 把 Enter 这类
		// 排在后面的绑定吞掉, 还会为没有说明的绑定留下空段.
		WithShowHelp(false).
		WithShowErrors(true)
}

// openRoot 建根菜单: 一个列出各分区及当前取值的 Select.
// 不重置 p.section: Huh Select 按绑定值恢复 selected, 从子级返回时
// 才能停在进入前那一项. 首次打开仍是空串, Huh 落到第一项.
func (p *optionsPanel) openRoot() tea.Cmd {
	p.current = ""
	p.bind = nil
	options := []huh.Option[string]{
		huh.NewOption(p.rootLabel(t("tui.interface"), p.interfaceSummary()), sectionInterface),
	}
	if p.session != nil {
		cfg := p.session.Config
		options = append(options,
			huh.NewOption(p.rootLabel(t("tui.execution_and_models"), config.FormatKanbanAgentsSummary(cfg)+" · "+cfg.Launcher), sectionExecution),
			huh.NewOption(p.rootLabel(t("tui.review_and_models"), p.reviewSummary()), sectionReview),
			huh.NewOption(t("rules.modules"), sectionRules),
		)
	}
	options = append(options,
		huh.NewOption(p.rootLabel(t("tui.environment_check"), ""), sectionDoctor),
		huh.NewOption(p.rootLabel(t("tui.save_and_apply"), p.dirtyLabel()), sectionSave),
		huh.NewOption(p.rootLabel(t("tui.close_2"), ""), sectionClose),
	)
	form := p.newForm(rootKeyMap(), huh.NewGroup(
		huh.NewSelect[string]().
			Description(t("tui.enter_to_open_esc_to_close")).
			Options(options...).
			Value(&p.section),
	))
	return p.startForm(form)
}

func (p *optionsPanel) rootLabel(name, value string) string {
	if value == "" {
		return name
	}
	return name + ": " + value
}

func (p *optionsPanel) dirtyLabel() string {
	if p.dirty {
		return t("tui.unsaved_changes")
	}
	return ""
}

func (p *optionsPanel) interfaceSummary() string {
	out := p.app.Context.themeLabel(p.app.Theme) + " · " +
		strconv.Itoa(p.app.Columns) + t("tui.cols_2") + " · " +
		strconv.Itoa(p.app.RefreshSecs) + t("tui.s")
	if p.session != nil {
		out += " · " + config.FormatLanguageSummary(p.session.Config.Language)
	}
	return out
}

func (p *optionsPanel) reviewSummary() string {
	if p.session == nil {
		return ""
	}
	cfg := p.session.Config
	out := ""
	for i, role := range config.ReviewRoles {
		if i > 0 {
			out += ", "
		}
		out += role + " " + cfg.Reviewers[role]
	}
	return out
}

// 关闭前确认的三个取值.
const (
	closeSave    = "save"
	closeDiscard = "discard"
	closeBack    = "back"
)

// openCloseConfirm 在有未保存改动时挡一道, 避免改完直接关掉.
func (p *optionsPanel) openCloseConfirm() tea.Cmd {
	if !p.confirming {
		p.closeChoice = closeSave
	}
	p.confirming = true
	p.current = ""
	p.bind = nil
	form := p.newForm(rootKeyMap(), huh.NewGroup(
		huh.NewSelect[string]().
			Description(t("tui.there_are_unsaved_configuration_changes")).
			Options(
				huh.NewOption(t("tui.save_and_close"), closeSave),
				huh.NewOption(t("tui.discard_and_close"), closeDiscard),
				huh.NewOption(t("tui.keep_editing"), closeBack),
			).
			Value(&p.closeChoice),
	))
	return p.startForm(form)
}

// openSection 建某个分区的表单.
func (p *optionsPanel) openSection(section string) tea.Cmd {
	p.current = section
	bind := &formBinding{
		theme:     p.app.Theme,
		columns:   p.app.Columns,
		minWidth:  p.app.MinColumnWidth,
		refresh:   p.app.RefreshSecs,
		single:    p.app.Model.Single,
		archived:  p.app.Model.ShowArchived,
		reviewers: map[string]*string{},
		stages:    map[string]*string{},
	}
	p.bind = bind
	var group *huh.Group
	switch section {
	case sectionInterface:
		group = p.interfaceGroup(bind)
	case sectionExecution:
		group = p.executionGroup(bind)
	case sectionReview:
		group = p.reviewGroup(bind)
	case sectionRules:
		group = p.rulesGroup(bind)
	default:
		return p.openRoot()
	}
	form := p.newForm(sectionKeyMap(), group)
	return p.startForm(form)
}

func toOptions(choices []menu.Choice) []huh.Option[string] {
	out := make([]huh.Option[string], 0, len(choices))
	for _, choice := range choices {
		out = append(out, huh.NewOption(choice.Label, choice.Value))
	}
	return out
}

// tip 给 inline Confirm / Input 的 Description 加前导空格.
// Huh 把 Title 和 Description 直接拼在同一行, 需要空格分隔.
func tip(text string) string {
	if text == "" {
		return ""
	}
	return " " + text
}

func (p *optionsPanel) interfaceGroup(bind *formBinding) *huh.Group {
	bind.reset()
	if bind.minWidth == 0 {
		bind.minWidth = minColumnWidth
	}
	themeOptions := make([]huh.Option[string], 0, len(themes))
	for _, name := range themes {
		themeOptions = append(themeOptions, huh.NewOption(p.app.Context.themeLabel(name), name))
	}
	counts := []int{}
	for count := minColumns; count <= maxColumns(); count++ {
		counts = append(counts, count)
	}
	widths := minColumnWidthChoices(bind.minWidth)
	refreshes := []int{5, 10, 15, 30, 60, 120, 300}
	if p.session != nil {
		bind.language = p.session.Config.Language
		bind.fieldIndex[interfaceFocusKey("language")] = bind.focusable
		bind.addField(huh.NewSelect[string]().
			Title(t("menu.default_language")).
			Description(t("tui.ui_and_command_output")).
			Options(toOptions(p.session.LanguageChoices())...).
			Value(&bind.language).
			Inline(true))
		bind.addSpacer()
	}
	bind.fieldIndex[interfaceFocusKey("theme")] = bind.focusable
	bind.addField(huh.NewSelect[string]().
		Title(t("tui.color_theme")).
		Description(t("tui.follow_the_terminal_or_lock_light_dark")).
		Options(themeOptions...).
		Value(&bind.theme).
		Inline(true))
	bind.addSpacer()
	bind.addField(huh.NewSelect[int]().
		Title(t("tui.auto_refresh_s")).
		Description(t("tui.board_auto_reload_interval")).
		Options(huh.NewOptions(refreshes...)...).
		Value(&bind.refresh).
		Inline(true))
	bind.addSpacer()
	bind.fieldIndex[interfaceFocusKey("single")] = bind.focusable
	bind.addField(huh.NewConfirm().
		Title(t("tui.show_only_the_current_column")).
		Value(&bind.single).
		Inline(true))
	if !bind.single {
		bind.addSpacer()
		bind.addField(huh.NewSelect[int]().
			Title(t("tui.max_columns_on_screen")).
			Description(t("tui.narrow_terminals_show_fewer")).
			Options(huh.NewOptions(counts...)...).
			Value(&bind.columns).
			Inline(true))
		bind.addSpacer()
		bind.addField(huh.NewSelect[int]().
			Title(t("tui.minimum_column_width")).
			Options(huh.NewOptions(widths...)...).
			Value(&bind.minWidth).
			Inline(true))
	}
	bind.addSpacer()
	bind.addField(huh.NewConfirm().
		Title(t("tui.show_all_columns")).
		Value(&bind.archived).
		Inline(true))
	return huh.NewGroup(bind.formFields...)
}

func minColumnWidthChoices(current int) []int {
	out := make([]int, 0, (maxMinColumnWidth-minMinColumnWidth)/minColumnWidthStep+2)
	seen := map[int]bool{}
	current = clampMinColumnWidth(current)
	for width := minMinColumnWidth; width <= maxMinColumnWidth; width += minColumnWidthStep {
		out = append(out, width)
		seen[width] = true
	}
	if !seen[current] {
		out = append([]int{current}, out...)
	}
	return out
}

// modelIndent 是嵌在 Agent 选项下方的模型字段的缩进, 用来表示归属关系.
const modelIndent = "  "

// 重建分区后用来定位选择器的键.
func scaleFocusKey(scale string) string    { return "scale:" + scale }
func roleFocusKey(role string) string      { return "role:" + role }
func interfaceFocusKey(name string) string { return "ui:" + name }

func (p *optionsPanel) executionGroup(bind *formBinding) *huh.Group {
	session := p.session
	cfg := session.Config
	bind.large = cfg.KanbanAgents["large"]
	bind.small = cfg.KanbanAgents["small"]
	bind.launcher = cfg.Launcher
	bind.prevLaunch = cfg.Launcher
	bind.reset()

	titles := map[string]string{
		"large": "tui.titles.large",
		"small": "tui.titles.small",
	}
	notes := map[string]string{
		"large": "tui.notes.large",
		"small": "tui.notes.small",
	}
	values := map[string]*string{"large": &bind.large, "small": &bind.small}

	for _, scale := range config.TaskScales {
		if len(bind.formFields) > 0 {
			bind.addSpacer()
		}
		title, note := titles[scale], notes[scale]
		bind.fieldIndex[scaleFocusKey(scale)] = bind.focusable
		bind.addField(huh.NewSelect[string]().
			Title(t(title)).
			Description(t(note)).
			Options(toOptions(session.ExecutionChoicesFor(*values[scale]))...).
			Value(values[scale]).
			Inline(true))
		// 模型与推理档位紧跟在它所属的 Agent 下面, 中间不留空行.
		for _, field := range p.modelInputs(bind, session.ExecutionModelFieldsFor(scale)) {
			bind.addField(field)
		}
	}
	// 启动方式与具体 Agent 无关, 放在最后, 前面空一行隔开.
	bind.addSpacer()
	bind.addField(huh.NewSelect[string]().
		Title(t("menu.launcher")).
		Description(t("tui.how_to_launch_the_agent_when_claiming_a_task")).
		Options(toOptions(session.LauncherChoices())...).
		Value(&bind.launcher).
		Inline(true))
	return huh.NewGroup(bind.formFields...)
}

// modelInputs 把一组模型字段做成缩进的文本输入.
// 同一个 Agent 被两个规模或两个角色共用时, 指向的是同一份配置, 只显示一次.
func (p *optionsPanel) modelInputs(bind *formBinding, fields []menu.ModelField) []huh.Field {
	out := make([]huh.Field, 0, len(fields))
	for _, field := range fields {
		if _, ok := bind.modelSeen[field.Key()]; ok {
			continue
		}
		bind.modelSeen[field.Key()] = struct{}{}
		index := len(bind.modelFields)
		bind.modelFields = append(bind.modelFields, field)
		value := field.Value()
		bind.modelValues = append(bind.modelValues, &value)
		// Inline 让标题与取值同占一行, 一个角色/规模的整块从 7 行压到 4 行.
		out = append(out, huh.NewInput().
			Title(modelIndent+field.Short+"  ").
			Prompt("").
			Inline(true).
			Placeholder(t("tui.empty_means_cli_default")).
			Value(bind.modelValues[index]))
	}
	return out
}

func (p *optionsPanel) reviewGroup(bind *formBinding) *huh.Group {
	session := p.session
	cfg := session.Config
	bind.reset()
	for _, role := range config.ReviewRoles {
		if len(bind.formFields) > 0 {
			bind.addSpacer()
		}
		value := cfg.Reviewers[role]
		bind.reviewers[role] = &value
		bind.fieldIndex[roleFocusKey(role)] = bind.focusable
		bind.addField(huh.NewSelect[string]().
			Title(role + " Reviewer").
			Description(t("tui.which_agent_reviews_this_role")).
			Options(toOptions(session.ReviewerChoicesFor(value))...).
			Value(bind.reviewers[role]).
			Inline(true))
		// 该角色的环节与模型档位紧跟其后, 中间不留空行.
		stage := cfg.ReviewStages[role]
		bind.stages[role] = &stage
		bind.addField(huh.NewSelect[string]().
			Title(modelIndent + t("tui.review_stage", role)).
			// 取值行跟着标题一起缩进: Select 的取值另起一行, 只缩进标题会参差.
			Options(reviewStageOptions()...).
			Value(bind.stages[role]).
			Inline(true))
		for _, field := range p.modelInputs(bind, session.ReviewModelFieldsFor(role)) {
			bind.addField(field)
		}
	}
	return huh.NewGroup(bind.formFields...)
}

// reviewStageOptions 是审核环节三档的界面文案; 配置取值仍是 auto/skip/required.
func reviewStageOptions() []huh.Option[string] {
	labels := map[string]string{
		"auto":     "tui.labels.auto",
		"skip":     "tui.labels.skip",
		"required": "tui.labels.required",
	}
	out := make([]huh.Option[string], 0, len(config.ReviewStageModes))
	for _, mode := range config.ReviewStageModes {
		pair := labels[mode]
		out = append(out, huh.NewOption(modelIndent+t(pair), mode))
	}
	return out
}

// apply 把表单里已经变化的取值即时写回 App 与配置会话.
// Huh 的绑定指针随光标移动就更新, 所以这里做到「边选边生效」;
// Esc 返回时不会丢改动, 因为改动早已落在会话里.
// 有副作用的项 (安装 tmux) 不在这里执行, 见 commitSideEffects.
func (b *formBinding) apply(p *optionsPanel) {
	switch p.current {
	case sectionRules:
		b.applyRules(p)
	case sectionInterface:
		b.applyInterface(p)
		if p.session != nil && b.language != "" && p.session.Config.Language != b.language {
			p.session.SetLanguage(b.language)
			p.app.Context = tuiPageContext()
			p.markDirty()
		}
	case sectionExecution:
		session := p.session
		// Agent 变了, 下面的模型字段就换了对象, 必须重建本屏;
		// 焦点回到刚改的那一行, 不打断操作.
		if session.Config.KanbanAgents["large"] != b.large {
			session.SetExecutionAgent("large", b.large)
			p.markDirty()
			p.rebuildAt(scaleFocusKey("large"))
		}
		if session.Config.KanbanAgents["small"] != b.small {
			session.SetExecutionAgent("small", b.small)
			p.markDirty()
			p.rebuildAt(scaleFocusKey("small"))
		}
		if b.launcher != menu.LauncherInstallValue && session.Config.Launcher != b.launcher {
			session.SetLauncher(b.launcher)
			p.markDirty()
		}
		b.applyModels(p)
	case sectionReview:
		session := p.session
		for _, role := range config.ReviewRoles {
			value := b.reviewers[role]
			if value != nil && session.Config.Reviewers[role] != *value {
				session.SetReviewer(role, *value)
				// 旧取值是照着旧 Reviewer 配的, 换人之后重置成新 Reviewer 的默认值.
				session.ResetReviewRoleModel(role)
				p.markDirty()
				p.rebuildAt(roleFocusKey(role))
			}
		}
		for role, value := range b.stages {
			if value != nil && session.Config.ReviewStages[role] != *value {
				session.SetReviewStage(role, *value)
				p.markDirty()
			}
		}
		b.applyModels(p)
	}
}

// applyModels 把模型输入框里的改动即时写回配置会话.
func (b *formBinding) applyModels(p *optionsPanel) {
	for i, field := range b.modelFields {
		if i >= len(b.modelValues) || b.modelValues[i] == nil {
			continue
		}
		if field.Value() != *b.modelValues[i] {
			field.Set(*b.modelValues[i])
			p.markDirty()
		}
	}
}

func (b *formBinding) applyInterface(p *optionsPanel) {
	app := p.app
	changed := false
	if containsString(themes, b.theme) && app.Theme != b.theme {
		app.Theme = b.theme
		changed = true
		// Huh 在 Update 时缓存正文, 此时缓存仍使用旧主题.
		// 复用分区重建路径, 让当前帧生效并把焦点保留在主题选择器.
		p.rebuildAt(interfaceFocusKey("theme"))
	}
	if count := clampColumns(b.columns); app.Columns != count {
		app.Columns = count
		changed = true
	}
	if width := clampMinColumnWidth(b.minWidth); app.MinColumnWidth != width {
		app.MinColumnWidth = width
		changed = true
	}
	if refresh := clampRefresh(b.refresh); app.RefreshSecs != refresh {
		app.RefreshSecs = refresh
		changed = true
	}
	if app.Model.Single != b.single {
		app.Model.Single = b.single
		changed = true
		p.rebuildAt(interfaceFocusKey("single"))
	}
	if app.Model.ShowArchived != b.archived {
		app.Model.ToggleArchived()
		changed = true
	}
	// 界面偏好改完立刻写入 config.json, Esc 返回也不会丢.
	if changed {
		p.persistUI()
	}
}

// commitSideEffects 执行只能由用户确认整节后才触发的动作:
// 安装 tmux 会占用终端并改动本机环境, 不能随光标移动就跑.
func (b *formBinding) commitSideEffects(p *optionsPanel) {
	switch p.current {
	case sectionExecution:
		b.commitLauncher(p)
	}
}

func (b *formBinding) commitLauncher(p *optionsPanel) {
	session := p.session
	if b.launcher != menu.LauncherInstallValue {
		return
	}
	// 安装 tmux 会占用当前终端, 先把终端还给它, 结束后再回到 alt-screen.
	p.app.pendingShell = func() {
		lines, installed := session.InstallTmux()
		menu.FlushReport(lines)
		if installed {
			session.SetLauncher("tmux")
			p.markDirty()
		}
	}
}
