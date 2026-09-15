package board

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoardPayloadMatchesScanFieldsAndIgnoresInvalidWithoutWarning(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	if code := RunNew([]string{"feature", "web-payload", "中文看板"}); code != 0 {
		t.Fatalf("new code=%d", code)
	}
	taskID := todayID("web-payload")
	if err := os.WriteFile(filepath.Join(root, "backlog", "notes.md"), []byte("随手记"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	payload, err := BoardPayload(root)
	_ = w.Close()
	os.Stderr = oldErr
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("BoardPayload wrote stderr: %s", buf.String())
	}
	if len(payload.Tasks) != 1 {
		t.Fatalf("tasks=%d", len(payload.Tasks))
	}
	task := payload.Tasks[0]
	if task.TaskID != taskID || task.Title != "中文看板" || task.State != "backlog" {
		t.Fatalf("summary %+v", task)
	}
	if task.Kind != "small" || task.Type != "Feature" || task.Document != "" {
		t.Fatalf("kind/type/document %+v", task)
	}
	if task.Time == "" || task.Time == "-" {
		t.Fatalf("time %q", task.Time)
	}

	_, _, stderr := capture(t, func() int { return RunList(nil) })
	if !strings.Contains(stderr, "kander check") {
		t.Fatalf("LoadBoard should warn: %s", stderr)
	}
}

func TestTaskPayloadTaskGroupLegacyAndCurrent(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	capture(t, func() int { return RunNew([]string{"chore", "web-task-group", "分组"}) })
	taskID := todayID("web-task-group")
	path := filepath.Join(root, "backlog", taskID, "spec.md")

	setMeta(t, path, "- TASK_GROUP:\n", "")
	payload, err := BoardPayload(root)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Tasks[0].TaskGroup != "" {
		t.Fatalf("missing group %q", payload.Tasks[0].TaskGroup)
	}

	legacy := "20260820-legacy-web-group"
	setMeta(t, path, "## DISCUSSION\n\n", "## DISCUSSION\n\nTASK_GROUP: "+legacy+"\nPREREQUISITES: N/A\n\n")
	detail, err := TaskPayload(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TaskGroup != legacy {
		t.Fatalf("legacy group %q", detail.TaskGroup)
	}
	if !strings.Contains(detail.Document, "# 分组") {
		t.Fatalf("document %s", detail.Document)
	}

	current := "20260820-current-web-group"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "- TASK_GROUP:") {
		text = strings.Replace(text, "- OWNER:", "- TASK_GROUP: "+current+"\n- OWNER:", 1)
	} else {
		text = strings.Replace(text, "- TASK_GROUP:\n", "- TASK_GROUP: "+current+"\n", 1)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	detail, err = TaskPayload(root, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TaskGroup != current {
		t.Fatalf("current group %q", detail.TaskGroup)
	}
}

func TestTaskPayloadUsesTargetedScan(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	if code := RunNew([]string{"feature", "target-card", "目标卡"}); code != 0 {
		t.Fatalf("new target code=%d", code)
	}
	if code := RunNew([]string{"chore", "other-card", "无关卡"}); code != 0 {
		t.Fatalf("new other code=%d", code)
	}
	targetID := todayID("target-card")
	otherID := todayID("other-card")
	otherPath := filepath.Join(root, "backlog", otherID, "spec.md")
	if err := os.WriteFile(otherPath, []byte("not-utf8\xff\xfe"), 0o644); err != nil {
		t.Fatal(err)
	}

	detail, err := TaskPayload(root, targetID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TaskID != targetID || detail.Title != "目标卡" || !strings.Contains(detail.Document, "# 目标卡") {
		t.Fatalf("detail %+v", detail)
	}
	if strings.Contains(detail.Document, "无关卡") || strings.Contains(detail.Document, "not-utf8") {
		t.Fatal("targeted payload included an unrelated body")
	}

	scanned, err := ScanTargets(root, []string{targetID})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := scanned.Entries[otherID]; exists {
		t.Fatal("targeted scan retained an unrelated entry")
	}
	if _, err := scanned.Document(otherID); err == nil {
		t.Fatal("unrelated document was captured")
	}
	entry, err := Locate(scanned, targetID)
	if err != nil {
		t.Fatal(err)
	}
	text, err := scanned.Document(targetID)
	if err != nil {
		t.Fatal(err)
	}
	want := TaskSummaryOf(entry, text)
	if detail.Title != want.Title || detail.Kind != want.Kind || detail.Type != want.Type || detail.State != want.State {
		t.Fatalf("summary mismatch got %+v want %+v", detail, want)
	}

	board, err := BoardPayload(root)
	if err == nil {
		t.Fatalf("full payload should fail on invalid sibling UTF-8: %+v", board)
	}
}

func TestTaskPayloadKeepsDuplicateAndSizeChecks(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	if code := RunNew([]string{"--large", "feature", "dup-size", "大卡"}); code != 0 {
		t.Fatalf("new code=%d", code)
	}
	id := todayID("dup-size")
	detail, err := TaskPayload(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Kind != "large" || MetadataFrom(detail.Document, FieldLanguage) == "" {
		t.Fatalf("size/language %+v", detail)
	}

	src := filepath.Join(root, "backlog", id)
	data, err := os.ReadFile(filepath.Join(src, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	dup := filepath.Join(root, "todo", id)
	if err := os.Mkdir(dup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dup, "spec.md"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskPayload(root, id); err == nil {
		t.Fatal("duplicate id must fail targeted locate")
	}
}
