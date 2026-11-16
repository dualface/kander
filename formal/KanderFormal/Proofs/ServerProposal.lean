import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ServerProposal

/-
Proof obligations of the server proposal slot machine: a dispatch ID has at
most one proposal when every creation goes through the slot, whatever the
number of requests and creations. They hold for every value of `maxCount`.
-/
namespace KanderFormal.Proofs.ServerProposal

open KanderFormal KanderFormal.Machines.ServerProposal KanderFormal.Proofs.Scalar

abbrev M := serverProposalMachine

theorem wellTyped : M.wellTyped = true := by decide

def enc (n : Nat) : State := [("count", natV n)]

theorem enc_inj {x y : Nat} (h : enc x = enc y) : x = y := by
  simp [enc, natV] at h; exact h

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) : ∃ n, s = enc n ∧ n ≤ p "maxCount" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v)], hn =>
    simp [serverProposalMachine] at hn
    subst hn
    obtain ⟨t, ht, c⟩ := hc "count" v (by simp [lookup])
    cases (rfl : M.fieldTy "count" = some (.scalar (.nat "maxCount"))).symm.trans ht
    obtain ⟨n, rfl, hn⟩ := conforms_nat c
    exact ⟨n, rfl, hn⟩

theorem find_admit : M.findEvent "admit" = some admitEvent := rfl
theorem find_repeat : M.findEvent "repeat" = some repeatEvent := rfl
theorem find_lookup : M.findEvent "lookup" = some lookupEvent := rfl
theorem ty_count : M.fieldTy "count" = some (.scalar (.nat "maxCount")) := rfl

macro "sp_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_admit, find_repeat, find_lookup, ty_count, lookup, admitEvent, repeatEvent,
      lookupEvent, bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, boolAnd, boolOr, boolNot, enumV, boolV, natV,
      f, eq, ge, evalAssigns, applyUpdates, enc, $ts,*])

theorem step_admit (p : Params) (n : Nat) :
    step M p (enc n) ⟨"admit", []⟩ = if n = 0 ∧ 1 ≤ p "maxCount" then some (enc 1) else none := by
  by_cases h : 1 ≤ p "maxCount" <;> sp_simp [h] <;> split <;> simp_all

theorem step_repeat (p : Params) (n : Nat) :
    step M p (enc n) ⟨"repeat", []⟩ = if 1 ≤ n then some (enc n) else none := by
  sp_simp []

theorem step_lookup (p : Params) (n : Nat) :
    step M p (enc n) ⟨"lookup", []⟩ = if n = 1 then some (enc n) else none := by
  sp_simp []

/-! ## Theorem 5: a dispatch ID has one proposal -/

/-- Theorem 5: a creation takes the slot only when no proposal has the dispatch
ID, and the slot then holds exactly one. -/
theorem admit_spec (p : Params) (n : Nat) :
    step M p (enc n) ⟨"admit", []⟩ ≠ none ↔ n = 0 ∧ 1 ≤ p "maxCount" := by
  rw [step_admit]
  by_cases h : n = 0 ∧ 1 ≤ p "maxCount" <;> simp [h]

theorem admit_result (p : Params) (n : Nat) {s' : State} (h : step M p (enc n) ⟨"admit", []⟩ = some s') :
    s' = enc 1 := by
  rw [step_admit] at h
  split at h
  · cases h; rfl
  · cases h

/-- Theorem 5: a second creation of the dispatch ID never takes the slot: once
a creation was admitted, the next one finds the proposal instead. -/
theorem admit_once (p : Params) (n : Nat) {s' : State} (h : step M p (enc n) ⟨"admit", []⟩ = some s') :
    step M p s' ⟨"admit", []⟩ = none ∧ step M p s' ⟨"repeat", []⟩ ≠ none ∧ step M p s' ⟨"lookup", []⟩ ≠ none := by
  rw [admit_result p n h, step_admit, step_repeat, step_lookup]
  simp

/-- Theorem 5: a creation either takes the slot or finds a proposal, never
both, and a slot that holds a proposal is never taken. -/
theorem admit_repeat_exclusive (p : Params) (n : Nat) :
    step M p (enc n) ⟨"admit", []⟩ ≠ none → step M p (enc n) ⟨"repeat", []⟩ = none := by
  rw [admit_spec, step_repeat]
  rintro ⟨h0, -⟩
  simp [h0]

/-- Theorem 5: a creation finds the proposal exactly when the slot holds one. -/
theorem repeat_spec (p : Params) (n : Nat) :
    step M p (enc n) ⟨"repeat", []⟩ ≠ none ↔ 1 ≤ n := by
  rw [step_repeat]
  by_cases h : 1 ≤ n <;> simp [h]

/-- Theorem 5: the formal preparation looks up exactly one proposal; a slot
with two (a hand-edited task directory) is ambiguous and refused. -/
theorem lookup_spec (p : Params) (n : Nat) :
    step M p (enc n) ⟨"lookup", []⟩ ≠ none ↔ n = 1 := by
  rw [step_lookup]
  by_cases h : n = 1 <;> simp [h]

theorem lookup_ambiguous (p : Params) (n : Nat) (h : 2 ≤ n) : step M p (enc n) ⟨"lookup", []⟩ = none := by
  rw [step_lookup]; simp; omega

theorem ambiguous_not_admitted (p : Params) (n : Nat) (h : 1 ≤ n) : step M p (enc n) ⟨"admit", []⟩ = none := by
  rw [step_admit]; simp; omega

/-! ## Theorem 9: the invariant -/

/-- Every reachable slot holds at most one proposal. -/
def Inv (p : Params) (s : State) : Prop := M.StateConforms p s ∧ ∀ n, s = enc n → n ≤ 1

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro n hn
  have : n = 0 := enc_inj (hn.symm.trans rfl)
  omega

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨n, rfl, hn⟩ := shape hc
  have hle := hp n rfl
  intro m hm
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨name, vs⟩ := e
  simp only at hname hb
  subst hname
  have hcases : ev = admitEvent ∨ ev = repeatEvent ∨ ev = lookupEvent := by simpa [serverProposalMachine] using hmem
  rcases hcases with rfl | rfl | rfl
  · have hv : vs = [] := (bindArgs_nil (show bindArgs M p [] vs = some args from hb)).1
    subst hv
    change step M p (enc n) ⟨"admit", []⟩ = some s' at h
    rw [step_admit] at h
    split at h
    · cases h; have := enc_inj hm; omega
    · cases h
  · have hv : vs = [] := (bindArgs_nil (show bindArgs M p [] vs = some args from hb)).1
    subst hv
    change step M p (enc n) ⟨"repeat", []⟩ = some s' at h
    rw [step_repeat] at h
    split at h
    · cases h; have := enc_inj hm; omega
    · cases h
  · have hv : vs = [] := (bindArgs_nil (show bindArgs M p [] vs = some args from hb)).1
    subst hv
    change step M p (enc n) ⟨"lookup", []⟩ = some s' at h
    rw [step_lookup] at h
    split at h
    · cases h; have := enc_inj hm; omega
    · cases h

def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 5, for every number of requests and creations: in every reachable
state the slot holds at most one proposal, so a dispatch ID identifies one
proposal. -/
theorem reachable_unique {p : Params} {n : Nat} (h : Reachable M p (enc n)) : n ≤ 1 :=
  (reachable_inv h).2 n rfl

end KanderFormal.Proofs.ServerProposal
