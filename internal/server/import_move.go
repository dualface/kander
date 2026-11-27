package server

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/fs"
)

// moveUploadedImport stages a received upload by renaming its tree to the
// import staging, instead of copying every file as copyImport does for a
// local source. The result is the tree the copy of the same plan would
// stage: the files the plan leaves out and the directories the copy would
// not create are removed, and a merge then copies the destination board's
// files next to the moved ones.
//
// The move is the last step that observes cancellation, as the end of the
// copy is for a local source: after it, validation and publication run to
// their end.
func (p *project) moveUploadedImport(ctx context.Context, received, staging string, plan importStage, progress *importEmitter) error {
	if err := p.server.importCheckpoint("received"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// One rename inside the project directory: never a copy across
	// directories or file systems.
	if filepath.Dir(filepath.Dir(received)) != p.root || filepath.Dir(staging) != p.root {
		return errors.New("the received upload and the import staging are not in the same project directory")
	}
	if err := os.Rename(received, staging); err != nil {
		return err
	}
	if err := p.server.importCheckpoint("moved"); err != nil {
		return err
	}
	ctx = context.WithoutCancel(ctx)
	if err := pruneMovedImport(staging, plan); err != nil {
		return err
	}
	moved := len(plan.sourceFiles)
	var movedBytes int64
	for _, rel := range plan.sourceFiles {
		movedBytes += plan.source.sizes[rel]
	}
	if !plan.merge {
		progress.phaseCounts(api.ImportPhaseCopy, moved, moved, movedBytes, movedBytes)
	} else {
		jobs := make([]importCopyJob, 0, len(plan.target.files))
		for _, rel := range plan.target.files {
			jobs = append(jobs, importCopyJob{rel: rel, root: p.board})
		}
		count := importCopyCount{done: moved, total: moved + len(jobs), bytes: movedBytes, totalBytes: movedBytes + plan.target.bytes}
		progress.phaseCounts(api.ImportPhaseCopy, count.done, count.total, count.bytes, count.totalBytes)
		if err := p.ensureImportDirectories(ctx, staging, slices.Concat(plan.target.dirs, importJobDirs(jobs))); err != nil {
			return err
		}
		if err := p.copyImportFiles(ctx, staging, jobs, progress, count); err != nil {
			return err
		}
	}
	if err := p.server.importCheckpoint("copied"); err != nil {
		return err
	}
	return writeImportDefaults(staging, plan)
}

// pruneMovedImport removes from a moved upload what the copy of the same
// plan would not stage: first every received file outside plan.sourceFiles
// (atomic write remnants, and in a merge the files of skipped and existing
// cards and of records left behind), then every received directory the copy
// would not create, deepest first. A merge removes before it copies: an
// existing card has the same paths on both boards.
func pruneMovedImport(staging string, plan importStage) error {
	kept := make(map[string]bool, len(plan.sourceFiles))
	for _, rel := range plan.sourceFiles {
		kept[rel] = true
	}
	for _, rel := range plan.source.files {
		if kept[rel] {
			continue
		}
		if _, err := fs.RemoveRegularFileIfExists(staging, filepath.Join(staging, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	dirs := copiedImportDirs(plan)
	// The received directories are sorted, so a child follows its parent.
	for _, rel := range slices.Backward(plan.source.dirs) {
		if dirs[rel] {
			continue
		}
		if _, err := fs.RemoveEmptyDirectoryIfExists(staging, filepath.Join(staging, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}

// copiedImportDirs is the set of directories copyImport creates for plan,
// before InitStorage adds the board layout: the directories the plan names,
// the parent of every copied file, and all their parents.
func copiedImportDirs(plan importStage) map[string]bool {
	dirs := map[string]bool{}
	add := func(rel string) {
		for rel != "." && rel != "" && !dirs[rel] {
			dirs[rel] = true
			rel = path.Dir(rel)
		}
	}
	files := plan.sourceFiles
	if plan.merge {
		files = slices.Concat(plan.target.files, plan.sourceFiles)
		for _, rel := range plan.target.dirs {
			add(rel)
		}
	}
	for _, rel := range plan.sourceDirs {
		add(rel)
	}
	for _, rel := range files {
		add(path.Dir(rel))
	}
	return dirs
}
