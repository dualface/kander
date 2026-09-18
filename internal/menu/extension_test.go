package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func setupExtensionHome(t *testing.T) (string, config.InstallPaths, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents", "kander")}
	spec, _, ok := config.AgentExtension("pi")
	if !ok {
		t.Fatal("pi extension spec missing")
	}
	target := filepath.Join(home, filepath.FromSlash(spec.Global))
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, paths, target
}

func TestReportAgentExtensionsRepairsMissing(t *testing.T) {
	_, paths, target := setupExtensionHome(t)
	if reportAgentExtensions(paths, false) {
		t.Fatal("a missing extension must make doctor unhealthy")
	}
	if !reportAgentExtensions(paths, true) {
		t.Fatal("repair must succeed")
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("extension not written: %v", err)
	}
	if !reportAgentExtensions(paths, false) {
		t.Fatal("installed extension must be healthy")
	}
	if !reportAgentExtensions(paths, true) {
		t.Fatal("repair must be idempotent")
	}
}

func TestReportAgentExtensionsReportsModifiedWithoutOverwrite(t *testing.T) {
	_, paths, target := setupExtensionHome(t)
	if !reportAgentExtensions(paths, true) {
		t.Fatal("initial write failed")
	}
	foreign := []byte("// user edit\n")
	if err := os.WriteFile(target, foreign, 0o644); err != nil {
		t.Fatal(err)
	}
	lines := CaptureReport(func() {
		if !reportAgentExtensions(paths, false) {
			t.Error("a locally modified extension is a hint, not unhealthy")
		}
	})
	var found bool
	for _, line := range lines {
		if strings.Contains(line.Text, target) {
			found = true
		}
	}
	if !found {
		t.Fatalf("modified report misses the target: %+v", lines)
	}
	lines = CaptureReport(func() {
		if !reportAgentExtensions(paths, true) {
			t.Error("repair must keep the modified file and stay healthy")
		}
	})
	got, _ := os.ReadFile(target)
	if string(got) != string(foreign) {
		t.Fatalf("modified file overwritten: %q", got)
	}
}

func TestReportAgentExtensionsCoversProject(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	paths := config.InstallPaths{
		Mode:        config.ModeProject,
		ProjectRoot: project,
		RulesDir:    filepath.Join(project, ".kander", "rules"),
	}
	spec, _, _ := config.AgentExtension("pi")
	target := filepath.Join(project, filepath.FromSlash(spec.Project))
	if reportAgentExtensions(paths, false) {
		t.Fatal("missing project extension must be unhealthy")
	}
	if !reportAgentExtensions(paths, true) {
		t.Fatal("project repair failed")
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("project extension not written: %v", err)
	}
	if !reportAgentExtensions(paths, false) {
		t.Fatal("installed project extension must be healthy")
	}
}

func TestReportAgentExtensionsSkipsMissingPiDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents", "kander")}
	lines := CaptureReport(func() {
		if !reportAgentExtensions(paths, false) {
			t.Error("no ~/.pi/agent must produce no findings and stay healthy")
		}
	})
	if len(lines) != 0 {
		t.Fatalf("unexpected report lines: %+v", lines)
	}
	if !reportAgentExtensions(paths, true) {
		t.Fatal("repair with no pi must be a no-op success")
	}
	if _, err := os.Lstat(filepath.Join(home, ".pi")); !os.IsNotExist(err) {
		t.Fatalf("repair created ~/.pi: %v", err)
	}
}
