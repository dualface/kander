package review

import (
	"github.com/dualface/kander/internal/board"
)

// reviewStore keeps the execution engine independent of board location. Remote
// stores retain only private runtime output on the execution host; all formal
// inputs, originals and producer records belong to the service.
type reviewStore interface {
	Root() string
	Lock(string) (func() error, error)
	Lookup(string) (board.ReviewRun, bool, error)
	Run(string) (board.ReviewRun, error)
	Batch(string) (board.ReviewBatch, error)
	Snapshot(string) (board.Snapshot, error)
	Prepare(board.ReviewInput, map[string]string, *board.ReviewAdvance, map[string][]byte, string) (board.ReviewRun, bool, error)
	Staging(string) (string, error)
	Update(board.ReviewRun) error
	Artifact(string, string, []byte) error
	Finalize(board.ReviewRun, []byte) (board.ReviewRun, error)
	Publish(string) (board.ReviewRun, map[string]error, error)
	Original(string, string) ([]byte, error)
	Input(string, string) ([]byte, error)
	Incremental(string) ([]byte, error)
	Remaining([]string) ([]string, error)
	Complete(string) error
	NeedsInterpretation(string) (bool, error)
}

type localReviewStore struct{ root string }

func (s localReviewStore) Root() string { return s.root }
func (s localReviewStore) Lock(id string) (func() error, error) {
	return board.LockReviewRun(s.root, id)
}
func (s localReviewStore) Lookup(id string) (board.ReviewRun, bool, error) {
	return board.LookupReviewRun(s.root, id)
}
func (s localReviewStore) Run(id string) (board.ReviewRun, error) {
	return board.ReadReviewRun(s.root, id)
}
func (s localReviewStore) Batch(id string) (board.ReviewBatch, error) {
	return board.ReadReviewBatch(s.root, id)
}
func (s localReviewStore) Snapshot(id string) (board.Snapshot, error) {
	return board.ReadSnapshot(s.root, id)
}
func (s localReviewStore) Prepare(input board.ReviewInput, requirements map[string]string, advance *board.ReviewAdvance, files map[string][]byte, version string) (board.ReviewRun, bool, error) {
	return board.PrepareReviewRun(s.root, input, requirements, advance, files, version)
}
func (s localReviewStore) Staging(id string) (string, error) { return board.ReviewStaging(s.root, id) }
func (s localReviewStore) Update(run board.ReviewRun) error {
	return board.UpdateReviewRun(s.root, run)
}
func (s localReviewStore) Artifact(id, name string, data []byte) error {
	return board.StoreReviewArtifact(s.root, id, name, data)
}
func (s localReviewStore) Finalize(run board.ReviewRun, report []byte) (board.ReviewRun, error) {
	return board.FinalizeReviewRun(s.root, run, report)
}
func (s localReviewStore) Publish(id string) (board.ReviewRun, map[string]error, error) {
	return board.PublishReviewRun(s.root, id)
}
func (s localReviewStore) Original(id, name string) ([]byte, error) {
	return board.ReadReviewOriginal(s.root, id, name)
}
func (s localReviewStore) Input(id, name string) ([]byte, error) {
	return board.ReadReviewInput(s.root, id, name)
}
func (s localReviewStore) Incremental(id string) ([]byte, error) {
	return board.ReviewIncrementalContext(s.root, id)
}
func (s localReviewStore) Remaining(ids []string) ([]string, error) {
	return board.ReviewRemainingMembers(s.root, ids)
}
func (s localReviewStore) Complete(id string) error {
	return board.ReviewPublicationComplete(s.root, id)
}
func (s localReviewStore) NeedsInterpretation(id string) (bool, error) {
	return board.ReviewNeedsInterpretation(s.root, id)
}

func selectedReviewStore(root string, stores []reviewStore) reviewStore {
	if len(stores) > 0 && stores[0] != nil {
		return stores[0]
	}
	return localReviewStore{root: root}
}
