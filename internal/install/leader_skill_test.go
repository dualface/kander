package install

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/skills"
)

func assertSkill(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"SKILL.md", "references/handover.md", "references/deployment.md", "references/server-mode.md"} {
		want, err := skills.Leader.ReadFile(leaderName + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("skill %s/%s: does not match bundle: %v", dir, name, err)
		}
	}
}

func TestInstallLeaderSkillLifecycle(t *testing.T) {
	for _, run := range []string{"global"} {
		t.Run(run, func(t *testing.T) {
			home := setupInstallHome(t)
			req := Request{Language: "en", Source: stubBinary(t)}
			root := home
			if err := os.Mkdir(filepath.Join(root, ".claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			result, err := Perform(req)
			if err != nil {
				t.Fatal(err)
			}
			shared := filepath.Join(root, ".agents", "skills", leaderName)
			claude := filepath.Join(root, ".claude", "skills", leaderName)
			assertSkill(t, shared)
			assertSkill(t, claude)
			// Model the previous installation's receipt and an unrepaired missing reference.
			old := []byte("previous distributed version")
			entry := filepath.Join(shared, "SKILL.md")
			writeIntegrateFile(t, entry, string(old))
			s, err := inspectLeaderSkill(shared, result.Paths)
			if err != nil {
				t.Fatal(err)
			}
			s.receipt.Files["SKILL.md"] = fileHash(old)
			record, err := json.Marshal(s.receipt)
			if err != nil {
				t.Fatal(err)
			}
			writeIntegrateFile(t, filepath.Join(shared, leaderReceipt), string(record))
			if err := os.Remove(filepath.Join(shared, "references", "handover.md")); err != nil {
				t.Fatal(err)
			}
			custom := filepath.Join(claude, "SKILL.md")
			writeIntegrateFile(t, custom, "my leader")
			if _, err = Perform(req); err != nil {
				t.Fatal(err)
			}
			assertSkill(t, shared)
			if got := readBytes(t, custom); got != "my leader" {
				t.Fatalf("customized skill overwritten: %q", got)
			}
			if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), result.Paths), io.Discard); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(shared); !os.IsNotExist(err) {
				t.Fatalf("clean skill remains: %v", err)
			}
			if readBytes(t, custom) != "my leader" || readBytes(t, filepath.Join(claude, "references", "handover.md")) == "" {
				t.Fatal("uninstall damaged customized skill")
			}
			req.RulesChoice = map[string]bool{"codex": true}
			if _, err := Perform(req); err != nil {
				t.Fatal(err)
			}
			assertSkill(t, shared)
		})
	}
}

func TestLeaderSkillPreservesManualDirectoryAndClaudeLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory symlinks require privileges")
	}
	home := setupInstallHome(t)
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
	result, err := Perform(Request{Language: "en", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), result.Paths), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(link); err != nil || readBytes(t, entry) != "my own" {
		t.Fatalf("unmanaged skill or link changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(shared, leaderReceipt)); !os.IsNotExist(err) {
		t.Fatalf("unmanaged skill was adopted: %v", err)
	}
}

func TestLeaderSkillRespectsSelection(t *testing.T) {
	home := setupInstallHome(t)
	req := Request{Language: "en", Source: stubBinary(t), RulesChoice: map[string]bool{"codex": false, "claude": true}}
	root := filepath.Join(home, "custom-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(req); err != nil {
		t.Fatal(err)
	}
	assertSkill(t, filepath.Join(root, "skills", leaderName))
	if _, err := os.Lstat(filepath.Join(home, ".agents", "skills", leaderName)); !os.IsNotExist(err) {
		t.Fatalf("disabled skill was written: %v", err)
	}
}

func TestLeaderUninstallRechecksPreview(t *testing.T) {
	home := setupInstallHome(t)
	result, err := Perform(Request{Language: "en", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agents", "skills", leaderName)
	plan := PlanUninstall(context.Background(), result.Paths)
	writeIntegrateFile(t, filepath.Join(dir, "SKILL.md"), "changed at confirmation")
	if err := ApplyUninstall(context.Background(), plan, io.Discard); err == nil {
		t.Fatal("expected stale preview error")
	}
	if readBytes(t, filepath.Join(dir, "references", "deployment.md")) == "" {
		t.Fatal("removed reference before checking for edits")
	}
}

func TestLeaderUninstallKeepsExtraFiles(t *testing.T) {
	home := setupInstallHome(t)
	result, err := Perform(Request{Language: "en", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agents", "skills", leaderName)
	extra := filepath.Join(dir, "references", "personal.md")
	writeIntegrateFile(t, extra, "personal notes")
	if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), result.Paths), io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := readBytes(t, extra); got != "personal notes" {
		t.Fatalf("uninstall changed extra file: %q", got)
	}
	for _, name := range append([]string{leaderReceipt}, leaderFiles...) {
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("managed file %s remains: %v", name, err)
		}
	}
}

func TestLeaderUninstallSharedAgentConfigRoot(t *testing.T) {
	home := setupInstallHome(t)
	root := filepath.Join(home, ".agents")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	result, err := Perform(Request{Language: "en", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "skills", leaderName)
	assertSkill(t, dir)
	if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), result.Paths), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("shared skill remains: %v", err)
	}
}

func TestLeaderUninstallPreservesCopiedReceipt(t *testing.T) {
	home := setupInstallHome(t)
	req := Request{Language: "en", Source: stubBinary(t)}
	result, err := Perform(req)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(home, ".agents", "skills", leaderName)
	dst := filepath.Join(home, ".claude", "skills", leaderName)
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(req); err != nil {
		t.Fatal(err)
	}
	if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), result.Paths), io.Discard); err != nil {
		t.Fatal(err)
	}
	assertSkill(t, dst)
	if readBytes(t, filepath.Join(dst, leaderReceipt)) == "" {
		t.Fatal("user's copied receipt removed")
	}
}

func TestLeaderSkillUsesConfirmedTargets(t *testing.T) {
	home := setupInstallHome(t)
	root := filepath.Join(home, ".agents")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	req := Request{Language: "en", Source: stubBinary(t)}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	req.leaderTargets = leaderDestinations(paths)
	groups := planRulesInjection(paths, req.leaderTargets)
	switches, err := config.ScopeInstallRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	req.RulesChoice = rulesChoice(groups, nil, switches)
	// The Claude root appears after preview, before execution. The wizard
	// did not list a Claude destination, and the user deselected Codex.
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(req); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "skills", leaderName)); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed skill target written: %v", err)
	}
}

func TestLeaderSkillSkipsLinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory symlinks require privileges")
	}
	home := setupInstallHome(t)
	dotfiles := t.TempDir()
	entry := filepath.Join(dotfiles, leaderName, "SKILL.md")
	writeIntegrateFile(t, entry, "personal skill")
	root := filepath.Join(home, ".claude")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	result, err := Perform(Request{Language: "en", Source: stubBinary(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.SkippedSkills) != 1 || !strings.Contains(result.SkippedSkills[0].Error(), filepath.Join(root, "skills")) {
		t.Fatalf("missing linked path diagnostic: %v", result.SkippedSkills)
	}
	assertSkill(t, filepath.Join(home, ".agents", "skills", leaderName))
	if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), result.Paths), io.Discard); err != nil {
		t.Fatal(err)
	}
	if readBytes(t, entry) != "personal skill" {
		t.Fatal("modified skill through linked parent")
	}
}

func TestLeaderSkillConfigRootCaseChange(t *testing.T) {
	home := setupInstallHome(t)
	root := filepath.Join(home, "ClaudeRoot")
	variant := filepath.Join(home, "clauderoot")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	req := Request{Language: "en", Source: stubBinary(t)}
	result, err := Perform(req)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "skills", leaderName)
	other := filepath.Join(variant, "skills", leaderName)
	t.Setenv("CLAUDE_CONFIG_DIR", variant)
	if _, err := os.Stat(variant); os.IsNotExist(err) {
		// Independent directories on a case-sensitive volume can hold
		// identical copies, including the receipt.
		if err := os.CopyFS(other, os.DirFS(dir)); err != nil {
			t.Fatal(err)
		}
		if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), result.Paths), io.Discard); err != nil {
			t.Fatal(err)
		}
		assertSkill(t, other)
		return
	} else if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "references", "deployment.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Perform(req); err != nil {
		t.Fatal(err)
	}
	assertSkill(t, other)
	if err := ApplyUninstall(context.Background(), PlanUninstall(context.Background(), result.Paths), io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("same skill remains after uninstall using alternate case: %v", err)
	}
}
