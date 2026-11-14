import KanderFormal.Obligations
import KanderFormal.Machines.ReviewObligation

/-
Proof obligations of the review obligation machine.
-/
namespace KanderFormal.Proofs.ReviewObligation

open KanderFormal KanderFormal.Machines.ReviewObligation

abbrev M := reviewObligationMachine

theorem wellTyped : M.wellTyped = true := by decide

end KanderFormal.Proofs.ReviewObligation
