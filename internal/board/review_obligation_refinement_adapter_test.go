package board

import (
	"fmt"
	"slices"
	"testing"
)

// obligationFixture drives real obligation functions. Plan admission, cycle
// changes and valid closures are supplied inputs, not verified producers.
type obligationFixture struct {
	root, id string
	plan     ReviewPlan
	cycle    int
	serial   int
}

func newObligationFixture(t *testing.T) *obligationFixture {
	t.Helper()
	root := tempBoard(t)
	return &obligationFixture{root: root, id: gateCard(t, root, "correspondence"), cycle: 1}
}

func (f *obligationFixture) scope() LockScope { return reviewScope([]string{f.id}, false) }

func (f *obligationFixture) savePlan(tx *Transaction) error {
	return tx.PutGroup(reviewControlGroup, planName(f.plan.PlanID), reviewJSON(f.plan))
}

func (f *obligationFixture) step(event string) error {
	return WithTransaction(f.root, f.scope(), func(tx *Transaction) error {
		switch event {
		case "planNA", "planRequired":
			f.plan = ReviewPlan{Schema: 1, PlanID: "correspondence-plan", TaskIDs: []string{f.id}}
			if err := tx.PutGroup(reviewControlGroup, taskPlanName(f.id), reviewJSON(map[string]string{"plan_id": f.plan.PlanID})); err != nil {
				return err
			}
			return f.appendBatch(tx, event == "planRequired")
		case "appendNA", "appendRequired":
			return f.appendBatch(tx, event == "appendRequired")
		case "reset":
			if err := stageRecoveryReview(tx, f.id, fmt.Sprintf("reset-%d", f.cycle)); err != nil {
				return err
			}
			f.cycle++
			s, err := tx.Snapshot(f.id)
			if err != nil {
				return err
			}
			// Only planCycle's input is synthesized; no execution/reset API is
			// claimed to have been exercised by this function-level fixture.
			body, err := setMetadata(s.Text, FieldStartedAt, fmt.Sprintf("2026-01-01 00:%02d", f.cycle))
			if err != nil {
				return err
			}
			return tx.Put(f.id, "spec.md", body)
		case "rebind", "done":
			// These events do not mutate either selected obligation family.
			return nil
		case "close":
			return f.closeBatch(tx)
		case "drop", "dropReleased":
			at := len(f.plan.Batches) - 1
			dropped := slices.Clone(f.plan.Batches[at:])
			f.plan.Batches = f.plan.Batches[:at]
			decision := ""
			if event == "dropReleased" {
				decision = "fixture-user-decision"
			}
			recordDroppedRoles(&f.plan, dropped)
			if err := keepDropObligations(tx, &f.plan, dropped, at, decision); err != nil {
				return err
			}
			if err := moveRecoveryReviews(tx, f.plan, planRequiredRoles(ReviewPlan{Batches: dropped})); err != nil {
				return err
			}
			return f.savePlan(tx)
		case "releaseDrop", "releaseRecovery":
			kind := releaseDropped
			if event == "releaseRecovery" {
				kind = releaseRecovery
			}
			if err := releaseObligation(tx, &f.plan, ReviewObligationRelease{Kind: kind, TaskIDs: []string{f.id}}, "fixture-user-decision"); err != nil {
				return err
			}
			return f.savePlan(tx)
		default:
			return fmt.Errorf("unmapped event %q", event)
		}
	})
}

func (f *obligationFixture) appendBatch(tx *Transaction, required bool) error {
	f.serial++
	requirements := noReviewRequirements()
	if required {
		requirements["PMQA"] = "required"
	}
	f.plan.Batches = append(f.plan.Batches, ReviewPlanBatch{
		BatchID: fmt.Sprintf("batch-%d", f.serial), TaskIDs: []string{f.id}, Requirements: requirements,
	})
	return f.savePlan(tx)
}

func (f *obligationFixture) closeBatch(tx *Transaction) error {
	b := f.plan.Batches[len(f.plan.Batches)-1]
	s, err := tx.Snapshot(f.id)
	if err != nil {
		return err
	}
	// The selected readers consume only closure existence and conclusion-cycle
	// bindings. Git, findings, author dispositions and publication are assumed.
	runID := "run-" + b.BatchID
	c := ReviewClosure{
		Request: ReviewCloseRequest{Roles: map[string]ReviewRoleConclusion{"PMQA": {RunID: runID}}},
		View:    ReviewBatchView{Runs: []ReviewRunView{{Run: ReviewRun{ReviewInput: ReviewInput{RunID: runID}, MemberCycles: map[string]string{f.id: planCycle(s)}}}}},
	}
	return tx.PutGroup(reviewControlGroup, closureName(b.BatchID), reviewJSON(c))
}

type obligationObservation struct {
	Drop, Recovery, RecoveryRole bool
}

func (f *obligationFixture) observe() (out obligationObservation, err error) {
	err = WithTransaction(f.root, reviewScope([]string{f.id}, true), func(tx *Transaction) error {
		drop, e := dropObligationsOwed(tx, f.plan, true)
		if e != nil {
			return e
		}
		roles, recovery, e := recoveryReviewOwed(tx, f.plan, f.id)
		if e != nil {
			return e
		}
		if len(drop) > 1 || len(drop) == 1 && drop[0] != f.id+"/PMQA" || len(roles) > 1 || len(roles) == 1 && roles[0] != "PMQA" {
			return fmt.Errorf("unexpected obligation roles: drop=%v recovery=%v", drop, roles)
		}
		out = obligationObservation{Drop: len(drop) > 0, Recovery: recovery, RecoveryRole: len(roles) > 0}
		return nil
	})
	return
}
