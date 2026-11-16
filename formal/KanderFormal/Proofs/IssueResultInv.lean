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
  cid_iff : x.cpos = true ↔ x.cid ≠ 0
  has_sound : x.st ≠ 0 → x.sound = true

def Inv (p : Params) (s : State) : Prop := M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, false, false, false, false, 0, 0, 0, 0, 0, 0, 0, 0, 0, false⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  refine ⟨by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp⟩

/-- Closes the new properties of a step that changes the fields they read in
the way the event says. -/
macro "pfin" : tactic => `(tactic| first | omega | (simp_all <;> omega) | simp_all)

set_option maxHeartbeats 16000000 in
theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hst, hun, hnf, hnd, hbo, hci, hch, hwk, hcw, hfh, hfp⟩ := shape hc
  obtain ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩ := hp x rfl
  intro y hy
  obtain ⟨ev, rfl⟩ := accepted_ev h
  cases ev with
  | read ro sm nw =>
    simp only [Ev.toEvent] at h; rw [step_read] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, fun _ => rfl, P4, P5, P6, P7, P8, P9⟩
  | reconcile fo nc =>
    simp only [Ev.toEvent] at h; rw [step_reconcile p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => P3 (by omega), by simp, P5, by simp, by simp,
      ?_, fun _ => P9 (by omega)⟩
    dsimp only; simp; omega
  | reassess =>
    simp only [Ev.toEvent] at h; rw [step_reassess p x hcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩
  | intent nf nm dp bo nb nk ns =>
    simp only [Ev.toEvent] at h; rw [step_intent p x hcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    obtain ⟨-, -, -, hi, hu, -, -, -, hm, -, -⟩ := hcn
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => hi, by simp, ?_, by simp, by simp, by simp,
      fun _ => rfl⟩
    intro hf; dsimp only at hf ⊢; cases nf <;> simp_all
  | equivalent nf nm fo nb nc nk ns =>
    simp only [Ev.toEvent] at h; rw [step_equivalent p x hcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    obtain ⟨-, -, -, hi, hu, -, -, hm, -, hnc, -⟩ := hcn
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => hi, by simp, ?_, by simp, by simp, ?_,
      fun _ => rfl⟩
    · intro hf; dsimp only at hf ⊢; cases nf <;> simp_all
    · dsimp only; simp; omega
  | publish ok nc =>
    simp only [Ev.toEvent] at h; rw [step_publish p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => P3 (by omega), by simp, P5, by simp, by simp,
      ?_, fun _ => P9 (by omega)⟩
    dsimp only; simp; omega
  | reject nh =>
    simp only [Ev.toEvent] at h; rw [step_reject p x hun hfh] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    obtain ⟨-, -, hs1, -, hu1⟩ := hcn
    have hcp : x.cpos = false := by
      by_cases hh : x.cpos = true
      · have := P4.mp hh; omega
      · simpa using hh
    refine ⟨by dsimp only; omega, by dsimp only; omega, fun _ => P3 (by omega), ?_, P5, fun _ => P6 (by omega),
      by simp, ?_, fun _ => P9 (by omega)⟩
    · dsimp only; simp [hcp]
    · dsimp only; simp [hcp] at P8 ⊢; exact P8
  | decide dg cl sc fk =>
    simp only [Ev.toEvent] at h; rw [step_decide] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    obtain ⟨-, -, hst2, -⟩ := hcn
    exact ⟨P1, P2, P3, P4, P5, fun hn => by dsimp only at hn ⊢; omega, P7, P8, P9⟩
  | keep dg cl sc =>
    simp only [Ev.toEvent] at h; rw [step_keep] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩
  | failClose nh =>
    simp only [Ev.toEvent] at h; rw [step_failClose p x hfh] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩
  | otherIntent =>
    simp only [Ev.toEvent] at h; rw [step_otherIntent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    refine ⟨by dsimp only; omega, fun h1 => by dsimp only; have := P2 h1; omega, P3, P4, P5, P6, P7, P8, P9⟩
  | otherSettle =>
    simp only [Ev.toEvent] at h; rw [step_otherSettle p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    refine ⟨by dsimp only; omega, fun h1 => by dsimp only at h1 ⊢; omega, P3, P4, P5, P6, P7, P8, P9⟩
  | update pr ns is nfn pf ndn =>
    simp only [Ev.toEvent] at h; rw [step_update] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩
  | work ns id cs ws vo =>
    simp only [Ev.toEvent] at h; rw [step_work] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩
  | idKeep sm =>
    simp only [Ev.toEvent] at h; rw [step_idKeep] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩
  | valid vs ap cp oo nf nm =>
    simp only [Ev.toEvent] at h; rw [step_valid] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩
  | issueNeeded hr ip =>
    simp only [Ev.toEvent] at h; rw [step_issueNeeded] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj (hy.symm.trans hz); subst this
    exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩

/-- The bounds a conforming state has. -/
theorem bounds {p : Params} {x : Rq} (hi : Inv p (enc x)) :
    x.st < 6 ∧ x.unc ≤ p "maxN" ∧ x.nf ≤ p "maxN" ∧ x.nd ≤ p "maxN" ∧ x.bod ≤ p "maxId" ∧ x.cid ≤ p "maxId" ∧
      x.chk ≤ p "maxId" ∧ x.wk ≤ p "maxId" ∧ x.cw ≤ p "maxId" ∧ x.fh ≤ p "maxId" ∧ x.fp ≤ p "maxId" := by
  obtain ⟨z, hz, h1, h2, h3, h4, h5, h6, h7, h8, h9, h10, h11⟩ := shape hi.1
  have := enc_inj hz.symm
  subst this
  exact ⟨h1, h2, h3, h4, h5, h6, h7, h8, h9, h10, h11⟩

/-- An `uncertain` record has no comment. -/
theorem Props.cid_zero {x : Rq} (P : Props x) (h : x.st = 1) : x.cid = 0 := by
  refine Classical.byContradiction fun hne => ?_
  have h1 := P.cid_iff.mpr hne
  have h2 := P.cpos_iff.mp h1
  omega

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
