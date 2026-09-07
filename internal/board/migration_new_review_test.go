package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/fs"
)

func TestMigrationIdleStrayFileDoesNotBlockInit(t *testing.T) {
	root := tempBoard(t)
	legacyMigrationCard(t, root, "backlog", linkD, true, "# Ready\n- TYPE: Chore\n- SIZE: small\n")
	path := filepath.Join(root, "backlog", "notes.md")
	if err := os.WriteFile(path, []byte("ordinary notes"), 0600); err != nil {
		t.Fatal(err)
	}
	if n, err := MigrateCards(root, InitOptions{}); err != nil || n != 0 {
		t.Fatalf("idle init: %d %v", n, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "ordinary notes" {
		t.Fatal("stray changed")
	}
	legacyMigrationCard(t, root, "backlog", linkA, false, "# Needs migration\n")
	if _, err := MigrateCards(root, InitOptions{}); err == nil || !strings.Contains(err.Error(), "kander check") {
		t.Fatalf("migration should report structure: %v", err)
	}
}

func TestMigrationRecoveryPrecedesUnrelatedStructureFailure(t *testing.T) {
	root := tempBoard(t)
	r := migrationRecord(t, root)
	missing := filepath.Join(root, "done", "20260907-missing-spec-task")
	if err := os.Mkdir(missing, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("missing spec silently accepted")
	}
	data, err := os.ReadFile(filepath.Join(root, r.Migrations[0].To, "spec.md"))
	if err != nil || string(data) != r.Migrations[0].After {
		t.Fatalf("recovery did not precede structural failure: %v", err)
	}
	records, err := operationRecords(root)
	if err != nil || records[0].Phase != "committed" {
		t.Fatal("recovery not committed")
	}
	if _, err := os.Stat(missing); err != nil {
		t.Fatal("unknown directory removed")
	}
}

func TestCheckAggregatesUnreadableDocuments(t *testing.T) {
	root := tempBoard(t)
	for _, id := range []string{linkA, linkB} {
		path := legacyMigrationCard(t, root, "backlog", id, true, "# Placeholder\n")
		if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	code, summary, problems, err := CheckBoard(root, nil, true)
	if err != nil || code != 1 || summary == "" || len(problems) != 2 {
		t.Fatalf("lost diagnostics: %d %q %v %v", code, summary, problems, err)
	}
}

func TestStructuralScanDoesNotMislabelSize(t *testing.T) {
	root := tempBoard(t)
	legacyMigrationCard(t, root, "backlog", linkA, true, "# Small\n- TYPE: Chore\n- SIZE: small\n")
	b, err := scan(root)
	if err != nil || b.Entries[linkA].Kind != "" {
		t.Fatal("structural scan pretends to know size")
	}
	targeted, _ := scanTargetsOnce(root, []string{linkA})
	if targeted.Entries[linkA].Kind != "" {
		t.Fatal("target scan mislabeled size")
	}
	published, err := Scan(root)
	if err != nil || published.Entries[linkA].Kind != "small" {
		t.Fatal("public snapshot lost size")
	}
}

func TestLegacyDirectoryGuardAndMissingTypeLayout(t *testing.T) {
	root := tempBoard(t)
	legacyMigrationCard(t, root, "backlog", linkA, false, "# Original\n")
	verdict, err := GuardWrite(root, filepath.Join(root, "backlog", linkA, "plan.md"))
	if err != nil || verdict.Allowed || !strings.Contains(verdict.Reason, "init") {
		t.Fatal(verdict, err)
	}
	for _, body := range []string{"# Title\nbody\n", "# Title\r\nbody\r\n", "# Title"} {
		got := addSize(body, "small")
		if !strings.HasPrefix(got, "# Title") || !strings.Contains(got, "SIZE: small") {
			t.Fatal(got)
		}
	}
}

func TestMigrationUnknownArtifactsStillBlockIdleInit(t *testing.T) {
	root := tempBoard(t)
	if err := ensureControl(root); err != nil {
		t.Fatal(err)
	}
	path := control(root, "migrations", "unknown")
	if err := fs.CreatePrivateDirectory(root, path); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("unknown artifact silently allowed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("unknown artifact removed")
	}
}
