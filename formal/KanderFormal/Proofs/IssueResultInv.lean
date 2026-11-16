import KanderFormal.Proofs.IssueResultCases

/-
The invariant of the issue result machine (theorem 9) and the facts about the
accepted events that the other theorems use.
-/
namespace KanderFormal.Proofs.IssueResult

open KanderFormal KanderFormal.Machines.IssueResult KanderFormal.Proofs.Scalar

/-- The safety properties of one record and of the store around it: at most one
record is `uncertain`, and the record under examination is that one when it is
`uncertain`; a record has a store with a known issue identity; a record that a
comment carries has a positive comment ID, and only such a record has one; a
fully resolved record has only `met` outcomes; only a `published` or
`equivalent` record has close decisions; and the status is one the store
writes. -/
structure Props (x : Rq) : Prop where
  unc_le : x.unc ≤ 1
  unc_one : x.st = 1 → x.unc = 1
  has_iid : x.st ≠ 0 → x.iid = true
  cpos_iff : x.cpos = true ↔ (x.st = 2 ∨ x.st = 3)
  full_met : x.full = true → x.met = true
  no_decision : ¬(x.st = 2 ∨ x.st = 3) → x.nd = 0
  known : x.st ≠ 5

def Inv (p : Params) (s : State) : Prop := M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, false, false, false, false, 0, 0⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  refine ⟨by simp, by simp, by simp, by simp, by simp, by simp, by simp⟩

/-- A step that ends in the encoding of `z` from the encoding of `x`, with the
new record named. -/
theorem same_inv {x y : Rq} (hx : Props x) (h : y = x) : Props y := by subst h; exact hx

set_option maxHeartbeats 8000000 in
theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hst, hun, hnf, hnd⟩ := shape hc
  obtain ⟨P1, P2, P3, P4, P5, P6, P7⟩ := hp x rfl
  intro y hy
  obtain ⟨ev, rfl⟩ := accepted_ev h
  cases ev with
  | read ro sm =>
    simp only [Ev.toEvent] at h; rw [step_read] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, fun _ => rfl, P4, P5, P6, P7⟩
  | reconcile fo =>
    simp only [Ev.toEvent] at h; rw [step_reconcile p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => P3 (by omega), by simp, P5, by simp, by simp⟩
  | reassess =>
    simp only [Ev.toEvent] at h; rw [step_reassess] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7⟩
  | intent nf nm dp bo =>
    simp only [Ev.toEvent] at h; rw [step_intent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    obtain ⟨-, hi, hu, -, -, -, hm⟩ := hcn
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => hi, by simp, ?_, by simp, by simp⟩
    intro hf; dsimp only at hf ⊢; cases nf <;> simp_all
  | equivalent nf nm fo =>
    simp only [Ev.toEvent] at h; rw [step_equivalent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    obtain ⟨hi, hu, -, -, hm⟩ := hcn
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => hi, by simp, ?_, by simp, by simp⟩
    intro hf; dsimp only at hf ⊢; cases nf <;> simp_all
  | publish ok =>
    simp only [Ev.toEvent] at h; rw [step_publish p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => P3 (by omega), by simp, P5, by simp, by simp⟩
  | reject =>
    simp only [Ev.toEvent] at h; rw [step_reject p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    obtain ⟨-, hs1, hu1⟩ := hcn
    have hcp : x.cpos = false := by
      by_cases hh : x.cpos = true
      · have := P4.mp hh; omega
      · simpa using hh
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => P3 (by omega), ?_, P5, fun _ => P6 (by omega),
      by simp⟩
    dsimp only; simp [hcp]
  | decide dg cl sc fk =>
    simp only [Ev.toEvent] at h; rw [step_decide] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    obtain ⟨-, -, hst2, -⟩ := hcn
    exact ⟨P1, P2, P3, P4, P5, fun hn => by dsimp only at hn ⊢; omega, P7⟩
  | keep dg cl sc =>
    simp only [Ev.toEvent] at h; rw [step_keep] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7⟩
  | failClose =>
    simp only [Ev.toEvent] at h; rw [step_failClose] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7⟩
  | otherIntent =>
    simp only [Ev.toEvent] at h; rw [step_otherIntent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    refine ⟨by dsimp only; omega, fun h1 => by dsimp only; have := P2 h1; omega, P3, P4, P5, P6, P7⟩
  | otherSettle =>
    simp only [Ev.toEvent] at h; rw [step_otherSettle p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    refine ⟨by dsimp only; omega, fun h1 => by dsimp only at h1 ⊢; omega, P3, P4, P5, P6, P7⟩
  | update pr ns is nfn pf ndn =>
    simp only [Ev.toEvent] at h; rw [step_update] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7⟩
  | work ns id cs ws vo =>
    simp only [Ev.toEvent] at h; rw [step_work] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7⟩
  | idKeep sm =>
    simp only [Ev.toEvent] at h; rw [step_idKeep] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7⟩
  | valid vs ap cp oo nf nm =>
    simp only [Ev.toEvent] at h; rw [step_valid] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7⟩
  | issueNeeded hr ip =>
    simp only [Ev.toEvent] at h; rw [step_issueNeeded] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7⟩

def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant, for every value
of `maxN`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

theorem reachable_props {p : Params} {x : Rq} (h : Reachable M p (enc x)) : Props x := (reachable_inv h).2 x rfl

/-- Theorem 9: at most one record of the store is `uncertain`: the count never
exceeds one. -/
theorem reachable_one_uncertain {p : Params} {x : Rq} (h : Reachable M p (enc x)) : x.unc ≤ 1 :=
  (reachable_props h).unc_le

/-- Theorem 9: a record that is `uncertain` is the one uncertain record. -/
theorem reachable_uncertain_counted {p : Params} {x : Rq} (h : Reachable M p (enc x)) (hs : x.st = 1) :
    x.unc = 1 := (reachable_props h).unc_one hs

/-- Theorem 9: a record has a store with a positive issue identity. -/
theorem reachable_issue_identity {p : Params} {x : Rq} (h : Reachable M p (enc x)) (hs : x.st ≠ 0) :
    x.iid = true := (reachable_props h).has_iid hs

/-- Theorem 9: a `published` or `equivalent` record has a positive comment ID. -/
theorem reachable_comment {p : Params} {x : Rq} (h : Reachable M p (enc x)) (hs : x.st = 2 ∨ x.st = 3) :
    x.cpos = true := ((reachable_props h).cpos_iff).mpr hs

/-- Theorem 9: a fully resolved record has only `met` outcomes. -/
theorem reachable_resolved_met {p : Params} {x : Rq} (h : Reachable M p (enc x)) (hf : x.full = true) :
    x.met = true := (reachable_props h).full_met hf

/-- Theorem 9: an `uncertain` or `rejected` record has no close decision. -/
theorem reachable_no_decision {p : Params} {x : Rq} (h : Reachable M p (enc x)) (hs : x.st = 1 ∨ x.st = 4) :
    x.nd = 0 := (reachable_props h).no_decision (by omega)

end KanderFormal.Proofs.IssueResult
