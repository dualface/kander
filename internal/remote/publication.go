package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
)

func (s *Session) publicationPayload(operation string, value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	if object, ok := normalized.(map[string]any); ok {
		if operation == "review-prepare" {
			if input, ok := object["input"].(map[string]any); ok {
				input["cwd"] = "project:" + s.Info.ID
			}
		}
		if operation == "review-plan" {
			object["cwd"] = "project:" + s.Info.ID
		}
		if strings.HasPrefix(operation, "review-close:") {
			if evidence, ok := object["evidence"].(map[string]any); ok {
				evidence["cwd"] = "project:" + s.Info.ID
			}
		}
	}
	return json.Marshal(normalized)
}

// ConfirmationPending is the evidence_incomplete answer of a review write whose
// joint publication still lacks holder confirmations. A caller that owns a
// retry identity, such as a review run, tells it apart from other
// evidence_incomplete answers by this type and adds that identity.
type ConfirmationPending struct{ Cause *api.Error }

func (e *ConfirmationPending) Error() string { return e.Cause.Error() }
func (e *ConfirmationPending) Unwrap() error { return e.Cause }

// ReviewAuthority prepares a joint operation, confirms locally owned members,
// and asks other holders to confirm the exact prepared material. Pending
// confirmations are durable and do not publish any card evidence.
func (s *Session) ReviewAuthority(ctx context.Context, operation string, payload any, tasks []string) (*client.Claim, error) {
	proof, _, err := s.reviewAuthority(ctx, operation, operation, payload, tasks, "")
	return proof, err
}

// PrepareReviewAuthority is ReviewAuthority for the prepare of a review run.
// recorded is the version of the existing run, or empty for a new one: the
// client binary that prepared the run may have been replaced since, and its
// completed joint publication is still found by that version.
func (s *Session) PrepareReviewAuthority(ctx context.Context, request api.ReviewPrepare, tasks []string, recorded string) (*client.Claim, error) {
	proof, _, err := s.reviewAuthority(ctx, "review-prepare", "review-prepare", request, tasks, recorded)
	return proof, err
}

// reviewAuthority looks prior publications up by replay, which equals
// operation except for operations whose material does not identify their
// result. The second result reports that the proof names an already published
// publication, whose saved result the server returns unchanged. recorded is
// the version of an existing prepared run, and empty for every other call.
func (s *Session) reviewAuthority(ctx context.Context, operation, replay string, payload any, tasks []string, recorded string) (*client.Claim, bool, error) {
	raw, err := s.publicationPayload(operation, payload)
	if err != nil {
		return nil, false, err
	}
	var members []api.PublicationMember
	var shown map[string]api.Task
	read, routed := false, false
	if len(tasks) == 1 {
		// The holder's direct proof needs no saved result, and so does not
		// depend on the local record directory that holds them.
		members, shown, err = s.liveReviewMembers(ctx, tasks)
		switch {
		case api.IsCode(err, api.CodeLeaseInvalid):
			// A released claim cannot take part in a new write, but the saved
			// result of a published operation still answers a retry.
		case err != nil:
			return nil, false, err
		default:
			read = true
			if len(members) == 1 {
				proof, joint, err := s.singleMemberAuthority(operation, tasks, members, shown)
				if err != nil || !joint {
					return proof, false, err
				}
				routed = true
			}
		}
	}
	saved, publication, err := s.completedReviewPublication(ctx, operation, replay, raw, recorded)
	if err != nil {
		return nil, false, err
	}
	if saved != nil {
		// The replay records are shared by every execution of this client
		// home. A one-member publication is routed for the calling execution
		// first, so a saved result never replaces the holder's direct proof
		// and never answers a process that is neither holder nor coordinator.
		if len(publication.Members) == 1 {
			if proof, direct, err := s.replayRoute(ctx, operation, tasks, publication.Members[0].TaskID); err != nil || direct {
				return proof, false, err
			}
		}
		return saved, true, nil
	}
	if !read {
		if members, shown, err = s.liveReviewMembers(ctx, tasks); err != nil {
			return nil, false, err
		}
	}
	if operation == "review-abandon" && len(members) > 1 {
		if err := s.requireAbandonCaller(members, shown); err != nil {
			return nil, false, err
		}
	}
	if len(members) <= 1 && !routed {
		proof, joint, err := s.singleMemberAuthority(operation, tasks, members, shown)
		if err != nil || !joint {
			return proof, false, err
		}
	}
	if proof, err := s.coordinatorAuthority(ctx, operation, members, shown); err != nil || proof != nil {
		return proof, false, err
	}
	raw, err = s.freezePublicationMaterial(operation, raw, members)
	if err != nil {
		return nil, false, err
	}
	id, err := publicationID(operation, raw, members)
	if err != nil {
		return nil, false, err
	}
	if _, err := s.publicationHistory(operation, replay, raw, id); err != nil {
		return nil, false, err
	}
	prepared, err := s.Project.PreparePublication(ctx, api.PublicationPrepare{ID: id, Operation: operation, Payload: raw, Members: members})
	if err != nil {
		return nil, false, err
	}
	if prepared.Status == "published" {
		return &client.Claim{PublicationID: id, PublicationPayload: raw}, true, nil
	}
	var pending []string
	for _, member := range members {
		credential, found, err := s.Credential(member.TaskID)
		if err != nil {
			return nil, false, err
		}
		if found && !credential.Lost && credential.Generation == member.Generation {
			prepared, err = s.Project.ConfirmPublication(ctx, id, api.PublicationApproval{TaskID: member.TaskID, Digest: prepared.Digest, Revision: member.Revision, Generation: member.Generation}, *credential.Claim())
			if err != nil {
				return nil, false, err
			}
		} else if _, approved := prepared.Approvals[member.TaskID]; !approved {
			if err := s.requestConfirmation(ctx, id, member); err != nil {
				return nil, false, err
			}
			pending = append(pending, member.TaskID)
		}
	}
	if len(pending) > 0 {
		return nil, false, &ConfirmationPending{Cause: &api.Error{Code: api.CodeEvidenceIncomplete, Message: config.Text("remote.review_confirmation_pending", id, strings.Join(pending, ", "))}}
	}
	return &client.Claim{PublicationID: id, PublicationPayload: raw}, false, nil
}

// requestConfirmation asks the holder of member to confirm the publication, once. A review
// request of the member's generation whose text names the publication is the question already,
// whatever its status but superseded: the client that wrote it may have been an earlier version
// with another text and another key, and a retry in another interface language writes another
// text. Only when there is none does it create one, under a key that also names the text, so a
// later change of the wording never reuses the key of an older request.
func (s *Session) requestConfirmation(ctx context.Context, id string, member api.PublicationMember) error {
	requests, err := s.Project.Requests(ctx, member.TaskID)
	if err != nil {
		return err
	}
	for _, request := range requests {
		if request.Kind == api.RequestReview && request.TargetGeneration == member.Generation && request.Status != api.RequestSuperseded && strings.Contains(request.Message, id) {
			return nil
		}
	}
	message := config.Text("remote.publication_confirmation_request", id, member.TaskID)
	key := sha256.Sum256([]byte(id + "\x00" + member.TaskID + "\x00" + message))
	_, err = s.Project.CreateRequest(ctx, "approval-"+hex.EncodeToString(key[:]), member.TaskID, api.NewCollabRequest{Kind: api.RequestReview, TargetGeneration: member.Generation, Message: message})
	return err
}

// publicationID names a joint publication by its operation, material and members: the same three
// always give the same ID.
func publicationID(operation string, raw json.RawMessage, members []api.PublicationMember) (string, error) {
	identity, err := json.Marshal(struct {
		Operation string
		Payload   json.RawMessage
		Members   []api.PublicationMember
	}{operation, raw, members})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(identity)
	return "review-" + hex.EncodeToString(digest[:]), nil
}

// replayRoute routes a saved one-member publication for the calling execution.
// direct reports that the proof or the refusal replaces the saved result. A card
// that left the batch, or whose claim was released, leaves the saved result as
// the answer, so a retry of a published operation still succeeds.
func (s *Session) replayRoute(ctx context.Context, operation string, tasks []string, member string) (proof *client.Claim, direct bool, err error) {
	members, shown, err := s.liveReviewMembers(ctx, tasks)
	if api.IsCode(err, api.CodeLeaseInvalid) {
		return nil, false, nil
	}
	if err != nil || len(members) != 1 || members[0].TaskID != member {
		return nil, false, err
	}
	proof, joint, err := s.singleMemberAuthority(operation, tasks, members, shown)
	return proof, !joint, err
}

// liveReviewMembers reads each task once and returns the version of every
// working or review card, in task ID order, with the card as it was read. A
// live card whose claim no longer stands cannot take part in any review write.
func (s *Session) liveReviewMembers(ctx context.Context, tasks []string) ([]api.PublicationMember, map[string]api.Task, error) {
	members := make([]api.PublicationMember, 0, len(tasks))
	shown := make(map[string]api.Task, len(tasks))
	for _, id := range slices.Sorted(slices.Values(tasks)) {
		task, err := s.Project.Show(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if task.State != "working" && task.State != "review" {
			continue
		}
		if task.Claim == nil || !api.ClaimStands(task.Claim.Status) {
			return nil, nil, &api.Error{Code: api.CodeLeaseInvalid, Message: "review member has no live execution: " + id}
		}
		members = append(members, api.PublicationMember{TaskID: id, Revision: task.Revision, Generation: task.Claim.Generation})
		shown[id] = task
	}
	return members, shown, nil
}

// singleMemberAuthority chooses the proof for a review write with at most one
// live member. The holder of the member's current generation proves it
// directly. A group coordinator process holds no credential of that
// generation, so it takes the coordinator write authority when it applies
// (coordinatorAuthority) and otherwise prepares a joint publication with that
// one member and waits for the holder. joint reports that the caller coordinates
// the card's group: the flow goes on with the replay of a published
// publication and the coordinator write check, and ends in the one-member
// publication when neither applies. Any other process without a credential for the
// member is refused here with the holder named, because the server refuses
// every unproven write to a claimed card. A credential of an older generation
// held by any other process is sent as it is, and the server answers with the
// takeover.
//
// The choice only selects the flow. The server decides the authority: the
// holder's confirmation of the exact material, or, when coordinatorAuthority
// applies, the group lease and the card state. A process that merely claims to
// be a coordinator gains none.
func (s *Session) singleMemberAuthority(operation string, tasks []string, members []api.PublicationMember, shown map[string]api.Task) (proof *client.Claim, joint bool, err error) {
	ids := tasks
	if len(tasks) > 1 {
		ids = make([]string, len(members))
		for i, member := range members {
			ids[i] = member.TaskID
		}
	}
	proof, err = s.ClaimsFor(ids)
	if err != nil || len(members) == 0 {
		return proof, false, err
	}
	member := members[0]
	var held uint64
	if proof != nil {
		held = proof.Members[member.TaskID].Generation
	}
	if held == member.Generation {
		return proof, false, nil
	}
	// The holder alone submits its own disposition of the findings. A
	// coordinator that still holds a replaced generation's credential asks the
	// current holder too, so the stale proof is never sent in its place.
	if operation != "review-disposition" {
		coordinator, err := s.coordinatesTask(shown[member.TaskID])
		if err != nil || coordinator {
			return nil, coordinator, err
		}
	}
	if held != 0 {
		return proof, false, nil
	}
	claim := shown[member.TaskID].Claim
	return nil, false, &api.Error{Code: api.CodeHeldByOther, Message: config.Text("remote.review_member_held_by_other", member.TaskID, claim.ExecutionID, claim.Owner, claim.Generation)}
}

// requireAbandonCaller refuses a review abandon of several live members from a
// process that holds the credential of the current generation of none of them
// and coordinates none of their groups. Unless the coordinator write authority
// applies, every holder still confirms the exact material before the server
// writes; this keeps a stranger from creating that
// material and one confirmation request per holder, as a single-member abandon
// already refuses it.
func (s *Session) requireAbandonCaller(members []api.PublicationMember, shown map[string]api.Task) error {
	for _, member := range members {
		credential, found, err := s.Credential(member.TaskID)
		if err != nil {
			return err
		}
		if found && !credential.Lost && credential.Generation == member.Generation {
			return nil
		}
		coordinator, err := s.coordinatesTask(shown[member.TaskID])
		if err != nil || coordinator {
			return err
		}
	}
	first := shown[members[0].TaskID].Claim
	return &api.Error{Code: api.CodeHeldByOther, Message: config.Text("remote.review_member_held_by_other", members[0].TaskID, first.ExecutionID, first.Owner, first.Generation)}
}

// coordinatesTask reports whether this process holds the coordinator
// credential of the group the card belongs to.
func (s *Session) coordinatesTask(task api.Task) (bool, error) {
	group := board.TaskGroupFrom(task.Text)
	if group == "" {
		return false, nil
	}
	credential, found, err := s.CoordinatorCredential(group)
	if err != nil {
		return false, err
	}
	return found && !credential.Lost && credential.ResourceKind == "group", nil
}

// ConfirmReviewPublication confirms only the credential available to this
// process, preserving fixed execution identity across restarts and takeovers.
func (s *Session) ConfirmReviewPublication(ctx context.Context, id, task string) (api.Publication, error) {
	prepared, err := s.Project.Publication(ctx, id)
	if err != nil {
		return api.Publication{}, err
	}
	credential, found, err := s.Credential(task)
	if err != nil {
		return api.Publication{}, err
	}
	if !found || credential.Lost {
		return api.Publication{}, &api.Error{Code: api.CodeHeldByOther, Message: "this process does not hold the publication member"}
	}
	for _, member := range prepared.Members {
		if member.TaskID == task {
			return s.Project.ConfirmPublication(ctx, id, api.PublicationApproval{TaskID: task, Digest: prepared.Digest, Revision: member.Revision, Generation: member.Generation}, *credential.Claim())
		}
	}
	return api.Publication{}, &api.Error{Code: api.CodeInvalidRequest, Message: "task is not a publication member"}
}
