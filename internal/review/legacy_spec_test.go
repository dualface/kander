package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
)

const legacySpec = "# 旧卡\n\n- 类型: Bug\n- SIZE: small\n- 创建时间: 2026-09-01 03:00\n- 结果:\n\n## 任务目标\n\n目标\n"

// A spec written before the schema rename is shown to a reviewer, and read for
// its size, with English schema names; the card on disk is not touched.
func TestSpecReadersConvertLegacyCard(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(spec, []byte(legacySpec), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := reviewScaleFromTask(spec); got != "small" {
		t.Fatalf("scale %q", got)
	}
	runtime := filepath.Join(dir, "runtime")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(runtime, "task-spec.md")
	if err := copySpecSnapshot(spec, runtime, copied); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## GOAL") || !strings.Contains(string(data), "- CREATED_AT: 2026-09-01 03:00") || strings.Contains(string(data), "任务目标") {
		t.Fatalf("snapshot not converted:\n%s", data)
	}
	disk, err := os.ReadFile(spec)
	if err != nil || string(disk) != legacySpec {
		t.Fatalf("spec on disk changed: %v", err)
	}
}

// A batch frozen before the schema rename holds the Chinese bytes of a legacy
// card; the run that passes that original again must hash to the same identity.
func TestTaskContextBytesKeepsTheSpellingOfAFrozenBatch(t *testing.T) {
	frozen := board.ReviewDigest([]byte(board.WithoutResolvedRecords(legacySpec)))
	english := board.WithoutResolvedRecords(board.NormalizeLegacyCard(legacySpec))
	for name, tc := range map[string]struct {
		frozen string
		want   string
	}{
		"frozen with the Chinese bytes": {frozen, board.WithoutResolvedRecords(legacySpec)},
		"new batch":                     {"", english},
		"frozen with English names":     {board.ReviewDigest([]byte(english)), english},
	} {
		if got := string(taskContextBytes(legacySpec, tc.frozen)); got != tc.want {
			t.Fatalf("%s: got\n%s\nwant\n%s", name, got, tc.want)
		}
	}
	if got := string(taskContextBytes(english, board.ReviewDigest([]byte(english)))); got != english {
		t.Fatalf("an English card changed:\n%s", got)
	}
}
