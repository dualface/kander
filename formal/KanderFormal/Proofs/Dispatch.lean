import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.Dispatch

/-
Proof obligations of the dispatch round machine. They hold for every value of
`maxRound` and `maxEpoch`.
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

def enc (x : Dp) : State :=
  [("round", natV x.round), ("epoch", natV x.epoch), ("bound", boolV x.bound), ("phase", enumV x.ph),
    ("kind", enumV x.kd), ("released", boolV x.rel), ("acceptedReceipt", boolV x.racc),
    ("completedReceipt", boolV x.rcmp)]

theorem enc_inj {x y : Dp} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _, _, _, _⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.ph < 7 ∧ x.kd < 3 ∧ x.round ≤ p "maxRound" ∧ x.epoch ≤ p "maxEpoch" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5), (_, v6), (_, v7), (_, v8)], hn =>
    simp [dispatchMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "round" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "epoch" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "bound" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "phase" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "kind" v5 (by simp [lookup])
    obtain ⟨t6, ht6, c6⟩ := hc "released" v6 (by simp [lookup])
    obtain ⟨t7, ht7, c7⟩ := hc "acceptedReceipt" v7 (by simp [lookup])
    obtain ⟨t8, ht8, c8⟩ := hc "completedReceipt" v8 (by simp [lookup])
    cases (rfl : M.fieldTy "round" = some (.scalar (.nat "maxRound"))).symm.trans ht1
    cases (rfl : M.fieldTy "epoch" = some (.scalar (.nat "maxEpoch"))).symm.trans ht2
    cases (rfl : M.fieldTy "bound" = some (.scalar .bool)).symm.trans ht3
    cases (rfl : M.fieldTy "phase" = some (.scalar (.enum "phase"))).symm.trans ht4
    cases (rfl : M.fieldTy "kind" = some (.scalar (.enum "kind"))).symm.trans ht5
    cases (rfl : M.fieldTy "released" = some (.scalar .bool)).symm.trans ht6
    cases (rfl : M.fieldTy "acceptedReceipt" = some (.scalar .bool)).symm.trans ht7
    cases (rfl : M.fieldTy "completedReceipt" = some (.scalar .bool)).symm.trans ht8
    obtain ⟨r, rfl, hr⟩ := conforms_nat c1
    obtain ⟨e, rfl, he⟩ := conforms_nat c2
    obtain ⟨b, rfl⟩ := conforms_bool c3
    obtain ⟨ph, rfl, hph⟩ := conforms_enum (rfl : M.enumSize "phase" = some 7) c4
    obtain ⟨kd, rfl, hkd⟩ := conforms_enum (rfl : M.enumSize "kind" = some 3) c5
    obtain ⟨rl, rfl⟩ := conforms_bool c6
    obtain ⟨ra, rfl⟩ := conforms_bool c7
    obtain ⟨rc, rfl⟩ := conforms_bool c8
    exact ⟨⟨r, e, b, ph, kd, rl, ra, rc⟩, rfl, hph, hkd, hr, he⟩

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
theorem ctors_phase : M.ctorsOf "phase" = some ["none", "prepared", "unknown", "accepted", "completed", "failed", "cancelled"] := rfl
theorem ctors_kind : M.ctorsOf "kind" = some ["fix", "sync", "wrapUp"] := rfl
theorem ctors_col : M.ctorsOf "col" = some ["working", "review", "other"] := rfl
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

macro "dp_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_prepare, find_retry, find_attempt, find_reauthorize, find_accept, find_complete,
      find_end, find_cancel, find_retain, find_exec, ctors_phase, ctors_kind, ctors_col, size_phase, size_kind,
      size_col, ty_round, ty_epoch, ty_bound, ty_phase, ty_kind, ty_released, ty_acceptedReceipt, ty_completedReceipt, lookup,
      prepareEvent, retryEvent, attemptEvent, reauthorizeEvent, acceptEvent, completeEvent, endEvent,
      cancelEvent, retainEvent, execEvent, fenced, isOpen, isTerm, isLegacy, isPending, phaseIs, kindIs,
      phaseLit, authRound, authEpoch, authArgs, natArg, boolArg, enumArg, all, imp, eq, ne, inc, zero, f, a, tt,
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
        some (enc ⟨x.round + 1, if x.bound = true then x.epoch + 1 else 1, true, 1, kd, false, false, false⟩) else none := by
  by_cases hk : kd < 3 <;> by_cases hc : cl < 3 <;> cases hb : x.bound <;> dp_simp [hk, hc, hb, Term] <;> ite_close

theorem step_attempt (p : Params) (x : Dp) (ar ae : Nat) (cas cycle evidence before : Bool) :
    step M p (enc x) ⟨"attempt", [natV ar, natV ae, boolV cas, boolV cycle, boolV evidence, boolV before]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ cas = true ∧ Fenced x ar ae ∧ cycle = true ∧
          evidence = true ∧ Pend x ∧ before = true then some (enc { x with ph := 2 }) else none := by
  by_cases h1 : ar ≤ p "maxRound" <;> by_cases h2 : ae ≤ p "maxEpoch" <;>
    dp_simp [h1, h2, Fenced, Pend] <;> ite_close

theorem step_reauthorize (p : Params) (x : Dp) (ar ae : Nat) (cas room cycle fresh before : Bool) :
    step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV cas, boolV room, boolV cycle, boolV fresh,
        boolV before]⟩ =
      if ar ≤ p "maxRound" ∧ ae ≤ p "maxEpoch" ∧ cas = true ∧ Fenced x ar ae ∧ Open x ∧ room = true ∧
          cycle = true ∧ (if x.ph = 3 then fresh = true else before = true) ∧ x.epoch + 1 ≤ p "maxEpoch" then
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
        some (enc ⟨x.round, x.epoch, false, if toFailed = true then 5 else 6, x.kd, true, x.racc, x.rcmp⟩) else none := by
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

/-! ## The events an accepted step ran -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = prepareEvent ∨ ev = retryEvent ∨ ev = attemptEvent ∨ ev = reauthorizeEvent ∨ ev = acceptEvent ∨
      ev = completeEvent ∨ ev = endEvent ∨ ev = cancelEvent ∨ ev = retainEvent ∨ ev = execEvent := by
  simpa [dispatchMachine] using h

theorem nat_arg {p : Params} {b : String} {v : Val} (h : Ty.conforms M p (.scalar (.nat b)) v = true) :
    ∃ n, v = natV n := by
  obtain ⟨n, rfl, -⟩ := conforms_nat h; exact ⟨n, rfl⟩

/-- Every accepted event is one of the ten, with arguments of its declared types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
      (∃ kd cl fr rm, e = ⟨"prepare", [enumV kd, enumV cl, boolV fr, boolV rm]⟩) ∨
      (∃ sm, e = ⟨"retry", [boolV sm]⟩) ∨
      (∃ ar ae cs cy ev bf, e = ⟨"attempt", [natV ar, natV ae, boolV cs, boolV cy, boolV ev, boolV bf]⟩) ∨
      (∃ ar ae cs rm cy fr bf, e = ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV fr, boolV bf]⟩) ∨
      (∃ ar ae rc cy bf, e = ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩) ∨
      (∃ ar ae rc cy td cm ev dp sm cl, e = ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td, boolV cm, boolV ev, boolV dp, boolV sm, enumV cl]⟩) ∨
      (∃ ar ae cs rm cy dc tf, e = ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc, boolV tf]⟩) ∨
      (∃ rs ao cy, e = ⟨"cancel", [boolV rs, boolV ao, boolV cy]⟩) ∨
      (∃ ar ae dc rv rc cy, e = ⟨"retain", [natV ar, natV ae, boolV dc, boolV rv, boolV rc, boolV cy]⟩) ∨
      (∃ ar ae rc bf au, e = ⟨"exec", [natV ar, natV ae, boolV rc, boolV bf, boolV au]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · -- prepare
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    obtain ⟨kd, rfl, -⟩ := conforms_enum size_kind c0
    obtain ⟨cl, rfl, -⟩ := conforms_enum size_col c1
    obtain ⟨fr, rfl⟩ := conforms_bool c2
    obtain ⟨rm, rfl⟩ := conforms_bool c3
    exact .inl ⟨kd, cl, fr, rm, rfl⟩
  · -- retry
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h0
    obtain ⟨sm, rfl⟩ := conforms_bool c0
    exact .inr (.inl ⟨sm, rfl⟩)
  · -- attempt
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨cs, rfl⟩ := conforms_bool c2
    obtain ⟨cy, rfl⟩ := conforms_bool c3
    obtain ⟨ev, rfl⟩ := conforms_bool c4
    obtain ⟨bf, rfl⟩ := conforms_bool c5
    exact .inr (.inr (.inl ⟨ar, ae, cs, cy, ev, bf, rfl⟩))
  · -- reauthorize
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨cs, rfl⟩ := conforms_bool c2
    obtain ⟨rm, rfl⟩ := conforms_bool c3
    obtain ⟨cy, rfl⟩ := conforms_bool c4
    obtain ⟨fr, rfl⟩ := conforms_bool c5
    obtain ⟨bf, rfl⟩ := conforms_bool c6
    exact .inr (.inr (.inr (.inl ⟨ar, ae, cs, rm, cy, fr, bf, rfl⟩)))
  · -- accept
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨rc, rfl⟩ := conforms_bool c2
    obtain ⟨cy, rfl⟩ := conforms_bool c3
    obtain ⟨bf, rfl⟩ := conforms_bool c4
    exact .inr (.inr (.inr (.inr (.inl ⟨ar, ae, rc, cy, bf, rfl⟩))))
  · -- complete
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨v7, vs7, _, rfl, c7, h7, -⟩ := bindArgs_cons h6
    obtain ⟨v8, vs8, _, rfl, c8, h8, -⟩ := bindArgs_cons h7
    obtain ⟨v9, vs9, _, rfl, c9, h9, -⟩ := bindArgs_cons h8
    obtain ⟨rfl, -⟩ := bindArgs_nil h9
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨rc, rfl⟩ := conforms_bool c2
    obtain ⟨cy, rfl⟩ := conforms_bool c3
    obtain ⟨td, rfl⟩ := conforms_bool c4
    obtain ⟨cm, rfl⟩ := conforms_bool c5
    obtain ⟨ev, rfl⟩ := conforms_bool c6
    obtain ⟨dp, rfl⟩ := conforms_bool c7
    obtain ⟨sm, rfl⟩ := conforms_bool c8
    obtain ⟨cl, rfl, -⟩ := conforms_enum size_col c9
    exact .inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, rc, cy, td, cm, ev, dp, sm, cl, rfl⟩)))))
  · -- end
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨cs, rfl⟩ := conforms_bool c2
    obtain ⟨rm, rfl⟩ := conforms_bool c3
    obtain ⟨cy, rfl⟩ := conforms_bool c4
    obtain ⟨dc, rfl⟩ := conforms_bool c5
    obtain ⟨tf, rfl⟩ := conforms_bool c6
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, cs, rm, cy, dc, tf, rfl⟩))))))
  · -- cancel
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    obtain ⟨rs, rfl⟩ := conforms_bool c0
    obtain ⟨ao, rfl⟩ := conforms_bool c1
    obtain ⟨cy, rfl⟩ := conforms_bool c2
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨rs, ao, cy, rfl⟩)))))))
  · -- retain
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨dc, rfl⟩ := conforms_bool c2
    obtain ⟨rv, rfl⟩ := conforms_bool c3
    obtain ⟨rc, rfl⟩ := conforms_bool c4
    obtain ⟨cy, rfl⟩ := conforms_bool c5
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, dc, rv, rc, cy, rfl⟩))))))))
  · -- exec
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨rc, rfl⟩ := conforms_bool c2
    obtain ⟨bf, rfl⟩ := conforms_bool c3
    obtain ⟨au, rfl⟩ := conforms_bool c4
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (⟨ar, ae, rc, bf, au, rfl⟩)))))))))

/-! ## The forms of an accepted step -/

/-- What an accepted event does to the components, event by event. -/
def Forms (x y : Dp) : Prop :=
  y = x ∨
  (y.round = x.round + 1 ∧ y.bound = true ∧ y.ph = 1 ∧ y.rel = false ∧ (x.bound = false ∨ Term x) ∧
      y.epoch = (if x.bound = true then x.epoch + 1 else 1) ∧ y.racc = false ∧ y.rcmp = false) ∨
  (y = { x with ph := 2 } ∧ x.bound = true ∧ x.round ≠ 0 ∧ Pend x) ∨
  (y = { x with epoch := x.epoch + 1, ph := 1, racc := false, rcmp := false } ∧ x.bound = true ∧ x.round ≠ 0 ∧
      Open x) ∨
  (y = { x with ph := if Pend x then 3 else x.ph, racc := if Pend x then true else x.racc } ∧ x.bound = true ∧ x.round ≠ 0 ∧
      (Pend x ∨ x.ph = 3 ∨ x.ph = 4)) ∨
  (y = { x with ph := 4, rcmp := true } ∧ x.bound = true ∧ x.round ≠ 0 ∧ (x.ph = 3 ∨ x.ph = 4)) ∨
  (y.round = x.round ∧ y.epoch = x.epoch ∧ y.bound = false ∧ y.rel = true ∧ (y.ph = 5 ∨ y.ph = 6) ∧
      y.kd = x.kd ∧ x.bound = true ∧ x.round ≠ 0 ∧ x.ph ≠ 4 ∧ x.rel = false ∧ (Leg x → y.ph = x.ph) ∧
      y.racc = x.racc ∧ y.rcmp = x.rcmp) ∨
  (y = { x with ph := if Open x then 6 else x.ph } ∧ x.bound = true ∧ (Open x ∨ Leg x))

theorem ite_some_inv {c : Prop} [Decidable c] {a b : State} (h : (if c then some a else none) = some b) :
    c ∧ a = b := by
  by_cases hc : c <;> simp [hc] at h <;> exact ⟨hc, h⟩

theorem forms {p : Params} {x y : Dp} {s' : State} {e : Event} (h : step M p (enc x) e = some s')
    (hy : s' = enc y) : Forms x y := by
  rcases accepted_cases h with ⟨kd, cl, fr, rm, rfl⟩ | ⟨sm, rfl⟩ | ⟨ar, ae, cs, cy, ev, bf, rfl⟩ |
    ⟨ar, ae, cs, rm, cy, fr, bf, rfl⟩ | ⟨ar, ae, rc, cy, bf, rfl⟩ |
    ⟨ar, ae, rc, cy, td, cm, ev, dp, sm, cl, rfl⟩ | ⟨ar, ae, cs, rm, cy, dc, tf, rfl⟩ | ⟨rs, ao, cy, rfl⟩ |
    ⟨ar, ae, dc, rv, rc, cy, rfl⟩ | ⟨ar, ae, rc, bf, au, rfl⟩
  · rw [step_prepare] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; left; cases hb : x.bound <;> simp_all
  · rw [step_retry] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this; left; rfl
  · rw [step_attempt] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; left; simp_all [Fenced] <;> omega
  · rw [step_reauthorize] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; left; simp_all [Fenced] <;> omega
  · rw [step_accept] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; left; simp_all [Fenced]
    obtain ⟨-, -, -, -, -, h3⟩ := hc
    rcases h3 with h3 | h3 | ⟨h3, -⟩ <;> simp [h3]
  · rw [step_complete] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; left; simp_all [Fenced] <;> omega
  · rw [step_end] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; right; left; simp_all [Fenced]
    intro hleg
    obtain ⟨-, -, -, -, -, -, -, -, -, hl⟩ := hc
    rcases hl with hl | ⟨-, hl⟩
    · exact absurd hleg hl
    · unfold Leg at hleg; cases tf <;> simp at hl ⊢ <;> omega
  · rw [step_cancel] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; right; right; simp_all
  · rw [step_retain] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this; left; rfl
  · rw [step_exec] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this; left; rfl

/-! ## Theorem 1: one non-terminal binding -/

/-- Theorem 1: a `prepare` that creates a round is accepted only when the card
has no binding or its binding is terminal, so a card never holds two open
rounds. -/
theorem prepare_single {p : Params} {x : Dp} {kd cl : Nat} {free room : Bool} {s' : State}
    (h : step M p (enc x) ⟨"prepare", [enumV kd, enumV cl, boolV free, boolV room]⟩ = some s') :
    x.bound = false ∨ Term x := by
  rw [step_prepare] at h
  obtain ⟨hc, -⟩ := ite_some_inv h
  exact hc.2.2.2.2.1

/-! ## Theorem 2: the authorization pair -/

/-- The pair of the dispatch record. -/
def PairLe (x y : Dp) : Prop := x.round < y.round ∨ x.round = y.round ∧ x.epoch ≤ y.epoch

/-- Theorem 2 (monotone): no accepted event lowers the pair `(round, epoch)` of
the dispatch record in lexicographic order. -/
theorem pair_monotone {p : Params} {x y : Dp} {s' : State} {e : Event} (h : step M p (enc x) e = some s')
    (hy : s' = enc y) : PairLe x y := by
  unfold PairLe
  rcases forms h hy with h0 | ⟨hr, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨hr, he, -⟩ | ⟨h1, -⟩
  · subst h0; right; simp
  · left; omega
  · subst h1; right; simp
  · subst h1; right; simp
  · subst h1; right; simp
  · subst h1; right; simp
  · right; omega
  · subst h1; right; simp

/-- Theorem 2 (strict): a `prepare` that creates a round raises the round and
sets the epoch to the card's epoch plus one, which is 1 on a card without
binding. -/
theorem prepare_strict {p : Params} {x y : Dp} {kd cl : Nat} {free room : Bool} {s' : State}
    (h : step M p (enc x) ⟨"prepare", [enumV kd, enumV cl, boolV free, boolV room]⟩ = some s')
    (hy : s' = enc y) : y.round = x.round + 1 ∧ y.epoch = (if x.bound = true then x.epoch + 1 else 1) := by
  rw [step_prepare] at h
  obtain ⟨-, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  simp

/-- Theorem 2 (strict): a `reauthorize` keeps the round and raises the epoch by
one. -/
theorem reauthorize_strict {p : Params} {x y : Dp} {ar ae : Nat} {cs rm cy fr bf : Bool} {s' : State}
    (h : step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV fr,
        boolV bf]⟩ = some s')
    (hy : s' = enc y) : y.round = x.round ∧ y.epoch = x.epoch + 1 ∧ y.ph = 1 := by
  rw [step_reauthorize] at h
  obtain ⟨-, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  simp

/-- Theorem 2 (fencing): `accept`, `complete`, and the other writes that present
an authorization pair different from the card binding are refused. A refused
event leaves the state unchanged (`run_refused_unchanged`). The binding of a
card without one is the empty pair. -/
theorem fence_refused (p : Params) (x : Dp) (ar ae : Nat)
    (hne : ¬(x.bound = true ∧ ar = x.round ∧ ae = x.epoch)) (rc cy bf : Bool) :
    step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩ = none ∧
    (∀ td cm ev dp sm cl, step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td,
        boolV cm, boolV ev, boolV dp, boolV sm, enumV cl]⟩ = none) ∧
    (∀ cs rm dc tf, step M p (enc x) ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc,
        boolV tf]⟩ = none) ∧
    (∀ au, x.bound = true ∨ ar ≠ 0 ∨ ae ≠ 0 →
      step M p (enc x) ⟨"exec", [natV ar, natV ae, boolV rc, boolV bf, boolV au]⟩ = none) := by
  have hf : ¬Fenced x ar ae := fun ⟨h1, h2, h3, h4, h5⟩ => hne ⟨h1, h4, h5⟩
  refine ⟨?_, ?_, ?_, ?_⟩
  · rw [step_accept]; simp [hf]
  · intro td cm ev dp sm cl; rw [step_complete]; simp [hf]
  · intro cs rm dc tf; rw [step_end]; simp [hf]
  · intro au hb; rw [step_exec]
    simp only [hf, false_and, or_false]
    split
    · rename_i hc
      obtain ⟨-, -, hb0, h0, h0'⟩ := hc
      rcases hb with hb | hb | hb <;> simp_all
    · rfl

/-! ## Theorem 3: acceptance and completion -/

/-- Theorem 3: an `accept` that changes the phase starts from a prepared or
uncertain round, before its acceptance deadline, and ends in `accepted` with
its receipt. -/
theorem accept_change {p : Params} {x y : Dp} {ar ae : Nat} {rc cy bf : Bool} {s' : State}
    (h : step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩ = some s')
    (hy : s' = enc y) (hne : y.ph ≠ x.ph) : Pend x ∧ bf = true ∧ y.ph = 3 ∧ y.racc = true := by
  rw [step_accept] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, -, -, -, h3⟩ := hc
  by_cases hp : Pend x
  · rcases h3 with h3 | h3 | ⟨-, h3⟩
    · exfalso; unfold Pend at hp; omega
    · exfalso; unfold Pend at hp; omega
    · simp [hp, h3]
  · simp [hp] at hne

/-- Theorem 3: a `complete` that changes the phase starts from an accepted round
of a card in `working` (`col` is `working`, index 0) and ends in `completed` with
its receipt. -/
theorem complete_change {p : Params} {x y : Dp} {ar ae : Nat} {rc cy td cm ev dp sm : Bool} {cl : Nat}
    {s' : State}
    (h : step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td, boolV cm, boolV ev,
        boolV dp, boolV sm, enumV cl]⟩ = some s')
    (hy : s' = enc y) (hne : y.ph ≠ x.ph) : x.ph = 3 ∧ cl = 0 ∧ y.ph = 4 ∧ y.rcmp = true := by
  rw [step_complete] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, -, -, -, -, -, -, -, -, h3⟩ := hc
  rcases h3 with ⟨h3, -⟩ | ⟨h3, h4⟩
  · simp [h3] at hne
  · exact ⟨h3, h4, rfl, rfl⟩

/-- Theorem 3: a round of kind `wrap-up` (index 2) completes only by a move into
`done`, and any other kind only by a move into `review`. -/
theorem complete_kind {p : Params} {x : Dp} {ar ae : Nat} {rc cy td cm ev dp sm : Bool} {cl : Nat}
    {s' : State}
    (h : step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td, boolV cm, boolV ev,
        boolV dp, boolV sm, enumV cl]⟩ = some s') :
    (x.kd = 2 → td = true) ∧ (x.kd ≠ 2 → td = false) := by
  rw [step_complete] at h
  obtain ⟨hc, -⟩ := ite_some_inv h
  have hk := hc.2.2.2.2.2.2.1
  cases td <;> simp at hk ⊢ <;> omega

/-! ## Theorem 4: replays -/

/-- Theorem 4: an `accept` of an accepted or completed round is accepted as a
replay and leaves the state unchanged, whatever the deadline says. -/
theorem accept_replay (p : Params) (x : Dp) (ar ae : Nat) (rc cy bf : Bool)
    (hb1 : ar ≤ p "maxRound") (hb2 : ae ≤ p "maxEpoch") (hf : Fenced x ar ae) (hr : rc = true) (hc : cy = true)
    (hp : x.ph = 3 ∨ x.ph = 4) :
    step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩ = some (enc x) := by
  rw [step_accept]
  have hn : ¬Pend x := by unfold Pend; omega
  simp [hb1, hb2, hf, hr, hc, hp, hn]

/-- Theorem 4: a `complete` of a completed round that carries the identical stored
receipt is accepted as a replay and leaves the state unchanged. -/
theorem complete_replay (p : Params) (x : Dp) (ar ae : Nat) (td : Bool) (cl : Nat)
    (hb1 : ar ≤ p "maxRound") (hb2 : ae ≤ p "maxEpoch") (hcl : cl < 3) (hf : Fenced x ar ae)
    (hk : decide (x.kd = 2) = td) (hp : x.ph = 4) (hr : x.rcmp = true) :
    step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV true, boolV true, boolV td, boolV true, boolV true,
        boolV true, boolV true, enumV cl]⟩ = some (enc x) := by
  rw [step_complete]
  have : { x with ph := 4, rcmp := true } = x := by cases x; simp_all
  simp [hb1, hb2, hcl, hf, hk, hp, this]

/-! ## Theorem 5: terminal phases -/

/-- Theorem 5: within one round, a terminal phase never changes: only `prepare`
leaves it, by creating the next round. -/
theorem terminal_stable {p : Params} {x y : Dp} {s' : State} {e : Event} (h : step M p (enc x) e = some s')
    (hy : s' = enc y) (ht : Term x) (hr : y.round = x.round) : y.ph = x.ph := by
  unfold Term at ht
  rcases forms h hy with h0 | ⟨h1, -⟩ | ⟨h1, -, -, hp⟩ | ⟨h1, -, -, ho⟩ | ⟨h1, -, -, hp⟩ | ⟨h1, -, -, hp⟩ |
    ⟨-, -, -, -, -, -, -, -, h4, -, hl, -⟩ | ⟨h1, -, ho⟩
  · subst h0; rfl
  · omega
  · exfalso; unfold Pend at hp; omega
  · exfalso; unfold Open at ho; omega
  · subst h1; have : ¬Pend x := by unfold Pend; omega
    simp [this]
  · subst h1; simp; omega
  · apply hl; unfold Leg; omega
  · subst h1; have : ¬Open x := by unfold Open; omega
    simp [this]

/-- Theorem 5: `attempt` and `reauthorize` refuse a terminal round. -/
theorem terminal_refused (p : Params) (x : Dp) (ht : Term x) (ar ae : Nat) (cs cy ev bf rm fr : Bool) :
    step M p (enc x) ⟨"attempt", [natV ar, natV ae, boolV cs, boolV cy, boolV ev, boolV bf]⟩ = none ∧
    step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV fr, boolV bf]⟩ =
      none := by
  have hp : ¬Pend x := by unfold Term at ht; unfold Pend; omega
  have ho : ¬Open x := by unfold Term at ht; unfold Open; omega
  constructor
  · rw [step_attempt]; simp [hp]
  · rw [step_reauthorize]; simp [ho]

/-! ## Theorem 6: ending a round -/

/-- Theorem 6: an accepted `end` of an accepted round carries a decision, a
failed or cancelled round is released only with a decision and the same phase,
and the release unbinds the card. -/
theorem end_spec {p : Params} {x y : Dp} {ar ae : Nat} {cs rm cy dc tf : Bool} {s' : State}
    (h : step M p (enc x) ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc, boolV tf]⟩ =
      some s')
    (hy : s' = enc y) :
    (x.ph = 3 → dc = true) ∧ (Leg x → dc = true ∧ y.ph = x.ph) ∧ y.bound = false ∧ y.rel = true ∧
      x.rel = false ∧ y.round = x.round ∧ y.epoch = x.epoch := by
  rw [step_end] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, -, -, -, hr, -, -, h3, hl⟩ := hc
  refine ⟨fun h => ?_, fun hleg => ?_, rfl, rfl, hr, rfl, rfl⟩
  · rcases h3 with h3 | h3
    · exact absurd h h3
    · exact h3
  · rcases hl with hl | ⟨hd, hi⟩
    · exact absurd hleg hl
    · refine ⟨hd, ?_⟩
      unfold Leg at hleg
      cases tf <;> simp at hi ⊢ <;> omega

/-- Theorem 6: a round whose release was written is never ended again. -/
theorem end_once (p : Params) (x : Dp) (hr : x.rel = true) (ar ae : Nat) (cs rm cy dc tf : Bool) :
    step M p (enc x) ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc, boolV tf]⟩ = none := by
  rw [step_end]; simp [hr]

/-- Theorem 6: only `prepare` clears a release, and it starts the next round: a
round is released at most once. -/
theorem release_once {p : Params} {x y : Dp} {s' : State} {e : Event} (h : step M p (enc x) e = some s')
    (hy : s' = enc y) (hr : x.rel = true) (hs : y.round = x.round) : y.rel = true := by
  rcases forms h hy with h0 | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨-, -, -, hr', -⟩ | ⟨h1, -⟩
  · subst h0; exact hr
  · omega
  · subst h1; exact hr
  · subst h1; exact hr
  · subst h1; exact hr
  · subst h1; exact hr
  · exact hr'
  · subst h1; exact hr

/-! ## Theorem 7: reauthorization -/

/-- Theorem 7: an accepted `reauthorize` of an accepted round carries fresh
stopped evidence; it raises the epoch by one, returns the round to `prepared`,
and clears the receipts of its acceptance and completion. -/
theorem reauthorize_spec {p : Params} {x y : Dp} {ar ae : Nat} {cs rm cy fr bf : Bool} {s' : State}
    (h : step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV fr,
        boolV bf]⟩ = some s')
    (hy : s' = enc y) :
    (x.ph = 3 → fr = true) ∧ (x.ph ≠ 3 → bf = true) ∧ y.epoch = x.epoch + 1 ∧ y.ph = 1 ∧
      y.round = x.round ∧ y.racc = false ∧ y.rcmp = false := by
  rw [step_reauthorize] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, -, -, -, -, -, h8, -⟩ := hc
  refine ⟨fun h3 => ?_, fun h3 => ?_, rfl, rfl, rfl, rfl, rfl⟩
  · simpa [h3] using h8
  · simpa [h3] using h8

/-! ## Theorem 8: cycles -/

/-- Theorem 8: with a plan cycle that differs from the round's, `attempt`,
`reauthorize`, `accept`, `complete`, `end`, `cancel`, and `retain` are refused;
`prepare` and `exec` carry no cycle. -/
theorem cycle_refused (p : Params) (x : Dp) (ar ae : Nat) (b1 b2 b3 b4 : Bool) (td cm ev dp sm : Bool)
    (cl : Nat) (rs ao : Bool) :
    step M p (enc x) ⟨"attempt", [natV ar, natV ae, boolV b1, boolV false, boolV b2, boolV b3]⟩ = none ∧
    step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV b1, boolV b2, boolV false, boolV b3, boolV b4]⟩ =
      none ∧
    step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV b1, boolV false, boolV b2]⟩ = none ∧
    step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV b1, boolV false, boolV td, boolV cm, boolV ev,
        boolV dp, boolV sm, enumV cl]⟩ = none ∧
    step M p (enc x) ⟨"end", [natV ar, natV ae, boolV b1, boolV b2, boolV false, boolV b3, boolV b4]⟩ = none ∧
    step M p (enc x) ⟨"cancel", [boolV rs, boolV ao, boolV false]⟩ = none ∧
    step M p (enc x) ⟨"retain", [natV ar, natV ae, boolV b1, boolV b2, boolV b3, boolV false]⟩ = none := by
  refine ⟨?_, ?_, ?_, ?_, ?_, ?_, ?_⟩
  · rw [step_attempt]; simp
  · rw [step_reauthorize]; simp
  · rw [step_accept]; simp
  · rw [step_complete]; simp
  · rw [step_end]; simp
  · rw [step_cancel]; simp
  · rw [step_retain]; simp

/-! ## Theorem 9: the invariant -/

/-- The safety properties of the slot: the phase and the epoch exist exactly
once a round exists, a binding names a round, a release belongs to a failed or
cancelled round and an empty binding, and an empty binding is the initial
state or a released round. -/
structure Props (x : Dp) : Prop where
  phaseRound : x.ph = 0 ↔ x.round = 0
  epochRound : x.epoch = 0 ↔ x.round = 0
  boundLive : x.bound = true → x.ph ≠ 0
  releasedUnbound : x.rel = true → x.bound = false ∧ Leg x
  unbound : x.bound = false → x.ph = 0 ∨ x.rel = true
  idleReceipts : (x.ph = 0 ∨ x.ph = 1 ∨ x.ph = 2) → x.racc = false ∧ x.rcmp = false
  acceptedReceipts : x.ph = 3 → x.racc = true ∧ x.rcmp = false
  completedReceipts : x.ph = 4 → x.racc = true ∧ x.rcmp = true
  endedReceipts : (x.ph = 5 ∨ x.ph = 6) → x.rcmp = false

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, false, 0, 0, false, false, false⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp⟩

macro "props_close" : tactic =>
  `(tactic| (refine ⟨?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_⟩ <;> intros <;> simp_all [Pend, Open, Leg] <;> omega))

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hlt, -⟩ := shape hc
  obtain ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩ := hp x rfl
  intro y hy
  have hrel : x.bound = true → x.rel = false := fun hb => by
    cases hq : x.rel
    · rfl
    · have := (P4 hq).1; simp_all
  rcases forms h hy with h0 | ⟨hr, hb, hph, hrl, -, he, hra, hrc⟩ | ⟨h1, hb, hr, hp⟩ | ⟨h1, hb, hr, ho⟩ |
    ⟨h1, hb, hr, hp⟩ | ⟨h1, hb, hr, hr'⟩ | ⟨hr, he, hb, hrl, hph, -, hxb, hxr, hx4, hxrel, -, hra, hrc⟩ |
    ⟨h1, hb, ho⟩
  · subst h0; exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9⟩
  · have : y.epoch ≠ 0 := by rw [he]; split <;> omega
    refine ⟨by omega, by omega, by simp [hph], by simp [hb, hrl], by simp [hb], ?_, ?_, ?_, ?_⟩ <;>
      intros <;> simp_all
  · subst h1; have := hrel hb; props_close
  · subst h1; have := hrel hb; props_close
  · subst h1; have := hrel hb
    by_cases hq : x.ph = 1 ∨ x.ph = 2 <;> props_close
  · subst h1; have := hrel hb
    have hx : x.ph = 3 ∨ x.ph = 4 := hr'
    rcases hx with hq | hq <;> props_close
  · have hx : x.ph ≠ 0 := P3 hxb
    have hleg := P9
    have hq : x.ph = 1 ∨ x.ph = 2 ∨ x.ph = 3 ∨ x.ph = 5 ∨ x.ph = 6 := by omega
    refine ⟨by omega, by omega, by simp [hb], fun _ => ⟨hb, by unfold Leg; omega⟩, fun _ => Or.inr hrl, ?_, ?_, ?_, ?_⟩ <;>
      intros <;> rcases hq with hq | hq | hq | hq | hq <;> simp_all <;> omega
  · subst h1; have := hrel hb
    have hx : x.ph = 1 ∨ x.ph = 2 ∨ x.ph = 3 ∨ x.ph = 5 ∨ x.ph = 6 := by
      unfold Open Leg at ho; omega
    rcases hx with hx | hx | hx | hx | hx <;> props_close

/-- The dispatch round machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant, for every value of
`maxRound` and `maxEpoch`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 9: in every reachable state a card without binding has no round or
a released one, so `accept`, `complete`, `attempt`, `reauthorize`, `end`, `cancel`, and `retain` are
refused there. -/
theorem reachable_unbound {p : Params} {x : Dp} (hr : Reachable M p (enc x)) (hb : x.bound = false) :
    x.ph = 0 ∨ x.rel = true := ((reachable_inv hr).2 x rfl).unbound hb

/-- Theorem 9: in every reachable state the receipts follow the phase: none while
a round is prepared or uncertain, the acceptance receipt once accepted, both once
completed, and never a completion receipt on a failed or cancelled round. -/
theorem reachable_receipts {p : Params} {x : Dp} (hr : Reachable M p (enc x)) :
    (x.ph = 0 ∨ x.ph = 1 ∨ x.ph = 2 → x.racc = false ∧ x.rcmp = false) ∧
    (x.ph = 3 → x.racc = true ∧ x.rcmp = false) ∧ (x.ph = 4 → x.racc = true ∧ x.rcmp = true) ∧
    (x.ph = 5 ∨ x.ph = 6 → x.rcmp = false) := by
  have h := (reachable_inv hr).2 x rfl
  exact ⟨h.idleReceipts, h.acceptedReceipts, h.completedReceipts, h.endedReceipts⟩

end KanderFormal.Proofs.Dispatch
