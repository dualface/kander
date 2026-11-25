package remote

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

// EvidenceCleanupTimeout bounds the whole completion cleanup: the scope read,
// listing the refs and deleting them. A command that was cancelled after its
// move still cleans up, so the cleanup takes its own deadline. Tests replace
// it; nothing else writes it.
var EvidenceCleanupTimeout = 60 * time.Second

// evidenceRefBatch bounds the refs one ls-remote or push names.
const evidenceRefBatch = 64

// EvidenceCleanupReport is the outcome of one completion cleanup. A cleanup
// never fails its command: the caller prints Warning and keeps its result.
type EvidenceCleanupReport struct {
	// Deleted are the refs the common remote no longer lists.
	Deleted []string
	// Kept are the refs the remote still lists after the deletion.
	Kept []KeptEvidenceRef
	// Err ends the cleanup before any deletion: the scope or the remote could
	// not be read, or no checkout of the project is available.
	Err error
	// Scope names what the service owned when Err came before the remote was
	// listed: the task evidence prefixes and the exact source refs. Whether the
	// remote still holds them is unknown.
	Scope []string
}

// KeptEvidenceRef is an evidence ref that could not be deleted, and why.
// Unconfirmed marks a ref whose deletion could not be checked because the
// remote could not be listed again; it may or may not still exist.
type KeptEvidenceRef struct {
	Ref         string
	Reason      string
	Unconfirmed bool
}

// Warning is the localized text for standard error, empty when the cleanup
// deleted every listed ref or had nothing to do.
func (r EvidenceCleanupReport) Warning() string {
	if r.Err != nil {
		lines := []string{config.Text("remote.evidence_cleanup_failed", r.Err.Error())}
		if len(r.Scope) > 0 {
			lines = append(lines, config.Text("remote.evidence_cleanup_scope_unknown"))
			for _, ref := range r.Scope {
				lines = append(lines, "  "+ref)
			}
		}
		return strings.Join(lines, "\n")
	}
	if len(r.Kept) == 0 {
		return ""
	}
	summary := "remote.evidence_cleanup_kept"
	if slices.ContainsFunc(r.Kept, func(kept KeptEvidenceRef) bool { return kept.Unconfirmed }) {
		// Some deletions may have happened: the summary claims no outcome.
		summary = "remote.evidence_cleanup_unsettled"
	}
	lines := []string{config.Text(summary, len(r.Kept))}
	for _, kept := range r.Kept {
		id := "remote.evidence_cleanup_kept_ref"
		if kept.Unconfirmed {
			id = "remote.evidence_cleanup_unconfirmed_ref"
		}
		lines = append(lines, "  "+config.Text(id, kept.Ref, kept.Reason))
	}
	return strings.Join(lines, "\n")
}

// MoveTaskCleaningEvidence is MoveTask followed, when the card is done, by the
// completion cleanup of its Git evidence. A replayed move cleans up as well:
// the deletion is idempotent and finishes a cleanup an earlier run could not
// complete. The cleanup never changes the result of the move.
func (s *Session) MoveTaskCleaningEvidence(ctx context.Context, id string, request api.Move) (api.Task, EvidenceCleanupReport, error) {
	var recorded string
	if request.State == "done" {
		// The move may release the credential that names the worktree.
		recorded = s.recordedWorktree(id)
	}
	task, err := s.MoveTask(ctx, id, request)
	if err != nil || task.State != "done" {
		return task, EvidenceCleanupReport{}, err
	}
	return task, s.CleanTaskEvidence(ctx, task.TaskID, recorded), nil
}

// recordedWorktree is the worktree the execution credential of task names, or
// empty.
func (s *Session) recordedWorktree(task string) string {
	id, err := board.NormalizeTaskID(task)
	if err != nil {
		return ""
	}
	value, found, err := s.Credential(id)
	if err != nil || !found {
		return ""
	}
	return value.Worktree
}

// CleanTaskEvidence deletes the Git evidence of a card once it no longer has a
// reader: a standalone card once it is done, the cards of a task group once the
// group is settled. recorded is the worktree of the execution credential;
// the current directory is the fallback checkout.
func (s *Session) CleanTaskEvidence(ctx context.Context, task, recorded string) EvidenceCleanupReport {
	if s.Info.Remote == "" {
		return EvidenceCleanupReport{}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), EvidenceCleanupTimeout)
	defer cancel()
	scope, err := s.Project.TaskEvidenceCleanup(ctx, task)
	if err != nil {
		return EvidenceCleanupReport{Err: err}
	}
	return s.cleanEvidence(ctx, scope, recorded)
}

// CleanGroupEvidence deletes the Git evidence of a settled task group, in the
// checkout dir.
func (s *Session) CleanGroupEvidence(ctx context.Context, group, dir string) EvidenceCleanupReport {
	if s.Info.Remote == "" {
		return EvidenceCleanupReport{}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), EvidenceCleanupTimeout)
	defer cancel()
	scope, err := s.Project.GroupEvidenceCleanup(ctx, group)
	if err != nil {
		return EvidenceCleanupReport{Err: err}
	}
	return s.cleanEvidence(ctx, scope, dir)
}

// evidenceOwnership says which refs of the common remote a cleanup scope owns:
// every generation below the task prefix of a done card, and exactly the source
// ref of each recorded integration execution. Nothing is owned by prefix
// below integrations/.
type evidenceOwnership struct {
	project  string
	tasks    map[string]bool
	sources  map[string]bool
	patterns []string
}

func newEvidenceOwnership(project string, scope api.EvidenceCleanup) (evidenceOwnership, error) {
	invalid := errors.New(config.Text("remote.evidence_cleanup_scope_invalid"))
	o := evidenceOwnership{project: project, tasks: map[string]bool{}, sources: map[string]bool{}}
	if !api.ValidIdentity(project) || strings.ContainsAny(project, "*?[\\") {
		return o, invalid
	}
	for _, task := range scope.Tasks {
		id, err := board.NormalizeTaskID(task)
		if err != nil || id != task {
			return o, invalid
		}
		o.tasks[id] = true
		o.patterns = append(o.patterns, taskEvidencePrefix(project, id)+"*")
	}
	for _, source := range scope.Sources {
		if !api.ValidIdentity(source.ExecutionID) || strings.ContainsAny(source.ExecutionID, "*?[\\") || !api.CommitSHA(source.Commit) {
			return o, invalid
		}
		ref := api.IntegrationSourceRef(project, source.ExecutionID, source.Commit)
		if !o.sources[ref] {
			o.sources[ref] = true
			o.patterns = append(o.patterns, ref)
		}
	}
	return o, nil
}

func taskEvidencePrefix(project, task string) string {
	return "refs/heads/kander/" + project + "/" + task + "/"
}

// owns accepts a listed ref only in its exact form: a task evidence ref must
// be what DeliveryRef builds for one of the done cards.
func (o evidenceOwnership) owns(ref string) bool {
	if o.sources[ref] {
		return true
	}
	for task := range o.tasks {
		rest, found := strings.CutPrefix(ref, taskEvidencePrefix(o.project, task))
		if !found {
			continue
		}
		generation, commit, found := strings.Cut(rest, "/")
		value, err := strconv.ParseUint(strings.TrimPrefix(generation, "g"), 10, 64)
		if found && strings.HasPrefix(generation, "g") && err == nil && api.CommitSHA(commit) && api.DeliveryRef(o.project, task, value, commit) == ref {
			return true
		}
	}
	return false
}

func (s *Session) cleanEvidence(ctx context.Context, scope api.EvidenceCleanup, recorded string) EvidenceCleanupReport {
	if !scope.Ready || len(scope.Tasks) == 0 && len(scope.Sources) == 0 {
		return EvidenceCleanupReport{}
	}
	ownership, err := newEvidenceOwnership(s.Info.ID, scope)
	if err != nil {
		return EvidenceCleanupReport{Err: err}
	}
	unknown := func(err error) EvidenceCleanupReport {
		return EvidenceCleanupReport{Err: err, Scope: ownership.patterns}
	}
	// The same check as a completion: origin and its push URL are the
	// registered remote. A push to origin reaches every push URL it has, so a
	// deletion also needs that URL to be the only one.
	dir, err := s.completionCheckout(ctx, recorded)
	if err != nil {
		return unknown(err)
	}
	if err := singlePushRemote(ctx, dir, s.Info.Remote); err != nil {
		return unknown(err)
	}
	listed, err := listEvidenceRefs(ctx, dir, ownership)
	if err != nil {
		return unknown(err)
	}
	if len(listed) == 0 {
		return EvidenceCleanupReport{}
	}
	var pushErr error
	for batch := range slices.Chunk(listed, evidenceRefBatch) {
		args := append([]string{"push", "--porcelain", "--delete", "origin"}, batch...)
		if _, err := gitCommand(ctx, dir, args...); err != nil {
			pushErr = errors.Join(pushErr, err)
		}
	}
	report := EvidenceCleanupReport{Deleted: listed}
	if pushErr == nil {
		return report
	}
	// A refused or interrupted push may still have deleted some refs, and
	// another client may have deleted others: only a ref the remote still
	// lists is kept.
	remaining, err := listEvidenceRefs(ctx, dir, ownership)
	report.Deleted = nil
	if err != nil {
		reason := errors.Join(pushErr, err).Error()
		for _, ref := range listed {
			report.Kept = append(report.Kept, KeptEvidenceRef{Ref: ref, Reason: reason, Unconfirmed: true})
		}
		return report
	}
	for _, ref := range listed {
		if slices.Contains(remaining, ref) {
			report.Kept = append(report.Kept, KeptEvidenceRef{Ref: ref, Reason: pushErr.Error()})
		} else {
			report.Deleted = append(report.Deleted, ref)
		}
	}
	return report
}

// singlePushRemote refuses a checkout whose origin pushes anywhere besides the
// registered remote: git push sends a deletion to every push URL of origin.
func singlePushRemote(ctx context.Context, dir, expected string) error {
	urls, err := gitCommand(ctx, dir, "remote", "get-url", "--push", "--all", "origin")
	if err != nil {
		return err
	}
	if urls != expected {
		return errors.New(config.Text("remote.git_push_remote_mismatch"))
	}
	return nil
}

// listEvidenceRefs lists the owned refs the common remote holds, sorted.
func listEvidenceRefs(ctx context.Context, dir string, ownership evidenceOwnership) ([]string, error) {
	var refs []string
	for batch := range slices.Chunk(ownership.patterns, evidenceRefBatch) {
		args := append([]string{"ls-remote", "--refs", "origin"}, batch...)
		text, err := gitCommand(ctx, dir, args...)
		if err != nil {
			return nil, err
		}
		for line := range strings.Lines(text) {
			_, ref, found := strings.Cut(strings.TrimSpace(line), "\t")
			if found && ownership.owns(ref) && !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
		}
	}
	slices.Sort(refs)
	return refs, nil
}
