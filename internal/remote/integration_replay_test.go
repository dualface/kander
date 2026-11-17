package remote_test

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
)

// replayFixture reviews a range of two commits: the first changes work.txt, the
// second adds extra.txt. mid is the first commit of the range.
func replayFixture(t *testing.T) (f *integrationFixture, mid string) {
	t.Helper()
	f = newIntegrationGitFixture(t)
	mid = f.commit
	f.commit = commitFile(t, f.first, "extra.txt", "extra work\n", "Add extra work")
	f.reviewedTask()
	if _, err := f.b.FetchDelivery(t.Context(), f.second, f.task.TaskID, f.commit); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.second, "fetch", "-q", "origin")
	return f, mid
}

// pickOnTarget lands foreign work on the target and then replays commits onto
// it from the second clone, outside kander. Each step is a commit to cherry-pick
// or a file write written as name=content. It returns the foreign commit and the
// new target.
func (f *integrationFixture) pickOnTarget(steps ...string) (foreign, target string) {
	f.t.Helper()
	deliveryGit(f.t, f.second, "fetch", "-q", "origin")
	deliveryGit(f.t, f.second, "checkout", "-q", "-B", "replay", "origin/v2")
	foreign = commitFile(f.t, f.second, "foreign.txt", "foreign\n", "Foreign work")
	for _, step := range steps {
		if name, content, ok := strings.Cut(step, "="); ok {
			commitFile(f.t, f.second, name, content+"\n", "Write "+name)
			continue
		}
		deliveryGit(f.t, f.second, "cherry-pick", step)
	}
	deliveryGit(f.t, f.second, "push", "origin", "HEAD:refs/heads/v2")
	return foreign, deliveryGit(f.t, f.second, "rev-parse", "HEAD")
}

func TestIntegrationReplayedRangeAlreadyInTheTargetPushesNothing(t *testing.T) {
	f, mid := replayFixture(t)
	foreign, tail := f.pickOnTarget(mid, f.commit)
	laterForeign := commitFile(t, f.second, "later.txt", "foreign later\n", "Foreign work after the replay")
	deliveryGit(t, f.second, "push", "origin", "HEAD:refs/heads/v2")
	result, err := f.a.Integrate(t.Context(), f.first, f.options("replayed-range"))
	if err != nil {
		t.Fatal(err)
	}
	request := result.Request
	if request.Containment != api.IntegrationReplayed || !request.AlreadyContained || request.Commit != f.commit || request.ViaCommit != "" {
		t.Fatalf("receipt: %+v", request)
	}
	replay := request.Replay
	if replay == nil || replay.ReviewBase != f.base || replay.ReviewTarget != f.commit || replay.ReplayBase != foreign || replay.ReplayTarget != tail {
		t.Fatalf("replay record: %+v (want base %s, tail %s)", replay, foreign, tail)
	}
	if got := f.targetHead(); got != laterForeign || result.ObservedTarget != laterForeign {
		t.Fatalf("a replayed integration moved the target: %s, observed %s", got, result.ObservedTarget)
	}
	f.complete(result)
}

func TestIntegrationReplayRejectsRangesThatAreNotEqualCommitByCommit(t *testing.T) {
	cases := []struct {
		name  string
		steps func(mid, top string) []string
	}{
		{"fewer commits than the review range", func(mid, top string) []string { return []string{mid} }},
		{"the review range in another order", func(mid, top string) []string { return []string{top, mid} }},
		{"a foreign commit inside the range", func(mid, top string) []string { return []string{mid, "other.txt=foreign", top} }},
		{"a commit with another patch", func(mid, top string) []string { return []string{mid, "extra.txt=different work"} }},
		{"only the second commit", func(mid, top string) []string { return []string{top} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, mid := replayFixture(t)
			_, target := f.pickOnTarget(c.steps(mid, f.commit)...)
			_, err := f.a.Integrate(t.Context(), f.first, f.options("not-equal"))
			if err == nil || !strings.Contains(err.Error(), "via_commit") {
				t.Fatalf("unequal range accepted or unhelpful error: %v", err)
			}
			if got := f.targetHead(); got != target {
				t.Fatal("rejected integration moved the target")
			}
			if _, err := f.a.Project.Integration(t.Context(), api.IntegrationID("refs/heads/v2")); !api.IsCode(err, api.CodeNotFound) {
				t.Fatalf("rejected integration claimed the target: %v", err)
			}
		})
	}
}

func TestIntegrationReplayRejectsAMergeInTheReviewRange(t *testing.T) {
	f := newIntegrationGitFixture(t)
	deliveryGit(t, f.first, "checkout", "-q", "-b", "side", f.base)
	side := commitFile(t, f.first, "side.txt", "side work\n", "Side work")
	deliveryGit(t, f.first, "checkout", "-q", "-")
	deliveryGit(t, f.first, "merge", "--no-edit", side)
	f.commit = deliveryGit(t, f.first, "rev-parse", "HEAD")
	f.reviewedTask()
	if _, err := f.b.FetchDelivery(t.Context(), f.second, f.task.TaskID, f.commit); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.second, "fetch", "-q", "origin")
	deliveryGit(t, f.second, "checkout", "-q", "-B", "replay", "origin/v2")
	commitFile(t, f.second, "foreign.txt", "foreign\n", "Foreign work")
	deliveryGit(t, f.second, "cherry-pick", "-m", "1", f.commit)
	deliveryGit(t, f.second, "push", "origin", "HEAD:refs/heads/v2")
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("merge-range")); err == nil {
		t.Fatal("a review range with a merge was judged equal")
	}
}
