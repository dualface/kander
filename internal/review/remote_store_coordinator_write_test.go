//go:build unix

package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// inReview moves cards the holder started to review, with the delivery facts
// the service needs for that move.
func (r *coordinatedRun) inReview(ids ...string) {
	r.t.Helper()
	commit := strings.Repeat("c", 40)
	for _, id := range ids {
		own, found, err := r.holder.Credential(id)
		if err != nil || !found {
			r.t.Fatalf("holder credential: %v", err)
		}
		card, err := r.holder.Project.Show(r.t.Context(), id)
		if err != nil {
			r.t.Fatal(err)
		}
		text := strings.Replace(card.Text, "- TASK_BRANCH:", "- TASK_BRANCH: task/"+id, 1)
		if card, err = r.holder.Project.PutDocument(r.t.Context(), "", id, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, own.Claim()); err != nil {
			r.t.Fatal(err)
		}
		evidence := api.DeliveryEvidence{TaskID: id, ExecutionID: own.ExecutionID, Generation: own.Generation, CardRevision: card.Revision, Commit: commit, Ref: api.DeliveryRef(r.holder.Info.ID, id, own.Generation, commit), Remote: r.holder.Info.Remote, ObservedAt: time.Now().UTC()}
		if _, err := r.holder.Project.PublishDelivery(r.t.Context(), "", evidence, *own.Claim()); err != nil {
			r.t.Fatal(err)
		}
		if _, err := r.holder.Project.Move(r.t.Context(), "", id, api.Move{State: "review", DeliveryCommit: commit}, own.Claim()); err != nil {
			r.t.Fatal(err)
		}
	}
}

// A coordinator that holds the group lease prepares and publishes a run of
// review members with the same run ID, once each, with no holder confirmation.
// The run's private writer secret is still required.
func TestCoordinatorRunNeedsNoConfirmationForReviewMembers(t *testing.T) {
	for _, members := range []int{1, 2} {
		t.Run(map[int]string{1: "one member", 2: "one live of two members"}[members], func(t *testing.T) {
			coordinatedRemote = "git@example.invalid:demo.git"
			t.Cleanup(func() { coordinatedRemote = "" })
			r := newCoordinatedRun(t, members)
			r.inReview(r.ids[0])
			unlock, err := r.store.Lock("coordinated-run")
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			requirements := map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
			originals := map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}
			run, fresh, err := r.store.Prepare(r.input(), requirements, nil, originals, "test")
			if err != nil || !fresh {
				t.Fatalf("prepare needed more than the coordinator: %+v %v", run, err)
			}
			run.LaunchStatus, run.ExecutionStatus = "started", "ok"
			if run, err = r.store.Finalize(run, []byte(emptyStructuredReview)); err != nil {
				t.Fatal(err)
			}
			// The coordinator proof alone does not publish: the run's writer is not its secret.
			proof, err := r.coordinate.ReviewAuthority(t.Context(), "review-publish:"+run.RunID, run.RunID, run.TaskIDs)
			if err != nil || len(proof.CoordinatorBasis) == 0 {
				t.Fatalf("the coordinator proof was not chosen: %+v %v", proof, err)
			}
			proof.ReviewWriter = strings.Repeat("b", 64)
			_, err = r.coordinate.Project.ReviewRuntime(t.Context(), client.NewKey(), run.RunID, "publish", map[string]string{}, proof)
			if !api.IsCode(err, api.CodeHeldByOther) {
				t.Fatalf("a foreign writer secret published the run: %v", err)
			}
			published, _, err := r.store.Publish(run.RunID)
			if err != nil || !published.Published[r.ids[0]] {
				t.Fatalf("publish needed more than the coordinator: %+v %v", published, err)
			}
			if requests, err := r.holder.Project.Requests(t.Context(), r.ids[0]); err != nil || len(requests) != 0 {
				t.Fatalf("a confirmation was requested from the holder: %+v %v", requests, err)
			}
			r.assignWithoutConfirmation(published)
		})
	}
}

// assignWithoutConfirmation runs the assign command path of the coordinator:
// one command writes the assignment.
func (r *coordinatedRun) assignWithoutConfirmation(run board.ReviewRun) {
	r.t.Helper()
	assignment := board.ReviewAssignment{RunID: run.RunID, BatchID: run.BatchID, Author: "coordinator", Basis: "coordinator write", Items: map[string][]string{}}
	data, err := json.Marshal(assignment)
	if err != nil {
		r.t.Fatal(err)
	}
	input := filepath.Join(r.t.TempDir(), "assignment.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		r.t.Fatal(err)
	}
	if _, err := remoteEvidenceAction(r.t.Context(), r.coordinate, "/host-b/clone", "assign", input, nil); err != nil {
		r.t.Fatalf("assign needed more than the coordinator: %v", err)
	}
}

// A joint publication that was published while its members were in working (as
// before the coordinator write existed) still answers a retry after the members
// reached review and meet every condition: the saved result comes back, no new
// write happens, and no request is created.
func TestPublishedJointPublicationReplaysWhenMembersMeetCoordinatorConditions(t *testing.T) {
	for _, members := range []int{1, 2} {
		t.Run(map[int]string{1: "one member", 2: "two live members"}[members], func(t *testing.T) {
			coordinatedRemote = "git@example.invalid:demo.git"
			t.Cleanup(func() { coordinatedRemote = "" })
			r := newCoordinatedRunKeeping(t, members, members, true)
			live := r.ids[:members]
			_, err := r.coordinate.AggregateReview(t.Context(), "coordinated-batch")
			id := regexp.MustCompile(`review-[0-9a-f]{64}`).FindString(errString(err))
			if !api.IsCode(err, api.CodeEvidenceIncomplete) || id == "" {
				t.Fatalf("want a joint publication: %v", err)
			}
			for _, task := range live {
				if _, err := r.holder.ConfirmReviewPublication(t.Context(), id, task); err != nil {
					t.Fatal(err)
				}
			}
			first, err := r.coordinate.AggregateReview(t.Context(), "coordinated-batch")
			if err != nil {
				t.Fatalf("joint publication: %v", err)
			}
			r.inReview(live...)
			revisions := map[string]uint64{}
			requests := map[string]int{}
			for _, task := range live {
				card, err := r.holder.Project.Show(t.Context(), task)
				if err != nil {
					t.Fatal(err)
				}
				list, err := r.holder.Project.Requests(t.Context(), task)
				if err != nil {
					t.Fatal(err)
				}
				revisions[task], requests[task] = card.Revision, len(list)
			}
			again, err := r.coordinate.AggregateReview(t.Context(), "coordinated-batch")
			if err != nil || !sameJSON(t, first, again) {
				t.Fatalf("the saved result was not replayed: %v", err)
			}
			for _, task := range live {
				card, err := r.holder.Project.Show(t.Context(), task)
				if err != nil {
					t.Fatal(err)
				}
				list, err := r.holder.Project.Requests(t.Context(), task)
				if err != nil {
					t.Fatal(err)
				}
				if card.Revision != revisions[task] || len(list) != requests[task] {
					t.Fatalf("%s: the replay wrote: revision %d -> %d, requests %d -> %d", task, revisions[task], card.Revision, requests[task], len(list))
				}
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// sameJSON compares values, not bytes: the saved result is stored compact.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}
