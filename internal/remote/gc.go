package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/fs"
)

// GCOptions selects one pass of the client gc of one project. A dry run only
// plans: it runs no git fetch, changes no ref, object or worktree, and writes
// nothing below the client directory except lock files.
type GCOptions struct {
	DryRun    bool
	OlderThan time.Duration
	// Now is the time the retention period counts back from.
	Now time.Time
	// Probe classifies the retained executions of a worktree. A nil probe
	// classifies every recorded executor as unknown, which keeps its worktree.
	Probe ExecutorProbe
	// Mainline is the integration target of a card without INTEGRATION_TARGET.
	Mainline string
	// Checkout is a clone of this project the command runs in, or empty. When
	// set, its common Git directory must be the one of every worktree.
	Checkout string
}

// ExecutorProbe classifies each retained execution, in input order, with the
// liveness status words: only "stopped" lets a worktree go.
type ExecutorProbe func(ctx context.Context, executions []RetainedExecution) []string

// GCProject is the plan or the outcome of the client gc of one project.
// Errors lists the failures that stopped a category or an entry; they make the
// command exit 1 like the kept entries do.
type GCProject struct {
	Name          string     `json:"name"`
	ID            string     `json:"id"`
	Worktrees     GCCategory `json:"worktrees"`
	Branches      GCCategory `json:"branches"`
	ReviewRuntime GCCategory `json:"review_runtime"`
	Journal       GCCategory `json:"journal"`
	EvidenceRefs  GCCategory `json:"evidence_refs"`
	// TrackingRefs are the local tracking refs of evidence refs the common
	// remote no longer holds.
	TrackingRefs GCCategory `json:"tracking_refs"`
	// Skipped says why a category did not run; it does not change the exit
	// status.
	Skipped []string `json:"skipped,omitempty"`
	Errors  []string `json:"errors,omitempty"`
	// work marks a plan with work no category counts: an emptied task
	// directory to remove, or an interrupted removal to finish.
	work bool
}

// GCCategory counts the entries removed (or, in a dry run, to remove) and
// their bytes. Kept lists the candidates that stayed, with the reason.
// Deferred lists the worktrees a dry run could not judge without a fetch;
// they are not counted and do not change the exit status.
type GCCategory struct {
	Count    int       `json:"count"`
	Bytes    int64     `json:"bytes"`
	Kept     []GCEntry `json:"kept,omitempty"`
	Deferred []GCEntry `json:"deferred,omitempty"`
}

// GCEntry names one entry of a category and why it was kept or deferred.
type GCEntry struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func (c *GCCategory) add(bytes int64) { c.Count, c.Bytes = c.Count+1, c.Bytes+bytes }
func (c *GCCategory) keep(id, reason string) {
	c.Kept = append(c.Kept, GCEntry{ID: id, Reason: reason})
}

// Incomplete reports a kept candidate or an error, which makes the command
// exit 1.
func (p GCProject) Incomplete() bool {
	if len(p.Errors) > 0 {
		return true
	}
	for _, c := range p.categories() {
		if len(c.Kept) > 0 {
			return true
		}
	}
	return false
}

// Empty reports a project with nothing to do: nothing to remove, no worktree
// deferred to the fetch of a run, and no other work.
func (p GCProject) Empty() bool {
	if p.work || len(p.Worktrees.Deferred) > 0 {
		return false
	}
	for _, c := range p.categories() {
		if c.Count > 0 {
			return false
		}
	}
	return true
}

func (p GCProject) categories() []GCCategory {
	return []GCCategory{p.Worktrees, p.Branches, p.ReviewRuntime, p.Journal, p.EvidenceRefs, p.TrackingRefs}
}

// gcTerminalState reports the card states whose local data a gc may remove.
func gcTerminalState(state string) bool {
	return state == "done" || state == "archived" || state == "trash"
}

// gcRun is one pass over one project: the options, the board read once at
// the start, and the report the categories fill in.
type gcRun struct {
	s      *Session
	opts   GCOptions
	root   string
	cutoff time.Time
	tasks  map[string]api.TaskSummary
	report *GCProject
	// work is set when the pass finds work no category counts.
	work bool
	// repository is the first main repository a worktree of this project
	// named; the evidence category uses it when no checkout was given.
	repository string
}

func (r *gcRun) fail(err error) { r.report.Errors = append(r.report.Errors, err.Error()) }

// gcNoLazyFetch keeps a Git command from fetching missing objects of a partial
// clone from its promisor remote, so a dry run writes no object.
var gcNoLazyFetch = []string{"GIT_NO_LAZY_FETCH=1"}

// git runs one Git command of the pass; a dry run never fetches lazily.
func (r *gcRun) git(ctx context.Context, dir string, args ...string) (string, error) {
	if r.opts.DryRun {
		return gitCommandEnv(ctx, dir, gcNoLazyFetch, args...)
	}
	return gitCommand(ctx, dir, args...)
}

// ancestor reports whether source is an ancestor of target, like git; a dry
// run never fetches lazily.
func (r *gcRun) ancestor(ctx context.Context, dir, source, target string) (bool, error) {
	if r.opts.DryRun {
		return gitAncestorEnv(ctx, dir, gcNoLazyFetch, source, target)
	}
	return gitAncestor(ctx, dir, source, target)
}

// CollectGarbage plans (opts.DryRun) or runs the client gc of this project.
// The local categories need the project's client directory; the evidence refs
// need only a checkout.
// The worktree category runs before the evidence refs, so a worktree whose
// branch tip only an evidence ref holds is judged while the ref still exists.
func (s *Session) CollectGarbage(ctx context.Context, opts GCOptions) GCProject {
	report := GCProject{Name: s.Info.Name, ID: s.Info.ID}
	root := filepath.Join(s.Home, "projects", projectNamespace(s.ServerURL, s.Info.ID))
	exists, err := fs.DirectoryExists(s.Home, root)
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report
	}
	list, err := s.Project.List(ctx, "")
	if err != nil {
		report.Errors = append(report.Errors, err.Error())
		return report
	}
	r := &gcRun{s: s, opts: opts, root: root, cutoff: opts.Now.Add(-opts.OlderThan), tasks: map[string]api.TaskSummary{}, report: &report}
	for _, task := range list.Tasks {
		r.tasks[task.TaskID] = task
	}
	// Without a client project directory there is no local data, but the
	// common remote can still hold evidence refs of the project.
	if exists {
		r.collectWorktrees(ctx)
		r.collectReviewRuntime(ctx)
		r.collectJournal()
	}
	r.collectEvidenceRefs(ctx)
	report.work = r.work
	return report
}

// terminalTask returns the list row of a card in a terminal state.
func (r *gcRun) terminalTask(id string) (api.TaskSummary, bool) {
	task, found := r.tasks[id]
	return task, found && gcTerminalState(task.State)
}

// cardTime parses a card timestamp, which the board writes in local time.
func cardTime(value string) (time.Time, bool) {
	at, err := time.ParseInLocation("2006-01-02 15:04", value, time.Local)
	return at, err == nil
}

// tryLock takes an exclusive lock without waiting. A dry run never creates
// the lock file: a missing one is held by nobody. held is false when another
// process holds the lock; release is then nil.
func (r *gcRun) tryLock(path string) (release func() error, held bool, err error) {
	var file *os.File
	if r.opts.DryRun {
		file, err = fs.OpenExistingLockFile(r.s.Home, path)
		if err != nil || file == nil {
			return func() error { return nil }, true, err
		}
	} else {
		if err := fs.EnsurePrivateDirectory(r.s.Home, filepath.Dir(path), true); err != nil {
			return nil, false, err
		}
		if file, err = fs.OpenLockFile(r.s.Home, path); err != nil {
			return nil, false, err
		}
	}
	lock, held, err := fs.TryLockExclusive(file)
	if err != nil || !held {
		return nil, false, errors.Join(err, file.Close())
	}
	return func() error { return errors.Join(lock.Unlock(), file.Close()) }, true, nil
}

// treeSize sums the regular files below path without following links.
func treeSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// removeTree removes a directory of regular files and directories below root
// without following links; anything else stops it.
func removeTree(root, path string) error {
	entries, err := fs.ListDirectory(root, path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name)
		switch entry.Kind {
		case fs.KindFile:
			_, err = fs.RemoveRegularFileIfExists(root, child)
		case fs.KindDirectory:
			err = removeTree(root, child)
		default:
			err = errors.New(child + ": not a regular file or directory")
		}
		if err != nil {
			return err
		}
	}
	_, err = fs.RemoveEmptyDirectoryIfExists(root, path)
	return err
}
