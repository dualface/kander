package menu

// DoctorReport 以结构化形式返回 kander doctor 的检查结果, 供 TUI 面板渲染.
// 第二个返回值与 kander doctor 的退出码含义一致: true 表示环境健康.
func DoctorReport(tools TerminalTools) ([]ReportLine, bool) {
	healthy := false
	lines := CaptureReport(func() {
		healthy = printDoctorWithTools(tools, true)
	})
	return lines, healthy
}
