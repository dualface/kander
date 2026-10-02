package board

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/internal/fs"
)

// pruneCard creates a working directory card, optionally in a task group.
func pruneCard(t *testing.T, root, slug, group string) string {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	text := readyText(s)
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: prune", 1)
	err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, ExclusiveBoard: true}, func(tx *Transaction) error {
		if err := tx.Put(s.Entry.TaskID, "spec.md", text); err != nil {
			return err
		}
		return tx.Relocate(s.Entry.TaskID, "working")
	})
	if err != nil {
		t.Fatal(err)
	}
	return s.Entry.TaskID
}

func pruneRun(t *testing.T, root, run, batch string, ids ...string) {
	t.Helper()
	input := archiveInput(ids, run, "PMQA")
	input.BatchID = batch
	publishRun(t, root, finalizedRun(t, root, input).RunID)
}

func archiveWithReport(t *testing.T, root, id string) PruneReport {
	t.Helper()
	s := transactionSnapshot(t, root, id)
	var report PruneReport
	if _, err := MoveWithOptions(s.Entry, root, "archived", MoveOptions{Result: "cancelled", Reason: "用户取消", Decision: "取消决定", Pruned: &report}); err != nil {
		t.Fatal(err)
	}
	return report
}

func reviewPath(root string, parts ...string) string {
	return control(root, append([]string{"groups", reviewControlGroup}, parts...)...)
}

func requireExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if want && err != nil || !want && !os.IsNotExist(err) {
		t.Fatalf("%s exists=%v: %v", path, !want, err)
	}
}

func requirePrunedCheck(t *testing.T, root string, ids ...string) {
	t.Helper()
	if problems, err := CheckReviewEvidence(root, ids); err != nil || len(problems) > 0 {
		t.Fatalf("check after prune: %v %+v", err, problems)
	}
	if code, _, stderr, err := CheckBoard(root, nil, true); err != nil || code != 0 {
		t.Fatalf("check --all after prune: %d %v %v", code, stderr, err)
	}
}

func pruneIntents(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(control(root, "groups", pruneControlGroup, "pending"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestArchivePrunesSingleCardEvidence(t *testing.T) {
	root := tempBoard(t)
	id := pruneCard(t, root, "prune-single", "")
	pruneRun(t, root, "prune-single-run", "prune-single-batch", id)
	card := transactionSnapshot(t, root, id)
	if err := UpdateDocument(root, id, UpdateOptions{Document: "report.md", Text: "报告\n", ExpectedRevision: card.Revision}); err != nil {
		t.Fatal(err)
	}
	report := archiveWithReport(t, root, id)
	if report.Err != nil || len(report.Warnings) > 0 || !slices.Equal(report.Runs, []string{"prune-single-run"}) || !slices.Equal(report.Batches, []string{"prune-single-batch"}) || report.Files == 0 || report.Bytes == 0 {
		t.Fatalf("report: %+v", report)
	}
	s := transactionSnapshot(t, root, id)
	requireExists(t, filepath.Join(s.Entry.Path, "reviews"), false)
	requireExists(t, filepath.Join(s.Entry.Path, "report.md"), true)
	requireExists(t, reviewPath(root, "runs", "prune-single-run"), false)
	requireExists(t, reviewPath(root, "batches", "prune-single-batch.json"), false)
	if indexes, err := ParseReviewIndexes(s.Text); err != nil || len(indexes) != 1 {
		t.Fatalf("REVIEWS index changed: %v %v", indexes, err)
	}
	receipt, err := readPruneReceipt(root, id)
	if err != nil || !slices.Equal(receipt.Runs, []string{"prune-single-run"}) {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	if intents := pruneIntents(t, root); len(intents) != 0 {
		t.Fatalf("intents left: %v", intents)
	}
	requirePrunedCheck(t, root, id)
}

func TestArchiveKeepsSharedEvidenceUntilEveryMemberIsArchived(t *testing.T) {
	root := tempBoard(t)
	a, b := pruneCard(t, root, "prune-shared-a", "20261002-prune-shared-group"), pruneCard(t, root, "prune-shared-b", "20261002-prune-shared-group")
	pruneRun(t, root, "prune-shared-run", "prune-shared-batch", a, b)
	if report := archiveWithReport(t, root, a); report.Err != nil || !report.Empty() {
		t.Fatalf("shared evidence removed early: %+v", report)
	}
	requireExists(t, reviewPath(root, "runs", "prune-shared-run"), true)
	requireExists(t, filepath.Join(transactionSnapshot(t, root, a).Entry.Path, "reviews", "prune-shared-run"), true)
	if problems, err := CheckReviewEvidence(root, []string{a, b}); err != nil || len(problems) > 0 {
		t.Fatalf("live member evidence broken: %v %+v", err, problems)
	}
	report := archiveWithReport(t, root, b)
	if report.Err != nil || !slices.Equal(report.Runs, []string{"prune-shared-run"}) {
		t.Fatalf("report: %+v", report)
	}
	for _, id := range []string{a, b} {
		requireExists(t, filepath.Join(transactionSnapshot(t, root, id).Entry.Path, "reviews"), false)
	}
	requireExists(t, reviewPath(root, "runs", "prune-shared-run"), false)
	requirePrunedCheck(t, root, a, b)
}

// writeControl creates a synthetic control record the pruner must classify.
func writeControl(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestArchivePrunesStartDispatchAndGroupRecordsOnlyWhenGroupSettles(t *testing.T) {
	root := tempBoard(t)
	group := "20261002-prune-fixture-group"
	a, b := pruneCard(t, root, "prune-group-a", group), pruneCard(t, root, "prune-group-b", group)
	// A trashed member can never move again, so it does not hold the group open.
	dropped := transactionSnapshot(t, root, pruneCard(t, root, "prune-group-c", group))
	if _, err := MoveWithOptions(dropped.Entry, root, "trash", MoveOptions{Result: "trashed", Reason: "删除", Decision: "删除决定"}); err != nil {
		t.Fatal(err)
	}
	start := control(root, "groups", taskStartGroup, a, "current.json")
	dispatch := control(root, "groups", dispatchRegistry, "0123456789abcdef0123456789abcdef.json")
	shared := control(root, "groups", dispatchRegistry, "fedcba9876543210fedcba9876543210.json")
	checkpoint := control(root, "groups", group, "checkpoint.json")
	writeControl(t, start, "{}\n")
	writeControl(t, dispatch, `{"task_id":"`+a+`"}`)
	writeControl(t, shared, `{"task_id":"`+b+`"}`)
	writeControl(t, checkpoint, `{"members":{"`+a+`":{},"`+b+`":{}}}`)
	if report := archiveWithReport(t, root, a); report.Err != nil || !report.Empty() {
		t.Fatalf("unsettled group pruned: %+v", report)
	}
	for _, path := range []string{start, dispatch, shared, checkpoint} {
		requireExists(t, path, true)
	}
	report := archiveWithReport(t, root, b)
	dispatches := []string{"0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"}
	if report.Err != nil || !slices.Equal(report.Tasks, []string{a}) || !slices.Equal(report.Dispatches, dispatches) || !slices.Equal(report.Groups, []string{group}) {
		t.Fatalf("report: %+v", report)
	}
	for _, path := range []string{filepath.Dir(start), dispatch, shared, filepath.Dir(checkpoint)} {
		requireExists(t, path, false)
	}
}

func TestArchivePruneLeavesUnlistedAndUntrustedData(t *testing.T) {
	root := tempBoard(t)
	id := pruneCard(t, root, "prune-keep", "")
	pruneRun(t, root, "prune-keep-run", "prune-keep-batch", id)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	writeControl(t, outside, "keep")
	kept := []string{
		control(root, "versions", id+".json"),
		control(root, "issue-results", "v1", "state.json"),
		reviewPath(root, "unknown-kind", "x.json"),
		control(root, "groups", taskStartGroup, "not-a-task", "current.json"),
	}
	for _, path := range kept[1:] {
		writeControl(t, path, "{}\n")
	}
	evil := control(root, "groups", dispatchRegistry, "abcdefabcdefabcdefabcdefabcdefab.json")
	writeControl(t, evil, `{"task_id":"../../`+filepath.Base(filepath.Dir(outside))+`"}`)
	kept = append(kept, evil)
	forged := reviewPath(root, "runs", "forged-run", "run.json")
	writeControl(t, forged, `{"run_id":"forged-run","phase":"finalized","task_ids":["../escape"],"published":{"../escape":true}}`)
	kept = append(kept, forged)
	report := archiveWithReport(t, root, id)
	if report.Err != nil {
		t.Fatal(report.Err)
	}
	for _, want := range []string{"../escape", "not-a-task", "../../"} {
		if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, want) }) {
			t.Fatalf("no warning for %s: %v", want, report.Warnings)
		}
	}
	for _, path := range append(kept, outside) {
		requireExists(t, path, true)
	}
	requireExists(t, reviewPath(root, "runs", "prune-keep-run"), false)
}

func TestArchivePruneRefusesLinksAndResumes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := tempBoard(t)
	id := pruneCard(t, root, "prune-link", "")
	pruneRun(t, root, "prune-link-run", "prune-link-batch", id)
	target := filepath.Join(t.TempDir(), "victim.txt")
	writeControl(t, target, "keep")
	link := reviewPath(root, "runs", "prune-link-run", "inputs", "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, id)
	var report PruneReport
	moved, err := MoveWithOptions(s.Entry, root, "archived", MoveOptions{Result: "cancelled", Reason: "取消", Decision: "取消决定", Pruned: &report})
	if err != nil || moved.State != "archived" || report.Err == nil {
		t.Fatalf("cleanup failure must keep the archive: %v %+v", err, report)
	}
	requireExists(t, target, true)
	requireExists(t, reviewPath(root, "runs", "prune-link-run", "run.json"), true)
	requireExists(t, reviewPath(root, "runs", "prune-link-run", "inputs"), true)
	if intents := pruneIntents(t, root); !slices.Equal(intents, []string{id + ".json"}) {
		t.Fatalf("intent not kept: %v", intents)
	}
	requirePrunedCheck(t, root, id)
	if err = os.Remove(link); err != nil {
		t.Fatal(err)
	}
	var pruned PruneReport
	if code, _, stderr := capture(t, func() int { return RunInit(nil) }); code != 0 {
		t.Fatalf("init: %s", stderr)
	} else if !strings.Contains(stderr, "prune-link-run") {
		t.Fatalf("init summary: %s", stderr)
	}
	requireExists(t, reviewPath(root, "runs", "prune-link-run"), false)
	if intents := pruneIntents(t, root); len(intents) != 0 {
		t.Fatalf("intent left: %v", intents)
	}
	if pruned, err = PruneArchived(root); err != nil || !pruned.Empty() {
		t.Fatalf("second round: %+v %v", pruned, err)
	}
	requirePrunedCheck(t, root, id)
}

// Cards archived by an older binary carry no intent; init backfills them.
func TestInitBackfillsCardsArchivedBeforeUpgrade(t *testing.T) {
	root := tempBoard(t)
	id := pruneCard(t, root, "prune-backfill", "")
	pruneRun(t, root, "prune-backfill-run", "prune-backfill-batch", id)
	s := transactionSnapshot(t, root, id)
	text, err := moveMetadata(s.Text, "working", "archived", MoveOptions{Result: "cancelled", Reason: "取消", Decision: "取消决定"})
	if err != nil {
		t.Fatal(err)
	}
	if err = WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		if e := tx.Put(id, "spec.md", text); e != nil {
			return e
		}
		return tx.Relocate(id, "archived")
	}); err != nil {
		t.Fatal(err)
	}
	requireExists(t, reviewPath(root, "runs", "prune-backfill-run"), true)
	var first, second PruneReport
	if _, _, _, _, err = InitBoardWithOptions("", InitOptions{Pruned: &first}); err != nil || first.Err != nil || !slices.Equal(first.Runs, []string{"prune-backfill-run"}) {
		t.Fatalf("backfill: %+v %v", first, err)
	}
	if _, _, _, _, err = InitBoardWithOptions("", InitOptions{Pruned: &second}); err != nil || !second.Empty() {
		t.Fatalf("second init: %+v %v", second, err)
	}
	requirePrunedCheck(t, root, id)
}

func TestConcurrentArchivesPruneSharedEvidenceOnce(t *testing.T) {
	root := tempBoard(t)
	a, b := pruneCard(t, root, "prune-race-a", "20261002-prune-race-group"), pruneCard(t, root, "prune-race-b", "20261002-prune-race-group")
	pruneRun(t, root, "prune-race-run", "prune-race-batch", a, b)
	reports := make([]PruneReport, 2)
	var wg sync.WaitGroup
	for i, id := range []string{a, b} {
		wg.Go(func() { reports[i] = archiveWithReport(t, root, id) })
	}
	wg.Wait()
	removed := 0
	for _, r := range reports {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		if slices.Contains(r.Runs, "prune-race-run") {
			removed++
		}
	}
	if removed != 1 {
		t.Fatalf("shared run removed %d times: %+v", removed, reports)
	}
	requireExists(t, reviewPath(root, "runs", "prune-race-run"), false)
	if intents := pruneIntents(t, root); len(intents) != 0 {
		t.Fatalf("intents left: %v", intents)
	}
	requirePrunedCheck(t, root, a, b)
}

func TestMoveCommandPrintsPruneSummary(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := pruneCard(t, root, "prune-cli", "")
	pruneRun(t, root, "prune-cli-run", "prune-cli-batch", id)
	code, out, stderr := capture(t, func() int {
		return RunMove([]string{id, "archived", "--result", "cancelled", "--reason", "取消", "--decision", "取消决定"})
	})
	if code != 0 || !strings.Contains(out, filepath.Join("archived", id)) {
		t.Fatalf("move: %d %s %s", code, out, stderr)
	}
	if !strings.Contains(stderr, "prune-cli-run") || !strings.Contains(stderr, "prune-cli-batch") || !strings.Contains(stderr, " B") && !strings.Contains(stderr, "KiB") {
		t.Fatalf("summary: %s", stderr)
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KiB", 5 << 20: "5.0 MiB"} {
		if got := formatBytes(n); got != want {
			t.Fatalf("%d: %s", n, got)
		}
	}
}

func TestArchivePrunesPlanClosureAndTaskPlans(t *testing.T) {
	root := tempBoard(t)
	id := pruneCard(t, root, "prune-plan", "")
	p := gatePlan(t, root, []string{id}, archiveRequirements())
	run := gateRun(t, root, archiveInput([]string{id}, "prune-plan-run", "PMQA"), emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)}); err != nil {
		t.Fatal(err)
	}
	card := transactionSnapshot(t, root, id).Entry.Path
	controlled := []string{
		reviewPath(root, "plans", p.PlanID+".json"),
		reviewPath(root, "batches", "batch.json"),
		reviewPath(root, "closures", "batch.json"),
		reviewPath(root, "task-plans", id+".json"),
		filepath.Join(card, "reviews", "plan.json"),
		filepath.Join(card, "reviews", "batches", "batch"),
	}
	for _, path := range controlled {
		requireExists(t, path, true)
	}
	report := archiveWithReport(t, root, id)
	if report.Err != nil || len(report.Warnings) > 0 || !slices.Equal(report.Plans, []string{p.PlanID}) || !slices.Equal(report.Batches, []string{"batch"}) {
		t.Fatalf("report: %+v", report)
	}
	for _, path := range controlled {
		requireExists(t, path, false)
	}
	requirePrunedCheck(t, root, id)
}

func TestArchivePrunesUnfinishedRunsUnlessAGateHoldsThem(t *testing.T) {
	root := tempBoard(t)
	id := pruneCard(t, root, "prune-unfinished", "")
	for _, run := range []string{"prune-unfinished-run", "prune-busy-run"} {
		input := archiveInput([]string{id}, run, "PMQA")
		input.BatchID = run + "-batch"
		if _, _, err := PrepareReviewRun(root, input, archiveRequirements(), nil, archiveOriginals(), "test"); err != nil {
			t.Fatal(err)
		}
	}
	f, err := fs.OpenLockFile(root, control(root, "locks", "review-run-prune-busy-run.lock"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := fs.LockExclusive(f)
	if err != nil {
		t.Fatal(err)
	}
	report := archiveWithReport(t, root, id)
	if report.Err != nil || !slices.Equal(report.Runs, []string{"prune-unfinished-run"}) {
		t.Fatalf("report: %+v", report)
	}
	if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, "prune-busy-run") }) {
		t.Fatalf("busy run not reported: %v", report.Warnings)
	}
	requireExists(t, reviewPath(root, "runs", "prune-unfinished-run"), false)
	requireExists(t, reviewPath(root, "runs", "prune-busy-run"), true)
	requireExists(t, reviewPath(root, "batches", "prune-busy-run-batch.json"), true)
	if err = errors.Join(lock.Unlock(), f.Close()); err != nil {
		t.Fatal(err)
	}
	if next, err := PruneArchived(root); err != nil || !slices.Equal(next.Runs, []string{"prune-busy-run"}) {
		t.Fatalf("released run: %+v %v", next, err)
	}
	requirePrunedCheck(t, root, id)
}

func TestUnreadableCardKeepsGroupRecords(t *testing.T) {
	root := tempBoard(t)
	group := "20261002-prune-unreadable-group"
	a, b := pruneCard(t, root, "prune-unreadable-a", group), pruneCard(t, root, "prune-unreadable-b", group)
	checkpoint := control(root, "groups", group, "checkpoint.json")
	writeControl(t, checkpoint, `{"members":{"`+a+`":{}}}`)
	if err := os.WriteFile(transactionSnapshot(t, root, b).Entry.Document, []byte("# broken\n\xff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := archiveWithReport(t, root, a)
	if report.Err != nil || len(report.Groups) > 0 {
		t.Fatalf("report: %+v", report)
	}
	requireExists(t, checkpoint, true)
	if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, b) }) {
		t.Fatalf("unreadable card not reported: %v", report.Warnings)
	}
}

func TestRemovePathRefusesUnsafeTreeBeforeDeleting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	requireRemovePathRefuses(t, func(dir string) error {
		return os.Symlink(filepath.Join(t.TempDir(), "victim"), filepath.Join(dir, "inputs", "link"))
	})
}

// requireRemovePathRefuses builds a run-like tree, adds an unsafe entry with
// addUnsafe, and requires removePath to refuse it without deleting anything.
// Listing order is filesystem-dependent and cannot be forced. Creating the
// unsafe entry first puts it last on tmpfs, which lists newest first, and 64
// siblings make it all but certain that a hashed listing also reaches a
// regular file before it, so an order-dependent removal fails this check.
func requireRemovePathRefuses(t *testing.T, addUnsafe func(dir string) error) {
	t.Helper()
	root := tempBoard(t)
	dir := reviewPath(root, "runs", "remove-unsafe")
	if err := os.MkdirAll(filepath.Join(dir, "inputs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := addUnsafe(dir); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i := range 64 {
		path := filepath.Join(dir, fmt.Sprintf("f%02d.json", i))
		writeControl(t, path, "{}\n")
		paths = append(paths, path)
	}
	nested := filepath.Join(dir, "inputs", "input.json")
	writeControl(t, nested, "{}\n")
	paths = append(paths, nested)
	files, bytes, err := removePath(root, dir, true)
	if err == nil || files != 0 || bytes != 0 {
		t.Fatalf("unsafe tree must be refused untouched: %d %d %v", files, bytes, err)
	}
	for _, path := range paths {
		requireExists(t, path, true)
	}
}
