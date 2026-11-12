# Recovery provenance repair

The released no-plan recovery gap is repaired in the experimental model. The
revised baseline agrees with production's selected obligation queries at every
checked prefix, without the previous known-gap exception. Production logic is
unchanged. This remains bounded correspondence evidence, not a proof that the
experimental kernel can replace production.

The repair base is `b0747fab54b5b0f971fb6111084cd747f718e545`. The historical
[correspondence report](REFINEMENT.md) explains how the gap was discovered;
[EXPERIMENT.md](EXPERIMENT.md) describes the current interfaces and assumptions.
Measurements below are from Linux amd64, Go 1.27.3 and pinned TLC 1.8.0 on
2026-11-12.

## Changed rule

`unplanned` now means a persistent no-plan recovery record exists.
`unplannedReleased` separately records its no-role release decision.

- With no current or remembered required role, a release discharges the debt.
- Once a required role appears, that record needs a current-cycle required
  closure, regardless of the release decision. Dropping the role retains it.
- A reset with a plan and role history replaces the no-plan record with planned
  recovery and a new evidence floor. A planned reset without roles preserves the
  old record and release; another reset without a plan replaces it unreleased.
- A second release of an already released record is refused.

This follows `stageRecoveryReview`, `recoveryReviewOwed`, `moveRecoveryReviews`
and `releaseRecoveryReview`. The generated `done` guard uses this rule, and the
real-query projection tests the same abstract debt predicate.

The independent observer does not read either generated no-plan field. It
records no-plan recovery from `reset` events, retains that provenance across
release, remembers the cycle of required closures, and recreates debt on role
introduction when current-cycle evidence is absent. A planned reset with role
history replaces that debt with planned recovery. This fixes the shared missing
domain assumption in the earlier transition model and observer.

Mutation 4 deliberately clears provenance on release. The generator now accepts
integer expression literals through 4 and mutation bounds 0–4. Initial values
and reachable state values remain 0–3. Mutations are experiment-only controls.

## Safety and reachability

| TLC case | Generated states | Distinct states | Result |
| --- | ---: | ---: | --- |
| One member, 3 cycles, 3 batches | 5,762 | 2,752 | Complete; four invariants pass |
| Two independent members, 2 cycles, 2 batches | 188,035 | 40,449 | Complete; four invariants pass |

The four invariants are `DebtSafety`, `BoundSafety`, `MemberIsolation`, and
`TypeOK`. No search states remain queued in either safety run. TLC states include
observer and action bookkeeping, so their counts differ from local kernel states.

All four intentional faults violate `DebtSafety`: lost reset obligation, reused
old closure, drop release clearing recovery, and release forgetting provenance.
Mutation 4 produces the previously discovered shortest completion counterexample:

```text
reset -> planNA -> close -> releaseRecovery
      -> appendRequired -> dropReleased -> done
```

With mutation disabled, both that sequence and its `drop -> releaseDrop` variant
refuse the final `done` without changing state. The public board entrance tests
independently reproduce both sequences and confirm the production completion gate
refuses the retained PMQA recovery debt.

Three witness runs find debt-free completion: both independent members finish;
a member resets twice and pays renewed debt; and a released recovery acquires a
role, receives its required review, and finishes:

```text
reset -> planNA -> close -> releaseRecovery
      -> appendRequired -> close -> done
```

These witnesses establish selected reachability, not universal liveness. All
seven diagnostic/witness traces contain 52 transitions. Go reproduces their full
recorded states. Replaying all four mutant action traces with mutation disabled
refuses the final completion. The real-query replay sees debt for each mutant
and no debt for the four member witness paths (the pair contributes two).

## Strict production comparison

The adapter and production functions are unchanged. The separate history bit and
known-gap exception are removed from `TestObligationRefinement`. Every prefix now
requires exact agreement on `(drop owed, recovery owed, PMQA recovery role
missing)`. Every accepted modeled completion must have no production debt.

| Representative-path comparison | Default 2/2 | Full 3/3 |
| --- | ---: | ---: |
| Reachable abstract states / representative paths | 128 | 1,866 |
| Non-initial prefix observations | 616 | 14,838 |
| Disagreements | 0 | 0 |
| Debt-free completed states | 24 | 211 |

The Python reference evaluator separately tries every event at all 1,866 local
states: 3,218 accepted and 19,174 refused edges. Together with the 52 fresh TLC
edges, generated Go agrees with all 22,444 vectors. Ordinary Go tests also replay
the 52 checked-in TLC vectors. Six Python compiler tests and four Go kernel tests
pass. The fixture was refreshed from actual TLC output, then the entire runner
was rerun successfully; no diagnostic trace was hand-authored.

Fewer states than the historical prototype do not imply reduced bounds: the
same cycle and batch caps are used. Persisting provenance removes incorrect
completions and repeated release transitions; planned recovery replaces the
no-plan record instead of retaining two records for one production obligation.

## Reproduce and verification

From the repository root:

```sh
python3 experiments/review-obligations/generate.py --check
python3 experiments/review-obligations/check.py --output /tmp/kander-provenance-new
TMPDIR=/dev/shm GOTMPDIR=/dev/shm go test -count=1 -v ./internal/board -run '^TestObligationRefinement'
TMPDIR=/dev/shm GOTMPDIR=/dev/shm KANDER_OBLIGATION_REFINEMENT_FULL=1 go test -count=1 -timeout=20m -v ./internal/board -run '^TestObligationRefinement'
TMPDIR=/dev/shm GOTMPDIR=/dev/shm go test -race -count=1 ./internal/board ./experiments/review-obligations -run 'TestObligationRefinement|TestTransitionVectors|TestBaselineRejectsMutantCounterexamples|TestNoPlanRecovery|TestInvalidInvocation'
go vet ./internal/board ./experiments/review-obligations
TMPDIR=/dev/shm GOTMPDIR=/dev/shm go test -count=1 -timeout=20m -json ./...
```

The runner requires a new output directory and the pinned Java/TLC prerequisites
from `EXPERIMENT.md`; use `--fetch-tools` if the verified jar is absent. Ordinary
Go tests need no Java or runtime verification. The `/dev/shm` settings are Linux
fixture placement for practical test duration, not a durability claim. On other
platforms choose suitable local temporary storage and omit those assignments.

The full bounded comparison passed in 18.766 seconds. Default comparison, race
checking and vet passed. The required pre-commit repository-wide run exited 1:
29 packages passed, one failed, and four had no tests. Top-level outcomes were
2,628 passed, nine skipped, and one failed. The board package passed in 279.912
seconds, including its existing composite model tests. The only failure was the
previously known `internal/tui.TestExecutionModelInputSavesWhenAgentsMatch`:

```text
options_test.go:790: disk large_model="gpt-6.1-sol" want "gpt-5.6-solX"
```

A separate run at the unchanged base reproduced the same failure. TUI code was
not changed; the repository-wide suite is not claimed to pass. Card verification
attachments retain the full JSON stream, summary, and baseline reproduction.

## Remaining limits

The BFS still replays only one shortest representative history per abstract
state, not all concrete histories or production edges. The adapter's minimal
records exercise real debt queries and mutations but bypass full producer
admission; only the two explicit regressions use the full selected board entrance
sequence. Shared plans, multiple roles, arbitrary bounds, real concurrency,
crash recovery, execution fencing, reviewer/Git effects, native macOS/Windows,
universal liveness, and generator correctness are not proved.

The next useful expansion is to validate the production adapter and evidence
producer assumptions for one selected obligation function before replacing it.
A passing finite model alone still does not justify production code generation.
