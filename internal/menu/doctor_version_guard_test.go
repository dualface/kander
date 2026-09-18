package menu

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
	"github.com/dualface/kander/internal/version"
)

func stubRunningVersion(t *testing.T, value string) {
	t.Helper()
	old := version.Version
	version.Version = value
	t.Cleanup(func() { version.Version = old })
}

func TestReportOutdatedRunningKanderWarnsOnNewerPathBinary(t *testing.T) {
	stubRunningVersion(t, "1.0.0")
	binaries := []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/opt/kander/bin/kander", Version: "1.2.0"},
	}
	var healthy bool
	lines := CaptureReport(func() { healthy = reportOutdatedRunningKander(binaries) })
	if healthy {
		t.Fatal("a strictly newer PATH binary must report unhealthy")
	}
	text := reportText(lines)
	for _, want := range []string{"1.0.0", "/opt/kander/bin/kander", "1.2.0"} {
		if !strings.Contains(text, want) {
			t.Fatalf("warning missing %q:\n%s", want, text)
		}
	}
}

func TestReportOutdatedRunningKanderSkips(t *testing.T) {
	cases := []struct {
		name     string
		running  string
		binaries []install.PathKanderBinary
	}{
		{"unparseable running version", "dev", []install.PathKanderBinary{{Path: "/opt/kander", Version: "9.9.9"}}},
		{"empty running version", "", []install.PathKanderBinary{{Path: "/opt/kander", Version: "9.9.9"}}},
		{"no binaries", "1.0.0", nil},
		{"equal version", "1.0.0", []install.PathKanderBinary{{Path: "/opt/kander", Version: "1.0.0"}}},
		{"older path binary", "1.0.0", []install.PathKanderBinary{{Path: "/opt/kander", Version: "0.9.0"}}},
		{"unprobed path binary", "1.0.0", []install.PathKanderBinary{{Path: "/opt/kander", Version: ""}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubRunningVersion(t, tc.running)
			var healthy bool
			lines := CaptureReport(func() { healthy = reportOutdatedRunningKander(tc.binaries) })
			if !healthy {
				t.Fatalf("case %q must stay healthy:\n%s", tc.name, reportText(lines))
			}
			if len(lines) != 0 {
				t.Fatalf("case %q printed %v, want silence", tc.name, lines)
			}
		})
	}
}

// A newer PATH binary stops doctor before the environment check header, so no
// check, prompt, or repair can run.
func TestPrintDoctorStopsBeforeChecksWhenPathKanderIsNewer(t *testing.T) {
	stubRunningVersion(t, "1.0.0")
	binaries := []install.PathKanderBinary{{Path: "/opt/kander/bin/kander", Version: "2.0.0"}}
	stubKanderInventory(t, binaries)
	var healthy bool
	lines := CaptureReport(func() {
		healthy = printDoctorWithTools(TerminalTools{}, true, false)
	})
	if healthy {
		t.Fatal("doctor must report unhealthy")
	}
	text := reportText(lines)
	if strings.Contains(text, config.Text("menu.kander_environment_check")) {
		t.Fatalf("doctor continued past the version guard:\n%s", text)
	}
	if !strings.Contains(text, "/opt/kander/bin/kander") || !strings.Contains(text, "2.0.0") {
		t.Fatalf("missing guard warning:\n%s", text)
	}
}

// The TUI report path aborts the same way.
func TestDoctorReportStopsWhenPathKanderIsNewer(t *testing.T) {
	stubRunningVersion(t, "1.0.0")
	stubKanderInventory(t, []install.PathKanderBinary{{Path: "/opt/kander/bin/kander", Version: "2.0.0"}})
	lines, healthy := DoctorReport(TerminalTools{})
	if healthy {
		t.Fatal("DoctorReport must report unhealthy")
	}
	text := reportText(lines)
	if strings.Contains(text, config.Text("menu.kander_environment_check")) {
		t.Fatalf("doctor continued past the version guard:\n%s", text)
	}
	if !strings.Contains(text, "/opt/kander/bin/kander") {
		t.Fatalf("missing guard warning:\n%s", text)
	}
}

// reportPathKanders consumes the listing the version guard already probed, so
// the PATH binaries are not probed again.
func TestReportPathKandersDoesNotReprobe(t *testing.T) {
	calls := 0
	old := pathKanderBinaries
	pathKanderBinaries = func() []install.PathKanderBinary {
		calls++
		return nil
	}
	t.Cleanup(func() { pathKanderBinaries = old })
	binaries := []install.PathKanderBinary{
		{Path: "/usr/local/bin/kander", Version: "1.0.0"},
		{Path: "/opt/kander/bin/kander", Version: "0.9.0"},
	}
	CaptureReport(func() { reportPathKanders(false, binaries) })
	if calls != 0 {
		t.Fatalf("reportPathKanders re-probed PATH %d times", calls)
	}
}
