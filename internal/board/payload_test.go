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
	path := filepath.Join(root, "backlog", taskID+".md")

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
