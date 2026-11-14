import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ExecutionLedger

/-
Proof obligations of the execution ledger machine. They hold for every value of
`maxSeq` and `maxRev`.
-/
namespace KanderFormal.Proofs.ExecutionLedger

open KanderFormal KanderFormal.Machines.ExecutionLedger KanderFormal.Proofs.Scalar

abbrev M := executionLedgerMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `col`: 0 todo, 1 working, 2 review,
3 closed. `st`: 0 none, 1 pending, 2 running, 3 unresolved, 4 rolled back,
5 handed off. `kd`: 0 none, 1 start, 2 claim, 3 takeover, 4 reclaim, 5 recover. -/
structure Led where
  col : Nat
  kd : Nat
  st : Nat
  rc : Nat
  seq : Nat
  rev : Nat
  cyc : Nat
  rr : Nat

def enc (x : Led) : State :=
  [("col", enumV x.col), ("kind", enumV x.kd), ("status", enumV x.st), ("record", natV x.rc),
    ("seq", natV x.seq), ("rev", natV x.rev), ("cycle", natV x.cyc), ("recRev", natV x.rr)]

theorem enc_inj {x y : Led} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _, _, _, _⟩ := y
  simp [enc, enumV, natV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

/-- Bounds of a conforming state. -/
structure Bounded (p : Params) (x : Led) : Prop where
  col : x.col < 4
  kd : x.kd < 6
  st : x.st < 6
  rc : x.rc ≤ p "maxSeq"
  seq : x.seq ≤ p "maxSeq"
  rev : x.rev ≤ p "maxRev"
  cyc : x.cyc ≤ p "maxSeq"
  rr : x.rr ≤ p "maxRev"

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) : ∃ x, s = enc x ∧ Bounded p x := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5), (_, v6), (_, v7), (_, v8)], hn =>
    simp [executionLedgerMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "col" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "kind" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "status" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "record" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "seq" v5 (by simp [lookup])
    obtain ⟨t6, ht6, c6⟩ := hc "rev" v6 (by simp [lookup])
    obtain ⟨t7, ht7, c7⟩ := hc "cycle" v7 (by simp [lookup])
    obtain ⟨t8, ht8, c8⟩ := hc "recRev" v8 (by simp [lookup])
    cases (rfl : M.fieldTy "col" = some (.scalar (.enum "col"))).symm.trans ht1
    cases (rfl : M.fieldTy "kind" = some (.scalar (.enum "kind"))).symm.trans ht2
    cases (rfl : M.fieldTy "status" = some (.scalar (.enum "status"))).symm.trans ht3
    cases (rfl : M.fieldTy "record" = some (.scalar (.nat "maxSeq"))).symm.trans ht4
    cases (rfl : M.fieldTy "seq" = some (.scalar (.nat "maxSeq"))).symm.trans ht5
    cases (rfl : M.fieldTy "rev" = some (.scalar (.nat "maxRev"))).symm.trans ht6
    cases (rfl : M.fieldTy "cycle" = some (.scalar (.nat "maxSeq"))).symm.trans ht7
    cases (rfl : M.fieldTy "recRev" = some (.scalar (.nat "maxRev"))).symm.trans ht8
    obtain ⟨c, rfl, hcol⟩ := conforms_enum (rfl : M.enumSize "col" = some 4) c1
    obtain ⟨k, rfl, hk⟩ := conforms_enum (rfl : M.enumSize "kind" = some 6) c2
    obtain ⟨st, rfl, hst⟩ := conforms_enum (rfl : M.enumSize "status" = some 6) c3
    obtain ⟨r, rfl, hr⟩ := conforms_nat c4
    obtain ⟨sq, rfl, hsq⟩ := conforms_nat c5
    obtain ⟨rv, rfl, hrv⟩ := conforms_nat c6
    obtain ⟨cy, rfl, hcy⟩ := conforms_nat c7
    obtain ⟨rr, rfl, hrr⟩ := conforms_nat c8
    exact ⟨⟨c, k, st, r, sq, rv, cy, rr⟩, rfl, ⟨hcol, hk, hst, hr, hsq, hrv, hcy, hrr⟩⟩

/-! ## Events on an encoded state -/

theorem find_claim : M.findEvent "claim" = some claimEvent := rfl
theorem find_start : M.findEvent "start" = some startEvent := rfl
theorem find_reclaim : M.findEvent "reclaim" = some reclaimEvent := rfl
theorem find_recover : M.findEvent "recover" = some recoverEvent := rfl
theorem find_relocate : M.findEvent "relocate" = some relocateEvent := rfl
theorem find_write : M.findEvent "write" = some writeEvent := rfl
theorem find_confirm : M.findEvent "confirm" = some confirmEvent := rfl
theorem find_unresolve : M.findEvent "unresolve" = some unresolveEvent := rfl
theorem find_adopt : M.findEvent "adopt" = some adoptEvent := rfl
theorem find_takeover : M.findEvent "takeover" = some takeoverEvent := rfl
theorem find_rollback : M.findEvent "rollback" = some rollbackEvent := rfl
theorem find_field : M.findEvent "field" = some fieldEvent := rfl
theorem ctors_col : M.ctorsOf "col" = some ["todo", "working", "review", "closed"] := rfl
theorem ctors_status : M.ctorsOf "status" =
    some ["none", "pending", "running", "unresolved", "rolledBack", "handedOff"] := rfl
theorem ctors_kind : M.ctorsOf "kind" = some ["none", "start", "claim", "takeover", "reclaim", "recover"] := rfl
theorem size_col : M.enumSize "col" = some 4 := rfl
theorem size_status : M.enumSize "status" = some 6 := rfl
theorem size_kind : M.enumSize "kind" = some 6 := rfl
theorem ty_col : M.fieldTy "col" = some (.scalar (.enum "col")) := rfl
theorem ty_kind : M.fieldTy "kind" = some (.scalar (.enum "kind")) := rfl
theorem ty_status : M.fieldTy "status" = some (.scalar (.enum "status")) := rfl
theorem ty_record : M.fieldTy "record" = some (.scalar (.nat "maxSeq")) := rfl
theorem ty_seq : M.fieldTy "seq" = some (.scalar (.nat "maxSeq")) := rfl
theorem ty_rev : M.fieldTy "rev" = some (.scalar (.nat "maxRev")) := rfl
theorem ty_cycle : M.fieldTy "cycle" = some (.scalar (.nat "maxSeq")) := rfl
theorem ty_recRev : M.fieldTy "recRev" = some (.scalar (.nat "maxRev")) := rfl

macro "led_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_claim, find_start, find_reclaim, find_recover, find_relocate, find_write,
      find_confirm, find_unresolve, find_adopt, find_takeover, find_rollback, find_field, ctors_col, ctors_status,
      ctors_kind, size_col, size_status, size_kind, ty_col, ty_kind, ty_status, ty_record, ty_seq, ty_rev, ty_cycle,
      ty_recRev, lookup, claimEvent, startEvent,
      reclaimEvent, recoverEvent, relocateEvent, writeEvent, confirmEvent, unresolveEvent, adoptEvent,
      takeoverEvent, rollbackEvent, fieldEvent, newRecord, allOf, isCol, statusIs, kindIs, col, held, atRev, fenced,
      ended, unfinished, atArg, fenceArg, boolArg, inc, eq, bindArgs, Ty.conforms, STy.conforms,
      eval, evalCmp, natAdd, natSub, boolAnd, boolOr, boolNot, indexOf, enumV, boolV, natV, f, a,
      nat, tt, ff, andE, orE, notE, evalAssigns, applyUpdates, enc, or_assoc,
      and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

/-- Closes an equation of two `if`s whose conditions are equivalent. -/
macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

/-- The previous record has ended. -/
def Ended (x : Led) : Prop := x.st = 0 ∨ x.st = 4 ∨ x.st = 5
/-- The card is in `working` or `review`. -/
def Held (x : Led) : Prop := x.col = 1 ∨ x.col = 2

instance (x : Led) : Decidable (Ended x) := by unfold Ended; infer_instance
instance (x : Led) : Decidable (Held x) := by unfold Held; infer_instance

/-- A new current record fits the bounds. -/
def Fits (p : Params) (x : Led) (claims : Nat) : Prop :=
  x.rc + 1 ≤ p "maxSeq" ∧ x.seq + 1 ≤ p "maxSeq" ∧ x.rev + 1 ≤ p "maxRev" ∧ x.cyc + claims ≤ p "maxSeq"

instance (p : Params) (x : Led) (c : Nat) : Decidable (Fits p x c) := by unfold Fits; infer_instance

/-- The state after a new current record of kind `k` and status `st`. -/
def fresh (x : Led) (k st claims : Nat) : Led :=
  ⟨1, k, st, x.rc + 1, x.seq + 1, x.rev + 1, x.cyc + claims, x.rev + 1⟩

set_option maxHeartbeats 2000000 in
theorem step_claim (p : Params) (x : Led) (hb : Bounded p x) (at_ : Nat) (owner decision released chain : Bool) :
    step M p (enc x) ⟨"claim", [natV at_, boolV owner, boolV decision, boolV released, boolV chain]⟩ =
      if at_ ≤ p "maxRev" ∧ x.col = 0 ∧ at_ = x.rev ∧ decision = false ∧ ¬(owner = true ∧ released = true) ∧
          Ended x ∧ chain = true ∧ Fits p x (if owner then 1 else 0) then
        some (enc (fresh x 2 2 (if owner then 1 else 0))) else none := by
  by_cases hat : at_ ≤ p "maxRev" <;> cases owner <;>
    led_simp [hat, Ended, Fits, fresh, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col] <;> ite_close

set_option hygiene false in
macro "led_close" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| (have := hb.rc; have := hb.seq; have := hb.rev; have := hb.cyc; have := hb.rr; led_simp [Ended, Held, Fits, fresh, $ts,*] <;> ite_close))

set_option maxHeartbeats 2000000 in
theorem step_start (p : Params) (x : Led) (hb : Bounded p x) (at_ : Nat) (chain : Bool) :
    step M p (enc x) ⟨"start", [natV at_, boolV chain]⟩ =
      if at_ ≤ p "maxRev" ∧ x.col = 0 ∧ at_ = x.rev ∧ Ended x ∧ chain = true ∧ Fits p x 1 then
        some (enc (fresh x 1 1 1)) else none := by
  by_cases hat : at_ ≤ p "maxRev" <;> led_close [hat, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

set_option maxHeartbeats 2000000 in
theorem step_reclaim (p : Params) (x : Led) (hb : Bounded p x) (at_ : Nat) (decision proof : Bool) :
    step M p (enc x) ⟨"reclaim", [natV at_, boolV decision, boolV proof]⟩ =
      if at_ ≤ p "maxRev" ∧ x.col = 2 ∧ at_ = x.rev ∧ decision = true ∧ proof = true ∧ Fits p x 1 then
        some (enc (fresh x 4 2 1)) else none := by
  by_cases hat : at_ ≤ p "maxRev" <;> led_close [hat, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

set_option maxHeartbeats 2000000 in
theorem step_recover (p : Params) (x : Led) (hb : Bounded p x) (at_ : Nat) (unbound cas : Bool) :
    step M p (enc x) ⟨"recover", [natV at_, boolV unbound, boolV cas]⟩ =
      if at_ ≤ p "maxRev" ∧ Held x ∧ at_ = x.rev ∧ x.st ≠ 1 ∧ unbound = true ∧ cas = true ∧ Fits p x 1 then
        some (enc (fresh x 5 2 1)) else none := by
  by_cases hat : at_ ≤ p "maxRev" <;> led_close [hat, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

/-- The moves that record no execution. -/
def Reloc (c t : Nat) : Prop :=
  c = 1 ∧ t = 2 ∨ c = 2 ∧ t = 1 ∨ c ≠ 3 ∧ t = 3 ∨ c = 0 ∧ t = 0 ∨ c = 3 ∧ t = 3

instance (c t : Nat) : Decidable (Reloc c t) := by unfold Reloc; infer_instance

set_option maxHeartbeats 2000000 in
theorem step_relocate (p : Params) (x : Led) (hb : Bounded p x) (at_ to : Nat) :
    step M p (enc x) ⟨"relocate", [natV at_, enumV to]⟩ =
      if at_ ≤ p "maxRev" ∧ to < 4 ∧ at_ = x.rev ∧ Reloc x.col to ∧ x.rev + 1 ≤ p "maxRev" then
        some (enc { x with col := to, rev := x.rev + 1 }) else none := by
  by_cases hat : at_ ≤ p "maxRev" <;> by_cases hto : to < 4 <;>
    led_close [hat, hto, Reloc, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

set_option maxHeartbeats 2000000 in
theorem step_write (p : Params) (x : Led) (hb : Bounded p x) (at_ : Nat) :
    step M p (enc x) ⟨"write", [natV at_]⟩ =
      if at_ ≤ p "maxRev" ∧ at_ = x.rev ∧ x.rev + 1 ≤ p "maxRev" then
        some (enc { x with rev := x.rev + 1 }) else none := by
  by_cases hat : at_ ≤ p "maxRev" <;> led_close [hat, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

/-- The writer's fence is the current record. -/
def Fenced (x : Led) (fence : Nat) : Prop := fence = x.rc ∧ 1 ≤ x.rc

instance (x : Led) (fence : Nat) : Decidable (Fenced x fence) := by unfold Fenced; infer_instance

set_option maxHeartbeats 2000000 in
theorem step_confirm (p : Params) (x : Led) (hb : Bounded p x) (fence : Nat) (m session : Bool) :
    step M p (enc x) ⟨"confirm", [natV fence, boolV m, boolV session]⟩ =
      if fence ≤ p "maxSeq" ∧ Fenced x fence ∧ m = true ∧
          (x.st = 2 ∨ x.st = 1 ∧ (session = false ∨ Held x)) ∧
          (x.st = 1 → x.seq + 1 ≤ p "maxSeq") ∧ (x.st = 1 ∧ session = true → x.rev + 1 ≤ p "maxRev") then
        some (enc ⟨x.col, x.kd, 2, x.rc, if x.st = 1 then x.seq + 1 else x.seq,
          if x.st = 1 ∧ session = true then x.rev + 1 else x.rev, x.cyc, x.rr⟩) else none := by
  by_cases hf : fence ≤ p "maxSeq" <;> by_cases h1 : x.st = 1 <;> cases session <;>
    led_close [hf, h1, Fenced, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

set_option maxHeartbeats 2000000 in
theorem step_unresolve (p : Params) (x : Led) (hb : Bounded p x) (fence at_ : Nat) (launcher window : Bool) :
    step M p (enc x) ⟨"unresolve", [natV fence, natV at_, boolV launcher, boolV window]⟩ =
      if fence ≤ p "maxSeq" ∧ at_ ≤ p "maxRev" ∧ Fenced x fence ∧ launcher = true ∧ x.st = 1 ∧ x.rr ≤ at_ ∧
          x.seq + 1 ≤ p "maxSeq" ∧ (window = true ∧ Held x → x.rev + 1 ≤ p "maxRev") then
        some (enc ⟨x.col, x.kd, 3, x.rc, x.seq + 1, if window = true ∧ Held x then x.rev + 1 else x.rev,
          x.cyc, x.rr⟩) else none := by
  by_cases hf : fence ≤ p "maxSeq" <;> by_cases hat : at_ ≤ p "maxRev" <;> cases window <;>
    by_cases h1 : x.col = 1 <;> by_cases h2 : x.col = 2 <;>
    led_close [hf, hat, h1, h2, Fenced, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

set_option maxHeartbeats 2000000 in
theorem step_adopt (p : Params) (x : Led) (hb : Bounded p x) (fence : Nat) (decision identity : Bool) :
    step M p (enc x) ⟨"adopt", [natV fence, boolV decision, boolV identity]⟩ =
      if fence ≤ p "maxSeq" ∧ Fenced x fence ∧ decision = true ∧ (x.st = 1 ∨ x.st = 3) ∧ Held x ∧
          identity = true ∧ x.seq + 1 ≤ p "maxSeq" then
        some (enc { x with st := 2, seq := x.seq + 1 }) else none := by
  by_cases hf : fence ≤ p "maxSeq" <;> led_close [hf, Fenced, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

set_option maxHeartbeats 2000000 in
theorem step_takeover (p : Params) (x : Led) (hb : Bounded p x) (fence : Nat) (decision changes : Bool) :
    step M p (enc x) ⟨"takeover", [natV fence, boolV decision, boolV changes]⟩ =
      if fence ≤ p "maxSeq" ∧ Fenced x fence ∧ ¬Ended x ∧ (¬(x.st = 1 ∨ x.st = 3) ∨ decision = true) ∧
          (changes = false ∨ Held x) ∧ x.rc + 1 ≤ p "maxSeq" ∧ x.seq + 1 ≤ p "maxSeq" ∧
          (changes = true → x.rev + 1 ≤ p "maxRev") then
        some (enc ⟨x.col, 3, 1, x.rc + 1, x.seq + 1, if changes then x.rev + 1 else x.rev, x.cyc,
          if changes then x.rev + 1 else x.rev⟩) else none := by
  by_cases hf : fence ≤ p "maxSeq" <;> cases changes <;>
    led_close [hf, Fenced, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

set_option maxHeartbeats 4000000 in
theorem step_rollback_start (p : Params) (x : Led) (hb : Bounded p x) (fence at_ rk rs : Nat) (launcher : Bool) :
    step M p (enc x) ⟨"rollback", [natV fence, natV at_, boolV launcher, boolV true, enumV rk, enumV rs]⟩ =
      if fence ≤ p "maxSeq" ∧ at_ ≤ p "maxRev" ∧ rk < 6 ∧ rs < 6 ∧ Fenced x fence ∧ at_ = x.rev ∧
          launcher = true ∧ x.st = 1 ∧ x.rr ≤ at_ ∧ x.kd = 1 ∧ x.col = 1 ∧ 1 ≤ x.cyc ∧
          x.seq + 1 ≤ p "maxSeq" ∧ x.rev + 1 ≤ p "maxRev" then
        some (enc ⟨0, x.kd, 4, x.rc, x.seq + 1, x.rev + 1, x.cyc - 1, x.rr⟩)
      else none := by
  by_cases hf : fence ≤ p "maxSeq" <;> by_cases hat : at_ ≤ p "maxRev" <;> by_cases hk : rk < 6 <;>
    by_cases hs : rs < 6 <;> by_cases hc : 1 ≤ x.cyc <;>
    led_close [hf, hat, hk, hs, hc, Fenced, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

/-- The kind and status a takeover rollback may reinstate. -/
def Restorable (rk rs : Nat) : Prop :=
  rk ≠ 0 ∧ (rs = 1 ∨ rs = 2 ∨ rs = 3) ∧ (rs = 1 → rk = 1 ∨ rk = 3)

instance (rk rs : Nat) : Decidable (Restorable rk rs) := by unfold Restorable; infer_instance

set_option maxHeartbeats 4000000 in
theorem step_rollback_takeover (p : Params) (x : Led) (hb : Bounded p x) (fence at_ rk rs : Nat)
    (launcher : Bool) :
    step M p (enc x) ⟨"rollback", [natV fence, natV at_, boolV launcher, boolV false, enumV rk, enumV rs]⟩ =
      if fence ≤ p "maxSeq" ∧ at_ ≤ p "maxRev" ∧ rk < 6 ∧ rs < 6 ∧ Fenced x fence ∧ at_ = x.rev ∧
          launcher = true ∧ x.st = 1 ∧ x.rr ≤ at_ ∧ x.kd = 3 ∧ Held x ∧ Restorable rk rs ∧
          x.rc + 1 ≤ p "maxSeq" ∧ x.seq + 1 ≤ p "maxSeq" ∧ x.rev + 1 ≤ p "maxRev" then
        some (enc ⟨x.col, rk, rs, x.rc + 1, x.seq + 1, x.rev + 1, x.cyc, x.rev + 1⟩)
      else none := by
  by_cases hf : fence ≤ p "maxSeq" <;> by_cases hat : at_ ≤ p "maxRev" <;> by_cases hk : rk < 6 <;>
    by_cases hs : rs < 6 <;>
    led_close [hf, hat, hk, hs, Fenced, Restorable, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

set_option maxHeartbeats 2000000 in
theorem step_field (p : Params) (x : Led) (hb : Bounded p x) (fence : Nat) (text : Bool) :
    step M p (enc x) ⟨"field", [natV fence, boolV text]⟩ =
      if fence ≤ p "maxSeq" ∧ Fenced x fence ∧ (text = false ∨ Held x) ∧ x.seq + 1 ≤ p "maxSeq" ∧
          (text = true → x.rev + 1 ≤ p "maxRev") then
        some (enc ⟨x.col, x.kd, x.st, x.rc, x.seq + 1, if text then x.rev + 1 else x.rev, x.cyc, x.rr⟩) else none := by
  by_cases hf : fence ≤ p "maxSeq" <;> cases text <;>
    led_close [hf, Fenced, hb.rev, hb.rc, hb.seq, hb.cyc, hb.rr, hb.col]

/-! ## The events an accepted step ran -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = claimEvent ∨ ev = startEvent ∨ ev = reclaimEvent ∨ ev = recoverEvent ∨ ev = relocateEvent ∨
      ev = writeEvent ∨ ev = confirmEvent ∨ ev = unresolveEvent ∨ ev = adoptEvent ∨ ev = takeoverEvent ∨
      ev = rollbackEvent ∨ ev = fieldEvent := by
  simpa [executionLedgerMachine] using h

theorem nat_arg {p : Params} {b : String} {v : Val} (h : Ty.conforms M p (.scalar (.nat b)) v = true) :
    ∃ n, v = natV n := by
  obtain ⟨n, rfl, -⟩ := conforms_nat h; exact ⟨n, rfl⟩

theorem bool_arg {p : Params} {v : Val} (h : Ty.conforms M p (.scalar .bool) v = true) : ∃ b, v = boolV b :=
  conforms_bool h

theorem enum_arg {p : Params} {e : String} {n : Nat} (hn : M.enumSize e = some n) {v : Val}
    (h : Ty.conforms M p (.scalar (.enum e)) v = true) : ∃ i, v = enumV i := by
  obtain ⟨i, rfl, -⟩ := conforms_enum hn h; exact ⟨i, rfl⟩

/-- Every accepted event is one of the twelve, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ at_ o d r c, e = ⟨"claim", [natV at_, boolV o, boolV d, boolV r, boolV c]⟩) ∨
      (∃ at_ c, e = ⟨"start", [natV at_, boolV c]⟩) ∨
      (∃ at_ d pr, e = ⟨"reclaim", [natV at_, boolV d, boolV pr]⟩) ∨
      (∃ at_ u c, e = ⟨"recover", [natV at_, boolV u, boolV c]⟩) ∨
      (∃ at_ to, e = ⟨"relocate", [natV at_, enumV to]⟩) ∨
      (∃ at_, e = ⟨"write", [natV at_]⟩) ∨
      (∃ fe m se, e = ⟨"confirm", [natV fe, boolV m, boolV se]⟩) ∨
      (∃ fe at_ l w, e = ⟨"unresolve", [natV fe, natV at_, boolV l, boolV w]⟩) ∨
      (∃ fe d i, e = ⟨"adopt", [natV fe, boolV d, boolV i]⟩) ∨
      (∃ fe d c, e = ⟨"takeover", [natV fe, boolV d, boolV c]⟩) ∨
      (∃ fe at_ l t rk rs, e = ⟨"rollback", [natV fe, natV at_, boolV l, boolV t, enumV rk, enumV rs]⟩) ∨
      (∃ fe t, e = ⟨"field", [natV fe, boolV t]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    match bs, hl with
    | [o, d, r, c], _ => exact .inl ⟨n1, o, d, r, c, rfl⟩
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    match bs, hl with
    | [c], _ => exact .inr (.inl ⟨n1, c, rfl⟩)
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    match bs, hl with
    | [d, pr], _ => exact .inr (.inr (.inl ⟨n1, d, pr, rfl⟩))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    match bs, hl with
    | [u, c], _ => exact .inr (.inr (.inr (.inl ⟨n1, u, c, rfl⟩)))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    obtain ⟨n1, rfl⟩ := nat_arg c1
    obtain ⟨to, rfl⟩ := enum_arg (rfl : M.enumSize "col" = some 4) c2
    exact .inr (.inr (.inr (.inr (.inl ⟨n1, to, rfl⟩))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    exact .inr (.inr (.inr (.inr (.inr (.inl ⟨n1, rfl⟩)))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    match bs, hl with
    | [m, se], _ => exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨n1, m, se, rfl⟩))))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h2
    obtain ⟨n1, rfl⟩ := nat_arg c1
    obtain ⟨n2, rfl⟩ := nat_arg c2
    match bs, hl with
    | [l, w], _ => exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨n1, n2, l, w, rfl⟩)))))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    match bs, hl with
    | [d, i], _ => exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨n1, d, i, rfl⟩))))))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    match bs, hl with
    | [d, c], _ => exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨n1, d, c, rfl⟩)))))))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    obtain ⟨n1, rfl⟩ := nat_arg c1
    obtain ⟨n2, rfl⟩ := nat_arg c2
    obtain ⟨l, rfl⟩ := bool_arg c3
    obtain ⟨t, rfl⟩ := bool_arg c4
    obtain ⟨rk, rfl⟩ := enum_arg (rfl : M.enumSize "kind" = some 6) c5
    obtain ⟨rs, rfl⟩ := enum_arg (rfl : M.enumSize "status" = some 6) c6
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨n1, n2, l, t, rk, rs, rfl⟩))))))))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [boolArg]) h1
    obtain ⟨n1, rfl⟩ := nat_arg c1
    match bs, hl with
    | [t], _ => exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr ⟨n1, t, rfl⟩))))))))))

/-! ## Theorem 5: stale writers -/

theorem step_rollback (p : Params) (x : Led) (hb : Bounded p x) (fence at_ rk rs : Nat) (launcher toTodo : Bool)
    (hs : step M p (enc x) ⟨"rollback", [natV fence, natV at_, boolV launcher, boolV toTodo, enumV rk, enumV rs]⟩ ≠ none) :
    Fenced x fence ∧ at_ = x.rev ∧ x.rr ≤ at_ := by
  cases toTodo
  · rw [step_rollback_takeover p x hb] at hs
    split at hs
    · rename_i hg; exact ⟨hg.2.2.2.2.1, hg.2.2.2.2.2.1, hg.2.2.2.2.2.2.2.2.1⟩
    · exact absurd rfl hs
  · rw [step_rollback_start p x hb] at hs
    split at hs
    · rename_i hg; exact ⟨hg.2.2.2.2.1, hg.2.2.2.2.2.1, hg.2.2.2.2.2.2.2.2.1⟩
    · exact absurd rfl hs

/-- Theorem 5, revision-checked writes: a claim, a start, a reclaim, a recovery,
a move, a card write, or a rollback whose card revision is not the card's is
refused, and an unresolve whose cursor is older than the current record is
refused. A refused event leaves the state unchanged (`run_refused_unchanged`). -/
theorem stale_revision_refused (p : Params) (x : Led) (hb : Bounded p x) (at_ : Nat) (h : at_ ≠ x.rev) :
    (∀ o d r c, step M p (enc x) ⟨"claim", [natV at_, boolV o, boolV d, boolV r, boolV c]⟩ = none) ∧
      (∀ c, step M p (enc x) ⟨"start", [natV at_, boolV c]⟩ = none) ∧
      (∀ d pr, step M p (enc x) ⟨"reclaim", [natV at_, boolV d, boolV pr]⟩ = none) ∧
      (∀ u c, step M p (enc x) ⟨"recover", [natV at_, boolV u, boolV c]⟩ = none) ∧
      (∀ to, step M p (enc x) ⟨"relocate", [natV at_, enumV to]⟩ = none) ∧
      step M p (enc x) ⟨"write", [natV at_]⟩ = none ∧
      (∀ fe l t rk rs, step M p (enc x) ⟨"rollback", [natV fe, natV at_, boolV l, boolV t, enumV rk, enumV rs]⟩ = none) := by
  refine ⟨fun o d r c => ?_, fun c => ?_, fun d pr => ?_, fun u c => ?_, fun to => ?_, ?_, fun fe l t rk rs => ?_⟩
  · rw [step_claim p x hb]; simp [h]
  · rw [step_start p x hb]; simp [h]
  · rw [step_reclaim p x hb]; simp [h]
  · rw [step_recover p x hb]; simp [h]
  · rw [step_relocate p x hb]; simp [h]
  · rw [step_write p x hb]; simp [h]
  · exact Classical.byContradiction fun hs => h (step_rollback p x hb fe at_ rk rs l t hs).2.1

theorem unresolve_stale (p : Params) (x : Led) (hb : Bounded p x) (fence at_ : Nat) (l w : Bool) (h : at_ < x.rr) :
    step M p (enc x) ⟨"unresolve", [natV fence, natV at_, boolV l, boolV w]⟩ = none := by
  rw [step_unresolve p x hb]; simp; omega

/-- Theorem 5, fenced writes: a confirmation, an unresolve, an adoption, a
takeover, a rollback, or a field write fenced to another record than the
current one is refused, and leaves the state unchanged. -/
theorem stale_fence_refused (p : Params) (x : Led) (hb : Bounded p x) (fence : Nat) (h : fence ≠ x.rc) :
    (∀ m se, step M p (enc x) ⟨"confirm", [natV fence, boolV m, boolV se]⟩ = none) ∧
      (∀ at_ l w, step M p (enc x) ⟨"unresolve", [natV fence, natV at_, boolV l, boolV w]⟩ = none) ∧
      (∀ d i, step M p (enc x) ⟨"adopt", [natV fence, boolV d, boolV i]⟩ = none) ∧
      (∀ d c, step M p (enc x) ⟨"takeover", [natV fence, boolV d, boolV c]⟩ = none) ∧
      (∀ at_ l t rk rs, step M p (enc x) ⟨"rollback", [natV fence, natV at_, boolV l, boolV t, enumV rk, enumV rs]⟩ = none) ∧
      (∀ t, step M p (enc x) ⟨"field", [natV fence, boolV t]⟩ = none) := by
  refine ⟨fun m se => ?_, fun at_ l w => ?_, fun d i => ?_, fun d c => ?_, fun at_ l t rk rs => ?_, fun t => ?_⟩
  · rw [step_confirm p x hb]; simp [Fenced, h]
  · rw [step_unresolve p x hb]; simp [Fenced, h]
  · rw [step_adopt p x hb]; simp [Fenced, h]
  · rw [step_takeover p x hb]; simp [Fenced, h]
  · exact Classical.byContradiction fun hs => h (step_rollback p x hb fence at_ rk rs l t hs).1.1
  · rw [step_field p x hb]; cases t <;> simp [Fenced, h]

/-! ## Theorem 7: the invariant -/

/-- The safety properties of a ledger: a card has a current record exactly when
the record position is positive, and then a kind; no more records than
transitions and no more claim lines than records; a card in `backlog` or `todo`
has no record that still owns it; only a start or a takeover is pending; and
the current record is never newer than the card. -/
structure Props (x : Led) : Prop where
  noneZero : x.st = 0 ↔ x.rc = 0
  kindNone : x.kd = 0 ↔ x.st = 0
  recSeq : x.rc ≤ x.seq
  cycRec : x.cyc ≤ x.rc
  todoEnded : x.col = 0 → Ended x
  pendingKind : x.st = 1 → x.kd = 1 ∨ x.kd = 3
  rrLe : x.rr ≤ x.rev

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, 0, 0, 0, 0, 0, 0⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨by simp, by simp, by simp, by simp, fun _ => by simp [Ended], by simp, by simp⟩

/-- Closes the properties of a post-state from the guard and the pre-state's
properties. -/
macro "props_close" : tactic =>
  `(tactic| (constructor <;> simp_all [Ended, Held, Fenced, Fits, fresh, Reloc, Restorable] <;> omega))

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hb⟩ := shape hc
  have P := hp x rfl
  obtain ⟨P1, P2, P3, P4, P5, P6, P7⟩ := P
  intro y hy
  rcases accepted_cases h with ⟨at_, o, d, r, c, rfl⟩ | ⟨at_, c, rfl⟩ | ⟨at_, d, pr, rfl⟩ | ⟨at_, u, c, rfl⟩ |
    ⟨at_, to, rfl⟩ | ⟨at_, rfl⟩ | ⟨fe, m, se, rfl⟩ | ⟨fe, at_, l, w, rfl⟩ | ⟨fe, d, i, rfl⟩ | ⟨fe, d, c, rfl⟩ |
    ⟨fe, at_, l, t, rk, rs, rfl⟩ | ⟨fe, t, rfl⟩
  · rw [step_claim p x hb] at h
    cases o <;> simp only [Bool.false_eq_true, ite_false, ite_true] at h <;>
      (split at h <;> cases h; have := enc_inj hy; subst this; props_close)
  · rw [step_start p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_reclaim p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_recover p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_relocate p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_write p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_confirm p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this
    by_cases h1 : x.st = 1 <;> cases se <;> props_close
  · rw [step_unresolve p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this
    by_cases hh : Held x <;> cases w <;> props_close
  · rw [step_adopt p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_takeover p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this
    cases c <;> props_close
  · cases t
    · rw [step_rollback_takeover p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this
      props_close
    · rw [step_rollback_start p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this
      props_close
  · rw [step_field p x hb] at h; split at h <;> cases h; have := enc_inj hy; subst this
    cases t <;> props_close

/-- The execution ledger machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 7: every reachable state satisfies the invariant, for every value
of `maxSeq` and `maxRev`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

end KanderFormal.Proofs.ExecutionLedger
