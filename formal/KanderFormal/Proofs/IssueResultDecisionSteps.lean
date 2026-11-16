import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.IssueResultDecision

/-
The step lemmas of the issue close decision machine: each event on an encoded
state evaluates to an explicit `if`, on which `IssueResultDecision.lean` proves
the theorems.
-/
namespace KanderFormal.Proofs.IssueResultDecision

open KanderFormal KanderFormal.Machines.IssueResultDecision KanderFormal.Proofs.Scalar

abbrev M := issueResultDecisionMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `ds`: 0 none, 1 declined, 2 uncertain,
3 closed, 4 observed, 5 superseded, 6 rejected, 7 other. `acc`: 0 open,
1 uncertain, 2 refusal, 3 consent, 4 returned. -/
structure Rq where
  ds : Nat
  acc : Nat

def enc (x : Rq) : State := [("ds", enumV x.ds), ("acc", enumV x.acc)]

theorem enc_inj {x y : Rq} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _⟩ := x
  obtain ⟨_, _⟩ := y
  simp [enc, enumV] at h
  obtain ⟨rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) : ∃ x, s = enc x ∧ x.ds < 8 ∧ x.acc < 5 := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2)], hn =>
    simp [issueResultDecisionMachine] at hn
    obtain ⟨rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "ds" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "acc" v2 (by simp [lookup])
    cases (rfl : M.fieldTy "ds" = some (.scalar (.enum "dstat"))).symm.trans ht1
    cases (rfl : M.fieldTy "acc" = some (.scalar (.enum "scan"))).symm.trans ht2
    obtain ⟨d, rfl, hd⟩ := conforms_enum (rfl : M.enumSize "dstat" = some 8) c1
    obtain ⟨c, rfl, hcc⟩ := conforms_enum (rfl : M.enumSize "scan" = some 5) c2
    exact ⟨⟨d, c⟩, rfl, hd, hcc⟩

/-! ## Events on an encoded state -/

theorem find_appendNo : M.findEvent "appendNo" = some appendNoEvent := rfl
theorem find_appendYes : M.findEvent "appendYes" = some appendYesEvent := rfl
theorem find_closeDone : M.findEvent "closeDone" = some closeDoneEvent := rfl
theorem find_closeRejected : M.findEvent "closeRejected" = some closeRejectedEvent := rfl
theorem find_reconcile : M.findEvent "reconcile" = some reconcileEvent := rfl
theorem find_visit : M.findEvent "visit" = some visitEvent := rfl
theorem find_settle : M.findEvent "settle" = some settleEvent := rfl
theorem find_valid : M.findEvent "valid" = some validEvent := rfl
theorem ctors_dstat : M.ctorsOf "dstat" =
    some ["none", "declined", "uncertain", "closed", "observed", "superseded", "rejected", "other"] := rfl
theorem ctors_scan : M.ctorsOf "scan" = some ["open", "uncertain", "refusal", "consent", "returned"] := rfl
theorem size_dstat : M.enumSize "dstat" = some 8 := rfl
theorem size_scan : M.enumSize "scan" = some 5 := rfl
theorem ty_ds : M.fieldTy "ds" = some (.scalar (.enum "dstat")) := rfl
theorem ty_acc : M.fieldTy "acc" = some (.scalar (.enum "scan")) := rfl

macro "dc_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_appendNo, find_appendYes, find_closeDone, find_closeRejected, find_reconcile,
      find_visit, find_settle, find_valid, ctors_dstat, ctors_scan, size_dstat, size_scan, ty_ds, ty_acc, lookup,
      appendNoEvent, appendYesEvent, closeDoneEvent, closeRejectedEvent, reconcileEvent, visitEvent, settleEvent,
      validEvent, classify, consentStatus, dsIs, dsC, decIs, accIs, scanC, boolArg, dstatArg, bindArgs, Ty.conforms,
      STy.conforms, eval, evalCmp, boolAnd, boolOr, boolNot, indexOf, enumV, boolV, natV, f, a, tt, andE, orE, notE,
      eq, ne, evalAssigns, applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite,
      ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_appendNo (p : Params) (x : Rq) :
    step M p (enc x) ⟨"appendNo", []⟩ = if x.ds = 0 then some (enc { x with ds := 1 }) else none := by
  dc_simp [] <;> ite_close

theorem step_appendYes (p : Params) (x : Rq) :
    step M p (enc x) ⟨"appendYes", []⟩ = if x.ds = 0 then some (enc { x with ds := 2 }) else none := by
  dc_simp [] <;> ite_close

theorem step_closeDone (p : Params) (x : Rq) :
    step M p (enc x) ⟨"closeDone", []⟩ = if x.ds = 2 then some (enc { x with ds := 3 }) else none := by
  dc_simp [] <;> ite_close

theorem step_closeRejected (p : Params) (x : Rq) :
    step M p (enc x) ⟨"closeRejected", []⟩ = if x.ds = 2 then some (enc { x with ds := 6 }) else none := by
  dc_simp [] <;> ite_close

theorem step_reconcile (p : Params) (x : Rq) (closed same : Bool) :
    step M p (enc x) ⟨"reconcile", [boolV closed, boolV same]⟩ =
      if x.ds = 2 ∧ (closed = true ∨ same = false) then
        some (enc { x with ds := if closed = true then 4 else 5 }) else none := by
  cases closed <;> cases same <;> dc_simp [] <;> ite_close

/-- The class of the earlier decision `d` as a blocker: 0 passes, 1 uncertain,
2 refusal, 3 consent, 4 returned. -/
def cls (d : Nat) (rc nn : Bool) : Nat :=
  if d = 2 then 1
  else if d = 1 ∧ rc = false then (if nn = true then 4 else 2)
  else if (d = 3 ∨ d = 4 ∨ d = 5 ∨ d = 6) ∧ ¬(d = 6 ∧ rc = true) then 3 else 0

set_option maxHeartbeats 8000000 in
theorem step_visit (p : Params) (x : Rq) (hx : x.acc < 5) (d : Nat) (rc nn : Bool) :
    step M p (enc x) ⟨"visit", [enumV d, boolV rc, boolV nn]⟩ =
      if d < 8 then some (enc { x with acc := if x.acc = 0 then cls d rc nn else x.acc }) else none := by
  by_cases h : d < 8
  · simp only [h, ite_true]
    have hh : d = 0 ∨ d = 1 ∨ d = 2 ∨ d = 3 ∨ d = 4 ∨ d = 5 ∨ d = 6 ∨ d = 7 := by omega
    rcases hh with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl <;>
    by_cases ha : x.acc = 0 <;> cases rc <;> cases nn <;> dc_simp [cls, ha, hx] <;> ite_close
  · simp only [h, ite_false]
    dc_simp [h, hx]

set_option maxHeartbeats 4000000 in
theorem step_settle (p : Params) (x : Rq) (nd : Nat) (idSame : Bool) :
    step M p (enc x) ⟨"settle", [enumV nd, boolV idSame]⟩ =
      if nd < 8 ∧ idSame = true ∧ x.ds ≠ 0 ∧ (x.ds = 2 ∨ nd = x.ds) then some (enc x) else none := by
  by_cases h : nd < 8 <;> cases idSame <;> dc_simp [h] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_valid (p : Params) (x : Rq) (yes : Bool) (v : Nat) :
    step M p (enc x) ⟨"valid", [boolV yes, enumV v]⟩ =
      if v < 8 ∧ ((yes = true ∧ 2 ≤ v ∧ v ≤ 6) ∨ (yes = false ∧ v = 1)) then some (enc x) else none := by
  by_cases h : v < 8
  · simp only [h, true_and]
    (have hh : v = 0 ∨ v = 1 ∨ v = 2 ∨ v = 3 ∨ v = 4 ∨ v = 5 ∨ v = 6 ∨ v = 7 := by omega
     rcases hh with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl) <;> cases yes <;> dc_simp [] <;> ite_close
  · simp only [h, false_and, ite_false]
    dc_simp [h]

end KanderFormal.Proofs.IssueResultDecision
