# Composite state model test

`internal/board/model_test.go` runs the real board commands over private board copies and walks the abstract states they reach. A state with no way to `done/` inside the search bounds is a dead end. Every dead end must be on the allow list, and every entry on the list must still exist. A new transition that strands a card fails the test.

The search is bounded. It does not prove that no dead end exists.

## Terms

- **Operation**: one supported command or launcher step, one `modelOp` in `model_ops_test.go` or `model_ops_review_test.go`. Names carry a prefix: `L` launcher, `X` executor, `H` human, `T` terminal move, `U` card update, `D` dispatch, `R` review, `C` coordinator, `F` completion tail.
- **Abstract state**: the state key (`modelFacts.key`). Revisions, identifiers and time stay out of it, so two paths that differ only in those reach one state.
- **Exit**: a path of operations from a state to a finished board. Every card ends terminal, and when a member reached `done/` the coordinator reconcile succeeds. An archive or a trash is never part of an exit.
- **`F.done`**: the completion tail of one card, counted as one operation. It claims an unstarted card, lets a running launcher confirm, takes over a card with no live owner, and then delivers. A single card ends with a plain `move done`. A member goes through `review/` and a wrap-up dispatch.
- **Seed**: a named path of operations. The search starts at the state a seed reaches, because most dead ends sit deeper than the search bound. Depth counts operations from the seed.
- **Focus**: a seed that names one member tries the operations of that member only, plus `F.done` of both. The two members are interchangeable, so this removes mirror states.

## Modes

| Mode | Selected by | Single card | Two-member group |
| --- | --- | --- | --- |
| default | `go test ./...` | explore 3, exit 2 | explore 2, exit 2, focused |
| full | `KANDER_MODEL_DEPTH=full` | explore 4, exit 3 | explore 3, exit 3, focused |

`platform-checks.yml` runs the default mode on Linux, macOS and Windows and the full mode in the `model-full` job on Linux. On Linux the search runs in `/dev/shm` when that mount has at least 4 GiB free, because board writes sync every file. The full mode peaks near 2.7 GiB there.

## Reading a failure

- `unlisted dead end in <scenario>`: the search found a state with no exit that no allow list row covers. The failure prints three lines:
  - `path`: the operations from the seed.
  - `state`: the abstract state.
  - `why`: the reconcile result of the state, the tail result of each card, and the reconcile result after the tails.

  Replay the path by hand with the commands that the operations in `path` name (`cli` on each `modelOp`). When the state really has no exit, fix the transition. When the new transition is deliberate and the fix is a later card, add a row. When the state has an exit that needs more operations than the bound allows, teach `F.done` that step.
- `DE-nn: the model no longer reaches this dead end`: a change removed the dead end, or moved it out of the bounds. When the fix is deliberate, remove the row. When it is not, a seed no longer reaches the state.
- `<invariant> after <operation>`: a transition broke an enabled invariant.
- A directed test named `TestDeadEnd...` in `internal/launch` fails: the launcher dead end it reproduces is gone. Remove the test and its row.

A failure that says `model infrastructure` is a defect of the test itself, not a board refusal.

## Adding a transition

1. Add one `modelOp` that calls the same board function as the command. Give it a guard that reads `modelFacts`, and return the board's error unchanged: an error means the command refused.
2. Add any new fact the transition depends on to `memberKey` or `reviewKey`. A fact outside the key merges states that behave differently.
3. Allocate identifiers with `nextID` and `nextCommit`, so one path gives one set of identifiers in every run.
4. Add a seed when the new state sits deeper than the bounds.
5. When the card repairs a dead end, remove its row. When it adds the records an invariant needs, give that row of `modelInvariants` a `check` and drop its `owner`.
6. Run `go test ./internal/board -run TestModel` and `KANDER_MODEL_DEPTH=full go test ./internal/board -run TestModelCompositeStates`. Compare the time with the budgets: 30 seconds for the default mode and 5 minutes for the full mode.

An operation that always strands a card, or that only exists to reach one dead end, sets `narrows`, so the search does not expand everything behind it.

## Allow list and invariant registry

`model_registry_test.go` holds both.

- `modelDeadEnds` has one row per known dead end, with its owner card and its evidence class: `model` (the search reaches it), `directed` (a directed test reproduces it), or `outside` (neither reproduces it). Rows are ordered: a state can show two causes, and the first row that covers it names it.
- `modelInvariants` has one row per invariant. A row with a `check` is enabled. A row with an `owner` waits for the records that card adds. `check` receives the facts before and after one transition.

`model_meta_test.go` keeps both tables honest: it fails on a missing owner, on a directed test that does not exist, on a removed row, on a repaired dead end that stays listed, and on an injected dead end.

## Limits

- Time stays out of the model. Expiry transitions (`AcceptBefore`, the wrap-up grant deadline) are not modelled.
- Git is structural: the reconcile verifier accepts every fact, so a rewritten history is covered by a directed test in `internal/launch`.
- Launcher steps run as the board functions the launcher calls. Container and process effects are the `executor` flag of the model; `internal/launch` covers the real order of those steps.
