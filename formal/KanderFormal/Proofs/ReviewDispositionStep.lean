import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ReviewDisposition

/-
The review disposition machine on an encoded state: every event evaluates to an
explicit `if` on the components of the state. The theorems of
`Proofs/ReviewDisposition.lean` are proved on these forms.
-/
namespace KanderFormal.Proofs.ReviewDisposition

open KanderFormal KanderFormal.Machines.ReviewDisposition KanderFormal.Proofs.Scalar

set_option maxHeartbeats 2000000

abbrev M := reviewDispositionMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. -/
structure Dp where
  assigned : Bool
  chain : Bool
  closed : Bool

def enc (x : Dp) : State :=
  [("assigned", boolV x.assigned), ("chain", boolV x.chain), ("closed", boolV x.closed)]

theorem enc_inj {x y : Dp} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _⟩ := x
  obtain ⟨_, _, _⟩ := y
  simp [enc, boolV] at h
  obtain ⟨rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) : ∃ x, s = enc x := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3)], hn =>
    simp [reviewDispositionMachine] at hn
    obtain ⟨rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "assigned" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "chain" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "closed" v3 (by simp [lookup])
    cases (rfl : M.fieldTy "assigned" = some (.scalar .bool)).symm.trans ht1
    cases (rfl : M.fieldTy "chain" = some (.scalar .bool)).symm.trans ht2
    cases (rfl : M.fieldTy "closed" = some (.scalar .bool)).symm.trans ht3
    obtain ⟨b1, rfl⟩ := conforms_bool c1
    obtain ⟨b2, rfl⟩ := conforms_bool c2
    obtain ⟨b3, rfl⟩ := conforms_bool c3
    exact ⟨⟨b1, b2, b3⟩, rfl⟩

/-! ## Events on an encoded state -/

theorem find_assign : M.findEvent "assign" = some assignEvent := rfl
theorem find_close : M.findEvent "close" = some closeEvent := rfl
theorem find_submit : M.findEvent "submit" = some submitEvent := rfl
theorem find_withdraw : M.findEvent "withdraw" = some withdrawEvent := rfl
theorem size_status : M.enumSize "status" = some 8 := rfl
theorem size_dup : M.enumSize "dup" = some 3 := rfl
theorem size_ref : M.enumSize "ref" = some 3 := rfl
theorem ctors_status : M.ctorsOf "status" =
    some ["confirmed", "fixed", "rejected", "unverifiable", "waived", "deferred", "withdrawn", "other"] := rfl
theorem ctors_dup : M.ctorsOf "dup" = some ["none", "same", "conflict"] := rfl
theorem ctors_ref : M.ctorsOf "ref" = some ["empty", "last", "other"] := rfl

theorem ty_assigned : M.fieldTy "assigned" = some (.scalar .bool) := rfl
theorem ty_chain : M.fieldTy "chain" = some (.scalar .bool) := rfl
theorem ty_closed : M.fieldTy "closed" = some (.scalar .bool) := rfl

macro "rd_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_assign, find_close, find_submit, find_withdraw, size_status, size_dup, size_ref,
      ty_assigned, ty_chain, ty_closed, ctors_status, ctors_dup, ctors_ref, lookup, assignEvent, closeEvent,
      submitEvent, withdrawEvent, submitFacts, withdrawFacts, statusArg, dupArg, refArg, statusIs, dupIs, refIs,
      writableE, chainE, statusE, waiverRoleE, allArgs, boolArgs, bindArgs, Ty.conforms, STy.conforms, eval,
      evalCmp, natAdd, boolAnd, boolOr, boolNot, indexOf, enumV, boolV, natV, f, a, tt, andE, orE, notE, eq,
      evalAssigns, applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite,
      ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

/-- The statuses `submit` accepts for a tier: not `withdrawn`, not `other`, not
`deferred` for a gate tier, and one of `fixed`, `rejected`, `deferred` for the
others. Statuses: 0 confirmed, 1 fixed, 2 rejected, 3 unverifiable, 4 waived,
5 deferred, 6 withdrawn, 7 other. -/
abbrev StatusOk (st : Nat) (gate : Bool) : Prop :=
  st ≠ 6 ∧ st ≠ 7 ∧ (gate = true ∧ st ≠ 5 ∨ gate = false ∧ (st = 1 ∨ st = 5 ∨ st = 2))

/-- The chain rule. `dup`: 0 none, 1 same, 2 conflict. `ref`: 0 empty, 1 last,
2 other. -/
abbrev ChainOk (chain : Bool) (dup ref : Nat) : Prop :=
  dup ≠ 2 ∧ (dup ≠ 1 ∨ chain = true) ∧ (dup = 1 ∨ ref = 0 ∧ chain = false ∨ ref = 1 ∧ chain = true)

theorem step_assign (p : Params) (x : Dp) :
    step M p (enc x) ⟨"assign", []⟩ =
      if x.assigned = false then some (enc { x with assigned := true }) else none := by
  by_cases h0 : x.assigned = true <;> rd_simp [h0] <;> ite_close

theorem step_close (p : Params) (x : Dp) :
    step M p (enc x) ⟨"close", []⟩ =
      if x.closed = false then some (enc { x with closed := true }) else none := by
  by_cases h0 : x.closed = true <;> rd_simp [h0] <;> ite_close

theorem step_submit (p : Params) (x : Dp) (st du rf : Nat) (g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc x) ⟨"submit", [enumV st, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3,
        boolV e4, boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ =
      if st < 8 ∧ du < 3 ∧ rf < 3 ∧ x.closed = false ∧ x.assigned = true ∧
          (e1 = true ∧ e2 = true ∧ e3 = true ∧ e4 = true ∧ e5 = true ∧ e6 = true ∧ e7 = true ∧ e8 = true ∧
            e9 = true ∧ e10 = true ∧ e11 = true ∧ e12 = true) ∧
          StatusOk st g ∧ (st ≠ 4 ∨ se = true) ∧ ChainOk x.chain du rf then
        some (enc { x with chain := true }) else none := by
  by_cases h1 : st < 8 <;> by_cases h2 : du < 3 <;> by_cases h3 : rf < 3 <;> by_cases h4 : x.closed = true <;>
    by_cases h5 : x.assigned = true <;> rd_simp [h1, h2, h3, h4, h5, StatusOk, ChainOk] <;> ite_close

theorem step_withdraw (p : Params) (x : Dp) (du rf : Nat) (e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12 : Bool) :
    step M p (enc x) ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV e5,
        boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩ =
      if du < 3 ∧ rf < 3 ∧ x.closed = false ∧ x.assigned = true ∧
          (e1 = true ∧ e2 = true ∧ e3 = true ∧ e4 = true ∧ e5 = true ∧ e6 = true ∧ e7 = true ∧ e8 = true ∧
            e9 = true ∧ e10 = true ∧ e11 = true ∧ e12 = true) ∧ ChainOk x.chain du rf then
        some (enc { x with chain := true }) else none := by
  by_cases h2 : du < 3 <;> by_cases h3 : rf < 3 <;> by_cases h4 : x.closed = true <;>
    by_cases h5 : x.assigned = true <;> rd_simp [h2, h3, h4, h5, ChainOk] <;> ite_close

end KanderFormal.Proofs.ReviewDisposition
