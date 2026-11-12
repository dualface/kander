# Production obligation correspondence

**The prototype is not a replacement for the production obligation logic.**
Trace comparison found one missing model rule: releasing a no-role recovery does
not erase its history. A later required role can make that recovery owed again.
Production retains the record and refuses completion; the generated baseline
kernel can permit completion. Neither production code nor the original model is
changed by this experiment. Tests pin this disagreement as a research result.

## Reproduce

From the repository root, with the ordinary Go toolchain:

```sh
go test -count=1 -v ./internal/board -run '^TestObligationRefinement'
KANDER_OBLIGATION_REFINEMENT_FULL=1 go test -timeout=20m -count=1 -v ./internal/board -run '^TestObligationRefinement'
go test -race -count=1 ./internal/board -run '^TestObligationRefinement'
python3 experiments/review-obligations/check.py --output /tmp/kander-obligations-new-run
```

The output directory of `check.py` must not already exist. Its pinned TLC/Java
prerequisites remain those of [EXPERIMENT.md](EXPERIMENT.md). The Go comparisons
need neither Java nor a runtime verifier. The default bound is two cycles and two
allocated batches. The optional full bound is three of each. Every batch identity
is allocated monotonically, including identities of dropped batches.

## Counterexample

The shortest completion disagreement found by the representative-path search is:

```text
reset -> planNA -> close -> releaseRecovery
      -> appendRequired -> dropReleased -> done
```

1. Reset without a plan creates a recovery record with no `PlanID`.
2. Create and close a plan whose roles are all N/A.
3. Explicitly release the no-role recovery. Production records `ReleaseDecision`
   but retains the recovery record. The model clears `unplanned`.
4. Append a required PMQA batch. Production reads the new role from the plan and
   owes it, even though the record still has a release decision. This is already
   a five-event disagreement in the obligation query.
5. Drop that batch with an explicit decision. This releases its drop obligation;
   production preserves PMQA in the no-plan recovery record's roles.
6. Attempt completion. The generated baseline accepts it. Production reports
   `recovery-review:<member>` and `recovery-role:<member>/PMQA` and refuses it.

Replacing `dropReleased` with `drop -> releaseDrop` produces the same problem.
`TestObligationRefinementReleasedRecovery` reproduces both variants with
`CreateReviewPlan`, `ExtendReviewPlan`, `ReviewTaskProgress`, and
`MoveWithOptions`, using the existing coordinator reset helper. The plan is
initially unsealed to permit extension, and sealed before attempting completion;
sealing is a storage/protocol step absent from the prototype. Existing helpers
supply coordinator and Git evidence fixtures, not actual agents or Git reviews.

This is a model under-approximation of debt, not evidence of a production bypass.
The production checks are stricter. The formal baseline still passes its original
properties because `Oracle.tla` also removes the `unplanned` debt on release and
does not recreate it when `appendRequired` occurs. A separately handwritten
observer can share a missing domain assumption with its transition model.

## Compared functions and observations

The test-only adapter lives in
`internal/board/review_obligation_refinement_adapter_test.go`. The generated Go
kernel is imported only by `_test.go` files. There is no new production dependency
or exported board API.

| Event | Real obligation implementation exercised |
| --- | --- |
| `reset` | `stageRecoveryReview` |
| `drop`, `dropReleased` | `keepDropObligations`, `recordDroppedRoles`, `moveRecoveryReviews` |
| `releaseDrop`, `releaseRecovery` | `releaseObligation` and its kind-specific implementation |
| After each event | `dropObligationsOwed(..., true)`, `recoveryReviewOwed` |

The observed tuple is `(drop owed, recovery owed, PMQA recovery role missing)`.
Unexpected role names or cardinality fail. The unmodified abstract projection
uses exactly the obligation conditions from the generated baseline `done` guard:

- Drop is owed when `dropOwed = 1` and `closed <= dropFloor`.
- Planned recovery is owed when `recoverOwed = 1` and there is no required closure
  later than `recoverFloor` in the current cycle.
- No-plan recovery is owed when `unplanned = 1` and there is no current-cycle
  required closure for the plan's present or remembered role. A no-role recovery
  can be owed without a missing role name.

Production keeps a batch array. Dropping its tail moves positional `FromBatch`
floors through the real implementation. The model keeps allocated serials, so a
new batch's serial exceeds every older floor. The comparison checks resulting
debt predicates, not equality of these differently represented floor values.

The adapter supplies plan pointers and minimal plan/closure records containing
the fields these functions consume. It synthesizes the card timestamp that
`planCycle` reads. It does not pass those records through full plan/closure
admission. `rebind` and `done` do not change either selected obligation family and
are observation-only steps. Each mutating event commits through `WithTransaction`
before the next event/query; no read-your-writes behavior is assumed. This uses
ordinary local persistence but does not verify crash consistency, recovery,
execution fencing, closure integrity, or the full transaction adapter.

## Coverage and interpretation

`TestObligationRefinement` enumerates the generated local kernel with BFS. For
each distinct abstract state it replays one shortest representative path on a
fresh temporary board and compares every non-initial prefix. Prefixes shared by
several paths are compared repeatedly. Deduplication uses only the abstract
state, so this is **not exhaustive exploration of concrete histories or edges**.
Refused abstract events are not sent to production, and no equivalence of
production admission/refusal conditions is asserted.

The one confirmed gap has an explicit test predicate: a prior no-plan recovery
release, no active abstract planned/unplanned recovery marker, a present or
remembered required role, and no current-cycle required closure. That predicate
uses an extra history bit outside the generated state. In that case the test
requires production to owe PMQA even though the original model says otherwise.
Elsewhere it requires exact tuple agreement. Unexpected disagreement fails, as
does production losing the debt in a known-gap state. A passing regression test
therefore means the known discrepancy and all other checked expectations were
reproduced; it does not mean refinement has been established.

Measurements on Linux amd64, Go 1.27.3, 2026-11-12:

| Comparison | Default 2/2 | Full 3/3 |
| --- | ---: | ---: |
| Distinct abstract states / representative paths | 130 | 1,964 |
| Replayed prefix observations | 631 | 15,734 |
| Observations with the known discrepancy | 17 | 536 |
| Distinct abstract states with that discrepancy on these paths | 6 | 97 |
| Modeled completions retaining production recovery debt | 2 | 22 |

Drop-debt observations agree throughout these paths. All recovery disagreements
fall into the confirmed release-history gap. These counts are observations of
representative paths, not counts of independent bugs or all concrete states.
The initial default comparison plus diagnostics took about six seconds; the
initial full exploration took 92 seconds. A later disk replay timed out after
ten minutes in `Fsync`. On Linux, setting both `TMPDIR` and `GOTMPDIR` to
`/dev/shm` completed the full regression in 15.216 seconds. Go 1.27's
`testing.TempDir` uses `GOTMPDIR` when set; changing `TMPDIR` alone did not move
these fixtures. These are environment observations, not performance guarantees
or evidence of crash durability on disk.

`TestObligationRefinementTLC` additionally consumes the existing 38 TLC transition
vectors, preserving their mutation settings and checking their recorded states.
At completion, production still owes recovery for each of the three deliberate
mutants. The two independent-member completion paths and the twice-reset path
end without debt. The two-member trace is split into two independent single-plan
replays; this establishes no shared-plan or concurrency property.

The original `check.py` checks still pass: both finite safety searches complete,
three deliberate mutants are detected, two success witnesses are found, and
23,606 generated/reference/TLC vectors agree. That result is compatible with the
new discrepancy because those checks do not establish the production mapping.

## Repository verification

PMQA passed with one non-blocking recommendation to run the repository-wide Go
suite. The follow-up command at code commit
`02b9393ca1be3f91cf9fea13a764dcaad903ef5f` was:

```sh
TMPDIR=/dev/shm GOTMPDIR=/dev/shm go test -count=1 -timeout=20m -json ./...
```

It exited 1: 29 packages passed, one failed, and four had no tests. Top-level
test outcomes were 2,628 passed, nine skipped, and one failed. The only failure
was `internal/tui.TestExecutionModelInputSavesWhenAgentsMatch`:

```text
options_test.go:790: disk large_model="gpt-6.1-sol" want "gpt-5.6-solX"
```

Running that test alone reproduced the same failure at both the code commit
and the unchanged base `43850bb7a9d478e1e75f1dbdd79715aabb6d73a7`.
It is a pre-existing test failure in this environment; the suite is not reported
as passing and this task does not change TUI behavior. The board package passed,
including its existing composite model test (1,818 single-member states and
2,665 group states, no dead ends at its default bounds). Targeted correspondence
tests, race checking, `go vet` for board and the experiment, and the delivery
check also passed. Task-card verification artifacts retain the full JSON test
stream, summary, baseline reproduction, and timeout diagnostics.

## Next step and limits

Before generating a production replacement, distinguish persistent no-plan
recovery provenance from its current release/debt status in the specification.
Update the independent history property from production's domain rule, rerun
TLC, and require the two explicit completion counterexamples to be rejected by
the revised kernel while retaining legitimate success witnesses. Do not simply
change production to match the current model or remove these regression cases.

Not established: arbitrary-bound correctness, every concrete history, shared
plans, multiple roles, liveness, public entrance correspondence beyond the two
reproduced cases, real reviewer/Git execution, failures between writes, native
macOS/Windows behavior, or proof of the generator. The existing production code
and model tests are unchanged.
