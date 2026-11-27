package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// gcRemoval is the record a run writes before it removes a generation's
// worktree, so a run interrupted between the worktree and its task branch is
// finished by the next one: the branch goes only while it still has the
// recorded tip, and the intent record goes last.
type gcRemoval struct {
	Schema      int    `json:"schema"`
	Repository  string `json:"repository"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Tip         string `json:"tip"`
	ExecutionID string `json:"execution_id"`
	Generation  uint64 `json:"generation"`
}

func (r *gcRun) removalPath(task string, generation uint64) string {
	return filepath.Join(r.root, "gc-removals", task, fmt.Sprintf("g%d.json", generation))
}

// removeWorktree records the removal, removes the registered worktree, then
// finishes the removal: the task branch, the intent record and the removal
// record. A Git error with the worktree directory gone counts as removed; with
// the directory still there it drops the record again, keeps the branch and
// the intent, and returns false; when the directory cannot be checked the
// record stays for the next run.
func (r *gcRun) removeWorktree(ctx context.Context, w gcWorktree, repo string, repos map[string]*gcRepository) bool {
	tip, err := r.git(ctx, repo, "rev-parse", "--verify", "refs/heads/"+w.intent.Branch+"^{commit}")
	if err != nil {
		r.report.Worktrees.keep(w.id, err.Error())
		return false
	}
	record := gcRemoval{Schema: 1, Repository: repo, Path: w.intent.Path, Branch: w.intent.Branch, Tip: tip, ExecutionID: w.intent.ExecutionID, Generation: w.intent.Generation}
	path := r.removalPath(w.task, w.intent.Generation)
	data, _ := json.Marshal(record)
	err = fs.EnsurePrivateDirectory(r.s.Home, filepath.Dir(path), true)
	if err == nil {
		err = fs.WriteTextAtomic(r.s.Home, path, string(data)+"\n", true)
	}
	if err != nil {
		r.report.Worktrees.keep(w.id, err.Error())
		return false
	}
	repos[repo].pruned = true
	if _, err := r.git(ctx, repo, "worktree", "remove", w.intent.Path); err != nil {
		// A Git error does not prove the worktree is still there: the removal
		// may have finished before the process was reaped. Look again under the
		// same lock; when that cannot settle it, the record stays and the next
		// run looks under the generation's lock.
		exists, statErr := fs.DirectoryExists(r.s.Home, w.intent.Path)
		switch {
		case statErr == nil && !exists:
		case statErr == nil:
			r.report.Worktrees.keep(w.id, err.Error())
			if _, rmErr := fs.RemoveRegularFileIfExists(r.s.Home, path); rmErr != nil {
				r.fail(rmErr)
			}
			return false
		default:
			r.report.Worktrees.keep(w.id, errors.Join(err, statErr).Error())
			return false
		}
	}
	r.finishRemoval(ctx, w.task, record, path)
	return true
}

// finishRemoval deletes the task branch while it still has the recorded tip,
// then the intent record and the removal record. A branch that moved stays and
// is reported; a failed deletion keeps both records for the next run.
func (r *gcRun) finishRemoval(ctx context.Context, task string, record gcRemoval, path string) {
	ref := "refs/heads/" + record.Branch
	current, err := r.git(ctx, record.Repository, "for-each-ref", "--format=%(objectname)", ref)
	switch {
	case err != nil:
		r.report.Branches.keep(record.Branch, err.Error())
		return
	case current == "":
		// An interrupted run already deleted it.
	case current != record.Tip:
		r.report.Branches.keep(record.Branch, config.Text("gc.client_branch_moved", record.Tip, current))
	case r.opts.DryRun:
		r.report.Branches.add(0)
	default:
		if _, err := r.git(ctx, record.Repository, "update-ref", "-d", ref, record.Tip); err != nil {
			r.report.Branches.keep(record.Branch, err.Error())
			return
		}
		r.report.Branches.add(0)
	}
	if r.opts.DryRun {
		return
	}
	intent := filepath.Join(r.root, "worktree-intents", task, fmt.Sprintf("g%d.json", record.Generation))
	if _, err := fs.RemoveRegularFileIfExists(r.s.Home, intent); err != nil {
		r.fail(err)
		return
	}
	if _, err := fs.RemoveRegularFileIfExists(r.s.Home, path); err != nil {
		r.fail(err)
	}
}

// resumeRemovals finishes the removals an interrupted run recorded. A record
// whose worktree directory still exists was interrupted before the worktree
// went; it is dropped and the worktree is judged again like any other.
func (r *gcRun) resumeRemovals(ctx context.Context, repos map[string]*gcRepository) {
	dir := filepath.Join(r.root, "gc-removals")
	for _, task := range r.listTaskDirectories(dir) {
		entries, err := fs.ListDirectory(r.s.Home, filepath.Join(dir, task))
		if err != nil {
			r.fail(err)
			continue
		}
		for _, entry := range entries {
			generation, ok := intentGeneration(entry)
			if ok {
				r.resumeRemoval(ctx, task, generation, filepath.Join(dir, task, entry.Name), repos)
			}
		}
		if !r.opts.DryRun {
			r.removeEmptyDirectory(filepath.Join(dir, task))
		}
	}
}

func (r *gcRun) resumeRemoval(ctx context.Context, task string, generation uint64, path string, repos map[string]*gcRepository) {
	id := fmt.Sprintf("%s/g%d", task, generation)
	var record gcRemoval
	data, err := fs.ReadRegularFile(r.s.Home, path)
	if err == nil {
		err = json.Unmarshal(data, &record)
	}
	expected := filepath.Join(r.root, "worktrees", task, fmt.Sprintf("g%d", generation))
	if err != nil || record.Schema != 1 || record.Generation != generation || filepath.Clean(record.Path) != expected ||
		!api.KnownExecutionBranch(record.Branch, r.s.Info.ID, task, generation) || !api.CommitSHA(record.Tip) ||
		!api.ValidIdentity(record.ExecutionID) || !filepath.IsAbs(record.Repository) {
		r.report.Worktrees.keep(id, config.Text("gc.client_removal_damaged", path))
		return
	}
	lockPath := filepath.Join(credentialDir(r.s.Home, r.s.ServerURL, r.s.Info.ID, task), "worktree-"+record.ExecutionID+".lock")
	release, held, err := r.tryLock(lockPath)
	if err != nil {
		r.report.Worktrees.keep(id, err.Error())
		return
	}
	if !held {
		r.report.Worktrees.keep(id, config.Text("gc.client_locked", lockPath))
		return
	}
	defer func() {
		if err := release(); err != nil {
			r.fail(err)
		}
	}()
	exists, err := fs.DirectoryExists(r.s.Home, expected)
	if err != nil {
		r.report.Worktrees.keep(id, err.Error())
		return
	}
	if exists {
		if !r.opts.DryRun {
			if _, err := fs.RemoveRegularFileIfExists(r.s.Home, path); err != nil {
				r.fail(err)
			}
		}
		return
	}
	if _, err := r.git(ctx, record.Repository, "rev-parse", "--git-common-dir"); err != nil {
		r.report.Branches.keep(record.Branch, err.Error())
		return
	}
	r.work = true
	if !r.opts.DryRun {
		cache, found := repos[record.Repository]
		if !found {
			cache = &gcRepository{targets: map[string]gcTarget{}}
			repos[record.Repository] = cache
		}
		cache.pruned = true
	}
	r.finishRemoval(ctx, task, record, path)
}
