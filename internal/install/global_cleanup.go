package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// Kander no longer writes the agents' global rules files; kander init writes a project
// reference instead. Earlier releases wrote a Kander load command, a reference block, a symlink
// to the rules entry, or a whole ~/.cursor/rules/kander.mdc into them. The global injection
// cleanup removes exactly that content once per agent configuration directory, records the
// directories it finished in a marker file of the rules directory, and serializes concurrent
// runs with a lock file next to it.
const (
	// globalCleanupFileName records the configuration directories whose global injection is
	// already removed.
	globalCleanupFileName = "kander-global-cleanup.json"
	// globalCleanupLockName serializes the cleanup between kander processes.
	globalCleanupLockName = "kander-global-cleanup.lock"
)

// globalCleanupState is the content of the marker file.
type globalCleanupState struct {
	ConfigDirs []string `json:"config_dirs"`
}

// GlobalCleanup is the result of one global injection cleanup.
type GlobalCleanup struct {
	// Ran is false when the cleanup was not attempted: every configuration directory was
	// already recorded, or another kander process held the lock.
	Ran bool
	// Removed lists the files Kander removed or rewrote without its content.
	Removed []string
	// Kept lists the files that keep content after the cleanup by design, with the reason: a
	// .mdc rule the user changed, or a rules file the cleanup never edits.
	Kept []UninstallItem
	// Err joins the failures; the marker is not written, so the next run tries again.
	Err error
}

// Changed reports whether the cleanup removed anything, so the projects need kander init.
func (c GlobalCleanup) Changed() bool { return len(c.Removed) > 0 }

// globalCleanupPlan is what the cleanup would change and the configuration directories it covers.
type globalCleanupPlan struct {
	Items      []UninstallItem
	ConfigDirs []string
	// Err joins the paths whose inspection failed, including an agent whose configuration
	// directory or rules file could not be resolved.
	Err error
}

// planGlobalInjectionCleanup plans removing the Kander content from the global rules file of
// every embedded agent, and its existing AGENTS.override.md sibling. A file reached from two
// configuration directories (one rules file linked to another) is planned once.
func planGlobalInjectionCleanup(paths config.InstallPaths) globalCleanupPlan {
	var plan globalCleanupPlan
	seen := map[string]struct{}{}
	dirs := map[string]struct{}{}
	for _, agent := range config.ExecutionAgents {
		root, _, err := agentConfigRoot(agent, paths)
		if err != nil {
			plan.Items = append(plan.Items, UninstallItem{Action: UninstallKeep, Path: agent, Reason: err.Error(), failed: true})
			continue
		}
		if root == "" {
			continue
		}
		dirs[configDirKey(root)] = struct{}{}
		target, err := GlobalRulesTarget(agent, paths)
		if err != nil {
			plan.Items = append(plan.Items, UninstallItem{Action: UninstallKeep, Path: agent, Reason: err.Error(), failed: true})
			continue
		}
		if target == "" {
			continue
		}
		files := []struct {
			path     string
			override bool
		}{{target, false}}
		if override := rulesOverrideTarget(target); override != "" {
			files = append(files, struct {
				path     string
				override bool
			}{override, true})
		}
		for _, file := range files {
			item, ok := planReferenceRemoval(file.path, file.override, paths)
			if !ok {
				continue
			}
			key := item.Path
			if item.Action != UninstallRemove {
				key = rulesFileKey(item.Path)
			}
			if _, done := seen[key]; done {
				continue
			}
			seen[key] = struct{}{}
			plan.Items = append(plan.Items, item)
		}
	}
	for _, item := range plan.Items {
		if item.failed {
			plan.Err = errors.Join(plan.Err, fmt.Errorf("%s: %s", item.Path, item.Reason))
		}
	}
	for dir := range dirs {
		plan.ConfigDirs = append(plan.ConfigDirs, dir)
	}
	slices.Sort(plan.ConfigDirs)
	return plan
}

// configDirKey normalizes a configuration directory for the marker: its path with symlinks
// resolved when it exists, otherwise the cleaned path.
func configDirKey(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return filepath.Clean(dir)
}

// CleanGlobalInjection removes the Kander content from the global agent rules files now,
// whatever the marker says; kander install and kander doctor run it every time. It records the
// configuration directories in the marker when everything succeeded. Another process holding
// the lock makes it skip.
func CleanGlobalInjection(paths config.InstallPaths) GlobalCleanup {
	return runGlobalCleanup(paths, true)
}

// AutoCleanGlobalInjection is the cleanup every kander command runs first: it skips when the
// marker already records every configuration directory of the current environment, so it acts
// once per directory. It never asks a question and never fails the command.
func AutoCleanGlobalInjection(paths config.InstallPaths) GlobalCleanup {
	if testing.Testing() && !autoCleanInjected.Load() {
		// A test of another package that runs a command must never touch the real home.
		return GlobalCleanup{}
	}
	// Without the rules root, the cleanup runs only when there is something to remove: an
	// earlier release may have installed the flat layout (~/.agents/KANDER-AGENTS.md) and
	// written its references, but a home Kander never used gets no marker or lock.
	if info, err := os.Lstat(paths.RulesDir); (err != nil || !info.IsDir()) && !planRemovesContent(planGlobalInjectionCleanup(paths)) {
		return GlobalCleanup{}
	}
	return runGlobalCleanup(paths, false)
}

// planRemovesContent reports whether the plan edits or removes a file.
func planRemovesContent(plan globalCleanupPlan) bool {
	return slices.ContainsFunc(plan.Items, func(item UninstallItem) bool {
		return item.Action == UninstallEdit || item.Action == UninstallRemove
	})
}

// autoCleanInjected lets a test of the automatic cleanup run it; every other test skips it.
var autoCleanInjected atomic.Bool

func runGlobalCleanup(paths config.InstallPaths, force bool) GlobalCleanup {
	var result GlobalCleanup
	plan := planGlobalInjectionCleanup(paths)
	if !force && plan.Err == nil && globalCleanupRecorded(paths, plan.ConfigDirs) {
		return result
	}
	if err := fs.EnsureInheritedDirectoryPath(paths.RulesDir); err != nil {
		result.Err = err
		return result
	}
	lockPath := filepath.Join(paths.RulesDir, globalCleanupLockName)
	anchor, err := fileAnchor(lockPath)
	if err != nil {
		result.Err = err
		return result
	}
	file, err := fs.OpenLockFile(anchor, lockPath)
	if err != nil {
		result.Err = err
		return result
	}
	defer file.Close()
	lock, taken, err := fs.TryLockExclusive(file)
	if err != nil {
		result.Err = err
		return result
	}
	if !taken {
		return result
	}
	defer lock.Unlock()
	// Another process may have finished between the first plan and the lock: plan again.
	plan = planGlobalInjectionCleanup(paths)
	if !force && plan.Err == nil && globalCleanupRecorded(paths, plan.ConfigDirs) {
		return result
	}
	result.Ran = true
	result.Err = plan.Err
	for _, item := range plan.Items {
		if item.Reason != "" && !item.failed {
			result.Kept = append(result.Kept, item)
		}
	}
	_, changed, applyErr := applyUninstallItems(plan.Items, io.Discard)
	result.Err = errors.Join(result.Err, applyErr)
	result.Removed = changed
	if result.Err != nil {
		return result
	}
	if err := recordGlobalCleanup(paths, plan.ConfigDirs); err != nil {
		result.Err = err
	}
	return result
}

// globalCleanupRecorded reports whether the marker records every directory of dirs.
func globalCleanupRecorded(paths config.InstallPaths, dirs []string) bool {
	state, err := loadGlobalCleanup(paths)
	if err != nil {
		return false
	}
	for _, dir := range dirs {
		if !slices.Contains(state.ConfigDirs, dir) {
			return false
		}
	}
	return true
}

func loadGlobalCleanup(paths config.InstallPaths) (globalCleanupState, error) {
	var state globalCleanupState
	path := filepath.Join(paths.RulesDir, globalCleanupFileName)
	anchor, err := fileAnchor(path)
	if err != nil {
		return state, err
	}
	data, ok, err := fs.ReadRegularFileIfExists(anchor, path)
	if err != nil || !ok {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		// A damaged marker only means the cleanup runs again.
		return globalCleanupState{}, nil
	}
	return state, nil
}

// recordGlobalCleanup adds dirs to the marker, keeping the directories other environments
// recorded before.
func recordGlobalCleanup(paths config.InstallPaths, dirs []string) error {
	state, err := loadGlobalCleanup(paths)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if !slices.Contains(state.ConfigDirs, dir) {
			state.ConfigDirs = append(state.ConfigDirs, dir)
		}
	}
	slices.Sort(state.ConfigDirs)
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(paths.RulesDir, globalCleanupFileName)
	anchor, err := fileAnchor(path)
	if err != nil {
		return err
	}
	return fs.WriteBytesAtomicInherited(anchor, path, append(payload, '\n'), true)
}

// GlobalCleanupNotices renders the outcome of a cleanup as notice lines, one code each: what
// was removed, with the follow-up kander init; what was kept and why; what failed. A cleanup
// that did nothing renders nothing.
func GlobalCleanupNotices(result GlobalCleanup) []GlobalCleanupNotice {
	var out []GlobalCleanupNotice
	if result.Changed() {
		out = append(out, GlobalCleanupNotice{Code: NoticeGlobalRulesCleaned, Text: config.Text("install.global_rules_cleaned", joinLines(result.Removed))})
	}
	for _, item := range result.Kept {
		out = append(out, GlobalCleanupNotice{Code: NoticeGlobalRulesKept, Text: config.Text("install.global_rules_kept", item.Path, item.Reason)})
	}
	if result.Err != nil {
		out = append(out, GlobalCleanupNotice{Code: NoticeGlobalRulesCleanupFailed, Text: config.Text("install.global_rules_cleanup_failed", result.Err.Error())})
	}
	return out
}

// GlobalCleanupNotice is one line of the cleanup outcome with its --json notice code.
type GlobalCleanupNotice struct {
	Code string
	Text string
}

// Notice codes of the global injection cleanup.
const (
	NoticeGlobalRulesCleaned       = "global_rules_cleaned"
	NoticeGlobalRulesKept          = "global_rules_kept"
	NoticeGlobalRulesCleanupFailed = "global_rules_cleanup_failed"
)

func joinLines(paths []string) string {
	return strings.Join(paths, ", ")
}
