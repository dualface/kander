package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func publicationName(id string) string { return "publications/" + id + ".json" }

func payloadDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	raw, err = json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (p *project) readPublication(id string) (api.Publication, error) {
	if !api.ValidIdentity(id) {
		return api.Publication{}, apiError(api.CodeInvalidRequest, "invalid publication ID")
	}
	var value api.Publication
	found, err := readJSON(p.root, servicePath(filepath.FromSlash(publicationName(id))), &value)
	if err != nil {
		return value, err
	}
	if !found {
		return value, apiError(api.CodeNotFound, "joint publication does not exist")
	}
	return value, nil
}

func (p *project) publicationBinding(id, operation string, request any, tasks []string) (api.Publication, error) {
	value, err := p.readPublication(id)
	if err != nil {
		return value, err
	}
	digest, err := payloadDigest(request)
	if err != nil {
		return value, err
	}
	if value.Operation != operation || value.Digest != digest {
		return value, apiError(api.CodeEvidenceIncomplete, "publication material differs from the confirmed operation")
	}
	members := make([]string, len(value.Members))
	for i, member := range value.Members {
		members[i] = member.TaskID
	}
	ids := slices.Clone(tasks)
	slices.Sort(ids)
	slices.Sort(members)
	if !slices.Equal(ids, members) {
		return value, apiError(api.CodeEvidenceIncomplete, "joint publication membership changed")
	}
	return value, nil
}

func (p *project) validatePublication(tx *board.Transaction, value api.Publication) error {
	if value.Status != "prepared" {
		return apiError(api.CodeRevisionConflict, "publication is no longer prepared")
	}
	for _, member := range value.Members {
		if _, err := tx.Expect(member.TaskID, "", member.Revision); err != nil {
			return err
		}
		claim, found, err := p.readClaim(member.TaskID)
		if err != nil {
			return err
		}
		approval, approved := value.Approvals[member.TaskID]
		if !found || claim.Status != api.ClaimActive || !p.server.now().Before(claim.ExpiresAt) || claim.Generation != member.Generation {
			return apiError(api.CodeLeaseInvalid, "publication member execution changed or expired")
		}
		if !approved || approval.Digest != value.Digest || approval.Revision != member.Revision || approval.Generation != member.Generation || approval.ClientID != claim.ClientID || approval.ExecutionID != claim.ExecutionID {
			return apiError(api.CodeEvidenceIncomplete, "publication still needs the current holder's confirmation for "+member.TaskID)
		}
	}
	return nil
}

func (s *Server) publicationRoutes(mux *http.ServeMux) {
	const base = "/api/v1/projects/{project}/publications"
	s.projectRoute(mux, "GET "+base+"/{publication}", func(p *project, r *http.Request) outcome {
		value, err := p.readPublication(r.PathValue("publication"))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "POST "+base, func(p *project, r *http.Request) outcome {
		var request api.PublicationPrepare
		if err := decode(r, &request); err != nil {
			return outcome{err: err}
		}
		if !api.ValidIdentity(request.ID) || !strings.HasPrefix(request.Operation, "review-") || len(request.Members) == 0 || len(request.Members) > 256 || len(request.Payload) == 0 {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid publication identity or members")}
		}
		digest, err := payloadDigest(request.Payload)
		if err != nil {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid publication material")}
		}
		ids, seen := []string{}, map[string]bool{}
		for _, member := range request.Members {
			if !api.ValidIdentity(member.TaskID) || seen[member.TaskID] || member.Revision == 0 || member.Generation == 0 {
				return outcome{err: apiError(api.CodeInvalidRequest, "invalid publication member version")}
			}
			seen[member.TaskID] = true
			ids = append(ids, member.TaskID)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		value := api.Publication{PublicationPrepare: request, Digest: digest, Status: "prepared", Approvals: map[string]api.PublicationApproval{}, CreatedAt: p.server.now()}
		err = board.WithTransaction(p.board, board.LockScope{Tasks: ids, Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
			existing, err := p.readPublication(request.ID)
			if err == nil {
				oldDigest, _ := payloadDigest(existing.PublicationPrepare)
				newDigest, _ := payloadDigest(request)
				if oldDigest != newDigest {
					return apiError(api.CodeIdempotencyMismatch, "publication ID reused with different material")
				}
				value = existing
				return nil
			}
			if !api.IsCode(err, api.CodeNotFound) {
				return err
			}
			for _, member := range request.Members {
				snapshot, err := tx.Expect(member.TaskID, "", member.Revision)
				if err != nil {
					return err
				}
				claim, found, err := p.readClaim(member.TaskID)
				if err != nil {
					return err
				}
				if !claimedState(snapshot.Entry.State) || !found || claim.Generation != member.Generation || claim.Status != api.ClaimActive {
					return apiError(api.CodeLeaseInvalid, "publication member is not the observed execution")
				}
			}
			return stageJSON(tx, publicationName(request.ID), value)
		})
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "POST "+base+"/{publication}/confirm", func(p *project, r *http.Request) outcome {
		var request api.PublicationApproval
		if err := decode(r, &request); err != nil {
			return outcome{err: err}
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		value, err := p.readPublication(r.PathValue("publication"))
		if err != nil {
			return outcome{err: err}
		}
		if value.Status != "prepared" || request.Digest != value.Digest {
			return outcome{err: apiError(api.CodeRevisionConflict, "publication material changed or was published")}
		}
		var member *api.PublicationMember
		for i := range value.Members {
			if value.Members[i].TaskID == request.TaskID {
				member = &value.Members[i]
				break
			}
		}
		if member == nil || member.Revision != request.Revision || member.Generation != request.Generation {
			return outcome{err: apiError(api.CodeEvidenceIncomplete, "confirmation does not match the prepared version")}
		}
		err = board.WithTransaction(p.board, board.LockScope{Tasks: []string{request.TaskID}, Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
			if _, err := tx.Expect(request.TaskID, "", request.Revision); err != nil {
				return err
			}
			claim, err := p.requireHolder(request.TaskID, holderOf(r))
			if err != nil {
				return err
			}
			if claim.Generation != request.Generation {
				return apiError(api.CodeLeaseInvalid, "confirmation execution changed")
			}
			if old, exists := value.Approvals[request.TaskID]; exists && old.Revision == request.Revision && old.Generation == request.Generation && old.Digest == request.Digest && old.ClientID == claim.ClientID && old.ExecutionID == claim.ExecutionID {
				return nil
			}
			request.ClientID, request.ExecutionID, request.ConfirmedAt = claim.ClientID, claim.ExecutionID, p.server.now()
			value.Approvals[request.TaskID] = request
			return stageJSON(tx, publicationName(value.ID), value)
		})
		return result(http.StatusOK, value, err)
	})
}
