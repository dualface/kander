package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// RemoveStartupState removes only the startup version file of earlier releases; a missing file is
// not an error.
func TestRemoveStartupState(t *testing.T) {
	paths := config.InstallPaths{RulesDir: t.TempDir()}
	state := filepath.Join(paths.RulesDir, startupStateFileName)
	other := filepath.Join(paths.RulesDir, "KANDER-AGENTS.md")
	for _, path := range []string{state, other} {
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveStartupState(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(state); !os.IsNotExist(err) {
		t.Fatalf("startup file stays: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("another file was removed: %v", err)
	}
	if err := RemoveStartupState(paths); err != nil {
		t.Fatalf("missing file: %v", err)
	}
}
