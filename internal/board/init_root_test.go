package board

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestPlannedInitRootMatchesInitBoard(t *testing.T) {
	resetLang(t)
	project := t.TempDir()
	t.Setenv(EnvBoardDir, "")
	rulesDir := filepath.Join(t.TempDir(), "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "KANDER-KANBAN-RULES.md"), []byte("# rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installPathsFn = func() (config.InstallPaths, error) {
		return config.InstallPaths{Mode: config.ModeGlobal, RulesDir: rulesDir}, nil
	}
	t.Cleanup(func() { installPathsFn = config.CurrentInstallPaths })

	planned, err := PlannedInitRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	if planned != filepath.Join(project, "kanban") {
		t.Fatalf("planned=%s", planned)
	}
	root, _, _, err := InitBoard(project)
	if err != nil {
		t.Fatal(err)
	}
	if root != planned {
		t.Fatalf("init=%s planned=%s", root, planned)
	}

	cwd := t.TempDir()
	t.Chdir(cwd)
	planned, err = PlannedInitRoot("")
	if err != nil {
		t.Fatal(err)
	}
	if planned != filepath.Join(cwd, "kanban") {
		t.Fatalf("cwd planned=%s", planned)
	}
}
