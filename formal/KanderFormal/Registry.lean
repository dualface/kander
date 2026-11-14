import KanderFormal.Proofs.Example
import KanderFormal.Proofs.ReviewObligation
import KanderFormal.Proofs.ReviewPlan

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
  ⟨"close", [natV 0, roles true true]⟩,
  ⟨"close", [natV 0, roles false false]⟩,
  ⟨"close", [natV 1, roles true false]⟩,
  ⟨"newCycle", []⟩,
  ⟨"reset", []⟩,
  ⟨"rebind", []⟩,
  ⟨"drop", [natV 1, boolV false]⟩,
  ⟨"drop", [natV 1, boolV true]⟩,
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
      ``KanderFormal.Proofs.ReviewObligation.wellTyped
    ] },
  { machine := Machines.ReviewPlan.reviewPlanMachine
    source := "KanderFormal/Machines/ReviewPlan.lean"
    vectors := { depth := 5, paramSets := [[("maxBatch", 2)]], natDomain := [], events := planEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewPlan.wellTyped
    ] }
]

end KanderFormal.Registry
