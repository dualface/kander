package install

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/dualface/kander/v2/internal/config"
)

// ErrNoGlobalEntry reports a host without a global kander: kander install has not run.
var ErrNoGlobalEntry = errors.New("no global kander entry")

// GlobalEntry returns the global entry: the kander users run from PATH, which a service
// definition starts. The first kander on PATH wins when it is the entry the installer copied or
// a Homebrew link; otherwise the copied entry, then a Homebrew link found elsewhere. A Homebrew
// entry is the link in the Homebrew bin directory, never its Cellar target, so the definition
// still names the current version after an upgrade. The caller refuses a Microsoft Store
// install before: its executable lives in a versioned package directory.
func GlobalEntry() (string, error) {
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		return "", err
	}
	direct := filepath.Join(paths.BinDir, binaryName())
	if first, err := firstPathBinary(os.Getenv("PATH"), pathNames()); err == nil && first != "" {
		if sameFile(first, direct) {
			return direct, nil
		}
		if resolved, err := filepath.EvalSymlinks(first); err == nil && isBrewManaged(resolved) {
			return first, nil
		}
	}
	if info, err := os.Lstat(direct); err == nil && info.Mode().IsRegular() {
		return direct, nil
	}
	if brew := uninstallBrewEntry(); brew != "" {
		return brew, nil
	}
	return "", errors.Join(ErrNoGlobalEntry, errors.New(config.Text("service.no_global_entry", direct)))
}

// RunningFromGlobalEntry reports whether the running binary is the global entry: both resolve
// to the same file, so a Homebrew bin link to the running Cellar binary counts. A Microsoft
// Store install always counts: its package executable is the only entry it has.
func RunningFromGlobalEntry() (bool, error) {
	if Packaged() {
		return true, nil
	}
	running, err := lookupExecutable()
	if err != nil {
		return false, err
	}
	entry, err := GlobalEntry()
	if errors.Is(err, ErrNoGlobalEntry) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return sameResolvedFile(running, entry), nil
}

// sameResolvedFile reports whether two paths name the same file after following links.
func sameResolvedFile(left, right string) bool {
	a, err := os.Stat(left)
	if err != nil {
		return false
	}
	b, err := os.Stat(right)
	return err == nil && os.SameFile(a, b)
}
