# Formal Verification of State Machines

Kander's state machines are defined once in Lean 4, proved there, and compiled into Go decision kernels. This document describes the definition language, its semantics and generic theorems, the proof obligations every machine carries, the generated Go interface, the commands, the trust boundary, how to add a machine, and the inventory of v2 state machines.

The toolchain lives in `formal/`; the generated kernels live in `internal/statemachine/<machine>/`. Lean is a development and CI tool only: production code and `go test ./...` never need it.

## Layout

| Path | Content |
| --- | --- |
| `formal/lean-toolchain` | The exact Lean version (`leanprover/lean4:v4.35.0`) |
| `formal/lakefile.toml`, `formal/lake-manifest.json` | The Lake project; it has no dependencies (no Mathlib) |
| `formal/KanderFormal/Dsl.lean` | Types, values, expressions, declarations, conformance |
| `formal/KanderFormal/Semantics.lean` | `eval`, `step`, `run`, `Reachable`, the generic theorems |
| `formal/KanderFormal/Typing.lean` | The type checker `Machine.wellTyped` and its consequences |
| `formal/KanderFormal/Obligations.lean` | The proof obligation template `SafetyCert` and shape lemmas |
| `formal/KanderFormal/EvalLemmas.lean` | Evaluation lemmas for the binder, array, and Boolean forms |
| `formal/KanderFormal/Machines/*.lean` | Machine definitions (the single source of each machine) |
| `formal/KanderFormal/Proofs/*.lean` | Machine proofs; `Proofs/Scalar.lean` holds the helpers of machines whose fields and arguments are all scalars; the dispatch machine's proofs span `Dispatch.lean` (encoding and event lemmas), `DispatchForms.lean` (the forms of an accepted step), `DispatchSafety.lean` (theorems 1 to 9), and `DispatchGrant.lean` (theorems 10 and 11) |
| `formal/KanderFormal/Registry.lean` | Registered machines, vector bounds, audited theorems |
| `formal/KanderFormal/Gen/*.lean` | Go emitter, test emitter, vector emitter, JSON encoding, SHA-256 |
| `formal/Main.lean` | The `kander-formal` executable (`gen`) |
| `formal/Audit.lean` | The axiom audit |
| `internal/statemachine/<machine>/` | Generated kernel, generated test, `testdata/vectors.jsonl` |
| `internal/statemachine/vectortest/` | Hand-written replay of differential vectors |
| `scripts/formal-gen.sh`, `scripts/formal-check.sh` | Regeneration and the read-only check |
| `.github/workflows/formal-checks.yml` | The Linux CI job |

The example machine (`Machines/Example.lean`, package `internal/statemachine/example`) exists only to exercise the toolchain. Production code does not use it.

## Definition Language

A machine is a Lean value of type `Machine` (a deep embedding). Names are lowerCamel ASCII identifiers; the emitter capitalizes them for Go.

### Declarations

- `enums`: named enums with 1 to 64 constructors.
- `params`: natural number parameters. Every parameter value is allowed; a statement quantified over `Params` holds for all of them.
- `fields`: state fields with a declared type and an initial value. Initial values are literals; a bounded natural number starts at 0 (it must conform when every parameter is 0, see `Ty.conforms_zero`). The initial value of an array field is the value of every element, because the length is a parameter (`initVal`); the initial state therefore depends on the parameters (`initial m p`).
- `events`: a name, typed arguments (scalars or sets), a Boolean guard, and a list of assignments `(field, expression)`. Assignments are simultaneous: every right-hand side reads the pre-state, and a field is assigned at most once.
- `queries`: derived values, a name, an expression type, and a body over state and parameters.

### Value Types

| DSL type | Meaning | Go representation |
| --- | --- | --- |
| `.scalar .bool` | Boolean | `bool` |
| `.scalar (.enum e)` | A constructor of enum `e` | `type E uint8` with constants `EC` and `ECount` |
| `.scalar (.nat b)` | A natural number `n ≤ p b` for parameter `b` | `uint64` (parameters are `uint32`) |
| `.map k t` | One scalar of type `t` per constructor of enum `k` | `[KCount]T` |
| `.set e` | A finite set of constructors of enum `e` | `type ESet uint64` with `Has`, `With`, `Without` |
| `.arr b t` | Exactly `p b` elements of `t` (a scalar or a set) for parameter `b` | `[]T` |

### Expressions

| Form | Meaning |
| --- | --- |
| `boolLit`, `enumLit e c`, `natLit n` | Literals; `n < 2^32` |
| `field f`, `param x`, `arg a` | Pre-state field, parameter, event argument |
| `cmp op a b` | `eq`, `ne` on any type (arrays compare by content); `lt`, `le`, `gt`, `ge` on naturals |
| `add a b`, `sub a b` | Natural addition; subtraction below zero is undefined |
| `and`, `or`, `not` | Boolean; both operands of `and`/`or` are evaluated |
| `ite c t e` | Only the selected branch is evaluated |
| `get m k`, `put m k v` | Map read and functional update |
| `mem s x`, `insert s x`, `erase s x` | Set membership and functional update |
| `aget a i`, `aput a i v` | Array read and functional update; an index outside the array is undefined |
| `afill a lo v` | The array or map `a` with every entry at index `lo` or above set to `v` |
| `any lo hi body` | Whether `body` holds for some `i` with `lo ≤ i < hi` |
| `first lo hi body` | The least such `i` for which `body` holds, else `hi` |
| `setOf e body`, `mapOf k body` | The set of constructors of `e` for which `body` holds; the map from each constructor of `k` to `body` |
| `bvar k` | The variable of the `k`-th enclosing `any`, `first`, `setOf`, or `mapOf`, 0 being the innermost |

`any`, `first`, `setOf`, and `mapOf` evaluate `body` for every index or constructor; one undefined evaluation makes the whole expression undefined.

An undefined expression (subtraction below zero, an array index outside the array) refuses the event, or makes a query return `ok = false`. A well-typed machine has no other undefined expression.

## Semantics and Generic Theorems

`step m p s e : Option State` looks up the event, binds its arguments (refusing a wrong count or a value outside its declared type or bound), evaluates the guard, evaluates every right-hand side in the pre-state, checks each value against the target field's declared type and bound, and applies all assignments at once. `none` means refused. `run` is the total form the Go `Step` implements: a refused event returns the original state and `false`.

These theorems hold for every machine and are registered in `Registry.genericTheorems`:

| Theorem | Statement |
| --- | --- |
| `run_refused_unchanged` | A refused event leaves the state unchanged |
| `run_accepted_iff`, `step_deterministic`, `run_deterministic` | One state, parameter value, and event give one result |
| `step_assign_reads_prestate`, `wellTyped_assign_reads_prestate` | Each assigned field receives its right-hand side evaluated in the pre-state |
| `step_unassigned_unchanged` | A field no assignment targets keeps its value |
| `step_refuses_out_of_bound` | An assignment outside its declared type or above its parameter bound refuses the event; nothing wraps or clamps |
| `step_preserves_conforms`, `initial_conforms`, `reachable_conforms` | Every reachable state of a well-typed machine conforms to its declared types and bounds, for every parameter value |
| `SafetyCert.reachable` | The obligation template below |

Determinism is also structural: `step` is a Lean function.

## Proof Obligations

Each machine registers, in its `Proofs/` file:

1. `wellTyped : M.wellTyped = true`, proved by `decide`.
2. A `SafetyCert M`: an invariant `Inv : Params → State → Prop`, `init : ∀ p, Inv p (initial M)`, and `step : Inv p s → step M p s e = some s' → Inv p s'`.
3. `reachable_inv`, derived through `SafetyCert.reachable`: every reachable state satisfies the invariant, for every parameter value.
4. Machine-specific safety properties as corollaries of the invariant (for the example, `reachable_sealed_passed`).

A machine with arrays or binders is easier to prove through a model: Lean functions for its state and events, an encoding into a machine state, and one theorem that every event the definition accepts on an encoded state decodes to a model event and yields the encoding of the model's result (`step_enc` in `Proofs/ReviewObligationModel.lean`). `EvalLemmas.lean` turns the evaluation of `any`, `first`, `setOf`, `mapOf`, array updates, and Boolean operators into ordinary Lean values. Machine-specific theorems are then proved on the model and carried over through the encoding.

`Obligations.lean` provides the shape lemmas (`conforms_bool`, `conforms_nat`, `conforms_enum`, `conforms_map_enum`, `conforms_set`, `bindArgs_nil`, `bindArgs_cons`) that turn a conforming state and event into explicit components. `Proofs/Example.lean` shows the pattern: an explicit state constructor `mk`, a `shape` lemma, and one case per event that evaluates the guard and assignments with `simp`.

Proofs never use `sorry`, custom axioms, or `native_decide`. The axiom audit (`formal/Audit.lean`) checks every declaration of every `KanderFormal` module, not only the registered ones, and allows only `propext`, `Classical.choice`, and `Quot.sound`. It also fails when a registered theorem is missing or a machine registers none.

## Generated Go Interface

For each registered machine `m`, `kander-formal gen` writes three files below `internal/statemachine/m/`:

- `zz_generated.go`, starting with `// Code generated by kander-formal; DO NOT EDIT.` and the path and SHA-256 of the definition file. It declares:
  - one `uint8` type per enum with its constants and `Count` constant, and one bit-set type per enum used in a set;
  - `Params` (one `uint32` field per parameter) and `State` (one field per state field);
  - `Event`, a sealed interface, and one struct per event (`type Claim struct{}`, `type Review struct { Role Role; Pass bool }`); the choice of one struct per event keeps arguments typed and makes the event set closed;
  - `Initial(p Params) State`;
  - `Step(s State, e Event, p Params) (State, bool)`: a refused event, including an argument outside its range, returns `s` and `false`;
  - one function per query, `func Q(s State, p Params) T`, or `(result T, ok bool)` when the body can be undefined: it contains a subtraction or an array read or update (`aget`, `aput`).
- `zz_generated_test.go`: the vector alphabet as Go values, canonical JSON encoders, and `TestVectors`.
- `testdata/vectors.jsonl`: the differential vectors.

Generated code uses no reflection. `Step` allocates only for array updates: `aput` and `afill` copy the array (`slices.Clone`) and never write the caller's state. `any`, `first`, `setOf`, and `mapOf` become loops. Every compound expression is bound to its own temporary, so gofmt has nothing to rewrite. Naturals are `uint64` inside the kernel; literals stay below 2^32, parameters are `uint32`, and there is no multiplication, so no expression overflows.

The hand-written shell around a kernel only projects persisted records into a kernel `State`, persists, and runs transactions, processes, and network calls. Every decision the kernel covers goes through the generated `Step` or a generated query.

## Differential Vectors

The registry declares, per machine, a depth (at least 4 for the example), the parameter sets, and the values tried for natural number arguments. The alphabet is every event with every argument combination from those domains (enum arguments use every constructor, Boolean arguments both values, set arguments every subset). A machine whose full alphabet is too large to walk registers its alphabet as an explicit event list instead.

`Gen/Vectors.lean` evaluates the Lean semantics on every state reachable within `depth - 1` events from each parameter set's initial state, for every event of the alphabet, and writes one JSON line per step: the parameter set index, the state, the event index, whether it was accepted, the next state, and the query results of the state. The first line is a header with the machine name, depth, parameter sets, and alphabet.

`vectortest.Check` checks the header against the Go alphabet and parameter sets, then walks every event sequence of length `depth` from each initial state through the Go `Step`, looks each step up in the Lean rows, and compares the acceptance flag, the next state, and the query results byte for byte through the canonical encoding. It fails on a missing row, a mismatch, a duplicate row, or a row no sequence reaches. For the example this replays 48,312 steps.

## Commands

```sh
scripts/formal-gen.sh     # regenerate kernels and vectors in place
scripts/formal-check.sh   # read-only check, also run by CI
```

`formal-check.sh` runs `lake build` (failing on any `sorry` warning), the axiom audit (`lake env lean Audit.lean`), regenerates into a temporary directory and compares every generated file byte for byte (listing each differing, missing, or stale file), checks every header digest against `sha256sum` of its source file, and runs `gofmt -l`, `go vet`, and `go test` on `internal/statemachine/...`. It never writes to the repository.

Both scripts put `~/.elan/bin` on `PATH` when it exists. To install the pinned toolchain locally, install elan and let it fetch the version in `formal/lean-toolchain`, or follow the verified steps of the CI job.

## CI

`.github/workflows/formal-checks.yml` runs on Linux for pushes to `v2` and `group/**`, for pull requests targeting `v2`, and on manual dispatch, filtered to the formal paths. It downloads elan `v4.2.4` and the Lean `4.35.0` archive, verifies both against pinned SHA-256 digests, installs the archive as the toolchain `formal/lean-toolchain` names (so elan downloads nothing), caches `formal/.lake`, and runs `scripts/formal-check.sh`. macOS and Windows are not checked by this job; the generated Go is covered there by the ordinary `go test ./...`.

## Trust Boundary

- **Trusted:** the Lean 4 kernel and the statement of each theorem. The theorems are about the semantics in `Semantics.lean`.
- **Not proved, compensated:** the generators (Go emitter, test emitter, vector emitter) are ordinary Lean programs without a correctness proof. The differential vectors check the generated Go against the Lean semantics on every event sequence within the registered bounds, and the freshness check ensures the checked-in Go is exactly what the current definitions produce. Behavior outside the bounds relies on the emitter mirroring the semantics construct by construct.
- **Covered by refinement tests:** the correspondence between a hand-written shell and its kernel. For a production entry that goes through a kernel, a refinement test asserts `projection(after write) = Step(projection(before write), event)` for an accepted event, and that the projection is unchanged for a refused one. Later cards write these tests per entry.
- **Assumptions, not proved:** M13 (board multi-file transactions, revisions, and form migration recovery) and M14 (archive evidence cleanup) are treated as file-system level assumptions. Liveness is not proved.

## Adding a State Machine

1. Write `formal/KanderFormal/Machines/<Name>.lean` with one `Machine` value. Prefer one `def` per event so proofs can case on them.
2. Write `formal/KanderFormal/Proofs/<Name>.lean`: `wellTyped` by `decide`, the invariant, `init`, `step_inv`, a `SafetyCert`, `reachable_inv`, and the safety corollaries.
3. Register the machine in `Registry.entries` with its source path, vector bounds (depth at least 4 unless the report records why not), and theorems; import the proof file in `Registry.lean`.
4. Run `scripts/formal-gen.sh`, then `scripts/formal-check.sh`, and commit the definition, the proof, and the generated package together.
5. Route the production decision through the generated `Step` or query and add the refinement tests for each entry.

A DSL extension (a new type or expression) must extend `eval`, `infer`, the generic theorems, the Go emitter, the test emitter, and the vector encoding together, and keep the example machine passing. Later cards may extend the language and the generated interface but must not break them.

## Review Obligations and Review Plans (M9 to M11)

Two machines replace the hand-written decisions of `internal/board` for review plans, batches, and obligations:

| Machine | Definition | Kernel | Scope |
| --- | --- | --- | --- |
| `reviewobligation` | `Machines/ReviewObligation.lean` | `internal/statemachine/reviewobligation` | One plan member: the batches of its plan as it sees them, cycle binding, rebind position, drop obligations, dropped roles, the recovery record of a reset, completion |
| `reviewplan` | `Machines/ReviewPlan.lean` | `internal/statemachine/reviewplan` | One plan: creation, extension shape and admission, append, seal, rebind, target sync, drop, release, resolution, run, advance, view, close |

Members share no obligation state: a plan event (an appended batch, a close, a drop) is one event of each member's kernel. Where a plan decision depends on members (a sealed plan admitting a batch, the seal, a close needing every cycle bound, the completion gate of the other members), the plan event takes the member kernels' query results as an argument. Integrity facts (a CAS, a view hash, a run chain, Git evidence, finding and disposition conclusions of M7 and M8) are Boolean event arguments the shell computes; the kernel computes none of them and refuses the event when one fails. A close also tells each member's kernel which required roles have a conclusion covering the member's current cycle; the kernel refuses to close a batch that names the member while one is missing. Whether a drop leaves obligations on a member is the kernel event `dropTakes`; the shell writes the obligation records of the members it accepts.

Proofs, for every role list and every batch bound:

| Theorem | Statement |
| --- | --- |
| `ReviewObligation.debt_safety` | Whenever the machine accepts a completion, the independent observer of the history finds no review owed |
| `ReviewObligation.evidence_floor` | At a completion every debt of a drop or a planned reset is paid by a closed batch added after it arose, closed in the debt's cycle or later on a conclusion that covered the cycle current at the close; the no-plan record by a review of the current cycle |
| `ReviewObligation.release_once` | A released no-plan recovery record is not released again |
| `ReviewObligation.reachable_inv` | Every reachable state encodes a well-formed model state related to the observer of a history that reaches it |
| `ReviewObligation.counter_dropReleased`, `counter_releaseDrop` | The two counterexample traces of the replaced prototype refuse completion |
| `ReviewObligation.counter_staleClose` | After a reset, a batch that names the member cannot close on a conclusion that does not cover the current cycle |
| `ReviewObligation.witness_members`, `witness_twoResets`, `witness_releasedRole` | Its three witness traces complete (two members as two single-card traces) |
| `ReviewPlan.integrity_refused` | An advance, close, or resolution with a failed integrity fact is refused and leaves the state unchanged |
| `ReviewPlan.sealed_append`, `sealed_seal`, `sealed_admit` | A sealed plan takes a batch only when a member owes one, never with another seal |
| `ReviewPlan.reachable_inv` | A reachable state without a plan is the initial state |

The independent observer (`Proofs/ReviewObligationObserver.lean`) reads only the event history. It gives every batch an identity in the order batches were added and keeps debts with their provenance: a drop of required batches and a planned reset owe one review per role, paid only by a closed batch with an identity at least that of the debt; a reset with no plan leaves a no-plan record kept across its release, which once a role applies needs a review of the current cycle; a rebind owes a batch from the member's first open batch on. The machine instead keeps positions and floors that a drop moves to the new end of the plan; `Proofs/ReviewObligationRel.lean` and `Proofs/ReviewObligationSafety.lean` prove the two agree. The observer follows the "Changed rule" of the prototype it replaces, with production's role set: a plan requires the union of its batches' roles, and a no-plan record remembers the roles a drop took out of the plan.

The hand-written shell is `internal/board/review_kernel.go`: it projects the plan, batch, closure, drop obligation, and recovery records into kernel states, runs kernel events, and persists what they accept in the existing record formats. Refinement tests check every production entrance that goes through a kernel: `internal/board/review_kernel_entrance_test.go` asserts that the projection after each write equals the kernel step from the projection before, and that a refused write leaves it unchanged; `internal/board/review_kernel_refinement_test.go` walks every kernel state within a depth breadth first and drives the shell functions to each, comparing the projection after every step (`KANDER_OBLIGATION_REFINEMENT_FULL=1` raises the depth from 6 to 8).

The differential vectors of both machines use an explicit alphabet and depth 5: the full alphabet of every Boolean fact combination is too large to walk, and depth 5 keeps the replay within a few seconds.

Background: the TLA+ and Python prototype `experiments/review-obligations/` explored these obligations with bounded model checking. It is replaced by the machines above and removed; its counterexample and witness traces are Lean theorems now.

## Card Lifecycle, Execution Ownership, and Server Claims (M1, M2)

Three machines replace the hand-written decisions of card columns, contract freezing, member exclusion, the execution ledger, and the v2 server claim record:

| Machine | Definition | Kernel | Scope |
| --- | --- | --- | --- |
| `cardlifecycle` | `Machines/CardLifecycle.lean` | `internal/statemachine/cardlifecycle` | One card: its column, whether its contract is frozen, its `CONTRACT_DECISIONS` records, and its member exclusion |
| `executionledger` | `Machines/ExecutionLedger.lean` | `internal/statemachine/executionledger` | One card's execution ledger: the kind and status of the current record, the record position, the transition count, the card revision, the claim lines, and the revision of the current record |
| `serverclaim` | `Machines/ServerClaim.lean` | `internal/statemachine/serverclaim` | One task's claim record on the v2 server: whether the card is in `working` or `review`, the status, the generation, and whether the lease has not expired |

The split follows the theorems: the transition table and the contract freeze involve only the card, the stale-writer refusals of the ledger involve only the ledger, and "at most one valid claim" involves only the claim record, so each invariant stays small and no product machine is needed. A server entrance that changes both a claim and the ledger (a claim, a takeover, a launch rollback) runs both kernels from its shell. All fields and arguments are scalars; `Proofs/Scalar.lean` turns an accepted step into its event and arguments, and each event has a lemma that evaluates `step` on an encoded state to an explicit `if`, on which the theorems are proved. The kernels take integrity facts the shells compute (a verified release, a recovery CAS, a dispatch authorization, an owner that matches, an observation ticket) as Boolean arguments, as the review kernels do.

`kander pick` is the move into `todo`, so `move` covers it. Lease expiry and a pending record of an earlier binary are environment events with no timing assumption. The ledger's `relocate` and `write` events describe the moves and card writes that change no execution fact; they keep the revision of the projection exact, and the refinement tests step them, while the production decision of those moves is the `cardlifecycle` transition table.

Proofs, for every parameter value:

| Theorem | Statement |
| --- | --- |
| `CardLifecycle.move_accepted_iff`, `pick_accepted_iff` | (1) An ordinary move, and a pick, is accepted exactly when its source and target columns are a row of the transition table |
| `CardLifecycle.rollback_spec`, `dispatchMove_spec`, `recover_spec` | (2) The preconditions and result column of a start rollback (`working` to `todo`; a takeover rollback keeps the column), a same-column dispatch move (`working`, authorized), and a recovery reset (`working` or `review` to `working`, with its CAS, a blocked member, no dispatch binding, and no pending start) |
| `ServerClaim.holder_unique`, `reachable_valid_held` | (3) `requireHolder` accepts only the current generation and its token, so at most one claim is valid; a valid claim belongs to a card in `working` or `review` |
| `ServerClaim.gen_monotone` | (4) No accepted event lowers the generation; a takeover raises it by one |
| `ServerClaim.stale_holder_refused`, `stale_terminate_refused`, `stale_takeover_refused`; `ExecutionLedger.stale_revision_refused`, `unresolve_stale`, `stale_fence_refused` | (5) A holder write of another generation, a termination whose token belongs to an older generation while a claim is valid, and a takeover that observed another generation are refused; a revision-checked ledger write (claim, start, reclaim, recovery, move, write, rollback, window restore) at another revision, an unresolve older than the record, and a fenced write (confirm, unresolve, adopt, takeover, rollback, field) of another record are refused. Refused events leave the state unchanged (`run_refused_unchanged`) |
| `CardLifecycle.frozen_change`, `reachable_frozen_change`, `decisions_only_update` | (6) Once a card left `backlog`, an update that changes a frozen field only ticks acceptance items in `working` or `review`, or carries a decision and adds one `CONTRACT_DECISIONS` record; no other event changes the record count |
| `CardLifecycle.reachable_inv`, `ExecutionLedger.reachable_inv`, `ServerClaim.reachable_inv` | (7) The invariants hold in every reachable state: a card outside `backlog` is frozen, an excluded member is in `archived` or `trash`; a ledger card in `backlog` or `todo` has no record that owns it, only a start or a takeover is pending, and claim lines never outnumber records; a valid claim is on a claimed card and a record exists exactly when its generation is positive |

Theorem (5) states the behavior of v2 as it is (contract decisions of 2026-11-14 on `20261114-formal-card-lifecycle-task`): a termination needs no holder proof once no claim is valid, a termination with the current token and a generation header of another generation passes as `lease_invalid`, and the fenced ledger entrances never compare the card revision, so a fast executor's own updates do not fail its launcher's confirmation.

The shells are `internal/board/lifecycle_kernel.go`, `internal/board/execution_kernel.go`, and `internal/server/claim_kernel.go`. They project the card (column, `contract_frozen` of the version record, decision records, exclusion record), the ledger (current record, chain, revision, claim lines), and the claim record (status, generation, the server clock against `ExpiresAt`) into kernel states; map claim headers to the generation the header and the token belong to (the token is still compared by hash in constant time); step the kernels; persist what they accept; and select the existing error for a refused event in the order the entrance has always checked. Five projection rules matter: history before the first stored ledger link never holds a card in `backlog` or `todo`, as before; the handoff link of an old takeover, which the first write after it stores, counts in the transition count before it is stored; because the kernel parameters are `uint32` and its decisions only compare these values with each other and add or subtract one, an operation fixes a shift from the state before it (a generation, record position, transition count, or claim count from 2^31 on projects to 1; a card revision from 2^31 on keeps the 2^30 revisions below it in order, and an older one projects to 0) and maps the state, the event arguments, and the result with that same shift, so values up to the last `uint64` keep their decisions and the refinement equation; the archive cleanup (M14) removes an archived card's exclusion record after its move commits, so the exclusion flag of an archived card is not projected; `RecordExecutionBranch` writes no ledger link and stays outside the kernel. One unreachable call is now refused: a rollback of a takeover attempt into `todo`, which no caller makes.

`internal/board/lifecycle_kernel_entrance_test.go` and `internal/server/claim_kernel_test.go` drive every entrance that runs a kernel on a temporary board or server and assert `projection(after) = Step(projection(before), event)`, or for a refused entrance an error, a kernel refusal, and an unchanged projection; each theorem has at least one refused scenario there.

The differential vectors use explicit alphabets: `cardlifecycle` and `serverclaim` replay every sequence of depth 5, `executionledger` of depth 4, because its 30 events would make depth 5 walk about 24 million sequences.

## Durable Dispatch Rounds, Wrap-Up Grants, and Settlements (M3 to M5)

One machine replaces the hand-written acceptance and refusal decisions of the dispatch rounds of a card, of the wrap-up-only grant a round can hold, and of the settlement of a completed binding:

| Machine | Definition | Kernel | Scope |
| --- | --- | --- | --- |
| `dispatch` | `Machines/Dispatch.lean` | `internal/statemachine/dispatch` | The slot a card offers one round of dispatch: the round, its epoch, whether the card binding is set, the phase and kind of the round, whether its release was written, and whether the record holds the receipt of its acceptance and the receipt of its completion (the content of a receipt is not modeled), whether the round holds a wrap-up grant, and whether its completed binding was settled |

The events are `prepare` (a new round) and `retry` (the same ID with an equal input), `attempt` (`BeginDispatchAttempt`), `reauthorize` (`ReauthorizeDispatch`), `accept` (the move into `working`), `complete` (the move into `review` or `done`), `end` (`EndDispatch`, fail or cancel, with the release), `cancel` (the unfinished binding at a move into `archived` or `trash`), `retain` (a takeover that keeps the round), and `exec` (`requireExecution`, for author and non-author writes). The round number abstracts the dispatch ID, whose global uniqueness stays with the registry. A release keeps the pair `(round, epoch)` of the record while the card binding is empty.

The wrap-up grant (M4) adds `authorizeWrapUp` (`AuthorizeDispatchWrapUp`: a fresh confirmed exit, outcome `stopped`, or `reclaimed` with a decision; the epoch rises by one, the round returns to `prepared`, and both receipts are cleared), `full` (`requireFullExecution`: a start rollback, an execution identity write, or an author conclusion), and `wrapUpWrite` (`validateWrapUpUpdate`: no contract decision, and an append to `spec.md` or the dedicated record path). `attempt` and `reauthorize` refuse a round that holds a grant. The settlement (M5) adds `settle` (`SettleDispatch`), `terminateSettle` (the move of a completed binding into `archived` or `trash`), and `takeoverSettle` (the `settle-completed` mode of a takeover). A settlement keeps the round completed and its delivery commit, and only empties the card binding; it never releases. The grant and the settlement describe the current round, and a new `prepare` clears both.

A request presents an authorization pair; the kernel compares it with the binding. The shell passes as Boolean arguments what it computes and the kernel does not: the server clock against the acceptance deadline, the plan cycle of the round, the card and record revisions, the integrity of stored receipts and originals, the result of the wrap-up evidence checks, whether a start is unfinished, whether the epoch is below the last `uint64`, and whether a decision was recorded.

Proofs, for every round and epoch bound:

| Theorem | Statement |
| --- | --- |
| `Dispatch.prepare_single` | (1) A new round is accepted only when the card has no binding or its binding is terminal |
| `Dispatch.pair_monotone`, `prepare_strict`, `reauthorize_strict`, `fence_refused` | (2) No accepted event lowers the pair `(round, epoch)` of the record in lexicographic order; a new round raises the round and sets the epoch to the card's epoch plus one (1 without a binding); a reauthorization raises the epoch by one; an `accept`, a `complete`, an `end`, or an executor write that presents a pair other than the binding is refused and the state stays |
| `Dispatch.accept_change`, `complete_change`, `complete_kind` | (3) An `accept` that changes the phase starts from a prepared or uncertain round before its deadline and records its receipt; a `complete` that changes it starts from an accepted round of a card in `working`; a wrap-up round completes only into `done`, any other kind only into `review` |
| `Dispatch.accept_replay`, `complete_replay` | (4) An `accept` of an accepted or completed round, and a `complete` of a completed round with the identical receipt, are accepted and change nothing |
| `Dispatch.terminal_stable`, `terminal_refused` | (5) Within one round a terminal phase never changes; `attempt` and `reauthorize` refuse it |
| `Dispatch.end_spec`, `end_once`, `release_once` | (6) Ending an accepted round needs a decision, a failed or cancelled round is released only with a decision and its own phase, a release empties the binding, and a round is released once |
| `Dispatch.reauthorize_spec` | (7) A reauthorization raises the epoch by one, returns the round to `prepared`, and clears both receipts; an accepted round needs fresh stopped evidence, any other an unexpired deadline |
| `Dispatch.cycle_refused` | (8) A round of another plan cycle is refused by `attempt`, `reauthorize`, `accept`, `complete`, `end`, `cancel`, and `retain`; `prepare` and `exec` carry no cycle |
| `Dispatch.reachable_inv`, `reachable_unbound`, `reachable_unbound_settled`, `reachable_receipts`, `reachable_grant_kind` | (9) In every reachable state the phase and the epoch exist exactly once a round exists, a binding names a round, a release belongs to a failed or cancelled round and an empty binding, an empty binding is the initial state, a released round, or a settled one (`reachable_unbound` names the states that are not settled), the receipts follow the phase: none while prepared or uncertain, the acceptance receipt once accepted, both once completed, no completion receipt on a failed or cancelled round, a grant belongs to a round of kind wrap-up, and a settlement belongs to a completed round that is neither bound nor released |
| `Dispatch.authorize_spec` | (10) A grant is accepted only for an open round of kind wrap-up on a card in `working` or `review` that holds no grant, with a fresh exit whose outcome is `stopped` or `reclaimed` with a decision; it raises the epoch by one, returns the round to `prepared`, clears both receipts, and sets the grant |
| `Dispatch.grant_refused`, `wrapUpWrite_spec` | (10) While a round holds a grant, `attempt`, `reauthorize`, a second grant, a full-execution write, and a write with a contract decision are refused and the state stays; a write the grant allows has no contract decision and appends to `spec.md` or writes the record path with no other content |
| `Dispatch.authorize_fences_previous`, `grant_complete_done`, `authorize_complete_done` | (10) After a grant, `accept`, `complete`, `end`, and executor writes that present the epoch before it are refused; a round that holds a grant completes only into `done` |
| `Dispatch.settle_spec`, `terminate_settle_spec`, `takeover_settle_spec`, `settle_once` | (11) A settlement acts on a completed round that is not settled, from `settle` also on a card in `working` or `review`; the round stays completed, the binding is empty, and a round settles at most once |
| `Dispatch.settled_fences`, `release_settle_exclusive` | (11) After a settlement, `accept`, `complete`, `end`, and executor writes that present the pair of the round are refused; a release and a settlement never meet in one round |
| `Dispatch.cycle_refused_grant` | (11) A round of another plan cycle is refused by `authorizeWrapUp`, `settle`, `terminateSettle`, and `takeoverSettle` |

The shell is `internal/board/dispatch_kernel.go`, with the entrances in `dispatch.go` (`prepareDispatch`, `BeginDispatchAttempt`, `ReauthorizeDispatch`), `dispatch_receipt.go` (`requireExecution`, `stageDispatchMove`, `stageDispatchTermination`), `dispatch_release.go` (`EndDispatch`), `dispatch_takeover.go` (the `retain` and `settle-completed` modes), `dispatch_wrapup.go` (`AuthorizeDispatchWrapUp`, `requireFullExecution`, `validateWrapUpUpdate`), and `dispatch_settlement.go` (`SettleDispatch`). The coordinator check of a wrap-up, the integration evidence, the exit observation and its 30-second window, and the reading and writing of originals stay in the shell, and the kernel takes their results as Boolean arguments. Projection rules:

1. The round is relative: 1 when the card has a binding or the entrance holds a record, else 0. A new round adds one to the round of the state before it.
2. The epoch of the state is the card's `EXECUTION_EPOCH` when bound, else the epoch of the held record, else 0. `DISPATCH_ID` and `EXECUTION_EPOCH` are written and cleared together, so a card never has an epoch without a binding. As in M2, an operation fixes a shift from the state before it (an epoch from 2^31 on projects to 1) and maps the state, the presented epochs, and the result with it. The ceiling of the `uint64` representation is the `room` argument.
3. A presented ID or epoch projects to the binding's round or epoch when it equals it, to another non-empty value when it does not, and to 0 when empty.
4. The two receipt flags project to whether the held record has an acceptance receipt and a completion receipt.
5. The grant flag projects to whether the held record has a wrap-up authority, and the settled flag to whether it has a settlement.
6. A card without binding projects to the empty state, unless the entrance holds the released record.
7. Checks that the original code ran in a fixed order keep it: an entrance runs the kernel on a state that no other fact can refuse (a probe) to answer one fact at a time, so a request that fails several returns the error of the first, as before. The last step runs on the projected state.

`internal/board/dispatch_kernel_entrance_test.go` and `dispatch_grant_kernel_test.go` drive each entrance on a temporary board and assert `projection(after) = Step(projection(before), event)`, or for a refused entrance an error, a kernel refusal, and an unchanged projection; each theorem has at least one refused scenario there, and the grant and settlement entrances also assert the error of the first refused check. The differential vectors use an explicit alphabet of 67 events at depth 4. A settled round is four events from the initial state, so the vectors cover the events that settle but not the events a settled round refuses; the proofs (`settle_once`, `settled_fences`) and the refinement tests cover those.

## Review Run Execution and Publication (M6)

One machine replaces the hand-written acceptance and refusal decisions of a review run: its preparation, the phase updates the driver makes, the input artifacts, the finalization, and the per-card publication.

| Machine | Definition | Kernel | Scope |
| --- | --- | --- | --- |
| `reviewrun` | `Machines/ReviewRun.lean` | `internal/statemachine/reviewrun` | One run as seen by one member: the phase, the launch status, the execution result, whether the exit code is 0, the three input artifacts, the originals, and the member's `published` and `skipped` flags |

The machine has no parameter and all fields are scalars, so one instance describes one run for one member. The run-level fields are the same in the instance of every member; the shell steps a run-level event on one representative and a publication event on the member's own instance. `phase`, `launch`, and `exec` are the persisted strings of `ReviewRun`; a string that is not one of the persisted values projects to `other`, because `UpdateReviewRun` stores what it is given.

The events are `create` and `retry` (`PrepareReviewRunWithTransaction`: a new run, and the same ID again), `update` (`UpdateReviewRunWithTransaction`), `storePrompt`, `storeContract`, and `storeEvidence` (`StoreReviewArtifactWithTransaction`), `finalize` (`FinalizeReviewRunWithTransaction` and `FinalizeUploadedReview`), and `stage`, `skip`, `verify`, and `passSkipped` (`stageReviewRunCard`, from `PublishReviewRun` and `PublishReviewRunAtomic`). The queries are `final` (`ReadReviewOriginal`, and the finalized check of `PublishReviewRun`), `complete` (`ReviewPublicationComplete`, the previous-run check of `Prepare`, and the coordinator mirror), and `consumable` (a complete run whose execution result is `ok`).

The shell passes as Boolean arguments what it computes and the kernel does not: the identity, the cards, the batch binding with its advance and requirements, the frozen `ReviewInput` (`sameInput`), whether the report is non-empty and a structured report is invalid, the failure reason, whether every original exists, and for a member the group binding, whether the member left the batch, and the integrity of the batch binding, hashes, sidecar, manifest, and index line. It checks them in the order the entrance always has and returns the first failure; the kernel decides what depends on the state of the run.

Proofs, for every run and member:

| Theorem | Statement |
| --- | --- |
| `ReviewRun.update_refused_finalized`, `finalize_refused_finalized`, `finalized_frozen`, `archive_sequences_accepted` | (1) `update` and `finalize` refuse a finalized run, and no accepted event changes its phase, launch status, execution result, exit code, input artifacts, or originals. The kernel accepts every call sequence of the `archiveExecution` driver (prepared, launching, running, finalize; a launch failure that finalizes from launching with the launch status not started; the recovery that finalizes an unfinished run as interrupted from prepared, launching, and running; and an ok run whose report is invalid) |
| `ReviewRun.finalize_ok`, `reachable_ok_facts` | (2) A finalization that leaves the run `ok` had launch status `started`, exit code 0, and a non-empty report, and a reachable run with originals that is `ok` has launch status `started` and exit code 0 |
| `ReviewRun.store_only_prepared`, `artifacts_only_prepared`, `originals_only_finalize` | (3) An input artifact is stored only while the run is `prepared` (the same bytes pass again, other bytes are refused); the originals are written only by an accepted finalization of a run that was not finalized |
| `ReviewRun.retry_spec`, `retry_refused`, `create_refused`, `input_conflict_refused` | (4) A retry changes nothing and is accepted only for an existing run whose frozen input, batch, and requirements hold; a new run needs that no run exists; a different frozen input is refused by every event that carries it |
| `ReviewRun.stage_spec`, `skip_spec`, `publish_once` | (5) A member takes a copy, the manifest, and the index line exactly when `stage` is accepted, and then it was not published, not skipped, and not excluded; a member that left is published and skipped without a copy; a published member is neither staged nor skipped again |
| `ReviewRun.publish_needs_finalized`, `reachable_published_finalized` | (6) Only a finalized run stages, skips, verifies, or passes a member; a published member belongs to a finalized run, and a skipped member is published |
| `ReviewRun.published_frozen` | (7) Once a member is published no accepted event changes its instance, so once every member is published or skipped the run no longer changes |
| `ReviewRun.reachable_inv` | (8) In every reachable state: a run that does not exist is the initial state, a skipped member is published, a published member belongs to a finalized run, the originals belong to a finalized run, and an `ok` run with originals has launch status `started` and exit code 0 |

Known differences from the statement of the theorems, kept so that the production behavior does not change:

- `UpdateReviewRun` stores any phase the caller presents (a rollback, `collecting`, `cleaning`, even `finalized`), so a run can reach `finalized` through `update` without originals. The invariant therefore states `originals → finalized`, not the converse. No production caller does this.
- `UpdateReviewRun` and `FinalizeReviewRun` write the whole run the caller presents, including `Published`, `SkippedMembers`, and `MemberCycles` (`Hashes` is recomputed by a finalization). The kernel models the fields they advance (phase, launch status, execution result, exit code); theorems (5) and (7) and the invariants about `published` and `skipped` hold for the kernel events, not for a caller that forges those fields. `internal/board/review_run_kernel_entrance_test.go` pins this behavior (`TestUpdateReviewRunReplacesPublicationFields`). Every production caller passes the run it read from `Prepare` or `ReadReviewRun`, changing only the phase and the execution facts. Hardening (refusing a run whose publication fields differ from the stored ones) is a separate card.

The shell is `internal/board/review_run_kernel.go`, with the entrances in `reviews.go` (`PrepareReviewRunWithTransaction`, `UpdateReviewRunWithTransaction`, `StoreReviewArtifactWithTransaction`, `FinalizeReviewRunWithTransaction`, `ReviewPublicationComplete`, `ReadReviewOriginal`), `review_publication.go` (`PublishReviewRun`, `PublishReviewRunAtomic`, `stageReviewRunCard`), and `review_upload.go` (`FinalizeUploadedReview`, through the finalization). `internal/server/review_runtime.go` calls these entrances and is not changed here. Projection rules are listed in the header of `review_run_kernel.go`. `internal/board/review_run_kernel_entrance_test.go` drives each entrance on a temporary board and asserts `projection(after) = Step(projection(before), event)` per member, or for a refused entrance an error, a kernel refusal, and an unchanged projection; `internal/review/archive_sequence_test.go` asserts that the calls the execution driver makes are the sequences of theorem (1). The differential vectors use an explicit alphabet of 42 events at depth 4.

## Review Finding Sources, Assignment, and Author Records (M7, M8)

Two machines replace the hand-written decisions about where the findings of a run come from, how they are assigned, and which author records a card accepts. One machine cannot hold both: a run with no findings has no (finding, card) pair, so the run-level facts and the pair-level facts are separate instances.

| Machine | Definition | Kernel | Scope |
| --- | --- | --- | --- |
| `reviewfindings` | `Machines/ReviewFindings.lean` | `internal/statemachine/reviewfindings` | One run: its schema (`legacy` for 0 or less, `report` for 1 and the other positive values, `receiver` for 2), whether its report is structured or holds the block marker, whether an interpretation or a legacy mapping is stored, whether the assignment exists, and whether the batch is closed |
| `reviewdisposition` | `Machines/ReviewDisposition.lean` | `internal/statemachine/reviewdisposition` | One finding and one card: whether the assignment gives the finding to the card, whether the ledger holds a record for the pair, and whether the batch is closed |

The machines share no state. The shell passes the run-level facts to a pair event as Boolean arguments (`ledger`: the assignment exists; `available`: the findings of the run can be obtained). Neither machine has a parameter, and every field is a scalar, so the proofs hold for every number of findings, members, and records: those numbers only enter through the Boolean facts the shell computes from the persisted records (`covers`, `members`, `active`, `owners`, `lineage`, `same`, `dup`, `ref`).

`reviewfindings` has the events `open` (the run is read in once, for a consumable run; a structured report holds the block marker), `close` (the batch closes), `interpret` (`InterpretReviewWithTransaction`), `mapLegacy` (`MapLegacyReviewWithTransaction`), and `assign` (`AssignReviewFindingsWithTransaction`). Its queries are `available` (the findings can be obtained: a receiver run has an interpretation or a parsable report, a report run a parsable report, a legacy run a parsable report or a mapping), `pending` (a receiver run with neither: `ReviewNeedsInterpretation`), `writable` (read in and not closed: `reviewRunForMutation`), `interpWritable`, and `legacySchema`.

`reviewdisposition` has the events `assign` (the pair is assigned), `close`, `submit` (an author record that is not a withdrawal: `SubmitReviewDispositionWithTransaction`, `appendDisposition`, `validateDisposition`, and the replay of `readDispositionLedger`), and `withdraw` (`withdrawReviewFinding`, `requireWithdrawalBinding`). `status` is the persisted status string (`other` is any string outside the seven values); `gate` is whether the tier of the finding is `blocking`, `high`, or `medium`; `security` is whether the role of the run is `Security`, `CSA`, or `Hacker`. `dup` relates the presented record to the ledger (no record with its identifier, an identical record, a different one) and `ref` relates its `previous_record_id` to the chain (empty, the last record, another record). The identifier scan, the chain, and the withdrawal binding are computed by the shell and decided by the kernel.

The shell passes what it computes and the kernel does not: the report hash, the text validation of an interpretation, a mapping, and an author record, the lineage of the findings, the recorded content and the stored copies, the execution authorization and the card revision, the fix and waiver evidence, and the time check of a `timed-out` waiver. It checks them in the order the entrance always has and returns the first failure; the kernel decides what depends on the state of the run or of the pair. Where the entrance has to name which state-dependent check failed, it asks the kernel with every other fact holding (a probe) and returns the error of the check the kernel refuses. The facts that the entrance computes before the report is read (the provenance of an assignment) go to the kernel as true.

Proofs, for every run, finding, and card:

| Theorem | Statement |
| --- | --- |
| `ReviewFindings.interpret_refused_schema`, `mapLegacy_refused_schema`, `mapLegacy_refused_fenced`, `interpret_refused_structured`, `reachable_structured_no_source`, `reachable_one_source` | (1) A run has at most one source and only the one its schema allows: an interpretation only for a receiver run whose report is not structured, a legacy mapping only for a legacy run whose report holds no block marker; a schema 0 report takes no interpretation, a schema 2 report no legacy mapping, a structured report neither |
| `ReviewFindings.interpret_spec`, `mapLegacy_spec`, `interpret_replay`, `interpret_conflict`, `mapLegacy_replay`, `mapLegacy_conflict`, `assign_replay`, `assign_conflict`, `written_frozen` | (2) A written interpretation, legacy mapping, or assignment never changes: while the batch is open the same record again is accepted and changes nothing (an assignment after its owners are frozen again from the current cards), and a different record is refused; no accepted event clears a source, the assignment, or the closing |
| `ReviewFindings.assign_refused_unavailable`, `assign_refused_pending`, `reachable_pending_unassigned`; `ReviewDisposition.submit_refused_no_findings`, `withdraw_refused_no_findings` | (3) A run whose findings are not available (a receiver run waiting for its interpretation among them) takes no assignment, so it has none, and a record needs the assignment and the findings |
| `ReviewFindings.assign_spec`, `assign_refused_coverage`, `assign_refused_foreign`, `assign_refused_excluded` | (4) An accepted assignment covers every finding, gives findings only to run members, gives none to an excluded member, and gives every assigned card an owner |
| `ReviewDisposition.submit_spec`, `submit_refused_unassigned`, `submit_refused_gate_deferred`, `submit_refused_nongate`, `submit_refused_waiver_role`, `submit_refused_other_status`, `reachable_chain_assigned` | (5) A record is accepted only for a card the finding is assigned to; a gate finding cannot be deferred; a finding below the gate takes only `fixed`, `deferred`, or `rejected`; a waiver is for a security role; a status outside the seven values is refused; a pair has records only when it is assigned |
| `ReviewDisposition.submit_chain_spec`, `withdraw_chain_spec`, `submit_stale_predecessor`, `submit_replay`, `submit_conflict`, `withdraw_conflict`, `submit_same_needs_chain` | (6) The records of a pair form a chain: a new record names the last record (or nothing for an empty chain), otherwise it is refused; the same record again is accepted and changes nothing; a different record with a stored identifier is refused |
| `ReviewDisposition.submit_refuses_withdrawn`, `withdraw_spec`, `withdraw_refused_not_excluded`, `withdraw_refused_unnamed`, `withdraw_refused_unassigned` | (7) A withdrawal is accepted only for an excluded member and names its current exclusion; `submit` never writes the status `withdrawn` |
| `ReviewFindings.closed_writes_refused`, `interpret_closed`; `ReviewDisposition.closed_refused` | (8) Once the batch is closed no legacy mapping, assignment, author record, or withdrawal is accepted; an interpretation only as the replay of the stored one, which changes nothing (`InterpretReviewWithTransaction` compares with the stored interpretation before it checks the closing) |
| `ReviewFindings.reachable_inv`, `ReviewDisposition.reachable_inv` | (9) In every reachable state: a run that was not read in is the initial state, a structured report holds the block marker, an interpretation or a legacy mapping belongs to its schema and report, an assignment belongs to a run whose findings are available, a closed batch belongs to a run that was read in; a pair has records only when it is assigned |

Behavior the theorems state and the code already had, recorded so that no later card changes it by accident:

- `InterpretReviewWithTransaction` reads the stored interpretation before it checks that the batch is open, so the same interpretation again is accepted after the batch closes; the other three entrances and the withdrawal check the batch first.
- `validateInterpretation` and `validateLegacyMap` keep their schema and block-marker terms: they also validate stored records on every read, so they stay text validations. The kernel decides the same rules from the state of the run and agrees by construction.
- The pair projection takes `closed` as false, because `reviewRunForMutation` refuses a closed batch before the pair is read; the closing is decided by `reviewfindings.Writable` there and proved for both machines.
- `receiverFindings` is folded into `loadReviewFindings`, which reads the report original, the interpretation of a receiver run, and the legacy mapping of a legacy run whose report does not parse, and projects them; `runFindings` and `ReviewNeedsInterpretation` ask the kernel for `available` and `pending`.

The shell is `internal/board/review_findings_kernel.go` (projection, the loader, and the event constructors) with the entrances in `review_interpretation.go` (`InterpretReviewWithTransaction`, `ReviewNeedsInterpretation`) and `review_disposition.go` (`runFindings`, `reviewRunForMutation`, `MapLegacyReviewWithTransaction`, `AssignReviewFindingsWithTransaction`, `SubmitReviewDispositionWithTransaction`, `appendDisposition`, `validateDisposition`, `withdrawReviewFinding`, `requireWithdrawalBinding`, `readDispositionLedger`). `internal/server/review_actions.go` and `review_evidence.go` call these entrances and are not changed here. Projection rules are listed in the header of `review_findings_kernel.go`. `review_findings_kernel_entrance_test.go` and `review_disposition_kernel_entrance_test.go` drive each entrance on a temporary board and assert `projection(after) = Step(projection(before), event)`, or for a refused entrance an error, a kernel refusal of the same event, and an unchanged projection. The differential vectors use explicit alphabets of 27 events (`reviewfindings`) and 47 events (`reviewdisposition`) at depth 4.

## v2 State Machine Inventory

M1 to M14 are the machines of `20261103-state-machine-simplification-audit-task`. Group A is `20261114-formal-kernel-foundation-group`; group B is `20261116-formal-dispatch-group`; group C is `20261116-formal-review-evidence-group`; group D is created after group C integrates into `v2`.

| Machine | Scope | Owner |
| --- | --- | --- |
| M1 | Card lifecycle, contract freezing, member exclusion | Proved and generated: `cardlifecycle` (A, `20261114-formal-card-lifecycle-task`) |
| M2 | Execution ownership: start, claim, takeover, execution chain | Proved and generated: `executionledger` (A, `20261114-formal-card-lifecycle-task`) |
| v2 server single-card claim record and lease expiry (`internal/server/claims.go`) | Execution ownership on the server | Proved and generated: `serverclaim` (A, `20261114-formal-card-lifecycle-task`) |
| M3 | Durable dispatch, rounds, accept and complete receipts | Proved and generated: `dispatch` (B, `20261116-formal-dispatch-rounds-task`) |
| M4 | Wrap-up-only capability grant | Proved and generated: `dispatch` (B, `20261116-formal-dispatch-grant-settlement-task`) |
| M5 | Settlement of completed dispatches | Proved and generated: `dispatch` (B, `20261116-formal-dispatch-grant-settlement-task`) |
| M6 | Review run execution and per-card publication | Proved and generated: `reviewrun` (C, `20261116-formal-review-run-task`) |
| M7 | Report interpretation and legacy mapping | Proved and generated: `reviewfindings` (C, `20261116-formal-review-findings-task`) |
| M8 | Finding assignment, author disposition, withdrawal | Proved and generated: `reviewfindings`, `reviewdisposition` (C, `20261116-formal-review-findings-task`) |
| M9 | Review batch target advance, aggregation, closure | Proved and generated: `reviewplan` (A, `20261114-formal-review-obligations-task`) |
| M10 | Review plan, cycle binding, unplanned batch admission | Proved and generated: `reviewplan`, `reviewobligation` (A, `20261114-formal-review-obligations-task`) |
| M11 | Review obligations: new cycle, drop, reset | Proved and generated: `reviewobligation` (A, `20261114-formal-review-obligations-task`) |
| M12 | Coordinator authority, mirror, recovery, Git witnesses | C |
| M13 | Board multi-file transactions, revisions, form migration recovery | Assumption, not proved |
| M14 | Archive evidence cleanup | Assumption, not proved |
| v2 server group leases (`internal/server/group_leases.go`) | Group coordination leases | D |
| v2 server integration operation (`internal/server/integration*.go`) | Group integration | D |
| v2 server issue results (`internal/server/issue_results.go`) | Issue result publication | D |
| v2 server deliveries and dispatch requests (`internal/server/deliveries.go`, `dispatch_request.go`, `requests.go`) | Delivery records and requested dispatches | D |
