package menu

import (
	"encoding/json"
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

func TestRestoreFlatReviewStageKeepsOtherScale(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.overlayRaw["review_stages"] = map[string]any{"PM": "skip"}
	if err := session.RestoreInherit("review_stages", "large", "PM"); err != nil {
		t.Fatal(err)
	}
	if session.FieldOverridden("review_stages", "large", "PM") {
		t.Fatalf("large PM still overridden: %#v", session.overlayRaw)
	}
	if !session.FieldOverridden("review_stages", "small", "PM") {
		t.Fatalf("small PM override lost: %#v", session.overlayRaw)
	}
	large, err := config.ReviewStageFor(session.Config, "large", "PM")
	if err != nil {
		t.Fatal(err)
	}
	small, err := config.ReviewStageFor(session.Config, "small", "PM")
	if err != nil {
		t.Fatal(err)
	}
	if large != "auto" || small != "skip" {
		t.Fatalf("large=%s small=%s", large, small)
	}
}

func TestRestoreInheritRollsBackInvalidRules(t *testing.T) {
	session, _ := gitOffOverlaySession(t)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.overlayRaw["rules"] = map[string]any{"git": true, "task_groups": true}
	if err := session.rebuildOverlayConfig(); err != nil {
		t.Fatal(err)
	}
	if err := session.RestoreInherit("rules", "git"); err == nil {
		t.Fatal("restoring git under task_groups should fail")
	}
	if !config.OverlayHas(session.overlayRaw, "rules", "git") {
		t.Fatalf("failed restore deleted git: %#v", session.overlayRaw)
	}
	if !config.OverlayHas(session.overlayRaw, "rules", "task_groups") {
		t.Fatal("failed restore dropped task_groups")
	}
}

func TestSetTargetRollsBackWhenOverlayInvalid(t *testing.T) {
	session, _ := gitOffOverlaySession(t)
	session.overlayRaw["rules"] = map[string]any{"task_groups": true}
	if err := session.SetTarget(config.TargetOverlay); err == nil {
		t.Fatal("invalid overlay should keep the global tab")
	}
	if session.Target != config.TargetScope {
		t.Fatalf("target=%s", session.Target)
	}
}

func gitOffOverlaySession(t *testing.T) (*Session, string) {
	t.Helper()
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Rules[config.RuleGit] = false
	cfg.Rules[config.RuleTaskGroups] = false
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session, err := NewSessionForTest(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	loc := config.OverlayLocation{ProjectRoot: dir, Path: filepath.Join(dir, config.OverlayFilename)}
	if err := session.AttachOverlay(config.ModeGlobal, loc, nil); err != nil {
		t.Fatal(err)
	}
	return session, loc.Path
}

func TestFlatReviewStagesSaveDoesNotConflict(t *testing.T) {
	session, path := tempOverlaySession(t, config.ModeGlobal)
	flat := map[string]any{"review_stages": map[string]any{"PM": "skip"}}
	if _, err := config.SaveOverlayIfUnchanged(path, flat, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := session.AttachOverlay(config.ModeGlobal, config.OverlayLocation{ProjectRoot: filepath.Dir(path), Path: path, Exists: true}, flat); err != nil {
		t.Fatal(err)
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.SetLauncher("foreground")
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveAllDirtySetsWelcomeComplete(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	session.scopeConfig.WelcomeComplete = false
	session.Config.WelcomeComplete = false
	session.SetLanguage("ja")
	if _, err := session.SaveAllDirty(); err != nil {
		t.Fatal(err)
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if !scopeCfg.WelcomeComplete {
		t.Fatal("scope save left welcome_complete false")
	}
}

func TestOverlayRuleEditSyncsEffectiveConfig(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeProject)
	scopePath := os.Getenv(config.EnvConfig)
	raw, err := config.LoadScopeDocument(true)
	if err != nil {
		t.Fatal(err)
	}
	delete(raw, "rules")
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scopePath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	session.scopeRaw = nil
	if err := session.ensureScopeRaw(); err != nil {
		t.Fatal(err)
	}
	if err := session.rebuildOverlayConfig(); err != nil {
		t.Fatal(err)
	}
	rules := session.Config.Rules.Clone()
	rules[config.RuleCode] = false
	if err := session.SetRules(rules); err != nil {
		t.Fatal(err)
	}
	if session.Config.Rules[config.RuleGit] {
		t.Fatal("effective git should follow raw merge, not the filled UI copy")
	}
	if !config.OverlayHas(session.overlayRaw, "rules", "code") {
		t.Fatal("code override missing")
	}
	if config.OverlayHas(session.overlayRaw, "rules", "git") {
		t.Fatal("inherited git copied into overlay")
	}
}

func TestUnsavedGlobalLauncherUpdatesProjectInherit(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	session.SetLauncher("foreground")
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	if session.Config.Launcher != "foreground" {
		t.Fatalf("project inherit launcher=%s", session.Config.Launcher)
	}
	if config.OverlayHas(session.overlayRaw, "launcher") {
		t.Fatal("unsaved global launcher copied into overlay")
	}
}

func TestResetReviewRoleOnOverlayDoesNotSeed(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	session.overlayRaw["models"] = map[string]any{"review_roles": map[string]any{"PM": map[string]any{"model": "old"}}}
	session.ResetReviewRoleModel("PM")
	if session.FieldOverridden("models", "review_roles", "PM") {
		t.Fatalf("reset seeded overlay: %#v", session.overlayRaw)
	}
}

func TestGlobalTUISyncUpdatesProjectInheritance(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	value := session.Config.TUI
	value.Theme = "dark"
	session.SyncTUI(value, true)
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	if session.Config.TUI.Theme != "dark" {
		t.Fatal("project kept old global theme")
	}
	session.SetTUIField("theme", "light")
	if err := session.RestoreInherit("tui", "theme"); err != nil {
		t.Fatal(err)
	}
	if session.Config.TUI.Theme != "dark" {
		t.Fatal("restore kept old global theme")
	}
}

func TestInvalidOverlayEditRemainsDirtyAndCannotSave(t *testing.T) {
	session, path := tempOverlaySession(t, config.ModeProject)
	delete(session.scopeRaw, "tui")
	data, _ := json.Marshal(session.scopeRaw)
	if err := os.WriteFile(os.Getenv(config.EnvConfig), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SetTUIField("theme", "dark"); err == nil {
		t.Fatal("missing merge error")
	}
	if !session.OverlayDirty || session.Config.TUI.Theme != "dark" || !session.FieldOverridden("tui", "theme") {
		t.Fatal("failed edit lost")
	}
	if _, err := session.Save(); err == nil {
		t.Fatal("invalid edit reported saved")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid edit created overlay")
	}
	if !session.OverlayDirty {
		t.Fatal("failed save cleared dirty state")
	}
	if err := session.RestoreInherit("tui", "theme"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidOverlayDraftRestoresFieldsAcrossTabs(t *testing.T) {
	session, path := tempOverlaySession(t, config.ModeGlobal)
	delete(session.scopeRaw, "tui")
	data, _ := json.Marshal(session.scopeRaw)
	if err := os.WriteFile(os.Getenv(config.EnvConfig), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	inherited := session.Config.TUI
	if err := session.SetTUIField("theme", "dark"); err == nil {
		t.Fatal("expected incomplete section error")
	}
	if err := session.SetTUIField("refresh", 17); err == nil {
		t.Fatal("expected second incomplete section error")
	}
	if err := session.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	session.SetLauncher("foreground")
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatalf("cannot reopen invalid draft: %v", err)
	}
	if session.Config.Launcher != "foreground" || session.FieldOverridden("launcher") {
		t.Fatal("draft did not inherit the latest Global launcher")
	}
	if session.Config.TUI.Refresh != 17 || session.Config.TUI.Theme != "dark" {
		t.Fatal("tab switch discarded draft values")
	}
	if err := session.RestoreInherit("tui", "theme"); err != nil {
		t.Fatal(err)
	}
	if session.FieldOverridden("tui", "theme") || session.Config.TUI.Theme != inherited.Theme {
		t.Fatal("theme did not return to inheritance")
	}
	if !session.FieldOverridden("tui", "refresh") || session.Config.TUI.Refresh != 17 {
		t.Fatal("restoring theme lost the other rejected edit")
	}
	if _, err := session.Save(); err == nil {
		t.Fatal("partial restoration made invalid draft saveable")
	}
	if err := session.RestoreInherit("tui", "refresh"); err != nil {
		t.Fatal(err)
	}
	if session.Config.TUI != inherited || session.overlayDraft {
		t.Fatal("final restoration did not return to the valid view")
	}
	if _, err := session.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("restoring all draft edits created an overlay")
	}
}

func TestDraftProjectionPreservesRawRulesAndExplicitLanguage(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	delete(session.scopeRaw, "tui")
	delete(session.scopeRaw, "rules")
	data, _ := json.Marshal(session.scopeRaw)
	if err := os.WriteFile(os.Getenv(config.EnvConfig), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	rules := session.Config.Rules.Clone()
	rules[config.RuleCode] = false
	if err := session.SetRules(rules); err != nil {
		t.Fatal(err)
	}
	session.SetLanguage("ja")
	if err := session.SetTUIField("theme", "dark"); err == nil {
		t.Fatal("expected invalid TUI draft")
	}
	if err := session.SetTarget(config.TargetScope); err != nil {
		t.Fatal(err)
	}
	session.SetLauncher("foreground")
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	if session.Config.Rules[config.RuleGit] || session.FieldOverridden("rules", "git") {
		t.Fatal("draft projection filled missing rule keys")
	}
	if session.Config.Language != "ja" || !session.FieldOverridden("language") {
		t.Fatal("draft projection lost explicit language")
	}
	if session.Config.Launcher != "foreground" || session.FieldOverridden("launcher") {
		t.Fatal("draft projection froze inherited launcher")
	}
	if session.Config.TUI.Theme != "dark" {
		t.Fatal("draft projection lost rejected input")
	}
	if _, err := session.Save(); err == nil {
		t.Fatal("presentation bypassed raw validation")
	}
}

func TestGlobalReviewerResetUpdatesProjectModelInheritance(t *testing.T) {
	session, _ := tempOverlaySession(t, config.ModeGlobal)
	session.Config.Models.Review["claude"] = map[string]string{"model": "claude-model", "effort": "high"}
	session.Config.Models.ReviewRoles["PM"] = map[string]string{"model": "old-codex", "effort": "low"}
	session.scopeRaw, _ = config.DocumentFromConfig(session.Config)
	session.SetReviewer("PM", "claude")
	session.ResetReviewRoleModel("PM")
	if err := session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	entry := session.Config.Models.ReviewRoles["PM"]
	if entry["model"] != "claude-model" || entry["effort"] != "high" {
		t.Fatalf("inherited stale role defaults: %#v", entry)
	}
	if session.FieldOverridden("models") {
		t.Fatal("reset created project overrides")
	}
	fields := session.ReviewModelFieldsFor("PM")
	fields[0].Set("project-model")
	session.NoteModelOverride(fields[0], "project-model")
	if err := session.RestoreInherit("models", "review_roles", "PM", "model"); err != nil {
		t.Fatal(err)
	}
	if got := session.Config.Models.ReviewRoles["PM"]["model"]; got != "claude-model" {
		t.Fatalf("restore used old reviewer defaults: %q", got)
	}
}
