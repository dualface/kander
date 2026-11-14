package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/servicectl"
)

func wrapUpGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=Kander Test", "-c", "user.email=kander@example.invalid", "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// startGitlessServer runs the complete binary's server command as a child
// whose working directory is no checkout and whose PATH holds no Git, so any
// server-side Git call fails the scenario.
func startGitlessServer(t *testing.T) (string, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, cwd, empty := t.TempDir(), t.TempDir(), t.TempDir()
	logFile, err := os.Create(filepath.Join(t.TempDir(), "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "server", "start", "--foreground", "--data", data, "--listen", address, "--ttl", "10m", "--renew", "1m")
	cmd.Dir, cmd.Stdout, cmd.Stderr = cwd, logFile, logFile
	cmd.Env = []string{"KANDER_TEST_SERVER_CHILD=1", "PATH=" + empty, "HOME=" + t.TempDir(), config.EnvConfig + "=" + filepath.Join(t.TempDir(), "config.json")}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = logFile.Close()
	})
	target := "http://" + address
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		info, err := servicectl.Info(ctx, target)
		cancel()
		if err == nil && info.PID == cmd.Process.Pid {
			return target, data
		}
		time.Sleep(50 * time.Millisecond)
	}
	log, _ := os.ReadFile(logFile.Name())
	t.Fatalf("gitless server did not start:\n%s", log)
	return "", ""
}

type wrapUpScenario struct {
	t                    *testing.T
	coordinator, holder  *remote.Session
	first, exec, second  string
	foreign              string
	base, commit, group  string
	task                 string
	credential           remote.Credential
	final                api.Integration
	coordinatorClientDir string
	data                 string
}

func openWrapUpSession(t *testing.T, cfg *config.Config, home, dir string) *remote.Session {
	t.Helper()
	t.Setenv(remote.EnvClientHome, home)
	session, err := remote.Open(t.Context(), cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	session.WorkerStarter = func(string) error { return nil }
	return session
}

// newWrapUpScenario delivers one group member through the gitless service:
// a published delivery, a closed joint review and a final integration.
func newWrapUpScenario(t *testing.T) *wrapUpScenario {
	t.Helper()
	target, data := startGitlessServer(t)
	root := t.TempDir()
	s := &wrapUpScenario{t: t, group: "20261114-gitless-wrap-group", data: data}
	bare := filepath.Join(root, "common.git")
	s.first, s.exec, s.second, s.foreign = filepath.Join(root, "first"), filepath.Join(root, "exec"), filepath.Join(root, "second"), filepath.Join(root, "foreign")
	wrapUpGit(t, root, "init", "--bare", bare)
	wrapUpGit(t, root, "clone", bare, s.first)
	if err := os.WriteFile(filepath.Join(s.first, "work.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wrapUpGit(t, s.first, "add", "work.txt")
	wrapUpGit(t, s.first, "commit", "-m", "Initial work")
	s.base = wrapUpGit(t, s.first, "rev-parse", "HEAD")
	wrapUpGit(t, s.first, "push", "origin", "HEAD:refs/heads/v2")
	wrapUpGit(t, root, "clone", "--branch", "v2", bare, s.exec)
	wrapUpGit(t, root, "clone", "--branch", "v2", bare, s.second)
	wrapUpGit(t, root, "init", s.foreign)
	wrapUpGit(t, s.foreign, "remote", "add", "origin", filepath.Join(root, "other.git"))

	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "forbidden-board"))
	cfg := config.DefaultConfig()
	cfg.Language, cfg.AgentLanguage = "en", "en"
	cfg.Server.URL, cfg.Project.Name = target, "gitless-wrap"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	s.holder = openWrapUpSession(t, cfg, filepath.Join(root, "holder-home"), s.exec)
	s.coordinatorClientDir = filepath.Join(root, "coordinator-home")
	s.coordinator = openWrapUpSession(t, cfg, s.coordinatorClientDir, s.first)
	t.Chdir(s.first)

	ctx := t.Context()
	task, err := s.holder.Project.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: "gitless-wrap", Title: "Wrap up through a gitless service", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.ReplaceAll(text, "- [ ]", "- [x]")
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+s.group, 1)
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: task/gitless-wrap", 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	text = strings.Replace(text, "## SUMMARY\n", "## SUMMARY\n\nDelivered and verified in an isolated two-clone fixture.\n", 1)
	if task, err = s.holder.Project.PutDocument(ctx, "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.holder.Project.Move(ctx, "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	s.task = task.TaskID
	grant, err := s.holder.Project.Claim(ctx, "", s.task, api.ClaimRequest{ClientID: s.holder.Client.ClientID(), ExecutionID: "gitless-wrap-executor", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	s.credential = remote.Credential{ServerURL: s.holder.ServerURL, ClientID: s.holder.Client.ClientID(), ProjectID: s.holder.Info.ID, TaskID: s.task, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, Worktree: s.exec}
	if err := s.holder.SaveCredential(s.credential); err != nil {
		t.Fatal(err)
	}
	if _, err := s.coordinator.AcquireCoordinator(ctx, board.CoordinatorClaim{GroupID: s.group, Owner: "coordinator", Token: "gitless-wrap-session", Basis: "coordinate the confirmed group", Members: []string{s.task}}, "explicit group coordination"); err != nil {
		t.Fatal(err)
	}
	groupRef := "refs/heads/group/" + s.group
	options := remote.IntegrationOptions{OperationID: "initialize-gitless-group", Purpose: "initialize", TargetRef: groupRef, Commit: s.base, GroupID: s.group, Decision: "initialize the confirmed common anchor"}
	if _, err := s.coordinator.Integrate(ctx, s.first, options); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.exec, "work.txt"), []byte("reviewed delivery\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wrapUpGit(t, s.exec, "commit", "-am", "Deliver reviewed work")
	s.commit = wrapUpGit(t, s.exec, "rev-parse", "HEAD")
	if _, err := s.holder.PublishCommit(ctx, s.exec, s.task, s.commit, s.credential); err != nil {
		t.Fatal(err)
	}
	if _, err := s.holder.Project.Move(ctx, "", s.task, api.Move{State: "review", DeliveryCommit: s.commit}, s.credential.Claim()); err != nil {
		t.Fatal(err)
	}
	options.OperationID, options.Purpose, options.Commit, options.Contributors = "assemble-gitless-delivery", "assemble", s.commit, []string{s.task}
	if _, err := s.coordinator.Integrate(ctx, s.first, options); err != nil {
		t.Fatal(err)
	}
	s.closeReview()
	options.OperationID, options.Purpose, options.TargetRef, options.Contributors = "deliver-gitless-group", "deliver", "refs/heads/v2", nil
	if s.final, err = s.coordinator.Integrate(ctx, s.first, options); err != nil || s.final.Phase != "integrated" {
		t.Fatalf("final delivery: %+v %v", s.final, err)
	}
	return s
}

func (s *wrapUpScenario) closeReview() {
	t := s.t
	plan := board.ReviewPlan{Schema: 1, PlanID: "gitless-wrap-plan", Sealed: true, Author: "coordinator", Basis: "review at the assembled group target", CWD: "project:" + s.holder.Info.ID, ReportLanguage: "en", TaskIDs: []string{s.task}, Batches: []board.ReviewPlanBatch{{BatchID: "gitless-wrap-batch", TaskIDs: []string{s.task}, Base: s.base, TargetCommit: s.commit, Requirements: map[string]string{"PMQA": "N/A: integration fixture", "Security": "N/A: repository policy"}}}}
	if _, err := s.holder.Project.ReviewPlan(t.Context(), "", plan, s.credential.Claim()); err != nil {
		t.Fatal(err)
	}
	raw, err := s.holder.Project.ReviewAggregate(t.Context(), "", "gitless-wrap-batch", s.credential.Claim())
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
	evidence := board.ReviewGitEvidence{CWD: plan.CWD, Head: s.commit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	if _, err := s.holder.Project.ReviewClose(t.Context(), "", map[string]any{"request": request, "evidence": evidence}, s.credential.Claim()); err != nil {
		t.Fatal(err)
	}
}

// notify runs the coordinator's real CLI entrance with one evidence file.
func (s *wrapUpScenario) notify(id string, git map[string]any) commandResult {
	t := s.t
	t.Setenv(remote.EnvClientHome, s.coordinatorClientDir)
	data, err := json.Marshal(map[string]any{"wrap_up": map[string]any{"git": git}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "evidence.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	return runKander(t, "notify", "--kind", "wrap-up", "--dispatch-id", id, "--base", s.commit, "--evidence-file", file, "--timeout", "600", "--message", "Record completion and clean the execution workspace", s.task)
}

func (s *wrapUpScenario) requests() []api.CollabRequest {
	requests, err := s.coordinator.Project.Requests(s.t.Context(), s.task)
	if err != nil {
		s.t.Fatal(err)
	}
	return requests
}

func TestGitlessServiceCompletesDistributedWrapUpFromAnotherCheckout(t *testing.T) {
	s := newWrapUpScenario(t)
	ctx := t.Context()
	// The pre-fix documented shape: no published integration identity.
	legacy := map[string]any{"cwd": s.first, "source_commit": s.commit, "target_commit": s.final.ObservedTarget, "target_ref": "refs/heads/v2", "author": "coordinator", "basis": "verified final integration"}
	out := s.notify("legacy-shape-wrap", legacy)
	if out.code == 0 || !strings.Contains(out.stderr, "integration_id, integration_execution, integration_generation") || !strings.Contains(out.stderr, "integration-show refs/heads/v2") || strings.Contains(out.stderr, "Integration remains unresolved") {
		t.Fatalf("legacy shape diagnostic: code=%d\n%s", out.code, out.stderr)
	}
	partial := map[string]any{"integration_id": s.final.ID}
	for key, value := range legacy {
		partial[key] = value
	}
	out = s.notify("partial-shape-wrap", partial)
	if out.code == 0 || !strings.Contains(out.stderr, "missing integration_execution, integration_generation") {
		t.Fatalf("partial shape diagnostic: code=%d\n%s", out.code, out.stderr)
	}
	complete := map[string]any{"integration_execution": s.final.Request.ExecutionID, "integration_generation": s.final.Generation}
	for key, value := range partial {
		complete[key] = value
	}
	wrongGeneration := map[string]any{"integration_generation": s.final.Generation + 1}
	wrongTarget := map[string]any{"target_ref": "refs/remotes/origin/v2"}
	for name, change := range map[string]map[string]any{"wrong-generation": wrongGeneration, "wrong-target": wrongTarget} {
		forged := map[string]any{}
		for key, value := range complete {
			forged[key] = value
		}
		for key, value := range change {
			forged[key] = value
		}
		out = s.notify(name+"-wrap", forged)
		if out.code == 0 || !strings.Contains(out.stderr, "differs from the published integration") {
			t.Fatalf("%s accepted or misdiagnosed: code=%d\n%s", name, out.code, out.stderr)
		}
	}
	wrongExecution := map[string]any{}
	for key, value := range complete {
		wrongExecution[key] = value
	}
	wrongExecution["integration_execution"] = "unrelated-execution"
	if out = s.notify("wrong-execution-wrap", wrongExecution); out.code == 0 {
		t.Fatal("unrelated integration execution accepted")
	}
	if requests := s.requests(); len(requests) != 0 {
		t.Fatalf("rejected evidence created requests: %+v", requests)
	}
	if _, err := s.holder.Project.Dispatch(ctx, s.task, "legacy-shape-wrap"); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("rejected evidence created a dispatch: %v", err)
	}

	out = s.notify("published-receipt-wrap", complete).require(t, "complete wrap-up proposal")
	requests := s.requests()
	if len(requests) != 1 || requests[0].Dispatch == nil || requests[0].Dispatch.ID != "published-receipt-wrap" {
		t.Fatalf("proposal=%s requests=%+v", out.stdout, requests)
	}
	proposal := requests[0]
	prepared, err := s.holder.Project.DispatchAction(ctx, "prepare-published-wrap", s.task, "prepare", api.DispatchCommand{Input: proposal.Dispatch, CardRevision: proposal.CardRevision}, *s.credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := s.holder.Project.Move(ctx, "accept-published-wrap", s.task, api.Move{State: "working", Authorization: prepared.Authorization}, s.credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.holder.Project.PutDocument(ctx, "wrap-report", s.task, api.Document{Path: "report.md", Text: "Integrated through the published receipt; the execution worktree was removed before completion.", ExpectedRevision: accepted.Revision, Authorization: prepared.Authorization}, s.credential.Claim()); err != nil {
		t.Fatal(err)
	}
	// Cleanup removes the recorded execution worktree before completion.
	if err := os.RemoveAll(s.exec); err != nil {
		t.Fatal(err)
	}
	completion := api.Move{State: "done", Result: "completed", Authorization: prepared.Authorization, DeliveryCommit: s.commit}
	t.Chdir(s.foreign)
	if _, err := s.holder.MoveTask(ctx, s.task, completion); err == nil || !strings.Contains(err.Error(), "No checkout of this project") {
		t.Fatalf("another project's clone verified the completion: %v", err)
	}
	forged := completion
	forged.Authorization.Epoch++
	if _, err := s.holder.Project.Move(ctx, "forged-epoch-done", s.task, forged, s.credential.Claim()); err == nil {
		t.Fatal("service accepted a completion of another epoch")
	}
	t.Chdir(s.second)
	done, err := s.holder.MoveTask(ctx, s.task, completion)
	if err != nil || done.State != "done" || done.MoveReceipt == nil || done.MoveReceipt.Dispatch == nil || done.MoveReceipt.Dispatch.State != board.DispatchCompleted || done.Claim.Status != api.ClaimReleased {
		t.Fatalf("gitless service completion: %+v %v", done, err)
	}
	settled := s.requests()
	if len(settled) != 1 || settled[0].Status != api.RequestDone {
		t.Fatalf("wrap-up request after completion: %+v", settled)
	}
	replay, err := s.holder.MoveTask(ctx, s.task, completion)
	if err != nil || replay.MoveReceipt == nil || !replay.MoveReceipt.Replayed || replay.Revision != done.Revision {
		t.Fatalf("completed receipt replay: %+v %v", replay, err)
	}
	if again := s.requests(); again[0].Revision != settled[0].Revision {
		t.Fatalf("replay rewrote the settled request: %+v", again)
	}
	// A server before this fix left the request accepted after completion. The
	// exact completed receipt settles it without a takeover of the done card,
	// from any client; another payload neither replays nor settles.
	s.rewindRequest(settled[0])
	if _, err := s.coordinator.Project.Move(ctx, "forged-replay", s.task, api.Move{State: "done", Result: "completed", Authorization: prepared.Authorization, DeliveryCommit: s.base}, nil); err == nil {
		t.Fatal("replay with another delivery commit accepted")
	}
	if again := s.requests(); again[0].Status != api.RequestAccepted {
		t.Fatalf("forged replay settled the request: %+v", again)
	}
	if _, err := s.coordinator.Project.Move(ctx, "exact-replay", s.task, completion, nil); err != nil {
		t.Fatal(err)
	}
	if again := s.requests(); again[0].Status != api.RequestDone {
		t.Fatalf("exact replay left the request: %+v", again)
	}
}

// rewindRequest restores the bytes an earlier server left: the settled
// request back in accepted.
func (s *wrapUpScenario) rewindRequest(request api.CollabRequest) {
	t := s.t
	var path string
	err := filepath.WalkDir(s.data, func(name string, entry os.DirEntry, err error) error {
		if err == nil && entry.Name() == request.ID+".json" && filepath.Base(filepath.Dir(filepath.Dir(name))) == "requests" {
			path = name
		}
		return err
	})
	if err != nil || path == "" {
		t.Fatalf("request file: %q %v", path, err)
	}
	request.Status, request.Note, request.Revision = api.RequestAccepted, "holder prepared the proposed dispatch; business acceptance still requires its receipt", request.Revision-1
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
