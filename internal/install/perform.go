package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/rules"
)

// Request is a complete, non-interactive installation request.
type Request struct {
	Language     string
	Mode         config.Mode
	Project      string
	Source       string
	DeleteLegacy bool
}

// Result records what Perform wrote.
type Result struct {
	Paths         config.InstallPaths
	DestBinary    string
	Copied        bool
	Legacy        []string
	LegacyRemoved bool
}

// Perform copies the current binary and extracts embedded rules into the requested scope.
func Perform(req Request) (Result, error) {
	var result Result
	source := req.Source
	if source == "" {
		var err error
		source, err = lookupExecutable()
		if err != nil {
			return result, fmt.Errorf("%s", config.Text("install.cannot_resolve_executable"))
		}
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return result, err
	}
	if err := rejectSource(source); err != nil {
		return result, err
	}
	paths, err := resolvePaths(req)
	if err != nil {
		return result, err
	}
	result.Paths = paths
	project := paths.Mode == config.ModeProject
	if err := rejectProjectLayout(paths, project); err != nil {
		return result, err
	}
	dest := filepath.Join(paths.BinDir, binaryName())
	result.DestBinary = dest
	if err := rejectDest(dest, project); err != nil {
		return result, err
	}
	for _, name := range rules.Names() {
		if err := rejectDest(filepath.Join(paths.RulesDir, name), project); err != nil {
			return result, err
		}
	}
	if !project {
		legacy, err := scanLegacy(paths.BinDir)
		if err != nil {
			return result, err
		}
		result.Legacy = legacy
	}
	for _, dir := range []string{paths.BinDir, paths.RulesDir, paths.ShareDir} {
		if err := fs.EnsureInheritedDirectoryPath(dir); err != nil {
			return result, err
		}
	}
	if project {
		if _, err := config.EnsureProjectGitExclude(paths.ProjectRoot); err != nil {
			return result, err
		}
	}
	copied, err := installBinary(source, dest)
	if err != nil {
		return result, fmt.Errorf("%s", config.Text("install.failed_to_write_binary", err.Error()))
	}
	result.Copied = copied
	lang := req.Language
	if lang == "" {
		lang = config.ResolveLanguage()
	}
	if err := extractRules(paths, lang, project); err != nil {
		return result, fmt.Errorf("%s", config.Text("install.failed_to_extract_rules", err.Error()))
	}
	if err := config.SetLanguageIfPresent(paths.ConfigPath, lang); err != nil {
		return result, err
	}
	if err := linkAgentsEntry(paths); err != nil {
		return result, fmt.Errorf("%s", config.Text("install.failed_to_link_agents", err.Error()))
	}
	if req.DeleteLegacy && len(result.Legacy) > 0 {
		if !destIsExecutable(dest) {
			return result, fmt.Errorf("%s", config.Text("install.new_entry_not_executable", dest))
		}
		if err := removeLegacy(paths.BinDir, result.Legacy); err != nil {
			return result, err
		}
		result.LegacyRemoved = true
	}
	CleanupStaleBinary(paths)
	return result, nil
}

func resolvePaths(req Request) (config.InstallPaths, error) {
	if req.Mode == config.ModeProject {
		return config.ProjectInstallPaths(req.Project)
	}
	return config.GlobalInstallPaths()
}

func rejectProjectLayout(paths config.InstallPaths, project bool) error {
	if !project {
		return nil
	}
	for _, dir := range []string{paths.InstallRoot, paths.BinDir, paths.RulesDir, paths.ShareDir} {
		if _, err := os.Lstat(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if fs.IsReparsePoint(dir) {
			return fmt.Errorf("%s", config.Text("install.target_is_symlink", dir))
		}
	}
	return nil
}

func installBinary(source, dest string) (bool, error) {
	if sameFile(source, dest) {
		return false, nil
	}
	data, err := readSource(source)
	if err != nil {
		return false, err
	}
	if err := writeBinary(dest, data); err != nil {
		return false, err
	}
	return true, nil
}

func readSource(path string) ([]byte, error) {
	if err := rejectSource(path); err != nil {
		return nil, err
	}
	anchor, err := fileAnchor(path)
	if err != nil {
		return nil, err
	}
	return fs.ReadRegularFile(anchor, path)
}

var (
	writeExec   = fs.WriteExecutableAtomicInherited
	fileIsBusy  = fs.IsBusyFile
	asideOnBusy = windowsBusyAside
)

func windowsBusyAside() bool {
	return runtime.GOOS == "windows"
}

func writeBinary(dest string, data []byte) error {
	anchor, err := fileAnchor(dest)
	if err != nil {
		return err
	}
	CleanupStaleBinary(config.InstallPaths{BinDir: filepath.Dir(dest)})
	err = writeExec(anchor, dest, data, true)
	if err == nil {
		return nil
	}
	if !asideOnBusy() || !fileIsBusy(err) {
		return err
	}
	aside := dest + ".old"
	_, _ = fs.RemoveNonDirectoryIfExists(anchor, aside)
	if renameErr := fs.Rename(anchor, dest, aside); renameErr != nil {
		return err
	}
	return writeExec(anchor, dest, data, false)
}
