package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

// errImportCrash marks a fault-injected process death in tests: the import
// stops without in-process publication recovery, so only the startup recovery
// of project.open can finish it.
var errImportCrash = errors.New("simulated import crash")

// importRecord is control/import.json. Snapshot is the digest of the source
// board, whatever DefaultedSize changed in the published copy; a repeated
// import of that snapshot answers with the record, DefaultedSize and Warnings
// included.
type importRecord struct {
	Snapshot      string              `json:"snapshot"`
	Source        string              `json:"source"`
	Files         int                 `json:"files"`
	Tasks         int                 `json:"tasks"`
	ImportedAt    time.Time           `json:"imported_at"`
	DefaultedSize []string            `json:"defaulted_size,omitempty"`
	Warnings      []api.ImportWarning `json:"warnings,omitempty"`
}

// existing answers a repeated import of the recorded snapshot.
func (r importRecord) existing(info api.Project, source string) api.ImportResult {
	result := r.result(info, source)
	result.Existing = true
	return result
}

func (r importRecord) result(info api.Project, source string) api.ImportResult {
	return api.ImportResult{Project: info, Source: source, Files: r.Files, Tasks: r.Tasks, Snapshot: r.Snapshot, DefaultedSize: r.DefaultedSize, Warnings: r.Warnings}
}

// Import copies a quiescent v1 board into an empty project. Source recovery
// precedes a board-exclusive snapshot; original evidence bytes are retained.
// A durable directory publication intent is recovered before the server opens.
func (s *Server) Import(name, remote, source string) (api.ImportResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ImportTimeout)
	defer cancel()
	return s.ImportContext(ctx, name, remote, source)
}

// ImportTimeout bounds the snapshot lock wait and the copy of one import.
// The initial recovery of the source board waits for its lock without a
// deadline, and validation and publication do not observe cancellation.
const ImportTimeout = 5 * time.Minute

// ImportContext runs offline and inside a serving instance alike. While it
// holds p.mu, every request of the project waits; a publication that fails and
// cannot be recovered in process closes the project until restart.
func (s *Server) ImportContext(ctx context.Context, name, remote, source string) (api.ImportResult, error) {
	return s.ImportWithProgress(ctx, name, remote, source, nil)
}

// ImportWithProgress is ImportContext that reports each phase and the file
// counts of the long ones to report. report runs while the project lock is
// held, one call at a time, so it must not block. The latest report is repeated
// once per second while a step takes longer, so a reader sees progress at
// least that often.
func (s *Server) ImportWithProgress(ctx context.Context, name, remote, source string, report func(api.ImportProgress)) (result api.ImportResult, err error) {
	progress := newImportEmitter(report)
	defer progress.stop()
	progress.phase(api.ImportPhaseLock)
	source, err = filepath.Abs(source)
	if err != nil {
		return result, err
	}
	if importPathsOverlap(source, s.data) {
		return result, apiError(api.CodeInvalidRequest, "source board and server data directories must not overlap")
	}
	releaseSourceRemarks := s.routeBoardRemarks(source)
	defer releaseSourceRemarks()
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
	if result, done, err := p.precheckImport(ctx, info, source, progress); done || err != nil {
		return result, err
	}
	if err := p.lockBoard(); err != nil {
		return result, err
	}
	defer p.unlockBoard()
	if err := p.recoverImport(); err != nil {
		return result, err
	}
	published := false
	err = board.WithTransactionContext(ctx, source, board.LockScope{ExclusiveBoard: true, ReadOnly: true}, func(*board.Transaction) error {
		progress.phase(api.ImportPhaseScan)
		src, err := scanTree(source, progress.counter(api.ImportPhaseScan, 0))
		if err != nil {
			return err
		}
		files, snapshot := src.files, src.digest
		var previous importRecord
		found, err := readJSON(p.root, filepath.Join("control", "import.json"), &previous)
		if err != nil {
			return err
		}
		if found && previous.Snapshot == snapshot {
			result = previous.existing(info, source)
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
		releaseStagingRemarks := s.routeBoardRemarks(staging)
		defer releaseStagingRemarks()
		if err := removeImportDirectory(p.root, staging); err != nil {
			return err
		}
		if err := ensureDir(p.root, "board.import"); err != nil {
			return err
		}
		progress.phaseCounts(api.ImportPhaseCopy, 0, len(files), 0, src.bytes)
		var copied int64
		for i, rel := range files {
			if err := s.importCheckpoint("copying"); err != nil {
				return err
			}
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
			copied += int64(len(data))
			progress.count(api.ImportPhaseCopy, i+1, len(files), copied, src.bytes)
		}
		if err := s.importCheckpoint("copied"); err != nil {
			return err
		}
		progress.phase(api.ImportPhaseVerify)
		if err := board.InitStorage(staging); err != nil {
			return err
		}
		staged, err := scanTree(staging, progress.counter(api.ImportPhaseVerify, src.bytes))
		if err != nil {
			return err
		}
		if staged.digest != snapshot || !slices.Equal(staged.files, files) {
			return importRejected("staged board differs from the source; nothing was published")
		}
		progress.phase(api.ImportPhaseCheck)
		defaults, err := board.DefaultMissingSize(staging)
		if err != nil {
			return fmt.Errorf("default the missing SIZE lines: %w", err)
		}
		if len(defaults) > 0 {
			// The expected difference is exactly the defaulted cards, each by one
			// inserted line; the validation below compares against this tree.
			if staged, err = scanTree(staging, nil); err != nil {
				return err
			}
			if err := verifyDefaultedTree(source, src, staged, defaults); err != nil {
				return err
			}
		}
		view, err := board.LoadBoard(staging)
		if err != nil {
			return err
		}
		check, err := board.CheckBoardForImportProblems(staging)
		if err != nil {
			return fmt.Errorf("staged board check failed: %w", err)
		}
		if len(check.Blocking) > 0 {
			return importRejected("staged board fails check; nothing was published: " + sourcePaths(strings.Join(check.Blocking, "; "), staging, source) + "; " + importCheckHint)
		}
		progress.phase(api.ImportPhasePublish)
		// A non-cooperating external writer must also invalidate publication.
		_, again, err := snapshotTree(source)
		if err != nil {
			return err
		}
		if again != snapshot {
			return importRejected("source board changed during import; nothing was published")
		}
		// Validation uses read locks, which may add empty lock files. They carry no
		// evidence, but must be removed from staging to retain the staged tree.
		if err := removeImportValidationFiles(staging, files, staged.digest); err != nil {
			return err
		}

		record := importRecord{Snapshot: snapshot, Source: source, Files: len(files), Tasks: len(view.Entries), ImportedAt: s.now(), Warnings: importWarnings(check.Warnings)}
		for _, d := range defaults {
			record.DefaultedSize = append(record.DefaultedSize, d.TaskID)
		}
		pending := importPublication{Schema: 1, Record: record, EmptyDigest: emptyDigest, StagedDigest: staged.digest}
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
		result = record.result(info, source)
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
	p.log().Info("board imported", "files", result.Files, "tasks", result.Tasks, "snapshot", result.Snapshot, "defaulted_size", len(result.DefaultedSize), "warnings", len(result.Warnings))
	return result, nil
}

func importWarnings(findings []board.ImportFinding) []api.ImportWarning {
	var warnings []api.ImportWarning
	for _, f := range findings {
		warnings = append(warnings, api.ImportWarning{TaskIDs: f.TaskIDs, Message: f.Message})
	}
	return warnings
}

// verifyDefaultedTree accepts a staged tree only when it equals the source
// tree except for the spec.md of the defaulted cards, which must be the source
// text with the one inserted SIZE line.
func verifyDefaultedTree(source string, src, staged treeScan, defaults []board.SizeDefault) error {
	differs := importRejected("staged board differs from the source beyond the default SIZE lines; nothing was published")
	if !slices.Equal(src.files, staged.files) {
		return differs
	}
	defaulted := map[string]struct{}{}
	for _, d := range defaults {
		defaulted[d.Document] = struct{}{}
	}
	for _, rel := range src.files {
		want := src.sums[rel]
		if _, ok := defaulted[rel]; ok {
			data, err := fs.ReadRegularFile(source, filepath.Join(source, filepath.FromSlash(rel)))
			if err != nil {
				return err
			}
			want = sha256.Sum256([]byte(board.DefaultedSizeText(string(data))))
		}
		if staged.sums[rel] != want {
			return differs
		}
	}
	return nil
}

// importCheckHint follows the blocking problems of a refused import. A
// problem of an active card is either a card to fix or evidence that a review
// or an agent was still writing when the source was copied.
const importCheckHint = "fix these cards on the source board, or wait until no review or agent writes to it, then import again"

// importRejected is a deterministic refusal of the source board. It is a
// domain rejection, so the client receives the reason instead of the generic
// internal error.
func importRejected(message string) error {
	return apiError(api.CodeBoardRejected, message)
}

// sourcePaths names the source board instead of its staged copy in check
// text. The caller knows the source path, and a refusal must not reveal the
// server data directory that holds the staging copy.
func sourcePaths(text, staging, source string) string {
	return strings.ReplaceAll(text, staging, source)
}

func projectNotEmpty(name string) error {
	return apiError(api.CodeProjectNotEmpty, "project "+name+" already has tasks; an import only fills an empty project")
}

// precheckImport answers an import into a project that already has tasks
// without holding p.mu while the source is digested, so a busy project is not
// blocked by a full source walk. A project without tasks takes the locked
// import path, which keeps every earlier outcome.
func (p *project) precheckImport(ctx context.Context, info api.Project, source string, progress *importEmitter) (api.ImportResult, bool, error) {
	var record importRecord
	tasks, found, err := func() (int, bool, error) {
		if err := p.lockBoard(); err != nil {
			return 0, false, err
		}
		defer p.unlockBoard()
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
		progress.phase(api.ImportPhaseScan)
		src, err := scanTree(source, progress.counter(api.ImportPhaseScan, 0))
		snapshot = src.digest
		return err
	})
	if err != nil {
		return api.ImportResult{}, true, err
	}
	if snapshot != record.Snapshot {
		return api.ImportResult{}, true, projectNotEmpty(info.Name)
	}
	return record.existing(info, source), true, nil
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
	p.indexFull = true
	if err := p.events.reset(); err != nil {
		p.log().Error("event stream reset failed after a board import", "error", err)
	}
	clear(p.starts)
}
