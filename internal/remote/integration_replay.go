package remote

import (
	"context"
	"os"
	"regexp"
	"strconv"
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
		return "", newGitTransportError(err)
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

// findReplay looks for a run of consecutive commits of the target that repeats
// the review range B..C commit by commit. Candidates are the single-parent
// commits of B..T, wherever they entered the target, so a replay behind a merge
// and a replay that follows commits the target already held both qualify. A run
// is a chain in which each commit has the previous one as its parent.
func findReplay(ctx context.Context, dir, base, commit, target string) (*api.IntegrationReplay, error) {
	reviewed, ok, err := reviewCommits(ctx, dir, base, commit)
	if err != nil || !ok || len(reviewed) > replayMaxCommits {
		return nil, err
	}
	listing, err := gitCommand(ctx, dir, "rev-list", "--reverse", "--parents", "--max-count="+strconv.Itoa(replayMaxChain+1), base+".."+target)
	if err != nil {
		return nil, err
	}
	var candidates []string
	parent := map[string]string{}
	children := map[string][]string{}
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(candidates) == replayMaxChain {
			return nil, nil
		}
		candidates = append(candidates, fields[0])
		if len(fields) == 2 {
			parent[fields[0]] = fields[1]
			children[fields[1]] = append(children[fields[1]], fields[0])
		}
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
	run := replayRuns{reviewed: reviewed, children: children, patch: patch, memo: map[runKey]string{}}
	for _, start := range candidates {
		if parent[start] == "" {
			continue
		}
		last, err := run.from(start, 0)
		if err != nil {
			return nil, err
		}
		if last != "" {
			return &api.IntegrationReplay{ReviewBase: base, ReviewTarget: commit, ReplayBase: parent[start], ReplayTarget: last}, nil
		}
	}
	return nil, nil
}

type runKey struct {
	commit string
	index  int
}

// replayRuns follows chains of parent links whose patches repeat the reviewed
// commits in order. A commit may have several children with the same patch, so
// every branch is tried; the memo keeps the walk to one visit per commit and
// position.
type replayRuns struct {
	reviewed []string
	children map[string][]string
	patch    func(string) (string, error)
	memo     map[runKey]string
}

// from returns the last commit of a complete run that starts at commit as the
// reviewed commit at index, or an empty string when no branch completes.
func (r replayRuns) from(commit string, index int) (string, error) {
	key := runKey{commit, index}
	if last, seen := r.memo[key]; seen {
		return last, nil
	}
	r.memo[key] = ""
	want, err := r.patch(r.reviewed[index])
	if err != nil {
		return "", err
	}
	got, err := r.patch(commit)
	if err != nil || got != want {
		return "", err
	}
	if index == len(r.reviewed)-1 {
		r.memo[key] = commit
		return commit, nil
	}
	for _, child := range r.children[commit] {
		last, err := r.from(child, index+1)
		if err != nil {
			return "", err
		}
		if last != "" {
			r.memo[key] = last
			return last, nil
		}
	}
	return "", nil
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
