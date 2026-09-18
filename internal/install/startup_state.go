package install

import (
	"encoding/json"
	"path/filepath"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/version"
)

// startupStateFileName records per scope which binary version already passed the startup
// doctor gate, so bare kander reruns doctor once per new version instead of every launch.
const startupStateFileName = "kander-startup-state.json"

// startupState persists the last binary version that ran the startup doctor gate.
type startupState struct {
	Version string `json:"version"`
}

// StartupVersion returns the binary version stamped by the last startup doctor gate,
// or "" when this scope never stamped one.
func StartupVersion(paths config.InstallPaths) string {
	path := filepath.Join(paths.RulesDir, startupStateFileName)
	anchor, err := fileAnchor(path)
	if err != nil {
		return ""
	}
	data, ok, err := fs.ReadRegularFileIfExists(anchor, path)
	if err != nil || !ok {
		return ""
	}
	var state startupState
	if err := json.Unmarshal(data, &state); err != nil {
		return ""
	}
	return state.Version
}

// RecordStartupVersion stamps the running binary's version after the startup doctor gate ran.
func RecordStartupVersion(paths config.InstallPaths) error {
	payload, err := json.Marshal(startupState{Version: version.String()})
	if err != nil {
		return err
	}
	if err := fs.EnsureInheritedDirectoryPath(paths.RulesDir); err != nil {
		return err
	}
	path := filepath.Join(paths.RulesDir, startupStateFileName)
	anchor, err := fileAnchor(path)
	if err != nil {
		return err
	}
	return fs.WriteBytesAtomicInherited(anchor, path, append(payload, '\n'), true)
}
