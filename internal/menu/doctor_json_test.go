package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/command/commandtest"
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
	confirmationsOf = func(TerminalTools) []string { return nil }
	t.Cleanup(func() { confirmationsOf = old })
	code, stdout, stderr := runDoctorJSON(t)
	if (code != 0 && code != 1) || stderr != "" || !strings.Contains(stdout, `"lines"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
