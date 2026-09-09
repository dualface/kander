package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func tempOverlaySession(t *testing.T, mode config.Mode) (*Session, string) {
	t.Helper()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	cfg.TUI.Theme = "light"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	loc := config.OverlayLocation{ProjectRoot: dir, Path: filepath.Join(dir, config.OverlayFilename)}
	if err := session.AttachOverlay(mode, loc, nil); err != nil {
		t.Fatal(err)
	}
	return session, loc.Path
}

func TestSessionOverlaySaveIsSparseAndIsolated(t *testing.T) {
	session, path := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.SetLanguage("ja")
	session.SetTUIField("theme", "dark")
	saved, err := session.Save()
	if err != nil {
		t.Fatal(err)
	}
	if saved != path {
		t.Fatalf("saved %s", saved)
	}
	raw, err := config.ReadOverlayFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if raw["language"] != "ja" {
		t.Fatalf("overlay %#v", raw)
	}
	if _, ok := raw["kanban_agent"]; ok {
		t.Fatal("inherited key copied")
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.Language == "ja" || scopeCfg.TUI.Theme == "dark" {
		t.Fatalf("scope leaked: %+v", scopeCfg)
	}
}

func TestSessionRestoreInheritDeletesOnlyThatKey(t *testing.T) {
	session, path := tempOverlaySession(t, config.ModeProject)
	session.SetLanguage("ja")
	session.SetTUIField("theme", "dark")
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	if err := session.RestoreInherit("language"); err != nil {
		t.Fatal(err)
	}
	if session.Config.Language != "en" {
		t.Fatalf("effective language=%s", session.Config.Language)
	}
	if !session.FieldOverridden("tui", "theme") {
		t.Fatal("theme override lost")
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := config.ReadOverlayFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["language"]; ok {
		t.Fatal("language key remained")
	}
	theme, ok := raw["tui"].(map[string]any)
	if !ok || theme["theme"] != "dark" {
		t.Fatalf("theme override missing: %#v", raw)
	}
}

func TestSessionSeedReviewDoesNotCreateOverlay(t *testing.T) {
	session, path := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	_ = session.ReviewModelFieldsFor("PM")
	if len(session.overlayRaw) != 0 {
		t.Fatalf("seed wrote overlay: %#v", session.overlayRaw)
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("browse/save created an overlay file")
	}
}

func TestSessionTabSwitchKeepsUnsavedEdits(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	session.SetLanguage("ja")
	if !session.ScopeDirty {
		t.Fatal("scope edit should be dirty")
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.SetLauncher("foreground")
	if err := session.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	if session.Config.Language != "ja" {
		t.Fatalf("scope language lost: %s", session.Config.Language)
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	if session.Config.Launcher != "foreground" {
		t.Fatalf("overlay launcher lost: %s", session.Config.Launcher)
	}
	if !config.OverlayHas(session.overlayRaw, "launcher") {
		t.Fatal("overlay key missing after tab switch")
	}
	if config.OverlayHas(session.overlayRaw, "language") {
		t.Fatal("scope language copied into overlay")
	}
}

func TestSessionExplicitEqualOverrideIsKept(t *testing.T) {
	session, path := tempOverlaySession(t, config.ModeProject)
	session.SetLanguage("en")
	if !session.FieldOverridden("language") {
		t.Fatal("equal value must still be an override")
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := config.ReadOverlayFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if raw["language"] != "en" {
		t.Fatalf("explicit equal override dropped: %#v", raw)
	}
}

func TestProjectInstallSaveDoesNotWriteScopeFile(t *testing.T) {
	session, overlay := tempOverlaySession(t, config.ModeProject)
	scopePath := os.Getenv(config.EnvConfig)
	before, err := os.ReadFile(scopePath)
	if err != nil {
		t.Fatal(err)
	}
	session.SetLanguage("ja")
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(scopePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("project-install save rewrote scope:\n%s", after)
	}
	raw, err := config.ReadOverlayFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if raw["language"] != "ja" {
		t.Fatalf("overlay %#v", raw)
	}
}

func TestSaveAllDirtyWritesBothTabs(t *testing.T) {
	session, overlay := tempOverlaySession(t, config.ModeGlobal)
	session.SetLanguage("ja")
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.SetLauncher("foreground")
	if _, err := session.SaveAllDirty(); err != nil {
		t.Fatal(err)
	}
	if session.HasUnsaved() {
		t.Fatal("explicit save left a dirty tab")
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.Language != "ja" {
		t.Fatalf("scope language=%s", scopeCfg.Language)
	}
	raw, err := config.ReadOverlayFile(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if raw["launcher"] != "foreground" {
		t.Fatalf("overlay %#v", raw)
	}
	if raw["language"] != nil {
		t.Fatal("scope language copied into overlay")
	}
}

func TestGlobalEmptyAgentPathDoesNotDeleteOverlay(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	config.OverlaySet(session.overlayRaw, "/bin/true", "agents", "codex", "path")
	session.OverlayDirty = true
	if !session.FieldOverridden("agents", "codex", "path") {
		t.Fatal("overlay path missing")
	}
	if err := session.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	session.NoteModelOverride(ModelField{Agent: "codex", field: "path"}, "")
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	if !session.FieldOverridden("agents", "codex", "path") {
		t.Fatal("global empty path deleted the project overlay key")
	}
}

func TestRestoreFlatReviewStageDeletesLegacyKey(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.overlayRaw["review_stages"] = map[string]any{"PM": "skip"}
	if err := session.RestoreInherit("review_stages", "large", "PM"); err != nil {
		t.Fatal(err)
	}
	if session.FieldOverridden("review_stages", "large", "PM") {
		t.Fatalf("flat review stage remained: %#v", session.overlayRaw)
	}
}
