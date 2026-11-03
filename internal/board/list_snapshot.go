package board

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// ListSnapshotSchemaVersion is the schema_version of `kander list --json`.
// See docs/board-snapshot.md for the contract.
const ListSnapshotSchemaVersion = 1

// ListSnapshot is the machine-readable board snapshot of `kander list --json`.
type ListSnapshot struct {
	SchemaVersion int        `json:"schema_version"`
	ChangeToken   string     `json:"change_token"`
	GeneratedAt   string     `json:"generated_at"`
	Root          string     `json:"root"`
	Tasks         []ListTask `json:"tasks"`
}

// ListTask is one card of a ListSnapshot. Every string field is always
// present; a missing value is the empty string.
type ListTask struct {
	TaskID     string     `json:"task_id"`
	Title      string     `json:"title"`
	State      string     `json:"state"`
	Size       string     `json:"size"`
	Type       string     `json:"type"`
	TaskGroup  string     `json:"task_group"`
	Owner      string     `json:"owner"`
	Session    string     `json:"session"`
	Window     string     `json:"window"`
	Launcher   string     `json:"launcher"`
	CreatedAt  string     `json:"created_at"`
	StartedAt  string     `json:"started_at"`
	FinishedAt string     `json:"finished_at"`
	Result     string     `json:"result"`
	Revision   uint64     `json:"revision"`
	Agent      *ListAgent `json:"agent"`
}

// ListAgent is the observed executing agent of one card.
type ListAgent struct {
	Status       string `json:"status"`
	RuntimeState string `json:"runtime_state"`
	Reason       string `json:"reason"`
	ObservedAt   string `json:"observed_at"`
	Valid        bool   `json:"valid"`
}

// AgentStatusRequest names one card whose executing agent a snapshot observes.
type AgentStatusRequest struct {
	TaskID  string
	Session string
	Window  string
}

// AgentStatusFunc observes the requested agents and returns one ListAgent per
// request, in request order. internal/liveness supplies it; board never probes
// terminals itself.
type AgentStatusFunc func(ctx context.Context, requests []AgentStatusRequest) []ListAgent

// BuildListSnapshot turns the board loaded from root into a snapshot,
// optionally limited to one state. A nil observe leaves every agent null and
// runs no probe; otherwise working and review cards with a WINDOW are observed.
func BuildListSnapshot(ctx context.Context, root string, board Board, state string, observe AgentStatusFunc) (ListSnapshot, error) {
	entries := selectedEntries(board.Entries, state)
	tasks := make([]ListTask, 0, len(entries))
	sortKeys := make(map[string]string, len(entries))
	documents := make(map[string]string, len(entries))
	for _, entry := range entries {
		text, err := board.Document(entry.TaskID)
		if err != nil {
			return ListSnapshot{}, err
		}
		revision, err := board.Revision(entry.TaskID)
		if err != nil {
			return ListSnapshot{}, err
		}
		tasks = append(tasks, listTaskOf(entry, text, revision))
		sortKeys[entry.TaskID] = TaskDisplayTime(entry, text)
		documents[entry.TaskID] = text
	}
	// State in board order, then newest TaskDisplayTime, then task ID. Unlike
	// the table, backlog, todo, archived and trash cards sort by CREATED_AT.
	sort.SliceStable(tasks, func(i, j int) bool {
		if ki, kj := sortKeys[tasks[i].TaskID], sortKeys[tasks[j].TaskID]; ki != kj {
			return ki > kj
		}
		return tasks[i].TaskID > tasks[j].TaskID
	})
	sort.SliceStable(tasks, func(i, j int) bool {
		return stateIndex(tasks[i].State) < stateIndex(tasks[j].State)
	})
	if observe != nil {
		observeListAgents(ctx, tasks, observe)
	}
	snapshot := ListSnapshot{
		SchemaVersion: ListSnapshotSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Root:          root,
		Tasks:         tasks,
	}
	token, err := listChangeToken(snapshot, documents)
	if err != nil {
		return ListSnapshot{}, err
	}
	snapshot.ChangeToken = token
	return snapshot, nil
}

func listTaskOf(entry Entry, text string, revision uint64) ListTask {
	summary := TaskSummaryOf(entry, text)
	launcher, _, _ := strings.Cut(summary.Window, ":")
	return ListTask{
		TaskID:     summary.TaskID,
		Title:      summary.Title,
		State:      summary.State,
		Size:       summary.Kind,
		Type:       MetadataFrom(text, FieldType),
		TaskGroup:  summary.TaskGroup,
		Owner:      summary.Assignee,
		Session:    summary.Session,
		Window:     summary.Window,
		Launcher:   launcher,
		CreatedAt:  summary.CreatedAt,
		StartedAt:  summary.StartedAt,
		FinishedAt: summary.CompletedAt,
		// MetadataFrom stays on the field line; resultFrom would read the next
		// line of an empty RESULT field.
		Result:   MetadataFrom(text, FieldResult),
		Revision: revision,
	}
}

// observeListAgents fills the agent of every in-flight card with a WINDOW in
// one batch observation.
func observeListAgents(ctx context.Context, tasks []ListTask, observe AgentStatusFunc) {
	var requests []AgentStatusRequest
	var indexes []int
	for index, task := range tasks {
		if (task.State == "working" || task.State == "review") && task.Window != "" {
			requests = append(requests, AgentStatusRequest{TaskID: task.TaskID, Session: task.Session, Window: task.Window})
			indexes = append(indexes, index)
		}
	}
	if len(requests) == 0 {
		return
	}
	agents := observe(ctx, requests)
	for n, index := range indexes {
		agent := agents[n]
		tasks[index].Agent = &agent
	}
}

// listChangeToken digests the snapshot without generated_at and observed_at,
// plus the full spec.md of every listed card, so a body edit that bypasses
// the revision still changes the token.
func listChangeToken(snapshot ListSnapshot, documents map[string]string) (string, error) {
	type tokenTask struct {
		ListTask
		Document [sha256.Size]byte `json:"document"`
	}
	content := struct {
		SchemaVersion int         `json:"schema_version"`
		Root          string      `json:"root"`
		Tasks         []tokenTask `json:"tasks"`
	}{SchemaVersion: snapshot.SchemaVersion, Root: snapshot.Root, Tasks: make([]tokenTask, len(snapshot.Tasks))}
	for index, task := range snapshot.Tasks {
		if task.Agent != nil {
			agent := *task.Agent
			agent.ObservedAt = ""
			task.Agent = &agent
		}
		content.Tasks[index] = tokenTask{ListTask: task, Document: sha256.Sum256([]byte(documents[task.TaskID]))}
	}
	data, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
