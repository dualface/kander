package board

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/dualface/kander/internal/statemachine/reviewplan"
)

// A refusal of a request that carries an expected value names the value the
// request holds, the value the board holds, and the next step. The label the
// refusal always had stays its prefix, so a caller or a test that matched it
// still does; the board code, and with it the API code and the HTTP status,
// never changes. Values may be revisions, epochs, hashes, commits, IDs, owners
// and card states. A token, a lease or claim proof, an execution credential, a
// report body, and a disposition reason are never written.

// refusalText joins a label, the parts that say what differs, and the next step.
func refusalText(label, next string, parts ...string) string {
	text := label + ": " + strings.Join(parts, "; ")
	if next != "" {
		text += "\n" + next
	}
	return text
}

// reviewRefusal is a review evidence refusal under its label.
func reviewRefusal(label, next string, parts ...string) error {
	return reviewError(refusalText(label, next, parts...))
}

// coordinatorRefusal is a coordinator conflict under its label.
func coordinatorRefusal(label, next string, parts ...string) error {
	return coordinatorError(refusalText(label, next, parts...))
}

// quoted writes an empty value as "" so a missing value is not mistaken for text
// that was cut off.
func quoted(s string) string {
	if s == "" {
		return `""`
	}
	// A value the request names is echoed as the caller wrote it, so it is cut to
	// a bounded single line: a long or multi-line value never fills the message.
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if runes := []rune(s); len(runes) > refusalValueLimit {
		s = string(runes[:refusalValueLimit]) + "…"
	}
	return s
}

// refusalValueLimit bounds a request value written into a message; it is longer
// than any commit, hash, or ID the board accepts.
const refusalValueLimit = 96

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func isDigest(s string) bool { return digestPattern.MatchString(s) }

func isTaskID(s string) bool {
	_, err := NormalizeTaskID(s)
	return err == nil
}

// shown writes a value the request names only when it has the form its field has.
// Any other text may be a token or a report that went into the wrong field, so
// the message says the value is invalid and does not repeat it.
func shown(s string, valid func(string) bool) string {
	switch {
	case s == "":
		return `""`
	case valid(s):
		return s
	}
	return "<invalid>"
}

// shownOwner writes the owner of a request unless it is one of the tokens of the
// group, which a caller may have put into the wrong field.
func shownOwner(owner string, tokens ...string) string {
	if slices.Contains(tokens, owner) {
		return "<invalid>"
	}
	return quoted(owner)
}

// RefusalTaskID writes a task ID a caller named in full when it is one, and
// otherwise marks it invalid, for the messages of the service.
func RefusalTaskID(s string) string { return shown(s, isTaskID) }

// refusalFact is one fact a kernel event carries and whether it was true.
type refusalFact struct {
	name string
	ok   bool
}

// falseFacts lists the names of the facts that were false, in the given order.
func falseFacts(facts []refusalFact) []string {
	var names []string
	for _, f := range facts {
		if !f.ok {
			names = append(names, f.name)
		}
	}
	return names
}

// kernelRefusal names why a plan kernel refused an event that the shell found no
// reason for: the false facts of the event, or, when every fact is true, the
// conditions of the plan state that do not hold.
func kernelRefusal(label, op string, facts []refusalFact, conditions []string) error {
	if names := falseFacts(facts); len(names) > 0 {
		return reviewRefusal(label, "", t("board.refusal_kernel_facts", op, strings.Join(names, ", ")))
	}
	if len(conditions) == 0 {
		conditions = []string{t("board.refusal_state_unknown")}
	}
	return reviewRefusal(label, "", t("board.refusal_kernel_state", op, strings.Join(conditions, ", ")))
}

// closeRefusal is the refusal of a close the kernel refused although the shell
// found no cause: the false facts, or the state conditions of `stepClose` that
// do not hold. at is the position of the batch in the plan.
func closeRefusal(planID, batchID string, at uint64, f reviewplan.Close, st reviewplan.State) error {
	var conditions []string
	if !st.Planned {
		conditions = append(conditions, t("board.refusal_state_unplanned", planID))
	}
	if at >= st.Count || at >= uint64(len(st.Closed)) {
		conditions = append(conditions, t("board.refusal_state_position", at, st.Count))
	} else if st.Closed[at] {
		conditions = append(conditions, t("board.refusal_state_closed", batchID))
	}
	return kernelRefusal("batch close refused", "close", []refusalFact{
		{"Revision", f.Revision}, {"ViewHash", f.ViewHash}, {"Conclusions", f.Conclusions}, {"Git", f.Git},
		{"Current", f.Current}, {"PreviousClosed", f.PreviousClosed}, {"Bound", f.Bound},
	}, conditions)
}

// sealRefusal is the refusal of a seal the kernel refused although the shell
// found no cause. `stepSeal` also needs a plan that is planned and not sealed.
func sealRefusal(planID string, st reviewplan.State, ev reviewplan.Seal) error {
	var conditions []string
	if !st.Planned {
		conditions = append(conditions, t("board.refusal_state_unplanned", planID))
	}
	if st.Sealed {
		conditions = append(conditions, t("board.refusal_state_sealed", planID))
	}
	return kernelRefusal("plan extension refused", "seal", []refusalFact{
		{"AllAssigned", ev.AllAssigned}, {"NoDropOwed", ev.NoDropOwed},
	}, conditions)
}

// appendRefusal is the refusal of a batch append the kernel refused although the
// shell found no cause. `stepAppend` needs room for one more batch, and a plan
// that is not sealed unless the batch is owed to a member; a sealing append also
// needs every finding assigned and no owed drop.
func appendRefusal(planID string, st reviewplan.State, params reviewplan.Params, ev reviewplan.Append) error {
	var conditions []string
	if !st.Planned {
		conditions = append(conditions, t("board.refusal_state_unplanned", planID))
	}
	if st.Count >= uint64(params.MaxBatch) {
		conditions = append(conditions, t("board.refusal_state_limit", st.Count, params.MaxBatch))
	}
	if st.Sealed && (ev.Seal || !ev.Owed) {
		conditions = append(conditions, t("board.refusal_state_sealed", planID))
	}
	facts := []refusalFact{
		{"Binding", ev.Binding}, {"MembersActive", ev.MembersActive}, {"PreviousClosed", ev.PreviousClosed},
		{"Requirements", ev.Requirements},
	}
	if ev.Seal {
		facts = append(facts, refusalFact{"AllAssigned", ev.AllAssigned}, refusalFact{"NoDropOwed", ev.NoDropOwed})
	}
	return kernelRefusal("plan extension refused", "append", facts, conditions)
}

// admitRefusal is the refusal of a plan extension the kernel refused: the
// request revision, and a sealed plan that takes only the operations that
// `stepAdmit` lets through.
func admitRefusal(p ReviewPlan, st reviewplan.State, x ReviewPlanExtension, ev reviewplan.Admit) error {
	var parts []string
	if !ev.Cas {
		parts = append(parts, t("board.refusal_revision_plan", p.PlanID, x.ExpectedRevision, p.Revision))
	}
	if st.Sealed && !(ev.OtherOp || ev.BatchOnly && ev.Owed) {
		parts = append(parts, t("board.refusal_plan_sealed", p.PlanID))
	}
	if !st.Planned {
		parts = append(parts, t("board.refusal_state_unplanned", p.PlanID))
	}
	if len(parts) == 0 {
		return kernelRefusal("plan extension refused", "admit", nil, nil)
	}
	next := ""
	if !ev.Cas {
		next = t("board.refusal_next_plan", p.PlanID)
	}
	return reviewRefusal("plan extension CAS/sealed conflict", next, parts...)
}

// planRevisionRefusal is the refusal of a request that names another revision of
// a plan than the plan has.
func planRevisionRefusal(label string, p ReviewPlan, expected uint64) error {
	return reviewRefusal(label, t("board.refusal_next_plan", p.PlanID), t("board.refusal_revision_plan", p.PlanID, expected, p.Revision))
}

// batchRevisionRefusal is the refusal of a request that names another revision of
// a batch than the batch has.
func batchRevisionRefusal(label string, b ReviewBatch, expected uint64) error {
	return reviewRefusal(label, t("board.refusal_next_batch", b.BatchID), t("board.refusal_revision_batch", b.BatchID, expected, b.Revision))
}

// closeRequestRefusal is the refusal of a close request that does not describe the
// view it closes: another batch, another revision, another view hash, or no author.
func closeRequestRefusal(v ReviewBatchView, r ReviewCloseRequest) error {
	b := v.Batch
	var parts []string
	if r.BatchID != b.BatchID {
		parts = append(parts, t("board.refusal_close_batch_id", shown(r.BatchID, ValidReviewID), b.BatchID))
	}
	if r.ExpectedRevision != b.Revision {
		parts = append(parts, t("board.refusal_revision_batch", b.BatchID, r.ExpectedRevision, b.Revision))
	}
	if hash := ReviewViewDigest(v); r.ViewHash != hash {
		parts = append(parts, t("board.refusal_view_hash", shown(r.ViewHash, isDigest), hash))
	}
	if len(parts) > 0 {
		return reviewRefusal("close target/revision/view CAS", t("board.refusal_next_close", b.BatchID), parts...)
	}
	if strings.TrimSpace(r.Author) == "" {
		return reviewRefusal("close author required", "", t("board.refusal_author_required"))
	}
	return nil
}

// advanceRefusal is the refusal of an advance whose target conditions fail: the
// previous target is not the batch's, the target does not move, is no full
// commit, or has no reason.
func advanceRefusal(a ReviewAdvance, b ReviewBatch) error {
	var parts []string
	if a.PreviousTarget != b.TargetCommit {
		parts = append(parts, t("board.refusal_previous_target", shown(a.PreviousTarget, validCommit), b.BatchID, b.TargetCommit))
	}
	if a.Target == a.PreviousTarget {
		parts = append(parts, t("board.refusal_target_same", shown(a.Target, validCommit)))
	}
	if !validCommit(a.Target) {
		parts = append(parts, t("board.refusal_target_not_commit", shown(a.Target, validCommit)))
	}
	if strings.TrimSpace(a.Reason) == "" {
		parts = append(parts, t("board.refusal_reason_required"))
	}
	return reviewRefusal(advanceLabel(a.PreviousTarget != b.TargetCommit), t("board.refusal_next_advance", b.BatchID), parts...)
}

// advanceLabel is the label of a refused advance: only a previous_target that is
// not the batch's target is a CAS refusal; the other conditions say what the
// target or the reason lacks.
func advanceLabel(cas bool) string {
	if cas {
		return "batch target CAS conflict"
	}
	return "batch target invalid"
}

// runAdvanceRefusal is the refusal of a run whose commit is not the batch target
// and whose advance file does not move the batch there.
func runAdvanceRefusal(b ReviewBatch, commit string, a *ReviewAdvance) error {
	var parts []string
	switch {
	case a == nil:
		parts = append(parts, t("board.refusal_advance_missing", b.BatchID, b.TargetCommit, commit))
	default:
		if a.PreviousTarget != b.TargetCommit {
			parts = append(parts, t("board.refusal_previous_target", shown(a.PreviousTarget, validCommit), b.BatchID, b.TargetCommit))
		}
		if a.Target != commit {
			parts = append(parts, t("board.refusal_advance_target", shown(a.Target, validCommit), commit))
		}
		if strings.TrimSpace(a.Reason) == "" {
			parts = append(parts, t("board.refusal_reason_required"))
		}
	}
	return reviewRefusal(advanceLabel(a == nil || a.PreviousTarget != b.TargetCommit), t("board.refusal_next_advance", b.BatchID), parts...)
}

// cycleList writes the members of a cycle map as sorted id=cycle pairs.
func cycleList(cycles map[string]string, ids []string) string {
	pairs := make([]string, 0, len(ids))
	for _, id := range ids {
		pairs = append(pairs, id+"="+quoted(cycles[id]))
	}
	return strings.Join(pairs, ", ")
}

// rebindRefusal is the refusal of a rebind_cycles request that does not name
// exactly the members whose cycle changed: the members it omits, the members it
// names without a change, and the members it names with another cycle.
func rebindRefusal(changed, requested map[string]string) error {
	var missing, extra, differs []string
	for _, id := range slices.Sorted(maps.Keys(changed)) {
		if got, ok := requested[id]; !ok {
			missing = append(missing, id)
		} else if got != changed[id] {
			differs = append(differs, fmt.Sprintf("%s: %s, %s", id, shown(got, isDigest), quoted(changed[id])))
		}
	}
	for _, id := range slices.Sorted(maps.Keys(requested)) {
		if _, ok := changed[id]; !ok {
			extra = append(extra, shown(id, isTaskID))
		}
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, t("board.refusal_rebind_missing", cycleList(changed, missing)))
	}
	if len(extra) > 0 {
		parts = append(parts, t("board.refusal_rebind_extra", strings.Join(extra, ", ")))
	}
	if len(differs) > 0 {
		parts = append(parts, t("board.refusal_rebind_differs", strings.Join(differs, "; ")))
	}
	if len(parts) == 0 {
		return kernelRefusal("cycle rebind CAS requires exactly all changed members", "rebind", nil, nil)
	}
	next := t("board.refusal_next_rebind")
	if len(changed) == 0 {
		next = t("board.refusal_next_rebind_none")
	}
	return reviewRefusal("cycle rebind CAS requires exactly all changed members", next, parts...)
}

// dispositionRefusal is the refusal of a disposition whose previous record is not
// the last record of its chain: the entrance has already checked the assignment
// and the status, so the previous record is the only fact that can fail.
func dispositionRefusal(d ReviewDisposition, last string) error {
	return reviewRefusal("disposition predecessor CAS conflict", t("board.refusal_next_disposition"),
		t("board.refusal_disposition_ref", d.RunID, d.FindingID, d.TaskID, shown(d.PreviousRecordID, ValidReviewID), quoted(last)))
}

// memberStateRefusal is the refusal of a review over a member that is not in
// working or review. A member that has not started may start, and a card of a
// group that has not started may also leave the group; neither holds for a card
// that is finished or thrown away.
func memberStateRefusal(id, state, group string) error {
	next := ""
	switch {
	case (state == "backlog" || state == "todo") && group != "":
		next = t("board.refusal_member_start_or_remove", id)
	case state == "backlog" || state == "todo":
		next = t("board.refusal_member_wait", id)
	}
	return reviewRefusal(id+": working/review", next, t("board.refusal_member_state", id, state))
}

// groupCASKey names the message of a request to a group checkpoint that carries
// a stale revision or epoch, or finds no room below the revision ceiling, with
// the values it renders. The checks run in this order, the one `removeCASError`
// has always used.
func groupCASKey(group string, expectedRevision, expectedEpoch uint64, c CoordinatorCheckpoint) (string, []any) {
	switch {
	case expectedRevision != c.Revision:
		return "board.remove_member_stale_revision", []any{group, expectedRevision, c.Revision}
	case expectedEpoch != c.Authority.Epoch:
		return "board.remove_member_stale_epoch", []any{group, expectedEpoch, c.Authority.Epoch}
	}
	return "board.remove_member_revision_limit", []any{group}
}

// recoverCASError is the refusal of a recover request whose revision or epoch is
// stale. Its text is the one of a stale remove-member request under the label.
func recoverCASError(c CoordinatorCheckpoint, r CoordinatorRecover) error {
	key, args := groupCASKey(r.GroupID, r.ExpectedRevision, r.ExpectedEpoch, c)
	return coordinatorError("recover CAS: " + t(key, args...))
}

// claimRefusal is the refusal of a claim: a stale revision or epoch, the token of
// the current authority used again, or no room below the ceiling. The token
// itself is never written.
func claimRefusal(c CoordinatorCheckpoint, exists bool, r CoordinatorClaim) error {
	var parts []string
	cas := false
	if r.ExpectedRevision != c.Revision {
		parts, cas = append(parts, t("board.refusal_group_revision", r.GroupID, r.ExpectedRevision, c.Revision)), true
	}
	if r.ExpectedEpoch != c.Authority.Epoch {
		parts, cas = append(parts, t("board.refusal_group_epoch", r.GroupID, r.ExpectedEpoch, c.Authority.Epoch)), true
	}
	if exists && c.Authority.Token == r.Token {
		parts = append(parts, t("board.refusal_token_reused"))
	}
	if len(parts) == 0 {
		parts = append(parts, t("board.refusal_group_limit", r.GroupID))
	}
	next := ""
	if cas {
		next = t("board.refusal_next_group", r.GroupID)
	}
	return coordinatorRefusal("claim CAS or reused token", next, parts...)
}

// authorityRefusal is the refusal of a reconcile that finds no checkpoint or
// another authority than the checkpoint's. An owner and an epoch are written
// with both values; the token only as different.
func authorityRefusal(c CoordinatorCheckpoint, exists bool, r CoordinatorReconcile) error {
	if !exists {
		return coordinatorRefusal("checkpoint missing", "", t("board.refusal_checkpoint_missing", r.GroupID))
	}
	var parts []string
	if c.Authority.Owner != r.Authority.Owner {
		parts = append(parts, t("board.refusal_authority_owner", shownOwner(r.Authority.Owner, r.Authority.Token, c.Authority.Token), c.Authority.Owner))
	}
	if c.Authority.Epoch != r.Authority.Epoch {
		parts = append(parts, t("board.refusal_authority_epoch", r.Authority.Epoch, c.Authority.Epoch))
	}
	if c.Authority.Token != r.Authority.Token {
		parts = append(parts, t("board.refusal_authority_token"))
	}
	if len(parts) == 0 {
		parts = append(parts, t("board.refusal_state_unknown"))
	}
	return coordinatorRefusal("stale coordinator authority", t("board.refusal_next_group", r.GroupID), parts...)
}

// checkpointRevisionRefusal is the refusal of a reconcile that holds the
// authority but names another checkpoint revision, and is not the retry of the
// last request.
func checkpointRevisionRefusal(c CoordinatorCheckpoint, r CoordinatorReconcile) error {
	return coordinatorRefusal("checkpoint CAS", t("board.refusal_next_group", r.GroupID),
		t("board.refusal_group_revision", r.GroupID, r.ExpectedRevision, c.Revision),
		t("board.refusal_request_replay", coordinatorHash(r), quoted(c.LastRequestHash)))
}

// staleTaskError is the transaction conflict of a request that holds another
// revision, or another state, of a card than the card has. The code stays
// board.transaction_conflict, and the text keeps its retry hint.
func staleTaskError(id string, expectedRevision, revision uint64, expectedState, state string) error {
	var parts []string
	if expectedRevision != revision {
		parts = append(parts, t("board.refusal_task_revision", expectedRevision, revision, id))
	}
	if expectedState != "" {
		parts = append(parts, t("board.refusal_task_state", expectedState, state))
	}
	if len(parts) == 0 {
		return kanbanError("board.transaction_conflict", id)
	}
	return kanbanError("board.transaction_conflict", id+": "+strings.Join(parts, "; "))
}

// moveConflictError is the transaction conflict of a move whose entry went stale
// or whose --expect-revision is not the card's revision.
func moveConflictError(entry Entry, s Snapshot, options MoveOptions) error {
	var parts []string
	if options.ExpectedRevision != nil && s.Revision != *options.ExpectedRevision {
		parts = append(parts, t("board.refusal_task_revision", *options.ExpectedRevision, s.Revision, entry.TaskID))
	}
	if entry.Version != nil && s.Revision != entry.Version.revision && (options.ExpectedRevision == nil || entry.Version.revision != *options.ExpectedRevision) {
		parts = append(parts, t("board.refusal_task_revision", entry.Version.revision, s.Revision, entry.TaskID))
	}
	if s.Entry.State != entry.State {
		parts = append(parts, t("board.refusal_task_state", entry.State, s.Entry.State))
	}
	if len(parts) == 0 {
		return kanbanError("board.transaction_conflict", entry.TaskID)
	}
	return kanbanError("board.transaction_conflict", entry.TaskID+": "+strings.Join(parts, "; "))
}

// dispatchRefusalParts says what a refused dispatch request got wrong. A stale
// dispatch revision, and for an attempt a stale card revision, are named with the
// value of the request and the current one. When both are current, the kernel
// refused the dispatch's own state, and the text gives the state, the
// authorization epoch, and the dispatch and epoch the card binds.
func dispatchRefusalParts(d Dispatch, s Snapshot, expected uint64, cardRevision []uint64) []string {
	var parts []string
	if d.Revision != expected {
		parts = append(parts, t("board.refusal_dispatch_revision", expected, d.Revision))
	}
	if len(cardRevision) > 0 && s.Revision != cardRevision[0] {
		parts = append(parts, t("board.refusal_dispatch_card_revision", cardRevision[0], s.Revision))
	}
	if len(parts) == 0 {
		bound := authFrom(s.Text)
		parts = append(parts, t("board.refusal_dispatch_state", d.State, d.Authorization.Epoch, quoted(bound.DispatchID), bound.Epoch))
	}
	return parts
}

// dispatchRefusal is the dispatch conflict of a request the kernel refused.
func dispatchRefusal(id string, d Dispatch, s Snapshot, expected uint64, cardRevision ...uint64) error {
	return dispatchError(id + ": " + strings.Join(dispatchRefusalParts(d, s, expected, cardRevision), "; "))
}

// wrapUpRefusal is the evidence refusal of a wrap-up grant the kernel refused at
// its first check: a stale dispatch revision, no author or reason, or the
// state of the dispatch when both hold.
func wrapUpRefusal(d Dispatch, s Snapshot, expected uint64, named bool) error {
	var parts []string
	stale := d.Revision != expected
	if stale {
		parts = append(parts, t("board.refusal_dispatch_revision", expected, d.Revision))
	}
	if !named {
		parts = append(parts, t("board.refusal_wrapup_named"))
	}
	if len(parts) == 0 {
		parts = dispatchRefusalParts(d, s, expected, nil)
	}
	text := "wrap-up authority conflict: " + strings.Join(parts, "; ") + ". " + t("board.wrap_up_grant_hint")
	if stale {
		text += " " + t("board.refusal_next_dispatch", d.Input.TaskID, d.Input.ID)
	}
	return dispatchEvidenceError(text)
}
