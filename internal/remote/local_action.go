package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// LocalAction is a private receipt for a physical launch. Its logical identity
// is independent of the caller process and survives a lost command response.
type LocalAction struct {
	Schema        int               `json:"schema"`
	Kind          string            `json:"kind"`
	ExecutionID   string            `json:"execution_id"`
	Phase         string            `json:"phase"`
	ClaimKey      string            `json:"claim_key,omitempty"`
	ClaimInput    *api.ClaimRequest `json:"claim_input,omitempty"`
	PreviousProof *client.Claim     `json:"previous_proof,omitempty"`
	Takeover      bool              `json:"takeover,omitzero"`
	TaskID        string            `json:"task_id,omitempty"`
	Session       string            `json:"session,omitempty"`
	Window        string            `json:"window,omitempty"`
	TaskFile      string            `json:"task_file,omitempty"`
	AgentPID      int               `json:"agent_pid,omitzero"`
	AgentStart    string            `json:"agent_start,omitempty"`
	BootID        string            `json:"boot_id,omitempty"`
	Result        json.RawMessage   `json:"result,omitempty"`
}

type ActionJournal struct {
	Value LocalAction
	Path  string
	save  func() error
	close func() error
}

func (a *ActionJournal) Save() error  { return a.save() }
func (a *ActionJournal) Close() error { return a.close() }

// BeginAction serializes one logical invocation and starts a new attempt over a failed receipt.
// Callers must inspect existing receipts before changing central state or performing any
// terminal operation. BeginActionGated with retryFailed false keeps a failed receipt instead.
func (s *Session) BeginAction(kind string, logical any) (journal *ActionJournal, fresh bool, err error) {
	return s.beginAction(kind, logical, true)
}

// actionName is the file name of the receipt of one logical invocation. Every reader and writer
// of the receipts takes the name from here.
func actionName(kind string, logical any) (string, error) {
	data, err := json.Marshal(struct {
		Kind  string
		Input any
	}{kind, logical})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + ".json", nil
}

// decodeAction reads one stored receipt and checks it: the schema, the kind it was stored for,
// and its execution identity.
func decodeAction(data []byte, kind string) (LocalAction, error) {
	var action LocalAction
	if err := json.Unmarshal(data, &action); err != nil || action.Schema != 1 || action.Kind != kind || !api.ValidIdentity(action.ExecutionID) {
		return LocalAction{}, errors.New(config.Text("remote.launch_receipt_damaged"))
	}
	return action, nil
}

// PeekAction reads the receipt of one logical invocation without creating, locking, or changing
// anything: not even the directory that holds the receipts. found is false when the invocation
// has no receipt yet. A receipt is written atomically, so the read never sees a partial one.
func (s *Session) PeekAction(kind string, logical any) (action LocalAction, found bool, err error) {
	return peekAction(s.Home, filepath.Join(s.Home, "projects", projectNamespace(s.ServerURL, s.Info.ID)), kind, logical)
}

// TaskLaunchFile finds the one receipt for the current physical launch when
// its original invocation arguments are unavailable. The task file name binds
// the prompt to this execution. The lookup neither opens the task file nor
// returns the claim data stored in the receipt.
func (s *Session) TaskLaunchFile(ctx context.Context, credential Credential) (string, error) {
	dir := filepath.Join(s.Home, "projects", projectNamespace(s.ServerURL, s.Info.ID), "launch-actions")
	entries, err := fs.ListDirectory(s.Home, dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var taskFile string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		data, found, err := fs.ReadRegularFileIfExists(s.Home, filepath.Join(dir, entry.Name))
		if err != nil {
			return "", err
		}
		if !found {
			continue
		}
		var action LocalAction
		if err := json.Unmarshal(data, &action); err != nil {
			return "", errors.New(config.Text("remote.launch_receipt_damaged"))
		}
		if action.Kind != "task-launch" || action.ExecutionID != credential.ExecutionID {
			continue
		}
		if taskFile != "" || action.Schema != 1 || action.TaskID != credential.TaskID ||
			action.Window != credential.Window || !filepath.IsAbs(action.TaskFile) {
			return "", errors.New(config.Text("remote.launch_receipt_damaged"))
		}
		taskFile = action.TaskFile
	}
	return taskFile, nil
}

func peekAction(home, root, kind string, logical any) (action LocalAction, found bool, err error) {
	name, err := actionName(kind, logical)
	if err != nil {
		return action, false, err
	}
	data, found, err := fs.ReadRegularFileIfExists(home, filepath.Join(root, "launch-actions", name))
	if err != nil || !found {
		return action, false, err
	}
	action, err = decodeAction(data, kind)
	return action, err == nil, err
}

// ReadOnlyHost is what a command can learn about the receipts of this host and project without
// creating or registering anything: it reads the client identity if there is one, asks the
// service to resolve the project by name (which never registers it), and reads receipts below
// the namespace of that server and project. It exists for the refusals that must come before
// Open, which creates the client identity and registers the project.
type ReadOnlyHost struct {
	home, serverURL, projectID string
}

// LookupHost opens the read-only view for cfg. nothing is true when no receipt of this project
// can exist: the host has no client identity, or the service does not know the project. An error
// means the view could not be determined (the service did not answer after the retries of the
// client, say). The caller reports it as the result of its own call: it must not open the
// session on that basis, because Open registers a project the service does not know.
func LookupHost(ctx context.Context, cfg *config.Config) (host *ReadOnlyHost, nothing bool, err error) {
	if cfg == nil || cfg.Project.Name == "" {
		return nil, false, ErrNotConfigured
	}
	home, err := Home()
	if err != nil {
		return nil, false, err
	}
	data, found, err := fs.ReadRegularFileIfExists(home, filepath.Join(home, "client-id"))
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, true, nil
	}
	id := strings.TrimSpace(string(data))
	if !api.ValidIdentity(id) {
		return nil, false, errors.New("client identity file under " + home + " is damaged")
	}
	serverURL := config.ServerURL(cfg)
	c, err := client.New(serverURL, id)
	if err != nil {
		return nil, false, err
	}
	project, err := c.ProjectByName(ctx, cfg.Project.Name)
	if err != nil {
		var refused *api.Error
		if errors.As(err, &refused) && refused.Code == api.CodeNotFound {
			return nil, true, nil
		}
		return nil, false, err
	}
	return &ReadOnlyHost{home: home, serverURL: serverURL, projectID: project.ID}, false, nil
}

// PeekAction reads the receipt of one logical invocation of this project, like Session.PeekAction.
func (h *ReadOnlyHost) PeekAction(kind string, logical any) (action LocalAction, found bool, err error) {
	return peekAction(h.home, filepath.Join(h.home, "projects", projectNamespace(h.serverURL, h.projectID)), kind, logical)
}

// ActionGate is called with the lock of a receipt held and the receipt as it stands (nil when
// there is none), before beginAction writes the receipt. An error ends the call, leaves the
// receipt as it was, and releases the lock. The gate is not the first write of the call: on an
// empty client directory the project directory, the receipt directory and the lock file are
// created before it runs.
type ActionGate func(existing *LocalAction) error

// BeginActionGated is BeginAction (retryFailed true) or, with retryFailed false, the same call
// that keeps a failed receipt, with a gate that decides under the lock from the current state
// of the receipt.
func (s *Session) BeginActionGated(kind string, logical any, retryFailed bool, gate ActionGate) (journal *ActionJournal, fresh bool, err error) {
	return s.beginActionGated(kind, logical, retryFailed, gate)
}

func (s *Session) beginAction(kind string, logical any, retryFailed bool) (journal *ActionJournal, fresh bool, err error) {
	return s.beginActionGated(kind, logical, retryFailed, nil)
}

func (s *Session) beginActionGated(kind string, logical any, retryFailed bool, gate ActionGate) (journal *ActionJournal, fresh bool, err error) {
	name, err := actionName(kind, logical)
	if err != nil {
		return nil, false, err
	}
	root, err := s.LocalDataRoot()
	if err != nil {
		return nil, false, err
	}
	dir := filepath.Join(root, "launch-actions")
	if err := fs.EnsurePrivateDirectory(s.Home, dir, true); err != nil {
		return nil, false, err
	}
	path := filepath.Join(dir, name)
	file, err := fs.OpenLockFile(s.Home, path+".lock")
	if err != nil {
		return nil, false, err
	}
	lock, err := fs.LockExclusive(file)
	if err != nil {
		_ = file.Close()
		return nil, false, err
	}
	journal = &ActionJournal{Path: path, close: func() error { return errors.Join(lock.Unlock(), file.Close()) }}
	journal.save = func() error {
		raw, err := json.Marshal(journal.Value)
		if err != nil {
			return err
		}
		return fs.WriteTextAtomic(s.Home, path, string(raw), true)
	}
	previous, found, err := fs.ReadRegularFileIfExists(s.Home, path)
	if err != nil {
		_ = journal.Close()
		return nil, false, err
	}
	var existing *LocalAction
	if found {
		stored, err := decodeAction(previous, kind)
		if err != nil {
			_ = journal.Close()
			return nil, false, err
		}
		existing = &stored
	}
	if gate != nil {
		if err := gate(existing); err != nil {
			_ = journal.Close()
			return nil, false, err
		}
	}
	if existing != nil {
		journal.Value = *existing
		if journal.Value.Phase == "failed" && retryFailed {
			journal.Value = LocalAction{Schema: 1, Kind: kind, ExecutionID: NewExecutionID(), Phase: "prepared"}
			if err := journal.Save(); err != nil {
				_ = journal.Close()
				return nil, false, err
			}
			return journal, true, nil
		}
		return journal, false, nil
	}
	journal.Value = LocalAction{Schema: 1, Kind: kind, ExecutionID: NewExecutionID(), Phase: "prepared"}
	if err := journal.Save(); err != nil {
		_ = journal.Close()
		return nil, false, err
	}
	return journal, true, nil
}
