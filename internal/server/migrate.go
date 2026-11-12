package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

// ImportResult describes one board import.
type ImportResult struct {
	Project  api.Project `json:"project"`
	Files    int         `json:"files"`
	Tasks    int         `json:"tasks"`
	Snapshot string      `json:"snapshot"`
	Existing bool        `json:"existing,omitzero"`
}

type importRecord struct {
	Snapshot   string    `json:"snapshot"`
	Source     string    `json:"source"`
	Files      int       `json:"files"`
	Tasks      int       `json:"tasks"`
	ImportedAt time.Time `json:"imported_at"`
}

// Import copies a quiescent v1 board into an empty project. Source recovery
// precedes a board-exclusive snapshot; original evidence bytes are retained.
// A durable directory publication intent is recovered before the server opens.
func (s *Server) Import(name, remote, source string) (ImportResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return s.ImportContext(ctx, name, remote, source)
}

func (s *Server) ImportContext(ctx context.Context, name, remote, source string) (result ImportResult, err error) {
	source, err = filepath.Abs(source)
	if err != nil {
		return result, err
	}
	if importPathsOverlap(source, s.data) {
		return result, errors.New("source board and server data directories must not overlap")
	}
	if err := board.RecoverTransactions(source); err != nil {
		return result, fmt.Errorf("recover the source board first: %w", err)
	}
	info, _, err := s.register(api.RegisterProject{Name: name, Remote: remote})
	if err != nil {
		return result, err
	}
	p, err := s.project(info.ID)
	if err != nil {
		return result, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.recoverImport(); err != nil {
		return result, err
	}
	err = board.WithTransactionContext(ctx, source, board.LockScope{ExclusiveBoard: true, ReadOnly: true}, func(*board.Transaction) error {
		files, snapshot, err := snapshotTree(source)
		if err != nil {
			return err
		}
		var previous importRecord
		found, err := readJSON(p.root, filepath.Join("control", "import.json"), &previous)
		if err != nil {
			return err
		}
		if found && previous.Snapshot == snapshot {
			result = ImportResult{Project: info, Files: previous.Files, Tasks: previous.Tasks, Snapshot: snapshot, Existing: true}
			return nil
		}
		current, err := board.LoadBoard(p.board)
		if err != nil {
			return err
		}
		if len(current.Entries) > 0 {
			return apiError(api.CodeProjectNotEmpty, "project "+name+" already has tasks; an import only fills an empty project")
		}
		_, emptyDigest, err := snapshotTree(p.board)
		if err != nil {
			return err
		}
		staging := filepath.Join(p.root, "board.import")
		if err := removeImportDirectory(p.root, staging); err != nil {
			return err
		}
		if err := ensureDir(p.root, "board.import"); err != nil {
			return err
		}
		for _, rel := range files {
			if err := ctx.Err(); err != nil {
				return err
			}
			data, err := fs.ReadRegularFile(source, filepath.Join(source, rel))
			if err != nil {
				return err
			}
			target := filepath.Join(staging, rel)
			if err := fs.EnsurePrivateDirectory(p.root, filepath.Dir(target), true); err != nil {
				return err
			}
			if err := fs.WriteBytesAtomicInherited(p.root, target, data, false); err != nil {
				return err
			}
		}
		if err := s.importCheckpoint("copied"); err != nil {
			return err
		}
		if err := board.InitStorage(staging); err != nil {
			return err
		}
		stagedFiles, stagedDigest, err := snapshotTree(staging)
		if err != nil {
			return err
		}
		if stagedDigest != snapshot || !slices.Equal(stagedFiles, files) {
			return errors.New("staged board differs from the source; nothing was published")
		}
		staged, err := board.LoadBoard(staging)
		if err != nil {
			return err
		}
		code, _, problems, err := board.CheckBoardForImport(staging)
		if err != nil {
			return fmt.Errorf("staged board check failed: %w", err)
		}
		if code != 0 {
			return fmt.Errorf("staged board fails check; nothing was published: %s", strings.Join(problems, "; "))
		}
		// A non-cooperating external writer must also invalidate publication.
		_, again, err := snapshotTree(source)
		if err != nil {
			return err
		}
		if again != snapshot {
			return errors.New("source board changed during import; nothing was published")
		}
		// Validation uses read locks, which may add empty lock files. They carry no
		// evidence, but must be removed from staging to retain the exact snapshot.
		if err := removeImportValidationFiles(staging, files, snapshot); err != nil {
			return err
		}
		record := importRecord{Snapshot: snapshot, Source: source, Files: len(files), Tasks: len(staged.Entries), ImportedAt: s.now()}
		pending := importPublication{Schema: 1, Record: record, EmptyDigest: emptyDigest}
		if err := writeJSON(p.root, filepath.Join("control", "import-pending.json"), pending); err != nil {
			return err
		}
		if err := s.importCheckpoint("prepared"); err != nil {
			return err
		}
		if err := p.publishImport(pending); err != nil {
			return err
		}
		result = ImportResult{Project: info, Files: record.Files, Tasks: record.Tasks, Snapshot: snapshot}
		return nil
	})
	if err != nil {
		return result, err
	}
	if err := p.reconcileClaims(); err != nil {
		return result, err
	}
	p.log().Info("board imported", "files", result.Files, "tasks", result.Tasks, "snapshot", result.Snapshot)
	return result, nil
}

// snapshotTree lists every regular file below root (sorted, root-relative)
// and digests their paths and bytes. Symlinks, junctions, and other entries
// refuse the snapshot, as every board operation does.
func snapshotTree(root string) ([]string, string, error) {
	var files []string
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := fs.ListDirectory(root, filepath.Join(root, rel))
		if err != nil {
			return err
		}
		for _, e := range entries {
			child := filepath.Join(rel, e.Name)
			switch e.Kind {
			case fs.KindDirectory:
				if err := walk(child); err != nil {
					return err
				}
			case fs.KindFile:
				files = append(files, filepath.ToSlash(child))
			default:
				return errors.New("board entry " + child + " is not a regular file or directory")
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, "", err
	}
	slices.Sort(files)
	h := sha256.New()
	for _, rel := range files {
		data, err := fs.ReadRegularFile(root, filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%x\n", rel, sum)
	}
	return files, hex.EncodeToString(h.Sum(nil)), nil
}
