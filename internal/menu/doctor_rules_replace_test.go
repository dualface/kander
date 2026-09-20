//go:build unix

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

// stubModifiedRuleConfirm swaps the interactive decision seam for a fixed answer.
func stubModifiedRuleConfirm(t *testing.T, answer func(modified []string) bool) {
	t.Helper()
	old := confirmModifiedRules
	confirmModifiedRules = answer
	t.Cleanup(func() { confirmModifiedRules = old })
}

// modifiedRulesFixture installs the embedded rules into the temp home through one doctor
// repair pass, then rewrites one file so the next inspection reports it modified.
func modifiedRulesFixture(t *testing.T, h *harness) (paths config.InstallPaths, file string) {
	t.Helper()
	h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground", "language": "en"}))
	CaptureReport(func() {
		printDoctorWithTools(TerminalTools{}, true, false)
	})
	paths, err := config.CurrentInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(paths.RulesDir, "KANDER-CODE-RULES.md")
	if err := os.WriteFile(file, []byte("edited locally\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return paths, file
}

func ruleBackups(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var backups []string
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".backup-") {
			backups = append(backups, entry.Name())
		}
	}
	return backups
}

func TestDoctorModifiedRulesConfirmBacksUpAndReplaces(t *testing.T) {
	h := newHarness(t)
	paths, file := modifiedRulesFixture(t, h)
	asked := false
	stubModifiedRuleConfirm(t, func(modified []string) bool {
		asked = true
		if len(modified) != 1 || modified[0] != "KANDER-CODE-RULES.md" {
			t.Fatalf("modified=%v", modified)
		}
		return true
	})
	lines := CaptureReport(func() {
		printDoctorWithTools(TerminalTools{}, true, true)
	})
	if !asked {
		t.Fatal("interactive doctor did not ask about the modified rules")
	}
	text := reportText(lines)
	backups := ruleBackups(t, paths.RulesDir)
	if len(backups) != 1 {
		t.Fatalf("backups=%v", backups)
	}
	data, err := os.ReadFile(filepath.Join(paths.RulesDir, backups[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "edited locally\n" {
		t.Fatalf("backup content %q", data)
	}
	want, err := rules.File("KANDER-CODE-RULES.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("modified rule file was not replaced with the embedded copy")
	}
	if !strings.Contains(text, backups[0]) {
		t.Fatalf("report does not name the backup:\n%s", text)
	}
}

func TestDoctorModifiedRulesDeclineKeepsFiles(t *testing.T) {
	h := newHarness(t)
	paths, file := modifiedRulesFixture(t, h)
	stubModifiedRuleConfirm(t, func(modified []string) bool { return false })
	lines := CaptureReport(func() {
		printDoctorWithTools(TerminalTools{}, true, true)
	})
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "edited locally\n" {
		t.Fatalf("declined replace still rewrote the file: %q", data)
	}
	if backups := ruleBackups(t, paths.RulesDir); len(backups) != 0 {
		t.Fatalf("declined replace created backups: %v", backups)
	}
	if !strings.Contains(reportText(lines), config.Text("install.rule_modified", "KANDER-CODE-RULES.md")) {
		t.Fatalf("modified hint missing:\n%s", reportText(lines))
	}
}

func TestDoctorModifiedRulesNonInteractiveNeverAsks(t *testing.T) {
	h := newHarness(t)
	paths, file := modifiedRulesFixture(t, h)
	stubModifiedRuleConfirm(t, func(modified []string) bool {
		t.Fatal("non-interactive doctor asked about the modified rules")
		return true
	})
	lines := CaptureReport(func() {
		printDoctorWithTools(TerminalTools{}, true, false)
	})
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "edited locally\n" {
		t.Fatalf("non-interactive doctor rewrote the file: %q", data)
	}
	if backups := ruleBackups(t, paths.RulesDir); len(backups) != 0 {
		t.Fatalf("non-interactive doctor created backups: %v", backups)
	}
	if !strings.Contains(reportText(lines), config.Text("install.rule_modified", "KANDER-CODE-RULES.md")) {
		t.Fatalf("modified hint missing:\n%s", reportText(lines))
	}
}

func TestDoctorModifiedRulesReplaceFailureIsReported(t *testing.T) {
	h := newHarness(t)
	paths, file := modifiedRulesFixture(t, h)
	// Replace the rule file with a symlink: global-mode linked rules stay user-managed, so
	// the confirmed replace reports a per-file failure and leaves the file in place.
	linkTarget := filepath.Join(t.TempDir(), "managed-rules.md")
	if err := os.WriteFile(linkTarget, []byte("user managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkTarget, file); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	stubModifiedRuleConfirm(t, func(modified []string) bool { return true })
	var healthy bool
	lines := CaptureReport(func() {
		healthy = printDoctorWithTools(TerminalTools{}, true, true)
	})
	if healthy {
		t.Fatal("a failed replace must report unhealthy")
	}
	if !strings.Contains(reportText(lines), config.Text("install.rule_replace_failed", "KANDER-CODE-RULES.md", "")) {
		t.Fatalf("failure warning missing:\n%s", reportText(lines))
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "user managed\n" {
		t.Fatalf("link target changed: %q", data)
	}
	_ = paths
}

// feedStdin connects os.Stdin to a pipe carrying input so the real askChoice line path runs;
// an empty input exercises the EOF branch.
func feedStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		_ = r.Close()
	})
}

func TestAskModifiedRulesReplaceAnswers(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"confirm by number", "1\n", true},
		{"keep by number", "2\n", false},
		{"empty line keeps", "\n", false},
		{"input ended keeps", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			feedStdin(t, tc.input)
			// askChoice writes to stderr directly; only the decision matters here.
			if got := askModifiedRulesReplace([]string{"KANDER-AGENTS.md"}); got != tc.want {
				t.Fatalf("input %q: got %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
