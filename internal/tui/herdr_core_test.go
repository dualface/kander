package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/dualface/kander/internal/config"
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
	if runtime.GOOS == "windows" {
		t.Skip("herdr-core is POSIX only")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "hc"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Launcher = "herdr-core"
	_, panel := openPanel(t, cfg)
	pumpPanel(panel, panel.openSection(sectionExecution))
	if panel.bind == nil || panel.bind.launcher != "herdr-core" {
		t.Fatalf("launcher field: %+v", panel.bind)
	}
	values := map[string]bool{}
	for _, choice := range panel.session.LauncherChoices() {
		values[choice.Value] = true
	}
	if !values["herdr-core"] {
		t.Fatalf("launcher choices lack herdr-core: %v", values)
	}
	_, view := panel.view()
	if plain := ansi.Strip(view); !strings.Contains(plain, uiText("herdr_core.menu_choice")) {
		t.Fatalf("launcher field does not show herdr-core:\n%s", plain)
	}
}
