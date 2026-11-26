package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/fs"
)

func (p *project) planImport(source string, src treeScan, skipConflicts bool) (importStage, error) {
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
		messages := make([]string, 0, len(sourceView.Problems))
		for _, problem := range sourceView.Problems {
			messages = append(messages, problem.Message)
		}
		return importStage{}, importRejected("staged board fails check; nothing was published: " + strings.Join(messages, "; ") + "; " + importCheckHint)
	}
	return planImportMerge(source, p.board, src, dst, sourceView, targetView, skipConflicts)
}

func (p *project) stageImport(ctx context.Context, source string, plan importStage, progress *importEmitter) (importPublication, error) {
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
	displayRoot := source
	if plan.merge {
		displayRoot = "merged board"
	}
	record := importRecord{
		Snapshot: plan.source.digest, Source: source, Files: len(plan.source.files), Tasks: plan.tasks,
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
			if err := verifyDefaultedTree(source, plan.source, staged, defaults); err != nil {
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
			return importPublication{}, importRejected("staged board fails check; nothing was published: " + sourcePaths(invalid.Error(), staging, displayRoot) + "; " + importCheckHint)
		}
		return importPublication{}, fmt.Errorf("staged board check failed: %w", err)
	}
	if len(check.Blocking) > 0 {
		return importPublication{}, importRejected("staged board fails check; nothing was published: " + sourcePaths(strings.Join(check.Blocking, "; "), staging, displayRoot) + "; " + importCheckHint)
	}
	for i := range check.Warnings {
		check.Warnings[i].Message = sourcePaths(check.Warnings[i].Message, staging, displayRoot)
	}
	record.Warnings = importWarnings(check.Warnings)
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
	return importPublication{Schema: 2, Record: record, TargetDigest: plan.target.digest, StagedDigest: staged.digest}, nil
}

func (p *project) copyImport(ctx context.Context, source, staging string, plan importStage, progress *importEmitter) error {
	total := len(plan.sourceFiles)
	var done int
	var copied, totalBytes int64
	if plan.merge {
		total += len(plan.target.files)
		totalBytes += plan.target.bytes
	}
	for _, rel := range plan.sourceFiles {
		totalBytes += plan.source.sizes[rel]
	}
	progress.phaseCounts(api.ImportPhaseCopy, 0, total, 0, totalBytes)
	copyFile := func(rel string, data []byte) error {
		path := filepath.Join(staging, filepath.FromSlash(rel))
		if err := fs.EnsurePrivateDirectory(p.root, filepath.Dir(path), true); err != nil {
			return err
		}
		if err := fs.WriteBytesAtomicInherited(p.root, path, data, false); err != nil {
			return err
		}
		done++
		copied += int64(len(data))
		progress.count(api.ImportPhaseCopy, done, total, copied, totalBytes)
		return nil
	}
	if plan.merge {
		for _, rel := range plan.target.dirs {
			if err := fs.EnsurePrivateDirectory(p.root, filepath.Join(staging, filepath.FromSlash(rel)), true); err != nil {
				return err
			}
		}
		for _, rel := range plan.target.files {
			data, err := readImportFile(p.board, rel)
			if err != nil {
				return err
			}
			if err := copyFile(rel, data); err != nil {
				return err
			}
		}
	}
	for _, rel := range plan.sourceDirs {
		if err := fs.EnsurePrivateDirectory(p.root, filepath.Join(staging, filepath.FromSlash(rel)), true); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, rel := range plan.sourceFiles {
		if err := p.server.importCheckpoint("copying"); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := readScannedImportFile(source, rel, plan.source)
		if err != nil {
			return err
		}
		if err := copyFile(rel, data); err != nil {
			return err
		}
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

func (plan importStage) verify(staged treeScan) error {
	if !plan.merge {
		if staged.digest == plan.source.digest {
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
