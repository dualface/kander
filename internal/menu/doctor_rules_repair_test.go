//go:build unix

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/install"
)

// Doctor repair must name every rule file it rewrote from the embedded copy, and stay
// silent once the installed set converges. The config pins language=en so the asserted
// text does not depend on the environment locale.
func TestDoctorReportsRewrittenRuleFiles(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground", "language": "en"}))
	// The first doctor run installs the embedded rules and reports every write.
	code, _, out := h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	if !strings.Contains(out, "rewrote rule file from the embedded copy: KANDER-AGENTS.md") {
		t.Fatalf("missing rewrite report: %s", out)
	}
	// A converged install reports nothing and rewrites nothing.
	code, _, out = h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	if strings.Contains(out, "embedded copy") {
		t.Fatalf("unexpected rewrite report on converged rules: %s", out)
	}
	// A deleted rule file is restored and reported by name.
	removed := filepath.Join(h.home, ".agents", "kander", "KANDER-CODE-RULES.md")
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	code, _, out = h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	if !strings.Contains(out, "rewrote rule file from the embedded copy: KANDER-CODE-RULES.md") {
		t.Fatalf("missing rewrite report for restored file: %s", out)
	}
	if strings.Contains(out, "rule file is missing") {
		t.Fatalf("restored file still reported missing: %s", out)
	}
}

func TestDoctorRepairsOnlyEnabledManagedLeaderSkills(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground", "language": "en"}))
	codex := filepath.Join(h.home, ".agents", "skills", "kander-leader")
	claude := filepath.Join(h.home, ".claude", "skills", "kander-leader")
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(claude)), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, _, out := h.run("doctor"); code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	for _, dir := range []string{codex, claude} {
		if _, err := os.Lstat(dir); !os.IsNotExist(err) {
			t.Fatalf("doctor created an unconfirmed skill at %s: %v", dir, err)
		}
	}
	if _, err := install.Perform(install.Request{Language: "en"}); err != nil {
		t.Fatal(err)
	}
	missingCodex := filepath.Join(codex, "references", "handover.md")
	missingClaude := filepath.Join(claude, "references", "handover.md")
	want, err := os.ReadFile(missingCodex)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{missingCodex, missingClaude} {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
	h.writeConfig(defaultPayload(map[string]any{
		"launcher": "foreground",
		"language": "en",
		"agents":   map[string]any{"claude": map[string]any{"install_rules": false}},
	}))
	if code, _, out := h.run("doctor"); code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	if got, err := os.ReadFile(missingCodex); err != nil || string(got) != string(want) {
		t.Fatalf("doctor did not restore the managed Codex document: %v", err)
	}
	if _, err := os.Lstat(missingClaude); !os.IsNotExist(err) {
		t.Fatalf("doctor restored a document of a disabled agent: %v", err)
	}
}
