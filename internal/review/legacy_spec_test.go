package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
