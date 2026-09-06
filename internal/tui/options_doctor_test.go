package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

func TestDoctorSyncPreservesPendingSettings(t *testing.T) {
	app := newPanelApp(t)
	app.Session = newTestSession(t)
	app.openOptions()
	panel := app.Options
	panel.dirty = true
	panel.session.Config.Language = "en"
	panel.session.Config.Models.ReviewRoles["PM"]["model"] = "pending-model"
	before := config.DefaultConfig()
	after := config.DefaultConfig()
	after.WelcomeComplete = true
	after.Reviewers["QA"] = "claude"
	after.Models.ReviewRoles["QA"]["model"] = "opus"
	app.applyWork(doctorResult{before: before, after: after})
	got := panel.session.Config
	if got.Language != "en" || got.Models.ReviewRoles["PM"]["model"] != "pending-model" {
		t.Fatal("doctor discarded pending edits")
	}
	if got.Reviewers["QA"] != "claude" || got.Models.ReviewRoles["QA"]["model"] != "opus" || !got.WelcomeComplete {
		t.Fatal("settings still contain stale values after repair")
	}
}

func TestDoctorInstallDecision(t *testing.T) {
	for _, action := range []string{"default", "escape", "confirm", "available"} {
		t.Run(action, func(t *testing.T) {
			// An empty PATH guarantees the confirmation flow only ever meets a missing installer and never performs a real download.
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("KANDER_CONFIG", filepath.Join(home, "config.json"))
			t.Setenv("PATH", t.TempDir())
			app := newPanelApp(t)
			app.Session = newTestSession(t)
			app.openOptions()
			panel := app.Options
			tools := menu.TerminalTools{}
			if action == "available" {
				tools.Tmux.Path = "tmux"
			}
			pumpPanel(panel, app.applyWork(terminalToolsResult{tools: tools}))
			if action == "available" {
				if app.pendingWork == nil || app.pendingShell != nil {
					t.Fatal("available tools should proceed without installation")
				}
				return
			}
			if panel.current != sectionDoctor || panel.installHerdr || app.pendingShell != nil {
				t.Fatal("must wait for explicit installation confirmation")
			}
			switch action {
			case "escape":
				drivePanel(panel, keyMsg("esc"))
			case "confirm":
				drivePanel(panel, keyMsg("left"))
				drivePanel(panel, keyMsg("enter"))
			default:
				drivePanel(panel, keyMsg("enter"))
			}
			if action == "confirm" {
				if app.pendingShell == nil || app.pendingWork != nil {
					t.Fatal("installation must finish before further checks")
				}
				run := app.pendingShell
				app.pendingShell = nil
				run()
			} else if app.pendingShell != nil {
				t.Fatal("skipping installation must not execute the installer")
			}
			if app.pendingWork == nil {
				t.Fatal("doctor must continue after the installation decision")
			}
			app.applyWork(app.pendingWork())
			if panel.report == nil || panel.current != "" {
				t.Fatal("doctor must show its report")
			}
			if action == "confirm" {
				failed := false
				for _, line := range panel.report.lines {
					if line.Level == menu.LevelWarning && strings.Contains(line.Text, "herdr") {
						failed = true
					}
				}
				if !failed {
					t.Fatal("installation failure must be retained in the report")
				}
			}
			drivePanel(panel, keyMsg("enter"))
			if panel.form == nil || panel.report != nil || app.Options == nil {
				t.Fatal("must return to settings after the report")
			}
		})
	}
}
