import KanderFormal.Semantics
import KanderFormal.Proofs.ServerIntegrationFlow

/-
The differential vector alphabet and the audited theorems of the server
integration machine. They live in their own file so that the registry stays
below the file size limit.
-/
namespace KanderFormal.Registry

open Lean (Name)

/-- The server integration vector alphabet. Owner requests are the headers of
the first and second generation (`g`, `tg`), with the actor's lease of the same
generation as the authority; a second family has a stale header, a token of
another generation, a missing header, another client, or an actor that is not
current. Claims try the first generation, the next one over a record, a contained
request, a purpose, and every refusal of the guard; the actions name the
revisions of the records the claims reach, with and without room. -/
def integrationEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let own (g tg : Nat) : List Val := [t, natV g, natV tg, t]
  let lease (g ag : Nat) : List Val := own g g ++ [t, natV ag, t, t]
  let act (g : Nat) (er : Nat) (room : Bool) : List Val := lease g g ++ [natV er, boolV room]
  let claim (eg er ag pu : Nat) (ct : Bool) : Event :=
    ⟨"claim", [natV eg, natV er, t, t, x, t, t, natV ag, enumV pu, boolV ct]⟩
  [ claim 0 0 1 3 false, claim 0 0 1 3 true, claim 0 0 2 2 false, claim 1 1 1 3 false, claim 1 2 2 3 false,
    claim 1 3 1 3 true, claim 0 1 1 3 false,
    ⟨"claim", [natV 0, natV 0, t, t, t, t, t, natV 1, enumV 3, x]⟩,
    ⟨"claim", [natV 0, natV 0, t, t, x, x, t, natV 1, enumV 3, x]⟩,
    ⟨"claim", [natV 0, natV 0, t, t, x, t, x, natV 1, enumV 3, x]⟩,
    ⟨"claim", [natV 0, natV 0, x, t, x, t, t, natV 1, enumV 3, x]⟩,
    ⟨"claim", [natV 0, natV 0, t, x, x, t, t, natV 1, enumV 3, x]⟩,
    ⟨"claim", [natV 0, natV 0, t, t, x, t, t, natV 1, enumV 0, x]⟩,
    ⟨"claimGuard", [t, t, t]⟩, ⟨"claimGuard", [x, t, t]⟩, ⟨"claimGuard", [t, t, x]⟩,
    ⟨"require", lease 1 1⟩, ⟨"require", lease 2 2⟩, ⟨"require", lease 1 2⟩, ⟨"require", own 1 2 ++ [t, natV 1, t, t]⟩,
    ⟨"require", [x, natV 1, natV 1, t, t, natV 1, t, t]⟩, ⟨"require", own 1 1 ++ [x, natV 1, t, t]⟩,
    ⟨"requireOwner", own 1 1⟩, ⟨"requireOwner", own 2 2⟩, ⟨"requireOwner", own 1 2⟩,
    ⟨"renew", lease 1 1 ++ [t]⟩, ⟨"renew", lease 2 2 ++ [t]⟩, ⟨"renew", lease 1 1 ++ [x]⟩,
    ⟨"check", act 1 1 true⟩, ⟨"check", act 2 3 true⟩, ⟨"check", act 1 0 true⟩, ⟨"check", act 1 1 false⟩,
    ⟨"checkOwner", own 1 1 ++ [natV 1, t]⟩, ⟨"checkOwner", own 1 1 ++ [natV 0, t]⟩,
    ⟨"pushing", act 1 1 true ++ [t, t]⟩, ⟨"pushing", act 1 1 true ++ [t, x]⟩, ⟨"pushing", act 1 2 true ++ [t, t]⟩,
    ⟨"pushing", act 2 3 true ++ [t, t]⟩, ⟨"pushing", act 1 1 false ++ [t, t]⟩, ⟨"pushing", act 1 1 true ++ [x, t]⟩,
    ⟨"confirm", act 1 1 true ++ [t, t, t]⟩, ⟨"confirm", act 1 2 true ++ [t, t, t]⟩, ⟨"confirm", act 1 3 true ++ [t, t, t]⟩,
    ⟨"confirm", act 2 3 true ++ [t, t, t]⟩, ⟨"confirm", act 1 1 true ++ [x, t, t]⟩,
    ⟨"confirm", act 1 1 true ++ [t, x, t]⟩, ⟨"confirm", act 1 1 true ++ [t, t, x]⟩,
    ⟨"release", own 1 1 ++ [natV 1, t, t, t, x]⟩, ⟨"release", own 1 1 ++ [natV 2, t, t, t, x]⟩,
    ⟨"release", own 1 1 ++ [natV 3, t, t, x, t]⟩, ⟨"release", own 2 2 ++ [natV 3, t, t, x, t]⟩,
    ⟨"release", own 1 1 ++ [natV 1, t, x, t, x]⟩, ⟨"release", own 1 1 ++ [natV 1, t, t, x, x]⟩,
    ⟨"release", own 1 1 ++ [natV 1, x, t, t, x]⟩,
    ⟨"guard", lease 1 1 ++ [t, t]⟩, ⟨"guard", lease 2 2 ++ [t, t]⟩, ⟨"guard", lease 1 1 ++ [x, t]⟩,
    ⟨"guardRelease", own 1 1 ++ [t]⟩, ⟨"guardRelease", own 1 1 ++ [x]⟩,
    ⟨"completion", [t, x, x, t, t]⟩, ⟨"completion", [t, t, t, x, x]⟩, ⟨"completion", [x, t, t, t, t]⟩,
    ⟨"completion", [t, x, x, x, t]⟩,
    ⟨"wrapUp", [t, x, x, natV 0, x, x, x, x, x, x, x]⟩,
    ⟨"wrapUp", [x, t, t, natV 1, t, t, t, t, t, t, t]⟩, ⟨"wrapUp", [x, t, t, natV 2, t, t, t, t, t, t, t]⟩,
    ⟨"wrapUp", [x, x, t, natV 1, t, t, t, t, t, t, t]⟩, ⟨"wrapUp", [x, t, t, natV 0, t, t, t, t, t, t, t]⟩,
    ⟨"wrapUp", [x, t, x, natV 1, t, t, t, t, t, t, t]⟩, ⟨"wrapUp", [x, t, t, natV 1, x, t, t, t, t, t, t]⟩,
    ⟨"wrapUp", [x, t, t, natV 1, t, t, t, x, t, t, t]⟩, ⟨"wrapUp", [x, t, t, natV 1, t, t, t, t, t, t, x]⟩,
    ⟨"expire", []⟩ ]

/-- The theorems of the server integration machine the axiom audit checks. -/
def serverIntegrationTheorems : List Name := [
  ``KanderFormal.Proofs.ServerIntegration.wellTyped,
  ``KanderFormal.Proofs.ServerIntegration.require_owner_iff,
  ``KanderFormal.Proofs.ServerIntegration.require_iff,
  ``KanderFormal.Proofs.ServerIntegration.owner_unique,
  ``KanderFormal.Proofs.ServerIntegration.owner_required,
  ``KanderFormal.Proofs.ServerIntegration.actor_required,
  ``KanderFormal.Proofs.ServerIntegration.stale_refused,
  ``KanderFormal.Proofs.ServerIntegration.claim_spec,
  ``KanderFormal.Proofs.ServerIntegration.claim_result,
  ``KanderFormal.Proofs.ServerIntegration.claim_accepts,
  ``KanderFormal.Proofs.ServerIntegration.claim_accepts_expired,
  ``KanderFormal.Proofs.ServerIntegration.claim_valid_refused,
  ``KanderFormal.Proofs.ServerIntegration.claim_stale_refused,
  ``KanderFormal.Proofs.ServerIntegration.claim_room_refused,
  ``KanderFormal.Proofs.ServerIntegration.claim_identity_refused,
  ``KanderFormal.Proofs.ServerIntegration.renew_keeps,
  ``KanderFormal.Proofs.ServerIntegration.action_cas,
  ``KanderFormal.Proofs.ServerIntegration.rev_change,
  ``KanderFormal.Proofs.ServerIntegration.gen_auth_change,
  ``KanderFormal.Proofs.ServerIntegration.claim_authority,
  ``KanderFormal.Proofs.ServerIntegration.claim_guard_spec,
  ``KanderFormal.Proofs.ServerIntegration.renew_spec,
  ``KanderFormal.Proofs.ServerIntegration.guard_spec,
  ``KanderFormal.Proofs.ServerIntegration.guard_release_spec,
  ``KanderFormal.Proofs.ServerIntegration.facts_ticket_refused,
  ``KanderFormal.Proofs.ServerIntegration.completion_spec,
  ``KanderFormal.Proofs.ServerIntegration.completion_needs_receipt,
  ``KanderFormal.Proofs.ServerIntegration.completion_wrong_receipt_refused,
  ``KanderFormal.Proofs.ServerIntegration.wrapup_spec,
  ``KanderFormal.Proofs.ServerIntegration.wrapup_needs_evidence,
  ``KanderFormal.Proofs.ServerIntegration.wrapup_not_required,
  ``KanderFormal.Proofs.ServerIntegration.pushing_spec,
  ``KanderFormal.Proofs.ServerIntegration.pushing_result,
  ``KanderFormal.Proofs.ServerIntegration.pushing_refused,
  ``KanderFormal.Proofs.ServerIntegration.confirm_spec,
  ``KanderFormal.Proofs.ServerIntegration.confirm_result,
  ``KanderFormal.Proofs.ServerIntegration.confirm_refused,
  ``KanderFormal.Proofs.ServerIntegration.release_spec,
  ``KanderFormal.Proofs.ServerIntegration.release_result,
  ``KanderFormal.Proofs.ServerIntegration.release_refused,
  ``KanderFormal.Proofs.ServerIntegration.refused_unchanged,
  ``KanderFormal.Proofs.ServerIntegration.accepted_trans,
  ``KanderFormal.Proofs.ServerIntegration.init,
  ``KanderFormal.Proofs.ServerIntegration.step_inv,
  ``KanderFormal.Proofs.ServerIntegration.reachable_inv,
  ``KanderFormal.Proofs.ServerIntegration.reachable_valid,
  ``KanderFormal.Proofs.ServerIntegration.reachable_ended_released,
  ``KanderFormal.Proofs.ServerIntegration.reachable_integrated_obs,
  ``KanderFormal.Proofs.ServerIntegration.trans_phase_flow,
  ``KanderFormal.Proofs.ServerIntegration.phase_flow,
  ``KanderFormal.Proofs.ServerIntegration.terminal_frozen,
  ``KanderFormal.Proofs.ServerIntegration.terminal_keeps_counters
]

end KanderFormal.Registry
