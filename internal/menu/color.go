package menu

import (
	"os"
)

// Severity of a report line. The TUI panel colors lines by severity, the line-based fallback prefixes them by severity.
const (
	LevelInfo    = "info"
	LevelOK      = "ok"
	LevelWarning = "warn"
	LevelNote    = "note"
)

// ReportLine is one output line of doctor and the options panel.
type ReportLine struct {
	Level string
	Text  string
}

// While reportSink is non-nil, every report line is collected instead of written to stderr.
var reportSink *[]ReportLine

// CaptureReport collects the report lines produced during fn without writing to stderr. It nests.
func CaptureReport(fn func()) []ReportLine {
	lines := []ReportLine{}
	previous := reportSink
	reportSink = &lines
	defer func() { reportSink = previous }()
	fn()
	return lines
}

// FlushReport writes collected report lines back to stderr at their original severity.
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
