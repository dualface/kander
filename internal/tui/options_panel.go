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

// 选项面板的各个分区. 根表单是一个 Select, 选中后打开对应分区表单.
const (
	sectionInterface = "interface"
	sectionExecution = "execution"
	sectionReview    = "review"
	sectionRules     = "rules"
	sectionDoctor    = "doctor"
	sectionSave      = "save"
	sectionClose     = "close"
)

// reportView 展示 doctor 与保存结果这类多行文本.
// 原始行保留到渲染时再按弹窗宽度折行, 因为宽度依赖终端尺寸.
type reportView struct {
	title string
	extra string
	lines []menu.ReportLine
	view  viewport.Model
	width int
}

// optionsPanel 是「按 o 打开」的选项弹窗.
// 表单部分由 Huh 承载, 报告部分由 Bubbles viewport 承载.
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
	// confirming 为真时表单是「关闭前确认」而不是某个设置分区.
	confirming  bool
	closeChoice string
	// rebuildFocus 非空表示本轮更新后要重建当前分区, 并把焦点落回该选择器.
	// 记的是选择器的标识而不是行号: 重建后字段数量可能变化 (不同 Agent 的
	// 模型字段数不同), 行号必须在新表单里重新查.
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

	// 最近一次渲染的几何与正文行, 供鼠标命中测试使用.
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

// Init 返回面板启动时要执行的命令 (载入中的 spinner 或表单初始化).
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

// applyWork 消化后台任务的结果, 由 Bubble Tea 外壳在收到 workMsg 时调用.
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

// showReport 用 viewport 展示一段报告, 覆盖在表单之上.
func (p *optionsPanel) showReport(title string, lines []menu.ReportLine, extra string) {
	p.report = &reportView{title: title, extra: extra, lines: lines, view: viewport.New(1, 1)}
}

// spaceSaveReport 把 Agent 接入结果和最后的注意事项分成可读的段落.
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

// build 按给定宽度折行并着色, 返回内容实际行数.
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

// Update 是面板的输入入口: 报告优先, 其次是当前 Huh 表单.
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
			// 与看板其他位置一致: q 关闭, o 再按一次也关闭.
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

// acceptsText 报告当前分区是否含有文本输入框;
// 这种时候 q 与 o 必须原样交给输入框, 关闭面板改用 Esc.
func (p *optionsPanel) acceptsText() bool {
	return p.current == sectionExecution || p.current == sectionReview
}

// rebuildAt 请求在本轮更新后重建当前分区, 并把焦点落回 key 标识的选择器.
// 执行 Agent 或 Reviewer 改变时, 同屏的模型字段换了对象, 必须重建.
func (p *optionsPanel) rebuildAt(key string) {
	p.rebuildFocus = key
}

// rebuildSection 重建当前分区并把焦点移回原来那个字段.
// 焦点移动交给 Bubble Tea 异步执行, 不能在这里同步跑表单命令:
// 文本输入框的闪烁链里有 tea.Tick, 同步执行会阻塞半秒以上.
func (p *optionsPanel) rebuildSection() tea.Cmd {
	key := p.rebuildFocus
	p.rebuildFocus = ""
	section := p.current
	if section == "" {
		return nil
	}
	cmd := p.openSection(section)
	// openSection 重建了 bind, 这时才知道该选择器在新表单里排第几.
	index := 0
	if p.bind != nil {
		index = p.bind.fieldIndex[key]
	}
	return tea.Batch(cmd, repeatCmd(index, huh.NextField))
}

// resizeForm 重建表单，让每个页面按新终端宽高重新测量自然高度。
// Huh 的 WithHeight 不能撤销；曾被矮终端压缩的表单只能通过重建退出滚动布局。
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

// requestClose 关闭面板; 有未保存改动时先让用户选一次.
func (p *optionsPanel) requestClose() tea.Cmd {
	if !p.dirty {
		p.close()
		return nil
	}
	return p.openCloseConfirm()
}

func (p *optionsPanel) updateForm(msg tea.Msg) tea.Cmd {
	// Enter 在面板里一律表示「提交当前表单」, 不管光标停在第几个字段;
	// Huh 默认把 Enter 当成跳到下一个字段, 这里先截下来.
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
	// 绑定值随光标移动即时写回, 主题一类的界面偏好因此可以边选边预览.
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

// finishSection 在一个分区表单确认完成时执行副作用并回到根菜单.
// 普通取值在 apply 里已经即时写回; 界面 / 任务执行 / 审核 / 规则在提交时落盘,
// 不必再回到根菜单点「保存并应用」.
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

// persistNow 把当前会话写入配置文件, 界面偏好一并落盘.
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

// abortSection 处理 Esc: 分区退回根菜单, 根菜单则关闭面板.
// 已改的取值保留 (apply 早已写回), 只有需要确认的副作用不执行.
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
	// 落盘失败时只更新会话中的编辑值, 不推进基线.
	p.session.SyncTUI(written, err == nil)
	if err != nil {
		p.app.PrefsError = err.Error()
		return
	}
	p.app.PrefsError = ""
}
