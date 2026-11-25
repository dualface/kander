package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/issue"
)

type issueResultRecord struct {
	Revision uint64            `json:"revision"`
	Store    issue.ResultStore `json:"store"`
}

func issueResultName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "issue-results/" + hex.EncodeToString(sum[:]) + ".json"
}
func (p *project) issueResultInputs(query api.IssueResultQuery) (issue.Repository, int, issue.ResultWork, error) {
	index, err := issue.LoadIndex(p.board)
	if err != nil {
		return issue.Repository{}, 0, issue.ResultWork{}, err
	}
	anchor, found := index[query.SourceKey]
	if found && anchor.TaskID != query.TaskID {
		return issue.Repository{}, 0, issue.ResultWork{}, apiError(api.CodeInvalidRequest, "result query names a sibling; use --card "+anchor.TaskID)
	}
	if !found {
		return issue.Repository{}, 0, issue.ResultWork{}, apiError(api.CodeInvalidRequest, "result query must name the canonical imported source and anchor")
	}
	r := anchor.Repository
	repository := issue.Repository{Host: r.Host, Owner: r.Owner, Name: r.Name, URL: r.URL, Private: r.Private}
	work, err := issue.ReadResultWork(p.board, repository, anchor.Number, query.TaskID)
	return repository, anchor.Number, work, err
}
func (p *project) readIssueResult(ctx context.Context, query api.IssueResultQuery, repository issue.Repository, number int) (value issueResultRecord, err error) {
	found, err := readJSON(p.root, servicePath(issueResultName(query.SourceKey)), &value)
	if err != nil {
		return value, err
	}
	if !newIssueRevision(value.Revision).read(found) {
		return value, apiError(api.CodeEvidenceIncomplete, "central result record has no resource version")
	}
	if !found {
		value.Store, err = issue.ReadResultStore(ctx, p.board, repository, number, query.TaskID)
	}
	if err == nil {
		err = issue.ValidateResultStore(value.Store, query.SourceKey, query.TaskID)
	}
	return value, err
}
func issueResultState(value issueResultRecord, work issue.ResultWork) (api.IssueResultState, error) {
	rawWork, err := json.Marshal(work)
	if err != nil {
		return api.IssueResultState{}, err
	}
	rawStore, err := json.Marshal(value.Store)
	if err != nil {
		return api.IssueResultState{}, err
	}
	return api.IssueResultState{Revision: value.Revision, Work: rawWork, Store: rawStore}, nil
}
func (p *project) issueResultView(ctx context.Context, query api.IssueResultQuery) (api.IssueResultState, error) {
	if err := p.lockBoard(); err != nil {
		return api.IssueResultState{}, err
	}
	defer p.mu.Unlock()
	repository, number, work, err := p.issueResultInputs(query)
	if err != nil {
		return api.IssueResultState{}, err
	}
	value, err := p.readIssueResult(ctx, query, repository, number)
	if err != nil {
		return api.IssueResultState{}, err
	}
	return issueResultState(value, work)
}
func (p *project) saveIssueResult(ctx context.Context, key string, input api.IssueResultWrite) outcome {
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.mu.Unlock()
	operation := "issue-result-save:" + input.Query.SourceKey
	return p.idempotent(key, operation, input, func() outcome {
		repository, number, work, err := p.issueResultInputs(input.Query)
		if err != nil {
			return outcome{err: err}
		}
		// The issue result kernel decides the work digest, the revision compare-and-swap,
		// the room for the next revision, both store relations, and the transaction
		// guard; each check asks it with every other fact holding, in the order the
		// save has always made them.
		workCurrent := input.WorkDigest != "" && input.WorkDigest == work.Digest
		if _, ok := newIssueRevision(0).save(0, workCurrent, true, true); !ok {
			return outcome{err: apiError(api.CodeRevisionConflict, "issue result work changed since inspection")}
		}
		previous, err := p.readIssueResult(ctx, input.Query, repository, number)
		if err != nil {
			return outcome{err: err}
		}
		revision := newIssueRevision(previous.Revision)
		if _, ok := revision.save(input.ExpectedRevision, true, true, true); !ok {
			return outcome{err: apiError(api.CodeRevisionConflict, "issue result resource version changed")}
		}
		var store issue.ResultStore
		if err := board.DecodeReviewJSON(input.Store, &store); err != nil {
			return outcome{err: err}
		}
		relations := issue.ValidateResultStoreUpdate(previous.Store, store)
		if relations == nil {
			relations = issue.ValidateResultStoreWork(previous.Store, store, work)
		}
		nextRevision, ok := revision.save(input.ExpectedRevision, true, relations == nil, true)
		if !ok {
			return outcome{err: relations}
		}
		next := issueResultRecord{Revision: nextRevision, Store: store}
		resultState, err := issueResultState(next, work)
		if err != nil {
			return outcome{err: err}
		}
		ids := make([]string, 0, len(work.Cards)+len(work.Excluded))
		versions := map[string]uint64{}
		for _, card := range work.Cards {
			ids = append(ids, card.TaskID)
		}
		for _, card := range work.Excluded {
			ids = append(ids, card.TaskID)
		}
		ids = slices.Compact(slices.Sorted(slices.Values(ids)))
		for _, id := range ids {
			snapshot, err := board.ReadSnapshot(p.board, id)
			if err != nil {
				return outcome{err: err}
			}
			versions[id] = snapshot.Revision
		}
		held := func(tx *board.Transaction) error {
			reader := tx.ReadOnly()
			for _, id := range ids {
				if _, err := reader.Expect(id, "", versions[id]); err != nil {
					return err
				}
			}
			current, err := issue.ReadResultWorkInTransaction(tx, repository, number, input.Query.TaskID)
			if err != nil {
				return err
			}
			if current.Digest != work.Digest || !reflect.DeepEqual(current.Excluded, work.Excluded) {
				return apiError(api.CodeRevisionConflict, "issue result membership or evidence changed")
			}
			return nil
		}
		// The guard runs before the write and again after it; the kernel accepts the
		// save only while it holds.
		guard := func(tx *board.Transaction) error {
			err := held(tx)
			if _, ok := revision.save(input.ExpectedRevision, true, true, err == nil); !ok {
				return err
			}
			return nil
		}
		err = board.WithTransactionContext(ctx, p.board, board.LockScope{Tasks: ids, Groups: []string{serviceGroup}, ExclusiveBoard: true}, func(tx *board.Transaction) error {
			if err := guard(tx); err != nil {
				return err
			}
			if err := stageJSON(tx, issueResultName(input.Query.SourceKey), next); err != nil {
				return err
			}
			return p.stageOperationReceipt(tx, key, operation, input, resultState)
		}, board.TransactionExtension{After: guard})
		if err == nil {
			for _, id := range ids {
				p.changed(id, 0)
			}
		}
		return result(http.StatusOK, resultState, err)
	}, func() (outcome, bool) { return outcome{}, false })
}
func (s *Server) issueResultRoutes(mux *http.ServeMux) {
	const base = "/api/v1/projects/{project}/issues/results"
	s.projectRoute(mux, "POST "+base+"/view", func(p *project, r *http.Request) outcome {
		var input api.IssueResultQuery
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		value, err := p.issueResultView(r.Context(), input)
		return result(http.StatusOK, value, err)
	})
	s.projectRoute(mux, "POST "+base+"/save", func(p *project, r *http.Request) outcome {
		var input api.IssueResultWrite
		if err := decode(r, &input); err != nil {
			return outcome{err: err}
		}
		return p.saveIssueResult(r.Context(), r.Header.Get(api.HeaderIdempotencyKey), input)
	})
}
