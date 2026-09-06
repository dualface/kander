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

type rulesState struct {
	Language string            `json:"language"`
	Files    map[string]string `json:"files"`
}

// RulesReport is doctor's view of installed rule files versus the embedded copies.
type RulesReport struct {
	Missing           []string
	Outdated          []string
	Modified          []string
	LanguageDrift     bool
	InstalledLanguage string
	ConfigLanguage    string
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
func InspectRules(paths config.InstallPaths, cfgLang string) (RulesReport, error) {
	report := RulesReport{ConfigLanguage: cfgLang}
	state, err := loadRulesState(paths)
	if err != nil {
		return report, err
	}
	report.InstalledLanguage = state.Language
	if state.Language != "" && cfgLang != "" && state.Language != cfgLang {
		report.LanguageDrift = true
	}
	compareLang := state.Language
	if compareLang == "" {
		compareLang = cfgLang
	}
	if compareLang == "" {
		compareLang = rules.LangCN
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
		want, _, err := rules.Hash(compareLang, name)
		if err != nil {
			return report, err
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
		if got != want {
			report.Modified = append(report.Modified, name)
		}
	}
	return report, nil
}

func extractRules(paths config.InstallPaths, lang string, project bool) error {
	state := rulesState{Language: lang, Files: map[string]string{}}
	for _, name := range rules.Names() {
		data, _, err := rules.File(lang, name)
		if err != nil {
			return err
		}
		if err := writeRule(paths, name, data, project); err != nil {
			return err
		}
		state.Files[name] = fileHash(data)
	}
	return saveRulesState(paths, state)
}

func writeRule(paths config.InstallPaths, name string, data []byte, project bool) error {
	dest := filepath.Join(paths.RulesDir, name)
	if err := rejectDest(dest, project); err != nil {
		return err
	}
	if !project && fs.IsReparsePoint(dest) {
		return nil
	}
	anchor, err := fileAnchor(dest)
	if err != nil {
		return err
	}
	return fs.WriteBytesAtomicInherited(anchor, dest, data, true)
}

// RepairRules restores missing and outdated rule files. Locally edited files are left untouched.
func RepairRules(paths config.InstallPaths, cfgLang string) error {
	report, err := InspectRules(paths, cfgLang)
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
	lang := state.Language
	if lang == "" {
		lang = cfgLang
	}
	if lang == "" {
		lang = rules.LangCN
	}
	if state.Files == nil {
		state.Files = map[string]string{}
	}
	if state.Language == "" {
		state.Language = lang
	}
	changed := false
	repair := append(append([]string{}, report.Missing...), report.Outdated...)
	project := paths.Mode == config.ModeProject
	for _, name := range repair {
		data, _, err := rules.File(lang, name)
		if err != nil {
			return err
		}
		if err := writeRule(paths, name, data, project); err != nil {
			return err
		}
		state.Files[name] = fileHash(data)
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
