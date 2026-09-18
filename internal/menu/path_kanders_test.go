package menu

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/install"
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

func stubBrewUpgrade(t *testing.T, ran *bool, err error) {
	t.Helper()
	old := runBrewUpgrade
	runBrewUpgrade = func() error {
		*ran = true
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
	binaries := []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	}
	stubKanderInventory(t, binaries)
	var brewRan bool
	stubBrewUpgrade(t, &brewRan, nil)
	stubAskChoice(t, []string{"upgrade", ""})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true, binaries) })
	if !brewRan {
		t.Fatal("outdated brew install must offer `brew upgrade kander`")
	}
}

func TestReportPathKandersSkipsBrewUpgrade(t *testing.T) {
	binaries := []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	}
	stubKanderInventory(t, binaries)
	var brewRan bool
	stubBrewUpgrade(t, &brewRan, nil)
	stubAskChoice(t, []string{"skip", ""})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true, binaries) })
	if brewRan {
		t.Fatal("skip answer must not run brew upgrade")
	}
}

func TestReportPathKandersBrewFailureWarns(t *testing.T) {
	binaries := []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	}
	stubKanderInventory(t, binaries)
	var brewRan bool
	stubBrewUpgrade(t, &brewRan, errors.New("brew missing"))
	stubAskChoice(t, []string{"upgrade", ""})
	var removed []string
	stubRemoval(t, &removed, nil)
	var healthy bool
	lines := CaptureReport(func() { healthy = reportPathKanders(true, binaries) })
	if healthy || !brewRan {
		t.Fatalf("failed brew upgrade: healthy=%v ran=%v", healthy, brewRan)
	}
	if !strings.Contains(reportText(lines), "brew upgrade failed") {
		t.Fatalf("missing failure warning:\n%s", reportText(lines))
	}
}

func TestReportPathKandersKeepOneRemovesOthers(t *testing.T) {
	inventory := []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/home/u/bin/kander", Version: "0.9.0"},
		{Path: "/opt/homebrew/bin/kander", Version: "1.0.0", Brew: true},
	}
	stubKanderInventoryPtr(t, &inventory)
	var brewRan bool
	stubBrewUpgrade(t, &brewRan, nil)
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
	if brewRan {
		t.Fatal("brew at latest must not trigger upgrade")
	}
	if !strings.Contains(reportText(lines), "brew") {
		t.Fatalf("kept-nonbrew choice must hint brew-managed files stay:\n%s", reportText(lines))
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
	runBrewUpgrade = func() error {
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
