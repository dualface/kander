import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.StartOrder

/-
The step lemmas of the start order machine: each event on an encoded state
evaluates to an explicit `if`, on which `StartOrder.lean` proves the theorems.
-/
namespace KanderFormal.Proofs.StartOrder

open KanderFormal KanderFormal.Machines.StartOrder KanderFormal.Proofs.Scalar

abbrev M := startOrderMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `st`: 0 none, 1 pending, 2 accepted,
3 started, 4 failed, 5 cancelled, 6 expired. `bgen` is the bound generation,
0 while unbound. -/
structure Ord where
  st : Nat
  bgen : Nat

def enc (x : Ord) : State := [("st", enumV x.st), ("bgen", natV x.bgen)]

theorem enc_inj {x y : Ord} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _⟩ := x
  obtain ⟨_, _⟩ := y
  simp [enc, enumV, natV] at h
  obtain ⟨rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.st < 7 ∧ x.bgen ≤ p "maxGen" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2)], hn =>
    simp [startOrderMachine] at hn
    obtain ⟨rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "st" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "bgen" v2 (by simp [lookup])
    cases (rfl : M.fieldTy "st" = some (.scalar (.enum "st"))).symm.trans ht1
    cases (rfl : M.fieldTy "bgen" = some (.scalar (.nat "maxGen"))).symm.trans ht2
    obtain ⟨r, rfl, hr⟩ := conforms_enum (rfl : M.enumSize "st" = some 7) c1
    obtain ⟨g, rfl, hg⟩ := conforms_nat c2
    exact ⟨⟨r, g⟩, rfl, hr, hg⟩

/-! ## Events on an encoded state -/

theorem find_create : M.findEvent "create" = some createEvent := rfl
theorem find_accept : M.findEvent "accept" = some acceptEvent := rfl
theorem find_claim : M.findEvent "claim" = some claimEvent := rfl
theorem find_confirm : M.findEvent "confirm" = some confirmEvent := rfl
theorem find_rollback : M.findEvent "rollback" = some rollbackEvent := rfl
theorem find_supersede : M.findEvent "supersede" = some supersedeEvent := rfl
theorem find_fail : M.findEvent "fail" = some failEvent := rfl
theorem find_cancel : M.findEvent "cancel" = some cancelEvent := rfl
theorem find_expire : M.findEvent "expire" = some expireEvent := rfl
theorem find_hostLost : M.findEvent "hostLost" = some hostLostEvent := rfl
theorem find_preempt : M.findEvent "preempt" = some preemptEvent := rfl
theorem ctors_st : M.ctorsOf "st" =
    some ["none", "pending", "accepted", "started", "failed", "cancelled", "expired"] := rfl
theorem size_st : M.enumSize "st" = some 7 := rfl
theorem ty_st : M.fieldTy "st" = some (.scalar (.enum "st")) := rfl
theorem ty_bgen : M.fieldTy "bgen" = some (.scalar (.nat "maxGen")) := rfl

macro "so_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_create, find_accept, find_claim, find_confirm, find_rollback, find_supersede,
      find_fail, find_cancel, find_expire, find_hostLost, find_preempt, ctors_st, size_st, ty_st, ty_bgen,
      lookup, createEvent, acceptEvent, claimEvent, confirmEvent, rollbackEvent, supersedeEvent, failEvent,
      cancelEvent, expireEvent, hostLostEvent, preemptEvent, genArg, boolArg, stIs, st, unbound, boundTo,
      bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr, boolNot, indexOf, enumV,
      boolV, natV, f, a, andE, orE, eq, ne, zero, evalAssigns, applyUpdates, enc, or_assoc, and_assoc,
      Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_create (p : Params) (x : Ord) :
    step M p (enc x) ⟨"create", []⟩ = if x.st = 0 then some (enc ⟨1, x.bgen⟩) else none := by
  so_simp [] <;> ite_close

theorem step_accept (p : Params) (x : Ord) (target : Bool) :
    step M p (enc x) ⟨"accept", [boolV target]⟩ =
      if x.st = 1 ∧ target = true then some (enc ⟨2, x.bgen⟩) else none := by
  cases target <;> so_simp [] <;> ite_close

set_option maxHeartbeats 4000000 in
theorem step_claim (p : Params) (x : Ord) (target task : Bool) (gen : Nat) :
    step M p (enc x) ⟨"claim", [boolV target, boolV task, natV gen]⟩ =
      if gen ≤ p "maxGen" ∧ x.st = 2 ∧ x.bgen = 0 ∧ target = true ∧ task = true ∧ gen ≠ 0 then
        some (enc ⟨2, gen⟩) else none := by
  by_cases h1 : gen ≤ p "maxGen" <;> cases target <;> cases task <;> so_simp [h1] <;> ite_close

theorem step_confirm (p : Params) (x : Ord) (gen : Nat) :
    step M p (enc x) ⟨"confirm", [natV gen]⟩ =
      if gen ≤ p "maxGen" ∧ x.st = 2 ∧ x.bgen ≠ 0 ∧ x.bgen = gen then some (enc ⟨3, x.bgen⟩) else none := by
  by_cases h1 : gen ≤ p "maxGen" <;> so_simp [h1] <;> ite_close

theorem step_rollback (p : Params) (x : Ord) (gen : Nat) :
    step M p (enc x) ⟨"rollback", [natV gen]⟩ =
      if gen ≤ p "maxGen" ∧ x.st = 2 ∧ x.bgen ≠ 0 ∧ x.bgen = gen then some (enc ⟨4, x.bgen⟩) else none := by
  by_cases h1 : gen ≤ p "maxGen" <;> so_simp [h1] <;> ite_close

theorem step_supersede (p : Params) (x : Ord) (gen : Nat) :
    step M p (enc x) ⟨"supersede", [natV gen]⟩ =
      if gen ≤ p "maxGen" ∧ x.st = 2 ∧ x.bgen ≠ 0 ∧ x.bgen = gen then some (enc ⟨4, x.bgen⟩) else none := by
  by_cases h1 : gen ≤ p "maxGen" <;> so_simp [h1] <;> ite_close

theorem step_fail (p : Params) (x : Ord) (target : Bool) :
    step M p (enc x) ⟨"fail", [boolV target]⟩ =
      if x.st = 2 ∧ x.bgen = 0 ∧ target = true then some (enc ⟨4, x.bgen⟩) else none := by
  cases target <;> so_simp [] <;> ite_close

theorem step_cancel (p : Params) (x : Ord) :
    step M p (enc x) ⟨"cancel", []⟩ =
      if x.st = 1 ∨ (x.st = 2 ∧ x.bgen = 0) then some (enc ⟨5, x.bgen⟩) else none := by
  so_simp [] <;> ite_close

theorem step_expire (p : Params) (x : Ord) (due : Bool) :
    step M p (enc x) ⟨"expire", [boolV due]⟩ =
      if x.st = 1 ∧ due = true then some (enc ⟨6, x.bgen⟩) else none := by
  cases due <;> so_simp [] <;> ite_close

theorem step_hostLost (p : Params) (x : Ord) (lost : Bool) :
    step M p (enc x) ⟨"hostLost", [boolV lost]⟩ =
      if x.st = 2 ∧ x.bgen = 0 ∧ lost = true then some (enc ⟨4, x.bgen⟩) else none := by
  cases lost <;> so_simp [] <;> ite_close

theorem step_preempt (p : Params) (x : Ord) :
    step M p (enc x) ⟨"preempt", []⟩ =
      if (x.st = 1 ∨ x.st = 2) ∧ x.bgen = 0 then some (enc ⟨4, x.bgen⟩) else none := by
  so_simp [] <;> ite_close

end KanderFormal.Proofs.StartOrder
