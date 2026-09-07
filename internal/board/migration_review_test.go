package board

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/fs"
)

func TestMigrationPreservesUnregisteredAtomicTemporary(t *testing.T) {
	root := tempBoard(t)
	r := migrationRecord(t, root)
	interrupted := errors.New("interrupted inside unrecorded atomic replacement")
	stage := control(root, "migrations", r.ID, filepath.Base(r.Migrations[0].To))
	err := applyRecordWithCheckpoint(root, control(root, "operations", r.ID+".json"), &r, func(at string) error {
		if at != "migration-source" {
			return nil
		}
		f, e := fs.OpenAppendFile(root, filepath.Join(stage, ".spec.md.123.456.tmp"))
		if e != nil {
			return e
		}
		if _, e = f.WriteString("partial"); e != nil {
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
		return interrupted
	})
	if !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = MigrateCards(root, InitOptions{}); err == nil {
			t.Fatal("unknown temporary unexpectedly recovered")
		}
	}
	if _, err = os.Stat(filepath.Join(root, r.Migrations[0].From)); !os.IsNotExist(err) {
		t.Fatal("expected source already removed")
	}
	if _, err = os.Stat(filepath.Join(stage, ".spec.md.123.456.tmp")); err != nil {
		t.Fatal("unknown artifact was deleted")
	}
	t.Log("Unregistered leftovers from older atomic writes remain conflicts; only journal-bound scratch files can be recovered")
}

func TestMigrationResumesOnlyMatchingPartialRewrite(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		root := tempBoard(t)
		r := migrationRecord(t, root)
		stopped := errors.New("stop before writing replacement")
		if err := applyRecordWithCheckpoint(root, control(root, "operations", r.ID+".json"), &r, func(at string) error {
			if at == "migration-write-created" {
				return stopped
			}
			return nil
		}); !errors.Is(err, stopped) {
			t.Fatal(err)
		}
		stage := control(root, "migrations", r.ID, filepath.Base(r.Migrations[0].To))
		rewrite, _ := migrationWriteNames(r.ID, r.Migrations[0])
		partial := r.Migrations[0].After[:15]
		if corrupt {
			partial = "unrelated bytes"
		}
		file, err := fs.OpenAppendFile(root, filepath.Join(stage, rewrite))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.WriteString(partial); err != nil {
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
		if corrupt {
			if _, err := MigrateCards(root, InitOptions{}); err == nil {
				t.Fatal("unrelated content adopted")
			}
			data, err := fs.ReadRegularFile(root, filepath.Join(stage, rewrite))
			if err != nil || string(data) != partial {
				t.Fatal("conflicting bytes removed")
			}
		} else {
			assertMigrationRecovery(t, root, "migration-write-created")
		}
	}
}

func TestMigrationLegacyRecordUsesOperationBoundScratchNames(t *testing.T) {
	root := tempBoard(t)
	r := migrationRecord(t, root)
	r.Migrations[0].Rewrite = ""
	r.Migrations[0].Original = ""
	path := control(root, "operations", r.ID+".json")
	if err := writeOperation(root, path, r, true); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("legacy record interrupted with named scratch")
	if err := applyRecordWithCheckpoint(root, path, &r, func(at string) error {
		if at == "migration-write-created" {
			return stop
		}
		return nil
	}); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	assertMigrationRecovery(t, root, "migration-write-created")
}
