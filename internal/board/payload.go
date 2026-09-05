package board

import (
	"os"
	"sort"
	"time"
)

// TaskSummary 是 tui 扫描用的任务摘要, 字段与 onevoke payload 对齐.
type TaskSummary struct {
	TaskID      string `json:"task_id"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Kind        string `json:"kind"`
	Type        string `json:"type"`
	TaskGroup   string `json:"task_group"`
	Assignee    string `json:"assignee"`
	CreatedAt   string `json:"created_at"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	Time        string `json:"time"`
	Result      string `json:"result"`
	Document    string `json:"document,omitempty"`
}

// BoardView 是只读看板 JSON, 供 tui 使用.
type BoardView struct {
	GeneratedAt string        `json:"generated_at"`
	Root        string        `json:"root"`
	Tasks       []TaskSummary `json:"tasks"`
}

// TaskDisplayTime 对标 onevoke task_display_time.
func TaskDisplayTime(entry Entry, text string) string {
	switch entry.State {
	case "working", "review":
		if value := MetadataFrom(text, "开始时间"); value != "" {
			return value
		}
		return "-"
	case "done":
		if value := MetadataFrom(text, "完成时间"); value != "" {
			return value
		}
		info, err := os.Stat(entry.Document)
		if err == nil {
			return time.Unix(info.ModTime().Unix(), 0).Format("2006-01-02 15:04")
		}
		return "-"
	}
	if value := MetadataFrom(text, "创建时间"); value != "" {
		return value
	}
	return "-"
}

// TaskSummaryOf 从入口和文档构造摘要.
func TaskSummaryOf(entry Entry, text string) TaskSummary {
	kind := entry.Kind
	if kind == "" {
		kind = "small"
	}
	taskType := MetadataFrom(text, "类型")
	if taskType == "" {
		taskType = "-"
	}
	return TaskSummary{
		TaskID:      entry.TaskID,
		Title:       TitleFrom(text),
		State:       entry.State,
		Kind:        kind,
		Type:        taskType,
		TaskGroup:   taskGroupFrom(text),
		Assignee:    MetadataFrom(text, "负责人"),
		CreatedAt:   MetadataFrom(text, "创建时间"),
		StartedAt:   MetadataFrom(text, "开始时间"),
		CompletedAt: MetadataFrom(text, "完成时间"),
		Time:        TaskDisplayTime(entry, text),
		Result:      resultFrom(text),
	}
}

func sortTaskSummaries(tasks []TaskSummary) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Time != tasks[j].Time {
			return tasks[i].Time > tasks[j].Time
		}
		return tasks[i].TaskID > tasks[j].TaskID
	})
	sort.SliceStable(tasks, func(i, j int) bool {
		return stateIndex(tasks[i].State) < stateIndex(tasks[j].State)
	})
}

// BoardPayload 用 Scan 构建 payload, 不经 LoadBoard, 不向终端打 check 警告.
func BoardPayload(root string) (BoardView, error) {
	scanned, err := Scan(root)
	if err != nil {
		return BoardView{}, err
	}
	tasks := make([]TaskSummary, 0, len(scanned.Entries))
	for _, entry := range scanned.Entries {
		text, err := ReadDocument(entry)
		if err != nil {
			return BoardView{}, err
		}
		tasks = append(tasks, TaskSummaryOf(entry, text))
	}
	sortTaskSummaries(tasks)
	return BoardView{
		GeneratedAt: time.Now().Format("2006-01-02 15:04:05"),
		Root:        root,
		Tasks:       tasks,
	}, nil
}

// TaskPayload 返回单卡摘要加正文, 同样走 Scan.
func TaskPayload(root, taskID string) (TaskSummary, error) {
	scanned, err := Scan(root)
	if err != nil {
		return TaskSummary{}, err
	}
	entry, err := Locate(scanned, taskID)
	if err != nil {
		return TaskSummary{}, err
	}
	text, err := ReadDocument(entry)
	if err != nil {
		return TaskSummary{}, err
	}
	summary := TaskSummaryOf(entry, text)
	summary.Document = text
	return summary, nil
}
