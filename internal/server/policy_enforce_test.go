package server_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
)

// enforcedPolicy is a policy write: the branch mode, the PMQA mode of both
// scales, the Security mode of both scales (auto when empty), and the review
// rule.
type enforcedPolicy struct {
	mode               string
	smallPMQA, largePM string
	review             bool
	security           string
}

// putPolicy writes values as the next policy revision and returns it.
func putPolicy(t *testing.T, p *client.Project, values enforcedPolicy) uint64 {
	t.Helper()
	current, err := p.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	rules := config.DefaultRules(true)
	rules[config.RuleReview] = values.review
	security := values.security
	if security == "" {
		security = "auto"
	}
	raw, err := json.Marshal(map[string]any{
		"git":            map[string]string{"branch_mode": values.mode},
		"review_stages":  map[string]map[string]string{"small": {"PMQA": values.smallPMQA, "Security": security}, "large": {"PMQA": values.largePM, "Security": security}},
		"rules":          rules,
		"agent_language": "en",
		"issue":          map[string]string{"result_on_done": "auto"},
	})
	if err != nil {
		t.Fatal(err)
	}
	written, err := p.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: current.Revision, Values: raw})
	if err != nil {
		t.Fatal(err)
	}
	return written.Revision
}

func requiredPMQA() enforcedPolicy {
	return enforcedPolicy{mode: "main-develop", smallPMQA: "required", largePM: "required", review: true}
}

// claimFor claims task id for a fresh execution with the policy context.
func claimFor(t *testing.T, p *client.Project, key, id, execution string, revision uint64, mode string) (api.ClaimGrant, error) {
	t.Helper()
	return p.Claim(t.Context(), key, id, api.ClaimRequest{ClientID: "host-a", ExecutionID: execution, Owner: "claude", PolicyRevision: revision, BranchMode: mode})
}

// reviewer prepares runs of one claimed card.
type reviewer struct {
	p     *client.Project
	task  string
	proof client.Claim
}

func newReviewer(t *testing.T, p *client.Project, slug string, revision uint64) reviewer {
	t.Helper()
	task := todoTask(t, p, slug)
	grant, err := claimFor(t, p, "", task.TaskID, "exec-"+slug, revision, "main-develop")
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("c", 64)
	return reviewer{p: p, task: task.TaskID, proof: client.Claim{Token: grant.Token, Generation: grant.Claim.Generation, ReviewWriter: secret}}
}

// request prepares a run of the first role requirements marks required.
func (r reviewer) request(run, batch string, requirements, decisions map[string]string, revision uint64) api.ReviewPrepare {
	role := "PMQA"
	if requirements["PMQA"] != "required" {
		role = "Security"
	}
	return api.ReviewPrepare{
		Input:        board.ReviewInput{RunID: run, BatchID: batch, TaskIDs: []string{r.task}, Role: role, Reviewer: "codex", Model: "test", Effort: "high", CWD: "/clone", Base: strings.Repeat("a", 40), Commit: strings.Repeat("b", 40), ReportLanguage: "en"},
		Requirements: requirements, RequirementDecisions: decisions, PolicyRevision: revision,
		Originals: map[string][]byte{"task-context.md": []byte("frozen task"), "review-context.md": []byte("context")},
		Version:   "test", WriterHash: board.ReviewDigest([]byte(r.proof.ReviewWriter)),
	}
}

func (r reviewer) prepare(t *testing.T, key string, request api.ReviewPrepare) (board.ReviewPreparation, error) {
	t.Helper()
	proof := r.proof
	raw, err := r.p.ReviewRuntime(t.Context(), key, request.Input.RunID, "prepare", request, &proof)
	var prepared board.ReviewPreparation
	if err == nil {
		err = json.Unmarshal(raw, &prepared)
	}
	return prepared, err
}

func (r reviewer) batch(t *testing.T, id string) (board.ReviewBatch, bool) {
	t.Helper()
	raw, err := r.p.ReviewResource(t.Context(), "batches", id)
	if missingReviewResource(err) {
		return board.ReviewBatch{}, false
	}
	var batch board.ReviewBatch
	if err == nil {
		err = json.Unmarshal(raw, &batch)
	}
	if err != nil {
		t.Fatal(err)
	}
	return batch, true
}

func (r reviewer) runExists(t *testing.T, id string) bool {
	t.Helper()
	_, err := r.p.ReviewResource(t.Context(), "runs", id)
	if missingReviewResource(err) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

// missingReviewResource reports a review record that does not exist: the
// board refuses a missing batch as invalid evidence.
func missingReviewResource(err error) bool {
	var e *api.Error
	return errors.As(err, &e) && (e.Code == api.CodeNotFound || e.BoardCode == "board.review_evidence_invalid" && strings.Contains(e.Message, "missing"))
}

func (r reviewer) decide(t *testing.T) string {
	t.Helper()
	record, err := r.p.RecordDecision(t.Context(), "", r.task, api.DecisionHuman, humanDecision())
	if err != nil {
		t.Fatal(err)
	}
	return record.ID
}

const naPMQA = "N/A: the user waived it"

// A policy that requires PMQA refuses a new batch that marks it N/A without a
// decision record, or with one no member has, and writes nothing; with a
// decision of a member it accepts the batch and records the decision and the
// policy revision on it.
func TestNewBatchMarkingARequiredRoleNAQuotesADecisionRecord(t *testing.T) {
	english(t)
	p := newFixture(t).project("host-a")
	revision := putPolicy(t, p, requiredPMQA())
	r := newReviewer(t, p, "required-na", revision)
	requirements := map[string]string{"PMQA": naPMQA, "Security": "required"}

	_, err := r.prepare(t, "missing", r.request("run-missing", "batch-na", requirements, nil, revision))
	refusal(t, err, api.CodePolicyRequirementUnmet, http.StatusBadRequest, "", "batch batch-na", "requires PMQA for small tasks")
	if _, found := r.batch(t, "batch-na"); found || r.runExists(t, "run-missing") {
		t.Fatal("a refused batch left a batch or run behind")
	}
	_, err = r.prepare(t, "unknown", r.request("run-unknown", "batch-na", requirements, map[string]string{"PMQA": "no-such-decision"}, revision))
	refusal(t, err, api.CodePolicyRequirementUnmet, http.StatusBadRequest, "", `decision record "no-such-decision" for PMQA does not exist`)

	decision := r.decide(t)
	prepared, err := r.prepare(t, "decided", r.request("run-decided", "batch-na", requirements, map[string]string{"PMQA": decision, "Security": "extra"}, revision))
	if err != nil || !prepared.Fresh {
		t.Fatalf("decided batch: %+v %v", prepared, err)
	}
	batch, found := r.batch(t, "batch-na")
	if !found || batch.PolicyRevision != revision || !reflect.DeepEqual(batch.RequirementDecisions, map[string]string{"PMQA": decision}) {
		t.Fatalf("batch record: %+v", batch)
	}
}

// Only a role the policy requires and the request marks N/A needs a decision:
// skip and auto roles take any valid value, and a revision 0 or a policy with
// review off checks nothing.
func TestNewBatchRoleCombinationsThatNeedNoDecision(t *testing.T) {
	english(t)
	for _, tc := range []struct {
		name   string
		policy *enforcedPolicy
		pmqa   string
	}{
		{"skip role required", &enforcedPolicy{mode: "main-develop", smallPMQA: "skip", largePM: "skip", review: true}, "required"},
		{"auto role required", &enforcedPolicy{mode: "main-develop", smallPMQA: "auto", largePM: "auto", review: true}, "required"},
		{"auto role N/A", &enforcedPolicy{mode: "main-develop", smallPMQA: "auto", largePM: "auto", review: true}, naPMQA},
		{"required role required", &enforcedPolicy{mode: "main-develop", smallPMQA: "required", largePM: "required", review: true}, "required"},
		{"review off", &enforcedPolicy{mode: "main-develop", smallPMQA: "required", largePM: "required", review: false}, naPMQA},
		{"no policy", nil, naPMQA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newFixture(t).project("host-a")
			var revision uint64
			if tc.policy != nil {
				revision = putPolicy(t, p, *tc.policy)
			}
			r := newReviewer(t, p, "combination", revision)
			if _, err := r.prepare(t, "", r.request("run", "batch", map[string]string{"PMQA": tc.pmqa, "Security": "required"}, nil, revision)); err != nil {
				t.Fatal(err)
			}
			batch, _ := r.batch(t, "batch")
			if batch.PolicyRevision != revision || batch.RequirementDecisions != nil {
				t.Fatalf("batch record: %+v", batch)
			}
		})
	}
}

// The scale of a batch is that of its members: a large card takes the large
// stages.
func TestNewBatchOfALargeCardUsesTheLargeStages(t *testing.T) {
	english(t)
	p := newFixture(t).project("host-a")
	revision := putPolicy(t, p, enforcedPolicy{mode: "main-develop", smallPMQA: "skip", largePM: "required", review: true})
	task, err := p.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "large-card", Title: "Large", Language: "en", Large: true})
	if err != nil {
		t.Fatal(err)
	}
	r := newReviewer(t, p, "small-card", revision)
	_, err = r.prepare(t, "", r.request("run", "batch", map[string]string{"PMQA": naPMQA, "Security": "required"}, nil, revision))
	if err != nil {
		t.Fatalf("small card under skip: %v", err)
	}
	large := todoLarge(t, p, task)
	lr := r
	lr.task = large.task
	lr.proof = large.proof
	_, err = lr.prepare(t, "", lr.request("run-large", "batch-large", map[string]string{"PMQA": naPMQA, "Security": "required"}, nil, revision))
	refusal(t, err, api.CodePolicyRequirementUnmet, http.StatusBadRequest, "", "requires PMQA for large tasks")
}

// todoLarge completes a card created with --large and claims it.
func todoLarge(t *testing.T, p *client.Project, task api.Task) reviewer {
	t.Helper()
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	task, err := p.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	current, err := p.Policy(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	grant, err := claimFor(t, p, "", task.TaskID, "exec-large", current.Revision, "main-develop")
	if err != nil {
		t.Fatal(err)
	}
	return reviewer{p: p, task: task.TaskID, proof: client.Claim{Token: grant.Token, Generation: grant.Claim.Generation, ReviewWriter: strings.Repeat("d", 64)}}
}

// A stale policy revision refuses a new batch before anything is written,
// not even a receipt: the same key with the current revision then succeeds.
// A run of an existing batch and a retry of a prepared run are not checked:
// the batch keeps the revision it was created with, and the run identity has
// no policy revision.
func TestBatchRequestsCarryTheCurrentPolicyRevision(t *testing.T) {
	english(t)
	p := newFixture(t).project("host-a")
	first := putPolicy(t, p, enforcedPolicy{mode: "main-develop", smallPMQA: "auto", largePM: "auto", review: true})
	r := newReviewer(t, p, "stale-batch", first)
	second := putPolicy(t, p, enforcedPolicy{mode: "main-develop", smallPMQA: "auto", largePM: "auto", review: true})
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: x"}
	_, err := r.prepare(t, "same-key", r.request("run", "batch", requirements, nil, first))
	refusal(t, err, api.CodePolicyRevisionStale, http.StatusConflict, "", "revision 2", "revision 1")
	if _, found := r.batch(t, "batch"); found || r.runExists(t, "run") {
		t.Fatal("a stale request left a batch or run behind")
	}
	prepared, err := r.prepare(t, "same-key", r.request("run", "batch", requirements, nil, second))
	if err != nil || !prepared.Fresh {
		t.Fatalf("resend with the current revision: %+v %v", prepared, err)
	}
	// The policy now requires PMQA; the existing batch marks Security N/A only.
	third := putPolicy(t, p, requiredPMQA())
	again, err := r.prepare(t, "retry", r.request("run", "batch", requirements, nil, first))
	if err != nil || again.Fresh || again.Run.RunID != "run" {
		t.Fatalf("retry of the prepared run after a policy change: %+v %v", again, err)
	}
	if _, err := r.prepare(t, "second-run", r.request("run-two", "batch", requirements, nil, first)); err != nil {
		t.Fatalf("second run of an existing batch: %v", err)
	}
	if batch, _ := r.batch(t, "batch"); batch.PolicyRevision != second || third == second {
		t.Fatalf("existing batch changed: %+v", batch)
	}
}

// An existing batch created without a policy keeps working after the policy
// requires its N/A role: a run of it is not checked.
func TestBatchCreatedBeforeThePolicyIsNotCheckedAgain(t *testing.T) {
	english(t)
	p := newFixture(t).project("host-a")
	r := newReviewer(t, p, "old-batch", 0)
	requirements := map[string]string{"PMQA": naPMQA, "Security": "required"}
	if _, err := r.prepare(t, "", r.request("run-old", "batch-old", requirements, nil, 0)); err != nil {
		t.Fatal(err)
	}
	revision := putPolicy(t, p, requiredPMQA())
	if _, err := r.prepare(t, "", r.request("run-later", "batch-old", requirements, nil, revision)); err != nil {
		t.Fatalf("later run of an old batch: %v", err)
	}
	if batch, _ := r.batch(t, "batch-old"); batch.PolicyRevision != 0 || batch.RequirementDecisions != nil {
		t.Fatalf("old batch gained policy fields: %+v", batch)
	}
}

// kander review plan and extend-plan check the batches they create like a
// run does.
func TestPlanAndExtensionCheckTheBatchesTheyCreate(t *testing.T) {
	english(t)
	p := newFixture(t).project("host-a")
	revision := putPolicy(t, p, requiredPMQA())
	r := newReviewer(t, p, "planned", revision)
	proof := client.Claim{Token: r.proof.Token, Generation: r.proof.Generation}
	requirements := map[string]string{"PMQA": naPMQA, "Security": "required"}
	plan := board.ReviewPlan{Schema: 1, PlanID: "plan-policy", Author: "coordinator", Basis: "policy", CWD: "project:" + p.ID(), ReportLanguage: "en", TaskIDs: []string{r.task}, PolicyRevision: revision,
		Batches: []board.ReviewPlanBatch{{BatchID: "planned-batch", TaskIDs: []string{r.task}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: requirements}}}
	_, err := p.ReviewPlan(t.Context(), "plan-missing", plan, &proof)
	refusal(t, err, api.CodePolicyRequirementUnmet, http.StatusBadRequest, "", "batch planned-batch")
	stale := plan
	stale.PolicyRevision = revision - 1
	_, err = p.ReviewPlan(t.Context(), "plan-stale", stale, &proof)
	refusal(t, err, api.CodePolicyRevisionStale, http.StatusConflict, "")
	if _, found := r.batch(t, "planned-batch"); found {
		t.Fatal("a refused plan created its batch")
	}
	decision := r.decide(t)
	plan.Batches[0].RequirementDecisions = map[string]string{"PMQA": decision}
	if _, err := p.ReviewPlan(t.Context(), "plan-decided", plan, &proof); err != nil {
		t.Fatal(err)
	}
	if batch, _ := r.batch(t, "planned-batch"); batch.PolicyRevision != revision || batch.RequirementDecisions["PMQA"] != decision {
		t.Fatalf("planned batch: %+v", batch)
	}
	raw, err := p.ReviewProgress(t.Context(), r.task)
	if err != nil || !strings.Contains(string(raw), `"batch_policies":{"planned-batch":{"policy_revision":`) {
		t.Fatalf("progress: %s %v", raw, err)
	}

	extension := board.ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: 1, Author: "coordinator", Basis: "append", PolicyRevision: revision,
		Batch: &board.ReviewPlanBatch{BatchID: "appended-batch", PreviousBatchID: "planned-batch", TaskIDs: []string{r.task}, Base: strings.Repeat("b", 40), TargetCommit: strings.Repeat("c", 40), Requirements: requirements}}
	_, err = p.ReviewAction(t.Context(), "extend-missing", "extend-plan", extension, &proof)
	refusal(t, err, api.CodePolicyRequirementUnmet, http.StatusBadRequest, "", "batch appended-batch")
	extension.PolicyRevision = revision - 1
	_, err = p.ReviewAction(t.Context(), "extend-stale", "extend-plan", extension, &proof)
	refusal(t, err, api.CodePolicyRevisionStale, http.StatusConflict, "")
	if _, found := r.batch(t, "appended-batch"); found {
		t.Fatal("a refused extension created its batch")
	}
}

// Security is checked like PMQA: a policy that requires it refuses a new
// batch that marks it N/A without a decision record of a member, and accepts
// and records one that quotes such a decision.
func TestNewBatchMarkingARequiredSecurityNAQuotesADecisionRecord(t *testing.T) {
	english(t)
	p := newFixture(t).project("host-a")
	revision := putPolicy(t, p, enforcedPolicy{mode: "main-develop", smallPMQA: "auto", largePM: "auto", review: true, security: "required"})
	r := newReviewer(t, p, "security-na", revision)
	requirements := map[string]string{"PMQA": "required", "Security": "N/A: no trust boundary changed"}
	_, err := r.prepare(t, "", r.request("run-missing", "batch-security", requirements, nil, revision))
	refusal(t, err, api.CodePolicyRequirementUnmet, http.StatusBadRequest, "", "requires Security for small tasks")
	if _, found := r.batch(t, "batch-security"); found || r.runExists(t, "run-missing") {
		t.Fatal("a refused batch was written")
	}
	decision := r.decide(t)
	if _, err := r.prepare(t, "", r.request("run-decided", "batch-security", requirements, map[string]string{"Security": decision}, revision)); err != nil {
		t.Fatal(err)
	}
	if batch, _ := r.batch(t, "batch-security"); batch.PolicyRevision != revision || !reflect.DeepEqual(batch.RequirementDecisions, map[string]string{"Security": decision}) {
		t.Fatalf("batch record: %+v", batch)
	}
}
