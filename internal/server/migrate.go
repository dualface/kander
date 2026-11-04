package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
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

// Import copies a v1 board into a project that has no tasks. It runs with
// the data directory locked (the server is not serving). The source must be
// quiet: its interrupted transactions are recovered first, then every
// regular file is copied byte for byte into a staging directory and compared
// by SHA-256 against the source, the staged board is loaded and checked, and
// only then is it published in one directory rename. The source board is
// never changed and stays as the local backup. Importing the same snapshot
// into the same project again returns the earlier import; any other import
// into a project with tasks is refused.
func (s *Server) Import(name, remote, source string) (ImportResult, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return ImportResult{}, err
	}
	if err := board.RecoverTransactions(source); err != nil {
		return ImportResult{}, fmt.Errorf("recover the source board first: %w", err)
	}
	files, snapshot, err := snapshotTree(source)
	if err != nil {
		return ImportResult{}, err
	}
	sourceBoard, err := board.LoadBoard(source)
	if err != nil {
		return ImportResult{}, err
	}
	info, _, err := s.register(api.RegisterProject{Name: name, Remote: remote})
	if err != nil {
		return ImportResult{}, err
	}
	p, err := s.project(info.ID)
	if err != nil {
		return ImportResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var previous importRecord
	if found, err := readJSON(p.root, filepath.Join("control", "import.json"), &previous); err != nil {
		return ImportResult{}, err
	} else if found && previous.Snapshot == snapshot {
		return ImportResult{Project: info, Files: previous.Files, Tasks: previous.Tasks, Snapshot: snapshot, Existing: true}, nil
	}
	current, err := board.LoadBoard(p.board)
	if err != nil {
		return ImportResult{}, err
	}
	if len(current.Entries) > 0 {
		return ImportResult{}, apiError(api.CodeProjectNotEmpty, "project "+name+" already has tasks; an import only fills an empty project")
	}
	staging := filepath.Join(p.root, "board.import")
	if err := os.RemoveAll(staging); err != nil {
		return ImportResult{}, err
	}
	if err := ensureDir(p.root, "board.import"); err != nil {
		return ImportResult{}, err
	}
	for _, rel := range files {
		data, err := fs.ReadRegularFile(source, filepath.Join(source, rel))
		if err != nil {
			return ImportResult{}, err
		}
		target := filepath.Join(staging, rel)
		if err := fs.EnsurePrivateDirectory(p.root, filepath.Dir(target), true); err != nil {
			return ImportResult{}, err
		}
		if err := fs.WriteBytesAtomicInherited(p.root, target, data, false); err != nil {
			return ImportResult{}, err
		}
	}
	// Empty state directories carry no file; create the full layout.
	if err := board.InitStorage(staging); err != nil {
		return ImportResult{}, err
	}
	stagedFiles, stagedSnapshot, err := snapshotTree(staging)
	if err != nil {
		return ImportResult{}, err
	}
	if stagedSnapshot != snapshot || !slices.Equal(stagedFiles, files) {
		return ImportResult{}, errors.New("staged board differs from the source; nothing was published")
	}
	staged, err := board.LoadBoard(staging)
	if err != nil {
		return ImportResult{}, err
	}
	if len(staged.Entries) != len(sourceBoard.Entries) {
		return ImportResult{}, errors.New("staged board lists another task set; nothing was published")
	}
	if code, _, problems, err := board.CheckBoard(staging, nil, true); err != nil || code != 0 {
		return ImportResult{}, fmt.Errorf("staged board fails check (%s); nothing was published: %w", strings.Join(problems, "; "), err)
	}
	if err := os.RemoveAll(p.board); err != nil {
		return ImportResult{}, err
	}
	// Both directories are siblings the server created in its own private
	// project directory; fs.Rename only renames below board state directories.
	if fs.IsReparsePoint(staging) || fs.IsReparsePoint(p.root) {
		return ImportResult{}, errors.New("project directory contains a reparse point")
	}
	if err := os.Rename(staging, p.board); err != nil {
		return ImportResult{}, err
	}
	record := importRecord{Snapshot: snapshot, Source: source, Files: len(files), Tasks: len(staged.Entries), ImportedAt: s.now()}
	if err := writeJSON(p.root, filepath.Join("control", "import.json"), record); err != nil {
		return ImportResult{}, err
	}
	// Claimed cards of the old board have no claim record; they read as lost
	// generation 0 and need an explicit takeover.
	if err := p.reconcileClaims(); err != nil {
		return ImportResult{}, err
	}
	p.log().Info("board imported", "files", record.Files, "tasks", record.Tasks, "snapshot", snapshot)
	return ImportResult{Project: info, Files: record.Files, Tasks: record.Tasks, Snapshot: snapshot}, nil
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
