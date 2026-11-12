package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func deliveryPath(task, commit string) string {
	return filepath.Join("deliveries", task, commit+".json")
}

func (p *project) readDelivery(task, commit string) (api.DeliveryEvidence, error) {
	var value api.DeliveryEvidence
	if _, err := board.NormalizeTaskID(task); err != nil || !api.CommitSHA(commit) {
		return value, apiError(api.CodeInvalidRequest, "delivery requires a task and full commit SHA")
	}
	found, err := readJSON(p.root, servicePath(deliveryPath(task, commit)), &value)
	if err == nil && !found {
		err = apiError(api.CodeEvidenceIncomplete, "commit has no published remote delivery")
	}
	return value, err
}

func (p *project) publishDelivery(key, task string, input api.DeliveryEvidence, h holder) outcome {
	if (input.Purpose != "" && input.Purpose != "delivery" && input.Purpose != "baseline") || input.TaskID != task || !api.CommitSHA(input.Commit) || !api.ValidIdentity(input.ExecutionID) || input.Generation == 0 || input.CardRevision == 0 || input.Ref != api.DeliveryRef(p.info.ID, task, input.Generation, input.Commit) || input.Remote == "" || input.Remote != p.info.Remote || input.ObservedAt.IsZero() {
		return outcome{err: apiError(api.CodeEvidenceIncomplete, "delivery must bind the registered remote, exact commit, card version and generation")}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	operation := "delivery:" + task
	return p.idempotent(key, operation, input, func() outcome {
		err := board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}, Tasks: []string{task}}, func(tx *board.Transaction) error {
			claim, err := p.requireHolder(task, h)
			if err != nil {
				return err
			}
			if claim.ExecutionID != input.ExecutionID || claim.Generation != input.Generation {
				return apiError(api.CodeLeaseInvalid, "delivery producer is not the current execution")
			}
			if _, err := tx.Expect(task, "", input.CardRevision); err != nil {
				return err
			}
			// A later generation or content revision may attest the same SHA;
			// the previous immutable version remains available in history.
			name := deliveryPath(task, input.Commit)
			if previous, found, err := tx.ReadGroup(serviceGroup, name); err != nil {
				return err
			} else if found {
				var old api.DeliveryEvidence
				if err := json.Unmarshal(previous, &old); err != nil {
					return err
				}
				if err := tx.PutGroup(serviceGroup, filepath.Join("delivery-history", task, digest(string(previous))+".json"), string(previous)); err != nil {
					return err
				}
			}
			if err := stageJSON(tx, name, input); err != nil {
				return err
			}
			if err := stageJSON(tx, deliveryPath(task, "latest"), input); err != nil {
				return err
			}
			return p.stageOperationReceipt(tx, key, operation, input, input)
		})
		return result(http.StatusOK, input, err)
	}, func() (outcome, bool) { return outcome{}, false })
}

func (p *project) latestDelivery(task string) (*api.DeliveryEvidence, error) {
	var value api.DeliveryEvidence
	found, err := readJSON(p.root, servicePath(deliveryPath(task, "latest")), &value)
	if err != nil || !found {
		return nil, err
	}
	if value.TaskID != task || !api.CommitSHA(value.Commit) || value.Remote != p.info.Remote || value.Ref != api.DeliveryRef(p.info.ID, task, value.Generation, value.Commit) {
		return nil, apiError(api.CodeEvidenceIncomplete, "latest delivery index does not match its project and commit")
	}
	return &value, nil
}

// deliveryGuard runs in the state-changing transaction, including its final
// validation pass. A prior version's upload cannot authorize later content.
func (p *project) deliveryGuard(task, commit string, h holder) func(*board.Transaction) error {
	return func(tx *board.Transaction) error {
		claim, err := p.requireHolder(task, h)
		if err != nil {
			return err
		}
		snapshot, err := tx.Snapshot(task)
		if err != nil {
			return err
		}
		value, err := p.readDelivery(task, commit)
		if err != nil {
			return err
		}
		if value.Purpose == "baseline" || value.Generation != claim.Generation || value.ExecutionID != claim.ExecutionID || value.CardRevision != snapshot.Revision {
			return apiError(api.CodeEvidenceIncomplete, "delivery belongs to another content version or execution")
		}
		return nil
	}
}

func (s *Server) deliveryRoutes(mux *http.ServeMux) {
	const base = "/api/v1/projects/{project}/tasks/{task}/deliveries"
	s.projectRoute(mux, "POST "+base, func(p *project, r *http.Request) outcome {
		var input api.DeliveryEvidence
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		return p.publishDelivery(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("task"), input, holderOf(r))
	})
	s.projectRoute(mux, "GET "+base+"/{commit}", func(p *project, r *http.Request) outcome {
		value, err := p.readDelivery(r.PathValue("task"), r.PathValue("commit"))
		return result(http.StatusOK, value, err)
	})
}
