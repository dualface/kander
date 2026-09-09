package tui

import (
	"encoding/json"
	"os"
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

func TestProjectLanguageEditDoesNotOverrideDerivedAgentLanguage(t *testing.T) {
	_, panel := openPanel(t)
	basePath, err := config.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	baseData, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(baseData, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "agent_language")
	raw["language"] = "en"
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(basePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	initial, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	panel.session, err = menu.NewSessionForTest(initial)
	if err != nil {
		t.Fatal(err)
	}
	_, path := attachTempOverlay(t, panel.session, config.ModeProject)
	pumpPanel(panel, panel.openSection(sectionInterface))
	panel.bind.language = "ja"
	panel.bind.apply(panel)
	pumpPanel(panel, panel.rebuildSection())
	if panel.session.FieldOverridden("agent_language") || panel.bind.agentLanguage != "ja" {
		t.Fatalf("unedited derived language became an override: %q", panel.bind.agentLanguage)
	}
	if _, err := panel.session.Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := config.ReadOverlayFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved["language"] != "ja" {
		t.Fatalf("unexpected overrides: %#v", saved)
	}
}

func TestModelDraftSurvivesConfigReplacementAndFormRebuild(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeProject)
	fields := panel.session.ExecutionModelFieldsFor("large")
	model, effort := fields[0], fields[1]
	model.Set("first-model")
	panel.session.NoteModelOverride(model, "first-model")
	pumpPanel(panel, panel.openSection(sectionExecution))
	find := func(key string) int {
		for i, f := range panel.bind.modelFields {
			if f.Key() == key {
				return i
			}
		}
		t.Fatalf("missing field %s", key)
		return -1
	}
	*panel.bind.modelValues[find(model.Key())] = "second-model"
	panel.bind.modelInputs[model.Key()].input.Value(panel.bind.modelValues[find(model.Key())])
	panel.bind.apply(panel)
	*panel.bind.modelValues[find(effort.Key())] = ""
	panel.bind.modelInputs[effort.Key()].input.Value(panel.bind.modelValues[find(effort.Key())])
	panel.bind.apply(panel)
	pumpPanel(panel, panel.rebuildSection())
	if got := *panel.bind.modelValues[find(effort.Key())]; got != "" {
		t.Fatalf("invalid draft hidden by stale map: %q", got)
	}
	if _, err := panel.session.Save(); err == nil {
		t.Fatal("invalid effort saved")
	}
	*panel.bind.modelValues[find(effort.Key())] = "high"
	panel.bind.modelInputs[effort.Key()].input.Value(panel.bind.modelValues[find(effort.Key())])
	panel.bind.apply(panel)
	if _, err := panel.session.Save(); err != nil {
		t.Fatal(err)
	}
	if got := panel.session.Config.Models.Kanban[model.Agent][model.FieldName()]; got != "second-model" {
		t.Fatalf("earlier edit lost: %q", got)
	}
}
