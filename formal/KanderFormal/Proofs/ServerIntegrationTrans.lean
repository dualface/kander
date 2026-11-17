import KanderFormal.Proofs.ServerIntegrationEv

/-
The transition relation of the server integration machine: every accepted step
keeps the record, refreshes or expires its lease, claims it, or runs one of the
three actions. The relation carries the facts the guards give about the record
before the step, so the invariant and the theorems about all events are proved
by cases on seven constructors instead of fourteen events.
-/
namespace KanderFormal.Proofs.ServerIntegration

open KanderFormal KanderFormal.Machines.ServerIntegration KanderFormal.Proofs.Scalar

inductive Trans (x : It) : It → Prop where
  | keep : Trans x x
  | renew (hs : x.st = 1) (hl : x.live = true) : Trans x { x with live := true }
  | expire (hl : x.live = true) : Trans x { x with live := false }
  | claim (pu ag : Nat) (ct : Bool) (hv : ¬Valid x) (hpu : pu ≠ 0) :
      Trans x ⟨1, x.gen + 1, x.rev + 1, true, 1, pu, ct, ag, false⟩
  | pushing (ob : Bool) (hs : x.st = 1) (hl : x.live = true) (hr : x.rev ≠ 0) (hph : x.ph = 1)
      (hct : x.ct = false) : Trans x { x with ph := 2, rev := x.rev + 1, ob := ob }
  | confirm (hs : x.st = 1) (hl : x.live = true) (hr : x.rev ≠ 0) (hph : (x.ph = 1 ∧ x.ct = true) ∨ x.ph = 2) :
      Trans x { x with st := 3, ph := 3, live := false, rev := x.rev + 1, ob := true }
  | release (ob : Bool) (hs : x.st = 1) (hl : x.live = true) (hr : x.rev ≠ 0) :
      Trans x ⟨3, x.gen, x.rev + 1, false, (if x.ph = 2 then 5 else 4), x.pu, x.ct, x.au, ob⟩

theorem ite_ne_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : (if c then some v else none) ≠ none) : c := by
  refine Classical.byContradiction fun hc => ?_
  simp [hc] at h

theorem ite_some_enc {c : Prop} [Decidable c] {z : It} {s' : State}
    (h : (if c then some (enc z) else none) = some s') : c ∧ s' = enc z := by
  split at h
  · cases h; exact ⟨‹_›, rfl⟩
  · cases h

theorem ite_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : ¬c) : (if c then some v else none) = none := by
  simp [h]

/-- The events that write a new revision: a claim and the three actions. -/
def Ev.bumpsRev : Ev → Prop
  | .claim .. | .pushing .. | .confirm .. | .release .. => True
  | _ => False

/-- The claim. -/
def Ev.isClaim : Ev → Prop
  | .claim .. => True
  | _ => False

/-- The record an accepted event of kind `v` leaves, from the record `x` before
it. -/
def Ev.res (x : It) : Ev → It → Prop
  | .claim _ _ _ _ _ _ _ ag pu ct, y => y = ⟨1, x.gen + 1, x.rev + 1, true, 1, pu, ct, ag, false⟩
  | .renew .., y => y = { x with live := true }
  | .pushing _ _ _ _ _ _ _ _ _ _ _ os, y => y = { x with ph := 2, rev := x.rev + 1, ob := os }
  | .confirm .., y => y = { x with st := 3, ph := 3, live := false, rev := x.rev + 1, ob := true }
  | .release _ _ _ _ _ _ _ _ os, y =>
    y = ⟨3, x.gen, x.rev + 1, false, (if x.ph = 2 then 5 else 4), x.pu, x.ct, x.au, os⟩
  | .expire, y => y = { x with live := false }
  | _, y => y = x

/-- Every accepted step is a transition. -/
theorem accepted_trans {p : Params} {x : It} {s' : State} {v : Ev}
    (h : step M p (enc x) v.toEvent = some s') : ∃ y, s' = enc y ∧ Trans x y ∧ v.res x y := by
  cases v with
  | claim eg er rg rr se ac am ag pu ct =>
    simp only [Ev.toEvent] at h
    rw [step_claim] at h
    obtain ⟨hc, rfl⟩ := ite_some_enc h
    obtain ⟨-, -, -, -, -, -, hv, -, -, -, -, -, -, -, hpu⟩ := hc
    exact ⟨_, rfl, .claim pu ag ct hv hpu, rfl⟩
  | claimGuard t ac fa =>
    simp only [Ev.toEvent] at h
    rw [step_claimGuard] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | require hdr g tg c al ag ae ac =>
    simp only [Ev.toEvent] at h
    rw [step_require] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | requireOwner hdr g tg c =>
    simp only [Ev.toEvent] at h
    rw [step_requireOwner] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | renew hdr g tg c al ag ae ac fa =>
    simp only [Ev.toEvent] at h
    rw [step_renew] at h
    obtain ⟨hc, rfl⟩ := ite_some_enc h
    obtain ⟨-, -, -, ⟨-, hs, -, hl, -, -⟩, -⟩ := hc
    exact ⟨_, rfl, .renew hs hl, rfl⟩
  | check hdr g tg c al ag ae ac er rm =>
    simp only [Ev.toEvent] at h
    rw [step_check] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | checkOwner hdr g tg c er rm =>
    simp only [Ev.toEvent] at h
    rw [step_checkOwner] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | pushing hdr g tg c al ag ae ac er rm om os =>
    simp only [Ev.toEvent] at h
    rw [step_pushing] at h
    obtain ⟨hc, rfl⟩ := ite_some_enc h
    obtain ⟨-, -, -, -, -, ⟨-, hs, -, hl, -, -⟩, -, her, hne, -, hct, hph, -⟩ := hc
    exact ⟨_, rfl, .pushing os hs hl (her ▸ hne) hph hct, rfl⟩
  | confirm hdr g tg c al ag ae ac er rm os oa ee =>
    simp only [Ev.toEvent] at h
    rw [step_confirm] at h
    obtain ⟨hc, rfl⟩ := ite_some_enc h
    obtain ⟨-, -, -, -, -, ⟨-, hs, -, hl, -, -⟩, -, her, hne, -, hph, -⟩ := hc
    exact ⟨_, rfl, .confirm hs hl (her ▸ hne) hph, rfl⟩
  | release hdr g tg c er rm de oe os =>
    simp only [Ev.toEvent] at h
    rw [step_release] at h
    obtain ⟨hc, rfl⟩ := ite_some_enc h
    obtain ⟨-, -, -, -, ⟨-, hs, -, hl, -, -⟩, her, hne, -⟩ := hc
    exact ⟨_, rfl, .release os hs hl (her ▸ hne), rfl⟩
  | guard hdr g tg c al ag ae ac t fa =>
    simp only [Ev.toEvent] at h
    rw [step_guard] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | guardRelease hdr g tg c t =>
    simp only [Ev.toEvent] at h
    rw [step_guardRelease] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | completion cl pn nd me pc =>
    simp only [Ev.toEvent] at h
    rw [step_completion] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | wrapUp nr ig idm ig' sm tm cm me cl ps rs =>
    simp only [Ev.toEvent] at h
    rw [step_wrapUp] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .keep, rfl⟩
  | expire =>
    simp only [Ev.toEvent] at h
    rw [step_expire] at h
    obtain ⟨hl, rfl⟩ := ite_some_enc h
    exact ⟨_, rfl, .expire hl, rfl⟩


/-- Every accepted event of any form is a transition of one of the kinds. -/
theorem accepted_trans' {p : Params} {x : It} {s' : State} {e : Event}
    (h : step M p (enc x) e = some s') : ∃ v : Ev, e = v.toEvent ∧ ∃ y, s' = enc y ∧ Trans x y ∧ v.res x y := by
  obtain ⟨v, rfl⟩ := accepted_ev h
  exact ⟨v, rfl, accepted_trans h⟩

end KanderFormal.Proofs.ServerIntegration
