package tui

import "unicode"

func isPrintableKey(key string) bool {
	if key == "" || len([]rune(key)) != 1 {
		return false
	}
	r := []rune(key)[0]
	return unicode.IsPrint(r)
}

func (a *App) detailSelectionActive() bool {
	return a.DetailSelectMode != "" && a.DetailAnchor != nil
}

func (a *App) resetDetailPending() {
	a.DetailPendingG = false
	a.DetailCount = 0
	a.DetailCountOn = false
	a.DetailOp = ""
	a.DetailFindWait = ""
	a.DetailObjWait = ""
}

func (a *App) detailHasPending() bool {
	return a.DetailPendingG || a.DetailCountOn || a.DetailOp != "" || a.DetailFindWait != "" || a.DetailObjWait != ""
}

func (a *App) detailRepeat() int {
	if a.DetailCountOn && a.DetailCount > 0 {
		return a.DetailCount
	}
	return 1
}

func (a *App) setDetailCursor(coords [2]int) {
	lines := a.detailLines()
	a.DetailCursor = clampDetailPos(lines, posFrom(coords)).coords()
	a.ensureDetailCursorVisible(lines)
}

func (a *App) yankCharRange(start, end [2]int) {
	text := extractCharSelection(a.detailLines(), start, end)
	if text != "" {
		a.copyText(text)
	}
}

func (a *App) yankLineRange(startLine, endLine int) {
	lines := a.detailLines()
	if len(lines) == 0 {
		return
	}
	if startLine < 0 {
		startLine = 0
	}
	if endLine > len(lines)-1 {
		endLine = len(lines) - 1
	}
	if startLine > endLine {
		startLine, endLine = endLine, startLine
	}
	text := extractLineSelection(lines, [2]int{startLine, 0}, [2]int{endLine, 0})
	if text != "" {
		a.copyText(text)
	}
}

func (a *App) applyDetailMotion(dest detailPos, inclusive bool) {
	lines := a.detailLines()
	origin := posFrom(a.DetailCursor)
	dest = clampDetailPos(lines, dest)
	from, to := origin, dest
	if compareDetailPos(dest, origin) < 0 {
		from, to = dest, origin
	}
	if inclusive {
		to = bumpExclusive(lines, to)
	}
	if a.DetailOp == "y" && !a.detailSelectionActive() {
		a.yankCharRange(from.coords(), to.coords())
		a.resetDetailPending()
		return
	}
	if a.detailSelectionActive() {
		if inclusive && compareDetailPos(dest, origin) < 0 {
			end := to.coords()
			a.DetailAnchor = &end
			a.setDetailCursor(dest.coords())
		} else if inclusive {
			a.setDetailCursor(to.coords())
		} else {
			a.setDetailCursor(dest.coords())
		}
	} else {
		a.setDetailCursor(dest.coords())
	}
	a.resetDetailPending()
}

func (a *App) applyDetailRange(start, end detailPos) {
	start = clampDetailPos(a.detailLines(), start)
	end = clampDetailPos(a.detailLines(), end)
	if a.DetailOp == "y" && !a.detailSelectionActive() {
		a.yankCharRange(start.coords(), end.coords())
		a.resetDetailPending()
		return
	}
	if !a.detailSelectionActive() {
		return
	}
	a.DetailSelectMode = "char"
	a.resetMouseSelection()
	anchor := start.coords()
	a.DetailAnchor = &anchor
	a.setDetailCursor(end.coords())
	a.resetDetailPending()
}

func (a *App) repeatDetailPos(count int, step func(detailPos) detailPos) detailPos {
	p := posFrom(a.DetailCursor)
	for i := 0; i < count; i++ {
		next := step(p)
		if compareDetailPos(next, p) == 0 {
			return next
		}
		p = next
	}
	return p
}

func (a *App) detailToggleSelect(mode string) {
	if a.DetailSelectMode == mode {
		a.resetDetailSelection()
		return
	}
	a.DetailSelectMode = mode
	a.resetMouseSelection()
	line, col := a.DetailCursor[0], a.DetailCursor[1]
	lines := a.detailLines()
	if mode == "line" {
		if len(lines) == 0 {
			a.DetailAnchor = &[2]int{0, 0}
			a.DetailCursor = [2]int{0, 0}
			return
		}
		if line > len(lines)-1 {
			line = len(lines) - 1
		}
		if line < 0 {
			line = 0
		}
		a.DetailAnchor = &[2]int{line, 0}
		a.DetailCursor = [2]int{line, runeCount(lines[line])}
		return
	}
	a.DetailAnchor = &[2]int{line, col}
}

func (a *App) detailYank() {
	if a.DetailSelectMode == "" || a.DetailAnchor == nil {
		return
	}
	lines := a.detailLines()
	var text string
	if a.DetailSelectMode == "line" {
		text = extractLineSelection(lines, *a.DetailAnchor, a.DetailCursor)
	} else {
		text = extractCharSelection(lines, *a.DetailAnchor, a.DetailCursor)
	}
	if text != "" {
		a.copyText(text)
	}
	a.resetDetailSelection()
}

func (a *App) detailMoveCursor(deltaLine, deltaCol int) {
	lines := a.detailLines()
	if len(lines) == 0 {
		return
	}
	line, col := a.DetailCursor[0], a.DetailCursor[1]
	line += deltaLine
	if line < 0 {
		line = 0
	}
	if line > len(lines)-1 {
		line = len(lines) - 1
	}
	lineText := lines[line]
	if deltaCol != 0 {
		col += deltaCol
		n := runeCount(lineText)
		if col < 0 {
			col = 0
		}
		if col > n {
			col = n
		}
	} else if deltaLine != 0 {
		n := runeCount(lineText)
		if col > n {
			col = n
		}
	}
	a.DetailCursor = [2]int{line, col}
	a.ensureDetailCursorVisible(lines)
}

func (a *App) handleDetailSearchKey(key string) {
	switch key {
	case "enter":
		a.applyDetailSearch()
	case "esc":
		a.DetailQuery = ""
		a.DetailMatchIndex = 0
		a.DetailSearching = false
		a.ShowCursor = false
	case "backspace":
		if a.DetailQuery != "" {
			runes := []rune(a.DetailQuery)
			a.DetailQuery = string(runes[:len(runes)-1])
		}
	default:
		if isPrintableKey(key) {
			a.DetailQuery += key
		}
	}
}

func (a *App) handleDetailFindChar(key string) {
	cmd := a.DetailFindWait
	a.DetailFindWait = ""
	if key == "esc" || len([]rune(key)) != 1 {
		a.resetDetailPending()
		return
	}
	ch := []rune(key)[0]
	a.runDetailFind(cmd, ch, a.detailRepeat(), true)
}

func (a *App) runDetailFind(cmd string, ch rune, count int, remember bool) {
	forward := cmd == "f" || cmd == "t"
	till := cmd == "t" || cmd == "T"
	dest, ok := findChar(a.detailLines(), posFrom(a.DetailCursor), ch, forward, till, count)
	if !ok {
		a.resetDetailPending()
		return
	}
	if remember {
		a.DetailLastFind = cmd
		a.DetailLastChar = ch
	}
	a.applyDetailMotion(dest, inclusiveMotion(cmd))
}

func (a *App) handleDetailObject(key string) {
	inner := a.DetailObjWait == "i"
	a.DetailObjWait = ""
	lines := a.detailLines()
	p := posFrom(a.DetailCursor)
	count := a.detailRepeat()
	switch key {
	case "w":
		start, end, ok := wordRange(lines, p, false, !inner, count)
		if !ok {
			a.resetDetailPending()
			return
		}
		a.applyDetailRange(start, end)
	case "W":
		start, end, ok := wordRange(lines, p, true, !inner, count)
		if !ok {
			a.resetDetailPending()
			return
		}
		a.applyDetailRange(start, end)
	case "`", `"`, "'":
		quote := []rune(key)[0]
		if p.line < 0 || p.line >= len(lines) {
			a.resetDetailPending()
			return
		}
		startCol, endCol, ok := quoteRange(lines[p.line], p.col, quote, !inner, count)
		if !ok {
			a.resetDetailPending()
			return
		}
		a.applyDetailRange(detailPos{line: p.line, col: startCol}, detailPos{line: p.line, col: endCol})
	default:
		a.resetDetailPending()
	}
}

func (a *App) swapDetailVisualEnds() {
	if !a.detailSelectionActive() {
		return
	}
	anchor := *a.DetailAnchor
	cursor := a.DetailCursor
	a.DetailAnchor = &cursor
	a.setDetailCursor(anchor)
}

func (a *App) handleDetailKey(key string) {
	if a.DetailSearching {
		a.handleDetailSearchKey(key)
		return
	}
	if a.DetailFindWait != "" {
		a.handleDetailFindChar(key)
		return
	}
	if a.DetailObjWait != "" {
		a.handleDetailObject(key)
		return
	}
	if key == "esc" || key == "backspace" {
		if a.detailHasPending() {
			a.resetDetailPending()
			return
		}
		if a.detailSelectionActive() {
			a.resetDetailSelection()
			return
		}
		if key == "esc" || key == "backspace" || key == "q" || key == "Q" {
			a.closeDetail()
		}
		return
	}
	if a.DetailPendingG {
		a.DetailPendingG = false
		if key == "g" {
			a.gotoDetailLine(a.detailRepeat(), false)
			a.resetDetailPending()
			return
		}
	}
	if a.appendDetailCount(key) {
		return
	}
	switch key {
	case "up", "k", "K":
		n := a.detailRepeat()
		a.resetDetailPending()
		for i := 0; i < n; i++ {
			a.detailMoveCursor(-1, 0)
		}
		return
	case "down", "j", "J":
		n := a.detailRepeat()
		a.resetDetailPending()
		for i := 0; i < n; i++ {
			a.detailMoveCursor(1, 0)
		}
		return
	case "left", "h", "H":
		n := a.detailRepeat()
		a.resetDetailPending()
		for i := 0; i < n; i++ {
			a.detailMoveCursor(0, -1)
		}
		return
	case "right", "l", "L":
		n := a.detailRepeat()
		a.resetDetailPending()
		for i := 0; i < n; i++ {
			a.detailMoveCursor(0, 1)
		}
		return
	case "w":
		a.applyDetailMotion(a.repeatDetailPos(a.detailRepeat(), func(p detailPos) detailPos {
			return wordForward(a.detailLines(), p, false)
		}), false)
		return
	case "W":
		a.applyDetailMotion(a.repeatDetailPos(a.detailRepeat(), func(p detailPos) detailPos {
			return wordForward(a.detailLines(), p, true)
		}), false)
		return
	case "b":
		a.applyDetailMotion(a.repeatDetailPos(a.detailRepeat(), func(p detailPos) detailPos {
			return wordBack(a.detailLines(), p, false)
		}), false)
		return
	case "B":
		a.applyDetailMotion(a.repeatDetailPos(a.detailRepeat(), func(p detailPos) detailPos {
			return wordBack(a.detailLines(), p, true)
		}), false)
		return
	case "e":
		a.applyDetailMotion(a.repeatDetailPos(a.detailRepeat(), func(p detailPos) detailPos {
			return wordEnd(a.detailLines(), p, false)
		}), true)
		return
	case "E":
		a.applyDetailMotion(a.repeatDetailPos(a.detailRepeat(), func(p detailPos) detailPos {
			return wordEnd(a.detailLines(), p, true)
		}), true)
		return
	case "0":
		a.applyDetailMotion(lineBegin(posFrom(a.DetailCursor)), false)
		return
	case "^":
		a.applyDetailMotion(lineFirstNonBlank(a.detailLines(), posFrom(a.DetailCursor)), false)
		return
	case "$":
		p := posFrom(a.DetailCursor)
		for i := 1; i < a.detailRepeat(); i++ {
			if p.line < len(a.detailLines())-1 {
				p.line++
			}
		}
		a.applyDetailMotion(lineEnd(a.detailLines(), p), true)
		return
	case "}":
		a.applyDetailMotion(a.repeatDetailPos(a.detailRepeat(), func(p detailPos) detailPos {
			return paraForward(a.detailLines(), p)
		}), false)
		return
	case "{":
		a.applyDetailMotion(a.repeatDetailPos(a.detailRepeat(), func(p detailPos) detailPos {
			return paraBack(a.detailLines(), p)
		}), false)
		return
	case "%":
		if dest, ok := matchBracket(a.detailLines(), posFrom(a.DetailCursor)); ok {
			a.applyDetailMotion(dest, true)
		} else {
			a.resetDetailPending()
		}
		return
	case "f", "F", "t", "T":
		a.DetailFindWait = key
		return
	case ";":
		if a.DetailLastFind != "" && a.DetailLastChar != 0 {
			a.runDetailFind(a.DetailLastFind, a.DetailLastChar, a.detailRepeat(), false)
		} else {
			a.resetDetailPending()
		}
		return
	case ",":
		if a.DetailLastFind != "" && a.DetailLastChar != 0 {
			a.runDetailFind(flipFindCmd(a.DetailLastFind), a.DetailLastChar, a.detailRepeat(), false)
		} else {
			a.resetDetailPending()
		}
		return
	case "i", "a":
		if a.detailSelectionActive() || a.DetailOp == "y" {
			a.DetailObjWait = key
			return
		}
	case "o":
		if a.detailSelectionActive() {
			a.swapDetailVisualEnds()
			a.resetDetailPending()
			return
		}
	case "Y":
		origin := posFrom(a.DetailCursor)
		a.yankCharRange(origin.coords(), lineEnd(a.detailLines(), origin).coords())
		a.resetDetailPending()
		return
	case "y":
		if a.detailSelectionActive() {
			a.detailYank()
			a.resetDetailPending()
			return
		}
		if a.DetailOp == "y" {
			line := a.DetailCursor[0]
			a.yankLineRange(line, line+a.detailRepeat()-1)
			a.resetDetailPending()
			return
		}
		a.DetailOp = "y"
		return
	}
	if a.detailSelectionActive() {
		switch key {
		case "v":
			a.detailToggleSelect("char")
			a.resetDetailPending()
			return
		case "V":
			a.detailToggleSelect("line")
			a.resetDetailPending()
			return
		case "q", "Q":
			a.resetDetailSelection()
			a.resetDetailPending()
			return
		}
	}
	pageHeight := a.detailBodyHeight()
	half := pageHeight / 2
	if half < 1 {
		half = 1
	}
	switch key {
	case "q", "Q":
		a.closeDetail()
	case "pgup", "ctrl-b":
		a.scrollDetailBy(-pageHeight)
		a.resetDetailPending()
	case "pgdn", "ctrl-f":
		a.scrollDetailBy(pageHeight)
		a.resetDetailPending()
	case "ctrl-u":
		a.scrollDetailBy(-half)
		a.resetDetailPending()
	case "ctrl-d":
		a.scrollDetailBy(half)
		a.resetDetailPending()
	case "g":
		a.DetailPendingG = true
	case "G", "end":
		if a.DetailCountOn {
			a.gotoDetailLine(a.detailRepeat(), key == "G")
		} else {
			lines := a.detailLines()
			if len(lines) > 0 {
				last := len(lines) - 1
				a.DetailCursor = [2]int{last, runeCount(lines[last])}
			}
			a.DetailScroll = 1 << 30
			a.ensureDetailCursorVisible(a.detailLines())
		}
		a.resetDetailPending()
	case "home":
		a.DetailCursor = [2]int{0, 0}
		a.DetailScroll = 0
		a.resetDetailPending()
	case "/":
		a.DetailSearching = true
		a.ShowCursor = true
		a.resetDetailPending()
	case "?":
		a.Help = true
		a.resetDetailPending()
	case "n":
		n := a.detailRepeat()
		a.resetDetailPending()
		for i := 0; i < n; i++ {
			a.jumpDetailMatch(1)
		}
	case "N":
		n := a.detailRepeat()
		a.resetDetailPending()
		for i := 0; i < n; i++ {
			a.jumpDetailMatch(-1)
		}
	case "v":
		a.resetDetailPending()
		a.detailToggleSelect("char")
	case "V":
		a.resetDetailPending()
		a.detailToggleSelect("line")
	default:
		a.resetDetailPending()
	}
}

func (a *App) appendDetailCount(key string) bool {
	if len(key) != 1 || key[0] < '0' || key[0] > '9' {
		return false
	}
	digit := int(key[0] - '0')
	if key == "0" && !a.DetailCountOn {
		return false
	}
	a.DetailCountOn = true
	next := a.DetailCount*10 + digit
	if next > 9999 {
		next = 9999
	}
	a.DetailCount = next
	return true
}

func (a *App) gotoDetailLine(number int, firstNonBlank bool) {
	lines := a.detailLines()
	if len(lines) == 0 {
		a.DetailCursor = [2]int{0, 0}
		a.DetailScroll = 0
		return
	}
	line := number - 1
	if line < 0 {
		line = 0
	}
	if line > len(lines)-1 {
		line = len(lines) - 1
	}
	col := 0
	if firstNonBlank {
		col = lineFirstNonBlank(lines, detailPos{line: line}).col
	}
	a.DetailCursor = [2]int{line, col}
	a.ensureDetailCursorVisible(lines)
}
