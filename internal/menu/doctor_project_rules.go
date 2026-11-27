package menu

import (
	"os"
	"path/filepath"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
)

// reportGlobalInjectionCleanup removes what earlier releases wrote into the agents' global
// rules files and reports it. Doctor runs it on every run, check and repair alike, whatever
// the marker of the automatic cleanup says; it returns false when the cleanup failed.
func reportGlobalInjectionCleanup(paths config.InstallPaths) bool {
	result := install.CleanGlobalInjection(paths)
	for _, notice := range install.GlobalCleanupNotices(result) {
		switch notice.Code {
		case install.NoticeGlobalRulesCleaned:
			success(notice.Text)
		case install.NoticeGlobalRulesKept:
			hint(notice.Text)
		default:
			warning(notice.Text)
		}
	}
	return result.Err == nil
}

// reportProjectReferences reports, in a checkout bound to a project (its overlay sets
// project.name), whether every project rules file kander init writes references the Kander
// rules entry. Doctor never writes a project rules file, in check or repair: a missing or
// retired reference is reported with the kander init hint, and the user commits the result.
func reportProjectReferences(paths config.InstallPaths, switches config.InstallRules) bool {
	root, ok := boundMainWorktree()
	if !ok {
		return true
	}
	project, err := install.NewProjectRules(root, paths)
	if err != nil {
		warning(err.Error())
		return false
	}
	healthy := true
	missing := false
	for _, state := range install.InspectProjectReferences(project, switches) {
		label := install.AgentLabelList(state.Agents)
		rel := displayProjectPath(project.Root, state.Target)
		if len(state.DisabledBy) > 0 {
			hint(config.Text("menu.rules_injection_disabled", install.AgentLabelList(state.DisabledBy), rel))
			continue
		}
		switch {
		case state.Legacy:
			healthy, missing = false, true
			warning(config.Text("projectrules.doctor_legacy_reference", label, rel))
		case state.Present:
			success(config.Text("projectrules.doctor_reference_present", label, rel))
		default:
			healthy, missing = false, true
			warning(config.Text("projectrules.doctor_reference_missing", label, state.Detail))
		}
	}
	if missing {
		hint(config.Text("projectrules.doctor_run_init"))
	}
	return healthy
}

// boundMainWorktree returns the Git main worktree of the current directory when its project
// overlay sets project.name.
func boundMainWorktree() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	main, err := config.MainWorktree(cwd)
	if err != nil || main == "" {
		return "", false
	}
	loc, err := config.ResolveOverlayLocation(cwd)
	if err != nil || !loc.Exists {
		return "", false
	}
	overlay, err := config.ReadOverlayFile(loc.Path)
	if err != nil {
		return "", false
	}
	name, ok := config.OverlayGet(overlay, "project", "name")
	if text, isText := name.(string); !ok || !isText || text == "" {
		return "", false
	}
	return main, true
}

// displayProjectPath shows a path below the main worktree relative to it, with forward slashes.
func displayProjectPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !filepath.IsAbs(rel) && (len(rel) < 3 || rel[:3] != ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return path
}
