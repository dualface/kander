package board

import (
	"fmt"
	"slices"
	"strings"
)

// Cards that own the follow-up work the registries below name.
const (
	cardLedger   = "20261030-execution-ledger-task"
	cardIssue22  = "20261030-gh-dualface-kander-22-task"
	cardEvidence = "20261030-review-evidence-dead-ends-task"
	cardRecovery = "20261030-group-member-recovery-task"
)

// modelInvariant is one row of the invariant table. A row with a check is
// enabled; a row without one waits for the records its owner card adds.
type modelInvariant struct {
	id         string
	definition string
	owner      string
	check      func(before, after *modelFacts) error
}

var modelInvariants = []modelInvariant{
	{id: "I1", owner: cardLedger, definition: "A card in working or review has exactly one current execution record, with status pending, running or unresolved."},
	{id: "I2", owner: cardLedger, definition: "Each execution cycle change has one execution transition record that links the old cycle to the new cycle."},
	{id: "I3", owner: cardLedger, definition: "Each OWNER, SESSION or WINDOW change has one execution transition record."},
	{id: "I4", owner: cardLedger, definition: "When the execution authorization is set, the dispatch it names exists, that dispatch's execution cycle equals the current cycle, and the card has no other nonterminal dispatch."},
	{id: "I5", owner: cardLedger, definition: "When a card enters done, each completed fix or sync dispatch of the card has a later dispatch that replaced it, or a settlement record; a wrap-up dispatch completes on move done and is exempt. When a card enters archived or trash, the same transaction writes the settlement original of a completed binding."},
	{id: "I6", owner: cardEvidence, definition: "Each review run of a plan member belongs to a batch of that plan. Successful runs of one role form one chain, or carry an explicit replacement record."},
	{id: "I7", definition: "Before a review batch advances, each assigned finding has an author disposition.", check: modelCheckI7},
	{id: "I8", owner: cardEvidence, definition: "A member that ends in archived or trash gets a member exclusion record in the same transaction, and later plan writes skip its copy. An excluded member's assigned findings may wait for withdrawal, but they need a withdrawn disposition before aggregation and closure. A done member is not excluded, and archiving it later does not exclude it, but plan writes skip the copies of every archived member."},
	{id: "I9", owner: cardLedger, definition: "Each checkpoint mirror has a chain of execution transition and dispatch records to the member's current state."},
}

func modelCheckI7(before, after *modelFacts) error {
	if before == nil {
		return nil
	}
	for _, b := range after.review.batches {
		old, ok := before.review.batch(b.id)
		if !ok || old.target == b.target {
			continue
		}
		for _, run := range old.runs {
			if open := run.undisposed(); len(open) > 0 {
				return fmt.Errorf("batch %s advanced from %s with undisposed findings %v", b.id, old.target, open)
			}
		}
	}
	return nil
}

// modelCheckInvariants runs every enabled invariant over one transition.
func modelCheckInvariants(before, after *modelFacts, op string) []string {
	var violations []string
	for _, inv := range modelInvariants {
		if inv.check == nil {
			continue
		}
		if err := inv.check(before, after); err != nil {
			violations = append(violations, fmt.Sprintf("%s after %s: %v", inv.id, op, err))
		}
	}
	return violations
}

// modelEvidenceClass says how the test knows a listed dead end still exists.
type modelEvidenceClass string

const (
	modelReached  modelEvidenceClass = "model"    // the search reaches it
	modelDirected modelEvidenceClass = "directed" // a directed test reproduces it
	modelOutside  modelEvidenceClass = "outside"  // neither reproduces it
)

type modelDeadEnd struct {
	id     string
	owner  string
	class  modelEvidenceClass
	proof  string // the directed test, or the reason the dead end stays outside
	covers func(modelDiagnosis) bool
}

func (d modelDiagnosis) strandedBy(causes ...string) bool {
	for _, m := range d.facts.world.members {
		if slices.Contains(causes, m.stranded) {
			return true
		}
	}
	return false
}

// pendingAttemptChanged reports a stranded attempt whose card moved on: a
// takeover, a reclaim or a dispatch changed facts the pending record binds.
func (d modelDiagnosis) pendingAttemptChanged() bool {
	f := d.facts
	for i, m := range f.world.members {
		s := f.cards[i]
		if f.attempts[i].Status != "pending" || m.stranded == "" && !modelTerminal(s.Entry.State) {
			continue
		}
		if MetadataFrom(s.Text, FieldOwner) != f.attempts[i].Owner || MetadataFrom(s.Text, FieldSession) != f.attempts[i].Session || f.bound[i] != nil {
			return true
		}
	}
	return false
}

// racedStart reports a takeover or reclaim that overtook a running launcher, or
// that made its confirmation fail.
func (d modelDiagnosis) racedStart() bool {
	f := d.facts
	for i, m := range f.world.members {
		if f.attempts[i].Status != "pending" {
			continue
		}
		owner := MetadataFrom(f.cards[i].Text, FieldOwner)
		if m.stranded == "confirm-binding" || m.launcher != nil && (owner != f.attempts[i].Owner || MetadataFrom(f.cards[i].Text, FieldSession) != f.attempts[i].Session) {
			return true
		}
	}
	return false
}

func (d modelDiagnosis) terminalMemberOwesFinding() bool {
	f := d.facts
	for i, m := range f.world.members {
		if state := f.cards[i].Entry.State; state != "trash" && state != "archived" {
			continue
		}
		for _, b := range f.review.batches {
			if b.closed {
				continue
			}
			for _, run := range b.runs {
				if slices.ContainsFunc(run.undisposed(), func(item string) bool { return strings.HasPrefix(item, m.id+"/") }) {
					return true
				}
			}
		}
	}
	return false
}

// membershipDriftedAndEnded reports a member set that changed with a member that
// no command can move back into the group.
func (d modelDiagnosis) membershipDriftedAndEnded() bool {
	drifted, ended := false, false
	for _, s := range d.facts.cards {
		drifted = drifted || modelGroupClass(TaskGroupFrom(s.Text)) != "same"
		ended = ended || modelTerminal(s.Entry.State)
	}
	return drifted && ended
}

func (d modelDiagnosis) planMemberEnded() bool {
	f := d.facts
	for i := range f.world.members {
		state := f.cards[i].Entry.State
		if state != "trash" && state != "archived" {
			continue
		}
		for _, b := range f.review.batches {
			if containsID(b.members, f.world.members[i].id) && (!b.closed || d.has("plan member is terminal") || d.has("archived cards are final")) {
				return true
			}
		}
	}
	return false
}

// modelDeadEnds is the allow list. Order matters: a state can show two causes,
// and the first row that covers it names it. Each repair card removes its rows
// and adds its transition to the operations.
var modelDeadEnds = []modelDeadEnd{
	{id: "DE-08", owner: cardEvidence, class: modelReached, covers: func(d modelDiagnosis) bool { return d.facts.world.stray }},
	{id: "DE-14", owner: cardLedger, class: modelReached, covers: modelDiagnosis.membershipDriftedAndEnded},
	{id: "DE-09", owner: cardLedger, class: modelReached, covers: func(d modelDiagnosis) bool {
		return d.has("member execution cycle changed without first-start facts")
	}},
	{id: "DE-20", owner: cardLedger, class: modelReached, covers: func(d modelDiagnosis) bool {
		return d.has("unconfirmed start cannot establish delivery")
	}},
	{id: "DE-19", owner: cardLedger, class: modelReached, covers: func(d modelDiagnosis) bool { return d.has("wrap-up completion state") }},
	{id: "DE-04", owner: cardIssue22, class: modelReached, covers: modelDiagnosis.racedStart},
	{id: "DE-15", owner: cardIssue22, class: modelReached, covers: func(d modelDiagnosis) bool { return d.strandedBy("confirm-dispatch") }},
	{id: "DE-03", owner: cardIssue22, class: modelReached, covers: modelDiagnosis.pendingAttemptChanged},
	{id: "DE-01", owner: cardIssue22, class: modelReached, covers: func(d modelDiagnosis) bool { return d.strandedBy("rollback") }},
	{id: "DE-02", owner: cardLedger, class: modelReached, covers: func(d modelDiagnosis) bool { return d.strandedBy("confirm-session") }},
	{id: "DE-07", owner: cardLedger, class: modelReached, covers: func(d modelDiagnosis) bool {
		return d.has("unbound member lacks dispatch termination and release originals")
	}},
	{id: "DE-13", owner: cardEvidence, class: modelReached, covers: modelDiagnosis.terminalMemberOwesFinding},
	{id: "DE-12", owner: cardEvidence, class: modelReached, covers: modelDiagnosis.planMemberEnded},
	{id: "DE-10", owner: cardEvidence, class: modelReached, covers: func(d modelDiagnosis) bool { return d.has("unconnected successful role run") }},
	{id: "DE-11", owner: cardEvidence, class: modelReached, covers: func(d modelDiagnosis) bool {
		return d.has("previous closed batch required") || d.has("closed batch chain mismatch")
	}},
	{id: "DE-16", owner: cardLedger, class: modelDirected, proof: "internal/launch:TestDeadEndPaneWindowWriteLosesToExecutor"},
	{id: "DE-17", owner: cardLedger, class: modelDirected, proof: "internal/launch:TestDeadEndTakeoverDiscoveryUsesStaleCursor"},
	{id: "DE-18", owner: cardRecovery, class: modelDirected, proof: "internal/launch:TestCoordinatorRechecksActualGitAfterWrapUpRestart"},
}

// modelClassify names the dead end that covers a state, or "" for an unlisted one.
func modelClassify(list []modelDeadEnd, d modelDiagnosis) string {
	for _, entry := range list {
		if entry.covers != nil && entry.covers(d) {
			return entry.id
		}
	}
	return ""
}
