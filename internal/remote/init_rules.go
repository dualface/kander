package remote

import (
	"fmt"
	"path/filepath"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
)

// Notice codes of the project reference step of kander init.
const (
	noticeRulesCommit     = "project_rules_commit"
	noticeRulesNotGit     = "project_rules_not_git"
	noticeRulesNotWritten = "project_rules_not_written"
	noticeRulesDisabled   = "project_rules_disabled"
)

// initRules is what the project reference step of kander init did.
type initRules struct {
	// Written lists the project rules files the step created or changed.
	Written []string
	// Present lists the project rules files that already referenced the entry.
	Present []string
}

// writeProjectReferences writes the reference to the global rules entry into the project
// rules files of the main worktree of dir, for every agent whose install_rules switch allows
// it. It runs no Git command: the user commits and pushes the files. Everything it cannot do
// is a remark; the binding stands.
func writeProjectReferences(dir string) initRules {
	var out initRules
	main, err := config.MainWorktree(dir)
	if err != nil {
		remark(noticeRulesNotWritten, config.Text("projectrules.init_failed", dir, err.Error()))
		return out
	}
	if main == "" {
		remark(noticeRulesNotGit, config.Text("projectrules.init_not_git", dir))
		return out
	}
	paths, err := config.GlobalInstallPaths()
	if err == nil {
		var project install.ProjectRules
		if project, err = install.NewProjectRules(main, paths); err == nil {
			var switches config.InstallRules
			if switches, err = config.ScopeInstallRules(paths); err == nil {
				out.collect(install.EnsureProjectReferences(project, switches))
				return out
			}
		}
	}
	remark(noticeRulesNotWritten, config.Text("projectrules.init_failed", main, err.Error()))
	return out
}

func (r *initRules) collect(results []install.AgentIntegration) {
	for _, item := range results {
		label := install.AgentLabelList([]string{item.Agent})
		switch {
		case item.Err != nil:
			remark(noticeRulesNotWritten, config.Text("projectrules.init_failed", item.Target, item.Err.Error()))
			continue
		case item.Status == install.IntegrationDisabled:
			remark(noticeRulesDisabled, config.Text("projectrules.init_disabled", install.AgentLabelList(item.DisabledBy), item.Target))
			continue
		}
		r.add(label, item.IntegrationOutcome)
		if item.Override != nil {
			r.add(label, *item.Override)
		}
	}
}

func (r *initRules) add(label string, outcome install.IntegrationOutcome) {
	if outcome.Status == install.IntegrationPresent {
		r.Present = append(r.Present, outcome.Target)
	} else {
		r.Written = append(r.Written, outcome.Target)
	}
	if outcome.Inactive {
		remark(noticeRulesNotWritten, config.Text("menu.rules_reference_inactive", label, outcome.Target))
	}
}

// report prints the files the step wrote and the commit hint; with --json the hint is a
// notice and the files are in the result object.
func (r initRules) report() {
	if len(r.Written) == 0 {
		for _, path := range r.Present {
			if !command.JSON() {
				fmt.Fprintln(stdout, config.Text("projectrules.init_present", filepath.Clean(path)))
			}
		}
		return
	}
	if !command.JSON() {
		for _, path := range r.Written {
			fmt.Fprintln(stdout, config.Text("projectrules.init_written", filepath.Clean(path)))
		}
	}
	remark(noticeRulesCommit, config.Text("projectrules.init_commit_hint"))
}
