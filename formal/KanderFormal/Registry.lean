import KanderFormal.Proofs.Example
import KanderFormal.Proofs.ReviewObligation
import KanderFormal.Proofs.ReviewPlan
import KanderFormal.Proofs.CardLifecycle
import KanderFormal.Proofs.ExecutionLedger
import KanderFormal.Proofs.ServerClaim

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
    ⟨"field", [natV 1, t]⟩, ⟨"field", [natV 2, x]⟩ ]

/-- The server claim vector alphabet: holder requests of the first generations,
of no generation, and of an older one; terminations; takeovers with the
current, an older, and a newer observed generation; and the environment events. -/
def serverEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let g (n : Nat) : List Val := [natV n, natV n]
  [ ⟨"claim", [t]⟩, ⟨"claim", [x]⟩, ⟨"renew", g 1⟩, ⟨"renew", g 0⟩, ⟨"write", g 1⟩, ⟨"write", g 2⟩,
    ⟨"leave", g 1 ++ [x]⟩, ⟨"leave", g 0 ++ [t]⟩, ⟨"leave", g 1 ++ [t]⟩, ⟨"release", g 1⟩, ⟨"rollback", g 1⟩,
    ⟨"takeover", [natV 1, t, x] ++ g 0⟩, ⟨"takeover", [natV 1, t, t] ++ g 1⟩,
    ⟨"takeover", [natV 0, t, x] ++ g 0⟩, ⟨"takeover", [natV 2, t, x] ++ g 0⟩,
    ⟨"expire", []⟩, ⟨"pending", [t]⟩, ⟨"pending", [x]⟩, ⟨"reconcile", [t]⟩, ⟨"reconcile", [x]⟩ ]

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
    ] }
]

end KanderFormal.Registry
