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

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/probe"
)

const continuationLimit = 32 << 20

type continuationFile struct {
	Path       string `json:"path"`
	Data       []byte `json:"data"`
	Executable bool   `json:"executable,omitzero"`
}

// Continuation freezes local changes before a stopped executor is handed off.
// Ignored files stay in the predecessor worktree. No predecessor is modified.
type Continuation struct {
	Base     string             `json:"base"`
	Index    string             `json:"index"`
	Worktree string             `json:"worktree"`
	Files    []continuationFile `json:"files,omitempty"`
}

func continuationFailure() error {
	return errors.New(config.Text("remote.continuation_recovery_required"))
}

func continuationGit(ctx context.Context, dir string, args ...string) (string, error) {
	result, err := probe.CaptureWithEnvDirLimit(ctx, "git", args, append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never"), dir, continuationLimit)
	if err != nil || result.Code != 0 || result.Overflow {
		return "", continuationFailure()
	}
	return result.Stdout, nil
}

func captureContinuation(ctx context.Context, dir string) (value Continuation, err error) {
	value.Base, err = LocalCommit(ctx, dir)
	if err != nil {
		return value, err
	}
	value.Index, err = continuationGit(ctx, dir, "diff", "--binary", "--no-ext-diff", "--no-textconv", "--cached", "HEAD", "--")
	if err != nil {
		return value, err
	}
	value.Worktree, err = continuationGit(ctx, dir, "diff", "--binary", "--no-ext-diff", "--no-textconv", "--")
	if err != nil {
		return value, err
	}
	paths, err := continuationGit(ctx, dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return value, err
	}
	total := len(value.Index) + len(value.Worktree)
	if total > continuationLimit {
		return value, continuationFailure()
	}
	for path := range strings.SplitSeq(paths, "\x00") {
		if path == "" {
			continue
		}
		if !filepath.IsLocal(path) || strings.Contains(path, "\\") {
			return value, continuationFailure()
		}
		full := filepath.Join(dir, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if err != nil || !info.Mode().IsRegular() || info.Size() > int64(continuationLimit-total) {
			return value, continuationFailure()
		}
		data, err := fs.ReadRegularFile(dir, full)
		if err != nil {
			return value, err
		}
		total += len(data)
		if total > continuationLimit {
			return value, continuationFailure()
		}
		value.Files = append(value.Files, continuationFile{Path: path, Data: data, Executable: info.Mode().Perm()&0o111 != 0})
	}
	return value, nil
}

func continuationDigest(value Continuation) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// VerifyContinuation refuses a source edited after the snapshot was frozen.
func VerifyContinuation(ctx context.Context, source string, expected Continuation) error {
	actual, err := captureContinuation(ctx, source)
	if err != nil || continuationDigest(actual) != continuationDigest(expected) {
		return continuationFailure()
	}
	return nil
}

// CaptureContinuation detects concurrent edits and stores the full snapshot
// privately before the server handoff can retire the old execution.
func (s *Session) CaptureContinuation(ctx context.Context, credential Credential, base string) (Continuation, error) {
	value, err := captureContinuation(ctx, credential.Worktree)
	if err != nil {
		return value, err
	}
	again, err := captureContinuation(ctx, credential.Worktree)
	if err != nil || value.Base != base || continuationDigest(value) != continuationDigest(again) {
		return value, continuationFailure()
	}
	root, err := s.LocalDataRoot()
	if err != nil {
		return value, err
	}
	path := filepath.Join(root, "continuations", credential.TaskID, credential.ExecutionID, continuationDigest(value)+".json")
	if err := fs.EnsurePrivateDirectory(s.Home, filepath.Dir(path), true); err != nil {
		return value, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value, err
	}
	return value, fs.WriteTextAtomic(s.Home, path, string(data), true)
}

// RestoreContinuation records an intent before Git applies a patch. Recovery
// only recognizes an exact finished snapshot; a partial application is retained
// for inspection and is never applied twice.
func (s *Session) RestoreContinuation(ctx context.Context, target ExecutionWorktree, value Continuation, credential Credential) error {
	if value.Base != target.Base {
		return continuationFailure()
	}
	if err := s.checkExecution(ctx, credential); err != nil {
		return err
	}
	root, err := s.LocalDataRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "continuation-intents", credential.TaskID, credential.ExecutionID)
	if err := fs.EnsurePrivateDirectory(s.Home, dir, true); err != nil {
		return err
	}
	return withCredentialLock(s.Home, credential, "continuation-"+credential.ExecutionID, func() error {
		path := filepath.Join(dir, "intent.json")
		expected := continuationDigest(value)
		previous, found, err := fs.ReadRegularFileIfExists(s.Home, path)
		if err != nil {
			return err
		}
		if found && string(previous) != expected {
			return continuationFailure()
		}
		actual, err := captureContinuation(ctx, target.Path)
		if err != nil {
			return err
		}
		if continuationDigest(actual) == expected {
			return nil
		}
		if found || actual.Base != target.Base || actual.Index != "" || actual.Worktree != "" || len(actual.Files) != 0 {
			return continuationFailure()
		}
		if err := fs.WriteTextAtomic(s.Home, path, expected, false); err != nil {
			return err
		}
		for _, patch := range []struct {
			Name, Text string
			Index      bool
		}{{"index.patch", value.Index, true}, {"worktree.patch", value.Worktree, false}} {
			if patch.Text == "" {
				continue
			}
			patchPath := filepath.Join(dir, patch.Name)
			if err := fs.WriteTextAtomic(s.Home, patchPath, patch.Text, false); err != nil {
				return err
			}
			args := []string{"apply", "--binary", "--whitespace=nowarn"}
			if patch.Index {
				args = append(args, "--index")
			}
			args = append(args, "--", patchPath)
			if _, err := continuationGit(ctx, target.Path, args...); err != nil {
				return err
			}
		}
		for _, file := range value.Files {
			if !filepath.IsLocal(file.Path) || strings.Contains(file.Path, "\\") {
				return continuationFailure()
			}
			full := filepath.Join(target.Path, filepath.FromSlash(file.Path))
			if err := fs.EnsurePrivateDirectory(target.Path, filepath.Dir(full), true); err != nil {
				return err
			}
			if err := fs.WriteTextAtomic(target.Path, full, string(file.Data), false); err != nil {
				return err
			}
			if file.Executable {
				if err := os.Chmod(full, 0o700); err != nil {
					return err
				}
			}
		}
		actual, err = captureContinuation(ctx, target.Path)
		if err != nil || continuationDigest(actual) != expected {
			return continuationFailure()
		}
		return s.checkExecution(ctx, credential)
	})
}
