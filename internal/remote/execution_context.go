package remote

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/fs"
)

// ExecutionContext retains launcher facts before an orchestrator claims a
// group. It is private local routing data, never a board or service credential.
type ExecutionContext struct {
	ServerURL   string `json:"server_url"`
	ClientID    string `json:"client_id"`
	ProjectID   string `json:"project_id"`
	ExecutionID string `json:"execution_id"`
	AgentPID    int    `json:"agent_pid,omitzero"`
	AgentStart  string `json:"agent_start,omitempty"`
	BootID      string `json:"boot_id,omitempty"`
	Window      string `json:"window,omitempty"`
	Session     string `json:"session,omitempty"`
}

func executionContextPath(home, project, execution string) string {
	return filepath.Join(home, "sessions", project, execution+".json")
}
func SaveExecutionContext(home string, value ExecutionContext) error {
	if !api.ValidIdentity(value.ProjectID) || !api.ValidIdentity(value.ExecutionID) || !api.ValidIdentity(value.ClientID) {
		return errors.New("invalid local execution context")
	}
	path := executionContextPath(home, value.ProjectID, value.ExecutionID)
	if err := fs.EnsurePrivateDirectory(home, filepath.Dir(path), true); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return fs.WriteTextAtomic(home, path, string(data), true)
}
func RefreshExecutionContext(home string, c *Credential) error {
	if c.ResourceKind != "group" {
		return nil
	}
	data, found, err := fs.ReadRegularFileIfExists(home, executionContextPath(home, c.ProjectID, c.ExecutionID))
	if err != nil {
		return err
	}
	var value ExecutionContext
	if found {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if value.ServerURL != c.ServerURL || value.ClientID != c.ClientID || value.ProjectID != c.ProjectID || value.ExecutionID != c.ExecutionID {
			return errors.New("local execution context identity conflict")
		}
	} else {
		// An executing task may coordinate its own group. Its fixed execution ID
		// binds both leases to that same managed agent, never a latest pointer.
		credentials, err := Credentials(home)
		if err != nil {
			return err
		}
		for _, other := range credentials {
			if other.ResourceKind == "" && other.ProjectID == c.ProjectID && other.ExecutionID == c.ExecutionID && other.ServerURL == c.ServerURL && other.ClientID == c.ClientID {
				value = ExecutionContext{AgentPID: other.AgentPID, AgentStart: other.AgentStart, BootID: other.BootID, Window: other.Window, Session: other.Session}
				break
			}
		}
	}
	if c.AgentPID == 0 {
		c.AgentPID = value.AgentPID
	}
	for _, pair := range [][2]*string{{&c.AgentStart, &value.AgentStart}, {&c.BootID, &value.BootID}, {&c.Window, &value.Window}, {&c.Session, &value.Session}} {
		if *pair[0] == "" {
			*pair[0] = *pair[1]
		}
	}
	return nil
}
