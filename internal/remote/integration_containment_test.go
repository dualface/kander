package remote_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/remote"
)

func commitFile(t *testing.T, dir, name, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, dir, "add", name)
	deliveryGit(t, dir, "commit", "-m", message)
	return deliveryGit(t, dir, "rev-parse", "HEAD")
}

func (f *integrationFixture) targetHead() string {
	f.t.Helper()
	got := deliveryGit(f.t, f.first, "ls-remote", "--refs", "origin", "refs/heads/v2")
	return strings.Fields(got)[0]
}

// advanceTarget lands foreign work on v2 from the second clone, outside kander.
func (f *integrationFixture) advanceTarget(name string) string {
	f.t.Helper()
	deliveryGit(f.t, f.second, "fetch", "-q", "origin")
	deliveryGit(f.t, f.second, "checkout", "-q", "-B", "advance", "origin/v2")
	commit := commitFile(f.t, f.second, name, "foreign "+name+"\n", "Foreign "+name)
	deliveryGit(f.t, f.second, "push", "origin", "HEAD:refs/heads/v2")
	return commit
}

// publishLatestDelivery replaces the member's newest delivery after review.
func (f *integrationFixture) publishLatestDelivery(commit string) {
	f.t.Helper()
	if _, err := f.a.PublishCommit(f.t.Context(), f.first, f.task.TaskID, commit, f.credential); err != nil {
		f.t.Fatal(err)
	}
}

func (f *integrationFixture) complete(result api.Integration) {
	f.t.Helper()
	if err := f.b.VerifyIntegration(f.t.Context(), f.second, result); err != nil {
		f.t.Fatalf("receipt verification: %v", err)
	}
	f.credential.Worktree = f.first
	if err := f.a.SaveCredential(f.credential); err != nil {
		f.t.Fatal(err)
	}
	current, err := f.a.Project.Show(f.t.Context(), f.task.TaskID)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.a.Project.PutDocument(f.t.Context(), "", f.task.TaskID, api.Document{Path: "report.md", Text: "Completed through a contained integration.", ExpectedRevision: current.Revision}, f.credential.Claim()); err != nil {
		f.t.Fatal(err)
	}
	done, err := f.a.MoveTask(f.t.Context(), f.task.TaskID, api.Move{State: "done", Result: "completed"})
	if err != nil || done.State != "done" {
		f.t.Fatalf("completion: %+v %v", done, err)
	}
}

func TestIntegrationAcceptsReviewedCommitContainedInTheTarget(t *testing.T) {
	f := newIntegrationFixture(t)
	f.advanceTarget("other.txt")
	// The executor merges the reviewed commit into the target and publishes the
	// merge directly. Its latest delivery then descends from the reviewed one.
	deliveryGit(t, f.first, "fetch", "-q", "origin")
	deliveryGit(t, f.first, "merge", "--no-edit", "origin/v2")
	merged := deliveryGit(t, f.first, "rev-parse", "HEAD")
	deliveryGit(t, f.first, "push", "origin", "HEAD:refs/heads/v2")
	f.publishLatestDelivery(merged)
	before := f.targetHead()
	result, err := f.a.Integrate(t.Context(), f.first, f.options("contained-descendant"))
	if err != nil {
		t.Fatal(err)
	}
	request := result.Request
	if request.Containment != api.IntegrationContained || !request.AlreadyContained || request.Commit != f.commit || result.ObservedTarget != merged || result.SourceRef != api.IntegrationSourceRef(f.a.Info.ID, request.ExecutionID, f.commit) {
		t.Fatalf("receipt: %+v", result)
	}
	if request.ViaCommit != "" || request.Replay != nil {
		t.Fatalf("contained receipt carries other containment: %+v", request)
	}
	if after := f.targetHead(); after != before {
		t.Fatalf("a contained integration moved the target: %s -> %s", before, after)
	}
	f.complete(result)
}

func TestIntegrationContainedRejectsADeliveryOutsideBothSides(t *testing.T) {
	f := newIntegrationFixture(t)
	deliveryGit(t, f.first, "push", "origin", f.commit+":refs/heads/v2")
	deliveryGit(t, f.first, "checkout", "-q", "-b", "stray", f.base)
	stray := commitFile(t, f.first, "stray.txt", "unreviewed\n", "Unreviewed delivery")
	f.publishLatestDelivery(stray)
	before := f.targetHead()
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("stray-delivery")); err == nil || !strings.Contains(err.Error(), "latest delivery") {
		t.Fatalf("a delivery outside the target and the reviewed commit passed: %v", err)
	}
	if after := f.targetHead(); after != before {
		t.Fatal("rejected integration moved the target")
	}
	if _, err := f.a.Project.Integration(t.Context(), api.IntegrationID("refs/heads/v2")); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("rejected integration claimed the target: %v", err)
	}
}

func TestIntegrationViaCarrierFastForwardsTheDivergedTarget(t *testing.T) {
	f := newIntegrationFixture(t)
	foreign := f.advanceTarget("other.txt")
	deliveryGit(t, f.first, "fetch", "-q", "origin")
	deliveryGit(t, f.first, "merge", "--no-edit", "origin/v2")
	carrier := deliveryGit(t, f.first, "rev-parse", "HEAD")
	deliveryGit(t, f.first, "push", "origin", carrier+":refs/heads/carrier")
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("diverged-without-via")); err == nil || !strings.Contains(err.Error(), "via_commit") {
		t.Fatalf("diverged target without a carrier: %v", err)
	}
	if got := f.targetHead(); got != foreign {
		t.Fatal("rejected integration moved the target")
	}
	options := f.options("diverged-with-via")
	options.ViaCommit = carrier
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	request := result.Request
	if request.Containment != api.IntegrationVia || request.ViaCommit != carrier || request.AlreadyContained || request.Commit != f.commit || result.ObservedTarget != carrier {
		t.Fatalf("receipt: %+v", result)
	}
	if got := f.targetHead(); got != carrier {
		t.Fatalf("target is %s, want the carrier %s", got, carrier)
	}
	f.complete(result)
}

func TestIntegrationViaEqualToTheTargetPushesNothing(t *testing.T) {
	f := newIntegrationFixture(t)
	f.advanceTarget("other.txt")
	deliveryGit(t, f.first, "fetch", "-q", "origin")
	deliveryGit(t, f.first, "merge", "--no-edit", "origin/v2")
	carrier := deliveryGit(t, f.first, "rev-parse", "HEAD")
	deliveryGit(t, f.first, "push", "origin", "HEAD:refs/heads/v2")
	options := f.options("via-is-target")
	options.ViaCommit = carrier
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Containment != api.IntegrationVia || !result.Request.AlreadyContained || result.ObservedTarget != carrier || f.targetHead() != carrier {
		t.Fatalf("receipt: %+v", result)
	}
	f.complete(result)
}

func TestIntegrationViaMustDescendFromBothAndBeOnTheRemote(t *testing.T) {
	f := newIntegrationFixture(t)
	foreign := f.advanceTarget("other.txt")
	deliveryGit(t, f.first, "fetch", "-q", "origin")
	deliveryGit(t, f.first, "checkout", "-q", "-b", "side", "origin/v2")
	targetOnly := commitFile(t, f.first, "target-only.txt", "target only\n", "Only the target side")
	deliveryGit(t, f.first, "push", "origin", targetOnly+":refs/heads/target-only")
	deliveryGit(t, f.first, "checkout", "-q", "-b", "merge-side", f.commit)
	deliveryGit(t, f.first, "merge", "--no-edit", "origin/v2")
	unpushed := deliveryGit(t, f.first, "rev-parse", "HEAD")
	for name, via := range map[string]string{
		"not on the common remote":      unpushed,
		"not below the reviewed commit": targetOnly,
		"not below the target":          f.commit,
	} {
		if name == "not below the target" {
			deliveryGit(t, f.first, "push", "origin", f.commit+":refs/heads/reviewed-only")
		}
		options := f.options("bad-via-" + strings.ReplaceAll(name, " ", "-"))
		options.ViaCommit = via
		if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
			t.Fatalf("%s: carrier accepted", name)
		} else if name == "not on the common remote" && !strings.Contains(err.Error(), "Push it to the common remote") {
			t.Fatalf("%s: unhelpful error: %v", name, err)
		}
		if got := f.targetHead(); got != foreign {
			t.Fatalf("%s: rejected integration moved the target", name)
		}
	}
	if _, err := f.a.Project.Integration(t.Context(), api.IntegrationID("refs/heads/v2")); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("rejected carrier claimed the target: %v", err)
	}
	invalid := f.options("via-not-a-sha")
	invalid.ViaCommit = "carrier"
	if _, err := f.a.Integrate(t.Context(), f.first, invalid); err == nil {
		t.Fatal("a via value that is not a full SHA was accepted")
	}
	group := f.options("via-for-initialize")
	group.Purpose, group.TaskID, group.GroupID, group.ViaCommit = "initialize", "", "20261117-via-group", f.commit
	group.TargetRef = "refs/heads/group/20261117-via-group"
	if _, err := f.a.Integrate(t.Context(), f.first, group); err == nil {
		t.Fatal("a via commit outside delivery was accepted")
	}
}

func TestIntegrationReceiptWithoutContainmentStillVerifies(t *testing.T) {
	f := newIntegrationFixture(t)
	deliveryGit(t, f.first, "push", "origin", f.commit+":refs/heads/v2")
	result, err := f.a.Integrate(t.Context(), f.first, f.options("historical-receipt"))
	if err != nil {
		t.Fatal(err)
	}
	// A receipt written before the containment fields has only the contained
	// flag; it reads as a contained integration.
	result.Request.Containment = ""
	if got := result.Request.EffectiveContainment(); got != api.IntegrationContained {
		t.Fatalf("historical reading: %s", got)
	}
	if err := f.b.VerifyIntegration(t.Context(), f.second, result); err != nil {
		t.Fatal(err)
	}
}

func TestPatchNormalizationIgnoresOnlyIndexLinesAndHunkOffsets(t *testing.T) {
	first := "diff --git a/f b/f\nindex 1111111111111111111111111111111111111111..2222222222222222222222222222222222222222 100644\n--- a/f\n+++ b/f\n@@ -3,2 +3,3 @@\n a\n+b\n c\n"
	second := "diff --git a/f b/f\nindex 3333333333333333333333333333333333333333..4444444444444444444444444444444444444444 100644\n--- a/f\n+++ b/f\n@@ -40,2 +41,3 @@\n a\n+b\n c\n"
	if remote.NormalizePatch(first) != remote.NormalizePatch(second) {
		t.Fatal("index lines or hunk offsets changed the comparison")
	}
	changed := strings.Replace(second, "+b", "+B", 1)
	if remote.NormalizePatch(first) == remote.NormalizePatch(changed) {
		t.Fatal("a changed line compared equal")
	}
	spaced := strings.Replace(second, " c\n", " c \n", 1)
	if remote.NormalizePatch(first) == remote.NormalizePatch(spaced) {
		t.Fatal("a whitespace change compared equal")
	}
	mode := strings.Replace(second, "--- a/f", "old mode 100644\nnew mode 100755\n--- a/f", 1)
	if remote.NormalizePatch(first) == remote.NormalizePatch(mode) {
		t.Fatal("a mode change compared equal")
	}
}

func TestIntegrationInputAcceptsViaCommitAndStillRejectsUnknownFields(t *testing.T) {
	via := strings.Repeat("e", 40)
	input := `{"operation_id":"op","purpose":"deliver","target_ref":"refs/heads/v2","commit":"` + strings.Repeat("c", 40) + `","task_id":"20261117-via-task","via_commit":"` + via + `","decision":"d"}`
	var options remote.IntegrationOptions
	if err := board.DecodeReviewJSON([]byte(input), &options); err != nil || options.ViaCommit != via {
		t.Fatalf("via_commit input: %+v %v", options, err)
	}
	if err := board.DecodeReviewJSON([]byte(strings.Replace(input, `"via_commit"`, `"via"`, 1)), &options); err == nil {
		t.Fatal("a misspelled field was accepted")
	}
}
