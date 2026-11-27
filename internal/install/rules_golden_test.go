package install

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// The golden transcripts pin what kander init writes into the project rules files, what doctor
// detects there, and what the global injection cleanup and uninstall do to the files earlier
// releases wrote into the agents' global rules files, for every reference style and file format
// combination the embedded definitions produce. A refactor of the provider layer must leave
// them byte for byte unchanged. Regenerate them only for a deliberate change of written output:
// KANDER_UPDATE_GOLDEN=1 go test -run Golden.
const (
	rulesGoldenFile        = "testdata/rules_providers_golden.txt"
	rulesGoldenSymlinkFile = "testdata/rules_providers_symlink_golden.txt"
)

// goldenCombo is one agent with its project rules file (project) or its global rules file;
// together they cover the combinations of reference style and file format: claude-import with
// markdown, markdown-reference with a .mdc rule (global only), and markdown-reference with
// markdown. A project combination runs the kander init pipeline, a global one the cleanup.
type goldenCombo struct {
	name    string
	agent   string
	project bool
}

var goldenCombos = []goldenCombo{
	{"claude-global", "claude", false},
	{"claude-project", "claude", true},
	{"cursor-global", "cursor", false},
	{"cursor-project", "cursor", true},
	{"codex-global", "codex", false},
}

// globalBlock is the reference an earlier release wrote into a global rules file.
func globalBlock(agent string) string {
	if referenceStyleFor(agent).Name() == "claude-import" {
		return "@~/.agents/kander/KANDER-AGENTS.md\n"
	}
	return "## Kander Rules Entry\n\nAt the start of every session, read `~/.agents/kander/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry.\n"
}

// comboBlock is the block the combination's file holds when it references the entry.
func comboBlock(c goldenCombo) string {
	if c.project {
		return referenceBlock(c.agent)
	}
	return globalBlock(c.agent)
}

// goldenScenario prepares the rules file of one combination and names what the transcript
// records for it.
type goldenScenario struct {
	name string
	// content builds the initial rules file; ok=false leaves the file missing.
	content func(c goldenCombo, block string) (string, bool)
}

func goldenScenarios() []goldenScenario {
	fixed := func(text string) func(goldenCombo, string) (string, bool) {
		return func(goldenCombo, string) (string, bool) { return text, true }
	}
	withBlock := func(build func(block string) string) func(goldenCombo, string) (string, bool) {
		return func(_ goldenCombo, block string) (string, bool) { return build(block), true }
	}
	return []goldenScenario{
		{"create", func(goldenCombo, string) (string, bool) { return "", false }},
		{"append", fixed("# My own rules\n\nAlways be kind.\n")},
		{"append-no-trailing-newline", fixed("# My own rules")},
		{"append-after-blank-line", fixed("# My own rules\n\n")},
		{"detect-reference", withBlock(func(b string) string { return b })},
		{"detect-reference-after-notes", withBlock(func(b string) string { return "# Notes\n\n" + b })},
		{"detect-frontmatter-reference", withBlock(func(b string) string { return alwaysApplyFrontmatter + b })},
		{"detect-foreign-frontmatter", withBlock(func(b string) string { return "---\nglobs: '*.go'\n---\n\n" + b })},
		{"detect-unclosed-frontmatter", withBlock(func(b string) string { return "---\nalwaysApply: true\n" + b })},
		{"detect-comment-reference", withBlock(func(b string) string { return "<!--\n" + b + "-->\n" })},
		{"detect-fenced-reference", withBlock(func(b string) string { return "```\n" + b + "```\n" })},
		{"detect-negated-reference", withBlock(func(b string) string { return "This is deprecated, do not follow it.\n\n" + b })},
		{"detect-merged-entry", fixed("# Kander entry\n")},
		{"detect-unrelated", fixed("# Something else\n")},
		{"repair-legacy-import", fixed("# Personal\n\n@~/.agents/KANDER-AGENTS.md\n")},
		{"repair-legacy-bare-path", fixed("# Personal\n\nRead ~/.agents/KANDER-AGENTS.md first.\n\n~/.agents/KANDER-AGENTS.md\n")},
		{"repair-legacy-block", fixed("## Kander Rules Entry\n\nAt the start of every session, read `~/.agents/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry.\n")},
		{"repair-invalid-import", fixed("@/nowhere/KANDER-AGENTS.md\n# Personal\n")},
		{"repair-invalid-block", fixed("## Kander Rules Entry\n\nAt the start of every session, read `/nowhere/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry.\n")},
		{"repair-duplicate-import", withBlock(func(b string) string { return b + "\n" + b })},
		{"repair-duplicate-mixed", withBlock(func(b string) string {
			return "@~/.agents/kander/KANDER-AGENTS.md\n\n~/.agents/kander/KANDER-AGENTS.md\n\n" + b
		})},
		{"repair-stale-next-to-current", withBlock(func(b string) string { return "@~/.agents/KANDER-AGENTS.md\n" + b })},
		{"repair-import-inside-comment", fixed("<!-- @/nowhere/KANDER-AGENTS.md --> keep this\n")},
		{"repair-crlf", fixed("# Personal\r\n\r\n@/nowhere/KANDER-AGENTS.md\r\n")},
		{"uninstall-after-notes", withBlock(func(b string) string { return "# Personal\n\nKeep me.\n\n" + b })},
		{"uninstall-only-reference", withBlock(func(b string) string { return b })},
		{"uninstall-frontmatter-and-reference", withBlock(func(b string) string { return alwaysApplyFrontmatter + b })},
		{"uninstall-frontmatter-and-notes", withBlock(func(b string) string { return alwaysApplyFrontmatter + "Keep me.\n\n" + b })},
		{"uninstall-nothing-to-remove", fixed("# Personal\n")},
	}
}

// goldenEnvironment builds the isolated scope of one combination and returns its paths and
// the rules file the agent reads there. The temporary home is resolved first, so a TMPDIR that
// is an alias for another directory cannot make the spelling of a target differ from the real
// path an uninstall edit uses; the symlink scenarios create their own links below this home.
func goldenEnvironment(t *testing.T, c goldenCombo) (ProjectRules, string, string) {
	t.Helper()
	home := setupInstallHome(t)
	real, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if real != home {
		home = real
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
	}
	paths := config.InstallPaths{RulesDir: filepath.Join(home, ".agents", "kander"), ConfigPath: filepath.Join(home, ".config", "kander", "config.json")}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")
	root := filepath.Join(home, "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	project := ProjectRules{Root: root, Paths: paths}
	target := ProjectRulesTarget(c.agent, root)
	if !c.project {
		var err error
		if target, err = GlobalRulesTarget(c.agent, paths); err != nil {
			t.Fatal(err)
		}
	}
	if target == "" {
		t.Fatalf("no target for %s", c.name)
	}
	return project, target, home
}

// goldenTranscript renders paths and bytes with the temporary home abstracted away, so the
// transcript is identical on every machine and platform. Only paths and the messages that
// quote them are normalized; file bytes are written with %q and never altered, so a newline
// and a literal "\n" stay distinct.
type goldenTranscript struct {
	strings.Builder
	home string
}

// path renders a filesystem path.
func (g *goldenTranscript) path(path string) string {
	return filepath.ToSlash(strings.ReplaceAll(path, g.home, "<HOME>"))
}

// msg renders a message that quotes paths: the home goes first, then the slash style.
func (g *goldenTranscript) msg(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, g.home, "<HOME>"), "\\", "/")
}

// bytes renders file content losslessly.
func (g *goldenTranscript) bytes(data []byte) string {
	return fmt.Sprintf("%q", strings.ReplaceAll(string(data), g.home, "<HOME>"))
}

func (g *goldenTranscript) line(format string, args ...any) {
	g.WriteString(fmt.Sprintf(format, args...))
	g.WriteByte('\n')
}

func (g *goldenTranscript) file(label, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		g.line("%s: unreadable (%t)", label, err != nil)
		return
	}
	g.line("%s: %s", label, g.bytes(data))
}

func (g *goldenTranscript) outcome(o IntegrationOutcome, err error) string {
	text := fmt.Sprintf("status=%d removed=%d inactive=%t disabled=%v", o.Status, o.Removed, o.Inactive, o.DisabledBy)
	if o.Override != nil {
		text += fmt.Sprintf(" override={status=%d removed=%d inactive=%t}", o.Override.Status, o.Override.Removed, o.Override.Inactive)
	}
	if err != nil {
		text += " err=" + g.msg(err.Error())
	}
	return text
}

func goldenUninstall(g *goldenTranscript, paths config.InstallPaths, files ...string) {
	plan := PlanUninstall(paths)
	for _, item := range plan.Items {
		for _, file := range files {
			if item.Path != file {
				continue
			}
			g.line("uninstall: action=%d path=%s reason=%q content=%s", item.Action, g.path(item.Path), g.msg(item.Reason), g.bytes(item.content))
		}
	}
}

// text renders printed output as one quoted value. Only the home and the slash style of paths
// are normalized, and the normalization runs on the raw text, so every newline, including
// trailing ones, stays visible in the transcript.
func (g *goldenTranscript) text(raw string) string {
	return fmt.Sprintf("%q", g.msg(raw))
}

// goldenApplyUninstall runs the real uninstall in the isolated scope and records the plan
// prompt shown before the confirmation, the printed output, the error, the install_rules switch
// it writes, and what is left of each file.
func goldenApplyUninstall(g *goldenTranscript, paths config.InstallPaths, files ...string) {
	plan := PlanUninstall(paths)
	g.line("apply uninstall plan prompt: %s", g.text(planText(plan)))
	var out bytes.Buffer
	err := ApplyUninstall(plan, &out)
	g.line("apply uninstall: err=%t", err != nil)
	if err != nil {
		g.line("apply uninstall error: %s", g.text(err.Error()))
	}
	g.line("apply uninstall output: %s", g.text(out.String()))
	// Only the switch is recorded: the rest of the file is the default configuration, which
	// other changes own.
	if data, err := os.ReadFile(plan.ConfigFile); err == nil {
		g.line("apply uninstall config: install_rules=false %t", strings.Contains(string(data), `"install_rules": false`))
	}
	for _, file := range files {
		info, err := os.Lstat(file)
		switch {
		case err != nil:
			g.line("apply uninstall file %s: absent", g.path(file))
		case info.Mode()&os.ModeSymlink != 0:
			link, _ := os.Readlink(file)
			g.line("apply uninstall file %s: symlink to %s", g.path(file), g.path(link))
		default:
			data, _ := os.ReadFile(file)
			g.line("apply uninstall file %s: %s", g.path(file), g.bytes(data))
		}
	}
}

// runGoldenScenario records the observable behaviors for one scenario: for a project rules
// file what doctor detects and what kander init writes, for a global rules file what the
// cleanup and uninstall plan.
func runGoldenScenario(t *testing.T, g *goldenTranscript, c goldenCombo, s goldenScenario) {
	project, target, home := goldenEnvironment(t, c)
	g.home = home
	block := comboBlock(c)
	g.line("== %s / %s", c.name, s.name)
	g.line("block: %q", block)
	text, exists := s.content(c, block)
	if exists {
		writeIntegrateFile(t, target, text)
	}
	if !c.project {
		if exists {
			goldenUninstall(g, project.Paths, target)
		}
		return
	}
	ok, detail := ProjectReferenceStatus(c.agent, project)
	g.line("detect: ok=%t detail=%s", ok, g.msg(detail))
	for pass := 1; pass <= 2; pass++ {
		outcome, err := EnsureProjectReference(c.agent, project, config.InstallRulesOf(nil))
		g.line("ensure %d: %s target=%s", pass, g.outcome(outcome, err), g.path(outcome.Target))
		g.file(fmt.Sprintf("file after ensure %d", pass), target)
	}
	ok, detail = ProjectReferenceStatus(c.agent, project)
	g.line("detect after ensure: ok=%t detail=%s", ok, g.msg(detail))
}

// runGoldenApply records what a real uninstall does to the initial content of one global
// rules file scenario.
func runGoldenApply(t *testing.T, g *goldenTranscript, c goldenCombo, s goldenScenario) {
	project, target, home := goldenEnvironment(t, c)
	g.home = home
	g.line("== %s / %s / apply uninstall", c.name, s.name)
	if text, exists := s.content(c, comboBlock(c)); exists {
		writeIntegrateFile(t, target, text)
	}
	goldenApplyUninstall(g, project.Paths, target)
}

func TestRulesProvidersGolden(t *testing.T) {
	g := &goldenTranscript{}
	for _, c := range goldenCombos {
		for _, s := range goldenScenarios() {
			t.Run(c.name+"/"+s.name, func(t *testing.T) {
				runGoldenScenario(t, g, c, s)
			})
			if c.project {
				continue
			}
			t.Run(c.name+"/"+s.name+"/apply-uninstall", func(t *testing.T) {
				runGoldenApply(t, g, c, s)
			})
		}
	}
	// An AGENTS.override.md sibling is processed by the same pipeline and never created.
	t.Run("cursor-project/override", func(t *testing.T) {
		c := goldenCombos[3]
		project, target, home := goldenEnvironment(t, c)
		g.home = home
		g.line("== %s / override", c.name)
		override := filepath.Join(filepath.Dir(target), "AGENTS.override.md")
		writeIntegrateFile(t, target, "# Main\n")
		writeIntegrateFile(t, override, "# Override\n\n@/nowhere/KANDER-AGENTS.md\n")
		ok, detail := ProjectReferenceStatus(c.agent, project)
		g.line("detect: ok=%t detail=%s", ok, g.msg(detail))
		outcome, err := EnsureProjectReference(c.agent, project, config.InstallRulesOf(nil))
		g.line("ensure: %s", g.outcome(outcome, err))
		g.file("main", target)
		g.file("override", override)
	})
	t.Run("codex-global/override/apply-uninstall", func(t *testing.T) {
		c := goldenCombos[4]
		project, target, home := goldenEnvironment(t, c)
		g.home = home
		g.line("== %s / override / apply uninstall", c.name)
		override := filepath.Join(filepath.Dir(target), "AGENTS.override.md")
		writeIntegrateFile(t, target, "# Main\n\n"+globalBlock(c.agent))
		writeIntegrateFile(t, override, "# Override\n\n@/nowhere/KANDER-AGENTS.md\n")
		goldenUninstall(g, project.Paths, target, override)
		goldenApplyUninstall(g, project.Paths, target, override)
	})
	// Coverage order: kander init writes claude-import agents first.
	t.Run("project-coverage-order", func(t *testing.T) {
		g.line("== project-coverage-order: %v", ProjectReferenceAgents())
	})
	compareGolden(t, rulesGoldenFile, g.String())
}

// goldenLink builds one symlink arrangement below the target and returns the files the
// transcript follows afterwards.
type goldenLink struct {
	name  string
	build func(t *testing.T, paths config.InstallPaths, target, home string) []string
}

var goldenLinks = []goldenLink{
	{"symlink to other file", func(t *testing.T, _ config.InstallPaths, target, _ string) []string {
		other := filepath.Join(filepath.Dir(target), "other-rules.txt")
		writeIntegrateFile(t, other, "# Other\n")
		if err := os.Symlink(other, target); err != nil {
			t.Fatal(err)
		}
		return []string{target, other}
	}},
	{"symlink escaping the scope", func(t *testing.T, _ config.InstallPaths, target, home string) []string {
		outside := filepath.Join(home, "outside", "rules.md")
		writeIntegrateFile(t, outside, "# Outside\n")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		return []string{target, outside}
	}},
	{"symlink to the rules entry", func(t *testing.T, paths config.InstallPaths, target, _ string) []string {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(RulesEntry(paths), target); err != nil {
			t.Fatal(err)
		}
		return []string{target}
	}},
}

func TestRulesProvidersGoldenSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink scenarios need privileges on Windows")
	}
	g := &goldenTranscript{}
	for _, c := range goldenCombos {
		for _, link := range goldenLinks {
			t.Run(c.name+"/"+link.name, func(t *testing.T) {
				project, target, home := goldenEnvironment(t, c)
				g.home = home
				g.line("== %s / %s", c.name, link.name)
				files := link.build(t, project.Paths, target, home)
				if !c.project {
					goldenUninstall(g, project.Paths, files...)
					return
				}
				ok, detail := ProjectReferenceStatus(c.agent, project)
				g.line("detect: ok=%t detail=%s", ok, g.msg(detail))
				outcome, err := EnsureProjectReference(c.agent, project, config.InstallRulesOf(nil))
				g.line("ensure: %s", g.outcome(outcome, err))
				for _, file := range files[1:] {
					g.file("followed", file)
				}
			})
			if c.project {
				continue
			}
			t.Run(c.name+"/"+link.name+"/apply-uninstall", func(t *testing.T) {
				project, target, home := goldenEnvironment(t, c)
				g.home = home
				g.line("== %s / %s / apply uninstall", c.name, link.name)
				files := link.build(t, project.Paths, target, home)
				goldenApplyUninstall(g, project.Paths, files...)
			})
		}
	}
	compareGolden(t, rulesGoldenSymlinkFile, g.String())
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	if os.Getenv("KANDER_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(name, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	wantText := strings.ReplaceAll(string(want), "\r\n", "\n")
	if got == wantText {
		return
	}
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(wantText, "\n")
	for i := 0; i < max(len(gotLines), len(wantLines)); i++ {
		var a, b string
		if i < len(gotLines) {
			a = gotLines[i]
		}
		if i < len(wantLines) {
			b = wantLines[i]
		}
		if a != b {
			t.Fatalf("%s differs at line %d:\n got: %s\nwant: %s", name, i+1, a, b)
		}
	}
}
