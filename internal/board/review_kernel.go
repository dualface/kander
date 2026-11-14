package board

import (
	"slices"

	"github.com/dualface/kander/internal/statemachine/reviewobligation"
	"github.com/dualface/kander/internal/statemachine/reviewplan"
)

// The review obligation and review plan decisions are made by the generated
// kernels in internal/statemachine (formal/KanderFormal/Machines). This file
// projects the persisted records into kernel states and runs kernel events.
// See docs/formal-verification.md.

// kernelRoles maps the pinned production roles to the kernel role enum.
var kernelRoles = [reviewobligation.RoleCount]string{reviewobligation.RolePmqa: "PMQA", reviewobligation.RoleSecurity: "Security"}

func kernelRole(role string) (reviewobligation.Role, error) {
	at := slices.Index(kernelRoles[:], PinRole(role))
	if at < 0 {
		return 0, reviewError("unknown review role: " + role)
	}
	return reviewobligation.Role(at), nil
}

func kernelRoleSet(roles []string) (reviewobligation.RoleSet, error) {
	var set reviewobligation.RoleSet
	for _, role := range roles {
		r, err := kernelRole(role)
		if err != nil {
			return 0, err
		}
		set = set.With(r)
	}
	return set, nil
}

// roleNames lists the production names of a kernel role set, sorted.
func roleNames(set reviewobligation.RoleSet) []string {
	names := []string{}
	for r := reviewobligation.Role(0); r < reviewobligation.RoleCount; r++ {
		if set.Has(r) {
			names = append(names, kernelRoles[r])
		}
	}
	return names
}

// batchRequiredSet is the set of roles a batch requires.
func batchRequiredSet(b ReviewPlanBatch) (reviewobligation.RoleSet, error) {
	var set reviewobligation.RoleSet
	for role, requirement := range b.Requirements {
		if requirement != "required" {
			continue
		}
		r, err := kernelRole(role)
		if err != nil {
			return 0, err
		}
		set = set.With(r)
	}
	return set, nil
}

// kernelBound is the batch bound of the kernels of one operation on plan p: one
// above its length, so the operation can append a batch. The same bound serves
// the projections before and after the operation.
func kernelBound(p ReviewPlan) uint32 { return uint32(len(p.Batches) + 1) }

// errKernelState reports a projected state on which a kernel query is
// undefined, which a state reached by accepted events never is.
func errKernelState(id string) error {
	return reviewError("review obligation state out of range: " + id)
}

// projectObligation is the obligation kernel state of member id under plan p.
// With planned false the member has no plan and p is ignored.
func projectObligation(tx *Transaction, p ReviewPlan, planned bool, id string, bound uint32) (reviewobligation.State, error) {
	params := reviewobligation.Params{MaxBatch: bound}
	st := reviewobligation.Initial(params)
	s, err := tx.Snapshot(id)
	if err != nil {
		return st, err
	}
	excluded, err := memberExcluded(tx, id)
	if err != nil {
		return st, err
	}
	st.Done = excluded || s.Entry.State != "working" && s.Entry.State != "review"
	if planned {
		if len(p.Batches) >= int(bound) {
			return st, errKernelState(id)
		}
		cycle := planCycle(s)
		st.Planned, st.Bound, st.Count = true, p.Cycles[id] == cycle, uint64(len(p.Batches))
		for i, b := range p.Batches {
			st.Has[i] = containsID(b.TaskIDs, id)
			if st.Req[i], err = batchRequiredSet(b); err != nil {
				return st, err
			}
			var c ReviewClosure
			if st.Closed[i], err = readReviewJSON(tx, closureName(b.BatchID), &c); err != nil {
				return st, err
			}
			if st.Cur[i], err = currentRoles(b, c, st.Closed[i], id, cycle); err != nil {
				return st, err
			}
		}
		if from, ok := p.CycleBatchFrom[id]; ok {
			st.CycNeed, st.CycFrom = true, uint64(from)
		}
		for _, o := range p.DropObligations {
			if !containsID(o.TaskIDs, id) {
				continue
			}
			for _, role := range o.Roles {
				r, err := kernelRole(role)
				if err != nil {
					return st, err
				}
				st.DropRec = st.DropRec.With(r)
				st.DropFloor[r] = max(st.DropFloor[r], uint64(o.FromBatch))
			}
		}
		if st.DroppedRoles, err = kernelRoleSet(p.DroppedRoles[id]); err != nil {
			return st, err
		}
	}
	var o RecoveryReview
	exists, err := readReviewJSON(tx, recoveryReviewName(id), &o)
	if err != nil || !exists {
		return st, err
	}
	if o.Schema != 1 || o.TaskID != id || o.PlanID != "" && len(o.Roles) == 0 {
		return st, reviewError("invalid recovery review: " + id)
	}
	if st.RecRoles, err = kernelRoleSet(o.Roles); err != nil {
		return st, err
	}
	st.Rec, st.RecReleased = reviewobligation.RecoveryNoPlan, o.ReleaseDecision != ""
	if o.PlanID != "" {
		st.Rec = reviewobligation.RecoveryPlanned
		// A record of another plan counts every batch of this one.
		if o.PlanID == p.PlanID {
			st.RecFloor = uint64(o.FromBatch)
		}
	}
	return st, nil
}

// currentRoles lists the roles of closed batch b whose conclusion run reviewed
// cycle of member id. A run with no cycle binding never counts.
func currentRoles(b ReviewPlanBatch, c ReviewClosure, closed bool, id, cycle string) (reviewobligation.RoleSet, error) {
	var set reviewobligation.RoleSet
	if !closed {
		return set, nil
	}
	for role, requirement := range b.Requirements {
		if requirement != "required" {
			continue
		}
		if !closedInCycle(c, role, id, cycle) {
			continue
		}
		r, err := kernelRole(role)
		if err != nil {
			return 0, err
		}
		set = set.With(r)
	}
	return set, nil
}

// memberKernel is one member's obligation kernel within one operation.
type memberKernel struct {
	id     string
	state  reviewobligation.State
	params reviewobligation.Params
}

func loadMemberKernel(tx *Transaction, p ReviewPlan, planned bool, id string, bound uint32) (*memberKernel, error) {
	st, err := projectObligation(tx, p, planned, id, bound)
	if err != nil {
		return nil, err
	}
	return &memberKernel{id: id, state: st, params: reviewobligation.Params{MaxBatch: bound}}, nil
}

// step applies e and reports whether the kernel accepted it.
func (k *memberKernel) step(e reviewobligation.Event) bool {
	next, ok := reviewobligation.Step(k.state, e, k.params)
	k.state = next
	return ok
}

// query reads a kernel query, failing on a state where it is undefined.
func queryKernel[T any](k *memberKernel, q func(reviewobligation.State, reviewobligation.Params) (T, bool)) (T, error) {
	v, ok := q(k.state, k.params)
	if !ok {
		return v, errKernelState(k.id)
	}
	return v, nil
}

// planKernel is the plan kernel of plan p within one operation.
type planKernel struct {
	state  reviewplan.State
	params reviewplan.Params
}

// projectPlan is the plan kernel state of p: its seal, and per batch whether it
// closed and whether a run or an advance reached it.
func projectPlan(tx *Transaction, p ReviewPlan, bound uint32) (*planKernel, error) {
	params := reviewplan.Params{MaxBatch: bound}
	st := reviewplan.Initial(params)
	if len(p.Batches) >= int(bound) {
		return nil, reviewError("review plan state out of range: " + p.PlanID)
	}
	st.Planned, st.Sealed, st.Count = true, p.Sealed, uint64(len(p.Batches))
	for i, pb := range p.Batches {
		var c ReviewClosure
		closed, err := readReviewJSON(tx, closureName(pb.BatchID), &c)
		if err != nil {
			return nil, err
		}
		var b ReviewBatch
		found, err := readReviewJSON(tx, reviewBatchName(pb.BatchID), &b)
		if err != nil {
			return nil, err
		}
		touched := found && len(b.Advances) > 0
		if found && !touched {
			runs, err := batchRuns(tx, b)
			if err != nil {
				return nil, err
			}
			touched = len(runs) > 0
		}
		st.Closed[i], st.Touched[i] = closed, touched
	}
	return &planKernel{state: st, params: params}, nil
}

// unplannedBatchKernel projects an unplanned batch as a plan of that batch
// alone, so its advance goes through the same kernel event.
func unplannedBatchKernel(tx *Transaction, b ReviewBatch) (*planKernel, error) {
	params := reviewplan.Params{MaxBatch: 2}
	st := reviewplan.Initial(params)
	var c ReviewClosure
	closed, err := readReviewJSON(tx, closureName(b.BatchID), &c)
	if err != nil {
		return nil, err
	}
	st.Planned, st.Count, st.Closed[0], st.Touched[0] = true, 1, closed, true
	return &planKernel{state: st, params: params}, nil
}

func (k *planKernel) step(e reviewplan.Event) bool {
	next, ok := reviewplan.Step(k.state, e, k.params)
	k.state = next
	return ok
}

// firstError returns the first non-nil error; facts are checked in order and
// the first that failed names the refusal.
func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
