package board

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/statemachine/cardlifecycle"
)

const removalSchema = 1

// CoordinatorRemoveMember asks to take one unstarted member out of a task group
// that has a checkpoint. Every field is required: the two expected values bind the
// request to the checkpoint the decision was made on, and the decision reference
// is the audit basis.
type CoordinatorRemoveMember struct {
	GroupID          string `json:"group_id"`
	Member           string `json:"member"`
	ExpectedRevision uint64 `json:"expected_revision"`
	ExpectedEpoch    uint64 `json:"expected_epoch"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
}

// CoordinatorRemoval is the immutable record of one removal. It binds the
// checkpoint revision and the coordinator epoch it was decided on, so an old
// record or request cannot apply to a later checkpoint.
type CoordinatorRemoval struct {
	Schema             int    `json:"schema"`
	RemovalID          string `json:"removal_id"`
	GroupID            string `json:"group_id"`
	TaskID             string `json:"task_id"`
	CheckpointRevision uint64 `json:"checkpoint_revision"`
	CoordinatorEpoch   uint64 `json:"coordinator_epoch"`
	Decision           string `json:"decision_reference"`
	Reason             string `json:"reason"`
	FromTaskGroup      string `json:"from_task_group"`
	RecordedAt         string `json:"recorded_at"`
}

func removalName(id string) string { return "removals/" + id + ".json" }

// verifyCoordinatorRemovals requires every listed removal to equal its immutable
// record, and the removed card to be out of the member set.
func verifyCoordinatorRemovals(tx *Transaction, c CoordinatorCheckpoint) error {
	for _, r := range c.Removals {
		var stored CoordinatorRemoval
		b, ok, err := tx.ReadGroup(c.GroupID, removalName(r.RemovalID))
		if err != nil {
			return err
		}
		if !ok {
			return coordinatorError("removal record missing: " + r.RemovalID)
		}
		if err = DecodeReviewJSON(b, &stored); err != nil {
			return err
		}
		if !reflect.DeepEqual(stored, r) || r.Schema != removalSchema || r.GroupID != c.GroupID {
			return coordinatorError("removal record changed: " + r.RemovalID)
		}
		if _, member := c.Members[r.TaskID]; member {
			return coordinatorError("removed card is still a member: " + r.TaskID)
		}
	}
	return nil
}

// removeFacts are the facts of a removal, true as the shell found them. A fact
// the shell did not reach because an earlier one failed stays true, so the kernel
// refusal is attributed to the first failing fact.
type removeFacts struct {
	known, unstarted, membership, remaining, noDependents bool
	blockedAfter                                          int
}

// RemoveCoordinatorMemberWithTransaction takes one unstarted member out of the
// checkpointed group: the checkpoint drops it, the card becomes a standalone card
// with an audit record of the decision, and an immutable removal record is
// written. The request is refused as a whole, with nothing written, when any
// condition fails.
func RemoveCoordinatorMemberWithTransaction(ctx context.Context, root string, r CoordinatorRemoveMember, extensions ...TransactionExtension) (c CoordinatorCheckpoint, err error) {
	if !taskGroupRe.MatchString(r.GroupID) || r.ExpectedRevision == 0 || r.ExpectedEpoch == 0 || strings.TrimSpace(r.Decision) == "" || strings.TrimSpace(r.Reason) == "" {
		return c, kanbanError("board.remove_member_request_invalid")
	}
	id, err := NormalizeTaskID(r.Member)
	if err != nil {
		return c, err
	}
	scope := LockScope{Groups: []string{r.GroupID, reviewControlGroup, dispatchRegistry, taskStartGroup}, Tasks: []string{id}, ExclusiveBoard: true}
	err = WithTransactionContext(ctx, root, scope, func(tx *Transaction) error {
		defer func() { tx.SetOperationResult(c) }()
		if err := ctx.Err(); err != nil {
			return err
		}
		var exists bool
		var e error
		if c, exists, e = readCheckpoint(tx, r.GroupID); e != nil {
			return e
		}
		g := newGroupKernel(c, exists, len(c.Members))
		facts, refusal := removeFacts{known: true, unstarted: true, membership: true, remaining: true, noDependents: true, blockedAfter: int(g.state.Blocked)}, error(nil)
		if exists {
			if e = verifyCoordinatorRemovals(tx, c); e != nil {
				return e
			}
			if facts, refusal, e = collectRemoveFacts(tx, c, r, id); e != nil {
				return e
			}
		}
		next, ok := g.step(g.removeMember(r, facts))
		if !ok {
			if !exists {
				return coordinatorError("checkpoint missing")
			}
			// The request CAS and the revision room are the kernel's alone: asked
			// with every fact true, the refusal is theirs.
			if _, cas := g.step(g.removeMember(r, removeFacts{known: true, unstarted: true, membership: true, remaining: true, noDependents: true, blockedAfter: int(g.state.Blocked)})); !cas {
				return removeCASError(c, r)
			}
			return firstError(refusal, coordinatorError("remove refused"))
		}
		s, e := tx.Snapshot(id)
		if e != nil {
			return e
		}
		from := TaskGroupFrom(s.Text)
		if e = stageRemoveMemberCard(tx, s, r); e != nil {
			return e
		}
		record := CoordinatorRemoval{Schema: removalSchema, RemovalID: tx.record.ID, GroupID: r.GroupID, TaskID: id, CheckpointRevision: c.Revision,
			CoordinatorEpoch: c.Authority.Epoch, Decision: r.Decision, Reason: r.Reason, FromTaskGroup: from, RecordedAt: nowStamp()}
		delete(c.Members, id)
		c.Removals = append(c.Removals, record)
		c.Revision = g.revOut(next.Rev)
		c.Schema, c.LastRequestHash, c.MemberVersion = checkpointSchema, "", coordinatorHash(checkpointIDs(c))
		// No claim request can hash to a removal record, so an earlier claim
		// replayed as it was never reads as a replay of the new member set.
		c.ClaimHash = coordinatorHash(record)
		if e = tx.PutGroup(r.GroupID, removalName(record.RemovalID), reviewJSON(record)); e != nil {
			return e
		}
		return putCheckpoint(tx, &c)
	}, extensions...)
	return c, err
}

// removeCASError names what the refused request got wrong: the revision, the
// epoch, or the room below the revision ceiling.
func removeCASError(c CoordinatorCheckpoint, r CoordinatorRemoveMember) error {
	switch {
	case r.ExpectedRevision != c.Revision:
		return kanbanError("board.remove_member_stale_revision", r.GroupID, r.ExpectedRevision, c.Revision)
	case r.ExpectedEpoch != c.Authority.Epoch:
		return kanbanError("board.remove_member_stale_epoch", r.GroupID, r.ExpectedEpoch, c.Authority.Epoch)
	}
	return kanbanError("board.remove_member_revision_limit", r.GroupID)
}

// collectRemoveFacts reads the facts of a removal in the order the refusals are
// reported, and returns the error of the first one that fails. A fact that cannot
// be read is an error of its own, not a refusal.
func collectRemoveFacts(tx *Transaction, c CoordinatorCheckpoint, r CoordinatorRemoveMember, id string) (f removeFacts, refusal error, err error) {
	f = removeFacts{known: true, unstarted: true, membership: true, remaining: true, noDependents: true, blockedAfter: len(c.BlockedMembers())}
	fail := func(fact *bool, e error) {
		if refusal == nil {
			refusal = e
		}
		*fact = false
	}
	old, listed := c.Members[id]
	if !listed {
		fail(&f.known, kanbanError("board.remove_member_unknown", id, r.GroupID))
		return f, refusal, nil
	}
	if old.Blocked != nil {
		f.blockedAfter--
	}
	s, err := tx.Snapshot(id)
	if err != nil {
		return f, refusal, err
	}
	if group := TaskGroupFrom(s.Text); group != r.GroupID {
		fail(&f.membership, kanbanError("board.remove_member_group_mismatch", id, r.GroupID, group))
	}
	reason, err := startedReason(tx, s, old)
	if err != nil {
		return f, refusal, err
	}
	if reason != "" {
		fail(&f.unstarted, kanbanError("board.remove_member_started", id, reason))
	}
	if len(c.Members) < 2 {
		fail(&f.remaining, kanbanError("board.remove_member_last", id, r.GroupID))
	}
	dependents, err := removalDependents(tx, c, id)
	if err != nil {
		return f, refusal, err
	}
	if len(dependents) > 0 {
		fail(&f.noDependents, kanbanError("board.remove_member_dependents", id, strings.Join(dependents, ", ")))
	}
	return f, refusal, nil
}

// startedReason says why a member counts as started, or returns "" for an
// unstarted one: it waits in backlog or todo, its mirror awaits the start, and no
// execution record, start attempt, dispatch, review plan, review run, or member
// exclusion exists for it. The server claim is the service's to check.
func startedReason(tx *Transaction, s Snapshot, m CoordinatorMember) (string, error) {
	switch {
	case s.Entry.State != "backlog" && s.Entry.State != "todo":
		return t("board.remove_member_reason_state", s.Entry.State), nil
	case MetadataFrom(s.Text, FieldStartedAt) != "":
		return t("board.remove_member_reason_started_at"), nil
	case !m.AwaitingStart || m.Dispatch != nil || m.Review != nil:
		return t("board.remove_member_reason_mirror"), nil
	case authFrom(s.Text) != (ExecutionAuthorization{}):
		return t("board.remove_member_reason_dispatch"), nil
	}
	l, err := readExecutionLedger(tx, s)
	if err != nil {
		return "", err
	}
	if l.current != nil || len(l.chain) > 0 {
		return t("board.remove_member_reason_execution"), nil
	}
	rounds, err := dispatchRounds(tx, s)
	if err != nil {
		return "", err
	}
	if len(rounds) > 0 {
		return t("board.remove_member_reason_rounds"), nil
	}
	if reviewed, err := memberReviewed(tx, s); err != nil {
		return "", err
	} else if reviewed {
		return t("board.remove_member_reason_reviews"), nil
	}
	if _, planned, err := readTaskPlan(tx, s.Entry.TaskID); err != nil {
		return "", err
	} else if planned {
		return t("board.remove_member_reason_plan"), nil
	}
	if excluded, err := memberExcluded(tx, s.Entry.TaskID); err != nil {
		return "", err
	} else if excluded {
		return t("board.remove_member_reason_excluded"), nil
	}
	return "", nil
}

// memberReviewed says whether a review run names the card: the index the
// publication wrote into its REVIEWS section, or a persisted run of the review
// control group that lists it as a member, published or not.
func memberReviewed(tx *Transaction, s Snapshot) (bool, error) {
	indexes, err := ParseReviewIndexes(s.Text)
	if err != nil || len(indexes) > 0 {
		return len(indexes) > 0, err
	}
	entries, err := fs.ListDirectory(tx.root, control(tx.root, "groups", reviewControlGroup, "runs"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		var run ReviewRun
		if ok, err := readReviewJSON(tx, reviewRunName(entry.Name), &run); err != nil {
			return false, err
		} else if ok && containsID(run.TaskIDs, s.Entry.TaskID) {
			return true, nil
		}
	}
	return false, nil
}

// removalDependents lists the remaining members whose PREREQUISITES name the
// removed card. Only a card ID written there counts: a group reference never
// names the group the card is in. The removed card's own dependencies are not read.
func removalDependents(tx *Transaction, c CoordinatorCheckpoint, id string) ([]string, error) {
	b, err := scan(tx.root)
	if err != nil {
		return nil, err
	}
	dependents := []string{}
	for _, other := range checkpointIDs(c) {
		if other == id {
			continue
		}
		entry, ok := b.Entries[other]
		if !ok {
			return nil, coordinatorError("unknown or missing member")
		}
		text, err := readCardText(entry)
		if err != nil {
			return nil, err
		}
		prerequisites, err := prerequisiteIDsFrom(text, other)
		if err != nil {
			return nil, err
		}
		if slices.Contains(prerequisites, id) {
			dependents = append(dependents, other)
		}
	}
	return dependents, nil
}

var legacyGroupLineRe = regexp.MustCompile(`(?m)^` + FieldTaskGroup + `:[^\n]*\n?`)

// clearTaskGroup empties the group of a card. A group an older card wrote into
// DISCUSSION is removed too, or the empty field would read as that group.
func clearTaskGroup(text string) (string, error) {
	text, err := setMetadata(text, FieldTaskGroup, "")
	if err != nil {
		return "", err
	}
	at := regexp.MustCompile(`(?m)^## ` + SectionDiscussion + `[ \t\r]*$`).FindStringIndex(text)
	if at == nil {
		return text, nil
	}
	end := len(text)
	if next := headingRe.FindStringIndex(text[at[1]:]); next != nil {
		end = at[1] + next[0]
	}
	return text[:at[1]] + legacyGroupLineRe.ReplaceAllString(text[at[1]:end], "") + text[end:], nil
}

// stageRemoveMemberCard empties the TASK_GROUP of the removed card and records the
// decision in its protected CONTRACT_DECISIONS section, the way an update with a
// decision file does. The card lifecycle kernel accepts the contract change on a
// request that carries a decision.
func stageRemoveMemberCard(tx *Transaction, s Snapshot, r CoordinatorRemoveMember) error {
	id := s.Entry.TaskID
	crlf := strings.Contains(s.Text, "\r\n")
	old := lfLineEndings(s.Text)
	text, err := clearTaskGroup(old)
	if err != nil {
		return err
	}
	if TaskGroupFrom(text) != "" {
		return kanbanError("board.transaction_invalid", FieldTaskGroup)
	}
	k, err := projectCard(tx, s)
	if err != nil {
		return err
	}
	before, after := frozenContract(old), frozenContract(text)
	if _, ok := lifecycleStep(k, cardlifecycle.Update{Change: contractChange(before, after), Decision: true}); !ok {
		return kanbanError("board.archived_final", id)
	}
	text = appendRecordSection(text, "CONTRACT_DECISIONS", contractDecisionRecord(r.Decision, before, after))
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return tx.Put(id, "spec.md", text)
}

// contractDecisionRecord renders one CONTRACT_DECISIONS entry: the decision and
// the frozen fields before and after.
func contractDecisionRecord(decision string, before, after map[string]string) string {
	record := struct {
		At            string `json:"at"`
		Decision      string `json:"decision"`
		Before, After map[string]string
	}{nowStamp(), decision, before, after}
	b, _ := json.MarshalIndent(record, "", "  ")
	return "```json\n" + string(b) + "\n```"
}
