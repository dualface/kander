package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/terminal"
)

// A herdr-core card is observed like a herdr or luvus card: its terminal
// reports the agent identity and status.
func TestAgentObservableHerdrCoreCard(t *testing.T) {
	for _, state := range []string{"working", "review"} {
		if !agentObservable(Task{State: state, Window: "herdr-core:t8"}) {
			t.Fatalf("%s herdr-core card is not observed", state)
		}
	}
	if agentObservable(Task{State: "done", Window: "herdr-core:t8"}) {
		t.Fatal("done card observed")
	}
}

func TestOptionsLauncherOffersHerdrCore(t *testing.T) {
	savedDirs := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir { return nil }
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = savedDirs
		terminal.ReloadDefinitions()
	})
	bin := t.TempDir()
	name := "hc"
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	t.Setenv("PATH", bin)
	offered := func(panel *optionsPanel) (string, bool) {
		for _, choice := range panel.session.LauncherChoices() {
			if choice.Value == "herdr-core" {
				return choice.Label, true
			}
		}
		return "", false
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	// Another launcher is configured, so only detection can offer herdr-core:
	// a configured launcher stays listed whether it is installed or not.
	cfg.Launcher = "foreground"
	_, panel := openPanel(t, cfg)
	if label, ok := offered(panel); ok {
		t.Fatalf("herdr-core offered without hc: %q", label)
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if label, ok := offered(panel); !ok || label != uiText("herdr_core.menu_choice") {
		t.Fatalf("installed hc is not offered: %q %t", label, ok)
	}

	cfg.Launcher = "herdr-core"
	_, panel = openPanel(t, cfg)
	pumpPanel(panel, panel.openSection(sectionExecution))
	if panel.bind == nil || panel.bind.launcher != "herdr-core" {
		t.Fatalf("launcher field: %+v", panel.bind)
	}
	_, view := panel.view()
	if plain := ansi.Strip(view); !strings.Contains(plain, uiText("herdr_core.menu_choice")) {
		t.Fatalf("launcher field does not show herdr-core:\n%s", plain)
	}
}
