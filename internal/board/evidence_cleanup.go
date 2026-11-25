package board

import (
	"context"
	"slices"
)

// GroupEvidenceSettlement says whether the Git evidence of a task group has no
// reader left, and which members reached done.
type GroupEvidenceSettlement struct {
	// Settled holds when every member is in done, archived, or trash and, for a
	// group with a checkpoint, every completed wrap-up of a done member has its
	// wrap-up Git evidence record. Reconcile verifies a completed wrap-up in Git
	// once, against the deliveries of every member and the integration source;
	// the record ends that need.
	Settled bool
	// Done lists the members in done, sorted. Only their task evidence is
	// cleaned; archived and trash members keep theirs.
	Done []string
}

// GroupEvidenceSettled reads whether the Git evidence of group can go. A group
// without members, or whose membership cannot be read completely, is not
// settled. A group whose checkpoint archive cleanup removed counts as a group
// without a checkpoint.
func GroupEvidenceSettled(ctx context.Context, root, group string) (GroupEvidenceSettlement, error) {
	var out GroupEvidenceSettlement
	if !ValidTaskGroup(group) {
		return out, coordinatorError("invalid task group: " + group)
	}
	scanned, err := ScanContext(ctx, root)
	if err != nil {
		return out, err
	}
	membership := scanned.GroupMembership()
	if membership.ErrFor(group) != nil {
		return out, nil
	}
	members := membership.Groups[group]
	if len(members) == 0 {
		return out, nil
	}
	for _, id := range members {
		switch scanned.Entries[id].State {
		case "done":
			out.Done = append(out.Done, id)
		case "archived", "trash":
		default:
			return GroupEvidenceSettlement{}, nil
		}
	}
	slices.Sort(out.Done)
	scope := LockScope{Groups: []string{group, dispatchRegistry}, Tasks: out.Done, ReadOnly: true}
	err = WithTransactionContext(ctx, root, scope, func(tx *Transaction) error {
		_, exists, err := readCheckpoint(tx, group)
		if err != nil || !exists {
			out.Settled = err == nil
			return err
		}
		for _, id := range out.Done {
			witnessed, err := wrapUpsWitnessed(tx, group, id)
			if err != nil || !witnessed {
				return err
			}
		}
		out.Settled = true
		return nil
	})
	if err != nil {
		return GroupEvidenceSettlement{}, err
	}
	return out, nil
}

// wrapUpsWitnessed reports whether every completed wrap-up of a done member has
// its wrap-up Git evidence record.
func wrapUpsWitnessed(tx *Transaction, group, id string) (bool, error) {
	s, err := tx.Snapshot(id)
	if err != nil {
		return false, err
	}
	rounds, err := dispatchRounds(tx, s)
	if err != nil {
		return false, err
	}
	for _, d := range rounds {
		if d.Input.Kind != "wrap-up" || d.State != DispatchCompleted || d.Input.Evidence.WrapUp == nil {
			continue
		}
		recorded, err := readWrapUpEvidence(tx, group, d)
		if err != nil || !recorded {
			return false, err
		}
	}
	return true, nil
}
