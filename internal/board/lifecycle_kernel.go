package board

import (
	"reflect"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/statemachine/cardlifecycle"
)

// The column, contract freezing, and member exclusion decisions of a card are
// made by the generated kernel in internal/statemachine/cardlifecycle
// (formal/KanderFormal/Machines/CardLifecycle.lean). This file projects a card
// into a kernel state and runs kernel events. See docs/formal-verification.md.

// kernelColumns maps the kernel column enum to the board states.
var kernelColumns = [cardlifecycle.ColumnCount]string{
	cardlifecycle.ColumnBacklog: "backlog", cardlifecycle.ColumnTodo: "todo", cardlifecycle.ColumnWorking: "working",
	cardlifecycle.ColumnReview: "review", cardlifecycle.ColumnDone: "done", cardlifecycle.ColumnArchived: "archived",
	cardlifecycle.ColumnTrash: "trash",
}

func kernelColumn(state string) (cardlifecycle.Column, bool) {
	at := slices.Index(kernelColumns[:], state)
	return cardlifecycle.Column(at), at >= 0
}

// lifecycleParams bounds the decision records of one operation: one above the
// count, so an update can add a record.
func lifecycleParams(s cardlifecycle.State) cardlifecycle.Params {
	return cardlifecycle.Params{MaxDecision: uint32(s.Decisions + 1)}
}

// lifecycleStep runs one kernel event on s.
func lifecycleStep(s cardlifecycle.State, e cardlifecycle.Event) (cardlifecycle.State, bool) {
	return cardlifecycle.Step(s, e, lifecycleParams(s))
}

// columnState is the kernel state of a card in state that only the column
// decides: a move between columns reads nothing else.
func columnState(state string) (cardlifecycle.State, bool) {
	c, ok := kernelColumn(state)
	return cardlifecycle.State{Column: c, Frozen: c != cardlifecycle.ColumnBacklog}, ok
}

// allowedMove reports whether the transition table holds the move from one
// column to another.
func allowedMove(from, to string) bool {
	s, ok := columnState(from)
	target, known := kernelColumn(to)
	if !ok || !known {
		return false
	}
	_, accepted := lifecycleStep(s, cardlifecycle.Move{To: target})
	return accepted
}

// allowedDispatchMove reports whether a move with a dispatch authorization the
// dispatch staging accepted may stay in its column.
func allowedDispatchMove(from string, authorized bool) bool {
	s, ok := columnState(from)
	if !ok {
		return false
	}
	_, accepted := lifecycleStep(s, cardlifecycle.DispatchMove{Authorized: authorized})
	return accepted
}

// contractDecisionCount is the number of CONTRACT_DECISIONS records of a card.
func contractDecisionCount(text string) uint64 {
	body, _ := SectionBody(text, "CONTRACT_DECISIONS")
	return uint64(strings.Count(body, "```json"))
}

// projectCard is the kernel state of a committed card: its column, whether its
// contract is frozen (any column but backlog, or a card that left backlog
// once), its decision records, and whether a member exclusion record exists.
func projectCard(tx *Transaction, s Snapshot) (cardlifecycle.State, error) {
	c, ok := kernelColumn(s.Entry.State)
	if !ok {
		return cardlifecycle.State{}, kanbanError("board.transaction_invalid", s.Entry.State)
	}
	version, err := readVersion(tx.root, s.Entry.TaskID)
	if err != nil {
		return cardlifecycle.State{}, err
	}
	excluded, err := memberExcluded(tx, s.Entry.TaskID)
	if err != nil {
		return cardlifecycle.State{}, err
	}
	return cardlifecycle.State{
		Column: c, Frozen: c != cardlifecycle.ColumnBacklog || version.ContractFrozen,
		Decisions: contractDecisionCount(s.Text), Excluded: excluded,
	}, nil
}

// contractChange classifies what an update changes in the frozen contract
// fields: nothing, only top-level acceptance ticks, or anything else.
func contractChange(before, after map[string]string) cardlifecycle.Change {
	if reflect.DeepEqual(before, after) {
		return cardlifecycle.ChangeNone
	}
	untick := func(fields map[string]string) map[string]string {
		out := make(map[string]string, len(fields))
		for name, value := range fields {
			if name == SectionAcceptanceCriteria {
				value = acceptanceTickRe.ReplaceAllString(value, "- [ ] ")
			}
			out[name] = value
		}
		return out
	}
	if reflect.DeepEqual(untick(before), untick(after)) {
		return cardlifecycle.ChangeTicks
	}
	return cardlifecycle.ChangeOther
}

// updateState is the kernel state validSpecUpdate decides on: state is the
// column, or "frozen" for a backlog card whose contract froze earlier.
func updateState(state, text string) (cardlifecycle.State, bool) {
	if state == "frozen" {
		return cardlifecycle.State{Column: cardlifecycle.ColumnBacklog, Frozen: true, Decisions: contractDecisionCount(text)}, true
	}
	s, ok := columnState(state)
	s.Decisions = contractDecisionCount(text)
	return s, ok
}
