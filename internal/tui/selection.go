package tui

func orderedPoints(anchor, cursor [2]int) (start, end [2]int) {
	if anchor[0] < cursor[0] || (anchor[0] == cursor[0] && anchor[1] <= cursor[1]) {
		return anchor, cursor
	}
	return cursor, anchor
}

func extractMouseCharSelection(lines []string, anchor, cursor [2]int) string {
	start, end := orderedPoints(anchor, cursor)
	if start == end {
		return ""
	}
	end[1]++
	return extractCharSelection(lines, start, end)
}

func extractCharSelection(lines []string, anchor, cursor [2]int) string {
	if len(lines) == 0 {
		return ""
	}
	start, end := orderedPoints(anchor, cursor)
	startLine, startCol := start[0], start[1]
	endLine, endCol := end[0], end[1]
	if startLine < 0 {
		startLine = 0
	}
	if endLine < 0 {
		endLine = 0
	}
	if startLine > len(lines)-1 {
		startLine = len(lines) - 1
	}
	if endLine > len(lines)-1 {
		endLine = len(lines) - 1
	}
	if startLine == endLine {
		return sliceRunes(lines[startLine], startCol, endCol)
	}
	first := sliceRunes(lines[startLine], startCol, runeCount(lines[startLine]))
	chunks := []string{first}
	for i := startLine + 1; i < endLine; i++ {
		chunks = append(chunks, lines[i])
	}
	chunks = append(chunks, sliceRunes(lines[endLine], 0, endCol))
	out := chunks[0]
	for i := 1; i < len(chunks); i++ {
		out += "\n" + chunks[i]
	}
	return out
}

func extractLineSelection(lines []string, anchor, cursor [2]int) string {
	if len(lines) == 0 {
		return ""
	}
	start, end := orderedPoints(anchor, cursor)
	startLine := start[0]
	endLine := end[0]
	if startLine < 0 {
		startLine = 0
	}
	if endLine < 0 {
		endLine = 0
	}
	if startLine > len(lines)-1 {
		startLine = len(lines) - 1
	}
	if endLine > len(lines)-1 {
		endLine = len(lines) - 1
	}
	out := lines[startLine]
	for i := startLine + 1; i <= endLine; i++ {
		out += "\n" + lines[i]
	}
	return out
}

func selectionSpansForLine(lineIndex int, line string, anchor, cursor [2]int, lineMode, inclusiveEnd bool) [][2]int {
	start, end := orderedPoints(anchor, cursor)
	startLine, startCol := start[0], start[1]
	endLine, endCol := end[0], end[1]
	if !inclusiveEnd && startLine == endLine && startCol >= endCol {
		return nil
	}
	if inclusiveEnd && !lineMode && start == end {
		return nil
	}
	if lineMode {
		if startLine <= lineIndex && lineIndex <= endLine {
			return [][2]int{{0, runeCount(line)}}
		}
		return nil
	}
	if lineIndex < startLine || lineIndex > endLine {
		return nil
	}
	endExclusive := endCol
	if inclusiveEnd {
		endExclusive++
	}
	n := runeCount(line)
	if startLine == endLine {
		right := endExclusive
		if right > n {
			right = n
		}
		return [][2]int{{startCol, right}}
	}
	if lineIndex == startLine {
		return [][2]int{{startCol, n}}
	}
	if lineIndex == endLine {
		right := endExclusive
		if right > n {
			right = n
		}
		return [][2]int{{0, right}}
	}
	return [][2]int{{0, n}}
}

const (
	mouseBtn1Pressed = 1 << iota
	mouseBtn1Released
	mouseBtn1Clicked
	mouseBtn1Double
	mouseBtn4Pressed
	mouseBtn5Pressed
	mouseReportPos
)

func mouseWheelDelta(bstate int) int {
	if bstate&mouseBtn4Pressed != 0 {
		return -1
	}
	if bstate&mouseBtn5Pressed != 0 {
		return 1
	}
	return 0
}

func mouseLeftClicked(bstate int) bool {
	if bstate&mouseBtn1Double != 0 {
		return true
	}
	return bstate&mouseBtn1Clicked != 0
}

func mouseLeftPressed(bstate int) bool {
	return bstate&mouseBtn1Pressed != 0 &&
		bstate&mouseBtn1Released == 0 &&
		bstate&mouseBtn1Clicked == 0 &&
		bstate&mouseBtn1Double == 0
}

func mouseLeftDoubleClicked(bstate int) bool {
	return bstate&mouseBtn1Double != 0
}

func mouseButton1Released(bstate int) bool {
	return bstate&mouseBtn1Released != 0
}

func mouseButton1Dragging(bstate int) bool {
	if bstate&mouseReportPos != 0 {
		return true
	}
	return mouseLeftPressed(bstate)
}
