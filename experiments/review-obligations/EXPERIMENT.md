# Review obligation verification and code generation experiment

This experiment demonstrates an offline path from a restricted transition
definition to a TLA+ model and Go decision code. It checks a small obligation
kernel, not the entire Kander implementation. Production packages do not import
it, and the production binary needs none of its verification tools.

The original baseline was `f3dd5ed4931cf1bfd7763d6d2e1496ff78b90c54` on `develop`.
The provenance repair uses `b0747fab54b5b0f971fb6111084cd747f718e545` as its base.
The earlier state-machine audit is the research input. The active v2 distributed
execution implementation is outside this experiment's baseline.

## Reproduce

Requirements: Python 3.10+, a Go toolchain supporting the repository's `go.mod`,
and Java 17+. Commands run from the repository root:

```sh
python3 experiments/review-obligations/generate.py --check
python3 experiments/review-obligations/check.py --fetch-tools
go test ./experiments/review-obligations
```

`--fetch-tools` downloads the official `tlaplus/tlaplus` v1.8.0 `tla2tools.jar`
only if absent. It verifies SHA-256
`e1f11b75c4b5bc4478ba0ac5cb6ae28e40a01b0016bc33e0aa97da22dfdbcd7f`
before use, including for an existing cache entry or `--jar PATH`. The default
cache is `~/.cache/kander-formal/tla2tools-1.8.0.jar`. The release asset reports
`TLC2 Version 2026.11.06.222357 (rev: d8fb231)`.

Without the flag, a missing tool fails with instructions. No tool is added to
the production dependencies. Download source:
[official release](https://github.com/tlaplus/tlaplus/releases/tag/v1.8.0).

The runner prints an external temporary artifact directory. An explicit
`--output /absolute/new-directory` selects a new directory; an existing directory
is refused. It retains each model configuration, source snapshot, TLC log,
counterexample JSON, readable action trace, Go comparison log, and `summary.json`
with source hashes and measured counts/times. A timeout, missing expected
violation, parse failure, partial baseline search, or Go mismatch fails the run.
Each TLC invocation has a 120-second budget and a 1 GiB Java heap limit.

To edit transitions, change `transitions.json`, run `generate.py`, inspect both
generated files, and rerun the full experiment. `--check` never rewrites outputs.
Do not edit `generated.go` or `Generated.tla` manually. Ordinary `go test` replays
the checked-in TLC vectors without downloading tools or launching Java. The full
runner also passes fresh TLC and exhaustive reference vectors to the Go tests.

## Definition and generated interface

`transitions.json` is the sole transition definition. Its schema permits integer
fields, boolean guards, simultaneous assignments, comparisons, addition, boolean
conjunction/disjunction, and conditional expressions. The compiler type-checks
expressions and rejects unknown fields/operators/sections, duplicate JSON keys,
invalid arity, and non-integer assignments. This is an experiment-local compiler
subset, not a public specification language or general code generation service.

Generated `Step(state, member, event, bounds)` returns a new state and an accepted
flag. Refusals preserve the state; all right-hand sides read the pre-state.
The caller supplies a state reached from `Initial` through accepted steps. The
function validates invocation bounds and member indices, but is not a parser or
validator for arbitrary persisted state. Input and output slices must not be
concurrently mutated by the caller.

`Mutation=0` is the baseline. Values 1–4 deliberately introduce faults into the
same generated model and Go code. They exist only to test detection in this
isolated experiment, and must never be used as production feature switches.

## Abstract state and source correspondence

Each member has its **own single-role plan**. Two members are an asynchronous
composition of independent kernels, not two members sharing a production plan.
PMQA is the only role; Security is N/A. The second member checks addressed-member
isolation and interleaving, not shared-plan membership/role union behavior.

| Experiment fact or event | Baseline implementation correspondence |
| --- | --- |
| `cycle`, `bound`, `rebind` | `planCycle`, `ExtendReviewPlan` cycle binding |
| `planNA`, `planRequired` | `CreateReviewPlan` with one batch |
| `appendNA`, `appendRequired` | `ExtendReviewPlan` appends a batch |
| `reset` | `stageRecoverReset`, `stageRecoveryReview` |
| `drop`, `dropReleased` | `dropPlanBatches`, `keepDropObligations` |
| `remembered` | `recordDroppedRoles`, retained after a drop decision |
| `dropOwed`, `dropFloor` | `DropObligations`, `dropObligationsOwed` |
| `recoverOwed`, `recoverFloor` | `RecoveryReview`, `recoveryReviewOwed` |
| `unplanned`, `unplannedReleased` | Persistent no-plan record and its separate release decision |
| `cycleNeed`, `cycleFloor` | `CycleBatchFrom`, `cycleBatchMissing` |
| `close` | An abstract valid `CloseReviewBatch` result |
| `releaseDrop`, `releaseRecovery` | `releaseObligation`, `releaseRecoveryReview` |
| `done` | The obligation portion of `reviewProgress` / the completion gate |

The relevant source files are `internal/board/review_obligation.go`,
`review_recovery.go`, `review_plan_extend.go`, `review_exclusion.go`,
`review_cycle_evidence.go`, `coordinator_recover.go`, and `review_gate.go`.
These are semantic correspondences from inspection, not a proved refinement
mapping to those Go functions. The generated Go kernel is new experimental code;
it does not call or replace those production functions.

Within one kernel:

- There is at most one pending tail batch. Completed batches remain represented
  by their latest relevant evidence; the first batch cannot be dropped.
- `batch` is a monotonically allocated identity, including dropped batches.
  Obligation floors compare ordering rather than production positional indices.
  A new batch is later than every older or dropped batch. This abstracts the
  `FromBatch` adjustment on tail drop; it does not verify that implementation.
- The required role is present if a required batch is pending or a required
  closure exists. Dropped required roles remain in `remembered`, even after a
  user decision. A later reset therefore recreates their review requirement.
- `reset` advances the cycle and preserves role obligations; a plan with no
  required role creates only a new-cycle coverage obligation. Reset before plan
  creation records the persistent `unplanned` provenance. `releaseRecovery`
  sets `unplannedReleased` without erasing that record. With no role, the
  release pays the debt; when a role later appears, a current-cycle required
  closure is needed regardless of that release. A planned reset with role
  history replaces the no-plan record with planned recovery. A planned reset
  without roles leaves the record and its release status intact.
- `close` requires current cycle binding. It stands for successful interpretation,
  author disposition, Git checks and closure publication. It is an assumed
  evidence producer, not a model of those protocols.
- User consent, execution fencing, a valid reset precondition, and source
  integrity are event preconditions supplied by the environment. In particular,
  `releaseDrop` represents an explicit authorized decision, not an automatic
  exception. It cannot release recovery obligations. `releaseRecovery` applies
  only to no-plan recovery with no required role history.
- The finite allocation caps can strand states. Deadlock checking is explicitly
  disabled; this experiment checks safety and selected completion reachability,
  not universal recoverability or liveness.

The model does not cover multiple pending batches, shared group plans, mixed
roles, partial publication, finding graphs, advancing commit targets, member
exclusion, stale executors, dispatch delivery, legacy history migration,
filesystem recovery, or actual Git/process/terminal behavior. Abstract atomic
actions cannot be used to infer atomic external side effects.

## Independent properties and code correspondence

`Oracle.tla` is handwritten separately from the transition definition. It derives
debts from accepted events, remembers that a role was required, and records the
batch boundaries after which new evidence can pay debts. It separately retains
no-plan provenance and the cycle of the last required closure. Introducing a
role revives no-plan debt unless that cycle already has required evidence.
It never reads
`dropOwed`, `recoverOwed`, `unplanned`, `unplannedReleased`, `cycleNeed`, or the generated done guard.
It does read event context (`plan`, batch identity, batch role, and cycle).
Independence is therefore scoped: it detects omitted obligation bookkeeping and
bad discharge logic, but is not a completely independent model of plan storage.

`Model.tla` checks:

- `DebtSafety`: every completed member has no outstanding history debt. This
  includes freshness/order of evidence and release-kind separation.
- `BoundSafety`: a completed member is bound to its current execution cycle.
- `MemberIsolation`: a member event leaves every other kernel unchanged.
- `TypeOK`: the reachable state remains inside the selected finite domains.

The baseline runs explore all reachable states in two configured finite
instances: one member / three cycles / three allocated batches, and two members /
two cycles / two allocated batches. There is no trajectory-depth cutoff.
TLC uses state fingerprints and reports their collision estimate in its logs.
Passing these instances does not establish arbitrary-size correctness.

Three additional runs deliberately check false `NoCompletion` properties. Their
counterexamples are **success witnesses**: both members can finish; and a member
can reset twice after a required review, then pay the renewed debt and finish; and a released no-role recovery can
acquire a required role, pay it with a new closure, and finish.
The runner requires these exact witness-property failures with no safety failure.
Witness runs stop at the first witness and are not full-space checks.

Four deliberate faults must violate `DebtSafety`:

| Mutation | Changed rule | Expected bad behavior |
| --- | --- | --- |
| 1 | Reset omits the recovery obligation | An N/A review allows completion |
| 2 | Any old required closure pays recovery | Old-cycle evidence is reused |
| 3 | Drop release also clears recovery | Consent for one kind clears another |
| 4 | Recovery release erases no-plan provenance | Later role debt is forgotten |

The runner feeds the actual TLC JSON transitions into generated Go and compares
acceptance and every post-state field. A separate Python interpreter explores
every reachable **local** baseline state and tries every event, including refused
events. Those vectors also run through Go. This tests the two emitters via TLC
traces and the Go emitter against a reference evaluator. It is not a proof of the
compiler, and not an exhaustive comparison against every TLC edge. Both emitters
and the reference evaluator share the validated input definition.

The checked-in `testdata/tlc-vectors.jsonl` contains the seven diagnostic/witness
traces from TLC v1.8.0, single worker, seed 1, fingerprint polynomial 0. Its trace
labels identify the case, bounds and mutation. To refresh after a reviewed model
change, inspect a successful full run and copy its `tlc-vectors.jsonl` into that
path, then rerun Go tests. Go also replays the four mutant action sequences with
mutation disabled and requires the final incorrect completion to be refused.

## Results and next decision

The [production correspondence experiment](REFINEMENT.md) found a release-history
rule missing from both the original kernel and its observer. The subsequent
[provenance repair](PROVENANCE.md) corrects both and removes the known-gap
exception from the real Go query comparison. The old defect remains reproducible
as mutation 4. See that report for current measurements; [RESULTS.md](RESULTS.md)
and [REFINEMENT.md](REFINEMENT.md) preserve the earlier stages' results.

The experiment is a feasibility check for generated decision logic. It is not a
replacement for the existing board model tests, and introduces no runtime
verification service. Before production adoption, establish a refinement mapping
for one real board function, verify its transaction adapter, and expand the model
to that function's actual shared-plan semantics and evidence producers.
