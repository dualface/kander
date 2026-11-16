import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ServerDelivery

/-
The step lemmas of the server delivery machine: each event on an encoded state
evaluates to an explicit `if`, on which `ServerDelivery.lean` proves the
theorems.
-/
namespace KanderFormal.Proofs.ServerDelivery

open KanderFormal KanderFormal.Machines.ServerDelivery KanderFormal.Proofs.Scalar

abbrev M := serverDeliveryMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `pur`: 0 none, 1 delivery, 2 baseline.
`latest`: 0 none, 1 here, 2 other. -/
structure Dl where
  pur : Nat
  rgen : Nat
  rrev : Nat
  latest : Nat

def enc (x : Dl) : State :=
  [("rec", enumV x.pur), ("rgen", natV x.rgen), ("rrev", natV x.rrev), ("latest", enumV x.latest)]

theorem enc_inj {x y : Dl} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _⟩ := x
  obtain ⟨_, _, _, _⟩ := y
  simp [enc, enumV, natV] at h
  obtain ⟨rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.pur < 3 ∧ x.rgen ≤ p "maxGen" ∧ x.rrev ≤ p "maxRev" ∧ x.latest < 3 := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4)], hn =>
    simp [serverDeliveryMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "rec" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "rgen" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "rrev" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "latest" v4 (by simp [lookup])
    cases (rfl : M.fieldTy "rec" = some (.scalar (.enum "rec"))).symm.trans ht1
    cases (rfl : M.fieldTy "rgen" = some (.scalar (.nat "maxGen"))).symm.trans ht2
    cases (rfl : M.fieldTy "rrev" = some (.scalar (.nat "maxRev"))).symm.trans ht3
    cases (rfl : M.fieldTy "latest" = some (.scalar (.enum "latest"))).symm.trans ht4
    obtain ⟨r, rfl, hr⟩ := conforms_enum (rfl : M.enumSize "rec" = some 3) c1
    obtain ⟨g, rfl, hg⟩ := conforms_nat c2
    obtain ⟨v, rfl, hv⟩ := conforms_nat c3
    obtain ⟨l, rfl, hl⟩ := conforms_enum (rfl : M.enumSize "latest" = some 3) c4
    exact ⟨⟨r, g, v, l⟩, rfl, hr, hg, hv, hl⟩

/-! ## Events on an encoded state -/

theorem find_publish : M.findEvent "publish" = some publishEvent := rfl
theorem find_other : M.findEvent "other" = some otherEvent := rfl
theorem find_guard : M.findEvent "guard" = some guardEvent := rfl
theorem ctors_rec : M.ctorsOf "rec" = some ["none", "delivery", "baseline"] := rfl
theorem ctors_latest : M.ctorsOf "latest" = some ["none", "here", "other"] := rfl
theorem size_rec : M.enumSize "rec" = some 3 := rfl
theorem size_latest : M.enumSize "latest" = some 3 := rfl
theorem ty_rec : M.fieldTy "rec" = some (.scalar (.enum "rec")) := rfl
theorem ty_rgen : M.fieldTy "rgen" = some (.scalar (.nat "maxGen")) := rfl
theorem ty_rrev : M.fieldTy "rrev" = some (.scalar (.nat "maxRev")) := rfl
theorem ty_latest : M.fieldTy "latest" = some (.scalar (.enum "latest")) := rfl

macro "dl_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_publish, find_other, find_guard, ctors_rec, ctors_latest, size_rec, size_latest,
      ty_rec, ty_rgen, ty_rrev, ty_latest, lookup, publishEvent, otherEvent, guardEvent, genArg, revArg,
      boolArg, recIs, latestIs, bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr,
      boolNot, indexOf, enumV, boolV, natV, f, a, tt, andE, notE, eq, ne, zero, evalAssigns, applyUpdates, enc,
      or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

set_option maxHeartbeats 4000000 in
theorem step_publish (p : Params) (x : Dl) (holder exec baseline : Bool) (gen cgen rev card : Nat) :
    step M p (enc x) ⟨"publish", [boolV holder, boolV exec, boolV baseline, natV gen, natV cgen, natV rev,
        natV card]⟩ =
      if gen ≤ p "maxGen" ∧ cgen ≤ p "maxGen" ∧ rev ≤ p "maxRev" ∧ card ≤ p "maxRev" ∧
          holder = true ∧ exec = true ∧ gen ≠ 0 ∧ gen = cgen ∧ rev ≠ 0 ∧ rev = card then
        some (enc ⟨if baseline = true then 2 else 1, gen, rev, 1⟩) else none := by
  by_cases h1 : gen ≤ p "maxGen" <;> by_cases h2 : cgen ≤ p "maxGen" <;> by_cases h3 : rev ≤ p "maxRev" <;>
    by_cases h4 : card ≤ p "maxRev" <;> cases baseline <;> dl_simp [h1, h2, h3, h4] <;> ite_close

theorem step_other (p : Params) (x : Dl) :
    step M p (enc x) ⟨"other", []⟩ = some (enc { x with latest := 2 }) := by
  dl_simp []

theorem step_guard (p : Params) (x : Dl) (holder exec : Bool) (cgen card : Nat) :
    step M p (enc x) ⟨"guard", [boolV holder, boolV exec, natV cgen, natV card]⟩ =
      if cgen ≤ p "maxGen" ∧ card ≤ p "maxRev" ∧ holder = true ∧ x.pur = 1 ∧ x.rgen = cgen ∧ exec = true ∧
          x.rrev = card then some (enc x) else none := by
  by_cases h2 : cgen ≤ p "maxGen" <;> by_cases h4 : card ≤ p "maxRev" <;> dl_simp [h2, h4] <;> ite_close

end KanderFormal.Proofs.ServerDelivery
