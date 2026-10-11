package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

// subdirRuleNames returns the embedded files below templates/ and snippets/.
func subdirRuleNames() []string {
	var names []string
	for _, name := range rules.Names() {
		if strings.Contains(name, "/") {
			names = append(names, name)
		}
	}
	return names
}

func readStateFiles(t *testing.T, rulesDir string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rulesDir, stateFileName))
	if err != nil {
		t.Fatal(err)
	}
	var state rulesState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state.Files
}

func TestPerformInstallsTemplatesAndSnippets(t *testing.T) {
	home := setupInstallHome(t)
	if _, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	rulesDir := filepath.Join(home, ".agents", "kander")
	files := readStateFiles(t, rulesDir)
	for _, name := range []string{"templates/README.md", "snippets/README.md"} {
		if _, err := os.Stat(filepath.Join(rulesDir, filepath.FromSlash(name))); err != nil {
			t.Fatalf("%s not installed: %v", name, err)
		}
		want, err := rules.Hash(name)
		if err != nil {
			t.Fatal(err)
		}
		if files[name] != want {
			t.Fatalf("state key %s = %q, want %q", name, files[name], want)
		}
	}
}

// An installation made by a release that shipped only top-level rule files has no templates/
// or snippets/ directory. Doctor reports those files missing and repair installs them; the old
// files keep their classification.
func TestInspectRulesReportsSubdirFilesMissingOnOldInstall(t *testing.T) {
	paths := config.InstallPaths{Mode: config.ModeGlobal, RulesDir: t.TempDir()}
	state := rulesState{Files: map[string]string{}}
	for _, name := range rules.Names() {
		if strings.Contains(name, "/") {
			continue
		}
		data, err := rules.File(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(paths.RulesDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		state.Files[name] = fileHash(data)
	}
	if err := saveRulesState(paths, state); err != nil {
		t.Fatal(err)
	}
	report, err := InspectRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.Missing, ",") != strings.Join(subdirRuleNames(), ",") {
		t.Fatalf("missing=%v want %v", report.Missing, subdirRuleNames())
	}
	if len(report.Outdated) != 0 || len(report.Modified) != 0 {
		t.Fatalf("old files reclassified: %+v", report)
	}
	repair, err := RepairRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(repair.Rewritten, ",") != strings.Join(subdirRuleNames(), ",") {
		t.Fatalf("rewritten=%v", repair.Rewritten)
	}
	after, err := InspectRules(paths)
	if err != nil || len(after.Missing)+len(after.Outdated)+len(after.Modified) != 0 {
		t.Fatalf("after repair: %+v err=%v", after, err)
	}
	files := readStateFiles(t, paths.RulesDir)
	for _, name := range subdirRuleNames() {
		if want, _ := rules.Hash(name); files[name] != want {
			t.Fatalf("state not stamped for %s", name)
		}
	}
}

func TestReplaceModifiedTemplateBacksUpAndConverges(t *testing.T) {
	setupInstallHome(t)
	if _, err := Perform(Request{CopyBinary: true, Language: "cn", Source: stubBinary(t)}); err != nil {
		t.Fatal(err)
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(paths.RulesDir, "templates", "README.md")
	if err := os.WriteFile(edited, []byte("local template\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := InspectRules(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Modified) != 1 || report.Modified[0] != "templates/README.md" {
		t.Fatalf("modified=%v", report.Modified)
	}
	// Repair never touches a locally modified file.
	if repair, err := RepairRules(paths); err != nil || len(repair.Rewritten) != 0 {
		t.Fatalf("repair touched the modified template: %+v err=%v", repair, err)
	}
	result, err := ReplaceModifiedRules(paths, report.Modified)
	if err != nil || len(result.Failed) != 0 || len(result.Replaced) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	backup, err := os.ReadFile(result.Replaced[0].Backup)
	if err != nil || string(backup) != "local template\n" {
		t.Fatalf("backup=%q err=%v", backup, err)
	}
	if filepath.Dir(result.Replaced[0].Backup) != filepath.Dir(edited) {
		t.Fatalf("backup must sit next to the file: %s", result.Replaced[0].Backup)
	}
	got, err := os.ReadFile(edited)
	want, _ := rules.File("templates/README.md")
	if err != nil || string(got) != string(want) {
		t.Fatalf("template not restored: %v", err)
	}
	after, err := InspectRules(paths)
	if err != nil || len(after.Modified) != 0 {
		t.Fatalf("after replace: %+v err=%v", after, err)
	}
}
