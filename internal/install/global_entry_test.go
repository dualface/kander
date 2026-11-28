package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func setRunningExecutable(t *testing.T, path string) {
	t.Helper()
	old := lookupExecutable
	lookupExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { lookupExecutable = old })
}

// The running binary is the global entry when both resolve to one file: the copied entry, or a
// Cellar binary that the Homebrew bin link on PATH points at. Another binary is not, and a host
// without a global entry has none to be.
func TestRunningFromGlobalEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew links and executable bits are POSIX")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	oldBrew := uninstallBrewEntry
	uninstallBrewEntry = func() string { return "" }
	t.Cleanup(func() { uninstallBrewEntry = oldBrew })

	prefix := t.TempDir()
	cellarDir := filepath.Join(prefix, "Cellar", "kander", "2.0.0", "bin")
	if err := os.MkdirAll(cellarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cellar := writeFakeKander(t, cellarDir)
	brewBin := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(brewBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cellar, filepath.Join(brewBin, "kander")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", brewBin)
	setRunningExecutable(t, cellar)
	if global, err := RunningFromGlobalEntry(); err != nil || !global {
		t.Fatalf("Homebrew Cellar binary: %v %v", global, err)
	}

	dev := writeFakeKander(t, t.TempDir())
	setRunningExecutable(t, dev)
	if global, err := RunningFromGlobalEntry(); err != nil || global {
		t.Fatalf("development build: %v %v", global, err)
	}

	direct := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(direct, 0o755); err != nil {
		t.Fatal(err)
	}
	copied := writeFakeKander(t, direct)
	t.Setenv("PATH", direct)
	setRunningExecutable(t, copied)
	if global, err := RunningFromGlobalEntry(); err != nil || !global {
		t.Fatalf("copied entry: %v %v", global, err)
	}

	if err := os.Remove(copied); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	setRunningExecutable(t, dev)
	if global, err := RunningFromGlobalEntry(); err != nil || global {
		t.Fatalf("no global entry: %v %v", global, err)
	}
}
