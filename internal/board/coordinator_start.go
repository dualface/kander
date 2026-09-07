package board

import "context"

func coordinatorStartCycle(ctx context.Context, tx *Transaction, id string, old CoordinatorMember, s Snapshot) (string, bool, string, error) {
	a, exists, err := readTaskStart(tx, id, "")
	if err != nil {
		return "", false, "", err
	}
	if !exists {
		if old.StartAttempt != "" {
			return "", false, "", coordinatorError("start attempt missing")
		}
		cycle, awaiting, err := coordinatorMemberCycle(id, old, s, false)
		return cycle, awaiting, "", err
	}
	if err := coordinatorStartLineage(ctx, tx, id, old, a); err != nil {
		return "", false, "", err
	}
	empty := ReviewDigest([]byte(id + "\n"))
	cycle := planCycle(s)
	if s.Revision < a.Revision {
		return "", false, "", coordinatorError("start revision regressed")
	}
	switch a.Status {
	case "pending":
		if cycle != a.Cycle || MetadataFrom(s.Text, FieldOwner) != a.Owner || MetadataFrom(s.Text, FieldSession) != a.Session || old.Cycle != empty && old.Cycle != a.Cycle || old.DeliveryCommit != "" || old.Dispatch != nil {
			return "", false, "", coordinatorError("pending start facts changed")
		}
		// Metadata is only an attempt. Even a claim made while the launcher is
		// waiting must retain the unbound cycle until a durable result exists.
		return empty, true, a.ID, nil
	case "rolled-back":
		if s.Revision < a.FinishedRevision {
			return "", false, "", coordinatorError("start rollback revision")
		}
		if old.Cycle != empty {
			// A later explicit move --owner can establish a manual execution
			// after rollback. Its already observed cycle must remain unchanged.
			if old.StartAttempt == a.ID && !old.AwaitingStart && old.Revision > a.FinishedRevision && old.Cycle == cycle {
				return cycle, false, a.ID, nil
			}
			// A formerly premature cursor can be revoked only by the exact
			// producer rollback covering its observed revision and cycle.
			if old.Cycle != a.Cycle || old.Revision < a.Revision || old.Revision >= a.FinishedRevision || old.Review != nil || old.Dispatch != nil || old.DeliveryCommit != "" {
				return "", false, "", coordinatorError("rollback cannot replace confirmed cycle")
			}
			old.Cycle, old.AwaitingStart = empty, true
		}
		if cycle == empty {
			return empty, true, a.ID, nil
		}
		if s.Revision == a.FinishedRevision {
			return "", false, "", coordinatorError("rollback metadata not restored")
		}
	case "succeeded":
		if cycle != a.Cycle || s.Revision < a.FinishedRevision {
			return "", false, "", coordinatorError("confirmed start cycle changed")
		}
	}
	cycle, awaiting, err := coordinatorMemberCycle(id, old, s, a.Status == "succeeded")
	return cycle, awaiting, a.ID, err
}

func coordinatorStartLineage(ctx context.Context, tx *Transaction, id string, old CoordinatorMember, latest taskStart) error {
	if old.StartAttempt != "" && old.StartAttempt != latest.ID && !old.AwaitingStart {
		return coordinatorError("confirmed start attempt replaced")
	}
	found := old.StartAttempt == ""
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if seen[latest.ID] {
			return coordinatorError("start lineage cycle")
		}
		seen[latest.ID] = true
		if latest.ID == old.StartAttempt {
			found = true
		}
		if latest.PreviousID == "" {
			break
		}
		previous, exists, err := readTaskStart(tx, id, latest.PreviousID)
		if err != nil {
			return err
		}
		if !exists || previous.Status != "rolled-back" || previous.FinishedRevision >= latest.Revision {
			return coordinatorError("start retry lacks rollback original")
		}
		latest = previous
	}
	if !found {
		return coordinatorError("observed start missing from retry lineage")
	}
	return nil
}
