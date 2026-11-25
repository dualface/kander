package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/api"
)

func BenchmarkTaskList2000(b *testing.B) {
	s, err := Open(Options{DataDir: b.TempDir(), Logger: quietIndexLogger()})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	info, _, err := s.register(api.RegisterProject{Name: "benchmark"})
	if err != nil {
		b.Fatal(err)
	}
	p, err := s.project(info.ID)
	if err != nil {
		b.Fatal(err)
	}
	// Seed the canonical file layout without paying one server write for each
	// fixture card. Both query paths below return the same unpaginated list.
	for i := range 2000 {
		name := fmt.Sprintf("20261125-bench-%04d-task", i)
		path := filepath.Join(p.board, "backlog", name)
		if err := os.Mkdir(path, 0o700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "spec.md"), []byte("# Benchmark task\n\n- TYPE: Chore\n- SIZE: small\n"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	if err := p.reconcileIndex(); err != nil {
		b.Fatal(err)
	}
	b.Run("file scan", func(b *testing.B) {
		for b.Loop() {
			out, err := p.scanList(b.Context())
			if err != nil || len(out.Tasks) != 2000 {
				b.Fatalf("scan returned %d tasks: %v", len(out.Tasks), err)
			}
		}
	})
	b.Run("SQLite index", func(b *testing.B) {
		for b.Loop() {
			out, err := p.list(b.Context(), api.TaskListOptions{})
			if err != nil || len(out.Tasks) != 2000 {
				b.Fatalf("index returned %d tasks: %v", len(out.Tasks), err)
			}
		}
	})
}
