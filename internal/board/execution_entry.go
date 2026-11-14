package board

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/statemachine/executionledger"
)

// ErrExecutionSuperseded reports that a takeover, an adoption or a reclaim ended
// the record a launcher holds. It is not a failure: the launcher steps aside and
// leaves the container and the card alone.
var ErrExecutionSuperseded = errors.New("execution superseded")

// ExecutionFence names the record a writer acts for. It lives in the operation's
// Version cursor. A writer fenced to an earlier record or cycle is refused.
type ExecutionFence struct {
	RecordID string
	Cycle    string
	// launcher marks the operation that created the record; only it may roll
	// the record back.
	launcher bool
}

// ExecutionResult reports what a transition entrance saw. Advanced is true when
// the card had moved past the caller's cursor before this write: the revision or
// the column differed. The caller's own earlier writes keep the cursor current,
// so they never count. The entrance moves the cursor forward, so a caller must
// read Advanced from every call; the fact also stays on the cursor and is
// available from Entry.AdvancedPastCursor.
type ExecutionResult struct {
	Advanced bool
}

// TaskStartInput is what a launcher records when it claims a card.
type TaskStartInput struct {
	Owner   string
	Session string
	Window  string
	// ExecResolved is the EXEC_RESOLVED value of a card that pins something.
	ExecResolved string
}

// TakeoverInput is what a takeover records when it replaces an executor.
type TakeoverInput struct {
	Dispatch     *DispatchTakeover
	Owner        string
	Session      string
	Window       string
	ExecResolved string
	Decision     string
}

// ExecutionStanding is what a launcher must know before it destroys anything.
type ExecutionStanding struct {
	// Superseded: the record of this launcher ended without it.
	Superseded bool `json:"superseded"`
	// Evidence: the card moved past the cursor. The launcher's own writes keep
	// the cursor current, so they never count. A dispatch or a human update counts
	// too; that misjudgment keeps a container and is the safe direction.
	Evidence bool `json:"evidence"`
}

// ReadExecutionStanding judges a failed launch on the latest card. It writes
// nothing, so the launcher can call it before it closes a container.
func ReadExecutionStanding(root string, entry Entry) (standing ExecutionStanding, err error) {
	v := entry.Version
	if v == nil {
		return standing, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	scope := executionScope(entry)
	scope.ReadOnly = true
	err = WithTransaction(root, scope, func(tx *Transaction) error {
		s, e := tx.Snapshot(entry.TaskID)
		if e != nil {
			return e
		}
		l, e := readExecutionLedger(tx, s)
		if e != nil {
			return e
		}
		standing.Superseded = l.supersedes(v.fence)
		standing.Evidence = v.advanced || s.Revision != v.revision || s.Entry.State != entry.State
		return nil
	})
	return standing, err
}

// AdvancedPastCursor reports whether any entrance call on this cursor saw the
// card ahead of it.
func (e Entry) AdvancedPastCursor() bool {
	if e.Version == nil {
		return false
	}
	e.Version.mu.Lock()
	defer e.Version.mu.Unlock()
	return e.Version.advanced
}

func executionScope(entry Entry) LockScope {
	return LockScope{Groups: []string{taskStartGroup}, Tasks: []string{entry.TaskID}, warnings: entryWarningLog(entry)}
}

// CurrentExecution returns the current record of a card, stored or synthesized.
func CurrentExecution(root, task string) (r ExecutionRecord, exists bool, err error) {
	view, err := readExecutionView(context.Background(), root, task)
	return view.Record, view.Exists, err
}

// ExecutionView is the current record of a card as a probe reads it. Stored is
// false for a card no ledger link was written for: a card from before the ledger.
type ExecutionView struct {
	Record ExecutionRecord
	Exists bool
	Stored bool
}

// ReadEntryExecution reads the current record under the caller's probe deadline.
func ReadEntryExecution(ctx context.Context, entry Entry) (ExecutionView, error) {
	return readExecutionView(ctx, boardRootFromEntry(entry), entry.TaskID)
}

func readExecutionView(ctx context.Context, root, task string) (view ExecutionView, err error) {
	task, err = NormalizeTaskID(task)
	if err != nil {
		return view, err
	}
	err = withTransaction(ctx, root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{task}, ReadOnly: true}, func(tx *Transaction) error {
		s, e := tx.Snapshot(task)
		if e != nil {
			return e
		}
		l, e := readExecutionLedger(tx, s)
		if e != nil {
			return e
		}
		view.Stored = l.synthesized < len(l.chain)
		if l.current != nil {
			view.Record, view.Exists = *l.current, true
		}
		return nil
	})
	return view, err
}

// ExecutionCycle returns the execution cycle a card text carries.
func ExecutionCycle(entry Entry, text string) string {
	return planCycle(Snapshot{Entry: entry, Text: text})
}

// storedRecordID reads the current.json pointer without validating it. Every
// ledger writer holds the task lock exclusively, so the task lock alone makes this
// read consistent with the card the same transaction reads.
func (tx *Transaction) storedRecordID(task string) (string, error) {
	data, exists, err := fs.ReadRegularFileIfExists(tx.root, control(tx.root, "groups", taskStartGroup, task, "current.json"))
	if err != nil || !exists {
		return "", err
	}
	var pointer struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(data, &pointer); err != nil {
		return "", coordinatorError("start attempt identity")
	}
	return pointer.ID, nil
}

// pinExecution records which execution a snapshot belongs to, so a later binding
// can tell that execution from a successor.
func (tx *Transaction) pinExecution(s Snapshot) error {
	record, err := tx.storedRecordID(s.Entry.TaskID)
	if err != nil {
		return err
	}
	s.Entry.Version.record, s.Entry.Version.pinned = record, true
	return nil
}

// BindExecutionCursor fences a snapshot's cursor to the execution that snapshot
// read. A record that replaced it since then is a conflict, because every decision
// the caller drew from the snapshot is about the old execution. Reading the
// current record alone would hand the caller its successor. A launcher gets this
// binding from BeginTaskStart or BeginTakeover; every other writer binds once,
// before its first entrance call.
func BindExecutionCursor(root string, entry Entry) (r ExecutionRecord, err error) {
	v := entry.Version
	if v == nil {
		return r, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.pinned {
		return r, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	err = WithTransaction(root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{entry.TaskID}, ReadOnly: true}, func(tx *Transaction) error {
		record, e := tx.storedRecordID(entry.TaskID)
		if e != nil {
			return e
		}
		// The cursor's own fence also passes: its first write on a legacy card
		// stores the synthesized record it was bound to, so the pointer changes
		// while the execution stays the same.
		if record != v.record && record != v.fence.RecordID {
			return kanbanError("board.transaction_conflict", entry.TaskID)
		}
		s, e := tx.Snapshot(entry.TaskID)
		if e != nil {
			return e
		}
		l, e := readExecutionLedger(tx, s)
		if e != nil {
			return e
		}
		if l.current == nil {
			return coordinatorError("start attempt missing")
		}
		r = *l.current
		return nil
	})
	if err == nil {
		v.fence = ExecutionFence{RecordID: r.ID, Cycle: r.Cycle}
	}
	return r, err
}

// BeginTaskStart claims a todo card for a launcher in one transaction: the move,
// the start metadata, the pending record and its transition.
func BeginTaskStartWithTransaction(entry Entry, root string, in TaskStartInput, extensions ...TransactionExtension) (Entry, error) {
	moved, err := MoveWithOptions(entry, root, "working", MoveOptions{Owner: in.Owner, start: &in, Transaction: combineExtensions(extensions)})
	if err == nil {
		moved.Version.fence.launcher = true
	}
	return moved, err
}

// executionWrite changes fields of the current record on the latest snapshot. It
// checks the fence and the execution cycle, never the card revision. fn returns
// the new card text, or "" when the card stays unchanged; a change of text
// publishes one new revision and needs a card in working or review. A pure
// confirmation changes no text, so a fast executor may have moved the card on.
func executionWrite(ctx context.Context, root string, entry Entry, fn func(tx *Transaction, s Snapshot, l *executionLedger) (string, error), extensions ...TransactionExtension) (ExecutionResult, error) {
	return executionWriteGuarded(ctx, root, entry, fn, requireIdentityWrite, extensions...)
}
func executionWriteGuarded(ctx context.Context, root string, entry Entry, fn func(tx *Transaction, s Snapshot, l *executionLedger) (string, error), identityGuard func(*Transaction, Snapshot, *Version) error, extensions ...TransactionExtension) (result ExecutionResult, err error) {
	v := entry.Version
	if v == nil {
		return result, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var revision uint64
	err = withTransaction(ctx, root, executionScope(entry), func(tx *Transaction) error {
		s, e := tx.Snapshot(entry.TaskID)
		if e != nil {
			return e
		}
		l, e := readExecutionLedger(tx, s)
		if e != nil {
			return e
		}
		if l.supersedes(v.fence) {
			return ErrExecutionSuperseded
		}
		if l.current == nil || v.fence.RecordID == "" || l.current.ID != v.fence.RecordID || l.current.Cycle != v.fence.Cycle || l.current.Cycle != planCycle(s) {
			return coordinatorError("start success binding")
		}
		text, e := fn(tx, s, &l)
		if e != nil {
			return e
		}
		result.Advanced = s.Revision != v.revision || s.Entry.State != entry.State
		revision = s.Revision
		if text == "" || text == s.Text {
			return nil
		}
		if e = identityGuard(tx, s, v); e != nil {
			return e
		}
		revision++
		return tx.Put(entry.TaskID, "spec.md", text)
	}, extensions...)
	if err == nil {
		v.revision, v.advanced = revision, v.advanced || result.Advanced
	}
	return result, err
}

// requireIdentityWrite holds the checks every card write of an identity shares: a
// wrap-up-only grant owns no identity, and the cursor's authorization must still
// be the card's. An empty authorization counts: a card that a dispatch bound after
// the cursor was read refuses the write.
func requireIdentityWrite(tx *Transaction, s Snapshot, v *Version) error {
	if s.Entry.State != "working" && s.Entry.State != "review" {
		return coordinatorError("start success binding")
	}
	if err := tx.requireExecution(s, v.authorization, false); err != nil {
		return err
	}
	return tx.requireFullExecution(s)
}

// fencedLedger projects the card for a fenced entrance and names the record
// the cursor v is bound to.
func fencedLedger(s Snapshot, l *executionLedger, v *Version) (*ledgerKernel, uint64, error) {
	k, err := projectLedger(s, *l)
	if err != nil {
		return nil, 0, err
	}
	return k, k.fence(*l, s, v.fence), nil
}

func applyExecResolved(text, resolved string) (string, error) {
	if resolved == "" {
		return text, nil
	}
	return WithResolvedRecord(text, FieldExecResolved, resolved)
}

// RecordExecutionWindow writes WINDOW, with the EXEC_RESOLVED value of a relaunch.
func RecordExecutionWindowWithTransaction(root string, entry Entry, window, resolved string, extensions ...TransactionExtension) (ExecutionResult, error) {
	return executionWrite(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		text, err := setExecutionField(s.Text, FieldWindow, FieldSession, window)
		if err != nil {
			return "", err
		}
		if text, err = applyExecResolved(text, resolved); err != nil {
			return "", err
		}
		if MetadataFrom(s.Text, FieldWindow) == window {
			return text, nil
		}
		k, fence, err := fencedLedger(s, l, entry.Version)
		if err != nil {
			return "", err
		}
		if !k.step(executionledger.Field{Fence: fence, Text: true}) {
			return "", coordinatorError("start success binding")
		}
		updated := *l.current
		updated.Window = window
		if err = putCurrentRecord(tx, s, l, updated); err != nil {
			return "", err
		}
		return text, putTransition(tx, l, transitionFrom(TransitionWindowUpdate, s, l.current).withCard(s, text, s.Revision+1))
	}, extensions...)
}

// RecordExecutionSession replaces SESSION on the current record without changing
// its status. Session repair and the session a takeover or resume discovers use it.
func RecordExecutionSessionWithTransaction(ctx context.Context, root string, entry Entry, before, after string, extensions ...TransactionExtension) (ExecutionResult, error) {
	if before == "" || after == "" || before == after {
		return ExecutionResult{}, coordinatorError("start discovered session binding")
	}
	return executionWrite(ctx, root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		current := MetadataFrom(s.Text, FieldSession)
		if current == after {
			return "", nil
		}
		if current != before {
			return "", coordinatorError("start success binding")
		}
		text, err := setMetadata(s.Text, FieldSession, after)
		if err != nil {
			return "", err
		}
		k, fence, err := fencedLedger(s, l, entry.Version)
		if err != nil {
			return "", err
		}
		if !k.step(executionledger.Field{Fence: fence, Text: true}) {
			return "", coordinatorError("start success binding")
		}
		updated := *l.current
		updated.Session = after
		if err = putCurrentRecord(tx, s, l, updated); err != nil {
			return "", err
		}
		return text, putTransition(tx, l, transitionFrom(TransitionSessionRepair, s, l.current).withCard(s, text, s.Revision+1))
	}, extensions...)
}

// RecordExecutionProcess writes the process identity of the current record once.
// The original is immutable; the card stays unchanged.
func RecordExecutionProcessWithTransaction(root string, entry Entry, process ExecutionProcess, extensions ...TransactionExtension) (ExecutionResult, error) {
	return executionWrite(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		if l.current.Process != nil {
			return "", coordinatorError("immutable start original")
		}
		path := executionPath(l.task, l.current.ID, "process")
		if _, ok, err := tx.ReadGroup(taskStartGroup, path); err != nil {
			return "", err
		} else if ok {
			return "", coordinatorError("immutable start original")
		}
		k, fence, err := fencedLedger(s, l, entry.Version)
		if err != nil {
			return "", err
		}
		if !k.step(executionledger.Field{Fence: fence, Text: false}) {
			return "", coordinatorError("start success binding")
		}
		if err := tx.PutGroup(taskStartGroup, path, reviewJSON(process)); err != nil {
			return "", err
		}
		updated := *l.current
		updated.Process = &process
		if err := putCurrentRecord(tx, s, l, updated); err != nil {
			return "", err
		}
		return "", putTransition(tx, l, transitionFrom(TransitionProcessIdentity, s, l.current).withCard(s, s.Text, s.Revision))
	}, extensions...)
}

// BeginTakeover replaces the execution of a card in one transaction: the handoff
// link, the handed-off original of the old record, a new pending record, and
// OWNER, SESSION and WINDOW on the latest snapshot. It preserves the cycle.
func BeginTakeoverWithTransaction(root string, entry Entry, in TakeoverInput, extensions ...TransactionExtension) (ExecutionResult, error) {
	v := entry.Version
	if v == nil {
		return ExecutionResult{}, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	var fence ExecutionFence
	settled := false
	var nextAuthorization ExecutionAuthorization
	guard := func(tx *Transaction, s Snapshot, v *Version) error {
		if !settled {
			return requireIdentityWrite(tx, s, v)
		}
		if (s.Entry.State != "working" && s.Entry.State != "review") || v.authorization != authFrom(s.Text) {
			return dispatchError(v.authorization.DispatchID)
		}
		return tx.requireFullExecution(s)
	}
	result, err := executionWriteGuarded(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		cur := l.current
		k, kfence, err := fencedLedger(s, l, v)
		if err != nil {
			return "", err
		}
		decided := strings.TrimSpace(in.Decision) != ""
		// The refusals that do not depend on the new card text come first, before
		// any dispatch is staged.
		var refusal error
		if cur.ExecutionTerminal() {
			refusal = coordinatorError("start success binding")
		} else if cur.StartUnfinished() && !decided {
			refusal = kanbanError("board.takeover_decision_required", l.task, cur.Status)
		}
		if probe := *k; refusal != nil && !probe.step(executionledger.Takeover{Fence: kfence, Decision: decided}) {
			return "", refusal
		}
		source, released, err := stageTakeoverDispatch(tx, s, in.Dispatch, in.Decision)
		if err != nil {
			return "", err
		}
		settled = released
		text, err := renderTakeover(source, in)
		if err != nil {
			return "", err
		}
		// A takeover by the same identity publishes no new card revision.
		nextAuthorization = authFrom(text)
		revision := s.Revision
		if text != s.Text {
			revision++
		}
		if !k.step(executionledger.Takeover{Fence: kfence, Decision: decided, Changes: text != s.Text}) {
			return "", firstError(refusal, coordinatorError("start success binding"))
		}
		if err = storeLegacyTakeover(tx, s, l); err != nil {
			return "", err
		}
		retired := *cur
		retired.Schema, retired.Status, retired.FinishedRevision = executionSchema, ExecutionHandedOff, revision
		if err = putRetiredOriginal(tx, retired, statusOriginal(ExecutionHandedOff)); err != nil {
			return "", err
		}
		next := ExecutionRecord{Schema: executionSchema, ID: tx.record.ID, TaskID: l.task, PreviousID: cur.ID, Cycle: cur.Cycle, Kind: ExecutionTakeover, Status: ExecutionPending, Owner: in.Owner, Session: in.Session, Window: in.Window, Revision: revision}
		if err = putRecordOriginal(tx, next, "pending"); err != nil {
			return "", err
		}
		t := transitionFrom(TransitionHandoff, s, cur).withCard(s, text, revision)
		t.ToRecord, t.FromStatus, t.Decision = next.ID, cur.Status, in.Decision
		fence = ExecutionFence{RecordID: next.ID, Cycle: next.Cycle, launcher: true}
		return text, putTransition(tx, l, t)
	}, guard, extensions...)
	if err == nil {
		v.mu.Lock()
		v.fence, v.record, v.pinned = fence, fence.RecordID, true
		v.authorization = nextAuthorization
		v.mu.Unlock()
	}
	return result, err
}

// UnresolveExecution ends a start attempt whose late step failed while an executor
// may still work. Only the launcher that holds the attempt writes it. The cycle
// stays, and the card keeps its column. A container address the card lacks is
// recorded here, because the next owner needs it; a card that already left working
// and review keeps its text.
func UnresolveExecutionWithTransaction(root string, entry Entry, window, reason string, extensions ...TransactionExtension) (ExecutionResult, error) {
	if strings.TrimSpace(reason) == "" {
		return ExecutionResult{}, coordinatorError("start unresolved reason")
	}
	v := entry.Version
	if v == nil {
		return ExecutionResult{}, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	return executionWrite(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		cur := l.current
		k, fence, err := fencedLedger(s, l, v)
		if err != nil {
			return "", err
		}
		if !k.step(executionledger.Unresolve{Fence: fence, At: k.at(v.revision), Launcher: v.fence.launcher, Window: window != "" && window != MetadataFrom(s.Text, FieldWindow)}) {
			return "", coordinatorError("start unresolved binding")
		}
		text, revision := s.Text, s.Revision
		if window != "" && window != MetadataFrom(s.Text, FieldWindow) && (s.Entry.State == "working" || s.Entry.State == "review") {
			var err error
			if text, err = setExecutionField(s.Text, FieldWindow, FieldSession, window); err != nil {
				return "", err
			}
			revision++
		}
		r := *cur
		r.Schema, r.Status, r.FinishedRevision, r.Failure = executionSchema, ExecutionUnresolved, revision, reason
		r.Window = MetadataFrom(text, FieldWindow)
		if err := putRecordOriginal(tx, r, statusOriginal(ExecutionUnresolved)); err != nil {
			return "", err
		}
		t := transitionFrom(TransitionUnresolve, s, cur).withCard(s, text, revision)
		t.FromStatus = cur.Status
		return text, putTransition(tx, l, t)
	}, extensions...)
}

// StartIdentityChanged reports a start attempt whose card shows another OWNER or
// SESSION than the attempt recorded. A takeover of Kander 1.0.0 left this shape:
// it rewrote the card and left the attempt pending.
func StartIdentityChanged(s Snapshot, record ExecutionRecord) bool {
	owner, session, _ := cardExecutionFacts(s)
	return record.Owner != owner || record.Session != session
}

// CheckAdoptable refuses an adoption the card cannot take before any probe runs:
// the card left working and review, or it no longer shows the identity of the
// start attempt. Only a takeover ends such an attempt.
func CheckAdoptable(s Snapshot, record ExecutionRecord) error {
	if s.Entry.State != "working" && s.Entry.State != "review" || StartIdentityChanged(s, record) {
		return kanbanError("board.adopt_start_identity_changed", s.Entry.TaskID, record.Status)
	}
	return nil
}

// AdoptExecution keeps the running executor of an unfinished start and ends the
// attempt. observed is the snapshot the caller proved alive, with a cursor bound
// to its record. A handoff in between refuses the write through the fence. A new
// OWNER, SESSION or WINDOW on the same record refuses it here: the proof named the
// old identity. The card stays unchanged.
func AdoptExecutionWithTransaction(root string, observed Snapshot, decision string, extensions ...TransactionExtension) (ExecutionResult, error) {
	if strings.TrimSpace(decision) == "" {
		return ExecutionResult{}, kanbanError("board.adopt_decision_required", observed.Entry.TaskID)
	}
	owner, session, window := cardExecutionFacts(observed)
	return executionWrite(context.Background(), root, observed.Entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		cur := l.current
		adoptable := CheckAdoptable(s, *cur)
		o, ses, w := cardExecutionFacts(s)
		same := o == owner && ses == session && w == window
		var refusal error
		switch {
		case !cur.StartUnfinished():
			refusal = kanbanError("board.adopt_needs_unfinished_start", l.task, cur.Status)
		case adoptable != nil:
			refusal = adoptable
		case !same:
			refusal = coordinatorError("start adoption binding")
		}
		k, fence, err := fencedLedger(s, l, observed.Entry.Version)
		if err != nil {
			return "", err
		}
		if !k.step(executionledger.Adopt{Fence: fence, Decision: true, Identity: adoptable == nil && same}) {
			return "", firstError(refusal, coordinatorError("start adoption binding"))
		}
		r := *cur
		r.Schema, r.Status, r.FinishedRevision, r.Failure = executionSchema, ExecutionRunning, s.Revision, ""
		if err := putRecordOriginal(tx, r, statusOriginal(ExecutionRunning)); err != nil {
			return "", err
		}
		t := transitionFrom(TransitionAdopt, s, cur).withCard(s, s.Text, s.Revision)
		t.FromStatus, t.Decision = cur.Status, decision
		return "", putTransition(tx, l, t)
	}, extensions...)
}

// ConfirmTaskStart records launcher success. The card stays unchanged, so a fast
// executor keeps its revision. The cursor's fence stops a stale launcher.
func ConfirmTaskStartWithTransaction(root string, entry Entry, extensions ...TransactionExtension) error {
	return confirmExecution(root, entry, false, func(Snapshot, *executionLedger) bool { return true }, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		return "", finishConfirm(tx, s, s.Text, s.Revision, l)
	}, extensions...)
}

// ConfirmTaskStartSession replaces a launch-time provisional session and records
// start success. It compares the execution cycle, OWNER, the provisional SESSION
// and the column, never the card revision.
func ConfirmTaskStartSessionWithTransaction(root string, entry Entry, before, after string, extensions ...TransactionExtension) error {
	if before == "" || after == "" || before == after {
		return coordinatorError("start discovered session binding")
	}
	return confirmExecution(root, entry, true, func(s Snapshot, l *executionLedger) bool {
		if l.current.Status == ExecutionRunning {
			return l.current.Session == after
		}
		return MetadataFrom(s.Text, FieldSession) == before && l.current.Session == before
	}, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		text, err := setMetadata(s.Text, FieldSession, after)
		if err != nil {
			return "", err
		}
		l.current.Session = after
		return text, finishConfirm(tx, s, text, s.Revision+1, l)
	}, extensions...)
}

// confirmExecution runs a confirmation. An already running record of this fence
// is a retry that writes nothing. matches reports whether the card and the record
// show the identity the confirmation names, for a pending record or a retry;
// session marks a confirmation that writes SESSION.
func confirmExecution(root string, entry Entry, session bool, matches func(Snapshot, *executionLedger) bool, confirm func(*Transaction, Snapshot, *executionLedger) (string, error), extensions ...TransactionExtension) error {
	if entry.Version == nil {
		return coordinatorError("start cursor missing")
	}
	_, err := executionWrite(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		k, fence, err := fencedLedger(s, l, entry.Version)
		if err != nil {
			return "", err
		}
		match := l.current.Owner == MetadataFrom(s.Text, FieldOwner) && matches(s, l)
		if !k.step(executionledger.Confirm{Fence: fence, Match: match, Session: session}) {
			return "", coordinatorError("start success binding")
		}
		if l.current.Status == ExecutionRunning {
			return "", nil
		}
		return confirm(tx, s, l)
	}, extensions...)
	return err
}

func finishConfirm(tx *Transaction, s Snapshot, text string, revision uint64, l *executionLedger) error {
	r := *l.current
	r.Schema, r.Status, r.FinishedRevision = executionSchema, ExecutionRunning, revision
	if err := putRecordOriginal(tx, r, "result"); err != nil {
		return err
	}
	t := transitionFrom(TransitionConfirm, s, l.current)
	return putTransition(tx, l, t.withCard(s, text, revision))
}

// setExecutionField writes one metadata field, inserting it right below anchor
// when the card lacks it. A legacy localized field is updated in place.
func setExecutionField(text, field, anchor, value string) (string, error) {
	pattern := FieldLineRe(field)
	lines := pattern.FindAllString(text, -1)
	if len(lines) > 1 {
		return "", kanbanError("launch.task_document_must_contain_exactly_one_metadata_field", field)
	}
	if len(lines) == 1 {
		return replaceFieldLine(pattern, text, RenderField(field, value)), nil
	}
	anchorRe := FieldLineRe(anchor)
	anchors := anchorRe.FindAllString(text, -1)
	if len(anchors) != 1 {
		return "", kanbanError("launch.task_document_must_contain_exactly_one_metadata_field", anchor)
	}
	return replaceFieldLine(anchorRe, text, anchors[0]+"\n"+RenderField(field, value)), nil
}

func replaceFieldLine(pattern *regexp.Regexp, text, line string) string {
	return pattern.ReplaceAllStringFunc(text, func(string) string { return line })
}

// renderStart fills the metadata of a start. OWNER and STARTED_AT must be blank:
// a filled value means an earlier execution left the card.
func renderStart(text, owner string, in TaskStartInput) (string, error) {
	for _, field := range []string{FieldOwner, FieldStartedAt} {
		pattern := regexp.MustCompile(`(?m)^- ` + TokenPattern(field) + `:\s*$`)
		if len(pattern.FindAllStringIndex(text, -1)) != 1 {
			return "", kanbanError("launch.task_document_must_contain_exactly_one_metadata_field", field)
		}
		value := owner
		if field == FieldStartedAt {
			value = nowStamp()
		}
		text = replaceFieldLine(pattern, text, RenderField(field, value))
	}
	text, err := setExecutionField(text, FieldSession, FieldOwner, in.Session)
	if err != nil {
		return "", err
	}
	// WINDOW moves below SESSION: a card can list the two in another order.
	windowLine := regexp.MustCompile(`(?m)^- ` + TokenPattern(FieldWindow) + `:.*\n?`)
	if len(windowLine.FindAllStringIndex(text, -1)) > 1 {
		return "", kanbanError("launch.task_document_must_contain_exactly_one_metadata_field", FieldWindow)
	}
	text = windowLine.ReplaceAllString(text, "")
	if text, err = setExecutionField(text, FieldWindow, FieldSession, in.Window); err != nil {
		return "", err
	}
	if text, err = NewClaimMetadata(text); err != nil {
		return "", err
	}
	return applyExecResolved(text, in.ExecResolved)
}

func renderTakeover(text string, in TakeoverInput) (string, error) {
	for _, pair := range [][2]string{{FieldOwner, in.Owner}, {FieldSession, in.Session}} {
		pattern := FieldLineRe(pair[0])
		if len(pattern.FindAllStringIndex(text, -1)) != 1 {
			return "", kanbanError("launch.task_document_must_contain_exactly_one_metadata_field", pair[0])
		}
		text = replaceFieldLine(pattern, text, RenderField(pair[0], pair[1]))
	}
	text, err := setExecutionField(text, FieldWindow, FieldSession, in.Window)
	if err != nil {
		return "", err
	}
	return applyExecResolved(text, in.ExecResolved)
}
