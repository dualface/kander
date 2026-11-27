package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func plannedChanges(groups []PlanGroup) map[string]RulesChange {
	out := map[string]RulesChange{}
	for _, group := range groups {
		for _, file := range group.Files {
			out[file.Target] = file.Change
		}
	}
	return out
}

func TestPlanRulesInjectionListsOnlyExtensionsAndSkills(t *testing.T) {
	home := setupInstallHome(t)
	paths, _ := config.GlobalInstallPaths()
	for _, dir := range []string{".claude", ".codex", ".pi/agent"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := []byte("// old release\n")
	previousExtensionHashes = append(previousExtensionHashes, fileHash(old))
	t.Cleanup(func() { previousExtensionHashes = previousExtensionHashes[:len(previousExtensionHashes)-1] })
	writeIntegrateFile(t, piExtensionTarget(t, home, false), string(old))

	changes := plannedChanges(PlanRulesInjection(paths))
	if got := changes[piExtensionTarget(t, home, false)]; got != RulesChangeUpdate {
		t.Fatalf("pi extension change=%v", got)
	}
	for target := range changes {
		for _, global := range []string{filepath.Join(home, ".claude", "CLAUDE.md"), filepath.Join(home, ".codex", "AGENTS.md"), filepath.Join(home, ".pi", "agent", "AGENTS.md")} {
			if target == global {
				t.Fatalf("the plan lists a global rules file: %s", target)
			}
		}
	}
	if _, ok := changes[filepath.Join(home, ".agents", "skills", leaderName, "SKILL.md")]; !ok {
		t.Fatalf("leader skill missing from the plan: %v", changes)
	}
}

func TestRulesChoiceRecordsOnlyChanges(t *testing.T) {
	off := false
	cfg := config.DefaultConfig()
	config.SetAgentInstallRules(cfg, "codex", &off)
	switches := config.InstallRulesOf(cfg)
	groups := []PlanGroup{{Agents: []string{"claude"}}, {Agents: []string{"codex"}}, {Agents: []string{"pi"}}}
	if got := rulesChoice(groups, []int{0, 2}, switches); len(got) != 1 || got["codex"] {
		t.Fatalf("default selection recorded %v", got)
	}
	got := rulesChoice(groups, []int{1}, switches)
	want := map[string]bool{"claude": false, "codex": true, "pi": false}
	if len(got) != len(want) {
		t.Fatalf("choice=%v", got)
	}
	for agent, value := range want {
		if got[agent] != value {
			t.Fatalf("choice=%v", got)
		}
	}
	if !groupEnabled(groups[0], switches) || groupEnabled(groups[1], switches) {
		t.Fatal("default selection does not follow the switches")
	}
}

func TestPerformSavesRulesChoiceBeforeWriting(t *testing.T) {
	home := setupInstallHome(t)
	for _, dir := range []string{".claude", ".codex"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Perform(Request{Language: "ja", Source: stubBinary(t), RulesChoice: map[string]bool{"codex": false}}); err != nil {
		t.Fatal(err)
	}
	paths, _ := config.GlobalInstallPaths()
	switches, err := config.ScopeInstallRules(paths)
	if err != nil || switches.Enabled("codex") || !switches.Enabled("claude") {
		t.Fatalf("switches=%+v err=%v", switches, err)
	}
	data := readBytes(t, paths.ConfigPath)
	for _, want := range []string{`"welcome_complete": false`, `"language": "ja"`} {
		if !strings.Contains(data, want) {
			t.Fatalf("config misses %s:\n%s", want, data)
		}
	}
	for _, file := range []string{filepath.Join(home, ".codex", "AGENTS.md"), filepath.Join(home, ".claude", "CLAUDE.md")} {
		if _, err := os.Lstat(file); !os.IsNotExist(err) {
			t.Fatalf("global rules file written: %s: %v", file, err)
		}
	}
}

func TestRulesChoiceStoresFalseForInheritedOff(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.InstallRules = false
	groups := []PlanGroup{{Agents: []string{"claude"}}, {Agents: []string{"codex"}}}
	got := rulesChoice(groups, []int{1}, config.InstallRulesOf(cfg))
	if len(got) != 2 || got["claude"] || !got["codex"] {
		t.Fatalf("choice=%v", got)
	}
}
