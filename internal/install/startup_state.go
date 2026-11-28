package install

import (
	"path/filepath"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// startupStateFileName is the file in which earlier releases recorded the binary version that
// passed the startup doctor gate of bare kander. host.json records the version now; the version
// refresh removes this file, and uninstall removes a leftover one.
const startupStateFileName = "kander-startup-state.json"

// RemoveStartupState removes the startup version file earlier releases wrote into the rules
// directory. A missing file is not an error.
func RemoveStartupState(paths config.InstallPaths) error {
	path := filepath.Join(paths.RulesDir, startupStateFileName)
	anchor, err := fileAnchor(path)
	if err != nil {
		return err
	}
	_, err = fs.RemoveNonDirectoryIfExists(anchor, path)
	return err
}
