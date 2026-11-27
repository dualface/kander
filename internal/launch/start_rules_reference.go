package launch

import (
	"context"
	"os/exec"
	"strings"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/install"
)

// CodeRulesReferenceMissing is the error code of a start refused because the start base lacks
// the project reference to the Kander rules entry.
const CodeRulesReferenceMissing = "rules_reference_missing"

// noticeRulesCheckSkipped is the notice code of a start whose agent the reference check does
// not apply to.
const noticeRulesCheckSkipped = "project_rules_check_skipped"

// startReferenceCheck checks, before the claim, that the commit a new execution starts from
// carries the project reference the executing agent loads the Kander rules through. Without
// it the agent would run in a task worktree without the rules. It reports a skipped check
// once, however often the start base is resolved again.
type startReferenceCheck struct {
	cwd     string
	agent   string
	skipped bool
}

// check runs the check for commit, the start base on branch; an empty commit is the local
// HEAD of a project without a registered remote.
func (c *startReferenceCheck) check(ctx context.Context, commit, branch string) error {
	if c.skipped {
		return nil
	}
	main, err := config.MainWorktree(c.cwd)
	if err != nil {
		return err
	}
	if main == "" {
		return nil
	}
	paths, err := config.GlobalInstallPaths()
	if err != nil {
		return err
	}
	switches, err := config.ScopeInstallRules(paths)
	if err != nil {
		return err
	}
	rel, applicable := install.ProjectRulesApplicable(c.agent, main, switches)
	if !applicable {
		c.skipped = true
		reason := config.Text("projectrules.start_skip_switch_off")
		if rel == "" {
			reason = config.Text("projectrules.start_skip_no_rules_file")
		}
		command.Notify(noticeRulesCheckSkipped, config.Text("projectrules.start_check_skipped", c.agent, reason))
		return nil
	}
	if commit == "" {
		head, err := gitTreeOutput(ctx, c.cwd, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			return err
		}
		commit, branch = strings.TrimSpace(string(head)), "HEAD"
	}
	missing, err := install.MissingProjectReferenceInTree(c.agent, paths, main, commitTreeReader(ctx, c.cwd, commit))
	if err != nil {
		return err
	}
	if missing == "" {
		return nil
	}
	return command.Coded(CodeRulesReferenceMissing, launchError("projectrules.start_reference_missing", missing, branch, commit))
}

// commitTreeReader reads paths of commit, relative to its root whatever directory of the
// checkout dir names, from the repository of dir.
func commitTreeReader(ctx context.Context, dir, commit string) install.TreeReader {
	return func(rel string) (install.TreeFile, error) {
		out, err := gitTreeOutput(ctx, dir, "ls-tree", "--full-tree", "-z", commit, "--", rel)
		if err != nil {
			return install.TreeFile{}, err
		}
		meta, name, ok := strings.Cut(strings.TrimSuffix(string(out), "\x00"), "\t")
		fields := strings.Fields(meta)
		if !ok || name != rel || len(fields) != 3 || fields[1] != "blob" {
			return install.TreeFile{}, nil
		}
		data, err := gitTreeOutput(ctx, dir, "cat-file", "blob", fields[2])
		if err != nil {
			return install.TreeFile{}, err
		}
		return install.TreeFile{Data: data, Symlink: fields[0] == "120000", Found: true}, nil
	}
}

// gitTreeOutput runs a read-only Git command in dir. Paths are literal: a name read from a
// rules file is never a pattern.
func gitTreeOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--literal-pathspecs", "-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) > 0 {
			return nil, launchError("launch.value", strings.Join(args[:1], " "), strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, err
	}
	return out, nil
}
