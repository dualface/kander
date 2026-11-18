package board

import (
	"strings"
	"testing"
	"time"
)

func sha(c string) string { return strings.Repeat(c, 40) }

func TestReviewNoDeliveryDefinition(t *testing.T) {
	na := noReviewRequirements()
	required := map[string]string{"PMQA": "required", "Security": "N/A: fixture"}
	cases := []struct {
		name string
		b    ReviewPlanBatch
		want bool
	}{
		{"same commit, all N/A", ReviewPlanBatch{Base: sha("a"), TargetCommit: sha("a"), Requirements: na}, true},
		{"different commits", ReviewPlanBatch{Base: sha("a"), TargetCommit: sha("b"), Requirements: na}, false},
		{"required role", ReviewPlanBatch{Base: sha("a"), TargetCommit: sha("a"), Requirements: required}, false},
		{"non-Git N/A batch", ReviewPlanBatch{Base: "N/A", TargetCommit: "N/A", Requirements: na}, false},
		{"short SHA", ReviewPlanBatch{Base: "abc", TargetCommit: "abc", Requirements: na}, false},
		{"missing requirements", ReviewPlanBatch{Base: sha("a"), TargetCommit: sha("a")}, false},
	}
	for _, c := range cases {
		if got := ReviewNoDelivery(c.b); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// noDeliveryPlan plans one batch whose base equals its target, as a card with no
// delivery commit does.
func noDeliveryPlan(t *testing.T, root, id string) {
	t.Helper()
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + id, Author: "coordinator", Basis: "no delivery commit", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: sha("a"), TargetCommit: sha("a"), Requirements: noReviewRequirements()}}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
}

func closeWithEvidence(t *testing.T, root string, mark func(*ReviewGitEvidence)) (ReviewClosure, error) {
	t.Helper()
	v, err := ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{}}
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		t.Fatal(err)
	}
	evidence := ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}
	mark(&evidence)
	return CloseReviewBatch(root, r, evidence)
}

func batchClosed(t *testing.T, root string) bool {
	t.Helper()
	closed := false
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var c ReviewClosure
		var e error
		closed, e = readReviewJSON(tx, closureName("batch"), &c)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return closed
}

func TestNoDeliveryClosureIsBoundToANoDeliveryBatch(t *testing.T) {
	observed := sha("c")
	t.Run("closes and passes the gate", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "no-delivery-ok")
		noDeliveryPlan(t, root, id)
		c, err := closeWithEvidence(t, root, func(e *ReviewGitEvidence) { e.NoDelivery, e.ObservedHead = true, observed })
		if err != nil {
			t.Fatal(err)
		}
		if !c.Git.NoDelivery || c.Git.ObservedHead != observed || c.Git.Head != sha("a") {
			t.Fatalf("%+v", c.Git)
		}
		if !strings.Contains(reviewJSON(c), `"no_delivery": true`) || !strings.Contains(reviewJSON(c), `"observed_head": "`+observed+`"`) {
			t.Fatal(reviewJSON(c))
		}
		if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
			t.Fatalf("%v", problems)
		}
		if p, err := ReviewTaskProgress(root, id); err != nil || p.Status != "closed" {
			t.Fatalf("%+v %v", p, err)
		}
	})
	t.Run("a closure without the marker stays valid", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "no-delivery-legacy")
		noDeliveryPlan(t, root, id)
		if _, err := closeWithEvidence(t, root, func(*ReviewGitEvidence) {}); err != nil {
			t.Fatal(err)
		}
		if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
			t.Fatalf("%v", problems)
		}
	})
	t.Run("observed HEAD needs the marker, and the marker needs a full SHA", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "no-delivery-shape")
		noDeliveryPlan(t, root, id)
		for name, mark := range map[string]func(*ReviewGitEvidence){
			"head without marker": func(e *ReviewGitEvidence) { e.ObservedHead = observed },
			"marker without head": func(e *ReviewGitEvidence) { e.NoDelivery = true },
			"short head":          func(e *ReviewGitEvidence) { e.NoDelivery, e.ObservedHead = true, "abc" },
		} {
			if _, err := closeWithEvidence(t, root, mark); err == nil {
				t.Fatalf("%s accepted", name)
			}
			if batchClosed(t, root) {
				t.Fatalf("%s wrote a closure", name)
			}
		}
	})
	t.Run("a batch with a delivery refuses the marker", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "no-delivery-refused")
		gatePlan(t, root, []string{id}, noReviewRequirements())
		_, err := closeWithEvidence(t, root, func(e *ReviewGitEvidence) { e.NoDelivery, e.ObservedHead = true, observed })
		if err == nil {
			t.Fatalf("accepted: %v", err)
		}
		if batchClosed(t, root) {
			t.Fatal("closure written")
		}
	})
	t.Run("a required role is a delivery even when base equals target", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "no-delivery-required")
		p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + id, Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: sha("a"), TargetCommit: sha("a"), Requirements: archiveRequirements()}}}
		if err := CreateReviewPlan(root, p); err != nil {
			t.Fatal(err)
		}
		run := gateRun(t, root, ReviewInput{RunID: "r1", BatchID: "batch", TaskIDs: []string{id}, Role: "PMQA", Reviewer: "codex", Model: "model", Effort: "high", CWD: "/repo", Base: sha("a"), Commit: sha("a"), ReportLanguage: "en"}, emptyFindings())
		assignGate(t, root, run, map[string][]string{})
		v, err := ReadReviewBatchView(root, "batch")
		if err != nil {
			t.Fatal(err)
		}
		r := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{"PMQA": passRole(run)}}
		edges, _, err := ReviewClosureEdges(v, r)
		if err != nil {
			t.Fatal(err)
		}
		_, err = CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: sha("a"), NoDelivery: true, ObservedHead: observed, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
		if err == nil {
			t.Fatal("marker accepted on a batch with a required role")
		}
	})
}

// rewriteClosure replaces the stored closure and every card copy, as a forged or
// damaged record would look to the readers.
func rewriteClosure(t *testing.T, root, id string, mutate func(*ReviewClosure)) {
	t.Helper()
	err := WithTransaction(root, reviewScope([]string{id}, false), func(tx *Transaction) error {
		var c ReviewClosure
		if ok, e := readReviewJSON(tx, closureName("batch"), &c); e != nil || !ok {
			t.Fatalf("closure missing: %v", e)
		}
		mutate(&c)
		if e := tx.PutGroup(reviewControlGroup, closureName("batch"), reviewJSON(c)); e != nil {
			return e
		}
		return tx.Put(id, "reviews/batches/batch/closed.json", reviewJSON(c))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestForgedNoDeliveryMarkerFailsCheckAndDone(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "no-delivery-forged")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
		t.Fatalf("honest closure: %v", problems)
	}
	rewriteClosure(t, root, id, func(c *ReviewClosure) { c.Git.NoDelivery, c.Git.ObservedHead = true, sha("c") })
	problems := CheckReviewGate(root, []string{id})
	if len(problems) == 0 {
		t.Fatal("check accepted a no-delivery marker on a batch with a delivery")
	}
	s := transactionSnapshot(t, root, id)
	summary := strings.Replace(s.Text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\n完成", 1)
	updateSnapshot(t, root, s, summary)
	s = transactionSnapshot(t, root, id)
	if _, err := MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"}); err == nil {
		t.Fatal("done accepted a forged no-delivery closure")
	}
}

func TestClosureWithoutNoDeliveryFieldsReencodesUnchanged(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "no-delivery-bytes")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
		t.Fatal(err)
	}
	err := WithTransaction(root, reviewScope([]string{id}, true), func(tx *Transaction) error {
		raw, ok, e := tx.ReadGroup(reviewControlGroup, closureName("batch"))
		if e != nil || !ok {
			t.Fatalf("closure missing: %v", e)
		}
		if strings.Contains(string(raw), "no_delivery") || strings.Contains(string(raw), "observed_head") {
			t.Fatalf("old shape gained fields: %s", raw)
		}
		var c ReviewClosure
		if e = DecodeReviewJSON(raw, &c); e != nil {
			return e
		}
		if reviewJSON(c) != string(raw) {
			t.Fatalf("re-encoded bytes differ:\n%s\n%s", reviewJSON(c), raw)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
