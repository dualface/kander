package tui

import (
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

func combining(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r)
}

func runeDisplayWidth(r rune) int {
	if combining(r) {
		return 0
	}
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 1
	}
	return w
}

func displayWidth(text string) int {
	width := 0
	for _, r := range text {
		width += runeDisplayWidth(r)
	}
	return width
}

func printableText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r == '\n' {
			b.WriteRune(r)
			continue
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func clipText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	normalized := printableText(strings.ReplaceAll(strings.ReplaceAll(text, "\t", "    "), "\n", " "))
	if displayWidth(normalized) <= width {
		return normalized
	}
	suffix := "..."
	if width < 4 {
		suffix = strings.Repeat(".", width)
	}
	available := width - displayWidth(suffix)
	var b strings.Builder
	used := 0
	for _, r := range normalized {
		w := runeDisplayWidth(r)
		if used+w > available {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + suffix
}

func padText(text string, width int) string {
	clipped := clipText(text, width)
	pad := width - displayWidth(clipped)
	if pad < 0 {
		pad = 0
	}
	return clipped + strings.Repeat(" ", pad)
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	var result []string
	for _, source := range strings.Split(printableText(strings.ReplaceAll(text, "\t", "    ")), "\n") {
		if source == "" {
			result = append(result, "")
			continue
		}
		var current strings.Builder
		currentWidth := 0
		for _, r := range source {
			w := runeDisplayWidth(r)
			if current.Len() > 0 && currentWidth+w > width {
				result = append(result, current.String())
				current.Reset()
				currentWidth = 0
			}
			current.WriteRune(r)
			currentWidth += w
		}
		result = append(result, current.String())
	}
	if len(result) == 0 {
		return []string{""}
	}
	return result
}

func compactTime(value string) string {
	if len(value) > 5 && value[0] >= '0' && value[0] <= '9' && value[1] >= '0' && value[1] <= '9' &&
		value[2] >= '0' && value[2] <= '9' && value[3] >= '0' && value[3] <= '9' && value[4] == '-' {
		return value[5:]
	}
	return value
}

func compactGroup(value string) string {
	if len(value) > 9 {
		ok := true
		for i := 0; i < 8; i++ {
			if value[i] < '0' || value[i] > '9' {
				ok = false
				break
			}
		}
		if ok && value[8] == '-' {
			return value[9:]
		}
	}
	return value
}

func taskMatches(task Task, keyword string) bool {
	needle := strings.ToLower(strings.TrimSpace(keyword))
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		task.Title, task.TaskID, task.TaskGroup, task.Type, task.Assignee, task.State,
	}, " "))
	return strings.Contains(haystack, needle)
}

func lineMatchIndexes(lines []string, keyword string) []int {
	needle := strings.ToLower(strings.TrimSpace(keyword))
	if needle == "" {
		return nil
	}
	var out []int
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), needle) {
			out = append(out, i)
		}
	}
	return out
}

func matchSpans(text, keyword string) [][2]int {
	needle := strings.ToLower(strings.TrimSpace(keyword))
	if needle == "" {
		return nil
	}
	folded := strings.ToLower(text)
	if len(folded) != len(text) {
		if strings.Contains(folded, needle) {
			return [][2]int{{0, len(text)}}
		}
		return nil
	}
	var spans [][2]int
	start := 0
	step := len(needle)
	if step < 1 {
		step = 1
	}
	for {
		index := strings.Index(folded[start:], needle)
		if index < 0 {
			break
		}
		index += start
		spans = append(spans, [2]int{index, index + len(needle)})
		start = index + step
	}
	return spans
}

func displayColumnToCharIndex(text string, displayCol int) int {
	if displayCol <= 0 {
		return 0
	}
	used := 0
	index := 0
	for _, r := range text {
		w := runeDisplayWidth(r)
		if used+w > displayCol {
			break
		}
		used += w
		index++
	}
	return index
}

func displayColumnToCaretIndex(text string, displayCol int) int {
	if displayCol <= 0 {
		return 0
	}
	used := 0
	index := 0
	for _, r := range text {
		w := runeDisplayWidth(r)
		if w == 0 {
			index++
			continue
		}
		next := used + w
		if displayCol < next {
			return index + 1
		}
		used = next
		index++
	}
	return len([]rune(text))
}

func charIndexToDisplayColumn(text string, charIndex int) int {
	used := 0
	index := 0
	for _, r := range text {
		if index >= charIndex {
			break
		}
		used += runeDisplayWidth(r)
		index++
	}
	return used
}

func sliceRunes(text string, start, end int) string {
	runes := []rune(text)
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > len(runes) {
		start = len(runes)
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start > end {
		return ""
	}
	return string(runes[start:end])
}

func runeCount(text string) int {
	return len([]rune(text))
}
