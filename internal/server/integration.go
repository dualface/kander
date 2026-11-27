package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/statemachine/serverintegration"
)

type integrationRecord struct {
	Value api.Integration `json:"value"`
	Hash  string          `json:"token_hash"`
}

func integrationName(id string) string { return filepath.Join("integrations", id, "current.json") }
func (p *project) readIntegration(id string) (record integrationRecord, found bool, err error) {
	if !api.ValidIdentity(id) || !strings.HasPrefix(id, "i-") || len(id) != 66 {
		return record, false, apiError(api.CodeInvalidRequest, "invalid integration identity")
	}
	found, err = readJSON(p.root, servicePath(integrationName(id)), &record)
	return record, found, err
}
func (p *project) integrationView(id string) (api.Integration, error) {
	record, found, err := p.readIntegration(id)
	if err != nil {
		return record.Value, err
	}
	if !found {
		return api.Integration{}, apiError(api.CodeNotFound, "target has no integration record")
	}
	return p.integrationShown(record.Value)
}
func (p *project) integrationObservation(id string) (api.IntegrationObservation, error) {
	record, found, err := p.readIntegration(id)
	if err != nil {
		return api.IntegrationObservation{}, err
	}
	value := api.IntegrationObservation{}
	if found {
		snapshot, err := p.integrationView(id)
		if err != nil {
			return value, err
		}
		value.Integration = &snapshot
	}
	stamp := strconv.FormatInt(p.server.now().UnixNano(), 10)
	value.Ticket = stamp + "." + p.observationSignature("integration:"+id, record.Value.Revision, record.Value.Generation, stamp)
	return value, nil
}
func (p *project) validIntegrationObservation(id string, revision, generation uint64, ticket string) bool {
	return p.validObservationTicket("integration:"+id, revision, generation, ticket)
}
func integrationScope(input api.IntegrationRequest) (board.LockScope, error) {
	ids, err := integrationIDs(input)
	if err != nil {
		return board.LockScope{}, err
	}
	scope := board.IntegrationScope(ids, input.GroupID)
	scope.Groups = append(scope.Groups, serviceGroup)
	return scope, nil
}
func (p *project) stageIntegration(tx *board.Transaction, previous *integrationRecord, next integrationRecord) error {
	if previous != nil {
		name := filepath.Join("integrations", previous.Value.ID, "history", strconv.FormatUint(previous.Value.Generation, 10)+"-"+strconv.FormatUint(previous.Value.Revision, 10)+".json")
		if err := stageJSON(tx, name, previous); err != nil {
			return err
		}
	}
	if err := stageJSON(tx, filepath.Join("integrations", next.Value.ID, "executions", next.Value.Request.ExecutionID+".json"), next.Value); err != nil {
		return err
	}
	if next.Value.Phase == "integrated" && next.Value.Request.Purpose == "deliver" {
		for _, member := range next.Value.Request.Members {
			if member.DeliveryCommit != "" {
				if err := stageJSON(tx, "task-integrations/"+member.TaskID+"/"+strconv.FormatUint(member.Generation, 10)+".json", next.Value); err != nil {
					return err
				}
			}
		}
	}
	return stageJSON(tx, integrationName(next.Value.ID), next)
}
func (p *project) integrationToken(out outcome) outcome {
	if out.err != nil || out.status != http.StatusOK {
		return out
	}
	raw, err := json.Marshal(out.body)
	if err != nil {
		return outcome{err: err}
	}
	var grant api.IntegrationGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		return outcome{err: err}
	}
	grant.Token = p.server.token(p.info.ID, "integration:"+grant.Integration.ID, grant.Integration.Request.ExecutionID, grant.Integration.Generation)
	return outcome{status: out.status, body: grant}
}
func (p *project) claimIntegration(scope receiptScope, id string, input api.IntegrationRequest, h holder) outcome {
	lock, err := integrationScope(input)
	if err != nil {
		return outcome{err: err}
	}
	if id != api.IntegrationID(input.TargetRef) || !api.ValidIdentity(h.clientID) {
		return outcome{err: apiError(api.CodeInvalidRequest, "integration must bind a registered common remote and target")}
	}
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	// The registered remote can be bound late (bindRemote), so read it under the lock.
	if p.info.Remote == "" {
		return outcome{err: apiError(api.CodeInvalidRequest, "integration must bind a registered common remote and target")}
	}
	operation := "integration-claim:" + id
	out := p.idempotent(scope, operation, input, func() outcome {
		previous, found, err := p.readIntegration(id)
		if err != nil {
			return outcome{err: err}
		}
		k, err := p.projectIntegration(previous.Value, found, nil)
		if err != nil {
			return outcome{err: err}
		}
		actor, actorErr := p.integrationActor(input, h)
		decision := &integrationClaim{kernel: k, previous: previous, found: found, input: input, h: h, actor: actor, actorErr: actorErr}
		if !k.step(decision.event()) {
			return outcome{err: decision.err()}
		}
		now := p.server.now()
		next := integrationRecord{Value: api.Integration{SourceRef: api.IntegrationSourceRef(p.info.ID, input.ExecutionID, input.Commit), ID: id, Revision: previous.Value.Revision + 1, Generation: previous.Value.Generation + 1, ClientID: h.clientID, Status: api.ClaimActive, Phase: "reserved", Request: input, AuthorityGeneration: actor.Generation, RenewedAt: now, ExpiresAt: now.Add(p.server.ttl)}}
		next.Hash = tokenHash(p.server.token(p.info.ID, "integration:"+id, input.ExecutionID, next.Value.Generation))
		// The ticket and the actor are checked before the facts, which are only
		// read when both hold, so a refused claim answers the first failure.
		guard := func(tx *board.Transaction) error {
			ticket := p.validIntegrationObservation(id, input.ExpectedRevision, input.ExpectedGeneration, input.ObservationTicket)
			_, guardErr := p.integrationActor(input, h)
			var factsErr error
			if ticket && guardErr == nil {
				factsErr = p.integrationFacts(tx, input, true)
			}
			if k.accepts(serverintegration.ClaimGuard{Ticket: ticket, Actor: guardErr == nil, Facts: ticket && guardErr == nil && factsErr == nil}) {
				return nil
			}
			switch {
			case !ticket:
				return apiError(api.CodeEvidenceIncomplete, "integration target observation expired or changed")
			case guardErr != nil:
				return guardErr
			}
			return integrationRefusal(factsErr)
		}
		grant := api.IntegrationGrant{Integration: next.Value, RenewInterval: api.DurationOf(p.server.renew), TTL: api.DurationOf(p.server.ttl)}
		err = p.boardTransaction(lock, func(tx *board.Transaction) error {
			if err := guard(tx); err != nil {
				return err
			}
			var old *integrationRecord
			if found {
				old = &previous
			}
			if err := p.stageIntegration(tx, old, next); err != nil {
				return err
			}
			return p.stageOperationReceipt(tx, scope, operation, input, grant)
		}, board.TransactionExtension{After: guard})
		if err == nil {
			p.integrationChanged(next.Value)
		}
		return result(http.StatusOK, grant, err)
	}, func() (outcome, bool) { return outcome{}, false })
	return p.integrationToken(out)
}
func (p *project) integrationChanged(value api.Integration) {
	p.log().Info("integration changed", "integration", value.ID, "generation", value.Generation, "phase", value.Phase, "client", value.ClientID, "execution", value.Request.ExecutionID)
	for _, member := range value.Request.Members {
		_ = p.events.append(api.Event{Kind: api.EventTaskChanged, TaskID: member.TaskID, Generation: member.Generation})
	}
}
func (p *project) renewIntegration(id string, h holder) (api.Renewal, error) {
	if err := p.lockBoard(); err != nil {
		return api.Renewal{}, err
	}
	defer p.unlockBoard()
	lease, err := p.integrationHolder(id, h)
	if err != nil {
		return api.Renewal{}, err
	}
	record := lease.record
	scope, err := integrationScope(record.Value.Request)
	if err != nil {
		return api.Renewal{}, err
	}
	err = p.boardTransaction(scope, func(tx *board.Transaction) error {
		again, err := p.integrationHolder(id, h)
		if err != nil {
			return err
		}
		factsErr := p.integrationFacts(tx, record.Value.Request, false)
		if !again.kernel.step(again.renew(factsErr == nil)) {
			return integrationRefusal(factsErr)
		}
		record.Value.RenewedAt = p.server.now()
		record.Value.ExpiresAt = record.Value.RenewedAt.Add(p.server.ttl)
		return stageJSON(tx, integrationName(id), record)
	})
	renewal := api.Renewal{Claim: api.ClaimView{ClientID: record.Value.ClientID, ExecutionID: record.Value.Request.ExecutionID, Owner: "integration", Generation: record.Value.Generation, Status: record.Value.Status, RenewedAt: record.Value.RenewedAt, ExpiresAt: record.Value.ExpiresAt}}
	if err != nil {
		return renewal, err
	}
	return p.policyRenewal(renewal), nil
}
func (p *project) integrationAction(scope receiptScope, id string, input api.IntegrationCommand, h holder) outcome {
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	operation := "integration-action:" + id
	return p.idempotent(scope, operation, input, func() outcome {
		release := input.Action == "release"
		lease := p.integrationHolder
		if release {
			lease = p.integrationOwner
		}
		held, err := lease(id, h)
		if err != nil {
			return outcome{err: err}
		}
		record := held.record
		action := &integrationActionDecision{lease: held, input: input, now: p.server.now()}
		if !held.kernel.accepts(action.check()) {
			return outcome{err: action.revisionError()}
		}
		lock, err := integrationScope(record.Value.Request)
		if err != nil {
			return outcome{err: err}
		}
		next := record
		next.Value.Revision++
		next.Value.ObservedTarget, next.Value.ObservedAt = input.ObservedTarget, action.now
		event, ok := action.event()
		if !ok {
			return outcome{err: apiError(api.CodeInvalidRequest, "unknown integration action")}
		}
		if !held.kernel.step(event) {
			return outcome{err: action.refusal()}
		}
		switch input.Action {
		case "pushing":
			next.Value.Phase = "pushing"
		case "confirm":
			next.Value.Phase, next.Value.Status, next.Value.ExpiresAt = "integrated", api.ClaimReleased, action.now
		case "release":
			next.Value.Phase = "released"
			if record.Value.Phase == "pushing" {
				next.Value.Phase = "uncertain"
			}
			next.Value.Status, next.Value.ExpiresAt = api.ClaimReleased, action.now
		}
		guard := func(tx *board.Transaction) error {
			again, err := lease(id, h)
			if err != nil {
				return err
			}
			ticket := p.validIntegrationObservation(id, input.ExpectedRevision, record.Value.Generation, input.ObservationTicket)
			stale := apiError(api.CodeEvidenceIncomplete, "integration remote observation is stale")
			if release {
				if !again.kernel.step(again.guardRelease(ticket)) {
					return stale
				}
				return nil
			}
			var factsErr error
			if ticket {
				factsErr = p.integrationFacts(tx, record.Value.Request, false)
			}
			if !again.kernel.step(again.guard(ticket, ticket && factsErr == nil)) {
				if !ticket {
					return stale
				}
				return integrationRefusal(factsErr)
			}
			return nil
		}
		err = p.boardTransaction(lock, func(tx *board.Transaction) error {
			if err := guard(tx); err != nil {
				return err
			}
			if err := p.stageIntegration(tx, &record, next); err != nil {
				return err
			}
			return p.stageOperationReceipt(tx, scope, operation, input, next.Value)
		}, board.TransactionExtension{After: guard})
		if err == nil {
			p.integrationChanged(next.Value)
		}
		return result(http.StatusOK, next.Value, err)
	}, func() (outcome, bool) { return outcome{}, false })
}
func (s *Server) integrationRoutes(mux *routeTable) {
	const base = "/api/v1/projects/{project}/integrations/{integration}"
	s.projectRoute(mux, "GET "+base, func(p *project, r *http.Request) outcome {
		value, err := p.integrationView(r.PathValue("integration"))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "GET "+base+"/executions/{execution}", func(p *project, r *http.Request) outcome {
		value, err := p.integrationExecution(r.PathValue("integration"), r.PathValue("execution"))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "GET "+base+"/observation", func(p *project, r *http.Request) outcome {
		value, err := p.integrationObservation(r.PathValue("integration"))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "POST "+base+"/claim", func(p *project, r *http.Request) outcome {
		var input api.IntegrationRequest
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		return p.claimIntegration(scopeOf(r), r.PathValue("integration"), input, holderOf(r))
	})
	s.projectRoute(mux, "POST "+base+"/renew", func(p *project, r *http.Request) outcome {
		value, err := p.renewIntegration(r.PathValue("integration"), holderOf(r))
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "POST "+base, func(p *project, r *http.Request) outcome {
		var input api.IntegrationCommand
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		return p.integrationAction(scopeOf(r), r.PathValue("integration"), input, holderOf(r))
	})
}

func (p *project) integrationExecution(id, execution string) (api.Integration, error) {
	if !api.ValidIdentity(execution) {
		return api.Integration{}, apiError(api.CodeInvalidRequest, "invalid integration execution")
	}
	current, found, err := p.readIntegration(id)
	if err != nil {
		return api.Integration{}, err
	}
	if found && current.Value.Request.ExecutionID == execution {
		return p.integrationView(id)
	}
	var value api.Integration
	found, err = readJSON(p.root, servicePath("integrations", id, "executions", execution+".json"), &value)
	if err != nil {
		return value, err
	}
	if !found {
		return value, apiError(api.CodeNotFound, "integration execution has no published record")
	}
	return p.integrationShown(value)
}
