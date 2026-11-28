package menu

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/version"
)

func stubKanderInventory(t *testing.T, binaries []install.PathKanderBinary) {
	t.Helper()
	stubKanderInventoryPtr(t, &binaries)
}

// stubKanderInventoryPtr makes the inventory re-read on every call, so a test
// can model the post-upgrade or post-removal PATH state.
func stubKanderInventoryPtr(t *testing.T, binaries *[]install.PathKanderBinary) {
	t.Helper()
	old := pathKanderBinaries
	pathKanderBinaries = func() []install.PathKanderBinary { return *binaries }
	t.Cleanup(func() { pathKanderBinaries = old })
}

func stubAskChoice(t *testing.T, answers []string) {
	t.Helper()
	old := askKanderChoice
	remaining := append([]string{}, answers...)
	askKanderChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		if len(remaining) == 0 {
			return defaultValue, nil
		}
		next := remaining[0]
		remaining = remaining[1:]
		return next, nil
	}
	t.Cleanup(func() { askKanderChoice = old })
}

func stubRemoval(t *testing.T, removed *[]string, fail map[string]error) {
	t.Helper()
	old := removeKanderBinary
	removeKanderBinary = func(path string) error {
		if err := fail[path]; err != nil {
			return err
		}
		*removed = append(*removed, path)
		return nil
	}
	t.Cleanup(func() { removeKanderBinary = old })
}

func stubBrewUpgrade(t *testing.T, upgraded *[]string, err error) {
	t.Helper()
	old := runBrewUpgrade
	runBrewUpgrade = func(formula string) error {
		*upgraded = append(*upgraded, formula)
		return err
	}
	t.Cleanup(func() { runBrewUpgrade = old })
}

func reportText(lines []ReportLine) string {
	var parts []string
	for _, line := range lines {
		parts = append(parts, line.Text)
	}
	return strings.Join(parts, "\n")
}

func TestReportPathKandersSingleIsQuiet(t *testing.T) {
	binaries := []install.PathKanderBinary{{Path: "/usr/local/bin/kander", Version: "1.0.0"}}
	stubKanderInventory(t, binaries)
	lines := CaptureReport(func() {
		if !reportPathKanders(true, binaries) {
			t.Fatal("single binary must report healthy")
		}
	})
	if len(lines) != 0 {
		t.Fatalf("single binary printed %v, want silence", lines)
	}
}

func TestReportPathKandersListsAndHintsNonInteractive(t *testing.T) {
	binaries := []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
	}
	stubKanderInventory(t, binaries)
	var healthy bool
	lines := CaptureReport(func() { healthy = reportPathKanders(false, binaries) })
	if healthy {
		t.Fatal("duplicates must report unhealthy")
	}
	text := reportText(lines)
	for _, want := range []string{"/usr/local/bin/kander", "/opt/homebrew/bin/kander", "kander doctor"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q:\n%s", want, text)
		}
	}
}

func TestReportPathKandersOffersBrewUpgrade(t *testing.T) {
	t.Run("kander formula", func(t *testing.T) {
		binaries := []install.PathKanderBinary{
			{Path: "/opt/homebrew/bin/kander", Resolved: "/opt/homebrew/Cellar/kander/0.9.0/bin/kander", Version: "0.9.0", Brew: true},
			{Path: "/usr/local/bin/kander", Resolved: "/usr/local/bin/kander", Version: "1.0.0"},
		}
		stubKanderInventory(t, binaries)
		var upgraded []string
		stubBrewUpgrade(t, &upgraded, nil)
		stubAskChoice(t, []string{"upgrade", ""})
		var removed []string
		stubRemoval(t, &removed, nil)
		lines := CaptureReport(func() { reportPathKanders(true, binaries) })
		if !reflect.DeepEqual(upgraded, []string{install.BrewFormulaStable}) {
			t.Fatalf("upgraded=%v, want [kander]", upgraded)
		}
		if !strings.Contains(reportText(lines), "brew upgrade kander") {
			t.Fatalf("missing brew upgrade kander hint:\n%s", reportText(lines))
		}
	})

	t.Run("kander-beta formula", func(t *testing.T) {
		binaries := []install.PathKanderBinary{
			{Path: "/opt/homebrew/bin/kander", Resolved: "/opt/homebrew/Cellar/kander-beta/2.0.0-beta.4/bin/kander", Version: "2.0.0-beta.4", Brew: true},
			{Path: "/usr/local/bin/kander", Resolved: "/usr/local/bin/kander", Version: "2.0.0-beta.5"},
		}
		stubKanderInventory(t, binaries)
		var upgraded []string
		stubBrewUpgrade(t, &upgraded, nil)
		stubAskChoice(t, []string{"upgrade", "/usr/local/bin/kander"})
		var removed []string
		stubRemoval(t, &removed, nil)
		lines := CaptureReport(func() { reportPathKanders(true, binaries) })
		if !reflect.DeepEqual(upgraded, []string{install.BrewFormulaBeta}) {
			t.Fatalf("upgraded=%v, want [kander-beta]", upgraded)
		}
		text := reportText(lines)
		for _, want := range []string{"brew upgrade kander-beta", "brew uninstall kander-beta"} {
			if !strings.Contains(text, want) {
				t.Fatalf("report lacks %q:\n%s", want, text)
			}
		}
	})

	t.Run("brew 1.x kander coexisting with running direct v2 prerelease still uses kander", func(t *testing.T) {
		oldVersion := version.Version
		version.Version = "2.0.0-beta.5"
		t.Cleanup(func() { version.Version = oldVersion })
		binaries := []install.PathKanderBinary{
			{Path: "/opt/homebrew/bin/kander", Resolved: "/opt/homebrew/Cellar/kander/1.6.3/bin/kander", Version: "1.6.3", Brew: true},
			{Path: "/usr/local/bin/kander", Resolved: "/usr/local/bin/kander", Version: "2.0.0-beta.5"},
		}
		stubKanderInventory(t, binaries)
		var upgraded []string
		stubBrewUpgrade(t, &upgraded, nil)
		stubAskChoice(t, []string{"upgrade", "/usr/local/bin/kander"})
		var removed []string
		stubRemoval(t, &removed, nil)
		lines := CaptureReport(func() { reportPathKanders(true, binaries) })
		if !reflect.DeepEqual(upgraded, []string{install.BrewFormulaStable}) {
			t.Fatalf("upgraded=%v, want [kander]", upgraded)
		}
		text := reportText(lines)
		if strings.Contains(text, "kander-beta") {
			t.Fatalf("unexpected kander-beta mention for Cellar/kander:\n%s", text)
		}
		for _, want := range []string{"brew upgrade kander", "brew uninstall kander"} {
			if !strings.Contains(text, want) {
				t.Fatalf("report lacks %q:\n%s", want, text)
			}
		}
	})
}

func TestReportPathKandersSkipsBrewUpgrade(t *testing.T) {
	binaries := []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Resolved: "/opt/homebrew/Cellar/kander/0.9.0/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	}
	stubKanderInventory(t, binaries)
	var upgraded []string
	stubBrewUpgrade(t, &upgraded, nil)
	stubAskChoice(t, []string{"skip", ""})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true, binaries) })
	if len(upgraded) != 0 {
		t.Fatalf("skip answer must not run brew upgrade: %v", upgraded)
	}
}

func TestReportPathKandersBrewFailureWarns(t *testing.T) {
	binaries := []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Resolved: "/opt/homebrew/Cellar/kander/0.9.0/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	}
	stubKanderInventory(t, binaries)
	var upgraded []string
	stubBrewUpgrade(t, &upgraded, errors.New("brew missing"))
	stubAskChoice(t, []string{"upgrade", ""})
	var removed []string
	stubRemoval(t, &removed, nil)
	var healthy bool
	lines := CaptureReport(func() { healthy = reportPathKanders(true, binaries) })
	if healthy || len(upgraded) != 1 {
		t.Fatalf("failed brew upgrade: healthy=%v upgraded=%v", healthy, upgraded)
	}
	if !strings.Contains(reportText(lines), "brew upgrade failed") {
		t.Fatalf("missing failure warning:\n%s", reportText(lines))
	}
}

func TestReportPathKandersKeepOneRemovesOthers(t *testing.T) {
	inventory := []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/home/u/bin/kander", Version: "0.9.0"},
		{Path: "/opt/homebrew/bin/kander", Resolved: "/opt/homebrew/Cellar/kander/1.0.0/bin/kander", Version: "1.0.0", Brew: true},
	}
	stubKanderInventoryPtr(t, &inventory)
	var upgraded []string
	stubBrewUpgrade(t, &upgraded, nil)
	stubAskChoice(t, []string{"/home/u/bin/kander"})
	var removed []string
	stubRemoval(t, &removed, nil)
	var lines []ReportLine
	var healthy bool
	lines = CaptureReport(func() { healthy = reportPathKanders(true, inventory) })
	want := []string{"/usr/local/bin/kander"}
	if !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed %v, want %v", removed, want)
	}
	if len(upgraded) != 0 {
		t.Fatalf("brew at latest must not trigger upgrade: %v", upgraded)
	}
	if !strings.Contains(reportText(lines), "brew uninstall kander") {
		t.Fatalf("kept-nonbrew choice must hint brew uninstall kander:\n%s", reportText(lines))
	}
	if healthy {
		t.Fatal("stale inventory still holding duplicates must report unhealthy")
	}
}

// A successful keep-one that leaves a single file flips the report healthy.
func TestReportPathKandersHealthyAfterCleanup(t *testing.T) {
	inventory := []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/home/u/bin/kander", Version: "0.9.0"},
	}
	stubKanderInventoryPtr(t, &inventory)
	stubAskChoice(t, []string{"/home/u/bin/kander"})
	var removed []string
	old := removeKanderBinary
	removeKanderBinary = func(path string) error {
		removed = append(removed, path)
		inventory = []install.PathKanderBinary{{Path: "/home/u/bin/kander", Version: "0.9.0"}}
		return nil
	}
	t.Cleanup(func() { removeKanderBinary = old })
	var healthy bool
	CaptureReport(func() { healthy = reportPathKanders(true, inventory) })
	if !healthy {
		t.Fatal("cleanup leaving one binary must report healthy")
	}
	if !reflect.DeepEqual(removed, []string{"/usr/local/bin/kander"}) {
		t.Fatalf("removed %v", removed)
	}
}

// A successful brew upgrade re-probes before the keep-one question.
func TestReportPathKandersReprobesAfterBrewUpgrade(t *testing.T) {
	before := []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	}
	after := []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "1.0.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	}
	inventory := before
	stubKanderInventoryPtr(t, &inventory)
	var brewRan bool
	old := runBrewUpgrade
	runBrewUpgrade = func(string) error {
		brewRan = true
		inventory = after
		return nil
	}
	t.Cleanup(func() { runBrewUpgrade = old })
	var seenChoices [][]choice
	oldAsk := askKanderChoice
	askKanderChoice = func(prompt string, choices []choice, defaultValue string) (string, error) {
		seenChoices = append(seenChoices, choices)
		if len(choices) > 2 {
			return choices[0].Value, nil
		}
		return "upgrade", nil
	}
	t.Cleanup(func() { askKanderChoice = oldAsk })
	var removed []string
	oldRemove := removeKanderBinary
	removeKanderBinary = func(path string) error {
		removed = append(removed, path)
		inventory = after[:1]
		return nil
	}
	t.Cleanup(func() { removeKanderBinary = oldRemove })
	var healthy bool
	lines := CaptureReport(func() { healthy = reportPathKanders(true, before) })
	if !brewRan || len(seenChoices) != 2 {
		t.Fatalf("brew=%v prompts=%d", brewRan, len(seenChoices))
	}
	// The keep-one question must offer the post-upgrade inventory.
	var keepChoices []choice
	for _, c := range seenChoices {
		if len(c) > 2 {
			keepChoices = c
		}
	}
	if len(keepChoices) != 3 || keepChoices[0].Value != "/opt/homebrew/bin/kander" {
		t.Fatalf("keep-one choices = %+v", keepChoices)
	}
	_ = lines
	if !healthy {
		t.Fatal("keeping brew after upgrade leaves one binary -> healthy")
	}
	if !reflect.DeepEqual(removed, []string{"/usr/local/bin/kander"}) {
		t.Fatalf("removed %v", removed)
	}
}

func TestReportPathKandersKeepBrewRemovesNonBrew(t *testing.T) {
	binaries := []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "1.0.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "0.9.0"},
	}
	stubKanderInventory(t, binaries)
	stubAskChoice(t, []string{"/opt/homebrew/bin/kander"})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true, binaries) })
	want := []string{"/usr/local/bin/kander"}
	if !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed %v, want %v", removed, want)
	}
}

func TestReportPathKandersSkipRemovesNothing(t *testing.T) {
	binaries := []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/home/u/bin/kander", Version: "0.9.0"},
	}
	stubKanderInventory(t, binaries)
	stubAskChoice(t, []string{""})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true, binaries) })
	if len(removed) != 0 {
		t.Fatalf("skip answer removed %v", removed)
	}
}
