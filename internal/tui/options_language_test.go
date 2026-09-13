package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/i18n"
)

// useInterfaceLanguage pins the language inputs so only the bound config language decides the copy.
func useInterfaceLanguage(t *testing.T, bound string) {
	t.Helper()
	config.ApplyLanguageArgument(nil)
	t.Setenv(config.EnvLangCLI, "")
	t.Setenv(config.EnvLang, "en_US.UTF-8")
	config.BindConfigLanguage(&config.Config{Language: bound})
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
}

func englishConfig() *config.Config {
	stored := config.DefaultConfig()
	stored.WelcomeComplete = true
	stored.Language = "en"
	return stored
}

// openLanguageField opens the interface page with focus on the language selector and moves it one step right.
func openLanguageField(t *testing.T, panel *optionsPanel) {
	t.Helper()
	pumpPanel(panel, panel.dispatch(sectionInterface))
	if panel.bind.fieldIndex[interfaceFocusKey("language")] != 0 {
		t.Fatal("setup: language is not the first interface field")
	}
	drivePanel(panel, keyMsg("right"))
}

func panelText(panel *optionsPanel) string {
	_, view := panel.view()
	return ansi.Strip(view)
}

func assertLanguageCopy(t *testing.T, app *App, language string) {
	t.Helper()
	if got := config.ResolveLanguage(); got != language {
		t.Fatalf("resolved language=%s want %s", got, language)
	}
	if want := i18n.Text(language, "tui.task_board"); app.Context.Title != want {
		t.Fatalf("page context title=%q want %q", app.Context.Title, want)
	}
}

func TestLanguageChangeRedrawsFormAndPersistsOnSubmit(t *testing.T) {
	useInterfaceLanguage(t, "en")
	app, panel := openPanel(t, englishConfig())
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	openLanguageField(t, panel)

	if panel.session.Config.Language != "ja" {
		t.Fatalf("session language=%s", panel.session.Config.Language)
	}
	assertLanguageCopy(t, app, "ja")
	if form := ansi.Strip(panel.form.View()); !strings.Contains(form, i18n.Text("ja", "menu.default_language")) {
		t.Fatalf("current form kept the old language:\n%s", form)
	}
	if panel.form.GetFocusedField() != focusableFieldAt(panel.bind, panel.bind.fieldIndex[interfaceFocusKey("language")]) {
		t.Fatal("focus left the language selector after the redraw")
	}

	drivePanel(panel, keyMsg("enter"))
	plain := panelText(panel)
	for _, id := range []string{"tui.interface", "tui.tab_global", "tui.tab_project"} {
		if !strings.Contains(plain, i18n.Text("ja", id)) {
			t.Fatalf("root menu or tabs missing %s in the new language:\n%s", id, plain)
		}
	}
	scope, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scope.Language != "ja" {
		t.Fatalf("disk language=%s want ja", scope.Language)
	}

	pumpPanel(panel, panel.dispatch(sectionInterface))
	drivePanel(panel, keyMsg("esc"))
	if panel.session.Config.Language != "ja" {
		t.Fatalf("esc after submit restored language %s", panel.session.Config.Language)
	}
	assertLanguageCopy(t, app, "ja")
}

func TestLanguageChangeRefreshesAgentChoiceCopy(t *testing.T) {
	useInterfaceLanguage(t, "en")
	t.Chdir(t.TempDir())
	// An empty PATH makes every probed agent "not currently installed" without running any CLI.
	t.Setenv("PATH", t.TempDir())
	for _, name := range config.ExecutionAgents {
		t.Setenv(strings.ToUpper(name)+"_REVIEW_BIN", "")
	}
	_ = newTestSession(t, englishConfig())
	app := newPanelApp(t)
	app.openOptions()
	finishOptionsLoad(t, app)
	panel := app.Options
	agentLabel := func() string {
		current := panel.session.Config.KanbanAgents["large"]
		for _, choice := range panel.session.ExecutionChoicesFor(current) {
			if choice.Value == current {
				return choice.Label
			}
		}
		t.Fatalf("no execution choice for %q", current)
		return ""
	}
	if !strings.HasSuffix(agentLabel(), i18n.Text("en", "menu.not_currently_installed")) {
		t.Fatalf("setup: agent label=%q", agentLabel())
	}
	openLanguageField(t, panel)
	if !strings.HasSuffix(agentLabel(), i18n.Text("ja", "menu.not_currently_installed")) {
		t.Fatalf("agent label kept the old language: %q", agentLabel())
	}
}

func TestLanguageEscapeRestoresGlobalLanguage(t *testing.T) {
	useInterfaceLanguage(t, "en")
	app, panel := openPanel(t, englishConfig())
	openLanguageField(t, panel)
	assertLanguageCopy(t, app, "ja")

	drivePanel(panel, keyMsg("esc"))
	if panel.current != "" {
		t.Fatalf("esc should return to the root menu, got %q", panel.current)
	}
	if panel.session.Config.Language != "en" {
		t.Fatalf("session language=%s want en", panel.session.Config.Language)
	}
	assertLanguageCopy(t, app, "en")
	if plain := panelText(panel); !strings.Contains(plain, i18n.Text("en", "tui.interface")) {
		t.Fatalf("root menu not redrawn in the restored language:\n%s", plain)
	}
	scope, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scope.Language != "en" {
		t.Fatalf("cancel changed disk language to %s", scope.Language)
	}
}

func TestProjectLanguageCancelAndSubmit(t *testing.T) {
	t.Run("cancel keeps a missing overlay key missing", func(t *testing.T) {
		useInterfaceLanguage(t, "en")
		app, panel := openPanel(t, englishConfig())
		attachTempOverlay(t, panel.session, config.ModeGlobal)
		pumpPanel(panel, panel.switchTab(config.TargetOverlay))
		openLanguageField(t, panel)
		if !panel.session.FieldOverridden("language") {
			t.Fatal("setup: language edit did not create the overlay key")
		}
		drivePanel(panel, keyMsg("esc"))
		if panel.session.FieldOverridden("language") {
			t.Fatal("cancel left a language key in the overlay")
		}
		if panel.session.Config.Language != "en" {
			t.Fatalf("project language=%s want inherited en", panel.session.Config.Language)
		}
		assertLanguageCopy(t, app, "en")
	})

	t.Run("cancel restores an existing overlay value", func(t *testing.T) {
		useInterfaceLanguage(t, "cn")
		app, panel := openPanel(t, englishConfig())
		dir := t.TempDir()
		loc := config.OverlayLocation{ProjectRoot: dir, Path: filepath.Join(dir, config.OverlayFilename)}
		if err := panel.session.AttachOverlay(config.ModeGlobal, loc, map[string]any{"language": "cn"}); err != nil {
			t.Fatal(err)
		}
		pumpPanel(panel, panel.switchTab(config.TargetOverlay))
		openLanguageField(t, panel)
		if panel.session.Config.Language == "cn" {
			t.Fatal("setup: language did not change")
		}
		drivePanel(panel, keyMsg("esc"))
		if !panel.session.FieldOverridden("language") || panel.session.Config.Language != "cn" {
			t.Fatalf("overlay language=%s overridden=%v want cn", panel.session.Config.Language, panel.session.FieldOverridden("language"))
		}
		assertLanguageCopy(t, app, "cn")
	})

	t.Run("submit writes only the overlay", func(t *testing.T) {
		useInterfaceLanguage(t, "en")
		_, panel := openPanel(t, englishConfig())
		_, path := attachTempOverlay(t, panel.session, config.ModeGlobal)
		pumpPanel(panel, panel.switchTab(config.TargetOverlay))
		openLanguageField(t, panel)
		drivePanel(panel, keyMsg("enter"))
		raw, err := config.ReadOverlayFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if raw["language"] != "ja" {
			t.Fatalf("overlay %#v", raw)
		}
		scope, err := config.LoadScope(true)
		if err != nil {
			t.Fatal(err)
		}
		if scope.Language != "en" {
			t.Fatalf("project submit wrote scope language %s", scope.Language)
		}
	})
}

func TestCloseDiscardRestoresLanguage(t *testing.T) {
	useInterfaceLanguage(t, "en")
	app, panel := openPanel(t, englishConfig())
	openLanguageField(t, panel)
	pumpPanel(panel, panel.requestClose())
	if !panel.confirming {
		t.Fatal("setup: unsaved language did not ask before closing")
	}
	panel.closeChoice = closeDiscard
	pumpPanel(panel, panel.finishCloseConfirm())
	if app.Options != nil {
		t.Fatal("discard should close the panel")
	}
	if panel.session.Config.Language != "en" {
		t.Fatalf("session language=%s want en", panel.session.Config.Language)
	}
	assertLanguageCopy(t, app, "en")
}

func TestLanguageCancelIgnoresBindingFromPartialSave(t *testing.T) {
	t.Run("project submit leaves the unsaved global language cancellable", func(t *testing.T) {
		useInterfaceLanguage(t, "en")
		app, panel := openPanel(t, englishConfig())
		attachTempOverlay(t, panel.session, config.ModeGlobal)
		openLanguageField(t, panel)
		pumpPanel(panel, panel.switchTab(config.TargetOverlay))
		drivePanel(panel, keyMsg("enter"))
		if !panel.session.ScopeDirty || panel.session.OverlayDirty {
			t.Fatalf("setup: scope dirty=%v overlay dirty=%v", panel.session.ScopeDirty, panel.session.OverlayDirty)
		}
		pumpPanel(panel, panel.requestClose())
		panel.closeChoice = closeDiscard
		pumpPanel(panel, panel.finishCloseConfirm())
		if app.Options != nil {
			t.Fatal("discard should close the panel")
		}
		assertLanguageCopy(t, app, "en")
		scope, err := config.LoadScope(true)
		if err != nil {
			t.Fatal(err)
		}
		if scope.Language != "en" {
			t.Fatalf("disk language=%s want en", scope.Language)
		}
	})

	t.Run("failed submit keeps the load baseline", func(t *testing.T) {
		useInterfaceLanguage(t, "en")
		app, panel := openPanel(t, englishConfig())
		openLanguageField(t, panel)
		path, err := config.ConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		drivePanel(panel, keyMsg("enter"))
		if panel.report == nil || panel.current != sectionInterface {
			t.Fatal("setup: submit did not fail on the interface page")
		}
		drivePanel(panel, keyMsg("esc"))
		drivePanel(panel, keyMsg("esc"))
		if panel.current != "" {
			t.Fatalf("esc should return to the root menu, got %q", panel.current)
		}
		if panel.session.Config.Language != "en" {
			t.Fatalf("session language=%s want en", panel.session.Config.Language)
		}
		assertLanguageCopy(t, app, "en")
	})
}

func TestLanguageCancelAfterSubmitRestoresSubmittedBinding(t *testing.T) {
	useInterfaceLanguage(t, "cn")
	app, panel := openPanel(t, englishConfig())
	dir := t.TempDir()
	loc := config.OverlayLocation{ProjectRoot: dir, Path: filepath.Join(dir, config.OverlayFilename)}
	if err := panel.session.AttachOverlay(config.ModeGlobal, loc, map[string]any{"language": "cn"}); err != nil {
		t.Fatal(err)
	}
	openLanguageField(t, panel)
	drivePanel(panel, keyMsg("enter"))
	assertLanguageCopy(t, app, "ja")

	pumpPanel(panel, panel.dispatch(sectionInterface))
	drivePanel(panel, keyMsg("left"))
	if panel.session.Config.Language == "ja" {
		t.Fatal("setup: second edit did not change the language")
	}
	drivePanel(panel, keyMsg("esc"))
	if panel.session.Config.Language != "ja" {
		t.Fatalf("session language=%s want submitted ja", panel.session.Config.Language)
	}
	assertLanguageCopy(t, app, "ja")
}
