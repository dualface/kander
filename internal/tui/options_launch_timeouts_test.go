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
