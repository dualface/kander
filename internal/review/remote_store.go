package review

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/remote"
)

type remoteReviewStore struct {
	session *remote.Session
	root    string
	writer  string
}

func newRemoteReviewStore(session *remote.Session) (*remoteReviewStore, error) {
	root, err := session.LocalDataRoot()
	if err != nil {
		return nil, err
	}
	root = filepath.Join(root, "review-runtime")
	if err := fs.EnsurePrivateDirectory(session.Home, root, true); err != nil {
		return nil, err
	}
	return &remoteReviewStore{session: session, root: root}, nil
}
func (s *remoteReviewStore) Root() string { return s.root }
func (s *remoteReviewStore) Lock(id string) (func() error, error) {
	if !board.ValidReviewID(id) {
		return nil, archiveError("run identity")
	}
	dir := filepath.Join(s.root, id)
	if err := fs.EnsurePrivateDirectory(s.root, dir, true); err != nil {
		return nil, err
	}
	file, err := fs.OpenLockFile(s.root, filepath.Join(dir, "runtime.lock"))
	if err != nil {
		return nil, err
	}
	lock, err := fs.LockExclusive(file)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	unlock := func() error { return errors.Join(lock.Unlock(), file.Close()) }
	secretPath := filepath.Join(dir, "writer-secret")
	data, found, err := fs.ReadRegularFileIfExists(s.root, secretPath)
	if err == nil && !found {
		secret := make([]byte, 32)
		if _, err = rand.Read(secret); err == nil {
			data = []byte(hex.EncodeToString(secret))
			err = fs.WriteTextAtomic(s.root, secretPath, string(data), false)
		}
	}
	if err == nil && len(data) != 64 {
		err = archiveError("damaged private review writer")
	}
	if err != nil {
		return nil, errors.Join(err, unlock())
	}
	s.writer = string(data)
	return unlock, nil
}
func (s *remoteReviewStore) Lookup(id string) (board.ReviewRun, bool, error) {
	run, err := s.Run(id)
	if api.IsCode(err, api.CodeNotFound) {
		return run, false, nil
	}
	return run, err == nil, err
}
func (s *remoteReviewStore) Run(id string) (board.ReviewRun, error) {
	var value board.ReviewRun
	raw, err := s.session.Project.ReviewResource(context.Background(), "runs", id)
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	return value, err
}
func (s *remoteReviewStore) Batch(id string) (board.ReviewBatch, error) {
	var value board.ReviewBatch
	raw, err := s.session.Project.ReviewResource(context.Background(), "batches", id)
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	return value, err
}
func (s *remoteReviewStore) Snapshot(id string) (board.Snapshot, error) {
	value, err := s.session.Project.Show(context.Background(), id)
	return board.Snapshot{Entry: board.Entry{TaskID: id, State: value.State, Kind: board.MetadataFrom(value.Text, board.FieldSize)}, Text: value.Text, Revision: value.Revision, OperationID: value.OperationID}, err
}
func (s *remoteReviewStore) Prepare(input board.ReviewInput, requirements map[string]string, advance *board.ReviewAdvance, originals map[string][]byte, version string) (board.ReviewRun, bool, error) {
	input.CWD = "project:" + s.session.Info.ID
	request := api.ReviewPrepare{Input: input, Requirements: requirements, Advance: advance, Originals: originals, Version: version, WriterHash: board.ReviewDigest([]byte(s.writer))}
	ids := input.TaskIDs
	if batch, err := s.Batch(input.BatchID); err == nil && batch.PlanID != "" {
		raw, err := s.session.Project.ReviewResource(context.Background(), "plans", batch.PlanID)
		if err != nil {
			return board.ReviewRun{}, false, err
		}
		var plan board.ReviewPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return board.ReviewRun{}, false, err
		}
		ids = plan.TaskIDs
	}
	// The run may exist from a client of another version, which keyed its joint
	// publication by that version.
	var recorded string
	if existing, found, err := s.Lookup(input.RunID); err != nil {
		return board.ReviewRun{}, false, err
	} else if found {
		recorded = existing.KanderVersion
	}
	proof, err := s.session.PrepareReviewAuthority(context.Background(), request, ids, recorded)
	if err != nil {
		return board.ReviewRun{}, false, awaitingRun(input.RunID, err)
	}
	if proof == nil {
		proof = &client.Claim{}
	}
	proof.ReviewWriter = s.writer
	raw, err := s.session.Project.ReviewRuntime(context.Background(), client.NewKey(), input.RunID, "prepare", request, proof)
	var result board.ReviewPreparation
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result.Run, result.Fresh, err
}

// awaitingRun names the run in a wait for holder confirmation. A review that
// is started again without its run ID makes a new run with new material, which
// the holders must confirm again; only the same ID resumes this one.
func awaitingRun(run string, err error) error {
	var pending *remote.ConfirmationPending
	if !errors.As(err, &pending) {
		return err
	}
	return &api.Error{Code: pending.Cause.Code, Message: config.Text("remote.review_run_confirmation_pending", run, pending.Cause.Message)}
}
func (s *remoteReviewStore) Staging(id string) (string, error) {
	if !board.ValidReviewID(id) {
		return "", archiveError("run identity")
	}
	dir := filepath.Join(s.root, id, "staging")
	if err := fs.EnsurePrivateDirectory(s.root, dir, true); err != nil {
		return "", err
	}
	for _, name := range []string{"output.raw", "stdout.log", "error.log"} {
		path := filepath.Join(dir, name)
		if _, exists, err := fs.ReadRegularFileIfExists(s.root, path); err != nil {
			return "", err
		} else if !exists {
			if err := fs.WriteTextAtomic(s.root, path, "", false); err != nil {
				return "", err
			}
		}
	}
	return dir, nil
}
func (s *remoteReviewStore) runtime(run, action string, payload any, out any) error {
	raw, err := s.session.Project.ReviewRuntime(context.Background(), client.NewKey(), run, action, payload, &client.Claim{ReviewWriter: s.writer})
	if err == nil && out != nil {
		err = json.Unmarshal(raw, out)
	}
	return err
}
func (s *remoteReviewStore) Update(run board.ReviewRun) error {
	return s.runtime(run.RunID, "update", run, nil)
}

const artifactRequestLimit = 32 << 20 // Mirrors internal/server maxBody.

type artifactTooLargeError struct {
	name                          string
	rawBytes, requestBytes, limit int
}

func (e *artifactTooLargeError) Error() string {
	return fmt.Sprintf("%s: %d raw bytes, %d request bytes exceed the %d-byte limit",
		e.name, e.rawBytes, e.requestBytes, e.limit)
}

func (s *remoteReviewStore) Artifact(id, name string, data []byte) error {
	payload := api.ReviewArtifact{Name: name, Data: data, Digest: board.ReviewDigest(data)}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(encoded) > artifactRequestLimit {
		return &artifactTooLargeError{name: name, rawBytes: len(data), requestBytes: len(encoded), limit: artifactRequestLimit}
	}
	// Pass the already encoded request through the client without encoding
	// the artifact's base64 data a second time.
	return s.runtime(id, "artifact", json.RawMessage(encoded), nil)
}
func (s *remoteReviewStore) Finalize(run board.ReviewRun, report []byte) (board.ReviewRun, error) {
	dir, err := s.Staging(run.RunID)
	if err != nil {
		return run, err
	}
	request := api.ReviewFinalize{Run: run, Report: report}
	pendingPath := filepath.Join(s.root, run.RunID, "pending-finalization.json")
	if report == nil && run.ExecutionStatus == "interrupted" {
		data, found, err := fs.ReadRegularFileIfExists(s.root, pendingPath)
		if err != nil {
			return run, err
		}
		if found {
			if err := json.Unmarshal(data, &request); err != nil {
				return run, err
			}
			if !reflect.DeepEqual(request.Run.ReviewInput, run.ReviewInput) {
				return run, archiveError("pending finalization identity conflict")
			}
			err = s.runtime(run.RunID, "finalize", request, &run)
			return run, err
		}
	}

	for _, name := range []string{"output.raw", "stdout.log", "error.log", board.ReviewProcessEvidence} {
		data, found, err := fs.ReadRegularFileIfExists(s.root, filepath.Join(dir, name))
		if err != nil {
			return run, err
		}
		if found {
			request.Files = append(request.Files, api.ReviewArtifact{Name: name, Data: data, Digest: board.ReviewDigest(data)})
		}
	}
	// Retain exact pending material before transport. It remains local evidence
	// on lease loss; a retry never silently reruns the reviewer process.
	data, err := json.Marshal(request)
	if err != nil {
		return run, err
	}
	if err := fs.WriteTextAtomic(s.root, pendingPath, string(data), true); err != nil {
		return run, err
	}
	err = s.runtime(run.RunID, "finalize", request, &run)
	return run, err
}
func (s *remoteReviewStore) Publish(id string) (board.ReviewRun, map[string]error, error) {
	run, err := s.Run(id)
	if err != nil {
		return run, nil, err
	}
	proof, err := s.session.ReviewAuthority(context.Background(), "review-publish:"+id, id, run.TaskIDs)
	if err != nil {
		return run, nil, awaitingRun(id, err)
	}
	if proof == nil {
		proof = &client.Claim{}
	}
	proof.ReviewWriter = s.writer
	raw, err := s.session.Project.ReviewRuntime(context.Background(), client.NewKey(), id, "publish", map[string]string{}, proof)
	if err == nil {
		err = json.Unmarshal(raw, &run)
	}
	return run, nil, err
}
func (s *remoteReviewStore) evidence(id, kind, name string) ([]byte, error) {
	raw, err := s.session.Project.ReviewEvidence(context.Background(), id, kind, name)
	var data []byte
	if err == nil {
		err = json.Unmarshal(raw, &data)
	}
	return data, err
}
func (s *remoteReviewStore) Original(id, name string) ([]byte, error) {
	return s.evidence(id, "original", name)
}
func (s *remoteReviewStore) Input(id, name string) ([]byte, error) {
	return s.evidence(id, "input", name)
}
func (s *remoteReviewStore) Incremental(id string) ([]byte, error) {
	return s.evidence(id, "incremental", "")
}
func (s *remoteReviewStore) Remaining(ids []string) ([]string, error) {
	return s.session.Project.ReviewRemaining(context.Background(), ids)
}
func (s *remoteReviewStore) Complete(id string) error                    { _, err := s.status(id); return err }
func (s *remoteReviewStore) NeedsInterpretation(id string) (bool, error) { return s.status(id) }
func (s *remoteReviewStore) status(id string) (bool, error) {
	raw, err := s.session.Project.ReviewEvidence(context.Background(), id, "status", "")
	var value struct {
		NeedsInterpretation bool `json:"needs_interpretation"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	return value.NeedsInterpretation, err
}
func (s *remoteReviewStore) Compacted(id string) (bool, error) {
	raw, err := s.session.Project.ReviewEvidence(context.Background(), id, "compaction", "")
	var value api.ReviewCompaction
	if err == nil {
		err = json.Unmarshal(raw, &value)
	}
	return value.Compacted, err
}

var _ reviewStore = (*remoteReviewStore)(nil)
