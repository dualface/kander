package remote_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

func integrationGroupCards(t *testing.T, f *integrationFixture, group string) ([]string, []remote.Credential) {
	t.Helper()
	var ids []string
	var owners []remote.Credential
	for i, s := range []*remote.Session{f.a, f.b} {
		slug := []string{"group-first", "group-second"}[i]
		task, err := s.Project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: slug, Title: "Deliver independent group work", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: task/"+slug, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		task, err = s.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.TaskID)
	}
	for i, s := range []*remote.Session{f.a, f.b} {
		grant, err := s.Project.Claim(t.Context(), "", ids[i], api.ClaimRequest{ClientID: s.Client.ClientID(), ExecutionID: "group-owner-" + s.Client.ClientID(), Owner: "codex"})
		if err != nil {
			t.Fatal(err)
		}
		c := remote.Credential{ServerURL: s.ServerURL, ClientID: s.Client.ClientID(), ProjectID: s.Info.ID, TaskID: ids[i], ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
		if err := s.SaveCredential(c); err != nil {
			t.Fatal(err)
		}
		owners = append(owners, c)
	}
	return ids, owners
}
func integrationJointClose(t *testing.T, f *integrationFixture, ids []string, commit string) {
	t.Helper()
	plan := board.ReviewPlan{Schema: 1, PlanID: "cross-clone-plan", Sealed: true, Author: "coordinator", Basis: "joint review at the merged group target", CWD: "project:" + f.a.Info.ID, ReportLanguage: "en", TaskIDs: ids, Batches: []board.ReviewPlanBatch{{BatchID: "cross-clone-batch", TaskIDs: ids, Base: f.base, TargetCommit: commit, Requirements: map[string]string{"PMQA": "N/A: integration fixture", "Security": "N/A: repository policy"}}}}
	confirm := func(operation string, body any) *client.Claim {
		_, err := f.a.ReviewAuthority(t.Context(), operation, body, ids)
		publication := pendingPublicationID(t, err)
		if _, err := f.b.ConfirmReviewPublication(t.Context(), publication, ids[1]); err != nil {
			t.Fatal(err)
		}
		proof, err := f.a.ReviewAuthority(t.Context(), operation, body, ids)
		if err != nil {
			t.Fatal(err)
		}
		return proof
	}
	if _, err := f.a.Project.ReviewPlan(t.Context(), "joint-git-plan", plan, confirm("review-plan", plan)); err != nil {
		t.Fatal(err)
	}
	raw, err := f.a.Project.ReviewBatchView(t.Context(), "cross-clone-batch")
	if err != nil {
		t.Fatal(err)
	}
	var view board.ReviewBatchView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: view.Batch.BatchID, ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "coordinator", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"request": request, "evidence": board.ReviewGitEvidence{CWD: plan.CWD, Head: commit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}}
	if _, err := f.a.Project.ReviewClose(t.Context(), "joint-git-close", body, confirm("review-close:cross-clone-batch", body)); err != nil {
		t.Fatal(err)
	}
}
func TestIntegrationGroupAssemblyAndJointReviewAcrossIndependentClones(t *testing.T) {
	f := newIntegrationGitFixture(t)
	group := "20261112-cross-clone-group"
	ids, owners := integrationGroupCards(t, f, group)
	if _, err := f.a.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: group, Owner: "coordinator", Token: "cross-clone-session", Basis: "explicitly coordinate the complete group", Members: ids}, "explicit group coordination"); err != nil {
		t.Fatal(err)
	}
	target := "refs/heads/group/" + group
	options := remote.IntegrationOptions{OperationID: "initialize-cross-clone-group", Purpose: "initialize", TargetRef: target, Commit: f.base, GroupID: group, Decision: "initialize the confirmed common anchor"}
	initialized, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil || initialized.Phase != "integrated" {
		t.Fatalf("group anchor: %+v %v", initialized, err)
	}
	if err := os.WriteFile(filepath.Join(f.second, "second.txt"), []byte("second clone contribution\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.second, "add", "second.txt")
	deliveryGit(t, f.second, "commit", "-m", "Deliver second clone work")
	secondCommit := deliveryGit(t, f.second, "rev-parse", "HEAD")
	for i, s := range []*remote.Session{f.a, f.b} {
		dir, commit := []string{f.first, f.second}[i], []string{f.commit, secondCommit}[i]
		if _, err := s.PublishCommit(t.Context(), dir, ids[i], commit, owners[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Project.Move(t.Context(), "", ids[i], api.Move{State: "review", DeliveryCommit: commit}, owners[i].Claim()); err != nil {
			t.Fatal(err)
		}
	}
	options.OperationID, options.Purpose, options.Commit, options.Contributors = "assemble-first-delivery", "assemble", f.commit, []string{ids[0]}
	if _, err := f.a.Integrate(t.Context(), f.first, options); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.FetchDelivery(t.Context(), f.first, ids[1], secondCommit); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, f.first, "merge", "--no-edit", secondCommit)
	merged := deliveryGit(t, f.first, "rev-parse", "HEAD")
	// The composed merge has never been published under a task delivery ref.
	// Its own integration source ref must carry it to the other clone.
	options.OperationID, options.Commit, options.Contributors = "assemble-both-deliveries", merged, ids
	assembled, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil || assembled.Request.Commit != merged {
		t.Fatalf("composed assembly: %+v %v", assembled, err)
	}
	deliveryGit(t, f.second, "fetch", "origin", assembled.SourceRef)
	if got := deliveryGit(t, f.second, "show", merged+":work.txt"); got != "reviewed delivery" {
		t.Fatal(got)
	}
	if got := deliveryGit(t, f.second, "show", merged+":second.txt"); got != "second clone contribution" {
		t.Fatal(got)
	}
	integrationJointClose(t, f, ids, merged)
	options.OperationID, options.Purpose, options.TargetRef, options.Contributors = "deliver-reviewed-group", "deliver", "refs/heads/v2", nil
	final, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil || final.Phase != "integrated" || final.ObservedTarget != merged {
		t.Fatalf("final group delivery: %+v %v", final, err)
	}
	if err := f.b.VerifyIntegration(t.Context(), f.second, final); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		value, err := f.b.Project.TaskIntegration(t.Context(), id)
		if err != nil || value.Request.ExecutionID != final.Request.ExecutionID {
			t.Fatalf("member final receipt: %+v %v", value, err)
		}
	}
	// A delayed old CAS from the original base cannot erase the merged result.
	cmd := []string{"push", "--force-with-lease=refs/heads/v2:" + f.base, "origin", f.commit + ":refs/heads/v2"}
	if err := exec.CommandContext(t.Context(), "git", append([]string{"-C", f.first}, cmd...)...).Run(); err == nil {
		t.Fatal("late old target CAS succeeded")
	}
	if got := deliveryGit(t, f.second, "ls-remote", "--refs", "origin", "refs/heads/v2"); !strings.HasPrefix(got, merged+"\t") {
		t.Fatal("late CAS overwrote the reviewed group")
	}
}
