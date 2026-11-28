package menu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
	"github.com/dualface/kander/v2/internal/install/installtest"
)

// boundCheckout is a Git checkout in a temporary home whose overlay binds a project, made the
// process directory. It returns the main worktree and the global install paths.
func boundCheckout(t *testing.T, bound bool) (string, config.InstallPaths) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	root := filepath.Join(t.TempDir(), "app")
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if bound {
		writeRulesFile(t, filepath.Join(root, config.OverlayFilename), `{"project":{"name":"app"}}`)
	}
	t.Chdir(root)
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	writeRulesFile(t, install.RulesEntry(paths), "# Kander entry\n")
	return root, paths
}

func TestDoctorReportsMissingProjectReferencesWithoutWriting(t *testing.T) {
	root, paths := boundCheckout(t, true)
	var healthy bool
	lines := CaptureReport(func() { healthy = reportProjectReferences(paths, config.InstallRulesOf(nil)) })
	text := reportText(lines)
	if healthy || !strings.Contains(text, config.Text("projectrules.doctor_run_init")) {
		t.Fatalf("healthy=%v\n%s", healthy, text)
	}
	for _, file := range []string{"CLAUDE.md", "AGENTS.md"} {
		if _, err := os.Lstat(filepath.Join(root, file)); !os.IsNotExist(err) {
			t.Fatalf("doctor wrote %s: %v", file, err)
		}
	}
	writeRulesFile(t, filepath.Join(root, "AGENTS.md"), "@.kander/rules/KANDER-AGENTS.md\n")
	text = reportText(CaptureReport(func() { healthy = reportProjectReferences(paths, config.InstallRulesOf(nil)) }))
	if healthy || !strings.Contains(text, ".kander/rules/KANDER-AGENTS.md") {
		t.Fatalf("legacy reference not reported:\n%s", text)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(got) != "@.kander/rules/KANDER-AGENTS.md\n" {
		t.Fatalf("doctor rewrote the reference: %q", got)
	}
	project, err := install.NewProjectRules(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	install.EnsureProjectReferences(project, config.InstallRulesOf(nil))
	text = reportText(CaptureReport(func() { healthy = reportProjectReferences(paths, config.InstallRulesOf(nil)) }))
	if !healthy || hasWarning(CaptureReport(func() { reportProjectReferences(paths, config.InstallRulesOf(nil)) })) {
		t.Fatalf("referenced checkout unhealthy:\n%s", text)
	}
}

func TestDoctorSkipsProjectReferencesOfAnUnboundCheckout(t *testing.T) {
	_, paths := boundCheckout(t, false)
	var healthy bool
	lines := CaptureReport(func() { healthy = reportProjectReferences(paths, config.InstallRulesOf(nil)) })
	if !healthy || len(lines) != 0 {
		t.Fatalf("healthy=%v lines=%s", healthy, reportText(lines))
	}
}

func TestDoctorReportsAndRepairsLegacyProjectInstall(t *testing.T) {
	root, _ := boundCheckout(t, false)
	state := filepath.Join(root, ".kander", "rules", "kander-rules-state.json")
	writeRulesFile(t, state, "{}\n")
	writeRulesFile(t, filepath.Join(root, "CLAUDE.md"), "@.kander/rules/KANDER-AGENTS.md\n")
	var healthy bool
	text := reportText(CaptureReport(func() { healthy = reportLegacyProjectInstall(false) }))
	if healthy || !strings.Contains(text, config.Text("projectrules.legacy_would_remove", state)) || !strings.Contains(text, "CLAUDE.md") {
		t.Fatalf("check: healthy=%v\n%s", healthy, text)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("check removed a file: %v", err)
	}
	text = reportText(CaptureReport(func() { healthy = reportLegacyProjectInstall(true) }))
	if !healthy || !strings.Contains(text, config.Text("projectrules.legacy_removed", state)) {
		t.Fatalf("repair: healthy=%v\n%s", healthy, text)
	}
	if _, err := os.Lstat(filepath.Join(root, ".kander")); !os.IsNotExist(err) {
		t.Fatalf(".kander kept: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md")); string(got) != "@.kander/rules/KANDER-AGENTS.md\n" {
		t.Fatalf("doctor rewrote CLAUDE.md: %q", got)
	}
}
