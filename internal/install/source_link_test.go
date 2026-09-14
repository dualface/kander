package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPerformResolvesRunningBinaryLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows installation sources must not traverse reparse points")
	}
	for _, entry := range []string{"bin", "opt"} {
		t.Run(entry, func(t *testing.T) {
			setupInstallHome(t)
			prefix := t.TempDir()
			keg := filepath.Join(prefix, "Cellar", "kander", "0.6.0")
			for _, dir := range []string{filepath.Join(keg, "bin"), filepath.Join(prefix, "bin"), filepath.Join(prefix, "opt")} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			original := []byte("running package binary\n")
			source := filepath.Join(keg, "bin", binaryName())
			if err := os.WriteFile(source, original, 0o755); err != nil {
				t.Fatal(err)
			}
			links := map[string]string{
				filepath.Join(prefix, "bin", binaryName()): filepath.Join("..", "opt", "kander", "bin", binaryName()),
				filepath.Join(prefix, "opt", "kander"):     filepath.Join("..", "Cellar", "kander", "0.6.0"),
			}
			for link, target := range links {
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
			}
			executable := filepath.Join(prefix, "bin", binaryName())
			if entry == "opt" {
				executable = filepath.Join(prefix, "opt", "kander", "bin", binaryName())
			}
			previous := lookupExecutable
			lookupExecutable = func() (string, error) { return executable, nil }
			t.Cleanup(func() { lookupExecutable = previous })
			result, err := Perform(Request{CopyBinary: true, Language: "en"})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Copied {
				t.Fatal("package binary was not copied")
			}
			for _, path := range []string{source, result.DestBinary} {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != string(original) {
					t.Fatalf("binary %s: %q, %v", path, got, err)
				}
			}
			for link, target := range links {
				got, err := os.Readlink(link)
				if err != nil || got != target {
					t.Fatalf("package link %s changed: %q, %v", link, got, err)
				}
			}
		})
	}
}

func TestPerformRejectsInvalidRunningBinaryLinks(t *testing.T) {
	for _, target := range []string{"missing", "loop", "directory"} {
		t.Run(target, func(t *testing.T) {
			home := setupInstallHome(t)
			dir := t.TempDir()
			link := filepath.Join(dir, "loop")
			if err := os.Mkdir(filepath.Join(dir, "directory"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			previous := lookupExecutable
			lookupExecutable = func() (string, error) { return link, nil }
			t.Cleanup(func() { lookupExecutable = previous })
			if _, err := Perform(Request{CopyBinary: true, Language: "en"}); err == nil {
				t.Fatal("invalid executable source was accepted")
			}
			if _, err := os.Lstat(filepath.Join(home, ".local")); !os.IsNotExist(err) {
				t.Fatalf("installation wrote output for an invalid source: %v", err)
			}
		})
	}
}
