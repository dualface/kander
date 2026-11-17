import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ServerGroupLease

/-
The step lemmas of the server group lease machine: each event on an encoded
state evaluates to an explicit `if`, on which `ServerGroupLease.lean` proves the
theorems.
-/
namespace KanderFormal.Proofs.ServerGroupLease

open KanderFormal KanderFormal.Machines.ServerGroupLease KanderFormal.Proofs.Scalar

abbrev M := serverGroupLeaseMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `st`: 0 none, 1 active, 2 lost,
3 released, 4 lapsed (a view value that is never stored). -/
structure Gl where
  st : Nat
  gen : Nat
  live : Bool

def enc (x : Gl) : State :=
  [("status", enumV x.st), ("gen", natV x.gen), ("live", boolV x.live)]

theorem enc_inj {x y : Gl} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _⟩ := x
  obtain ⟨_, _, _⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.st < 5 ∧ x.gen ≤ p "maxGen" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3)], hn =>
    simp [serverGroupLeaseMachine] at hn
    obtain ⟨rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "status" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "gen" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "live" v3 (by simp [lookup])
    cases (rfl : M.fieldTy "status" = some (.scalar (.enum "status"))).symm.trans ht1
    cases (rfl : M.fieldTy "gen" = some (.scalar (.nat "maxGen"))).symm.trans ht2
    cases (rfl : M.fieldTy "live" = some (.scalar .bool)).symm.trans ht3
    obtain ⟨st, rfl, hst⟩ := conforms_enum (rfl : M.enumSize "status" = some 5) c1
    obtain ⟨g, rfl, hg⟩ := conforms_nat c2
    obtain ⟨lv, rfl⟩ := conforms_bool c3
    exact ⟨⟨st, g, lv⟩, rfl, hst, hg⟩

/-! ## Events on an encoded state -/

theorem find_claim : M.findEvent "claim" = some claimEvent := rfl
theorem find_renew : M.findEvent "renew" = some renewEvent := rfl
theorem find_observe : M.findEvent "observe" = some observeEvent := rfl
theorem find_release : M.findEvent "release" = some releaseEvent := rfl
theorem find_require : M.findEvent "require" = some requireEvent := rfl
theorem find_writeAfter : M.findEvent "writeAfter" = some writeAfterEvent := rfl
theorem find_recoverOwned : M.findEvent "recoverOwned" = some recoverOwnedEvent := rfl
theorem find_recoverLost : M.findEvent "recoverLost" = some recoverLostEvent := rfl
theorem find_expire : M.findEvent "expire" = some expireEvent := rfl
theorem find_startup : M.findEvent "startup" = some startupEvent := rfl
theorem find_standing : M.findEvent "standing" = some standingEvent := rfl
theorem ctors_status : M.ctorsOf "status" = some ["none", "active", "lost", "released", "lapsed"] := rfl
theorem size_status : M.enumSize "status" = some 5 := rfl
theorem ty_status : M.fieldTy "status" = some (.scalar (.enum "status")) := rfl
theorem ty_gen : M.fieldTy "gen" = some (.scalar (.nat "maxGen")) := rfl
theorem ty_live : M.fieldTy "live" = some (.scalar .bool) := rfl

macro "gl_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_claim, find_renew, find_observe, find_release, find_require, find_writeAfter,
      find_recoverOwned, find_recoverLost, find_expire, find_startup, find_standing, ctors_status, size_status, ty_status, ty_gen, ty_live,
      lookup, claimEvent, renewEvent, observeEvent, releaseEvent, requireEvent, writeAfterEvent,
      recoverOwnedEvent, recoverLostEvent, expireEvent, startupEvent, standingEvent, holder, valid, claimGuard, genArg, boolArg, holderArgs,
      statusIs, gen, inc, eq, bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr,
      boolNot, indexOf, enumV, boolV, natV, f, a, tt, ff, andE, orE, notE, evalAssigns, applyUpdates, enc,
      or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

/-- `requireGroup` accepts the request: holder authority, whatever the lease
clock says. -/
def Holder (x : Gl) (hdr : Bool) (g tg : Nat) : Prop :=
  hdr = true ∧ x.st = 1 ∧ tg = x.gen ∧ g = x.gen

instance (x : Gl) (hdr : Bool) (g tg : Nat) : Decidable (Holder x hdr g tg) := by
  unfold Holder; infer_instance

/-- A valid lease: active and not expired. -/
def Valid (x : Gl) : Prop := x.st = 1 ∧ x.live = true

instance (x : Gl) : Decidable (Valid x) := by unfold Valid; infer_instance

theorem step_renew (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) :
    step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg then some (enc { x with live := true })
      else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> gl_simp [h1, h2, Holder] <;> ite_close


theorem step_observe (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (stopped : Bool) :
    step M p (enc x) ⟨"observe", [boolV hdr, natV g, natV tg, boolV stopped]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg then
        some (enc ⟨if stopped = true then 2 else x.st, x.gen, if stopped = true then false else x.live⟩)
      else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> cases stopped <;>
    gl_simp [h1, h2, Holder] <;> ite_close

theorem step_release (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) :
    step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg then some (enc { x with st := 3 })
      else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> gl_simp [h1, h2, Holder] <;> ite_close

theorem step_require (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) :
    step M p (enc x) ⟨"require", [boolV hdr, natV g, natV tg]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> gl_simp [h1, h2, Holder] <;> ite_close

theorem step_writeAfter (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (members : Bool) :
    step M p (enc x) ⟨"writeAfter", [boolV hdr, natV g, natV tg, boolV members]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ members = true then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> cases members <;>
    gl_simp [h1, h2, Holder] <;> ite_close

set_option maxHeartbeats 2000000 in
theorem step_recoverOwned (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat)
    (stopped claimed member ticket pending : Bool) :
    step M p (enc x) ⟨"recoverOwned", [boolV hdr, natV g, natV tg, boolV stopped, boolV claimed, boolV member,
        boolV ticket, boolV pending]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ stopped = true ∧
          (claimed = false ∨ member = true ∧ ticket = true) ∧ pending = false then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> cases stopped <;> cases claimed <;>
    cases member <;> cases ticket <;> cases pending <;> gl_simp [h1, h2, Holder] <;> ite_close

set_option maxHeartbeats 2000000 in
theorem step_recoverLost (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (decision unknown execOk : Bool) :
    step M p (enc x) ⟨"recoverLost", [boolV hdr, natV g, natV tg, boolV decision, boolV unknown, boolV execOk]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ decision = true ∧ unknown = true ∧
          execOk = true then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> cases decision <;> cases unknown <;>
    cases execOk <;> gl_simp [h1, h2, Holder] <;> ite_close

theorem step_startup (p : Params) (x : Gl) :
    step M p (enc x) ⟨"startup", []⟩ = if x.st = 1 then some (enc { x with live := true }) else none := by
  by_cases h : x.st = 1 <;> gl_simp [h]

/-- The standing check of generation `g`. -/
def Standing (x : Gl) (g : Nat) : Prop := x.st = 1 ∧ x.gen = g

instance (x : Gl) (g : Nat) : Decidable (Standing x g) := by unfold Standing; infer_instance

theorem step_standing (p : Params) (x : Gl) (g : Nat) :
    step M p (enc x) ⟨"standing", [natV g]⟩ = if g ≤ p "maxGen" ∧ Standing x g then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> gl_simp [h1, Standing] <;> ite_close

theorem step_expire (p : Params) (x : Gl) :
    step M p (enc x) ⟨"expire", []⟩ = if x.live = true then some (enc { x with live := false }) else none := by
  cases hl : x.live <;> gl_simp [hl]

set_option maxHeartbeats 2000000 in
theorem step_claim (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sameExec sameClient : Bool)
    (expected : Nat) (room decision : Bool) :
    step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV sameExec, boolV sameClient, natV expected,
        boolV room, boolV decision]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ expected ≤ p "maxGen" ∧
          (if sameExec = true then sameClient = true ∧ Holder x hdr g tg ∧ x.gen ≤ p "maxGen"
           else ¬Valid x ∧ expected = x.gen ∧ room = true ∧ (x.st = 0 ∨ decision = true) ∧
             x.gen + 1 ≤ p "maxGen") then
        some (enc ⟨1, if sameExec = true then x.gen else x.gen + 1, true⟩) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h3 : expected ≤ p "maxGen" <;>
    cases sameExec <;> cases sameClient <;> cases room <;> cases decision <;> gl_simp [h1, h2, h3, Holder, Valid] <;>
    ite_close

end KanderFormal.Proofs.ServerGroupLease
