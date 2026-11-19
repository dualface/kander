package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
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

// BeginAction serializes one logical invocation. Callers must inspect existing
// receipts before changing central state or performing any terminal operation.
func (s *Session) BeginAction(kind string, logical any) (journal *ActionJournal, fresh bool, err error) {
	return s.beginAction(kind, logical, true)
}

// RetainAction preserves a conclusively failed attempt as well. An implicit
// native continuation must not turn a recovery retry into a fresh launch.
func (s *Session) RetainAction(kind string, logical any) (journal *ActionJournal, fresh bool, err error) {
	return s.beginAction(kind, logical, false)
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
	name, err := actionName(kind, logical)
	if err != nil {
		return action, false, err
	}
	root := filepath.Join(s.Home, "projects", projectNamespace(s.ServerURL, s.Info.ID))
	data, found, err := fs.ReadRegularFileIfExists(s.Home, filepath.Join(root, "launch-actions", name))
	if err != nil || !found {
		return action, false, err
	}
	action, err = decodeAction(data, kind)
	return action, err == nil, err
}

// MayReplayAction reports, reading only, whether this host may hold a receipt of the given kind
// for task, in any project: a receipt that names the task, or one that does not name any task
// yet. It needs no session. It is false when the host has no client directory or no receipt of
// the kind, so a command that is about to be refused can be refused before a session creates the
// client identity or registers the project. An empty task matches every receipt of the kind.
func MayReplayAction(kind, task string) (bool, error) {
	home, err := Home()
	if err != nil {
		return false, err
	}
	projects := filepath.Join(home, "projects")
	namespaces, err := fs.ListDirectory(home, projects)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, namespace := range namespaces {
		dir := filepath.Join(projects, namespace.Name, "launch-actions")
		entries, err := fs.ListDirectory(home, dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, err
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name, ".json") {
				continue
			}
			data, found, err := fs.ReadRegularFileIfExists(home, filepath.Join(dir, entry.Name))
			if err != nil || !found {
				continue
			}
			var action LocalAction
			if json.Unmarshal(data, &action) != nil {
				return true, nil
			}
			if action.Kind == kind && (task == "" || action.TaskID == "" || action.TaskID == task) {
				return true, nil
			}
		}
	}
	return false, nil
}

// ActionGate is called with the lock of a receipt held and the receipt as it stands (nil when
// there is none), before beginAction writes anything. An error ends the call and leaves the
// receipt as it was.
type ActionGate func(existing *LocalAction) error

// BeginActionGated is BeginAction (retryFailed true) or RetainAction (retryFailed false) with a
// gate that decides under the lock, from the current state of the receipt.
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
