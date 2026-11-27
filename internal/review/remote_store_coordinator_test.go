//go:build unix

package review

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

const (
	coordinatedGroup = "20261118-coordinated-group"
	coordinatedBase  = "1111111111111111111111111111111111111111"
	coordinatedHead  = "2222222222222222222222222222222222222222"
)

// coordinatedRemote is the Git remote the fixture registers its project with.
// A test that moves cards to review sets one, because the service accepts
// delivery facts only for a registered remote.
var coordinatedRemote string

// coordinatedClock, when set, is the clock of the fixture's server. A test that lapses a
// lease sets it and moves it past the lease.
var coordinatedClock func() time.Time

// coordinatedRun is a group review whose cards are held by a process on host a
// and whose reviewer runs on host b, the process that coordinates the group.
type coordinatedRun struct {
	t                  *testing.T
	holder, coordinate *remote.Session
	stranger           *remote.Session
	store              *remoteReviewStore
	ids                []string
	grants             map[string]api.ClaimGrant
}

// newCoordinatedRun plans one batch over members cards. A member after the
// first is trashed once the plan exists, so only the first stays live.
func newCoordinatedRun(t *testing.T, members int) *coordinatedRun {
	t.Helper()
	return newCoordinatedRunOf(t, members, members)
}

// newCoordinatedRunOf starts members cards of the group and plans the first
// planned of them; a started card the plan leaves out stays live and unplanned.
func newCoordinatedRunOf(t *testing.T, members, planned int) *coordinatedRun {
	t.Helper()
	return newCoordinatedRunKeeping(t, members, planned, false)
}

// newCoordinatedRunKeeping is newCoordinatedRunOf, and with keepLive the planned
// members after the first stay live, so a run can have several live members. The
// stranger session is a third host that holds no credential of the group.
func newCoordinatedRunKeeping(t *testing.T, members, planned int, keepLive bool) *coordinatedRun {
	t.Helper()
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: coordinatedClock})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	a, _ := client.New(httpServer.URL, "host-a")
	b, _ := client.New(httpServer.URL, "host-b")
	c, _ := client.New(httpServer.URL, "host-c")
	info, err := a.Register(t.Context(), "coordinated-run", coordinatedRemote)
	if err != nil {
		t.Fatal(err)
	}
	open := func(c *client.Client) *remote.Session {
		return &remote.Session{Client: c, Project: c.Project(info.ID), Info: info, ServerURL: httpServer.URL, Home: t.TempDir()}
	}
	r := &coordinatedRun{t: t, holder: open(a), coordinate: open(b), stranger: open(c), grants: map[string]api.ClaimGrant{}}
	for i := range members {
		task, err := r.holder.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: []string{"first-member", "second-member"}[i], Title: "Run a coordinated review", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+coordinatedGroup, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		if task, err = r.holder.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
			t.Fatal(err)
		}
		if task, err = r.holder.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
			t.Fatal(err)
		}
		r.ids = append(r.ids, task.TaskID)
	}
	request := board.CoordinatorClaim{GroupID: coordinatedGroup, Owner: "coordinator", Token: "label", Basis: "coordinated run fixture", Members: r.ids}
	if _, err := r.coordinate.AcquireCoordinator(t.Context(), request, ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range r.ids {
		grant, err := r.holder.Project.Claim(t.Context(), "", id, api.ClaimRequest{BranchMode: "main-develop", ClientID: a.ClientID(), ExecutionID: "execution-" + id, Owner: "codex"})
		if err != nil {
			t.Fatal(err)
		}
		credential := remote.Credential{ServerURL: httpServer.URL, ClientID: a.ClientID(), ProjectID: info.ID, TaskID: id, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
		if err := r.holder.SaveCredential(credential); err != nil {
			t.Fatal(err)
		}
		r.grants[id] = grant
	}
	plan := board.ReviewPlan{Schema: 1, PlanID: "coordinated-plan", Author: "coordinator", Basis: "coordinated run", CWD: "/host-a/clone", ReportLanguage: "en", TaskIDs: r.ids[:planned], Batches: []board.ReviewPlanBatch{{BatchID: "coordinated-batch", TaskIDs: r.ids[:planned], Base: coordinatedBase, TargetCommit: coordinatedHead, Requirements: map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}}}}
	proof, err := r.holder.ReviewAuthority(t.Context(), "review-plan", plan, r.ids[:planned])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.holder.Project.ReviewPlan(t.Context(), client.NewKey(), plan, proof); err != nil {
		t.Fatal(err)
	}
	for _, id := range r.ids[1:planned] {
		if keepLive {
			break
		}
		own, found, err := r.holder.Credential(id)
		if err != nil || !found {
			t.Fatalf("holder credential: %v", err)
		}
		if _, err := r.holder.Project.Move(t.Context(), "", id, api.Move{State: "trash", Result: "trashed", Reason: "left the batch", Decision: "fixture decision"}, own.Claim()); err != nil {
			t.Fatal(err)
		}
	}
	if r.store, err = newRemoteReviewStore(r.coordinate); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *coordinatedRun) input() board.ReviewInput {
	return board.ReviewInput{RunID: "coordinated-run", BatchID: "coordinated-batch", TaskIDs: r.ids[:1], Role: "PMQA", Reviewer: "codex", Model: "test", Effort: "high", Base: coordinatedBase, Commit: coordinatedHead, ReportLanguage: "en"}
}

// awaitHolder checks that the wait names the run and its retry, then has the
// holder confirm the publication it names.
func (r *coordinatedRun) awaitHolder(err error) {
	r.t.Helper()
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		r.t.Fatalf("want a wait for the holder: %v", err)
	}
	for _, want := range []string{"coordinated-run", "--run-id coordinated-run"} {
		if !strings.Contains(err.Error(), want) {
			r.t.Fatalf("wait omits %q: %v", want, err)
		}
	}
	id := regexp.MustCompile(`review-[0-9a-f]{64}`).FindString(err.Error())
	if id == "" {
		r.t.Fatalf("wait omits the publication: %v", err)
	}
	if _, err := r.holder.ConfirmReviewPublication(r.t.Context(), id, r.ids[0]); err != nil {
		r.t.Fatal(err)
	}
}

// assign runs the real assign command path of the coordinator for a published
// run: it waits for the holder once, then writes the assignment.
func (r *coordinatedRun) assign(run board.ReviewRun) {
	r.t.Helper()
	assignment := board.ReviewAssignment{RunID: run.RunID, BatchID: run.BatchID, Author: "coordinator", Basis: "single member fixture", Items: map[string][]string{}}
	data, err := json.Marshal(assignment)
	if err != nil {
		r.t.Fatal(err)
	}
	input := filepath.Join(r.t.TempDir(), "assignment.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		r.t.Fatal(err)
	}
	_, err = remoteEvidenceAction(r.t.Context(), r.coordinate, "/host-b/clone", "assign", input, nil)
	if !api.IsCode(err, api.CodeEvidenceIncomplete) {
		r.t.Fatalf("want a wait for the holder: %v", err)
	}
	id := regexp.MustCompile(`review-[0-9a-f]{64}`).FindString(err.Error())
	if _, err := r.holder.ConfirmReviewPublication(r.t.Context(), id, r.ids[0]); err != nil {
		r.t.Fatal(err)
	}
	if _, err := remoteEvidenceAction(r.t.Context(), r.coordinate, "/host-b/clone", "assign", input, nil); err != nil {
		r.t.Fatalf("confirmed assign: %v", err)
	}
}

func TestCoordinatorRunPublishesAfterHolderConfirmation(t *testing.T) {
	for _, members := range []int{1, 2} {
		t.Run(map[int]string{1: "one member", 2: "one live of two members"}[members], func(t *testing.T) {
			r := newCoordinatedRun(t, members)
			unlock, err := r.store.Lock("coordinated-run")
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			requirements := map[string]string{"PMQA": "required", "Security": "N/A: repository policy"}
			originals := map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")}
			_, _, err = r.store.Prepare(r.input(), requirements, nil, originals, "test")
			r.awaitHolder(err)
			if members == 2 && strings.Contains(err.Error(), r.ids[1]) {
				t.Fatalf("wait names the member that left the batch: %v", err)
			}
			run, fresh, err := r.store.Prepare(r.input(), requirements, nil, originals, "test")
			if err != nil || !fresh {
				t.Fatalf("confirmed prepare: %+v %v", run, err)
			}
			run.LaunchStatus, run.ExecutionStatus = "started", "ok"
			if run, err = r.store.Finalize(run, []byte(emptyStructuredReview)); err != nil {
				t.Fatal(err)
			}
			_, _, err = r.store.Publish(run.RunID)
			r.awaitHolder(err)
			published, _, err := r.store.Publish(run.RunID)
			if err != nil || !published.Published[r.ids[0]] {
				t.Fatalf("confirmed publish: %+v %v", published, err)
			}
			if members == 1 {
				r.assign(published)
			}
		})
	}
}
