package remote

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// ErrCredentialReleased prevents a delayed launcher or renewal from recreating
// an execution record after completion removed it.
var ErrCredentialReleased = errors.New("execution credential was released")

type credentialHead struct {
	ExecutionID string `json:"execution_id"`
	Generation  uint64 `json:"generation"`
}

func readCredentialHead(home, dir string) (head credentialHead, found bool, err error) {
	data, found, err := fs.ReadRegularFileIfExists(home, filepath.Join(dir, "latest-generation"))
	if err != nil || !found {
		return head, found, err
	}
	if err := json.Unmarshal(data, &head); err != nil || !api.ValidIdentity(head.ExecutionID) || head.Generation == 0 {
		return head, true, errors.New(config.Text("remote.credential_head_damaged"))
	}
	return head, true, nil
}

func withCredentialLock(home string, c Credential, name string, fn func() error) (err error) {
	if !api.ValidIdentity(c.ProjectID) || !api.ValidIdentity(c.TaskID) || !api.ValidIdentity(c.ExecutionID) {
		return errors.New("invalid execution credential identity")
	}
	dir := credentialDir(home, c.ProjectID, c.TaskID)
	if err := fs.EnsurePrivateDirectory(home, dir, true); err != nil {
		return err
	}
	file, err := fs.OpenLockFile(home, filepath.Join(dir, name+".lock"))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	lock, err := fs.LockExclusive(file)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	return fn()
}

// SaveCredential serializes the latest pointer with release. The lock order is
// task registry, then execution, so an old release cannot erase a newer pointer.
func (s *Session) SaveCredential(c Credential) error {
	if c.Generation == 0 {
		return &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.credential_stale")}
	}
	return withCredentialLock(s.Home, c, "registry", func() error {
		dir := credentialDir(s.Home, c.ProjectID, c.TaskID)
		head, found, err := readCredentialHead(s.Home, dir)
		if err != nil {
			return err
		}
		if !found {
			// Upgrade the earlier prototype's pointer without guessing its epoch.
			data, exists, err := fs.ReadRegularFileIfExists(s.Home, filepath.Join(dir, "latest"))
			if err != nil {
				return err
			}
			if exists {
				id := strings.TrimSpace(string(data))
				if !api.ValidIdentity(id) {
					return errors.New(config.Text("remote.credential_head_damaged"))
				}
				data, exists, err = fs.ReadRegularFileIfExists(s.Home, filepath.Join(dir, id+".json"))
				if err != nil {
					return err
				}
				if exists {
					var previous Credential
					if err := json.Unmarshal(data, &previous); err != nil {
						return err
					}
					head = credentialHead{ExecutionID: previous.ExecutionID, Generation: previous.Generation}
					found = true
				}
			}
		}
		if found && (head.Generation > c.Generation || head.Generation == c.Generation && head.ExecutionID != c.ExecutionID) {
			return &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.credential_stale")}
		}
		if err := WriteCredential(s.Home, c); err != nil {
			return err
		}
		data, err := json.Marshal(credentialHead{ExecutionID: c.ExecutionID, Generation: c.Generation})
		if err != nil {
			return err
		}
		// Keep the maximum generation after release. A delayed initial save must
		// not resurrect an older interactive pointer after the successor exits.
		if err := fs.WriteTextAtomic(s.Home, filepath.Join(dir, "latest-generation"), string(data), true); err != nil {
			return err
		}
		return fs.WriteTextAtomic(s.Home, filepath.Join(dir, "latest"), c.ExecutionID+"\n", true)
	})
}

// WriteCredential merges independently observed launcher and worker facts under
// an execution lock. A renewal never erases the PID, delivered request IDs or
// the irreversible lost-lease state recorded by another process.
func WriteCredential(home string, c Credential) error {
	return withCredentialLock(home, c, c.ExecutionID, func() error {
		dir := credentialDir(home, c.ProjectID, c.TaskID)
		released, err := fs.RegularFileExists(home, filepath.Join(dir, c.ExecutionID+".released"))
		if err != nil {
			return err
		}
		if released {
			return ErrCredentialReleased
		}
		path := filepath.Join(dir, c.ExecutionID+".json")
		oldBytes, exists, err := fs.ReadRegularFileIfExists(home, path)
		if err != nil {
			return err
		}
		if exists {
			var old Credential
			if err := json.Unmarshal(oldBytes, &old); err != nil {
				return errors.New("execution credential is damaged")
			}
			if old.ResourceKind != c.ResourceKind || old.ProjectID != c.ProjectID || old.TaskID != c.TaskID || old.ExecutionID != c.ExecutionID || old.Generation != c.Generation || old.Token != c.Token || old.ClientID != c.ClientID || old.ServerURL != c.ServerURL {
				return errors.New("immutable execution credential conflict")
			}
			if old.RenewedAt.After(c.RenewedAt) {
				c.RenewedAt = old.RenewedAt
			}
			if c.AgentPID != 0 && old.AgentPID != 0 && c.AgentPID != old.AgentPID {
				return errors.New("managed process identity conflict")
			}
			if c.AgentPID == 0 {
				c.AgentPID = old.AgentPID
			}
			for _, pair := range [][2]*string{{&c.AgentStart, &old.AgentStart}, {&c.BootID, &old.BootID}, {&c.Window, &old.Window}, {&c.Worktree, &old.Worktree}, {&c.Branch, &old.Branch}, {&c.BaseCommit, &old.BaseCommit}} {
				if *pair[0] != "" && *pair[1] != "" && *pair[0] != *pair[1] {
					return errors.New("managed agent identity conflict")
				}
				if *pair[0] == "" {
					*pair[0] = *pair[1]
				}
			}
			switch {
			case c.Session == "", sessionReferenceAdvances(c.Session, old.Session):
				c.Session = old.Session
			case old.Session == "", c.Session == old.Session, sessionReferenceAdvances(old.Session, c.Session):
			default:
				return errors.New("managed agent session conflict")
			}
			for _, target := range old.StopAttempts {
				if !slices.Contains(c.StopAttempts, target) {
					c.StopAttempts = append(c.StopAttempts, target)
				}
			}
			if c.StopResult == "" {
				c.StopResult = old.StopResult
			}

			c.Lost = c.Lost || old.Lost
			if c.LostReason == "" {
				c.LostReason = old.LostReason
			}
			for _, id := range old.Delivered {
				if !slices.Contains(c.Delivered, id) {
					c.Delivered = append(c.Delivered, id)
				}
			}
		}
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return err
		}
		return fs.WriteTextAtomic(home, path, string(data)+"\n", true)
	})
}

// RemoveCredential leaves a release tombstone before removing the secret. A
// late write cannot resurrect the credential, even after a worker restart.
func RemoveCredential(home string, c Credential) error {
	return withCredentialLock(home, c, "registry", func() error {
		return withCredentialLock(home, c, c.ExecutionID, func() error {
			dir := credentialDir(home, c.ProjectID, c.TaskID)
			previous, found, err := fs.ReadRegularFileIfExists(home, filepath.Join(dir, c.ExecutionID+".json"))
			if err != nil {
				return err
			}
			retained := c
			if found {
				if err := json.Unmarshal(previous, &retained); err != nil {
					return err
				}
				if retained.ExecutionID != c.ExecutionID || retained.Generation != c.Generation {
					return errors.New("execution history identity conflict")
				}
			}
			if err := retainExecution(home, retained, found); err != nil {
				return err
			}
			if err := fs.WriteTextAtomic(home, filepath.Join(dir, c.ExecutionID+".released"), "released\n", true); err != nil {
				return err
			}
			if _, err := fs.RemoveRegularFileIfExists(home, filepath.Join(dir, c.ExecutionID+".json")); err != nil {
				return err
			}
			latest, found, err := fs.ReadRegularFileIfExists(home, filepath.Join(dir, "latest"))
			if err != nil {
				return err
			}
			if found && strings.TrimSpace(string(latest)) == c.ExecutionID {
				_, err = fs.RemoveRegularFileIfExists(home, filepath.Join(dir, "latest"))
			}
			return err
		})
	})
}

// Session discovery only fills a previously missing reference for the same
// agent. A delayed heartbeat cannot erase it or replace a known reference.
func sessionReferenceAdvances(previous, next string) bool {
	return previous != "" && !strings.ContainsAny(previous, " \t\r\n") && strings.HasPrefix(next, previous+" ") && strings.TrimSpace(strings.TrimPrefix(next, previous+" ")) != "" && !strings.ContainsAny(next, "\r\n")
}
