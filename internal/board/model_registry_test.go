package board

import (
	"errors"
	"fmt"
	"slices"
)

// modelInvariant is one row of the invariant table. Every row is enabled.
type modelInvariant struct {
	id         string
	definition string
	check      func(before, after *modelFacts) error
}

var modelInvariants = []modelInvariant{
	{id: "I1", check: modelCheckI1, definition: "A card in working or review has exactly one current execution record, with status pending, running or unresolved."},
	{id: "I2", check: modelCheckI2, definition: "Each execution cycle change has one execution transition record that links the old cycle to the new cycle."},
	{id: "I3", check: modelCheckI3, definition: "Each OWNER, SESSION or WINDOW change has one execution transition record."},
	{id: "I4", check: modelCheckI4, definition: "When the execution authorization is set, the dispatch it names exists, that dispatch's execution cycle equals the current cycle, and the card has no other nonterminal dispatch."},
	{id: "I5", check: modelCheckI5, definition: "When a card enters done, each completed fix or sync dispatch of the card has a later dispatch that replaced it, or a settlement record; a wrap-up dispatch completes on move done and is exempt. When a card enters archived or trash, the same transaction writes the settlement original of a completed binding."},
	{id: "I6", check: modelCheckI6, definition: "Each review run of a plan member belongs to a batch of that plan. Successful runs of one role form one chain, or carry an explicit replacement record."},
	{id: "I7", definition: "Before a review batch advances, each assigned finding has an author disposition.", check: modelCheckI7},
	{id: "I8", check: modelCheckI8, definition: "A member that ends in archived or trash gets a member exclusion record in the same transaction, and later plan writes skip its copy. An excluded member's assigned findings may wait for withdrawal, but they need a withdrawn disposition before aggregation and closure. A done member is not excluded, and archiving it later does not exclude it, but plan writes skip the copies of every archived member."},
	{id: "I9", check: modelCheckI9, definition: "Each checkpoint mirror has a chain of execution transition and dispatch records to the member's current state. A recovery that moved a mirror has an immutable record: a reset names the recover transition on the chain, and a confirmation names the cycle of the card."},
}

// modelCheckEach runs check for every member, and hands it the same member of the
// state before, or nil for the first state of a search.
func modelCheckEach(before, after *modelFacts, check func(i int, old *modelMemberFacts, now modelMemberFacts) error) error {
	for i := range after.world.members {
		var old *modelMemberFacts
		if before != nil {
			member := before.member(i)
			old = &member
		}
		if err := check(i, old, after.member(i)); err != nil {
			return fmt.Errorf("%s: %w", after.world.members[i].label, err)
		}
	}
	return nil
}

func modelCheckI1(before, after *modelFacts) error {
	return modelCheckEach(before, after, func(_ int, _ *modelMemberFacts, m modelMemberFacts) error {
		if state := m.card.Entry.State; state != "working" && state != "review" {
			return nil
		}
		cur := m.ledger.current
		if cur == nil || cur.ExecutionTerminal() {
			return fmt.Errorf("card in %s has no current execution record: %+v", m.card.Entry.State, cur)
		}
		return nil
	})
}

func modelCheckI2(before, after *modelFacts) error {
	return modelCheckEach(before, after, func(_ int, old *modelMemberFacts, m modelMemberFacts) error {
		if old == nil || !m.chained() || planCycle(old.card) == planCycle(m.card) {
			return nil
		}
		if m.ledger.end() <= old.ledger.end() {
			return errors.New("the cycle changed with no transition")
		}
		if tail, _ := m.ledger.tail(); tail.ToCycle != planCycle(m.card) {
			return errors.New("the last transition does not reach the cycle of the card")
		}
		return nil
	})
}

func modelCheckI3(before, after *modelFacts) error {
	return modelCheckEach(before, after, func(_ int, old *modelMemberFacts, m modelMemberFacts) error {
		if old == nil || !m.chained() {
			return nil
		}
		was := [3]string{}
		was[0], was[1], was[2] = cardExecutionFacts(old.card)
		now := [3]string{}
		now[0], now[1], now[2] = cardExecutionFacts(m.card)
		if was != now && m.ledger.end() <= old.ledger.end() {
			return fmt.Errorf("execution fields changed from %v to %v with no transition", was, now)
		}
		// A card an old binary changed stays off its chain until a recovery.
		if old.ledger.assertCard(old.card) != nil {
			return nil
		}
		return m.ledger.assertCard(m.card)
	})
}

func modelCheckI4(before, after *modelFacts) error {
	return modelCheckEach(before, after, func(_ int, _ *modelMemberFacts, m modelMemberFacts) error {
		a := authFrom(m.card.Text)
		if a.DispatchID == "" || modelTerminal(m.card.Entry.State) {
			return nil
		}
		at := slices.IndexFunc(m.rounds, func(d Dispatch) bool { return d.Input.ID == a.DispatchID })
		if at < 0 {
			return fmt.Errorf("authorization names a missing dispatch %s", a.DispatchID)
		}
		if m.rounds[at].Cycle != planCycle(m.card) {
			return fmt.Errorf("dispatch %s belongs to another cycle", a.DispatchID)
		}
		for i, d := range m.rounds {
			if i != at && !coordinatorDispatchTerminal(d.State) {
				return fmt.Errorf("dispatch %s is live beside the bound %s", d.Input.ID, a.DispatchID)
			}
		}
		return nil
	})
}

func modelCheckI5(before, after *modelFacts) error {
	return modelCheckEach(before, after, func(_ int, old *modelMemberFacts, m modelMemberFacts) error {
		if old == nil || old.card.Entry.State == m.card.Entry.State {
			return nil
		}
		switch m.card.Entry.State {
		case "done":
			for i, d := range m.rounds {
				if d.State == DispatchCompleted && d.Input.Kind != "wrap-up" && d.Settlement == nil && i == len(m.rounds)-1 {
					return fmt.Errorf("completed dispatch %s has no successor and no settlement", d.Input.ID)
				}
			}
		case "archived", "trash":
			if !m.chained() {
				return nil
			}
			if a := authFrom(old.card.Text); a.DispatchID != "" {
				at := slices.IndexFunc(m.rounds, func(d Dispatch) bool { return d.Input.ID == a.DispatchID })
				if at >= 0 && m.rounds[at].State == DispatchCompleted && m.rounds[at].Settlement == nil {
					return fmt.Errorf("completed binding %s has no settlement original", a.DispatchID)
				}
			}
		}
		return nil
	})
}

func modelCheckI9(_, after *modelFacts) error {
	if after.ckpt == nil {
		return nil
	}
	return modelCheckEach(nil, after, func(i int, _ *modelMemberFacts, m modelMemberFacts) error {
		id := after.world.members[i].id
		c := after.ckpt.Members[id]
		if !m.chained() {
			return nil
		}
		for _, r := range after.ckpt.Recoveries {
			if r.TaskID != id {
				continue
			}
			switch r.Mode {
			case RecoverReset:
				if r.TransitionSeq == 0 || r.TransitionSeq > m.ledger.end() || m.ledger.chain[r.TransitionSeq-1].Kind != TransitionRecover || m.ledger.chain[r.TransitionSeq-1].Decision != r.Decision {
					return fmt.Errorf("reset %s names no recover transition", r.RecoveryID)
				}
			case RecoverConfirm:
				if c.Confirmed != nil && c.Confirmed.RecoveryID == r.RecoveryID && c.Confirmed.Cycle != planCycle(m.card) {
					return fmt.Errorf("confirmation %s names another cycle", r.RecoveryID)
				}
			default:
				return fmt.Errorf("recovery %s has mode %q", r.RecoveryID, r.Mode)
			}
		}
		// A blocked mirror stays where the last good reconcile left it. A confirmed
		// one names the card, and the checks above bound it to its record.
		if c.Blocked != nil || c.Confirmed != nil {
			return nil
		}
		if c.TransitionSeq > m.ledger.end() || c.DispatchSeq > uint64(len(m.rounds)) {
			return fmt.Errorf("mirror at %d/%d is past the chain %d/%d", c.TransitionSeq, c.DispatchSeq, m.ledger.end(), len(m.rounds))
		}
		if c.TransitionSeq > 0 && !c.AwaitingStart && m.ledger.chain[c.TransitionSeq-1].ToCycle != c.Cycle {
			return fmt.Errorf("mirror cycle is not on the chain at %d", c.TransitionSeq)
		}
		if d := c.Dispatch; d != nil && !slices.ContainsFunc(m.rounds, func(r Dispatch) bool { return r.Input.ID == d.ID }) {
			return fmt.Errorf("mirror names a dispatch outside the chain: %s", d.ID)
		}
		return nil
	})
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

func modelCheckI6(_, after *modelFacts) error {
	// An old binary left the stray run; only a resolution clears it.
	if len(after.review.foreign) > 0 && after.world.stray == "" {
		return fmt.Errorf("runs outside the plan: %v", after.review.foreign)
	}
	for _, b := range after.review.batches {
		if !b.closed {
			continue
		}
		for _, role := range b.required {
			head, ok := b.head(role)
			if !ok {
				return fmt.Errorf("closed batch %s has no %s run", b.id, role)
			}
			chain := b.chain(head)
			for _, r := range b.runs {
				if _, retired := b.superseded[r.id]; r.role == role && !chain[r.id] && !retired {
					return fmt.Errorf("run %s of closed batch %s is outside the chain with no record", r.id, b.id)
				}
			}
		}
	}
	return nil
}

func modelCheckI8(before, after *modelFacts) error {
	if before == nil {
		return nil
	}
	for i, m := range after.world.members {
		was, now := before.cards[i].Entry.State, after.cards[i].Entry.State
		if was != now && (now == "archived" || now == "trash") {
			member := was != "todo" && before.review.planned() || after.world.group
			wants := member && !(was == "done" && now == "archived")
			if !after.member(i).chained() {
				continue
			}
			if after.excluded[i] != wants {
				return fmt.Errorf("%s: move %s to %s: exclusion record %v, want %v", m.label, was, now, after.excluded[i], wants)
			}
		}
		// A copy can only disappear, with the archive cleanup.
		if (before.excluded[i] || was == "archived") && after.copies[i] != "" && after.copies[i] != before.copies[i] {
			return fmt.Errorf("%s: a plan write reached a member that left", m.label)
		}
	}
	for _, b := range after.review.batches {
		old, ok := before.review.batch(b.id)
		if !b.closed || !ok || old.closed {
			continue
		}
		for _, r := range b.runs {
			if open := r.undisposed(); len(open) > 0 {
				return fmt.Errorf("batch %s closed with undisposed findings %v", b.id, open)
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

// modelDeadEnds is the allow list. Order matters: a state can show two causes,
// and the first row that covers it names it. Each repair card removes its rows
// and adds its transition to the operations.
var modelDeadEnds = []modelDeadEnd{}

// modelClassify names the dead end that covers a state, or "" for an unlisted one.
func modelClassify(list []modelDeadEnd, d modelDiagnosis) string {
	for _, entry := range list {
		if entry.covers != nil && entry.covers(d) {
			return entry.id
		}
	}
	return ""
}
