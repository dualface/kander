package install

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/config"
	kfs "github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/rules"
)

// UninstallAction is what kander uninstall does with one path.
type UninstallAction int

const (
	// UninstallEdit rewrites an agent rules file without its Kander load commands.
	UninstallEdit UninstallAction = iota
	// UninstallRemove deletes a file Kander owns.
	UninstallRemove
	// UninstallKeep leaves a path in place and reports why.
	UninstallKeep
)

// UninstallItem is one planned path of kander uninstall.
type UninstallItem struct {
	Action UninstallAction
	Path   string
	// Reason explains a kept path.
	Reason  string
	content []byte
	// rules marks a file of the rules directory, which the plan lists as one count.
	rules bool
}

// UninstallPlan is everything kander uninstall removes or edits in one scope, computed
// before anything changes.
type UninstallPlan struct {
	Paths config.InstallPaths
	// ConfigFile receives install_rules=false so a later doctor repair writes nothing.
	ConfigFile string
	Items      []UninstallItem
	// Dirs are the rules directory and its subdirectories, deepest first; each goes once empty.
	Dirs []string
}

var (
	uninstallPromptIn  io.Reader = os.Stdin
	uninstallPromptOut io.Writer = os.Stderr
	uninstallTerminal            = func() bool { return isTTY(os.Stdin) && isTTY(os.Stderr) }
)

// RunUninstall is the kander uninstall subcommand.
func RunUninstall(args []string) int {
	yes := false
	for _, arg := range args {
		if arg == "--yes" {
			yes = true
			continue
		}
		fmt.Fprintln(os.Stderr, config.CommandUsage("uninstall"))
		fmt.Fprintln(os.Stderr, config.Text("cli.error_unknown_option", arg))
		return 2
	}
	paths, err := config.CurrentInstallPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kander:", err)
		return 1
	}
	plan := PlanUninstall(paths)
	printUninstallPlan(os.Stdout, plan)
	if !yes {
		if !uninstallTerminal() {
			fmt.Fprintln(os.Stderr, config.Text("install.uninstall_requires_yes"))
			return 2
		}
		confirmed, err := askUninstall()
		if err != nil {
			fmt.Fprintln(os.Stderr, "kander:", err)
			return 1
		}
		if !confirmed {
			fmt.Fprintln(os.Stderr, config.Text("install.uninstall_cancelled"))
			return 1
		}
	}
	if err := ApplyUninstall(plan, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "kander:", err)
		return 1
	}
	return 0
}

func askUninstall() (bool, error) {
	fmt.Fprint(uninstallPromptOut, config.Text("install.uninstall_question"))
	line, err := bufio.NewReader(uninstallPromptIn).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	return parseCopyAnswer(line), nil
}

// PlanUninstall lists what kander uninstall would change in the scope, without writing.
// Every embedded agent's rules file is covered whatever its switch, so references written
// before a switch was turned off go too.
func PlanUninstall(paths config.InstallPaths) UninstallPlan {
	plan := UninstallPlan{Paths: paths, ConfigFile: config.ScopeConfigFile(paths)}
	seen := map[string]struct{}{}
	for _, agent := range config.ExecutionAgents {
		target, err := AgentRulesTarget(agent, paths)
		if err != nil {
			plan.Items = append(plan.Items, UninstallItem{Action: UninstallKeep, Path: agent, Reason: err.Error()})
			continue
		}
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		if item, ok := planReferenceRemoval(target, false, paths); ok {
			plan.Items = append(plan.Items, item)
		}
		if override := rulesOverrideTarget(target); override != "" {
			if item, ok := planReferenceRemoval(override, true, paths); ok {
				plan.Items = append(plan.Items, item)
			}
		}
	}
	for _, agent := range config.ExecutionAgents {
		if item, ok := planExtensionRemoval(agent, paths); ok {
			plan.Items = append(plan.Items, item)
		}
	}
	items, dirs := planRulesDirRemoval(paths.RulesDir)
	plan.Items = append(plan.Items, items...)
	plan.Dirs = dirs
	return plan
}

// planReferenceRemoval plans dropping the Kander load commands from one rules file. A file left
// with only blank lines, or a .mdc rule left with only the alwaysApply frontmatter Kander wrote,
// is deleted; an AGENTS.override.md sibling and a symlinked rules file are only edited.
func planReferenceRemoval(target string, override bool, paths config.InstallPaths) (UninstallItem, bool) {
	keep := func(reason string) (UninstallItem, bool) {
		return UninstallItem{Action: UninstallKeep, Path: target, Reason: reason}, true
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return UninstallItem{}, false
	}
	if err != nil {
		return keep(err.Error())
	}
	linked := info.Mode()&os.ModeSymlink != 0
	if linked && isMDCTarget(target) {
		return keep(config.Text("menu.mdc_rule_is_symlink", target))
	}
	// The write goes to the real path, as EnsureRulesIntegration's does, so a config directory
	// the user keeps behind a symlink is edited in place; a project file must stay inside the
	// main worktree.
	resolved, err := realRulesPath(target, linked)
	if err != nil {
		return keep(err.Error())
	}
	if paths.Mode == config.ModeProject && !withinDirectory(paths.ProjectRoot, resolved) {
		return keep(config.Text("install.rules_target_symlink_escapes_project", target))
	}
	if linked && sameResolved(resolved, RulesEntry(paths)) {
		return keep(config.Text("install.uninstall_link_to_entry"))
	}
	if info, err := os.Stat(resolved); err != nil {
		return keep(err.Error())
	} else if !info.Mode().IsRegular() {
		return keep(config.Text("menu.cannot_read", target))
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return keep(err.Error())
	}
	stripped, removed := RemoveRulesReferences(string(data))
	if removed == 0 {
		return UninstallItem{}, false
	}
	rest := strings.TrimSpace(stripped)
	kanderOnly := rest == "" || isMDCTarget(target) && rest == strings.TrimSpace(alwaysApplyFrontmatter)
	// Deletion never follows a symlink: a file whose directory is reached through one is
	// emptied of the Kander lines instead.
	if kanderOnly && !linked && !override && resolved == filepath.Clean(target) {
		return UninstallItem{Action: UninstallRemove, Path: target}, true
	}
	return UninstallItem{Action: UninstallEdit, Path: resolved, content: []byte(stripped)}, true
}

// realRulesPath resolves a rules file to the path without symlinks: the link target for a
// symlinked file, otherwise the file below its resolved directory.
func realRulesPath(target string, linked bool) (string, error) {
	if linked {
		return filepath.EvalSymlinks(target)
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(target)), nil
}

// planExtensionRemoval plans deleting one agent's rules extension when its digest is a released
// version; a locally modified copy stays.
func planExtensionRemoval(agent string, paths config.InstallPaths) (UninstallItem, bool) {
	outcome, err := InspectExtension(agent, paths)
	if outcome.Target == "" && err == nil {
		return UninstallItem{}, false
	}
	path := cmp.Or(outcome.Target, agent)
	if err != nil {
		return UninstallItem{Action: UninstallKeep, Path: path, Reason: err.Error()}, true
	}
	switch outcome.Status {
	case ExtensionInstalled, ExtensionOutdated:
		return UninstallItem{Action: UninstallRemove, Path: path}, true
	case ExtensionModified:
		return UninstallItem{Action: UninstallKeep, Path: path, Reason: config.Text("install.uninstall_extension_modified")}, true
	default:
		return UninstallItem{}, false
	}
}

// planRulesDirRemoval plans deleting the rule files and state files Kander installs into the
// rules directory; every other entry stays and is reported. Symlinks are never followed.
func planRulesDirRemoval(dir string) ([]UninstallItem, []string) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return []UninstallItem{{Action: UninstallKeep, Path: dir, Reason: err.Error()}}, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || kfs.IsReparsePoint(dir) || !info.IsDir() {
		return []UninstallItem{{Action: UninstallKeep, Path: dir, Reason: config.Text("install.target_is_symlink", dir)}}, nil
	}
	known := map[string]struct{}{stateFileName: {}, startupStateFileName: {}}
	for _, name := range rules.Names() {
		known[name] = struct{}{}
	}
	var items []UninstallItem
	dirs := []string{dir}
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			items = append(items, UninstallItem{Action: UninstallKeep, Path: path, Reason: err.Error()})
			return nil
		}
		if path == dir {
			return nil
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if _, ok := known[filepath.ToSlash(rel)]; ok && entry.Type().IsRegular() {
			items = append(items, UninstallItem{Action: UninstallRemove, Path: path, rules: true})
			return nil
		}
		items = append(items, UninstallItem{Action: UninstallKeep, Path: path, Reason: config.Text("install.uninstall_not_kander_file")})
		return nil
	})
	if walkErr != nil {
		items = append(items, UninstallItem{Action: UninstallKeep, Path: dir, Reason: walkErr.Error()})
	}
	// Deepest first, so a parent is only tried after its children are gone.
	slices.SortFunc(dirs, func(a, b string) int {
		return cmp.Compare(strings.Count(b, string(os.PathSeparator)), strings.Count(a, string(os.PathSeparator)))
	})
	return items, dirs
}

// ApplyUninstall turns the scope's switch off first, so a doctor repair that runs meanwhile or
// afterwards writes nothing, then edits and deletes the planned files and removes the empty
// rules directories. A failed config write changes nothing else.
func ApplyUninstall(plan UninstallPlan, out io.Writer) error {
	if err := disableRulesInjection(plan.ConfigFile); err != nil {
		return err
	}
	fmt.Fprintln(out, config.Text("install.uninstall_config_written", plan.ConfigFile))
	var failed error
	removedRules := 0
	for _, item := range plan.Items {
		var err error
		switch item.Action {
		case UninstallEdit:
			err = writeUninstallEdit(item)
			if err == nil {
				fmt.Fprintln(out, config.Text("install.uninstall_edited", item.Path))
			}
		case UninstallRemove:
			err = removeUninstallFile(item.Path)
			if err == nil && item.rules {
				removedRules++
			} else if err == nil {
				fmt.Fprintln(out, config.Text("install.uninstall_removed", item.Path))
			}
		default:
			continue
		}
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", item.Path, err))
		}
	}
	if removedRules > 0 {
		fmt.Fprintln(out, config.Text("install.uninstall_rules_removed", removedRules, plan.Paths.RulesDir))
	}
	for _, dir := range plan.Dirs {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			continue
		}
		anchor, err := fileAnchor(dir)
		if err == nil {
			_, err = kfs.RemoveEmptyDirectoryIfExists(anchor, dir)
		}
		if err != nil {
			failed = errors.Join(failed, fmt.Errorf("%s: %w", dir, err))
		}
	}
	return failed
}

// disableRulesInjection writes install_rules=false and drops every agent value. A missing
// config file is created from the defaults; welcome_complete stays false.
func disableRulesInjection(file string) error {
	_, statErr := os.Lstat(file)
	created := os.IsNotExist(statErr)
	_, err := config.UpdateAt(file, func(cfg *config.Config) error {
		cfg.InstallRules = false
		config.ClearAgentInstallRules(cfg)
		if created {
			cfg.Language = config.ResolveLanguage()
			cfg.AgentLanguage = config.DefaultAgentLanguage(cfg.Language)
		}
		return nil
	})
	return err
}

func writeUninstallEdit(item UninstallItem) error {
	anchor, err := fileAnchor(item.Path)
	if err != nil {
		return err
	}
	return kfs.WriteBytesAtomicInherited(anchor, item.Path, item.content, true)
}

func removeUninstallFile(path string) error {
	anchor, err := fileAnchor(path)
	if err != nil {
		return err
	}
	_, err = kfs.RemoveNonDirectoryIfExists(anchor, path)
	return err
}

func printUninstallPlan(out io.Writer, plan UninstallPlan) {
	if plan.Paths.Mode == config.ModeProject {
		fmt.Fprintln(out, config.Text("install.uninstall_plan_project", plan.Paths.ProjectRoot))
	} else {
		fmt.Fprintln(out, config.Text("install.uninstall_plan_global"))
	}
	rulesFiles := 0
	for _, item := range plan.Items {
		switch item.Action {
		case UninstallEdit:
			fmt.Fprintln(out, config.Text("install.uninstall_plan_edit", item.Path))
		case UninstallRemove:
			if item.rules {
				rulesFiles++
				continue
			}
			fmt.Fprintln(out, config.Text("install.uninstall_plan_remove", item.Path))
		}
	}
	if rulesFiles > 0 {
		fmt.Fprintln(out, config.Text("install.uninstall_plan_rules", rulesFiles, plan.Paths.RulesDir))
	}
	for _, item := range plan.Items {
		if item.Action == UninstallKeep {
			fmt.Fprintln(out, config.Text("install.uninstall_plan_keep", item.Path, item.Reason))
		}
	}
	fmt.Fprintln(out, config.Text("install.uninstall_plan_config", plan.ConfigFile))
	fmt.Fprintln(out, config.Text("install.uninstall_plan_untouched"))
}
