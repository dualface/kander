package install

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
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

func TestPlanRulesInjectionMarksEveryChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	paths, _ := config.GlobalInstallPaths()
	for _, dir := range []string{".claude", ".codex", ".grok", ".config/opencode", ".pi/agent", ".kimi-code"} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entry := "~/.agents/kander/KANDER-AGENTS.md"
	codex := filepath.Join(home, ".codex", "AGENTS.md")
	writeIntegrateFile(t, codex, "# Mine\n")
	override := filepath.Join(home, ".codex", "AGENTS.override.md")
	writeIntegrateFile(t, override, "# Override\n\n"+referenceBlock("codex", paths))
	grok := filepath.Join(home, ".grok", "AGENTS.md")
	writeIntegrateFile(t, grok, "# Mine\n\n"+referenceBlock("grok", paths)+"\n"+entry+"\n")
	opencode := filepath.Join(home, ".config", "opencode", "AGENTS.md")
	if err := os.Symlink(legacyRulesEntry(paths), opencode); err != nil {
		t.Fatal(err)
	}
	pi := filepath.Join(home, ".pi", "agent", "AGENTS.md")
	writeIntegrateFile(t, pi, referenceBlock("pi", paths))
	old := []byte("// old release\n")
	previousExtensionHashes = append(previousExtensionHashes, fileHash(old))
	t.Cleanup(func() { previousExtensionHashes = previousExtensionHashes[:len(previousExtensionHashes)-1] })
	writeIntegrateFile(t, piExtensionTarget(t, home, false), string(old))
	kimi := filepath.Join(home, ".kimi-code", "AGENTS.md")
	before := map[string]string{codex: "# Mine\n", override: readBytes(t, override), grok: readBytes(t, grok), pi: readBytes(t, pi)}

	changes := plannedChanges(PlanRulesInjection(paths))
	want := map[string]RulesChange{
		filepath.Join(home, ".claude", "CLAUDE.md"): RulesChangeCreate,
		codex:                             RulesChangeAppend,
		override:                          RulesChangeNone,
		grok:                              RulesChangeRewrite,
		opencode:                          RulesChangeReplace,
		pi:                                RulesChangeNone,
		piExtensionTarget(t, home, false): RulesChangeUpdate,
		kimi:                              RulesChangeCreate,
	}
	for target, change := range want {
		if got, ok := changes[target]; !ok || got != change {
			t.Errorf("%s: change=%v ok=%v want %v", target, got, ok, change)
		}
	}
	for path, content := range before {
		if got := readBytes(t, path); got != content {
			t.Errorf("planning wrote %s: %q", path, got)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "CLAUDE.md")); !os.IsNotExist(err) {
		t.Fatalf("planning created a file: %v", err)
	}
}

func TestPlanRulesInjectionGroupsSharedProjectFile(t *testing.T) {
	setupInstallHome(t)
	project := initGitRepo(t, filepath.Join(t.TempDir(), "app"))
	paths, err := config.ProjectInstallPaths(project)
	if err != nil {
		t.Fatal(err)
	}
	groups := PlanRulesInjection(paths)
	if len(groups) != 2 || !slices.Equal(groups[0].Agents, []string{"claude"}) {
		t.Fatalf("groups=%+v", groups)
	}
	shared := groups[1]
	if !slices.Contains(shared.Agents, "codex") || !slices.Contains(shared.Agents, "pi") {
		t.Fatalf("shared agents=%v", shared.Agents)
	}
	targets := []string{}
	for _, file := range shared.Files {
		targets = append(targets, file.Target)
	}
	if !slices.Equal(targets, []string{filepath.Join(project, "AGENTS.md"), piExtensionTarget(t, project, true)}) {
		t.Fatalf("shared files=%v", targets)
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
	if _, err := os.Lstat(filepath.Join(home, ".codex", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("codex rules file written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		t.Fatal(err)
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

func TestPlanRulesInjectionAssumesLegacyMigration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setupInstallHome(t)
	paths, _ := config.GlobalInstallPaths()
	legacy := legacyRulesEntry(paths)
	writeIntegrateFile(t, legacy, "# previous entry\n")
	for _, dir := range []string{".claude", ".codex"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	claude := filepath.Join(home, ".claude", "CLAUDE.md")
	writeIntegrateFile(t, claude, "# Mine\n\n@~/.agents/KANDER-AGENTS.md\n")
	codex := filepath.Join(home, ".codex", "AGENTS.md")
	if err := os.Symlink(legacy, codex); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(home, ".codex", "AGENTS.override.md")
	writeIntegrateFile(t, override, "# Override\n")
	changes := plannedChanges(PlanRulesInjection(paths))
	for target, want := range map[string]RulesChange{claude: RulesChangeRewrite, codex: RulesChangeReplace, override: RulesChangeAppend} {
		if got, ok := changes[target]; !ok || got != want {
			t.Errorf("%s: change=%v ok=%v want %v", target, got, ok, want)
		}
	}
	result, err := Perform(Request{Language: "cn", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]IntegrationStatus{}
	for _, item := range result.Integrations {
		actual[item.Target] = item.Status
		if item.Override != nil {
			actual[item.Override.Target] = item.Override.Status
		}
	}
	if actual[claude] != IntegrationRewritten || actual[override] != IntegrationUpdated || !slices.Contains(result.LegacyLinksRemoved, codex) {
		t.Fatalf("install did not match the plan: %v links=%v", actual, result.LegacyLinksRemoved)
	}
}
