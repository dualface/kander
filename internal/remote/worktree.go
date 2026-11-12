package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// ExecutionWorktree is host-private routing data. Each generation owns a new
// branch and directory; recovery never resets or deletes an earlier worktree.
type ExecutionWorktree struct {
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	Base        string `json:"base"`
	ExecutionID string `json:"execution_id"`
	Generation  uint64 `json:"generation"`
	Phase       string `json:"phase"`
}

func (s *Session) checkExecution(ctx context.Context, credential Credential) error {
	if credential.Lost || credential.ResourceKind != "" || credential.Generation == 0 || credential.ProjectID != s.Info.ID || credential.ClientID != s.Client.ClientID() {
		return &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.execution_holder_required")}
	}
	card, err := s.Project.Show(ctx, credential.TaskID)
	if err != nil {
		return err
	}
	if card.Claim == nil || card.Claim.Generation != credential.Generation || card.Claim.ExecutionID != credential.ExecutionID || card.Claim.ClientID != credential.ClientID || card.Claim.Status != api.ClaimActive {
		return &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.git_owner_changed")}
	}
	return nil
}

func worktreeFailure() error { return errors.New(config.Text("remote.worktree_recovery_required")) }

// PrepareWorktree records the intent before Git creates anything. An uncertain
// result is reconciled against Git's actual registration, branch and common
// directory; partial or conflicting artifacts are retained for inspection.
func (s *Session) PrepareWorktree(ctx context.Context, dir, base string, credential Credential) (result ExecutionWorktree, err error) {
	if err := s.checkExecution(ctx, credential); err != nil {
		return result, err
	}
	if base == "" {
		base = credential.BaseCommit
	}
	if base == "" {
		base, err = gitCommand(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			return result, err
		}
	}
	if !api.CommitSHA(base) {
		return result, errors.New(config.Text("remote.git_evidence_sha"))
	}
	if s.Info.Remote != "" {
		if err := s.FetchCommit(ctx, dir, base); err != nil {
			return result, err
		}
	}
	root, err := s.LocalDataRoot()
	if err != nil {
		return result, err
	}
	result = ExecutionWorktree{Path: filepath.Join(root, "worktrees", credential.TaskID, fmt.Sprintf("g%d", credential.Generation)), Branch: fmt.Sprintf("kander/%s/%s/g%d", s.Info.ID, credential.TaskID, credential.Generation), Base: base, ExecutionID: credential.ExecutionID, Generation: credential.Generation, Phase: "prepared"}
	intentPath := filepath.Join(root, "worktree-intents", credential.TaskID, fmt.Sprintf("g%d.json", credential.Generation))
	if err := fs.EnsurePrivateDirectory(s.Home, filepath.Dir(intentPath), true); err != nil {
		return result, err
	}
	if err := fs.EnsurePrivateDirectory(s.Home, filepath.Dir(result.Path), true); err != nil {
		return result, err
	}
	err = withCredentialLock(s.Home, credential, "worktree-"+credential.ExecutionID, func() error {
		previous, found, err := fs.ReadRegularFileIfExists(s.Home, intentPath)
		if err != nil {
			return err
		}
		if found {
			var old ExecutionWorktree
			if err := json.Unmarshal(previous, &old); err != nil {
				return worktreeFailure()
			}
			if old.Path != result.Path || old.Branch != result.Branch || old.Base != base || old.ExecutionID != credential.ExecutionID || old.Generation != credential.Generation {
				return worktreeFailure()
			}
		}
		store := func(phase string) error {
			result.Phase = phase
			data, err := json.Marshal(result)
			if err != nil {
				return err
			}
			return fs.WriteTextAtomic(s.Home, intentPath, string(data), true)
		}
		if !found {
			if err := store("prepared"); err != nil {
				return err
			}
		}
		ready, conflict, err := inspectExecutionWorktree(ctx, dir, result)
		if err != nil {
			return err
		}
		if !ready {
			if conflict {
				return worktreeFailure()
			}
			if err := s.checkExecution(ctx, credential); err != nil {
				return err
			}
			if err := store("creating"); err != nil {
				return err
			}
			_, createErr := gitCommand(ctx, dir, "worktree", "add", "-b", result.Branch, result.Path, base)
			ready, _, err = inspectExecutionWorktree(ctx, dir, result)
			if err != nil || !ready {
				return errors.Join(createErr, err, worktreeFailure())
			}
		}
		return store("ready")
	})
	if err != nil {
		return result, fmt.Errorf("%w: %s", err, intentPath)
	}
	credential.Worktree, credential.Branch, credential.BaseCommit = result.Path, result.Branch, result.Base
	if err := WriteCredential(s.Home, credential); err != nil {
		return result, err
	}
	return result, s.checkExecution(ctx, credential)
}

func inspectExecutionWorktree(ctx context.Context, dir string, value ExecutionWorktree) (ready, conflict bool, err error) {
	listing, err := gitCommand(ctx, dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, false, err
	}
	registered := false
	for line := range strings.SplitSeq(listing, "\x00") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok && filepath.Clean(path) == filepath.Clean(value.Path) {
			registered = true
		}
	}
	if registered {
		common, err := gitCommand(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return false, true, err
		}
		actual, err := gitCommand(ctx, value.Path, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil || filepath.Clean(common) != filepath.Clean(actual) {
			return false, true, worktreeFailure()
		}
		branch, err := gitCommand(ctx, value.Path, "symbolic-ref", "HEAD")
		if err != nil || branch != "refs/heads/"+value.Branch {
			return false, true, worktreeFailure()
		}
		if _, err := gitCommand(ctx, value.Path, "merge-base", "--is-ancestor", value.Base, "HEAD"); err != nil {
			return false, true, worktreeFailure()
		}
		return true, false, nil
	}
	refs, err := gitCommand(ctx, dir, "for-each-ref", "--format=%(refname)", "refs/heads/"+value.Branch)
	if err != nil {
		return false, false, err
	}
	// DirectoryExists refuses symlinks; a pre-existing directory is a conflict,
	// even if empty. Git alone owns creation after the recorded intent.
	exists, err := fs.DirectoryExists(filepath.Dir(value.Path), value.Path)
	if err != nil {
		return false, true, err
	}
	return false, exists || refs != "", nil
}
