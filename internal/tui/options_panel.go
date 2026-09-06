package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

// The sections of the options panel. The root form is a Select that opens the matching section form.
const (
	sectionInterface = "interface"
	sectionExecution = "execution"
	sectionReview    = "review"
	sectionRules     = "rules"
	sectionDoctor    = "doctor"
	sectionSave      = "save"
	sectionClose     = "close"
)

// reportView displays multi-line text such as doctor output and save results.
// The raw lines are kept until render time and wrapped to the popup width there, because that width depends on the terminal size.
type reportView struct {
	title string
	extra string
	lines []menu.ReportLine
	view  viewport.Model
	width int
}

// optionsPanel is the options popup opened with o.
// The form part is carried by Huh, the report part by a Bubbles viewport.
type optionsPanel struct {
	app     *App
	session *menu.Session
	loadErr string

	form        *huh.Form
	formTheme   *huh.Theme
	formNatural int
	formWidth   int
	section     string
	current     string
	// While confirming is true the form is the "confirm before closing" prompt rather than a settings section.
	confirming  bool
	closeChoice string
	// A non-empty rebuildFocus means the current section must be rebuilt after this update, with focus landing back on that selector.
	// It records the selector's identifier rather than a line number: a rebuild may change the field count (different agents have
	// different numbers of model fields), so the line number has to be looked up again in the new form.
	rebuildFocus string
	bind         *formBinding
	report       *reportView
	spinner      spinner.Model
	status       string
	doctorTools  menu.TerminalTools
	doctorLines  []menu.ReportLine
	installHerdr bool
	dirty        bool
	initial      string

	// The geometry and body lines of the most recent render, for mouse hit testing.
	box        popupBox
	bodyX      int
	bodyY      int
	bodyWidth  int
	bodyHeight int
	bodyLines  []string
}

func (a *App) openOptions() {
	a.openOptionsAt("")
}

func (a *App) openOptionsAt(section string) {
	if a.Options != nil {
		return
	}
	p := themePalette(a.Theme)
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().Foreground(p.Accent).Background(p.Bg)
	panel := &optionsPanel{app: a, spinner: spin, initial: section}
	a.Options = panel
	if a.Session != nil {
		panel.session = a.Session
		if section == "" {
			panel.openRoot()
		} else {
			panel.dispatch(section)
		}
		return
	}
	panel.requestSession()
}

// Init returns the command to run when the panel starts (the loading spinner or the form initialization).
func (p *optionsPanel) Init() tea.Cmd {
	if p.form != nil {
		return p.form.Init()
	}
	return p.spinner.Tick
}

func (p *optionsPanel) requestSession() {
	p.app.pendingWork = func() any {
		existing, err := config.Load(true)
		valid := err == nil
		if err != nil {
			existing = config.DefaultConfig()
		}
		session, sessionErr := menu.NewSession(existing, valid)
		return sessionResult{session: session, err: sessionErr}
	}
}

type sessionResult struct {
	session *menu.Session
	err     error
}

type doctorResult struct {
	lines   []menu.ReportLine
	healthy bool
	before  *config.Config
	after   *config.Config
}

// applyWork consumes the result of a background task; the Bubble Tea shell calls it on a workMsg.
func (a *App) applyWork(payload any) tea.Cmd {
	panel := a.Options
	if panel == nil {
		return nil
	}
	switch result := payload.(type) {
	case sessionResult:
		if result.err != nil {
			panel.loadErr = result.err.Error()
			return nil
		}
		a.Session = result.session
		panel.session = result.session
		if panel.initial != "" {
			section := panel.initial
			panel.initial = ""
			return panel.dispatch(section)
		}
		return panel.openRoot()
	case terminalToolsResult:
		return panel.beginDoctor(result.tools)
	case doctorResult:
		panel.status = ""
		if panel.session != nil && result.after != nil {
			if err := panel.session.ApplyDoctorConfig(result.before, result.after, panel.dirty); err != nil {
				result.lines = append(result.lines, menu.ReportLine{Level: menu.LevelWarning, Text: err.Error()})
			}
		}
		panel.showReport(t("tui.environment_check"), append(panel.doctorLines, result.lines...), "")
		panel.doctorLines = nil
	}
	return nil
}

func (p *optionsPanel) close() {
	p.app.Options = nil
}

func (p *optionsPanel) markDirty() {
	p.dirty = true
}

// showReport displays one report through a viewport, on top of the form.
func (p *optionsPanel) showReport(title string, lines []menu.ReportLine, extra string) {
	p.report = &reportView{title: title, extra: extra, lines: lines, view: viewport.New(1, 1)}
}

// spaceSaveReport splits the agent wiring results and the closing notes into readable paragraphs.
func spaceSaveReport(lines []menu.ReportLine) []menu.ReportLine {
	out := make([]menu.ReportLine, 0, len(lines)+2)
	for index, line := range lines {
		if index > 0 && (line.Level == menu.LevelOK || line.Level == menu.LevelWarning ||
			(line.Level == menu.LevelNote && lines[index-1].Level != menu.LevelNote)) {
			out = append(out, menu.ReportLine{})
		}
		out = append(out, line)
	}
	return out
}

// build wraps and colors the lines at the given width and returns the actual line count.
func (r *reportView) build(palette palette, width int) int {
	var body []string
	if r.extra != "" {
		for _, text := range wrapText(r.extra, width) {
			body = append(body, styleFor("popup-ok", palette).Render(text))
		}
		if len(r.lines) > 0 {
			body = append(body, "")
		}
	}
	for _, line := range r.lines {
		style := styleFor(reportTag(line.Level), palette)
		for _, raw := range strings.Split(line.Text, "\n") {
			for _, text := range wrapText(raw, width) {
				body = append(body, style.Render(text))
			}
		}
	}
	if len(body) == 0 {
		body = append(body, styleFor("popup-dim", palette).Render(t("tui.no_output")))
	}
	r.view.SetContent(strings.Join(body, "\n"))
	r.width = width
	return len(body)
}

func reportTag(level string) string {
	switch level {
	case menu.LevelWarning:
		return "popup-warn"
	case menu.LevelOK:
		return "popup-ok"
	case menu.LevelNote:
		return "popup-title"
	}
	return "popup"
}

// Update is the input entry point of the panel: the report first, then the current Huh form.
func (p *optionsPanel) Update(msg tea.Msg) tea.Cmd {
	if tick, ok := msg.(spinner.TickMsg); ok {
		if p.form != nil || p.loadErr != "" {
			return nil
		}
		var cmd tea.Cmd
		p.spinner, cmd = p.spinner.Update(tick)
		return cmd
	}
	if p.report != nil {
		return p.updateReport(msg)
	}
	if event, ok := msg.(tea.KeyMsg); ok && p.form != nil && !p.acceptsText() {
		switch mapKey(event) {
		case "q", "Q", "o", "O":
			// Consistent with the rest of the board: q closes, and pressing o again closes too.
			return p.requestClose()
		}
	}
	if p.form == nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch mapKey(key) {
			case "esc", "q", "Q":
				p.close()
			}
		}
		return nil
	}
	return p.updateForm(msg)
}

func (p *optionsPanel) updateReport(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch mapKey(key) {
		case "esc", "q", "Q", "enter", "backspace":
			p.report = nil
			if p.form == nil && p.session != nil {
				return p.openRoot()
			}
			return nil
		}
	}
	var cmd tea.Cmd
	p.report.view, cmd = p.report.view.Update(msg)
	return cmd
}

// acceptsText reports whether the current section contains a text input;
// while it does, q and o must reach the input verbatim and the panel is closed with Esc instead.
func (p *optionsPanel) acceptsText() bool {
	return p.current == sectionExecution || p.current == sectionReview
}

// rebuildAt requests a rebuild of the current section after this update, with focus landing back on the selector named by key.
// When the execution agent or the reviewer changes, the model fields on the same screen belong to a different object and must be rebuilt.
func (p *optionsPanel) rebuildAt(key string) {
	p.rebuildFocus = key
}

// rebuildSection rebuilds the current section and moves focus back to the original field.
// The focus move is left to Bubble Tea to run asynchronously; the form commands must not run synchronously here,
// because the blink chain of a text input contains a tea.Tick that would block for over half a second.
func (p *optionsPanel) rebuildSection() tea.Cmd {
	key := p.rebuildFocus
	p.rebuildFocus = ""
	section := p.current
	if section == "" {
		return nil
	}
	cmd := p.openSection(section)
	// openSection rebuilt bind, so only now is it known which position that selector holds in the new form.
	index := 0
	if p.bind != nil {
		index = p.bind.fieldIndex[key]
	}
	return tea.Batch(cmd, repeatCmd(index, huh.NextField))
}

// resizeForm rebuilds the form so every page re-measures its natural height against the new terminal size.
// Huh's WithHeight cannot be undone; a form once compressed by a short terminal can only leave the scrolling layout through a rebuild.
func (p *optionsPanel) resizeForm() tea.Cmd {
	if p.form == nil || p.report != nil {
		return nil
	}
	if p.confirming {
		return p.openCloseConfirm()
	}
	if p.current == "" {
		return p.openRoot()
	}
	index := 0
	if p.bind != nil {
		focused := p.form.GetFocusedField()
		for _, field := range p.bind.formFields {
			if field.Skip() {
				continue
			}
			if field == focused {
				break
			}
			index++
		}
	}
	section := p.current
	cmd := p.openSection(section)
	return tea.Batch(cmd, repeatCmd(index, huh.NextField))
}

// requestClose closes the panel; unsaved changes make the user choose first.
func (p *optionsPanel) requestClose() tea.Cmd {
	if !p.dirty {
		p.close()
		return nil
	}
	return p.openCloseConfirm()
}

func (p *optionsPanel) updateForm(msg tea.Msg) tea.Cmd {
	// Inside the panel Enter always means "submit the current form", no matter which field the cursor is on;
	// Huh treats Enter as "move to the next field" by default, so it is intercepted here first.
	if event, ok := msg.(tea.KeyMsg); ok && mapKey(event) == "enter" {
		if p.bind != nil {
			p.bind.apply(p)
		}
		if p.confirming {
			return p.finishCloseConfirm()
		}
		return p.finishSection()
	}
	model, cmd := p.form.Update(msg)
	if form, ok := model.(*huh.Form); ok {
		p.form = form
	}
	// Bound values are written back as the cursor moves, which is what lets UI preferences such as the theme preview while being selected.
	if p.bind != nil {
		p.bind.apply(p)
	}
	if p.rebuildFocus != "" {
		return tea.Batch(cmd, p.rebuildSection())
	}
	switch p.form.State {
	case huh.StateCompleted:
		if p.confirming {
			return p.finishCloseConfirm()
		}
		return tea.Batch(cmd, p.finishSection())
	case huh.StateAborted:
		if p.confirming {
			p.confirming = false
			return p.openRoot()
		}
		return p.abortSection()
	}
	return cmd
}

func (p *optionsPanel) finishCloseConfirm() tea.Cmd {
	p.confirming = false
	switch p.closeChoice {
	case closeSave:
		p.save()
		return nil
	case closeDiscard:
		p.close()
		return nil
	}
	return p.openRoot()
}

// finishSection runs the side effects and returns to the root menu once a section form is confirmed.
// Ordinary values were already written back in apply; interface / task execution / review / rules reach disk on submit,
// so there is no need to return to the root menu and pick "save and apply".
func (p *optionsPanel) finishSection() tea.Cmd {
	if p.current == sectionDoctor {
		return p.finishHerdrInstall()
	}
	if p.current == "" {
		return p.dispatch(p.section)
	}
	if p.bind != nil {
		p.bind.commitSideEffects(p)
	}
	if p.savesOnSubmit() {
		if err := p.persistNow(); err != nil {
			p.showReport(t("tui.save_failed"), nil, err.Error())
			return nil
		}
	}
	p.current = ""
	return p.openRoot()
}

func (p *optionsPanel) savesOnSubmit() bool {
	return p.current == sectionInterface || p.current == sectionExecution || p.current == sectionReview || p.current == sectionRules
}

// persistNow writes the current session to the config file, UI preferences included.
func (p *optionsPanel) persistNow() error {
	p.persistUI()
	if p.session == nil {
		return nil
	}
	p.session.Config.WelcomeComplete = true
	if _, err := p.session.Save(); err != nil {
		return err
	}
	p.dirty = false
	return nil
}

// abortSection handles Esc: a section returns to the root menu, the root menu closes the panel.
// Changed values are kept (apply wrote them back long ago); only side effects that need confirmation are skipped.
func (p *optionsPanel) abortSection() tea.Cmd {
	if p.current == sectionDoctor {
		p.installHerdr = false
		return p.finishHerdrInstall()
	}
	if p.current == "" {
		p.close()
		return nil
	}
	p.current = ""
	return p.openRoot()
}

func (p *optionsPanel) dispatch(section string) tea.Cmd {
	switch section {
	case sectionSave:
		p.save()
		return nil
	case sectionClose:
		p.close()
		return nil
	case sectionDoctor:
		p.status = t("tui.running_environment_check")
		p.app.pendingWork = func() any {
			return terminalToolsResult{tools: menu.CheckTerminalTools()}
		}
		p.form = nil
		return nil
	case "":
		return p.openRoot()
	}
	return p.openSection(section)
}

func (p *optionsPanel) save() {
	p.persistUI()
	if p.session == nil {
		p.status = t("tui.environment_not_ready_interface_preferences_saved")
		return
	}
	finishLines, err := p.session.Finish()
	finishLines = spaceSaveReport(finishLines)
	if err != nil {
		p.showReport(t("tui.save_failed"), finishLines, err.Error())
		return
	}
	path, err := p.session.Save()
	if err != nil {
		p.showReport(t("tui.save_failed"), finishLines, err.Error())
		return
	}
	p.dirty = false
	p.app.Context = tuiPageContext()
	p.showReport(
		t("tui.configuration_saved"),
		finishLines,
		t("tui.configuration_saved_2")+path,
	)
	p.form = nil
}

func (p *optionsPanel) persistUI() {
	prefs := uiPrefs{
		Columns:        p.app.Columns,
		MinColumnWidth: p.app.MinColumnWidth,
		Theme:          p.app.Theme,
		Refresh:        p.app.RefreshSecs,
		Single:         p.app.Model.Single,
	}
	written, err := savePrefs(prefs)
	// When the write fails, only the edited value in the session is updated and the baseline is not advanced.
	p.session.SyncTUI(written, err == nil)
	if err != nil {
		p.app.PrefsError = err.Error()
		return
	}
	p.app.PrefsError = ""
}
