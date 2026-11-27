package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

func TestClassifyMDCFrontmatter(t *testing.T) {
	for _, tt := range []struct {
		text string
		want mdcFrontmatter
	}{
		{"", mdcNoFrontmatter},
		{"# rules\n", mdcNoFrontmatter},
		{"---\nalwaysApply: true\n---\nbody\n", mdcAlwaysApply},
		{"---\r\ndescription: x\r\nalwaysApply: true\r\n---\r\n", mdcAlwaysApply},
		{"---\nalwaysApply: false\n---\n", mdcOtherFrontmatter},
		{"---\nglobs: '*.go'\n---\n", mdcOtherFrontmatter},
		{"---\nalwaysApply: true\nnever closed\n", mdcOtherFrontmatter},
	} {
		if got := classifyMDCFrontmatter(tt.text); got != tt.want {
			t.Errorf("classify(%q)=%v want %v", tt.text, got, tt.want)
		}
	}
}

func cursorRuleSetup(t *testing.T) (string, string) {
	t.Helper()
	home := setupInstallHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, filepath.Join(home, ".cursor", "rules", "kander.mdc")
}

// The cleanup deletes a kander.mdc that holds only what Kander wrote, and keeps one the user
// changed, without its Kander load command.
func TestCursorRuleCleanup(t *testing.T) {
	block := "## Kander Rules Entry\n\nAt the start of every session, read `~/.agents/kander/KANDER-AGENTS.md` and follow it as the Kander workflow rules entry.\n"
	for _, tt := range []struct {
		name, text, want string
		removed          bool
	}{
		{"unmodified", alwaysApplyFrontmatter + block, "", true},
		{"modified", alwaysApplyFrontmatter + "# my notes\n\n" + block, alwaysApplyFrontmatter + "# my notes\n", false},
		{"user frontmatter", "---\nalwaysApply: false\n---\n\n" + block, "---\nalwaysApply: false\n---\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home, target := cursorRuleSetup(t)
			paths := globalIntegrationPaths(t, home)
			writeIntegrateFile(t, target, tt.text)
			result := CleanGlobalInjection(paths)
			if result.Err != nil || len(result.Removed) != 1 {
				t.Fatalf("cleanup=%+v", result)
			}
			if tt.removed {
				if _, err := os.Lstat(target); !os.IsNotExist(err) || len(result.Kept) != 0 {
					t.Fatalf("unmodified rule kept: %v %+v", err, result.Kept)
				}
				return
			}
			if got, _ := os.ReadFile(target); string(got) != tt.want {
				t.Fatalf("rule=%q want %q", got, tt.want)
			}
			if len(result.Kept) != 1 || result.Kept[0].Reason != config.Text("install.global_rules_file_modified_kept") {
				t.Fatalf("kept=%+v", result.Kept)
			}
		})
	}
}

func TestCursorRuleSymlinkIsNeverFollowed(t *testing.T) {
	home, target := cursorRuleSetup(t)
	paths := globalIntegrationPaths(t, home)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "rules.md")
	entry := RulesEntry(paths)
	writeIntegrateFile(t, linked, entry+"\n")
	if err := os.Symlink(linked, target); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	result := CleanGlobalInjection(paths)
	if result.Err != nil || len(result.Removed) != 0 || len(result.Kept) != 1 || !strings.Contains(result.Kept[0].Reason, target) {
		t.Fatalf("cleanup=%+v", result)
	}
	if data, _ := os.ReadFile(linked); string(data) != entry+"\n" {
		t.Fatalf("link target changed: %q", data)
	}
}
