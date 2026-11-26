package install

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func TestReferenceStylesMatchTheConfigNames(t *testing.T) {
	var names []string
	for _, style := range referenceStyles {
		names = append(names, style.Name())
	}
	want := slices.Clone(config.RulesIntegrations())
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("styles %v, config accepts %v", names, want)
	}
	if len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Fatalf("duplicate style names: %v", names)
	}
}

func TestEveryEmbeddedAgentGetsItsDeclaredStyle(t *testing.T) {
	for _, agent := range config.ExecutionAgents {
		spec, _ := config.RulesSpec(agent)
		if got := referenceStyleFor(agent).Name(); got != spec.Integration {
			t.Errorf("%s: style %q, definition declares %q", agent, got, spec.Integration)
		}
	}
}

func TestProvidersOfTheBuiltInCombinations(t *testing.T) {
	for _, tt := range []struct {
		agent, target, style, format string
	}{
		{"claude", "CLAUDE.md", "claude-import", "markdown"},
		{"cursor", "kander.mdc", "markdown-reference", "mdc-rule"},
		{"cursor", "AGENTS.md", "markdown-reference", "markdown"},
		{"codex", "AGENTS.md", "markdown-reference", "markdown"},
	} {
		if got := referenceStyleFor(tt.agent).Name(); got != tt.style {
			t.Errorf("%s style=%q want %q", tt.agent, got, tt.style)
		}
		if got := fileFormatFor(filepath.Join("dir", tt.target)).Name(); got != tt.format {
			t.Errorf("%s format=%q want %q", tt.target, got, tt.format)
		}
	}
	if got := referenceStyleFor("no-such-agent").Name(); got != "markdown-reference" {
		t.Errorf("unknown agent style=%q", got)
	}
}

func TestFileFormatIsSelectedByExtensionOnly(t *testing.T) {
	for target, want := range map[string]string{
		"kander.mdc":      "mdc-rule",
		"KANDER.MDC":      "mdc-rule",
		"AGENTS.md":       "markdown",
		"rules.mdc.bak":   "markdown",
		"mdc":             "markdown",
		"AGENTS.override": "markdown",
	} {
		if got := fileFormatFor(target).Name(); got != want {
			t.Errorf("fileFormatFor(%q)=%q want %q", target, got, want)
		}
	}
}

func TestReferenceStyleBehaviors(t *testing.T) {
	claude, markdown := referenceStyleFor("claude"), referenceStyleFor("codex")
	if claude.Block("~/e/KANDER-AGENTS.md") != "@~/e/KANDER-AGENTS.md\n" {
		t.Errorf("claude block=%q", claude.Block("~/e/KANDER-AGENTS.md"))
	}
	if !claude.ProjectWriteFirst() || markdown.ProjectWriteFirst() {
		t.Error("only claude-import writes first in project scope")
	}
	if !claude.ProjectSymlinkMayDiffer() || markdown.ProjectSymlinkMayDiffer() {
		t.Error("only claude-import may read a project symlink that does not point at the entry")
	}
	for _, tt := range []struct {
		line   string
		claude bool
		bare   bool
	}{
		{"@~/.agents/kander/KANDER-AGENTS.md", true, false},
		{"~/.agents/kander/KANDER-AGENTS.md", false, true},
		{"`~/.agents/kander/KANDER-AGENTS.md`", false, true},
		{"@KANDER-AGENTS.md", false, false},
		{"read ~/.agents/kander/KANDER-AGENTS.md", false, false},
	} {
		if _, ok := claude.LoadCommand(tt.line); ok != tt.claude {
			t.Errorf("claude LoadCommand(%q)=%t", tt.line, ok)
		}
		if _, ok := markdown.LoadCommand(tt.line); ok != tt.bare {
			t.Errorf("markdown LoadCommand(%q)=%t", tt.line, ok)
		}
		_, any := loadCommandToken(tt.line)
		if any != (tt.claude || tt.bare) {
			t.Errorf("loadCommandToken(%q)=%t", tt.line, any)
		}
	}
}

func TestFileFormatBehaviors(t *testing.T) {
	plain, mdc := fileFormatFor("AGENTS.md"), fileFormatFor("kander.mdc")
	if state, _ := plain.Inspect("---\nglobs: x\n---\n"); state != formatReady || plain.Prefix() != "" || plain.SymlinkRefusal() != "" || plain.OnlyKander("x") {
		t.Error("markdown accepts any text, adds nothing, allows symlinks and owns no content")
	}
	for _, tt := range []struct {
		text  string
		state formatState
		key   string
	}{
		{"# rules\n", formatMissingPrefix, "menu.mdc_rule_missing_frontmatter"},
		{"---\nalwaysApply: true\n---\n", formatReady, ""},
		{"---\nglobs: x\n---\n", formatForeign, "menu.mdc_rule_not_always_applied"},
	} {
		if state, key := mdc.Inspect(tt.text); state != tt.state || key != tt.key {
			t.Errorf("Inspect(%q)=%v,%q", tt.text, state, key)
		}
	}
	if mdc.Prefix() != alwaysApplyFrontmatter || mdc.SymlinkRefusal() == "" {
		t.Error("mdc adds alwaysApply frontmatter and refuses symlinks")
	}
	if !mdc.OnlyKander("---\nalwaysApply: true\n---") || mdc.OnlyKander("---\nalwaysApply: true\n---\n\nKeep me.") {
		t.Error("mdc owns only the frontmatter Kander wrote")
	}
}
