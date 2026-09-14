package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/dualface/kander/internal/issue"
)

// issuesLayout is the shared geometry of the issues overlay. Rendering, list
// capacity and mouse hit testing all read the same computation.
type issuesLayout struct {
	box         popupBox
	body        popupBox
	inner       int
	bodyHeight  int
	wide        bool
	listWidth   int
	detailWidth int
	listScroll  int
}

func (l issuesLayout) listIndexAt(x, y, count int) (int, bool) {
	if x < l.body.X || x >= l.body.X+l.listWidth {
		return 0, false
	}
	relative := y - l.body.Y - issuesListHeader
	if relative < 0 {
		return 0, false
	}
	capacity := (l.bodyHeight - issuesListHeader) / issuesItemLines
	if capacity < 1 {
		capacity = 1
	}
	row := relative / issuesItemLines
	if row < 0 || row >= capacity {
		return 0, false
	}
	index := l.listScroll + row
	if index < 0 || index >= count {
		return 0, false
	}
	return index, true
}

func (a *App) issuesFrame() popup {
	title := a.Context.IssuesTitle
	if repository := a.issuesRepository(); repository != nil {
		title += " - " + repository.Owner + "/" + repository.Name
	}
	hint := a.issuesActionHint()
	if notice := a.issuesNotice(); notice != "" {
		hint = notice
	}
	return popup{
		Title:    issue.SanitizeRemoteText(title),
		Hint:     hint,
		MaxWidth: issuesMaxWidth,
	}
}

// issuesActionHint is the overlay footer for the current selection. Bound and
// unbound issues advertise different keys so a jump cannot look like a failed
// import; with no selection the card-action keys stay hidden.
func (a *App) issuesActionHint() string {
	if a.issuesSelectedNumber() <= 0 {
		return t("tui.issues_hint_none")
	}
	if card, ok := a.issuesSelectedBound(); ok {
		if card.State == "done" {
			return t("tui.issues_hint_done")
		}
		return t("tui.issues_hint_bound")
	}
	return t("tui.issues_hint")
}

func (a *App) issuesLayout() issuesLayout {
	h, w := a.size()
	frame := a.issuesFrame()
	wantedInner := w - 4
	if wantedInner > issuesMaxWidth-4 {
		wantedInner = issuesMaxWidth - 4
	}
	if wantedInner < 20 {
		wantedInner = 20
	}
	inner := frame.inner(w, h, wantedInner)
	bodyHeight := h - frame.chrome() - 4
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	box := centerPopup(w, h, inner+4, frame.chrome()+bodyHeight, frame.MaxWidth, frame.TightFit)
	inner = max(1, box.Width-4)
	bodyHeight = max(1, box.Height-frame.chrome())
	body := popupBox{X: box.X + 2, Y: box.Y + 1, Width: inner, Height: bodyHeight}
	if frame.Title != "" {
		body.Y += blockHeight(frame.Title) + 1
	}
	layout := issuesLayout{box: box, body: body, inner: inner, bodyHeight: bodyHeight}
	if a.Issues != nil {
		layout.listScroll = a.Issues.listScroll
	}
	layout.wide = w >= issuesWideMinWidth && inner >= 60
	if layout.wide {
		layout.listWidth = clampInt(inner*45/100, 30, 60)
		layout.detailWidth = inner - layout.listWidth - 1
	} else {
		layout.listWidth = inner
	}
	return layout
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// renderIssues paints the overlay and returns its geometry for mouse hit
// testing.
func (a *App) renderIssues() (popupBox, string) {
	p := themePalette(a.Theme)
	h, w := a.size()
	layout := a.issuesLayout()
	frame := a.issuesFrame()
	box, _, out := frame.render(p, w, h, layout.inner, a.issuesBody(layout, p))
	return box, out
}

func (a *App) issuesBody(layout issuesLayout, p palette) string {
	if layout.wide {
		list := a.issuesListPane(layout.listWidth, layout.bodyHeight, p)
		detail := a.issuesDetailPane(layout.detailWidth, layout.bodyHeight, p)
		separator := make([]string, layout.bodyHeight)
		for i := range separator {
			separator[i] = p.ink(p.Separator).Render("│")
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, list, strings.Join(separator, "\n"), detail)
	}
	if a.Issues != nil && a.Issues.showDetail {
		return a.issuesDetailPane(layout.inner, layout.bodyHeight, p)
	}
	return a.issuesListPane(layout.inner, layout.bodyHeight, p)
}

func (a *App) issuesHeaderText(width int) string {
	st := a.Issues
	if st == nil {
		return ""
	}
	if st.editing != "" {
		prompt := a.Context.IssuesSearchPrompt
		if st.editing == "label" {
			prompt = a.Context.IssuesLabelPrompt
		}
		return clipText(prompt+st.input, width)
	}
	if st.listErr != "" {
		return clipText(a.Context.IssuesLoadFailed+": "+st.listErr, width)
	}
	query := t("tui.issues_query", a.Context.issueStateLabel(st.state), orDash(st.label), orDash(st.search))
	count := t("tui.issues_count", itoa(len(st.items)), itoa(st.limit))
	if st.more {
		count += " " + a.Context.IssuesMore
	}
	gap := width - displayWidth(query) - displayWidth(count)
	if gap < 1 {
		return clipText(query, width)
	}
	return query + strings.Repeat(" ", gap) + count
}

func (a *App) issuesListPane(width, height int, p palette) string {
	st := a.Issues
	lines := make([]string, 0, height)
	header := a.issuesHeaderText(width)
	headerStyle := styleFor("popup-group", p)
	if st != nil && (st.editing != "" || st.listErr != "") {
		headerStyle = styleFor("popup-warn", p)
	}
	lines = append(lines, headerStyle.Render(padLine(header, width)))
	capacity := (height - issuesListHeader) / issuesItemLines
	if capacity < 1 {
		capacity = 1
	}
	switch {
	case st == nil:
	case st.loading && len(st.items) == 0:
		lines = append(lines, styleFor("popup-dim", p).Render(padLine(clipText(a.Context.IssuesLoading, width), width)))
	case len(st.items) == 0:
		message := a.Context.IssuesEmpty
		if st.listErr != "" {
			message = st.listErr
		}
		lines = append(lines, a.issuesMessageLines(message, width, "popup-warn", p)...)
	default:
		scroll := st.listScroll
		if scroll > len(st.items)-1 {
			scroll = max(0, len(st.items)-1)
		}
		for index := scroll; index < len(st.items) && len(lines) < height; index++ {
			lines = append(lines, a.issuesItemPaneLines(st.items[index], index == st.selected, width, p)...)
		}
	}
	for len(lines) < height {
		lines = append(lines, p.fillLine(width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (a *App) issuesItemPaneLines(item issue.IssueSummary, selected bool, width int, p palette) []string {
	first := "#" + itoa(item.Number) + "  " + a.Context.issueStateLabel(item.State) + "  " + formatIssueTime(item.UpdatedAt)
	labels := issue.SanitizeRemoteText(strings.Join(item.Labels, ", "))
	marker := ""
	if local, ok := a.issuesLocalCard(item.Number); ok {
		text := t("tui.issues_imported", local.TaskID, a.Context.stateLabel(local.State))
		if item.UpdatedAt.After(local.IssueUpdatedAt) {
			text += " · " + t("tui.issues_import_update")
		}
		marker = issue.SanitizeRemoteText(text)
	}
	content := []string{
		issue.SanitizeRemoteText(first),
		issue.SanitizeRemoteText(item.Title),
		labels,
	}
	out := make([]string, 0, issuesItemLines)
	for index, text := range content {
		var style lipgloss.Style
		switch {
		case selected:
			style = styleFor("popup-sel", p)
		case index == 1:
			style = styleFor("popup", p)
		default:
			style = styleFor("popup-dim", p)
		}
		if index == 2 && marker != "" {
			out = append(out, paintIssuesBoundLabelsLine(labels, marker, selected, width, p))
			continue
		}
		out = append(out, style.Render(padLine(clipText(text, width), width)))
	}
	return out
}

// issuesBoundMarkerStyle emphasizes the local-card marker. Unselected rows use
// the theme accent; selected rows keep the same selection surface as the rest
// of the line (Accent on SelectionBg, or an underline on legacy reverse rows)
// so the marker stays distinct without splitting the selection background.
func issuesBoundMarkerStyle(p palette, selected bool) lipgloss.Style {
	if selected {
		if p.SelectionBg != "" {
			return lipgloss.NewStyle().Foreground(p.Accent).Background(p.SelectionBg).Bold(true)
		}
		// Legacy themes reverse PopupFg for selection; Accent-on-reversed-PopupFg
		// contrast is too low, and Reverse(Accent) would paint an Accent chip.
		return styleFor("popup-sel", p).Underline(true)
	}
	return styleFor("popup-group", p)
}

// paintIssuesBoundLabelsLine clips labels and the bound-card marker as plain
// text, then colors only the visible marker fragment. Truncation ellipsis and
// trailing padding keep the row base style so ANSI never drives width.
func paintIssuesBoundLabelsLine(labels, marker string, selected bool, width int, p palette) string {
	prefix := labels
	if strings.TrimSpace(labels) != "" {
		prefix += "  "
	}
	full := prefix + marker
	clipped := clipText(full, width)
	plain := padLine(clipped, width)
	base := styleFor("popup-dim", p)
	if selected {
		base = styleFor("popup-sel", p)
	}
	accent := issuesBoundMarkerStyle(p, selected)

	// Count against the same printable text clipText uses so tabs do not shift
	// the colored marker window.
	normalizedPrefix := printableText(strings.ReplaceAll(strings.ReplaceAll(prefix, "\t", "    "), "\n", " "))
	markerStart := runeCount(normalizedPrefix)
	keptEnd := runeCount(clipped)
	const ellipsis = "..."
	if displayWidth(full) > width {
		if width < 4 || !strings.HasSuffix(clipped, ellipsis) {
			return base.Render(plain)
		}
		keptEnd -= runeCount(ellipsis)
	}
	if keptEnd <= markerStart {
		return base.Render(plain)
	}
	left := sliceRunes(plain, 0, markerStart)
	mid := sliceRunes(plain, markerStart, keptEnd)
	right := sliceRunes(plain, keptEnd, runeCount(plain))
	return base.Render(left) + accent.Render(mid) + base.Render(right)
}

// issuesMessageLines wraps one message into the available width. The pane
// truncates the block, so a message can never push the layout around.
func (a *App) issuesMessageLines(message string, width int, tag string, p palette) []string {
	wrapped := wrapText(issue.SanitizeRemoteText(message), width)
	out := make([]string, 0, len(wrapped))
	style := styleFor(tag, p)
	for _, line := range wrapped {
		out = append(out, style.Render(padLine(line, width)))
	}
	return out
}

func (a *App) issuesDetailPane(width, height int, p palette) string {
	st := a.Issues
	lines := make([]string, 0, height)
	if st == nil {
		return strings.Join(lines, "\n")
	}
	if st.detail == nil {
		switch {
		case st.detailLoading || st.detailPending != 0:
			lines = append(lines, a.issuesMessageLines(a.Context.IssuesLoading, width, "popup-dim", p)...)
		case st.detailErr != "":
			lines = append(lines, a.issuesMessageLines(st.detailErr, width, "popup-warn", p)...)
		default:
			lines = append(lines, a.issuesMessageLines(a.Context.IssuesDetailHint, width, "popup-dim", p)...)
		}
		for len(lines) < height {
			lines = append(lines, p.fillLine(width))
		}
		if len(lines) > height {
			lines = lines[:height]
		}
		return strings.Join(lines, "\n")
	}
	snapshot := st.detail
	identity := "#" + itoa(snapshot.Number) + "  " + a.Context.issueStateLabel(snapshot.State) + "  " + issue.SanitizeRemoteText(snapshot.Title)
	lines = append(lines, styleFor("popup-title", p).Render(padLine(clipText(identity, width), width)))
	meta := "@" + orDash(issue.SanitizeRemoteText(snapshot.Author)) + "  " + formatIssueTime(snapshot.UpdatedAt)
	if len(snapshot.Labels) > 0 {
		meta += "  " + issue.SanitizeRemoteText(strings.Join(snapshot.Labels, ", "))
	}
	if local, ok := a.issuesLocalCard(snapshot.Number); ok {
		meta += "  " + issue.SanitizeRemoteText(t("tui.issues_imported", local.TaskID, a.Context.stateLabel(local.State)))
		if snapshot.UpdatedAt.After(local.IssueUpdatedAt) {
			meta += " · " + issue.SanitizeRemoteText(t("tui.issues_import_update"))
		}
	}
	lines = append(lines, styleFor("popup-dim", p).Render(padLine(clipText(meta, width), width)))
	view := a.issuesDetailView()
	bodyHeight := view.Height
	scroll := view.YOffset
	maxScroll := view.TotalLineCount() - bodyHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	progress := ""
	if maxScroll > 0 {
		progress = " " + itoa(scroll+1) + "/" + itoa(maxScroll+1)
	}
	rule := strings.Repeat("─", width)
	if progress != "" {
		rule = strings.Repeat("─", max(0, width-displayWidth(progress))) + progress
	}
	lines = append(lines, styleFor("popup-edge", p).Render(rule))
	for _, line := range strings.Split(view.View(), "\n") {
		if len(lines) >= height {
			break
		}
		lines = append(lines, padLineFill(withDefaultColors(strings.TrimRight(line, " "), p.ink(p.Base)), width, p))
	}
	for len(lines) < height {
		lines = append(lines, p.fillLine(width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// issuesDetailView prepares the detail viewport for the current layout and
// content, then reads the scroll position back: the viewport clamps it against
// the rendered body height, which is the only place that number is known.
func (a *App) issuesDetailView() *viewport.Model {
	st := a.Issues
	if st == nil {
		return &viewport.Model{}
	}
	layout := a.issuesLayout()
	width := layout.inner
	if layout.wide {
		width = layout.detailWidth
	}
	view := &st.detailView
	view.Width, view.Height = width, a.issuesDetailBodyHeight()
	view.SetContent(strings.Join(a.issuesDetailBodyLines(width), "\n"))
	view.SetYOffset(st.detailScroll)
	st.detailScroll = view.YOffset
	return view
}

func (a *App) issuesDetailBodyLines(width int) []string {
	st := a.Issues
	if st == nil || st.detail == nil {
		return nil
	}
	key := resolveTheme(a.Theme) + "\x00" + itoa(width) + "\x00" + itoa(int(st.detailStamp))
	if st.renderKey == key {
		return st.renderLines
	}
	doc := a.issuesDetailDocument(*st.detail)
	lines := renderMarkdown(doc, width, a.Theme)
	st.renderKey = key
	st.renderLines = lines
	return lines
}

func (a *App) issuesDetailDocument(snapshot issue.IssueSnapshot) string {
	var builder strings.Builder
	body := issue.SanitizeRemoteText(snapshot.Body)
	if strings.TrimSpace(body) == "" {
		builder.WriteString(a.Context.IssuesNoBody)
	} else {
		builder.WriteString(body)
	}
	if snapshot.CommentsLoaded && len(snapshot.Comments) > 0 {
		builder.WriteString("\n\n## " + t("tui.issues_comments") + "\n")
		for _, comment := range snapshot.Comments {
			builder.WriteString("\n### @" + orDash(issue.SanitizeRemoteText(comment.Author)) + " · " + formatIssueTime(comment.CreatedAt) + "\n\n")
			builder.WriteString(issue.SanitizeRemoteText(comment.Body))
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func (a *App) issuesDetailBodyHeight() int {
	layout := a.issuesLayout()
	height := layout.bodyHeight - 3
	if height < 1 {
		height = 1
	}
	return height
}

func (a *App) issuesScrollDetail(delta int) {
	st := a.Issues
	if st == nil {
		return
	}
	st.detailScroll += delta
	a.issuesClampDetailScroll()
}

func (a *App) issuesClampDetailScroll() {
	st := a.Issues
	if st == nil {
		return
	}
	a.issuesDetailView()
}

func formatIssueTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}
