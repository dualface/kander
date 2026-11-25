package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
)

// A group coordinator writes the review evidence of review-state members
// without the holders' confirmation when every condition of
// coordinatorAuthority holds for the whole operation. The decision is not a
// kernel event of its own: the group lease check is requireGroup (the group
// kernel's require event), the member execution check is standing (the claim
// kernel's standing event), and the rest are card and dispatch facts. See
// docs/distributed-server.md "Review gate".

// coordinatorBasis is the parsed Kander-Coordinator-Basis header of a request.
// err keeps the refusal of a malformed header until reviewWrite answers it.
type coordinatorBasis struct {
	members api.CoordinatorBasis
	err     error
}

// coordinatorBasisOf parses the declaration of a request, or returns nil when
// the request carries none.
func coordinatorBasisOf(r *http.Request) *coordinatorBasis {
	values := r.Header.Values(api.HeaderCoordinatorBasis)
	if len(values) == 0 {
		return nil
	}
	invalid := func(reason string) *coordinatorBasis {
		return &coordinatorBasis{err: apiError(api.CodeInvalidRequest, "invalid coordinator basis: "+reason)}
	}
	if len(values) != 1 || len(values[0]) > 64<<10 {
		return invalid("one header of at most 64 KiB is required")
	}
	var members api.CoordinatorBasis
	if err := json.Unmarshal([]byte(values[0]), &members); err != nil || len(members) == 0 || len(members) > 256 {
		return invalid("a list of 1 to 256 members is required")
	}
	seen := map[string]bool{}
	for _, member := range members {
		if !api.ValidIdentity(member.TaskID) || member.Revision == 0 || member.Generation == 0 || seen[member.TaskID] {
			return invalid("each member needs a distinct valid task ID, revision and generation")
		}
		seen[member.TaskID] = true
	}
	return &coordinatorBasis{members: members}
}

// coordinatorConflict refuses a malformed coordinator declaration, or one that
// accompanies another proof. The declaration proves the group lease, never a
// card claim or a joint publication, so mixing them is ambiguous. Every review
// write answers it, including the recovery of a prepared run, which returns
// before it reaches reviewWrite.
func (h holder) coordinatorConflict() error {
	if h.basis == nil {
		return nil
	}
	if h.publication != "" || h.members != nil || h.invalid != nil {
		return apiError(api.CodeInvalidRequest, "a coordinator basis cannot accompany a publication or multi-task claim proofs")
	}
	return h.basis.err
}

// coordinatorRefused is the answer to a coordinator write that the conditions
// do not allow. The request is not retried as a joint publication; the client
// reads the cards again and selects the flow anew.
func coordinatorRefused(task, reason string) error {
	return apiError(api.CodeHeldByOther, "coordinator write authority does not apply to "+task+": "+reason)
}

// coordinatorAuthority decides whether the holder h of a group lease may write
// the review evidence of operation, which touches the live cards in live. It
// returns the group of the lease. All of these must hold, or the whole
// operation is refused and nothing is written:
//   - the operation is not a disposition, which only a holder submits;
//   - the declared members are exactly the live cards;
//   - every live card is in review and belongs to one task group;
//   - h holds that group's lease, which the lease clock does not decide;
//   - every live card has the declared revision and a standing claim of the
//     declared generation;
//   - no live card has a dispatch in progress.
//
// read is the card reader of the caller: the board outside a transaction or
// the transaction itself, so the same facts are checked again at write time.
// dispatch reads a dispatch record and is nil inside a transaction, where the
// revision check already fixes the card header the binding comes from.
func (p *project) coordinatorAuthority(operation string, live []string, h holder, read func(string) (board.Snapshot, error), dispatch func(task, id string) (board.Dispatch, error)) (string, error) {
	if operation == "review-disposition" {
		return "", apiError(api.CodeHeldByOther, "a review disposition needs the card holder; the coordinator write authority does not cover it")
	}
	declared := make(map[string]api.PublicationMember, len(h.basis.members))
	for _, member := range h.basis.members {
		declared[member.TaskID] = member
	}
	if len(declared) != len(live) {
		return "", apiError(api.CodeEvidenceIncomplete, "the coordinator basis does not name exactly the live members of the operation")
	}
	snapshots := make(map[string]board.Snapshot, len(live))
	group := ""
	for _, id := range live {
		if _, ok := declared[id]; !ok {
			return "", apiError(api.CodeEvidenceIncomplete, "the coordinator basis does not name exactly the live members of the operation")
		}
		snapshot, err := read(id)
		if err != nil {
			return "", err
		}
		snapshots[id] = snapshot
		switch cardGroup := board.TaskGroupFrom(snapshot.Text); {
		case snapshot.Entry.State != "review":
			return "", coordinatorRefused(id, "the card is in "+snapshot.Entry.State+", not review")
		case cardGroup == "":
			return "", coordinatorRefused(id, "the card belongs to no task group")
		case group == "":
			group = cardGroup
		case group != cardGroup:
			return "", coordinatorRefused(id, "the live members belong to different task groups")
		}
	}
	if _, err := p.requireGroup(group, h); err != nil {
		return "", err
	}
	for _, id := range live {
		member, snapshot := declared[id], snapshots[id]
		if snapshot.Revision != member.Revision {
			return "", apiError(api.CodeRevisionConflict, "task "+id+" is at revision "+strconv.FormatUint(snapshot.Revision, 10)+", not the declared "+strconv.FormatUint(member.Revision, 10))
		}
		claim, found, err := p.readClaim(id)
		if err != nil {
			return "", err
		}
		standing, err := p.standing(claim, found, member.Generation)
		if err != nil {
			return "", err
		}
		if !standing {
			return "", apiError(api.CodeLeaseInvalid, "the declared execution of "+id+" is no longer current")
		}
		if dispatch != nil {
			if err := openDispatch(id, snapshot.Text, dispatch); err != nil {
				return "", err
			}
		}
	}
	return group, nil
}

// openDispatch refuses a card whose bound dispatch is still in progress. A
// bound dispatch that completed is not in progress: the holder handed its work
// back and nothing settles it before the next review.
func openDispatch(task, text string, read func(task, id string) (board.Dispatch, error)) error {
	id := board.MetadataFrom(text, "DISPATCH_ID")
	if id == "" {
		return nil
	}
	record, err := read(task, id)
	if err != nil {
		return coordinatorRefused(task, "its dispatch "+id+" cannot be read, so it counts as in progress")
	}
	if board.DispatchInProgress(record.State) {
		return coordinatorRefused(task, "its dispatch "+id+" is "+string(record.State))
	}
	return nil
}
