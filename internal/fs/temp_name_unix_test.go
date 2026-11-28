//go:build unix

package fs

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestWriteTextAtomicSameClockWritersUseDistinctTempNames reproduces two
// writers of one process that read the same clock value, as on macOS, whose
// clock counts microseconds: the first writer's temporary sibling is still in
// place when the second one creates its own. The second write must not fail
// with "protected path already exists", and it must not touch the first
// writer's temporary file.
func TestWriteTextAtomicSameClockWritersUseDistinctTempNames(t *testing.T) {
	previous := unixNano
	unixNano = func() int64 { return 1_789_464_141_307_356_000 }
	t.Cleanup(func() { unixNano = previous })

	root := t.TempDir()
	target := filepath.Join(root, "projects", "p", "project.json")
	if err := EnsurePrivateDirectory(root, filepath.Dir(target), true); err != nil {
		t.Fatal(err)
	}
	inFlight := filepath.Join(filepath.Dir(target), tempSiblingName(filepath.Base(target)))
	if err := os.WriteFile(inFlight, []byte("first writer\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteTextAtomic(root, target, "second writer\n", false); err != nil {
		t.Fatalf("write beside a same-clock temporary file: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "second writer\n" {
		t.Fatalf("target = %q, %v", data, err)
	}
	if data, err := os.ReadFile(inFlight); err != nil || string(data) != "first writer\n" {
		t.Fatalf("first writer's temporary file = %q, %v", data, err)
	}

	// internal/board recognizes interrupted writes by this name shape.
	shape := regexp.MustCompile(`^\.project\.json\.[1-9][0-9]{0,9}\.[1-9][0-9]{0,18}\.tmp$`)
	if name := tempSiblingName("project.json"); !shape.MatchString(name) {
		t.Fatalf("temporary name %q lost the remnant shape", name)
	}
}
