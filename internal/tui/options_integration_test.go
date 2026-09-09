package tui

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

func TestOptionsRestoreAndTabSwitchResetPreviewBaseline(t *testing.T) {
	initial := config.DefaultConfig()
	initial.TUI.Theme = "light"
	_, panel := openPanel(t, initial)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	pumpPanel(panel, panel.switchTab(config.TargetOverlay))
	pumpPanel(panel, panel.openSection(sectionInterface))
	panel.bind.theme = "dark"
	panel.bind.apply(panel)
	pumpPanel(panel, panel.rebuildSection())
	for _, item := range panel.bind.restores {
		if reflect.DeepEqual(item.path, []string{"tui", "theme"}) {
			*item.flag = true
		}
	}
	panel.bind.apply(panel)
	pumpPanel(panel, panel.rebuildSection())
	if panel.session.FieldOverridden("tui", "theme") || panel.bind.theme != "light" {
		t.Fatal("restored inheritance was copied back by the preview baseline")
	}
	pumpPanel(panel, panel.switchTab(config.TargetScope))
	pumpPanel(panel, panel.switchTab(config.TargetOverlay))
	if panel.session.FieldOverridden("tui") {
		t.Fatal("switching tabs created TUI overrides")
	}
}

func TestProjectOptionsLoadKeepsOverlayLanguage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.Language = "en"
	app := newPanelApp(t)
	_ = newTestSession(t, initial)
	raw := map[string]any{"language": "ja"}
	writeTempOverlay(t, dir, raw)
	original := newOptionsSession
	newOptionsSession = func(existing *config.Config, valid bool) (*menu.Session, error) {
		session, err := menu.NewSessionForTest(existing)
		if err != nil {
			return nil, err
		}
		return session, session.AttachOverlay(config.ModeProject, config.OverlayLocation{
			ProjectRoot: dir, Path: filepath.Join(dir, config.OverlayFilename), Exists: true,
		}, raw)
	}
	t.Cleanup(func() { newOptionsSession = original; config.BindConfigLanguage(nil) })
	app.openOptions()
	finishOptionsLoad(t, app)
	if !app.Session.EditingOverlay() || app.Session.Config.Language != "ja" {
		t.Fatal("captured scope language overwrote the Project edit buffer")
	}
	base, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if base.Language != "en" {
		t.Fatal("loading Project changed the scope")
	}
}
