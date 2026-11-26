package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/v2/api"
)

func TestImportMergeResumesPublicationOnStartup(t *testing.T) {
	for _, boundary := range []string{"prepared", "backup-renamed", "published", "recorded"} {
		t.Run(boundary, func(t *testing.T) {
			target, _ := newImportFixture(t, "existing")
			source, added := newImportFixture(t, "incoming")
			dir := t.TempDir()
			s, err := Open(Options{DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			p, _ := importProject(t, s, target)
			before := scanImportTestTree(t, p.board)
			s.importFault = func(step string) error {
				if step == boundary {
					return errImportCrash
				}
				return nil
			}
			if _, err := s.Import("demo", "", source); !errors.Is(err, errImportCrash) {
				t.Fatalf("simulated crash: %v", err)
			}
			if boundary == "recorded" {
				// Simulate a second interruption during backup removal.
				if err := os.RemoveAll(filepath.Join(p.root, "board.before-import", "backlog")); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(Options{DataDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			found, err := reopened.Import("demo", "", source)
			if err != nil || !found.Existing || found.AddedTasks != 1 {
				t.Fatalf("recovered receipt: %+v %v", found, err)
			}
			after := scanImportTestTree(t, p.board)
			assertOriginalImportFiles(t, before, after)
			rel := "backlog/" + added + "/spec.md"
			if _, exists := after.sums[rel]; !exists {
				t.Errorf("recovery lost %s", rel)
			}
			for _, name := range []string{"board.import", "board.before-import", "control/import-pending.json"} {
				if _, err := os.Lstat(filepath.Join(p.root, filepath.FromSlash(name))); !os.IsNotExist(err) {
					t.Errorf("recovery artifact %s: %v", name, err)
				}
			}
		})
	}
}

func TestImportMergePreservesOriginalsWhenRecoveryIsImpossible(t *testing.T) {
	target, _ := newImportFixture(t, "existing")
	source, added := newImportFixture(t, "incoming")
	dir := t.TempDir()
	s, err := Open(Options{DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, first := importProject(t, s, target)
	before := scanImportTestTree(t, p.board)
	s.importFault = func(step string) error {
		if step == "backup-renamed" {
			return errImportCrash
		}
		return nil
	}
	if _, err := s.Import("demo", "", source); !errors.Is(err, errImportCrash) {
		t.Fatalf("simulated crash: %v", err)
	}
	staging := filepath.Join(p.root, "board.import")
	writeImportFixture(t, staging, "backlog/"+added+"/spec.md", "changed after preparation")
	wantStaging := scanImportTestTree(t, staging)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(Options{DataDir: dir}); err == nil {
		reopened.Close()
		t.Fatal("opened with an invalid prepared merged board")
	}
	if got := scanImportTestTree(t, filepath.Join(p.root, "board.before-import")); got.digest != before.digest {
		t.Fatal("backup was changed")
	}
	if got := scanImportTestTree(t, staging); got.digest != wantStaging.digest {
		t.Fatal("staging evidence was changed")
	}
	if _, err := os.Lstat(p.board); !os.IsNotExist(err) {
		t.Fatalf("invalid merge published a board: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(p.root, "control", "import-pending.json")); err != nil {
		t.Fatalf("publication evidence was lost: %v", err)
	}
	var record importRecord
	found, err := readJSON(p.root, importRecordPath(first.Snapshot), &record)
	if err != nil || !found {
		t.Fatalf("earlier receipt was lost: %v", err)
	}
}

func TestImportMergeRefusesV2ClaimsAsSourceRecords(t *testing.T) {
	target, _ := newImportFixture(t, "existing")
	source, added := newImportFixture(t, "incoming")
	rel := filepath.ToSlash(servicePath("claims", added+".json"))
	writeImportFixture(t, source, rel[len("board/"):], `{"generation":7}`)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	importProject(t, s, target)
	if _, err := s.Import("demo", "", source); !api.IsCode(err, api.CodeBoardRejected) {
		t.Fatalf("v2 source claim was not rejected: %v", err)
	}
}
