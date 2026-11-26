package menu

import (
	"fmt"
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
// between a checkout with an overlay and CI without one. The new directory is
// created under os.TempDir; when TMPDIR points inside a project, the
// directory would still resolve to that project's overlay, so it must be its
// own overlay root with no overlay file, or the run stops with an error that
// names the project root instead of failing assertions later. The returned
// function returns to the package directory and removes the temporary one.
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
	remove := func() { _ = os.RemoveAll(dir) }
	if err := requireNeutral(dir); err != nil {
		remove()
		return nil, err
	}
	if err := os.Chdir(dir); err != nil {
		remove()
		return nil, err
	}
	// Windows cannot remove the current directory, so leave it first.
	return func() {
		_ = os.Chdir(packageDir)
		remove()
	}, nil
}

// requireNeutral reports an error unless dir resolves to itself as the overlay
// project root and holds no overlay file.
func requireNeutral(dir string) error {
	location, err := config.ResolveOverlayLocation(dir)
	if err != nil {
		return err
	}
	if location.ProjectRoot != dir || location.Exists {
		return fmt.Errorf("temporary directory %s resolves to the project overlay root %s; set TMPDIR outside any Git worktree or Kander project", dir, location.ProjectRoot)
	}
	return nil
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
