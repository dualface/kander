package remote

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// RetainedExecution keeps routing and workspace facts after the secret is
// removed. It grants no authority to renew or edit a task.
type RetainedExecution struct {
	ServerURL   string `json:"server_url"`
	ClientID    string `json:"client_id"`
	ProjectID   string `json:"project_id"`
	TaskID      string `json:"task_id"`
	ExecutionID string `json:"execution_id"`
	Generation  uint64 `json:"generation"`
	Session     string `json:"session,omitempty"`
	Window      string `json:"window,omitempty"`
	Worktree    string `json:"worktree,omitempty"`
	Branch      string `json:"branch,omitempty"`
	BaseCommit  string `json:"base_commit,omitempty"`
}

func retainExecution(home string, c Credential, replace bool) error {
	if c.ResourceKind != "" {
		return nil
	}
	path := filepath.Join(home, "retained-executions", projectNamespace(c.ServerURL, c.ProjectID), c.TaskID, c.ExecutionID+".json")
	if err := fs.EnsurePrivateDirectory(home, filepath.Dir(path), true); err != nil {
		return err
	}
	if !replace {
		_, found, err := fs.ReadRegularFileIfExists(home, path)
		if err != nil || found {
			return err
		}
	}
	value := RetainedExecution{ServerURL: c.ServerURL, ClientID: c.ClientID, ProjectID: c.ProjectID, TaskID: c.TaskID, ExecutionID: c.ExecutionID, Generation: c.Generation, Session: c.Session, Window: c.Window, Worktree: c.Worktree, Branch: c.Branch, BaseCommit: c.BaseCommit}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return fs.WriteTextAtomic(home, path, string(data), true)
}

// Retained resolves only this host's exact public execution. A process bound
// to a predecessor cannot use it to clean up a successor's terminal.
func (s *Session) Retained(task api.Task) (RetainedExecution, error) {
	var value RetainedExecution
	if task.Claim == nil || task.Claim.ClientID != s.Client.ClientID() || !api.ValidIdentity(task.TaskID) || !api.ValidIdentity(task.Claim.ExecutionID) {
		return value, errors.New(config.Text("remote.local_execution_required"))
	}
	if bound := os.Getenv(EnvExecutionID); bound != "" && bound != task.Claim.ExecutionID {
		return value, errors.New(config.Text("remote.local_execution_required"))
	}
	credential, found, err := s.Credential(task.TaskID)
	if err != nil {
		return value, err
	}
	if found && credential.ExecutionID == task.Claim.ExecutionID && credential.Generation == task.Claim.Generation {
		return RetainedExecution{ServerURL: credential.ServerURL, ClientID: credential.ClientID, ProjectID: credential.ProjectID, TaskID: credential.TaskID, ExecutionID: credential.ExecutionID, Generation: credential.Generation, Session: credential.Session, Window: credential.Window, Worktree: credential.Worktree, Branch: credential.Branch, BaseCommit: credential.BaseCommit}, nil
	}
	path := filepath.Join(s.Home, "retained-executions", projectNamespace(s.ServerURL, s.Info.ID), task.TaskID, task.Claim.ExecutionID+".json")
	data, err := fs.ReadRegularFile(s.Home, path)
	if err != nil {
		return value, err
	}
	if json.Unmarshal(data, &value) != nil || value.ServerURL != s.ServerURL || value.ClientID != s.Client.ClientID() || value.ProjectID != s.Info.ID || value.TaskID != task.TaskID || value.ExecutionID != task.Claim.ExecutionID || value.Generation != task.Claim.Generation {
		return RetainedExecution{}, errors.New(config.Text("remote.local_execution_required"))
	}
	return value, nil
}

// SceneIsLocal requires this host's exact saved execution routing. A public
// client ID alone is insufficient: a released recovery generation may retain
// the predecessor's WINDOW as evidence without owning that terminal.
func (s *Session) SceneIsLocal(task string, claim *api.ClaimView, window, session string) bool {
	if window == "" || session == "" {
		return false
	}
	local, err := s.Retained(api.Task{TaskID: task, Claim: claim})
	return err == nil && local.Window == window && local.Session == session
}
