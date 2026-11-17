import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ServerIntegration

/-
The step lemmas of the server integration machine: each event on an encoded
state evaluates to an explicit `if`, on which `ServerIntegration.lean` proves the
theorems.
-/
namespace KanderFormal.Proofs.ServerIntegration

open KanderFormal KanderFormal.Machines.ServerIntegration KanderFormal.Proofs.Scalar

abbrev M := serverIntegrationMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `st`: 0 none, 1 active, 2 lost,
3 released. `ph`: 0 none, 1 reserved, 2 pushing, 3 integrated, 4 released,
5 uncertain. `pu`: 0 none, 1 initialize, 2 assemble, 3 deliver. -/
structure It where
  st : Nat
  gen : Nat
  rev : Nat
  live : Bool
  ph : Nat
  pu : Nat
  ct : Bool
  au : Nat
  ob : Bool

def enc (x : It) : State :=
  [("status", enumV x.st), ("gen", natV x.gen), ("rev", natV x.rev), ("live", boolV x.live),
    ("phase", enumV x.ph), ("purpose", enumV x.pu), ("contained", boolV x.ct), ("auth", natV x.au),
    ("obs", boolV x.ob)]

theorem enc_inj {x y : It} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _, _, _, _, _⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.st < 4 ∧ x.gen ≤ p "maxGen" ∧ x.rev ≤ p "maxRev" ∧ x.ph < 6 ∧ x.pu < 4 ∧
      x.au ≤ p "maxAuth" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5), (_, v6), (_, v7), (_, v8), (_, v9)], hn =>
    simp [serverIntegrationMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "status" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "gen" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "rev" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "live" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "phase" v5 (by simp [lookup])
    obtain ⟨t6, ht6, c6⟩ := hc "purpose" v6 (by simp [lookup])
    obtain ⟨t7, ht7, c7⟩ := hc "contained" v7 (by simp [lookup])
    obtain ⟨t8, ht8, c8⟩ := hc "auth" v8 (by simp [lookup])
    obtain ⟨t9, ht9, c9⟩ := hc "obs" v9 (by simp [lookup])
    cases (rfl : M.fieldTy "status" = some (.scalar (.enum "status"))).symm.trans ht1
    cases (rfl : M.fieldTy "gen" = some (.scalar (.nat "maxGen"))).symm.trans ht2
    cases (rfl : M.fieldTy "rev" = some (.scalar (.nat "maxRev"))).symm.trans ht3
    cases (rfl : M.fieldTy "live" = some (.scalar .bool)).symm.trans ht4
    cases (rfl : M.fieldTy "phase" = some (.scalar (.enum "phase"))).symm.trans ht5
    cases (rfl : M.fieldTy "purpose" = some (.scalar (.enum "purpose"))).symm.trans ht6
    cases (rfl : M.fieldTy "contained" = some (.scalar .bool)).symm.trans ht7
    cases (rfl : M.fieldTy "auth" = some (.scalar (.nat "maxAuth"))).symm.trans ht8
    cases (rfl : M.fieldTy "obs" = some (.scalar .bool)).symm.trans ht9
    obtain ⟨st, rfl, hst⟩ := conforms_enum (rfl : M.enumSize "status" = some 4) c1
    obtain ⟨g, rfl, hg⟩ := conforms_nat c2
    obtain ⟨rv, rfl, hrv⟩ := conforms_nat c3
    obtain ⟨lv, rfl⟩ := conforms_bool c4
    obtain ⟨ph, rfl, hph⟩ := conforms_enum (rfl : M.enumSize "phase" = some 6) c5
    obtain ⟨pu, rfl, hpu⟩ := conforms_enum (rfl : M.enumSize "purpose" = some 4) c6
    obtain ⟨ct, rfl⟩ := conforms_bool c7
    obtain ⟨au, rfl, hau⟩ := conforms_nat c8
    obtain ⟨ob, rfl⟩ := conforms_bool c9
    exact ⟨⟨st, g, rv, lv, ph, pu, ct, au, ob⟩, rfl, hst, hg, hrv, hph, hpu, hau⟩

/-! ## Events on an encoded state -/

theorem find_claim : M.findEvent "claim" = some claimEvent := rfl
theorem find_claimGuard : M.findEvent "claimGuard" = some claimGuardEvent := rfl
theorem find_require : M.findEvent "require" = some requireEvent := rfl
theorem find_requireOwner : M.findEvent "requireOwner" = some requireOwnerEvent := rfl
theorem find_renew : M.findEvent "renew" = some renewEvent := rfl
theorem find_check : M.findEvent "check" = some checkEvent := rfl
theorem find_checkOwner : M.findEvent "checkOwner" = some checkOwnerEvent := rfl
theorem find_pushing : M.findEvent "pushing" = some pushingEvent := rfl
theorem find_confirm : M.findEvent "confirm" = some confirmEvent := rfl
theorem find_release : M.findEvent "release" = some releaseEvent := rfl
theorem find_guard : M.findEvent "guard" = some guardEvent := rfl
theorem find_guardRelease : M.findEvent "guardRelease" = some guardReleaseEvent := rfl
theorem find_completion : M.findEvent "completion" = some completionEvent := rfl
theorem find_wrapUp : M.findEvent "wrapUp" = some wrapUpEvent := rfl
theorem find_expire : M.findEvent "expire" = some expireEvent := rfl
theorem ctors_status : M.ctorsOf "status" = some ["none", "active", "lost", "released"] := rfl
theorem ctors_phase :
    M.ctorsOf "phase" = some ["none", "reserved", "pushing", "integrated", "released", "uncertain"] := rfl
theorem ctors_purpose : M.ctorsOf "purpose" = some ["none", "initialize", "assemble", "deliver"] := rfl
theorem size_status : M.enumSize "status" = some 4 := rfl
theorem size_phase : M.enumSize "phase" = some 6 := rfl
theorem size_purpose : M.enumSize "purpose" = some 4 := rfl
theorem ty_status : M.fieldTy "status" = some (.scalar (.enum "status")) := rfl
theorem ty_gen : M.fieldTy "gen" = some (.scalar (.nat "maxGen")) := rfl
theorem ty_rev : M.fieldTy "rev" = some (.scalar (.nat "maxRev")) := rfl
theorem ty_live : M.fieldTy "live" = some (.scalar .bool) := rfl
theorem ty_phase : M.fieldTy "phase" = some (.scalar (.enum "phase")) := rfl
theorem ty_purpose : M.fieldTy "purpose" = some (.scalar (.enum "purpose")) := rfl
theorem ty_contained : M.fieldTy "contained" = some (.scalar .bool) := rfl
theorem ty_auth : M.fieldTy "auth" = some (.scalar (.nat "maxAuth")) := rfl
theorem ty_obs : M.fieldTy "obs" = some (.scalar .bool) := rfl

macro "it_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_claim, find_claimGuard, find_require, find_requireOwner, find_renew, find_check,
      find_checkOwner, find_pushing, find_confirm, find_release, find_guard, find_guardRelease, find_completion, find_wrapUp,
      find_expire, ctors_status, ctors_phase, ctors_purpose, size_status, size_phase, size_purpose, ty_status,
      ty_gen, ty_rev, ty_live, ty_phase, ty_purpose, ty_contained, ty_auth, ty_obs, lookup, claimEvent,
      claimGuardEvent, requireEvent, requireOwnerEvent, renewEvent, checkEvent, checkOwnerEvent, pushingEvent,
      confirmEvent, releaseEvent, guardEvent, guardReleaseEvent, completionEvent, wrapUpEvent, expireEvent, valid, owner,
      actor, lease, revOk, genArg, revArg, authArg, boolArg, purposeArg, ownerArgs, actorArgs, leaseArgs,
      actionArgs, statusIs, phaseIs, purposeIs, gen, rev, auth, inc, zero, eq, ne, bindArgs, Ty.conforms,
      STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr, boolNot, indexOf, enumV, boolV, natV, f, a, tt, ff,
      andE, orE, notE, evalAssigns, applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc,
      map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

/-- `requireIntegrationOwner` accepts. -/
def Owner (x : It) (hdr : Bool) (g tg : Nat) (client : Bool) : Prop :=
  hdr = true ∧ x.st = 1 ∧ g = x.gen ∧ x.live = true ∧ client = true ∧ tg = x.gen

/-- `currentIntegrationActor` accepts. -/
def Actor (x : It) (al : Bool) (ag : Nat) (ae ac : Bool) : Prop :=
  al = true ∧ ag = x.au ∧ ae = true ∧ ac = true

instance (x : It) (h : Bool) (g tg : Nat) (c : Bool) : Decidable (Owner x h g tg c) := by
  unfold Owner; infer_instance

instance (x : It) (al : Bool) (ag : Nat) (ae ac : Bool) : Decidable (Actor x al ag ae ac) := by
  unfold Actor; infer_instance

theorem step_requireOwner (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client : Bool) :
    step M p (enc x) ⟨"requireOwner", [boolV hdr, natV g, natV tg, boolV client]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Owner x hdr g tg client then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> it_simp [h1, h2, Owner] <;> ite_close

theorem step_require (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) :
    step M p (enc x) ⟨"require", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
        boolV ac]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧ Actor x al ag ae ac then
        some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h3 : ag ≤ p "maxAuth" <;>
    it_simp [h1, h2, h3, Owner, Actor] <;> ite_close

/-- A valid lease: an active record that has not expired. -/
def Valid (x : It) : Prop := x.st = 1 ∧ x.live = true

instance (x : It) : Decidable (Valid x) := by unfold Valid; infer_instance

theorem step_renew (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac facts : Bool) :
    step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae, boolV ac,
        boolV facts]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧ Actor x al ag ae ac ∧
          facts = true then some (enc { x with live := true }) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h3 : ag ≤ p "maxAuth" <;>
    it_simp [h1, h2, h3, Owner, Actor] <;> ite_close

theorem step_check (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (room : Bool) :
    step M p (enc x) ⟨"check", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae, boolV ac,
        natV er, boolV room]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ er ≤ p "maxRev" ∧ Owner x hdr g tg client ∧
          Actor x al ag ae ac ∧ er = x.rev ∧ er ≠ 0 ∧ room = true then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h3 : ag ≤ p "maxAuth" <;>
    by_cases h4 : er ≤ p "maxRev" <;> it_simp [h1, h2, h3, h4, Owner, Actor] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_checkOwner (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client : Bool) (er : Nat) (room : Bool) :
    step M p (enc x) ⟨"checkOwner", [boolV hdr, natV g, natV tg, boolV client, natV er, boolV room]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ er ≤ p "maxRev" ∧ Owner x hdr g tg client ∧ er = x.rev ∧ er ≠ 0 ∧
          room = true then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h4 : er ≤ p "maxRev" <;>
    it_simp [h1, h2, h4, Owner] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_pushing (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (room obsMatch obsSha : Bool) :
    step M p (enc x) ⟨"pushing", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae, boolV ac,
        natV er, boolV room, boolV obsMatch, boolV obsSha]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ er ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
          Owner x hdr g tg client ∧ Actor x al ag ae ac ∧ er = x.rev ∧ er ≠ 0 ∧ room = true ∧ x.ct = false ∧
          x.ph = 1 ∧ obsMatch = true then
        some (enc { x with ph := 2, rev := x.rev + 1, ob := obsSha }) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h3 : ag ≤ p "maxAuth" <;>
    by_cases h4 : er ≤ p "maxRev" <;> by_cases h5 : x.rev + 1 ≤ p "maxRev" <;>
    it_simp [h1, h2, h3, h4, h5, Owner, Actor] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_confirm (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (room obsSha obsAt edgesOk : Bool) :
    step M p (enc x) ⟨"confirm", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae, boolV ac,
        natV er, boolV room, boolV obsSha, boolV obsAt, boolV edgesOk]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ er ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
          Owner x hdr g tg client ∧ Actor x al ag ae ac ∧ er = x.rev ∧ er ≠ 0 ∧ room = true ∧
          ((x.ph = 1 ∧ x.ct = true) ∨ x.ph = 2) ∧ obsSha = true ∧ obsAt = true ∧ edgesOk = true then
        some (enc { x with st := 3, ph := 3, live := false, rev := x.rev + 1, ob := true }) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h3 : ag ≤ p "maxAuth" <;>
    by_cases h4 : er ≤ p "maxRev" <;> by_cases h5 : x.rev + 1 ≤ p "maxRev" <;>
    it_simp [h1, h2, h3, h4, h5, Owner, Actor] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_release (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client : Bool) (er : Nat)
    (room decision obsEmpty obsSha : Bool) :
    step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg, boolV client, natV er, boolV room, boolV decision,
        boolV obsEmpty, boolV obsSha]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ er ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧ Owner x hdr g tg client ∧
          er = x.rev ∧ er ≠ 0 ∧ room = true ∧ decision = true ∧ (obsEmpty = true ∨ obsSha = true) then
        some (enc ⟨3, x.gen, x.rev + 1, false, (if x.ph = 2 then 5 else 4), x.pu, x.ct, x.au, obsSha⟩)
      else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h4 : er ≤ p "maxRev" <;>
    by_cases h5 : x.rev + 1 ≤ p "maxRev" <;> by_cases h6 : x.ph = 2 <;> it_simp [h1, h2, h4, h5, h6, Owner] <;>
    ite_close

theorem step_guard (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac ticket facts : Bool) :
    step M p (enc x) ⟨"guard", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae, boolV ac,
        boolV ticket, boolV facts]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧ Actor x al ag ae ac ∧
          ticket = true ∧ facts = true then some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h3 : ag ≤ p "maxAuth" <;>
    it_simp [h1, h2, h3, Owner, Actor] <;> ite_close

theorem step_guardRelease (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client ticket : Bool) :
    step M p (enc x) ⟨"guardRelease", [boolV hdr, natV g, natV tg, boolV client, boolV ticket]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Owner x hdr g tg client ∧ ticket = true then some (enc x)
      else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> it_simp [h1, h2, Owner] <;> ite_close

theorem step_claimGuard (p : Params) (x : It) (ticket actor facts : Bool) :
    step M p (enc x) ⟨"claimGuard", [boolV ticket, boolV actor, boolV facts]⟩ =
      if ticket = true ∧ actor = true ∧ facts = true then some (enc x) else none := by
  it_simp [] <;> ite_close

theorem step_expire (p : Params) (x : It) :
    step M p (enc x) ⟨"expire", []⟩ = if x.live = true then some (enc { x with live := false }) else none := by
  cases hl : x.live <;> it_simp [hl]

set_option maxHeartbeats 8000000 in
theorem step_claim (p : Params) (x : It) (eg er : Nat) (roomGen roomRev sameExec actor actorMatch : Bool)
    (ag pu : Nat) (contained : Bool) :
    step M p (enc x) ⟨"claim", [natV eg, natV er, boolV roomGen, boolV roomRev, boolV sameExec, boolV actor,
        boolV actorMatch, natV ag, enumV pu, boolV contained]⟩ =
      if eg ≤ p "maxGen" ∧ er ≤ p "maxRev" ∧ ag ≤ p "maxAuth" ∧ pu < 4 ∧ x.gen + 1 ≤ p "maxGen" ∧
          x.rev + 1 ≤ p "maxRev" ∧ ¬Valid x ∧ eg = x.gen ∧ er = x.rev ∧ roomGen = true ∧ roomRev = true ∧
          sameExec = false ∧ actor = true ∧ actorMatch = true ∧ pu ≠ 0 then
        some (enc ⟨1, x.gen + 1, x.rev + 1, true, 1, pu, contained, ag, false⟩) else none := by
  by_cases h1 : eg ≤ p "maxGen" <;> by_cases h2 : er ≤ p "maxRev" <;> by_cases h3 : ag ≤ p "maxAuth" <;>
    by_cases h4 : pu < 4 <;> by_cases h5 : x.gen + 1 ≤ p "maxGen" <;> by_cases h6 : x.rev + 1 ≤ p "maxRev" <;>
    it_simp [h1, h2, h3, h4, h5, h6, Valid] <;> ite_close

theorem step_completion (p : Params) (x : It) (closed planNA noDelivery member planIsCommit : Bool) :
    step M p (enc x) ⟨"completion", [boolV closed, boolV planNA, boolV noDelivery, boolV member,
        boolV planIsCommit]⟩ =
      if (closed = true ∧ planNA = true ∧ noDelivery = true) ∨
          (x.ph = 3 ∧ x.st = 3 ∧ x.pu = 3 ∧ x.ob = true ∧ member = true ∧ closed = true ∧
            planIsCommit = true) then some (enc x) else none := by
  it_simp [] <;> ite_close

theorem step_wrapUp (p : Params) (x : It) (notRequired idGiven idMatch : Bool) (ig : Nat)
    (srcMatch tgtMatch cwdMatch member closed planIsSrc reviewIsSrc : Bool) :
    step M p (enc x) ⟨"wrapUp", [boolV notRequired, boolV idGiven, boolV idMatch, natV ig, boolV srcMatch,
        boolV tgtMatch, boolV cwdMatch, boolV member, boolV closed, boolV planIsSrc, boolV reviewIsSrc]⟩ =
      if ig ≤ p "maxGen" ∧ (notRequired = true ∨
          (idGiven = true ∧ ig ≠ 0 ∧ idMatch = true ∧ x.ph = 3 ∧ x.st = 3 ∧ ig = x.gen ∧ x.pu = 3 ∧
            srcMatch = true ∧ tgtMatch = true ∧ cwdMatch = true ∧ member = true ∧ closed = true ∧
            planIsSrc = true ∧ reviewIsSrc = true)) then some (enc x) else none := by
  by_cases h1 : ig ≤ p "maxGen" <;> it_simp [h1] <;> ite_close

end KanderFormal.Proofs.ServerIntegration
