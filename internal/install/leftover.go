package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

func legacyNames() []string {
	if runtime.GOOS == "windows" {
		return []string{
			"onevoke", "onevoke.exe", "onevoke.cmd",
			"kanban", "kanban.exe", "kanban.cmd",
			"onevoke-review", "onevoke-review.exe", "onevoke-review.cmd",
			"onevoke-review.sh",
		}
	}
	return []string{"onevoke", "kanban", "onevoke-review.sh", "onevoke-review"}
}

func scanLegacy(binDir string) ([]string, error) {
	var found []string
	for _, name := range legacyNames() {
		target := filepath.Join(binDir, name)
		_, lerr := os.Lstat(target)
		if lerr != nil {
			if os.IsNotExist(lerr) {
				continue
			}
			return nil, lerr
		}
		if st, err := os.Stat(target); err == nil && st.IsDir() {
			return nil, fmt.Errorf("%s", config.Text("install.legacy_is_directory", target))
		}
		found = append(found, name)
	}
	return found, nil
}

func removeLegacy(binDir string, names []string) error {
	anchor, err := fileAnchor(binDir)
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, err := fs.RemoveNonDirectoryIfExists(anchor, filepath.Join(binDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func destIsExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

// CleanupStaleBinary removes leftover Windows rename-aside component copies.
func CleanupStaleBinary(paths config.InstallPaths) {
	if runtime.GOOS != "windows" {
		return
	}
	for _, name := range []string{binaryName(), componentBinaryName("kander-server")} {
		aside := filepath.Join(paths.BinDir, name+".old")
		anchor, err := fileAnchor(aside)
		if err != nil {
			continue
		}
		_, _ = fs.RemoveNonDirectoryIfExists(anchor, aside)
	}
}

// componentBinaryName also names obsolete binaries removed by uninstall.
func componentBinaryName(component string) string {
	if runtime.GOOS == "windows" {
		return component + ".exe"
	}
	return component
}
