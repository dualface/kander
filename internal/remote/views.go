package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

type cachedList struct {
	Server   string       `json:"server"`
	Project  string       `json:"project"`
	Observed time.Time    `json:"observed"`
	List     api.TaskList `json:"list"`
}

func (s *Session) cachePath() string {
	sum := sha256.Sum256([]byte(s.ServerURL + "\x00" + s.Info.ID))
	return filepath.Join(s.Home, "cache", hex.EncodeToString(sum[:])+".json")
}

// Location is a resource identity for display and prompts, never a local path.
func (s *Session) Location(task string) string { return location(s, task) }

// ListView reads the server snapshot. An unreachable server may show a private
// read-only cache, explicitly marked stale. Writes always go to the service.
func (s *Session) ListView(ctx context.Context, allowStale bool) (board.BoardView, error) {
	list, err := s.Project.List(ctx, "")
	observed, stale := time.Now(), false
	if err != nil {
		if !allowStale || !client.IsUnreachable(err) {
			return board.BoardView{}, err
		}
		data, exists, cacheErr := fs.ReadRegularFileIfExists(s.Home, s.cachePath())
		if cacheErr != nil {
			return board.BoardView{}, errors.Join(err, cacheErr)
		}
		if !exists {
			return board.BoardView{}, err
		}
		var cached cachedList
		if cacheErr := json.Unmarshal(data, &cached); cacheErr != nil {
			return board.BoardView{}, errors.Join(err, cacheErr)
		}
		if cached.Server != s.ServerURL || cached.Project != s.Info.ID {
			return board.BoardView{}, errors.New("cached board identity mismatch")
		}
		list, observed, stale = cached.List, cached.Observed, true
	} else {
		data, err := json.Marshal(cachedList{Server: s.ServerURL, Project: s.Info.ID, Observed: observed, List: list})
		if err != nil {
			return board.BoardView{}, err
		}
		path := s.cachePath()
		if err := fs.EnsurePrivateDirectory(s.Home, filepath.Dir(path), true); err != nil {
			return board.BoardView{}, err
		}
		if err := fs.WriteTextAtomic(s.Home, path, string(data), true); err != nil {
			return board.BoardView{}, err
		}
	}
	view := board.BoardView{Root: s.Location(""), GeneratedAt: observed.Format("2006-01-02 15:04:05"), Warnings: list.Warnings, Stale: stale, Cursor: list.Cursor, Tasks: []board.TaskSummary{}}
	if stale {
		view.Warnings = append(view.Warnings, config.Text("remote.stale_snapshot", view.GeneratedAt))
	}
	for _, item := range list.Tasks {
		summary := board.TaskSummary{TaskID: item.TaskID, Title: item.Title, State: item.State, Kind: item.Kind, Type: item.Type, TaskGroup: item.TaskGroup, Assignee: item.Assignee, CreatedAt: item.CreatedAt, StartedAt: item.StartedAt, CompletedAt: item.CompletedAt, Time: item.Time, Result: item.Result}
		if item.Claim != nil {
			summary.ClaimStatus, summary.ClaimClientID = item.Claim.Status, item.Claim.ClientID
			if item.Claim.Observation != nil || item.Claim.Status == api.ClaimLost {
				agent := ClaimAgent(item.Claim, list.ObservedAt, time.Now())
				if stale {
					agent.Status, agent.RuntimeState, agent.Valid = "unknown", "unknown", false
					agent.Reason = config.Text("remote.stale_label")
				}
				summary.AgentObservation = &agent
			}
			// A remote terminal address has meaning only on its execution host.
			if item.Claim.ClientID == s.Client.ClientID() && !stale {
				summary.Session, summary.Window = item.Session, item.Window
			}
		}
		view.Tasks = append(view.Tasks, summary)
	}
	return view, nil
}

// TaskView reads a card body without creating a local card or worktree.
func (s *Session) TaskView(ctx context.Context, id string) (board.TaskSummary, error) {
	task, err := s.Project.Show(ctx, id)
	if err != nil {
		return board.TaskSummary{}, err
	}
	kind := board.MetadataFrom(task.Text, board.FieldSize)
	entry := board.Entry{TaskID: task.TaskID, State: task.State, Kind: kind}
	summary := board.TaskSummaryOf(entry, task.Text)
	summary.Document = task.Text
	if task.Claim != nil {
		summary.ClaimStatus, summary.ClaimClientID = task.Claim.Status, task.Claim.ClientID
		if task.Claim.Observation != nil || task.Claim.Status == api.ClaimLost {
			agent := ClaimAgent(task.Claim, task.ObservedAt, time.Now())
			summary.AgentObservation = &agent
		}
	}
	if task.Claim == nil || task.Claim.ClientID != s.Client.ClientID() {
		summary.Session, summary.Window = "", ""
	}
	return summary, nil
}

// MoveTask uses the process-bound execution credential and server CAS. It
// never queues a mutation for offline replay.
func (s *Session) MoveTask(ctx context.Context, id string, request api.Move) (api.Task, error) {
	credential, found, err := s.Credential(id)
	if err != nil {
		return api.Task{}, err
	}
	var claim *client.Claim
	if found {
		claim = credential.Claim()
	}
	if request.State == "review" && s.Info.Remote != "" {
		current, err := s.Project.Show(ctx, id)
		if err != nil {
			return api.Task{}, err
		}
		if request.ExpectedRevision != nil && *request.ExpectedRevision != current.Revision {
			return api.Task{}, &api.Error{Code: api.CodeRevisionConflict, Message: config.Text("remote.delivery_card_changed")}
		}
		if current.State == "working" {
			if !found {
				return api.Task{}, &api.Error{Code: api.CodeClaimRequired, Message: config.Text("remote.delivery_holder_required")}
			}
			dir, err := os.Getwd()
			if err != nil {
				return api.Task{}, err
			}
			delivery, err := s.PublishCommit(ctx, dir, current.TaskID, request.DeliveryCommit, credential)
			if err != nil {
				return api.Task{}, err
			}
			request.DeliveryCommit = delivery.Commit
			if request.ExpectedRevision == nil {
				revision := delivery.CardRevision
				request.ExpectedRevision = &revision
			}
		}
	}
	return s.Project.Move(ctx, client.NewKey(), id, request, claim)
}

// LocalDataRoot isolates host-only issue evidence and recovery files by server
// project. It contains no card-state directories and is never a board backend.
func (s *Session) LocalDataRoot() (string, error) {
	sum := sha256.Sum256([]byte(s.ServerURL + "\x00" + s.Info.ID))
	root := filepath.Join(s.Home, "projects", hex.EncodeToString(sum[:]))
	if err := fs.EnsurePrivateDirectory(s.Home, root, true); err != nil {
		return "", err
	}
	return root, nil
}
