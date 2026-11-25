package remote_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

// errJoint ends a review authority at the point it would prepare a joint
// publication, so a test tells the joint flow from the coordinator proof.
var errJoint = errors.New("joint publication requested")

// reviewStub answers only what the choice of a review proof reads.
type reviewStub struct {
	remote.ProjectService
	tasks      map[string]api.Task
	dispatches map[string]board.DispatchState
	unreadable bool
	prepared   []api.PublicationPrepare
	published  map[string]api.Publication
}

func (s *reviewStub) Show(_ context.Context, id string) (api.Task, error) { return s.tasks[id], nil }

func (s *reviewStub) Dispatch(_ context.Context, _, id string) (board.Dispatch, error) {
	state, found := s.dispatches[id]
	if s.unreadable || !found {
		return board.Dispatch{}, &api.Error{Code: api.CodeNotFound, Message: "dispatch"}
	}
	return board.Dispatch{State: state}, nil
}

func (s *reviewStub) PreparePublication(_ context.Context, request api.PublicationPrepare) (api.Publication, error) {
	s.prepared = append(s.prepared, request)
	return api.Publication{}, errJoint
}

func (s *reviewStub) Publication(_ context.Context, id string) (api.Publication, error) {
	if value, found := s.published[id]; found {
		return value, nil
	}
	return api.Publication{}, &api.Error{Code: api.CodeNotFound, Message: "publication"}
}

const coordinatedGroup = "20261120-coordinated-group"

// coordinated is a two-member batch in review whose group a client process
// coordinates, with a stub in place of the server.
type coordinated struct {
	t       *testing.T
	stub    *reviewStub
	session *remote.Session
	ids     []string
}

func cardText(group, dispatch string) string {
	text := "# Review member\n\n- TYPE: Chore\n- SIZE: small\n- TASK_GROUP: " + group + "\n"
	if dispatch != "" {
		text += "- DISPATCH_ID: " + dispatch + "\n"
	}
	return text + "\n## GOAL\n\nx\n"
}

func newCoordinated(t *testing.T) *coordinated {
	t.Helper()
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	f := &coordinated{t: t, stub: &reviewStub{tasks: map[string]api.Task{}, dispatches: map[string]board.DispatchState{}, published: map[string]api.Publication{}}, ids: []string{"20261120-first-task", "20261120-second-task"}}
	f.session = &remote.Session{Project: f.stub, Info: api.Project{ID: "stub-project"}, ServerURL: "http://stub.invalid", Home: t.TempDir()}
	for i, id := range f.ids {
		f.stub.tasks[id] = api.Task{TaskID: id, State: "review", Revision: uint64(10 + i), Text: cardText(coordinatedGroup, ""), Claim: &api.ClaimView{Status: api.ClaimActive, Generation: uint64(1 + i), ExecutionID: "execution-" + id, Owner: "codex"}}
	}
	f.save(remote.Credential{ResourceKind: "group", TaskID: coordinatedGroup, ExecutionID: "coordinator", Generation: 3, Token: strings.Repeat("c", 64)})
	return f
}

func (f *coordinated) save(credential remote.Credential) {
	f.t.Helper()
	credential.ServerURL, credential.ClientID, credential.ProjectID = f.session.ServerURL, "host-c", f.session.Info.ID
	if err := f.session.SaveCredential(credential); err != nil {
		f.t.Fatal(err)
	}
}

// edit changes one card of the batch.
func (f *coordinated) edit(index int, change func(*api.Task)) {
	task := f.stub.tasks[f.ids[index]]
	change(&task)
	f.stub.tasks[f.ids[index]] = task
}

func (f *coordinated) authority(operation string, ids ...string) (*client.Claim, error) {
	f.t.Helper()
	if len(ids) == 0 {
		ids = f.ids
	}
	return f.session.ReviewAuthority(f.t.Context(), operation, map[string]string{"run_id": "coordinated-run"}, ids)
}

// wantCoordinator checks the proof of a coordinator write and that no joint
// publication was prepared.
func (f *coordinated) wantCoordinator(operation string, ids ...string) {
	f.t.Helper()
	if len(ids) == 0 {
		ids = f.ids
	}
	proof, err := f.authority(operation, ids...)
	if err != nil || proof == nil {
		f.t.Fatalf("%s: %v", operation, err)
	}
	if proof.PublicationID != "" || proof.Members != nil || proof.Token != strings.Repeat("c", 64) || proof.Generation != 3 || proof.CoordinatorNote == "" {
		f.t.Fatalf("%s: not the group lease proof: %+v", operation, proof)
	}
	if len(proof.CoordinatorBasis) != len(f.liveIDs(ids)) {
		f.t.Fatalf("%s: basis %+v", operation, proof.CoordinatorBasis)
	}
	for _, member := range proof.CoordinatorBasis {
		task := f.stub.tasks[member.TaskID]
		if member.Revision != task.Revision || member.Generation != task.Claim.Generation {
			f.t.Fatalf("%s: basis %+v does not match the card read %+v", operation, member, task)
		}
	}
	if len(f.stub.prepared) != 0 {
		f.t.Fatalf("%s: a joint publication was prepared: %+v", operation, f.stub.prepared)
	}
}

func (f *coordinated) liveIDs(ids []string) []string {
	var live []string
	for _, id := range ids {
		if f.stub.tasks[id].State == "review" || f.stub.tasks[id].State == "working" {
			live = append(live, id)
		}
	}
	return live
}

// wantJoint checks that the choice stays with the joint publication.
func (f *coordinated) wantJoint(operation string) {
	f.t.Helper()
	_, err := f.authority(operation)
	if !errors.Is(err, errJoint) || len(f.stub.prepared) != 1 || len(f.stub.prepared[0].Members) != len(f.ids) {
		f.t.Fatalf("%s: want a joint publication of all members: %v %+v", operation, err, f.stub.prepared)
	}
}

func TestCoordinatorProofIsChosenForReviewMembersOfItsGroup(t *testing.T) {
	for _, operation := range []string{"review-plan", "review-extend-plan", "review-resolve-batch", "review-advance", "review-assign", "review-interpret", "review-map-legacy", "review-abandon", "review-aggregate:batch", "review-close:batch", "review-prepare", "review-publish:run"} {
		t.Run(operation, func(t *testing.T) {
			newCoordinated(t).wantCoordinator(operation)
		})
	}
	t.Run("one live member", func(t *testing.T) {
		f := newCoordinated(t)
		f.edit(1, func(task *api.Task) { task.State = "done" })
		f.wantCoordinator("review-plan")
	})
	t.Run("a completed dispatch", func(t *testing.T) {
		f := newCoordinated(t)
		f.stub.dispatches["round-1"] = board.DispatchCompleted
		f.edit(0, func(task *api.Task) { task.Text = cardText(coordinatedGroup, "round-1") })
		f.wantCoordinator("review-plan")
	})
	t.Run("a dispatch that ended without work", func(t *testing.T) {
		for _, state := range []board.DispatchState{board.DispatchFailed, board.DispatchCancelled} {
			f := newCoordinated(t)
			f.stub.dispatches["round-1"] = state
			f.edit(0, func(task *api.Task) { task.Text = cardText(coordinatedGroup, "round-1") })
			f.wantCoordinator("review-plan")
		}
	})
}

// Each case leaves the batch outside the conditions, so the whole operation
// stays with the joint publication of every live member.
func TestCoordinatorProofIsNotChosenOutsideItsConditions(t *testing.T) {
	inProgress := func(state board.DispatchState, unreadable bool) func(*coordinated) {
		return func(f *coordinated) {
			f.stub.dispatches["round-1"] = state
			f.stub.unreadable = unreadable
			f.edit(1, func(task *api.Task) { task.Text = cardText(coordinatedGroup, "round-1") })
		}
	}
	for _, step := range []struct {
		name  string
		setup func(*coordinated)
	}{
		{"a member in working", func(f *coordinated) { f.edit(1, func(task *api.Task) { task.State = "working" }) }},
		{"a prepared dispatch", inProgress(board.DispatchPrepared, false)},
		{"a delivery-unknown dispatch", inProgress(board.DispatchUnknown, false)},
		{"an accepted dispatch", inProgress(board.DispatchAccepted, false)},
		{"an unreadable dispatch", inProgress(board.DispatchCompleted, true)},
		{"a member of another group", func(f *coordinated) {
			f.edit(1, func(task *api.Task) { task.Text = cardText("20261120-other-group", "") })
		}},
		{"a member without a group", func(f *coordinated) { f.edit(1, func(task *api.Task) { task.Text = cardText("", "") }) }},
		{"no group credential", func(f *coordinated) {
			f.save(remote.Credential{ResourceKind: "group", TaskID: coordinatedGroup, ExecutionID: "coordinator", Generation: 3, Token: strings.Repeat("c", 64), Lost: true})
		}},
		{"a group credential of another group", func(f *coordinated) {
			f.save(remote.Credential{ResourceKind: "group", TaskID: coordinatedGroup, ExecutionID: "coordinator", Generation: 3, Token: strings.Repeat("c", 64), Lost: true})
			f.save(remote.Credential{ResourceKind: "group", TaskID: "20261120-other-group", ExecutionID: "coordinator", Generation: 1, Token: strings.Repeat("d", 64)})
		}},
		{"a member credential of this process", func(f *coordinated) {
			f.save(remote.Credential{TaskID: f.ids[0], ExecutionID: "execution-" + f.ids[0], Generation: 1, Token: strings.Repeat("a", 64)})
		}},
	} {
		t.Run(step.name, func(t *testing.T) {
			f := newCoordinated(t)
			step.setup(f)
			f.wantJoint("review-plan")
		})
	}
}

// The process that holds a member's own credential proves it directly. A
// disposition is never written by a coordinator.
func TestCoordinatorProofGivesWayToTheHolderAndNeverCoversDisposition(t *testing.T) {
	t.Run("the holder of a single member", func(t *testing.T) {
		f := newCoordinated(t)
		f.save(remote.Credential{TaskID: f.ids[0], ExecutionID: "execution-" + f.ids[0], Generation: 1, Token: strings.Repeat("a", 64)})
		proof, err := f.authority("review-plan", f.ids[0])
		if err != nil || proof.CoordinatorBasis != nil || proof.Members[f.ids[0]].Generation != 1 || proof.Token != "" {
			t.Fatalf("the holder's own proof was not used: %+v %v", proof, err)
		}
	})
	t.Run("a disposition", func(t *testing.T) {
		f := newCoordinated(t)
		_, err := f.authority("review-disposition", f.ids[0])
		if !api.IsCode(err, api.CodeHeldByOther) || len(f.stub.prepared) != 0 {
			t.Fatalf("a coordinator submitted a disposition: %v", err)
		}
	})
}

// A joint publication that was published before the coordinator write existed
// still answers a retry: the saved result comes back and no coordinator proof
// is built, although the members now meet every condition.
func TestPublishedJointPublicationReplaysBeforeCoordinatorProof(t *testing.T) {
	f := newCoordinated(t)
	f.edit(1, func(task *api.Task) { task.State = "working" })
	f.wantJoint("review-aggregate:batch")
	prepared := f.stub.prepared[0]
	f.stub.published[prepared.ID] = api.Publication{PublicationPrepare: prepared, Status: "published", Result: []byte(`{"batch_id":"batch"}`)}
	f.stub.prepared = nil
	f.edit(1, func(task *api.Task) { task.State = "review" })
	proof, err := f.authority("review-aggregate:batch")
	if err != nil || proof.PublicationID != prepared.ID || proof.CoordinatorBasis != nil || proof.Token != "" || len(f.stub.prepared) != 0 {
		t.Fatalf("the saved joint publication was not replayed: %+v %v", proof, err)
	}
}
