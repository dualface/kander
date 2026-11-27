package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// collectReviewRuntime removes the local runtime directory of each review run
// the server reports finalized or abandoned, or no longer knows. A run the
// server no longer knows keeps its directory while a pending finalization is
// recorded there: that finalization never reached the server.
func (r *gcRun) collectReviewRuntime(ctx context.Context) {
	dir := filepath.Join(r.root, "review-runtime")
	entries, err := fs.ListDirectory(r.s.Home, dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		r.fail(err)
		return
	}
	for _, entry := range entries {
		if entry.Kind != fs.KindDirectory || !board.ValidReviewID(entry.Name) {
			continue
		}
		run := filepath.Join(dir, entry.Name)
		pending, err := fs.RegularFileExists(r.s.Home, filepath.Join(run, "pending-finalization.json"))
		if err != nil {
			r.report.ReviewRuntime.keep(entry.Name, err.Error())
			continue
		}
		raw, err := r.s.Project.ReviewResource(ctx, "runs", entry.Name)
		switch {
		case api.IsCode(err, api.CodeNotFound):
			if pending {
				r.report.ReviewRuntime.keep(entry.Name, config.Text("gc.client_runtime_pending"))
				continue
			}
		case err != nil:
			r.fail(fmt.Errorf("%s: %w", entry.Name, err))
			continue
		default:
			var value board.ReviewRun
			if err := json.Unmarshal(raw, &value); err != nil {
				r.fail(fmt.Errorf("%s: %w", entry.Name, err))
				continue
			}
			if value.Phase != "finalized" && value.Abandonment == nil {
				continue
			}
		}
		r.removeRuntime(entry.Name, run)
	}
}

// removeRuntime removes one run directory under its runtime lock. The lock
// file goes last, after the lock is released, so the removal also works where
// an open file cannot be deleted.
func (r *gcRun) removeRuntime(id, dir string) {
	lockPath := filepath.Join(dir, "runtime.lock")
	release, held, err := r.tryLock(lockPath)
	if err != nil {
		r.report.ReviewRuntime.keep(id, err.Error())
		return
	}
	if !held {
		r.report.ReviewRuntime.keep(id, config.Text("gc.client_locked", lockPath))
		return
	}
	size := treeSize(dir)
	if r.opts.DryRun {
		r.report.ReviewRuntime.add(size)
		r.releaseLock(release)
		return
	}
	err = r.removeRuntimeContents(dir)
	r.releaseLock(release)
	if err == nil {
		_, err = fs.RemoveRegularFileIfExists(r.s.Home, lockPath)
	}
	if err == nil {
		_, err = fs.RemoveEmptyDirectoryIfExists(r.s.Home, dir)
	}
	if err != nil {
		r.report.ReviewRuntime.keep(id, config.Text("gc.client_remove_failed", err.Error()))
		return
	}
	r.report.ReviewRuntime.add(size)
}

func (r *gcRun) removeRuntimeContents(dir string) error {
	entries, err := fs.ListDirectory(r.s.Home, dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name)
		switch {
		case entry.Name == "runtime.lock":
		case entry.Kind == fs.KindFile:
			_, err = fs.RemoveRegularFileIfExists(r.s.Home, path)
		case entry.Kind == fs.KindDirectory:
			err = removeTree(r.s.Home, path)
		default:
			err = errors.New(path + ": not a regular file or directory")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *gcRun) releaseLock(release func() error) {
	if err := release(); err != nil {
		r.fail(err)
	}
}

// gcJournals are the private journals whose records carry a terminal phase
// and the card they belong to. The other journals (worktree intents, review
// operations and material, continuations) have no such mark; worktree intents
// go with their worktree.
var gcJournals = []struct {
	dir      string
	terminal []string
}{
	{"launch-actions", []string{"complete"}},
	{"request-calls", []string{"complete", "failed"}},
}

// gcJournalRecord is what the gc reads of a journal record.
type gcJournalRecord struct {
	Schema    int       `json:"schema"`
	Phase     string    `json:"phase"`
	TaskID    string    `json:"task_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// collectJournal removes the journal records that reached a terminal phase,
// belong to a terminal card and are older than the retention period. A record
// goes under the lock its writer takes; the empty lock file stays, because
// removing it could let two writers lock different files.
func (r *gcRun) collectJournal() {
	for _, journal := range gcJournals {
		dir := filepath.Join(r.root, journal.dir)
		entries, err := fs.ListDirectory(r.s.Home, dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			r.fail(err)
			continue
		}
		for _, entry := range entries {
			if entry.Kind == fs.KindFile && strings.HasSuffix(entry.Name, ".json") {
				r.collectRecord(journal.dir+"/"+entry.Name, filepath.Join(dir, entry.Name), journal.terminal)
			}
		}
	}
}

func (r *gcRun) collectRecord(id, path string, terminal []string) {
	data, record, ok := r.readRecord(path, terminal)
	if !ok {
		return
	}
	at := record.UpdatedAt
	if at.IsZero() {
		info, err := os.Lstat(path)
		if err != nil {
			return
		}
		at = info.ModTime()
	}
	if !at.Before(r.cutoff) {
		return
	}
	release, held, err := r.tryLock(path + ".lock")
	if err != nil {
		r.report.Journal.keep(id, err.Error())
		return
	}
	if !held {
		r.report.Journal.keep(id, config.Text("gc.client_locked", path+".lock"))
		return
	}
	defer r.releaseLock(release)
	// The writer may have changed the record before the lock was taken.
	if current, _, ok := r.readRecord(path, terminal); !ok || !bytes.Equal(current, data) {
		return
	}
	if !r.opts.DryRun {
		if _, err := fs.RemoveRegularFileIfExists(r.s.Home, path); err != nil {
			r.report.Journal.keep(id, config.Text("gc.client_remove_failed", err.Error()))
			return
		}
	}
	r.report.Journal.add(int64(len(data)))
}

// readRecord reads a journal record and reports whether it is a candidate:
// schema 1, a terminal phase, and a terminal card.
func (r *gcRun) readRecord(path string, terminal []string) ([]byte, gcJournalRecord, bool) {
	var record gcJournalRecord
	data, found, err := fs.ReadRegularFileIfExists(r.s.Home, path)
	if err != nil || !found || json.Unmarshal(data, &record) != nil {
		return nil, record, false
	}
	if record.Schema != 1 || !slices.Contains(terminal, record.Phase) || !api.ValidIdentity(record.TaskID) {
		return nil, record, false
	}
	_, ok := r.terminalTask(record.TaskID)
	return data, record, ok
}

// collectEvidenceRefs deletes the Git evidence refs of terminal cards and
// settled groups through the completion cleanup: one listing of the project's
// evidence prefix selects the cards and groups worth asking about, and the
// service's cleanup scope decides what is deleted.
func (r *gcRun) collectEvidenceRefs(ctx context.Context) {
	if r.s.Info.Remote == "" {
		return
	}
	checkout := r.opts.Checkout
	if checkout == "" {
		checkout = r.repository
	}
	if checkout == "" {
		r.report.Skipped = append(r.report.Skipped, config.Text("gc.client_no_checkout"))
		return
	}
	dir, err := r.s.completionCheckout(ctx, checkout)
	if err != nil {
		r.fail(err)
		return
	}
	listed, err := gitCommand(ctx, dir, "ls-remote", "--refs", "origin", "refs/heads/kander/"+r.s.Info.ID+"/*")
	if err != nil {
		r.fail(err)
		return
	}
	var refs []string
	tasks, groups := map[string]bool{}, map[string]bool{}
	integrations := false
	for line := range strings.Lines(listed) {
		_, ref, found := strings.Cut(strings.TrimSpace(line), "\t")
		rest, ok := strings.CutPrefix(ref, "refs/heads/kander/"+r.s.Info.ID+"/")
		if !found || !ok {
			continue
		}
		refs = append(refs, ref)
		first, _, _ := strings.Cut(rest, "/")
		if first == "integrations" {
			integrations = true
		} else if task, ok := r.terminalTask(first); ok {
			tasks[first] = true
			if task.TaskGroup != "" {
				groups[task.TaskGroup] = true
			}
		}
	}
	if integrations {
		for _, task := range r.tasks {
			if gcTerminalState(task.State) && task.TaskGroup != "" {
				groups[task.TaskGroup] = true
			}
		}
	}
	counted := map[string]bool{}
	for _, task := range slices.Sorted(maps.Keys(tasks)) {
		r.evidenceScope(ctx, refs, counted, func(ctx context.Context) (api.EvidenceCleanup, error) {
			return r.s.Project.TaskEvidenceCleanup(ctx, task)
		}, func(ctx context.Context) EvidenceCleanupReport { return r.s.CleanTaskEvidence(ctx, task, dir) })
	}
	for _, group := range slices.Sorted(maps.Keys(groups)) {
		r.evidenceScope(ctx, refs, counted, func(ctx context.Context) (api.EvidenceCleanup, error) {
			return r.s.Project.GroupEvidenceCleanup(ctx, group)
		}, func(ctx context.Context) EvidenceCleanupReport { return r.s.CleanGroupEvidence(ctx, group, dir) })
	}
	if !r.opts.DryRun && r.report.EvidenceRefs.Count > 0 {
		prefix := "refs/heads/kander/" + r.s.Info.ID + "/*"
		if _, err := gitCommand(ctx, dir, "fetch", "--quiet", "--prune", "origin", prefix+":refs/remotes/origin/kander/"+r.s.Info.ID+"/*"); err != nil {
			r.fail(err)
		}
	}
}

// evidenceScope plans or runs the cleanup of one card or group. A dry run
// counts the listed refs a Ready scope owns; a real run deletes them through
// the completion cleanup. A ref two scopes own counts once.
func (r *gcRun) evidenceScope(ctx context.Context, listed []string, counted map[string]bool, scope func(context.Context) (api.EvidenceCleanup, error), clean func(context.Context) EvidenceCleanupReport) {
	value, err := scope(ctx)
	if err != nil {
		r.fail(err)
		return
	}
	if !value.Ready {
		return
	}
	if r.opts.DryRun {
		ownership, err := newEvidenceOwnership(r.s.Info.ID, value)
		if err != nil {
			r.fail(err)
			return
		}
		for _, ref := range listed {
			if ownership.owns(ref) && !counted[ref] {
				counted[ref] = true
				r.report.EvidenceRefs.add(0)
			}
		}
		return
	}
	report := clean(ctx)
	if report.Err != nil {
		r.fail(report.Err)
	}
	for _, ref := range report.Deleted {
		if !counted[ref] {
			counted[ref] = true
			r.report.EvidenceRefs.add(0)
		}
	}
	for _, kept := range report.Kept {
		r.report.EvidenceRefs.keep(kept.Ref, kept.Reason)
	}
}
