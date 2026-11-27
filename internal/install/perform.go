package install

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/rules"
)

// Request is a complete, non-interactive installation request.
type Request struct {
	Language     string
	Source       string
	DeleteLegacy bool
	// CopyBinary opts into the binary copy into the global bin directory.
	CopyBinary bool
	// RulesChoice is the install wizard's per-agent install_rules values that differ from
	// the scope's current switches; Perform saves them before writing any rules file.
	RulesChoice map[string]bool
	// leaderTargets holds the destinations shown by the interactive wizard.
	leaderTargets []leaderDestination
}

// Result records what Perform wrote.
type Result struct {
	Paths         config.InstallPaths
	DestBinary    string
	RunBinary     string
	Copied        bool
	Legacy        []string
	LegacyRemoved bool
	// LegacyRules records rule files migrated away from the previous global rules root.
	LegacyRules LegacyRulesMigration
	// GlobalCleanup records removing what earlier releases wrote into the agents' global
	// rules files; install never writes them.
	GlobalCleanup GlobalCleanup
	// Extensions records ensuring each covered agent's rules-extension file.
	Extensions []AgentExtension
	// SkippedSkills names skill directories that could not be safely inspected.
	SkippedSkills []error
}

// Perform installs the rules directory and, when requested, copies the binary into the global
// bin directory. RunBinary is the entry used afterward. It never writes an agent's global
// rules file: it removes what earlier releases wrote there instead.
func Perform(req Request) (Result, error) {
	var result Result
	copyBinary := req.CopyBinary
	source, err := resolveSource(req.Source, copyBinary)
	if err != nil {
		return result, err
	}
	result.RunBinary = source
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		return result, err
	}
	result.Paths = paths
	if req.leaderTargets == nil {
		req.leaderTargets = leaderDestinations(paths)
	}
	dest := filepath.Join(paths.BinDir, binaryName())
	if copyBinary {
		result.DestBinary = dest
		if err := rejectDest(dest); err != nil {
			return result, err
		}
	}
	for _, name := range rules.Names() {
		if err := rejectDest(filepath.Join(paths.RulesDir, filepath.FromSlash(name))); err != nil {
			return result, err
		}
	}
	legacy, err := scanLegacy(paths.BinDir)
	if err != nil {
		return result, err
	}
	result.Legacy = legacy
	dirs := []string{paths.RulesDir, paths.ShareDir}
	if copyBinary {
		dirs = append([]string{paths.BinDir}, dirs...)
	}
	for _, dir := range dirs {
		if err := fs.EnsureInheritedDirectoryPath(dir); err != nil {
			return result, err
		}
	}
	if copyBinary {
		result.Copied, err = installBinary(source, dest)
		if err != nil {
			return result, fmt.Errorf("%s", config.Text("install.failed_to_write_binary", err.Error()))
		}
		result.RunBinary = dest

	}
	lang := req.Language
	if lang == "" {
		lang = config.ResolveLanguage()
	}
	if err := extractRules(paths); err != nil {
		return result, fmt.Errorf("%s", config.Text("install.failed_to_extract_rules", err.Error()))
	}
	if err := config.SetLanguageIfPresent(paths.ConfigPath, lang); err != nil {
		return result, err
	}
	if err := saveRulesChoice(paths, req.RulesChoice, lang); err != nil {
		return result, err
	}
	cleanupAgentsEntryLink(paths)
	// The cleanup runs before the previous rules location retires, so a link aimed at the
	// previous entry is removed as a link instead of being left dangling.
	result.GlobalCleanup = CleanGlobalInjection(paths)
	result.LegacyRules = migrateLegacyRules(paths)
	result.Extensions = integrateAgentExtensions(paths)
	if err := result.installLeaderSkills(req.leaderTargets); err != nil {
		return result, err
	}
	if req.DeleteLegacy && len(result.Legacy) > 0 {
		if !destIsExecutable(result.RunBinary) {
			return result, fmt.Errorf("%s", config.Text("install.new_entry_not_executable", result.RunBinary))
		}
		if err := removeLegacy(paths.BinDir, result.Legacy); err != nil {
			return result, err
		}
		result.LegacyRemoved = true
	}
	if copyBinary {
		CleanupStaleBinary(paths)
	}
	return result, nil
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
	if writeErr := writeExec(anchor, dest, data, false); writeErr != nil {
		if restoreErr := fs.Rename(anchor, aside, dest); restoreErr != nil {
			return fmt.Errorf("write replacement: %w; restore original: %v", writeErr, restoreErr)
		}
		return writeErr
	}
	return nil
}
