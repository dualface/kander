package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

var allLeaderSwitches = config.InstallRulesOf(nil)

func leaderOutcomeFor(t *testing.T, outcomes []LeaderSkillOutcome, agent string) LeaderSkillOutcome {
	t.Helper()
	for _, outcome := range outcomes {
		if outcome.Agent == agent {
			if outcome.Err != nil {
				t.Fatalf("%s: %v", agent, outcome.Err)
			}
			return outcome
		}
	}
	t.Fatalf("no outcome for %s in %+v", agent, outcomes)
	return LeaderSkillOutcome{}
}

func leaderDoctorHome(t *testing.T) (string, config.InstallPaths) {
	t.Helper()
	home := setupInstallHome(t)
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, config.InstallPaths{RulesDir: filepath.Join(home, ".agents", "kander")}
}

func TestEnsureLeaderSkillsInstallsAndUpdates(t *testing.T) {
	home, paths := leaderDoctorHome(t)
	shared := filepath.Join(home, ".agents", "skills", leaderName)
	claude := filepath.Join(home, ".claude", "skills", leaderName)

	inspected := InspectLeaderSkills(paths, allLeaderSwitches)
	for _, agent := range []string{"codex", "claude"} {
		if got := leaderOutcomeFor(t, inspected, agent).Status; got != LeaderSkillMissing {
			t.Fatalf("%s before repair: status %d, want missing", agent, got)
		}
	}
	if _, err := os.Lstat(shared); !os.IsNotExist(err) {
		t.Fatalf("inspection wrote the skill: %v", err)
	}

	ensured := EnsureLeaderSkills(paths, allLeaderSwitches)
	for _, agent := range []string{"codex", "claude"} {
		if got := leaderOutcomeFor(t, ensured, agent).Status; got != LeaderSkillWritten {
			t.Fatalf("%s repair: status %d, want written", agent, got)
		}
	}
	assertSkill(t, shared)
	assertSkill(t, claude)
	for _, outcome := range EnsureLeaderSkills(paths, allLeaderSwitches) {
		if outcome.Err != nil || outcome.Status != LeaderSkillInstalled {
			t.Fatalf("second repair must be a no-op: %+v", outcome)
		}
	}

	// Model a skill installed by an earlier binary.
	old := []byte("previous distributed version")
	writeIntegrateFile(t, filepath.Join(shared, "SKILL.md"), string(old))
	s, err := inspectLeaderSkill(shared, paths)
	if err != nil {
		t.Fatal(err)
	}
	s.receipt.Files["SKILL.md"] = fileHash(old)
	record, err := json.Marshal(s.receipt)
	if err != nil {
		t.Fatal(err)
	}
	writeIntegrateFile(t, filepath.Join(shared, leaderReceipt), string(record))
	if got := leaderOutcomeFor(t, InspectLeaderSkills(paths, allLeaderSwitches), "codex").Status; got != LeaderSkillOutdated {
		t.Fatalf("status %d, want outdated", got)
	}
	if got := leaderOutcomeFor(t, EnsureLeaderSkills(paths, allLeaderSwitches), "codex").Status; got != LeaderSkillUpdated {
		t.Fatalf("status %d, want updated", got)
	}
	assertSkill(t, shared)
}

func TestEnsureLeaderSkillsKeepsLocalEdits(t *testing.T) {
	home, paths := leaderDoctorHome(t)
	EnsureLeaderSkills(paths, allLeaderSwitches)
	claude := filepath.Join(home, ".claude", "skills", leaderName)
	custom := filepath.Join(claude, "SKILL.md")
	writeIntegrateFile(t, custom, "my leader")

	inspected := leaderOutcomeFor(t, InspectLeaderSkills(paths, allLeaderSwitches), "claude")
	if inspected.Status != LeaderSkillModified || !inspected.Modified {
		t.Fatalf("edited skill: %+v", inspected)
	}
	if got := leaderOutcomeFor(t, EnsureLeaderSkills(paths, allLeaderSwitches), "claude"); got.Status != LeaderSkillModified {
		t.Fatalf("repair must leave an edit-only skill alone: %+v", got)
	}

	reference := filepath.Join(claude, "references", "handover.md")
	if err := os.Remove(reference); err != nil {
		t.Fatal(err)
	}
	inspected = leaderOutcomeFor(t, InspectLeaderSkills(paths, allLeaderSwitches), "claude")
	if inspected.Status != LeaderSkillMissing || !inspected.Modified {
		t.Fatalf("missing reference beside an edit: %+v", inspected)
	}
	repaired := leaderOutcomeFor(t, EnsureLeaderSkills(paths, allLeaderSwitches), "claude")
	if repaired.Status != LeaderSkillUpdated || !repaired.Modified {
		t.Fatalf("repair beside an edit: %+v", repaired)
	}
	if readBytes(t, custom) != "my leader" {
		t.Fatal("repair overwrote the local edit")
	}
	if readBytes(t, reference) == "" {
		t.Fatal("repair did not restore the missing reference")
	}
}

func TestEnsureLeaderSkillsKeepsUserOwnedSkills(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory symlinks require privileges")
	}
	home, paths := leaderDoctorHome(t)
	shared := filepath.Join(home, ".agents", "skills", leaderName)
	entry := filepath.Join(shared, "SKILL.md")
	writeIntegrateFile(t, entry, "my own")
	link := filepath.Join(home, ".claude", "skills", leaderName)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", ".agents", "skills", leaderName), link); err != nil {
		t.Fatal(err)
	}
	outcomes := EnsureLeaderSkills(paths, allLeaderSwitches)
	for _, agent := range []string{"codex", "claude"} {
		if got := leaderOutcomeFor(t, outcomes, agent).Status; got != LeaderSkillUnmanaged {
			t.Fatalf("%s: status %d, want unmanaged", agent, got)
		}
	}
	if readBytes(t, entry) != "my own" {
		t.Fatal("repair overwrote a user-owned skill")
	}
	if _, err := os.Lstat(filepath.Join(shared, leaderReceipt)); !os.IsNotExist(err) {
		t.Fatalf("repair adopted a user-owned skill: %v", err)
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("repair replaced the link: %v", err)
	}
}

func TestEnsureLeaderSkillsRespectsInstallRules(t *testing.T) {
	home, paths := leaderDoctorHome(t)
	switches := config.InstallRules{Global: true, Agents: map[string]bool{"codex": false}}
	outcomes := EnsureLeaderSkills(paths, switches)
	if got := leaderOutcomeFor(t, outcomes, "codex").Status; got != LeaderSkillDisabled {
		t.Fatalf("codex: status %d, want disabled", got)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "skills", leaderName)); !os.IsNotExist(err) {
		t.Fatalf("disabled skill was written: %v", err)
	}
	assertSkill(t, filepath.Join(home, ".claude", "skills", leaderName))
}

func TestLeaderSkillsCoverClaudeOnlyWhenInstalled(t *testing.T) {
	home := setupInstallHome(t)
	paths := config.InstallPaths{RulesDir: filepath.Join(home, ".agents", "kander")}
	outcomes := EnsureLeaderSkills(paths, allLeaderSwitches)
	if len(outcomes) != 1 || outcomes[0].Agent != "codex" {
		t.Fatalf("outcomes without a Claude configuration: %+v", outcomes)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("repair created the Claude configuration directory: %v", err)
	}
}
