package launch

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

// snapshotHost is one client home against a shared server.
func snapshotHost(t *testing.T, url, name string) (*remote.Session, *config.Config) {
	t.Helper()
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), name))
	cfg := config.DefaultConfig()
	cfg.Server.URL = url
	cfg.Project.Name = "snapshot-project"
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	session, err := remote.Open(t.Context(), cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	return session, cfg
}

func snapshotServer(t *testing.T) string {
	t.Helper()
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	t.Chdir(t.TempDir())
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "forbidden-board"))
	return httpServer.URL
}

func readyCard(t *testing.T, session *remote.Session, slug, group string) api.Task {
	t.Helper()
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Snapshot " + slug, Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	review := "SELF_REVIEW: PASS\n"
	if group != "" {
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		review += "CARD_REVIEW: PASS\n"
	}
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\n"+review, 1)
	if task, err = session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if task, err = session.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	return task
}

func policyMode(t *testing.T, session *remote.Session, mode string) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Git.BranchMode = mode
	putTestPolicy(t, session, cfg)
}

// The end-to-end case of the card: a group claimed while the policy says
// main-develop keeps that branch model after the policy moves to main-only.
// The coordinator view still shows it, the start branch of a member is
// computed from it, and the dispatch evidence check accepts develop and
// refuses main for the group. A group claimed afterwards, by a host whose
// policy read is stale, gets main-only after the client refreshed it.
func TestGroupSnapshotKeepsTheMainlineAfterThePolicyChanges(t *testing.T) {
	url := snapshotServer(t)
	a, cfgA := snapshotHost(t, url, "host-a")
	policyMode(t, a, config.GitBranchModeMainDevelop)
	b, _ := snapshotHost(t, url, "host-b")
	group := "20261127-snapshot-group"
	members := []string{readyCard(t, a, "snapshot-one", group).TaskID, readyCard(t, a, "snapshot-two", group).TaskID}
	if _, err := b.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: group, Owner: "codex", Token: "public-label", Basis: "snapshot fixture", Members: members}, "snapshot fixture"); err != nil {
		t.Fatal(err)
	}

	policyMode(t, a, config.GitBranchModeMainOnly)
	if err := a.RefreshPolicy(t.Context()); err != nil {
		t.Fatal(err)
	}
	if mode := cfgA.Git.BranchMode; mode != config.GitBranchModeMainOnly {
		t.Fatalf("host A does not work with the new policy: %s", mode)
	}
	view, err := a.Project.Group(t.Context(), group)
	if err != nil || view.Claim == nil || view.Claim.BranchSnapshot == nil || view.Claim.BranchSnapshot.BranchMode != config.GitBranchModeMainDevelop {
		t.Fatalf("coordinator view: %+v %v", view.Claim, err)
	}
	shown, err := json.Marshal(coordinatorShowResult{CoordinatorCheckpoint: *view.Checkpoint, BranchSnapshot: groupSnapshot(view)})
	if err != nil || !strings.Contains(string(shown), `"branch_snapshot":{"branch_mode":"main-develop","policy_revision":1,"scope":"group"}`) || !strings.Contains(string(shown), `"group_id":"`+group+`"`) {
		t.Fatalf("coordinator show: %s %v", shown, err)
	}
	member, err := a.Project.Show(t.Context(), members[0])
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := branchSnapshotOf(t.Context(), a, member)
	if err != nil || snapshot == nil || snapshot.Scope != api.BranchSnapshotGroup {
		t.Fatalf("member snapshot: %+v %v", snapshot, err)
	}
	if mainline := mainlineFor(cfgA, snapshot); mainline != "develop" {
		t.Fatalf("member mainline: %s", mainline)
	}
	if branch, err := startBranch(cfgA, member.Text, snapshot); err != nil || branch != "group/"+group {
		t.Fatalf("member start branch: %s %v", branch, err)
	}

	cwd, first := integrationGit(t)
	gitIn(t, cwd, "branch", "main", first)
	useGitBranchMode(t, config.GitBranchModeMainOnly)
	if err := checkIntegrationTarget(t.Context(), targetEvidence(cwd, first, "refs/heads/develop"), "", snapshot, false); err != nil {
		t.Fatalf("develop refused for the group: %v", err)
	}
	if err := checkIntegrationTarget(t.Context(), targetEvidence(cwd, first, "refs/heads/main"), "", snapshot, false); err == nil {
		t.Fatal("main accepted for the group")
	}

	// Host B still works with the policy it read before the change.
	later := "20261127-later-snapshot-group"
	laterMember := readyCard(t, a, "later-one", later).TaskID
	grant, err := b.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: later, Owner: "codex", Token: "public-label", Basis: "snapshot fixture", Members: []string{laterMember}}, "snapshot fixture")
	if err != nil {
		t.Fatalf("stale group claim was not retried: %v", err)
	}
	if s := grant.Claim.BranchSnapshot; s == nil || s.BranchMode != config.GitBranchModeMainOnly || s.PolicyRevision != b.PolicyRevision() {
		t.Fatalf("later group snapshot: %+v (host B at revision %d)", s, b.PolicyRevision())
	}
}

// Without a snapshot the mainline branch stays that of the configuration;
// with one, the configuration is not read.
func TestStartBranchFollowsTheSnapshot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Git.BranchMode = config.GitBranchModeMainOnly
	develop := &api.BranchSnapshot{BranchMode: config.GitBranchModeMainDevelop, Scope: api.BranchSnapshotTask}
	card := "# Card\n\n- TYPE: Feature\n"
	for _, tc := range []struct {
		snapshot *api.BranchSnapshot
		want     string
	}{{nil, "main"}, {develop, "develop"}} {
		if branch, err := startBranch(cfg, card, tc.snapshot); err != nil || branch != tc.want {
			t.Fatalf("snapshot %+v: %s %v", tc.snapshot, branch, err)
		}
	}
	targetMain := "# Card\n\n- TYPE: Feature\n- INTEGRATION_TARGET: main\n"
	if _, err := startBranch(cfg, targetMain, develop); err == nil {
		t.Fatal("main accepted as a target under a main-develop snapshot")
	}
	if err := verifyPortableWrapUpTarget(t.TempDir(), "main", "refs/heads/main", develop); err == nil {
		t.Fatal("main accepted as a wrap-up target under a main-develop snapshot")
	}
}

// The launch journal of a claim refused as stale takes the new revision in
// one save and resends the claim with its key.
func TestJournaledClaimTakesTheNewPolicyRevision(t *testing.T) {
	url := snapshotServer(t)
	session, _ := snapshotHost(t, url, "host-journal")
	policyMode(t, session, config.GitBranchModeMainDevelop)
	if err := session.RefreshPolicy(t.Context()); err != nil {
		t.Fatal(err)
	}
	frozen := session.PolicyRevision()
	task := readyCard(t, session, "journal-claim", "")
	policyMode(t, session, config.GitBranchModeMainOnly)
	request := api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "journal-execution", Owner: "codex", PolicyRevision: frozen, BranchMode: config.GitBranchModeMainDevelop}
	journal, _, err := session.BeginAction("journal-claim-test", task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := freezeRemoteLaunch(journal, task.TaskID, request, nil, false, ""); err != nil {
		t.Fatal(err)
	}
	key := journal.Value.ClaimKey
	grant, err := claimJournaled(t.Context(), session, journal, nil)
	if err != nil {
		journal.Close()
		t.Fatal(err)
	}
	if journal.Value.ClaimKey != key || journal.Value.ClaimInput.PolicyRevision != frozen+1 || journal.Value.ClaimInput.BranchMode != config.GitBranchModeMainOnly {
		t.Fatalf("journal: %+v", journal.Value)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	saved, found, err := session.PeekAction("journal-claim-test", task.TaskID)
	if err != nil || !found || saved.ClaimInput == nil || saved.ClaimInput.PolicyRevision != frozen+1 {
		t.Fatalf("saved journal: %+v %v", saved, err)
	}
	if s := grant.Claim.BranchSnapshot; s == nil || s.BranchMode != config.GitBranchModeMainOnly {
		t.Fatalf("snapshot: %+v", s)
	}
}

// A card without a declared integration target integrates into the mainline
// branch of its branch snapshot: a distributed wrap-up receipt for another
// branch is refused, even after the policy moved to that branch's model. A
// card claimed before snapshots keeps letting the receipt supply the target.
func TestPortableWrapUpTargetDefaultsToTheSnapshotMainline(t *testing.T) {
	cwd, _ := integrationGit(t)
	refused := launchError("board.dispatch_evidence_invalid", "integration CWD or target ref").Error()
	develop := &api.BranchSnapshot{BranchMode: config.GitBranchModeMainDevelop, Scope: api.BranchSnapshotGroup}
	mainOnly := &api.BranchSnapshot{BranchMode: config.GitBranchModeMainOnly, PolicyRevision: 2, Scope: api.BranchSnapshotTask}
	useGitBranchMode(t, config.GitBranchModeMainOnly)
	for _, tc := range []struct {
		name     string
		snapshot *api.BranchSnapshot
		ref      string
		ok       bool
	}{
		{"main-develop snapshot, develop", develop, "refs/heads/develop", true},
		{"main-develop snapshot, main", develop, "refs/heads/main", false},
		{"main-develop snapshot, other branch", develop, "refs/heads/v2", false},
		{"main-only snapshot, main", mainOnly, "refs/heads/main", true},
		{"main-only snapshot, develop", mainOnly, "refs/heads/develop", false},
		{"no snapshot", nil, "refs/heads/v2", true},
	} {
		err := verifyPortableWrapUpTarget(cwd, "", tc.ref, tc.snapshot)
		if tc.ok && err != nil || !tc.ok && (err == nil || err.Error() != refused) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if !portableTargetNeedsSnapshot("") || !portableTargetNeedsSnapshot("main") || portableTargetNeedsSnapshot("v2") {
		t.Fatal("the snapshot lookup does not cover the default target")
	}
}

// The coordinator Git facts of a wrap-up are checked against the group's
// snapshot through the service: a group claimed under main-develop keeps
// refusing an integration receipt for main after the policy moved to
// main-only, before any receipt or Git verification.
func TestCoordinatorWrapUpFactsRefuseAReceiptOutsideTheSnapshotMainline(t *testing.T) {
	url := snapshotServer(t)
	a, _ := snapshotHost(t, url, "host-a")
	policyMode(t, a, config.GitBranchModeMainDevelop)
	b, _ := snapshotHost(t, url, "host-b")
	group := "20261127-wrap-up-target-group"
	member := readyCard(t, a, "wrap-up-target", group).TaskID
	if _, err := b.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: group, Owner: "codex", Token: "public-label", Basis: "snapshot fixture", Members: []string{member}}, "snapshot fixture"); err != nil {
		t.Fatal(err)
	}
	policyMode(t, a, config.GitBranchModeMainOnly)

	cwd, _ := integrationGit(t)
	refused := launchError("board.dispatch_evidence_invalid", "integration CWD or target ref").Error()
	facts := board.CoordinatorGitFacts{Integration: board.DispatchIntegration{TaskID: member, TargetRef: "refs/heads/main"}}
	if err := verifyRemoteCoordinatorGitFacts(t.Context(), a, cwd, facts); err == nil || err.Error() != refused {
		t.Fatalf("a receipt for main passed the group's main-develop snapshot: %v", err)
	}
	// develop passes the target check and reaches the receipt verification,
	// which refuses this incomplete receipt for its own reason.
	facts.Integration.TargetRef = "refs/heads/develop"
	if err := verifyRemoteCoordinatorGitFacts(t.Context(), a, cwd, facts); err == nil || err.Error() == refused {
		t.Fatalf("develop was refused as the target of the group: %v", err)
	}
}

// The review end-to-end case of the card, with two client homes: host A sets a
// policy that requires PMQA for large tasks while host B's own configuration
// skips it. B's batch that marks PMQA N/A without a decision record is
// refused, and the same batch quoting a decision record of the card is
// accepted and records it.
func TestReviewPolicyOfOneHostBindsTheBatchesOfAnother(t *testing.T) {
	url := snapshotServer(t)
	a, _ := snapshotHost(t, url, "host-a")
	required := config.DefaultConfig()
	required.ReviewStages["large"]["PMQA"] = "required"
	putTestPolicy(t, a, required)

	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "host-b"))
	local := config.DefaultConfig()
	local.Server.URL, local.Project.Name = url, "snapshot-project"
	local.ReviewStages["large"]["PMQA"] = "skip"
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	b, err := remote.Open(t.Context(), local, dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := config.LocalPolicyValues(local).ReviewStages["large"]["PMQA"]; mode != "skip" {
		t.Fatalf("host B's own configuration: %s", mode)
	}

	task, err := b.Project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "two-homes", Title: "Two homes", Language: "en", Large: true})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	if task, err = b.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	grant, err := b.Project.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: b.Client.ClientID(), ExecutionID: "two-homes", Owner: "codex", PolicyRevision: b.PolicyRevision(), BranchMode: b.BranchMode()})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	plan := board.ReviewPlan{Schema: 1, PlanID: "two-homes-plan", Author: "codex", Basis: "two homes", CWD: "project:" + b.Info.ID, ReportLanguage: "en", TaskIDs: []string{task.TaskID}, PolicyRevision: b.PolicyRevision(),
		Batches: []board.ReviewPlanBatch{{BatchID: "two-homes-batch", TaskIDs: []string{task.TaskID}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: map[string]string{"PMQA": "N/A: host B skips it", "Security": "required"}}}}
	_, err = b.Project.ReviewPlan(t.Context(), "two-homes-missing", plan, &proof)
	if !api.IsCode(err, api.CodePolicyRequirementUnmet) {
		t.Fatalf("host B's N/A without a decision: %v", err)
	}
	decision, err := b.Project.RecordDecision(t.Context(), "", task.TaskID, api.DecisionHuman, api.DecisionInput{Question: "Skip PMQA for this card?", Options: []api.DecisionOption{{Key: "skip", Label: "Skip"}}, Answer: "skip", Channel: "web", RecordedBy: "reported-account"})
	if err != nil {
		t.Fatal(err)
	}
	plan.Batches[0].RequirementDecisions = map[string]string{"PMQA": decision.ID}
	if _, err := b.Project.ReviewPlan(t.Context(), "two-homes-decided", plan, &proof); err != nil {
		t.Fatalf("host B's N/A with a decision: %v", err)
	}
	raw, err := a.Project.ReviewResource(t.Context(), "batches", "two-homes-batch")
	var batch board.ReviewBatch
	if err == nil {
		err = json.Unmarshal(raw, &batch)
	}
	if err != nil || batch.PolicyRevision != a.PolicyRevision() || batch.RequirementDecisions["PMQA"] != decision.ID {
		t.Fatalf("batch read by host A: %+v %v", batch, err)
	}
}
