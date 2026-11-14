import KanderFormal.Proofs.Example

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
    ] }
]

end KanderFormal.Registry
