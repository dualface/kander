package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/menu"
)

func uiText(id string) string {
	return t(id)
}

func attachTempOverlay(t *testing.T, session *menu.Session, mode config.Mode) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, config.OverlayFilename)
	if err := session.AttachOverlay(mode, config.OverlayLocation{ProjectRoot: dir, Path: path}, nil); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func TestOptionsTabsFollowInstallMode(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	pumpPanel(panel, panel.openRoot())
	_, view := panel.view()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, uiText("tui.tab_global")) || !strings.Contains(plain, uiText("tui.tab_project")) {
		t.Fatalf("global install should show both tabs:\n%s", plain)
	}
	if !strings.Contains(plain, config.OverlayFilename) && !strings.Contains(plain, ".kander-config") {
		t.Fatalf("missing overlay path:\n%s", plain)
	}

	_, panel = openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeProject)
	pumpPanel(panel, panel.openRoot())
	_, view = panel.view()
	plain = ansi.Strip(view)
	if strings.Contains(plain, uiText("tui.tab_global")) && strings.Contains(plain, uiText("tui.tab_project")) {
		t.Fatalf("project install should hide the tab bar:\n%s", plain)
	}
	if !strings.Contains(plain, uiText("tui.tab_project")) && !strings.Contains(plain, filepath.Base(config.OverlayFilename)) {
		if !strings.Contains(plain, config.OverlayFilename) {
			t.Fatalf("project tab should still show the overlay path:\n%s", plain)
		}
	}
}

func TestOptionsTabSwitchKeepsEditsAndDoesNotSave(t *testing.T) {
	_, panel := openPanel(t)
	_, path := attachTempOverlay(t, panel.session, config.ModeGlobal)
	pumpPanel(panel, panel.openRoot())
	panel.session.SetLauncher("foreground")
	panel.markDirty()
	drivePanel(panel, keyMsg("]"))
	if panel.session.Target != config.TargetOverlay {
		t.Fatalf("target=%s", panel.session.Target)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("tab switch created an overlay")
	}
	panel.session.SetLauncher("herdr")
	drivePanel(panel, keyMsg("["))
	if panel.session.Target != config.TargetScope {
		t.Fatalf("target=%s", panel.session.Target)
	}
	if panel.session.Config.Launcher != "foreground" {
		t.Fatalf("scope edit lost: %s", panel.session.Config.Launcher)
	}
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	if panel.session.Config.Launcher != "herdr" {
		t.Fatalf("overlay edit lost: %s", panel.session.Config.Launcher)
	}
	if !panel.session.FieldOverridden("launcher") {
		t.Fatal("overlay key missing after tab switch")
	}
}

func TestProjectTabSaveWritesOverlayOnly(t *testing.T) {
	_, panel := openPanel(t)
	_, path := attachTempOverlay(t, panel.session, config.ModeGlobal)
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	panel.session.SetLauncher("foreground")
	panel.markDirty()
	if err := panel.persistNow(); err != nil {
		t.Fatal(err)
	}
	raw, err := config.ReadOverlayFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if raw["launcher"] != "foreground" {
		t.Fatalf("overlay %#v", raw)
	}
	if _, ok := raw["kanban_agent"]; ok {
		t.Fatal("inherited key copied")
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.Launcher == "foreground" {
		t.Fatal("project save wrote launcher into the scope file")
	}
}

func TestProjectBrowseAndReviewDoesNotCreateOverlay(t *testing.T) {
	_, panel := openPanel(t)
	_, path := attachTempOverlay(t, panel.session, config.ModeGlobal)
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	pumpPanel(panel, panel.dispatch(sectionReview))
	drivePanel(panel, keyMsg("esc"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("opening review created an overlay")
	}
}

func TestInheritedPrefixOnProjectTab(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	pumpPanel(panel, panel.dispatch(sectionInterface))
	_, view := panel.view()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, uiText("tui.inherit_global")) {
		t.Fatalf("missing inherit prefix:\n%s", plain)
	}
}

func TestProjectInstallInheritPrefixUsesDefault(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeProject)
	pumpPanel(panel, panel.dispatch(sectionInterface))
	_, view := panel.view()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, uiText("tui.inherit_default")) {
		t.Fatalf("missing default prefix:\n%s", plain)
	}
}

func TestOverlaySaveFailureKeepsEdits(t *testing.T) {
	_, panel := openPanel(t)
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o555); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocked, config.OverlayFilename)
	if err := panel.session.AttachOverlay(config.ModeGlobal, config.OverlayLocation{ProjectRoot: dir, Path: path}, nil); err != nil {
		t.Fatal(err)
	}
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	panel.session.SetLauncher("foreground")
	panel.markDirty()
	if err := panel.persistNow(); err == nil {
		t.Fatal("expected save failure")
	}
	if panel.session.Config.Launcher != "foreground" {
		t.Fatal("failed save dropped the edit")
	}
	if !panel.session.OverlayDirty {
		t.Fatal("failed save cleared overlay dirty")
	}
}

func TestScopeChromeFitsNarrowScreen(t *testing.T) {
	app, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	app.Width, app.Height = 72, 12
	pumpPanel(panel, panel.openRoot())
	_, view := panel.view()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, config.OverlayFilename) {
		t.Fatalf("narrow view dropped overlay path:\n%s", plain)
	}
}

func TestSaveAndApplyPersistsBothDirtyTabs(t *testing.T) {
	_, panel := openPanel(t)
	_, path := attachTempOverlay(t, panel.session, config.ModeGlobal)
	panel.session.SetLanguage("ja")
	panel.markDirty()
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	panel.session.SetLauncher("foreground")
	panel.markDirty()
	panel.save()
	if panel.session.HasUnsaved() {
		t.Fatal("save-and-apply left a dirty tab")
	}
	raw, err := config.ReadOverlayFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if raw["launcher"] != "foreground" {
		t.Fatalf("overlay %#v", raw)
	}
	scopeCfg, err := config.LoadScope(true)
	if err != nil {
		t.Fatal(err)
	}
	if scopeCfg.Language != "ja" {
		t.Fatalf("scope language=%s", scopeCfg.Language)
	}
}

func TestRestoreTUIInheritRevertsAppTheme(t *testing.T) {
	app, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	inherited := panel.session.Config.TUI.Theme
	override := "light"
	if inherited == "light" {
		override = "dark"
	}
	app.Theme = override
	panel.session.SetTUIField("theme", override)
	if err := panel.session.RestoreInherit("tui", "theme"); err != nil {
		t.Fatal(err)
	}
	panel.syncAppFromSession()
	if panel.session.FieldOverridden("tui", "theme") {
		t.Fatal("theme still overridden")
	}
	if app.Theme != inherited || panel.session.Config.TUI.Theme != inherited {
		t.Fatalf("app theme=%s effective=%s inherited=%s", app.Theme, panel.session.Config.TUI.Theme, inherited)
	}
}

func TestMouseClickAccountsForScopeChrome(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	pumpPanel(panel, panel.openRoot())
	panel.view()
	if panel.chromeLines < 1 {
		t.Fatal("expected scope chrome")
	}
	formLines := panel.currentBodyLines()
	lo, _, ok := focusRange(formLines)
	if !ok {
		t.Fatal("no focused row")
	}
	target := lo + 2
	if target >= len(formLines) {
		t.Skip("popup too short for this assertion")
	}
	pumpPanel(panel, panel.HandleMouse(panel.bodyX+1, panel.bodyY+panel.chromeLines+target, mouseBtn1Clicked))
	moved, _, ok := focusRange(panel.currentBodyLines())
	if !ok || moved != target {
		t.Fatalf("click through chrome should focus form row %d, focus is %d", target, moved)
	}
}

func TestCloseConfirmDoesNotPaintFallbackNotice(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	panel.detectOverlayNotice()
	panel.confirming = true
	panel.view()
	if panel.chromeLines != 0 {
		t.Fatalf("confirm chromeLines=%d", panel.chromeLines)
	}
}

func TestCloseConfirmClearsTabHits(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	pumpPanel(panel, panel.openRoot())
	panel.view()
	if len(panel.tabHits) < 2 {
		t.Fatalf("tabHits=%d", len(panel.tabHits))
	}
	hit := panel.tabHits[1]
	x := panel.bodyX + (hit.x0+hit.x1)/2
	y := panel.bodyY
	panel.confirming = true
	panel.closeChoice = closeSave
	panel.view()
	if len(panel.tabHits) != 0 {
		t.Fatalf("confirm left tabHits=%d", len(panel.tabHits))
	}
	pumpPanel(panel, panel.HandleMouse(x, y, mouseBtn1Clicked))
	if panel.session.Target != config.TargetScope {
		t.Fatalf("confirm click switched tab to %s", panel.session.Target)
	}
}

func TestProjectLauncherChangeRefreshesInherit(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	pumpPanel(panel, panel.openSection(sectionExecution))
	_, view := panel.view()
	plain := ansi.Strip(view)
	inherited := panel.session.FormatInherited(panel.session.Config.Launcher)
	if !strings.Contains(plain, inherited) {
		t.Fatalf("missing inherit prefix:\n%s", plain)
	}
	if panel.bind == nil {
		t.Fatal("no bind")
	}
	next := "foreground"
	if panel.bind.launcher == next {
		next = "herdr"
	}
	panel.bind.launcher = next
	panel.bind.apply(panel)
	if panel.rebuildFocus != launcherFocusKey() {
		t.Fatalf("rebuildFocus=%q", panel.rebuildFocus)
	}
	pumpPanel(panel, panel.rebuildSection())
	_, view = panel.view()
	plain = ansi.Strip(view)
	if strings.Contains(plain, inherited) {
		t.Fatalf("inherit prefix remained:\n%s", plain)
	}
	if !strings.Contains(plain, uiText("tui.restore_field_inherit")) {
		t.Fatalf("missing restore control:\n%s", plain)
	}
}

func TestModelOverrideDoesNotRebuildInput(t *testing.T) {
	_, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	if err := panel.session.SetTarget(config.TargetOverlay); err != nil {
		t.Fatal(err)
	}
	pumpPanel(panel, panel.openSection(sectionExecution))
	if panel.bind == nil || len(panel.bind.modelValues) == 0 || panel.bind.modelValues[0] == nil {
		t.Fatal("expected a model input")
	}
	*panel.bind.modelValues[0] = *panel.bind.modelValues[0] + "-x"
	panel.bind.apply(panel)
	if panel.rebuildFocus != "" {
		t.Fatalf("model keystroke rebuilt the form: %q", panel.rebuildFocus)
	}
}

func TestSwitchTabSyncsAppFromSession(t *testing.T) {
	app, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	inherited := panel.session.Config.TUI.Theme
	override := "light"
	if inherited == "light" {
		override = "dark"
	}
	pumpPanel(panel, panel.switchTab(config.TargetOverlay))
	app.Theme = override
	panel.session.SetTUIField("theme", override)
	pumpPanel(panel, panel.switchTab(config.TargetScope))
	if app.Theme != inherited {
		t.Fatalf("global tab kept overlay theme %s, want %s", app.Theme, inherited)
	}
}

func TestAppUpdateRoutesOptionsClick(t *testing.T) {
	app, panel := openPanel(t)
	attachTempOverlay(t, panel.session, config.ModeGlobal)
	pumpPanel(panel, panel.openRoot())
	panel.view()
	if panel.chromeLines < 1 {
		t.Fatal("expected scope chrome")
	}
	formLines := panel.currentBodyLines()
	lo, _, ok := focusRange(formLines)
	if !ok {
		t.Fatal("no focused row")
	}
	target := lo + 2
	if target >= len(formLines) {
		t.Skip("popup too short for this assertion")
	}
	x := panel.bodyX + 1
	y := panel.bodyY + panel.chromeLines + target
	_ = app.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	cmd := app.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	pumpPanel(panel, cmd)
	moved, _, ok := focusRange(panel.currentBodyLines())
	if !ok || moved != target {
		t.Fatalf("App.Update click should focus form row %d, focus is %d", target, moved)
	}
}
