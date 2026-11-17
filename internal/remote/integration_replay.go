package remote

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/probe"
)

// Bounds keep the search for a replayed range finite. A range or a target
// history beyond them is never judged equal; the caller names a via commit.
const (
	replayMaxCommits   = 256
	replayMaxChain     = 4096
	replayPatchMaxSize = 32 << 20
)

var patchHunk = regexp.MustCompile(`(?m)^@@ -[0-9]+(,[0-9]+)? \+[0-9]+(,[0-9]+)? @@`)
var patchIndex = regexp.MustCompile(`(?m)^index [0-9a-f]+\.\.[0-9a-f]+(?: [0-7]+)?\n`)

// NormalizePatch keeps a patch complete, including whitespace, context, modes
// and binary changes. Only blob hashes and line offsets may differ after an
// unrelated rebase, so only the index lines and the hunk offsets are dropped.
func NormalizePatch(patch string) string {
	return patchHunk.ReplaceAllString(patchIndex.ReplaceAllString(patch, ""), "@@ -x${1} +x${2} @@")
}

// commitPatch is the normalized patch of one commit against its first parent.
func commitPatch(ctx context.Context, dir, commit string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--binary", "--full-index", "--no-renames", commit + "^1", commit, "--"}
	result, err := probe.CaptureWithEnvDirLimit(ctx, "git", args, env, dir, replayPatchMaxSize)
	if err != nil {
		return "", integrationFailure("remote.git_interrupted")
	}
	if result.Code != 0 || result.Overflow {
		return "", integrationFailure("remote.git_unverified")
	}
	return NormalizePatch(result.Stdout), nil
}

// reviewCommits lists the commits of a linear review range, oldest first. A
// range with a merge commit has no per-commit patches to compare, so it is
// reported as absent rather than as an error.
func reviewCommits(ctx context.Context, dir, base, target string) ([]string, bool, error) {
	merges, err := gitCommand(ctx, dir, "rev-list", "--merges", "--max-count=1", base+".."+target)
	if err != nil || merges != "" {
		return nil, false, err
	}
	return revList(ctx, dir, base+".."+target, false)
}

// revList returns the commits of a range, oldest first, and false when the
// range exceeds the search bounds or is empty.
func revList(ctx context.Context, dir, rng string, firstParent bool) ([]string, bool, error) {
	args := []string{"rev-list", "--reverse"}
	if firstParent {
		args = append(args, "--first-parent")
	}
	output, err := gitCommand(ctx, dir, append(args, "--max-count=4097", rng)...)
	if err != nil {
		return nil, false, err
	}
	commits := strings.Fields(output)
	return commits, len(commits) > 0 && len(commits) <= replayMaxChain, nil
}

// patchesEqual reports whether every commit of the first list has the same
// normalized patch as the commit at the same position of the second.
func patchesEqual(ctx context.Context, dir string, reviewed, replayed []string) (bool, error) {
	if len(reviewed) != len(replayed) {
		return false, nil
	}
	for i := range reviewed {
		want, err := commitPatch(ctx, dir, reviewed[i])
		if err != nil {
			return false, err
		}
		got, err := commitPatch(ctx, dir, replayed[i])
		if err != nil {
			return false, err
		}
		if want != got {
			return false, nil
		}
	}
	return true, nil
}

// findReplay looks for a run of consecutive commits on the first-parent chain
// of the target that repeats the review range B..C commit by commit. The
// search only starts after the merge base of C and the target.
func findReplay(ctx context.Context, dir, base, commit, target string) (*api.IntegrationReplay, error) {
	reviewed, ok, err := reviewCommits(ctx, dir, base, commit)
	if err != nil || !ok || len(reviewed) > replayMaxCommits {
		return nil, err
	}
	fork, err := gitCommand(ctx, dir, "merge-base", commit, target)
	if err != nil || fork == "" {
		return nil, nil
	}
	chain, ok, err := revList(ctx, dir, fork+".."+target, true)
	if err != nil || !ok {
		return nil, err
	}
	patches := map[string]string{}
	patch := func(c string) (string, error) {
		if value, found := patches[c]; found {
			return value, nil
		}
		value, err := commitPatch(ctx, dir, c)
		patches[c] = value
		return value, err
	}
	for start := 0; start+len(reviewed) <= len(chain); start++ {
		matched := true
		for i, c := range reviewed {
			want, err := patch(c)
			if err != nil {
				return nil, err
			}
			got, err := patch(chain[start+i])
			if err != nil {
				return nil, err
			}
			if want != got {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		first, err := gitCommand(ctx, dir, "rev-parse", chain[start]+"^1")
		if err != nil {
			return nil, err
		}
		return &api.IntegrationReplay{ReviewBase: base, ReviewTarget: commit, ReplayBase: first, ReplayTarget: chain[start+len(reviewed)-1]}, nil
	}
	return nil, nil
}

// verifyReplay repeats the commit-by-commit comparison of a recorded replay.
func (s *Session) verifyReplay(ctx context.Context, dir string, replay api.IntegrationReplay) error {
	for _, commit := range []string{replay.ReviewBase, replay.ReviewTarget, replay.ReplayBase, replay.ReplayTarget} {
		if err := s.FetchCommit(ctx, dir, commit); err != nil {
			return err
		}
	}
	reviewed, ok, err := reviewCommits(ctx, dir, replay.ReviewBase, replay.ReviewTarget)
	if err != nil {
		return err
	}
	replayed, replayOK, err := revList(ctx, dir, replay.ReplayBase+".."+replay.ReplayTarget, true)
	if err != nil {
		return err
	}
	if !ok || !replayOK || len(reviewed) > replayMaxCommits {
		return integrationFailure("remote.integration_recovery_required")
	}
	equal, err := patchesEqual(ctx, dir, reviewed, replayed)
	if err != nil {
		return err
	}
	if !equal {
		return integrationFailure("remote.integration_recovery_required")
	}
	return nil
}
