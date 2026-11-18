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

// publicationHistory keys the replay record by replay and the logical
// material. replay is the operation name for every operation except aggregate,
// so existing records keep their names.
func (s *Session) publicationHistory(operation, replay string, raw json.RawMessage, add string) (ids []string, err error) {
	logical, err := verificationIdentity(operation, raw)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(struct {
		Operation string
		Material  json.RawMessage
	}{replay, logical})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	root, err := s.LocalDataRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "review-operations")
	if err := fs.EnsurePrivateDirectory(s.Home, dir, true); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
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

// completedReviewPublication recognizes a prior successful operation before
// looking at versions changed by that very publication or by a later executor.
// It returns only an immutable receipt, with the publication it names; it
// grants no new write authority. A nil proof means there is none.
func (s *Session) completedReviewPublication(ctx context.Context, operation, replay string, raw json.RawMessage) (*client.Claim, api.Publication, error) {
	ids, err := s.publicationHistory(operation, replay, raw, "")
	if err != nil {
		return nil, api.Publication{}, err
	}
	logical, err := verificationIdentity(operation, raw)
	if err != nil {
		return nil, api.Publication{}, err
	}
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
		material, err := verificationIdentity(operation, publication.Payload)
		if err != nil || !bytes.Equal(material, logical) {
			return nil, api.Publication{}, errors.New(config.Text("remote.review_material_conflict"))
		}
		if publication.Status == "published" {
			return &client.Claim{PublicationID: id, PublicationPayload: publication.Payload}, publication, nil
		}
	}
	return nil, api.Publication{}, nil
}
