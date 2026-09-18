//go:build unix

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLegacyResidue places files at the flat ~/.agents root that earlier global
// installs used as their rules directory; doctor repair must report what it did
// with each of them.
func writeLegacyResidue(t *testing.T, home string, files []string, dirs []string) string {
	t.Helper()
	legacyDir := filepath.Join(home, ".agents")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(legacyDir, name), []byte("legacy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range dirs {
		if err := os.MkdirAll(filepath.Join(legacyDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return legacyDir
}

func TestDoctorReportsLegacyRulesRemoved(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground"}))
	removed := []string{"KANDER-AGENTS.md", "KANDER-BASE-RULES.md", "kander-rules-state.json"}
	legacyDir := writeLegacyResidue(t, h.home, removed, nil)
	code, _, out := h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	for _, name := range removed {
		path := filepath.Join(legacyDir, name)
		if !strings.Contains(out, "已删除旧规则位置的规则文件: "+path) {
			t.Fatalf("missing removal report for %s: %s", name, out)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy file still present: %s", path)
		}
	}
	// Without residue a second run must not print migration lines again.
	code, _, out = h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	if strings.Contains(out, "旧规则位置") {
		t.Fatalf("unexpected migration output without residue: %s", out)
	}
}

func TestDoctorReportsLegacyRulesKept(t *testing.T) {
	h := newHarness(t)
	h.installFake(false)
	h.writeConfig(defaultPayload(map[string]any{"launcher": "foreground"}))
	// A directory carrying a rule name cannot be removed by the migration and is
	// reported as kept instead of failing the repair.
	legacyDir := writeLegacyResidue(t, h.home, nil, []string{"KANDER-CODE-RULES.md"})
	kept := filepath.Join(legacyDir, "KANDER-CODE-RULES.md")
	code, _, out := h.run("doctor")
	if code != 0 {
		t.Fatalf("doctor=%d %s", code, out)
	}
	if !strings.Contains(out, "无法删除旧规则位置的文件，已保留: "+kept) {
		t.Fatalf("missing kept report: %s", out)
	}
	info, err := os.Lstat(kept)
	if err != nil || !info.IsDir() {
		t.Fatalf("kept directory lost: %v", err)
	}
}
