package remote

import (
	"context"
	"errors"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
)

// ResolveStartBase returns the commit refs/heads/<branch> names on the
// registered remote and makes that commit available in the clone at dir. A new
// execution starts from it, never from the caller's HEAD: that HEAD may sit on
// another branch or hold commits that were never pushed. A branch the remote
// does not have is an error; the start never falls back to another branch.
func (s *Session) ResolveStartBase(ctx context.Context, dir, branch string) (string, error) {
	if err := verifiedRemote(ctx, dir, s.Info.Remote); err != nil {
		return "", err
	}
	ref := "refs/heads/" + branch
	// ls-remote matches patterns against the tail of each ref name, so only the
	// line that names exactly this ref counts.
	text, err := gitCommand(ctx, dir, "ls-remote", "--refs", "origin", ref)
	if err != nil {
		return "", err
	}
	commit := ""
	for line := range strings.SplitSeq(text, "\n") {
		sha, name, found := strings.Cut(strings.TrimSpace(line), "\t")
		if !found || name != ref {
			continue
		}
		if commit != "" || !api.CommitSHA(sha) {
			return "", errors.New(config.Text("remote.git_full_sha"))
		}
		commit = sha
	}
	if commit == "" {
		return "", errors.New(config.Text("remote.start_branch_missing", branch))
	}
	if err := s.FetchCommit(ctx, dir, commit); err != nil {
		return "", err
	}
	return commit, nil
}
