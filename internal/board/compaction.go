package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

// CompactionCondition numbers the settlement conditions a review run must meet
// before compaction removes its large evidence. Conditions 4, 5, 6 and 8 hold
// in normal operation for a long time; 1, 2, 3, 7 and CompactionRemoval mark a
// run that needs attention.
type CompactionCondition int

const (
	// CompactionRunFinal: the run is finalized or abandoned.
	CompactionRunFinal CompactionCondition = 1
	// CompactionPublished: every member has a published copy or was skipped.
	CompactionPublished CompactionCondition = 2
	// CompactionBatchClosed: the batch of the run is closed, or the run is abandoned.
	CompactionBatchClosed CompactionCondition = 3
	// CompactionMembersFinal: every member card is in done, archived, or trash.
	CompactionMembersFinal CompactionCondition = 4
	// CompactionGroupSettled: the task group of every member is settled.
	CompactionGroupSettled CompactionCondition = 5
	// CompactionRetention: the run finished before the retention period.
	CompactionRetention CompactionCondition = 6
	// CompactionRunLock: the execution locks of the run and of every run that
	// names it as a predecessor were free.
	CompactionRunLock CompactionCondition = 7
	// CompactionSuccessor: no unsettled run names the run as its predecessor.
	CompactionSuccessor CompactionCondition = 8
	// CompactionRemoval: the run met every condition, but a receipt, an unsafe
	// entry, or a failed removal kept some of its files.
	CompactionRemoval CompactionCondition = 9
)

// compactionBatchSize is how many runs one round compacts under one exclusive
// board lock, so a large backlog never blocks the board for long.
var compactionBatchSize = 50

// compactionInterrupt, when set by a test, may stop a round after the named
// step: "receipts", "copies", or "control".
var compactionInterrupt func(step string) error

// CompactionOptions selects the runs to compact.
type CompactionOptions struct {
	// OlderThan is the retention period; it must be positive.
	OlderThan time.Duration
	// Now is the current time; the zero value means time.Now().
	Now time.Time
	// DryRun only plans: it writes no receipt and removes nothing.
	DryRun bool
	// KanderVersion is recorded in each receipt.
	KanderVersion string
	// NoWait takes the exclusive board lock of each round without waiting:
	// a held lock ends the call with board.lock_busy instead.
	NoWait bool
}

// CompactedRun is one run compaction removed files from, or would remove
// them from in a dry run.
type CompactedRun struct {
	RunID string
	Files int
	Bytes int64
}

// SkippedRun is a run compaction kept, with every condition that failed.
type SkippedRun struct {
	RunID      string
	Conditions []CompactionCondition
	Reason     string
}

// CompactionReport describes one call of CompactReviewRuns. A run whose
// removal failed part way lists what it removed in Runs and the rest in
// Skipped under CompactionRemoval; the next call finishes it.
type CompactionReport struct {
	Runs     []CompactedRun
	Skipped  []SkippedRun
	Files    int
	Bytes    int64
	Warnings []string
	Err      error
}

// compactionTarget is one file or tree compaction removes.
type compactionTarget struct {
	location, name, path string
	tree                 bool
	files                int
	bytes                int64
}

type compactionCandidate struct {
	run     ReviewRun
	targets []compactionTarget
}

// compactionPlan is computed from one board snapshot under the exclusive
// board lock. candidates meet conditions 1-6 and 8 and still hold files.
type compactionPlan struct {
	candidates map[string]*compactionCandidate
	successors map[string][]string
	skipped    map[string]*SkippedRun
	warnings   []string
}

func (p *compactionPlan) skip(id string, c CompactionCondition, reason string) {
	s := p.skipped[id]
	if s == nil {
		s = &SkippedRun{RunID: id}
		p.skipped[id] = s
	}
	if !slices.Contains(s.Conditions, c) {
		s.Conditions = append(s.Conditions, c)
		if s.Reason != "" {
			s.Reason += "; "
		}
		s.Reason += reason
	}
}

// CompactReviewRuns removes the large evidence files of settled review runs
// and records each removal in a compaction receipt first. A dry run only
// reports what a run would remove. Every round takes the exclusive board lock
// and never waits for a run's execution lock; an interrupted call is finished
// by the next one.
func CompactReviewRuns(ctx context.Context, root string, options CompactionOptions) (report CompactionReport, err error) {
	defer func() { report.Err = err }()
	if options.OlderThan <= 0 {
		return report, reviewError("compaction retention must be positive")
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	if err = ensureLayout(root); err != nil {
		return report, err
	}
	state := compactionState{skipped: map[string]SkippedRun{}, warnings: map[string]bool{}, attempted: map[string]bool{}}
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var done int
		done, err = compactionRound(ctx, root, options, &state, &report)
		if err != nil || done == 0 || options.DryRun {
			break
		}
	}
	for _, id := range sortedKeys(state.skipped) {
		report.Skipped = append(report.Skipped, state.skipped[id])
	}
	report.Warnings = sortedKeys(state.warnings)
	return report, err
}

// compactionState carries what the rounds of one call learned.
type compactionState struct {
	skipped   map[string]SkippedRun
	warnings  map[string]bool
	attempted map[string]bool
}

// compactionRound plans under the exclusive board lock and processes one batch
// of candidates; a dry run probes every candidate. It returns the number of
// runs it took.
func compactionRound(ctx context.Context, root string, options CompactionOptions, state *compactionState, report *CompactionReport) (taken int, err error) {
	// A caller that gives up while a board writer holds the lock must not
	// compact once the lock frees up.
	var locks lockSet
	if options.NoWait {
		locks, err = tryExclusiveBoard(root)
	} else {
		locks, err = acquireContext(ctx, root, LockScope{ExclusiveBoard: true})
	}
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, locks.close()) }()
	if err = requireNoPendingOperations(root, nil); err != nil {
		return 0, err
	}
	plan, err := planCompaction(root, options.Now, options.OlderThan)
	if err != nil {
		return 0, err
	}
	for _, w := range plan.warnings {
		state.warnings[w] = true
	}
	for id, s := range plan.skipped {
		state.skipped[id] = *s
	}
	var batch []string
	for _, id := range sortedKeys(plan.candidates) {
		if state.attempted[id] {
			continue
		}
		if !options.DryRun && len(batch) == compactionBatchSize {
			break
		}
		batch = append(batch, id)
		state.attempted[id] = true
	}
	held := runLockSet{root: root, keep: !options.DryRun, taken: map[string]func() error{}, busy: map[string]error{}}
	defer func() { err = errors.Join(err, held.close()) }()
	var runs []*compactionCandidate
	for _, id := range batch {
		if blocker, busy := held.chain(id, plan.successors); busy != nil {
			if blocker == id {
				plan.skip(id, CompactionRunLock, "run execution lock is held: "+busy.Error())
			} else {
				plan.skip(id, CompactionRunLock, "successor run "+blocker+" execution lock is held")
			}
			state.skipped[id] = *plan.skipped[id]
			continue
		}
		runs = append(runs, plan.candidates[id])
	}
	if options.DryRun {
		for _, c := range runs {
			var r CompactedRun
			r.RunID = c.run.RunID
			for _, target := range c.targets {
				r.Files += target.files
				r.Bytes += target.bytes
			}
			addCompactedRun(report, r)
		}
		return len(batch), nil
	}
	return len(batch), executeCompaction(ctx, root, options, runs, state, report)
}

func addCompactedRun(report *CompactionReport, r CompactedRun) {
	report.Runs = append(report.Runs, r)
	report.Files += r.Files
	report.Bytes += r.Bytes
}

// executeCompaction follows PruneArchived: receipts first, then card copies,
// then control records, so an interruption never leaves a removed file
// without its receipt.
func executeCompaction(ctx context.Context, root string, options CompactionOptions, runs []*compactionCandidate, state *compactionState, report *CompactionReport) error {
	if len(runs) == 0 {
		return nil
	}
	// The last point at which a cancellation leaves the board untouched.
	if err := ctx.Err(); err != nil {
		return err
	}
	interrupt := func(step string) error {
		if compactionInterrupt == nil {
			return nil
		}
		return compactionInterrupt(step)
	}
	if err := ensurePruneDir(root, "compactions"); err != nil {
		return err
	}
	for _, c := range runs {
		if err := writeCompactionReceipt(root, c, options); err != nil {
			return err
		}
	}
	if err := interrupt("receipts"); err != nil {
		return err
	}
	// removed holds the runs with at least one completed removal: a file, or a
	// tree down to its emptied directory. A run whose removals all failed is
	// only skipped.
	removed := map[string]*CompactedRun{}
	failed := map[string][]string{}
	var failures []error
	remove := func(c *compactionCandidate, cards bool) {
		for _, target := range c.targets {
			if strings.HasPrefix(target.location, compactedCardPre) != cards {
				continue
			}
			files, bytes, err := removePath(root, target.path, target.tree)
			if err == nil || files > 0 {
				r := removed[c.run.RunID]
				if r == nil {
					r = &CompactedRun{RunID: c.run.RunID}
					removed[c.run.RunID] = r
				}
				r.Files += files
				r.Bytes += bytes
			}
			if err != nil {
				failures = append(failures, err)
				failed[c.run.RunID] = append(failed[c.run.RunID], err.Error())
			}
		}
	}
	finish := func() {
		for _, c := range runs {
			id := c.run.RunID
			// A run counts once a removal of it completed, even when only an
			// emptied directory was left from an interrupted round, as a dry run
			// counts it.
			if r := removed[id]; r != nil {
				addCompactedRun(report, *r)
			}
			if reasons := failed[id]; len(reasons) > 0 {
				state.skipped[id] = SkippedRun{RunID: id, Conditions: []CompactionCondition{CompactionRemoval}, Reason: strings.Join(reasons, "; ")}
			} else {
				delete(state.skipped, id)
			}
		}
	}
	for _, c := range runs {
		remove(c, true)
	}
	if err := interrupt("copies"); err != nil {
		finish()
		return err
	}
	for _, c := range runs {
		remove(c, false)
	}
	finish()
	if err := errors.Join(failures...); err != nil {
		return err
	}
	return interrupt("control")
}

// writeCompactionReceipt merges this round's targets into the run's receipt.
// A receipt that already lists every target is left unchanged.
func writeCompactionReceipt(root string, c *compactionCandidate, options CompactionOptions) error {
	previous, exists, err := runCompaction(root, c.run.RunID, c.run.TaskIDs)
	if err != nil {
		return err
	}
	files := slices.Clone(previous.Files)
	for _, target := range c.targets {
		if !previous.covers(target.location, target.name) {
			files = append(files, compactedFile{Location: target.location, Name: target.name, Bytes: target.bytes})
		}
	}
	if exists && len(files) == len(previous.Files) {
		return nil
	}
	slices.SortFunc(files, func(a, b compactedFile) int {
		return strings.Compare(a.Location+"/"+a.Name, b.Location+"/"+b.Name)
	})
	r := compactionReceipt{Schema: 1, RunID: c.run.RunID, TaskIDs: slices.Clone(c.run.TaskIDs), Files: files, CompactedAt: options.Now.UTC().Format(time.RFC3339), KanderVersion: options.KanderVersion}
	return writeJSON(root, compactionReceiptPath(root, c.run.RunID), r, true)
}

// runLockSet takes run execution locks without waiting. keep holds them until
// close; otherwise each lock is released as soon as it was taken.
type runLockSet struct {
	root  string
	keep  bool
	taken map[string]func() error
	busy  map[string]error
}

func (s *runLockSet) take(id string) error {
	if _, ok := s.taken[id]; ok {
		return nil
	}
	if err, ok := s.busy[id]; ok {
		return err
	}
	release, err := tryLockReviewRun(s.root, id)
	if err == nil && !s.keep {
		err = release()
		release = func() error { return nil }
	}
	if err != nil {
		s.busy[id] = err
		return err
	}
	s.taken[id] = release
	return nil
}

// chain takes the lock of id and of every run that names it, directly or
// through other runs, as its predecessor: a review gate working on a successor
// may still read the predecessor's evidence. It returns the first busy run.
func (s *runLockSet) chain(id string, successors map[string][]string) (string, error) {
	queue := []string{id}
	for seen := map[string]bool{}; len(queue) > 0; queue = queue[1:] {
		next := queue[0]
		if seen[next] {
			continue
		}
		seen[next] = true
		if err := s.take(next); err != nil {
			return next, err
		}
		queue = append(queue, successors[next]...)
	}
	return "", nil
}

func (s *runLockSet) close() error {
	var err error
	for _, id := range sortedKeys(s.taken) {
		err = errors.Join(err, s.taken[id]())
	}
	s.taken = nil
	return err
}

// tryExclusiveBoard takes the exclusive board lock that acquireContext waits
// for, or fails at once with board.lock_busy.
func tryExclusiveBoard(root string) (lockSet, error) {
	if err := ensureControl(root); err != nil {
		return nil, err
	}
	path := control(root, "locks", "board.lock")
	f, err := fs.OpenLockFile(root, path)
	if err != nil {
		return nil, err
	}
	lock, ok, err := fs.TryLockExclusive(f)
	if err == nil && !ok {
		err = kanbanError("board.lock_busy", path)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return lockSet{{f, lock}}, nil
}

// tryLockReviewRun takes the lock LockReviewRun waits for, or fails at once
// with board.lock_busy.
func tryLockReviewRun(root, runID string) (func() error, error) {
	path := control(root, "locks", "review-run-"+runID+".lock")
	f, err := fs.OpenLockFile(root, path)
	if err != nil {
		return nil, err
	}
	lock, ok, err := fs.TryLockExclusive(f)
	if err == nil && !ok {
		err = kanbanError("board.lock_busy", path)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return func() error { return errors.Join(lock.Unlock(), f.Close()) }, nil
}

// planCompaction evaluates conditions 1-6 and 8 for every run and lists the
// files each settled run still holds. It reads only; the caller holds the
// exclusive board lock, so it reads group records through a lock-free view
// instead of a transaction that would take the board lock again.
func planCompaction(root string, now time.Time, olderThan time.Duration) (*compactionPlan, error) {
	b, err := scan(root)
	if err != nil {
		return nil, err
	}
	plan := &compactionPlan{candidates: map[string]*compactionCandidate{}, successors: map[string][]string{}, skipped: map[string]*SkippedRun{}}
	warn := func(name string, e error) { plan.warnings = append(plan.warnings, name+": "+e.Error()) }
	b.documents, b.documentErrors = map[string]string{}, map[string]error{}
	for _, id := range sortedKeys(b.Entries) {
		text, e := readCardText(b.Entries[id])
		if e != nil {
			warn(id, e)
			b.documentErrors[id] = e
			continue
		}
		b.documents[id] = text
	}
	// The membership view keeps every scan and read problem, including a card
	// the scan dropped for a missing spec.md, and fails closed for any group
	// such a card may belong to.
	membership := b.GroupMembership()
	final := func(id string) bool {
		e, ok := b.Entries[id]
		_, unreadable := b.documentErrors[id]
		return ok && !unreadable && (e.State == "done" || e.State == "archived" || e.State == "trash")
	}
	// memberGroup returns the task group of a member card, and false when the
	// card's membership cannot be read unambiguously.
	memberGroup := func(id string) (string, bool) {
		text, ok := b.documents[id]
		if !ok {
			return "", false
		}
		g := TaskGroupFrom(text)
		return g, len(fieldLines(text, FieldTaskGroup)) <= 1 && (g == "" || taskGroupRe.MatchString(g))
	}
	settledGroups := map[string]bool{}
	groupSettled := func(g string) bool {
		if settled, known := settledGroups[g]; known {
			return settled
		}
		settled := false
		if e := membership.ErrFor(g); e != nil {
			warn("groups/"+g, e)
		} else {
			settled = compactionGroupSettled(root, g, membership.Groups[g], final, b.Entries, warn)
		}
		settledGroups[g] = settled
		return settled
	}

	review := func(parts ...string) string {
		return control(root, append([]string{"groups", reviewControlGroup}, parts...)...)
	}
	runs := map[string]ReviewRun{}
	for _, name := range dirNames(root, review("runs"), warn) {
		var v ReviewRun
		if e := readControlJSON(root, review("runs", name, "run.json"), &v); e != nil || v.RunID != name {
			warn("runs/"+name, errors.Join(e, reviewError("invalid intent")))
			continue
		}
		runs[name] = v
	}
	settled := map[string]bool{}
	for _, id := range sortedKeys(runs) {
		run := runs[id]
		if run.PreviousRunID != "" {
			plan.successors[run.PreviousRunID] = append(plan.successors[run.PreviousRunID], id)
		}
		settled[id] = compactionConditions(root, run, now, olderThan, plan, final, memberGroup, groupSettled, warn)
	}
	// A run that an unsettled run names as its predecessor stays until the
	// successor settles too, as planPrune keeps predecessor chains.
	for changed := true; changed; {
		changed = false
		for _, id := range sortedKeys(runs) {
			previous := runs[id].PreviousRunID
			if !settled[id] && settled[previous] {
				settled[previous], changed = false, true
				plan.skip(previous, CompactionSuccessor, "successor run "+id+" is not settled")
			}
		}
	}
	for _, id := range sortedKeys(settled) {
		if !settled[id] {
			continue
		}
		run := runs[id]
		if _, _, e := runCompaction(root, id, run.TaskIDs); e != nil {
			plan.skip(id, CompactionRemoval, e.Error())
			continue
		}
		targets, e := compactionTargets(root, run, b.Entries, warn)
		if e != nil {
			plan.skip(id, CompactionRemoval, e.Error())
			continue
		}
		if len(targets) > 0 {
			plan.candidates[id] = &compactionCandidate{run: run, targets: targets}
		}
	}
	return plan, nil
}

// compactionConditions records every failed condition among 1-6 and reports
// whether all of them hold.
func compactionConditions(root string, run ReviewRun, now time.Time, olderThan time.Duration, plan *compactionPlan, final func(string) bool, memberGroup func(string) (string, bool), groupSettled func(string) bool, warn func(string, error)) bool {
	id := run.RunID
	ok := true
	fail := func(c CompactionCondition, reason string) {
		ok = false
		plan.skip(id, c, reason)
	}
	abandoned := run.Abandonment != nil
	if run.Phase != "finalized" && !abandoned {
		fail(CompactionRunFinal, "run is neither finalized nor abandoned")
	}
	for _, task := range run.TaskIDs {
		if !run.Published[task] && !slices.Contains(run.SkippedMembers, task) {
			fail(CompactionPublished, "member "+task+" has no published copy")
			break
		}
	}
	if !abandoned {
		_, closed, e := fs.ReadRegularFileIfExists(root, control(root, "groups", reviewControlGroup, "closures", run.BatchID+".json"))
		if e != nil {
			warn("closures/"+run.BatchID, e)
		}
		if !ValidReviewID(run.BatchID) || e != nil || !closed {
			fail(CompactionBatchClosed, "batch "+run.BatchID+" is not closed")
		}
	}
	if len(run.TaskIDs) == 0 {
		fail(CompactionMembersFinal, "run has no members")
	}
	for _, task := range run.TaskIDs {
		if !final(task) {
			fail(CompactionMembersFinal, "member "+task+" is not in done, archived, or trash")
			break
		}
	}
	for _, task := range run.TaskIDs {
		g, known := memberGroup(task)
		if !known {
			fail(CompactionGroupSettled, "task group of member "+task+" cannot be read")
			break
		}
		if g != "" && !groupSettled(g) {
			fail(CompactionGroupSettled, "task group "+g+" is not settled")
			break
		}
	}
	finished := run.FinishedAt
	if finished == "" && abandoned {
		finished = run.Abandonment.AbandonedAt
	}
	at, e := time.Parse(time.RFC3339Nano, finished)
	switch {
	case finished == "" || e != nil:
		fail(CompactionRetention, "run has no valid finish time")
	case !at.Before(now.Add(-olderThan)):
		fail(CompactionRetention, "run finished at "+finished+", inside the retention period")
	}
	return ok
}

// compactionGroupSettled has the semantics of GroupEvidenceSettled without
// taking any lock. The caller has checked that the membership of group is
// complete. Every member is final and, for a group with a checkpoint,
// every completed wrap-up of a done member has its Git evidence record.
func compactionGroupSettled(root, group string, members []string, final func(string) bool, entries map[string]Entry, warn func(string, error)) bool {
	if !ValidTaskGroup(group) || len(members) == 0 {
		return false
	}
	var done []string
	for _, id := range members {
		if !final(id) {
			return false
		}
		if entries[id].State == "done" {
			done = append(done, id)
		}
	}
	tx := &Transaction{root: root, scope: LockScope{Groups: []string{group, dispatchRegistry}, Tasks: done, ReadOnly: true}}
	_, exists, err := readCheckpoint(tx, group)
	if err != nil {
		warn("groups/"+group, err)
		return false
	}
	if !exists {
		return true
	}
	for _, id := range done {
		witnessed, err := wrapUpsWitnessed(tx, group, id)
		if err != nil {
			warn(id, err)
			return false
		}
		if !witnessed {
			return false
		}
	}
	return true
}

// originalNames are the files FinalizeReviewRun writes to originals/;
// cardCopyNames adds what publication and the review records write to a
// member card's reviews/<run>/ copy.
var (
	originalNames = append([]string{"report.md", ReviewProcessEvidence}, compactedNames...)
	cardCopyNames = append([]string{"manifest.json", "sidecar.json", "assignment.json", "interpretation.json", "legacy-map.json", "dispositions"}, originalNames...)
)

// compactionTargets lists the files of a settled run that compaction removes
// and that still exist. A listed name that is not a regular file, or a tree
// that holds anything but regular files and directories, keeps the whole run.
func compactionTargets(root string, run ReviewRun, entries map[string]Entry, warn func(string, error)) ([]compactionTarget, error) {
	var targets []compactionTarget
	// unknown reports, and keeps, every entry of a protected directory that no
	// review producer writes; compaction never removes it.
	unknown := func(dir string, known []string) {
		list, err := fs.ListDirectory(root, dir)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			warn(dir, err)
			return
		}
		for _, entry := range list {
			want := fs.KindFile
			if entry.Name == "dispositions" {
				want = fs.KindDirectory
			}
			switch {
			case !slices.Contains(known, entry.Name):
				warn(filepath.Join(dir, entry.Name), reviewError("unknown review evidence entry; kept"))
			case entry.Kind != want:
				// A link or special file under a known name is kept and reported too.
				warn(filepath.Join(dir, entry.Name), reviewError("review evidence entry of an unexpected type; kept"))
			}
		}
	}
	file := func(location, path, name string) error {
		f, err := fs.OpenRegularFileIfExists(root, path)
		if err != nil || f == nil {
			return err
		}
		info, err := f.Stat()
		if err = errors.Join(err, f.Close()); err != nil {
			return err
		}
		targets = append(targets, compactionTarget{location: location, name: name, path: path, files: 1, bytes: info.Size()})
		return nil
	}
	for _, task := range run.TaskIDs {
		e, ok := entries[task]
		if !ok || !e.IsDirectory() {
			continue
		}
		copyDir := filepath.Join(e.Path, "reviews", run.RunID)
		unknown(copyDir, cardCopyNames)
		for _, name := range compactedNames {
			if err := file(compactedCardPre+task, filepath.Join(copyDir, name), name); err != nil {
				return nil, err
			}
		}
	}
	dir := control(root, "groups", reviewControlGroup, "runs", run.RunID)
	unknown(filepath.Join(dir, "originals"), originalNames)
	for _, name := range compactedNames {
		if err := file(compactedOriginals, filepath.Join(dir, "originals", name), name); err != nil {
			return nil, err
		}
	}
	for _, location := range []string{compactedInputs, compactedStaging} {
		path := filepath.Join(dir, location)
		files, bytes, exists, err := regularTreeSize(root, path)
		if err != nil {
			return nil, err
		}
		if exists {
			targets = append(targets, compactionTarget{location: location, name: compactedTree, path: path, tree: true, files: files, bytes: bytes})
		}
	}
	return targets, nil
}

// regularTreeSize counts the files and bytes below path. It fails on anything
// but regular files and directories, like requireRegularTree.
func regularTreeSize(root, path string) (files int, bytes int64, exists bool, err error) {
	entries, err := fs.ListDirectory(root, path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name)
		switch entry.Kind {
		case fs.KindFile:
			f, e := fs.OpenRegularFileIfExists(root, child)
			if e != nil || f == nil {
				return 0, 0, true, e
			}
			info, e := f.Stat()
			if e = errors.Join(e, f.Close()); e != nil {
				return 0, 0, true, e
			}
			files++
			bytes += info.Size()
		case fs.KindDirectory:
			n, size, _, e := regularTreeSize(root, child)
			if e != nil {
				return 0, 0, true, e
			}
			files += n
			bytes += size
		default:
			return 0, 0, true, reviewError(child + ": not a regular file or directory")
		}
	}
	return files, bytes, true, nil
}
