package install

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestClassifyMDCFrontmatter(t *testing.T) {
	for _, tt := range []struct {
		text string
		want mdcFrontmatter
	}{
		{"", mdcNoFrontmatter},
		{"# rules\n", mdcNoFrontmatter},
		{"---\nalwaysApply: true\n---\nbody\n", mdcAlwaysApply},
		{"---\r\ndescription: x\r\nalwaysApply: true\r\n---\r\n", mdcAlwaysApply},
		{"---\nalwaysApply: false\n---\n", mdcOtherFrontmatter},
		{"---\nglobs: '*.go'\n---\n", mdcOtherFrontmatter},
		{"---\nalwaysApply: true\nnever closed\n", mdcOtherFrontmatter},
	} {
		if got := classifyMDCFrontmatter(tt.text); got != tt.want {
			t.Errorf("classify(%q)=%v want %v", tt.text, got, tt.want)
		}
	}
}

func cursorRuleSetup(t *testing.T) (string, string) {
	t.Helper()
	home := setupInstallHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(home, ".cursor", "rules", "kander.mdc")
}

func TestCursorRuleCoverageCreatesRulesDirectory(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalIntegrationPaths(t, home)
	if slices.Contains(IntegrationAgents(paths), "cursor") {
		t.Fatal("missing ~/.cursor must not be covered")
	}
	integrateAgentRules(paths)
	if _, err := os.Lstat(filepath.Join(home, ".cursor")); !os.IsNotExist(err) {
		t.Fatalf("missing ~/.cursor created: %v", err)
	}
	legacy := filepath.Join(home, ".cursor", "AGENTS.md")
	writeIntegrateFile(t, legacy, "# user cursor notes\n")
	if !slices.Contains(IntegrationAgents(paths), "cursor") {
		t.Fatal("existing ~/.cursor without rules/ must be covered")
	}
	for _, result := range integrateAgentRules(paths) {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	}
	target := filepath.Join(home, ".cursor", "rules", "kander.mdc")
	data, err := os.ReadFile(target)
	if err != nil || !strings.HasPrefix(string(data), alwaysApplyFrontmatter+"## Kander Rules Entry") {
		t.Fatalf("rule=%q err=%v", data, err)
	}
	if ok, detail := RulesIntegration("cursor", paths); !ok || detail != target {
		t.Fatalf("not integrated: %s", detail)
	}
	if data, err := os.ReadFile(legacy); err != nil || string(data) != "# user cursor notes\n" {
		t.Fatalf("legacy ~/.cursor/AGENTS.md changed: %q %v", data, err)
	}
}

func TestCursorRuleRepairsFrontmatterAndReference(t *testing.T) {
	home, target := cursorRuleSetup(t)
	paths := globalIntegrationPaths(t, home)
	block := referenceBlock("cursor", paths)

	writeIntegrateFile(t, target, block)
	if ok, detail := RulesIntegration("cursor", paths); ok || detail != config.Text("menu.mdc_rule_missing_frontmatter", target) {
		t.Fatalf("missing frontmatter: integrated=%v detail=%s", ok, detail)
	}
	out, err := EnsureRulesIntegration("cursor", paths)
	if err != nil || out.Status != IntegrationFrontmatterAdded {
		t.Fatalf("prepend: %+v %v", out, err)
	}
	if data, _ := os.ReadFile(target); string(data) != alwaysApplyFrontmatter+block {
		t.Fatalf("prepended=%q", data)
	}

	front := "---\ndescription: mine\nalwaysApply: true\n---\n\n# notes\n"
	writeIntegrateFile(t, target, front)
	if out, err := EnsureRulesIntegration("cursor", paths); err != nil || out.Status != IntegrationUpdated {
		t.Fatalf("append: %+v %v", out, err)
	}
	if data, _ := os.ReadFile(target); string(data) != front+"\n"+block {
		t.Fatalf("appended=%q", data)
	}
	if ok, detail := RulesIntegration("cursor", paths); !ok {
		t.Fatalf("not integrated after append: %s", detail)
	}
}

func TestCursorRuleKeepsUserFrontmatter(t *testing.T) {
	home, target := cursorRuleSetup(t)
	paths := globalIntegrationPaths(t, home)
	original := "---\nalwaysApply: false\n---\n\n" + referenceBlock("cursor", paths)
	writeIntegrateFile(t, target, original)
	if ok, detail := RulesIntegration("cursor", paths); ok || detail != config.Text("menu.mdc_rule_not_always_applied", target) {
		t.Fatalf("integrated=%v detail=%s", ok, detail)
	}
	if _, err := EnsureRulesIntegration("cursor", paths); err == nil {
		t.Fatal("repair accepted a rule without alwaysApply")
	}
	if data, _ := os.ReadFile(target); string(data) != original {
		t.Fatalf("user frontmatter rewritten: %q", data)
	}
}

func TestCursorRuleRejectsSymlink(t *testing.T) {
	home, target := cursorRuleSetup(t)
	paths := globalIntegrationPaths(t, home)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "rules.md")
	entry := RulesEntry(paths)
	writeIntegrateFile(t, linked, entry+"\n"+entry+"\n")
	if err := os.Symlink(linked, target); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if issues, err := InspectRulesReferences("cursor", paths); err != nil || issues.Total() != 0 {
		t.Fatalf("symlinked rule inspected: %+v %v", issues, err)
	}
	if ok, detail := RulesIntegration("cursor", paths); ok || !strings.Contains(detail, target) {
		t.Fatalf("integrated=%v detail=%s", ok, detail)
	}
	if _, err := EnsureRulesIntegration("cursor", paths); err == nil {
		t.Fatal("repair wrote through a symlinked rule")
	}
	if data, _ := os.ReadFile(linked); string(data) != entry+"\n"+entry+"\n" {
		t.Fatalf("link target changed: %q", data)
	}
	if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v", err)
	}
}
