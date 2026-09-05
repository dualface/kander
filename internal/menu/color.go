package menu

import (
	"os"
)

// 报告行的级别. TUI 面板按级别着色, 行式回退按级别加前缀.
const (
	LevelInfo    = "info"
	LevelOK      = "ok"
	LevelWarning = "warn"
	LevelNote    = "note"
)

// ReportLine 是 doctor 与选项面板的一行输出.
type ReportLine struct {
	Level string
	Text  string
}

// reportSink 非空时, 全部报告行改为收集而不写 stderr.
var reportSink *[]ReportLine

// CaptureReport 收集 fn 期间产生的报告行, 期间不写 stderr. 可嵌套.
func CaptureReport(fn func()) []ReportLine {
	lines := []ReportLine{}
	previous := reportSink
	reportSink = &lines
	defer func() { reportSink = previous }()
	fn()
	return lines
}

// FlushReport 把收集到的报告行按原级别写回 stderr.
func FlushReport(lines []ReportLine) {
	for _, line := range lines {
		emit(line.Level, line.Text)
	}
}

func emit(level, text string) {
	if reportSink != nil {
		*reportSink = append(*reportSink, ReportLine{Level: level, Text: text})
		return
	}
	switch level {
	case LevelWarning:
		os.Stderr.WriteString(color("1;31", "[!] "+text) + "\n")
	case LevelOK:
		os.Stderr.WriteString(color("1;32", "[OK] "+text) + "\n")
	case LevelNote:
		os.Stderr.WriteString(color("1;33", text) + "\n")
	default:
		os.Stderr.WriteString(text + "\n")
	}
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminal(os.Stderr)
}

func color(code, text string) string {
	if !useColor() {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

func warning(text string) {
	emit(LevelWarning, text)
}

func success(text string) {
	emit(LevelOK, text)
}

func hint(text string) {
	emit(LevelInfo, text)
}

func note(text string) {
	emit(LevelNote, text)
}

func announceChoice(prompt, label string) {
	os.Stderr.WriteString("\n" + color("1;36", prompt) + "\n")
	os.Stderr.WriteString("  → " + label + "\n")
}
