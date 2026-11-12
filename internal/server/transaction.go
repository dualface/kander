package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// serviceGroup shares the board redo journal with cards and execution records.
// Renewals write only this group, so they never advance a card revision.
const serviceGroup = "00000000-service-group"

func servicePath(parts ...string) string {
	return filepath.Join(append([]string{"board", ".kander", "groups", serviceGroup}, parts...)...)
}

func stageJSON(tx *board.Transaction, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return tx.PutGroup(serviceGroup, name, string(data)+"\n")
}

func (p *project) taskFromTransaction(tx *board.Transaction, id string, claim *claimRecord) (api.Task, error) {
	snapshot, err := tx.StagedSnapshot(id)
	if err != nil {
		return api.Task{}, err
	}
	task := api.Task{ObservedAt: p.server.now(), TaskID: id, State: snapshot.Entry.State, Revision: snapshot.Revision, OperationID: snapshot.OperationID, Text: snapshot.Text}
	if claim == nil {
		stored, found, err := p.readClaim(id)
		if err != nil {
			return api.Task{}, err
		}
		if found {
			claim = &stored
		}
	}
	if claim != nil {
		task.Claim = claim.view(p.server.now())
	}
	return task, nil
}

// taskTransaction publishes a receipt and an optional claim in the card's
// transaction. A crash at any publication step rolls all of them forward.
func (p *project) taskTransaction(key, operation string, request any, id string, validate func(*board.Transaction) error, claim *claimRecord, grant bool) board.TransactionExtension {
	return board.TransactionExtension{
		Groups: []string{serviceGroup}, Before: validate,
		After: func(tx *board.Transaction) error {
			if validate != nil {
				if err := validate(tx); err != nil {
					return err
				}
			}
			if claim != nil {
				snapshot, err := tx.StagedSnapshot(id)
				if err != nil {
					return err
				}
				if grant {
					record, err := tx.StagedExecutionRecord(id)
					if err != nil {
						return err
					}
					if record.StartUnfinished() {
						claim.LaunchRecordID = record.ID
					}
				}
				claim.LaunchRevision = snapshot.Revision
				if err := stageJSON(tx, "claims/"+id+".json", claim); err != nil {
					return err
				}
			}
			task, err := p.taskFromTransaction(tx, id, claim)
			if err != nil {
				return err
			}
			var body any = task
			if grant {
				body = api.ClaimGrant{Task: task, Claim: *claim.view(p.server.now()), RenewInterval: api.DurationOf(p.server.renew), TTL: api.DurationOf(p.server.ttl)}
			}
			data, err := json.Marshal(body)
			if err != nil {
				return err
			}
			r := receipt{Key: key, Operation: operation, Digest: requestDigest(operation, request), Status: receiptDone, HTTP: http.StatusOK, Body: data, CreatedAt: p.server.now()}
			return stageJSON(tx, filepath.Join("receipts", filepath.Base(receiptPath(key))), r)
		},
	}
}

// mutationGuard runs inside the board transaction. Even a write addressed to
// an unclaimed card refuses an old execution's credential after release.
func (p *project) mutationGuard(id string, h holder, terminate bool) func(*board.Transaction) error {
	return func(tx *board.Transaction) error {
		snapshot, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		if !claimedState(snapshot.Entry.State) && h.token == "" && h.generation == 0 {
			return nil
		}
		_, err = p.requireHolder(id, h)
		if err != nil && terminate && api.IsCode(err, api.CodeLeaseInvalid) {
			return nil
		}
		return err
	}
}
