package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/fs"
)

// errImportCrash marks a fault-injected process death in tests: the import
// stops without in-process publication recovery, so only the startup recovery
// of project.open can finish it.
var errImportCrash = errors.New("simulated import crash")

// importRecord is control/imports/<snapshot>.json. Snapshot is the digest of the source
// board, whatever DefaultedSize changed in the published copy; a repeated
// import of that snapshot answers with the record, DefaultedSize and Warnings
// included.
type importRecord struct {
	Snapshot         string               `json:"snapshot"`
	Source           string               `json:"source"`
	Files            int                  `json:"files"`
	Tasks            int                  `json:"tasks"`
	ImportedAt       time.Time            `json:"imported_at"`
	DefaultedSize    []string             `json:"defaulted_size,omitempty"`
	Warnings         []api.ImportWarning  `json:"warnings,omitempty"`
	AddedTasks       int                  `json:"added_tasks,omitzero"`
	ExistingTasks    []string             `json:"existing_tasks,omitempty"`
	SkippedConflicts []api.ImportConflict `json:"skipped_conflicts,omitempty"`
}

// existing answers a repeated import of the recorded snapshot.
func (r importRecord) existing(info api.Project, source string) api.ImportResult {
	result := r.result(info, source)
	result.Existing = true
	return result
}

func (r importRecord) result(info api.Project, source string) api.ImportResult {
	return api.ImportResult{
		Project: info, Source: source, Files: r.Files, Tasks: r.Tasks, Snapshot: r.Snapshot,
		DefaultedSize: r.DefaultedSize, Warnings: r.Warnings, AddedTasks: r.AddedTasks,
		ExistingTasks: r.ExistingTasks, SkippedConflicts: r.SkippedConflicts,
	}
}

// Import copies or merges a quiescent v1 board into a project. Source recovery
// precedes a board-exclusive snapshot; original evidence bytes are retained.
// A durable directory publication intent is recovered before the server opens.
func (s *Server) Import(name, remote, source string) (api.ImportResult, error) {
	return s.ImportContext(context.Background(), name, remote, source)
}

// ImportTimeout bounds only the wait for the source board snapshot lock. The
// initial recovery of the source board waits for its lock without a deadline;
// the scan and the copy stop only when the caller cancels or the server shuts
// down, and validation and publication do not observe cancellation.
const ImportTimeout = 5 * time.Minute

// ImportContext runs offline and inside a serving instance alike. While it
// holds p.mu, every request of the project waits, and that time does not count
// against the leases active when it took the lock (pauseImportLeases); a
// publication that fails and cannot be recovered in process closes the project
// until restart.
func (s *Server) ImportContext(ctx context.Context, name, remote, source string) (api.ImportResult, error) {
	return s.ImportWithProgress(ctx, name, remote, source, nil)
}

// ImportWithProgress is ImportContext that reports each phase and the file
// counts of the long ones to report. report runs while the project lock is
// held, one call at a time, so it must not block. The latest report is repeated
// once per second while a step takes longer, so a reader sees progress at
// least that often.
func (s *Server) ImportWithProgress(ctx context.Context, name, remote, source string, report func(api.ImportProgress)) (result api.ImportResult, err error) {
	return s.importV1Board(ctx, api.ImportRequest{Project: name, Remote: remote, From: source}, report)
}

func (s *Server) importV1Board(ctx context.Context, req api.ImportRequest, report func(api.ImportProgress)) (result api.ImportResult, err error) {
	defer func() {
		if err != nil {
			s.logImportFailure(req.Project, err)
		}
	}()
	progress := newImportEmitter(report)
	defer progress.stop()
	progress.phase(api.ImportPhaseLock)
	source, err := filepath.Abs(req.From)
	if err != nil {
		return api.ImportResult{}, err
	}
	if importPathsOverlap(source, s.data) {
		return api.ImportResult{}, apiError(api.CodeInvalidRequest, "source board and server data directories must not overlap")
	}
	releaseSourceRemarks := s.routeBoardRemarks(source)
	defer releaseSourceRemarks()
	if err := board.RecoverTransactions(source); err != nil {
		var invalid *board.Error
		if errors.As(err, &invalid) {
			return api.ImportResult{}, importRejected("recover the source board first: " + invalid.Error())
		}
		return api.ImportResult{}, fmt.Errorf("recover the source board first: %w", err)
	}
	if err := s.importCheckpoint("recovered"); err != nil {
		return api.ImportResult{}, err
	}
	return s.importPreparedSource(ctx, req, source, source, nil, progress)
}

// importPreparedSource runs the same merge and publication for a recovered
// local source or a fully verified, server-owned upload. An uploaded snapshot
// is never recovered on this host and no network request can supply its path.
// upload is the scan of an upload taken from its manifest, nil for a local
// source: the upload is not read again, and its received tree is moved into
// the import staging instead of copied (moveUploadedImport).
func (s *Server) importPreparedSource(ctx context.Context, req api.ImportRequest, source, displaySource string, upload *treeScan, progress *importEmitter) (result api.ImportResult, err error) {
	if err := ctx.Err(); err != nil {
		return result, s.classifyImportCancel(err, ctx, nil)
	}
	info, _, err := s.register(api.RegisterProject{Name: req.Project, Remote: req.Remote})
	if err != nil {
		return result, err
	}
	p, err := s.project(info.ID)
	if err != nil {
		return result, err
	}
	// An import replaces the board directory: a gc of this project that
	// works on the board outside the project lock must not run meanwhile.
	p.maintenance.Lock()
	defer p.maintenance.Unlock()
	if err := p.lockBoard(); err != nil {
		return result, err
	}
	defer p.unlockBoard()
	p.beginImportPause()
	defer func() {
		if errors.Is(err, errImportCrash) {
			p.importCrashed = true
		}
	}()
	if err := p.recoverImport(); err != nil {
		return result, err
	}
	var pending importPublication
	uploaded := upload != nil
	prepare := func() error {
		var src treeScan
		if uploaded {
			// A local source is scanned under ctx. An upload has no scan, so
			// a cancellation that arrived while it waited for the project
			// lock is observed here, before a recorded snapshot answers.
			if err := ctx.Err(); err != nil {
				return err
			}
			src = *upload
			progress.phaseCounts(api.ImportPhaseScan, len(src.files), len(src.files), src.bytes, src.bytes)
		} else {
			progress.phase(api.ImportPhaseScan)
			scanned, err := scanImportTree(ctx, source, progress.counter(api.ImportPhaseScan, 0), false)
			if err != nil {
				return err
			}
			src = scanned
		}
		previous, err := p.readImportRecord(src.digest)
		if err != nil {
			return err
		}
		if previous.Snapshot != "" {
			result = previous.existing(info, displaySource)
			if uploaded && previous.Source != displaySource {
				for i := range result.Warnings {
					result.Warnings[i].Message = sourcePaths(result.Warnings[i].Message, previous.Source, displaySource)
				}
			}
			return nil
		}
		if !uploaded {
			if err := checkImportRecordLayout(src.files); err != nil {
				return err
			}
		}
		plan, err := p.planImport(source, displaySource, src, req.SkipConflicts, uploaded)
		if err != nil {
			return err
		}
		pending, err = p.stageImport(ctx, source, displaySource, plan, progress, uploaded)
		return err
	}
	if !uploaded {
		// Only the lock wait has a fixed limit. The scan and the copy run
		// under ctx, so a large board is never cut off while it progresses.
		lockWait, cancelLockWait := context.WithTimeout(ctx, s.importLockWait)
		err = board.WithImportSnapshot(lockWait, source, prepare)
		err = s.classifyImportCancel(err, ctx, lockWait)
		cancelLockWait()
	} else {
		err = s.classifyImportCancel(prepare(), ctx, nil)
	}
	if err != nil {
		return result, p.settleFailedImport(err)
	}
	if pending.Schema == 0 {
		return result, nil // Repeated snapshot: no board or event changes.
	}
	if err := writeJSON(p.root, filepath.Join("control", "import-pending.json"), pending); err != nil {
		return result, p.settleFailedImport(err)
	}
	if err := s.importCheckpoint("prepared"); err != nil {
		return result, p.settleFailedImport(err)
	}
	if err := p.publishImport(pending); err != nil {
		return result, p.settleFailedImport(err)
	}
	p.importPublished()
	result = pending.Record.result(info, displaySource)
	p.log().Info("board imported", "files", result.Files, "tasks", result.Tasks, "added_tasks", result.AddedTasks, "snapshot", result.Snapshot, "defaulted_size", len(result.DefaultedSize), "warnings", len(result.Warnings))
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
	return verifyDefaultedFiles(src, staged, defaults, func(d board.SizeDefault) ([]byte, error) {
		return fs.ReadRegularFile(source, filepath.Join(source, filepath.FromSlash(d.Document)))
	})
}

// verifyMovedDefaultedTree is verifyDefaultedTree for an upload moved into
// the staging: the source text of a defaulted spec.md is the one
// DefaultMissingSize read from the staging, which must still be the received
// file the manifest describes.
func verifyMovedDefaultedTree(src, staged treeScan, defaults []board.SizeDefault) error {
	return verifyDefaultedFiles(src, staged, defaults, func(d board.SizeDefault) ([]byte, error) {
		if sha256.Sum256([]byte(d.Original)) != src.sums[d.Document] {
			return nil, defaultedTreeDiffers()
		}
		return []byte(d.Original), nil
	})
}

func defaultedTreeDiffers() error {
	return importRejected("staged board differs from the source beyond the default SIZE lines; nothing was published")
}

// verifyDefaultedFiles compares the defaulted spec.md files with the text
// original returns for each.
func verifyDefaultedFiles(src, staged treeScan, defaults []board.SizeDefault, original func(board.SizeDefault) ([]byte, error)) error {
	if !slices.Equal(src.files, staged.files) {
		return defaultedTreeDiffers()
	}
	defaulted := map[string]board.SizeDefault{}
	for _, d := range defaults {
		defaulted[d.Document] = d
	}
	for _, rel := range src.files {
		want := src.sums[rel]
		if d, ok := defaulted[rel]; ok {
			data, err := original(d)
			if err != nil {
				return err
			}
			want = sha256.Sum256([]byte(board.DefaultedSizeText(string(data))))
		}
		if staged.sums[rel] != want {
			return defaultedTreeDiffers()
		}
	}
	return nil
}

// importCheckHint follows the blocking problems of a refused import. A
// problem of an active card is either a card to fix or evidence that a review
// or an agent was still writing when the source was copied.
const importCheckHint = "fix these cards on the source or destination board, or wait until no review or agent writes to the source, then import again"

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
	if staging == "" {
		return text
	}
	return strings.NewReplacer(quotedPath(staging), quotedPath(source), staging, source).Replace(text)
}

func quotedPath(path string) string {
	q := strconv.Quote(path)
	return q[1 : len(q)-1]
}

// settleFailedImport runs under p.mu. A failure before the publication intent
// removes the staging copy, whichever path ran the import. A failure after
// the intent is recovered at once so a serving project never stays without
// its board; when that also fails, the project refuses every board request
// until a restart retries the recovery in project.open.
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
		p.discardImportStaging()
		return failure
	}
	if err := p.publishImport(pending); err != nil {
		p.importBroken = true
		p.log().Error("board import publication failed and could not be recovered; restart the server", "error", failure, "recovery_error", err)
		return errors.Join(failure, err)
	}
	p.importPublished()
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
	p.openOrders = nil
}
