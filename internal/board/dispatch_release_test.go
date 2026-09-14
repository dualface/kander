package board

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func endDispatchCommand(t *testing.T, d Dispatch, verb, decision string) {
	t.Helper()
	args := []string{verb, d.Input.TaskID, d.Input.ID, strconv.FormatUint(d.Revision, 10), "executor stopped"}
	if decision != "" {
		args = append(args, "--decision", decision)
	}
	if code := RunDispatch(args); code != 0 {
		t.Fatalf("dispatch %s: %d", verb, code)
	}
}

func TestDispatchReleaseCommandsFenceOldAuthorization(t *testing.T) {
	for _, verb := range []string{"fail", "cancel"} {
		t.Run(verb, func(t *testing.T) {
			root := tempBoard(t)
			s := dispatchCard(t, root, "release")
			d := prepareTestDispatch(t, root, dispatchInput(s, "release-one"))
			if RunMove([]string{s.Entry.TaskID, "working", "--owner", "claude", "--decision", "user-reclaim"}) == 0 {
				t.Fatal("active grant reclaimed")
			}
			before := transactionSnapshot(t, root, s.Entry.TaskID)
			if RunDispatch([]string{verb, s.Entry.TaskID, d.Input.ID, "999", "reason", "--decision", "decision"}) == 0 {
				t.Fatal("stale termination accepted")
			}
			if RunDispatch([]string{verb, s.Entry.TaskID, d.Input.ID, "1", "reason", "--decision", " "}) == 0 {
				t.Fatal("empty explicit decision accepted")
			}
			if got := transactionSnapshot(t, root, s.Entry.TaskID); got.Revision != before.Revision {
				t.Fatal("rejected command wrote card")
			}
			endDispatchCommand(t, d, verb, "user-stop")
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			historical, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
			if err != nil {
				t.Fatal(err)
			}
			if authFrom(s.Text) != (ExecutionAuthorization{}) || s.Revision != before.Revision+1 || historical.Release == nil || historical.Release.Decision != "user-stop" || historical.Release.Termination.Decision != "user-stop" {
				t.Fatal("termination and release not atomic", historical)
			}
			for _, target := range []string{"working", "review", "done"} {
				o := MoveOptions{Authorization: d.Authorization, DeliveryCommit: strings.Repeat("b", 40), Disposition: &ArtifactReference{TaskID: s.Entry.TaskID, Path: "spec.md"}}
				if target == "working" {
					o.DeliveryCommit = ""
					o.Disposition = nil
				}
				if _, err := MoveWithOptions(s.Entry, root, target, o); err == nil {
					t.Fatal("old epoch moved", target)
				}
			}
			if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: s.Text + "\nold writer\n", ExpectedRevision: s.Revision, Authorization: d.Authorization}); err == nil {
				t.Fatal("old epoch authored")
			}
			stale := s.Entry
			stale.Version.authorization = d.Authorization
			if err := WriteManagedDocument(root, stale, strings.Replace(s.Text, "- WINDOW:", "- WINDOW: stale", 1)); err == nil {
				t.Fatal("old epoch changed WINDOW")
			}
			if RunMove([]string{s.Entry.TaskID, "working", "--owner", "claude"}) == 0 {
				t.Fatal("released reclaim lacked decision")
			}
			if RunMove([]string{s.Entry.TaskID, "working", "--owner", "claude", "--decision", "user-reclaim"}) != 0 {
				t.Fatal("authorized reclaim failed")
			}
			current := transactionSnapshot(t, root, s.Entry.TaskID)
			if MetadataFrom(current.Text, FieldOwner) != "claude" || MetadataFrom(current.Text, FieldStartedAt) == "" || planCycle(current) == planCycle(s) || !strings.Contains(current.Text, `"decision_reference":"user-reclaim"`) {
				t.Fatal("missing handoff")
			}
			if err := UpdateDocument(root, current.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: current.Text + "\nold writer\n", ExpectedRevision: current.Revision, Authorization: d.Authorization}); err == nil {
				t.Fatal("old epoch survived reclaim")
			}
			if _, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID); err != nil {
				t.Fatal("history lost", err)
			}
		})
	}
}

func TestLegacyTerminalReleasePreservesOriginalsAndRejectsNewBinding(t *testing.T) {
	for _, state := range []DispatchState{DispatchFailed, DispatchCancelled} {
		t.Run(string(state), func(t *testing.T) {
			root := tempBoard(t)
			s := dispatchCard(t, root, "legacy-release")
			d := prepareTestDispatch(t, root, dispatchInput(s, "legacy-one"))
			if _, err := dispatchMove(t, root, d, "working"); err != nil {
				t.Fatal(err)
			}
			d, _ = ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
			// Reproduce a pre-upgrade state, whose terminal command retained the binding.
			d.State, d.Reason, d.Revision = state, "historical reason", d.Revision+1
			if err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error { return putDispatch(tx, d) }); err != nil {
				t.Fatal(err)
			}
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			original, err := os.ReadFile(filepath.Join(s.Entry.Path, dispatchPath(d.Input.ID, "intent")))
			if err != nil {
				t.Fatal(err)
			}
			if err := EndDispatch(root, s.Entry.TaskID, d.Input.ID, d.Revision, state, "new reason"); err == nil {
				t.Fatal("legacy release without decision")
			}
			verb := "fail"
			if state == DispatchCancelled {
				verb = "cancel"
			}
			endDispatchCommand(t, d, verb, "legacy-release-decision")
			released, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
			if err != nil || released.Reason != "historical reason" || released.Accepted == nil || !released.Release.Legacy {
				t.Fatal("legacy history replaced", err)
			}
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			if _, err := os.Stat(filepath.Join(s.Entry.Path, dispatchPath(d.Input.ID, "termination"))); !os.IsNotExist(err) {
				t.Fatal("legacy termination fabricated", err)
			}
			after, err := os.ReadFile(filepath.Join(s.Entry.Path, dispatchPath(d.Input.ID, "intent")))
			if err != nil || string(original) != string(after) {
				t.Fatal("intent rewritten", err)
			}
			newer := prepareTestDispatch(t, root, dispatchInput(s, "newer"))
			if err := EndDispatch(root, s.Entry.TaskID, d.Input.ID, released.Revision, state, "reason", "another decision"); err == nil {
				t.Fatal("old release removed newer binding")
			}
			if got := authFrom(transactionSnapshot(t, root, s.Entry.TaskID).Text); got != newer.Authorization {
				t.Fatal("new binding changed")
			}
		})
	}
}

func TestDecisionOnlyAppliesToReleasedReclaim(t *testing.T) {
	root := tempBoard(t)
	s := coordinatorTodo(t, root, "ordinary-claim")
	if RunMove([]string{s.Entry.TaskID, "working", "--owner", "codex", "--decision", "unused"}) == 0 {
		t.Fatal("ordinary claim accepted decision")
	}
	if RunMove([]string{s.Entry.TaskID, "working", "--owner", "codex"}) != 0 {
		t.Fatal("ordinary claim failed")
	}
	first := transactionSnapshot(t, root, s.Entry.TaskID)
	if _, err := MoveEntry(first.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	if RunMove([]string{s.Entry.TaskID, "working", "--owner", "codex", "--decision", "unused"}) == 0 {
		t.Fatal("never-bound reclaim accepted decision")
	}
	if RunMove([]string{s.Entry.TaskID, "working", "--owner", "codex"}) != 0 {
		t.Fatal("ordinary reclaim failed")
	}
	second := transactionSnapshot(t, root, s.Entry.TaskID)
	// Equalize the display timestamp; the fresh claim component alone must differ.
	second.Text = strings.Replace(second.Text, MetadataFrom(second.Text, FieldStartedAt), MetadataFrom(first.Text, FieldStartedAt), 1)
	if planCycle(first) == planCycle(second) || claimIdentity(first.Text) == claimIdentity(second.Text) {
		t.Fatal("same-minute claim reused cycle")
	}
	legacy := Snapshot{Entry: s.Entry, Text: "- STARTED_AT: 2026-01-01 00:00\n"}
	if planCycle(legacy) != ReviewDigest([]byte(s.Entry.TaskID+"\n2026-01-01 00:00")) {
		t.Fatal("legacy cycle changed")
	}
}

func TestCoordinatorAndReviewRecoverReleasedReclaim(t *testing.T) {
	for _, launch := range []string{"manual", "succeeded", "rolled-back"} {
		t.Run(launch, func(t *testing.T) {
			root := tempBoard(t)
			s := coordinatorTodo(t, root, "handoff")
			c := coordinatorClaim(t, root, s.Entry.TaskID)
			if launch != "manual" {
				entry := startAttemptFixture(t, root, s)
				if launch == "succeeded" {
					if err := ConfirmTaskStart(root, entry); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := RollbackDocument(root, entry, s.Text, "todo"); err != nil {
						t.Fatal(err)
					}
					if RunMove([]string{s.Entry.TaskID, "working", "--owner", "codex"}) != 0 {
						t.Fatal("claim after rollback")
					}
				}
			} else if RunMove([]string{s.Entry.TaskID, "working", "--owner", "codex"}) != 0 {
				t.Fatal("manual claim failed")
			}
			c = coordinatorReconcile(t, root, c)
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			p := gatePlan(t, root, []string{s.Entry.TaskID}, noReviewRequirements())
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
				t.Fatal(err)
			}
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			d := prepareTestDispatch(t, root, dispatchInput(s, "handoff-dispatch"))
			c = coordinatorReconcile(t, root, c)
			original := c
			endDispatchCommand(t, d, "fail", "stop-decision")
			unbound := coordinatorReconcile(t, root, c)
			if unbound.Members[s.Entry.TaskID].Dispatch != nil || len(unbound.Members[s.Entry.TaskID].ReleasedDispatches) != 1 {
				t.Fatal("release not checkpointed")
			}
			if RunMove([]string{s.Entry.TaskID, "working", "--owner", "claude", "--decision", "reclaim-decision"}) != 0 {
				t.Fatal("reclaim failed")
			}
			progress, err := ReviewTaskProgress(root, s.Entry.TaskID)
			if err != nil || progress.Status != "requirements-needed" || len(progress.RebindCycles) != 1 {
				t.Fatal(progress, err)
			}
			if err = ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: 1, RebindCycles: progress.RebindCycles, Author: "claude", Basis: "reclaim-decision"}); err != nil {
				t.Fatal(err)
			}
			c = coordinatorReconcile(t, root, unbound)
			m := c.Members[s.Entry.TaskID]
			if m.Cycle != progress.RebindCycles[s.Entry.TaskID] || len(m.Handoffs) != 1 || m.StartAttempt != original.Members[s.Entry.TaskID].StartAttempt {
				t.Fatal("cycle handoff not consumed")
			}
			if again := coordinatorReconcile(t, root, c); !reflect.DeepEqual(again, c) {
				t.Fatal("handoff replay changed checkpoint")
			}
			// Reconcile a previously observed bound member directly across both changes.
			old := original.Members[s.Entry.TaskID]
			err = WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, Groups: []string{taskStartGroup, reviewControlGroup}}, func(tx *Transaction) error {
				next, e := reconcileCoordinatorMember(context.Background(), tx, s.Entry.TaskID, old, CoordinatorObservation{Revision: transactionRevision(t, root, s.Entry.TaskID)}, nil)
				if e == nil && (next.Dispatch != nil || next.Cycle != m.Cycle) {
					t.Fatal("old bound cursor not recovered")
				}
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Read the persisted revision without entering another board transaction.
func transactionRevision(t *testing.T, root, id string) uint64 {
	t.Helper()
	v, err := readVersion(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return v.Revision
}

func TestCoordinatorRejectsMissingReleaseAndHandoffEvidence(t *testing.T) {
	for _, damage := range []string{"termination", "release", "handoff", "unproven-unbind", "replay-release", "replay-handoff"} {
		t.Run(damage, func(t *testing.T) {
			root := tempBoard(t)
			s := coordinatorTodo(t, root, "missing-evidence")
			if RunMove([]string{s.Entry.TaskID, "working", "--owner", "codex"}) != 0 {
				t.Fatal("claim")
			}
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
				t.Fatal(err)
			}
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			d := prepareTestDispatch(t, root, dispatchInput(s, "evidence"))
			c := coordinatorClaim(t, root, s.Entry.TaskID)
			c = coordinatorReconcile(t, root, c)
			if damage == "unproven-unbind" {
				s = transactionSnapshot(t, root, s.Entry.TaskID)
				text, _ := setMetadata(s.Text, "DISPATCH_ID", "")
				text, _ = setMetadata(text, "EXECUTION_EPOCH", "")
				if err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error { return tx.Put(s.Entry.TaskID, "spec.md", text) }); err != nil {
					t.Fatal(err)
				}
			} else {
				endDispatchCommand(t, d, "cancel", "decision")
				if damage == "replay-release" {
					c = coordinatorReconcile(t, root, c)
				}
				if damage == "handoff" || damage == "replay-handoff" {
					if RunMove([]string{s.Entry.TaskID, "working", "--owner", "claude", "--decision", "reclaim"}) != 0 {
						t.Fatal("reclaim")
					}
					if damage == "replay-handoff" {
						c = coordinatorReconcile(t, root, c)
					}
					s = transactionSnapshot(t, root, s.Entry.TaskID)
					lines := strings.Split(s.Text, "\n")
					var kept []string
					for _, line := range lines {
						if !strings.Contains(line, `"kind":"reclaim"`) {
							kept = append(kept, line)
						}
					}
					if err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error { return tx.Put(s.Entry.TaskID, "spec.md", strings.Join(kept, "\n")) }); err != nil {
						t.Fatal(err)
					}
				} else {
					s = transactionSnapshot(t, root, s.Entry.TaskID)
					if err := os.Remove(filepath.Join(s.Entry.Path, dispatchPath(d.Input.ID, strings.TrimPrefix(damage, "replay-")))); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := ReconcileCoordinator(context.Background(), root, coordinatorRequest(t, root, c), nil); err == nil {
				t.Fatal("missing evidence accepted", damage)
			}
			after, err := ReadCoordinatorCheckpoint(root, c.GroupID)
			if err != nil || !reflect.DeepEqual(after, c) {
				t.Fatal("failed reconcile changed checkpoint", err)
			}
		})
	}
}

func TestReleasedEpochCannotSubmitDisposition(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "released-author")
	gatePlan(t, root, []string{id}, archiveRequirements())
	input := archiveInput([]string{id}, "pm-release", "PMQA")
	input.FindingsSchema = 1
	finding := ReviewFinding{ID: "PM-1", Tier: "medium", Text: "fixture finding", Evidence: "fixture"}
	run := gateRun(t, root, input, ReviewFindings{Findings: []ReviewFinding{finding}, NonBlocking: []ReviewFinding{}})
	assignGate(t, root, run, map[string][]string{finding.ID: {id}})
	s := transactionSnapshot(t, root, id)
	d := prepareTestDispatch(t, root, dispatchInput(s, "author-round"))
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	d, _ = ReadDispatch(root, id, d.Input.ID)
	endDispatchCommand(t, d, "fail", "stop")
	s = transactionSnapshot(t, root, id)
	record := ReviewDisposition{RecordID: "stale-author", RunID: run.RunID, FindingID: finding.ID, BatchID: run.BatchID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: finding.Text, Status: "confirmed", Basis: "stale epoch", Authorization: &d.Authorization}
	if err := SubmitReviewDisposition(root, record, s.Revision); err == nil {
		t.Fatal("released epoch submitted disposition")
	}
}

func TestTerminalReleaseRejectsWrongStateOrSupersededBinding(t *testing.T) {
	for _, kind := range []string{"mismatched-failed", "mismatched-cancelled", "completed", "superseded-failed", "superseded-cancelled"} {
		t.Run(kind, func(t *testing.T) {
			root := tempBoard(t)
			s := dispatchCard(t, root, "terminal-rejection")
			d := prepareTestDispatch(t, root, dispatchInput(s, "old-terminal"))
			verb := "fail"
			if kind == "completed" {
				if _, err := dispatchMove(t, root, d, "working"); err != nil {
					t.Fatal(err)
				}
				if _, err := dispatchMove(t, root, d, "review"); err != nil {
					t.Fatal(err)
				}
				d, _ = ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
			} else {
				// Reproduce an old terminal record that still owns the binding and has no release.
				d.State, d.Reason, d.Revision = DispatchFailed, "legacy", d.Revision+1
				if strings.HasSuffix(kind, "cancelled") {
					d.State = DispatchCancelled
					verb = "cancel"
				}
				if err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}}, func(tx *Transaction) error { return putDispatch(tx, d) }); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(kind, "mismatched") {
					if verb == "fail" {
						verb = "cancel"
					} else {
						verb = "fail"
					}
				}
			}
			if strings.HasPrefix(kind, "superseded") {
				s = transactionSnapshot(t, root, s.Entry.TaskID)
				prepareTestDispatch(t, root, dispatchInput(s, "new-active"))
			}
			before := transactionSnapshot(t, root, s.Entry.TaskID)
			if RunDispatch([]string{verb, s.Entry.TaskID, d.Input.ID, strconv.FormatUint(d.Revision, 10), "release", "--decision", "user-decision"}) == 0 {
				t.Fatal("invalid terminal release accepted")
			}
			after := transactionSnapshot(t, root, s.Entry.TaskID)
			if before.Text != after.Text || before.Revision != after.Revision {
				t.Fatal("rejection changed card or newer binding")
			}
			unchanged, err := ReadDispatch(root, s.Entry.TaskID, d.Input.ID)
			if err != nil || !reflect.DeepEqual(unchanged, d) {
				t.Fatal("rejection changed old terminal facts", err)
			}
		})
	}
}
