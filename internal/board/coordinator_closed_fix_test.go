package board

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCoordinatorCompletedFixSurvivesClosedBatchRestart(t *testing.T) {
	for _, damage := range []string{"author", "report", "closure", "assignment"} {
		t.Run(damage, func(t *testing.T) {
			root, c, in, run, record := coordinatorAdvancedFix(t)
			roles := map[string]ReviewRoleConclusion{}
			for _, role := range []string{"PM", "QA"} {
				input := archiveInput([]string{in.TaskID}, "passing-"+strings.ToLower(role), role)
				input.Commit = record.FixCommit
				if role == "PM" {
					input.PreviousRunID, input.ReviewedCommit = run.RunID, run.Commit
				}
				passing := gateRun(t, root, input, emptyFindings())
				assignGate(t, root, passing, map[string][]string{})
				roles[role] = passRole(passing)
			}
			if _, err := gateClose(t, root, roles); err != nil {
				t.Fatal(err)
			}
			// There is deliberately no wrap-up intent. Restart from the persisted
			// pre-closure checkpoint and the still-current completed fix receipt.
			c, err := ReadCoordinatorCheckpoint(root, c.GroupID)
			if err != nil {
				t.Fatal(err)
			}
			r := coordinatorRequest(t, root, c)
			next, err := ReconcileCoordinator(context.Background(), root, r, nil)
			if err != nil {
				t.Fatal("closed batch blocked historical completion", err)
			}
			m := next.Members[in.TaskID]
			if m.Review.Status != "closed" || m.DeliveryCommit != record.FixCommit || m.Dispatch.PendingDelivery || m.Dispatch.ID != in.ID {
				t.Fatal("closed history lost completed round", m)
			}
			before, err := operationRecords(root)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				replay, err := ReconcileCoordinator(context.Background(), root, r, nil)
				if err != nil || !reflect.DeepEqual(replay, next) {
					t.Fatal("duplicate changed closed cursor", err)
				}
			}
			after, err := operationRecords(root)
			if err != nil || len(before) != len(after) {
				t.Fatal("duplicate created transaction", err)
			}
			// Test the closed gate directly as well: old targets alone must not be
			// the reason creation and transport remain forbidden.
			if err = WithTransaction(root, reviewScope([]string{in.TaskID}, true), func(tx *Transaction) error {
				_, e := reviewRunForMutation(tx, run.RunID)
				return e
			}); err == nil {
				t.Fatal("closed run still mutable")
			}
			if err = ValidateDispatchEvidence(root, in.TaskID, in.ID); err == nil {
				t.Fatal("closed fix still sendable")
			}
			copy := in
			copy.ID = "another-fix"
			if _, err = PrepareDispatch(root, copy); err == nil {
				t.Fatal("closed fix recreated")
			}
			s := transactionSnapshot(t, root, in.TaskID)
			path := map[string]string{
				"author":     filepath.Join(s.Entry.Path, dispositionPath(record)),
				"report":     filepath.Join(s.Entry.Path, "reviews", run.RunID, "report.md"),
				"closure":    filepath.Join(s.Entry.Path, "reviews", "batches", run.BatchID, "closed.json"),
				"assignment": filepath.Join(s.Entry.Path, "reviews", run.RunID, "assignment.json"),
			}[damage]
			if err = os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if _, err = ReconcileCoordinator(context.Background(), root, r, nil); err == nil {
				t.Fatal("damaged historical original accepted")
			}
			actual, err := ReadCoordinatorCheckpoint(root, c.GroupID)
			if err != nil || !reflect.DeepEqual(actual, next) {
				t.Fatal("damage changed cursor", err)
			}
		})
	}
}
