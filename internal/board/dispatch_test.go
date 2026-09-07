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
)

func dispatchCard(t *testing.T, root, slug string) Snapshot {
	t.Helper()
	s := transactionCard(t, root, slug, false)
	text := readyText(s)
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: dispatch-test", 1)
	text = strings.ReplaceAll(text, "<FILL_IN>", "fixture")
	updateSnapshot(t, root, s, text)
	for _, state := range []string{"todo", "working", "review"} {
		s = transactionSnapshot(t, root, s.Entry.TaskID)
		if _, err := MoveEntry(s.Entry, root, state); err != nil {
			t.Fatal(err)
		}
	}
	return transactionSnapshot(t, root, s.Entry.TaskID)
}
func dispatchInput(s Snapshot, id string) DispatchInput {
	return DispatchInput{ID: id, TaskID: s.Entry.TaskID, Kind: "sync", Message: "修复本轮问题", Base: strings.Repeat("a", 40)}
}
func prepareTestDispatch(t *testing.T, root string, in DispatchInput) Dispatch {
	t.Helper()
	d, err := PrepareDispatch(root, in)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func dispatchMove(t *testing.T, root string, d Dispatch, target string) (Entry, error) {
	t.Helper()
	s := transactionSnapshot(t, root, d.Input.TaskID)
	o := MoveOptions{Authorization: d.Authorization}
	if target != "working" {
		o.DeliveryCommit = strings.Repeat("b", 40)
	}
	if target == "done" {
		o.Result = "completed"
	}
	return MoveWithOptions(s.Entry, root, target, o)
}

func TestDispatchIntentIdempotencyAndGlobalConflict(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "intent")
	in := dispatchInput(s, "intent-one")
	var wg sync.WaitGroup
	results := make(chan Dispatch, 4)
	failures := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); d, err := PrepareDispatch(root, in); results <- d; failures <- err }()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first Dispatch
	for d := range results {
		if first.Input.ID == "" {
			first = d
		} else if !reflect.DeepEqual(first, d) {
			t.Fatal("duplicate changed intent/deadline")
		}
	}
	if first.State != DispatchPrepared || first.Accepted != nil || first.Completed != nil {
		t.Fatal(first)
	}
	current := transactionSnapshot(t, root, s.Entry.TaskID)
	if current.Revision != s.Revision+1 {
		t.Fatalf("duplicate advanced revision: %d -> %d", s.Revision, current.Revision)
	}
	other := dispatchCard(t, root, "other")
	for _, change := range []func(*DispatchInput){func(i *DispatchInput) { i.Message += "wrong" }, func(i *DispatchInput) { i.Base = strings.Repeat("b", 40) }, func(i *DispatchInput) { i.TaskID = other.Entry.TaskID }, func(i *DispatchInput) { i.Kind = "fix" }, func(i *DispatchInput) { i.ConfirmBy = time.Now().Add(time.Hour) }} {
		bad := in
		change(&bad)
		if _, err := PrepareDispatch(root, bad); err == nil {
			t.Fatal("different input accepted")
		}
	}
	if _, err := PrepareDispatch(root, dispatchInput(s, "second")); err == nil {
		t.Fatal("two active grants")
	}
	if _, err := BeginDispatchAttempt(root, s.Entry.TaskID, in.ID, first.Revision); err != nil {
		t.Fatal(err)
	}
	retried := prepareTestDispatch(t, root, in)
	if retried.State != DispatchUnknown || retried.Accepted != nil || !retried.Input.ConfirmBy.Equal(first.Input.ConfirmBy) {
		t.Fatal(retried)
	}
	if _, err := BeginDispatchAttempt(root, s.Entry.TaskID, in.ID, first.Revision); err == nil {
		t.Fatal("stale attempt CAS accepted")
	}
}

func TestDispatchReceiptsFenceUpdatesAndFastRounds(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "receipts")
	d := prepareTestDispatch(t, root, dispatchInput(s, "round-one"))
	before := transactionSnapshot(t, root, s.Entry.TaskID)
	if _, err := dispatchMove(t, root, d, "review"); err == nil {
		t.Fatal("completion without acceptance")
	}
	if _, err := MoveEntry(before.Entry, root, "working"); err == nil {
		t.Fatal("unbound move bypassed grant")
	}
	if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "notes.md", Text: "premature", ExpectedRevision: before.Revision, Authorization: d.Authorization}); err == nil {
		t.Fatal("prepared author wrote")
	}
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	accepted := transactionSnapshot(t, root, s.Entry.TaskID)
	current, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Accepted == nil || current.Accepted.CardRevision != accepted.Revision || accepted.Entry.State != "working" {
		t.Fatal(current)
	}
	replay := false
	if _, err = MoveWithOptions(accepted.Entry, root, "working", MoveOptions{Authorization: d.Authorization, Replayed: &replay}); err != nil || !replay {
		t.Fatalf("replay %v %v", replay, err)
	}
	if got := transactionSnapshot(t, root, s.Entry.TaskID); got.Revision != accepted.Revision {
		t.Fatal("replay rewrote revision")
	}
	for _, auth := range []ExecutionAuthorization{{}, {DispatchID: d.Input.ID, Epoch: d.Authorization.Epoch + 1}, {DispatchID: "wrong", Epoch: d.Authorization.Epoch}} {
		if err = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "notes.md", Text: "stale", ExpectedRevision: accepted.Revision, Authorization: auth}); err == nil {
			t.Fatal("unauthorized body accepted")
		}
	}
	if err = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "notes.md", Text: "new content", ExpectedRevision: accepted.Revision, Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	if err = WriteManagedDocument(root, accepted.Entry, accepted.Text+"stale window"); err == nil {
		t.Fatal("stale rollback overwrote body")
	}
	if _, err = dispatchMove(t, root, d, "review"); err != nil {
		t.Fatal(err)
	}
	completed := transactionSnapshot(t, root, s.Entry.TaskID)
	current, err = ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != DispatchCompleted || current.Completed.CardRevision != completed.Revision {
		t.Fatal(current)
	}
	replay = false
	if _, err = MoveWithOptions(completed.Entry, root, "review", MoveOptions{Authorization: d.Authorization, DeliveryCommit: strings.Repeat("b", 40), Replayed: &replay}); err != nil || !replay {
		t.Fatalf("completion replay %v %v", replay, err)
	}
	if _, err = MoveWithOptions(completed.Entry, root, "review", MoveOptions{Authorization: d.Authorization, DeliveryCommit: strings.Repeat("c", 40)}); err == nil {
		t.Fatal("changed completion evidence accepted")
	}
	next := prepareTestDispatch(t, root, dispatchInput(completed, "round-two"))
	if next.Authorization.Epoch != d.Authorization.Epoch+1 {
		t.Fatal("epoch not advanced")
	}
	if _, err = dispatchMove(t, root, d, "working"); err == nil {
		t.Fatal("old epoch accepted after new intent")
	}
	if _, err = dispatchMove(t, root, next, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err = dispatchMove(t, root, next, "review"); err != nil {
		t.Fatal(err)
	}
	latest := transactionSnapshot(t, root, s.Entry.TaskID)
	if err = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "notes.md", Text: "late", ExpectedRevision: latest.Revision, Authorization: d.Authorization}); err == nil {
		t.Fatal("old executor bypassed with fresh revision")
	}
	if err = WriteManagedDocument(root, completed.Entry, completed.Text); err == nil {
		t.Fatal("old window operation revived")
	}
	data, err := os.ReadFile(filepath.Join(latest.Entry.Path, "notes.md"))
	if err != nil || string(data) != "new content" {
		t.Fatalf("new content lost: %s %v", data, err)
	}
}

func TestDispatchWrapUpTakeoverAndExpiry(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "wrap")
	in := dispatchInput(s, "wrap-one")
	bindWrapUpFixture(t, root, &in)
	d := prepareTestDispatch(t, root, in)
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	successor, err := ReauthorizeDispatch(root, s.Entry.TaskID, d.Input.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !successor.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
		t.Fatal("takeover reset deadline")
	}
	if _, err = dispatchMove(t, root, d, "done"); err == nil {
		t.Fatal("old epoch completed")
	}
	if _, err = dispatchMove(t, root, successor, "working"); err != nil {
		t.Fatal(err)
	}
	if _, err = dispatchMove(t, root, successor, "review"); err == nil {
		t.Fatal("wrap-up completed into review")
	}
	if _, err = dispatchMove(t, root, successor, "done"); err != nil {
		t.Fatal(err)
	}
	now, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	final := transactionSnapshot(t, root, s.Entry.TaskID)
	if now.Completed.State != "done" || now.Completed.CardRevision != final.Revision || MetadataFrom(final.Text, FieldResult) != "completed" {
		t.Fatal(now)
	}
	s = dispatchCard(t, root, "expiry")
	in = dispatchInput(s, "expired")
	in.CreatedAt = time.Now().Add(-time.Hour)
	in.ConfirmBy = time.Now().Add(-time.Minute)
	expired := prepareTestDispatch(t, root, in)
	if _, err = dispatchMove(t, root, expired, "working"); err == nil {
		t.Fatal("expired grant accepted")
	}
	if _, err = BeginDispatchAttempt(root, s.Entry.TaskID, expired.Input.ID, expired.Revision); err == nil {
		t.Fatal("expired grant sent")
	}
	if err = EndDispatch(root, s.Entry.TaskID, expired.Input.ID, expired.Revision, DispatchCancelled, "explicit cancellation"); err != nil {
		t.Fatal(err)
	}
	if _, err = dispatchMove(t, root, expired, "working"); err == nil {
		t.Fatal("cancelled accepted")
	}
}

func TestDispatchCLIAndManagedArtifacts(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "cli")
	in := dispatchInput(s, "")
	path := filepath.Join(t.TempDir(), "intent.json")
	b, _ := json.Marshal(in)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := capture(t, func() int { return RunDispatch([]string{"prepare", path}) })
	if code != 0 {
		t.Fatal(stderr)
	}
	var d Dispatch
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.Input.ID == "" {
		t.Fatal("missing generated ID")
	}
	args := []string{s.Entry.TaskID, "working", "--dispatch-id", d.Input.ID, "--execution-epoch", "1"}
	code, out, stderr = capture(t, func() int { return RunMove(args) })
	if code != 0 || !strings.Contains(out, `"replayed":false`) {
		t.Fatalf("%s %s", out, stderr)
	}
	fresh := transactionSnapshot(t, root, s.Entry.TaskID)
	if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: dispatchPath(d.Input.ID, "state"), Text: "forged", ExpectedRevision: fresh.Revision, Authorization: d.Authorization}); err == nil {
		t.Fatal("managed dispatch overwritten")
	}
	code, out, stderr = capture(t, func() int { return RunMove(args) })
	if code != 0 || !strings.Contains(out, `"replayed":true`) {
		t.Fatalf("%s %s", out, stderr)
	}
}

func TestDispatchFencesReviewAuthorDisposition(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "dispatch-author")
	gatePlan(t, root, []string{id}, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "原文", Evidence: "x:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	run := gateRun(t, root, archiveInput([]string{id}, "pm", "PM"), findings)
	assignGate(t, root, run, map[string][]string{f.ID: {id}})
	s := transactionSnapshot(t, root, id)
	grant := prepareTestDispatch(t, root, dispatchInput(s, "author-round"))
	if _, err := dispatchMove(t, root, grant, "working"); err != nil {
		t.Fatal(err)
	}
	d := ReviewDisposition{RecordID: "one", RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "rejected", Basis: "具体代码与契约证据"}
	s = transactionSnapshot(t, root, id)
	if err := SubmitReviewDisposition(root, d, s.Revision); err == nil {
		t.Fatal("unbound author bypassed dispatch fence")
	}
	wrong := grant.Authorization
	wrong.Epoch++
	d.Authorization = &wrong
	if err := SubmitReviewDisposition(root, d, s.Revision); err == nil {
		t.Fatal("wrong epoch submitted review disposition")
	}
	d.Authorization = &grant.Authorization
	if err := SubmitReviewDisposition(root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchLifecycleCancellationPreservesReceipts(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "cancel-lifecycle")
	d := prepareTestDispatch(t, root, dispatchInput(s, "cancel-round"))
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	if _, err := MoveWithOptions(s.Entry, root, "archived", MoveOptions{Result: "cancelled"}); err == nil {
		t.Fatal("missing lifecycle decision accepted")
	}
	if _, err := MoveWithOptions(s.Entry, root, "archived", MoveOptions{Result: "cancelled", Reason: "用户取消任务", Decision: "本次明确决定"}); err != nil {
		t.Fatal(err)
	}
	current, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != DispatchCancelled || current.Accepted == nil {
		t.Fatal("cancellation erased original receipt")
	}
}

func TestDispatchWindowWritesFromCommittedScans(t *testing.T) {
	root := tempBoard(t)
	s := dispatchCard(t, root, "scan-grant")
	d := prepareTestDispatch(t, root, dispatchInput(s, "scan-round"))
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() (Board, error){func() (Board, error) { return Scan(root) }, func() (Board, error) { return ScanTargets(root, []string{s.Entry.TaskID}) }} {
		view, err := read()
		if err != nil {
			t.Fatal(err)
		}
		entry, err := Locate(view, s.Entry.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		text, err := view.Document(s.Entry.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if err = WriteManagedDocument(root, entry, text+"\nwindow writer record\n"); err != nil {
			t.Fatal("scan lost execution cursor", err)
		}
	}
}
