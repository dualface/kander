package board

import (
	"context"
	"reflect"
	"slices"
	"strings"
)

// Recovery modes. The column of the member selects the mode.
const (
	RecoverReset   = "reset"
	RecoverConfirm = "confirm"
)

const recoverySchema = 1

// CoordinatorRecover asks to recover one blocked member. Every field is required:
// the two expected values bind the request to the checkpoint the decision was
// made on, and the decision reference is the audit basis.
type CoordinatorRecover struct {
	GroupID          string `json:"group_id"`
	Member           string `json:"member"`
	ExpectedRevision uint64 `json:"expected_revision"`
	ExpectedEpoch    uint64 `json:"expected_epoch"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
}

// CoordinatorRecovery is the immutable record of one recovery. It binds the
// checkpoint revision and the coordinator epoch it was decided on, so an old
// record or request cannot apply to a later checkpoint.
type CoordinatorRecovery struct {
	Schema             int    `json:"schema"`
	RecoveryID         string `json:"recovery_id"`
	GroupID            string `json:"group_id"`
	TaskID             string `json:"task_id"`
	Mode               string `json:"mode"`
	CheckpointRevision uint64 `json:"checkpoint_revision"`
	CoordinatorEpoch   uint64 `json:"coordinator_epoch"`
	Decision           string `json:"decision_reference"`
	Reason             string `json:"reason"`
	BlockedReason      string `json:"blocked_reason"`
	FromState          string `json:"from_state"`
	FromCycle          string `json:"from_cycle"`
	ToCycle            string `json:"to_cycle"`
	TransitionSeq      uint64 `json:"transition_seq,omitzero"`
	RecordedAt         string `json:"recorded_at"`
}

// CoordinatorConfirmation lets reconcile accept a finished member as it is, for the
// execution cycle the user looked at.
type CoordinatorConfirmation struct {
	RecoveryID string `json:"recovery_id"`
	Cycle      string `json:"cycle"`
}

func recoveryName(id string) string { return "recoveries/" + id + ".json" }

func memberConfirmed(s Snapshot, old CoordinatorMember) bool {
	finished := s.Entry.State == "done" || s.Entry.State == "archived" || s.Entry.State == "trash"
	return finished && old.Confirmed != nil && old.Confirmed.Cycle == planCycle(s)
}

// verifyCoordinatorRecoveries requires every listed recovery to equal its immutable
// record, and every confirmation of a mirror to name a listed confirm record.
func verifyCoordinatorRecoveries(tx *Transaction, c CoordinatorCheckpoint) error {
	for _, r := range c.Recoveries {
		var stored CoordinatorRecovery
		b, ok, err := tx.ReadGroup(c.GroupID, recoveryName(r.RecoveryID))
		if err != nil {
			return err
		}
		if !ok {
			return coordinatorError("recovery record missing: " + r.RecoveryID)
		}
		if err = DecodeReviewJSON(b, &stored); err != nil {
			return err
		}
		if !reflect.DeepEqual(stored, r) || r.Schema != recoverySchema || r.GroupID != c.GroupID {
			return coordinatorError("recovery record changed: " + r.RecoveryID)
		}
	}
	for id, m := range c.Members {
		if m.Confirmed == nil {
			continue
		}
		confirmed := slices.ContainsFunc(c.Recoveries, func(r CoordinatorRecovery) bool {
			return r.RecoveryID == m.Confirmed.RecoveryID && r.TaskID == id && r.Mode == RecoverConfirm && r.ToCycle == m.Confirmed.Cycle
		})
		if !confirmed {
			return coordinatorError("confirmation has no recovery record: " + id)
		}
	}
	return nil
}

// RecoverCoordinator is the last resort for a blocked member. Working and review
// members are reset: a new execution cycle replaces the old one and its review
// evidence. Finished members are confirmed: the audit record and the mirror change,
// and no card, cycle or review evidence does. The caller reads the card at revision
// observed and probes its executor before this runs, with no lock held.
func RecoverCoordinator(ctx context.Context, root string, r CoordinatorRecover, observed uint64) (c CoordinatorCheckpoint, err error) {
	if !taskGroupRe.MatchString(r.GroupID) || r.ExpectedRevision == 0 || r.ExpectedEpoch == 0 || strings.TrimSpace(r.Decision) == "" || strings.TrimSpace(r.Reason) == "" {
		return c, kanbanError("board.recover_request_invalid")
	}
	id, err := NormalizeTaskID(r.Member)
	if err != nil {
		return c, err
	}
	scope := LockScope{Groups: []string{r.GroupID, reviewControlGroup, dispatchRegistry, taskStartGroup}, Tasks: []string{id}, ExclusiveBoard: true}
	err = WithTransactionContext(ctx, root, scope, func(tx *Transaction) error {
		var exists bool
		var e error
		if c, exists, e = readCheckpoint(tx, r.GroupID); e != nil {
			return e
		}
		if !exists {
			return coordinatorError("checkpoint missing")
		}
		if c.Revision != r.ExpectedRevision || c.Authority.Epoch != r.ExpectedEpoch || c.Revision == ^uint64(0) {
			return coordinatorError("recover CAS")
		}
		old, ok := c.Members[id]
		if !ok {
			return coordinatorError("unknown or missing member")
		}
		if old.Blocked == nil {
			return kanbanError("board.recover_not_blocked", id)
		}
		s, e := tx.Expect(id, "", observed)
		if e != nil {
			return e
		}
		record := CoordinatorRecovery{Schema: recoverySchema, RecoveryID: tx.record.ID, GroupID: r.GroupID, TaskID: id, CheckpointRevision: c.Revision, CoordinatorEpoch: c.Authority.Epoch, Decision: r.Decision, Reason: r.Reason, BlockedReason: old.Blocked.Reason, FromState: s.Entry.State, FromCycle: planCycle(s), RecordedAt: nowStamp()}
		var m CoordinatorMember
		switch s.Entry.State {
		case "working", "review":
			record.Mode = RecoverReset
			m, e = stageRecoverReset(tx, s, r, &record)
		case "done", "archived", "trash":
			record.Mode = RecoverConfirm
			m = confirmedMirror(tx, s, old, &record)
		default:
			return kanbanError("board.recover_state", id, s.Entry.State)
		}
		if e != nil {
			return e
		}
		c.Members[id] = m
		c.Recoveries = append(c.Recoveries, record)
		c.Revision++
		c.Schema, c.LastRequestHash = checkpointSchema, ""
		if e = tx.PutGroup(r.GroupID, recoveryName(record.RecoveryID), reviewJSON(record)); e != nil {
			return e
		}
		return putCheckpoint(tx, &c)
	})
	return c, err
}

// stageRecoverReset gives the member a new execution cycle. Its column returns to
// working, because a delivery of the old cycle is void. OWNER, SESSION and WINDOW
// stay: the takeover that follows replaces them and closes the old container.
func stageRecoverReset(tx *Transaction, s Snapshot, r CoordinatorRecover, record *CoordinatorRecovery) (m CoordinatorMember, err error) {
	id := s.Entry.TaskID
	if authFrom(s.Text) != (ExecutionAuthorization{}) {
		return m, kanbanError("board.recover_dispatch_bound", id, MetadataFrom(s.Text, "DISPATCH_ID"))
	}
	l, err := readExecutionLedger(tx, s)
	if err != nil {
		return m, err
	}
	rounds, err := dispatchRounds(tx, s)
	if err != nil {
		return m, err
	}
	text, err := setMetadata(s.Text, FieldStartedAt, nowStamp())
	if err != nil {
		return m, err
	}
	if text, err = NewClaimMetadata(text); err != nil {
		return m, err
	}
	if err = validateTarget(s.Entry, "working", text); err != nil {
		return m, err
	}
	next := Snapshot{Entry: s.Entry, Text: text}
	owner, session, window := cardExecutionFacts(next)
	revision := s.Revision + 1
	exec := ExecutionRecord{Schema: executionSchema, ID: tx.record.ID, TaskID: id, Cycle: planCycle(next), Kind: ExecutionRecover, Status: ExecutionRunning, Owner: owner, Session: session, Window: window, Revision: revision}
	t := transitionFrom(TransitionRecover, s, l.current).onChain(l)
	t.ToRecord, t.Decision = exec.ID, r.Decision
	if cur := l.current; cur != nil {
		exec.PreviousID, t.FromStatus = cur.ID, cur.Status
		if !cur.ExecutionTerminal() {
			retired := *cur
			retired.Schema, retired.Status, retired.FinishedRevision = executionSchema, ExecutionHandedOff, revision
			if err = putRetiredOriginal(tx, retired, statusOriginal(ExecutionHandedOff)); err != nil {
				return m, err
			}
		}
	}
	if err = tx.Put(id, "spec.md", text); err != nil {
		return m, err
	}
	if s.Entry.State != "working" {
		if err = tx.Relocate(id, "working"); err != nil {
			return m, err
		}
	}
	if err = putRecordOriginal(tx, exec, "result"); err != nil {
		return m, err
	}
	if err = putTransition(tx, &l, t.withCard(s, text, revision)); err != nil {
		return m, err
	}
	if err = stageRecoveryReview(tx, id, exec.ID); err != nil {
		return m, err
	}
	record.ToCycle, record.TransitionSeq = exec.Cycle, l.end()
	return CoordinatorMember{Revision: revision, State: "working", Cycle: exec.Cycle, StartAttempt: l.startAttempt(), TransitionSeq: l.end(), DispatchSeq: uint64(len(rounds))}, nil
}

// confirmedMirror rebinds the mirror of a finished member. A ledger or dispatch
// that cannot be read is a reason a member can be blocked, so it stops nothing
// here: the position stays where it was and the confirmation does not use it.
func confirmedMirror(tx *Transaction, s Snapshot, old CoordinatorMember, record *CoordinatorRecovery) CoordinatorMember {
	m := old
	record.ToCycle = planCycle(s)
	m.State, m.Revision, m.Cycle, m.AwaitingStart, m.Blocked = s.Entry.State, s.Revision, record.ToCycle, false, nil
	m.Confirmed = &CoordinatorConfirmation{RecoveryID: record.RecoveryID, Cycle: record.ToCycle}
	if l, err := readExecutionLedger(tx, s); err == nil {
		m.TransitionSeq, m.StartAttempt = l.end(), l.startAttempt()
		record.TransitionSeq = l.end()
	}
	if rounds, err := dispatchRounds(tx, s); err == nil {
		m.DispatchSeq = uint64(len(rounds))
	}
	return m
}
