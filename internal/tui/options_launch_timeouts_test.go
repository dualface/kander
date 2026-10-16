package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
)

func TestExtendedLaunchTimeoutsSwitchFollowsLauncherAndSaves(t *testing.T) {
	_, panel := openPanel(t)
	pumpPanel(panel, panel.openSection(sectionExecution))
	_, view := panel.view()
	plain := ansi.Strip(view)
	launcher := strings.Index(plain, optionTitle(uiText("menu.launcher")))
	timeouts := strings.Index(plain, optionTitle(uiText("tui.extended_launch_timeouts")))
	if launcher < 0 || timeouts < launcher {
		t.Fatalf("launcher=%d timeouts=%d\n%s", launcher, timeouts, plain)
	}
	panel.bind.launchTimeouts = true
	panel.bind.apply(panel)
	if !panel.session.Config.ExtendedLaunchTimeouts {
		t.Fatal("switch edit did not reach the session")
	}
	if _, err := panel.session.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(os.Getenv(config.EnvConfig))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := config.ValidateJSON(data)
	if err != nil || !saved.ExtendedLaunchTimeouts {
		t.Fatalf("saved extended=%v err=%v", saved != nil && saved.ExtendedLaunchTimeouts, err)
	}
}

func TestExtendedLaunchTimeoutsChineseTitle(t *testing.T) {
	config.ApplyLanguageArgument([]string{"--lang", "cn"})
	defer config.ApplyLanguageArgument(nil)
	if got := uiText("tui.extended_launch_timeouts"); got != "延长启动超时" {
		t.Fatalf("cn title=%q", got)
	}
}

func TestExtendedLaunchTimeoutsSeparatedByBlankLines(t *testing.T) {
	config.ApplyLanguageArgument([]string{"--lang", "cn"})
	defer config.ApplyLanguageArgument(nil)
	if got := uiText("tui.extended_launch_timeouts_tip"); got != "延长启动超时为 120s" {
		t.Fatalf("cn tip=%q", got)
	}
	_, panel := openPanel(t)
	pumpPanel(panel, panel.openSection(sectionExecution))
	_, view := panel.view()
	lines := strings.Split(ansi.Strip(view), "\n")
	title := optionTitle(uiText("tui.extended_launch_timeouts"))
	blank := func(line string) bool {
		return strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "│┃")) == ""
	}
	for i, line := range lines {
		if !strings.Contains(line, title) {
			continue
		}
		if i == 0 || i+1 >= len(lines) || !blank(lines[i-1]) || !blank(lines[i+1]) {
			t.Fatalf("switch not separated by blank lines:\n%s", strings.Join(lines, "\n"))
		}
		return
	}
	t.Fatalf("switch missing:\n%s", strings.Join(lines, "\n"))
}
