package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardWriteRejectsMissingDirectChildAndReportsMove(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("guard-card")
	capture(t, func() int { return RunNew([]string{"chore", "guard-card", "护栏测试"}) })
	existing := filepath.Join(root, "backlog", taskID+".md")

	// 已存在的卡: 放行.
	if code, _, errOut := capture(t, func() int { return RunGuardWrite([]string{existing}) }); code != 0 {
		t.Fatalf("existing card rejected: %d %s", code, errOut)
	}
	// 状态目录直接子项且不存在, 同 ID 在别处: 拒绝并提示迁移.
	stale := filepath.Join(root, "working", taskID+".md")
	code, _, errOut := capture(t, func() int { return RunGuardWrite([]string{stale}) })
	if code != 1 || !strings.Contains(errOut, taskID) || !strings.Contains(errOut, "backlog") {
		t.Fatalf("stale path: %d %s", code, errOut)
	}
	// 直接子项且全看板无此 ID: 同样拒绝 (新建只能经 kander new).
	fresh := filepath.Join(root, "todo", "20990101-nonexistent-task.md")
	if code, _, errOut := capture(t, func() int { return RunGuardWrite([]string{fresh}) }); code != 1 || !strings.Contains(errOut, "kander new") {
		t.Fatalf("fresh direct child: %d %s", code, errOut)
	}
	// 目录卡内部文件: 目录卡入口存在时放行, 不存在时拒绝 (否则写入会静默重建整个目录卡).
	dirID := todayID("guard-dir")
	if err := os.MkdirAll(filepath.Join(root, "review", dirID), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(root, "review", dirID, "plan.md")
	if code, _, errOut := capture(t, func() int { return RunGuardWrite([]string{inner}) }); code != 0 {
		t.Fatalf("existing directory card inner file rejected: %d %s", code, errOut)
	}
	staleInner := filepath.Join(root, "working", dirID, "report.md")
	if code, _, errOut := capture(t, func() int { return RunGuardWrite([]string{staleInner}) }); code != 1 || !strings.Contains(errOut, "review") {
		t.Fatalf("moved directory card inner file: %d %s", code, errOut)
	}
	if code, _, _ := capture(t, func() int {
		return RunGuardWrite([]string{filepath.Join(root, "working", taskID, "plan.md")})
	}); code != 1 {
		t.Fatal("inner file under missing card entry must be rejected")
	}
	// 看板之外与非状态目录: 放行.
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if code, _, _ := capture(t, func() int { return RunGuardWrite([]string{outside}) }); code != 0 {
		t.Fatal("outside path rejected")
	}
	if code, _, _ := capture(t, func() int { return RunGuardWrite([]string{filepath.Join(root, "notes.md")}) }); code != 0 {
		t.Fatal("non-state kanban file rejected")
	}
	// 缺参数: 用法错误.
	if code, _, _ := capture(t, func() int { return RunGuardWrite(nil) }); code != 2 {
		t.Fatal("missing argument must be a usage error")
	}
}

func TestGuardWriteAllowsWhenBoardIsNotLocatable(t *testing.T) {
	resetLang(t)
	t.Setenv(EnvBoardDir, "")
	dir := t.TempDir()
	t.Chdir(dir)
	target := filepath.Join(dir, "kanban", "working", "20990101-x-task.md")
	if code, _, errOut := capture(t, func() int { return RunGuardWrite([]string{target}) }); code != 0 {
		t.Fatalf("no board must allow: %d %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "kanban")); !os.IsNotExist(err) {
		t.Fatal("guard must not create board directories")
	}
}

func TestShowPrintsCurrentStateAndPath(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("show-location")
	capture(t, func() int { return RunNew([]string{"chore", "show-location", "定位头测试"}) })
	code, out, errOut := capture(t, func() int { return RunShow([]string{taskID}) })
	if code != 0 {
		t.Fatalf("show: %d %s", code, errOut)
	}
	path := filepath.Join(root, "backlog", taskID+".md")
	if !strings.Contains(out, "状态: backlog") || !strings.Contains(out, "路径: "+path) {
		t.Fatalf("missing location header: %s", out)
	}
	if !strings.Contains(out, "# 定位头测试") {
		t.Fatalf("missing document body: %s", out)
	}
}
