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
  obtain ⟨-, hun, -, -, -, -, -, -, hcw, hfh, -⟩ := bounds hi
  obtain ⟨ev, rfl⟩ := accepted_ev h
  cases ev with
  | read ro sm nw =>
    simp only [Ev.toEvent] at h; rw [step_read] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | reconcile fo nc =>
    simp only [Ev.toEvent] at h; rw [step_reconcile p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; dsimp only; omega
  | reassess =>
    simp only [Ev.toEvent] at h; rw [step_reassess p x hcw] at h; have := enc_inj (ite_some_enc h).2; subst this
    exact .inl rfl
  | intent nf nm dp bo nb nk ns =>
    simp only [Ev.toEvent] at h; rw [step_intent p x hcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; dsimp only; omega
  | equivalent nf nm fo nb nc nk ns =>
    simp only [Ev.toEvent] at h; rw [step_equivalent p x hcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; dsimp only; omega
  | publish ok nc =>
    simp only [Ev.toEvent] at h; rw [step_publish p x hun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; dsimp only; omega
  | reject nh =>
    simp only [Ev.toEvent] at h; rw [step_reject p x hun hfh] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this; dsimp only; omega
  | decide dg cl sc fk =>
    simp only [Ev.toEvent] at h; rw [step_decide] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | keep dg cl sc =>
    simp only [Ev.toEvent] at h; rw [step_keep] at h; have := enc_inj (ite_some_enc h).2; subst this; exact .inl rfl
  | failClose nh =>
    simp only [Ev.toEvent] at h; rw [step_failClose p x hfh] at h; have := enc_inj (ite_some_enc h).2; subst this
    exact .inl rfl
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

/-- The record of the new store is the record of the old store, as
`reflect.DeepEqual` sees it: every field of the record, none of the store. -/
abbrev SameRecord (x y : Rq) : Prop :=
  x.st = y.st ∧ x.full = y.full ∧ x.met = y.met ∧ x.cpos = y.cpos ∧ x.nf = y.nf ∧ x.nd = y.nd ∧ x.bod = y.bod ∧
    x.cid = y.cid ∧ x.chk = y.chk ∧ x.wk = y.wk ∧ x.fh = y.fh ∧ x.fp = y.fp ∧ x.sound = y.sound

/-- The facts of `ValidateResultStoreUpdate` for a record that went from `x` to
`y`, read off the two records: the new store has the record; its body and
author are the old ones and its comment is the old one once the old record had
one (`identSame`); its failures are the old ones, or the old ones and one more
(`prefixF`); it has no fewer failures and no fewer decisions. -/
def updFacts (x y : Rq) : Event :=
  ⟨"update", [boolV (decide (y.st ≠ 0)), enumV y.st,
    boolV (decide (x.bod = y.bod ∧ (x.cid = 0 ∨ x.cid = y.cid))), natV y.nf,
    boolV (decide ((y.nf = x.nf ∧ y.fh = x.fh) ∨ (y.nf = x.nf + 1 ∧ y.fp = x.fh))), natV y.nd]⟩

/-- The facts of `ValidateResultStoreWork`: the new record is an unchanged
`rejected` record (`identical`), keeps its verification evidence
(`checksSame`), matches the current work set (`workSame`), and its version is
the one its assessment computes (`versionOk`). -/
def workFacts (x y : Rq) : Event :=
  ⟨"work", [enumV y.st, boolV (decide (x.st = 4 ∧ SameRecord x y)), boolV (decide (x.chk = y.chk)),
    boolV (decide (y.wk = y.cw)), boolV y.sound]⟩

/-- The facts of `ValidateResultStore` for the new record. -/
def validFacts (y : Rq) : Event :=
  ⟨"valid", [enumV y.st, boolV y.sound, boolV y.cpos, boolV y.sound, boolV y.full, boolV y.met]⟩

/-- Theorem 6: after a client event on a record of the old store (`x.st ≠ 0`),
the new record passes `ValidateResultStoreUpdate`. The comparison facts are not
assumed: they are read off the records before and after the event. -/
theorem client_update_ok {p : Params} {x y : Rq} {ev : Ev} (hi : Inv p (enc x)) (hs : x.st ≠ 0)
    (hc : ev.client = true) (h : step M p (enc x) ev.toEvent = some (enc y)) :
    step M p (enc x) (updFacts x y) ≠ none := by
  have hy := step_inv p _ _ _ hi h
  obtain ⟨-, hxun, -, -, -, -, -, -, hxcw, hxfh, -⟩ := bounds hi
  obtain ⟨hyst, -, hynf, hynd, -⟩ := bounds hy
  have P := hi.2 x rfl
  have hc0 := P.cid_zero
  have hn0 := P.no_decision
  unfold updFacts
  rw [update_spec]
  refine ⟨hyst, hynf, hynd, ?_⟩
  cases ev with
  | read ro sm nw =>
    simp only [Ev.toEvent] at h; rw [step_read] at h; have := enc_inj (ite_some_enc h).2; subst this
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | reconcile fo nc =>
    simp only [Ev.toEvent] at h; rw [step_reconcile p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    have := hc0 hcn.2.1
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | reassess =>
    simp only [Ev.toEvent] at h; rw [step_reassess p x hxcw] at h; have := enc_inj (ite_some_enc h).2; subst this
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | intent nf nm dp bo nb nk ns =>
    simp only [Ev.toEvent] at h; rw [step_intent p x hxcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    have := hn0 (by omega)
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | equivalent nf nm fo nb nc nk ns =>
    simp only [Ev.toEvent] at h; rw [step_equivalent p x hxcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    have := hn0 (by omega)
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | publish ok nc =>
    simp only [Ev.toEvent] at h; rw [step_publish p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    have := hc0 hcn.2.1
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | reject nh =>
    simp only [Ev.toEvent] at h; rw [step_reject p x hxun hxfh] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | decide dg cl sc fk =>
    simp only [Ev.toEvent] at h; rw [step_decide] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | keep dg cl sc =>
    simp only [Ev.toEvent] at h; rw [step_keep] at h; have := enc_inj (ite_some_enc h).2; subst this
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
  | failClose nh =>
    simp only [Ev.toEvent] at h; rw [step_failClose p x hxfh] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp only [decide_eq_true_eq]; (try simp) <;> omega
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
`uncertain`, and it matches the current work set and its version. The old
record is absent only for an event that writes a new record. -/
theorem client_work_ok {p : Params} {x y : Rq} {ev : Ev} (hi : Inv p (enc x))
    (hs : x.st ≠ 0 ∨ ev.newRecord = true) (hc : ev.client = true)
    (h : step M p (enc x) ev.toEvent = some (enc y)) :
    step M p (enc x) (workFacts x y) ≠ none := by
  have hy := step_inv p _ _ _ hi h
  obtain ⟨hxst, hxun, -, -, -, -, -, -, hxcw, hxfh, -⟩ := bounds hi
  obtain ⟨hyst, -⟩ := bounds hy
  have P := hi.2 x rfl
  have hk := P.known
  unfold workFacts
  rw [work_spec]
  refine ⟨hyst, ?_⟩
  cases ev with
  | read ro sm nw =>
    simp only [Ev.toEvent] at h; rw [step_read] at h; have := enc_inj (ite_some_enc h).2; subst this
    (try simp only [Ev.newRecord, Bool.false_eq_true, or_false] at hs)
    (try dsimp only); simp [SameRecord] <;> omega
  | reconcile fo nc =>
    simp only [Ev.toEvent] at h; rw [step_reconcile p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | reassess =>
    simp only [Ev.toEvent] at h; rw [step_reassess p x hxcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | intent nf nm dp bo nb nk ns =>
    simp only [Ev.toEvent] at h; rw [step_intent p x hxcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | equivalent nf nm fo nb nc nk ns =>
    simp only [Ev.toEvent] at h; rw [step_equivalent p x hxcw] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | publish ok nc =>
    simp only [Ev.toEvent] at h; rw [step_publish p x hxun] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | reject nh =>
    simp only [Ev.toEvent] at h; rw [step_reject p x hxun hxfh] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | decide dg cl sc fk =>
    simp only [Ev.toEvent] at h; rw [step_decide] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | keep dg cl sc =>
    simp only [Ev.toEvent] at h; rw [step_keep] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | failClose nh =>
    simp only [Ev.toEvent] at h; rw [step_failClose p x hxfh] at h; obtain ⟨hcn, hz⟩ := ite_some_enc h
    have := enc_inj hz; subst this
    (try dsimp only); simp [SameRecord] <;> omega
  | otherIntent => simp [Ev.client] at hc
  | otherSettle => simp [Ev.client] at hc
  | update pr ns is nfn pf ndn => simp [Ev.client] at hc
  | work ns id cs ws vo => simp [Ev.client] at hc
  | idKeep sm => simp [Ev.client] at hc
  | valid vs ap cp oo nf nm => simp [Ev.client] at hc
  | issueNeeded hr ip => simp [Ev.client] at hc

/-- Theorem 6: the issue identity a save carries is the one the store already
had. `readResult` is the only step that writes it, and it accepts a remote only
when the store has no identity or the remote's is the same (`same`); the other
client events do not touch it. So the fact `ValidateResultStoreUpdate` asks for
is the `same` that `readResult` was accepted with. -/
theorem read_idKeep_ok (p : Params) (x : Rq) (ro sm : Bool) (nw : Nat) {s' : State}
    (h : step M p (enc x) ⟨"read", [boolV ro, boolV sm, natV nw]⟩ = some s') :
    (x.iid = true → sm = true) ∧ step M p (enc x) ⟨"idKeep", [boolV sm]⟩ ≠ none := by
  have hr := (read_spec p x ro sm nw).mp (by rw [h]; simp)
  have hk : x.iid = true → sm = true := by
    intro hx; rcases hr.2.2.1 with h1 | h1
    · simp [hx] at h1
    · exact h1
  refine ⟨hk, (idKeep_spec p x sm).mpr ?_⟩
  by_cases hx : x.iid = true
  · exact .inr (hk hx)
  · exact .inl (by simpa using hx)

/-- Theorem 6: a record of a reachable state is structurally valid
(`ValidateResultStore`): the new store a client entrance writes is valid. -/
theorem props_valid {p : Params} {x : Rq} (hi : Inv p (enc x)) (h0 : x.st ≠ 0) :
    step M p (enc x) (validFacts x) ≠ none := by
  obtain ⟨hst, -⟩ := bounds hi
  have P := hi.2 x rfl
  unfold validFacts
  rw [valid_spec]
  refine ⟨hst, by have := P.known; omega, P.has_sound h0, ?_, P.has_sound h0, ?_⟩
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
the update relation and the work relation, and the record it writes is
structurally valid. -/
theorem reachable_client_save {p : Params} {x y : Rq} {ev : Ev} (hr : Reachable M p (enc x)) (hc : ev.client = true)
    (h : step M p (enc x) ev.toEvent = some (enc y)) :
    (x.st ≠ 0 → step M p (enc x) (updFacts x y) ≠ none) ∧
    ((x.st ≠ 0 ∨ ev.newRecord = true) → step M p (enc x) (workFacts x y) ≠ none) ∧
    (y.st ≠ 0 → step M p (enc y) (validFacts y) ≠ none) :=
  ⟨fun hs => client_update_ok (reachable_inv hr) hs hc h, fun hs => client_work_ok (reachable_inv hr) hs hc h,
    fun h0 => props_valid (step_inv p _ _ _ (reachable_inv hr) h) h0⟩

end KanderFormal.Proofs.IssueResult
