package install

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/rules"
)

// RuleReplacement records one modified rule file ReplaceModifiedRules restored: Backup is
// the absolute path that now holds the exact pre-replacement content.
type RuleReplacement struct {
	Name   string
	Backup string
}

// RuleReplaceFailure records one modified rule file that could not be restored. The file
// and its rules-state stamp are left untouched, so InspectRules still reports it modified.
type RuleReplaceFailure struct {
	Name string
	Err  error
}

// ModifiedRulesResult reports the per-file outcome of ReplaceModifiedRules.
type ModifiedRulesResult struct {
	Replaced []RuleReplacement
	Failed   []RuleReplaceFailure
}

// ReplaceModifiedRules backs up every named locally modified rule file and then installs
// the embedded copy, converging the file's rules-state stamp. Each file is independent: a
// backup or replace failure is recorded under Failed and the remaining files still run. The
// stamp file is saved once after the loop, so a save failure is the function error while the
// per-file results stay returned for diagnosis. Names are processed in the given order.
func ReplaceModifiedRules(paths config.InstallPaths, names []string) (ModifiedRulesResult, error) {
	var result ModifiedRulesResult
	state, err := loadRulesState(paths)
	if err != nil {
		return result, err
	}
	if err := fs.EnsureInheritedDirectoryPath(paths.RulesDir); err != nil {
		return result, err
	}
	if state.Files == nil {
		state.Files = map[string]string{}
	}
	project := paths.Mode == config.ModeProject
	changed := false
	for _, name := range names {
		replacement, fail := replaceModifiedRule(paths, name, project)
		if fail != nil {
			result.Failed = append(result.Failed, *fail)
			continue
		}
		data, err := rules.File(name)
		if err != nil {
			result.Failed = append(result.Failed, RuleReplaceFailure{Name: name, Err: err})
			continue
		}
		state.Files[name] = fileHash(data)
		changed = true
		result.Replaced = append(result.Replaced, *replacement)
	}
	if !changed {
		return result, nil
	}
	return result, saveRulesState(paths, state)
}

// replaceModifiedRule backs up the installed copy of name and replaces it with the embedded
// file. The backup must exist before the destination is touched, so a failed backup leaves
// the local edit in place. A global-mode reparse point stays user-managed: the file is not
// backed up or replaced and the failure names that boundary.
func replaceModifiedRule(paths config.InstallPaths, name string, project bool) (*RuleReplacement, *RuleReplaceFailure) {
	fail := func(err error) (*RuleReplacement, *RuleReplaceFailure) {
		return nil, &RuleReplaceFailure{Name: name, Err: err}
	}
	data, ok, err := readInstalledRule(paths, name)
	if err != nil {
		return fail(err)
	}
	if !ok {
		return fail(fmt.Errorf("%s", config.Text("install.rule_replace_missing", name)))
	}
	dest := filepath.Join(paths.RulesDir, name)
	if !project && fs.IsReparsePoint(dest) {
		return fail(fmt.Errorf("%s", config.Text("install.rule_backup_refused_link", name)))
	}
	backup, err := uniqueRuleBackupPath(paths.RulesDir, name)
	if err != nil {
		return fail(err)
	}
	anchor, err := fileAnchor(backup)
	if err != nil {
		return fail(err)
	}
	// replace=false publishes exclusively: a backup path taken between the uniqueness
	// check and the write fails instead of overwriting somebody else's file.
	if err := fs.WriteBytesAtomicInherited(anchor, backup, data, false); err != nil {
		return fail(err)
	}
	embedded, err := rules.File(name)
	if err != nil {
		return fail(err)
	}
	wrote, err := writeRule(paths, name, embedded, project)
	if err != nil {
		return fail(err)
	}
	if !wrote {
		return fail(fmt.Errorf("%s", config.Text("install.rule_backup_refused_link", name)))
	}
	return &RuleReplacement{Name: name, Backup: backup}, nil
}

// uniqueRuleBackupPath picks a timestamped backup name for name inside dir that no existing
// entry claims, including earlier backups from the same second.
func uniqueRuleBackupPath(dir, name string) (string, error) {
	stamp := time.Now().Format("20060102-150405")
	base := name + ".backup-" + stamp
	candidate := filepath.Join(dir, base)
	for suffix := 1; ; suffix++ {
		_, err := os.Lstat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", err
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d", base, suffix))
	}
}
