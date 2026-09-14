package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dualface/kander/internal/config"
)

func resolveSource(source string, copying bool) (string, error) {
	explicit := source != ""
	if !explicit {
		var err error
		source, err = lookupExecutable()
		if err != nil {
			return "", fmt.Errorf("%s", config.Text("install.cannot_resolve_executable"))
		}
		// Resolve package-manager links only for the running POSIX binary.
		// Copying still uses the no-follow reads in internal/fs.
		if runtime.GOOS != "windows" {
			source, err = filepath.EvalSymlinks(source)
			if err != nil {
				return "", err
			}
		}
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	if copying || explicit {
		if err := rejectSource(source); err != nil {
			return "", err
		}
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s", config.Text("install.source_not_regular", source))
	}
	return source, nil
}
