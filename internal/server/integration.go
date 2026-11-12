package server

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
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
	value := record.Value
	if value.Status == api.ClaimActive && !p.server.now().Before(value.ExpiresAt) {
		value.Status = api.ClaimLost
	}
	return value, nil
}
func (p *project) requireIntegrationOwner(id string, h holder) (integrationRecord, error) {
	record, found, err := p.readIntegration(id)
	if err != nil {
		return record, err
	}
	value := record.Value
	if h.invalid != nil {
		return record, h.invalid
	}
	if !found || value.Status != api.ClaimActive || value.Generation != h.generation || !p.server.now().Before(value.ExpiresAt) {
		return record, apiError(api.CodeLeaseInvalid, "integration lease is no longer current")
	}
	if h.clientID != value.ClientID || h.token == "" || subtle.ConstantTimeCompare([]byte(tokenHash(h.token)), []byte(record.Hash)) != 1 {
		return record, apiError(api.CodeHeldByOther, "target is held by another integration execution")
	}
	return record, nil
}

func (p *project) requireIntegration(id string, h holder) (integrationRecord, error) {
	record, err := p.requireIntegrationOwner(id, h)
	if err != nil {
		return record, err
	}
	if err := p.currentIntegrationActor(record.Value); err != nil {
		return record, err
	}
	return record, nil
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
func (p *project) claimIntegration(key, id string, input api.IntegrationRequest, h holder) outcome {
	scope, err := integrationScope(input)
	if err != nil {
		return outcome{err: err}
	}
	if id != api.IntegrationID(input.TargetRef) || !api.ValidIdentity(h.clientID) || p.info.Remote == "" {
		return outcome{err: apiError(api.CodeInvalidRequest, "integration must bind a registered common remote and target")}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	operation := "integration-claim:" + id
	out := p.idempotent(key, operation, input, func() outcome {
		previous, found, err := p.readIntegration(id)
		if err != nil {
			return outcome{err: err}
		}
		if found && previous.Value.Status == api.ClaimActive && p.server.now().Before(previous.Value.ExpiresAt) {
			return outcome{err: apiError(api.CodeLeaseActive, "target already has an active integration lease")}
		}
		if input.ExpectedGeneration != previous.Value.Generation || input.ExpectedRevision != previous.Value.Revision {
			return outcome{err: apiError(api.CodeRevisionConflict, "integration target changed since inspection")}
		}
		if previous.Value.Generation == ^uint64(0) || previous.Value.Revision == ^uint64(0) || found && previous.Value.Request.ExecutionID == input.ExecutionID {
			return outcome{err: apiError(api.CodeLeaseInvalid, "integration requires a fresh execution identity and generation")}
		}
		actor, err := p.integrationActor(input, h)
		if err != nil {
			return outcome{err: err}
		}
		if actor.ClientID != h.clientID || actor.ExecutionID != input.ActorExecutionID {
			return outcome{err: apiError(api.CodeHeldByOther, "integration actor differs from the presented task or coordinator execution")}
		}
		now := p.server.now()
		next := integrationRecord{Value: api.Integration{SourceRef: api.IntegrationSourceRef(p.info.ID, input.ExecutionID, input.Commit), ID: id, Revision: previous.Value.Revision + 1, Generation: previous.Value.Generation + 1, ClientID: h.clientID, Status: api.ClaimActive, Phase: "reserved", Request: input, AuthorityGeneration: actor.Generation, RenewedAt: now, ExpiresAt: now.Add(p.server.ttl)}}
		next.Hash = tokenHash(p.server.token(p.info.ID, "integration:"+id, input.ExecutionID, next.Value.Generation))
		guard := func(tx *board.Transaction) error {
			if !p.validIntegrationObservation(id, input.ExpectedRevision, input.ExpectedGeneration, input.ObservationTicket) {
				return apiError(api.CodeEvidenceIncomplete, "integration target observation expired or changed")
			}
			if _, err := p.integrationActor(input, h); err != nil {
				return err
			}
			return p.integrationFacts(tx, input, true)
		}
		grant := api.IntegrationGrant{Integration: next.Value, RenewInterval: api.DurationOf(p.server.renew), TTL: api.DurationOf(p.server.ttl)}
		err = board.WithTransaction(p.board, scope, func(tx *board.Transaction) error {
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
			return p.stageOperationReceipt(tx, key, operation, input, grant)
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
	p.mu.Lock()
	defer p.mu.Unlock()
	record, err := p.requireIntegration(id, h)
	if err != nil {
		return api.Renewal{}, err
	}
	scope, err := integrationScope(record.Value.Request)
	if err != nil {
		return api.Renewal{}, err
	}
	err = board.WithTransaction(p.board, scope, func(tx *board.Transaction) error {
		if _, err := p.requireIntegration(id, h); err != nil {
			return err
		}
		if err := p.integrationFacts(tx, record.Value.Request, false); err != nil {
			return err
		}
		record.Value.RenewedAt = p.server.now()
		record.Value.ExpiresAt = record.Value.RenewedAt.Add(p.server.ttl)
		return stageJSON(tx, integrationName(id), record)
	})
	return api.Renewal{Claim: api.ClaimView{ClientID: record.Value.ClientID, ExecutionID: record.Value.Request.ExecutionID, Owner: "integration", Generation: record.Value.Generation, Status: record.Value.Status, RenewedAt: record.Value.RenewedAt, ExpiresAt: record.Value.ExpiresAt}}, err
}
func (p *project) integrationAction(key, id string, input api.IntegrationCommand, h holder) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	operation := "integration-action:" + id
	return p.idempotent(key, operation, input, func() outcome {
		require := p.requireIntegration
		if input.Action == "release" {
			require = p.requireIntegrationOwner
		}
		record, err := require(id, h)
		if err != nil {
			return outcome{err: err}
		}
		if input.ExpectedRevision == 0 || input.ExpectedRevision != record.Value.Revision {
			return outcome{err: apiError(api.CodeRevisionConflict, "integration requires its inspected resource version")}
		}
		if record.Value.Revision == ^uint64(0) {
			return outcome{err: apiError(api.CodeRevisionConflict, "integration revision exhausted")}
		}
		scope, err := integrationScope(record.Value.Request)
		if err != nil {
			return outcome{err: err}
		}
		next := record
		next.Value.Revision++
		next.Value.ObservedTarget, next.Value.ObservedAt = input.ObservedTarget, p.server.now()
		switch input.Action {
		case "pushing":
			if record.Value.Request.AlreadyContained || record.Value.Phase != "reserved" || input.ObservedTarget != record.Value.Request.ExpectedTarget {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "push intent must bind the unchanged inspected remote target")}
			}
			next.Value.Phase = "pushing"
		case "confirm":
			reconciles := record.Value.Phase == "reserved" && record.Value.Request.AlreadyContained
			if (!reconciles && record.Value.Phase != "pushing") || !api.CommitSHA(input.ObservedTarget) || input.ObservedAt.IsZero() {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "completion requires a push intent and fresh exact remote facts")}
			}
			required := []api.IntegrationEdge{{Ancestor: record.Value.Request.Commit, Descendant: input.ObservedTarget}}
			if !reflect.DeepEqual(normalizedIntegrationEdges(required), normalizedIntegrationEdges(input.Edges)) {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "completion must prove the integrated source is in the observed remote target")}
			}
			next.Value.Phase, next.Value.Status, next.Value.ExpiresAt = "integrated", api.ClaimReleased, p.server.now()
		case "release":
			if strings.TrimSpace(input.Decision) == "" || (input.ObservedTarget != "" && !api.CommitSHA(input.ObservedTarget)) {
				return outcome{err: apiError(api.CodeEvidenceIncomplete, "release requires a decision and inspected remote state")}
			}
			next.Value.Phase = "released"
			if record.Value.Phase == "pushing" {
				next.Value.Phase = "uncertain"
			}
			next.Value.Status, next.Value.ExpiresAt = api.ClaimReleased, p.server.now()
		default:
			return outcome{err: apiError(api.CodeInvalidRequest, "unknown integration action")}
		}
		guard := func(tx *board.Transaction) error {
			if _, err := require(id, h); err != nil {
				return err
			}
			if !p.validIntegrationObservation(id, input.ExpectedRevision, record.Value.Generation, input.ObservationTicket) {
				return apiError(api.CodeEvidenceIncomplete, "integration remote observation is stale")
			}
			if input.Action == "release" {
				return nil
			}
			return p.integrationFacts(tx, record.Value.Request, false)
		}
		err = board.WithTransaction(p.board, scope, func(tx *board.Transaction) error {
			if err := guard(tx); err != nil {
				return err
			}
			if err := p.stageIntegration(tx, &record, next); err != nil {
				return err
			}
			return p.stageOperationReceipt(tx, key, operation, input, next.Value)
		}, board.TransactionExtension{After: guard})
		if err == nil {
			p.integrationChanged(next.Value)
		}
		return result(http.StatusOK, next.Value, err)
	}, func() (outcome, bool) { return outcome{}, false })
}
func (s *Server) integrationRoutes(mux *http.ServeMux) {
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
		return p.claimIntegration(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("integration"), input, holderOf(r))
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
		return p.integrationAction(r.Header.Get(api.HeaderIdempotencyKey), r.PathValue("integration"), input, holderOf(r))
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
	if value.Status == api.ClaimActive && !p.server.now().Before(value.ExpiresAt) {
		value.Status = api.ClaimLost
	}
	return value, nil
}
