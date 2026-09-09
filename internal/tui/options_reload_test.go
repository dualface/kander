package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestOpenOptionsReloadsScopeFromDisk(t *testing.T) {
	app := newPanelApp(t)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.KanbanAgent = "codex"
	stale := newTestSession(t, initial)
	stale.Config.KanbanAgent = "claude"
	useTestOptionsSession(t)
	app.Session = stale
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Session == stale {
		t.Fatal("open reused the in-memory session")
	}
	if app.Session.Config.KanbanAgent != "codex" {
		t.Fatalf("session agent=%s", app.Session.Config.KanbanAgent)
	}
	app.Options.close()
	loaded, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	loaded.KanbanAgent = "grok"
	if _, err := config.Save(loaded); err != nil {
		t.Fatal(err)
	}
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Session.Config.KanbanAgent != "grok" {
		t.Fatalf("reopen agent=%s", app.Session.Config.KanbanAgent)
	}
}

func TestOpenOptionsReloadsOverlayNoticeAndLanguage(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "en_US.UTF-8")
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	dir := t.TempDir()
	t.Chdir(dir)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.Language = "en"
	initial.KanbanAgent = "codex"
	app := newPanelApp(t)
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Options.overlayNotice != "" {
		t.Fatalf("notice without overlay: %q", app.Options.overlayNotice)
	}
	if app.Session.Config.Language != "en" {
		t.Fatalf("scope language=%s", app.Session.Config.Language)
	}
	app.Options.close()

	writeTempOverlay(t, dir, map[string]any{"language": "ja", "kanban_agent": "claude"})
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Session.Config.KanbanAgent != "codex" || app.Session.Config.Language != "en" {
		t.Fatalf("overlay leaked into session: agent=%s lang=%s", app.Session.Config.KanbanAgent, app.Session.Config.Language)
	}
	if !strings.Contains(app.Options.overlayNotice, ".kander-config") {
		t.Fatalf("missing overlay notice: %q", app.Options.overlayNotice)
	}
	if app.Options.overlayNotice != config.Text("tui.overlay_notice") {
		t.Fatalf("notice language=%q", app.Options.overlayNotice)
	}
	if config.ResolveLanguage() != "ja" {
		t.Fatalf("bound language=%s", config.ResolveLanguage())
	}
	app.Options.close()

	writeTempOverlay(t, dir, map[string]any{"language": "cn"})
	app.openOptions()
	finishOptionsLoad(t, app)
	if config.ResolveLanguage() != "cn" {
		t.Fatalf("updated overlay language=%s", config.ResolveLanguage())
	}
	if app.Options.overlayNotice != config.Text("tui.overlay_notice") {
		t.Fatalf("notice did not follow overlay language: %q", app.Options.overlayNotice)
	}
	app.Options.close()

	if err := os.Remove(filepath.Join(dir, config.OverlayFilename)); err != nil {
		t.Fatal(err)
	}
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Options.overlayNotice != "" {
		t.Fatalf("notice after overlay delete: %q", app.Options.overlayNotice)
	}
	if config.ResolveLanguage() != "en" {
		t.Fatalf("language after overlay delete=%s", config.ResolveLanguage())
	}
}

func TestOpenOptionsDiscardsUnsavedEditsOnReopen(t *testing.T) {
	app := newPanelApp(t)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.KanbanAgent = "codex"
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.openOptions()
	finishOptionsLoad(t, app)
	app.Session.Config.KanbanAgent = "claude"
	app.Options.markDirty()
	app.Options.close()
	if app.Session.Config.KanbanAgent != "claude" {
		t.Fatal("precondition: closed session still holds the edit")
	}
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Session.Config.KanbanAgent != "codex" {
		t.Fatalf("reopen kept unsaved agent=%s", app.Session.Config.KanbanAgent)
	}
}

func TestOpenOptionsKeepsOverlayOutOfScopeSave(t *testing.T) {
	dir := t.TempDir()
	_, original := writeTempOverlay(t, dir, map[string]any{
		"kanban_agent": "claude",
		"language":     "ja",
	})
	t.Chdir(dir)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.KanbanAgent = "codex"
	initial.Language = "en"
	app := newPanelApp(t)
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Session.Config.KanbanAgent != "codex" || app.Session.Config.Language != "en" {
		t.Fatalf("editable session used overlay: agent=%s lang=%s", app.Session.Config.KanbanAgent, app.Session.Config.Language)
	}
	if _, err := app.Session.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.OverlayFilename))
	if err != nil || string(data) != string(original) {
		t.Fatalf("overlay bytes changed: %s", data)
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.KanbanAgent == "claude" || scopeCfg.Language == "ja" {
		t.Fatalf("save wrote overlay keys into scope: agent=%s lang=%s", scopeCfg.KanbanAgent, scopeCfg.Language)
	}
}

func TestOpenOptionsLoadErrorsClearSession(t *testing.T) {
	t.Run("scope", func(t *testing.T) {
		app := newPanelApp(t)
		stale := newTestSession(t)
		app.Session = stale
		useTestOptionsSession(t)
		if err := os.WriteFile(os.Getenv(config.EnvConfig), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		app.openOptions()
		runOptionsLoad(t, app)
		if app.Options.loadErr == "" {
			t.Fatal("expected scope loadErr")
		}
		if app.Session != nil || app.Options.session != nil {
			t.Fatal("failed load kept the old session")
		}
	})
	t.Run("overlay", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		app := newPanelApp(t)
		stale := newTestSession(t)
		app.Session = stale
		useTestOptionsSession(t)
		if err := os.WriteFile(filepath.Join(dir, config.OverlayFilename), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		app.openOptions()
		runOptionsLoad(t, app)
		if app.Options.loadErr == "" {
			t.Fatal("expected overlay loadErr")
		}
		if app.Session != nil || app.Options.session != nil {
			t.Fatal("failed overlay load kept the old session")
		}
	})
}

func TestOpenOptionsAtDoesNotReloadWhileOpen(t *testing.T) {
	app := newPanelApp(t)
	_ = newTestSession(t)
	useTestOptionsSession(t)
	app.openOptions()
	if app.pendingWork == nil {
		t.Fatal("first open should load")
	}
	app.pendingWork = nil
	app.openOptions()
	if app.pendingWork != nil {
		t.Fatal("open while the panel exists must not reload")
	}
}
