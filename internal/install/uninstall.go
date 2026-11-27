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

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	kfs "github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/rules"
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
	rules       bool
	skillDigest string
	// failed marks a kept path whose inspection failed, so the global injection cleanup
	// cannot tell whether Kander content remains there.
	failed bool
}

// UninstallPlan is everything kander uninstall removes or edits in one scope, computed
// before anything changes.
type UninstallPlan struct {
	Paths config.InstallPaths
	// ConfigFile receives install_rules=false so a later doctor repair writes nothing.
	ConfigFile string
	Items      []UninstallItem
	// Dirs lists rules and managed skill directories, children before parents, to remove if empty.
	Dirs []string
}

var (
	uninstallPromptIn  io.Reader = os.Stdin
	uninstallPromptOut io.Writer = os.Stderr
	uninstallTerminal            = func() bool { return isTTY(os.Stdin) && isTTY(os.Stderr) }
)

// NewUninstallCommand builds kander uninstall.
func NewUninstallCommand() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:  "uninstall",
		Args: command.Args(0, 0),
		RunE: func(*cobra.Command, []string) error { return command.Exit(runUninstall(yes)) },
	}
	c.Flags().BoolVar(&yes, "yes", false, "")
	return c
}

func runUninstall(yes bool) int {
	if command.JSON() && !yes {
		return command.FailStatus(2, command.Interactive(config.Text("install.uninstall_requires_yes")))
	}
	paths, err := config.CurrentInstallPaths()
	if err != nil {
		return command.Fail(err)
	}
	plan := PlanUninstall(paths)
	if command.JSON() {
		return uninstallJSON(plan)
	}
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
		return command.Fail(err)
	}
	return 0
}

// uninstallItem is one planned path in the result of kander uninstall.
type uninstallItem struct {
	Action string `json:"action"`
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

// uninstallJSON applies the plan without a prompt, which --yes already answered, and returns
// what changed.
func uninstallJSON(plan UninstallPlan) int {
	if err := ApplyUninstall(plan, io.Discard); err != nil {
		return command.Fail(err)
	}
	actions := map[UninstallAction]string{UninstallEdit: "edit", UninstallRemove: "remove", UninstallKeep: "keep"}
	items := make([]uninstallItem, 0, len(plan.Items))
	for _, item := range plan.Items {
		items = append(items, uninstallItem{Action: actions[item.Action], Path: item.Path, Reason: item.Reason})
	}
	return command.Result(struct {
		Mode       string          `json:"mode"`
		ConfigFile string          `json:"config_file"`
		RulesDir   string          `json:"rules_dir"`
		Items      []uninstallItem `json:"items"`
	}{"global", plan.ConfigFile, plan.Paths.RulesDir, items})
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

// PlanUninstall lists what kander uninstall would change, without writing. Every embedded
// agent's global rules file is covered whatever its switch, so references written by an
// earlier release go too; it is the same plan the global injection cleanup applies.
func PlanUninstall(paths config.InstallPaths) UninstallPlan {
	plan := UninstallPlan{Paths: paths, ConfigFile: config.ScopeConfigFile(paths)}
	plan.Items = append(plan.Items, planGlobalInjectionCleanup(paths).Items...)
	for _, agent := range config.ExecutionAgents {
		if item, ok := planExtensionRemoval(agent, paths); ok {
			plan.Items = append(plan.Items, item)
		}
	}
	items, dirs := planRulesDirRemoval(paths.RulesDir)
	plan.Items = append(plan.Items, items...)
	plan.Dirs = dirs
	var previousSkillDir string
	for _, agent := range []string{"codex", "claude"} {
		dir, err := leaderTarget(agent, paths)
		if err != nil {
			plan.Items = append(plan.Items, UninstallItem{Action: UninstallKeep, Path: agent, Reason: err.Error()})
			continue
		}
		if dir == "" || sameSkillDirectory(previousSkillDir, dir) {
			continue
		}
		previousSkillDir = dir
		items, dirs := planLeaderRemoval(dir, paths)
		plan.Items = append(plan.Items, items...)
		plan.Dirs = append(plan.Dirs, dirs...)
	}
	return plan
}

// planReferenceRemoval plans dropping the Kander load commands from one global rules file. A
// file left with only blank lines, or a .mdc rule left with only the alwaysApply frontmatter
// Kander wrote, is deleted; an AGENTS.override.md sibling and a symlinked rules file are only
// edited, and a .mdc rule the user changed keeps its other content. A rules file that is a
// symlink to the Kander rules entry, current or previous, is removed as a link.
func planReferenceRemoval(target string, override bool, paths config.InstallPaths) (UninstallItem, bool) {
	keep := func(reason string) (UninstallItem, bool) {
		return UninstallItem{Action: UninstallKeep, Path: target, Reason: reason}, true
	}
	fail := func(err error) (UninstallItem, bool) {
		return UninstallItem{Action: UninstallKeep, Path: target, Reason: err.Error(), failed: true}, true
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return UninstallItem{}, false
	}
	if err != nil {
		return fail(err)
	}
	linked := info.Mode()&os.ModeSymlink != 0
	if linked && linksToRulesEntry(target, paths) {
		// The link itself goes, never its target; it is removed below the resolved directory
		// so the removal does not walk through a symlinked config directory.
		dir, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil {
			return fail(err)
		}
		return UninstallItem{Action: UninstallRemove, Path: filepath.Join(dir, filepath.Base(target))}, true
	}
	format := fileFormatFor(target)
	if key := format.SymlinkRefusal(); linked && key != "" {
		return keep(config.Text(key, target))
	}
	// The write goes to the real path, so a config directory the user keeps behind a symlink
	// is edited in place.
	resolved, err := realRulesPath(target, linked)
	if err != nil {
		return fail(err)
	}
	if info, err := os.Stat(resolved); err != nil {
		return fail(err)
	} else if !info.Mode().IsRegular() {
		return fail(fmt.Errorf("%s", config.Text("menu.cannot_read", target)))
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fail(err)
	}
	stripped, removed := RemoveRulesReferences(string(data))
	if removed == 0 {
		return UninstallItem{}, false
	}
	rest := strings.TrimSpace(stripped)
	kanderOnly := rest == "" || format.OnlyKander(rest)
	// Deletion never follows a symlink: a file whose directory is reached through one is
	// emptied of the Kander lines instead.
	if kanderOnly && !linked && !override && resolved == filepath.Clean(target) {
		return UninstallItem{Action: UninstallRemove, Path: target}, true
	}
	item := UninstallItem{Action: UninstallEdit, Path: resolved, content: []byte(stripped)}
	if format.Prefix() != "" && !kanderOnly {
		// A rule file Kander created and the user changed afterwards stays without the
		// Kander load command; it is reported as kept.
		item.Reason = config.Text("install.global_rules_file_modified_kept")
	}
	return item, true
}

// linksToRulesEntry reports whether target is a symlink to the Kander rules entry: the current
// one, or the previous global entry, whose link may already dangle.
func linksToRulesEntry(target string, paths config.InstallPaths) bool {
	link, err := os.Readlink(target)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(target), link)
	}
	link = filepath.Clean(link)
	entries := []string{RulesEntry(paths)}
	if legacy := legacyRulesEntry(paths); legacy != "" {
		entries = append(entries, legacy)
	}
	for _, entry := range entries {
		if link == filepath.Clean(entry) || sameResolved(target, entry) {
			return true
		}
	}
	return false
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
	known := map[string]struct{}{stateFileName: {}, startupStateFileName: {}, globalCleanupFileName: {}, globalCleanupLockName: {}}
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

// ApplyUninstall rechecks the skill preview and turns off the scope's switch
// before editing or removing files. It then removes empty managed directories.
func ApplyUninstall(plan UninstallPlan, out io.Writer) error {
	// A user can edit a skill while the confirmation prompt is open. Recheck
	// the whole preview before removing any of its interdependent documents.
	for _, item := range plan.Items {
		if item.skillDigest != "" {
			if err := verifySkillRemoval(item); err != nil {
				return err
			}
		}
	}
	if err := disableRulesInjection(plan.ConfigFile); err != nil {
		return err
	}
	fmt.Fprintln(out, config.Text("install.uninstall_config_written", plan.ConfigFile))
	removedRules, _, failed := applyUninstallItems(plan.Items, out)
	if failed != nil && errors.Is(failed, errSkillRemoval) {
		return failed
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

// errSkillRemoval marks a failed removal of a bundled skill file, which stops the uninstall so
// the receipt stays and a later run can finish the bundle.
var errSkillRemoval = errors.New("skill removal failed")

// applyUninstallItems edits and removes the planned paths, reporting each to out, and returns
// the number of removed rules directory files, the paths it changed, and every failure joined.
func applyUninstallItems(items []UninstallItem, out io.Writer) (int, []string, error) {
	var failed error
	var changed []string
	removedRules := 0
	for _, item := range items {
		var err error
		switch item.Action {
		case UninstallEdit:
			err = writeUninstallEdit(item)
			if err == nil {
				fmt.Fprintln(out, config.Text("install.uninstall_edited", item.Path))
			}
		case UninstallRemove:
			if item.skillDigest != "" {
				if err = removeSkillFile(item); err != nil {
					// Keep the receipt so a later run can finish this bundle.
					return removedRules, changed, errors.Join(failed, errSkillRemoval, err)
				}
			} else {
				err = removeUninstallFile(item.Path)
			}
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
			continue
		}
		changed = append(changed, item.Path)
	}
	return removedRules, changed, failed
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
	fmt.Fprintln(out, config.Text("install.uninstall_plan_global"))
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
