# Prototype results

The restricted definition successfully generates TLA+ and Go. Finite baseline
checks complete, all three deliberate faults produce debt-safety counterexamples,
and the generated Go agrees with the tested model/reference transitions. This
supports another small refinement experiment, not replacing production board
logic or generating the entire Kander state machine.

## Measured run

Measured on Linux amd64, OpenJDK 17.0.21.0.3, Python 3.15.2 and Go 1.27.3,
on 2026-11-12. TLC is the pinned v1.8.0 artifact described in
[EXPERIMENT.md](EXPERIMENT.md). These are representative local wall times,
including JVM startup; they are not CI performance guarantees.

| Check | Members / cycles / batches | Distinct states | Seconds | Result |
| --- | --- | ---: | ---: | --- |
| Baseline local kernel | 1 / 3 / 3 | 2,909 | 0.48 | Complete, no violation |
| Independent kernel composition | 2 / 2 / 2 | 41,601 | 2.02 | Complete, no violation |
| Omitted reset obligation | 1 / 3 / 3 | 480 | 0.43 | DebtSafety violation |
| Old closure pays recovery | 1 / 3 / 3 | 480 | 0.49 | DebtSafety violation |
| Drop release clears recovery | 1 / 3 / 3 | 2,478 | 0.48 | DebtSafety violation |
| Both members finish | 2 / 2 / 2 | 1,128 | 0.45 | Completion witness |
| Finish after two resets | 1 / 3 / 3 | 932 | 0.45 | Completion witness |

Both baseline queues were empty on completion. Mutation and witness runs stop
when the expected invariant fails, with states remaining; their counts do not
represent exhaustive exploration. No arbitrary-member/cycle bound is proved.
Deadlock checking is disabled because allocation caps can prevent further work.
The two-member run contains independent plans, not a shared group plan.

The reference interpreter enumerated 1,964 reachable local baseline states and
tried all twelve events: 3,354 accepted transitions and 20,214 refusals. Together
with 38 actual TLC trace transitions, 23,606 fresh vectors were compared with the
generated Go code. All accepted/refused results and all post-state fields matched.
The different TLC/reference state counts are expected: TLC also stores the
independent history observer and last event; the reference enumerates the
generated local state only.

Six Python compiler tests passed. Four top-level Go tests passed, including three
mutation subtests. These include immutable input/member isolation, baseline
refusal of all three mutant completion traces, and the explicit decision required
by no-plan recovery under an all-N/A plan. This is targeted experiment validation,
not a statement that the repository's full test suite was run.

## Counterexamples and successful recovery

The first two faults have the same shortest action sequence:

```text
planRequired -> close -> reset -> rebind -> appendNA -> close -> done
```

The old required closure belongs to the earlier cycle. The later N/A closure
pays the cycle-coverage requirement but does not pay the recovery role. Mutation 1
fails to record that role; mutation 2 incorrectly accepts the old closure. The
independent observer still owes `reset` in either case. With mutation disabled,
Go accepts the prefix and refuses the final `done`.

The third fault crosses decision scope:

```text
planRequired -> close -> appendRequired -> reset -> drop
  -> releaseDrop -> rebind -> appendNA -> close -> done
```

The explicit drop release is valid for the drop obligation. It cannot release the
reset obligation. The mutant clears both; the observer detects the remaining
reset debt when completion is attempted. The baseline refuses completion.

A legitimate recovery path remains possible:

```text
planRequired -> close -> reset -> reset -> rebind
  -> appendRequired -> close -> done
```

The new required closure pays both the renewed recovery role and current-cycle
coverage. This witness guards against satisfying safety by refusing every
completion. A separate witness finishes two members under interleaving.

## What this establishes

- A narrow executable transition definition can supply both a checkable model
  and ordinary Go decision code with no runtime verifier dependency.
- An event-history observer can catch missing bookkeeping as well as incorrect
  evidence acceptance and release scope. Reusing the generated done predicate as
  the oracle would not provide this independence.
- Fresh model traces, an exhaustive local reference comparison, and mutation
  checks are practical at this scale. Generating Go does not itself make the
  compiler or transaction adapter correct.

The most useful next step is a single real obligation function with an explicit
refinement mapping and production-function trace tests, then shared-plan
membership and role semantics. Keep the model small while establishing that
connection. No production migration or general-purpose generator is justified by
this experiment alone.

Not verified here: the existing Go board implementation's conformance, the
generator's correctness for every permitted input, arbitrary-size safety,
liveness, shared plans, closure production, real I/O failure handling, and native
macOS/Windows execution. The production model tests remain unchanged.
