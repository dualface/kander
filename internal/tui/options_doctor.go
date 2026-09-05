package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

type terminalToolsResult struct{ tools menu.TerminalTools }

func (p *optionsPanel) beginDoctor(tools menu.TerminalTools) tea.Cmd {
	p.doctorTools = tools
	p.doctorLines = nil
	if !tools.NeedsHerdrInstall() {
		return p.continueDoctor()
	}
	p.status = ""
	p.current = sectionDoctor
	p.bind = nil
	p.installHerdr = false
	return p.startForm(p.newForm(sectionKeyMap(), huh.NewGroup(
		huh.NewConfirm().
			Title(menu.HerdrInstallPrompt()).
			Description(menu.HerdrInstallCommand()).
			Affirmative(t("menu.install_herdr")).
			Negative(t("tui.skip_and_continue")).
			Value(&p.installHerdr),
	)))
}

func (p *optionsPanel) finishHerdrInstall() tea.Cmd {
	if !p.installHerdr {
		return p.continueDoctor()
	}
	p.form = nil
	p.current = ""
	p.status = t("tui.installing_herdr")
	p.app.pendingShell = func() {
		lines, _ := p.session.InstallHerdr()
		p.doctorLines = lines
		p.status = t("tui.running_environment_check")
		// 安装失败也重新探测并继续; 不在安装器运行期间并发检查.
		p.app.pendingWork = func() any {
			return runDoctorReport(menu.CheckTerminalTools())
		}
	}
	return nil
}

func (p *optionsPanel) continueDoctor() tea.Cmd {
	p.form = nil
	p.current = ""
	p.status = t("tui.running_environment_check")
	tools := p.doctorTools
	p.app.pendingWork = func() any {
		return runDoctorReport(tools)
	}
	return nil
}

func runDoctorReport(tools menu.TerminalTools) doctorResult {
	before, _ := config.Load(true)
	lines, healthy := menu.DoctorReport(tools)
	after, _ := config.Load(false)
	return doctorResult{lines: lines, healthy: healthy, before: before, after: after}
}
