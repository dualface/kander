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
	old := pathKanderBinaries
	pathKanderBinaries = func() []install.PathKanderBinary { return binaries }
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
	stubKanderInventory(t, []install.PathKanderBinary{{Path: "/usr/local/bin/kander", Version: "1.0.0"}})
	lines := CaptureReport(func() {
		if !reportPathKanders(true) {
			t.Fatal("single binary must report healthy")
		}
	})
	if len(lines) != 0 {
		t.Fatalf("single binary printed %v, want silence", lines)
	}
}

func TestReportPathKandersListsAndHintsNonInteractive(t *testing.T) {
	stubKanderInventory(t, []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
	})
	var healthy bool
	lines := CaptureReport(func() { healthy = reportPathKanders(false) })
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
	stubKanderInventory(t, []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	})
	var brewRan bool
	stubBrewUpgrade(t, &brewRan, nil)
	stubAskChoice(t, []string{"upgrade", ""})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true) })
	if !brewRan {
		t.Fatal("outdated brew install must offer `brew upgrade kander`")
	}
}

func TestReportPathKandersSkipsBrewUpgrade(t *testing.T) {
	stubKanderInventory(t, []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	})
	var brewRan bool
	stubBrewUpgrade(t, &brewRan, nil)
	stubAskChoice(t, []string{"skip", ""})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true) })
	if brewRan {
		t.Fatal("skip answer must not run brew upgrade")
	}
}

func TestReportPathKandersBrewFailureWarns(t *testing.T) {
	stubKanderInventory(t, []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "0.9.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
	})
	var brewRan bool
	stubBrewUpgrade(t, &brewRan, errors.New("brew missing"))
	stubAskChoice(t, []string{"upgrade", ""})
	var removed []string
	stubRemoval(t, &removed, nil)
	var healthy bool
	lines := CaptureReport(func() { healthy = reportPathKanders(true) })
	if healthy || !brewRan {
		t.Fatalf("failed brew upgrade: healthy=%v ran=%v", healthy, brewRan)
	}
	if !strings.Contains(reportText(lines), "brew upgrade failed") {
		t.Fatalf("missing failure warning:\n%s", reportText(lines))
	}
}

func TestReportPathKandersKeepOneRemovesOthers(t *testing.T) {
	stubKanderInventory(t, []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/home/u/bin/kander", Version: "0.9.0"},
		{Path: "/opt/homebrew/bin/kander", Version: "1.0.0", Brew: true},
	})
	var brewRan bool
	stubBrewUpgrade(t, &brewRan, nil)
	stubAskChoice(t, []string{"/home/u/bin/kander"})
	var removed []string
	stubRemoval(t, &removed, nil)
	var lines []ReportLine
	var healthy bool
	lines = CaptureReport(func() { healthy = reportPathKanders(true) })
	if healthy {
		t.Fatal("cleanup flow must keep healthy=false for this report cycle")
	}
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
}

func TestReportPathKandersKeepBrewRemovesNonBrew(t *testing.T) {
	stubKanderInventory(t, []install.PathKanderBinary{
		{Path: "/opt/homebrew/bin/kander", Version: "1.0.0", Brew: true},
		{Path: "/usr/local/bin/kander", Version: "0.9.0"},
	})
	stubAskChoice(t, []string{"/opt/homebrew/bin/kander"})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true) })
	want := []string{"/usr/local/bin/kander"}
	if !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed %v, want %v", removed, want)
	}
}

func TestReportPathKandersSkipRemovesNothing(t *testing.T) {
	stubKanderInventory(t, []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/home/u/bin/kander", Version: "0.9.0"},
	})
	stubAskChoice(t, []string{""})
	var removed []string
	stubRemoval(t, &removed, nil)
	CaptureReport(func() { reportPathKanders(true) })
	if len(removed) != 0 {
		t.Fatalf("skip answer removed %v", removed)
	}
}
