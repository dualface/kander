package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fillSections(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, replacement := range []string{"实现目标", "产生可验证结果", "满足验收", "无额外范围"} {
		text = strings.Replace(text, "<填写>", replacement, 1)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendDiscussion(t *testing.T, path, line string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "## 讨论与决策\n", "## 讨论与决策\n\n"+line+"\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func moveToTodo(t *testing.T, root, taskID string) error {
	t.Helper()
	loaded, err := LoadBoard(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := Locate(loaded, taskID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = MoveEntry(entry, root, "todo")
	return err
}

func TestTodoGateRequiresSelfReviewRecord(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("gate-self")
	capture(t, func() int { return RunNew([]string{"chore", "gate-self", "自审门禁"}) })
	path := filepath.Join(root, "backlog", taskID+".md")
	fillSections(t, path)

	// Complete contract but no self-review record: rejected.
	if err := moveToTodo(t, root, taskID); err == nil || !strings.Contains(err.Error(), "自审") {
		t.Fatalf("expected self-review gate, got %v", err)
	}
	// Accepted once the self-review line is added.
	appendDiscussion(t, path, "自审: 通过, 契约与用户目标一致")
	if err := moveToTodo(t, root, taskID); err != nil {
		t.Fatal(err)
	}
}

func TestTodoGateRequiresCardReviewForGroupAndLarge(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)

	groupID := todayID("gate-group")
	capture(t, func() int { return RunNew([]string{"chore", "gate-group", "组员卡门禁"}) })
	groupPath := filepath.Join(root, "backlog", groupID+".md")
	fillSections(t, groupPath)
	setMeta(t, groupPath, "- 任务组:\n", "- 任务组: 20260906-demo-group\n")
	appendDiscussion(t, groupPath, "自审: 通过")
	if err := moveToTodo(t, root, groupID); err == nil || !strings.Contains(err.Error(), "卡审") {
		t.Fatalf("group member card must require card review, got %v", err)
	}
	appendDiscussion(t, groupPath, "卡审: 独立复核通过")
	if err := moveToTodo(t, root, groupID); err != nil {
		t.Fatal(err)
	}

	largeID := todayID("gate-large")
	capture(t, func() int { return RunNew([]string{"--large", "chore", "gate-large", "大卡门禁"}) })
	largeSpec := filepath.Join(root, "backlog", largeID, "spec.md")
	fillSections(t, largeSpec)
	appendDiscussion(t, largeSpec, "自审: 通过")
	if err := moveToTodo(t, root, largeID); err == nil || !strings.Contains(err.Error(), "卡审") {
		t.Fatalf("large card must require card review, got %v", err)
	}
	appendDiscussion(t, largeSpec, "- 卡审: 独立复核通过")
	if err := moveToTodo(t, root, largeID); err != nil {
		t.Fatal(err)
	}
}

func TestTodoGateRequiresAcceptanceCheckbox(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("gate-accept")
	capture(t, func() int { return RunNew([]string{"chore", "gate-accept", "验收条目门禁"}) })
	path := filepath.Join(root, "backlog", taskID+".md")
	fillSections(t, path)
	appendDiscussion(t, path, "自审: 通过")
	// Rewrite the acceptance criteria as plain text with no checklist item.
	data, _ := os.ReadFile(path)
	text := strings.Replace(string(data), "- [ ] 满足验收", "尽量做好", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveToTodo(t, root, taskID); err == nil || !strings.Contains(err.Error(), "- [ ]") {
		t.Fatalf("expected acceptance checkbox gate, got %v", err)
	}
}

func TestTodoGateIgnoresEmptyCRLFGroupMetadata(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	taskID := todayID("gate-crlf")
	capture(t, func() int { return RunNew([]string{"chore", "gate-crlf", "CRLF 门禁"}) })
	path := filepath.Join(root, "backlog", taskID+".md")
	fillSections(t, path)
	appendDiscussion(t, path, "自审: 通过")
	// Card saved with CRLF: an empty task-group field must not be treated as a group member needing card review.
	data, _ := os.ReadFile(path)
	crlf := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n", "\r\n")
	if err := os.WriteFile(path, []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveToTodo(t, root, taskID); err != nil {
		t.Fatalf("CRLF card with empty group demanded extra records: %v", err)
	}
}

func TestCheckFlagsContractDefectsForCommittedCards(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)

	// Backlog card with placeholders: not checked.
	capture(t, func() int { return RunNew([]string{"chore", "draft-card", "草稿"}) })

	// Working card that still has a placeholder marker and no acceptance item: report each contract defect.
	badID := todayID("bad-working")
	bad := "# 缺陷卡\n\n- 类型: Chore\n- 任务组:\n- 创建时间: 2026-09-06 03:00\n- 负责人:\n- 会话:\n- 窗口:\n- 开始时间:\n- 完成时间:\n- 任务分支:\n- 结果:\n\n" +
		"## 任务目标\n\n<填写>\n\n## 用户决策\n\nN/A\n\n## 预期成果\n\n成果\n\n## 验收条件\n\n尽量做好\n\n## 威胁模型\n\nN/A\n\n## 不在本轮范围\n\n- 无\n\n## 讨论与决策\n\n自审: 通过\n\n## 实施与验证\n\n\n## 完成总结\n"
	if err := os.WriteFile(filepath.Join(root, "working", badID+".md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errLines, err := CheckBoard(root, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(errLines, "\n")
	if code != 1 || !strings.Contains(joined, badID) || !strings.Contains(joined, "任务目标") || !strings.Contains(joined, "- [ ]") {
		t.Fatalf("code=%d out=%q err=%q", code, out, joined)
	}
	if strings.Contains(joined, "draft-card") {
		t.Fatalf("backlog draft must not be flagged: %s", joined)
	}
}
