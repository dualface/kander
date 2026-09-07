package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func coordinatorWrapInput(t *testing.T, root, task, dispatch string) DispatchInput {
	t.Helper()
	in := DispatchInput{ID: dispatch, TaskID: task, Kind: "wrap-up", Message: "集成后收尾", Base: strings.Repeat("b", 40)}
	in.Evidence.WrapUp = &DispatchWrapUpBinding{Artifact: ArtifactReference{task, dispatchPath(dispatch, "integration")}, Git: DispatchIntegration{DispatchID: dispatch, TaskID: task, CWD: t.TempDir(), SourceCommit: in.Base, ReviewTarget: in.Base, ReviewBase: strings.Repeat("a", 40), TargetCommit: in.Base, TargetRef: "refs/heads/develop", Author: "coordinator", Basis: "结构测试；实际 Git 另由 launch 测试", VerifiedAt: time.Now().UTC()}}
	return in
}

func TestCoordinatorWrapUpRestartsAfterPartialArchive(t *testing.T) {
	root := tempBoard(t)
	a := coordinatorCard(t, root, "first")
	b := coordinatorCard(t, root, "second")
	ids := []string{a.Entry.TaskID, b.Entry.TaskID}
	c := coordinatorClaim(t, root, ids...)
	gatePlan(t, root, ids, archiveRequirements())
	pm := gateRun(t, root, archiveInput(ids, "coordinator-pm", "PM"), emptyFindings())
	assignGate(t, root, pm, map[string][]string{})
	// A successful PM alone cannot manufacture QA or close the batch.
	if _, e := gateClose(t, root, map[string]ReviewRoleConclusion{"PM": passRole(pm)}); e == nil {
		t.Fatal("missing QA closed")
	}
	pending := coordinatorReconcile(t, root, c)
	if pending.Members[ids[0]].Review.Status != "pending" {
		t.Fatal("missing role reported closed")
	}
	qa := gateRun(t, root, archiveInput(ids, "coordinator-qa", "QA"), emptyFindings())
	assignGate(t, root, qa, map[string][]string{})
	closure, e := gateClose(t, root, map[string]ReviewRoleConclusion{"PM": passRole(pm), "QA": passRole(qa)})
	if e != nil {
		t.Fatal(e)
	}
	dispatches := []Dispatch{}
	for i, id := range ids {
		dispatches = append(dispatches, prepareTestDispatch(t, root, coordinatorWrapInput(t, root, id, []string{"wrap-first", "wrap-second"}[i])))
	}
	calls := 0
	verify := func(_ context.Context, f CoordinatorGitFacts) error {
		calls++
		if len(f.Closures) != 1 || !reflect.DeepEqual(f.Closures[0], closure) {
			t.Fatal("closure originals not consumed")
		}
		return nil
	}
	c, e = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, pending), verify)
	if e != nil {
		t.Fatal(e)
	}
	for i, d := range dispatches {
		for _, state := range []string{"working", "done"} {
			if _, e = dispatchMove(t, root, d, state); e != nil {
				t.Fatal(e)
			}
		}
		if i == 0 {
			s := transactionSnapshot(t, root, d.Input.TaskID)
			if _, e = MoveWithOptions(s.Entry, root, "archived", MoveOptions{Result: "completed", Reason: "用户确认归档", Decision: "测试决定"}); e != nil {
				t.Fatal(e)
			}
		}
		// A new consumer sees an archived first card and a still-pending second
		// card; neither requires historical review-working-done edges.
		c, e = ReadCoordinatorCheckpoint(root, coordinatorGroup)
		if e != nil {
			t.Fatal(e)
		}
		c, e = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify)
		if e != nil {
			t.Fatal(e)
		}
		if c.Members[d.Input.TaskID].Dispatch.PendingWrapUp {
			t.Fatal("completed round remains pending")
		}
	}
	if calls < 6 {
		t.Fatal("same delivery not reverified", calls)
	}
	for _, id := range ids {
		m := c.Members[id]
		if len(m.Review.Runs) != 2 || m.Dispatch.Integration.TaskID != id || m.Dispatch.PendingConfirmation || m.DeliveryCommit != strings.Repeat("b", 40) {
			t.Fatal(m)
		}
		// No-finding members did not acquire artificial author records.
		for _, run := range []ReviewRun{pm, qa} {
			v, e := ReadReviewBatchView(root, run.BatchID)
			if e != nil {
				t.Fatal(e)
			}
			for _, r := range v.Runs {
				if len(r.Records) != 0 {
					t.Fatal("fabricated author record")
				}
			}
		}
	}
	before := c
	s := transactionSnapshot(t, root, ids[0])
	if e = os.WriteFile(filepath.Join(s.Entry.Path, "reviews", pm.RunID, "report.md"), []byte("damaged"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify); e == nil {
		t.Fatal("archive lost report accepted")
	}
	after, e := ReadCoordinatorCheckpoint(root, c.GroupID)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("damage changed checkpoint", e)
	}
}

func TestCoordinatorWrapUpRequiresGitAndDedicatedGrant(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorCard(t, root, "grant")
	c := coordinatorClaim(t, root, s.Entry.TaskID)
	exemptReviewFixture(t, root, s.Entry.TaskID)
	d := prepareTestDispatch(t, root, coordinatorWrapInput(t, root, s.Entry.TaskID, "wrap-grant"))
	r := coordinatorRequest(t, root, c)
	if _, e := ReconcileCoordinator(context.Background(), root, r, nil); e == nil {
		t.Fatal("board inferred actual Git integration")
	}
	if _, e := ReconcileCoordinator(context.Background(), root, r, func(context.Context, CoordinatorGitFacts) error { return errors.New("Git ancestry absent") }); e == nil {
		t.Fatal("Git error ignored")
	}
	verify := func(context.Context, CoordinatorGitFacts) error { return nil }
	c, e := ReconcileCoordinator(context.Background(), root, r, verify)
	if e != nil {
		t.Fatal(e)
	}
	stored, e := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
	if e != nil {
		t.Fatal(e)
	}
	if stored.WrapUpAuthority != nil || stored.Authorization != d.Authorization {
		t.Fatal("reconciliation granted on-behalf authority")
	}
	// The dedicated producer still rejects absent, unknown or live identities.
	for _, outcome := range []string{"", "delivery-unknown", "alive"} {
		if _, e = AuthorizeDispatchWrapUp(root, s.Entry.TaskID, d.Input.ID, d.Revision, "coordinator", "测试", WrapUpExitEvidence{Outcome: outcome}); e == nil {
			t.Fatal("invalid exit granted")
		}
	}
	current := transactionSnapshot(t, root, s.Entry.TaskID)
	text, _ := setMetadata(current.Text, FieldSession, "original")
	if e = WriteManagedDocument(root, current.Entry, text); e != nil {
		t.Fatal(e)
	}
	current = transactionSnapshot(t, root, s.Entry.TaskID)
	exit := WrapUpExitEvidence{Outcome: "stopped", CardRevision: current.Revision, Session: "original", Window: MetadataFrom(current.Text, FieldWindow), Owner: MetadataFrom(current.Text, FieldOwner), StartedAt: MetadataFrom(current.Text, FieldStartedAt), ObservedAt: time.Now().UTC()}
	next, e := AuthorizeDispatchWrapUp(root, s.Entry.TaskID, d.Input.ID, d.Revision, "coordinator", "已确认退出，仅代收尾", exit)
	if e != nil {
		t.Fatal(e)
	}
	c, e = ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), verify)
	if e != nil {
		t.Fatal(e)
	}
	if c.Members[s.Entry.TaskID].Dispatch.WrapUpAuthority == nil || c.Members[s.Entry.TaskID].Dispatch.Epoch != next.Authorization.Epoch {
		t.Fatal("dedicated grant not consumed")
	}
	if _, e = dispatchMove(t, root, d, "working"); e == nil {
		t.Fatal("old executor not fenced")
	}
}
