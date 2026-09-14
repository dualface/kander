package board

import (
	"encoding/json"
	"strings"
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

func claimIdentity(text string) string {
	body, _ := SectionBody(text, "LIFECYCLE_DECISION")
	id := ""
	for _, line := range strings.Split(body, "\n") {
		var r claimRecord
		if json.Unmarshal([]byte(line), &r) == nil && r.Kind == "claim" {
			id = r.ClaimID
		}
	}
	return id
}

type lifecycleHandoff struct {
	Kind         string `json:"kind"`
	OldCycle     string `json:"old_cycle"`
	NewCycle     string `json:"new_cycle"`
	DispatchID   string `json:"dispatch_id"`
	Decision     string `json:"decision_reference"`
	Owner        string `json:"owner"`
	CardRevision uint64 `json:"card_revision"`
}

func stageReclaim(tx *Transaction, s Snapshot, target, text string, o MoveOptions) (string, error) {
	if o.Owner == "" || target != "working" {
		return text, nil
	}
	releases, err := cardReleases(s)
	if err != nil {
		return "", err
	}
	var released *DispatchRelease
	for i := range releases {
		if releases[i].Cycle == planCycle(s) {
			released = &releases[i]
		}
	}
	if released == nil {
		if o.Decision != "" {
			return "", kanbanError("board.transaction_invalid", "--decision")
		}
		return text, nil
	}
	if s.Entry.State != "review" || strings.TrimSpace(o.Decision) == "" {
		return "", kanbanError("board.transaction_decision_required")
	}
	if err := verifyCardRelease(tx, s, *released); err != nil {
		return "", err
	}
	handoff := lifecycleHandoff{Kind: "reclaim", OldCycle: planCycle(s), NewCycle: planCycle(Snapshot{Entry: s.Entry, Text: text}), DispatchID: released.Termination.Authorization.DispatchID, Decision: o.Decision, Owner: o.Owner, CardRevision: s.Revision + 1}
	return appendLifecycleRecord(text, "LIFECYCLE_DECISION", lifecycleJSON(handoff)), nil
}

// lifecycleHandoffs consumes only producer-owned records. Each link must bind a
// verified release and advance both cycle and revision; old owners remain history.
func lifecycleHandoffs(tx *Transaction, s Snapshot, from string) ([]lifecycleHandoff, error) {
	if from == planCycle(s) {
		return nil, nil
	}
	releases, err := cardReleases(s)
	if err != nil {
		return nil, err
	}
	body, _ := SectionBody(s.Text, "LIFECYCLE_DECISION")
	cycle := from
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
		result = append(result, h)
		cycle, revision = h.NewCycle, h.CardRevision
	}
	if cycle != planCycle(s) {
		return nil, coordinatorError("execution cycle changed without lifecycle handoff")
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
