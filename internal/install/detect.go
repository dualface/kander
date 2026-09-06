// Package install copies the running kander binary, extracts embedded rules, and runs the first-run wizard.
package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// EnvSkipInstall disables the first-run wizard, including on a developer machine whose config was deleted.
const EnvSkipInstall = "KANDER_SKIP_INSTALL"

var lookupExecutable = os.Executable

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "kander.exe"
	}
	return "kander"
}

func asideName() string {
	return binaryName() + ".old"
}

func fileAnchor(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(abs)
		if vol == "" {
			return "", fmt.Errorf("%s", config.Text("config.invalid_install_path", path))
		}
		return vol + `\`, nil
	}
	return string(os.PathSeparator), nil
}

func sameFile(left, right string) bool {
	a, err := filepath.Abs(left)
	if err != nil {
		return false
	}
	b, err := filepath.Abs(right)
	if err != nil {
		return false
	}
	if a == b {
		return true
	}
	if runtime.GOOS == "windows" && equalFoldPath(a, b) {
		return true
	}
	sa, errA := os.Lstat(a)
	sb, errB := os.Lstat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(sa, sb)
}

func equalFoldPath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right) ||
		(runtime.GOOS == "windows" && len(left) == len(right) && equalFoldASCII(left, right))
}

func equalFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := 0; i < len(left); i++ {
		a, b := left[i], right[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func looksLikeModule(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	line, _, _ := splitFirstLine(data)
	return line == "module github.com/dualface/kander"
}

func splitFirstLine(data []byte) (string, []byte, bool) {
	for i, b := range data {
		if b == '\n' {
			line := string(data[:i])
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			return line, data[i+1:], true
		}
	}
	return string(data), nil, false
}

func inSourceTree() bool {
	exe, err := lookupExecutable()
	if err != nil {
		return false
	}
	dir := filepath.Dir(exe)
	for range 4 {
		if looksLikeModule(dir) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

func alreadyInstalled() bool {
	exe, err := lookupExecutable()
	if err != nil {
		return false
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return false
	}
	dests := make([]string, 0, 2)
	if global, err := config.GlobalInstallPaths(); err == nil {
		dests = append(dests, filepath.Join(global.BinDir, binaryName()))
	}
	if current, err := config.CurrentInstallPaths(); err == nil {
		dests = append(dests, filepath.Join(current.BinDir, binaryName()))
	}
	for _, dest := range dests {
		if sameFile(exe, dest) {
			return true
		}
	}
	return false
}

// ShouldRunWizard reports whether a bare kander should open the first-run installer.
func ShouldRunWizard() (bool, error) {
	if os.Getenv(EnvSkipInstall) != "" {
		return false, nil
	}
	exists, err := config.Exists()
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if inSourceTree() {
		return false, nil
	}
	if alreadyInstalled() {
		return false, nil
	}
	return true, nil
}

func rejectDest(path string, project bool) error {
	_, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if st, statErr := os.Stat(path); statErr == nil && st.IsDir() {
		return fmt.Errorf("%s", config.Text("install.target_is_directory", path))
	}
	if project && fs.IsReparsePoint(path) {
		return fmt.Errorf("%s", config.Text("install.target_is_symlink", path))
	}
	return nil
}

func rejectSource(path string) error {
	if fs.IsReparsePoint(path) {
		return fmt.Errorf("%s", config.Text("install.source_is_symlink", path))
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s", config.Text("install.source_is_directory", path))
	}
	return nil
}
