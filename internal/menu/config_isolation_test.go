package menu

import (
	"os"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// isolateOverlay keeps tests that load configuration away from an inherited
// project overlay. During go test the working directory is the package
// directory, whose overlay resolves to the main worktree's .kander-config.json;
// config.Load merges it while SaveIfUnchanged compares against the scope file
// only, so a developer's own overlay would surface as a concurrent edit. A
// working directory that is itself the overlay's project root is a fixture the
// test created and is kept. t.Chdir restores the package directory afterwards.
func isolateOverlay(t *testing.T) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	location, err := config.ResolveOverlayLocation(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if location.ProjectRoot != cwd {
		t.Chdir(t.TempDir())
	}
}
