package server

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

type importPublication struct {
	Schema      int          `json:"schema"`
	Record      importRecord `json:"record"`
	EmptyDigest string       `json:"empty_digest"`
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
	for _, rel := range files {
		if slices.Contains(original, rel) {
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
	if pending.Schema != 1 || len(pending.Record.Snapshot) != 64 || len(pending.EmptyDigest) != 64 || pending.Record.Source == "" || pending.Record.Files < 0 || pending.Record.Tasks < 0 {
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
		published = digest == pending.Record.Snapshot
		if !published && digest != pending.EmptyDigest {
			return errors.New("import destination differs from both the original empty board and the prepared snapshot")
		}
	}
	if !published {
		stageExists, err := importDirectoryExists(p.root, staging)
		if err != nil {
			return err
		}
		if !stageExists {
			return errors.New("prepared import staging directory is missing")
		}
		_, digest, err := snapshotTree(staging)
		if err != nil {
			return err
		}
		if digest != pending.Record.Snapshot {
			return errors.New("prepared import staging digest changed")
		}
		backupExists, err := importDirectoryExists(p.root, backup)
		if err != nil {
			return err
		}
		if backupExists {
			_, digest, err := snapshotTree(backup)
			if err != nil {
				return err
			}
			if digest != pending.EmptyDigest || boardExists {
				return errors.New("import backup conflicts with the publication intent")
			}
		} else {
			if !boardExists {
				return errors.New("import lost its original empty board")
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
	if err := writeJSON(p.root, filepath.Join("control", "import.json"), pending.Record); err != nil {
		return err
	}
	if err := p.server.importCheckpoint("recorded"); err != nil {
		return err
	}
	// Finish the record before removing recovery material. Retrying either
	// cleanup is harmless; no card or evidence is reconstructed from defaults.
	if stagingExists, err := importDirectoryExists(p.root, staging); err != nil {
		return err
	} else if stagingExists {
		_, digest, err := snapshotTree(staging)
		if err != nil {
			return err
		}
		if digest != pending.Record.Snapshot {
			return errors.New("unused import staging conflicts with the published snapshot")
		}
		if err := removeImportDirectory(p.root, staging); err != nil {
			return err
		}
	}
	if err := removeImportDirectory(p.root, backup); err != nil {
		return err
	}
	_, err = fs.RemoveRegularFileIfExists(p.root, filepath.Join(p.root, "control", "import-pending.json"))
	return err
}
