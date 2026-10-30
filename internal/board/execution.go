package board

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

// taskStartGroup keeps its historical name: the execution ledger extends the
// start attempt control group, so old boards need no migration.
const (
	taskStartGroup   = "00000000-start-group"
	executionSchema  = 2
	transitionSchema = 1
)

// Execution record kinds and statuses. A record never changes kind.
const (
	ExecutionStart    = "start"
	ExecutionClaim    = "claim"
	ExecutionTakeover = "takeover"
	ExecutionReclaim  = "reclaim"

	ExecutionPending    = "pending"
	ExecutionRunning    = "running"
	ExecutionUnresolved = "unresolved"
	ExecutionRolledBack = "rolled-back"
	ExecutionHandedOff  = "handed-off"
)

// Execution transition kinds.
const (
	TransitionStart           = "start"
	TransitionConfirm         = "confirm"
	TransitionRollback        = "rollback"
	TransitionUnresolve       = "unresolve"
	TransitionAdopt           = "adopt"
	TransitionHandoff         = "handoff"
	TransitionClaim           = "claim"
	TransitionReclaim         = "reclaim"
	TransitionSessionRepair   = "session-repair"
	TransitionWindowUpdate    = "window-update"
	TransitionProcessIdentity = "process-identity"
)

// ExecutionProcess is the one-time process identity of a record without a pane.
type ExecutionProcess struct {
	Launcher    string `json:"launcher"`
	KanderPID   int    `json:"kander_pid"`
	KanderStart string `json:"kander_start"`
	AgentPID    int    `json:"agent_pid"`
	AgentStart  string `json:"agent_start"`
	BootID      string `json:"boot_id"`
	Executable  string `json:"executable"`
	RecordedAt  string `json:"recorded_at"`
}

// ExecutionRecord is one execution of a card. current.json points at the latest
// record; the originals below its record directory stay immutable. A schema 1
// start attempt reads as a record of kind start.
type ExecutionRecord struct {
	Schema           int               `json:"schema,omitzero"`
	ID               string            `json:"id"`
	TaskID           string            `json:"task_id"`
	PreviousID       string            `json:"previous_id,omitempty"`
	Cycle            string            `json:"cycle"`
	Kind             string            `json:"kind,omitempty"`
	Status           string            `json:"status"`
	Owner            string            `json:"owner"`
	Session          string            `json:"session"`
	Window           string            `json:"window,omitempty"`
	Process          *ExecutionProcess `json:"process,omitempty"`
	Revision         uint64            `json:"revision"`
	FinishedRevision uint64            `json:"finished_revision,omitzero"`
	Failure          string            `json:"failure,omitempty"`
}

// ExecutionTransition is one immutable link of a card's execution chain. Seq is
// gapless. Every OWNER, SESSION, WINDOW and cycle change of a card has one link.
type ExecutionTransition struct {
	Schema       int    `json:"schema"`
	Seq          uint64 `json:"seq"`
	TaskID       string `json:"task_id"`
	Kind         string `json:"kind"`
	FromRecord   string `json:"from_record,omitempty"`
	ToRecord     string `json:"to_record"`
	FromStatus   string `json:"from_status,omitempty"`
	FromCycle    string `json:"from_cycle"`
	ToCycle      string `json:"to_cycle"`
	OldOwner     string `json:"old_owner"`
	NewOwner     string `json:"new_owner"`
	OldSession   string `json:"old_session"`
	NewSession   string `json:"new_session"`
	OldWindow    string `json:"old_window"`
	NewWindow    string `json:"new_window"`
	CardRevision uint64 `json:"card_revision"`
	DispatchID   string `json:"dispatch_id,omitempty"`
	Decision     string `json:"decision_reference,omitempty"`
}

// ExecutionTerminal reports a record that no longer owns the card.
func (r ExecutionRecord) ExecutionTerminal() bool {
	return r.Status == ExecutionRolledBack || r.Status == ExecutionHandedOff
}

// StartUnfinished reports a start attempt that no launcher, adoption or takeover
// ended yet. Only those three may change the execution until one of them does.
func (r ExecutionRecord) StartUnfinished() bool {
	return r.Status == ExecutionPending || r.Status == ExecutionUnresolved
}

// SelfClaimed reports a record no launcher started, so no session exists.
func (r ExecutionRecord) SelfClaimed() bool {
	return r.Kind == ExecutionClaim || r.Kind == ExecutionReclaim
}

func executionPath(task, id, name string) string { return task + "/" + id + "/" + name + ".json" }
func transitionPath(task string, seq uint64) string {
	return task + "/transitions/" + strconv.FormatUint(seq, 10) + ".json"
}
func emptyCycle(task string) string { return ReviewDigest([]byte(task + "\n")) }

// statusOriginal names the immutable original of a status. Running and
// rolled-back exclude each other, so one record holds at most one result.
func statusOriginal(status string) string {
	switch status {
	case ExecutionPending:
		return "pending"
	case ExecutionRunning, ExecutionRolledBack:
		return "result"
	}
	return status
}

// firstOriginal is the original a record's identity is checked against.
func firstOriginal(kind string) string {
	if kind == ExecutionClaim || kind == ExecutionReclaim {
		return "result"
	}
	return "pending"
}

func readRecordJSON(tx *Transaction, path string) (r ExecutionRecord, exists bool, err error) {
	b, exists, err := tx.ReadGroup(taskStartGroup, path)
	if err == nil && exists {
		if err = DecodeReviewJSON(b, &r); err == nil {
			err = normalizeRecord(&r)
		}
	}
	return
}

// normalizeRecord reads a schema 1 start attempt as a schema 2 record. An
// unknown schema fails closed: the reader cannot know what the record binds.
func normalizeRecord(r *ExecutionRecord) error {
	switch r.Schema {
	case executionSchema:
	case 0:
		r.Kind = ExecutionStart
		if r.Status == "succeeded" {
			r.Status = ExecutionRunning
		}
	default:
		return coordinatorError("start attempt identity")
	}
	return nil
}

func sameRecordIdentity(a, b ExecutionRecord) bool {
	return a.ID == b.ID && a.TaskID == b.TaskID && a.PreviousID == b.PreviousID && a.Cycle == b.Cycle && a.Kind == b.Kind && a.Owner == b.Owner && a.Revision == b.Revision
}

func validRecord(a ExecutionRecord, task string) bool {
	switch a.Kind {
	case ExecutionStart, ExecutionTakeover, ExecutionReclaim:
		if a.Owner == "" {
			return false
		}
	case ExecutionClaim:
	default:
		return false
	}
	switch a.Status {
	case ExecutionPending, ExecutionRunning, ExecutionUnresolved, ExecutionRolledBack, ExecutionHandedOff:
	default:
		return false
	}
	return a.TaskID == task && validDispatchID(a.ID) && a.Revision != 0 && a.Cycle != ""
}

// readExecutionRecord returns the verified current record of a task. A pointer
// missing beside retained originals is damage, never an unstarted card.
func readExecutionRecord(tx *Transaction, task string) (a ExecutionRecord, exists bool, err error) {
	a, exists, err = readRecordJSON(tx, task+"/current.json")
	if err != nil {
		return a, exists, err
	}
	if !exists {
		entries, e := fs.ListDirectory(tx.root, control(tx.root, "groups", taskStartGroup, task))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return a, false, e
		}
		if len(entries) != 0 {
			return a, false, coordinatorError("start pointer missing with retained originals")
		}
		return a, false, nil
	}
	if !validRecord(a, task) {
		return a, true, coordinatorError("start attempt identity")
	}
	first := firstOriginal(a.Kind)
	original, ok, e := readRecordJSON(tx, executionPath(task, a.ID, first))
	if e != nil {
		return a, true, e
	}
	if !ok || !sameRecordIdentity(original, a) {
		return a, true, coordinatorError("start attempt original")
	}
	if a.Status == ExecutionPending {
		if a.FinishedRevision != 0 {
			return a, true, coordinatorError("pending start result")
		}
		return a, true, nil
	}
	name := statusOriginal(a.Status)
	if name == first {
		if original.Status != a.Status {
			return a, true, coordinatorError("start result original")
		}
		return a, true, nil
	}
	result, ok, e := readRecordJSON(tx, executionPath(task, a.ID, name))
	if e != nil {
		return a, true, e
	}
	if !ok || !sameRecordIdentity(result, a) || result.Status != a.Status || a.FinishedRevision < a.Revision {
		return a, true, coordinatorError("start result original")
	}
	return a, true, nil
}

// putRecordOriginal publishes one immutable original and moves the pointer. An
// existing original is never overwritten, so an old record cannot be replayed.
func putRecordOriginal(tx *Transaction, a ExecutionRecord, name string) error {
	path := executionPath(a.TaskID, a.ID, name)
	if _, ok, err := tx.ReadGroup(taskStartGroup, path); err != nil {
		return err
	} else if ok {
		return coordinatorError("immutable start original")
	}
	if err := tx.PutGroup(taskStartGroup, path, reviewJSON(a)); err != nil {
		return err
	}
	return tx.PutGroup(taskStartGroup, a.TaskID+"/current.json", reviewJSON(a))
}

// putRetiredOriginal records the end of a record its successor replaces.
func putRetiredOriginal(tx *Transaction, a ExecutionRecord, name string) error {
	path := executionPath(a.TaskID, a.ID, name)
	if _, ok, err := tx.ReadGroup(taskStartGroup, path); err != nil {
		return err
	} else if ok {
		return coordinatorError("immutable start original")
	}
	return tx.PutGroup(taskStartGroup, path, reviewJSON(a))
}

// putCurrentRecord replaces the pointer without a new status. A record that was
// only synthesized gets its originals here: the first write on a legacy card
// makes it a stored record.
func putCurrentRecord(tx *Transaction, a ExecutionRecord) error {
	if _, ok, err := tx.ReadGroup(taskStartGroup, executionPath(a.TaskID, a.ID, firstOriginal(a.Kind))); err != nil {
		return err
	} else if !ok {
		return publishRecord(tx, a)
	}
	return tx.PutGroup(taskStartGroup, a.TaskID+"/current.json", reviewJSON(a))
}

// publishRecord writes a record that starts in a settled status, with the
// originals its reader requires.
func publishRecord(tx *Transaction, a ExecutionRecord) error {
	if err := tx.PutGroup(taskStartGroup, executionPath(a.TaskID, a.ID, firstOriginal(a.Kind)), reviewJSON(a)); err != nil {
		return err
	}
	if name := statusOriginal(a.Status); name != firstOriginal(a.Kind) {
		a.FinishedRevision = a.Revision
		if err := tx.PutGroup(taskStartGroup, executionPath(a.TaskID, a.ID, name), reviewJSON(a)); err != nil {
			return err
		}
	}
	return tx.PutGroup(taskStartGroup, a.TaskID+"/current.json", reviewJSON(a))
}

// readTransitions returns the stored transitions in order. It requires a gapless
// sequence, so a removed record reads as damage.
func readTransitions(tx *Transaction, task string) ([]ExecutionTransition, error) {
	entries, err := fs.ListDirectory(tx.root, filepath.Join(control(tx.root, "groups", taskStartGroup, task), "transitions"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	seqs := make([]uint64, 0, len(entries))
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name, ".json")
		seq, e := strconv.ParseUint(name, 10, 64)
		if !ok || e != nil || seq == 0 {
			return nil, coordinatorError("execution transition name")
		}
		seqs = append(seqs, seq)
	}
	slices.Sort(seqs)
	result := make([]ExecutionTransition, 0, len(seqs))
	for i, seq := range seqs {
		b, ok, e := tx.ReadGroup(taskStartGroup, transitionPath(task, seq))
		if e != nil {
			return nil, e
		}
		var t ExecutionTransition
		if !ok {
			return nil, coordinatorError("execution transition missing")
		}
		if e = DecodeReviewJSON(b, &t); e != nil {
			return nil, e
		}
		if t.Schema != transitionSchema || t.Seq != seq || t.TaskID != task || !validTransitionKind(t.Kind) || t.ToRecord == "" || i > 0 && seq != seqs[i-1]+1 {
			return nil, coordinatorError("execution transition sequence")
		}
		result = append(result, t)
	}
	return result, nil
}

func validTransitionKind(kind string) bool {
	switch kind {
	case TransitionStart, TransitionConfirm, TransitionRollback, TransitionUnresolve, TransitionAdopt, TransitionHandoff, TransitionClaim, TransitionReclaim, TransitionSessionRepair, TransitionWindowUpdate, TransitionProcessIdentity:
		return true
	}
	return false
}

// putTransition appends one link. The caller fills the facts of the change; this
// function stamps the sequence and refuses to rewrite an existing record.
func putTransition(tx *Transaction, l *executionLedger, t ExecutionTransition) error {
	t.Schema, t.TaskID = transitionSchema, l.task
	t.Seq = uint64(len(l.chain)) + 1
	path := transitionPath(l.task, t.Seq)
	if _, ok, err := tx.ReadGroup(taskStartGroup, path); err != nil {
		return err
	} else if ok {
		return coordinatorError("immutable execution transition")
	}
	if err := tx.PutGroup(taskStartGroup, path, reviewJSON(t)); err != nil {
		return err
	}
	l.chain = append(l.chain, t)
	return nil
}

// executionLedger is the read view of one card: the current record, stored or
// synthesized, and the chain whose first synthesized links stay in memory.
type executionLedger struct {
	task        string
	current     *ExecutionRecord
	chain       []ExecutionTransition
	synthesized int
}

func (l executionLedger) end() uint64 { return uint64(len(l.chain)) }

// supersedes reports that a handoff, an adoption or a reclaim ended the record a
// launcher fenced. The launcher no longer speaks for the execution.
func (l executionLedger) supersedes(f ExecutionFence) bool {
	return f.launcher && slices.ContainsFunc(l.chain, func(t ExecutionTransition) bool {
		return t.FromRecord == f.RecordID && (t.Kind == TransitionHandoff || t.Kind == TransitionAdopt || t.Kind == TransitionReclaim)
	})
}

// tail returns the facts the last link leaves behind.
func (l executionLedger) tail() (ExecutionTransition, bool) {
	if len(l.chain) == 0 {
		return ExecutionTransition{}, false
	}
	return l.chain[len(l.chain)-1], true
}

// readExecutionLedger builds the view of one card. A card with stored
// transitions continues the synthesized prefix of its legacy history; a card
// without any is read entirely from legacy facts and nothing is written back.
func readExecutionLedger(tx *Transaction, s Snapshot) (executionLedger, error) {
	task := s.Entry.TaskID
	l := executionLedger{task: task}
	current, exists, err := readExecutionRecord(tx, task)
	if err != nil {
		return l, err
	}
	stored, err := readTransitions(tx, task)
	if err != nil {
		return l, err
	}
	if exists {
		l.current = &current
	}
	if len(stored) > 0 && stored[0].Seq == 1 {
		l.chain = stored
		return l, verifyChain(l)
	}
	prefix, err := synthesizeChain(tx, s, stored)
	if err != nil {
		return l, err
	}
	if len(stored) > 0 && stored[0].Seq != uint64(len(prefix))+1 {
		return l, coordinatorError("execution transition sequence")
	}
	l.chain = append(prefix, stored...)
	l.synthesized = len(prefix)
	if len(stored) == 0 {
		l.current = synthesizeCurrent(s, l.current, prefix)
	}
	return l, verifyChain(l)
}

// verifyChain requires each link to continue the previous one. Legacy links carry
// no session or window, so identity is checked from the first stored link on.
func verifyChain(l executionLedger) error {
	for i, t := range l.chain {
		if i == 0 {
			continue
		}
		p := l.chain[i-1]
		if t.Seq != p.Seq+1 || t.FromCycle != p.ToCycle || t.FromRecord != p.ToRecord {
			return coordinatorError("execution chain broken")
		}
		if i > l.synthesized && (t.OldOwner != p.NewOwner || t.OldSession != p.NewSession || t.OldWindow != p.NewWindow) {
			return coordinatorError("execution chain broken")
		}
	}
	return nil
}

// assertCard is the consistency assertion the snapshot comparison became: the
// chain ends exactly at the card, so a change no link records is an error.
func (l executionLedger) assertCard(s Snapshot) error {
	t, ok := l.tail()
	cycle := planCycle(s)
	if !ok {
		if cycle != emptyCycle(l.task) || l.current != nil || s.Entry.State == "working" || s.Entry.State == "review" {
			return coordinatorError("member execution cycle changed without first-start facts: " + l.task)
		}
		return nil
	}
	unrecorded := t.ToCycle != cycle
	if len(l.chain) > l.synthesized {
		unrecorded = unrecorded || t.NewOwner != MetadataFrom(s.Text, FieldOwner) || t.NewSession != MetadataFrom(s.Text, FieldSession) || t.NewWindow != MetadataFrom(s.Text, FieldWindow)
	}
	if l.current != nil && l.current.StartUnfinished() && (l.current.Owner != MetadataFrom(s.Text, FieldOwner) || l.current.Session != MetadataFrom(s.Text, FieldSession)) {
		unrecorded = true
	}
	if unrecorded {
		if l.current != nil && l.current.StartUnfinished() {
			return coordinatorError("pending start facts changed")
		}
		return coordinatorError("execution cycle changed without lifecycle handoff")
	}
	if l.current != nil && (s.Entry.State == "todo" || s.Entry.State == "backlog") && !l.current.ExecutionTerminal() {
		return coordinatorError("member execution cycle changed without first-start facts: " + l.task)
	}
	return nil
}

func cardExecutionFacts(s Snapshot) (owner, session, window string) {
	return MetadataFrom(s.Text, FieldOwner), MetadataFrom(s.Text, FieldSession), MetadataFrom(s.Text, FieldWindow)
}

// transitionFrom fills the from side of a link from the card before the change.
func transitionFrom(kind string, s Snapshot, cur *ExecutionRecord) ExecutionTransition {
	t := ExecutionTransition{Kind: kind, FromCycle: planCycle(s)}
	t.OldOwner, t.OldSession, t.OldWindow = cardExecutionFacts(s)
	t.NewOwner, t.NewSession, t.NewWindow = t.OldOwner, t.OldSession, t.OldWindow
	t.ToCycle = t.FromCycle
	if cur != nil {
		t.FromRecord, t.ToRecord = cur.ID, cur.ID
	}
	return t
}

// withCard fills the to side of a link from the card after the change.
func (t ExecutionTransition) withCard(s Snapshot, text string, revision uint64) ExecutionTransition {
	next := Snapshot{Entry: s.Entry, Text: text}
	t.ToCycle = planCycle(next)
	t.NewOwner, t.NewSession, t.NewWindow = cardExecutionFacts(next)
	t.CardRevision = revision
	return t
}

// stageExecutionMove writes the record of a claim, a reclaim or a start in the
// transaction of the move. handoff is the protected reclaim record, if any.
func stageExecutionMove(tx *Transaction, s Snapshot, target, text string, o MoveOptions, handoff *lifecycleHandoff) error {
	if target != "working" || s.Entry.State == "working" || s.Entry.State == "review" && o.Owner == "" {
		return nil
	}
	l, err := readExecutionLedger(tx, s)
	if err != nil {
		return err
	}
	next := Snapshot{Entry: s.Entry, Text: text}
	r := ExecutionRecord{Schema: executionSchema, ID: tx.record.ID, TaskID: s.Entry.TaskID, Cycle: planCycle(next), Kind: ExecutionClaim, Status: ExecutionRunning, Owner: MetadataFrom(text, FieldOwner), Session: MetadataFrom(text, FieldSession), Window: MetadataFrom(text, FieldWindow), Revision: s.Revision + 1}
	t := transitionFrom(TransitionClaim, s, l.current)
	t.ToRecord = r.ID
	if l.current != nil {
		r.PreviousID = l.current.ID
	}
	name := "result"
	switch {
	case o.start != nil:
		r.Kind, r.Status = ExecutionStart, ExecutionPending
		t.Kind, name = TransitionStart, "pending"
	case s.Entry.State == "review":
		r.Kind, t.Kind, t.Decision = ExecutionReclaim, TransitionReclaim, o.Decision
		if handoff != nil {
			t.DispatchID = handoff.DispatchID
		}
		if l.current != nil {
			t.FromStatus = l.current.Status
			retired := *l.current
			retired.Schema, retired.Status, retired.FinishedRevision = executionSchema, ExecutionHandedOff, s.Revision+1
			if err := putRetiredOriginal(tx, retired, statusOriginal(ExecutionHandedOff)); err != nil {
				return err
			}
		}
	}
	if s.Entry.State != "review" && l.current != nil && len(l.chain) > l.synthesized && (!l.current.ExecutionTerminal() || s.Revision < l.current.FinishedRevision) {
		return coordinatorError("unfinished or confirmed prior start")
	}
	if r.Kind == ExecutionStart && r.Owner == "" {
		return coordinatorError("start owner required")
	}
	if err := putRecordOriginal(tx, r, name); err != nil {
		return err
	}
	return putTransition(tx, &l, t.withCard(s, text, s.Revision+1))
}

// stageExecutionRollback records a failed launch. The launcher that owns the
// pending record ends it; a takeover also reinstates the record it replaced, so
// the card keeps one current record. Any other caller restores WINDOW only.
func stageExecutionRollback(tx *Transaction, s Snapshot, text, state string, fence ExecutionFence) error {
	l, err := readExecutionLedger(tx, s)
	if err != nil {
		return err
	}
	if l.supersedes(fence) {
		return ErrExecutionSuperseded
	}
	cur := l.current
	if cur == nil || !fence.launcher || cur.ID != fence.RecordID || cur.Status != ExecutionPending {
		return stageWindowRestore(tx, s, text, &l)
	}
	if cur.Cycle != planCycle(s) || s.Revision < cur.Revision || cur.Kind == ExecutionStart && state != "todo" {
		return coordinatorError("start rollback binding")
	}
	rolled := *cur
	rolled.Schema, rolled.Status, rolled.FinishedRevision = executionSchema, ExecutionRolledBack, s.Revision+1
	t := transitionFrom(TransitionRollback, s, cur).withCard(s, text, s.Revision+1)
	t.FromStatus = cur.Status
	if cur.Kind != ExecutionTakeover {
		if err := putRecordOriginal(tx, rolled, statusOriginal(ExecutionRolledBack)); err != nil {
			return err
		}
		return putTransition(tx, &l, t)
	}
	if err := putRetiredOriginal(tx, rolled, statusOriginal(ExecutionRolledBack)); err != nil {
		return err
	}
	replaced, ok, err := readRecordJSON(tx, executionPath(s.Entry.TaskID, cur.PreviousID, statusOriginal(ExecutionHandedOff)))
	if err != nil {
		return err
	}
	if !ok {
		return coordinatorError("start rollback binding")
	}
	var status string
	for _, link := range slices.Backward(l.chain) {
		if link.Kind == TransitionHandoff && link.ToRecord == cur.ID {
			status = link.FromStatus
			break
		}
	}
	if status == "" {
		return coordinatorError("start rollback binding")
	}
	restored := ExecutionRecord{Schema: executionSchema, ID: tx.record.ID, TaskID: s.Entry.TaskID, PreviousID: cur.ID, Cycle: cur.Cycle, Kind: replaced.Kind, Status: status, Owner: t.NewOwner, Session: t.NewSession, Window: t.NewWindow, Revision: s.Revision + 1, Failure: replaced.Failure}
	t.ToRecord = restored.ID
	if err := publishRecord(tx, restored); err != nil {
		return err
	}
	return putTransition(tx, &l, t)
}

// stageWindowRestore lets a failed relaunch restore WINDOW; it never moves
// ownership, because no record of this caller changed ownership.
func stageWindowRestore(tx *Transaction, s Snapshot, text string, l *executionLedger) error {
	if MetadataFrom(text, FieldOwner) != MetadataFrom(s.Text, FieldOwner) || MetadataFrom(text, FieldSession) != MetadataFrom(s.Text, FieldSession) || planCycle(Snapshot{Entry: s.Entry, Text: text}) != planCycle(s) {
		return coordinatorError("start rollback binding")
	}
	before, after := MetadataFrom(s.Text, FieldWindow), MetadataFrom(text, FieldWindow)
	if before == after {
		return nil
	}
	t := transitionFrom(TransitionWindowUpdate, s, l.current).withCard(s, text, s.Revision+1)
	if l.current != nil {
		updated := *l.current
		updated.Window = after
		if err := putCurrentRecord(tx, updated); err != nil {
			return err
		}
	}
	return putTransition(tx, l, t)
}

func lifecycleJSONLines(text string) []string {
	body, _ := SectionBody(text, "LIFECYCLE_DECISION")
	return strings.Split(body, "\n")
}
