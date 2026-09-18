package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func globalExtensionPaths(t *testing.T, home string) config.InstallPaths {
	t.Helper()
	return config.InstallPaths{Mode: config.ModeGlobal, RulesDir: filepath.Join(home, ".agents", "kander")}
}

func piExtensionTarget(t *testing.T, base string, project bool) string {
	t.Helper()
	spec, _, ok := config.AgentExtension("pi")
	if !ok {
		t.Fatal("pi extension spec missing")
	}
	rel := spec.Global
	if project {
		rel = spec.Project
	}
	return filepath.Join(base, filepath.FromSlash(rel))
}

func TestExtensionAgentsCoversOnlyExistingPiConfigDir(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	if got := ExtensionAgents(paths); len(got) != 0 {
		t.Fatalf("no ~/.pi/agent must cover nothing: %v", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ExtensionAgents(paths)
	if len(got) != 1 || got[0] != "pi" {
		t.Fatalf("coverage=%v", got)
	}
}

func TestExtensionAgentsCoversEveryDeclaringAgentOnProject(t *testing.T) {
	setupInstallHome(t)
	project := t.TempDir()
	paths := config.InstallPaths{
		Mode:        config.ModeProject,
		ProjectRoot: project,
		RulesDir:    filepath.Join(project, ".kander", "rules"),
	}
	got := ExtensionAgents(paths)
	if len(got) != 1 || got[0] != "pi" {
		t.Fatalf("coverage=%v", got)
	}
}

func TestEnsureExtensionGlobalWritesOnlyWhenPiDirExists(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	target := piExtensionTarget(t, home, false)
	// A missing ~/.pi/agent leaves the agent uncovered: the install-time loop
	// writes nothing, matching the rules-reference coverage.
	if got := integrateAgentExtensions(paths); len(got) != 0 {
		t.Fatalf("covered without ~/.pi/agent: %+v", got)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("extension written without ~/.pi/agent: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	outcome, err := EnsureExtension("pi", paths)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ExtensionWritten || outcome.Target != target {
		t.Fatalf("outcome=%+v", outcome)
	}
	_, embedded, _ := config.AgentExtension("pi")
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(embedded) {
		t.Fatalf("content mismatch: %v", err)
	}
	again, err := EnsureExtension("pi", paths)
	if err != nil || again.Status != ExtensionInstalled {
		t.Fatalf("second run: %+v %v", again, err)
	}
}

func TestEnsureExtensionProjectWritesUnderMainWorktree(t *testing.T) {
	setupInstallHome(t)
	project := t.TempDir()
	paths := config.InstallPaths{
		Mode:        config.ModeProject,
		ProjectRoot: project,
		RulesDir:    filepath.Join(project, ".kander", "rules"),
	}
	target := piExtensionTarget(t, project, true)
	outcome, err := EnsureExtension("pi", paths)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ExtensionWritten || outcome.Target != target {
		t.Fatalf("outcome=%+v", outcome)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("extension missing: %v", err)
	}
}

func TestInspectExtensionClassifiesStates(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := piExtensionTarget(t, home, false)
	report, err := InspectExtension("pi", paths)
	if err != nil || report.Status != ExtensionMissing {
		t.Fatalf("missing: %+v %v", report, err)
	}
	if _, err := EnsureExtension("pi", paths); err != nil {
		t.Fatal(err)
	}
	report, err = InspectExtension("pi", paths)
	if err != nil || report.Status != ExtensionInstalled {
		t.Fatalf("installed: %+v %v", report, err)
	}
	// A digest on the previous-release list classifies as outdated.
	old := []byte("// kander-rules v0\n")
	digest := fileHash(old)
	previousExtensionHashes = append(previousExtensionHashes, digest)
	defer func() {
		for i, v := range previousExtensionHashes {
			if v == digest {
				previousExtensionHashes = append(previousExtensionHashes[:i], previousExtensionHashes[i+1:]...)
				break
			}
		}
	}()
	if err := os.WriteFile(target, old, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = InspectExtension("pi", paths)
	if err != nil || report.Status != ExtensionOutdated {
		t.Fatalf("outdated: %+v %v", report, err)
	}
	// An unknown digest is a local edit: reported modified, never overwritten.
	foreign := []byte("// user edit\n")
	if err := os.WriteFile(target, foreign, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = InspectExtension("pi", paths)
	if err != nil || report.Status != ExtensionModified {
		t.Fatalf("modified: %+v %v", report, err)
	}
	outcome, err := EnsureExtension("pi", paths)
	if err != nil || outcome.Status != ExtensionModified {
		t.Fatalf("ensure on modified: %+v %v", outcome, err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(foreign) {
		t.Fatalf("modified file overwritten: %q", got)
	}
}

func TestEnsureExtensionRewritesOutdated(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := piExtensionTarget(t, home, false)
	old := []byte("// old release\n")
	digest := fileHash(old)
	previousExtensionHashes = append(previousExtensionHashes, digest)
	defer func() {
		for i, v := range previousExtensionHashes {
			if v == digest {
				previousExtensionHashes = append(previousExtensionHashes[:i], previousExtensionHashes[i+1:]...)
				break
			}
		}
	}()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, old, 0o644); err != nil {
		t.Fatal(err)
	}
	outcome, err := EnsureExtension("pi", paths)
	if err != nil || outcome.Status != ExtensionUpdated {
		t.Fatalf("outcome=%+v %v", outcome, err)
	}
	_, embedded, _ := config.AgentExtension("pi")
	got, _ := os.ReadFile(target)
	if string(got) != string(embedded) {
		t.Fatal("outdated file not rewritten")
	}
}

func TestEnsureExtensionRejectsReparsePoints(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := piExtensionTarget(t, home, false)
	extDir := filepath.Dir(target)
	// A symlinked extensions/ directory refuses the write.
	realDir := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, extDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := EnsureExtension("pi", paths); err == nil || !strings.Contains(err.Error(), extDir) {
		t.Fatalf("symlinked dir accepted: %v", err)
	}
	if _, err := InspectExtension("pi", paths); err == nil {
		t.Fatal("inspect through a symlinked dir must fail")
	}
	if err := os.Remove(extDir); err != nil {
		t.Fatal(err)
	}
	// A symlinked target file refuses as well.
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside.ts")
	if err := os.WriteFile(outside, []byte("// x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := EnsureExtension("pi", paths); err == nil {
		t.Fatal("symlinked file accepted")
	}
	got, _ := os.ReadFile(outside)
	if string(got) != "// x\n" {
		t.Fatalf("write escaped through the symlink: %q", got)
	}
}

func TestIntegrateAgentExtensionsIsIdempotent(t *testing.T) {
	home := setupInstallHome(t)
	paths := globalExtensionPaths(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := integrateAgentExtensions(paths)
	if len(first) != 1 || first[0].Err != nil || first[0].Status != ExtensionWritten {
		t.Fatalf("first=%+v", first)
	}
	second := integrateAgentExtensions(paths)
	if len(second) != 1 || second[0].Err != nil || second[0].Status != ExtensionInstalled {
		t.Fatalf("second=%+v", second)
	}
	target := piExtensionTarget(t, home, false)
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil || len(entries) != 1 {
		t.Fatalf("duplicate files: %v %v", entries, err)
	}
}
