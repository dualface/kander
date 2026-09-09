package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
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
	if app.Options.overlayNotice != config.Text("tui.overlay_file", filepath.Join(dir, config.OverlayFilename)) {
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
	if app.Options.overlayNotice != config.Text("tui.overlay_file", filepath.Join(dir, config.OverlayFilename)) {
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

func TestOpenOptionsIgnoresStaleReload(t *testing.T) {
	app := newPanelApp(t)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.KanbanAgent = "codex"
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.openOptions()
	staleLoad := app.pendingWork
	if staleLoad == nil {
		t.Fatal("first load")
	}
	app.pendingWork = nil
	staleResult := staleLoad()
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
	freshLoad := app.pendingWork
	if freshLoad == nil {
		t.Fatal("second load")
	}
	app.pendingWork = nil
	cmd := app.applyWork(freshLoad())
	if cmd != nil {
		pumpPanel(app.Options, cmd)
	}
	freshSession := app.Session
	if freshSession == nil || freshSession.Config.KanbanAgent != "grok" {
		t.Fatalf("fresh agent=%v", app.Session)
	}
	app.applyWork(staleResult)
	if app.Session != freshSession || app.Session.Config.KanbanAgent != "grok" {
		t.Fatalf("stale result overwrote session agent=%s", app.Session.Config.KanbanAgent)
	}
}

func TestOpenOptionsFormUsesScopeTUINotApp(t *testing.T) {
	app := newPanelApp(t)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.TUI.Theme = "light"
	initial.TUI.Columns = 3
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.Theme = "dark"
	app.Columns = 7
	app.openOptions()
	finishOptionsLoad(t, app)
	pumpPanel(app.Options, app.Options.dispatch(sectionInterface))
	if app.Options.bind == nil || app.Options.bind.theme != "light" || app.Options.bind.columns != 3 {
		t.Fatalf("form theme=%v columns=%v", app.Options.bind.theme, app.Options.bind.columns)
	}
	if app.Theme != "dark" || app.Columns != 7 {
		t.Fatalf("board theme=%s columns=%d", app.Theme, app.Columns)
	}
}

func TestOpenOptionsFormIgnoresOverlayTUI(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTempOverlay(t, dir, map[string]any{"tui": map[string]any{"theme": "dark", "columns": 2}})
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.TUI.Theme = "light"
	initial.TUI.Columns = 4
	app := newPanelApp(t)
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.openOptions()
	finishOptionsLoad(t, app)
	pumpPanel(app.Options, app.Options.dispatch(sectionInterface))
	if app.Options.bind == nil || app.Options.bind.theme != "light" || app.Options.bind.columns != 4 {
		t.Fatalf("overlay leaked into form: theme=%s columns=%d", app.Options.bind.theme, app.Options.bind.columns)
	}
}

func TestOpenOptionsBindsExplicitLanguageNotSchemaDefault(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "ja_JP.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	app := newPanelApp(t)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	session := newTestSession(t, cfg)
	_ = session
	path := os.Getenv(config.EnvConfig)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	delete(obj, "language")
	encoded, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	useTestOptionsSession(t)
	app.openOptions()
	finishOptionsLoad(t, app)
	if config.ResolveLanguage() != "ja" {
		t.Fatalf("missing language bound %q, want ja locale fallback", config.ResolveLanguage())
	}
}

func TestOpenOptionsInvalidOverlayLanguageIsLoadError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTempOverlay(t, dir, map[string]any{"language": "invalid"})
	app := newPanelApp(t)
	stale := newTestSession(t)
	app.Session = stale
	useTestOptionsSession(t)
	app.openOptions()
	runOptionsLoad(t, app)
	if app.Options.loadErr == "" {
		t.Fatal("invalid overlay language should set loadErr")
	}
	if app.Session != nil || app.Options.session != nil {
		t.Fatal("invalid overlay language kept the old session")
	}
}

func TestOpenOptionsBindsCapturedOverlayLanguage(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "en_US.UTF-8")
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	dir := t.TempDir()
	t.Chdir(dir)
	writeTempOverlay(t, dir, map[string]any{"language": "ja"})
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.Language = "en"
	app := newPanelApp(t)
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.openOptions()
	load := app.pendingWork
	if load == nil {
		t.Fatal("load")
	}
	app.pendingWork = nil
	result := load()
	if err := os.WriteFile(filepath.Join(dir, config.OverlayFilename), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := app.applyWork(result)
	if cmd != nil {
		pumpPanel(app.Options, cmd)
	}
	if app.Options.loadErr != "" {
		t.Fatalf("captured load failed: %s", app.Options.loadErr)
	}
	if config.ResolveLanguage() != "ja" {
		t.Fatalf("language=%s, want captured overlay ja", config.ResolveLanguage())
	}
}

func TestOpenOptionsBindsCapturedScopeLanguage(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "en_US.UTF-8")
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.Language = "ja"
	app := newPanelApp(t)
	_ = newTestSession(t, initial)
	original := newOptionsSession
	newOptionsSession = func(existing *config.Config, valid bool) (*menu.Session, error) {
		if err := os.WriteFile(os.Getenv(config.EnvConfig), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		return menu.NewSessionForTest(existing)
	}
	t.Cleanup(func() { newOptionsSession = original })
	app.openOptions()
	finishOptionsLoad(t, app)
	if config.ResolveLanguage() != "ja" {
		t.Fatalf("language=%s, want captured scope ja", config.ResolveLanguage())
	}
}

func TestOpenOptionsIgnoresOverlayLanguageBeforeWelcome(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	dir := t.TempDir()
	t.Chdir(dir)
	writeTempOverlay(t, dir, map[string]any{"language": "ja"})
	initial := config.DefaultConfig()
	initial.WelcomeComplete = false
	app := newPanelApp(t)
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.openOptions()
	finishOptionsLoad(t, app)
	if config.ResolveLanguage() != "en" {
		t.Fatalf("unwelcome overlay language bound %q", config.ResolveLanguage())
	}
}

func TestOpenOptionsAcceptsLegacyRulesOverlay(t *testing.T) {
	app := newPanelApp(t)
	_ = newTestSession(t)
	useTestOptionsSession(t)
	path := os.Getenv(config.EnvConfig)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	delete(obj, "rules")
	encoded, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)
	writeTempOverlay(t, dir, map[string]any{"rules": map[string]any{"git": false}})
	app.openOptions()
	finishOptionsLoad(t, app)
}

func TestOpenOptionsRefreshesThemeLabelsAfterLanguageReload(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "en_US.UTF-8")
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	dir := t.TempDir()
	t.Chdir(dir)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.Language = "en"
	app := newPanelApp(t)
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Context.themeLabel("auto") != "auto" {
		t.Fatalf("english theme label=%q", app.Context.themeLabel("auto"))
	}
	app.Options.close()
	writeTempOverlay(t, dir, map[string]any{"language": "ja"})
	app.openOptions()
	finishOptionsLoad(t, app)
	if app.Context.themeLabel("auto") != "自動" {
		t.Fatalf("reloaded theme label=%q", app.Context.themeLabel("auto"))
	}
	pumpPanel(app.Options, app.Options.dispatch(sectionInterface))
	if app.Options.bind == nil || app.Context.themeLabel("light") != "ライト" {
		t.Fatalf("interface theme label=%q", app.Context.themeLabel("light"))
	}
}

func TestOpenOptionsPersistsSingleWhenAppAlreadyMatches(t *testing.T) {
	app := newPanelApp(t)
	initial := config.DefaultConfig()
	initial.WelcomeComplete = true
	initial.TUI.Single = true
	_ = newTestSession(t, initial)
	useTestOptionsSession(t)
	app.Model.Single = false
	app.openOptions()
	finishOptionsLoad(t, app)
	pumpPanel(app.Options, app.Options.dispatch(sectionInterface))
	if app.Options.bind == nil || !app.Options.bind.single {
		t.Fatal("form should show scope single=true")
	}
	app.Options.bind.single = false
	app.Options.bind.applyInterface(app.Options)
	if app.Options.rebuildFocus != interfaceFocusKey("single") {
		t.Fatalf("rebuildFocus=%q", app.Options.rebuildFocus)
	}
	prefs := loadPrefs()
	if prefs.Single {
		t.Fatal("scope TUI single should be persisted as false")
	}
}
