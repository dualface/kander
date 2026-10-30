package board

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	modelPlanID  = "model-plan"
	modelMaxRuns = 2 // a third full run adds no state a second run does not reach
)

var modelFinding = ReviewFinding{ID: "PMQA-01", Tier: "medium", Text: "model finding", Evidence: "model.go:1"}

func modelRequired() map[string]string {
	return map[string]string{"PMQA": "required", "Security": "N/A: repository rule"}
}

func modelFindings(withFinding bool) ReviewFindings {
	f := emptyFindings()
	if withFinding {
		f.Findings = []ReviewFinding{modelFinding}
	}
	return f
}

func modelPlanBatch(id, previous string, ids []string, base, target string, requirements map[string]string) ReviewPlanBatch {
	return ReviewPlanBatch{BatchID: id, TaskIDs: ids, Base: base, TargetCommit: target, Requirements: requirements, PreviousBatchID: previous}
}

func (w *modelWorld) memberIDs() []string {
	ids := make([]string, 0, len(w.members))
	for _, m := range w.members {
		ids = append(ids, m.id)
	}
	slices.Sort(ids)
	return ids
}

func (w *modelWorld) createPlan(id string, ids []string, sealed bool, batches ...ReviewPlanBatch) error {
	p := ReviewPlan{Schema: 1, Sealed: sealed, PlanID: id, Author: "coordinator", Basis: "model", CWD: "/repo", ReportLanguage: "en", TaskIDs: ids, Batches: batches}
	if err := CreateReviewPlan(w.root, p); err != nil {
		return err
	}
	w.plans = append(w.plans, id)
	return nil
}

func (w *modelWorld) publishRun(in ReviewInput, requirements map[string]string, findings ReviewFindings) error {
	run, _, err := PrepareReviewRun(w.root, in, requirements, nil, archiveOriginals(), "model")
	if err != nil {
		return err
	}
	run.LaunchStatus, run.ExecutionStatus = "started", "ok"
	if run, err = FinalizeReviewRun(w.root, run, structuredReport(findings)); err != nil {
		return err
	}
	_, failures, err := PublishReviewRun(w.root, run.RunID)
	if err != nil {
		return err
	}
	for _, failure := range failures {
		err = errors.Join(err, failure)
	}
	return err
}

func (b modelBatchFacts) input(w *modelWorld, role, previous, reviewed string) ReviewInput {
	return ReviewInput{RunID: w.nextID("run"), BatchID: b.id, TaskIDs: b.live, Role: role, Reviewer: "codex", Model: "model", Effort: "high", CWD: "/repo", Base: b.base, Commit: b.target, PreviousRunID: previous, ReviewedCommit: reviewed, ReportLanguage: "en"}
}

func (w *modelWorld) closeBatch(id string, facts modelBatchFacts) error {
	v, err := ReadReviewBatchView(w.root, id)
	if err != nil {
		return err
	}
	roles := map[string]ReviewRoleConclusion{}
	superseded := map[string]ReviewSupersededRun{}
	for _, role := range facts.required {
		head, ok := facts.head(role)
		if !ok {
			continue
		}
		roles[role] = ReviewRoleConclusion{RunID: head.id, PassedAt: head.commit, Basis: "model conclusion"}
		chain := facts.chain(head)
		for _, run := range facts.runs {
			switch {
			case run.role != role || chain[run.id]:
			case run.commit == head.commit && run.previous == head.previous:
				superseded[run.id] = ReviewSupersededRun{Replacement: head.id, Basis: "model parallel duplicate"}
			default:
				superseded[run.id] = ReviewSupersededRun{Replacement: head.id, Basis: "model replaced root", Decision: "model-decision"}
			}
		}
	}
	r := ReviewCloseRequest{BatchID: id, ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: roles, SupersededRuns: superseded}
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		return err
	}
	_, err = CloseReviewBatch(w.root, r, ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
	return err
}

// chain returns the run IDs from head back to its root.
func (b modelBatchFacts) chain(head modelRunFacts) map[string]bool {
	chain := map[string]bool{}
	for id := head.id; id != ""; {
		chain[id] = true
		at := slices.IndexFunc(b.runs, func(r modelRunFacts) bool { return r.id == id })
		if at < 0 {
			break
		}
		id = b.runs[at].previous
	}
	return chain
}

func (w *modelWorld) assign(run modelRunFacts, batch string, task string) error {
	items := map[string][]string{}
	for _, finding := range run.findings {
		items[finding] = []string{task}
	}
	return AssignReviewFindings(w.root, ReviewAssignment{RunID: run.id, BatchID: batch, Author: "coordinator", Basis: "model", Items: items})
}

func (w *modelWorld) dispose(m *modelMember, run modelRunFacts, batch, finding string) error {
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	d := ReviewDisposition{RecordID: w.nextID("record"), RunID: run.id, FindingID: finding, BatchID: batch, TaskID: m.id, Author: MetadataFrom(s.Text, FieldOwner), ReportHash: run.report, Original: modelFinding.Text, Status: "rejected", Basis: "model record"}
	if a := authFrom(s.Text); a.DispatchID != "" {
		d.Authorization = &a
	}
	return SubmitReviewDisposition(w.root, d, s.Revision)
}

// withdraw is the orchestrator's record for a finding of an excluded member.
func (w *modelWorld) withdraw(m *modelMember, run modelRunFacts, batch, finding string) error {
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	var id string
	err = WithTransaction(w.root, reviewScope(nil, true), func(tx *Transaction) error {
		e, _, err := readMemberExclusion(tx, m.id)
		id = e.ExclusionID
		return err
	})
	if err != nil {
		return modelInfra(err)
	}
	d := ReviewDisposition{RecordID: w.nextID("record"), RunID: run.id, FindingID: finding, BatchID: batch, TaskID: m.id, Author: "coordinator", ReportHash: run.report, Original: modelFinding.Text, Status: "withdrawn", Basis: "model withdrawal", Withdrawal: &ReviewWithdrawal{ExclusionID: id, Decision: "model-decision"}}
	return SubmitReviewDisposition(w.root, d, s.Revision)
}

// open reports a batch that can still close: a batch with no member left neither
// closes nor waits.
func (b modelBatchFacts) open() bool { return !b.closed && len(b.live) > 0 }

// quiet reports that no launcher is mid-start. A review needs a delivered
// commit, so no review command races a starting executor.
func (f *modelFacts) quiet() bool {
	return !slices.ContainsFunc(f.world.members, func(m *modelMember) bool { return m.launcher != nil })
}

func (r modelReviewFacts) firstOpen() (modelBatchFacts, bool) {
	for _, b := range r.batches {
		if b.open() {
			return b, true
		}
	}
	return modelBatchFacts{}, false
}

// withdrawable names a finding an excluded member still owes.
func (f *modelFacts) withdrawable(i int) (modelBatchFacts, modelRunFacts, string, bool) {
	id := f.world.members[i].id
	if !f.excluded[i] {
		return modelBatchFacts{}, modelRunFacts{}, "", false
	}
	for _, b := range f.review.batches {
		if b.closed {
			continue
		}
		for _, r := range b.runs {
			for _, item := range r.undisposed() {
				if task, finding, _ := strings.Cut(item, "/"); task == id {
					return b, r, finding, true
				}
			}
		}
	}
	return modelBatchFacts{}, modelRunFacts{}, "", false
}

func (r modelReviewFacts) planned() bool {
	return slices.ContainsFunc(r.batches, func(b modelBatchFacts) bool { return b.plan != "" })
}

func modelReviewOps() []modelOp {
	allStarted := func(f *modelFacts, _ int) bool {
		if f.review.planned() || !f.quiet() {
			return false
		}
		for _, s := range f.cards {
			if s.Entry.State != "working" && s.Entry.State != "review" {
				return false
			}
		}
		return true
	}
	openBatch := func(f *modelFacts, _ int) bool {
		_, ok := f.review.firstOpen()
		return ok && f.quiet()
	}
	runRoom := func(f *modelFacts, i int) bool {
		b, ok := f.review.firstOpen()
		return ok && openBatch(f, i) && len(b.runs) < modelMaxRuns
	}
	run := func(name, cli string, findings bool) modelOp {
		return modelOp{name: name, cli: cli, shared: true, guard: runRoom,
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				b, _ := f.review.firstOpen()
				return w.publishRun(b.input(w, "PMQA", "", ""), nil, modelFindings(findings))
			}}
	}
	return []modelOp{
		{name: "R.planRequired", cli: "kander review plan: one sealed batch, PMQA required", shared: true, guard: allStarted,
			apply: func(w *modelWorld, _ *modelMember, _ *modelFacts) error {
				ids := w.memberIDs()
				return w.createPlan(modelPlanID, ids, true, modelPlanBatch("model-batch-1", "", ids, modelCommit(0), modelCommit(1), modelRequired()))
			}},
		{name: "R.planNA", cli: "kander review plan: one sealed batch, every role N/A", shared: true, guard: allStarted,
			apply: func(w *modelWorld, _ *modelMember, _ *modelFacts) error {
				ids := w.memberIDs()
				return w.createPlan(modelPlanID, ids, true, modelPlanBatch("model-batch-1", "", ids, modelCommit(0), modelCommit(1), noReviewRequirements()))
			}},
		{name: "R.planChained", cli: "kander review plan: two batches named in advance", shared: true, guard: allStarted,
			apply: func(w *modelWorld, _ *modelMember, _ *modelFacts) error {
				ids := w.memberIDs()
				return w.createPlan(modelPlanID, ids, true,
					modelPlanBatch("model-batch-1", "", ids, modelCommit(0), modelCommit(1), noReviewRequirements()),
					modelPlanBatch("model-batch-2", "model-batch-1", ids, modelCommit(1), modelCommit(2), noReviewRequirements()))
			}},
		{name: "R.planOpen", cli: "kander review plan: one unsealed batch", shared: true, guard: allStarted,
			apply: func(w *modelWorld, _ *modelMember, _ *modelFacts) error {
				ids := w.memberIDs()
				return w.createPlan(modelPlanID, ids, false, modelPlanBatch("model-batch-1", "", ids, modelCommit(0), modelCommit(1), modelRequired()))
			}},
		run("R.run", "kander review <cwd> <base> <commit> PMQA: a full run that reports one finding", true),
		run("R.runClean", "kander review <cwd> <base> <commit> PMQA: a full run with no findings", false),
		{name: "R.runIncremental", cli: "kander review --previous-run-id <run>: a clean incremental round", shared: true,
			guard: func(f *modelFacts, _ int) bool {
				b, ok := f.review.firstOpen()
				if !ok {
					return false
				}
				head, ok := b.head("PMQA")
				return ok && head.commit != b.target
			},
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				b, _ := f.review.firstOpen()
				head, _ := b.head("PMQA")
				return w.publishRun(b.input(w, "PMQA", head.id, head.commit), nil, modelFindings(false))
			}},
		{name: "R.assign", cli: "kander review assign <assignment.json>",
			guard: func(f *modelFacts, i int) bool {
				b, ok := f.review.firstOpen()
				return ok && !f.excluded[i] && slices.ContainsFunc(b.runs, func(r modelRunFacts) bool { return r.assigned == nil })
			},
			apply: func(w *modelWorld, m *modelMember, f *modelFacts) error {
				b, _ := f.review.firstOpen()
				for _, r := range b.runs {
					if r.assigned == nil {
						return w.assign(r, b.id, m.id)
					}
				}
				return nil
			}},
		{name: "X.dispose", cli: "kander review disposition <author-record.json> <revision>",
			guard: func(f *modelFacts, i int) bool {
				_, _, ok := f.review.fixTarget(f.world.members[i].id)
				return ok && f.cards[i].Entry.State == "working" && modelLive(f, i)
			},
			apply: func(w *modelWorld, m *modelMember, f *modelFacts) error {
				b, r, _ := f.review.fixTarget(m.id)
				return w.dispose(m, r, b.id, modelFinding.ID)
			}},
		{name: "R.withdraw", cli: "kander review disposition <withdrawn-record.json> <revision>",
			guard: func(f *modelFacts, i int) bool { _, _, _, ok := f.withdrawable(i); return ok },
			apply: func(w *modelWorld, m *modelMember, f *modelFacts) error {
				b, r, finding, _ := f.withdrawable(f.index(m))
				return w.withdraw(m, r, b.id, finding)
			}},
		{name: "R.advance", cli: "kander review advance <advance.json>", shared: true,
			guard: func(f *modelFacts, i int) bool {
				b, ok := f.review.firstOpen()
				return ok && openBatch(f, i) && b.advances == 0
			},
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				b, _ := f.review.firstOpen()
				view, err := ReadReviewBatch(w.root, b.id)
				if err != nil {
					return modelInfra(err)
				}
				next := w.nextCommit()
				return AdvanceReviewBatch(w.root, ReviewBatchAdvance{BatchID: b.id, ExpectedRevision: view.Revision, Advance: ReviewAdvance{PreviousTarget: b.target, Target: next, Reason: "model delivery", Deliveries: map[string]string{next: b.members[0]}}})
			}},
		{name: "R.close", cli: "kander review close <close-request.json>", shared: true, guard: openBatch,
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				b, _ := f.review.firstOpen()
				return w.closeBatch(b.id, b)
			}},
		{name: "R.rebind", cli: "kander review extend-plan: rebind_cycles", shared: true,
			guard: func(f *modelFacts, _ int) bool {
				if !f.review.planned() {
					return false
				}
				for i := range f.cards {
					if f.rebindNeeded(i) {
						return true
					}
				}
				return false
			},
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				for i, m := range w.members {
					if f.rebindNeeded(i) {
						return w.rebind(m)
					}
				}
				return nil
			}},
		{name: "R.dropBatch", cli: "kander review extend-plan: drop_batches", shared: true,
			guard: func(f *modelFacts, _ int) bool {
				count := len(f.review.batches)
				return count > 1 && len(f.review.batches[count-1].runs) == 0 && !f.review.batches[count-1].closed
			},
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				last := f.review.batches[len(f.review.batches)-1]
				p, err := ReadReviewPlan(w.root, last.plan)
				if err != nil {
					return modelInfra(err)
				}
				return ExtendReviewPlan(w.root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, DropBatches: []string{last.id}, Author: "coordinator", Basis: "model"})
			}},
		{name: "R.cycleBatch", cli: "kander review extend-plan: append a batch for a rebound cycle", shared: true,
			guard: func(f *modelFacts, _ int) bool { return len(f.review.cycleOwed) > 0 },
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error { return w.appendCycleBatch(f.review) }},
		{name: "R.extendSeal", cli: "kander review extend-plan: seal", shared: true,
			guard: func(f *modelFacts, _ int) bool {
				return slices.ContainsFunc(f.review.batches, func(b modelBatchFacts) bool { return b.plan != "" && !b.sealed })
			},
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				for _, b := range f.review.batches {
					if b.plan != "" && !b.sealed {
						return w.sealPlan(b.plan)
					}
				}
				return nil
			}},
		{name: "R.extendBatch", cli: "kander review extend-plan: append a batch", shared: true,
			guard: func(f *modelFacts, _ int) bool {
				count := len(f.review.batches)
				if count == 0 {
					return false
				}
				last := f.review.batches[count-1]
				return last.plan != "" && !last.sealed && last.closed && count < 2
			},
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				last := f.review.batches[len(f.review.batches)-1]
				p, err := ReadReviewPlan(w.root, last.plan)
				if err != nil {
					return modelInfra(err)
				}
				batch := modelPlanBatch("model-batch-2", last.id, last.live, last.target, w.nextCommit(), noReviewRequirements())
				return ExtendReviewPlan(w.root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, Batch: &batch, Author: "coordinator", Basis: "model"})
			}},
	}
}

// appendCycleBatch gives the rebound cycles an N/A batch after the last batch.
func (w *modelWorld) appendCycleBatch(review modelReviewFacts) error {
	last := review.batches[len(review.batches)-1]
	p, err := ReadReviewPlan(w.root, last.plan)
	if err != nil {
		return modelInfra(err)
	}
	batch := modelPlanBatch(fmt.Sprintf("model-cycle-%d", len(review.batches)), last.id, review.cycleOwed, last.target, w.nextCommit(), noReviewRequirements())
	return ExtendReviewPlan(w.root, ReviewPlanExtension{PlanID: p.PlanID, ExpectedRevision: p.Revision, Batch: &batch, Author: "coordinator", Basis: "model cycle batch"})
}

func (w *modelWorld) rebind(m *modelMember) error {
	p, err := ReviewTaskProgress(w.root, m.id)
	if err != nil {
		return err
	}
	plan, err := ReadReviewPlan(w.root, p.PlanID)
	if err != nil {
		return modelInfra(err)
	}
	return ExtendReviewPlan(w.root, ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: plan.Revision, RebindCycles: p.RebindCycles, Author: "coordinator", Basis: "model rebind"})
}

func (w *modelWorld) sealPlan(id string) error {
	plan, err := ReadReviewPlan(w.root, id)
	if err != nil {
		return modelInfra(err)
	}
	return ExtendReviewPlan(w.root, ReviewPlanExtension{PlanID: plan.PlanID, ExpectedRevision: plan.Revision, Seal: true, Author: "coordinator", Basis: "model seal"})
}

// settleReview finishes the review evidence one card needs for completion: it
// creates an N/A plan when none exists, rebinds a changed cycle, and closes each
// open batch of the plan. An unassigned finding goes to this card; a finding
// assigned to another card that has no record refuses the close.
func (w *modelWorld) settleReview(m *modelMember) error {
	p, err := ReviewTaskProgress(w.root, m.id)
	if err != nil {
		return err
	}
	if p.PlanID == "" {
		id := "model-na-" + strings.ToLower(m.label)
		if err := w.createPlan(id, []string{m.id}, true, modelPlanBatch(id, "", []string{m.id}, modelCommit(0), modelCommit(1), noReviewRequirements())); err != nil {
			return err
		}
	} else if len(p.RebindCycles) > 0 {
		if err := w.rebind(m); err != nil {
			return err
		}
	}
	for range 4 {
		review, err := w.readReview()
		if err != nil {
			return err
		}
		if len(review.cycleOwed) > 0 {
			if err := w.appendCycleBatch(review); err != nil {
				return err
			}
			continue
		}
		b, ok := review.openFor(m.id)
		if !ok {
			return w.sealOpenPlans(review)
		}
		if err := w.settleBatch(m, b.id); err != nil {
			return err
		}
		review, err = w.readReview()
		if err != nil {
			return err
		}
		if b, ok = review.batch(b.id); !ok {
			return errors.New("batch disappeared")
		}
		if err := w.closeBatch(b.id, b); err != nil {
			return err
		}
	}
	return errors.New("review settlement did not converge")
}

func (w *modelWorld) sealOpenPlans(review modelReviewFacts) error {
	for _, b := range review.batches {
		if b.plan != "" && !b.sealed {
			return w.sealPlan(b.plan)
		}
	}
	return nil
}

// settleBatch brings a batch to a closable state: every run assigned, every
// assigned finding recorded by a card that can still record it, and a run of
// each required role at the batch target.
func (w *modelWorld) settleBatch(m *modelMember, id string) error {
	refresh := func() (modelBatchFacts, error) {
		review, err := w.readReview()
		if err != nil {
			return modelBatchFacts{}, err
		}
		b, ok := review.batch(id)
		if !ok {
			return b, modelInfra(errors.New("batch disappeared"))
		}
		return b, nil
	}
	b, err := refresh()
	if err != nil {
		return err
	}
	for _, role := range b.required {
		head, ok := b.head(role)
		switch {
		case !ok:
			err = w.publishRun(b.input(w, role, "", ""), nil, modelFindings(false))
		case head.commit != b.target:
			err = w.publishRun(b.input(w, role, head.id, head.commit), nil, modelFindings(false))
		}
		if err != nil {
			return err
		}
	}
	if b, err = refresh(); err != nil {
		return err
	}
	for _, r := range b.runs {
		if r.assigned == nil {
			if err := w.assign(r, b.id, m.id); err != nil {
				return err
			}
		}
	}
	if b, err = refresh(); err != nil {
		return err
	}
	for _, r := range b.runs {
		for _, member := range w.members {
			s, err := w.snapshot(member)
			if err != nil {
				return err
			}
			gone := !containsID(b.live, member.id)
			if !gone && (s.Entry.State != "working" || !member.self && !member.executor) {
				continue
			}
			for _, finding := range r.findings {
				if !containsID(r.assigned[finding], member.id) || r.recorded(finding, member.id) {
					continue
				}
				record := w.dispose
				if gone {
					record = w.withdraw
				}
				if err := record(member, r, b.id, finding); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r modelReviewFacts) openFor(task string) (modelBatchFacts, bool) {
	for _, b := range r.batches {
		if b.open() && containsID(b.members, task) {
			return b, true
		}
	}
	return modelBatchFacts{}, false
}

func (r modelReviewFacts) batch(id string) (modelBatchFacts, bool) {
	for _, b := range r.batches {
		if b.id == id {
			return b, true
		}
	}
	return modelBatchFacts{}, false
}
