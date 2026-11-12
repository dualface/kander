package remote

import (
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// MutationCredential preserves a fixed executor's ended identity. Ordinary
// reads may use no bearer after release; a new write must never silently turn
// that same executor into an unclaimed interactive client. A different member
// the process never held still returns no proof for joint holder coordination.
func (s *Session) MutationCredential(task string) (Credential, bool, error) {
	id, err := board.NormalizeTaskID(task)
	if err != nil {
		return Credential{}, false, err
	}
	value, found, err := s.Credential(id)
	if err != nil {
		return value, found, err
	}
	execution := os.Getenv(EnvExecutionID)
	if execution != "" {
		if !api.ValidIdentity(execution) {
			return Credential{}, false, &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.fixed_execution_ended", execution, id)}
		}
		released, err := fs.RegularFileExists(s.Home, filepath.Join(credentialDir(s.Home, s.Info.ID, id), execution+".released"))
		if err != nil {
			return Credential{}, false, err
		}
		if released {
			return Credential{}, false, &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.fixed_execution_ended", execution, id)}
		}
	}
	if found && value.Lost {
		return Credential{}, false, &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.fixed_execution_ended", value.ExecutionID, id)}
	}
	return value, found, nil
}
