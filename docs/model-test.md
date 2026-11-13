# Composite state model test

`internal/board/model_test.go` runs the real board commands over private board copies and walks the abstract states they reach. A state with no way to `done/` inside the search bounds is a dead end. Every dead end must be on the allow list, and every entry on the list must still exist. A new transition that strands a card fails the test.

The search is bounded. It does not prove that no dead end exists.

`TestModelCompositeStates` re-executes the test binary to run the search in a
child process, selecting only that test and omitting `-test.testlogfile`.
Otherwise the search's millions of file operations produce a multi-gigabyte
testlog, which `go test` reads into memory to calculate the test cache key and
stores in the build cache. The parent reads `KANDER_MODEL_DEPTH` before starting
the child so default and full mode keep distinct cache keys. The small searches
in `model_meta_test.go` still run in the original test process.

The child inherits the environment, including `TMPDIR`, `TMP` and `TEMP`, and
uses the remaining parent timeout with time reserved for reporting its failure.
With `-timeout 0` it has no timeout. Its output is forwarded immediately, so `-v`
shows each scenario line when that scenario ends. A failed verdict, panic or
timeout fails the parent test with the child's output. Child execution does not
contribute to coverage reported by `-cover`.

## Terms

- **Operation**: one supported command or launcher step, one `modelOp` in `model_ops_test.go` or `model_ops_review_test.go`. Names carry a prefix: `L` launcher, `X` executor, `H` human, `T` terminal move, `U` card update, `D` dispatch, `R` review, `C` coordinator, `F` completion tail.
- **Abstract state**: the state key (`modelFacts.key`). Revisions, identifiers and time stay out of it, so two paths that differ only in those reach one state.
- **Exit**: a path of operations from a state to a finished board. Every card ends terminal, and when a member reached `done/` the coordinator reconcile blocks no member. An archive or a trash is never part of an exit.
- **`F.done`**: the completion tail of one card, counted as one operation. It reconciles a blocked member and recovers it when the block stays, claims an unstarted card, lets a running launcher confirm, takes over a card with no live owner, and then delivers. A single card ends with a plain `move done`. A member goes through `review/` and a wrap-up dispatch.
- **`F.doneOwing`**: a plain `move done` of a card the operation history says owes a review, with only a rebind before it. The operation never pays the debt, so the search also asks the gate while the debt is open. `F.done` settles first and only sees what stays after that.
- **Debt**: a review the operation history owes (`modelDebt`). A reset and a dropped required batch write one where the operation runs. Neither the gate, the recovery record, nor a drop obligation decides it, so a change that stops writing one of those still leaves the debt. Only a closed batch the plan did not hold when the debt arose pays it.
- **Seed**: a named path of operations. The search starts at the state a seed reaches, because most dead ends sit deeper than the search bound. Depth counts operations from the seed.
- **Focus**: a seed that names one member tries the operations of that member only, plus `F.done` of both. The two members are interchangeable, so this removes mirror states.

## Modes

| Mode | Selected by | Single card | Two-member group |
| --- | --- | --- | --- |
| default | `go test ./...` | explore 3, exit 2 | explore 2, exit 2, focused |
| full | `KANDER_MODEL_DEPTH=full` | explore 4, exit 3 | explore 3, exit 3, focused |

`platform-checks.yml` runs the default mode on Linux, macOS and Windows and the full mode in the `model-full` job on Linux. The test logs one line per scenario as soon as that scenario ends, with its `states`, `expanded` and `seconds`. The default-mode step runs with `-v`, so the CI log shows these lines at once, also in a run that times out.

Without a memory directory the search is much slower. The four passing `platform-checks` runs after commit 9c4341e (41285615796, 41394355819, 41394364238 and 41399684333) measured these model step times:

| Job | Mode | Minutes |
| --- | --- | --- |
| `test (ubuntu-latest)` | default | 5.0 to 6.5 |
| `test (macos-latest)` | default | 10.3 to 16.2 |
| `test (windows-latest)` | default | 61.5 to 83.7 |
| `model-full` (Linux) | full | 12.8 to 27.6 |

Windows takes 10 to 16 times as long as Linux in the same run, and one commit can vary widely: on commit 66c6b4b, Windows took 61.5 minutes in run 41399684333 and reached the earlier 90-minute limit in run 41399815626. Run 41363725793 also reached that limit. The default-mode step now allows 150 minutes and the `test` job 180, and the later steps of the job run after a model failure. The full mode comes close to its 30-minute limit.

The model steps set `TMPDIR`, `TMP` and `TEMP` to the runner temp directory, because `tempBoard` refuses the symlinked default temp path of macOS. On Linux the search runs in `/dev/shm` when that mount has at least 1 GiB free, because board writes sync every file. The full mode peaks near 310 MiB there. The search keeps few copies on disk: it drops a copy as soon as its state is known and expands at most 64 states between merges.

## Reading a failure

- `unlisted dead end in <scenario>`: the search found a state with no exit that no allow list row covers. The failure prints three lines:
  - `path`: the operations from the seed.
  - `state`: the abstract state.
  - `why`: the reconcile result of the state, the tail result of each card, and the reconcile result after the tails.

  Replay the path by hand with the commands that the operations in `path` name (`cli` on each `modelOp`). When the state really has no exit, fix the transition. When the new transition is deliberate and the fix is a later card, add a row. When the state has an exit that needs more operations than the bound allows, teach `F.done` that step.
- `DE-nn: another row covers every state of this row`: two rows overlap. Narrow one of them, so a repair of either dead end stays visible.
- `DE-nn: the model no longer reaches this dead end`: a change removed the dead end, or moved it out of the bounds. When the fix is deliberate, remove the row. When it is not, a seed no longer reaches the state.
- `<invariant> after <operation>`: a transition broke an enabled invariant.
- `F.doneOwing: the gate does not name <item>`: the card owes `<item>` by the operation history, and the done gate refused it for another reason. The gate stopped asking for that obligation, or an operation stopped writing its record. When the gate passes, the failure is `I11 after F.doneOwing`.
- A directed test named `TestDeadEnd...` in `internal/launch` or `internal/board` fails: the launcher dead end it reproduces is gone. Remove the test and its row.

A failure that says `model infrastructure` is a defect of the test itself, not a board refusal.

## Adding a transition

1. Add one `modelOp` that calls the same board function as the command. Give it a guard that reads `modelFacts`, and return the board's error unchanged: an error means the command refused.
2. Add any new fact the transition depends on to `memberKey` or `reviewKey`. An operation that owes or pays a review writes it in `modelDebt` too. A fact outside the key merges states that behave differently.
3. Allocate identifiers with `nextID` and `nextCommit`, so one path gives one set of identifiers in every run.
4. Add a seed when the new state sits deeper than the bounds.
5. When the card repairs a dead end, remove its row. When it adds the records an invariant needs, give that row of `modelInvariants` a `check` and drop its `owner`.
6. Run `go test ./internal/board -v -run TestModel` and `KANDER_MODEL_DEPTH=full go test ./internal/board -v -run TestModelCompositeStates`; without `-v` a passing test hides the scenario lines. Compare the `seconds` of each scenario with the same command before the change. A change that slows the search multiplies the Windows time in "Modes", which already uses most of its 150-minute limit, and the full mode, which is near its 30-minute limit.

`R.dropRequired` drops a required batch with no decision, from the seed `dropped-required`, which `R.planChainedRequired` builds. `H.recoverNoPlan` resets a member that has no plan, from the seed `recovered-no-plan`. The seed `dropped-recovered` resets a member after a drop left its plan with no required role. `R.dropBatch` drops an N/A batch and `H.recover` resets a member with a plan.

An operation that always strands a card, or that only exists to reach one dead end, sets `narrows`, so the search does not expand everything behind it.

A state only an old binary reaches has an operation with `seedOnly`: a seed can start there, and the search never tries it. `U.oldReclaim` is one. On a card with stored links it breaks I2 and I3 by construction, so I3 skips a card that was already off its chain. On a card with no stored link, the seed `old-start-reclaim`, it is the reclaim the synthesized prefix reads, and I12 requires that card to stay on its chain until its first stored link. The operation records that case where it runs, so I12 does not read the mark it proves. `U.oldStrayRun` is another: it writes a run in a batch outside the plan, which breaks I6 until `R.resolveBatch` accepts the batch, so I6 skips a world that holds such a run. `U.oldStart` writes the card and the schema 1 attempt of a 1.0.0 start, and `U.oldTakeover` changes `OWNER`, `SESSION` and `WINDOW` on that card with no record. The seeds `old-start`, `old-takeover` and `old-takeover-review` start there, and `X.firstWindow` and `X.firstSession` make the first ledger write. The state key holds `stored`, so a card before its first write and the same card after it stay two states.

## Allow list and invariant registry

`model_registry_test.go` holds both.

- `modelDeadEnds` is empty, and `TestModelRegistriesAreComplete` keeps it empty. A row names one known dead end, with its owner card and its evidence class: `model` (the search reaches it), `directed` (a directed test reproduces it), or `outside` (neither reproduces it). Rows are ordered: a state can show two causes, and the first row that covers it names it. Every `model` row must be needed: the test removes each row in turn and fails when no state turns unlisted, so one row cannot hide another.
- `modelInvariants` has one row per invariant. Every invariant is enabled and has a `check`, which receives the facts before and after one transition. I10 reads the batches, closures, drop obligations and recovery records itself and shares no check with the done gate, so it fails when that gate lets a card through. I11 reads only the debts, so it also fails when a reset stops writing its record. Removing the gate's drop check alone lets no card through, because the seal check keeps the plan unsealed, and `F.doneOwing` fails instead: the refusal no longer names the debt.

`model_meta_test.go` keeps both tables honest: it fails on a missing owner and on a directed test that does not exist. It also proves that the verdict fails on a removed row, on a repaired dead end that stays listed, and on an injected dead end.

## Limits

- Time stays out of the model. Expiry transitions (`AcceptBefore`, the wrap-up grant deadline) are not modelled.
- Git is structural: the reconcile verifier accepts every fact, and `C.gitRejected` stands for a fact that stopped holding. The real verification of a rewritten history is covered by directed tests in `internal/launch`.
- Launcher steps run as the board functions the launcher calls. Container and process effects are the `executor` flag of the model; `internal/launch` covers the real order of those steps. `L.fail` makes the judgement of a late failure, and `L.closeFailed` stands for a failed close. `H.adopt` opens only for an executor the probe confirms: `X.exit` stands for `stopped` and `X.probeLost` for `unknown`, and both leave a takeover. `internal/launch` covers the real probe.
