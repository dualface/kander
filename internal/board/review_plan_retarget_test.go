package board

import (
	"fmt"
	"testing"
)

// retargetWorld plans b1 (a..b) and b2 (b..c), both with every role N/A.
func retargetWorld(t *testing.T, slug string, sealed bool) (root, id string, p ReviewPlan) {
	t.Helper()
	root = tempBoard(t)
	id = gateCard(t, root, slug)
	p = ReviewPlan{Schema: 1, Sealed: sealed, PlanID: "plan-" + id, Author: "coordinator", Basis: "fixture", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{
		{BatchID: "b1", TaskIDs: []string{id}, Base: sha("a"), TargetCommit: sha("b"), Requirements: noReviewRequirements()},
		{BatchID: "b2", PreviousBatchID: "b1", TaskIDs: []string{id}, Base: sha("b"), TargetCommit: sha("c"), Requirements: noReviewRequirements()},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	stored, err := ReadReviewPlan(root, p.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	return root, id, stored
}

func retarget(root string, p ReviewPlan, batch string, r ReviewBatchRetarget) error {
	return ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, RetargetBatches: map[string]ReviewBatchRetarget{batch: r}, Author: "coordinator", Basis: "the target was written wrong"})
}

// untouched checks that a refused request changed no plan, batch, or copy.
func untouched(t *testing.T, root, id string, p ReviewPlan, batch string) {
	t.Helper()
	got, err := ReadReviewPlan(root, p.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != p.Revision || reviewJSON(got) != reviewJSON(p) || reviewJSON(readPlanCopy(t, root, id)) != reviewJSON(p) {
		t.Fatalf("plan changed: %+v", got)
	}
	b, err := ReadReviewBatch(root, batch)
	if err != nil {
		t.Fatal(err)
	}
	if b.Revision != 1 {
		t.Fatalf("batch revision %d", b.Revision)
	}
}

func TestRetargetCorrectsAnUnrunBatch(t *testing.T) {
	for _, sealed := range []bool{false, true} {
		t.Run(fmt.Sprintf("sealed=%v", sealed), func(t *testing.T) {
			root, id, p := retargetWorld(t, fmt.Sprintf("retarget-ok-%v", sealed), sealed)
			if err := retarget(root, p, "b2", ReviewBatchRetarget{PreviousBase: sha("b"), PreviousTarget: sha("c"), Base: sha("b"), Target: sha("d")}); err != nil {
				t.Fatal(err)
			}
			got, err := ReadReviewPlan(root, p.PlanID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Revision != p.Revision+1 || got.Sealed != sealed || got.Batches[1].TargetCommit != sha("d") || got.Batches[0].TargetCommit != sha("b") {
				t.Fatalf("plan: %+v", got)
			}
			if reviewJSON(readPlanCopy(t, root, id)) != reviewJSON(got) {
				t.Fatal("card plan copy differs from the plan")
			}
			b, err := ReadReviewBatch(root, "b2")
			if err != nil || b.TargetCommit != sha("d") || b.Base != sha("b") || b.Revision != 2 {
				t.Fatalf("batch: %+v %v", b, err)
			}
			var history struct {
				Previous   ReviewPlan          `json:"previous"`
				Request    ReviewPlanExtension `json:"request"`
				RecordedAt string              `json:"recorded_at"`
			}
			err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
				raw, ok, e := tx.ReadGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", p.PlanID, got.Revision))
				if e != nil || !ok {
					t.Fatalf("plan history missing: %v", e)
				}
				return DecodeReviewJSON(raw, &history)
			})
			if err != nil {
				t.Fatal(err)
			}
			r := history.Request.RetargetBatches["b2"]
			if history.Previous.Batches[1].TargetCommit != sha("c") || r.PreviousTarget != sha("c") || r.Target != sha("d") || history.Request.Author != "coordinator" || history.Request.Basis == "" {
				t.Fatalf("history: %+v", history)
			}
			if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
				t.Fatalf("%v", problems)
			}
		})
	}
}

func TestRetargetKeepsTheBatchChain(t *testing.T) {
	cases := []struct {
		name  string
		batch string
		r     ReviewBatchRetarget
	}{
		{"base differs from the predecessor target", "b2", ReviewBatchRetarget{PreviousBase: sha("b"), PreviousTarget: sha("c"), Base: sha("e"), Target: sha("d")}},
		{"target of a batch with a successor", "b1", ReviewBatchRetarget{PreviousBase: sha("a"), PreviousTarget: sha("b"), Base: sha("a"), Target: sha("e")}},
		{"stale previous target", "b2", ReviewBatchRetarget{PreviousBase: sha("b"), PreviousTarget: sha("d"), Base: sha("b"), Target: sha("e")}},
		{"stale previous base", "b2", ReviewBatchRetarget{PreviousBase: sha("e"), PreviousTarget: sha("c"), Base: sha("b"), Target: sha("d")}},
		{"nothing changes", "b2", ReviewBatchRetarget{PreviousBase: sha("b"), PreviousTarget: sha("c"), Base: sha("b"), Target: sha("c")}},
		{"short SHA", "b2", ReviewBatchRetarget{PreviousBase: sha("b"), PreviousTarget: sha("c"), Base: sha("b"), Target: "abc"}},
		{"unknown batch", "b9", ReviewBatchRetarget{PreviousBase: sha("b"), PreviousTarget: sha("c"), Base: sha("b"), Target: sha("d")}},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, id, p := retargetWorld(t, fmt.Sprintf("retarget-chain-%d", i), false)
			if err := retarget(root, p, c.batch, c.r); err == nil {
				t.Fatal("accepted")
			}
			untouched(t, root, id, p, "b1")
			untouched(t, root, id, p, "b2")
		})
	}
	t.Run("the base of a batch with a successor may change", func(t *testing.T) {
		root, _, p := retargetWorld(t, "retarget-first-base", false)
		if err := retarget(root, p, "b1", ReviewBatchRetarget{PreviousBase: sha("a"), PreviousTarget: sha("b"), Base: sha("e"), Target: sha("b")}); err != nil {
			t.Fatal(err)
		}
		got, err := ReadReviewPlan(root, p.PlanID)
		if err != nil || got.Batches[0].Base != sha("e") || got.Batches[1].Base != sha("b") {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func TestRetargetRefusesABatchThatHasEvidence(t *testing.T) {
	t.Run("run", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "retarget-run")
		gatePlan(t, root, []string{id}, archiveRequirements())
		gateRun(t, root, archiveInput([]string{id}, "first", "PMQA"), emptyFindings())
		p, err := ReadReviewPlan(root, "plan-"+id)
		if err != nil {
			t.Fatal(err)
		}
		if err = retarget(root, p, "batch", ReviewBatchRetarget{PreviousBase: sha("a"), PreviousTarget: sha("b"), Base: sha("a"), Target: sha("c")}); err == nil {
			t.Fatal("a batch with a run was retargeted")
		}
		after, _ := ReadReviewPlan(root, p.PlanID)
		if after.Revision != p.Revision {
			t.Fatal("plan changed")
		}
	})
	t.Run("advance", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "retarget-advance")
		gatePlan(t, root, []string{id}, noReviewRequirements())
		if err := AdvanceReviewBatch(root, ReviewBatchAdvance{BatchID: "batch", ExpectedRevision: 1, Advance: ReviewAdvance{PreviousTarget: sha("b"), Target: sha("c"), Reason: "member fix", Deliveries: map[string]string{sha("c"): id}}}); err != nil {
			t.Fatal(err)
		}
		p, err := ReadReviewPlan(root, "plan-"+id)
		if err != nil {
			t.Fatal(err)
		}
		if err = retarget(root, p, "batch", ReviewBatchRetarget{PreviousBase: sha("a"), PreviousTarget: sha("c"), Base: sha("a"), Target: sha("d")}); err == nil {
			t.Fatal("an advanced batch was retargeted")
		}
	})
	t.Run("closure", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "retarget-closed")
		gatePlan(t, root, []string{id}, noReviewRequirements())
		if _, err := gateClose(t, root, map[string]ReviewRoleConclusion{}); err != nil {
			t.Fatal(err)
		}
		p, err := ReadReviewPlan(root, "plan-"+id)
		if err != nil {
			t.Fatal(err)
		}
		if err = retarget(root, p, "batch", ReviewBatchRetarget{PreviousBase: sha("a"), PreviousTarget: sha("b"), Base: sha("a"), Target: sha("d")}); err == nil {
			t.Fatal("a closed batch was retargeted")
		}
		if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
			t.Fatalf("%v", problems)
		}
	})
}

func TestRetargetAfterAggregateInvalidatesTheOldView(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "retarget-aggregate")
	gatePlan(t, root, []string{id}, noReviewRequirements())
	old, err := PublishReviewDisposition(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ReadReviewPlan(root, "plan-"+id)
	if err != nil {
		t.Fatal(err)
	}
	if err = retarget(root, p, "batch", ReviewBatchRetarget{PreviousBase: sha("a"), PreviousTarget: sha("b"), Base: sha("a"), Target: sha("d")}); err != nil {
		t.Fatal(err)
	}
	view, err := ReadReviewBatchView(root, "batch")
	if err != nil || view.Batch.TargetCommit != sha("d") || view.Batch.Revision != 2 {
		t.Fatalf("%+v %v", view.Batch, err)
	}
	// The published copies carry the new target, so no copy keeps the old one.
	err = WithTransaction(root, reviewScope([]string{id}, true), func(tx *Transaction) error {
		raw, ok, e := tx.ReadGroup(reviewControlGroup, "dispositions/batch.json")
		if e != nil || !ok {
			t.Fatalf("aggregate view missing: %v", e)
		}
		text, e := tx.Read(id, "reviews/batches/batch/disposition.json")
		if e != nil {
			return e
		}
		if string(raw) != reviewJSON(view) || text != reviewJSON(view) {
			t.Fatal("published views were not rewritten")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	closeWith := func(v ReviewBatchView, hash string, revision uint64) error {
		r := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: revision, ViewHash: hash, Author: "coordinator", Roles: map[string]ReviewRoleConclusion{}}
		edges, _, e := ReviewClosureEdges(v, r)
		if e != nil {
			return e
		}
		_, e = CloseReviewBatch(root, r, ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: "2026-11-18T00:00:00Z", Edges: edges})
		return e
	}
	if err = closeWith(view, ReviewViewDigest(old), old.Batch.Revision); err == nil {
		t.Fatal("the view hash from before the correction closed the batch")
	}
	if batchClosed(t, root) {
		t.Fatal("closure written")
	}
	fresh, err := PublishReviewDisposition(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	if err = closeWith(fresh, ReviewViewDigest(fresh), fresh.Batch.Revision); err != nil {
		t.Fatal(err)
	}
	if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
		t.Fatalf("%v", problems)
	}
}

func TestRetargetExcludesTheOtherExtendOperations(t *testing.T) {
	root, _, p := retargetWorld(t, "retarget-exclusive", false)
	r := map[string]ReviewBatchRetarget{"b2": {PreviousBase: sha("b"), PreviousTarget: sha("c"), Base: sha("b"), Target: sha("d")}}
	for name, x := range map[string]ReviewPlanExtension{
		"seal":          {Seal: true},
		"drop":          {DropBatches: []string{"b2"}},
		"sync":          {SyncTargets: map[string]string{"b2": sha("c")}},
		"rebind":        {RebindCycles: map[string]string{"x": "y"}},
		"decision only": {Decision: "user-1"},
	} {
		x.PlanID, x.ExpectedRevision, x.RetargetBatches, x.Author, x.Basis = p.PlanID, p.Revision, r, "coordinator", "mixed"
		if err := ExtendReviewPlan(root, x); err == nil {
			t.Fatalf("%s combined with retarget_batches", name)
		}
	}
	if got, _ := ReadReviewPlan(root, p.PlanID); got.Revision != p.Revision {
		t.Fatal("plan changed")
	}
	if err := ExtendReviewPlan(root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision + 1, RetargetBatches: r, Author: "coordinator", Basis: "stale plan"}); err == nil {
		t.Fatalf("stale plan revision accepted: %v", err)
	}
}
