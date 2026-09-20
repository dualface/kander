package install

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

// installRules writes the embedded rule set into the temp-home rules directory and returns
// the resolved install paths.
func installRules(t *testing.T) (string, config.InstallPaths) {
	t.Helper()
	home := setupInstallHome(t)
	if _, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	return home, paths
}

func modifiedRule(t *testing.T, paths config.InstallPaths, name, content string) string {
	t.Helper()
	path := filepath.Join(paths.RulesDir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := InspectRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range report.Modified {
		if item == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s not reported modified: %+v", name, report)
	}
	return path
}

func TestReplaceModifiedRulesBacksUpAndConverges(t *testing.T) {
	_, paths := installRules(t)
	edited := modifiedRule(t, paths, "KANDER-CODE-RULES.md", "edited locally\n")

	result, err := ReplaceModifiedRules(paths, []string{"KANDER-CODE-RULES.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("failed=%+v", result.Failed)
	}
	if len(result.Replaced) != 1 || result.Replaced[0].Name != "KANDER-CODE-RULES.md" {
		t.Fatalf("replaced=%+v", result.Replaced)
	}
	backup := result.Replaced[0].Backup
	if !strings.HasPrefix(filepath.Base(backup), "KANDER-CODE-RULES.md.backup-") {
		t.Fatalf("unexpected backup name %q", backup)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "edited locally\n" {
		t.Fatalf("backup content %q does not match the pre-replacement file", data)
	}
	want, err := rules.File("KANDER-CODE-RULES.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(edited)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("modified file was not replaced with the embedded copy")
	}
	report, err := InspectRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Missing)+len(report.Outdated)+len(report.Modified) != 0 {
		t.Fatalf("rules not converged after replace: %+v", report)
	}
}

func TestReplaceModifiedRulesBacksUpWithoutOverwriting(t *testing.T) {
	_, paths := installRules(t)
	modifiedRule(t, paths, "KANDER-CODE-RULES.md", "first edit\n")
	first, err := ReplaceModifiedRules(paths, []string{"KANDER-CODE-RULES.md"})
	if err != nil {
		t.Fatal(err)
	}
	modifiedRule(t, paths, "KANDER-CODE-RULES.md", "second edit\n")
	second, err := ReplaceModifiedRules(paths, []string{"KANDER-CODE-RULES.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Replaced) != 1 || len(second.Replaced) != 1 {
		t.Fatalf("replaced=%+v %+v", first.Replaced, second.Replaced)
	}
	older, newer := first.Replaced[0].Backup, second.Replaced[0].Backup
	if older == newer {
		t.Fatalf("second backup reused %q", older)
	}
	data, err := os.ReadFile(older)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first edit\n" {
		t.Fatalf("earlier backup was overwritten: %q", data)
	}
	data, err = os.ReadFile(newer)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second edit\n" {
		t.Fatalf("new backup content %q", data)
	}
}

func TestReplaceModifiedRulesKeepsFailingFileUnconverged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink requires privileges on Windows")
	}
	_, paths := installRules(t)
	modifiedRule(t, paths, "KANDER-CODE-RULES.md", "edited locally\n")
	// A global-mode symlinked rule file stays user-managed: the inspection classifies it
	// modified, and the replace reports a per-file failure without touching it.
	linkTarget := filepath.Join(t.TempDir(), "managed-rules.md")
	if err := os.WriteFile(linkTarget, []byte("user managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(paths.RulesDir, "KANDER-BASE-RULES.md")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkTarget, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	report, err := InspectRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	foundLink := false
	for _, name := range report.Modified {
		if name == "KANDER-BASE-RULES.md" {
			foundLink = true
		}
	}
	if !foundLink {
		t.Fatalf("linked file not reported modified: %+v", report)
	}

	result, err := ReplaceModifiedRules(paths, []string{"KANDER-CODE-RULES.md", "KANDER-BASE-RULES.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Replaced) != 1 || result.Replaced[0].Name != "KANDER-CODE-RULES.md" {
		t.Fatalf("replaced=%+v", result.Replaced)
	}
	if len(result.Failed) != 1 || result.Failed[0].Name != "KANDER-BASE-RULES.md" {
		t.Fatalf("failed=%+v", result.Failed)
	}
	if result.Failed[0].Err == nil {
		t.Fatal("failure carries no reason")
	}
	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "user managed\n" {
		t.Fatalf("link target changed: %q", data)
	}
	report, err = InspectRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	stillModified := false
	for _, name := range report.Modified {
		if name == "KANDER-BASE-RULES.md" {
			stillModified = true
		}
		if name == "KANDER-CODE-RULES.md" {
			t.Fatalf("replaced file still modified: %+v", report)
		}
	}
	if !stillModified {
		t.Fatalf("failed file wrongly converged: %+v", report)
	}
}

func TestReplaceModifiedRulesReportsMissingFile(t *testing.T) {
	_, paths := installRules(t)
	modifiedRule(t, paths, "KANDER-CODE-RULES.md", "edited locally\n")
	if err := os.Remove(filepath.Join(paths.RulesDir, "KANDER-CODE-RULES.md")); err != nil {
		t.Fatal(err)
	}
	result, err := ReplaceModifiedRules(paths, []string{"KANDER-CODE-RULES.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Replaced) != 0 {
		t.Fatalf("replaced=%+v", result.Replaced)
	}
	if len(result.Failed) != 1 || result.Failed[0].Name != "KANDER-CODE-RULES.md" {
		t.Fatalf("failed=%+v", result.Failed)
	}
}
