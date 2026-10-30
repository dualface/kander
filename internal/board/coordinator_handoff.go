package board

import (
	"reflect"
	"strings"
)

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
		if !refs[ref] && !settlementReference(tx, s.Entry.TaskID, ref) {
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

// settlementReference reports a retained reference to a verified settlement.
func settlementReference(tx *Transaction, task string, ref ArtifactReference) bool {
	rest, ok := strings.CutPrefix(ref.Path, "dispatches/")
	id, name, cut := strings.Cut(rest, "/")
	if !ok || !cut || name != "settlement.json" || ref.TaskID != task {
		return false
	}
	d, err := readDispatch(tx, task, id)
	return err == nil && d.Settlement != nil
}
