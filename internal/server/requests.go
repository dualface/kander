package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

var requestKinds = []string{api.RequestFix, api.RequestSync, api.RequestReview, api.RequestStop}

func requestID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "req-" + hex.EncodeToString(sum[:8])
}

func requestDir(task string) string { return filepath.Join("control", "requests", task) }

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
	for _, e := range entries {
		if e.Kind != fs.KindFile || !strings.HasSuffix(e.Name, ".json") {
			continue
		}
		var r api.CollabRequest
		if _, err := readJSON(p.root, filepath.Join(requestDir(task), e.Name), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b api.CollabRequest) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

func (p *project) writeRequest(r api.CollabRequest) error {
	return writeJSON(p.root, filepath.Join(requestDir(r.TaskID), r.ID+".json"), r)
}

// requests lists the collaboration requests of a task, oldest first.
func (p *project) requests(id string) ([]api.CollabRequest, error) {
	id, err := board.NormalizeTaskID(id)
	if err != nil {
		return nil, err
	}
	return p.readRequests(id)
}

// createRequest records a request for the current holder. The target
// generation must be current: a request written for an execution that was
// taken over is refused instead of reaching the new execution.
func (p *project) createRequest(key, id string, req api.NewCollabRequest) outcome {
	if !slices.Contains(requestKinds, req.Kind) || !api.ValidIdentity(req.FromClient) || strings.TrimSpace(req.Message) == "" {
		return outcome{err: apiError(api.CodeInvalidRequest, "kind, from_client and message are required")}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		t, err := p.show(id)
		if err != nil {
			return outcome{err: err}
		}
		if t.Claim == nil || !claimedState(t.State) || t.Claim.Status == api.ClaimReleased {
			return outcome{err: apiError(api.CodeClaimRequired, "task "+t.TaskID+" has no holder to answer a request")}
		}
		if t.Claim.Generation != req.TargetGeneration {
			return outcome{err: apiError(api.CodeLeaseInvalid, "the target generation is not current")}
		}
		now := p.server.now()
		r := api.CollabRequest{
			ID: requestID(key), TaskID: t.TaskID, Kind: req.Kind, Message: req.Message,
			FromClient: req.FromClient, TargetGeneration: req.TargetGeneration, Status: api.RequestOpen, CreatedAt: now, UpdatedAt: now,
		}
		if err := p.writeRequest(r); err != nil {
			return outcome{err: err}
		}
		_ = p.events.append(api.Event{Kind: api.EventRequest, TaskID: t.TaskID, RequestID: r.ID, Generation: r.TargetGeneration})
		return outcome{status: http.StatusCreated, body: r}
	}
	// The request ID derives from the idempotency key, so the effect of an
	// interrupted attempt is the request file itself.
	applied := func() (outcome, bool) {
		task, err := board.NormalizeTaskID(id)
		if err != nil {
			return outcome{}, false
		}
		var r api.CollabRequest
		found, err := readJSON(p.root, filepath.Join(requestDir(task), requestID(key)+".json"), &r)
		return outcome{status: http.StatusCreated, body: r}, err == nil && found
	}
	return p.idempotent(key, "request:"+id, req, run, applied)
}

// answerRequest lets the live holder of the target generation settle a request.
func (p *project) answerRequest(key, id, requestID string, answer api.CollabAnswer, h holder) outcome {
	switch answer.Status {
	case api.RequestAccepted, api.RequestDone, api.RequestRejected:
	default:
		return outcome{err: apiError(api.CodeInvalidRequest, "status must be accepted, done or rejected")}
	}
	if !api.ValidIdentity(requestID) {
		return outcome{err: apiError(api.CodeInvalidRequest, "invalid request id")}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		id, err := board.NormalizeTaskID(id)
		if err != nil {
			return outcome{err: err}
		}
		claim, err := p.requireHolder(id, h)
		if err != nil {
			return outcome{err: err}
		}
		var r api.CollabRequest
		found, err := readJSON(p.root, filepath.Join(requestDir(id), requestID+".json"), &r)
		if err != nil {
			return outcome{err: err}
		}
		if !found {
			return outcome{err: apiError(api.CodeNotFound, "request "+requestID+" does not exist")}
		}
		if r.TargetGeneration != claim.Generation || r.Status == api.RequestSuperseded {
			return outcome{err: apiError(api.CodeLeaseInvalid, "the request targets another generation")}
		}
		r.Status, r.Note, r.UpdatedAt = answer.Status, answer.Note, p.server.now()
		if err := p.writeRequest(r); err != nil {
			return outcome{err: err}
		}
		_ = p.events.append(api.Event{Kind: api.EventRequest, TaskID: id, RequestID: r.ID, Generation: r.TargetGeneration})
		return outcome{status: http.StatusOK, body: r}
	}
	return p.idempotent(key, "answer:"+id+":"+requestID, answer, run, func() (outcome, bool) { return outcome{}, false })
}

// supersedeRequests closes the open requests of earlier generations.
func (p *project) supersedeRequests(task string, generation uint64) error {
	all, err := p.readRequests(task)
	if err != nil {
		return err
	}
	for _, r := range all {
		if r.TargetGeneration < generation && (r.Status == api.RequestOpen || r.Status == api.RequestAccepted) {
			r.Status, r.UpdatedAt = api.RequestSuperseded, time.Now().UTC()
			if err := p.writeRequest(r); err != nil {
				return err
			}
		}
	}
	return nil
}
