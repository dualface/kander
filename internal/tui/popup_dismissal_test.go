package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/dualface/kander/internal/config"
)

func preserveDismissalLanguage(t *testing.T) {
	t.Helper()
	bound, cli := config.BoundConfigLanguage(), config.CLILanguage()
	t.Setenv(config.EnvLang, os.Getenv(config.EnvLang))
	t.Setenv(config.EnvLangCLI, os.Getenv(config.EnvLangCLI))
	t.Cleanup(func() {
		var args []string
		if cli != "" {
			args = []string{"--lang", cli}
		}
		config.ApplyLanguageArgument(args)
		config.BindConfigLanguage(&config.Config{Language: bound})
	})
}

func TestOptionsDismissalHints(t *testing.T) {
	preserveDismissalLanguage(t)
	for _, language := range []string{"en", "cn", "ja"} {
		for _, width := range []int{48, 120} {
			t.Run(fmt.Sprintf("%s/%d", language, width), func(t *testing.T) {
				useInterfaceLanguage(t, language)
				cfg := englishConfig()
				cfg.Language = language
				app, panel := openPanel(t, cfg)
				attachTempOverlay(t, panel.session, config.ModeGlobal)
				app.Width, app.Height = width, 18
				pumpPanel(panel, panel.openRoot())
				app.View()
				if len(panel.actionHits) != 0 {
					t.Fatal("root still renders standalone action buttons")
				}
				label := config.Text("tui.options_close_hint")
				row := len(panel.bodyLines) - 1
				line := ansi.Strip(panel.bodyLines[row])
				index := strings.Index(line, label)
				if index < 0 {
					t.Fatalf("footer lost close hint: %s", line)
				}
				clickOptions(t, app, panel.bodyX+displayWidth(line[:index]), panel.bodyY+row)
				if app.Options != nil {
					t.Fatal("visible footer hint did not close options")
				}
			})
		}
	}
}

func TestOptionsDismissalOutsidePages(t *testing.T) {
	preserveDismissalLanguage(t)
	for _, page := range []string{"root", "section", "report", "flow"} {
		for _, dirty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dirty=%v", page, dirty), func(t *testing.T) {
				app, panel := openPanel(t)
				app.dismissWelcome()
				switch page {
				case "section":
					pumpPanel(panel, panel.openSection(sectionReviewStages))
				case "report":
					panel.showReport("report", nil, "body")
				case "flow":
					panel.openFlow()
				}
				if dirty {
					panel.session.SetLauncher("foreground")
					panel.markDirty()
				}
				app.View()
				// Use the underlying status action unless the tall flow covers it.
				x, y := app.statusHits[0].x, app.Height-1
				if panel.box.contains(x, y) {
					x = panel.box.X - 1
				}
				if page == "section" {
					// A section click only returns to the root; the next one tries to close.
					clickOptions(t, app, x, y)
					if app.Options == nil || panel.current != "" || panel.confirming || panel.dirty != dirty {
						t.Fatal("outside click on a section did not return to root")
					}
					app.View()
				}
				clickOptions(t, app, x, y)
				if !dirty {
					if app.Options != nil || app.Help {
						t.Fatal("outside click did not close clean options without fallthrough")
					}
					return
				}
				if app.Options == nil || !panel.confirming || panel.report != nil {
					t.Fatal("outside click did not show unsaved confirmation")
				}
				app.View()
				clickOptions(t, app, 0, 0)
				if app.Options == nil || !panel.confirming || !panel.dirty {
					t.Fatal("outside click bypassed unsaved confirmation")
				}
				clickOptionsText(t, app, config.Text("tui.keep_editing"))
				if app.Options == nil || panel.confirming || !panel.dirty || panel.session.Config.Launcher != "foreground" {
					t.Fatal("keep editing lost pending changes")
				}
			})
		}
	}
}

func TestOptionsDismissalUnsavedRoutes(t *testing.T) {
	preserveDismissalLanguage(t)
	for _, route := range []string{"footer", "description", "esc", "menu"} {
		t.Run(route, func(t *testing.T) {
			app, panel := openPanel(t)
			panel.session.SetLauncher("foreground")
			panel.markDirty()
			pumpPanel(panel, panel.openRoot())
			switch route {
			case "footer":
				clickOptionsText(t, app, config.Text("tui.options_close_hint"))
			case "description":
				_, suffix, _ := strings.Cut(panel.menuDescription, "Esc")
				clickOptionsText(t, app, "Esc"+suffix)
			case "esc":
				drivePanel(panel, keyMsg("esc"))
			case "menu":
				clickOptionsText(t, app, config.Text("tui.close_2"))
			}
			if app.Options == nil || !panel.confirming {
				t.Fatal("close route bypassed unsaved confirmation")
			}
		})
	}
}

func TestOptionsDismissalSaveAndDiscard(t *testing.T) {
	preserveDismissalLanguage(t)
	for _, choice := range []string{"save", "discard", "save-error"} {
		t.Run(choice, func(t *testing.T) {
			t.Chdir(t.TempDir())
			app, panel := openPanel(t)
			before, err := os.ReadFile(os.Getenv(config.EnvConfig))
			if err != nil {
				t.Fatal(err)
			}
			panel.session.SetLauncher("foreground")
			panel.markDirty()
			if choice == "save-error" {
				// An invalid file forces the normal save path to retain pending edits.
				if err := os.WriteFile(os.Getenv(config.EnvConfig), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			app.View()
			clickOptions(t, app, 0, 0)
			label := config.Text("tui.save_and_close")
			if choice == "discard" {
				label = config.Text("tui.discard_and_close")
			}
			clickOptionsText(t, app, label)
			switch choice {
			case "discard":
				after, err := os.ReadFile(os.Getenv(config.EnvConfig))
				if err != nil || string(before) != string(after) || app.Options != nil {
					t.Fatal("discard changed disk config or kept options open")
				}
			case "save-error":
				if app.Options == nil || panel.report == nil || !panel.dirty || panel.session.Config.Launcher != "foreground" {
					t.Fatal("failed save lost edits or error report")
				}
			case "save":
				saved, err := config.Load(false)
				if err != nil || saved.Launcher != "foreground" || panel.dirty || panel.report == nil {
					t.Fatal("save did not persist changes and show its receipt")
				}
				app.View()
				clickOptions(t, app, 0, 0)
				if app.Options != nil {
					t.Fatal("saved receipt did not close on outside click")
				}
			}
		})
	}
}

func TestPopupDismissalIgnoresInsideAndDrag(t *testing.T) {
	preserveDismissalLanguage(t)
	for _, help := range []bool{false, true} {
		t.Run(fmt.Sprintf("help=%v", help), func(t *testing.T) {
			app, panel := openPanel(t)
			if help {
				app.Options = nil
				app.openHelp()
			}
			for _, width := range []int{120, 48} {
				app.Width, app.Height = width, 20
				app.View()
				box := panel.box
				if help {
					box = app.helpBox
				}
				for _, pos := range [][2]int{{box.X, box.Y}, {box.X + box.Width - 1, box.Y + box.Height - 1}, {box.X + 1, box.Y + 1}} {
					clickOptions(t, app, pos[0], pos[1])
				}
				if help {
					clickOptions(t, app, box.X+box.Width/2, box.Y+box.Height/2)
				}
				app.Update(tea.MouseMsg{X: box.X + 1, Y: box.Y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
				app.Update(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
				app.Update(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
				if help && !app.Help || !help && app.Options == nil {
					t.Fatal("inside/border click, drag or wheel dismissed popup")
				}
			}
			if help {
				clickOptions(t, app, 0, 0)
				if app.Help || app.Options != nil {
					t.Fatal("outside click failed to close help without fallthrough")
				}
			}
		})
	}
}
