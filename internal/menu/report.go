package menu

// DoctorReport returns the checks of kander doctor in structured form for the TUI panel to render.
// The second return value matches the exit code of kander doctor: true means the environment is healthy.
func DoctorReport(tools TerminalTools) ([]ReportLine, bool) {
	healthy := false
	lines := CaptureReport(func() {
		healthy = printDoctorWithTools(tools, true)
	})
	return lines, healthy
}
