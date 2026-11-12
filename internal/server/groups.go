package server

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

func (p *project) groupWrite(key, operation, group string, request any, h holder, apply func(board.TransactionExtension) (board.CoordinatorCheckpoint, error)) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.idempotent(key, operation, request, func() outcome {
		if _, err := p.requireGroup(group, h); err != nil {
			return outcome{err: err}
		}
		guard := board.TransactionExtension{Groups: []string{serviceGroup}, Before: func(*board.Transaction) error { _, err := p.requireGroup(group, h); return err }, After: func(tx *board.Transaction) error {
			if _, err := p.requireGroup(group, h); err != nil {
				return err
			}
			for _, id := range tx.ChangedTaskIDs() {
				snapshot, err := tx.Snapshot(id)
				if err != nil {
					return err
				}
				if claimedState(snapshot.Entry.State) {
					if _, err := p.requireHolder(id, h); err != nil {
						return err
					}
				}
			}
			value, ok := tx.OperationResult().(board.CoordinatorCheckpoint)
			if !ok {
				return apiError(api.CodeInternal, "coordinator producer omitted its transaction result")
			}
			return p.groupReceipt(tx, key, operation, request, value)
		}}
		checkpoint, err := apply(guard)
		if err == nil || len(checkpoint.Members) > 0 {
			_ = p.events.append(api.Event{Kind: api.EventGroupChanged, GroupID: group})
		}
		return result(http.StatusOK, checkpoint, err)
	}, func() (outcome, bool) { return outcome{}, false })
}
func (s *Server) groupRoutes(mux *http.ServeMux) {
	const base = "/api/v1/projects/{project}/groups/{group}"
	route := func(pattern string, handler func(*project, *http.Request) outcome) {
		s.projectRoute(mux, pattern, func(p *project, r *http.Request) outcome {
			if !board.ValidTaskGroup(r.PathValue("group")) {
				return outcome{err: apiError(api.CodeInvalidRequest, "invalid task group identity")}
			}
			return handler(p, r)
		})
	}
	route("GET "+base, func(p *project, r *http.Request) outcome {
		value, err := p.groupView(r.Context(), r.PathValue("group"))
		return result(http.StatusOK, value, err)
	})
	route("POST "+base+"/claim", func(p *project, r *http.Request) outcome {
		var input api.GroupClaim
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		return p.claimGroup(r.Context(), r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("group"), r.Header.Get(api.HeaderClientID), input, holderOf(r))
	})
	route("POST "+base+"/renew", func(p *project, r *http.Request) outcome {
		value, err := p.renewGroup(r.PathValue("group"), holderOf(r))
		return result(http.StatusOK, value, err)
	})
	route("POST "+base+"/observation", func(p *project, r *http.Request) outcome { return p.observeGroup(r) })
	route("POST "+base+"/release", func(p *project, r *http.Request) outcome { return p.releaseGroup(r) })
	route("POST "+base+"/facts", func(p *project, r *http.Request) outcome {
		var request board.CoordinatorReconcile
		if err := decode(r, &request); err != nil {
			return outcome{err: err}
		}
		if request.GroupID != r.PathValue("group") {
			return outcome{err: apiError(api.CodeInvalidRequest, "group mismatch")}
		}
		request.CWD = "project:" + p.info.ID
		p.mu.Lock()
		defer p.mu.Unlock()
		facts, err := board.CollectCoordinatorGitFacts(r.Context(), p.board, request)
		return result(http.StatusOK, facts, err)
	})
	route("POST "+base+"/reconcile", func(p *project, r *http.Request) outcome {
		var input api.GroupReconcile
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		if input.Request.GroupID != r.PathValue("group") || len(input.Checks) > 10000 {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid coordinator reconciliation")}
		}
		input.Request.CWD = "project:" + p.info.ID
		checks := map[string]api.CoordinatorGitCheck{}
		for _, check := range input.Checks {
			if len(check.Error) > 4096 {
				return outcome{err: apiError(api.CodeInvalidRequest, "Git verdict exceeds its bound")}
			}
			digest := requestDigest("coordinator-fact", check.Facts)
			if previous, found := checks[digest]; found && previous.Error != check.Error {
				return outcome{err: apiError(api.CodeInvalidRequest, "conflicting Git verdicts")}
			}
			checks[digest] = check
		}
		return p.groupWrite(r.Header.Get(api.HeaderIdempotencyKey), "group-reconcile:"+input.Request.GroupID, input.Request.GroupID, input, holderOf(r), func(extension board.TransactionExtension) (board.CoordinatorCheckpoint, error) {
			return board.ReconcileCoordinatorWithTransaction(r.Context(), p.board, input.Request, func(_ context.Context, facts board.CoordinatorGitFacts) error {
				if check, found := checks[requestDigest("coordinator-fact", facts)]; found && reflect.DeepEqual(check.Facts, facts) {
					if check.Error != "" {
						return errors.New(check.Error)
					}
					return nil
				}
				return apiError(api.CodeEvidenceIncomplete, "coordinator Git facts changed or lack a client verdict")
			}, extension)
		})
	})
	route("POST "+base+"/recover", func(p *project, r *http.Request) outcome {
		var input api.GroupRecover
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		if input.Request.GroupID != r.PathValue("group") || input.Observation.Status != "stopped" {
			return outcome{err: apiError(api.CodeEvidenceIncomplete, "recovery needs a confirmed stopped execution")}
		}
		h := holderOf(r)
		return p.groupWrite(r.Header.Get(api.HeaderIdempotencyKey), "group-recover:"+input.Request.GroupID, input.Request.GroupID, input, h, func(extension board.TransactionExtension) (board.CoordinatorCheckpoint, error) {
			readiness := board.TransactionExtension{Before: func(tx *board.Transaction) error {
				view, err := tx.CurrentExecutionInTransaction(input.Request.Member)
				if err != nil {
					return err
				}
				if view.Exists && view.Record.Status == board.ExecutionPending {
					return apiError(api.CodeEvidenceIncomplete, "a pending start must be reconciled before coordinator recovery")
				}
				return nil
			}}
			return board.RecoverCoordinatorWithTransaction(r.Context(), p.board, input.Request, input.ObservedRevision, extension, readiness)
		})
	})
}
