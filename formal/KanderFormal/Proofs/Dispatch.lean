import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.Dispatch

/-
The dispatch round machine on the explicit components of a state: the encoding,
the evaluation lemma of every event, and the proof macros. The forms of an
accepted step are in `Proofs/DispatchForms.lean`, the proof obligations in
`Proofs/DispatchSafety.lean` and `Proofs/DispatchGrant.lean`; all of them hold
for every value of `maxRound` and `maxEpoch`.
-/
namespace KanderFormal.Proofs.Dispatch

open KanderFormal KanderFormal.Machines.Dispatch KanderFormal.Proofs.Scalar

abbrev M := dispatchMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `ph`: 0 none, 1 prepared, 2 unknown,
3 accepted, 4 completed, 5 failed, 6 cancelled. `kd`: 0 fix, 1 sync, 2 wrap-up. -/
structure Dp where
  round : Nat
  epoch : Nat
  bound : Bool
  ph : Nat
  kd : Nat
  rel : Bool
  racc : Bool
  rcmp : Bool
  grant : Bool
  settled : Bool

def enc (x : Dp) : State :=
  [("round", natV x.round), ("epoch", natV x.epoch), ("bound", boolV x.bound), ("phase", enumV x.ph),
    ("kind", enumV x.kd), ("released", boolV x.rel), ("acceptedReceipt", boolV x.racc),
    ("completedReceipt", boolV x.rcmp), ("grant", boolV x.grant), ("settled", boolV x.settled)]

theorem enc_inj {x y : Dp} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _, _, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _, _, _, _, _, _⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.ph < 7 ∧ x.kd < 3 ∧ x.round ≤ p "maxRound" ∧ x.epoch ≤ p "maxEpoch" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5), (_, v6), (_, v7), (_, v8), (_, v9), (_, v10)], hn =>
    simp [dispatchMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "round" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "epoch" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "bound" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "phase" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "kind" v5 (by simp [lookup])
    obtain ⟨t6, ht6, c6⟩ := hc "released" v6 (by simp [lookup])
    obtain ⟨t7, ht7, c7⟩ := hc "acceptedReceipt" v7 (by simp [lookup])
    obtain ⟨t8, ht8, c8⟩ := hc "completedReceipt" v8 (by simp [lookup])
    obtain ⟨t9, ht9, c9⟩ := hc "grant" v9 (by simp [lookup])
    obtain ⟨t10, ht10, c10⟩ := hc "settled" v10 (by simp [lookup])
    cases (rfl : M.fieldTy "round" = some (.scalar (.nat "maxRound"))).symm.trans ht1
    cases (rfl : M.fieldTy "epoch" = some (.scalar (.nat "maxEpoch"))).symm.trans ht2
    cases (rfl : M.fieldTy "bound" = some (.scalar .bool)).symm.trans ht3
    cases (rfl : M.fieldTy "phase" = some (.scalar (.enum "phase"))).symm.trans ht4
    cases (rfl : M.fieldTy "kind" = some (.scalar (.enum "kind"))).symm.trans ht5
    cases (rfl : M.fieldTy "released" = some (.scalar .bool)).symm.trans ht6
    cases (rfl : M.fieldTy "acceptedReceipt" = some (.scalar .bool)).symm.trans ht7
    cases (rfl : M.fieldTy "completedReceipt" = some (.scalar .bool)).symm.trans ht8
    cases (rfl : M.fieldTy "grant" = some (.scalar .bool)).symm.trans ht9
    cases (rfl : M.fieldTy "settled" = some (.scalar .bool)).symm.trans ht10
    obtain ⟨r, rfl, hr⟩ := conforms_nat c1
    obtain ⟨e, rfl, he⟩ := conforms_nat c2
    obtain ⟨b, rfl⟩ := conforms_bool c3
    obtain ⟨ph, rfl, hph⟩ := conforms_enum (rfl : M.enumSize "phase" = some 7) c4
    obtain ⟨kd, rfl, hkd⟩ := conforms_enum (rfl : M.enumSize "kind" = some 3) c5
    obtain ⟨rl, rfl⟩ := conforms_bool c6
    obtain ⟨ra, rfl⟩ := conforms_bool c7
    obtain ⟨rc, rfl⟩ := conforms_bool c8
    obtain ⟨gr, rfl⟩ := conforms_bool c9
    obtain ⟨st, rfl⟩ := conforms_bool c10
    exact ⟨⟨r, e, b, ph, kd, rl, ra, rc, gr, st⟩, rfl, hph, hkd, hr, he⟩

/-! ## Events on an encoded state -/

theorem find_prepare : M.findEvent "prepare" = some prepareEvent := rfl
theorem find_retry : M.findEvent "retry" = some retryEvent := rfl
theorem find_attempt : M.findEvent "attempt" = some attemptEvent := rfl
theorem find_reauthorize : M.findEvent "reauthorize" = some reauthorizeEvent := rfl
theorem find_accept : M.findEvent "accept" = some acceptEvent := rfl
theorem find_complete : M.findEvent "complete" = some completeEvent := rfl
theorem find_end : M.findEvent "end" = some endEvent := rfl
theorem find_cancel : M.findEvent "cancel" = some cancelEvent := rfl
theorem find_retain : M.findEvent "retain" = some retainEvent := rfl
theorem find_exec : M.findEvent "exec" = some execEvent := rfl
theorem find_authorize : M.findEvent "authorizeWrapUp" = some authorizeWrapUpEvent := rfl
theorem find_full : M.findEvent "full" = some fullEvent := rfl
theorem find_wrapUpWrite : M.findEvent "wrapUpWrite" = some wrapUpWriteEvent := rfl
theorem find_settle : M.findEvent "settle" = some settleEvent := rfl
theorem find_terminateSettle : M.findEvent "terminateSettle" = some terminateSettleEvent := rfl
theorem find_takeoverSettle : M.findEvent "takeoverSettle" = some takeoverSettleEvent := rfl
theorem ctors_phase : M.ctorsOf "phase" = some ["none", "prepared", "unknown", "accepted", "completed", "failed", "cancelled"] := rfl
theorem ctors_kind : M.ctorsOf "kind" = some ["fix", "sync", "wrapUp"] := rfl
theorem ctors_col : M.ctorsOf "col" = some ["working", "review", "other"] := rfl
theorem ctors_exit : M.ctorsOf "exit" = some ["stopped", "reclaimed", "other"] := rfl
theorem size_exit : M.enumSize "exit" = some 3 := rfl
theorem size_phase : M.enumSize "phase" = some 7 := rfl
theorem size_kind : M.enumSize "kind" = some 3 := rfl
theorem size_col : M.enumSize "col" = some 3 := rfl
theorem ty_round : M.fieldTy "round" = some (.scalar (.nat "maxRound")) := rfl
theorem ty_epoch : M.fieldTy "epoch" = some (.scalar (.nat "maxEpoch")) := rfl
theorem ty_bound : M.fieldTy "bound" = some (.scalar .bool) := rfl
theorem ty_phase : M.fieldTy "phase" = some (.scalar (.enum "phase")) := rfl
theorem ty_kind : M.fieldTy "kind" = some (.scalar (.enum "kind")) := rfl
theorem ty_released : M.fieldTy "released" = some (.scalar .bool) := rfl
theorem ty_acceptedReceipt : M.fieldTy "acceptedReceipt" = some (.scalar .bool) := rfl
theorem ty_completedReceipt : M.fieldTy "completedReceipt" = some (.scalar .bool) := rfl
theorem ty_grant : M.fieldTy "grant" = some (.scalar .bool) := rfl
theorem ty_settled : M.fieldTy "settled" = some (.scalar .bool) := rfl

macro "dp_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_prepare, find_retry, find_attempt, find_reauthorize, find_accept, find_complete,
      find_end, find_cancel, find_retain, find_exec, find_authorize, find_full, find_wrapUpWrite, find_settle,
      find_terminateSettle, find_takeoverSettle, ctors_phase, ctors_kind, ctors_col, ctors_exit, size_phase,
      size_kind, size_col, size_exit, ty_round, ty_epoch, ty_bound, ty_phase, ty_kind, ty_released, ty_acceptedReceipt,
      ty_completedReceipt, ty_grant, ty_settled, lookup,
      prepareEvent, retryEvent, attemptEvent, reauthorizeEvent, acceptEvent, completeEvent, endEvent,
      cancelEvent, retainEvent, execEvent, authorizeWrapUpEvent, fullEvent, wrapUpWriteEvent, settleEvent,
      terminateSettleEvent, takeoverSettleEvent, fenced, isOpen, isTerm, isLegacy, isPending, phaseIs, kindIs,
      exitIs, phaseLit, authRound, authEpoch, authArgs, natArg, boolArg, enumArg, all, imp, eq, ne, inc, zero, f, a, tt,
      andE, orE, notE, bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr, boolNot,
      indexOf, enumV, boolV, natV, evalAssigns, applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc,
      Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

def Fenced (x : Dp) (ar ae : Nat) : Prop := x.bound = true ∧ ar ≠ 0 ∧ ae ≠ 0 ∧ ar = x.round ∧ ae = x.epoch
instance (x : Dp) (ar ae : Nat) : Decidable (Fenced x ar ae) := by unfold Fenced; infer_instance

def Pend (x : Dp) : Prop := x.ph = 1 ∨ x.ph = 2
def Open (x : Dp) : Prop := x.ph = 1 ∨ x.ph = 2 ∨ x.ph = 3
def Term (x : Dp) : Prop := x.ph = 4 ∨ x.ph = 5 ∨ x.ph = 6
def Leg (x : Dp) : Prop := x.ph = 5 ∨ x.ph = 6
instance (x : Dp) : Decidable (Pend x) := by unfold Pend; infer_instance
instance (x : Dp) : Decidable (Open x) := by unfold Open; infer_instance
instance (x : Dp) : Decidable (Term x) := by unfold Term; infer_instance
instance (x : Dp) : Decidable (Leg x) := by unfold Leg; infer_instance

theorem step_exec (p : Params) (x : Dp) (ar ae : Nat) (rec before author : Bool) :
    step M p (enc x) ⟨"exec", [natV ar, natV ae, boolV rec, boolV before, boolV author]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧
          ((x.bound = false ∧ ar = 0 ∧ ae = 0) ∨
           (Fenced x ar ae ∧ rec = true ∧ (x.ph = 3 ∨ before = true) ∧ Open x ∧ (author = false ∨ x.ph = 3))) then
        some (enc x) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;>
    dp_simp [h1, h2, Fenced, Open] <;> ite_close

theorem step_retry (p : Params) (x : Dp) (same : Bool) :
    step M p (enc x) ⟨"retry", [boolV same]⟩ = if same = true then some (enc x) else none := by
  dp_simp [] <;> ite_close

theorem step_prepare (p : Params) (x : Dp) (kd cl : Nat) (free room : Bool) :
    step M p (enc x) ⟨"prepare", [enumV kd, enumV cl, boolV free, boolV room]⟩ =
      if kd < 3 ∧ cl < 3 ∧ cl ≠ 2 ∧ free = true ∧ (x.bound = false ∨ Term x) ∧ room = true ∧
          x.round + 1 ≤ p "maxRound" ∧ (if x.bound = true then x.epoch + 1 else 1) ≤ p "maxEpoch" then
        some (enc ⟨x.round + 1, if x.bound = true then x.epoch + 1 else 1, true, 1, kd, false, false, false, false, false⟩)
      else none := by
  by_cases hk : kd < 3 <;> by_cases hc : cl < 3 <;> cases hb : x.bound <;> dp_simp [hk, hc, hb, Term] <;> ite_close

theorem step_attempt (p : Params) (x : Dp) (ar ae : Nat) (cas cycle evidence before : Bool) :
    step M p (enc x) ⟨"attempt", [natV ar, natV ae, boolV cas, boolV cycle, boolV evidence, boolV before]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ x.grant = false ∧ cas = true ∧ Fenced x ar ae ∧ cycle = true ∧
          evidence = true ∧ Pend x ∧ before = true then some (enc { x with ph := 2 }) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;>
    dp_simp [h1, h2, Fenced, Pend] <;> ite_close

theorem step_reauthorize (p : Params) (x : Dp) (ar ae : Nat) (cas room cycle fresh before : Bool) :
    step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV cas, boolV room, boolV cycle, boolV fresh,
        boolV before]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ x.grant = false ∧ cas = true ∧ Fenced x ar ae ∧ Open x ∧
          room = true ∧ cycle = true ∧ (if x.ph = 3 then fresh = true else before = true) ∧
          x.epoch + 1 ≤ p "maxEpoch" then
        some (enc { x with epoch := x.epoch + 1, ph := 1, racc := false, rcmp := false }) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;> by_cases h3 : x.ph = 3 <;>
    dp_simp [h1, h2, h3, Fenced, Open] <;> ite_close

theorem step_accept (p : Params) (x : Dp) (ar ae : Nat) (rec cycle before : Bool) :
    step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV rec, boolV cycle, boolV before]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ Fenced x ar ae ∧ rec = true ∧ cycle = true ∧
          (x.ph = 3 ∨ x.ph = 4 ∨ Pend x ∧ before = true) then
        some (enc { x with ph := if Pend x then 3 else x.ph, racc := if Pend x then true else x.racc }) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;> by_cases h3 : x.ph = 1 <;>
    by_cases h4 : x.ph = 2 <;> dp_simp [h1, h2, h3, h4, Fenced, Pend] <;> ite_close

theorem step_complete (p : Params) (x : Dp) (ar ae : Nat) (rec cycle toDone commit evidence disp same : Bool)
    (cl : Nat) :
    step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV rec, boolV cycle, boolV toDone, boolV commit,
        boolV evidence, boolV disp, boolV same, enumV cl]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ cl < 3 ∧ Fenced x ar ae ∧ rec = true ∧ cycle = true ∧
          decide (x.kd = 2) = toDone ∧ commit = true ∧ evidence = true ∧ disp = true ∧
          (x.ph = 4 ∧ same = true ∨ x.ph = 3 ∧ cl = 0) then
        some (enc { x with ph := 4, rcmp := true }) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;> by_cases hc : cl < 3 <;>
    dp_simp [h1, h2, hc, Fenced] <;> ite_close

theorem step_end (p : Params) (x : Dp) (ar ae : Nat) (cas room cycle decided toFailed : Bool) :
    step M p (enc x) ⟨"end", [natV ar, natV ae, boolV cas, boolV room, boolV cycle, boolV decided,
        boolV toFailed]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ cas = true ∧ Fenced x ar ae ∧ x.ph ≠ 4 ∧ x.rel = false ∧
          room = true ∧ cycle = true ∧ (x.ph ≠ 3 ∨ decided = true) ∧
          (¬Leg x ∨ decided = true ∧ (x.ph = 5 ↔ toFailed = true)) then
        some (enc ⟨x.round, x.epoch, false, if toFailed = true then 5 else 6, x.kd, true, x.racc, x.rcmp, x.grant, x.settled⟩)
      else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;>
    cases toFailed <;> dp_simp [h1, h2, Fenced, Leg] <;> ite_close

theorem step_cancel (p : Params) (x : Dp) (reasoned authOk cycle : Bool) :
    step M p (enc x) ⟨"cancel", [boolV reasoned, boolV authOk, boolV cycle]⟩ =
      if x.bound = true ∧ reasoned = true ∧ authOk = true ∧ cycle = true ∧ (Open x ∨ Leg x) then
        some (enc { x with ph := if Open x then 6 else x.ph }) else none := by
  by_cases h1 : x.ph = 1 <;> by_cases h2 : x.ph = 2 <;> by_cases h3 : x.ph = 3 <;>
    dp_simp [h1, h2, h3, Open, Leg] <;> ite_close

theorem step_retain (p : Params) (x : Dp) (ar ae : Nat) (decided rev rec cycle : Bool) :
    step M p (enc x) ⟨"retain", [natV ar, natV ae, boolV decided, boolV rev, boolV rec, boolV cycle]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ Fenced x ar ae ∧ decided = true ∧ rev = true ∧ rec = true ∧
          cycle = true ∧ Open x then some (enc x) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;>
    dp_simp [h1, h2, Fenced, Open] <;> ite_close


set_option maxHeartbeats 1600000 in
theorem step_authorize (p : Params) (x : Dp) (ar ae : Nat) (cas room named cycle decided fresh evidence : Bool)
    (cl ot : Nat) :
    step M p (enc x) ⟨"authorizeWrapUp", [natV ar, natV ae, boolV cas, boolV room, enumV cl, boolV named,
        boolV cycle, enumV ot, boolV decided, boolV fresh, boolV evidence]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ cl < 3 ∧ ot < 3 ∧ cas = true ∧ Fenced x ar ae ∧ x.kd = 2 ∧
          Open x ∧ x.grant = false ∧ room = true ∧ cl ≠ 2 ∧ named = true ∧ cycle = true ∧
          (ot = 0 ∨ ot = 1 ∧ decided = true) ∧ fresh = true ∧ evidence = true ∧ x.epoch + 1 ≤ p "maxEpoch" then
        some (enc { x with epoch := x.epoch + 1, ph := 1, racc := false, rcmp := false, grant := true })
      else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;> by_cases hc : cl < 3 <;>
    by_cases ho : ot < 3 <;> dp_simp [h1, h2, hc, ho, Fenced, Open] <;> ite_close

theorem step_full (p : Params) (x : Dp) :
    step M p (enc x) ⟨"full", []⟩ = if x.bound = false ∨ x.grant = false then some (enc x) else none := by
  dp_simp [] <;> ite_close

set_option maxHeartbeats 1600000 in
theorem step_wrapUpWrite (p : Params) (x : Dp) (ct sp ap pa sm : Bool) :
    step M p (enc x) ⟨"wrapUpWrite", [boolV ct, boolV sp, boolV ap, boolV pa, boolV sm]⟩ =
      if x.bound = false ∨ x.grant = false ∨ ct = false ∧ (if sp = true then ap = true else pa = true ∧ sm = true)
      then some (enc x) else none := by
  cases ct <;> cases sp <;> cases ap <;> cases pa <;> cases sm <;> cases hb : x.bound <;> cases hg : x.grant <;>
    dp_simp [hb, hg]

theorem step_settle (p : Params) (x : Dp) (ar ae : Nat) (cas cycle : Bool) (cl : Nat) :
    step M p (enc x) ⟨"settle", [natV ar, natV ae, boolV cas, enumV cl, boolV cycle]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ cl < 3 ∧ cas = true ∧ Fenced x ar ae ∧ x.ph = 4 ∧
          x.settled = false ∧ cl ≠ 2 ∧ cycle = true then
        some (enc { x with bound := false, settled := true }) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;> by_cases hc : cl < 3 <;>
    dp_simp [h1, h2, hc, Fenced] <;> ite_close

theorem step_terminateSettle (p : Params) (x : Dp) (reasoned authOk cycle : Bool) :
    step M p (enc x) ⟨"terminateSettle", [boolV reasoned, boolV authOk, boolV cycle]⟩ =
      if x.bound = true ∧ reasoned = true ∧ authOk = true ∧ cycle = true ∧ x.ph = 4 then
        some (enc { x with bound := false, settled := true }) else none := by
  dp_simp [] <;> ite_close

theorem step_takeoverSettle (p : Params) (x : Dp) (ar ae : Nat) (decided rev rec cycle : Bool) :
    step M p (enc x) ⟨"takeoverSettle", [natV ar, natV ae, boolV decided, boolV rev, boolV rec, boolV cycle]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ Fenced x ar ae ∧ decided = true ∧ rev = true ∧ rec = true ∧
          cycle = true ∧ x.ph = 4 ∧ x.settled = false then
        some (enc { x with bound := false, settled := true }) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;>
    dp_simp [h1, h2, Fenced] <;> ite_close

end KanderFormal.Proofs.Dispatch
