package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func archiveCard(t *testing.T, root, slug string) string {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	text := readyText(s)
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: 20260907-evidence-group", 1)
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: evidence", 1)
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
func archiveInput(ids []string, run, role string) ReviewInput {
	return ReviewInput{RunID: run, BatchID: "batch", TaskIDs: ids, Role: role, Reviewer: "codex", Model: "model", Effort: "high", CWD: "/repo", Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"}
}
func archiveRequirements() map[string]string {
	return map[string]string{"PM": "required", "QA": "required", "CSA": "N/A: project", "Hacker": "N/A: project"}
}
func archiveOriginals() map[string][]byte {
	return map[string][]byte{"task-context.md": []byte("原任务\r\n"), "review-context.md": []byte("前轮原文\n")}
}
func finalizedRun(t *testing.T, root string, input ReviewInput) ReviewRun {
	t.Helper()
	run, fresh, err := PrepareReviewRun(root, input, archiveRequirements(), nil, archiveOriginals(), "test")
	if err != nil || !fresh {
		t.Fatalf("prepare %v %v", fresh, err)
	}
	run.LaunchStatus = "started"
	run.ExecutionStatus = "ok"
	run.ExitCode = 0
	run, err = FinalizeReviewRun(root, run, []byte("PASS\n"))
	if err != nil {
		t.Fatal(err)
	}
	return run
}
func publishRun(t *testing.T, root, id string) ReviewRun {
	t.Helper()
	run, failures, err := PublishReviewRun(root, id)
	if err != nil || len(failures) > 0 {
		t.Fatalf("publish %v %v", err, failures)
	}
	return run
}
func TestReviewPublishFollowsMoveAndPreservesConcurrentBody(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "archive-move")
	run := finalizedRun(t, root, archiveInput([]string{id}, "pm-run", "PM"))
	s := transactionSnapshot(t, root, id)
	if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, id)
	updateSnapshot(t, root, s, s.Text+"\n## NOTE\n\n执行端新记录\n")
	publishRun(t, root, run.RunID)
	s = transactionSnapshot(t, root, id)
	if !strings.Contains(s.Text, "执行端新记录") || s.Entry.State != "review" {
		t.Fatal(s.Text)
	}
	if _, err := os.Stat(filepath.Join(root, "working", id)); !os.IsNotExist(err) {
		t.Fatal("old card recreated")
	}
	if err := ReviewPublicationComplete(root, run.RunID); err != nil {
		t.Fatal(err)
	}
	problems, err := CheckReviewEvidence(root, []string{id})
	if err != nil || len(problems) > 0 {
		t.Fatalf("%v %v", problems, err)
	}
	indexes, err := ParseReviewIndexes(s.Text)
	if err != nil || len(indexes) != 1 {
		t.Fatalf("%v %v", indexes, err)
	}
}
func TestReviewRunIdentityAndFrozenLanguage(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "archive-identity")
	input := archiveInput([]string{id, id}, "stable-run", "PM")
	run := finalizedRun(t, root, input)
	publishRun(t, root, run.RunID)
	if run.ReportLanguage != "zh-CN" {
		t.Fatal(run.ReportLanguage)
	}
	input.ReportLanguage = "ja"
	retry, fresh, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "changed-version")
	if err != nil || fresh || retry.ReportLanguage != "zh-CN" {
		t.Fatalf("%v %v %+v", fresh, err, retry)
	}
	input.Commit = strings.Repeat("c", 40)
	if _, _, err = PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test"); err == nil {
		t.Fatal("different target reused ID")
	}
	input.Commit = run.Commit
	changed := archiveOriginals()
	changed["task-context.md"] = []byte("changed")
	if _, _, err = PrepareReviewRun(root, input, nil, nil, changed, "test"); err == nil {
		t.Fatal("different bytes reused ID")
	}
	publishRun(t, root, run.RunID)
	s := transactionSnapshot(t, root, id)
	indexes, _ := ParseReviewIndexes(s.Text)
	if len(indexes) != 1 {
		t.Fatal(indexes)
	}
}
func TestReviewParallelRolesAndSameRunPublication(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "archive-parallel")
	pm := finalizedRun(t, root, archiveInput([]string{id}, "pm", "PM"))
	qa := finalizedRun(t, root, archiveInput([]string{id}, "qa", "QA"))
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for _, runID := range []string{pm.RunID, qa.RunID, pm.RunID, qa.RunID} {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			_, failures, err := PublishReviewRun(root, runID)
			if err != nil {
				errs <- err
			}
			for _, e := range failures {
				errs <- e
			}
		}(runID)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	s := transactionSnapshot(t, root, id)
	indexes, err := ParseReviewIndexes(s.Text)
	if err != nil || len(indexes) != 2 {
		t.Fatalf("%v %v", indexes, err)
	}
}
func TestReviewPartialPublicationAndRetry(t *testing.T) {
	root := tempBoard(t)
	a := archiveCard(t, root, "archive-a")
	b := archiveCard(t, root, "archive-b")
	run := finalizedRun(t, root, archiveInput([]string{a, b}, "partial", "PM"))
	s := transactionSnapshot(t, root, b)
	if err := os.WriteFile(filepath.Join(s.Entry.Path, "reviews"), []byte("obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
	result, failures, err := PublishReviewRun(root, run.RunID)
	if err != nil || len(failures) != 1 || !result.Published[a] || result.Published[b] {
		t.Fatalf("%+v %v %v", result, failures, err)
	}
	if err = ReviewPublicationComplete(root, run.RunID); err == nil {
		t.Fatal("partial accepted")
	}
	before := transactionSnapshot(t, root, a)
	if err = os.Remove(filepath.Join(s.Entry.Path, "reviews")); err != nil {
		t.Fatal(err)
	}
	publishRun(t, root, run.RunID)
	after := transactionSnapshot(t, root, a)
	if before.Revision != after.Revision || before.Text != after.Text {
		t.Fatal("published card rewritten")
	}
	if err = ReviewPublicationComplete(root, run.RunID); err != nil {
		t.Fatal(err)
	}
}
func TestReviewBinaryOriginalsSurviveJournal(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "archive-binary")
	input := archiveInput([]string{id}, "binary", "QA")
	run, _, err := PrepareReviewRun(root, input, archiveRequirements(), nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	stage, err := ReviewStaging(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte{0xff, 0xfe, 0, 1, '\n'}
	if err = fs.WriteTextAtomic(stage, filepath.Join(stage, "output.raw"), string(raw), true); err != nil {
		t.Fatal(err)
	}
	run.ExecutionStatus = "failed"
	run.FailureReason = "invalid output"
	run.ExitCode = 1
	run.LaunchStatus = "started"
	run, err = FinalizeReviewRun(root, run, nil)
	if err != nil {
		t.Fatal(err)
	}
	publishRun(t, root, run.RunID)
	s := transactionSnapshot(t, root, id)
	data, err := os.ReadFile(filepath.Join(s.Entry.Path, "reviews", run.RunID, "output.raw"))
	if err != nil || !reflect.DeepEqual(data, raw) {
		t.Fatalf("%v %v", data, err)
	}
	records, err := operationRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		for _, f := range record.Files {
			if strings.HasSuffix(f.Path, "originals/output.raw") && f.After == string(raw) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("journal lost bytes")
	}
}
func TestReviewBatchCASAndExplicitPredecessor(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "archive-lineage")
	original := archiveInput([]string{id}, "first", "PM")
	run := finalizedRun(t, root, original)
	publishRun(t, root, run.RunID)
	next := original
	next.RunID = "second"
	next.Commit = strings.Repeat("c", 40)
	next.ReviewedCommit = original.Commit
	next.PreviousRunID = run.RunID
	advance := &ReviewAdvance{PreviousTarget: original.Commit, Target: next.Commit, Reason: "member fix", Deliveries: map[string]string{next.Commit: id}}
	if _, _, err := PrepareReviewRun(root, next, nil, nil, archiveOriginals(), "test"); err == nil {
		t.Fatal("advance without CAS")
	}
	next.PreviousRunID = "missing"
	if _, _, err := PrepareReviewRun(root, next, nil, advance, archiveOriginals(), "test"); err == nil {
		t.Fatal("missing predecessor")
	}
	next.PreviousRunID = run.RunID
	fixed, _, err := PrepareReviewRun(root, next, nil, advance, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	fixed.ExecutionStatus = "ok"
	fixed.LaunchStatus = "started"
	fixed, err = FinalizeReviewRun(root, fixed, []byte("PASS"))
	if err != nil {
		t.Fatal(err)
	}
	publishRun(t, root, fixed.RunID)
	problems, err := CheckReviewEvidence(root, []string{id})
	if err != nil || len(problems) > 0 {
		t.Fatalf("%v %v", problems, err)
	}
	third := next
	third.RunID = "third"
	third.Commit = strings.Repeat("d", 40)
	if _, _, err := PrepareReviewRun(root, third, nil, advance, archiveOriginals(), "test"); err == nil {
		t.Fatal("stale CAS")
	}
	s := transactionSnapshot(t, root, id)
	if err := os.WriteFile(filepath.Join(s.Entry.Path, "reviews", run.RunID, "report.md"), []byte("tampered prior report"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ReviewPublicationComplete(root, fixed.RunID); err == nil {
		t.Fatal("tampered predecessor accepted")
	}

}
func TestReviewCheckDetectsTamperingAndIncompleteIntent(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "archive-check")
	input := archiveInput([]string{id}, "check-run", "PM")
	run, _, err := PrepareReviewRun(root, input, archiveRequirements(), nil, archiveOriginals(), "test")
	if err != nil {
		t.Fatal(err)
	}
	problems, err := CheckReviewEvidence(root, []string{id})
	if err != nil || len(problems) == 0 {
		t.Fatalf("%v %v", problems, err)
	}
	run.ExecutionStatus = "interrupted"
	run.ExitCode = 2
	run.FailureReason = "gate crashed"
	run, err = FinalizeReviewRun(root, run, nil)
	if err != nil {
		t.Fatal(err)
	}
	publishRun(t, root, run.RunID)
	s := transactionSnapshot(t, root, id)
	p := filepath.Join(s.Entry.Path, "reviews", run.RunID, "task-context.md")
	if err = os.WriteFile(p, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	problems, err = CheckReviewEvidence(root, []string{id})
	if err != nil || len(problems) == 0 {
		t.Fatalf("%v %v", problems, err)
	}
	if err = ReviewPublicationComplete(root, run.RunID); err == nil {
		t.Fatal("tampered archive accepted")
	}
}
func TestReviewIndexBeforeOtherSections(t *testing.T) {
	text := "# card\n\n## REVIEWS\n\n## SUMMARY\n\nretain\n"
	for _, id := range []string{"a", "b"} {
		b, _ := json.Marshal(ReviewIndex{RunID: id, BatchID: "batch", Role: "PM"})
		var err error
		text, err = appendReviewIndex(text, string(b))
		if err != nil {
			t.Fatal(err)
		}
	}
	indexes, err := ParseReviewIndexes(text)
	if err != nil || len(indexes) != 2 || !strings.Contains(text, "## SUMMARY\n\nretain") {
		t.Fatalf("%v %v %s", indexes, err, text)
	}
}

func TestReviewPublishRacesMoveAndControlledUpdate(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "race-move-update")
	finalizedRun(t, root, archiveInput([]string{id}, "race-pm", "PM"))
	finalizedRun(t, root, archiveInput([]string{id}, "race-qa", "QA"))
	start := make(chan struct{})
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for _, runID := range []string{"race-pm", "race-qa"} {
		wg.Add(1)
		go func(runID string) {
			defer wg.Done()
			<-start
			_, failed, err := PublishReviewRun(root, runID)
			if err != nil {
				errs <- err
			}
			for _, err := range failed {
				errs <- err
			}
		}(runID)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		var last error
		for attempt := 0; attempt < 10; attempt++ {
			s, err := ReadSnapshot(root, id)
			if err != nil {
				last = err
				continue
			}
			if s.Entry.State == "review" {
				return
			}
			_, last = MoveEntry(s.Entry, root, "review")
			if last == nil {
				return
			}
		}
		errs <- last
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		var last error
		for attempt := 0; attempt < 10; attempt++ {
			s, err := ReadSnapshot(root, id)
			if err != nil {
				last = err
				continue
			}
			last = UpdateDocument(root, id, UpdateOptions{Document: "spec.md", Text: s.Text + "\n## LIVE_NOTE\n\nnew owner record\n", ExpectedRevision: s.Revision})
			if last == nil {
				return
			}
		}
		errs <- last
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	s := transactionSnapshot(t, root, id)
	indexes, err := ParseReviewIndexes(s.Text)
	if err != nil || len(indexes) != 2 || s.Entry.State != "review" || !strings.Contains(s.Text, "new owner record") {
		t.Fatalf("%+v %v %s", indexes, err, s.Text)
	}
	if _, err = os.Stat(filepath.Join(root, "working", id)); !os.IsNotExist(err) {
		t.Fatal("old root exists")
	}
}

func TestReviewRefusesChangedMembershipLanguageAndRequirements(t *testing.T) {
	root := tempBoard(t)
	a := archiveCard(t, root, "binding-a")
	b := archiveCard(t, root, "binding-b")
	original := archiveInput([]string{a, b}, "binding", "PM")
	run := finalizedRun(t, root, original)
	publishRun(t, root, run.RunID)
	changed := original
	changed.RunID = "wrong-members"
	changed.TaskIDs = []string{a}
	if _, _, err := PrepareReviewRun(root, changed, nil, nil, archiveOriginals(), "test"); err == nil {
		t.Fatal("changed batch membership")
	}
	changed = original
	changed.RunID = "wrong-requirements"
	requirements := archiveRequirements()
	requirements["QA"] = "N/A: changed"
	if _, _, err := PrepareReviewRun(root, changed, requirements, nil, archiveOriginals(), "test"); err == nil {
		t.Fatal("changed requirements")
	}
	s := transactionSnapshot(t, root, a)
	if err := WithTransaction(root, LockScope{Tasks: []string{a}}, func(tx *Transaction) error {
		return tx.Put(a, "spec.md", strings.Replace(s.Text, "- LANGUAGE: zh-CN", "- LANGUAGE: ja", 1))
	}); err != nil {
		t.Fatal(err)
	}
	changed = original
	changed.RunID = "wrong-language"
	if _, _, err := PrepareReviewRun(root, changed, nil, nil, archiveOriginals(), "test"); err == nil {
		t.Fatal("mixed language")
	}
	problems, err := CheckReviewEvidence(root, []string{a})
	if err != nil || len(problems) == 0 {
		t.Fatalf("%v %v", problems, err)
	}
}

func TestReviewRunLeaseSerializesIndependentCallers(t *testing.T) {
	root := tempBoard(t)
	unlock, err := LockReviewRun(root, "lease")
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan func() error, 1)
	failed := make(chan error, 1)
	go func() {
		release, err := LockReviewRun(root, "lease")
		if err != nil {
			failed <- err
			return
		}
		acquired <- release
	}()
	select {
	case release := <-acquired:
		_ = release()
		t.Fatal("lease did not block")
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(20 * time.Millisecond):
	}
	if err = unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case release := <-acquired:
		if err = release(); err != nil {
			t.Fatal(err)
		}
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("lease not released")
	}
}

func TestReviewBatchFreezesFallbackAcrossLaterRoles(t *testing.T) {
	root := tempBoard(t)
	id := archiveCard(t, root, "missing-language")
	s := transactionSnapshot(t, root, id)
	if err := WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
		return tx.Put(id, "spec.md", strings.Replace(s.Text, "- LANGUAGE: zh-CN\n", "", 1))
	}); err != nil {
		t.Fatal(err)
	}
	first := archiveInput([]string{id}, "fallback-pm", "PM")
	run := finalizedRun(t, root, first)
	publishRun(t, root, run.RunID)
	if run.ReportLanguage != "en" {
		t.Fatal(run.ReportLanguage)
	}
	next := archiveInput([]string{id}, "fallback-qa", "QA")
	next.ReportLanguage = "ja"
	run, _, err := PrepareReviewRun(root, next, nil, nil, archiveOriginals(), "test")
	if err != nil || run.ReportLanguage != "en" {
		t.Fatalf("%+v %v", run, err)
	}
}
