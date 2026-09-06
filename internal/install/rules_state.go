package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/rules"
)

const stateFileName = "kander-rules-state.json"

// rulesState stamps the digest of every rule file the installer wrote. Older state files also
// carried a "language" key from the bilingual era; it is ignored on read and dropped on write.
type rulesState struct {
	Files map[string]string `json:"files"`
}

// RulesReport is doctor's view of installed rule files versus the embedded copies.
type RulesReport struct {
	Missing  []string
	Outdated []string
	Modified []string
}

func loadRulesState(paths config.InstallPaths) (rulesState, error) {
	state := rulesState{Files: map[string]string{}}
	path := filepath.Join(paths.RulesDir, stateFileName)
	anchor, err := fileAnchor(path)
	if err != nil {
		return state, err
	}
	data, ok, err := fs.ReadRegularFileIfExists(anchor, path)
	if err != nil || !ok {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return rulesState{Files: map[string]string{}}, nil
	}
	if state.Files == nil {
		state.Files = map[string]string{}
	}
	return state, nil
}

func saveRulesState(paths config.InstallPaths, state rulesState) error {
	if state.Files == nil {
		state.Files = map[string]string{}
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(paths.RulesDir, stateFileName)
	anchor, err := fileAnchor(path)
	if err != nil {
		return err
	}
	return fs.WriteBytesAtomicInherited(anchor, path, append(payload, '\n'), true)
}

func fileHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readInstalledRule(paths config.InstallPaths, name string) ([]byte, bool, error) {
	path := filepath.Join(paths.RulesDir, name)
	anchor, err := fileAnchor(path)
	if err != nil {
		return nil, false, err
	}
	if fs.IsReparsePoint(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		return data, true, nil
	}
	return fs.ReadRegularFileIfExists(anchor, path)
}

// InspectRules compares installed markdown rules with the embedded copies.
func InspectRules(paths config.InstallPaths) (RulesReport, error) {
	var report RulesReport
	state, err := loadRulesState(paths)
	if err != nil {
		return report, err
	}
	for _, name := range rules.Names() {
		data, ok, err := readInstalledRule(paths, name)
		if err != nil {
			return report, err
		}
		if !ok {
			report.Missing = append(report.Missing, name)
			continue
		}
		got := fileHash(data)
		want, err := rules.Hash(name)
		if err != nil {
			return report, err
		}
		dest := filepath.Join(paths.RulesDir, name)
		// Global rule symlinks are user-managed; doctor must not classify them as
		// outdated because writeRule will not replace a reparse point.
		if paths.Mode != config.ModeProject && fs.IsReparsePoint(dest) {
			if got != want {
				report.Modified = append(report.Modified, name)
			}
			continue
		}
		stamped := state.Files[name]
		if stamped != "" {
			if got != stamped {
				report.Modified = append(report.Modified, name)
				continue
			}
			if got != want {
				report.Outdated = append(report.Outdated, name)
			}
			continue
		}
		if got == want {
			continue
		}
		if isPreviousOfficial(name, got) {
			report.Outdated = append(report.Outdated, name)
			continue
		}
		report.Modified = append(report.Modified, name)
	}
	return report, nil
}

func extractRules(paths config.InstallPaths, project bool) error {
	state := rulesState{Files: map[string]string{}}
	for _, name := range rules.Names() {
		data, err := rules.File(name)
		if err != nil {
			return err
		}
		wrote, err := writeRule(paths, name, data, project)
		if err != nil {
			return err
		}
		if digest, ok := stampDigest(paths, name, data, wrote); ok {
			state.Files[name] = digest
		}
	}
	return saveRulesState(paths, state)
}

func stampDigest(paths config.InstallPaths, name string, embedded []byte, wrote bool) (string, bool) {
	if wrote {
		return fileHash(embedded), true
	}
	installed, ok, err := readInstalledRule(paths, name)
	if err != nil || !ok {
		return "", false
	}
	digest := fileHash(installed)
	if digest != fileHash(embedded) {
		return "", false
	}
	return digest, true
}

func writeRule(paths config.InstallPaths, name string, data []byte, project bool) (bool, error) {
	dest := filepath.Join(paths.RulesDir, name)
	if err := rejectDest(dest, project); err != nil {
		return false, err
	}
	if !project && fs.IsReparsePoint(dest) {
		return false, nil
	}
	anchor, err := fileAnchor(dest)
	if err != nil {
		return false, err
	}
	if err := fs.WriteBytesAtomicInherited(anchor, dest, data, true); err != nil {
		return false, err
	}
	return true, nil
}

// RepairRules restores missing and outdated rule files. Locally edited files are left untouched.
func RepairRules(paths config.InstallPaths) error {
	report, err := InspectRules(paths)
	if err != nil {
		return err
	}
	state, err := loadRulesState(paths)
	if err != nil {
		return err
	}
	if err := fs.EnsureInheritedDirectoryPath(paths.RulesDir); err != nil {
		return err
	}
	if state.Files == nil {
		state.Files = map[string]string{}
	}
	changed := false
	repair := append(append([]string{}, report.Missing...), report.Outdated...)
	project := paths.Mode == config.ModeProject
	for _, name := range repair {
		data, err := rules.File(name)
		if err != nil {
			return err
		}
		wrote, err := writeRule(paths, name, data, project)
		if err != nil {
			return err
		}
		if digest, ok := stampDigest(paths, name, data, wrote); ok {
			state.Files[name] = digest
			changed = true
		}
	}
	for _, name := range rules.Names() {
		if _, ok := state.Files[name]; ok {
			continue
		}
		data, ok, err := readInstalledRule(paths, name)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		want, err := rules.Hash(name)
		if err != nil {
			return err
		}
		if fileHash(data) != want {
			continue
		}
		state.Files[name] = want
		changed = true
	}
	if !changed {
		return nil
	}
	return saveRulesState(paths, state)
}

func linkAgentsEntry(paths config.InstallPaths) error {
	link := filepath.Join(paths.RulesDir, "AGENTS.md")
	if _, err := os.Lstat(link); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	anchor, err := fileAnchor(link)
	if err != nil {
		return err
	}
	if err := fs.CreateRelativeSymlink(anchor, link, "KANDER-AGENTS.md"); err == nil {
		return nil
	} else if runtime.GOOS == "windows" {
		return fs.CreateRelativeHardLink(anchor, link, "KANDER-AGENTS.md")
	}
	return err
}
