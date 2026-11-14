package board

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/statemachine/executionledger"
)

type claimRecord struct {
	Kind    string `json:"kind"`
	ClaimID string `json:"claim_id"`
}

// NewClaimMetadata adds a fresh execution identity without changing published
// metadata field names or the display timestamp. Rollback restores the old body;
// takeover preserves it. Both launch and manual claim use this producer.
func NewClaimMetadata(text string) (string, error) {
	id, err := operationID()
	if err != nil {
		return "", err
	}
	return appendLifecycleRecord(text, "LIFECYCLE_DECISION", lifecycleJSON(claimRecord{Kind: "claim", ClaimID: id})), nil
}

func claimRecords(text string) []claimRecord {
	body, _ := SectionBody(text, "LIFECYCLE_DECISION")
	var records []claimRecord
	for _, line := range strings.Split(body, "\n") {
		var r claimRecord
		if json.Unmarshal([]byte(line), &r) == nil && r.Kind == "claim" {
			records = append(records, r)
		}
	}
	return records
}

func claimIdentity(text string) string {
	records := claimRecords(text)
	if len(records) == 0 {
		return ""
	}
	return records[len(records)-1].ClaimID
}

func claimLines(text string) int { return len(claimRecords(text)) }

type lifecycleHandoff struct {
	Kind         string `json:"kind"`
	OldCycle     string `json:"old_cycle"`
	NewCycle     string `json:"new_cycle"`
	DispatchID   string `json:"dispatch_id"`
	Decision     string `json:"decision_reference"`
	Owner        string `json:"owner"`
	CardRevision uint64 `json:"card_revision"`
}

func stageReclaim(tx *Transaction, s Snapshot, target, text string, o MoveOptions) (string, *lifecycleHandoff, error) {
	if o.Owner == "" || target != "working" {
		return text, nil, nil
	}
	releases, err := cardReleases(s)
	if err != nil {
		return "", nil, err
	}
	var released *DispatchRelease
	for i := range releases {
		if releases[i].Cycle == planCycle(s) {
			released = &releases[i]
		}
	}
	l, err := readExecutionLedger(tx, s)
	if err != nil {
		return "", nil, err
	}
	decided := strings.TrimSpace(o.Decision) != ""
	if s.Entry.State != "review" {
		// The previous record is the start entrance's to judge; this decision
		// covers the decision reference and the release only.
		var refusal error
		switch {
		case released != nil:
			refusal = kanbanError("board.transaction_decision_required")
		case o.Decision != "":
			refusal = kanbanError("board.transaction_invalid", "--decision")
		}
		accepted, err := ledgerStep(s, l, func(k *ledgerKernel) executionledger.Event {
			return executionledger.Claim{At: k.at(s.Revision), Owner: true, Decision: o.Decision != "", Released: released != nil, Chain: true}
		})
		if err != nil {
			return "", nil, err
		}
		if !accepted && refusal != nil {
			return "", nil, refusal
		}
		return text, nil, nil
	}
	handoff := lifecycleHandoff{Kind: "reclaim", OldCycle: planCycle(s), NewCycle: planCycle(Snapshot{Entry: s.Entry, Text: text}), Decision: o.Decision, Owner: o.Owner, CardRevision: s.Revision + 1}
	var refusal error
	if !decided {
		refusal = kanbanError("board.transaction_decision_required")
	} else if released != nil {
		if refusal = verifyCardRelease(tx, s, *released); refusal == nil {
			handoff.DispatchID = released.Termination.Authorization.DispatchID
		}
	} else {
		refusal = verifyDispatchlessHandoff(tx, s, releases, handoff)
	}
	accepted, err := ledgerStep(s, l, func(k *ledgerKernel) executionledger.Event {
		return executionledger.Reclaim{At: k.at(s.Revision), Decision: decided, Proof: decided && refusal == nil}
	})
	if err != nil {
		return "", nil, err
	}
	if !accepted {
		return "", nil, firstError(refusal, kanbanError("board.transaction_decision_required"))
	}
	return appendLifecycleRecord(text, "LIFECYCLE_DECISION", lifecycleJSON(handoff)), &handoff, nil
}

// cycleReleased reports whether a dispatch release ended a binding in the
// card's current cycle.
func cycleReleased(s Snapshot) (bool, error) {
	releases, err := cardReleases(s)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(releases, func(r DispatchRelease) bool { return r.Cycle == planCycle(s) }), nil
}

// verifyDispatchlessHandoff proves that the old cycle never held a dispatch.
// Only a release clears a binding, and a bound card refuses --owner, so a cycle
// that held one has a release of its own. A completed dispatch of an earlier
// cycle passes only when a release ended its binding before this handoff. A
// settlement ends a completed binding the way a release ends any other.
func verifyDispatchlessHandoff(tx *Transaction, s Snapshot, releases []DispatchRelease, h lifecycleHandoff) error {
	for _, r := range releases {
		if r.Cycle == h.OldCycle {
			return coordinatorError("dispatchless handoff hides a dispatch release")
		}
	}
	entries, err := fs.ListDirectory(tx.root, filepath.Join(s.Entry.Path, "dispatches"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		d, err := readDispatch(tx, s.Entry.TaskID, entry.Name)
		if err != nil {
			return err
		}
		receipt := cmp.Or(d.Completed, d.Accepted)
		if d.Release != nil || d.Settlement != nil || receipt == nil || receipt.CardRevision >= h.CardRevision {
			continue
		}
		ended := slices.ContainsFunc(releases, func(r DispatchRelease) bool {
			return r.CardRevision > receipt.CardRevision && r.CardRevision < h.CardRevision
		})
		if !ended {
			return coordinatorError("dispatchless handoff hides a dispatch binding")
		}
	}
	return nil
}

// lifecycleHandoffs consumes only producer-owned records. Each link must bind a
// verified release, or prove its old cycle never held a dispatch, and advance
// both cycle and revision; old owners remain history. unrecorded holds the marked
// links of the synthesized prefix, the only cycle changes a handoff need not record.
func lifecycleHandoffs(tx *Transaction, s Snapshot, from string, unrecorded []ExecutionTransition) ([]lifecycleHandoff, error) {
	if from == planCycle(s) {
		return nil, nil
	}
	releases, err := cardReleases(s)
	if err != nil {
		return nil, err
	}
	crossUnrecorded := func(cycle string) string {
		at := slices.IndexFunc(unrecorded, func(t ExecutionTransition) bool { return t.FromCycle == cycle })
		if at < 0 {
			return cycle
		}
		return unrecorded[at].ToCycle
	}
	body, _ := SectionBody(s.Text, "LIFECYCLE_DECISION")
	cycle := crossUnrecorded(from)
	var result []lifecycleHandoff
	var revision uint64
	for _, line := range strings.Split(body, "\n") {
		var h lifecycleHandoff
		if json.Unmarshal([]byte(line), &h) != nil || h.Kind != "reclaim" || h.OldCycle != cycle {
			continue
		}
		if h.NewCycle == h.OldCycle || h.NewCycle == "" || strings.TrimSpace(h.Decision) == "" || h.Owner == "" || h.CardRevision > s.Revision || h.CardRevision <= revision {
			return nil, coordinatorError("invalid lifecycle handoff")
		}
		if h.DispatchID == "" {
			if err := verifyDispatchlessHandoff(tx, s, releases, h); err != nil {
				return nil, err
			}
		} else {
			found := false
			for _, r := range releases {
				if r.Cycle == h.OldCycle && r.Termination.Authorization.DispatchID == h.DispatchID && r.CardRevision < h.CardRevision {
					if err := verifyCardRelease(tx, s, r); err != nil {
						return nil, err
					}
					found = true
					break
				}
			}
			if !found {
				return nil, coordinatorError("handoff lacks dispatch release original")
			}
		}
		result = append(result, h)
		cycle, revision = crossUnrecorded(h.NewCycle), h.CardRevision
	}
	if cycle != planCycle(s) {
		return nil, missingHandoffError()
	}
	return result, nil
}

func lifecycleJSON(value any) string { b, _ := json.Marshal(value); return string(b) }

func appendLifecycleRecord(text, section, record string) string {
	if _, exists := SectionBody(text, section); exists {
		return appendRecordSection(text, section, record)
	}
	at := strings.Index(text, "\n## ")
	if at < 0 {
		return appendRecordSection(text, section, record)
	}
	return text[:at] + "\n## " + section + "\n\n" + record + "\n" + text[at:]
}
