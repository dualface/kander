package board

import (
	"context"
	"errors"
	"regexp"
	"strings"
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
	Owner        string
	Session      string
	Window       string
	ExecResolved string
	Decision     string
}

// ExecutionStanding is what a launcher must know before it destroys anything.
type ExecutionStanding struct {
	// Superseded: the record of this launcher ended without it.
	Superseded bool
	// Evidence: the card moved past the cursor. The launcher's own writes keep
	// the cursor current, so they never count. A dispatch or a human update counts
	// too; that misjudgment keeps a container and is the safe direction.
	Evidence bool
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
	task, err = NormalizeTaskID(task)
	if err != nil {
		return r, false, err
	}
	err = WithTransaction(root, LockScope{Groups: []string{taskStartGroup}, Tasks: []string{task}, ReadOnly: true}, func(tx *Transaction) error {
		s, e := tx.Snapshot(task)
		if e != nil {
			return e
		}
		l, e := readExecutionLedger(tx, s)
		if e != nil {
			return e
		}
		if l.current != nil {
			r, exists = *l.current, true
		}
		return nil
	})
	return r, exists, err
}

// BindExecutionCursor fences an operation cursor to the card's current record.
// A launcher gets this binding from BeginTaskStart or BeginTakeover; every other
// writer binds once, before its first entrance call.
func BindExecutionCursor(root string, entry Entry) (ExecutionRecord, error) {
	if entry.Version == nil {
		return ExecutionRecord{}, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	r, exists, err := CurrentExecution(root, entry.TaskID)
	if err != nil {
		return r, err
	}
	if !exists {
		return r, coordinatorError("start attempt missing")
	}
	entry.Version.mu.Lock()
	defer entry.Version.mu.Unlock()
	entry.Version.fence = ExecutionFence{RecordID: r.ID, Cycle: r.Cycle}
	return r, nil
}

// BeginTaskStart claims a todo card for a launcher in one transaction: the move,
// the start metadata, the pending record and its transition.
func BeginTaskStart(entry Entry, root string, in TaskStartInput) (Entry, error) {
	moved, err := MoveWithOptions(entry, root, "working", MoveOptions{Owner: in.Owner, start: &in})
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
func executionWrite(ctx context.Context, root string, entry Entry, fn func(tx *Transaction, s Snapshot, l *executionLedger) (string, error)) (result ExecutionResult, err error) {
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
		if e = requireIdentityWrite(tx, s, v); e != nil {
			return e
		}
		revision++
		return tx.Put(entry.TaskID, "spec.md", text)
	})
	if err == nil {
		v.revision, v.advanced = revision, v.advanced || result.Advanced
	}
	return result, err
}

// requireIdentityWrite holds the checks every card write of an identity shares: a
// wrap-up-only grant owns no identity, and a dispatch cursor must still be current.
func requireIdentityWrite(tx *Transaction, s Snapshot, v *Version) error {
	if s.Entry.State != "working" && s.Entry.State != "review" {
		return coordinatorError("start success binding")
	}
	if v.authorization != (ExecutionAuthorization{}) {
		if err := tx.requireExecution(s, v.authorization, false); err != nil {
			return err
		}
	}
	return tx.requireFullExecution(s)
}

func applyExecResolved(text, resolved string) (string, error) {
	if resolved == "" {
		return text, nil
	}
	return WithResolvedRecord(text, FieldExecResolved, resolved)
}

// RecordExecutionWindow writes WINDOW, with the EXEC_RESOLVED value of a relaunch.
func RecordExecutionWindow(root string, entry Entry, window, resolved string) (ExecutionResult, error) {
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
		updated := *l.current
		updated.Window = window
		if err = putCurrentRecord(tx, updated); err != nil {
			return "", err
		}
		return text, putTransition(tx, l, transitionFrom(TransitionWindowUpdate, s, l.current).withCard(s, text, s.Revision+1))
	})
}

// RecordExecutionSession replaces SESSION on the current record without changing
// its status. Session repair and the session a takeover or resume discovers use it.
func RecordExecutionSession(ctx context.Context, root string, entry Entry, before, after string) (ExecutionResult, error) {
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
		updated := *l.current
		updated.Session = after
		if err = putCurrentRecord(tx, updated); err != nil {
			return "", err
		}
		return text, putTransition(tx, l, transitionFrom(TransitionSessionRepair, s, l.current).withCard(s, text, s.Revision+1))
	})
}

// RecordExecutionProcess writes the process identity of the current record once.
// The original is immutable; the card stays unchanged.
func RecordExecutionProcess(root string, entry Entry, process ExecutionProcess) (ExecutionResult, error) {
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
		if err := tx.PutGroup(taskStartGroup, path, reviewJSON(process)); err != nil {
			return "", err
		}
		updated := *l.current
		updated.Process = &process
		if err := putCurrentRecord(tx, updated); err != nil {
			return "", err
		}
		return "", putTransition(tx, l, transitionFrom(TransitionProcessIdentity, s, l.current).withCard(s, s.Text, s.Revision))
	})
}

// BeginTakeover replaces the execution of a card in one transaction: the handoff
// link, the handed-off original of the old record, a new pending record, and
// OWNER, SESSION and WINDOW on the latest snapshot. It preserves the cycle.
func BeginTakeover(root string, entry Entry, in TakeoverInput) (ExecutionResult, error) {
	v := entry.Version
	if v == nil {
		return ExecutionResult{}, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	var fence ExecutionFence
	result, err := executionWrite(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		cur := l.current
		if cur.ExecutionTerminal() {
			return "", coordinatorError("start success binding")
		}
		if cur.StartUnfinished() && strings.TrimSpace(in.Decision) == "" {
			return "", kanbanError("board.takeover_decision_required", l.task, cur.Status)
		}
		text, err := renderTakeover(s.Text, in)
		if err != nil {
			return "", err
		}
		// A takeover by the same identity publishes no new card revision.
		revision := s.Revision
		if text != s.Text {
			revision++
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
	})
	if err == nil {
		v.mu.Lock()
		v.fence = fence
		v.mu.Unlock()
	}
	return result, err
}

// UnresolveExecution ends a start attempt whose late step failed while an executor
// may still work. Only the launcher that holds the attempt writes it. The cycle
// stays, and the card keeps its column. A container address the card lacks is
// recorded here, because the next owner needs it; a card that already left working
// and review keeps its text.
func UnresolveExecution(root string, entry Entry, window, reason string) (ExecutionResult, error) {
	if strings.TrimSpace(reason) == "" {
		return ExecutionResult{}, coordinatorError("start unresolved reason")
	}
	v := entry.Version
	if v == nil {
		return ExecutionResult{}, kanbanError("board.transaction_conflict", entry.TaskID)
	}
	return executionWrite(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		cur := l.current
		if !v.fence.launcher || cur.Status != ExecutionPending || v.revision < cur.Revision {
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
	})
}

// AdoptExecution keeps the running executor of an unfinished start and ends the
// attempt. The caller proved the executor alive under a cursor bound to this
// record, so a handoff in between refuses the write. The card stays unchanged.
func AdoptExecution(root string, entry Entry, decision string) (ExecutionResult, error) {
	if strings.TrimSpace(decision) == "" {
		return ExecutionResult{}, kanbanError("board.adopt_decision_required", entry.TaskID)
	}
	return executionWrite(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		cur := l.current
		if !cur.StartUnfinished() {
			return "", kanbanError("board.adopt_needs_unfinished_start", l.task, cur.Status)
		}
		if s.Entry.State != "working" && s.Entry.State != "review" || cur.Owner != MetadataFrom(s.Text, FieldOwner) || cur.Session != MetadataFrom(s.Text, FieldSession) {
			return "", coordinatorError("start adoption binding")
		}
		r := *cur
		r.Schema, r.Status, r.FinishedRevision, r.Failure = executionSchema, ExecutionRunning, s.Revision, ""
		if err := putRecordOriginal(tx, r, statusOriginal(ExecutionRunning)); err != nil {
			return "", err
		}
		t := transitionFrom(TransitionAdopt, s, cur).withCard(s, s.Text, s.Revision)
		t.FromStatus, t.Decision = cur.Status, decision
		return "", putTransition(tx, l, t)
	})
}

// ConfirmTaskStart records launcher success. The card stays unchanged, so a fast
// executor keeps its revision. The cursor's fence stops a stale launcher.
func ConfirmTaskStart(root string, entry Entry) error {
	return confirmExecution(root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		return "", finishConfirm(tx, s, s.Text, s.Revision, l)
	}, func(l *executionLedger) error { return nil })
}

// ConfirmTaskStartSession replaces a launch-time provisional session and records
// start success. It compares the execution cycle, OWNER, the provisional SESSION
// and the column, never the card revision.
func ConfirmTaskStartSession(root string, entry Entry, before, after string) error {
	if before == "" || after == "" || before == after {
		return coordinatorError("start discovered session binding")
	}
	return confirmExecution(root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		if MetadataFrom(s.Text, FieldSession) != before || l.current.Session != before {
			return "", coordinatorError("start success binding")
		}
		text, err := setMetadata(s.Text, FieldSession, after)
		if err != nil {
			return "", err
		}
		l.current.Session = after
		return text, finishConfirm(tx, s, text, s.Revision+1, l)
	}, func(l *executionLedger) error {
		if l.current.Session != after {
			return coordinatorError("start success binding")
		}
		return nil
	})
}

// confirmExecution runs a confirmation. An already running record of this fence
// is a retry: replay checks it and writes nothing.
func confirmExecution(root string, entry Entry, confirm func(*Transaction, Snapshot, *executionLedger) (string, error), replay func(*executionLedger) error) error {
	if entry.Version == nil {
		return coordinatorError("start cursor missing")
	}
	_, err := executionWrite(context.Background(), root, entry, func(tx *Transaction, s Snapshot, l *executionLedger) (string, error) {
		cur := l.current
		if cur.ExecutionTerminal() || cur.Status == ExecutionUnresolved || cur.Owner != MetadataFrom(s.Text, FieldOwner) {
			return "", coordinatorError("start success binding")
		}
		if cur.Status == ExecutionRunning {
			return "", replay(l)
		}
		return confirm(tx, s, l)
	})
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
