package install

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
)

// PathKanderBinary is one distinct kander executable reachable through PATH.
type PathKanderBinary struct {
	Path     string // absolute path as found in a PATH directory
	Resolved string // symlink-resolved path when known
	Version  string // version reported by `kander version`, "" when probing failed
	Brew     bool   // managed by Homebrew (resolves into a Cellar path)
}

// pathKanderVersion runs `kander version` for one candidate and returns the raw version
// string; tests replace it to avoid executing binaries.
var pathKanderVersion = probeKanderVersion

// PathKanderBinaries lists every distinct kander executable reachable through PATH,
// deduplicated by file identity, in PATH precedence order. Version probing failures
// leave Version empty instead of dropping the entry.
func PathKanderBinaries() []PathKanderBinary {
	var binaries []PathKanderBinary
	seen := make([]os.FileInfo, 0, 8)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			if runtime.GOOS == "windows" {
				continue
			}
			dir = "."
		}
		for _, name := range pathNames() {
			candidate, err := filepath.Abs(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
				continue
			}
			duplicate := false
			for _, kept := range seen {
				if os.SameFile(info, kept) {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			seen = append(seen, info)
			resolved, resErr := filepath.EvalSymlinks(candidate)
			if resErr != nil {
				resolved = candidate
			}
			binaries = append(binaries, PathKanderBinary{
				Path:     candidate,
				Resolved: resolved,
				Version:  pathKanderVersion(candidate),
				Brew:     isBrewManaged(resolved),
			})
		}
	}
	return binaries
}

// isBrewManaged reports whether the resolved binary lives in a Homebrew Cellar,
// which covers cellar symlinks and direct cellar paths on macOS and Linux.
func isBrewManaged(resolved string) bool {
	return strings.Contains(filepath.ToSlash(resolved), "/Cellar/")
}

// probeKanderVersion runs `kander version` with a short deadline and returns the
// reported version, e.g. "1.2.3"; empty when the binary cannot report one.
func probeKanderVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return strings.TrimSpace(strings.TrimPrefix(line, "kander"))
}

// RemoveKanderBinary deletes one non-directory executable through the anchored fs layer.
// Deleting the running binary fails on Windows; the caller reports the error.
func RemoveKanderBinary(path string) error {
	anchor, err := fileAnchor(path)
	if err != nil {
		return err
	}
	removed, err := fs.RemoveNonDirectoryIfExists(anchor, path)
	if err != nil {
		return err
	}
	if !removed {
		return os.ErrNotExist
	}
	return nil
}
