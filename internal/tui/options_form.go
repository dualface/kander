package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
	"github.com/dualface/kander/internal/terminal/tmux"
)

// formBinding holds the mutable values bound to a Huh form. Huh needs stable pointers,
// so everything is flattened per section up front and written back to App and the config session when the form closes.
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
	agentLanguage  string
	rules          map[string]*bool
	rulePreset     string
	prevRulePreset string

	modelFields  []menu.ModelField
	modelInputs  map[string]modelInput
	reuseInputs  map[string]modelInput
	modelApplied []string
	// modelValues holds one string pointer per input. Binding the address of a slice element is not allowed:
	// a later append may reallocate the backing array and leave the pointers of already-built fields dangling.
	modelValues []*string
	modelSeen   map[string]struct{}
	// fieldIndex records which focusable position each selector holds in this form, so focus can be restored after a rebuild.
	fieldIndex map[string]int
	// formFields are all the fields of this form (including the Notes used as blank lines),
	// converted to line numbers by their actual rendered height during mouse hit testing.
	formFields []huh.Field
	// focusable is the number of focusable fields added so far; blank lines are excluded because NextField skips them.
	focusable int
	// restoreFlag is the page-level "restore inherit" Confirm; nil when the page has no project overrides.
	restoreFlag *bool
}

// addField appends one focusable field.
func (b *formBinding) addField(field huh.Field) {
	b.formFields = append(b.formFields, field)
	b.focusable++
}

// addSpacer appends a blank field one line tall, used to separate groups.
// A Note is skipped by default, so ↑↓ and NextField never select it.
func (b *formBinding) addSpacer() {
	b.formFields = append(b.formFields, huh.NewNote())
}

// reset clears the model fields left by the previous build so the same binding struct can be reused when rebuilding the form.
func (b *formBinding) reset() {
	b.modelFields = nil
	b.modelValues = nil
	b.modelApplied = nil
	b.modelInputs = map[string]modelInput{}
	b.modelSeen = map[string]struct{}{}
	b.fieldIndex = map[string]int{}
	b.formFields = nil
	b.focusable = 0
	b.restoreFlag = nil
}

func huhTheme(p palette) *huh.Theme {
	theme := huh.ThemeBase()
	applyHuhPalette(theme, p)
	return theme
}

// applyHuhPalette writes the light/dark canvas into an existing Huh theme.
// Huh fields only pick up this pointer on the first WithTheme and ignore later ones,
// so mutating in place only updates the styles; Huh's viewport cache still needs a form rebuild to refresh after a theme switch.
func applyHuhPalette(theme *huh.Theme, p palette) {
	if theme == nil {
		return
	}
	fresh := huh.ThemeBase()
	// Labels and values are ranked: labels are always dim and never bold, values are bold, and the selected one also takes the accent color.
	// Otherwise the two lines of an option share the same indentation and style, and at a glance neither reads as the label or the value.
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
	// Inline Selects draw "← value →" only while focused. Blurred must keep the
	// same left inset ("←" + margin) as two spaces so the value column does not jump.
	theme.Focused.PrevIndicator = p.paint(fresh.Focused.PrevIndicator.Foreground(p.Accent))
	theme.Focused.NextIndicator = p.paint(fresh.Focused.NextIndicator.Foreground(p.Accent))
	theme.Blurred.PrevIndicator = lipgloss.NewStyle().SetString("  ")
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Focused.TextInput.Text = p.paint(fresh.Focused.TextInput.Text.Foreground(p.Accent).Bold(true))
	theme.Blurred.TextInput.Text = p.paint(fresh.Blurred.TextInput.Text.Foreground(p.Base).Bold(true))
	theme.Focused.TextInput.Placeholder = p.paint(fresh.Focused.TextInput.Placeholder.Foreground(p.Dim))
	theme.Blurred.TextInput.Placeholder = p.paint(fresh.Blurred.TextInput.Placeholder.Foreground(p.Dim))
	theme.Focused.Base = p.paint(fresh.Focused.Base.Foreground(p.Base)).BorderForeground(p.PopupEdge).BorderBackground(p.Bg)
	theme.Blurred.Base = p.paint(fresh.Blurred.Base.Foreground(p.Base)).BorderForeground(p.Dim).BorderBackground(p.Bg)
	theme.Help.ShortKey = p.paint(fresh.Help.ShortKey.Foreground(p.Accent))
	theme.Help.ShortDesc = p.paint(fresh.Help.ShortDesc.Foreground(p.Dim))

	// The confirm buttons of Huh's default theme are "black on light grey / light grey on black": on a light terminal
	// the unselected one turns into a solid black block that reads as selected, so all three confirm items look inverted.
	// They are switched to the same accent color as the header: selected is a solid block, unselected is dim text only.
	// Fields are separated by a blank line by default, which is reduced to a plain newline: this package inserts the blank lines per group,
	// so the items of one agent/role stay together and only different groups are separated by a blank line.
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

// pillBorder is the outline of a confirm button. It uses square brackets rather than half-block characters:
// a half block needs both a font that has the glyph and a terminal that paints its color, and missing either shows nothing at all;
// square brackets show which one is selected in any font, any color scheme, even with no color at all.
var pillBorder = lipgloss.Border{Left: "[", Right: "]"}

// inlineConfirm is an options-row Confirm: title and Yes/No on one line, buttons left-aligned
// so Huh does not center them in a title-width box (which left a large gap after long labels
// such as "(inherited …)"). MarginLeft(1) on the buttons keeps a single space after the title.
func inlineConfirm() *huh.Confirm {
	return huh.NewConfirm().Inline(true).WithButtonAlignment(lipgloss.Left)
}

// confirmButtonStyles returns the selected and unselected styles of a confirm button.
// The selected state has the bracket outline plus a fill, the unselected one holds the same width with a hidden border, so toggling does not jitter.
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

// disableFilter turns off candidate filtering: the candidates are all short, and filtering would only swallow keys such as q.
func disableFilter(keys *huh.KeyMap) {
	keys.Select.Filter = key.NewBinding(key.WithDisabled())
	keys.Select.SetFilter = key.NewBinding(key.WithDisabled())
	keys.Select.ClearFilter = key.NewBinding(key.WithDisabled())
}

// rootKeyMap is for the root menu. The root menu is a list of items: ↑↓ moves between them, Enter enters one.
func rootKeyMap() *huh.KeyMap {
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", t("tui.close")))
	disableFilter(keys)
	return keys
}

// sectionKeyMap is for a section form. A section is a settings list rather than a wizard:
// ↑↓ moves between fields and ←→ edits in place, so there is no need to Enter all the way through.
func sectionKeyMap() *huh.KeyMap {
	keys := huh.NewDefaultKeyMap()
	keys.Quit = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", t("tui.back")))
	disableFilter(keys)

	// Enter always means "submit this section" instead of "jump to the next field"; fields move with ↑↓.
	// Tab still moves to the next field when only one Options tab is available.
	next := key.NewBinding(key.WithKeys("down", "tab"), key.WithHelp("↑↓", t("tui.move")))
	prev := key.NewBinding(key.WithKeys("up", "shift+tab"))
	submit := key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", t("tui.submit")))
	change := key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("←→", t("tui.change")))
	changeBack := key.NewBinding(key.WithKeys("left", "h"))

	keys.Select.Next = next
	keys.Select.Prev = prev
	keys.Select.Submit = submit
	// ↑↓ is given to field navigation and values change with ←→; only an Inline Select enables Left/Right.
	keys.Select.Up = key.NewBinding(key.WithDisabled())
	keys.Select.Down = key.NewBinding(key.WithDisabled())
	keys.Select.Left = changeBack
	keys.Select.Right = change

	keys.Confirm.Next = next
	keys.Confirm.Prev = prev
	keys.Confirm.Submit = submit
	keys.Confirm.Toggle = key.NewBinding(key.WithKeys("left", "right", "h", "l"), key.WithHelp("←→", t("tui.toggle")))
	// y/n would emit NextField and, on the last field, complete the whole section.
	keys.Confirm.Accept = key.NewBinding(key.WithDisabled())
	keys.Confirm.Reject = key.NewBinding(key.WithDisabled())

	// Inside a text input ←→ still moves the cursor, and only the up/down keys are given to field navigation.
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
		// The hint line is drawn by this package: Huh's help truncates by its internal width, swallowing later bindings
		// such as Enter, and it also leaves empty segments for bindings that have no description.
		WithShowHelp(false).
		WithShowErrors(true)
}

// openRoot builds the root menu: one Select listing the sections and their current values.
// p.section is deliberately not reset: a Huh Select restores its selection from the bound value, which is what lets a return
// from a child land on the item entered from. On the first open it is still empty and Huh falls to the first item.
func (p *optionsPanel) openRoot() tea.Cmd {
	p.current = ""
	p.bind = nil
	options := []huh.Option[string]{
		huh.NewOption(p.rootLabel(t("tui.interface"), p.interfaceSummary()), sectionInterface),
	}
	if p.session != nil {
		options = append(options,
			huh.NewOption(p.rootLabel(t("tui.execution_and_models"), p.executionSummary()), sectionExecution),
			huh.NewOption(p.rootLabel(t("tui.review_and_models"), p.reviewSummary()), sectionReview),
			huh.NewOption(p.rootLabel(t("tui.review_stages"), p.reviewStagesSummary()), sectionReviewStages),
			huh.NewOption(t("rules.modules"), sectionRules),
			huh.NewOption(t("flow.title"), sectionFlow),
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

func (p *optionsPanel) scopeTUI() config.TUI {
	if p.session != nil && p.session.Config != nil {
		return p.session.Config.TUI
	}
	return config.TUI{
		Theme:          p.app.Theme,
		Columns:        p.app.Columns,
		MinColumnWidth: p.app.MinColumnWidth,
		Refresh:        p.app.RefreshSecs,
		Single:         p.app.Model.Single,
	}
}

func (p *optionsPanel) interfaceSummary() string {
	tui := p.scopeTUI()
	out := p.app.Context.themeLabel(tui.Theme) + " · " +
		strconv.Itoa(tui.Columns) + t("tui.cols_2") + " · " +
		strconv.Itoa(tui.Refresh) + t("tui.s")
	if p.session != nil {
		out += " · " + config.FormatLanguageSummary(p.session.Config.Language)
	}
	return out
}

func (p *optionsPanel) executionSummary() string {
	if p.session == nil || p.session.Config == nil {
		return ""
	}
	cfg := p.session.Config
	return scaleValuePreview(cfg.KanbanAgents["large"], cfg.KanbanAgents["small"]) + " · " + cfg.Launcher
}

func (p *optionsPanel) reviewSummary() string {
	if p.session == nil || p.session.Config == nil {
		return ""
	}
	cfg := p.session.Config
	return roleScalePreview(config.ReviewRoles, func(scale, role string) string {
		return config.ReviewerFor(cfg, scale, role)
	})
}

func (p *optionsPanel) reviewStagesSummary() string {
	if p.session == nil || p.session.Config == nil {
		return ""
	}
	cfg := p.session.Config
	return roleScalePreview(config.ReviewRoles, func(scale, role string) string {
		stage, err := config.ReviewStageFor(cfg, scale, role)
		if err != nil {
			return "auto"
		}
		return stage
	})
}

// scaleValuePreview folds identical large/small values, otherwise large/small.
func scaleValuePreview(large, small string) string {
	if large == small {
		return large
	}
	return large + "/" + small
}

// roleScalePreview joins "ROLE value" entries with " · ", using valueLarge/valueSmall
// when the two scales differ for that role.
func roleScalePreview(roles []string, value func(scale, role string) string) string {
	parts := make([]string, 0, len(roles))
	for _, role := range roles {
		parts = append(parts, role+" "+scaleValuePreview(value("large", role), value("small", role)))
	}
	return strings.Join(parts, " · ")
}

// The three values of the confirm-before-closing prompt.
const (
	closeSave    = "save"
	closeDiscard = "discard"
	closeBack    = "back"
)

// openCloseConfirm interposes a step while there are unsaved changes, so an edit session is not simply closed away.
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

// modelInput keeps the input cursor and accessor stable across chrome refreshes.
type modelInput struct {
	input *huh.Input
	value *string
}

// openSection builds the form of one section.
func (p *optionsPanel) openSection(section string) tea.Cmd {
	return p.openSectionWithInputs(section, nil)
}

func (p *optionsPanel) openSectionWithInputs(section string, inputs map[string]modelInput) tea.Cmd {
	p.current = section
	tui := p.scopeTUI()
	bind := &formBinding{
		reuseInputs: inputs,
		theme:       tui.Theme,
		columns:     tui.Columns,
		minWidth:    tui.MinColumnWidth,
		refresh:     tui.Refresh,
		single:      tui.Single,
		archived:    p.app.Model.ShowArchived,
		reviewers:   map[string]*string{},
		stages:      map[string]*string{},
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
	case sectionReviewStages:
		group = p.reviewStagesGroup(bind)
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

// tip prepends a space to the Description of an inline Confirm / Input.
// Huh joins Title and Description on the same line, so a separating space is needed.
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
			Title(p.inheritTitle(t("menu.default_language"), bind.language, "language")).
			Options(toOptions(p.session.LanguageChoices())...).
			Value(&bind.language).
			Inline(true))
		bind.addSpacer()
		bind.agentLanguage = p.session.Config.AgentLanguage
		bind.fieldIndex[interfaceFocusKey("agent_language")] = bind.focusable
		bind.addField(huh.NewSelect[string]().
			Title(p.inheritTitle(t("menu.agent_language"), bind.agentLanguage, "agent_language")).
			Options(toOptions(p.session.AgentLanguageChoices())...).
			Value(&bind.agentLanguage).
			Inline(true))
		bind.addSpacer()
	}
	bind.fieldIndex[interfaceFocusKey("theme")] = bind.focusable
	bind.addField(huh.NewSelect[string]().
		Title(p.inheritTitle(t("tui.color_theme"), bind.theme, "tui", "theme")).
		Options(themeOptions...).
		Value(&bind.theme).
		Inline(true))
	bind.addSpacer()
	bind.fieldIndex[interfaceFocusKey("refresh")] = bind.focusable
	bind.addField(huh.NewSelect[int]().
		Title(p.inheritTitle(t("tui.auto_refresh_s"), formatInt(bind.refresh), "tui", "refresh")).
		Options(huh.NewOptions(refreshes...)...).
		Value(&bind.refresh).
		Inline(true))
	bind.addSpacer()
	bind.fieldIndex[interfaceFocusKey("single")] = bind.focusable
	bind.addField(inlineConfirm().
		Title(p.inheritTitle(t("tui.show_only_the_current_column"), formatBool(bind.single), "tui", "single")).
		Value(&bind.single))
	if !bind.single {
		bind.addSpacer()
		bind.fieldIndex[interfaceFocusKey("columns")] = bind.focusable
		bind.addField(huh.NewSelect[int]().
			Title(p.inheritTitle(t("tui.max_columns_on_screen"), formatInt(bind.columns), "tui", "columns")).
			Options(huh.NewOptions(counts...)...).
			Value(&bind.columns).
			Inline(true))
		bind.addSpacer()
		bind.fieldIndex[interfaceFocusKey("min_column_width")] = bind.focusable
		bind.addField(huh.NewSelect[int]().
			Title(p.inheritTitle(t("tui.minimum_column_width"), formatInt(bind.minWidth), "tui", "min_column_width")).
			Options(huh.NewOptions(widths...)...).
			Value(&bind.minWidth).
			Inline(true))
	}
	bind.addSpacer()
	bind.addField(inlineConfirm().
		Title(optionTitle(t("tui.show_all_columns"))).
		Value(&bind.archived))
	bind.addPageRestore(p)
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

// modelIndent is the indentation of the model fields nested under an agent option, expressing that they belong to it.
const modelIndent = "  "

// Keys used to locate a selector after a section rebuild.
func scaleFocusKey(scale string) string                 { return "scale:" + scale }
func roleFocusKey(role string) string                   { return "role:" + role }
func reviewerFocusKey(role, scale string) string        { return "reviewer:" + role + ":" + scale }
func stageFocusKey(role, scale string) string           { return "stage:" + role + ":" + scale }
func interfaceFocusKey(name string) string              { return "ui:" + name }
func launcherFocusKey() string                          { return "launcher" }
func modelFocusKey(field menu.ModelField) string {
	return "model:" + field.Key()
}

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
	values := map[string]*string{"large": &bind.large, "small": &bind.small}

	for _, scale := range config.TaskScales {
		if len(bind.formFields) > 0 {
			bind.addSpacer()
		}
		title := titles[scale]
		bind.fieldIndex[scaleFocusKey(scale)] = bind.focusable
		bind.addField(huh.NewSelect[string]().
			Title(p.inheritTitle(t(title), *values[scale], "kanban_agents", scale)).
			Options(toOptions(session.ExecutionChoicesFor(*values[scale]))...).
			Value(values[scale]).
			Inline(true))
		p.addModelInputs(bind, session.ExecutionModelFieldsFor(scale))
		// Agent restore is page-level; selecting an agent already materializes its settings.
	}
	// The launcher is independent of any particular agent, so it goes last, separated by a blank line.
	bind.addSpacer()
	bind.fieldIndex[launcherFocusKey()] = bind.focusable
	bind.addField(huh.NewSelect[string]().
		Title(p.inheritTitle(t("menu.launcher"), bind.launcher, "launcher")).
		Description(t("tui.how_to_launch_the_agent_when_claiming_a_task")).
		Options(toOptions(session.LauncherChoices())...).
		Value(&bind.launcher).
		Inline(true))
	bind.addPageRestore(p)
	return huh.NewGroup(bind.formFields...)
}

// modelInputs turns a set of model fields into indented text inputs.
// When one agent is shared by two scales or two roles they point at the same config entry, which is shown only once.
func (p *optionsPanel) addModelInputs(bind *formBinding, fields []menu.ModelField) {
	for _, field := range fields {
		if _, ok := bind.modelSeen[field.Key()]; ok {
			continue
		}
		bind.modelSeen[field.Key()] = struct{}{}
		bind.modelFields = append(bind.modelFields, field)
		value := field.Value()
		placeholder := field.Placeholder
		if placeholder == "" {
			placeholder = t("tui.empty_means_cli_default")
		}
		// Inline puts the title and the value on one line, compressing the block of one role/scale from 7 lines to 4.
		path := modelOverlayPath(field)
		title := optionTitle(modelIndent + field.Short)
		display := value
		if len(path) > 0 && p.session != nil && p.session.EditingOverlay() && !p.session.FieldOverridden(path...) {
			// Selects annotate inheritance on the label; Inputs keep a plain label and
			// put "(inherited …)" inside the field so the control itself shows the source.
			display = p.session.FormatInherited(value)
			placeholder = ""
		}
		bind.fieldIndex[modelFocusKey(field)] = bind.focusable
		input, ok := bind.reuseInputs[field.Key()]
		if !ok || *input.value != display {
			displayCopy := display
			input = modelInput{input: huh.NewInput().Value(&displayCopy), value: &displayCopy}
		}
		bind.modelValues = append(bind.modelValues, input.value)
		bind.modelApplied = append(bind.modelApplied, display)
		bind.modelInputs[field.Key()] = input
		bind.addField(input.input.Title(title).Prompt("").Inline(true).Placeholder(placeholder))
	}
}

func modelOverlayPath(field menu.ModelField) []string {
	if field.Agent == "" || field.FieldName() == "" {
		return nil
	}
	name := field.FieldName()
	switch name {
	case "model", "effort":
		return []string{"models", "review_roles", field.Agent, name}
	default:
		for _, role := range config.ReviewRoles {
			if field.Agent == role {
				return []string{"models", "review_roles", field.Agent, name}
			}
		}
		return []string{"models", "kanban", field.Agent, name}
	}
}

func (p *optionsPanel) reviewGroup(bind *formBinding) *huh.Group {
	session := p.session
	cfg := session.Config
	bind.reset()
	scaleTitles := map[string]string{
		"large": "tui.review_group_large",
		"small": "tui.review_group_small",
	}
	for i, scale := range config.TaskScales {
		if i > 0 {
			bind.addSpacer()
		}
		bind.formFields = append(bind.formFields, huh.NewNote().Title(t(scaleTitles[scale])))
		for _, role := range config.ReviewRoles {
			value := config.ReviewerFor(cfg, scale, role)
			focus := reviewerFocusKey(role, scale)
			bind.reviewers[focus] = &value
			bind.fieldIndex[focus] = bind.focusable
			bind.addField(huh.NewSelect[string]().
				Title(p.inheritTitle(role, value, "reviewers", scale, role)).
				Options(toOptions(session.ReviewerChoicesFor(value))...).
				Value(bind.reviewers[focus]).
				Inline(true))

			p.addModelInputs(bind, session.ReviewModelFieldsFor(role, scale))
		}
	}
	bind.addPageRestore(p)
	return huh.NewGroup(bind.formFields...)
}

func (p *optionsPanel) reviewStagesGroup(bind *formBinding) *huh.Group {
	session := p.session
	cfg := session.Config
	bind.reset()
	scaleTitles := map[string]string{
		"large": "tui.review_group_large",
		"small": "tui.review_group_small",
	}
	for i, scale := range config.TaskScales {
		if i > 0 {
			bind.addSpacer()
		}
		bind.formFields = append(bind.formFields, huh.NewNote().Title(t(scaleTitles[scale])))
		for _, role := range config.ReviewRoles {
			stage, err := config.ReviewStageFor(cfg, scale, role)
			if err != nil {
				stage = "auto"
			}
			stageKey := stageFocusKey(role, scale)
			bind.stages[stageKey] = &stage
			bind.fieldIndex[stageKey] = bind.focusable
			bind.addField(huh.NewSelect[string]().
				Title(p.inheritTitle(role, stage, "review_stages", scale, role)).
				Options(reviewStageOptions()...).
				Value(bind.stages[stageKey]).
				Inline(true))
		}
	}
	bind.addPageRestore(p)
	return huh.NewGroup(bind.formFields...)
}

// reviewStageOptions is the UI wording of the three review stage policies; the config values remain auto/skip/required.
// Labels stay indented via the field title; option text is flush so ←→ values are not double-indented.
func reviewStageOptions() []huh.Option[string] {
	labels := map[string]string{
		"auto":     "tui.labels.auto",
		"skip":     "tui.labels.skip",
		"required": "tui.labels.required",
	}
	out := make([]huh.Option[string], 0, len(config.ReviewStageModes))
	for _, mode := range config.ReviewStageModes {
		pair := labels[mode]
		out = append(out, huh.NewOption(t(pair), mode))
	}
	return out
}

// apply writes the values changed in the form back to App and the config session immediately.
// Huh updates its bound pointers as the cursor moves, which is what makes changes take effect while selecting;
// returning with Esc loses nothing, because the changes already landed in the session.
// Items with side effects (installing tmux) are not run here, see commitSideEffects.
func (b *formBinding) apply(p *optionsPanel) {
	if b.applyRestores(p) {
		return
	}
	switch p.current {
	case sectionRules:
		b.applyRules(p)
	case sectionInterface:
		// Capture edits before any setter rebuilds derived configuration values.
		languageChanged := p.session != nil && b.language != "" && p.session.Config.Language != b.language
		agentLanguageChanged := p.session != nil && b.agentLanguage != "" && p.session.Config.AgentLanguage != b.agentLanguage
		b.applyInterface(p)
		if languageChanged {
			before := p.overridePresence("language")
			p.session.SetLanguage(b.language)
			p.app.Context = tuiPageContext()
			p.markDirty()
			p.rebuildIfOverrideChanged(before, interfaceFocusKey("language"), "language")
		}
		if agentLanguageChanged {
			before := p.overridePresence("agent_language")
			p.session.SetAgentLanguage(b.agentLanguage)
			p.markDirty()
			p.rebuildIfOverrideChanged(before, interfaceFocusKey("agent_language"), "agent_language")
		}
		if p.session != nil && !agentLanguageChanged && b.agentLanguage != p.session.Config.AgentLanguage {
			b.agentLanguage = p.session.Config.AgentLanguage
			p.rebuildAt(interfaceFocusKey("language"))
		}
	case sectionExecution:
		session := p.session
		// The agent changed, so the model fields below it belong to a different object and this screen must be rebuilt;
		// focus returns to the line just edited so the interaction is not interrupted.
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
			before := p.overridePresence("launcher")
			session.SetLauncher(b.launcher)
			p.markDirty()
			p.rebuildIfOverrideChanged(before, launcherFocusKey(), "launcher")
		}
		b.applyModels(p)
	case sectionReview:
		session := p.session
		for _, role := range config.ReviewRoles {
			for _, scale := range config.TaskScales {
				focus := reviewerFocusKey(role, scale)
				value := b.reviewers[focus]
				if value == nil {
					continue
				}
				if config.ReviewerFor(session.Config, scale, role) != *value {
					session.SetReviewer(scale, role, *value)
					session.ResetReviewRoleModel(role, scale)
					p.markDirty()
					p.rebuildAt(focus)
				}
			}
		}
		b.applyModels(p)
	case sectionReviewStages:
		session := p.session
		for _, role := range config.ReviewRoles {
			for _, scale := range config.TaskScales {
				value := b.stages[stageFocusKey(role, scale)]
				if value == nil {
					continue
				}
				current, err := config.ReviewStageFor(session.Config, scale, role)
				if err != nil {
					current = ""
				}
				if current != *value {
					path := []string{"review_stages", scale, role}
					before := p.overridePresence(path...)
					session.SetReviewStage(scale, role, *value)
					p.markDirty()
					p.rebuildIfOverrideChanged(before, stageFocusKey(role, scale), path...)
				}
			}
		}
	}
}

// applyModels writes changes made in the model inputs back to the config session immediately.
func (b *formBinding) applyModels(p *optionsPanel) {
	for i, field := range b.modelFields {
		if i >= len(b.modelValues) || b.modelValues[i] == nil {
			continue
		}
		next := *b.modelValues[i]
		if b.modelApplied[i] == next {
			continue
		}
		path := modelOverlayPath(field)
		// Inherited Project-tab inputs show "(inherited …)"; an untouched marker or empty is not an edit.
		if len(path) > 0 && p.session != nil && p.session.EditingOverlay() &&
			!p.session.FieldOverridden(path...) {
			if next == "" || next == p.session.FormatInherited(field.Value()) {
				continue
			}
		}
		before := p.overridePresence(path...)
		agentScale := ""
		agentBefore := false
		if len(path) >= 4 && path[0] == "models" && path[1] == "kanban" {
			for _, scale := range config.TaskScales {
				if field.FieldName() == scale+"_model" || field.FieldName() == scale+"_effort" {
					agentScale = scale
					agentBefore = p.overridePresence("kanban_agents", scale)
					break
				}
			}
		}
		field.Set(next)
		if p.session != nil {
			p.session.NoteModelOverride(field, next)
		}
		b.modelApplied[i] = next
		p.markDirty()
		// Keep focus on the model being typed. Pinning the Agent must not move
		// rebuild focus onto the Agent selector mid-keystroke.
		needRebuild := before != p.overridePresence(path...)
		if agentScale != "" && agentBefore != p.overridePresence("kanban_agents", agentScale) {
			needRebuild = true
		}
		if needRebuild {
			p.rebuildAt(modelFocusKey(field))
		}
	}
}

func (b *formBinding) applyInterface(p *optionsPanel) {
	app := p.app
	previous := p.loadedTUI
	if p.appliedTUI != nil {
		previous = *p.appliedTUI
	}
	changed := false
	overlay := p.session != nil && p.session.EditingOverlay()
	var scopeTUI config.TUI
	if p.session != nil && p.session.Config != nil {
		scopeTUI = p.session.Config.TUI
	}
	if containsString(themes, b.theme) && previous.Theme != b.theme {
		app.Theme = b.theme
		scopeTUI.Theme = b.theme
		changed = true
		if overlay {
			p.setOverlayTUIField("theme", b.theme)
		}
		// Huh caches the body during Update, and that cache still uses the old theme at this point.
		// Reuse the section rebuild path so the current frame takes effect and focus stays on the theme selector.
		p.rebuildAt(interfaceFocusKey("theme"))
	}
	if count := clampColumns(b.columns); previous.Columns != count {
		app.Columns = count
		scopeTUI.Columns = count
		changed = true
		if overlay {
			before := p.overridePresence("tui", "columns")
			p.setOverlayTUIField("columns", count)
			p.rebuildIfOverrideChanged(before, interfaceFocusKey("columns"), "tui", "columns")
		}
	}
	if width := clampMinColumnWidth(b.minWidth); previous.MinColumnWidth != width {
		app.MinColumnWidth = width
		scopeTUI.MinColumnWidth = width
		changed = true
		if overlay {
			before := p.overridePresence("tui", "min_column_width")
			p.setOverlayTUIField("min_column_width", width)
			p.rebuildIfOverrideChanged(before, interfaceFocusKey("min_column_width"), "tui", "min_column_width")
		}
	}
	if refresh := clampRefresh(b.refresh); previous.Refresh != refresh {
		app.RefreshSecs = refresh
		scopeTUI.Refresh = refresh
		changed = true
		if overlay {
			before := p.overridePresence("tui", "refresh")
			p.setOverlayTUIField("refresh", refresh)
			p.rebuildIfOverrideChanged(before, interfaceFocusKey("refresh"), "tui", "refresh")
		}
	}
	if previous.Single != b.single {
		app.Model.Single = b.single
		scopeTUI.Single = b.single
		changed = true
		if overlay {
			p.setOverlayTUIField("single", b.single)
		}
		p.rebuildAt(interfaceFocusKey("single"))
	}
	if app.Model.ShowArchived != b.archived {
		app.Model.ToggleArchived()
	}
	if p.session != nil && p.session.Config != nil && !overlay {
		p.session.Config.TUI = scopeTUI
	}
	if !changed {
		return
	}
	app.Theme = scopeTUI.Theme
	app.Columns = scopeTUI.Columns
	app.MinColumnWidth = scopeTUI.MinColumnWidth
	app.RefreshSecs = scopeTUI.Refresh
	app.Model.Single = scopeTUI.Single
	p.appliedTUI = &scopeTUI
	if overlay {
		p.markDirty()
		return
	}
	// UI preferences reach config.json as soon as they change, so returning with Esc loses nothing.
	p.persistUI()
}

// commitSideEffects runs the actions that may only fire once the user confirms the whole section:
// installing tmux occupies the terminal and changes the local environment, so it must not run as the cursor moves.
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
	// Installing tmux occupies the current terminal, so hand the terminal over first and return to the alt-screen afterwards.
	p.app.pendingShell = func() {
		lines, installed := session.InstallTmux()
		menu.FlushReport(lines)
		if installed {
			session.SetLauncher(tmux.Name)
			p.markDirty()
		}
	}
}
