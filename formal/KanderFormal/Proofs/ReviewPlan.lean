import KanderFormal.Obligations
import KanderFormal.Machines.ReviewPlan

/-
Proof obligations of the review plan machine.
-/
namespace KanderFormal.Proofs.ReviewPlan

open KanderFormal KanderFormal.Machines.ReviewPlan

abbrev M := reviewPlanMachine

theorem wellTyped : M.wellTyped = true := by decide

end KanderFormal.Proofs.ReviewPlan
