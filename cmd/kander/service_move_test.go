package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/remote"
)

func moveServiceFixture(t *testing.T) (*remote.Session, *config.Config) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	forbidden := filepath.Join(t.TempDir(), "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	t.Cleanup(func() {
		if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
			t.Errorf("CLI used local board: %v", err)
		}
	})
	cfg := config.DefaultConfig()
	cfg.Language = "en"
	cfg.AgentLanguage = "en"
	session := commandService(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return session, cfg
}

func readyServiceCard(t *testing.T, session *remote.Session, task api.Task) api.Task {
	t.Helper()
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.ReplaceAll(text, "- [ ]", "- [x]")
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: move-fixture", 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	text = strings.Replace(text, "## SUMMARY\n", "## SUMMARY\n\nCompleted and verified in an isolated non-Git fixture.\n", 1)
	task, err := session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runKander(t, "move", task.TaskID, "todo").require(t, "ready")
	runKander(t, "move", task.TaskID, "working", "--owner", "codex").require(t, "claim")
	task, err = session.Project.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func closeNonGitFixturePlan(t *testing.T, session *remote.Session, task api.Task) {
	t.Helper()
	credential, found, err := session.Credential(task.TaskID)
	if err != nil || !found {
		t.Fatalf("credential: %v", err)
	}
	plan := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "move-notice-plan", Author: "codex", Basis: "isolated non-Git CLI fixture", CWD: "project:" + session.Info.ID, ReportLanguage: "en", TaskIDs: []string{task.TaskID}, Batches: []board.ReviewPlanBatch{{BatchID: "move-notice-batch", TaskIDs: []string{task.TaskID}, Base: "N/A", TargetCommit: "N/A", Requirements: map[string]string{"PMQA": "N/A: isolated fixture", "Security": "N/A: isolated fixture"}}}}
	if _, err := session.Project.ReviewPlan(t.Context(), "", plan, credential.Claim()); err != nil {
		t.Fatal(err)
	}
	raw, err := session.Project.ReviewAggregate(t.Context(), "", "move-notice-batch", credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	var view board.ReviewBatchView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: view.Batch.BatchID, ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "codex", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	evidence := board.ReviewGitEvidence{CWD: plan.CWD, Head: "N/A", NotApplicable: "isolated non-Git fixture", VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	if _, err := session.Project.ReviewClose(t.Context(), "", map[string]any{"request": request, "evidence": evidence}, credential.Claim()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceCLIDonePrintsCompletionAndIssueHandoffFromCentralSnapshots(t *testing.T) {
	session, _ := moveServiceFixture(t)
	repository := issue.Repository{Host: "github.com", Owner: "test", Name: "repo", URL: "https://github.com/test/repo"}
	snapshot, err := issue.BuildImportSnapshot(issue.IssueSnapshot{Repository: repository, Number: 42, Title: "Completion notice", Body: "Untrusted source body", State: "open", CreatedAt: time.Now(), UpdatedAt: time.Now(), FetchedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := issue.MarshalImportSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := session.Project.ImportIssue(t.Context(), "", api.IssueImport{Snapshot: raw, Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	var result issue.ImportResult
	if err := json.Unmarshal(imported, &result); err != nil {
		t.Fatal(err)
	}
	task, err := session.Project.Show(t.Context(), result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	task = readyServiceCard(t, session, task)
	closeNonGitFixturePlan(t, session, task)
	output := runKander(t, "move", task.TaskID, "done", "--result", "completed").require(t, "complete")
	resource := "kander-server:" + session.Info.Name + "/" + task.TaskID
	if output.stdout != resource+"\n" {
		t.Fatalf("stdout=%q", output.stdout)
	}
	for _, want := range []string{"# Kanban Task Completion Report", resource, "Final card state: done", `Write the report in "en".`, "kander issue result 42 --repo github.com/test/repo --card " + task.TaskID + " --action inspect", "issue.result_on_done=auto"} {
		if !strings.Contains(output.stderr, want) {
			t.Fatalf("missing %q:\n%s", want, output.stderr)
		}
	}
	shown, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil || shown.State != "done" || shown.Claim.Status != api.ClaimReleased {
		t.Fatalf("completion=%+v %v", shown, err)
	}
}

func TestServiceCLIDispatchMoveReturnsExactBusinessReceiptAndReplay(t *testing.T) {
	session, cfg := moveServiceFixture(t)
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "dispatch-output", Title: "Dispatch output", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	task = readyServiceCard(t, session, task)
	credential, _, err := session.Credential(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	input := board.DispatchInput{ID: "move-output-round", TaskID: task.TaskID, Kind: "sync", Base: strings.Repeat("a", 40), Message: "Synchronize this exact generation"}
	dispatch, err := session.Project.DispatchAction(t.Context(), "prepare-output", task.TaskID, "prepare", api.DispatchCommand{Input: &input, CardRevision: task.Revision}, *credential.Claim())
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"move", task.TaskID, "working", "--dispatch-id", dispatch.Input.ID, "--execution-epoch", strconv.FormatUint(dispatch.Authorization.Epoch, 10)}
	for _, replayed := range []bool{false, true} {
		output := runKander(t, args...).require(t, "accept/replay")
		var receipt api.MoveReceipt
		if err := json.Unmarshal([]byte(output.stdout), &receipt); err != nil || receipt.Dispatch == nil || receipt.Replayed != replayed || receipt.Dispatch.State != board.DispatchAccepted {
			t.Fatalf("receipt=%s err=%v", output.stdout, err)
		}
		if strings.Contains(output.stderr, "# Kanban Task Completion Report") {
			t.Fatal("non-terminal move printed completion")
		}
	}
	// Reporting disabled suppresses only advice, preserving the REST move.
	cfg.Rules[config.RuleReporting] = false
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	other, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "quiet-output", Title: "Quiet output", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	out := runKander(t, "move", other.TaskID, "trash", "--result", "trashed", "--reason", "test", "--decision", "test").require(t, "quiet terminal move")
	if strings.Contains(out.stderr, "# Kanban Task Completion Report") {
		t.Fatal("reporting off still printed template")
	}
}
