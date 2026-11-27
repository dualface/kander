package install

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
)

// UninstallAction is what kander uninstall does with one path or process.
type UninstallAction int

const (
	// UninstallEdit rewrites an agent rules file without its Kander load commands.
	UninstallEdit UninstallAction = iota
	// UninstallRemove deletes a file Kander owns.
	UninstallRemove
	// UninstallKeep leaves a path in place and reports why.
	UninstallKeep
	// UninstallStop stops a local background process.
	UninstallStop
	// UninstallSkip leaves an object to its owner: a binary a package manager installed, or a
	// process uninstall cannot stop. The reason says what to do instead.
	UninstallSkip
)

// uninstallActionNames are the action values of the --json result.
var uninstallActionNames = map[UninstallAction]string{
	UninstallEdit: "edit", UninstallRemove: "remove", UninstallKeep: "keep", UninstallStop: "stop", UninstallSkip: "skip",
}

// UninstallItem is one planned path of kander uninstall.
type UninstallItem struct {
	Action UninstallAction
	// Path is the file, or the description of a process.
	Path string
	// Reason explains a kept or skipped object.
	Reason  string
	content []byte
	// rules marks a file of the rules directory, which the plan lists as one count.
	rules       bool
	skillDigest string
	// failed marks a kept path whose inspection failed, so the global injection cleanup
	// cannot tell whether Kander content remains there.
	failed bool
	// process is the process an UninstallStop item stops.
	process *UninstallProcess
}

// UninstallPlan is everything kander uninstall stops, removes, edits, and keeps, computed before
// anything changes.
type UninstallPlan struct {
	Paths config.InstallPaths
	// Processes are the local server and worker, stopped first.
	Processes []UninstallItem
	// Items are the agent files and the rules directory.
	Items []UninstallItem
	// Dirs lists rules and managed skill directories, children before parents, to remove if empty.
	Dirs []string
	// Binary is the global entry, removed last, or why it stays.
	Binary []UninstallItem
	// Kept are the data directories uninstall never deletes.
	Kept []UninstallItem
	// Leases are the unreleased execution credentials of this host.
	Leases []UninstallLease

	clientHome, serverDataDir string
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
	ctx := context.Background()
	plan := PlanUninstall(ctx, paths)
	if command.JSON() {
		return uninstallJSON(ctx, &plan)
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
	failed := ApplyUninstall(ctx, plan, os.Stdout)
	printUninstallKept(os.Stdout, plan)
	if failed != nil {
		fmt.Fprintln(os.Stderr, config.Text("install.uninstall_failed", failed))
		return 1
	}
	return 0
}

// uninstallItem is one planned object in the result of kander uninstall.
type uninstallItem struct {
	Action  string `json:"action"`
	Path    string `json:"path,omitempty"`
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitzero"`
	Reason  string `json:"reason,omitempty"`
}

// uninstallJSON applies the plan without a prompt, which --yes already answered, and returns
// what it covered. A failed step still returns the result, with exit status 1.
func uninstallJSON(ctx context.Context, plan *UninstallPlan) int {
	outcomes, failed := applyUninstall(ctx, *plan, io.Discard)
	var items []uninstallItem
	for _, group := range [][]UninstallItem{plan.Processes, plan.Items, plan.Binary, outcomes, plan.Kept} {
		for _, item := range group {
			entry := uninstallItem{Action: uninstallActionNames[item.Action], Path: item.Path, Reason: item.Reason}
			if item.process != nil {
				entry.Path, entry.Process, entry.PID = "", item.Path, item.process.PID
			}
			items = append(items, entry)
		}
	}
	var failures []string
	status := 0
	if failed != nil {
		failures, status = strings.Split(failed.Error(), "\n"), 1
	}
	return command.ResultStatus(struct {
		Mode     string           `json:"mode"`
		RulesDir string           `json:"rules_dir"`
		Items    []uninstallItem  `json:"items"`
		Leases   []UninstallLease `json:"leases"`
		Failures []string         `json:"failures"`
	}{"global", plan.Paths.RulesDir, items, nonNil(plan.Leases), nonNil(failures)}, status)
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
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
func PlanUninstall(ctx context.Context, paths config.InstallPaths) UninstallPlan {
	plan := UninstallPlan{Paths: paths}
	plan.planBackground(ctx)
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
	plan.Binary = planBinaryRemoval(paths)
	plan.Kept = plan.keptDirectories()
	return plan
}

// keptDirectories lists the data directories uninstall keeps: the configuration directory, the
// share directory, the client home when it lives elsewhere, and the server data directory.
func (plan UninstallPlan) keptDirectories() []UninstallItem {
	var kept []UninstallItem
	if plan.Paths.ConfigPath != "" {
		kept = append(kept, UninstallItem{Action: UninstallKeep, Path: filepath.Dir(plan.Paths.ConfigPath), Reason: config.Text("install.uninstall_keep_config")})
	}
	if plan.Paths.ShareDir != "" {
		kept = append(kept, UninstallItem{Action: UninstallKeep, Path: plan.Paths.ShareDir, Reason: config.Text("install.uninstall_keep_share")})
	}
	if plan.clientHome != "" && (plan.Paths.ShareDir == "" || !within(plan.clientHome, plan.Paths.ShareDir)) {
		kept = append(kept, UninstallItem{Action: UninstallKeep, Path: plan.clientHome, Reason: config.Text("install.uninstall_keep_client")})
	}
	if plan.serverDataDir != "" {
		kept = append(kept, UninstallItem{Action: UninstallKeep, Path: plan.serverDataDir, Reason: config.Text("install.uninstall_keep_server")})
	}
	return kept
}

// within reports whether path is dir or lies below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// uninstallStopBudget bounds the stop of every process together.
const uninstallStopBudget = 2 * time.Minute

// ApplyUninstall stops the local processes, then edits and removes the agent files and the rules
// directory, removes empty managed directories, and deletes the global entry last. A failed step
// is reported and the others go on; the joined failures are returned. A failed skill removal
// keeps the binary, so a later run can finish the skill bundle.
func ApplyUninstall(ctx context.Context, plan UninstallPlan, out io.Writer) error {
	_, err := applyUninstall(ctx, plan, out)
	return err
}

// applyUninstall is ApplyUninstall that also returns the outcomes the plan could not name in
// advance: the global entry renamed aside on Windows, deleted by a helper or left behind.
func applyUninstall(ctx context.Context, plan UninstallPlan, out io.Writer) ([]UninstallItem, error) {
	// A user can edit a skill while the confirmation prompt is open. Recheck
	// the whole preview before removing any of its interdependent documents.
	for _, item := range plan.Items {
		if item.skillDigest != "" {
			if err := verifySkillRemoval(item); err != nil {
				return nil, err
			}
		}
	}
	stopCtx, cancel := context.WithTimeout(ctx, uninstallStopBudget)
	failed := stopProcesses(stopCtx, plan.Processes, out)
	cancel()
	removedRules, _, err := applyUninstallItems(plan.Items, out)
	if err != nil && errors.Is(err, errSkillRemoval) {
		return nil, errors.Join(failed, err)
	}
	failed = errors.Join(failed, err)
	if removedRules > 0 {
		fmt.Fprintln(out, config.Text("install.uninstall_rules_removed", removedRules, plan.Paths.RulesDir))
	}
	failed = errors.Join(failed, removeEmptyDirs(plan.Dirs))
	var outcomes []UninstallItem
	for _, item := range plan.Binary {
		if item.Action != UninstallRemove {
			continue
		}
		outcome, err := removeBinary(item, out)
		if err != nil {
			failed = joinFailure(failed, item.Path, err)
		}
		if outcome != nil {
			outcomes = append(outcomes, *outcome)
		}
	}
	return outcomes, failed
}

func printUninstallPlan(out io.Writer, plan UninstallPlan) {
	fmt.Fprintln(out, config.Text("install.uninstall_plan_global"))
	for _, item := range plan.Processes {
		if item.Action == UninstallStop {
			fmt.Fprintln(out, config.Text("install.uninstall_plan_stop", item.Path))
		}
	}
	rulesFiles := 0
	for _, item := range append(append([]UninstallItem{}, plan.Items...), plan.Binary...) {
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
	for _, group := range [][]UninstallItem{plan.Processes, plan.Binary} {
		for _, item := range group {
			if item.Action == UninstallSkip {
				fmt.Fprintln(out, config.Text("install.uninstall_plan_skip", item.Path, item.Reason))
			}
		}
	}
	for _, group := range [][]UninstallItem{plan.Items, plan.Binary, plan.Kept} {
		for _, item := range group {
			if item.Action == UninstallKeep {
				fmt.Fprintln(out, config.Text("install.uninstall_plan_keep", item.Path, item.Reason))
			}
		}
	}
	printLeases(out, plan.Leases)
	fmt.Fprintln(out, config.Text("install.uninstall_kept_repositories"))
}

// printUninstallKept ends the output with what stays: the data directories, and what kander
// init and the task flow committed or created in the repositories.
func printUninstallKept(out io.Writer, plan UninstallPlan) {
	fmt.Fprintln(out, config.Text("install.uninstall_kept"))
	for _, item := range plan.Kept {
		fmt.Fprintln(out, config.Text("install.uninstall_plan_keep", item.Path, item.Reason))
	}
	fmt.Fprintln(out, config.Text("install.uninstall_kept_repositories"))
}
