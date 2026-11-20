package server_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/statemachine/serverclaim"
	"github.com/dualface/kander/internal/statemachine/servergrouplease"
)

// The coordinator write authority adds no state of its own: the group lease
// check is the group kernel's require event and the member check is the claim
// kernel's standing event, and neither moves a record. Every accepted and every
// refused write leaves the lease record and the claim records of the members
// as the kernels say.
func TestCoordinatorReviewWriteLeavesTheLeaseAndTheClaimsToTheirKernels(t *testing.T) {
	w := newCoordWorld(t)
	lease := leaseProbe{t: t, f: w.f, p: w.coordinator, group: coordGroup}
	members := [2]memberProbe{{t: t, f: w.f, p: w.hosts[0], task: w.tasks[0]}, {t: t, f: w.f, p: w.hosts[1], task: w.tasks[1]}}
	writes := 0
	write := func(proof *client.Claim) func() error {
		return func() error {
			writes++
			return w.writePlan(w.plan("kernel-plan", notApplicable(), "N/A", "N/A"), "kernel-"+strconv.Itoa(writes), proof)
		}
	}
	// around runs fn and checks every member claim projection: unchanged, and
	// equal to the standing step of the claim kernel, which accepts it.
	around := func(fn func()) {
		t.Helper()
		var before [2]serverclaim.State
		var base [2]uint64
		for i := range members {
			before[i], base[i] = members[i].state(nil)
		}
		fn()
		for i := range members {
			after, _ := members[i].state(&base[i])
			want, ok := serverclaim.Step(before[i], serverclaim.Standing{G: before[i].Gen}, serverclaim.Params{MaxGen: uint32(before[i].Gen + 1)})
			if !ok || after != want || after != before[i] {
				t.Fatalf("member %d claim: before %+v, after %+v, standing step %+v (accepted %v)", i, before[i], after, want, ok)
			}
		}
	}
	held := servergrouplease.Require{Hdr: true, G: 1, Tg: 1}
	old := w.proof()

	// An accepted write is a require the lease accepts; nothing moves.
	around(func() { lease.stays(held, "", write(w.proof())) })

	// A write refused for a card fact leaves the lease and the claims alone.
	stale := w.proof()
	stale.CoordinatorBasis[0].Revision--
	around(func() { lease.stays(held, api.CodeRevisionConflict, write(stale)) })
	missing := w.proof(w.tasks[0])
	around(func() { lease.stays(held, api.CodeEvidenceIncomplete, write(missing)) })

	// A forged token is a require the group kernel refuses.
	forged := w.proof()
	forged.Token = strings.Repeat("f", 64)
	around(func() {
		lease.refines(servergrouplease.Require{Hdr: true, G: 1, Tg: 2}, api.CodeHeldByOther, write(forged))
	})

	// A member that was taken over has no standing at its old generation: the
	// claim kernel refuses that event, the lease kernel still accepts the
	// coordinator's require, and the entrance refuses the write.
	w.f.clock.advance(91 * time.Second)
	if _, err := w.f.project("host-c").Takeover(t.Context(), "", w.tasks[0], api.ClaimRequest{ClientID: "host-c", ExecutionID: "successor", Owner: "codex", Decision: "explicit takeover fixture", ExpectedGeneration: w.grants[0].Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	now, _ := members[0].state(nil)
	if _, ok := serverclaim.Step(now, serverclaim.Standing{G: now.Gen - 1}, serverclaim.Params{MaxGen: uint32(now.Gen + 1)}); ok {
		t.Fatalf("the claim kernel gives the replaced generation a standing: %+v", now)
	}
	replaced := w.proof()
	replaced.CoordinatorBasis[0].Generation = w.grants[0].Claim.Generation
	around(func() { lease.stays(held, api.CodeLeaseInvalid, write(replaced)) })

	// A coordinator that was taken over is a require the group kernel refuses.
	w.takeOverLease()
	around(func() {
		lease.refines(servergrouplease.Require{Hdr: true, G: 3, Tg: 3}, api.CodeTakenOver, write(old))
	})
}
