package config

// Git holds the Git workflow preferences the released rules read.
type Git struct {
	// BranchMode selects the mainline branch that single cards and task group
	// branches integrate into: develop below a stable main (main-develop), or
	// main itself (main-only).
	BranchMode string `json:"branch_mode"`
}

const (
	GitBranchModeMainDevelop = "main-develop"
	GitBranchModeMainOnly    = "main-only"
)

// GitBranchModes lists the accepted values of git.branch_mode.
var GitBranchModes = []string{GitBranchModeMainDevelop, GitBranchModeMainOnly}

func DefaultGit() Git {
	return Git{BranchMode: GitBranchModeMainDevelop}
}

// validateGit accepts a missing key as the default; a config written before
// the section existed therefore keeps the main + develop workflow.
func validateGit(raw any) (Git, error) {
	obj, ok := asObject(raw)
	if !ok {
		return Git{}, configErrorf("config.git_must_be_a_json_object")
	}
	git := DefaultGit()
	if value, exists := obj["branch_mode"]; exists {
		mode, err := validateChoice(value, GitBranchModes, "git.branch_mode")
		if err != nil {
			return Git{}, err
		}
		git.BranchMode = mode
	}
	return git, nil
}

// MainlineBranch names the branch that single cards and task group branches
// integrate into under the configured branch mode.
func MainlineBranch(cfg *Config) string {
	if cfg != nil && cfg.Git.BranchMode == GitBranchModeMainOnly {
		return "main"
	}
	return "develop"
}

// GitBranchModeLabel is the localized name of one branch_mode value.
func GitBranchModeLabel(mode string) string {
	switch mode {
	case GitBranchModeMainDevelop:
		return Text("config.git_branch_mode_main_develop")
	case GitBranchModeMainOnly:
		return Text("config.git_branch_mode_main_only")
	}
	return mode
}
