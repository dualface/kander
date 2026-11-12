package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func TestImportRecoversEveryDirectoryPublicationBoundary(t *testing.T) {
	for _, step := range []string{"prepared", "backup-renamed", "published", "recorded"} {
		t.Run(step, func(t *testing.T) {
			source := v1Board(t)
			_, snapshot, err := snapshotTree(source)
			if err != nil {
				t.Fatal(err)
			}
			data := t.TempDir()
			srv, err := Open(Options{DataDir: data})
			if err != nil {
				t.Fatal(err)
			}
			interrupted := errors.New("simulated import interruption")
			srv.importFault = func(current string) error {
				if current == step {
					return interrupted
				}
				return nil
			}
			if _, err := srv.Import("recovered", "", source); !errors.Is(err, interrupted) {
				t.Fatalf("boundary %s: %v", step, err)
			}
			if err := srv.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(Options{DataDir: data})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			again, err := reopened.Import("recovered", "", source)
			if err != nil || !again.Existing || again.Snapshot != snapshot || again.Tasks != 2 {
				t.Fatalf("recovered import: %+v %v", again, err)
			}
			p, err := reopened.project(again.Project.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"board.import", "board.before-import", filepath.Join("control", "import-pending.json")} {
				if _, err := os.Lstat(filepath.Join(p.root, name)); !os.IsNotExist(err) {
					t.Fatalf("recovery artifact %s: %v", name, err)
				}
			}
			if _, after, err := snapshotTree(source); err != nil || after != snapshot {
				t.Fatalf("source backup changed: %s %v", after, err)
			}
		})
	}
}
func TestImportRefusesTamperedPreparedSnapshotBeforeReadiness(t *testing.T) {
	source := v1Board(t)
	data := t.TempDir()
	srv, err := Open(Options{DataDir: data})
	if err != nil {
		t.Fatal(err)
	}
	srv.importFault = func(step string) error {
		if step == "prepared" {
			return errors.New("interrupted")
		}
		return nil
	}
	if _, err := srv.Import("tampered", "", source); err == nil {
		t.Fatal("missing interruption")
	}
	info, err := srv.projectByName("tampered")
	if err != nil {
		t.Fatal(err)
	}
	p, err := srv.project(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	view, err := board.LoadBoard(filepath.Join(p.root, "board.import"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range view.Entries {
		if err := os.WriteFile(filepath.Join(entry.Path, "spec.md"), []byte("tampered\n"), 0600); err != nil {
			t.Fatal(err)
		}
		break
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if next, err := Open(Options{DataDir: data}); err == nil {
		next.Close()
		t.Fatal("tampered staging was served")
	}
}
func TestImportHoldsSourceBoardExclusiveWhileCopying(t *testing.T) {
	source := v1Board(t)
	srv, err := Open(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	copied, release := make(chan struct{}), make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	srv.importFault = func(step string) error {
		if step == "copied" {
			close(copied)
			<-release
		}
		return nil
	}
	imported := make(chan error, 1)
	go func() { _, err := srv.ImportContext(t.Context(), "locked", "", source); imported <- err }()
	select {
	case <-copied:
	case err := <-imported:
		t.Fatalf("import did not reach copy: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("copy did not begin")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err = board.WithTransactionContext(ctx, source, board.LockScope{}, func(*board.Transaction) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("source writer bypassed snapshot lock: %v", err)
	}
	close(release)
	released = true
	if err := <-imported; err != nil {
		t.Fatal(err)
	}
}
func TestImportRejectsOverlappingDirectories(t *testing.T) {
	data := t.TempDir()
	srv, err := Open(Options{DataDir: data})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	for _, source := range []string{data, filepath.Join(data, "source"), filepath.Dir(data)} {
		if _, err := srv.Import("overlap", "", source); err == nil || !strings.Contains(err.Error(), "must not overlap") {
			t.Fatalf("overlap %s: %v", source, err)
		}
	}
}

func TestImportRejectsSourceMutationBeforePublication(t *testing.T) {
	source := v1Board(t)
	srv, err := Open(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	srv.importFault = func(step string) error {
		if step != "copied" {
			return nil
		}
		return os.WriteFile(filepath.Join(source, "external-writer.md"), []byte("concurrent source mutation\n"), 0600)
	}
	if _, err := srv.Import("changed-source", "", source); err == nil || !strings.Contains(err.Error(), "source board changed") {
		t.Fatalf("source mutation: %v", err)
	}
	info, err := srv.projectByName("changed-source")
	if err != nil {
		t.Fatal(err)
	}
	p, err := srv.project(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	view, err := board.LoadBoard(p.board)
	if err != nil || len(view.Entries) != 0 {
		t.Fatalf("changed source was published: %+v %v", view.Entries, err)
	}
	if _, err := os.Stat(filepath.Join(p.root, "control", "import-pending.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid source produced publication intent: %v", err)
	}
}
