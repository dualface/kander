package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// setupOverrideDoctorHome prepares a global scope where codex is covered and returns the
// AGENTS.md target together with its AGENTS.override.md sibling path.
func setupOverrideDoctorHome(t *testing.T) (config.InstallPaths, *config.Config, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents", "kander")}
	entry := filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
	writeRulesFile(t, entry, "# Kander entry\n")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	target := filepath.Join(home, ".codex", "AGENTS.md")
	override := filepath.Join(home, ".codex", "AGENTS.override.md")
	block := "## Kander Rules Entry\n\nAt the start of every session, read `" + entry +
		"` and follow it as the Kander workflow rules entry.\n"
	writeRulesFile(t, target, "# Personal rules\n\n"+block)
	return paths, cfg, target, override
}

func TestReportRulesIntegrationDetectsMissingOverrideReference(t *testing.T) {
	paths, cfg, _, override := setupOverrideDoctorHome(t)
	writeRulesFile(t, override, "# Override rules\n")

	lines := CaptureReport(func() {
		if reportRulesIntegration(cfg, paths, false) {
			t.Error("an override without the reference must make doctor unhealthy")
		}
	})
	var found bool
	for _, line := range lines {
		if strings.Contains(line.Text, override) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the report must name the override file: %+v", lines)
	}

	lines = CaptureReport(func() {
		if !reportRulesIntegration(cfg, paths, true) {
			t.Error("repair must succeed")
		}
	})
	found = false
	for _, line := range lines {
		if strings.Contains(line.Text, override) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the repair report must list the override result: %+v", lines)
	}
	data, err := os.ReadFile(override)
	if err != nil || !strings.Contains(string(data), "KANDER-AGENTS.md") {
		t.Fatalf("override lacks the reference: %v %q", err, data)
	}
	if !reportRulesIntegration(cfg, paths, false) {
		t.Fatal("report after repair must be healthy")
	}
}

func TestReportRulesIntegrationOverrideNeverCreated(t *testing.T) {
	paths, cfg, _, override := setupOverrideDoctorHome(t)
	if !reportRulesIntegration(cfg, paths, true) {
		t.Fatal("repair must succeed")
	}
	if _, err := os.Lstat(override); !os.IsNotExist(err) {
		t.Fatalf("the override file must not be created: %v", err)
	}
}

func TestReportRulesIntegrationHintsOverrideIssues(t *testing.T) {
	paths, cfg, _, override := setupOverrideDoctorHome(t)
	entry := filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
	block := "## Kander Rules Entry\n\nAt the start of every session, read `" + entry +
		"` and follow it as the Kander workflow rules entry.\n"
	writeRulesFile(t, override, "/dead/KANDER-AGENTS.md\n\n"+block+"\n"+block)

	lines := CaptureReport(func() {
		if !reportRulesIntegration(cfg, paths, false) {
			t.Error("a connected override keeps doctor healthy")
		}
	})
	var found bool
	for _, line := range lines {
		if strings.Contains(line.Text, override) && strings.Contains(line.Text, "invalid") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the hint must name the override file: %+v", lines)
	}
}
