package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"

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

func (s *Session) beginAction(kind string, logical any, retryFailed bool) (journal *ActionJournal, fresh bool, err error) {
	data, err := json.Marshal(struct {
		Kind  string
		Input any
	}{kind, logical})
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
	sum := sha256.Sum256(data)
	path := filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
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
	if found {
		if err := json.Unmarshal(previous, &journal.Value); err != nil || journal.Value.Schema != 1 || journal.Value.Kind != kind || !api.ValidIdentity(journal.Value.ExecutionID) {
			_ = journal.Close()
			return nil, false, errors.New(config.Text("remote.launch_receipt_damaged"))
		}
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
