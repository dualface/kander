//go:build unix

package menu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
