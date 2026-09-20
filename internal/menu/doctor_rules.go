package menu

import (
	"strconv"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/install"
)

// confirmModifiedRules asks once whether the locally modified rule files listed just above
// should be backed up and replaced by the embedded copies. It is a variable so tests can
// stub the interactive decision.
var confirmModifiedRules = askModifiedRulesReplace

// askModifiedRulesReplace is the one-shot interactive decision for modified rule files. The
// default keeps the local files: a declined prompt, an ended input, or a read error all keep
// the files and the modified report untouched.
func askModifiedRulesReplace(modified []string) bool {
	selected, err := askChoice(config.Text(
		"menu.modified_rules_backup_replace_prompt", strconv.Itoa(len(modified)),
	), []choice{
		{Value: "replace", Label: config.Text("menu.backup_and_replace_modified_rules")},
		{Value: "keep", Label: config.Text("menu.keep_modified_rules")},
	}, "keep")
	return err == nil && selected == "replace"
}

// promptModifiedRules runs the interactive backup-and-replace flow for doctor. Keeping the
// files is not an environment failure: the modified hints already printed stay the record.
// After a confirmed run every file reports its own success or failure line.
func promptModifiedRules(paths config.InstallPaths, modified []string) bool {
	if !confirmModifiedRules(modified) {
		return true
	}
	healthy := true
	result, err := install.ReplaceModifiedRules(paths, modified)
	for _, item := range result.Replaced {
		success(config.Text("install.rule_replaced_with_backup", item.Name, item.Backup))
	}
	for _, item := range result.Failed {
		healthy = false
		warning(config.Text("install.rule_replace_failed", item.Name, item.Err.Error()))
	}
	if err != nil {
		healthy = false
		warning(err.Error())
	}
	return healthy
}
