package board

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
)

// pruneControlGroup holds archive cleanup intents and per-card receipts.
const pruneControlGroup = "00000000-prune-group"

// PruneReport describes one archive cleanup. Lists hold the validated IDs the
// deletions were keyed by; Err is a cleanup failure that never undoes the
// archive move that triggered it.
type PruneReport struct {
	Runs       []string
	Batches    []string
	Plans      []string
	Tasks      []string
	Dispatches []string
	Groups     []string
	Files      int
	Bytes      int64
	Warnings   []string
	Err        error
}

// Empty reports whether nothing was removed.
func (r PruneReport) Empty() bool {
	return r.Files == 0 && len(r.Runs)+len(r.Batches)+len(r.Plans)+len(r.Tasks)+len(r.Dispatches)+len(r.Groups) == 0
}

// pruneReceipt records, per card, which review evidence was removed so check
// does not report the missing originals of a final card as damage.
type pruneReceipt struct {
	Schema   int      `json:"schema"`
	TaskID   string   `json:"task_id"`
	Runs     []string `json:"runs"`
	Batches  []string `json:"batches"`
	Plans    []string `json:"plans"`
	PrunedAt string   `json:"pruned_at"`
}

type pruneIntent struct {
	Schema int    `json:"schema"`
	TaskID string `json:"task_id"`
	At     string `json:"at"`
}

func pruneIntentName(id string) string  { return "pending/" + id + ".json" }
func pruneReceiptName(id string) string { return "receipts/" + id + ".json" }

// stageArchivePruneIntent records, inside the archive move itself, that the
// card's data still has to be cleaned, so an interrupted cleanup is resumed.
func stageArchivePruneIntent(tx *Transaction, id string) error {
	b, err := json.MarshalIndent(pruneIntent{Schema: 1, TaskID: id, At: nowStamp()}, "", "  ")
	if err != nil {
		return err
	}
	return tx.PutGroup(pruneControlGroup, pruneIntentName(id), string(b)+"\n")
}

// readPruneReceipt returns the runs already removed for a card.
func readPruneReceipt(root, id string) (pruneReceipt, error) {
	var r pruneReceipt
	data, ok, err := fs.ReadRegularFileIfExists(root, control(root, "groups", pruneControlGroup, pruneReceiptName(id)))
	if err != nil || !ok {
		return r, err
	}
	if err = json.Unmarshal(data, &r); err != nil || r.Schema != 1 || r.TaskID != id {
		return r, reviewError(id + ": invalid prune receipt")
	}
	return r, nil
}

// prunePlan is the complete deletion set computed from one board snapshot.
type prunePlan struct {
	runs, batches, plans, taskPlans, starts, dispatches, groups map[string]bool
	// receipts maps a card to the evidence removed for it in this round.
	receipts map[string]*pruneReceipt
	// copies lists card-side review directories and copyFiles the plan copies;
	// both go before control records so an interruption never leaves copies
	// the next round cannot find.
	copies    []string
	copyFiles []string
	// reviews lists the card reviews directories copies were removed from.
	reviews []string
	// runLocks holds the execution lock of every run to delete, so no review
	// gate is still writing into a run while it is removed.
	runLocks lockSet
}

// PruneArchived removes evidence and control records that belong only to
// settled archived cards. It holds the exclusive board lock for the whole
// round, so it never races a publication, a reader, or another archive.
func PruneArchived(root string) (PruneReport, error) {
	return pruneArchived(root, nil)
}

// pruneArchived routes journal advisories to the caller's log; nil keeps the
// CLI's stderr presentation.
func pruneArchived(root string, warnings *WarningLog) (report PruneReport, err error) {
	if err = ensureLayout(root); err != nil {
		return report, err
	}
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		return report, err
	}
	defer func() { err = errors.Join(err, locks.close()) }()
	if err = requireNoPendingOperations(root, warnings); err != nil {
		return report, err
	}
	plan, err := planPrune(root, &report)
	if err != nil {
		return report, err
	}
	defer func() { err = errors.Join(err, plan.runLocks.close()) }()
	if err = writePruneReceipts(root, plan); err != nil {
		return report, err
	}
	var failures []error
	remove := func(path string, tree bool) {
		files, bytes, e := removePath(root, path, tree)
		report.Files += files
		report.Bytes += bytes
		if e != nil {
			failures = append(failures, e)
		}
	}
	for _, path := range plan.copies {
		remove(path, true)
	}
	for _, path := range plan.copyFiles {
		remove(path, false)
	}
	for _, dir := range plan.reviews {
		removeEmptyParents(root, dir, &failures)
	}
	review := func(parts ...string) string {
		return control(root, append([]string{"groups", reviewControlGroup}, parts...)...)
	}
	for _, id := range sortedKeys(plan.runs) {
		remove(review("runs", id), true)
	}
	for _, id := range sortedKeys(plan.batches) {
		remove(review("closures", id+".json"), false)
		remove(review("dispositions", id+".json"), false)
		remove(review("batches", id+".json"), false)
	}
	for _, id := range sortedKeys(plan.plans) {
		remove(review("plan-history", id), true)
		remove(review("plans", id+".json"), false)
	}
	for _, id := range sortedKeys(plan.taskPlans) {
		remove(review("task-plans", id+".json"), false)
		remove(review("tracked-cycles", id+".json"), false)
	}
	for _, id := range sortedKeys(plan.starts) {
		remove(control(root, "groups", taskStartGroup, id), true)
	}
	for _, name := range sortedKeys(plan.dispatches) {
		remove(control(root, "groups", dispatchRegistry, name), false)
	}
	for _, id := range sortedKeys(plan.groups) {
		remove(control(root, "groups", id), true)
	}
	report.Runs, report.Batches, report.Plans = sortedKeys(plan.runs), sortedKeys(plan.batches), sortedKeys(plan.plans)
	report.Tasks, report.Groups = sortedKeys(plan.starts), sortedKeys(plan.groups)
	for _, name := range sortedKeys(plan.dispatches) {
		report.Dispatches = append(report.Dispatches, strings.TrimSuffix(name, ".json"))
	}
	if err = errors.Join(failures...); err != nil {
		return report, err
	}
	return report, clearPruneIntents(root)
}

func requireNoPendingOperations(root string, warnings *WarningLog) error {
	var records []OperationRecord
	err := withJournalLock(root, true, func() error {
		var e error
		records, e = readPendingRecords(root, warnings)
		return e
	})
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Phase == "prepared" {
			return kanbanError("board.transaction_pending", r.ID)
		}
	}
	return nil
}

// planPrune decides every deletion from records whose IDs pass validation;
// anything it cannot read or validate is kept and reported as a warning.
func planPrune(root string, report *PruneReport) (*prunePlan, error) {
	b, err := scan(root)
	if err != nil {
		return nil, err
	}
	warn := func(name string, e error) { report.Warnings = append(report.Warnings, name+": "+e.Error()) }
	groupOf := map[string]string{}
	groupMembers := map[string][]string{}
	// An unreadable card hides its group, so no group may count as settled.
	unreadable := map[string]bool{}
	for id, e := range b.Entries {
		text, e2 := readDocument(e)
		if e2 != nil {
			warn(id, e2)
			unreadable[id] = true
			continue
		}
		if g := TaskGroupFrom(text); g != "" {
			groupOf[id] = g
			groupMembers[g] = append(groupMembers[g], id)
		}
	}
	state := func(id string) string {
		if e, ok := b.Entries[id]; ok {
			return e.State
		}
		return ""
	}
	// A group settles once no member can move again: every member is archived
	// or in trash. Until then the coordinator may still read the records of
	// its archived members, so they are kept.
	settledGroup := func(g string) bool {
		members := groupMembers[g]
		return len(unreadable) == 0 && len(members) > 0 && !slices.ContainsFunc(members, func(id string) bool {
			return state(id) != "archived" && state(id) != "trash"
		})
	}
	invalid := map[string]bool{}
	settled := func(id string) bool {
		if !taskIDRe.MatchString(id) {
			invalid[id] = true
			return false
		}
		if state(id) != "archived" || unreadable[id] {
			return false
		}
		g := groupOf[id]
		return g == "" || settledGroup(g)
	}
	allSettled := func(ids []string) bool {
		return len(ids) > 0 && !slices.ContainsFunc(ids, func(id string) bool { return !settled(id) })
	}

	review := func(parts ...string) string {
		return control(root, append([]string{"groups", reviewControlGroup}, parts...)...)
	}
	plans := map[string]ReviewPlan{}
	for _, name := range jsonNames(root, review("plans"), warn) {
		var p ReviewPlan
		if e := readControlJSON(root, review("plans", name+".json"), &p); e != nil || p.PlanID != name {
			warn("plans/"+name, errors.Join(e, reviewError("invalid plan")))
			continue
		}
		plans[name] = p
	}
	batches := map[string]ReviewBatch{}
	for _, name := range jsonNames(root, review("batches"), warn) {
		var v ReviewBatch
		if e := readControlJSON(root, review("batches", name+".json"), &v); e != nil || v.BatchID != name {
			warn("batches/"+name, errors.Join(e, reviewError("invalid batch")))
			continue
		}
		batches[name] = v
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

	plan := &prunePlan{runs: map[string]bool{}, batches: map[string]bool{}, plans: map[string]bool{}, taskPlans: map[string]bool{}, starts: map[string]bool{}, dispatches: map[string]bool{}, groups: map[string]bool{}, receipts: map[string]*pruneReceipt{}}
	for id, p := range plans {
		plan.plans[id] = allSettled(p.TaskIDs)
	}
	for id, v := range batches {
		_, hasPlan := plans[v.PlanID]
		plan.batches[id] = allSettled(v.TaskIDs) && (!hasPlan || plan.plans[v.PlanID])
	}
	for id, v := range runs {
		_, hasBatch := batches[v.BatchID]
		// Unfinished runs of final cards can never complete, so they go too,
		// unless a review gate still holds the run's execution lock.
		plan.runs[id] = allSettled(v.TaskIDs) && (!hasBatch || plan.batches[v.BatchID])
	}
	for _, id := range sortedKeys(plan.runs) {
		if !plan.runs[id] {
			continue
		}
		if e := plan.runLocks.tryTake(root, control(root, "locks", "review-run-"+id+".lock")); e != nil {
			warn("runs/"+id, e)
			plan.runs[id] = false
		}
	}
	// A kept record must keep its whole predecessor chain, its batch and its
	// plan readable.
	for changed := true; changed; {
		changed = false
		for id, v := range runs {
			if !plan.runs[id] && plan.batches[v.BatchID] {
				plan.batches[v.BatchID], changed = false, true
			}
		}
		for id, v := range runs {
			if !plan.runs[id] && plan.runs[v.PreviousRunID] {
				plan.runs[v.PreviousRunID], changed = false, true
			}
		}
		for id, v := range batches {
			if !plan.batches[id] && plan.batches[v.PreviousBatchID] {
				plan.batches[v.PreviousBatchID], changed = false, true
			}
		}
		for id, v := range runs {
			if plan.runs[id] && !plan.batches[v.BatchID] {
				if _, hasBatch := batches[v.BatchID]; hasBatch {
					plan.runs[id], changed = false, true
				}
			}
		}
		for id, v := range batches {
			if !plan.batches[id] && plan.plans[v.PlanID] {
				plan.plans[v.PlanID], changed = false, true
			}
		}
		for id, v := range batches {
			if plan.batches[id] && !plan.plans[v.PlanID] {
				if _, hasPlan := plans[v.PlanID]; hasPlan {
					plan.batches[id], changed = false, true
				}
			}
		}
	}
	dropFalse(plan.runs)
	dropFalse(plan.batches)
	dropFalse(plan.plans)

	for _, name := range jsonNames(root, review("task-plans"), warn) {
		var v struct {
			PlanID string `json:"plan_id"`
		}
		if e := readControlJSON(root, review("task-plans", name+".json"), &v); e != nil {
			warn("task-plans/"+name, e)
			continue
		}
		if _, live := plans[v.PlanID]; settled(name) && (!live || plan.plans[v.PlanID]) {
			plan.taskPlans[name] = true
		}
	}
	for _, name := range dirNames(root, control(root, "groups", taskStartGroup), warn) {
		if settled(name) {
			plan.starts[name] = true
		}
	}
	for _, name := range jsonNames(root, control(root, "groups", dispatchRegistry), warn) {
		var v struct {
			TaskID string `json:"task_id"`
		}
		if e := readControlJSON(root, control(root, "groups", dispatchRegistry, name+".json"), &v); e != nil {
			warn("dispatch/"+name, e)
			continue
		}
		if settled(v.TaskID) {
			plan.dispatches[name+".json"] = true
		}
	}
	for _, name := range dirNames(root, control(root, "groups"), warn) {
		// Reserved control groups share the task-group spelling.
		if !taskGroupRe.MatchString(name) || strings.HasPrefix(name, "00000000-") {
			continue
		}
		var v struct {
			Members map[string]json.RawMessage `json:"members"`
		}
		if e := readControlJSON(root, control(root, "groups", name, "checkpoint.json"), &v); e != nil {
			if !errors.Is(e, os.ErrNotExist) {
				warn("groups/"+name, e)
			}
			continue
		}
		if allSettled(slices.Collect(maps.Keys(v.Members))) && settledGroup(name) {
			plan.groups[name] = true
		}
	}

	receipt := func(id string) *pruneReceipt {
		if plan.receipts[id] == nil {
			plan.receipts[id] = &pruneReceipt{Schema: 1, TaskID: id}
		}
		return plan.receipts[id]
	}
	cardReviews := func(id string) string {
		e := b.Entries[id]
		if !e.IsDirectory() {
			return ""
		}
		return filepath.Join(e.Path, "reviews")
	}
	touched := map[string]bool{}
	mark := func(dir string) { touched[dir] = true }
	for id := range plan.runs {
		for _, task := range runs[id].TaskIDs {
			receipt(task).Runs = append(receipt(task).Runs, id)
			if dir := cardReviews(task); dir != "" {
				plan.copies = append(plan.copies, filepath.Join(dir, id))
				mark(dir)
			}
		}
	}
	for id := range plan.batches {
		for _, task := range batches[id].TaskIDs {
			receipt(task).Batches = append(receipt(task).Batches, id)
			if dir := cardReviews(task); dir != "" {
				plan.copies = append(plan.copies, filepath.Join(dir, "batches", id))
				mark(dir)
			}
		}
	}
	for id := range plan.plans {
		for _, task := range plans[id].TaskIDs {
			receipt(task).Plans = append(receipt(task).Plans, id)
		}
	}
	for task := range plan.taskPlans {
		if dir := cardReviews(task); dir != "" {
			plan.copyFiles = append(plan.copyFiles, filepath.Join(dir, "plan.json"))
			mark(dir)
		}
	}
	slices.Sort(plan.copies)
	slices.Sort(plan.copyFiles)
	for _, id := range sortedKeys(invalid) {
		warn(id, reviewError("invalid task ID; its records are kept"))
	}
	plan.reviews = sortedKeys(touched)
	return plan, nil
}

// writePruneReceipts merges this round into each card's receipt before any
// deletion, so check never sees removed evidence without its receipt.
func writePruneReceipts(root string, plan *prunePlan) error {
	if len(plan.receipts) == 0 {
		return nil
	}
	if err := ensurePruneDir(root, "receipts"); err != nil {
		return err
	}
	for _, id := range sortedKeys(plan.receipts) {
		r := plan.receipts[id]
		previous, err := readPruneReceipt(root, id)
		if err != nil {
			return err
		}
		r.Runs = mergeIDs(previous.Runs, r.Runs)
		r.Batches = mergeIDs(previous.Batches, r.Batches)
		r.Plans = mergeIDs(previous.Plans, r.Plans)
		r.PrunedAt = time.Now().UTC().Format(time.RFC3339)
		if err = writeJSON(root, control(root, "groups", pruneControlGroup, pruneReceiptName(id)), r, true); err != nil {
			return err
		}
	}
	return nil
}

func ensurePruneDir(root string, name string) error {
	for _, p := range []string{control(root, "groups", pruneControlGroup), control(root, "groups", pruneControlGroup, name)} {
		if err := fs.EnsurePrivateDirectory(root, p, true); err != nil {
			return err
		}
	}
	return nil
}

func clearPruneIntents(root string) error {
	dir := control(root, "groups", pruneControlGroup, "pending")
	entries, err := fs.ListDirectory(root, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Kind != fs.KindFile || !taskIDRe.MatchString(strings.TrimSuffix(entry.Name, ".json")) {
			continue
		}
		if _, err = fs.RemoveRegularFileIfExists(root, filepath.Join(dir, entry.Name)); err != nil {
			return err
		}
	}
	return nil
}

// removePath deletes one regular file, or a whole tree without following
// links; a link or special file anywhere fails the item and keeps it.
func removePath(root, path string, tree bool) (files int, bytes int64, err error) {
	if !tree {
		return removeFile(root, path)
	}
	// Directory order is filesystem-dependent, so refusing an unsafe entry
	// mid-removal could already have deleted an intent such as run.json and
	// left a damaged directory. Refuse before deleting anything.
	if err = requireRegularTree(root, path); err != nil {
		return 0, 0, err
	}
	return removeTree(root, path)
}

// requireRegularTree fails when the tree under path holds anything other
// than regular files and directories.
func requireRegularTree(root, path string) error {
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
		case fs.KindDirectory:
			if err = requireRegularTree(root, child); err != nil {
				return err
			}
		default:
			return reviewError(child + ": not a regular file or directory")
		}
	}
	return nil
}

func removeTree(root, path string) (files int, bytes int64, err error) {
	entries, err := fs.ListDirectory(root, path)
	if errors.Is(err, os.ErrNotExist) {
		return removeFile(root, path)
	}
	if err != nil {
		return 0, 0, err
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name)
		var n int
		var size int64
		switch entry.Kind {
		case fs.KindFile:
			n, size, err = removeFile(root, child)
		case fs.KindDirectory:
			n, size, err = removeTree(root, child)
		default:
			err = reviewError(child + ": not a regular file or directory")
		}
		files += n
		bytes += size
		if err != nil {
			return files, bytes, err
		}
	}
	_, err = fs.RemoveEmptyDirectoryIfExists(root, path)
	return files, bytes, err
}

func removeFile(root, path string) (int, int64, error) {
	f, err := fs.OpenRegularFileIfExists(root, path)
	if err != nil || f == nil {
		return 0, 0, err
	}
	info, err := f.Stat()
	if err = errors.Join(err, f.Close()); err != nil {
		return 0, 0, err
	}
	removed, err := fs.RemoveRegularFileIfExists(root, path)
	if err != nil || !removed {
		return 0, 0, err
	}
	return 1, info.Size(), nil
}

// removeEmptyParents drops the reviews directories a cleanup emptied.
func removeEmptyParents(root, reviews string, failures *[]error) {
	for _, dir := range []string{filepath.Join(reviews, "batches"), reviews} {
		entries, err := fs.ListDirectory(root, dir)
		if errors.Is(err, os.ErrNotExist) || err == nil && len(entries) > 0 {
			continue
		}
		if err == nil {
			_, err = fs.RemoveEmptyDirectoryIfExists(root, dir)
		}
		if err != nil {
			*failures = append(*failures, err)
		}
	}
}

func readControlJSON(root, path string, value any) error {
	data, ok, err := fs.ReadRegularFileIfExists(root, path)
	if err != nil {
		return err
	}
	if !ok {
		return os.ErrNotExist
	}
	return json.Unmarshal(data, value)
}

// dirNames lists validated child directories; other entries are left alone.
func dirNames(root, dir string, warn func(string, error)) []string {
	return listNames(root, dir, fs.KindDirectory, "", warn)
}

// jsonNames lists validated `<id>.json` children without the suffix.
func jsonNames(root, dir string, warn func(string, error)) []string {
	return listNames(root, dir, fs.KindFile, ".json", warn)
}

func listNames(root, dir string, kind fs.Kind, suffix string, warn func(string, error)) []string {
	entries, err := fs.ListDirectory(root, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		warn(filepath.Base(dir), err)
		return nil
	}
	var names []string
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name, suffix)
		if entry.Kind != kind || !ok || !ValidReviewID(name) {
			warn(filepath.Join(filepath.Base(dir), entry.Name), reviewError("unexpected entry; kept"))
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func dropFalse(m map[string]bool) {
	for k, v := range m {
		if !v {
			delete(m, k)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

func mergeIDs(a, b []string) []string {
	merged := slices.Concat(a, b)
	slices.Sort(merged)
	return slices.Compact(merged)
}

// PruneSummary renders a cleanup for the CLI and the TUI: what was removed,
// what was kept and why, and whether the cleanup still has to resume.
func PruneSummary(r PruneReport) []string {
	var lines []string
	if r.Empty() {
		lines = append(lines, t("board.prune_nothing"))
	} else {
		none := func(ids []string) string {
			if len(ids) == 0 {
				return "-"
			}
			return strings.Join(ids, ", ")
		}
		lines = append(lines, t("board.prune_summary", itoa(r.Files), formatBytes(r.Bytes), none(r.Runs), none(r.Batches), none(r.Plans), none(r.Tasks), none(r.Dispatches), none(r.Groups)))
	}
	for _, w := range r.Warnings {
		lines = append(lines, t("board.prune_kept", w))
	}
	if r.Err != nil {
		lines = append(lines, t("board.prune_failed", r.Err.Error()))
	}
	return lines
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	value, suffix := float64(n), []string{"KiB", "MiB", "GiB", "TiB"}
	i := -1
	for value >= unit && i < len(suffix)-1 {
		value /= unit
		i++
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + suffix[i]
}
