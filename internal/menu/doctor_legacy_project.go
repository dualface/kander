package menu

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
)

// reportLegacyProjectInstall reports what a retired project install left in the main worktree
// of the current checkout, item by item, and in repair mode removes what Kander wrote there
// and migrates .kander/config.json into the project overlay. It never writes a project rules
// file: a reference to the retired entry is kander init's to replace.
func reportLegacyProjectInstall(repair bool) bool {
	cwd, err := os.Getwd()
	if err != nil {
		return true
	}
	root, err := config.MainWorktree(cwd)
	if err != nil || root == "" {
		return true
	}
	plan := install.InspectLegacyProjectInstall(root)
	references := legacyReferenceFiles(root)
	if !plan.Found() && len(references) == 0 {
		return true
	}
	warning(config.Text("projectrules.legacy_found", root))
	for _, file := range references {
		hint(config.Text("projectrules.legacy_reference", displayProjectPath(root, file)))
	}
	if !repair {
		reportLegacyPlan(plan)
		return false
	}
	result, err := install.RepairLegacyProjectInstall(plan)
	for _, path := range result.Removed {
		success(config.Text("projectrules.legacy_removed", path))
	}
	for _, item := range plan.Items {
		if item.Action == install.UninstallKeep {
			hint(config.Text("install.global_rules_kept", item.Path, item.Reason))
		}
	}
	for _, item := range result.Kept {
		hint(config.Text("install.global_rules_kept", item.Path, item.Reason))
	}
	if plan.Config != nil {
		reportLegacyConfig(*plan.Config, result)
	}
	if err != nil {
		warning(config.Text("projectrules.legacy_repair_failed", err.Error()))
		return false
	}
	return true
}

// reportLegacyPlan prints what a repair would do.
func reportLegacyPlan(plan install.LegacyProjectInstall) {
	for _, item := range plan.Items {
		switch item.Action {
		case install.UninstallRemove:
			hint(config.Text("projectrules.legacy_would_remove", item.Path))
		case install.UninstallEdit:
			hint(config.Text("projectrules.legacy_would_edit", item.Path))
		default:
			hint(config.Text("install.global_rules_kept", item.Path, item.Reason))
		}
	}
	if plan.Config != nil {
		reportLegacyConfig(*plan.Config, install.LegacyProjectRepair{})
	}
}

// reportLegacyConfig prints the migration of .kander/config.json: the keys moved into the
// overlay, the conflicts the overlay wins, and the settings the overlay cannot hold.
func reportLegacyConfig(plan install.LegacyProjectConfig, result install.LegacyProjectRepair) {
	if plan.Err != nil {
		warning(config.Text("projectrules.legacy_config_unreadable", plan.Path, plan.Err.Error()))
		return
	}
	if len(result.Migrated) > 0 {
		success(config.Text("projectrules.legacy_config_migrated", strings.Join(result.Migrated, ", "), plan.Overlay))
	} else if len(plan.Migrate) > 0 {
		hint(config.Text("projectrules.legacy_config_would_migrate", strings.Join(plan.Migrate, ", "), plan.Overlay))
	}
	if len(plan.Conflicts) > 0 {
		hint(config.Text("projectrules.legacy_config_conflicts", strings.Join(plan.Conflicts, ", "), plan.Overlay))
	}
	if len(plan.Retained) > 0 {
		hint(config.Text("projectrules.legacy_config_retained", strings.Join(plan.Retained, ", "), plan.Path))
	}
	if result.ConfigRemoved {
		success(config.Text("projectrules.legacy_removed", plan.Path))
	}
}

// legacyReferenceFiles lists the project rules files of the main worktree that still carry a
// load command naming the retired .kander/rules/KANDER-AGENTS.md.
func legacyReferenceFiles(root string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, agent := range install.ProjectReferenceAgents() {
		target := install.ProjectRulesTarget(agent, root)
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(root, resolved); err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		data, err := os.ReadFile(resolved)
		if err == nil && install.HasLegacyProjectReference(string(data)) {
			out = append(out, target)
		}
	}
	return out
}
