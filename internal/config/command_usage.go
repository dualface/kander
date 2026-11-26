package config

import (
	"path/filepath"

	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/rules"
)

// commandContractsDir is the rules-root directory that holds one contract file per command.
const commandContractsDir = rules.SnippetsDir + "/commands"

// rulesRootPlaceholder stands in for the rules root when no installation can be resolved.
const rulesRootPlaceholder = "<rules root>"

// CommandReadSentence is the conditional read sentence every rule file, prompt, and help
// output repeats verbatim, with rulesRoot in place of the rules root.
func CommandReadSentence(rulesRoot string) string {
	return "Before running any `kander` command in this session, read `" + rulesRoot + "/" + commandContractsDir +
		"/README.md` once. Before the first use of a command, read its contract file."
}

// ContractRoot returns the rules root of the current scope as an agent addresses it, and the
// directory it names on disk. Without a resolvable installation the root is a placeholder and
// the directory is empty.
func ContractRoot() (shown, dir string) {
	paths, err := CurrentInstallPaths()
	if err != nil || paths.RulesDir == "" {
		return rulesRootPlaceholder, ""
	}
	return process.AgentPath(filepath.Clean(paths.RulesDir)), paths.RulesDir
}
