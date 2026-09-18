package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFakeKander(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, binaryName())
	if err := os.WriteFile(path, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPathKanderBinariesListsInPathOrder(t *testing.T) {
	old := pathKanderVersion
	pathKanderVersion = func(path string) string { return "1.0.0" }
	t.Cleanup(func() { pathKanderVersion = old })
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".EXE")
	}
	first := t.TempDir()
	second := t.TempDir()
	firstBin := writeFakeKander(t, first)
	secondBin := writeFakeKander(t, second)
	t.Setenv("PATH", first+string(os.PathListSeparator)+second)

	binaries := PathKanderBinaries()
	if len(binaries) != 2 {
		t.Fatalf("got %d binaries, want 2: %+v", len(binaries), binaries)
	}
	if binaries[0].Path != firstBin || binaries[1].Path != secondBin {
		t.Fatalf("order = %q, %q; want %q, %q", binaries[0].Path, binaries[1].Path, firstBin, secondBin)
	}
	for _, bin := range binaries {
		if bin.Version != "1.0.0" || bin.Brew {
			t.Fatalf("binary %+v want version 1.0.0 non-brew", bin)
		}
	}
}

func TestPathKanderBinariesDeduplicatesSameFile(t *testing.T) {
	old := pathKanderVersion
	pathKanderVersion = func(string) string { return "1.0.0" }
	t.Cleanup(func() { pathKanderVersion = old })
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".EXE")
	}
	dir := t.TempDir()
	writeFakeKander(t, dir)
	// The same directory twice must not yield two entries.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+dir)
	if binaries := PathKanderBinaries(); len(binaries) != 1 {
		t.Fatalf("got %d binaries, want 1", len(binaries))
	}
	// A symlinked alias to the same file counts once too.
	alias := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, binaryName()), filepath.Join(alias, binaryName())); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+alias)
	if binaries := PathKanderBinaries(); len(binaries) != 1 {
		t.Fatalf("symlinked duplicate gave %d binaries, want 1", len(binaries))
	}
}

func TestPathKanderBinariesFlagsHomebrew(t *testing.T) {
	old := pathKanderVersion
	pathKanderVersion = func(string) string { return "1.0.0" }
	t.Cleanup(func() { pathKanderVersion = old })
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew is not supported on Windows")
	}
	cellar := filepath.Join(t.TempDir(), "Cellar", "kander", "1.0.0", "bin")
	if err := os.MkdirAll(cellar, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeKander(t, cellar)
	binDir := t.TempDir()
	if err := os.Symlink(filepath.Join(cellar, "kander"), filepath.Join(binDir, "kander")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("PATH", binDir)
	binaries := PathKanderBinaries()
	if len(binaries) != 1 || !binaries[0].Brew {
		t.Fatalf("got %+v, want one brew-managed binary", binaries)
	}
}

func TestPathKanderBinariesSkipsNonExecutable(t *testing.T) {
	old := pathKanderVersion
	pathKanderVersion = func(string) string { return "1.0.0" }
	t.Cleanup(func() { pathKanderVersion = old })
	if runtime.GOOS == "windows" {
		t.Skip("exec-bit filtering does not apply on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "kander")
	if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if binaries := PathKanderBinaries(); len(binaries) != 0 {
		t.Fatalf("non-executable file gave %+v, want none", binaries)
	}
}

func TestRemoveKanderBinary(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeKander(t, dir)
	if err := RemoveKanderBinary(path); err != nil {
		t.Fatalf("RemoveKanderBinary: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists after removal: %v", err)
	}
	if err := RemoveKanderBinary(path); err == nil {
		t.Fatal("removing a missing file must report an error")
	}
}
