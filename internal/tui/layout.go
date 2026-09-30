package tui

// visibleColumnCount settles how many columns are actually placed side by side this time.
// The user configures how many columns they want on screen (desired); the column width comes from columnGeometry splitting the terminal width evenly.
// Columns are dropped when the terminal cannot fit them, down to a single column when even two columns cannot meet the minimum width.
func visibleColumnCount(width, total int, single bool, desired, minWidth int) int {
	if single || total <= 1 {
		return 1
	}
	count := clampColumns(desired)
	minWidth = clampMinColumnWidth(minWidth)
	// Every pair of columns is separated by 1 column, so n columns fit only while n*minWidth + (n-1) <= width.
	// Ignoring the separators places one column too many, and after the even split every column falls below the user's minimum width.
	if fit := (width + 1) / (minWidth + 1); count > fit {
		count = fit
	}
	if count > total {
		count = total
	}
	if count < 1 {
		count = 1
	}
	return count
}

type columnGeom struct {
	X, Width     int
	HasSeparator bool
}

func columnGeometry(width, count int) []columnGeom {
	if count < 1 {
		count = 1
	}
	separators := count - 1
	usable := width - separators
	if usable < 0 {
		usable = 0
	}
	layout := make([]columnGeom, 0, count)
	cursor := 0
	for index := 0; index < count; index++ {
		start := index * usable / count
		end := (index + 1) * usable / count
		colWidth := end - start
		hasSep := index < separators
		layout = append(layout, columnGeom{X: cursor, Width: colWidth, HasSeparator: hasSep})
		extra := 0
		if hasSep {
			extra = 1
		}
		cursor += colWidth + extra
	}
	return layout
}

func effectiveDesiredColumns(total, desired int) int {
	if total < 1 {
		return 1
	}
	return min(clampColumns(desired), total)
}

// columnBodyLines is the column content height. A trailing gap is omitted,
// matching the old minimum that dropped the blank under the last card.
func columnBodyLines(model *BoardModel, state string) int {
	lines := columnRows(model, state)
	n := len(lines)
	if n == 0 {
		return 1
	}
	if lines[n-1].kind == "gap" {
		n--
	}
	if n < 1 {
		return 1
	}
	return n
}

func panelMinimumHeight(model *BoardModel, state string, skipTop bool) int {
	height := columnBodyLines(model, state) + 1 // bottom border
	if !skipTop {
		height++
	}
	return height
}

// balancedPanelHeights shares total height as evenly as possible without
// shrinking any panel below the height required to show all of its contents.
func balancedPanelHeights(minimums []int, total int) []int {
	heights := append([]int(nil), minimums...)
	if len(heights) == 0 {
		return heights
	}
	remaining := total
	active := make([]int, len(heights))
	for i := range active {
		active[i] = i
	}
	for len(active) > 0 {
		share := remaining / len(active)
		next := active[:0]
		for _, index := range active {
			if minimums[index] > share {
				heights[index] = minimums[index]
				remaining -= minimums[index]
				continue
			}
			next = append(next, index)
		}
		if len(next) == len(active) {
			for _, index := range next {
				heights[index] = share
			}
			for _, index := range next[:remaining-share*len(next)] {
				heights[index]++
			}
			break
		}
		active = next
	}
	return heights
}
