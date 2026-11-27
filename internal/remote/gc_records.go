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
// that needs nothing of it any more: the server reports it abandoned, or
// finalized and published to every member, or no longer knows it while no
// pending finalization is recorded there. A finalized run that still has to
// be published keeps its directory: the writer secret in it is the only one
// the server accepts for that publication.
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
		if entry.Kind == fs.KindDirectory && board.ValidReviewID(entry.Name) {
			r.collectRuntime(ctx, entry.Name, filepath.Join(dir, entry.Name))
		}
	}
}

// collectRuntime decides on one run directory and removes it, all under its
// runtime lock: a reviewer takes the same lock before it prepares, finalizes
// or publishes the run, so the pending record and the server state read here
// cannot change before the removal. The lock file goes last, after the lock
// is released, so the removal also works where an open file cannot be deleted.
func (r *gcRun) collectRuntime(ctx context.Context, id, dir string) {
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
	remove, reason, err := r.runtimeDone(ctx, id, dir)
	if err != nil || !remove {
		r.releaseLock(release)
		switch {
		case err != nil:
			r.fail(fmt.Errorf("%s: %w", id, err))
		case reason != "":
			r.report.ReviewRuntime.keep(id, reason)
		}
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

// runtimeDone reports whether the run needs its runtime directory no more. A
// reason is set when the directory is a candidate that stays.
func (r *gcRun) runtimeDone(ctx context.Context, id, dir string) (bool, string, error) {
	pending, err := fs.RegularFileExists(r.s.Home, filepath.Join(dir, "pending-finalization.json"))
	if err != nil {
		return false, "", err
	}
	raw, err := r.s.Project.ReviewResource(ctx, "runs", id)
	if api.IsCode(err, api.CodeNotFound) {
		if pending {
			return false, config.Text("gc.client_runtime_pending"), nil
		}
		return true, "", nil
	}
	if err != nil {
		return false, "", err
	}
	var run board.ReviewRun
	if err := json.Unmarshal(raw, &run); err != nil {
		return false, "", err
	}
	switch {
	case run.Abandonment != nil:
		return true, "", nil
	case run.Phase != "finalized":
		return false, "", nil
	case !runPublished(run):
		return false, config.Text("gc.client_runtime_unpublished"), nil
	}
	return true, "", nil
}

// runPublished reports whether a run was published into every member card.
func runPublished(run board.ReviewRun) bool {
	for _, id := range run.TaskIDs {
		if !run.Published[id] {
			return false
		}
	}
	return len(run.TaskIDs) > 0
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
	data, record, candidate, err := r.readRecord(path, terminal)
	if err != nil {
		r.report.Journal.keep(id, err.Error())
		return
	}
	if !candidate {
		return
	}
	at := record.UpdatedAt
	if at.IsZero() {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			r.report.Journal.keep(id, err.Error())
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
	current, _, candidate, err := r.readRecord(path, terminal)
	if err != nil {
		r.report.Journal.keep(id, err.Error())
		return
	}
	if !candidate || !bytes.Equal(current, data) {
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
// schema 1, a terminal phase, and a terminal card. A missing record is no
// candidate; a record that cannot be read or decoded is an error.
func (r *gcRun) readRecord(path string, terminal []string) ([]byte, gcJournalRecord, bool, error) {
	var record gcJournalRecord
	data, found, err := fs.ReadRegularFileIfExists(r.s.Home, path)
	if err != nil || !found {
		return nil, record, false, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, record, false, fmt.Errorf("%s: %w", path, err)
	}
	if record.Schema != 1 || !slices.Contains(terminal, record.Phase) || !api.ValidIdentity(record.TaskID) {
		return nil, record, false, nil
	}
	_, ok := r.terminalTask(record.TaskID)
	return data, record, ok, nil
}

// collectEvidenceRefs deletes the Git evidence refs of terminal cards and
// settled groups through the completion cleanup: one listing of the project's
// evidence prefix selects the cards and groups worth asking about, and the
// service's cleanup scope decides what is deleted. Then it drops the local
// tracking refs of evidence refs the common remote no longer holds.
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
	refs, err := r.remoteEvidence(ctx, dir)
	if err != nil {
		r.fail(err)
		return
	}
	prefix := evidenceHeads(r.s.Info.ID)
	tasks, groups := map[string]bool{}, map[string]bool{}
	integrations := false
	for _, ref := range refs {
		first, _, _ := strings.Cut(strings.TrimPrefix(ref, prefix), "/")
		if first == "integrations" {
			integrations = true
		} else if task, ok := r.terminalTask(first); ok {
			tasks[first] = true
			if task.TaskGroup != "" {
				groups[task.TaskGroup] = true
			}
		}
	}
	// An integration source ref names its integration execution, not its card:
	// a group's scope owns the sources of the group, a standalone card's scope
	// the sources of the card.
	if integrations {
		for _, task := range r.tasks {
			switch {
			case !gcTerminalState(task.State):
			case task.TaskGroup != "":
				groups[task.TaskGroup] = true
			default:
				tasks[task.TaskID] = true
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
	remaining := map[string]bool{}
	if !r.opts.DryRun && len(counted) > 0 {
		// The cleanup deleted refs: read what the remote holds now.
		if refs, err = r.remoteEvidence(ctx, dir); err != nil {
			r.fail(err)
			return
		}
		counted = map[string]bool{}
	}
	for _, ref := range refs {
		if !counted[ref] {
			remaining[ref] = true
		}
	}
	r.collectTrackingRefs(ctx, dir, remaining)
}

// evidenceHeads is the branch prefix of the evidence refs of a project.
func evidenceHeads(project string) string { return "refs/heads/kander/" + project + "/" }

// evidenceTracking is the prefix of their tracking refs in a clone.
func evidenceTracking(project string) string { return "refs/remotes/origin/kander/" + project + "/" }

// remoteEvidence lists the evidence refs of this project on the common remote.
func (r *gcRun) remoteEvidence(ctx context.Context, dir string) ([]string, error) {
	prefix := evidenceHeads(r.s.Info.ID)
	listed, err := r.git(ctx, dir, "ls-remote", "--refs", "origin", prefix+"*")
	if err != nil {
		return nil, err
	}
	var refs []string
	for line := range strings.Lines(listed) {
		if _, ref, found := strings.Cut(strings.TrimSpace(line), "\t"); found && strings.HasPrefix(ref, prefix) {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// collectTrackingRefs deletes the local tracking refs below the project's
// evidence prefix whose remote ref is gone. Each goes with update-ref and the
// value it was read with, so no fetch runs and no other ref can change.
func (r *gcRun) collectTrackingRefs(ctx context.Context, dir string, remote map[string]bool) {
	tracking := evidenceTracking(r.s.Info.ID)
	listed, err := r.git(ctx, dir, "for-each-ref", "--format=%(objectname) %(refname)", tracking)
	if err != nil {
		r.fail(err)
		return
	}
	for line := range strings.Lines(listed) {
		oid, ref, found := strings.Cut(strings.TrimSpace(line), " ")
		if !found || !strings.HasPrefix(ref, tracking) || remote[evidenceHeads(r.s.Info.ID)+strings.TrimPrefix(ref, tracking)] {
			continue
		}
		if !r.opts.DryRun {
			if _, err := r.git(ctx, dir, "update-ref", "-d", ref, oid); err != nil {
				r.report.TrackingRefs.keep(ref, config.Text("gc.client_remove_failed", err.Error()))
				continue
			}
		}
		r.report.TrackingRefs.add(0)
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
