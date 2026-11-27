package install

import (
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// fakeTree is a commit tree: a path maps to a regular file, or to a link target when it is in
// links.
type fakeTree struct {
	files map[string]string
	links map[string]string
}

func (f fakeTree) read(rel string) (TreeFile, error) {
	if target, ok := f.links[rel]; ok {
		return TreeFile{Data: []byte(target), Symlink: true, Found: true}, nil
	}
	if text, ok := f.files[rel]; ok {
		return TreeFile{Data: []byte(text), Found: true}, nil
	}
	return TreeFile{}, nil
}

func TestMissingProjectReferenceInTree(t *testing.T) {
	project, _ := projectFixture(t)
	block := referenceBlock("codex")
	for _, tt := range []struct {
		name  string
		agent string
		tree  fakeTree
		want  bool
	}{
		{"override without reference", "codex", fakeTree{files: map[string]string{"AGENTS.md": block, "AGENTS.override.md": "# Local\n"}}, false},
		{"override with reference", "codex", fakeTree{files: map[string]string{"AGENTS.md": block, "AGENTS.override.md": "# Local\n\n" + block}}, true},
		{"override linked to AGENTS.md", "codex", fakeTree{files: map[string]string{"AGENTS.md": block}, links: map[string]string{"AGENTS.override.md": "AGENTS.md"}}, true},
		{"override escaping the tree", "codex", fakeTree{files: map[string]string{"AGENTS.md": block}, links: map[string]string{"AGENTS.override.md": "../outside.md"}}, false},
		{"claude ignores the override", "claude", fakeTree{files: map[string]string{"CLAUDE.md": "@~/.agents/kander/KANDER-AGENTS.md\n", "AGENTS.override.md": "# Local\n"}}, true},
		{"claude import", "claude", fakeTree{files: map[string]string{"CLAUDE.md": "@~/.agents/kander/KANDER-AGENTS.md\n"}}, true},
		{"claude missing", "claude", fakeTree{files: map[string]string{"AGENTS.md": block}}, false},
		{"claude without reference", "claude", fakeTree{files: map[string]string{"CLAUDE.md": "# Mine\n"}}, false},
		{"claude imports AGENTS.md", "claude", fakeTree{files: map[string]string{"CLAUDE.md": "# Mine\n@AGENTS.md\n", "AGENTS.md": block}}, true},
		{"claude imports AGENTS.md without reference", "claude", fakeTree{files: map[string]string{"CLAUDE.md": "@AGENTS.md\n", "AGENTS.md": "# Mine\n"}}, false},
		{"claude import inside a comment", "claude", fakeTree{files: map[string]string{"CLAUDE.md": "<!-- @AGENTS.md -->\n", "AGENTS.md": block}}, false},
		{"claude import escaping the tree", "claude", fakeTree{files: map[string]string{"CLAUDE.md": "@../AGENTS.md\n"}}, false},
		{"claude linked to AGENTS.md", "claude", fakeTree{files: map[string]string{"AGENTS.md": "@~/.agents/kander/KANDER-AGENTS.md\n"}, links: map[string]string{"CLAUDE.md": "AGENTS.md"}}, true},
		{"link escaping the tree", "codex", fakeTree{links: map[string]string{"AGENTS.md": "../outside.md"}}, false},
		{"absolute link", "codex", fakeTree{links: map[string]string{"AGENTS.md": "/etc/AGENTS.md"}}, false},
		{"link loop", "codex", fakeTree{links: map[string]string{"AGENTS.md": "a.md", "a.md": "AGENTS.md"}}, false},
		{"markdown block", "codex", fakeTree{files: map[string]string{"AGENTS.md": "# Mine\n\n" + block}}, true},
		{"retired project install reference", "codex", fakeTree{files: map[string]string{"AGENTS.md": "@.kander/rules/KANDER-AGENTS.md\n"}}, false},
		{"markdown file does not follow imports", "codex", fakeTree{files: map[string]string{"AGENTS.md": "@docs/rules.md\n", "docs/rules.md": block}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			missing, err := MissingProjectReferenceInTree(tt.agent, project.Paths, project.Root, tt.tree.read)
			if err != nil || (missing == "") != tt.want {
				t.Fatalf("missing=%q err=%v want present=%v", missing, err, tt.want)
			}
			if !tt.want && strings.Contains(tt.name, "override") && missing != "AGENTS.override.md" {
				t.Fatalf("missing=%q, want the override", missing)
			}
		})
	}
}

func TestProjectRulesApplicable(t *testing.T) {
	project, _ := projectFixture(t)
	if rel, ok := ProjectRulesApplicable("claude", project.Root, config.InstallRulesOf(nil)); !ok || rel != "CLAUDE.md" {
		t.Fatalf("claude rel=%q ok=%v", rel, ok)
	}
	if rel, ok := ProjectRulesApplicable("wrapper", project.Root, config.InstallRulesOf(nil)); ok || rel != "" {
		t.Fatalf("custom agent rel=%q ok=%v", rel, ok)
	}
	cfg := config.DefaultConfig()
	off := false
	config.SetAgentInstallRules(cfg, "pi", &off)
	if rel, ok := ProjectRulesApplicable("codex", project.Root, config.InstallRulesOf(cfg)); ok || rel != "AGENTS.md" {
		t.Fatalf("shared file with a reader off rel=%q ok=%v", rel, ok)
	}
}
