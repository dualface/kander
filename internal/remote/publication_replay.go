package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// historyPath is the replay record of one operation and its logical material.
// replay is the operation name for every operation except aggregate, so
// existing records keep their names.
func (s *Session) historyPath(replay string, logical json.RawMessage) (string, error) {
	data, err := json.Marshal(struct {
		Operation string
		Material  json.RawMessage
	}{replay, logical})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	root, err := s.LocalDataRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "review-operations")
	if err := fs.EnsurePrivateDirectory(s.Home, dir, true); err != nil {
		return "", err
	}
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), nil
}

// publicationHistory keys the replay record by replay and the replay identity
// of the material.
func (s *Session) publicationHistory(operation, replay string, raw json.RawMessage, add string) (ids []string, err error) {
	logical, err := replayIdentity(operation, raw)
	if err != nil {
		return nil, err
	}
	path, err := s.historyPath(replay, logical)
	if err != nil {
		return nil, err
	}
	file, err := fs.OpenLockFile(s.Home, path+".lock")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		return nil, err
	}
	defer lock.Unlock()
	previous, found, err := fs.ReadRegularFileIfExists(s.Home, path)
	if err != nil {
		return nil, err
	}
	if found && (json.Unmarshal(previous, &ids) != nil || len(ids) > 256) {
		return nil, errors.New(config.Text("remote.review_material_conflict"))
	}
	if add != "" && !slices.Contains(ids, add) {
		if len(ids) >= 256 {
			return nil, errors.New(config.Text("remote.review_material_conflict"))
		}
		ids = append(ids, add)
		data, err := json.Marshal(ids)
		if err != nil {
			return nil, err
		}
		if err := fs.WriteTextAtomic(s.Home, path, string(data), true); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// legacyPrepareHistory reads the replay record an earlier client keyed by the
// whole prepare material, version included. recorded is the version the run
// was created with, which is the version that client wrote into the key.
func (s *Session) legacyPrepareHistory(replay string, raw json.RawMessage, recorded string) ([]string, error) {
	legacy, err := withVersion(raw, recorded)
	if err != nil {
		return nil, err
	}
	logical, err := verificationIdentity("review-prepare", legacy)
	if err != nil {
		return nil, err
	}
	path, err := s.historyPath(replay, logical)
	if err != nil {
		return nil, err
	}
	previous, found, err := fs.ReadRegularFileIfExists(s.Home, path)
	if err != nil || !found {
		return nil, err
	}
	var ids []string
	if json.Unmarshal(previous, &ids) != nil || len(ids) > 256 {
		return nil, errors.New(config.Text("remote.review_material_conflict"))
	}
	return ids, nil
}

// completedReviewPublication recognizes a prior successful operation before
// looking at versions changed by that very publication or by a later executor.
// It returns only an immutable receipt, with the publication it names; it
// grants no new write authority. A nil proof means there is none. recorded is
// the version a prepared run was created with, and lets a prepare find the
// record of a client that keyed it by version; it is empty for other calls.
func (s *Session) completedReviewPublication(ctx context.Context, operation, replay string, raw json.RawMessage, recorded string) (*client.Claim, api.Publication, error) {
	logical, err := replayIdentity(operation, raw)
	if err != nil {
		return nil, api.Publication{}, err
	}
	ids, err := s.publicationHistory(operation, replay, raw, "")
	if err != nil {
		return nil, api.Publication{}, err
	}
	proof, publication, err := s.publishedAmong(ctx, operation, ids, logical)
	if err != nil || proof != nil || recorded == "" || operation != "review-prepare" {
		return proof, publication, err
	}
	ids, err = s.legacyPrepareHistory(replay, raw, recorded)
	if err != nil {
		return nil, api.Publication{}, err
	}
	proof, publication, err = s.publishedAmong(ctx, operation, ids, logical)
	if err != nil || proof == nil {
		return nil, api.Publication{}, err
	}
	// Later retries find it under the current key without this lookup.
	if _, err := s.publicationHistory(operation, replay, raw, proof.PublicationID); err != nil {
		return nil, api.Publication{}, err
	}
	return proof, publication, nil
}

// publishedAmong returns the newest published publication of ids. Every
// recorded publication must carry the replay identity of logical.
func (s *Session) publishedAmong(ctx context.Context, operation string, ids []string, logical json.RawMessage) (*client.Claim, api.Publication, error) {
	for _, id := range slices.Backward(ids) {
		publication, err := s.Project.Publication(ctx, id)
		if api.IsCode(err, api.CodeNotFound) {
			continue
		}
		if err != nil {
			return nil, api.Publication{}, err
		}
		if publication.Operation != operation {
			return nil, api.Publication{}, errors.New(config.Text("remote.review_material_conflict"))
		}
		material, err := replayIdentity(operation, publication.Payload)
		if err != nil || !bytes.Equal(material, logical) {
			return nil, api.Publication{}, errors.New(config.Text("remote.review_material_conflict"))
		}
		if publication.Status == "published" {
			return &client.Claim{PublicationID: id, PublicationPayload: publication.Payload}, publication, nil
		}
	}
	return nil, api.Publication{}, nil
}
