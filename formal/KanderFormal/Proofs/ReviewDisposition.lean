import KanderFormal.Proofs.ReviewDispositionCases

/-
Proof obligations of the review disposition machine (M8). Every statement holds
for every value of the (empty) parameter list, for every finding and member, and
for every number of records: one instance describes one (finding, card) pair,
the chain of its records only enters through the state `chain`, and the number
of findings, members, and records of the run only through the Boolean facts the
shell computes from the persisted records.

In `submit` the facts are, in order: `expect` (the card revision and column),
`exec` and `full` (the execution authorization), `current` (the card owner is the
author), `ledger` (the assignment exists), `available` (the findings of the run
can be obtained), `shape` (the identity of the record), `authorOk`, `fieldsOk`,
`bound` (the withdrawal fields match the status), `evidenceOk` (the fix
evidence), and `waiverOk`. In `withdraw`, `current`, `exec`, and `full` are
replaced by `declared` (the record carries a withdrawal), `excluded` (the member
left the batch), and `named` (the withdrawal names its exclusion).
-/
namespace KanderFormal.Proofs.ReviewDisposition

open KanderFormal KanderFormal.Machines.ReviewDisposition KanderFormal.Proofs.Scalar

set_option maxHeartbeats 2000000

/-! ## The invariant -/

/-- A pair has records only when it is assigned. -/
structure Props (z : Dp) : Prop where
  chain : z.chain = true → z.assigned = true

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ z, s = enc z → Props z

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro z hz
  have : z = ⟨false, false, false⟩ := enc_inj (hz.symm.trans rfl)
  subst this
  exact ⟨by simp⟩

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨z, rfl⟩ := shape hc
  obtain ⟨P1⟩ := hp z rfl
  intro y hy
  rcases accepted_cases h with rfl | rfl |
    ⟨st, du, rf, g, se, e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12, rfl⟩ |
    ⟨du, rf, e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12, rfl⟩
  · rw [step_assign] at h; split at h <;> cases h; have := enc_inj hy; subst this
    exact ⟨by simp⟩
  · rw [step_close] at h; split at h <;> cases h; have := enc_inj hy; subst this
    exact ⟨by simpa using P1⟩
  · rw [step_submit] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; exact ⟨fun _ => hg.2.2.2.2.1⟩
  · rw [step_withdraw] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; exact ⟨fun _ => hg.2.2.2.1⟩

/-- The review disposition machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

theorem reachable_props {p : Params} {z : Dp} (hr : Reachable M p (enc z)) : Props z := (reachable_inv hr).2 z rfl

/-- Theorem 5: on every reachable pair, a record exists only for an assigned
pair. -/
theorem reachable_chain_assigned {p : Params} {z : Dp} (hr : Reachable M p (enc z)) (hc : z.chain = true) :
    z.assigned = true := (reachable_props hr).chain hc

/-! ## Assignment -/

/-- A pair is assigned once. -/
theorem assign_once (p : Params) (z : Dp) (h : z.assigned = true) : step M p (enc z) ⟨"assign", []⟩ = none := by
  rw [step_assign]; simp [h]

/-! ## Theorem 5: which records a pair accepts -/

/-- An accepted author record: the batch is open, the pair is assigned, every
fact holds, the status is allowed for the tier of the finding, a waiver is for a
security role, and the chain rule holds. -/
theorem submit_spec {p : Params} {z y : Dp} {s' : State} {st du rf : Nat}
    {g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool}
    (h : step M p (enc z) ⟨"submit", [enumV st, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = some s')
    (hy : s' = enc y) :
    z.closed = false ∧ z.assigned = true ∧
      (e1 = true ∧ e2 = true ∧ e3 = true ∧ e4 = true ∧ e5 = true ∧ e6 = true ∧ e7 = true ∧ e8 = true ∧
        e9 = true ∧ e10 = true ∧ e11 = true ∧ e12 = true) ∧
      StatusOk st g ∧ (st = 4 → se = true) ∧ ChainOk z.chain du rf ∧ y = { z with chain := true } := by
  rw [step_submit] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, hc, ha, hf, hs, hw, hch⟩ := hg
    exact ⟨hc, ha, hf, hs, fun h4 => by rcases hw with hw | hw <;> simp_all, hch, rfl⟩
  · cases h

/-- Theorem 5: a finding that is not assigned to the card takes no record. -/
theorem submit_refused_unassigned (p : Params) (z : Dp) (hn : z.assigned = false) (st du rf : Nat)
    (g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV st, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]; simp [hn]

/-- Theorem 5: a gate finding (blocking, high, or medium) cannot be deferred. -/
theorem submit_refused_gate_deferred (p : Params) (z : Dp) (du rf : Nat) (se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV 5, enumV du, enumV rf, boolV true, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]; simp [StatusOk]

/-- Theorem 5: a finding below the gate takes only `fixed`, `deferred`, or
`rejected` (so not `confirmed`, `unverifiable`, or `waived`). -/
theorem submit_refused_nongate (p : Params) (z : Dp) (st du rf : Nat) (hs : st = 0 ∨ st = 3 ∨ st = 4)
    (se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV st, enumV du, enumV rf, boolV false, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]
  rcases hs with rfl | rfl | rfl <;> simp [StatusOk]

/-- Theorem 5: a waiver is for a security role only. -/
theorem submit_refused_waiver_role (p : Params) (z : Dp) (du rf : Nat) (g e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV 4, enumV du, enumV rf, boolV g, boolV false, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]; simp

/-- Theorem 5: a status outside the seven values is refused. -/
theorem submit_refused_other_status (p : Params) (z : Dp) (du rf : Nat)
    (g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV 7, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]; simp [StatusOk]

/-! ## Theorem 7: a withdrawal needs the exclusion of the member -/

/-- Theorem 7: `submit` never writes a withdrawal: the status `withdrawn` is
refused whatever the facts. -/
theorem submit_refuses_withdrawn (p : Params) (z : Dp) (du rf : Nat)
    (g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV 6, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]; simp [StatusOk]

/-- Theorem 7: an accepted withdrawal had the withdrawal declared, the member
excluded, and the exclusion named, on an open batch with an assigned finding. -/
theorem withdraw_spec {p : Params} {z y : Dp} {s' : State} {du rf : Nat}
    {e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool}
    (h : step M p (enc z) ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV e5,
        boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = some s') (hy : s' = enc y) :
    z.closed = false ∧ z.assigned = true ∧ e2 = true ∧ e3 = true ∧ e4 = true ∧
      (e1 = true ∧ e5 = true ∧ e6 = true ∧ e7 = true ∧ e8 = true ∧ e9 = true ∧ e10 = true ∧ e11 = true ∧
        e12 = true) ∧ ChainOk z.chain du rf ∧ y = { z with chain := true } := by
  rw [step_withdraw] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this
    obtain ⟨-, -, hc, ha, hf, hch⟩ := hg
    obtain ⟨h1, h2, h3, h4, h5, h6, h7, h8, h9, h10, h11, h12⟩ := hf
    exact ⟨hc, ha, h2, h3, h4, ⟨h1, h5, h6, h7, h8, h9, h10, h11, h12⟩, hch, rfl⟩
  · cases h

/-- Theorem 7: a member that did not leave the batch takes no withdrawal. -/
theorem withdraw_refused_not_excluded (p : Params) (z : Dp) (du rf : Nat) (e1 e2 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV false, boolV e4, boolV e5,
        boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_withdraw]; simp

/-- Theorem 7: a withdrawal that does not name the exclusion of the member is
refused. -/
theorem withdraw_refused_unnamed (p : Params) (z : Dp) (du rf : Nat) (e1 e2 e3 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV e3, boolV false, boolV e5,
        boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_withdraw]; simp

/-- Theorem 7: a withdrawal for a finding that is not assigned to the card is
refused. -/
theorem withdraw_refused_unassigned (p : Params) (z : Dp) (hn : z.assigned = false) (du rf : Nat)
    (e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV e5,
        boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_withdraw]; simp [hn]

/-! ## Theorem 3: a run with no findings or no assignment takes no record -/

/-- Theorem 3: a record needs the assignment (`ledger`) and the findings of the
run (`available`); a run whose interpretation is pending has neither. -/
theorem submit_refused_no_findings (p : Params) (z : Dp) (st du rf : Nat)
    (g se e1 e2 e3 e4 e7 e8 e9 e10 e11 e12 : Bool) :
    (∀ e6, step M p (enc z) ⟨"submit", [enumV st, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV false, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none) ∧
    (∀ e5, step M p (enc z) ⟨"submit", [enumV st, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV false, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none) := by
  refine ⟨fun e6 => ?_, fun e5 => ?_⟩
  · rw [step_submit]; simp
  · rw [step_submit]; simp

theorem withdraw_refused_no_findings (p : Params) (z : Dp) (du rf : Nat)
    (e1 e2 e3 e4 e7 e8 e9 e10 e11 e12 : Bool) :
    (∀ e6, step M p (enc z) ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV false,
        boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none) ∧
    (∀ e5, step M p (enc z) ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV e5,
        boolV false, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none) := by
  refine ⟨fun e6 => ?_, fun e5 => ?_⟩
  · rw [step_withdraw]; simp
  · rw [step_withdraw]; simp

/-! ## Theorem 6: the chain of a pair -/

/-- Theorem 6: a new record (no record with its identifier is stored) is
accepted only when it names the last record of the chain, or nothing for an
empty chain. -/
theorem submit_chain_spec {p : Params} {z y : Dp} {s' : State} {st rf : Nat}
    {g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool}
    (h : step M p (enc z) ⟨"submit", [enumV st, enumV 0, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = some s')
    (hy : s' = enc y) :
    (rf = 0 ∧ z.chain = false ∨ rf = 1 ∧ z.chain = true) ∧ y.chain = true := by
  obtain ⟨-, -, -, -, -, hch, rfl⟩ := submit_spec h hy
  obtain ⟨-, -, hr⟩ := hch
  refine ⟨?_, rfl⟩
  rcases hr with hr | hr | hr <;> simp_all

/-- Theorem 6: a record that names another record than the last one, or names a
predecessor for an empty chain, or none for a non-empty chain, is refused. -/
theorem submit_stale_predecessor (p : Params) (z : Dp) (st rf : Nat)
    (hr : rf = 2 ∨ rf = 0 ∧ z.chain = true ∨ rf = 1 ∧ z.chain = false)
    (g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV st, enumV 0, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]
  split
  · rename_i hg; obtain ⟨-, -, -, -, -, -, -, -, hch⟩ := hg
    obtain ⟨-, -, hc⟩ := hch
    rcases hc with hc | hc | hc <;> rcases hr with hr | hr | hr <;> simp_all
  · rfl

/-- Theorem 6: the same record again (an identical record is stored) is accepted
and changes nothing, when the facts that hold for a first submission hold again
and the chain is not empty. -/
theorem submit_replay {p : Params} {z : Dp} (hr : Reachable M p (enc z)) (hch : z.chain = true) (st rf : Nat)
    (g se : Bool) (hok : StatusOk st g) (hse : st = 4 → se = true) (hlt : st < 8) (hrf : rf < 3)
    (hc : z.closed = false) :
    step M p (enc z) ⟨"submit", [enumV st, enumV 1, enumV rf, boolV g, boolV se, boolV true, boolV true, boolV true,
        boolV true, boolV true, boolV true, boolV true, boolV true, boolV true, boolV true, boolV true,
        boolV true]⟩ = some (enc z) := by
  have ha := reachable_chain_assigned hr hch
  have e : ({ z with chain := true } : Dp) = z := by cases z; simp_all
  rw [step_submit]
  split
  · rw [e]
  · rename_i hn
    exfalso; apply hn
    refine ⟨hlt, by decide, hrf, hc, ha, ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩, hok, ?_, ?_⟩
    · by_cases h4 : st = 4
      · exact Or.inr (hse h4)
      · exact Or.inl h4
    · refine ⟨by decide, Or.inr hch, Or.inl rfl⟩

/-- Theorem 6: a different record with a stored identifier is refused. -/
theorem submit_conflict (p : Params) (z : Dp) (st rf : Nat) (g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV st, enumV 2, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]; simp [ChainOk]

theorem withdraw_conflict (p : Params) (z : Dp) (rf : Nat) (e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"withdraw", [enumV 2, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV e5, boolV e6,
        boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_withdraw]; simp [ChainOk]

/-- Theorem 6: an identical record that is stored for another chain (so this
chain is empty) is refused as inconsistent. -/
theorem submit_same_needs_chain (p : Params) (z : Dp) (hn : z.chain = false) (st rf : Nat)
    (g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV st, enumV 1, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  rw [step_submit]; simp [ChainOk, hn]

/-- The withdrawal chain follows the same rule: a new withdrawal names the last
record of the chain. -/
theorem withdraw_chain_spec {p : Params} {z y : Dp} {s' : State} {rf : Nat}
    {e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool}
    (h : step M p (enc z) ⟨"withdraw", [enumV 0, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV e5,
        boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = some s') (hy : s' = enc y) :
    (rf = 0 ∧ z.chain = false ∨ rf = 1 ∧ z.chain = true) ∧ y.chain = true := by
  obtain ⟨-, -, -, -, -, -, hch, rfl⟩ := withdraw_spec h hy
  obtain ⟨-, -, hr⟩ := hch
  refine ⟨?_, rfl⟩
  rcases hr with hr | hr | hr <;> simp_all

/-! ## Theorem 8: after the batch closes -/

/-- Theorem 8: once the batch is closed, no author record and no withdrawal is
accepted, whatever the facts and whether or not it repeats a stored record. -/
theorem closed_refused (p : Params) (z : Dp) (hc : z.closed = true) (st du rf : Nat)
    (g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc z) ⟨"submit", [enumV st, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none ∧
      step M p (enc z) ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV e5, boolV e6,
        boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ = none := by
  refine ⟨?_, ?_⟩
  · rw [step_submit]; simp [hc]
  · rw [step_withdraw]; simp [hc]

/-- Once a pair has records or is assigned, no accepted event removes them. -/
theorem written_frozen {p : Params} {z y : Dp} {e : Event} {s' : State} (h : step M p (enc z) e = some s')
    (hy : s' = enc y) : (z.assigned = true → y.assigned = true) ∧ (z.chain = true → y.chain = true) ∧
      (z.closed = true → y.closed = true) := by
  rcases accepted_cases h with rfl | rfl |
    ⟨st, du, rf, g, se, e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12, rfl⟩ |
    ⟨du, rf, e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12, rfl⟩
  · rw [step_assign] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_close] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_submit] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_withdraw] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp

end KanderFormal.Proofs.ReviewDisposition
