import KanderFormal.Semantics
import KanderFormal.Proofs.StartOrder

/-
The differential vector alphabet and the audited theorems of the start order
machine. They live in their own file so that the registry stays below the file
size limit.
-/
namespace KanderFormal.Registry

open Lean (Name)

/-- The start order alphabet: every event with the target, task, due and lost
facts both ways, and the generations 0 (none), 1 (the operation's) and 2
(another one) where they decide. -/
def startOrderEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let n := natV
  [ ⟨"create", []⟩, ⟨"accept", [t]⟩, ⟨"accept", [x]⟩,
    ⟨"claim", [t, t, n 1]⟩, ⟨"claim", [t, t, n 2]⟩, ⟨"claim", [x, t, n 1]⟩, ⟨"claim", [t, x, n 1]⟩,
    ⟨"claim", [t, t, n 0]⟩,
    ⟨"confirm", [n 1]⟩, ⟨"confirm", [n 2]⟩, ⟨"rollback", [n 1]⟩, ⟨"rollback", [n 2]⟩,
    ⟨"supersede", [n 1]⟩, ⟨"supersede", [n 2]⟩,
    ⟨"fail", [t]⟩, ⟨"fail", [x]⟩, ⟨"cancel", []⟩, ⟨"expire", [t]⟩, ⟨"expire", [x]⟩,
    ⟨"hostLost", [t]⟩, ⟨"hostLost", [x]⟩, ⟨"preempt", []⟩ ]

/-- The audited theorems of the start order machine. -/
def startOrderTheorems : List Name := [
  ``KanderFormal.Proofs.StartOrder.wellTyped,
  ``KanderFormal.Proofs.StartOrder.terminal_stable,
  ``KanderFormal.Proofs.StartOrder.claim_spec,
  ``KanderFormal.Proofs.StartOrder.claim_refused,
  ``KanderFormal.Proofs.StartOrder.bind_once,
  ``KanderFormal.Proofs.StartOrder.started_only_confirm,
  ``KanderFormal.Proofs.StartOrder.pending_expires,
  ``KanderFormal.Proofs.StartOrder.unbound_host_lost,
  ``KanderFormal.Proofs.StartOrder.bound_ends,
  ``KanderFormal.Proofs.StartOrder.cancel_spec,
  ``KanderFormal.Proofs.StartOrder.init,
  ``KanderFormal.Proofs.StartOrder.step_inv,
  ``KanderFormal.Proofs.StartOrder.reachable_inv,
  ``KanderFormal.Proofs.StartOrder.reachable_started_bound,
  ``KanderFormal.Proofs.StartOrder.reachable_open_exit
]

end KanderFormal.Registry
