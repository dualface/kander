//go:build unix

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
