package remote

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/probe"
)

func (s *Session) integrationTarget(ctx context.Context, dir, ref string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !api.IntegrationTarget(ref) {
		return "", integrationFailure("remote.integration_input_required")
	}
	if err := verifiedRemote(ctx, dir, s.Info.Remote); err != nil {
		return "", err
	}
	if _, err := gitCommand(ctx, dir, "check-ref-format", ref); err != nil {
		return "", err
	}
	output, err := gitCommand(ctx, dir, "ls-remote", "--refs", "origin", ref)
	if err != nil || output == "" {
		return "", err
	}
	commit, actual, ok := strings.Cut(output, "\t")
	if !ok || actual != ref || !api.CommitSHA(commit) {
		return "", integrationFailure("remote.git_ref_conflict")
	}
	return commit, nil
}
func (s *Session) integrationEdges(ctx context.Context, dir string, input api.IntegrationRequest) error {
	commits := map[string]bool{}
	for _, edge := range input.Edges {
		commits[edge.Ancestor], commits[edge.Descendant] = true, true
	}
	for commit := range commits {
		if commit == input.Commit {
			continue
		}
		if err := s.FetchCommit(ctx, dir, commit); err != nil {
			return err
		}
	}
	for _, edge := range input.Edges {
		if _, err := gitCommand(ctx, dir, "merge-base", "--is-ancestor", edge.Ancestor, edge.Descendant); err != nil {
			return errors.Join(err, integrationFailure("remote.integration_fast_forward_required"))
		}
	}
	return nil
}
func (s *Session) integrationContains(ctx context.Context, dir, source, target string) (bool, error) {
	if target == "" {
		return false, nil
	}
	if err := s.FetchCommit(ctx, dir, target); err != nil {
		return false, err
	}
	if source == target {
		return true, nil
	}
	// Capture the exit status separately: a non-ancestor is an ordinary result,
	// while a failed probe must never authorize a push.
	return gitAncestor(ctx, dir, source, target)
}

func gitAncestor(ctx context.Context, dir, source, target string) (bool, error) {
	if !api.CommitSHA(source) || !api.CommitSHA(target) {
		return false, integrationFailure("remote.git_full_sha")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	result, err := probe.CaptureWithEnvDirLimit(ctx, "git", []string{"merge-base", "--is-ancestor", source, target}, append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never"), dir, 1<<20)
	if err != nil || result.Overflow {
		return false, integrationFailure("remote.git_interrupted")
	}
	switch result.Code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, integrationFailure("remote.git_unverified")
	}
}
