import KanderFormal.Proofs.IssueResultSpecs

/-
Proof obligations of the issue result machine, part two: the status flow of a
record, and the theorem that every save of a client entrance satisfies the
update and work relations (theorem 6). The theorems hold for every value of
`maxN`.
-/
namespace KanderFormal.Proofs.IssueResult

open KanderFormal KanderFormal.Machines.IssueResult KanderFormal.Proofs.Scalar

/-- Closes an arithmetic goal that may hold Boolean literals. -/
macro "fin" : tactic => `(tactic| first | omega | (simp <;> omega) | (simp_all <;> omega) | simp_all)

/-- The bounds a conforming state has. -/
theorem bounds {p : Params} {x : Rq} (hi : Inv p (enc x)) :
    x.st < 6 ∧ x.unc ≤ p "maxN" ∧ x.nf ≤ p "maxN" ∧ x.nd ≤ p "maxN" := by
  obtain ⟨z, hz, h1, h2, h3, h4⟩ := shape hi.1
  have := enc_inj hz.symm
  subst this
  exact ⟨h1, h2, h3, h4⟩

/-- The events that write a new record, absent or replacing a `rejected`
one. -/
def Ev.newRecord : Ev → Bool
  | .intent .. | .equivalent .. => true
  | _ => false

/-! ## Theorem 2: the status of a record -/

/-- Theorem 2: a record starts `uncertain` (an intent) or `equivalent` where
there was none or a `rejected` one, an `uncertain` record becomes `published`
or `rejected`, and nothing else changes a status. In particular a `published`
or `equivalent` record never changes. -/
theorem status_flow {p : Params} {x y : Rq} {e : Event} (hi : Inv p (enc x))
    (h : step M p (enc x) e = some (enc y)) :
    y.st = x.st ∨ ((x.st = 0 ∨ x.st = 4) ∧ (y.st = 1 ∨ y.st = 3)) ∨ (x.st = 1 ∧ (y.st = 2 ∨ y.st = 4)) := by
  obtain ⟨-, hun, -, -⟩ := bounds hi
  obtain ⟨ev, rfl⟩ := accepted_ev h
  cases ev with
  | read ro sm =>
    simp only [Ev.toEvent] at h; rw [step_read] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | reconcile fo =>
    simp only [Ev.toEvent] at h; rw [step_reconcile p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; exact .inr (.inr ⟨hcn.1, .inl rfl⟩)
  | reassess =>
    simp only [Ev.toEvent] at h; rw [step_reassess] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | intent nf nm dp bo =>
    simp only [Ev.toEvent] at h; rw [step_intent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; exact .inr (.inl ⟨hcn.2.2.2.1, .inl rfl⟩)
  | equivalent nf nm fo =>
    simp only [Ev.toEvent] at h; rw [step_equivalent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; exact .inr (.inl ⟨hcn.2.2.1, .inr rfl⟩)
  | publish ok =>
    simp only [Ev.toEvent] at h; rw [step_publish p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; exact .inr (.inr ⟨hcn.1, .inl rfl⟩)
  | reject =>
    simp only [Ev.toEvent] at h; rw [step_reject p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; exact .inr (.inr ⟨hcn.2.1, .inr rfl⟩)
  | decide dg cl sc fk =>
    simp only [Ev.toEvent] at h; rw [step_decide] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | keep dg cl sc =>
    simp only [Ev.toEvent] at h; rw [step_keep] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | failClose =>
    simp only [Ev.toEvent] at h; rw [step_failClose] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | otherIntent =>
    simp only [Ev.toEvent] at h; rw [step_otherIntent] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | otherSettle =>
    simp only [Ev.toEvent] at h; rw [step_otherSettle p x hun] at h; have := enc_inj (ite_some_enc h).2
    subst this; exact .inl rfl
  | update pr ns is nfn pf ndn =>
    simp only [Ev.toEvent] at h; rw [step_update] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | work ns id cs ws vo =>
    simp only [Ev.toEvent] at h; rw [step_work] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | idKeep sm =>
    simp only [Ev.toEvent] at h; rw [step_idKeep] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | valid vs ap cp oo nf nm =>
    simp only [Ev.toEvent] at h; rw [step_valid] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | issueNeeded hr ip =>
    simp only [Ev.toEvent] at h; rw [step_issueNeeded] at h; have := enc_inj (ite_some_enc h).2; subst this
    exact .inl rfl

/-- Theorem 2: a `published` or `equivalent` record is never changed by any
event. -/
theorem published_frozen {p : Params} {x y : Rq} {e : Event} (hi : Inv p (enc x)) (hx : x.st = 2 ∨ x.st = 3)
    (h : step M p (enc x) e = some (enc y)) : y.st = x.st := by
  rcases status_flow hi h with h1 | ⟨h1, -⟩ | ⟨h1, -⟩ <;> omega

/-- Theorem 2: a `rejected` record is only replaced by a new record. -/
theorem rejected_only_replaced {p : Params} {x y : Rq} {e : Event} (hi : Inv p (enc x)) (hx : x.st = 4)
    (h : step M p (enc x) e = some (enc y)) : y.st = 4 ∨ y.st = 1 ∨ y.st = 3 := by
  rcases status_flow hi h with h1 | ⟨-, h1⟩ | ⟨h1, -⟩ <;> omega

/-! ## Theorem 6: every save satisfies the update relation -/

/-- Theorem 6: after a client event on a record of the old store (`x.st ≠ 0`),
the new record passes `ValidateResultStoreUpdate`. The facts the shell would
compute are those of an entrance that only appends: the body, author, and
comment stay, and the failures extend the old ones. -/
theorem client_update_ok {p : Params} {x y : Rq} {ev : Ev} (hi : Inv p (enc x)) (hs : x.st ≠ 0)
    (hc : ev.client = true) (h : step M p (enc x) ev.toEvent = some (enc y)) :
    step M p (enc x) ⟨"update", [boolV true, enumV y.st, boolV true, natV y.nf, boolV true, natV y.nd]⟩ ≠
      none := by
  have hy := step_inv p _ _ _ hi h
  obtain ⟨-, hxun, -, -⟩ := bounds hi
  obtain ⟨hyst, -, hynf, hynd⟩ := bounds hy
  have P := hi.2 x rfl
  rw [update_spec]
  refine ⟨hyst, hynf, hynd, rfl, ?_⟩
  cases ev with
  | read ro sm =>
    simp only [Ev.toEvent] at h; rw [step_read] at h; have := enc_inj (ite_some_enc h).2; subst this
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | reconcile fo =>
    simp only [Ev.toEvent] at h; rw [step_reconcile p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | reassess =>
    simp only [Ev.toEvent] at h; rw [step_reassess] at h; have := enc_inj (ite_some_enc h).2; subst this
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | intent nf nm dp bo =>
    simp only [Ev.toEvent] at h; rw [step_intent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    have hnd := P.no_decision (by omega)
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | equivalent nf nm fo =>
    simp only [Ev.toEvent] at h; rw [step_equivalent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    have hnd := P.no_decision (by omega)
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | publish ok =>
    simp only [Ev.toEvent] at h; rw [step_publish p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | reject =>
    simp only [Ev.toEvent] at h; rw [step_reject p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | decide dg cl sc fk =>
    simp only [Ev.toEvent] at h; rw [step_decide] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | keep dg cl sc =>
    simp only [Ev.toEvent] at h; rw [step_keep] at h; have := enc_inj (ite_some_enc h).2; subst this
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | failClose =>
    simp only [Ev.toEvent] at h; rw [step_failClose] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    refine ⟨?_, ?_, rfl, ?_⟩ <;> (try dsimp only) <;> fin
  | otherIntent => simp [Ev.client] at hc
  | otherSettle => simp [Ev.client] at hc
  | update pr ns is nfn pf ndn => simp [Ev.client] at hc
  | work ns id cs ws vo => simp [Ev.client] at hc
  | idKeep sm => simp [Ev.client] at hc
  | valid vs ap cp oo nf nm => simp [Ev.client] at hc
  | issueNeeded hr ip => simp [Ev.client] at hc

/-- Theorem 6: after a client event, the new record passes
`ValidateResultStoreWork` for the current work set: an unchanged `rejected`
record is identical, a record that is not new keeps its verification evidence,
and a new record is `uncertain` or `equivalent`, no other record was
`uncertain`, and it matches the current work set. The old record is absent
only for an event that writes a new record. -/
theorem client_work_ok {p : Params} {x y : Rq} {ev : Ev} (hi : Inv p (enc x))
    (hs : x.st ≠ 0 ∨ ev.newRecord = true) (hc : ev.client = true)
    (h : step M p (enc x) ev.toEvent = some (enc y)) :
    step M p (enc x) ⟨"work", [enumV y.st, boolV (decide (x.st = 4 ∧ y.st = 4)), boolV true, boolV true,
      boolV true]⟩ ≠ none := by
  have hy := step_inv p _ _ _ hi h
  obtain ⟨hxst, hxun, -, -⟩ := bounds hi
  obtain ⟨hyst, -, -, -⟩ := bounds hy
  have P := hi.2 x rfl
  have hk := P.known
  rw [work_spec]
  refine ⟨hyst, ?_⟩
  cases ev with
  | read ro sm =>
    simp only [Ev.toEvent] at h; rw [step_read] at h; have := enc_inj (ite_some_enc h).2; subst this
    (try dsimp only); (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | reconcile fo =>
    simp only [Ev.toEvent] at h; rw [step_reconcile p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | reassess =>
    simp only [Ev.toEvent] at h; rw [step_reassess] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | intent nf nm dp bo =>
    simp only [Ev.toEvent] at h; rw [step_intent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | equivalent nf nm fo =>
    simp only [Ev.toEvent] at h; rw [step_equivalent] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | publish ok =>
    simp only [Ev.toEvent] at h; rw [step_publish p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | reject =>
    simp only [Ev.toEvent] at h; rw [step_reject p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | decide dg cl sc fk =>
    simp only [Ev.toEvent] at h; rw [step_decide] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | keep dg cl sc =>
    simp only [Ev.toEvent] at h; rw [step_keep] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | failClose =>
    simp only [Ev.toEvent] at h; rw [step_failClose] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs); simp <;> omega
  | otherIntent => simp [Ev.client] at hc
  | otherSettle => simp [Ev.client] at hc
  | update pr ns is nfn pf ndn => simp [Ev.client] at hc
  | work ns id cs ws vo => simp [Ev.client] at hc
  | idKeep sm => simp [Ev.client] at hc
  | valid vs ap cp oo nf nm => simp [Ev.client] at hc
  | issueNeeded hr ip => simp [Ev.client] at hc

/-- Theorem 6: the issue identity a save carries is the one the store already
had, so `ValidateResultStoreUpdate` accepts it. -/
theorem idKeep_same_accepted (p : Params) (x : Rq) : step M p (enc x) ⟨"idKeep", [boolV true]⟩ ≠ none :=
  (idKeep_spec p x true).mpr (.inr rfl)

/-- Theorem 6: a record of a reachable state is structurally valid
(`ValidateResultStore`): the new store a client entrance writes is valid. -/
theorem props_valid {p : Params} {x : Rq} (hi : Inv p (enc x)) (h0 : x.st ≠ 0) :
    step M p (enc x) ⟨"valid", [enumV x.st, boolV true, boolV x.cpos, boolV true, boolV x.full, boolV x.met]⟩ ≠
      none := by
  obtain ⟨hst, -, -, -⟩ := bounds hi
  have P := hi.2 x rfl
  rw [valid_spec]
  refine ⟨hst, by have := P.known; omega, rfl, ?_, rfl, ?_⟩
  · by_cases h : x.st = 2 ∨ x.st = 3
    · exact .inr (P.cpos_iff.mpr h)
    · exact .inl (by omega)
  · by_cases hf : x.full = true
    · exact .inr (P.full_met hf)
    · exact .inl (by simpa using hf)

theorem props_issueNeeded {p : Params} {x : Rq} (hi : Inv p (enc x)) :
    step M p (enc x) ⟨"issueNeeded", [boolV (decide (x.st ≠ 0)), boolV x.iid]⟩ ≠ none := by
  have P := hi.2 x rfl
  rw [issueNeeded_spec]
  by_cases h : x.st = 0
  · exact .inl (by simp [h])
  · exact .inr (P.has_iid h)

/-- Theorem 6: on a reachable state, every save of a client entrance satisfies
the update relation and the work relation. -/
theorem reachable_client_save {p : Params} {x y : Rq} {ev : Ev} (hr : Reachable M p (enc x)) (hc : ev.client = true)
    (h : step M p (enc x) ev.toEvent = some (enc y)) :
    (x.st ≠ 0 →
      step M p (enc x) ⟨"update", [boolV true, enumV y.st, boolV true, natV y.nf, boolV true, natV y.nd]⟩ ≠
        none) ∧
    ((x.st ≠ 0 ∨ ev.newRecord = true) →
      step M p (enc x) ⟨"work", [enumV y.st, boolV (decide (x.st = 4 ∧ y.st = 4)), boolV true, boolV true,
        boolV true]⟩ ≠ none) :=
  ⟨fun hs => client_update_ok (reachable_inv hr) hs hc h, fun hs => client_work_ok (reachable_inv hr) hs hc h⟩

end KanderFormal.Proofs.IssueResult
