package issue

import (
	"context"
	"encoding/json"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

type remoteResultKey struct{}
type remoteResultContext struct {
	session    *remote.Session
	projectDir string
}

// WithRemoteResult binds the existing result protocol to service data and
// private local launch evidence. It provides no local board backend.
func WithRemoteResult(ctx context.Context, session *remote.Session, projectDir string) context.Context {
	return context.WithValue(ctx, remoteResultKey{}, remoteResultContext{session, projectDir})
}
func remoteResultOf(ctx context.Context) (remoteResultContext, bool) {
	value, ok := ctx.Value(remoteResultKey{}).(remoteResultContext)
	return value, ok
}
func resultQuery(repository Repository, number int, cardID string) (api.IssueResultQuery, error) {
	key, err := repository.IssueSourceKey(number)
	return api.IssueResultQuery{SourceKey: key, TaskID: cardID}, err
}
func readResultWork(ctx context.Context, root string, repository Repository, number int, cardID string) (ResultWork, error) {
	backend, ok := remoteResultOf(ctx)
	if !ok {
		return ReadResultWork(root, repository, number, cardID)
	}
	query, err := resultQuery(repository, number, cardID)
	if err != nil {
		return ResultWork{}, err
	}
	state, err := backend.session.Project.IssueResult(ctx, query)
	if err != nil {
		return ResultWork{}, err
	}
	var work ResultWork
	if err := json.Unmarshal(state.Work, &work); err != nil {
		return work, err
	}
	if len(work.Cards) == 0 || work.Anchor().TaskID != cardID || work.Digest == "" {
		return work, resultError("invalid service result work")
	}
	return work, nil
}
func withRemoteResultStore(ctx context.Context, backend remoteResultContext, repository Repository, number int, cardID string, fn func(*resultStore, func() error) error) error {
	query, err := resultQuery(repository, number, cardID)
	if err != nil {
		return err
	}
	state, err := backend.session.Project.IssueResult(ctx, query)
	if err != nil {
		return err
	}
	var work ResultWork
	if err := json.Unmarshal(state.Work, &work); err != nil {
		return err
	}
	var store ResultStore
	if err := json.Unmarshal(state.Store, &store); err != nil {
		return err
	}
	if err := ValidateResultStore(store, query.SourceKey, cardID); err != nil {
		return err
	}
	save := func() error {
		raw, err := json.Marshal(store)
		if err != nil {
			return err
		}
		input := api.IssueResultWrite{Query: query, ExpectedRevision: state.Revision, WorkDigest: work.Digest, Store: raw}
		next, err := saveRemoteResult(ctx, backend.session, input)
		if err != nil {
			return err
		}
		state = next
		return nil
	}
	return fn(&store, save)
}

type resultSaveCall struct {
	Schema int                   `json:"schema"`
	Key    string                `json:"key"`
	Input  api.IssueResultWrite  `json:"input"`
	Result *api.IssueResultState `json:"result,omitempty"`
}

func saveRemoteResult(ctx context.Context, session *remote.Session, input api.IssueResultWrite) (api.IssueResultState, error) {
	journal, fresh, err := session.BeginAction("issue-result-save", resultHash(input))
	if err != nil {
		return api.IssueResultState{}, err
	}
	defer journal.Close()
	call := resultSaveCall{Schema: 1, Key: client.NewKey(), Input: input}
	if !fresh {
		if err := json.Unmarshal(journal.Value.Result, &call); err != nil {
			return api.IssueResultState{}, err
		}
		if call.Schema != 1 || resultHash(call.Input) != resultHash(input) {
			return api.IssueResultState{}, resultError("result save receipt differs from its input")
		}
		if call.Result != nil {
			return *call.Result, nil
		}
	}
	persist := func() error {
		data, err := json.Marshal(call)
		if err != nil {
			return err
		}
		journal.Value.Result = data
		journal.Value.Phase = "pending"
		if call.Result != nil {
			journal.Value.Phase = "complete"
		}
		return journal.Save()
	}
	if err := persist(); err != nil {
		return api.IssueResultState{}, err
	}
	value, err := session.Project.SaveIssueResult(ctx, call.Key, call.Input)
	if err != nil {
		return value, err
	}
	call.Result = &value
	return value, persist()
}
