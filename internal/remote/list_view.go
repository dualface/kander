package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

type listTask struct {
	board.ListTask
	Claim *api.ClaimView `json:"claim,omitempty"`
}

type listSnapshot struct {
	SchemaVersion int        `json:"schema_version"`
	ChangeToken   string     `json:"change_token"`
	GeneratedAt   string     `json:"generated_at"`
	Root          string     `json:"root"`
	ProjectID     string     `json:"project_id"`
	Cursor        string     `json:"cursor"`
	Tasks         []listTask `json:"tasks"`
}

// machineList retains the CLI snapshot contract used by dsh. Terminal facts
// are observed only for this host's claims; a foreign WINDOW is never probed.
func (s *Session) machineList(ctx context.Context, list api.TaskList, observe board.AgentStatusFunc) (listSnapshot, error) {
	out := listSnapshot{SchemaVersion: board.ListSnapshotSchemaVersion, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Root: s.Location(""), ProjectID: s.Info.ID, Cursor: list.Cursor, Tasks: []listTask{}}
	var requests []board.AgentStatusRequest
	var positions []int
	for _, item := range list.Tasks {
		task := listTask{ListTask: board.ListTask{TaskID: item.TaskID, Title: item.Title, State: item.State, Size: item.Kind, Type: item.Type, TaskGroup: item.TaskGroup, Owner: item.Assignee, CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, FinishedAt: item.CompletedAt, Result: item.Result, Revision: item.Revision}, Claim: item.Claim}
		if s.SceneIsLocal(item.TaskID, item.Claim, item.Window, item.Session) {
			task.Session, task.Window = item.Session, item.Window
			task.Launcher, _, _ = strings.Cut(item.Window, ":")
			if observe != nil && item.Claim.Status == api.ClaimActive && item.Window != "" {
				positions = append(positions, len(out.Tasks))
				requests = append(requests, board.AgentStatusRequest{TaskID: item.TaskID, Session: item.Session, Window: item.Window})
			}
		}
		if item.Claim != nil && (item.Claim.Status == api.ClaimLost || observe != nil && item.Claim.Observation != nil) {
			agent := ClaimAgent(item.Claim, list.ObservedAt, time.Now())
			task.Agent = &agent
		}
		out.Tasks = append(out.Tasks, task)
	}
	if len(requests) != 0 {
		agents := observe(ctx, requests)
		for i, agent := range agents {
			if i < len(positions) {
				copy := agent
				out.Tasks[positions[i]].Agent = &copy
			}
		}
	}
	// Observation timestamps do not make an otherwise unchanged board dirty.
	rows := append([]listTask(nil), out.Tasks...)
	for i := range rows {
		if rows[i].Agent != nil {
			agent := *rows[i].Agent
			agent.ObservedAt = ""
			rows[i].Agent = &agent
		}
		if rows[i].Claim != nil {
			claim := *rows[i].Claim
			claim.RenewedAt, claim.ExpiresAt = time.Time{}, time.Time{}
			if claim.Observation != nil {
				observation := *claim.Observation
				observation.ObservedAt = time.Time{}
				claim.Observation = &observation
			}
			rows[i].Claim = &claim
		}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return listSnapshot{}, err
	}
	digest := sha256.Sum256(raw)
	out.ChangeToken = hex.EncodeToString(digest[:])
	return out, nil
}
