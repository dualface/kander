package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/api"
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

// ReviewAuthority prepares a joint operation, confirms locally owned members,
// and asks other holders to confirm the exact prepared material. Pending
// confirmations are durable and do not publish any card evidence.
func (s *Session) ReviewAuthority(ctx context.Context, operation string, payload any, tasks []string) (*client.Claim, error) {
	raw, err := s.publicationPayload(operation, payload)
	if err != nil {
		return nil, err
	}
	if proof, found, err := s.completedReviewPublication(ctx, operation, raw); err != nil || found {
		return proof, err
	}
	if len(tasks) <= 1 {
		return s.ClaimsFor(tasks)
	}
	members := make([]api.PublicationMember, 0, len(tasks))
	for _, id := range slices.Sorted(slices.Values(tasks)) {
		task, err := s.Project.Show(ctx, id)
		if err != nil {
			return nil, err
		}
		if task.State != "working" && task.State != "review" {
			continue
		}
		if task.Claim == nil || task.Claim.Status != api.ClaimActive {
			return nil, &api.Error{Code: api.CodeLeaseInvalid, Message: "review member has no live execution: " + id}
		}
		members = append(members, api.PublicationMember{TaskID: id, Revision: task.Revision, Generation: task.Claim.Generation})
	}
	if len(members) <= 1 {
		ids := make([]string, len(members))
		for i, member := range members {
			ids[i] = member.TaskID
		}
		return s.ClaimsFor(ids)
	}
	raw, err = s.freezePublicationMaterial(operation, raw, members)
	if err != nil {
		return nil, err
	}
	identity, err := json.Marshal(struct {
		Operation string
		Payload   json.RawMessage
		Members   []api.PublicationMember
	}{operation, raw, members})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(identity)
	id := "review-" + hex.EncodeToString(digest[:])
	if _, err := s.publicationHistory(operation, raw, id); err != nil {
		return nil, err
	}
	prepared, err := s.Project.PreparePublication(ctx, api.PublicationPrepare{ID: id, Operation: operation, Payload: raw, Members: members})
	if err != nil {
		return nil, err
	}
	if prepared.Status == "published" {
		return &client.Claim{PublicationID: id, PublicationPayload: raw}, nil
	}
	var pending []string
	for _, member := range members {
		credential, found, err := s.Credential(member.TaskID)
		if err != nil {
			return nil, err
		}
		if found && !credential.Lost && credential.Generation == member.Generation {
			prepared, err = s.Project.ConfirmPublication(ctx, id, api.PublicationApproval{TaskID: member.TaskID, Digest: prepared.Digest, Revision: member.Revision, Generation: member.Generation}, *credential.Claim())
			if err != nil {
				return nil, err
			}
		} else if _, approved := prepared.Approvals[member.TaskID]; !approved {
			key := sha256.Sum256([]byte(id + "\x00" + member.TaskID))
			message := config.Text("remote.publication_confirmation_request", id, member.TaskID)
			if _, err := s.Project.CreateRequest(ctx, "approval-"+hex.EncodeToString(key[:]), member.TaskID, api.NewCollabRequest{Kind: api.RequestReview, TargetGeneration: member.Generation, Message: message}); err != nil {
				return nil, err
			}
			pending = append(pending, member.TaskID)
		}
	}
	if len(pending) > 0 {
		return nil, &api.Error{Code: api.CodeEvidenceIncomplete, Message: "publication " + id + " awaits holder confirmation for " + strings.Join(pending, ", ")}
	}
	return &client.Claim{PublicationID: id, PublicationPayload: raw}, nil
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
