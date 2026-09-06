package board

import (
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/dualface/kander/internal/i18n"
)

type listRow struct {
	state, kind, timestamp, taskID, title string
}

func combining(r rune) bool {
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r)
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F:
		return true
	case r == 0x2329 || r == 0x232A:
		return true
	case r >= 0x2E80 && r <= 0xA4CF && r != 0x303F:
		return true
	case r >= 0xAC00 && r <= 0xD7A3:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0xFE10 && r <= 0xFE19:
		return true
	case r >= 0xFE30 && r <= 0xFE6F:
		return true
	case r >= 0xFF00 && r <= 0xFF60:
		return true
	case r >= 0xFFE0 && r <= 0xFFE6:
		return true
	case r >= 0x1F300 && r <= 0x1F64F:
		return true
	case r >= 0x1F900 && r <= 0x1F9FF:
		return true
	case r >= 0x20000 && r <= 0x3FFFD:
		return true
	default:
		return false
	}
}

func displayWidthSimple(text string) int {
	width := 0
	for _, r := range text {
		if combining(r) {
			continue
		}
		if isWideRune(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

func stdoutTTY() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func useColor() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") == "1" {
		return true
	}
	return stdoutTTY()
}

func lightBackground() bool {
	value := os.Getenv("COLORFGBG")
	if value == "" {
		return false
	}
	parts := strings.Split(value, ";")
	last := parts[len(parts)-1]
	return last == "7" || last == "15"
}

func colorize(text, code string, enabled bool) string {
	if !enabled {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

func fmtPad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padDisplay(text string, width int) string {
	pad := width - displayWidthSimple(text)
	if pad < 0 {
		pad = 0
	}
	return text + strings.Repeat(" ", pad)
}

func taskTimestamp(entry Entry, text string) string {
	switch entry.State {
	case "working", "review":
		return MetadataFrom(text, "开始时间")
	case "done":
		if value := MetadataFrom(text, "完成时间"); value != "" {
			return value
		}
		info, err := os.Stat(entry.Document)
		if err == nil {
			return time.Unix(info.ModTime().Unix(), 0).Format("2006-01-02 15:04")
		}
	}
	return ""
}

func collectRows(board Board, state string) ([]listRow, error) {
	var rows []listRow
	for _, entry := range selectedEntries(board.Entries, state) {
		text, err := ReadDocument(entry)
		if err != nil {
			return nil, err
		}
		timestamp := taskTimestamp(entry, text)
		if timestamp == "" {
			timestamp = "-"
		}
		rows = append(rows, listRow{entry.State, entry.Kind, timestamp, entry.TaskID, TitleFrom(text)})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].timestamp != rows[j].timestamp {
			return rows[i].timestamp > rows[j].timestamp
		}
		return rows[i].taskID > rows[j].taskID
	})
	sort.SliceStable(rows, func(i, j int) bool {
		return stateIndex(rows[i].state) < stateIndex(rows[j].state)
	})
	return rows, nil
}

func palette() map[string]string {
	if lightBackground() {
		return map[string]string{
			"backlog": "30", "todo": "33", "working": "34", "review": "35", "done": "32",
			"archived": "35", "trash": "31", "small": "30", "large": "1;35",
			"task": "34", "title": "35",
		}
	}
	return map[string]string{
		"backlog": "90", "todo": "93", "working": "96", "review": "95", "done": "92",
		"archived": "94", "trash": "91", "small": "90", "large": "1;95",
		"task": "96", "title": "95",
	}
}

// FormatList renders the list output.
func FormatList(board Board, state string, mobile bool) (string, error) {
	rows, err := collectRows(board, state)
	if err != nil {
		return "", err
	}
	enabled := useColor()
	colors := palette()
	if mobile {
		var b strings.Builder
		for i, row := range rows {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(colorize(row.state, colors[row.state], enabled))
			b.WriteString("  ")
			b.WriteString(colorize(row.kind, colors[row.kind], enabled))
			b.WriteString("  ")
			b.WriteString(row.timestamp)
			b.WriteByte('\n')
			b.WriteString(colorize(row.taskID, colors["task"], enabled))
			b.WriteByte('\n')
			b.WriteString(colorize(row.title, colors["title"], enabled))
			b.WriteByte('\n')
		}
		return b.String(), nil
	}
	englishHeaders := []string{i18n.Text("en", "board.state"), i18n.Text("en", "board.size"), i18n.Text("en", "board.time")}
	headers := []string{t("board.state"), t("board.size"), t("board.time")}
	widths := make([]int, 3)
	for i, header := range headers {
		widths[i] = displayWidthSimple(header)
		if len(englishHeaders[i]) > widths[i] {
			widths[i] = len(englishHeaders[i])
		}
		for _, row := range rows {
			cells := []string{row.state, row.kind, row.timestamp}
			if len(cells[i]) > widths[i] {
				widths[i] = len(cells[i])
			}
		}
	}
	var b strings.Builder
	headingParts := make([]string, 3)
	for i, header := range headers {
		headingParts[i] = padDisplay(header, widths[i])
	}
	b.WriteString(colorize(strings.Join(headingParts, "  ")+t("board.task_id_title"), "1", enabled))
	b.WriteByte('\n')
	sep := make([]string, 3)
	for i, w := range widths {
		sep[i] = strings.Repeat("-", w)
	}
	b.WriteString(strings.Join(sep, "  ") + "  ---------------\n")
	for _, row := range rows {
		state := colorize(fmtPad(row.state, widths[0]), colors[row.state], enabled)
		size := colorize(fmtPad(row.kind, widths[1]), colors[row.kind], enabled)
		timestamp := fmtPad(row.timestamp, widths[2])
		b.WriteString(state + "  " + size + "  " + timestamp + "  ")
		b.WriteString(colorize(row.taskID, colors["task"], enabled))
		b.WriteString("  ")
		b.WriteString(colorize(row.title, colors["title"], enabled))
		b.WriteByte('\n')
	}
	return b.String(), nil
}
