import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ServerRequest

/-
The step lemmas of the server request machine: each event on an encoded state
evaluates to an explicit `if`, on which `ServerRequest.lean` proves the
theorems.
-/
namespace KanderFormal.Proofs.ServerRequest

open KanderFormal KanderFormal.Machines.ServerRequest KanderFormal.Proofs.Scalar

abbrev M := serverRequestMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `st`: 0 none, 1 open, 2 accepted,
3 done, 4 rejected, 5 superseded. -/
structure Rq where
  st : Nat
  rev : Nat
  tgen : Nat
  prop : Bool
  prev : Nat

def enc (x : Rq) : State :=
  [("status", enumV x.st), ("rev", natV x.rev), ("tgen", natV x.tgen), ("prop", boolV x.prop),
    ("prev", natV x.prev)]

theorem enc_inj {x y : Rq} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.st < 6 ∧ x.rev ≤ p "maxRev" ∧ x.tgen ≤ p "maxGen" ∧ x.prev ≤ p "maxRev" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5)], hn =>
    simp [serverRequestMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "status" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "rev" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "tgen" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "prop" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "prev" v5 (by simp [lookup])
    cases (rfl : M.fieldTy "status" = some (.scalar (.enum "status"))).symm.trans ht1
    cases (rfl : M.fieldTy "rev" = some (.scalar (.nat "maxRev"))).symm.trans ht2
    cases (rfl : M.fieldTy "tgen" = some (.scalar (.nat "maxGen"))).symm.trans ht3
    cases (rfl : M.fieldTy "prop" = some (.scalar .bool)).symm.trans ht4
    cases (rfl : M.fieldTy "prev" = some (.scalar (.nat "maxRev"))).symm.trans ht5
    obtain ⟨st, rfl, hst⟩ := conforms_enum (rfl : M.enumSize "status" = some 6) c1
    obtain ⟨rv, rfl, hrv⟩ := conforms_nat c2
    obtain ⟨tg, rfl, htg⟩ := conforms_nat c3
    obtain ⟨pr, rfl⟩ := conforms_bool c4
    obtain ⟨pv, rfl, hpv⟩ := conforms_nat c5
    exact ⟨⟨st, rv, tg, pr, pv⟩, rfl, hst, hrv, htg, hpv⟩

/-! ## Events on an encoded state -/

theorem find_bind : M.findEvent "bind" = some bindEvent := rfl
theorem find_create : M.findEvent "create" = some createEvent := rfl
theorem find_recreate : M.findEvent "recreate" = some recreateEvent := rfl
theorem find_answerSame : M.findEvent "answerSame" = some answerSameEvent := rfl
theorem find_answer : M.findEvent "answer" = some answerEvent := rfl
theorem find_supersede : M.findEvent "supersede" = some supersedeEvent := rfl
theorem find_prepare : M.findEvent "prepare" = some prepareEvent := rfl
theorem find_complete : M.findEvent "complete" = some completeEvent := rfl
theorem ctors_status :
    M.ctorsOf "status" = some ["none", "open", "accepted", "done", "rejected", "superseded"] := rfl
theorem size_status : M.enumSize "status" = some 6 := rfl
theorem ty_status : M.fieldTy "status" = some (.scalar (.enum "status")) := rfl
theorem ty_rev : M.fieldTy "rev" = some (.scalar (.nat "maxRev")) := rfl
theorem ty_tgen : M.fieldTy "tgen" = some (.scalar (.nat "maxGen")) := rfl
theorem ty_prop : M.fieldTy "prop" = some (.scalar .bool) := rfl
theorem ty_prev : M.fieldTy "prev" = some (.scalar (.nat "maxRev")) := rfl

macro "rq_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_bind, find_create, find_recreate, find_answerSame, find_answer, find_supersede,
      find_prepare, find_complete, ctors_status, size_status, ty_status, ty_rev, ty_tgen, ty_prop, ty_prev, lookup,
      bindEvent, createEvent, recreateEvent, answerSameEvent, answerEvent, supersedeEvent, prepareEvent,
      completeEvent, claimOk, answerChecks, sameAnswer, answerArgs, actionable, settled, statusIs, ansIs, genArg,
      revArg, boolArg, statusArg, bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr,
      boolNot, indexOf, enumV, boolV, natV, f, a, tt, ff, andE, orE, notE, eq, ne, lt, inc, zero, evalAssigns,
      applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none,
      $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_bind (p : Params) (x : Rq) (taskOk : Bool) (inRev card : Nat) :
    step M p (enc x) ⟨"bind", [boolV taskOk, natV inRev, natV card]⟩ =
      if inRev ≤ p "maxRev" ∧ card ≤ p "maxRev" ∧ taskOk = true ∧ inRev ≠ 0 ∧ inRev = card then some (enc x)
      else none := by
  by_cases h1 : inRev ≤ p "maxRev" <;> by_cases h2 : card ≤ p "maxRev" <;> cases taskOk <;> rq_simp [h1, h2] <;>
    ite_close

set_option maxHeartbeats 4000000 in
theorem step_create (p : Params) (x : Rq) (held standing : Bool) (target : Nat) (prop : Bool) (inRev : Nat) :
    step M p (enc x) ⟨"create", [boolV held, boolV standing, natV target, boolV prop, natV inRev]⟩ =
      if target ≤ p "maxGen" ∧ inRev ≤ p "maxRev" ∧ 1 ≤ p "maxRev" ∧ x.st = 0 ∧
          held = true ∧ standing = true ∧ (prop = true ↔ inRev ≠ 0) then
        some (enc ⟨1, 1, target, prop, inRev⟩) else none := by
  by_cases h2 : target ≤ p "maxGen" <;> by_cases h3 : inRev ≤ p "maxRev" <;>
    by_cases h4 : 1 ≤ p "maxRev" <;> cases prop <;> rq_simp [h2, h3, h4] <;> ite_close

theorem step_recreate (p : Params) (x : Rq) (held standing : Bool) (target card : Nat) (same : Bool) :
    step M p (enc x) ⟨"recreate", [boolV held, boolV standing, natV target, natV card, boolV same]⟩ =
      if target ≤ p "maxGen" ∧ card ≤ p "maxRev" ∧ x.st ≠ 0 ∧ x.prop = true ∧
          held = true ∧ standing = true ∧ x.tgen = target ∧ x.prev = card ∧
          same = true then some (enc x) else none := by
  by_cases h2 : target ≤ p "maxGen" <;> by_cases h3 : card ≤ p "maxRev" <;>
    cases same <;> rq_simp [h2, h3] <;> ite_close

theorem step_supersede (p : Params) (x : Rq) (ngen : Nat) :
    step M p (enc x) ⟨"supersede", [natV ngen]⟩ =
      if ngen ≤ p "maxGen" ∧ x.rev + 1 ≤ p "maxRev" ∧ x.tgen < ngen ∧ (x.st = 1 ∨ x.st = 2) then
        some (enc { x with st := 5, rev := x.rev + 1 }) else none := by
  by_cases h1 : ngen ≤ p "maxGen" <;> by_cases h2 : x.rev + 1 ≤ p "maxRev" <;> rq_simp [h1, h2] <;> ite_close

set_option maxHeartbeats 4000000 in
theorem step_prepare (p : Params) (x : Rq) (unique holder : Bool) (cgen : Nat) (window : Bool) (card : Nat)
    (material : Bool) :
    step M p (enc x) ⟨"prepare", [boolV unique, boolV holder, natV cgen, boolV window, natV card,
        boolV material]⟩ =
      if cgen ≤ p "maxGen" ∧ card ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧ unique = true ∧ holder = true ∧
          x.prop = true ∧ x.tgen = cgen ∧ (x.st = 1 ∨ x.st = 2) ∧ window = true ∧ x.prev = card ∧
          material = true then some (enc { x with st := 2, rev := x.rev + 1 }) else none := by
  by_cases h1 : cgen ≤ p "maxGen" <;> by_cases h2 : card ≤ p "maxRev" <;> by_cases h3 : x.rev + 1 ≤ p "maxRev" <;>
    cases unique <;> cases holder <;> cases window <;> cases material <;> rq_simp [h1, h2, h3] <;> ite_close

theorem step_complete (p : Params) (x : Rq) (completed found isMatch : Bool) (cgen : Nat) :
    step M p (enc x) ⟨"complete", [boolV completed, boolV found, boolV isMatch, natV cgen]⟩ =
      if cgen ≤ p "maxGen" ∧ x.rev + 1 ≤ p "maxRev" ∧ completed = true ∧ found = true ∧ isMatch = true ∧
          x.prop = true ∧ x.st = 2 ∧ x.tgen = cgen then some (enc { x with st := 3, rev := x.rev + 1 })
      else none := by
  by_cases h1 : cgen ≤ p "maxGen" <;> by_cases h2 : x.rev + 1 ≤ p "maxRev" <;> cases completed <;> cases found <;>
    cases isMatch <;> rq_simp [h1, h2] <;> ite_close

/-- The checks `answerRequest` makes before it compares the answer with the
request. -/
def AnsChecks (x : Rq) (holder : Bool) (cgen ans expected : Nat) (window : Bool) (card : Nat) : Prop :=
  (ans = 2 ∨ ans = 3 ∨ ans = 4) ∧ holder = true ∧ x.st ≠ 0 ∧ x.tgen = cgen ∧ x.st ≠ 5 ∧
    (expected = 0 ∨ expected = x.rev) ∧ (¬(x.prop = true ∧ x.st = 1 ∧ ans = 2) ∨ window = true ∧ x.prev = card)

/-- The answer repeats the status and the note of the request. -/
def SameAns (x : Rq) (ans : Nat) (sameNote : Bool) : Prop := ans = x.st ∧ sameNote = true

instance (x : Rq) (h : Bool) (c a e : Nat) (w : Bool) (d : Nat) : Decidable (AnsChecks x h c a e w d) := by
  unfold AnsChecks; infer_instance

instance (x : Rq) (a : Nat) (n : Bool) : Decidable (SameAns x a n) := by unfold SameAns; infer_instance

set_option maxHeartbeats 8000000 in
theorem step_answerSame (p : Params) (x : Rq) (holder : Bool) (cgen ans expected : Nat) (sameNote window : Bool)
    (card : Nat) :
    step M p (enc x) ⟨"answerSame", [boolV holder, natV cgen, enumV ans, natV expected, boolV sameNote,
        boolV window, natV card]⟩ =
      if cgen ≤ p "maxGen" ∧ ans < 6 ∧ expected ≤ p "maxRev" ∧ card ≤ p "maxRev" ∧
          AnsChecks x holder cgen ans expected window card ∧ SameAns x ans sameNote then some (enc x)
      else none := by
  by_cases h1 : cgen ≤ p "maxGen" <;> by_cases h2 : ans < 6 <;> by_cases h3 : expected ≤ p "maxRev" <;>
    by_cases h4 : card ≤ p "maxRev" <;> cases holder <;> cases sameNote <;> cases window <;>
    rq_simp [h1, h2, h3, h4, AnsChecks, SameAns] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_answer (p : Params) (x : Rq) (holder : Bool) (cgen ans expected : Nat) (sameNote window : Bool)
    (card : Nat) :
    step M p (enc x) ⟨"answer", [boolV holder, natV cgen, enumV ans, natV expected, boolV sameNote,
        boolV window, natV card]⟩ =
      if cgen ≤ p "maxGen" ∧ ans < 6 ∧ expected ≤ p "maxRev" ∧ card ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
          AnsChecks x holder cgen ans expected window card ∧ ¬SameAns x ans sameNote ∧
          ¬(x.st = 3 ∨ x.st = 4) then some (enc { x with st := ans, rev := x.rev + 1 }) else none := by
  by_cases h1 : cgen ≤ p "maxGen" <;> by_cases h2 : ans < 6 <;> by_cases h3 : expected ≤ p "maxRev" <;>
    by_cases h4 : card ≤ p "maxRev" <;> by_cases h5 : x.rev + 1 ≤ p "maxRev" <;> cases holder <;>
    cases sameNote <;> cases window <;> rq_simp [h1, h2, h3, h4, h5, AnsChecks, SameAns] <;> ite_close

end KanderFormal.Proofs.ServerRequest
