package board

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCacheRootAndEnsureCacheDir(t *testing.T) {
	root := t.TempDir()
	if got, want := CacheRoot(root), filepath.Join(root, ".kander", "caches"); got != want {
		t.Fatalf("CacheRoot=%s want %s", got, want)
	}
	path, err := EnsureCacheDir(root, "triage", "alice-repo-7")
	if err != nil {
		t.Fatalf("EnsureCacheDir: %v", err)
	}
	want := filepath.Join(root, ".kander", "caches", "triage", "alice-repo-7")
	if path != want {
		t.Fatalf("path=%s want %s", path, want)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("cache directory missing: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory mode=%o", info.Mode().Perm())
	}
}

func TestEnsureCacheDirRejectsEscapingParts(t *testing.T) {
	root := t.TempDir()
	for _, parts := range [][]string{
		{"..", "escape"},
		{"triage/../escape"},
		{"triage", ""},
		{"."},
		{"/absolute"},
		{`windows\escape`},
	} {
		if _, err := EnsureCacheDir(root, parts...); err == nil {
			t.Fatalf("parts %q must be refused", parts)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".kander")); err == nil {
		t.Fatal("a refused call must not create the cache root")
	}
}
