import KanderFormal.Proofs.Example
import KanderFormal.Proofs.ReviewObligation
import KanderFormal.Proofs.ReviewPlan
import KanderFormal.Proofs.CardLifecycle
import KanderFormal.Proofs.ExecutionLedger
import KanderFormal.Proofs.ServerClaim
import KanderFormal.Proofs.Dispatch
import KanderFormal.Proofs.DispatchForms
import KanderFormal.Proofs.DispatchSafety
import KanderFormal.Proofs.DispatchGrant
import KanderFormal.Proofs.ReviewRun

/-
The registry of machines: what the generator emits, the bounds of the
differential vectors, and the theorems the axiom audit checks. A new machine is
added here (see docs/formal-verification.md).
-/
namespace KanderFormal.Registry

open Lean (Name)

/-- Bounds of the differential vectors. The Go replay walks every event
sequence of length `depth` over `events` from the initial state of each
parameter set. -/
structure VectorBounds where
  depth : Nat
  paramSets : List (List (String × Nat))
  /-- Values tried for every `nat` event argument. -/
  natDomain : List Nat
  /-- When not empty, the event alphabet, replacing every event with every
  argument combination; for machines whose full alphabet is too large to walk. -/
  events : List Event := []
  deriving Inhabited

structure Entry where
  machine : Machine
  /-- The definition file, relative to `formal/`; its SHA-256 goes into the
  generated file header. -/
  source : String
  vectors : VectorBounds
  /-- Machine-specific theorems: well-typedness and the proof obligations. -/
  theorems : List Name
  deriving Inhabited

/-- Theorems that hold for every machine (`Semantics.lean`, `Typing.lean`,
`Obligations.lean`). -/
def genericTheorems : List Name := [
  ``KanderFormal.run_refused_unchanged,
  ``KanderFormal.run_accepted_iff,
  ``KanderFormal.step_deterministic,
  ``KanderFormal.run_deterministic,
  ``KanderFormal.step_assign_reads_prestate,
  ``KanderFormal.wellTyped_assign_reads_prestate,
  ``KanderFormal.step_unassigned_unchanged,
  ``KanderFormal.step_refuses_out_of_bound,
  ``KanderFormal.step_preserves_conforms,
  ``KanderFormal.initial_conforms,
  ``KanderFormal.SafetyCert.reachable,
  ``KanderFormal.reachable_conforms
]

/-- Role sets of the production roles (PMQA, Security). -/
def roles (pmqa security : Bool) : Val := .set [pmqa, security]

/-- The obligation vector alphabet: the events with the argument values that
reach every expression form within the depth. -/
def obligationEvents : List Event := [
  ⟨"create", []⟩,
  ⟨"addBatch", [boolV true, roles true false]⟩,
  ⟨"addBatch", [boolV true, roles false false]⟩,
  ⟨"addBatch", [boolV false, roles false true]⟩,
  ⟨"close", [natV 0, roles true true, roles true true]⟩,
  ⟨"close", [natV 0, roles false false, roles false false]⟩,
  ⟨"close", [natV 1, roles true false, roles true false]⟩,
  ⟨"close", [natV 1, roles false false, roles true false]⟩,
  ⟨"newCycle", []⟩,
  ⟨"reset", []⟩,
  ⟨"rebind", []⟩,
  ⟨"drop", [natV 1, boolV false]⟩,
  ⟨"drop", [natV 1, boolV true]⟩,
  ⟨"dropTakes", [boolV false]⟩,
  ⟨"dropTakes", [boolV true]⟩,
  ⟨"releaseDrop", []⟩,
  ⟨"releaseRecovery", []⟩,
  ⟨"done", [boolV true]⟩,
  ⟨"done", [boolV false]⟩
]

/-- The plan vector alphabet: each event with every fact holding, and with one
fact failing for the events whose facts the integrity theorem covers. -/
def planEvents : List Event :=
  let t := boolV true
  let x := boolV false
  [ ⟨"create", [natV 1, x, t, t, t, t]⟩,
    ⟨"create", [natV 2, t, t, t, t, t]⟩,
    ⟨"create", [natV 1, t, x, t, t, t]⟩,
    ⟨"extend", [t, x, x, x, x, x, x]⟩,
    ⟨"extend", [t, t, x, x, x, x, x]⟩,
    ⟨"extend", [x, x, x, x, t, x, t]⟩,
    ⟨"admit", [t, x, t, t]⟩,
    ⟨"admit", [t, x, t, x]⟩,
    ⟨"append", [x, x, t, t, t, t, t, t, t]⟩,
    ⟨"append", [x, t, t, t, t, t, t, t, t]⟩,
    ⟨"append", [t, x, t, t, t, t, t, t, t]⟩,
    ⟨"seal", [t, t, t]⟩,
    ⟨"rebind", [t, t, t]⟩,
    ⟨"sync", [t, t, t]⟩,
    ⟨"drop", [natV 1, t, t]⟩,
    ⟨"release", [t, t, t, t]⟩,
    ⟨"resolve", [t, t, t, t, t]⟩,
    ⟨"resolve", [t, t, t, t, x]⟩,
    ⟨"run", [natV 1]⟩,
    ⟨"advance", [natV 0, t, t, t, t, t, t]⟩,
    ⟨"advance", [natV 0, t, x, t, t, t, t]⟩,
    ⟨"aggregate", [natV 0, t, t, t]⟩,
    ⟨"close", [natV 0, t, t, t, t, t, t, t]⟩,
    ⟨"close", [natV 1, t, t, t, t, t, t, t]⟩,
    ⟨"close", [natV 0, t, x, t, t, t, t, t]⟩ ]


/-- The card lifecycle vector alphabet: every target column, the member flag
where it writes an exclusion, both outcomes of each integrity fact, and each
kind of contract change with and without a decision. -/
def cardEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let move (c : Nat) (m : Val) : Event := ⟨"move", [enumV c, m]⟩
  [ move 0 x, move 1 x, move 2 x, move 3 x, move 4 x, move 5 t, move 5 x, move 6 t,
    ⟨"rollback", [t, t]⟩, ⟨"rollback", [x, t]⟩, ⟨"rollback", [t, x]⟩,
    ⟨"dispatchMove", [t]⟩, ⟨"dispatchMove", [x]⟩,
    ⟨"recover", [t, t, t, t]⟩, ⟨"recover", [t, t, t, x]⟩,
    ⟨"update", [enumV 0, x]⟩, ⟨"update", [enumV 1, x]⟩, ⟨"update", [enumV 2, x]⟩, ⟨"update", [enumV 2, t]⟩ ]

/-- The execution ledger vector alphabet: each entrance at the revisions and
fences its first writes hold, with a stale revision or fence and a failing fact
for the theorems' refusals. -/
def ledgerEvents : List Event :=
  let t := boolV true
  let x := boolV false
  [ ⟨"claim", [natV 0, t, x, x, t]⟩, ⟨"claim", [natV 1, t, x, x, t]⟩, ⟨"claim", [natV 0, x, x, x, t]⟩,
    ⟨"claim", [natV 0, t, t, x, t]⟩, ⟨"start", [natV 0, t]⟩, ⟨"start", [natV 2, t]⟩, ⟨"start", [natV 0, x]⟩,
    ⟨"reclaim", [natV 2, t, t]⟩, ⟨"reclaim", [natV 2, x, t]⟩, ⟨"recover", [natV 1, t, t]⟩,
    ⟨"recover", [natV 2, t, t]⟩, ⟨"relocate", [natV 1, enumV 2]⟩, ⟨"relocate", [natV 2, enumV 1]⟩,
    ⟨"relocate", [natV 1, enumV 3]⟩, ⟨"relocate", [natV 0, enumV 0]⟩, ⟨"write", [natV 1]⟩,
    ⟨"confirm", [natV 1, t, x]⟩, ⟨"confirm", [natV 1, t, t]⟩, ⟨"confirm", [natV 2, t, x]⟩,
    ⟨"unresolve", [natV 1, natV 1, t, t]⟩, ⟨"unresolve", [natV 1, natV 0, t, x]⟩,
    ⟨"adopt", [natV 1, t, t]⟩, ⟨"takeover", [natV 1, t, t]⟩, ⟨"takeover", [natV 1, x, x]⟩,
    ⟨"rollback", [natV 1, natV 1, t, t, enumV 0, enumV 0]⟩,
    ⟨"rollback", [natV 2, natV 2, t, x, enumV 2, enumV 2]⟩,
    ⟨"field", [natV 1, t]⟩, ⟨"field", [natV 2, x]⟩, ⟨"restore", [natV 1, t, t]⟩, ⟨"restore", [natV 1, x, t]⟩ ]

/-- The server claim vector alphabet: holder requests of the first generations,
of no generation, and of an older one; terminations; takeovers with the
current, an older, and a newer observed generation; and the environment events. -/
def serverEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let g (n : Nat) : List Val := [natV n, natV n]
  [ ⟨"claim", [t]⟩, ⟨"claim", [x]⟩, ⟨"renew", g 1⟩, ⟨"renew", g 0⟩, ⟨"write", g 1 ++ [x]⟩, ⟨"write", g 2 ++ [x]⟩,
    ⟨"write", g 0 ++ [t]⟩, ⟨"write", [natV 0, natV 1, t]⟩,
    ⟨"leave", g 1 ++ [x]⟩, ⟨"leave", g 0 ++ [t]⟩, ⟨"leave", g 1 ++ [t]⟩, ⟨"release", g 1⟩, ⟨"rollback", g 1⟩,
    ⟨"takeover", [natV 1, t, x] ++ g 0⟩, ⟨"takeover", [natV 1, t, t] ++ g 1⟩,
    ⟨"takeover", [natV 0, t, x] ++ g 0⟩, ⟨"takeover", [natV 2, t, x] ++ g 0⟩,
    ⟨"expire", []⟩, ⟨"pending", [t]⟩, ⟨"pending", [x]⟩, ⟨"reconcile", [t]⟩, ⟨"reconcile", [x]⟩ ]


/-- The dispatch vector alphabet. Authorization pairs: `(1, 1)` is the first
round at its first epoch, `(1, 2)` its reauthorization, `(2, 1)` the next round,
and `(0, 0)` the empty pair. Each event is tried with every fact holding and with
one fact failing; the enum arguments try a working, a review, and another column. -/
def dispatchEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let a (r e : Nat) : List Val := [natV r, natV e]
  [ ⟨"prepare", [enumV 0, enumV 0, t, t]⟩, ⟨"prepare", [enumV 2, enumV 1, t, t]⟩,
    ⟨"prepare", [enumV 0, enumV 2, t, t]⟩, ⟨"prepare", [enumV 1, enumV 0, x, t]⟩,
    ⟨"prepare", [enumV 0, enumV 0, t, x]⟩,
    ⟨"retry", [t]⟩, ⟨"retry", [x]⟩,
    ⟨"attempt", a 1 1 ++ [t, t, t, t]⟩, ⟨"attempt", a 2 1 ++ [t, t, t, t]⟩, ⟨"attempt", a 1 1 ++ [t, t, t, x]⟩,
    ⟨"attempt", a 1 1 ++ [t, x, t, t]⟩, ⟨"attempt", a 1 1 ++ [x, t, t, t]⟩,
    ⟨"reauthorize", a 1 1 ++ [t, t, t, t, t]⟩, ⟨"reauthorize", a 1 1 ++ [t, t, t, x, x]⟩,
    ⟨"reauthorize", a 1 1 ++ [t, t, t, x, t]⟩, ⟨"reauthorize", a 1 1 ++ [t, x, t, t, t]⟩,
    ⟨"reauthorize", a 1 2 ++ [t, t, t, t, t]⟩,
    ⟨"accept", a 1 1 ++ [t, t, t]⟩, ⟨"accept", a 1 1 ++ [t, t, x]⟩, ⟨"accept", a 1 2 ++ [t, t, t]⟩,
    ⟨"accept", a 1 1 ++ [x, t, t]⟩,
    ⟨"complete", a 1 1 ++ [t, t, x, t, t, t, x, enumV 0]⟩, ⟨"complete", a 1 1 ++ [t, t, t, t, t, t, x, enumV 0]⟩,
    ⟨"complete", a 1 1 ++ [t, t, x, t, t, t, t, enumV 1]⟩, ⟨"complete", a 1 1 ++ [t, t, x, t, t, t, x, enumV 2]⟩,
    ⟨"complete", a 1 2 ++ [t, t, x, t, t, t, x, enumV 0]⟩, ⟨"complete", a 1 1 ++ [t, t, x, x, t, t, x, enumV 0]⟩,
    ⟨"end", a 1 1 ++ [t, t, t, t, t]⟩, ⟨"end", a 1 1 ++ [t, t, t, t, x]⟩, ⟨"end", a 1 1 ++ [t, t, t, x, t]⟩,
    ⟨"end", a 1 1 ++ [t, t, t, x, x]⟩, ⟨"end", a 2 1 ++ [t, t, t, t, t]⟩, ⟨"end", a 1 1 ++ [t, t, x, t, t]⟩,
    ⟨"cancel", [t, t, t]⟩, ⟨"cancel", [x, t, t]⟩, ⟨"cancel", [t, x, t]⟩,
    ⟨"retain", a 1 1 ++ [t, t, t, t]⟩, ⟨"retain", a 1 2 ++ [t, t, t, t]⟩, ⟨"retain", a 1 1 ++ [x, t, t, t]⟩,
    ⟨"exec", a 1 1 ++ [t, t, x]⟩, ⟨"exec", a 1 1 ++ [t, t, t]⟩, ⟨"exec", a 1 1 ++ [t, x, x]⟩,
    ⟨"exec", a 0 0 ++ [t, t, x]⟩, ⟨"exec", a 1 2 ++ [t, t, x]⟩,
    -- The wrap-up grant: a stopped, a reclaimed with and without a decision, and
    -- another exit, in a working and another column, with a stale exit, a stale
    -- revision, a failed integrity fact, and the epoch the grant raises to.
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 0, x, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 2, t, t, enumV 0, x, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 1, t, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 1, x, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 2, t, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 0, x, x, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [x, t, enumV 0, t, t, enumV 0, x, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 0, x, t, x]⟩,
    ⟨"authorizeWrapUp", a 1 2 ++ [t, t, enumV 0, t, t, enumV 0, x, t, t]⟩,
    ⟨"attempt", a 1 2 ++ [t, t, t, t]⟩,
    -- The full-execution write and the record writes: a contract decision, a spec
    -- append that does and does not append, and a record path that is and is not
    -- free of other content.
    ⟨"full", []⟩,
    ⟨"wrapUpWrite", [t, t, t, x, x]⟩, ⟨"wrapUpWrite", [x, t, t, x, x]⟩, ⟨"wrapUpWrite", [x, t, x, t, t]⟩,
    ⟨"wrapUpWrite", [x, x, x, t, t]⟩, ⟨"wrapUpWrite", [x, x, x, x, t]⟩,
    -- The settlement of a completed binding from the three sources.
    ⟨"settle", a 1 1 ++ [t, enumV 0, t]⟩, ⟨"settle", a 1 1 ++ [t, enumV 1, t]⟩, ⟨"settle", a 1 1 ++ [t, enumV 2, t]⟩,
    ⟨"terminateSettle", [t, t, t]⟩, ⟨"terminateSettle", [x, t, t]⟩,
    ⟨"takeoverSettle", a 1 1 ++ [t, t, t, t]⟩, ⟨"takeoverSettle", a 1 2 ++ [t, t, t, t]⟩ ]

/-- The review run vector alphabet: each event with every fact holding and with
one fact failing, the phase updates the driver makes and the odd ones the entrance
accepts, the finalizations of an `ok` run (valid, invalid report, launch status
other than started, empty report, missing originals), a failure with and without
a reason, a launch failure, and an interruption. -/
def runEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let upd (p l e : Nat) (z s : Bool) : Event := ⟨"update", [enumV p, enumV l, enumV e, boolV z, boolV s]⟩
  let fin (l e : Nat) (z r why inv cm : Bool) : Event :=
    ⟨"finalize", [enumV l, enumV e, boolV z, boolV r, boolV why, boolV inv, boolV cm, t]⟩
  [ ⟨"create", [t, t, t, t]⟩, ⟨"create", [x, t, t, t]⟩, ⟨"create", [t, t, t, x]⟩,
    ⟨"retry", [t, t, t, t, t]⟩, ⟨"retry", [t, t, x, t, t]⟩, ⟨"retry", [t, t, t, x, t]⟩,
    upd 1 0 0 true true, upd 2 1 0 true true, upd 3 2 0 true true, upd 4 2 0 true true, upd 6 2 1 true true,
    upd 0 0 0 true true, upd 7 3 5 false true, upd 3 2 0 true false,
    ⟨"storePrompt", [t]⟩, ⟨"storePrompt", [x]⟩, ⟨"storeContract", [t]⟩, ⟨"storeEvidence", [t]⟩,
    fin 2 1 true true false false true, fin 2 1 true true false true true, fin 1 1 true true false false true,
    fin 2 1 true false false false true, fin 2 3 false false true false true, fin 2 3 false false false false true,
    fin 0 2 false false true false true, fin 1 4 false false true false true, fin 2 1 true true false false false,
    ⟨"finalize", [enumV 2, enumV 1, t, t, x, x, t, x]⟩,
    ⟨"stage", [t, t, x, t, t]⟩, ⟨"stage", [t, t, t, t, t]⟩, ⟨"stage", [t, x, x, t, t]⟩, ⟨"stage", [x, t, x, t, t]⟩,
    ⟨"stage", [t, t, x, x, t]⟩, ⟨"stage", [t, t, x, t, x]⟩,
    ⟨"skip", [t, t, t]⟩, ⟨"skip", [t, t, x]⟩, ⟨"skip", [t, x, t]⟩,
    ⟨"verify", [t, t, t, t]⟩, ⟨"verify", [t, t, t, x]⟩, ⟨"verify", [t, t, x, t]⟩,
    ⟨"passSkipped", [t, t]⟩, ⟨"passSkipped", [t, x]⟩ ]

def entries : List Entry := [
  { machine := Machines.exampleMachine
    source := "KanderFormal/Machines/Example.lean"
    vectors := { depth := 4, paramSets := [[("maxEpoch", 0)], [("maxEpoch", 1)], [("maxEpoch", 2)]],
                 natDomain := [0, 1, 2, 3] }
    theorems := [
      ``KanderFormal.Proofs.Example.wellTyped,
      ``KanderFormal.Proofs.Example.init,
      ``KanderFormal.Proofs.Example.step_inv,
      ``KanderFormal.Proofs.Example.reachable_inv,
      ``KanderFormal.Proofs.Example.reachable_sealed_passed
    ] },
  { machine := Machines.ReviewObligation.reviewObligationMachine
    source := "KanderFormal/Machines/ReviewObligation.lean"
    vectors := { depth := 5, paramSets := [[("maxBatch", 2)]], natDomain := [], events := obligationEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewObligation.wellTyped,
      ``KanderFormal.Proofs.ReviewObligation.step_enc,
      ``KanderFormal.Proofs.ReviewObligation.debt_safety,
      ``KanderFormal.Proofs.ReviewObligation.evidence_floor,
      ``KanderFormal.Proofs.ReviewObligation.release_once,
      ``KanderFormal.Proofs.ReviewObligation.reachable_inv,
      ``KanderFormal.Proofs.ReviewObligation.counter_dropReleased,
      ``KanderFormal.Proofs.ReviewObligation.counter_releaseDrop,
      ``KanderFormal.Proofs.ReviewObligation.counter_staleClose,
      ``KanderFormal.Proofs.ReviewObligation.witness_members,
      ``KanderFormal.Proofs.ReviewObligation.witness_twoResets,
      ``KanderFormal.Proofs.ReviewObligation.witness_releasedRole,
      ``KanderFormal.Proofs.ReviewObligation.completes_reachable
    ] },
  { machine := Machines.ReviewPlan.reviewPlanMachine
    source := "KanderFormal/Machines/ReviewPlan.lean"
    vectors := { depth := 5, paramSets := [[("maxBatch", 2)]], natDomain := [], events := planEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewPlan.wellTyped,
      ``KanderFormal.Proofs.ReviewPlan.integrity_refused,
      ``KanderFormal.Proofs.ReviewPlan.sealed_append,
      ``KanderFormal.Proofs.ReviewPlan.sealed_seal,
      ``KanderFormal.Proofs.ReviewPlan.sealed_admit,
      ``KanderFormal.Proofs.ReviewPlan.reachable_inv
    ] },
  { machine := Machines.CardLifecycle.cardLifecycleMachine
    source := "KanderFormal/Machines/CardLifecycle.lean"
    vectors := { depth := 5, paramSets := [[("maxDecision", 2)]], natDomain := [], events := cardEvents }
    theorems := [
      ``KanderFormal.Proofs.CardLifecycle.wellTyped,
      ``KanderFormal.Proofs.CardLifecycle.move_accepted_iff,
      ``KanderFormal.Proofs.CardLifecycle.pick_accepted_iff,
      ``KanderFormal.Proofs.CardLifecycle.rollback_spec,
      ``KanderFormal.Proofs.CardLifecycle.dispatchMove_spec,
      ``KanderFormal.Proofs.CardLifecycle.recover_spec,
      ``KanderFormal.Proofs.CardLifecycle.frozen_change,
      ``KanderFormal.Proofs.CardLifecycle.reachable_frozen_change,
      ``KanderFormal.Proofs.CardLifecycle.decisions_only_update,
      ``KanderFormal.Proofs.CardLifecycle.init,
      ``KanderFormal.Proofs.CardLifecycle.step_inv,
      ``KanderFormal.Proofs.CardLifecycle.reachable_inv
    ] },
  { machine := Machines.ExecutionLedger.executionLedgerMachine
    source := "KanderFormal/Machines/ExecutionLedger.lean"
    vectors := { depth := 4, paramSets := [[("maxSeq", 3), ("maxRev", 4)]], natDomain := [], events := ledgerEvents }
    theorems := [
      ``KanderFormal.Proofs.ExecutionLedger.wellTyped,
      ``KanderFormal.Proofs.ExecutionLedger.stale_revision_refused,
      ``KanderFormal.Proofs.ExecutionLedger.unresolve_stale,
      ``KanderFormal.Proofs.ExecutionLedger.stale_fence_refused,
      ``KanderFormal.Proofs.ExecutionLedger.init,
      ``KanderFormal.Proofs.ExecutionLedger.step_inv,
      ``KanderFormal.Proofs.ExecutionLedger.reachable_inv
    ] },
  { machine := Machines.ServerClaim.serverClaimMachine
    source := "KanderFormal/Machines/ServerClaim.lean"
    vectors := { depth := 5, paramSets := [[("maxGen", 3)]], natDomain := [], events := serverEvents }
    theorems := [
      ``KanderFormal.Proofs.ServerClaim.wellTyped,
      ``KanderFormal.Proofs.ServerClaim.holder_unique,
      ``KanderFormal.Proofs.ServerClaim.reachable_valid_held,
      ``KanderFormal.Proofs.ServerClaim.gen_monotone,
      ``KanderFormal.Proofs.ServerClaim.stale_holder_refused,
      ``KanderFormal.Proofs.ServerClaim.stale_terminate_refused,
      ``KanderFormal.Proofs.ServerClaim.stale_takeover_refused,
      ``KanderFormal.Proofs.ServerClaim.init,
      ``KanderFormal.Proofs.ServerClaim.step_inv,
      ``KanderFormal.Proofs.ServerClaim.reachable_inv
    ] },
  { machine := Machines.Dispatch.dispatchMachine
    source := "KanderFormal/Machines/Dispatch.lean"
    vectors := { depth := 4, paramSets := [[("maxRound", 3), ("maxEpoch", 3)]], natDomain := [],
                 events := dispatchEvents }
    theorems := [
      ``KanderFormal.Proofs.Dispatch.wellTyped,
      ``KanderFormal.Proofs.Dispatch.prepare_single,
      ``KanderFormal.Proofs.Dispatch.pair_monotone,
      ``KanderFormal.Proofs.Dispatch.prepare_strict,
      ``KanderFormal.Proofs.Dispatch.reauthorize_strict,
      ``KanderFormal.Proofs.Dispatch.fence_refused,
      ``KanderFormal.Proofs.Dispatch.accept_change,
      ``KanderFormal.Proofs.Dispatch.complete_change,
      ``KanderFormal.Proofs.Dispatch.complete_kind,
      ``KanderFormal.Proofs.Dispatch.accept_replay,
      ``KanderFormal.Proofs.Dispatch.complete_replay,
      ``KanderFormal.Proofs.Dispatch.terminal_stable,
      ``KanderFormal.Proofs.Dispatch.terminal_refused,
      ``KanderFormal.Proofs.Dispatch.end_spec,
      ``KanderFormal.Proofs.Dispatch.end_once,
      ``KanderFormal.Proofs.Dispatch.release_once,
      ``KanderFormal.Proofs.Dispatch.reauthorize_spec,
      ``KanderFormal.Proofs.Dispatch.cycle_refused,
      ``KanderFormal.Proofs.Dispatch.init,
      ``KanderFormal.Proofs.Dispatch.step_inv,
      ``KanderFormal.Proofs.Dispatch.reachable_inv,
      ``KanderFormal.Proofs.Dispatch.reachable_unbound,
      ``KanderFormal.Proofs.Dispatch.reachable_receipts,
      ``KanderFormal.Proofs.Dispatch.reachable_unbound_settled,
      ``KanderFormal.Proofs.Dispatch.authorize_spec,
      ``KanderFormal.Proofs.Dispatch.grant_refused,
      ``KanderFormal.Proofs.Dispatch.wrapUpWrite_spec,
      ``KanderFormal.Proofs.Dispatch.authorize_fences_previous,
      ``KanderFormal.Proofs.Dispatch.grant_complete_done,
      ``KanderFormal.Proofs.Dispatch.authorize_complete_done,
      ``KanderFormal.Proofs.Dispatch.settle_spec,
      ``KanderFormal.Proofs.Dispatch.terminate_settle_spec,
      ``KanderFormal.Proofs.Dispatch.takeover_settle_spec,
      ``KanderFormal.Proofs.Dispatch.settle_once,
      ``KanderFormal.Proofs.Dispatch.settled_fences,
      ``KanderFormal.Proofs.Dispatch.release_settle_exclusive,
      ``KanderFormal.Proofs.Dispatch.reachable_grant_kind,
      ``KanderFormal.Proofs.Dispatch.cycle_refused_grant
    ] },
  { machine := Machines.ReviewRun.reviewRunMachine
    source := "KanderFormal/Machines/ReviewRun.lean"
    vectors := { depth := 4, paramSets := [[]], natDomain := [], events := runEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewRun.wellTyped,
      ``KanderFormal.Proofs.ReviewRun.update_refused_finalized,
      ``KanderFormal.Proofs.ReviewRun.finalize_refused_finalized,
      ``KanderFormal.Proofs.ReviewRun.finalized_frozen,
      ``KanderFormal.Proofs.ReviewRun.archive_sequences_accepted,
      ``KanderFormal.Proofs.ReviewRun.finalize_ok,
      ``KanderFormal.Proofs.ReviewRun.reachable_ok_facts,
      ``KanderFormal.Proofs.ReviewRun.store_only_prepared,
      ``KanderFormal.Proofs.ReviewRun.artifacts_only_prepared,
      ``KanderFormal.Proofs.ReviewRun.originals_only_finalize,
      ``KanderFormal.Proofs.ReviewRun.retry_spec,
      ``KanderFormal.Proofs.ReviewRun.retry_refused,
      ``KanderFormal.Proofs.ReviewRun.create_refused,
      ``KanderFormal.Proofs.ReviewRun.stage_spec,
      ``KanderFormal.Proofs.ReviewRun.skip_spec,
      ``KanderFormal.Proofs.ReviewRun.publish_once,
      ``KanderFormal.Proofs.ReviewRun.publish_needs_finalized,
      ``KanderFormal.Proofs.ReviewRun.input_conflict_refused,
      ``KanderFormal.Proofs.ReviewRun.published_frozen,
      ``KanderFormal.Proofs.ReviewRun.init,
      ``KanderFormal.Proofs.ReviewRun.step_inv,
      ``KanderFormal.Proofs.ReviewRun.reachable_inv,
      ``KanderFormal.Proofs.ReviewRun.reachable_published_finalized
    ] }
]

end KanderFormal.Registry
