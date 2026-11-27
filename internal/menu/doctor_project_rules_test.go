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

// A binary left in a retired project install runs with the global configuration, names the
// retirement in a notice, and every command but the exempt ones removes the global injection
// once.
func TestLegacyProjectBinaryRunsGloballyAndCleansTheGlobalInjection(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"kanban_agent": "grok"}))
	project := filepath.Join(h.root, "app")
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	bin := filepath.Join(project, ".kander", "bin", "kander")
	if err := copyFile(testKander, bin, 0o755); err != nil {
		t.Fatal(err)
	}
	projectConfig := filepath.Join(project, ".kander", "config.json")
	writeRulesFile(t, projectConfig, `{"schema_version":1,"kanban_agent":"claude"}`)
	claude := filepath.Join(h.home, ".claude", "CLAUDE.md")
	writeRulesFile(t, claude, "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n")
	// An earlier release installed the rules root next to the injection.
	writeRulesFile(t, filepath.Join(h.home, ".agents", "kander", "KANDER-AGENTS.md"), "# entry\n")
	run := func(args ...string) (int, string, string) {
		cmd := exec.Command(bin, args...)
		cmd.Env, cmd.Dir = h.env, project
		var out, errOut strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errOut
		code := 0
		if err := cmd.Run(); err != nil {
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		return code, out.String(), errOut.String()
	}
	if code, _, errOut := run("version"); code != 0 || strings.Contains(errOut, ".kander") {
		t.Fatalf("version: code=%d err=%q", code, errOut)
	}
	if got, _ := os.ReadFile(claude); !strings.Contains(string(got), "KANDER-AGENTS.md") {
		t.Fatalf("version cleaned the global injection: %q", got)
	}
	code, out, errOut := run("config")
	if code != 0 || !strings.Contains(out, "grok") || strings.Contains(out, "看板 Agent: claude") {
		t.Fatalf("config did not use the global configuration: code=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(errOut, filepath.Join(project, ".kander")) || !strings.Contains(errOut, "kander doctor") {
		t.Fatalf("no retirement notice: %q", errOut)
	}
	if !strings.Contains(errOut, "kander init") {
		t.Fatalf("no cleanup notice: %q", errOut)
	}
	if got, _ := os.ReadFile(claude); string(got) != "# Mine\n" {
		t.Fatalf("global injection kept: %q", got)
	}
	code, out, errOut = run("--json", "config")
	if code != 0 || !strings.Contains(out, `"code":"project_install_retired"`) || strings.Contains(out, `"code":"global_rules_cleaned"`) {
		t.Fatalf("json notices: code=%d out=%q err=%q", code, out, errOut)
	}
}
