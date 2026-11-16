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

// errImportCrash marks a fault-injected process death in tests: the import
// stops without in-process publication recovery, so only the startup recovery
// of project.open can finish it.
var errImportCrash = errors.New("simulated import crash")

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
func (s *Server) Import(name, remote, source string) (api.ImportResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ImportTimeout)
	defer cancel()
	return s.ImportContext(ctx, name, remote, source)
}

// ImportTimeout bounds the source lock wait and the copy of one import. Later
// validation and publication do not observe cancellation.
const ImportTimeout = 5 * time.Minute

// ImportContext runs offline and inside a serving instance alike. While it
// holds p.mu, every request of the project waits; a publication that fails and
// cannot be recovered in process closes the project until restart.
func (s *Server) ImportContext(ctx context.Context, name, remote, source string) (result api.ImportResult, err error) {
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
	if result, done, err := p.precheckImport(ctx, info, source); done || err != nil {
		return result, err
	}
	if err := p.lockBoard(); err != nil {
		return result, err
	}
	defer p.mu.Unlock()
	if err := p.recoverImport(); err != nil {
		return result, err
	}
	published := false
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
			result = api.ImportResult{Project: info, Source: source, Files: previous.Files, Tasks: previous.Tasks, Snapshot: snapshot, Existing: true}
			return nil
		}
		current, err := board.LoadBoard(p.board)
		if err != nil {
			return err
		}
		if len(current.Entries) > 0 {
			return projectNotEmpty(name)
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
			data, err := fs.ReadRegularFile(source, filepath.Join(source, filepath.FromSlash(rel)))
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
		published = true
		result = api.ImportResult{Project: info, Source: source, Files: record.Files, Tasks: record.Tasks, Snapshot: snapshot}
		return nil
	})
	if err != nil {
		return result, p.settleFailedImport(err)
	}
	if published {
		p.importPublished()
	}
	if err := p.reconcileClaims(); err != nil {
		return result, err
	}
	p.log().Info("board imported", "files", result.Files, "tasks", result.Tasks, "snapshot", result.Snapshot)
	return result, nil
}

func projectNotEmpty(name string) error {
	return apiError(api.CodeProjectNotEmpty, "project "+name+" already has tasks; an import only fills an empty project")
}

// precheckImport answers an import into a project that already has tasks
// without holding p.mu while the source is digested, so a busy project is not
// blocked by a full source walk. A project without tasks takes the locked
// import path, which keeps every earlier outcome.
func (p *project) precheckImport(ctx context.Context, info api.Project, source string) (api.ImportResult, bool, error) {
	var record importRecord
	tasks, found, err := func() (int, bool, error) {
		if err := p.lockBoard(); err != nil {
			return 0, false, err
		}
		defer p.mu.Unlock()
		// Recovery first: a leftover intent can leave no board to inspect.
		if err := p.recoverImport(); err != nil {
			return 0, false, err
		}
		view, err := board.LoadBoard(p.board)
		if err != nil {
			return 0, false, err
		}
		found, err := readJSON(p.root, filepath.Join("control", "import.json"), &record)
		return len(view.Entries), found, err
	}()
	if err != nil || tasks == 0 {
		return api.ImportResult{}, false, err
	}
	if !found {
		return api.ImportResult{}, true, projectNotEmpty(info.Name)
	}
	if err := p.server.importCheckpoint("precheck-digest"); err != nil {
		return api.ImportResult{}, true, err
	}
	var snapshot string
	err = board.WithTransactionContext(ctx, source, board.LockScope{ExclusiveBoard: true, ReadOnly: true}, func(*board.Transaction) error {
		var err error
		_, snapshot, err = snapshotTree(source)
		return err
	})
	if err != nil {
		return api.ImportResult{}, true, err
	}
	if snapshot != record.Snapshot {
		return api.ImportResult{}, true, projectNotEmpty(info.Name)
	}
	return api.ImportResult{Project: info, Source: source, Files: record.Files, Tasks: record.Tasks, Snapshot: snapshot, Existing: true}, true, nil
}

// settleFailedImport runs under p.mu. A failure after the publication intent
// is recovered at once so a serving project never stays without its board;
// when that also fails, the project refuses every board request until a
// restart retries the recovery in project.open.
func (p *project) settleFailedImport(failure error) error {
	if errors.Is(failure, errImportCrash) {
		return failure
	}
	var pending importPublication
	found, err := readJSON(p.root, filepath.Join("control", "import-pending.json"), &pending)
	if err != nil {
		p.importBroken = true
		p.log().Error("board import failed and its publication intent is unreadable", "error", failure, "intent_error", err)
		return errors.Join(failure, err)
	}
	if !found {
		return failure
	}
	if err := p.publishImport(pending); err != nil {
		p.importBroken = true
		p.log().Error("board import publication failed and could not be recovered; restart the server", "error", failure, "recovery_error", err)
		return errors.Join(failure, err)
	}
	p.importPublished()
	if err := p.reconcileClaims(); err != nil {
		return errors.Join(failure, err)
	}
	p.log().Warn("board import publication failed and was recovered in process", "error", failure)
	return failure
}

// importPublished makes every observer re-read the replaced board.
func (p *project) importPublished() {
	if err := p.events.reset(); err != nil {
		p.log().Error("event stream reset failed after a board import", "error", err)
	}
	clear(p.starts)
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
