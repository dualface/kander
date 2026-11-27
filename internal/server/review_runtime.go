package server

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

type reviewWriter struct {
	PrepareDigest string            `json:"prepare_digest"`
	RunID         string            `json:"run_id"`
	ClientID      string            `json:"client_id"`
	Hash          string            `json:"hash"`
	Generations   map[string]uint64 `json:"generations"`
}

func writerName(run string) string { return "review-writers/" + run + ".json" }
func (p *project) writer(run, client, secret string) (reviewWriter, error) {
	var writer reviewWriter
	found, err := readJSON(p.root, servicePath(writerName(run)), &writer)
	if err != nil {
		return writer, err
	}
	if !found || len(secret) != 64 || writer.ClientID != client || subtle.ConstantTimeCompare([]byte(writer.Hash), []byte(tokenHash(secret))) != 1 {
		return writer, apiError(api.CodeHeldByOther, "review upload requires its private writer credential")
	}
	return writer, nil
}
func (p *project) validateWriter(tx *board.Transaction, writer reviewWriter) error {
	remaining, err := board.ReviewRemainingMembersInTransaction(tx, slices.Sorted(maps.Keys(writer.Generations)))
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		return apiError(api.CodeLeaseInvalid, "review has no remaining members")
	}
	for _, id := range remaining {
		generation := writer.Generations[id]
		snapshot, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		claim, found, err := p.readClaim(id)
		if err != nil {
			return err
		}
		standing, err := p.standing(claim, found, generation)
		if err != nil {
			return err
		}
		if !claimedState(snapshot.Entry.State) || !standing {
			return apiError(api.CodeLeaseInvalid, "review writer belongs to a replaced execution")
		}
	}
	return nil
}

// prepareIdentityDigest recomputes the prepare digest of a request as if it
// carried the version the run was created with. The requesting client's version
// is a provenance record, not part of the run identity, so a same-ID retry from
// an upgraded client compares equal to the digest written at the first prepare,
// whichever version wrote that record. The run keeps its recorded version.
func prepareIdentityDigest(request api.ReviewPrepare, recordedVersion string) string {
	request.Version = recordedVersion
	return requestDigest("review-prepare", request)
}

func (s *Server) reviewRuntimeRoutes(mux *routeTable) {
	const base = "/api/v1/projects/{project}/reviews/runtime"
	s.projectRoute(mux, "POST "+base+"/prepare", func(p *project, r *http.Request) outcome {
		var request api.ReviewPrepare
		if err := decode(r, &request); err != nil {
			return outcome{err: err}
		}
		if err := holderOf(r).coordinatorConflict(); err != nil {
			return outcome{err: err}
		}
		secret, client := r.Header.Get(api.HeaderReviewWriter), r.Header.Get(api.HeaderClientID)
		hash, err := hex.DecodeString(request.WriterHash)
		if err != nil || len(hash) != 32 || len(secret) != 64 || request.WriterHash != tokenHash(secret) || !api.ValidIdentity(client) {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid private review writer binding")}
		}
		request.Input.CWD = "project:" + p.info.ID
		// The policy revision is checked, never part of the run identity: a
		// retry of the same run after a policy change still matches it.
		revision := request.PolicyRevision
		request.PolicyRevision = 0
		// A prepared identity is never a fresh launch on retry, including when its
		// initial joint-publication receipt still contains Fresh=true.
		if err := p.lockBoard(); err != nil {
			return outcome{err: err}
		}
		existing, found, lookupErr := board.LookupReviewRun(p.board, request.Input.RunID)
		if lookupErr == nil && found {
			writer, writerErr := p.writer(request.Input.RunID, client, secret)
			if writerErr != nil {
				p.mu.Unlock()
				return outcome{err: writerErr}
			}
			if writer.PrepareDigest != prepareIdentityDigest(request, existing.KanderVersion) {
				p.mu.Unlock()
				return outcome{err: apiError(api.CodeIdempotencyMismatch, "review run identity cannot be reused with different inputs")}
			}
			p.mu.Unlock()
			if err := board.RequireReviewRunNotAbandoned(existing); err != nil {
				return outcome{err: err}
			}
			return outcome{status: http.StatusOK, body: board.ReviewPreparation{Run: existing, Fresh: false}}
		}
		p.mu.Unlock()
		if lookupErr != nil {
			return outcome{err: lookupErr}
		}

		scope := func() ([]string, error) {
			batch, exists, err := board.LookupReviewRun(p.board, request.Input.RunID)
			if err != nil {
				return nil, err
			}
			if exists {
				return batch.TaskIDs, nil
			}
			// A planned advance synchronizes all plan copies.
			b, err := board.ReadReviewBatch(p.board, request.Input.BatchID)
			if err == nil && b.PlanID != "" {
				plan, err := board.ReadReviewPlan(p.board, b.PlanID)
				return plan.TaskIDs, err
			}
			return request.Input.TaskIDs, nil
		}
		// Only a run that creates its batch is checked against the policy.
		var attest board.BatchAttestations
		check := func() error {
			if _, exists, err := board.LookupReviewRun(p.board, request.Input.RunID); err != nil || exists {
				return err
			}
			if exists, err := p.reviewBatchExists(request.Input.BatchID); err != nil || exists {
				return err
			}
			var err error
			attest, err = p.checkNewReviewBatches(revision, []newReviewBatch{{id: request.Input.BatchID, taskIDs: request.Input.TaskIDs, requirements: request.Requirements, decisions: request.RequirementDecisions}})
			return err
		}
		return p.reviewWriteChecked(r.Header.Get(api.HeaderIdempotencyKey), "review-prepare", request, scope, holderOf(r), check, func(extension board.TransactionExtension) (any, error) {
			writer := reviewWriter{PrepareDigest: requestDigest("review-prepare", request), RunID: request.Input.RunID, ClientID: client, Hash: request.WriterHash, Generations: map[string]uint64{}}
			bind := board.TransactionExtension{Before: func(tx *board.Transaction) error {
				var old reviewWriter
				data, found, err := tx.ReadGroup(serviceGroup, writerName(writer.RunID))
				if err != nil {
					return err
				}
				if found {
					if err := json.Unmarshal(data, &old); err != nil {
						return err
					}
					if old.ClientID != client || old.Hash != writer.Hash {
						return apiError(api.CodeHeldByOther, "review run already has a writer; reuse never authorizes a new reviewer")
					}
					writer = old
					return p.validateWriter(tx, writer)
				}
				for _, id := range request.Input.TaskIDs {
					claim, found, err := p.readClaim(id)
					if err != nil {
						return err
					}
					standing, err := p.hasStanding(claim, found)
					if err != nil {
						return err
					}
					if !standing {
						return apiError(api.CodeLeaseInvalid, "review input requires active executions")
					}
					writer.Generations[id] = claim.Generation
				}
				return stageJSON(tx, writerName(writer.RunID), writer)
			}}
			run, fresh, err := board.PrepareReviewRunWithTransaction(p.board, request.Input, request.Requirements, attest, request.Advance, request.Originals, request.Version, extension, bind)
			return board.ReviewPreparation{Run: run, Fresh: fresh}, err
		})
	})
	s.projectRoute(mux, "POST "+base+"/{run}/{action}", func(p *project, r *http.Request) outcome {
		id, action := r.PathValue("run"), r.PathValue("action")
		if !board.ValidReviewID(id) {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid review identity")}
		}
		// Decode before entering a writer transaction, retaining the exact body in
		// the idempotency digest without accepting arbitrary board paths.
		var payload json.RawMessage
		if err := decode(r, &payload); err != nil {
			return outcome{err: err}
		}
		if action == "publish" {
			return p.reviewWrite(r.Header.Get(api.HeaderIdempotencyKey), "review-publish:"+id, id, p.runTasks(id), holderOf(r), func(extension board.TransactionExtension) (any, error) {
				writer, err := p.writer(id, r.Header.Get(api.HeaderClientID), r.Header.Get(api.HeaderReviewWriter))
				if err != nil {
					return nil, err
				}
				guard := board.TransactionExtension{Before: func(tx *board.Transaction) error { return p.validateWriter(tx, writer) }}
				return board.PublishReviewRunAtomic(p.board, id, guard, extension)
			})
		}
		if err := p.lockBoard(); err != nil {
			return outcome{err: err}
		}
		defer p.unlockBoard()
		writer, err := p.writer(id, r.Header.Get(api.HeaderClientID), r.Header.Get(api.HeaderReviewWriter))
		if err != nil {
			return outcome{err: err}
		}
		key, operation := r.Header.Get(api.HeaderIdempotencyKey), "review-runtime:"+id+":"+action
		return p.idempotent(key, operation, payload, func() outcome {
			run, err := board.ReadReviewRun(p.board, id)
			if err != nil {
				return outcome{err: err}
			}
			extension := board.TransactionExtension{Groups: []string{serviceGroup}, Published: p.indexPublication, Tasks: run.TaskIDs, Before: func(tx *board.Transaction) error { return p.validateWriter(tx, writer) }}
			extension.After = func(tx *board.Transaction) error {
				if err := p.validateWriter(tx, writer); err != nil {
					return err
				}
				data, err := json.Marshal(tx.OperationResult())
				if err != nil {
					return err
				}
				return stageJSON(tx, filepath.Join("receipts", filepath.Base(receiptPath(key))), receipt{Key: key, Operation: operation, Digest: requestDigest(operation, payload), Status: receiptDone, HTTP: http.StatusOK, Body: data, CreatedAt: p.server.now()})
			}
			switch action {
			case "update":
				var incoming board.ReviewRun
				if err := json.Unmarshal(payload, &incoming); err != nil {
					return outcome{err: apiError(api.CodeInvalidRequest, "invalid review phase")}
				}
				if !reflect.DeepEqual(run.ReviewInput, incoming.ReviewInput) || !slices.Contains([]string{"prepared", "launching", "running", "collecting", "cleaning"}, incoming.Phase) || !slices.Contains([]string{"not_started", "unknown", "started"}, incoming.LaunchStatus) {
					return outcome{err: apiError(api.CodeInvalidRequest, "invalid review phase transition")}
				}
				run.Phase, run.LaunchStatus = incoming.Phase, incoming.LaunchStatus
				return result(http.StatusOK, run, board.UpdateReviewRunWithTransaction(p.board, run, extension))
			case "artifact":
				var request api.ReviewArtifact
				if err := json.Unmarshal(payload, &request); err != nil || board.ReviewDigest(request.Data) != request.Digest {
					return outcome{err: apiError(api.CodeEvidenceIncomplete, "review artifact digest mismatch")}
				}
				return result(http.StatusOK, map[string]string{"run_id": id}, board.StoreReviewArtifactWithTransaction(p.board, id, request.Name, request.Data, extension))
			case "finalize":
				var request api.ReviewFinalize
				if err := json.Unmarshal(payload, &request); err != nil {
					return outcome{err: apiError(api.CodeInvalidRequest, "invalid finalization")}
				}
				if !reflect.DeepEqual(run.ReviewInput, request.Run.ReviewInput) {
					return outcome{err: apiError(api.CodeInvalidRequest, "finalization input conflict")}
				}
				files := map[string][]byte{}
				for _, file := range request.Files {
					if _, duplicate := files[file.Name]; duplicate || board.ReviewDigest(file.Data) != file.Digest {
						return outcome{err: apiError(api.CodeEvidenceIncomplete, "duplicate or corrupt runtime upload")}
					}
					files[file.Name] = file.Data
				}
				for _, name := range []string{"output.raw", "stdout.log", "error.log"} {
					if _, ok := files[name]; !ok {
						return outcome{err: apiError(api.CodeEvidenceIncomplete, "missing runtime upload")}
					}
				}
				run.LaunchStatus, run.ExecutionStatus, run.ExitCode, run.FailureReason = request.Run.LaunchStatus, request.Run.ExecutionStatus, request.Run.ExitCode, request.Run.FailureReason
				if !slices.Contains([]string{"not_started", "started", "unknown"}, run.LaunchStatus) || !slices.Contains([]string{"ok", "failed", "not_started", "interrupted"}, run.ExecutionStatus) {
					return outcome{err: apiError(api.CodeInvalidRequest, "invalid execution facts")}
				}
				value, err := board.FinalizeUploadedReview(p.board, run, request.Report, files, extension)
				return result(http.StatusOK, value, err)
			default:
				return outcome{err: apiError(api.CodeInvalidRequest, "unsupported review runtime action")}
			}
		}, func() (outcome, bool) { return outcome{}, false })
	})
	s.reviewEvidenceRoutes(mux)
}
