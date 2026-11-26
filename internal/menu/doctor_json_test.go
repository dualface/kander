//go:build unix

package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
)

func runDoctorJSON(t *testing.T) (int, string, string) {
	t.Helper()
	return commandtest.Capture(func() int { return commandtest.Run(NewDoctorCommand(), "--json") })
}

// A doctor run that would ask whether to back up and replace a modified rule file is refused
// with --json before it repairs anything: the modified file is kept, and a missing rule file
// that a repair would write is still missing.
func TestDoctorJSONRefusesBeforeAnyRepairWhenARuleFileIsModified(t *testing.T) {
	h := newHarness(t)
	paths, file := modifiedRulesFixture(t, h)
	missing := filepath.Join(paths.RulesDir, "KANDER-GIT-RULES.md")
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	stubModifiedRuleConfirm(t, func([]string) bool {
		t.Fatal("doctor --json asked about the modified rules")
		return true
	})
	code, stdout, stderr := runDoctorJSON(t)
	if code != 2 || stdout != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var object struct {
		Error command.Notice `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &object); err != nil || object.Error.Code != command.CodeInteractive {
		t.Fatalf("stderr=%q %v", stderr, err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "edited locally\n" {
		t.Fatalf("the modified rule file changed: %q %v", data, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("a repair ran before the refusal: %v", err)
	}
	if backups := ruleBackups(t, paths.RulesDir); len(backups) != 0 {
		t.Fatalf("backups were made: %v", backups)
	}
}

// With nothing to ask, doctor --json still repairs and reports as before.
func TestDoctorJSONRunsWhenNothingNeedsAConfirmation(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground", "language": "en"}))
	old := confirmationsOf
	confirmationsOf = func(TerminalTools, []install.PathKanderBinary) []string { return nil }
	t.Cleanup(func() { confirmationsOf = old })
	code, stdout, stderr := runDoctorJSON(t)
	if (code != 0 && code != 1) || stderr != "" || !strings.Contains(stdout, `"lines"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// Two kander executables of one version on PATH make an interactive doctor ask which to keep, so
// doctor --json refuses before it repairs, as for a modified rule file.
func TestDoctorJSONRefusesBeforeAnyRepairWhenPathHoldsTwoCopies(t *testing.T) {
	h := newHarness(t)
	paths, _ := modifiedRulesFixture(t, h)
	missing := filepath.Join(paths.RulesDir, "KANDER-GIT-RULES.md")
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	stubRunningVersion(t, "1.0.0")
	binaries := []install.PathKanderBinary{{Path: "/usr/local/bin/kander", Version: "1.0.0"}, {Path: "/opt/kander/bin/kander", Version: "1.0.0"}}
	stubKanderInventory(t, binaries)
	// The machine's agents and tools are left out: only the PATH question decides here.
	old := confirmationsOf
	confirmationsOf = func(_ TerminalTools, found []install.PathKanderBinary) []string {
		if pathCleanupOffered(found) {
			return []string{"path"}
		}
		return nil
	}
	t.Cleanup(func() { confirmationsOf = old })
	code, stdout, stderr := runDoctorJSON(t)
	if code != 2 || stdout != "" || !strings.Contains(stderr, command.CodeInteractive) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("a repair ran before the refusal: %v", err)
	}
	confirmationsOf = old
	reasons := doctorConfirmations(TerminalTools{}, binaries)
	want := config.Text("menu.doctor_confirm_path_cleanup", 2)
	found := false
	for _, reason := range reasons {
		found = found || reason == want
	}
	if !found {
		t.Fatalf("the precheck misses the PATH cleanup question: %v", reasons)
	}
}

// A running binary older than one on PATH gets the version diagnostic, not a refusal, even when
// a modified rule file would also ask a question.
func TestDoctorJSONReportsTheVersionGuardBeforeAskingAnything(t *testing.T) {
	h := newHarness(t)
	_, file := modifiedRulesFixture(t, h)
	stubRunningVersion(t, "1.0.0")
	stubKanderInventory(t, []install.PathKanderBinary{{Path: "/opt/kander/bin/kander", Version: "2.0.0"}})
	code, stdout, stderr := runDoctorJSON(t)
	if code != 1 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var result struct {
		Healthy bool `json:"healthy"`
		Lines   []struct {
			Message string `json:"message"`
		} `json:"lines"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || result.Healthy {
		t.Fatalf("stdout=%q %v", stdout, err)
	}
	text := ""
	for _, line := range result.Lines {
		text += line.Message + "\n"
	}
	if !strings.Contains(text, "/opt/kander/bin/kander") || !strings.Contains(text, "2.0.0") {
		t.Fatalf("the version diagnostic is missing: %s", text)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "edited locally\n" {
		t.Fatalf("the modified file changed: %q %v", data, err)
	}
}

// The questions about the PATH executables are decided by one set of conditions: for every
// inventory, the interactive report asks exactly when the doctor --json precheck says it would.
func TestDoctorPathQuestionsMatchTheJSONPrecheck(t *testing.T) {
	inventories := map[string][]install.PathKanderBinary{
		"one":                  {{Path: "/a/kander", Version: "1.0.0"}},
		"two plain":            {{Path: "/a/kander", Version: "1.0.0"}, {Path: "/b/kander", Version: "1.0.0"}},
		"brew only":            {{Path: "/a/kander", Version: "1.0.0", Brew: true}, {Path: "/b/kander", Version: "1.0.0", Brew: true}},
		"outdated brew":        {{Path: "/a/kander", Version: "1.0.0", Brew: true}, {Path: "/b/kander", Version: "2.0.0"}},
		"plain and brew equal": {{Path: "/a/kander", Version: "1.0.0", Brew: true}, {Path: "/b/kander", Version: "1.0.0"}},
	}
	for name, binaries := range inventories {
		asked := false
		old := askKanderChoice
		askKanderChoice = func(string, []choice, string) (string, error) {
			asked = true
			return "", nil
		}
		CaptureReport(func() { reportPathKanders(true, binaries) })
		askKanderChoice = old
		if want := brewKanderOutdated(binaries) || pathCleanupOffered(binaries); asked != want {
			t.Fatalf("%s: the interactive report asked=%v, the precheck says %v", name, asked, want)
		}
	}
}
