package menu

import (
	"os"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// packageDir is the package directory go test started in, recorded by
// enterNeutralDir before it leaves; tests that read repository files build
// their paths from it.
var packageDir string

// enterNeutralDir moves the test process out of the package directory before
// any test runs, and is called by both TestMain variants. During go test the
// working directory is the package directory, whose project overlay resolves
// to the main worktree's .kander-config.json (through git, or by walking up
// when a test hides git from PATH). config.Load would merge a developer's own
// overlay over the configuration a test writes, so results would differ
// between a checkout with an overlay and CI without one. A fresh temporary
// directory has no overlay on either lookup path. The returned function
// removes that directory.
func enterNeutralDir() (func(), error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	packageDir = cwd
	dir, err := os.MkdirTemp("", "kander-menu-cwd")
	if err != nil {
		return nil, err
	}
	if err := os.Chdir(dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return func() { _ = os.RemoveAll(dir) }, nil
}

// isolateOverlay keeps a test that loads configuration away from an inherited
// project overlay. TestMain already starts every test in a neutral directory
// (enterNeutralDir); this guard also covers a test that changed directory
// before calling it. config.Load merges the overlay while SaveIfUnchanged
// compares against the scope file only, so an inherited overlay would surface
// as a concurrent edit. A working directory that is itself the overlay's
// project root is a fixture the test created and is kept. t.Chdir restores the
// previous directory afterwards.
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
