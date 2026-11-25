package remote

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// verificationIdentity ignores only the verification time that is regenerated
// after a client rechecks identical immutable Git facts. The first actual
// verified payload stays frozen, and card revisions/generations stay in the key.
func verificationIdentity(operation string, raw json.RawMessage) (json.RawMessage, error) {
	return materialIdentity(operation, raw, false)
}

// replayIdentity is the material a replay record is keyed and compared by. A
// prepare also ignores the version of the client binary that wrote it: the
// version is a provenance record, so the same run prepared again by an upgraded
// binary finds the publication the first binary completed. Frozen material and
// publication IDs keep the version, so a new run never reuses another's.
func replayIdentity(operation string, raw json.RawMessage) (json.RawMessage, error) {
	return materialIdentity(operation, raw, operation == "review-prepare")
}

func materialIdentity(operation string, raw json.RawMessage, dropVersion bool) (json.RawMessage, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if object, ok := value.(map[string]any); ok {
		field := ""
		if strings.HasPrefix(operation, "review-close:") {
			field = "evidence"
		}
		if operation == "review-resolve-batch" {
			field = "git"
		}
		if nested, ok := object[field].(map[string]any); ok {
			delete(nested, "verified_at")
		}
		if dropVersion {
			delete(object, "version")
		}
	}
	return json.Marshal(value)
}

// withVersion returns a prepare payload as the client of that version wrote it.
func withVersion(raw json.RawMessage, version string) (json.RawMessage, error) {
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	object["version"] = version
	return json.Marshal(object)
}

func (s *Session) freezePublicationMaterial(operation string, raw json.RawMessage, members []api.PublicationMember) (json.RawMessage, error) {
	logical, err := verificationIdentity(operation, raw)
	if err != nil {
		return nil, err
	}
	identity, err := json.Marshal(struct {
		Operation string
		Payload   json.RawMessage
		Members   []api.PublicationMember
	}{operation, logical, members})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(identity)
	root, err := s.LocalDataRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "review-material")
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
	if found {
		old, err := verificationIdentity(operation, previous)
		if err != nil || !bytes.Equal(old, logical) {
			return nil, errors.New(config.Text("remote.review_material_conflict"))
		}
		return json.RawMessage(previous), nil
	}
	if err := fs.WriteTextAtomic(s.Home, path, string(raw), false); err != nil {
		return nil, err
	}
	return raw, nil
}
