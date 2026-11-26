package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/fs"
)

func TestImportSyncsTheFinalStagedTreeBeforeThePublicationIntent(t *testing.T) {
	data := t.TempDir()
	s, h := openTest(t, data, nil)
	defer h.Close()
	defer s.Close()
	source := v1Board(t)
	stripSize(t, source) // The staged specs change after the copy.
	intent := filepath.Join(projectRoot(data, "demo"), "control", "import-pending.json")
	var steps []string
	var syncedDigest string
	s.syncImportStage = func(root, dir string, files, dirs []string) error {
		steps = append(steps, "sync")
		if _, err := os.Lstat(intent); !os.IsNotExist(err) {
			t.Errorf("the intent exists before the sync: %v", err)
		}
		var err error
		if _, syncedDigest, err = snapshotTree(dir); err != nil {
			return err
		}
		return fs.SyncTree(root, dir, files, dirs)
	}
	s.importFault = func(step string) error {
		if slices.Contains([]string{"copied", "synced", "prepared"}, step) {
			steps = append(steps, step)
		}
		if step == "prepared" {
			var pending importPublication
			p, err := s.project(mustProject(t, s, "demo"))
			if err != nil {
				return err
			}
			if found, err := readJSON(p.root, filepath.Join("control", "import-pending.json"), &pending); err != nil || !found {
				t.Errorf("intent at prepared: %v %v", found, err)
			}
			if pending.StagedDigest != syncedDigest {
				t.Errorf("the staged tree changed after the sync: intent %s, synced %s", pending.StagedDigest, syncedDigest)
			}
		}
		return nil
	}
	if result, err := s.Import("demo", "", source); err != nil || len(result.DefaultedSize) != 2 {
		t.Fatalf("import: %+v %v", result, err)
	}
	if want := []string{"copied", "sync", "synced", "prepared"}; !slices.Equal(steps, want) {
		t.Fatalf("steps %v, want %v", steps, want)
	}
}

func TestImportCrashAfterTheSyncDiscardsTheStagingOnRestart(t *testing.T) {
	data := t.TempDir()
	s, err := Open(Options{DataDir: data})
	if err != nil {
		t.Fatal(err)
	}
	s.importFault = func(step string) error {
		if step == "synced" {
			return errImportCrash
		}
		return nil
	}
	if _, err := s.Import("demo", "", v1Board(t)); !errors.Is(err, errImportCrash) {
		t.Fatalf("import: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot(data, "demo"), "board.import")); err != nil {
		t.Fatalf("the crash must leave the staging behind: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, h := openTest(t, data, nil)
	defer h.Close()
	defer reopened.Close()
	assertNoStaging(t, data, "demo")
	assertNothingPublished(t, reopened, "demo")
	if _, err := os.Lstat(filepath.Join(projectRoot(data, "demo"), "control", "import-pending.json")); !os.IsNotExist(err) {
		t.Fatalf("intent after the restart: %v", err)
	}
}

func TestParallelCopyFailureStopsTheImport(t *testing.T) {
	data := t.TempDir()
	s, h := openTest(t, data, nil)
	defer h.Close()
	defer s.Close()
	source := v1Board(t)
	card := idleCardDir(t, source)
	for i := range 40 {
		writeImportFixture(t, source, fmt.Sprintf("%s/attachments/n%02d.txt", card, i), "note")
	}
	// The source changes after the scan: the read of that file fails its
	// digest check on a worker while other files are in flight.
	s.importFault = func(step string) error {
		if step == "copying" {
			writeImportFixture(t, source, card+"/attachments/n20.txt", "changed")
		}
		return nil
	}
	if _, err := s.Import("demo", "", source); err == nil || !strings.Contains(err.Error(), "source board changed during import") {
		t.Fatalf("import: %v", err)
	}
	assertNothingPublished(t, s, "demo")
	assertNoStaging(t, data, "demo")
}

// idleCardDir is the board path of the backlog card of v1Board.
func idleCardDir(t *testing.T, source string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(source, "backlog"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("backlog: %v %v", entries, err)
	}
	return "backlog/" + entries[0].Name()
}

func TestLocalImportKeepsAPOSIXNameWithABackslash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a backslash separates path components on Windows")
	}
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	source := v1Board(t)
	rel := idleCardDir(t, source) + `/run\output.txt`
	writeImportFixture(t, source, rel, "evidence")
	assertImportedAsIs(t, s, source, rel)
}

func TestImportCanceledWhilePreparingTheStagingDirectories(t *testing.T) {
	data := t.TempDir()
	s, h := openTest(t, data, nil)
	defer h.Close()
	defer s.Close()
	copied := false
	s.importFault = func(step string) error {
		if step == "copying" {
			copied = true
		}
		return nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The copy phase is reported before its directories are created. A
	// directory created after the cancellation would fail on this file,
	// which holds the name of the first one, instead of ending as a
	// cancellation.
	report := func(p api.ImportProgress) {
		if p.Phase == api.ImportPhaseCopy {
			cancel()
			if err := os.WriteFile(filepath.Join(projectRoot(data, "demo"), "board.import", ".kander"), nil, 0o600); err != nil {
				t.Error(err)
			}
		}
	}
	_, err := s.ImportWithProgress(ctx, "demo", "", v1Board(t), report)
	wantImportCanceled(t, err, importCancelCaller)
	if copied {
		t.Fatal("a file was handed out after the cancellation")
	}
	assertNothingPublished(t, s, "demo")
	assertNoStaging(t, data, "demo")
}
