package server

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// executionState preserves the launcher's evidence cursor across server restarts.
// Missing prototype cursors are conservatively treated as executor evidence.
func (p *project) executionState(id string, h holder) (api.ExecutionState, error) {
	var out api.ExecutionState
	id, err := board.NormalizeTaskID(id)
	if err != nil {
		return out, err
	}
	claim, err := p.requireHolder(id, h)
	if err != nil {
		return out, err
	}
	entry, err := p.cursor(id, claim.Generation)
	if err != nil {
		return out, err
	}
	out.Task, err = p.show(id)
	if err != nil {
		return out, err
	}
	out.Record, _, err = board.CurrentExecution(p.board, id)
	if err != nil {
		return out, err
	}
	out.Standing, err = board.ReadExecutionStanding(p.board, entry)
	out.Standing.Evidence = out.Standing.Evidence || claim.ExecutorEvidence || claim.LaunchRevision == 0 || claim.LaunchRevision != out.Task.Revision
	stamp := strconv.FormatInt(p.server.now().UnixNano(), 10)
	out.ObservationTicket = stamp + "." + p.observationSignature(id, out.Task.Revision, claim.Generation, stamp)
	return out, err
}

func (p *project) observationSignature(id string, revision, generation uint64, stamp string) string {
	return p.server.token(p.info.ID, "observation:"+id+":"+strconv.FormatUint(revision, 10), stamp, generation)
}

// The service-clock ticket bounds a client probe without comparing host clocks.
// It is bound to the version and generation the probe must still observe.
func (p *project) validObservationTicket(id string, revision, generation uint64, ticket string) bool {
	stamp, signature, ok := strings.Cut(ticket, ".")
	at, err := strconv.ParseInt(stamp, 10, 64)
	if !ok || err != nil {
		return false
	}
	age := p.server.now().Sub(time.Unix(0, at))
	return age >= 0 && age <= 30*time.Second && subtle.ConstantTimeCompare([]byte(signature), []byte(p.observationSignature(id, revision, generation, stamp))) == 1
}

// startCursor is the board cursor a launcher start holds between its claim
// and its confirmation. The board fences execution writes by the cursor of
// the start; the server keeps it in memory for the holder and rebuilds a
// bound cursor from the current execution record after a restart.
type startCursor struct {
	entry      board.Entry
	generation uint64
}

// cursor returns the execution cursor of task for generation.
func (p *project) cursor(task string, generation uint64) (board.Entry, error) {
	if c, ok := p.starts[task]; ok && c.generation == generation {
		return c.entry, nil
	}
	s, err := board.ReadSnapshot(p.board, task)
	if err != nil {
		return board.Entry{}, err
	}
	claim, found, err := p.readClaim(task)
	if err != nil {
		return board.Entry{}, err
	}
	if found && claim.Generation == generation && claim.LaunchRecordID != "" {
		record, _, err := board.CurrentExecution(p.board, task)
		if err != nil {
			return board.Entry{}, err
		}
		if record.ID == claim.LaunchRecordID && record.StartUnfinished() {
			if err := board.RestoreLauncherCursor(p.board, s.Entry, claim.LaunchRecordID, claim.LaunchRevision, claim.ExecutorEvidence); err != nil {
				return board.Entry{}, err
			}
			p.starts[task] = startCursor{entry: s.Entry, generation: generation}
			return s.Entry, nil
		}
	}
	if _, err := board.BindExecutionCursor(p.board, s.Entry); err != nil {
		return board.Entry{}, err
	}
	return s.Entry, nil
}

// execution applies one launcher fact of the live holder.
func (p *project) execution(key, id string, u api.ExecutionUpdate, h holder) outcome {
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
		entry, err := p.cursor(id, claim.Generation)
		if err != nil {
			return outcome{err: err}
		}
		nextClaim := claim
		current, err := board.ReadSnapshot(p.board, id)
		if err != nil {
			return outcome{err: err}
		}
		nextClaim.ExecutorEvidence = claim.ExecutorEvidence || claim.LaunchRevision == 0 || claim.LaunchRevision != current.Revision
		if u.Action == api.ExecutionConfirm || u.Action == api.ExecutionAdopt {
			nextClaim.OriginalText = ""
		}
		if u.Action == api.ExecutionRelease {
			nextClaim.Status, nextClaim.ExpiresAt = api.ClaimLost, p.server.now()
			if u.Presence != nil {
				observed := *u.Presence
				observed.ObservedAt = p.server.now()
				nextClaim.Observation = &observed
			}
		}
		if u.Action == api.ExecutionRollback {
			nextClaim.Status = api.ClaimReleased
		}
		validate := p.mutationGuard(id, h, false)
		if u.Action == api.ExecutionRollback {
			ownership := validate
			validate = func(tx *board.Transaction) error {
				if err := ownership(tx); err != nil {
					return err
				}
				snapshot, err := tx.Snapshot(id)
				if err != nil {
					return err
				}
				if claim.ExecutorEvidence || claim.LaunchRevision == 0 || snapshot.Revision != claim.LaunchRevision {
					return apiError(api.CodeEvidenceIncomplete, "executor evidence prevents launch rollback")
				}
				return nil
			}
		}
		if u.Action == api.ExecutionAdopt || u.Action == api.ExecutionWorkspace || u.Action == api.ExecutionRelease {
			ownership := validate
			validate = func(tx *board.Transaction) error {
				if err := ownership(tx); err != nil {
					return err
				}
				_, err := tx.Expect(id, current.Entry.State, u.ExpectedRevision)
				if (u.Action == api.ExecutionAdopt || u.Action == api.ExecutionRelease) && err == nil && !p.validObservationTicket(id, u.ExpectedRevision, claim.Generation, u.ObservationTicket) {
					return apiError(api.CodeEvidenceIncomplete, "executor observation ticket is stale or belongs to another version")
				}
				return err
			}
		}
		extension := p.taskTransaction(key, "execution:"+id, u, id, validate, &nextClaim, false)
		switch u.Action {
		case api.ExecutionRelease:
			if u.ExpectedRevision == 0 || strings.TrimSpace(u.Decision) == "" || u.Presence == nil || u.Presence.Status != "stopped" {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "lease release requires a decision and a fresh confirmed stopped executor")}
			}
			err = board.WithTransaction(p.board, board.LockScope{Tasks: []string{id}}, func(tx *board.Transaction) error {
				return stageJSON(tx, "releases/"+id+"/"+digest(key)+".json", struct {
					Generation  uint64    `json:"generation"`
					ExecutionID string    `json:"execution_id"`
					ClientID    string    `json:"client_id"`
					Decision    string    `json:"decision"`
					Revision    uint64    `json:"revision"`
					At          time.Time `json:"at"`
				}{claim.Generation, claim.ExecutionID, claim.ClientID, u.Decision, u.ExpectedRevision, p.server.now()})
			}, extension)
		case api.ExecutionWorkspace:
			expected := "kander/" + p.info.ID + "/" + id + "/g" + strconv.FormatUint(claim.Generation, 10)
			if p.info.Remote == "" || u.ExpectedRevision == 0 || u.Branch != expected {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "workspace must name the current generation branch and inspected card revision")}
			}
			_, err = board.RecordExecutionBranchWithTransaction(p.board, entry, u.Branch, extension)
		case api.ExecutionUnresolve:
			_, err = board.UnresolveExecutionWithTransaction(p.board, entry, u.Window, u.Reason, extension)
		case api.ExecutionAdopt:
			if u.ExpectedRevision == 0 || strings.TrimSpace(u.Decision) == "" || u.Presence == nil || u.Presence.Status != "alive" || u.Presence.ObservedAt.IsZero() {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "adoption requires an explicit decision and a fresh confirmed local executor observation")}
			}
			current.Entry = entry
			_, err = board.AdoptExecutionWithTransaction(p.board, current, u.Decision, extension)
		case api.ExecutionWindow:
			_, err = board.RecordExecutionWindowWithTransaction(p.board, entry, u.Window, u.ExecResolved, extension)
		case api.ExecutionSession:
			_, err = board.RecordExecutionSessionWithTransaction(context.Background(), p.board, entry, u.SessionBefore, u.SessionAfter, extension)
		case api.ExecutionConfirm:
			if u.SessionBefore != "" || u.SessionAfter != "" {
				err = board.ConfirmTaskStartSessionWithTransaction(p.board, entry, u.SessionBefore, u.SessionAfter, extension)
			} else {
				err = board.ConfirmTaskStartWithTransaction(p.board, entry, extension)
			}
			if err == nil {
				// A confirmed start is no longer a failed launch to undo.
				delete(p.starts, id)
			}
		case api.ExecutionRollback:
			if claim.OriginalText == "" {
				return outcome{err: apiError(api.CodeInvalidRequest, "only a launcher start can be rolled back")}
			}
			if err = board.RollbackDocumentWithTransaction(p.board, entry, claim.OriginalText, "todo", extension); err == nil {
				delete(p.starts, id)
				_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: id, Generation: claim.Generation})
			}
		default:
			return outcome{err: apiError(api.CodeInvalidRequest, "unknown execution action")}
		}
		if err != nil {
			return outcome{err: err}
		}
		p.changed(id, claim.Generation)
		t, err := p.show(id)
		if err != nil {
			return outcome{err: err}
		}
		return outcome{status: http.StatusOK, body: t}
	}
	// Execution writes are fenced by the board ledger; a retry of an applied
	// confirmation is a board replay, and a retried window or session write
	// of the same value changes nothing.
	return p.idempotent(key, "execution:"+id, u, run, func() (outcome, bool) { return outcome{}, false })
}
