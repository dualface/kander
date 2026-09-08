package board

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkJournalHistory compares identical cards with no history, the legacy
// mixed journal, and partitioned history (825 records, approximately 86 MB).
func BenchmarkJournalHistory(b *testing.B) {
	for _, layout := range []string{"empty", "legacy", "partitioned"} {
		b.Run(layout, func(b *testing.B) {
			root := b.TempDir()
			id := "20260908-benchmark-task"
			for _, state := range States {
				if err := os.Mkdir(filepath.Join(root, state), 0700); err != nil {
					b.Fatal(err)
				}
			}
			if err := ensureControl(root); err != nil {
				b.Fatal(err)
			}
			path := filepath.Join(root, "backlog", id)
			if err := os.Mkdir(path, 0700); err != nil {
				b.Fatal(err)
			}
			text := "# Benchmark\n- TYPE: Chore\n- SIZE: small\n"
			if err := os.WriteFile(filepath.Join(path, "spec.md"), []byte(text), 0600); err != nil {
				b.Fatal(err)
			}
			if layout != "empty" {
				partition := ""
				if layout == "partitioned" {
					partition = "committed"
				}
				payload := strings.Repeat("x", 104000)
				for i := 0; i < 825; i++ {
					r := OperationRecord{Schema: 1, ID: fmt.Sprintf("history-%04d", i), Phase: "committed", Revisions: map[string]uint64{id: uint64(i + 1)}, Files: []FileChange{{Path: "backlog/" + id + "/spec.md", After: payload}}}
					if err := writeJSON(root, control(root, "operations", partition, r.ID+".json"), r, false); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.Run("show", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := ReadSnapshot(root, id); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("list", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := Scan(root); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
