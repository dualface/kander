package board

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportAtomicRemnantRecognizesOnlyInterruptedWrites(t *testing.T) {
	const card = "done/20260901-bench-card-task"
	for rel, target := range map[string]string{
		".kander/groups/00000000-review-archive-group/runs/r/originals/.prompt.txt.3727038.1789464141307356827.tmp": ".kander/groups/00000000-review-archive-group/runs/r/originals/prompt.txt",
		".KANDER/versions/.x.json.1.2.tmp":       ".KANDER/versions/x.json",
		card + "/.spec.md.9999999999.1.tmp":      card + "/spec.md",
		card + "/attachments/.a.b.txt.12.34.tmp": card + "/attachments/a.b.txt",
		card + "/..hidden.1.2.tmp":               card + "/.hidden",
	} {
		if got, ok := ImportAtomicRemnant(rel); !ok || got != target {
			t.Errorf("%s: got %q %v, want %q", rel, got, ok, target)
		}
	}
	for _, rel := range []string{
		".kander/versions/.prompt.txt.tmp",
		".kander/versions/.prompt.txt.abc.123.tmp",
		".kander/versions/prompt.txt.1.2.tmp",
		".kander/versions/.prompt.txt.0.2.tmp",
		".kander/versions/..1.2.tmp",
		card + "/.prompt.txt.1.0.tmp",
		card + "/.prompt.txt.12345678901.2.tmp",
		card + "/.prompt.txt.1.2.TMP",
		".a.1.2.tmp",
		"backlog/.a.1.2.tmp",
		"backlog/notes/.a.1.2.tmp",
		"unknown/20260901-bench-card-task/.a.1.2.tmp",
	} {
		if target, ok := ImportAtomicRemnant(rel); ok {
			t.Errorf("%s was taken for a remnant of %q", rel, target)
		}
	}
}

func TestRecoverTransactionsLeavesAtomicWriteRemnants(t *testing.T) {
	root := t.TempDir()
	if err := InitStorage(root); err != nil {
		t.Fatal(err)
	}
	cardPath, err := NewTask(root, "feature", "kept", "Kept", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	remnants := map[string]string{
		filepath.Join(root, ".kander", "versions", ".x.json.1.2.tmp"): "record bytes",
		filepath.Join(cardPath, ".spec.md.3.4.tmp"):                   "card bytes",
	}
	for path, text := range remnants {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RecoverTransactions(root); err != nil {
		t.Fatal(err)
	}
	for path, text := range remnants {
		if data, err := os.ReadFile(path); err != nil || string(data) != text {
			t.Fatalf("%s after recovery: %q %v", path, data, err)
		}
	}
}
