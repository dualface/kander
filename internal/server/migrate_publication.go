package server

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/dualface/kander/v2/internal/fs"
)

// importPublication is control/import-pending.json. StagedDigest is the digest
// of the full staged board. Schema 1 stored EmptyDigest instead of TargetDigest
// and may omit StagedDigest if the staged board equals the source snapshot.
type importPublication struct {
	Schema       int          `json:"schema"`
	Record       importRecord `json:"record"`
	EmptyDigest  string       `json:"empty_digest,omitempty"`
	TargetDigest string       `json:"target_digest,omitempty"`
	StagedDigest string       `json:"staged_digest,omitempty"`
}

func (p importPublication) targetDigest() string {
	if p.Schema == 1 {
		return p.EmptyDigest
	}
	return p.TargetDigest
}

func validImportDigest(value string) bool {
	_, err := hex.DecodeString(value)
	return len(value) == 64 && strings.ToLower(value) == value && err == nil
}

func (p *project) readImportRecord(snapshot string) (importRecord, error) {
	var record importRecord
	found, err := readJSON(p.root, importRecordPath(snapshot), &record)
	if err != nil {
		return importRecord{}, err
	}
	if found {
		if record.Snapshot != snapshot {
			return importRecord{}, errors.New("import record does not match its snapshot")
		}
		return record, nil
	}
	found, err = readJSON(p.root, filepath.Join("control", "import.json"), &record)
	if !found || record.Snapshot != snapshot {
		return importRecord{}, err
	}
	return record, err
}

func importRecordPath(snapshot string) string {
	return filepath.Join("control", "imports", snapshot+".json")
}

func (p *project) importAlreadyRecorded(record importRecord) (bool, error) {
	var present importRecord
	found, err := readJSON(p.root, importRecordPath(record.Snapshot), &present)
	if err != nil || !found {
		return found, err
	}
	sameTime := record.ImportedAt.Equal(present.ImportedAt)
	present.ImportedAt = record.ImportedAt
	if !sameTime || !reflect.DeepEqual(record, present) {
		return true, errors.New("import record conflicts with the publication intent")
	}
	return true, nil
}

func (p importPublication) stagedDigest() string {
	if p.StagedDigest != "" {
		return p.StagedDigest
	}
	return p.Record.Snapshot
}

func (s *Server) importCheckpoint(step string) error {
	if s.importFault != nil {
		return s.importFault(step)
	}
	return nil
}
func importPathsOverlap(first, second string) bool {
	contains := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return contains(first, second) || contains(second, first)
}
func importDirectoryExists(root, path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || fs.IsReparsePoint(path) || fs.IsReparsePoint(root) {
		return false, errors.New("import publication directory is not a regular private directory")
	}
	return true, nil
}
func removeImportDirectory(root, path string) error {
	found, err := importDirectoryExists(root, path)
	if err != nil || !found {
		return err
	}
	return os.RemoveAll(path)
}
func removeImportValidationFiles(staging string, original []string, expected string) error {
	files, _, err := snapshotTree(staging)
	if err != nil {
		return err
	}
	retained := make(map[string]bool, len(original))
	for _, rel := range original {
		retained[rel] = true
	}
	for _, rel := range files {
		if retained[rel] {
			continue
		}
		if !strings.HasPrefix(rel, ".kander/locks/") || !strings.HasSuffix(rel, ".lock") {
			return errors.New("import validation unexpectedly created a data file")
		}
		path := filepath.Join(staging, filepath.FromSlash(rel))
		data, err := fs.ReadRegularFile(staging, path)
		if err != nil {
			return err
		}
		if len(data) != 0 {
			return errors.New("import validation changed a lock file")
		}
		if _, err := fs.RemoveRegularFileIfExists(staging, path); err != nil {
			return err
		}
	}
	_, digest, err := snapshotTree(staging)
	if err != nil {
		return err
	}
	// The caller compares this exact tree again against the durable intent.
	if digest != expected {
		return errors.New("import validation changed staged evidence")
	}
	return nil
}
func (p *project) recoverImport() error {
	var pending importPublication
	found, err := readJSON(p.root, filepath.Join("control", "import-pending.json"), &pending)
	if err != nil || !found {
		return err
	}
	return p.publishImport(pending)
}
func (p *project) publishImport(pending importPublication) error {
	if (pending.Schema != 1 && pending.Schema != 2) || !validImportDigest(pending.Record.Snapshot) ||
		!validImportDigest(pending.targetDigest()) || !validImportDigest(pending.stagedDigest()) ||
		(pending.Schema == 2 && pending.StagedDigest == "") ||
		pending.Record.Source == "" || pending.Record.Files < 0 || pending.Record.Tasks < 0 ||
		pending.Record.AddedTasks < 0 || pending.Record.AddedTasks > pending.Record.Tasks {
		return errors.New("invalid import publication intent")
	}
	staging, backup := filepath.Join(p.root, "board.import"), filepath.Join(p.root, "board.before-import")
	boardExists, err := importDirectoryExists(p.root, p.board)
	if err != nil {
		return err
	}
	published := false
	if boardExists {
		_, digest, err := snapshotTree(p.board)
		if err != nil {
			return err
		}
		published = digest == pending.stagedDigest()
		if !published && digest != pending.targetDigest() {
			return errors.New("import destination differs from both the original board and the prepared snapshot")
		}
	}
	recorded, err := p.importAlreadyRecorded(pending.Record)
	if err != nil {
		return err
	}
	if recorded && !published {
		return errors.New("recorded import is missing its published board")
	}
	backupExists, err := importDirectoryExists(p.root, backup)
	if err != nil {
		return err
	}
	if backupExists && !recorded {
		_, digest, err := snapshotTree(backup)
		if err != nil {
			return err
		}
		if digest != pending.targetDigest() || (boardExists && !published) {
			return errors.New("import backup conflicts with the publication intent")
		}
	}
	stageExists, err := importDirectoryExists(p.root, staging)
	if err != nil {
		return err
	}
	if !published && !stageExists {
		return errors.New("prepared import staging directory is missing")
	}
	if stageExists && !recorded {
		_, digest, err := snapshotTree(staging)
		if err != nil {
			return err
		}
		if digest != pending.stagedDigest() {
			return errors.New("prepared import staging digest changed")
		}
	}
	if !published {
		if !backupExists {
			if !boardExists {
				return errors.New("import lost its original board")
			}
			if err := os.Rename(p.board, backup); err != nil {
				return err
			}
			if err := p.server.importCheckpoint("backup-renamed"); err != nil {
				return err
			}
		}
		if err := os.Rename(staging, p.board); err != nil {
			return err
		}
		if err := p.server.importCheckpoint("published"); err != nil {
			return err
		}
	}
	if !recorded {
		if err := writeJSON(p.root, importRecordPath(pending.Record.Snapshot), pending.Record); err != nil {
			return err
		}
	}
	if err := p.server.importCheckpoint("recorded"); err != nil {
		return err
	}
	// A matching receipt and the published digest are enough to resume even
	// if a crash left these directories only partly removed.
	if err := removeImportDirectory(p.root, staging); err != nil {
		return err
	}
	if err := removeImportDirectory(p.root, backup); err != nil {
		return err
	}
	_, err = fs.RemoveRegularFileIfExists(p.root, filepath.Join(p.root, "control", "import-pending.json"))
	return err
}
