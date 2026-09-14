package board

import "reflect"

// Retained release and handoff cursors never substitute for their originals on
// replay. Check every referenced release, including after Dispatch became nil.
func verifyCoordinatorHandoffs(tx *Transaction, s Snapshot, old CoordinatorMember) error {
	releases, err := cardReleases(s)
	if err != nil {
		return err
	}
	refs := map[ArtifactReference]bool{}
	for _, r := range releases {
		if err := verifyCardRelease(tx, s, r); err != nil {
			return err
		}
		refs[ArtifactReference{s.Entry.TaskID, dispatchPath(r.Termination.Authorization.DispatchID, "release")}] = true
	}
	for _, ref := range old.ReleasedDispatches {
		if !refs[ref] {
			return coordinatorError("retained release original missing")
		}
	}
	if len(old.Handoffs) > 0 {
		chain, err := lifecycleHandoffs(tx, s, old.Handoffs[0].OldCycle)
		if err != nil {
			return err
		}
		if len(chain) < len(old.Handoffs) || !reflect.DeepEqual(chain[:len(old.Handoffs)], old.Handoffs) {
			return coordinatorError("retained lifecycle handoff changed")
		}
	}
	return nil
}
