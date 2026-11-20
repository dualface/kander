package remote

import (
	"context"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
)

// coordinatorAuthority returns the proof of a review write that the group
// coordinator makes without the holders' confirmation, or nil when the write
// needs the holders: any other flow stays as it was. It applies when every
// live member is in review, all members belong to one task group whose
// coordinator credential this process holds, no member has a dispatch in
// progress, and this process holds the current credential of no member (the
// holder's own proof comes first). members and shown are the cards as
// liveReviewMembers read them; the basis declares exactly those versions, and
// the server checks them again with the lease before it writes.
//
// The choice only selects the flow. The server decides the authority from the
// group lease and the card state, so a process that merely claims to be the
// coordinator is refused there. A refusal is final: the client neither retries
// it nor falls back to a joint publication, because the state it was decided
// on has changed.
func (s *Session) coordinatorAuthority(ctx context.Context, operation string, members []api.PublicationMember, shown map[string]api.Task) (*client.Claim, error) {
	if operation == "review-disposition" || len(members) == 0 {
		return nil, nil
	}
	group := ""
	for _, member := range members {
		task := shown[member.TaskID]
		cardGroup := board.TaskGroupFrom(task.Text)
		if task.State != "review" || cardGroup == "" || group != "" && cardGroup != group {
			return nil, nil
		}
		group = cardGroup
		credential, found, err := s.Credential(member.TaskID)
		if err != nil {
			return nil, err
		}
		if found && !credential.Lost && credential.Generation == member.Generation {
			return nil, nil
		}
	}
	credential, found, err := s.CoordinatorCredential(group)
	if err != nil {
		return nil, err
	}
	if !found || credential.Lost || credential.ResourceKind != "group" {
		return nil, nil
	}
	for _, member := range members {
		if s.dispatchInProgress(ctx, shown[member.TaskID]) {
			return nil, nil
		}
	}
	proof := credential.Claim()
	proof.CoordinatorBasis = api.CoordinatorBasis(members)
	proof.CoordinatorNote = config.Text("remote.coordinator_write_refused")
	return proof, nil
}

// dispatchInProgress reports whether the dispatch bound to a card is prepared,
// delivery-unknown or accepted. A bound dispatch that completed is not in
// progress, and one that cannot be read counts as in progress.
func (s *Session) dispatchInProgress(ctx context.Context, task api.Task) bool {
	id := board.MetadataFrom(task.Text, "DISPATCH_ID")
	if id == "" {
		return false
	}
	dispatch, err := s.Project.Dispatch(ctx, task.TaskID, id)
	if err != nil {
		return true
	}
	return board.DispatchInProgress(dispatch.State)
}
