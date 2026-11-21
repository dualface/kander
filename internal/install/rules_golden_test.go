package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// The golden transcripts pin what install, doctor and uninstall write and print for every
// reference style and file format combination the embedded definitions produce. They were
// generated from the implementation that predates the rules integration providers, so a
// refactor of the provider layer must leave them byte for byte unchanged. Regenerate them only
// for a deliberate change of written output: KANDER_UPDATE_GOLDEN=1 go test -run Golden.
const (
	rulesGoldenFile        = "testdata/rules_providers_golden.txt"
	rulesGoldenSymlinkFile = "testdata/rules_providers_symlink_golden.txt"
)

// goldenCombo is one agent in one scope; together the five cover the three combinations of
// reference style and file format: claude-import with markdown, markdown-reference with a
// .mdc rule, and markdown-reference with markdown.
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
// the rules file the agent reads there.
func goldenEnvironment(t *testing.T, c goldenCombo) (config.InstallPaths, string, string) {
	t.Helper()
	home := setupInstallHome(t)
	var paths config.InstallPaths
	if c.project {
		root := filepath.Join(home, "project")
		paths = config.InstallPaths{Mode: config.ModeProject, ProjectRoot: root, RulesDir: filepath.Join(root, ".kander", "rules")}
	} else {
		paths = config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents", "kander")}
	}
	writeIntegrateFile(t, RulesEntry(paths), "# Kander entry\n")
	target, err := AgentRulesTarget(c.agent, paths)
	if err != nil || target == "" {
		t.Fatalf("target=%q err=%v", target, err)
	}
	return paths, target, home
}

// goldenTranscript renders paths and bytes with the temporary home abstracted away, so the
// transcript is identical on every machine and platform.
type goldenTranscript struct {
	strings.Builder
	home string
}

func (g *goldenTranscript) line(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	text = strings.ReplaceAll(text, g.home, "<HOME>")
	text = strings.ReplaceAll(text, "\\", "/")
	g.WriteString(text)
	g.WriteByte('\n')
}

func (g *goldenTranscript) file(label, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		g.line("%s: unreadable (%v)", label, err != nil)
		return
	}
	g.line("%s: %q", label, string(data))
}

func goldenOutcome(o IntegrationOutcome, err error) string {
	text := fmt.Sprintf("status=%d removed=%d inactive=%t disabled=%v", o.Status, o.Removed, o.Inactive, o.DisabledBy)
	if o.Override != nil {
		text += fmt.Sprintf(" override={status=%d removed=%d inactive=%t}", o.Override.Status, o.Override.Removed, o.Override.Inactive)
	}
	if err != nil {
		text += " err=" + err.Error()
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
			g.line("uninstall: action=%d path=%s reason=%q content=%q", item.Action, item.Path, item.Reason, string(item.content))
		}
	}
}

// runGoldenScenario records the five observable behaviors for one scenario: what an install
// writes, what doctor detects, what a repair rewrites, and what uninstall plans.
func runGoldenScenario(t *testing.T, g *goldenTranscript, c goldenCombo, s goldenScenario) {
	paths, target, home := goldenEnvironment(t, c)
	g.home = home
	block := referenceBlock(c.agent, paths)
	g.line("== %s / %s", c.name, s.name)
	g.line("block: %q", block)
	text, exists := s.content(c, block)
	if exists {
		writeIntegrateFile(t, target, text)
	}
	ok, detail := RulesIntegration(c.agent, paths)
	g.line("detect: ok=%t detail=%s", ok, detail)
	if exists {
		issues, err := InspectRulesReferences(c.agent, paths)
		g.line("inspect: invalid=%d duplicates=%d files=%d err=%v", issues.Invalid, issues.Duplicates, len(issues.Files), err != nil)
		goldenUninstall(g, paths, target)
	}
	for pass := 1; pass <= 2; pass++ {
		outcome, err := EnsureRulesIntegration(c.agent, paths)
		g.line("ensure %d: %s target=%s", pass, goldenOutcome(outcome, err), outcome.Target)
		g.file(fmt.Sprintf("file after ensure %d", pass), target)
	}
	ok, detail = RulesIntegration(c.agent, paths)
	g.line("detect after ensure: ok=%t detail=%s", ok, detail)
	goldenUninstall(g, paths, target)
}

func TestRulesProvidersGolden(t *testing.T) {
	g := &goldenTranscript{}
	for _, c := range goldenCombos {
		for _, s := range goldenScenarios() {
			t.Run(c.name+"/"+s.name, func(t *testing.T) {
				runGoldenScenario(t, g, c, s)
			})
		}
	}
	// An AGENTS.override.md sibling is processed by the same pipeline and never created.
	for _, c := range []goldenCombo{{"cursor-project", "cursor", true}, {"codex-global", "codex", false}} {
		t.Run(c.name+"/override", func(t *testing.T) {
			paths, target, home := goldenEnvironment(t, c)
			g.home = home
			g.line("== %s / override", c.name)
			override := filepath.Join(filepath.Dir(target), "AGENTS.override.md")
			writeIntegrateFile(t, target, "# Main\n")
			writeIntegrateFile(t, override, "# Override\n\n@/nowhere/KANDER-AGENTS.md\n")
			ok, detail := RulesIntegration(c.agent, paths)
			g.line("detect: ok=%t detail=%s", ok, detail)
			outcome, err := EnsureRulesIntegration(c.agent, paths)
			g.line("ensure: %s", goldenOutcome(outcome, err))
			g.file("main", target)
			g.file("override", override)
			goldenUninstall(g, paths, target, override)
		})
	}
	// Coverage order: a project install writes claude-import agents first.
	t.Run("project-coverage-order", func(t *testing.T) {
		_, _, home := goldenEnvironment(t, goldenCombos[1])
		g.home = home
		paths := config.InstallPaths{Mode: config.ModeProject, ProjectRoot: filepath.Join(home, "project"), RulesDir: filepath.Join(home, "project", ".kander", "rules")}
		g.line("== project-coverage-order: %v", IntegrationAgents(paths))
	})
	compareGolden(t, rulesGoldenFile, g.String())
}

func TestRulesProvidersGoldenSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink scenarios need privileges on Windows")
	}
	g := &goldenTranscript{}
	for _, c := range goldenCombos {
		t.Run(c.name, func(t *testing.T) {
			paths, target, home := goldenEnvironment(t, c)
			g.home = home
			g.line("== %s / symlink to other file", c.name)
			other := filepath.Join(filepath.Dir(target), "other-rules.txt")
			writeIntegrateFile(t, other, "# Other\n")
			if err := os.Symlink(other, target); err != nil {
				t.Fatal(err)
			}
			ok, detail := RulesIntegration(c.agent, paths)
			g.line("detect: ok=%t detail=%s", ok, detail)
			issues, err := InspectRulesReferences(c.agent, paths)
			g.line("inspect: invalid=%d duplicates=%d files=%d err=%v", issues.Invalid, issues.Duplicates, len(issues.Files), err != nil)
			outcome, err := EnsureRulesIntegration(c.agent, paths)
			g.line("ensure: %s", goldenOutcome(outcome, err))
			g.file("other", other)
			goldenUninstall(g, paths, target, other)
		})
		t.Run(c.name+"/escape", func(t *testing.T) {
			paths, target, home := goldenEnvironment(t, c)
			g.home = home
			g.line("== %s / symlink escaping the scope", c.name)
			outside := filepath.Join(home, "outside", "rules.md")
			writeIntegrateFile(t, outside, "# Outside\n")
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}
			outcome, err := EnsureRulesIntegration(c.agent, paths)
			g.line("ensure: %s", goldenOutcome(outcome, err))
			g.file("outside", outside)
			goldenUninstall(g, paths, target, outside)
		})
		t.Run(c.name+"/entry", func(t *testing.T) {
			paths, target, home := goldenEnvironment(t, c)
			g.home = home
			g.line("== %s / symlink to the rules entry", c.name)
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(RulesEntry(paths), target); err != nil {
				t.Fatal(err)
			}
			ok, detail := RulesIntegration(c.agent, paths)
			g.line("detect: ok=%t detail=%s", ok, detail)
			outcome, err := EnsureRulesIntegration(c.agent, paths)
			g.line("ensure: %s", goldenOutcome(outcome, err))
			goldenUninstall(g, paths, target)
		})
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
