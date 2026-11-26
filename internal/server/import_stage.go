package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/fs"
)

func (p *project) planImport(source, displaySource string, src treeScan, skipConflicts, uploaded bool) (importStage, error) {
	plan, err := p.planImportCopy(source, displaySource, src, skipConflicts, uploaded)
	if err != nil {
		return plan, err
	}
	plan.source = src
	return plan, nil
}

// planImportCopy plans from the copied part of the source; planImport then
// restores the whole source the snapshot describes.
func (p *project) planImportCopy(source, displaySource string, src treeScan, skipConflicts, uploaded bool) (importStage, error) {
	copied, remnants := withoutRemnants(src)
	plan, err := p.planImportFiles(source, displaySource, copied, skipConflicts, uploaded)
	plan.copied, plan.remnants = copied, remnants
	return plan, err
}

func (p *project) planImportFiles(source, displaySource string, src treeScan, skipConflicts, uploaded bool) (importStage, error) {
	targetView, err := board.InspectImportCards(p.board)
	if err != nil {
		return importStage{}, err
	}
	dst, err := scanTree(p.board, nil)
	if err != nil {
		return importStage{}, err
	}
	// An earlier empty import may have left runtime files but no board
	// records. Do not discard surviving evidence in an otherwise empty board.
	persistent := slices.ContainsFunc(dst.files, func(path string) bool { return board.ImportRecord(path).Kind != "runtime" })
	if len(targetView.Entries) == 0 && len(targetView.Problems) == 0 && !persistent {
		return importStage{source: src, target: dst, sourceFiles: src.files}, nil
	}
	sourceView, err := board.InspectImportCards(source)
	if err != nil {
		return importStage{}, err
	}
	if len(sourceView.Problems) > 0 {
		if uploaded {
			var paths []string
			for _, problem := range sourceView.Problems {
				paths = append(paths, problem.Path)
			}
			return importStage{}, uploadCheckRejected(displaySource, source, paths, src.files, src.dirs)
		}
		messages := make([]string, 0, len(sourceView.Problems))
		for _, problem := range sourceView.Problems {
			messages = append(messages, problem.Message)
		}
		return importStage{}, importRejected("staged board fails check; nothing was published: " + strings.Join(messages, "; ") + "; " + importCheckHint)
	}
	return planImportMerge(source, p.board, src, dst, sourceView, targetView, skipConflicts)
}

func (p *project) stageImport(ctx context.Context, source, displaySource string, plan importStage, progress *importEmitter, uploaded bool) (importPublication, error) {
	staging := filepath.Join(p.root, "board.import")
	releaseRemarks := p.server.routeBoardRemarks(staging)
	defer releaseRemarks()
	if err := removeImportDirectory(p.root, staging); err != nil {
		return importPublication{}, err
	}
	if err := ensureDir(p.root, "board.import"); err != nil {
		return importPublication{}, err
	}
	if err := p.copyImport(ctx, source, staging, plan, progress); err != nil {
		return importPublication{}, err
	}
	progress.phase(api.ImportPhaseVerify)
	if err := board.InitStorage(staging); err != nil {
		return importPublication{}, err
	}
	staged, err := scanTree(staging, progress.counter(api.ImportPhaseVerify, 0))
	if err != nil {
		return importPublication{}, err
	}
	if err := plan.verify(staged); err != nil {
		return importPublication{}, err
	}
	progress.phase(api.ImportPhaseCheck)
	displayRoot := displaySource
	if plan.merge {
		displayRoot = "merged board"
	}
	record := importRecord{
		Snapshot: plan.source.digest, Source: displaySource, Files: len(plan.source.files), Tasks: plan.tasks,
		ImportedAt: p.server.now(), AddedTasks: plan.added, ExistingTasks: plan.existing, SkippedConflicts: plan.skipped,
	}
	if plan.merge {
		for rel := range plan.defaulted {
			record.DefaultedSize = append(record.DefaultedSize, filepath.Base(filepath.Dir(rel)))
		}
		slices.Sort(record.DefaultedSize)
	} else {
		defaults, err := board.DefaultMissingSize(staging)
		if err != nil {
			return importPublication{}, fmt.Errorf("default the missing SIZE lines: %w", err)
		}
		if len(defaults) > 0 {
			staged, err = scanTree(staging, nil)
			if err != nil {
				return importPublication{}, err
			}
			if err := verifyDefaultedTree(source, plan.copied, staged, defaults); err != nil {
				return importPublication{}, err
			}
		}
		for _, d := range defaults {
			record.DefaultedSize = append(record.DefaultedSize, d.TaskID)
		}
		view, err := board.InspectImportCards(staging)
		if err != nil {
			return importPublication{}, err
		}
		record.Tasks = len(view.Entries)
	}
	check, err := board.CheckBoardForImportProblems(staging)
	if err != nil {
		var invalid *board.Error
		if errors.As(err, &invalid) {
			if uploaded {
				return importPublication{}, uploadCheckRejected(displaySource, staging, nil, nil, nil)
			}
			return importPublication{}, importRejected("staged board fails check; nothing was published: " + sourcePaths(invalid.Error(), staging, displayRoot) + "; " + importCheckHint)
		}
		return importPublication{}, fmt.Errorf("staged board check failed: %w", err)
	}
	if len(check.Blocking) > 0 {
		if uploaded {
			dirs := plan.sourceDirs
			if !plan.merge {
				dirs = plan.source.dirs
			}
			return importPublication{}, uploadCheckRejected(displaySource, staging, check.BlockingPaths, plan.sourceFiles, dirs)
		}
		return importPublication{}, importRejected("staged board fails check; nothing was published: " + sourcePaths(strings.Join(check.Blocking, "; "), staging, displayRoot) + "; " + importCheckHint)
	}
	for i := range check.Warnings {
		check.Warnings[i].Message = sourcePaths(check.Warnings[i].Message, staging, displayRoot)
	}
	record.Warnings = append(plan.remnants, importWarnings(check.Warnings)...)
	progress.phase(api.ImportPhasePublish)
	// A non-cooperating external writer must also invalidate publication.
	_, again, err := snapshotTree(source)
	if err != nil {
		return importPublication{}, err
	}
	if again != plan.source.digest {
		return importPublication{}, importRejected("source board changed during import; nothing was published")
	}
	// Check only creates empty validation locks. Remove those, not any
	// pre-existing target locks nor (for an empty project) source locks.
	if err := removeImportValidationFiles(staging, staged.files, staged.digest); err != nil {
		return importPublication{}, err
	}
	// The copy wrote without flushing. The staged tree changes for the last
	// time above, and the intent recorded next names its digest, so it must
	// be durable before the intent: a restart then publishes it unchanged.
	if err := p.server.syncImportStage(p.root, staging, staged.files, staged.dirs); err != nil {
		return importPublication{}, err
	}
	if err := p.server.importCheckpoint("synced"); err != nil {
		return importPublication{}, err
	}
	return importPublication{Schema: 2, Record: record, TargetDigest: plan.target.digest, StagedDigest: staged.digest}, nil
}

func (p *project) copyImport(ctx context.Context, source, staging string, plan importStage, progress *importEmitter) error {
	var jobs []importCopyJob
	var totalBytes int64
	if plan.merge {
		for _, rel := range plan.target.files {
			jobs = append(jobs, importCopyJob{rel: rel, root: p.board})
		}
		totalBytes += plan.target.bytes
	}
	for _, rel := range plan.sourceFiles {
		jobs = append(jobs, importCopyJob{rel: rel, root: source, scanned: &plan.source})
		totalBytes += plan.source.sizes[rel]
	}
	progress.phaseCounts(api.ImportPhaseCopy, 0, len(jobs), 0, totalBytes)
	var dirs []string
	if plan.merge {
		dirs = append(dirs, plan.target.dirs...)
	}
	dirs = append(dirs, plan.sourceDirs...)
	for _, job := range jobs {
		if dir := path.Dir(job.rel); dir != "." {
			dirs = append(dirs, dir)
		}
	}
	slices.Sort(dirs)
	for _, rel := range slices.Compact(dirs) {
		if err := fs.EnsurePrivateDirectory(p.root, filepath.Join(staging, filepath.FromSlash(rel)), true); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.copyImportFiles(ctx, staging, jobs, progress, totalBytes); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.server.importCheckpoint("copied"); err != nil {
		return err
	}
	// Keep progress byte counts on the raw input. The normalized specs are
	// written only once the cancellable copy is complete.
	for rel, text := range plan.defaulted {
		path := filepath.Join(staging, filepath.FromSlash(rel))
		data, err := readImportFile(staging, rel)
		if err != nil {
			return err
		}
		if sha256.Sum256(data) != plan.source.sums[rel] {
			return importRejected("staged spec changed before defaulting SIZE; nothing was published")
		}
		if err := fs.WriteTextAtomicInherited(staging, path, text, true); err != nil {
			return err
		}
	}
	return nil
}

// importCopyWorkers bounds the files read and written at once by one copy.
var importCopyWorkers = min(8, runtime.GOMAXPROCS(0))

// importCopyJob is one file to stage. A file of a scanned source must still
// have the scanned digest; a file of the destination board is read as is.
type importCopyJob struct {
	rel     string
	root    string
	scanned *treeScan
}

func (j importCopyJob) read() ([]byte, error) {
	if j.scanned != nil {
		return readScannedImportFile(j.root, j.rel, *j.scanned)
	}
	return readImportFile(j.root, j.rel)
}

// copyImportFiles writes every job into the staging tree on a bounded pool.
// board.import is a private, single-use directory that verify digests again
// and stageImport makes durable as a whole before the publication intent, so
// each file is created once, without a temporary file or a flush of its own.
// The "copying" checkpoint runs before each source file is handed out, in
// order, on this goroutine. The first error stops the hand-out; files already
// handed out finish before it returns.
func (p *project) copyImportFiles(ctx context.Context, staging string, jobs []importCopyJob, progress *importEmitter, totalBytes int64) error {
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	writers := &importDirWriters{root: p.root, staging: staging, open: map[string]*importDirWriter{}}
	defer writers.close()
	queue := make(chan importCopyJob)
	var (
		workers sync.WaitGroup
		mu      sync.Mutex
		done    int
		copied  int64
	)
	for range importCopyWorkers {
		workers.Go(func() {
			for job := range queue {
				if ctx.Err() != nil {
					continue
				}
				data, err := job.read()
				if err == nil {
					err = writers.create(job.rel, data)
				}
				if err != nil {
					stop(err)
					continue
				}
				// Report under one lock so the counts never go back.
				mu.Lock()
				done++
				copied += int64(len(data))
				progress.count(api.ImportPhaseCopy, done, len(jobs), copied, totalBytes)
				mu.Unlock()
			}
		})
	}
	var handOut error
	for _, job := range jobs {
		if job.scanned != nil {
			if handOut = p.server.importCheckpoint("copying"); handOut != nil {
				break
			}
		}
		select {
		case queue <- job:
			continue
		case <-ctx.Done():
		}
		break
	}
	close(queue)
	workers.Wait()
	if handOut != nil {
		return handOut
	}
	return context.Cause(ctx)
}

// importDirWriters pins staging directories, so a file is created without
// resolving its path from the root again. Jobs arrive in path order, so the
// files of one directory are copied together; a directory nobody uses is
// closed once more than importOpenDirs are open, which bounds the handles.
type importDirWriters struct {
	root, staging string
	mu            sync.Mutex
	open          map[string]*importDirWriter
}

type importDirWriter struct {
	writer *fs.DirectoryWriter
	users  int
}

const importOpenDirs = 64

func (w *importDirWriters) create(rel string, data []byte) error {
	dir := path.Dir(rel)
	w.mu.Lock()
	entry, ok := w.open[dir]
	if !ok {
		writer, err := fs.OpenDirectoryWriter(w.root, filepath.Join(w.staging, filepath.FromSlash(dir)))
		if err != nil {
			w.mu.Unlock()
			return err
		}
		entry = &importDirWriter{writer: writer}
		w.open[dir] = entry
	}
	entry.users++
	w.mu.Unlock()
	err := entry.writer.CreateInherited(path.Base(rel), data)
	w.mu.Lock()
	entry.users--
	if entry.users == 0 && len(w.open) > importOpenDirs {
		_ = entry.writer.Close()
		delete(w.open, dir)
	}
	w.mu.Unlock()
	return err
}

func (w *importDirWriters) close() {
	for _, entry := range w.open {
		_ = entry.writer.Close()
	}
}

func (plan importStage) verify(staged treeScan) error {
	if !plan.merge {
		if staged.digest == plan.copied.digest {
			return nil
		}
		return importRejected("staged board differs from the source; nothing was published")
	}
	differs := importRejected("staged board differs from the expected merge; nothing was published")
	if len(staged.files) != len(plan.target.files)+len(plan.sourceFiles) {
		return differs
	}
	for _, rel := range plan.target.files {
		if staged.sums[rel] != plan.target.sums[rel] {
			return differs
		}
	}
	for _, rel := range plan.sourceFiles {
		sum := plan.source.sums[rel]
		if text, ok := plan.defaulted[rel]; ok {
			sum = sha256.Sum256([]byte(text))
		}
		if staged.sums[rel] != sum {
			return differs
		}
	}
	return nil
}

func readImportFile(root, rel string) ([]byte, error) {
	return fs.ReadRegularFile(root, filepath.Join(root, filepath.FromSlash(rel)))
}

func readScannedImportFile(root, rel string, tree treeScan) ([]byte, error) {
	data, err := readImportFile(root, rel)
	if err != nil {
		return nil, err
	}
	if sha256.Sum256(data) != tree.sums[rel] {
		return nil, importRejected("source board changed during import; nothing was published")
	}
	return data, nil
}

// Board checks may quote invalid field values. An upload refusal tells the
// caller where to investigate without including document text in an error.
func uploadCheckRejected(source, staging string, paths, files, dirs []string) error {
	message := fmt.Sprintf("staged board from %q fails check; nothing was published", source)
	var affected []string
	var detailBytes int
	for _, path := range paths {
		rel, err := filepath.Rel(staging, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		_, file := slices.BinarySearch(files, rel)
		_, dir := slices.BinarySearch(dirs, rel)
		if file || dir {
			separator := "/"
			if strings.Contains(source, `\`) && !strings.HasPrefix(source, "/") {
				separator = `\`
				rel = strings.ReplaceAll(rel, "/", separator)
			}
			prefix := source
			if !strings.HasSuffix(prefix, separator) {
				prefix += separator
			}
			entry := fmt.Sprintf("%q", prefix+rel)
			detailBytes += len(entry)
			if detailBytes > 32<<10 {
				break
			}
			affected = append(affected, entry)
			if len(affected) == 10 {
				break
			}
		}
	}
	if len(affected) > 0 {
		message += "; source entries: " + strings.Join(affected, ", ")
	}
	return importRejected(message + "; " + importCheckHint)
}
