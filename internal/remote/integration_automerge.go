package remote

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/probe"
)

// autoMergeRefPrefix holds the private ref that keeps a generated merge alive
// until the target branch carries it. It is outside refs/heads and never pushed.
const autoMergeRefPrefix = "refs/kander/auto-merge/"

// mergeConflictListed bounds how many conflicting paths an error names.
const mergeConflictListed = 10

// mergeTreeMaxOutput bounds the merge-tree listing of conflicting paths.
const mergeTreeMaxOutput = 4 << 20

// minimalGit is the first Git version with `merge-tree --write-tree`.
const minimalGit = "2.38"

func autoMergeRef(executionID string) string { return autoMergeRefPrefix + executionID }

// gitStatus runs Git and reports its exit code instead of mapping a non-zero
// exit to an error. The process is bounded like every other Git call.
func gitStatus(ctx context.Context, dir string, args ...string) (probe.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	result, err := probe.CaptureWithEnvDirLimit(ctx, "git", args, env, dir, mergeTreeMaxOutput)
	if err != nil {
		return result, newGitTransportError(err)
	}
	return result, nil
}

// autoMerge merges the observed target with source into one new commit, the
// target as first parent, and records it as the via commit of the request. The
// merge is built from objects only: it never touches the work tree, the index,
// HEAD or a local branch. A conflict, a Git older than 2.38 and a missing Git
// identity refuse the integration before the service is asked for anything.
func (s *Session) autoMerge(ctx context.Context, dir string, options IntegrationOptions, request *api.IntegrationRequest, source string, carried *api.IntegrationRequest) error {
	target := request.ExpectedTarget
	if adopted, err := adoptAutoMerge(ctx, dir, carried, request, source); adopted || err != nil {
		return err
	}
	if err := gitIdentity(ctx, dir); err != nil {
		return err
	}
	tree, conflicts, err := mergeTree(ctx, dir, target, source)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		listed := conflicts[:min(len(conflicts), mergeConflictListed)]
		return integrationFailure("remote.integration_auto_merge_conflict", strconv.Itoa(len(conflicts)), strings.Join(listed, ", "))
	}
	message := autoMergeMessage(options, request, source)
	commit, err := gitCommand(ctx, dir, "-c", "user.useConfigOnly=true", "commit-tree", tree, "-p", target, "-p", source, "-m", message)
	if err != nil {
		return err
	}
	if !api.CommitSHA(commit) {
		return integrationFailure("remote.git_unverified")
	}
	return recordAutoMerge(ctx, dir, request, commit, source)
}

// recordAutoMerge pins the merge under the private ref of this attempt and
// records it as the via commit of the request.
func recordAutoMerge(ctx context.Context, dir string, request *api.IntegrationRequest, commit, source string) error {
	if _, err := gitCommand(ctx, dir, "update-ref", autoMergeRef(request.ExecutionID), commit); err != nil {
		return err
	}
	request.Containment, request.ViaCommit, request.ViaAutoMerge, request.AlreadyContained = api.IntegrationVia, commit, true, false
	if source != request.Commit {
		request.ViaSource = source
	}
	return nil
}

// carriesAutoMerge is the set of reasons that retire an attempt without
// making its generated merge stale: the merge depends only on the observed
// target and the commit it merges, never on the lease or the process.
var carriesAutoMerge = map[string]bool{retireClaimEnded: true, retireIdentity: true, retireScope: true}

// carriedAutoMerge is the request of the last attempt of the operation when
// that attempt ended for a reason that keeps its merge usable: it was retired
// for one of those reasons, or the service refused its claim.
func carriedAutoMerge(state *integrationOperation) *api.IntegrationRequest {
	if len(state.Attempts) == 0 {
		return nil
	}
	last := state.Attempts[len(state.Attempts)-1]
	ended := last.Retired && carriesAutoMerge[last.RetiredFor] || last.Failed && !last.Retired
	if !ended || !last.Request.ViaAutoMerge {
		return nil
	}
	return &last.Request
}

// adoptAutoMerge reuses the merge of a retired attempt when the new attempt
// observes the same target and merges the same commit, so a command that was
// restarted after an interruption pushes the commit it first made.
func adoptAutoMerge(ctx context.Context, dir string, carried *api.IntegrationRequest, request *api.IntegrationRequest, source string) (bool, error) {
	viaSource := ""
	if source != request.Commit {
		viaSource = source
	}
	if carried == nil || carried.ExpectedTarget != request.ExpectedTarget || carried.Commit != request.Commit || carried.ViaSource != viaSource {
		return false, nil
	}
	named, err := gitStatus(ctx, dir, "rev-parse", "--verify", "--quiet", autoMergeRef(carried.ExecutionID)+"^{commit}")
	if err != nil || named.Code != 0 || strings.TrimSpace(named.Stdout) != carried.ViaCommit {
		return false, err
	}
	return true, recordAutoMerge(ctx, dir, request, carried.ViaCommit, source)
}

// autoMergeMessage is a fixed English commit message that names the target
// branch, the reviewed commit and the operation, plus the caller's via commit.
func autoMergeMessage(options IntegrationOptions, request *api.IntegrationRequest, source string) string {
	branch := strings.TrimPrefix(options.TargetRef, "refs/heads/")
	lines := []string{
		"Merge " + branch + " into reviewed commit",
		"",
		"Generated by kander during integration into " + branch + ".",
		"Reviewed commit: " + request.Commit,
	}
	if source != request.Commit {
		lines = append(lines, "Via commit: "+source)
	}
	return strings.Join(append(lines, "Operation: "+options.OperationID), "\n") + "\n"
}

// gitIdentity requires the repository to configure the author and committer
// itself. Git never guesses one from the host name for a kander-made commit.
func gitIdentity(ctx context.Context, dir string) error {
	for _, name := range []string{"GIT_AUTHOR_IDENT", "GIT_COMMITTER_IDENT"} {
		result, err := gitStatus(ctx, dir, "-c", "user.useConfigOnly=true", "var", name)
		if err != nil {
			return err
		}
		if result.Code != 0 {
			return integrationFailure("remote.integration_auto_merge_identity")
		}
	}
	return nil
}

// mergeTree merges two commits without a work tree. Exit status 1 with a tree
// object name on the first line is a conflict, and the listed paths follow;
// every other non-zero status is a Git error, and status 129 (an unknown
// option) means Git is older than 2.38.
func mergeTree(ctx context.Context, dir, target, source string) (string, []string, error) {
	result, err := gitStatus(ctx, dir, "merge-tree", "--write-tree", "-z", "--name-only", "--no-messages", target, source)
	if err != nil {
		return "", nil, err
	}
	if result.Overflow {
		return "", nil, integrationFailure("remote.git_unverified")
	}
	fields := strings.Split(result.Stdout, "\x00")
	switch {
	case result.Code == 0 && validObjectName(fields[0]):
		return fields[0], nil, nil
	case result.Code == 1 && validObjectName(fields[0]):
		var paths []string
		for _, path := range fields[1:] {
			if path == "" {
				break
			}
			paths = append(paths, path)
		}
		if len(paths) == 0 {
			return "", nil, integrationFailure("remote.git_unverified")
		}
		return "", paths, nil
	case result.Code == 129:
		return "", nil, integrationFailure("remote.integration_auto_merge_git_version", minimalGit)
	default:
		return "", nil, integrationFailure("remote.git_unverified")
	}
}

// validObjectName accepts a full SHA-1 or SHA-256 object name.
func validObjectName(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// autoMergeKept reports whether the generated merge can still be pushed or is
// already in the target: the private ref still names it, or the observed
// target contains it.
func (s *Session) autoMergeKept(ctx context.Context, dir string, request api.IntegrationRequest, head string) (bool, error) {
	named, err := gitStatus(ctx, dir, "rev-parse", "--verify", "--quiet", autoMergeRef(request.ExecutionID)+"^{commit}")
	if err != nil {
		return false, err
	}
	if named.Code == 0 && strings.TrimSpace(named.Stdout) == request.ViaCommit {
		return true, nil
	}
	if head == "" {
		return false, nil
	}
	// A merge that was never pushed and has no ref is gone for good; fetching
	// the target brings it back only when the target holds it.
	if err := s.FetchCommit(ctx, dir, head); err != nil {
		return false, err
	}
	local, err := gitStatus(ctx, dir, "cat-file", "-e", request.ViaCommit+"^{commit}")
	if err != nil || local.Code != 0 {
		return false, err
	}
	return gitAncestor(ctx, dir, request.ViaCommit, head)
}

// dropAutoMerge deletes the private ref of a generated merge, if any. A command
// that was cancelled still removes its ref, so cleanup gets its own deadline.
func dropAutoMerge(ctx context.Context, dir string, request api.IntegrationRequest) error {
	if !request.ViaAutoMerge {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_, err := gitCommand(ctx, dir, "update-ref", "-d", autoMergeRef(request.ExecutionID))
	return err
}

// dropAutoMerges deletes the private refs of every attempt of an operation.
func dropAutoMerges(ctx context.Context, dir string, state *integrationOperation) error {
	for _, attempt := range state.Attempts {
		if err := dropAutoMerge(ctx, dir, attempt.Request); err != nil {
			return err
		}
	}
	return nil
}
