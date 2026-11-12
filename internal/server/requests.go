package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

var requestKinds = []string{api.RequestFix, api.RequestSync, api.RequestReview, api.RequestStop}

func requestID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "req-" + hex.EncodeToString(sum[:8])
}
func requestName(task, id string) string { return filepath.Join("requests", task, id+".json") }
func requestDir(task string) string      { return servicePath("requests", task) }
func (p *project) readRequests(task string) ([]api.CollabRequest, error) {
	dir := filepath.Join(p.root, requestDir(task))
	exists, err := fs.DirectoryExists(p.root, dir)
	if err != nil || !exists {
		return []api.CollabRequest{}, err
	}
	entries, err := fs.ListDirectory(p.root, dir)
	if err != nil {
		return nil, err
	}
	out := []api.CollabRequest{}
	for _, entry := range entries {
		if entry.Kind != fs.KindFile || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		var request api.CollabRequest
		if _, err := readJSON(p.root, filepath.Join(requestDir(task), entry.Name), &request); err != nil {
			return nil, err
		}
		out = append(out, request)
	}
	slices.SortFunc(out, func(a, b api.CollabRequest) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}
func (p *project) requests(id string) ([]api.CollabRequest, error) {
	id, err := board.NormalizeTaskID(id)
	if err != nil {
		return nil, err
	}
	return p.readRequests(id)
}

// requestWrite shares generation validation, the request state and the exact
// operation receipt in one redo. It never advances the card's content version.
func (p *project) requestWrite(key, operation, id string, payload any, produce func(*board.Transaction) (api.CollabRequest, error), status int) outcome {
	return p.idempotent(key, operation, payload, func() outcome {
		var request api.CollabRequest
		err := board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}, Tasks: []string{id}}, func(tx *board.Transaction) error {
			var err error
			request, err = produce(tx)
			if err != nil {
				return err
			}
			if err := stageJSON(tx, requestName(id, request.ID), request); err != nil {
				return err
			}
			data, err := json.Marshal(request)
			if err != nil {
				return err
			}
			return stageJSON(tx, filepath.Join("receipts", filepath.Base(receiptPath(key))), receipt{Key: key, Operation: operation, Digest: requestDigest(operation, payload), Status: receiptDone, HTTP: status, Body: data, CreatedAt: p.server.now()})
		})
		if err != nil {
			return outcome{err: err}
		}
		_ = p.events.append(api.Event{Kind: api.EventRequest, TaskID: id, RequestID: request.ID, Generation: request.TargetGeneration})
		return outcome{status: status, body: request}
	}, func() (outcome, bool) { return outcome{}, false })
}
func (p *project) createRequest(key, id string, input api.NewCollabRequest) outcome {
	if !slices.Contains(requestKinds, input.Kind) || !api.ValidIdentity(input.FromClient) || strings.TrimSpace(input.Message) == "" || len(input.Message) > 64<<10 {
		return outcome{err: apiError(api.CodeInvalidRequest, "request needs kind, client and a message of at most 64 KiB")}
	}
	normalized, err := board.NormalizeTaskID(id)
	if err != nil {
		return outcome{err: err}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requestWrite(key, "request:"+id, normalized, input, func(tx *board.Transaction) (api.CollabRequest, error) {
		var request api.CollabRequest
		task, err := tx.Snapshot(normalized)
		if err != nil {
			return request, err
		}
		claim, found, err := p.readClaim(normalized)
		if err != nil {
			return request, err
		}
		if !claimedState(task.Entry.State) || !found || claim.Status != api.ClaimActive {
			return request, apiError(api.CodeClaimRequired, "task has no holder to answer a request")
		}
		if claim.Generation != input.TargetGeneration || !p.server.now().Before(claim.ExpiresAt) {
			return request, apiError(api.CodeLeaseInvalid, "request target generation is not live")
		}
		now := p.server.now()
		return api.CollabRequest{Revision: 1, ID: requestID(key), TaskID: normalized, Kind: input.Kind, Message: input.Message, FromClient: input.FromClient, TargetGeneration: input.TargetGeneration, Status: api.RequestOpen, CreatedAt: now, UpdatedAt: now}, nil
	}, http.StatusCreated)
}
func (p *project) answerRequest(key, id, requestID string, answer api.CollabAnswer, h holder) outcome {
	if !slices.Contains([]string{api.RequestAccepted, api.RequestDone, api.RequestRejected}, answer.Status) || !api.ValidIdentity(requestID) {
		return outcome{err: apiError(api.CodeInvalidRequest, "invalid request answer")}
	}
	normalized, err := board.NormalizeTaskID(id)
	if err != nil {
		return outcome{err: err}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requestWrite(key, "answer:"+id+":"+requestID, normalized, answer, func(tx *board.Transaction) (api.CollabRequest, error) {
		var request api.CollabRequest
		claim, err := p.requireHolder(normalized, h)
		if err != nil {
			return request, err
		}
		data, found, err := tx.ReadGroup(serviceGroup, requestName(normalized, requestID))
		if err != nil {
			return request, err
		}
		if !found {
			return request, apiError(api.CodeNotFound, "request does not exist")
		}
		if err := json.Unmarshal(data, &request); err != nil {
			return request, err
		}
		if request.TargetGeneration != claim.Generation || request.Status == api.RequestSuperseded {
			return request, apiError(api.CodeLeaseInvalid, "request targets another generation")
		}
		if answer.ExpectedRevision != 0 && answer.ExpectedRevision != request.Revision {
			return request, apiError(api.CodeRevisionConflict, "request revision changed")
		}
		if request.Status == answer.Status && request.Note == answer.Note {
			return request, nil
		}
		if request.Status == api.RequestDone || request.Status == api.RequestRejected {
			return request, apiError(api.CodeRevisionConflict, "a settled request cannot change its answer")
		}
		request.Status, request.Note, request.UpdatedAt = answer.Status, answer.Note, p.server.now()
		request.Revision++
		return request, nil
	}, http.StatusOK)
}

// stageSupersedeRequests runs inside the execution handoff transaction, so no
// old request remains actionable after the successor claim becomes visible.
func (p *project) stageSupersedeRequests(tx *board.Transaction, task string, generation uint64) error {
	requests, err := p.readRequests(task)
	if err != nil {
		return err
	}
	for _, request := range requests {
		if request.TargetGeneration < generation && (request.Status == api.RequestOpen || request.Status == api.RequestAccepted) {
			request.Status, request.UpdatedAt = api.RequestSuperseded, p.server.now()
			request.Revision++
			if err := stageJSON(tx, requestName(task, request.ID), request); err != nil {
				return err
			}
		}
	}
	return nil
}
