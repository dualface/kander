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
| `formal/KanderFormal/Proofs/*.lean` | Machine proofs |
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

## v2 State Machine Inventory

M1 to M14 are the machines of `20261103-state-machine-simplification-audit-task`. Group A is `20261114-formal-kernel-foundation-group`; groups B, C, and D are created after group A integrates into `v2`.

| Machine | Scope | Owner |
| --- | --- | --- |
| M1 | Card lifecycle, contract freezing, member exclusion | A (`20261114-formal-card-lifecycle-task`) |
| M2 | Execution ownership: start, claim, takeover, execution chain | A (`20261114-formal-card-lifecycle-task`) |
| v2 server single-card claim record and lease expiry (`internal/server/claims.go`) | Execution ownership on the server | A, with M2 |
| M3 | Durable dispatch, rounds, accept and complete receipts | B |
| M4 | Wrap-up-only capability grant | B |
| M5 | Settlement of completed dispatches | B |
| M6 | Review run execution and per-card publication | C |
| M7 | Report interpretation and legacy mapping | C |
| M8 | Finding assignment, author disposition, withdrawal | C |
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
