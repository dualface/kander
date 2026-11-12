package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
)

// gitCommand uses direct argv, bounded streams and an owned process tree. Git
// diagnostics can contain authenticated URLs, so shared errors omit raw output.
func gitCommand(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	result, err := probe.CaptureWithEnvDirLimit(ctx, "git", args, env, dir, 1<<20)
	if err != nil {
		return "", errors.New(config.Text("remote.git_interrupted"))
	}
	if result.Code != 0 || result.Overflow {
		return "", errors.New(config.Text("remote.git_unverified"))
	}
	return strings.TrimSpace(result.Stdout), nil
}

func verifiedRemote(ctx context.Context, dir, expected string) error {
	actual, err := gitCommand(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if !api.SafeGitRemote(actual) || actual != expected {
		return errors.New(config.Text("remote.git_remote_mismatch"))
	}
	// A separate push URL would send work to another repository.
	push, err := gitCommand(ctx, dir, "remote", "get-url", "--push", "origin")
	if err != nil {
		return err
	}
	if push != actual {
		return errors.New(config.Text("remote.git_push_remote_mismatch"))
	}
	return nil
}

func observeDeliveryRef(ctx context.Context, dir, ref, commit string) (bool, error) {
	text, err := gitCommand(ctx, dir, "ls-remote", "--refs", "origin", ref)
	if err != nil {
		return false, err
	}
	if text == "" {
		return false, nil
	}
	if text != commit+"\t"+ref {
		return false, errors.New(config.Text("remote.git_ref_conflict"))
	}
	return true, nil
}

// PublishCommit pushes only an immutable generation-specific delivery ref. A
// retry first inspects that ref, including after a push returned an error.
// It neither rewrites branches nor interprets transport failure as failure to push.
func (s *Session) PublishCommit(ctx context.Context, dir, task, commit string, credential Credential) (api.DeliveryEvidence, error) {
	return s.publishCommit(ctx, dir, task, commit, "delivery", credential)
}

// PublishBaseline makes a start commit available to other clones. It carries no
// completion authority; a later review move needs a separate delivery attestation.
func (s *Session) PublishBaseline(ctx context.Context, dir, task, commit string, credential Credential) (api.DeliveryEvidence, error) {
	return s.publishCommit(ctx, dir, task, commit, "baseline", credential)
}
func (s *Session) publishCommit(ctx context.Context, dir, task, commit, purpose string, credential Credential) (api.DeliveryEvidence, error) {
	var value api.DeliveryEvidence
	if credential.Lost || credential.ResourceKind != "" || credential.TaskID != task || credential.ProjectID != s.Info.ID || credential.ClientID != s.Client.ClientID() {
		return value, &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.git_credential_required")}
	}
	if err := verifiedRemote(ctx, dir, s.Info.Remote); err != nil {
		return value, err
	}
	if commit == "" {
		var err error
		commit, err = gitCommand(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			return value, err
		}
	}
	if !api.CommitSHA(commit) {
		return value, errors.New(config.Text("remote.git_full_sha"))
	}
	if _, err := gitCommand(ctx, dir, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return value, err
	}
	taskView, err := s.Project.Show(ctx, task)
	if err != nil {
		return value, err
	}
	if taskView.Claim == nil || taskView.Claim.Generation != credential.Generation || taskView.Claim.ExecutionID != credential.ExecutionID || taskView.Claim.Status != api.ClaimActive {
		return value, &api.Error{Code: api.CodeLeaseInvalid, Message: config.Text("remote.git_owner_changed")}
	}
	ref := api.DeliveryRef(s.Info.ID, task, credential.Generation, commit)
	exists, err := observeDeliveryRef(ctx, dir, ref, commit)
	if err != nil {
		return value, err
	}
	if !exists {
		_, pushErr := gitCommand(ctx, dir, "push", "--porcelain", "origin", commit+":"+ref)
		exists, err = observeDeliveryRef(ctx, dir, ref, commit)
		if err != nil || !exists {
			return value, errors.Join(pushErr, err, errors.New(config.Text("remote.git_publication_unconfirmed")))
		}
	}
	value = api.DeliveryEvidence{Purpose: purpose, TaskID: task, ExecutionID: credential.ExecutionID, Generation: credential.Generation, CardRevision: taskView.Revision, Commit: commit, Ref: ref, Remote: s.Info.Remote, ObservedAt: time.Now().UTC()}
	// A deterministic key binds the inspected content version; retries cannot
	// turn one upload into authority for an unrelated version.
	if previous, err := s.Project.Delivery(ctx, task, commit); err == nil && previous.Purpose == value.Purpose && previous.ExecutionID == value.ExecutionID && previous.Generation == value.Generation && previous.CardRevision == value.CardRevision && previous.Remote == value.Remote && previous.Ref == value.Ref {
		value = previous
	}
	key := "delivery-" + digestDelivery(value)
	return s.Project.PublishDelivery(ctx, key, value, *credential.Claim())
}

// FetchDelivery verifies the registered remote's current reference before
// fetching; a stale local object alone never satisfies another host's delivery.
func (s *Session) FetchDelivery(ctx context.Context, dir, task, commit string) (api.DeliveryEvidence, error) {
	value, err := s.Project.Delivery(ctx, task, commit)
	if err != nil {
		return value, err
	}
	if value.Remote != s.Info.Remote || value.Ref != api.DeliveryRef(s.Info.ID, task, value.Generation, commit) || !api.CommitSHA(commit) {
		return value, errors.New(config.Text("remote.git_delivery_mismatch"))
	}
	if err := verifiedRemote(ctx, dir, value.Remote); err != nil {
		return value, err
	}
	exists, err := observeDeliveryRef(ctx, dir, value.Ref, commit)
	if err != nil || !exists {
		return value, errors.Join(err, errors.New(config.Text("remote.git_commit_unavailable")))
	}
	if _, err := gitCommand(ctx, dir, "fetch", "--no-tags", "origin", value.Ref); err != nil {
		return value, err
	}
	_, err = gitCommand(ctx, dir, "cat-file", "-e", commit+"^{commit}")
	return value, err
}

// FetchCommit checks a review/dependency commit through the common remote even
// when an object with that SHA is already cached in this clone.
func (s *Session) FetchCommit(ctx context.Context, dir, commit string) error {
	if !api.CommitSHA(commit) {
		return errors.New(config.Text("remote.git_evidence_sha"))
	}
	if err := verifiedRemote(ctx, dir, s.Info.Remote); err != nil {
		return err
	}
	if _, err := gitCommand(ctx, dir, "fetch", "--no-tags", "origin", commit); err != nil {
		return err
	}
	_, err := gitCommand(ctx, dir, "cat-file", "-e", commit+"^{commit}")
	return err
}

func digestDelivery(value api.DeliveryEvidence) string {
	value.ObservedAt = time.Time{}
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// LocalCommit resolves a local baseline without trusting a ref as an option.
func LocalCommit(ctx context.Context, dir string) (string, error) {
	return gitCommand(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
}
