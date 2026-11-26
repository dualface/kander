package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install/installtest"
)

func TestReportLeaderSkillsRepairsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	installtest.IsolateAgentConfigEnv(t)
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents", "kander")}
	entry := filepath.Join(home, ".agents", "skills", "kander-leader", "SKILL.md")

	if reportLeaderSkills(paths, allInstallRules, false) {
		t.Fatal("a missing leader skill must make doctor unhealthy")
	}
	if _, err := os.Lstat(entry); !os.IsNotExist(err) {
		t.Fatalf("the read-only report wrote the skill: %v", err)
	}
	lines := CaptureReport(func() {
		if !reportLeaderSkills(paths, allInstallRules, true) {
			t.Error("repair must succeed")
		}
	})
	if _, err := os.Lstat(entry); err != nil {
		t.Fatalf("skill not written: %v", err)
	}
	var reported bool
	for _, line := range lines {
		if strings.Contains(line.Text, filepath.Dir(entry)) {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("repair report misses the skill directory: %+v", lines)
	}
	if !reportLeaderSkills(paths, allInstallRules, false) {
		t.Fatal("an installed leader skill must be healthy")
	}

	if err := os.WriteFile(entry, []byte("my leader"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !reportLeaderSkills(paths, allInstallRules, true) {
		t.Fatal("a locally edited skill is a hint, not unhealthy")
	}
	if got, _ := os.ReadFile(entry); string(got) != "my leader" {
		t.Fatalf("repair overwrote the local edit: %q", got)
	}
}
