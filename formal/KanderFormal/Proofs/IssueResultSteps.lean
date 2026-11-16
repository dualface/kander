import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.IssueResult

/-
The step lemmas of the issue result machine: each event on an encoded state
evaluates to an explicit `if`, on which `IssueResult.lean` proves the theorems.
-/
namespace KanderFormal.Proofs.IssueResult

open KanderFormal KanderFormal.Machines.IssueResult KanderFormal.Proofs.Scalar

abbrev M := issueResultMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `st`: 0 none, 1 uncertain, 2 published,
3 equivalent, 4 rejected, 5 other. -/
structure Rq where
  st : Nat
  unc : Nat
  iid : Bool
  full : Bool
  met : Bool
  cpos : Bool
  nf : Nat
  nd : Nat

def enc (x : Rq) : State :=
  [("st", enumV x.st), ("unc", natV x.unc), ("iid", boolV x.iid), ("full", boolV x.full), ("met", boolV x.met),
    ("cpos", boolV x.cpos), ("nf", natV x.nf), ("nd", natV x.nd)]

theorem enc_inj {x y : Rq} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _, _, _, _⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.st < 6 ∧ x.unc ≤ p "maxN" ∧ x.nf ≤ p "maxN" ∧ x.nd ≤ p "maxN" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5), (_, v6), (_, v7), (_, v8)], hn =>
    simp [issueResultMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "st" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "unc" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "iid" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "full" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "met" v5 (by simp [lookup])
    obtain ⟨t6, ht6, c6⟩ := hc "cpos" v6 (by simp [lookup])
    obtain ⟨t7, ht7, c7⟩ := hc "nf" v7 (by simp [lookup])
    obtain ⟨t8, ht8, c8⟩ := hc "nd" v8 (by simp [lookup])
    cases (rfl : M.fieldTy "st" = some (.scalar (.enum "rstat"))).symm.trans ht1
    cases (rfl : M.fieldTy "unc" = some (.scalar (.nat "maxN"))).symm.trans ht2
    cases (rfl : M.fieldTy "iid" = some (.scalar .bool)).symm.trans ht3
    cases (rfl : M.fieldTy "full" = some (.scalar .bool)).symm.trans ht4
    cases (rfl : M.fieldTy "met" = some (.scalar .bool)).symm.trans ht5
    cases (rfl : M.fieldTy "cpos" = some (.scalar .bool)).symm.trans ht6
    cases (rfl : M.fieldTy "nf" = some (.scalar (.nat "maxN"))).symm.trans ht7
    cases (rfl : M.fieldTy "nd" = some (.scalar (.nat "maxN"))).symm.trans ht8
    obtain ⟨st, rfl, hst⟩ := conforms_enum (rfl : M.enumSize "rstat" = some 6) c1
    obtain ⟨un, rfl, hun⟩ := conforms_nat c2
    obtain ⟨ii, rfl⟩ := conforms_bool c3
    obtain ⟨fu, rfl⟩ := conforms_bool c4
    obtain ⟨me, rfl⟩ := conforms_bool c5
    obtain ⟨cp, rfl⟩ := conforms_bool c6
    obtain ⟨nf, rfl, hnf⟩ := conforms_nat c7
    obtain ⟨nd, rfl, hnd⟩ := conforms_nat c8
    exact ⟨⟨st, un, ii, fu, me, cp, nf, nd⟩, rfl, hst, hun, hnf, hnd⟩

/-! ## Events on an encoded state -/

theorem find_read : M.findEvent "read" = some readEvent := rfl
theorem find_reconcile : M.findEvent "reconcile" = some reconcileEvent := rfl
theorem find_reassess : M.findEvent "reassess" = some reassessEvent := rfl
theorem find_intent : M.findEvent "intent" = some intentEvent := rfl
theorem find_equivalent : M.findEvent "equivalent" = some equivalentEvent := rfl
theorem find_publish : M.findEvent "publish" = some publishEvent := rfl
theorem find_reject : M.findEvent "reject" = some rejectEvent := rfl
theorem find_decide : M.findEvent "decide" = some decideEvent := rfl
theorem find_keep : M.findEvent "keep" = some keepEvent := rfl
theorem find_failClose : M.findEvent "failClose" = some failCloseEvent := rfl
theorem find_otherIntent : M.findEvent "otherIntent" = some otherIntentEvent := rfl
theorem find_otherSettle : M.findEvent "otherSettle" = some otherSettleEvent := rfl
theorem find_update : M.findEvent "update" = some updateEvent := rfl
theorem find_work : M.findEvent "work" = some workEvent := rfl
theorem find_idKeep : M.findEvent "idKeep" = some idKeepEvent := rfl
theorem find_valid : M.findEvent "valid" = some validEvent := rfl
theorem find_issueNeeded : M.findEvent "issueNeeded" = some issueNeededEvent := rfl
theorem ctors_rstat :
    M.ctorsOf "rstat" = some ["none", "uncertain", "published", "equivalent", "rejected", "other"] := rfl
theorem ctors_scan : M.ctorsOf "scan" = some ["open", "uncertain", "refusal", "consent", "returned"] := rfl
theorem size_rstat : M.enumSize "rstat" = some 6 := rfl
theorem size_scan : M.enumSize "scan" = some 5 := rfl
theorem ty_st : M.fieldTy "st" = some (.scalar (.enum "rstat")) := rfl
theorem ty_unc : M.fieldTy "unc" = some (.scalar (.nat "maxN")) := rfl
theorem ty_iid : M.fieldTy "iid" = some (.scalar .bool) := rfl
theorem ty_full : M.fieldTy "full" = some (.scalar .bool) := rfl
theorem ty_met : M.fieldTy "met" = some (.scalar .bool) := rfl
theorem ty_cpos : M.fieldTy "cpos" = some (.scalar .bool) := rfl
theorem ty_nf : M.fieldTy "nf" = some (.scalar (.nat "maxN")) := rfl
theorem ty_nd : M.fieldTy "nd" = some (.scalar (.nat "maxN")) := rfl

macro "ir_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_read, find_reconcile, find_reassess, find_intent, find_equivalent, find_publish,
      find_reject, find_decide, find_keep, find_failClose, find_otherIntent, find_otherSettle, find_update,
      find_work, find_idKeep, find_valid, find_issueNeeded, ctors_rstat, ctors_scan, size_rstat, size_scan, ty_st,
      ty_unc, ty_iid, ty_full, ty_met, ty_cpos, ty_nf, ty_nd, lookup, readEvent, reconcileEvent, reassessEvent,
      intentEvent, equivalentEvent, publishEvent, rejectEvent, decideEvent, keepEvent, failCloseEvent,
      otherIntentEvent, otherSettleEvent, updateEvent, workEvent, idKeepEvent, validEvent, issueNeededEvent,
      pubOrEq, fresh, kept, gate, stIs, stC, newIs, vstIs, scanIs, boolArg, numArg, rstatArg, scanArg, bindArgs,
      Ty.conforms, STy.conforms, eval, evalCmp, natAdd, natSub, boolAnd, boolOr, boolNot, indexOf, enumV, boolV,
      natV, f, a, tt, ff, andE, orE, notE, eq, ne, le, zero, one, inc, dec, evalAssigns, applyUpdates, enc,
      or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_read (p : Params) (x : Rq) (remoteOk same : Bool) :
    step M p (enc x) ⟨"read", [boolV remoteOk, boolV same]⟩ =
      if remoteOk = true ∧ (x.iid = false ∨ same = true) then some (enc { x with iid := true }) else none := by
  cases remoteOk <;> cases same <;> cases hi : x.iid <;> ir_simp [hi] <;> ite_close

theorem step_reconcile (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") (found : Bool) :
    step M p (enc x) ⟨"reconcile", [boolV found]⟩ =
      if x.st = 1 ∧ found = true ∧ 1 ≤ x.unc then
        some (enc { x with st := 2, cpos := true, unc := x.unc - 1 }) else none := by
  by_cases hu : 1 ≤ x.unc <;> cases found <;> ir_simp [hu, hb] <;> ite_close

theorem step_reassess (p : Params) (x : Rq) :
    step M p (enc x) ⟨"reassess", []⟩ = if x.unc = 0 ∧ (x.st = 2 ∨ x.st = 3) then some (enc x) else none := by
  ir_simp [] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_intent (p : Params) (x : Rq) (nfull nmet dup bodyOk : Bool) :
    step M p (enc x) ⟨"intent", [boolV nfull, boolV nmet, boolV dup, boolV bodyOk]⟩ =
      if x.unc + 1 ≤ p "maxN" ∧ x.iid = true ∧ x.unc = 0 ∧ (x.st = 0 ∨ x.st = 4) ∧ dup = false ∧
          bodyOk = true ∧ (nfull = false ∨ nmet = true) then
        some (enc { x with st := 1, unc := x.unc + 1, full := nfull, met := nmet, cpos := false, nd := 0 })
      else none := by
  by_cases h1 : x.unc + 1 ≤ p "maxN" <;> cases nfull <;> cases nmet <;> cases dup <;> cases bodyOk <;>
    cases hi : x.iid <;> ir_simp [h1, hi] <;> ite_close

theorem step_equivalent (p : Params) (x : Rq) (nfull nmet found : Bool) :
    step M p (enc x) ⟨"equivalent", [boolV nfull, boolV nmet, boolV found]⟩ =
      if x.iid = true ∧ x.unc = 0 ∧ (x.st = 0 ∨ x.st = 4) ∧ found = true ∧ (nfull = false ∨ nmet = true) then
        some (enc { x with st := 3, cpos := true, full := nfull, met := nmet, nd := 0 }) else none := by
  cases nfull <;> cases nmet <;> cases found <;> cases hi : x.iid <;> ir_simp [hi] <;> ite_close

theorem step_publish (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") (ok : Bool) :
    step M p (enc x) ⟨"publish", [boolV ok]⟩ =
      if x.st = 1 ∧ ok = true ∧ 1 ≤ x.unc then
        some (enc { x with st := 2, cpos := true, unc := x.unc - 1 }) else none := by
  by_cases hu : 1 ≤ x.unc <;> cases ok <;> ir_simp [hu, hb] <;> ite_close

theorem step_reject (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") :
    step M p (enc x) ⟨"reject", []⟩ =
      if x.nf + 1 ≤ p "maxN" ∧ x.st = 1 ∧ 1 ≤ x.unc then
        some (enc { x with st := 4, unc := x.unc - 1, nf := x.nf + 1 }) else none := by
  by_cases h1 : x.nf + 1 ≤ p "maxN" <;> by_cases hu : 1 ≤ x.unc <;> ir_simp [h1, hu, hb] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_decide (p : Params) (x : Rq) (digestOk closed : Bool) (scan : Nat) (fieldsOk : Bool) :
    step M p (enc x) ⟨"decide", [boolV digestOk, boolV closed, enumV scan, boolV fieldsOk]⟩ =
      if scan < 5 ∧ x.nd + 1 ≤ p "maxN" ∧ (x.st = 2 ∨ x.st = 3) ∧ digestOk = true ∧ x.full = true ∧
          closed = false ∧ scan = 0 ∧ fieldsOk = true then some (enc { x with nd := x.nd + 1 }) else none := by
  by_cases h1 : scan < 5 <;> by_cases h2 : x.nd + 1 ≤ p "maxN" <;> cases digestOk <;> cases closed <;>
    cases fieldsOk <;> cases hf : x.full <;> ir_simp [h1, h2, hf] <;> ite_close

set_option maxHeartbeats 8000000 in
theorem step_keep (p : Params) (x : Rq) (digestOk closed : Bool) (scan : Nat) :
    step M p (enc x) ⟨"keep", [boolV digestOk, boolV closed, enumV scan]⟩ =
      if scan < 5 ∧ (x.st = 2 ∨ x.st = 3) ∧ digestOk = true ∧ x.full = true ∧ (closed = true ∨ scan = 4) then
        some (enc x) else none := by
  by_cases h1 : scan < 5 <;> cases digestOk <;> cases closed <;> cases hf : x.full <;> ir_simp [h1, hf] <;>
    ite_close

theorem step_failClose (p : Params) (x : Rq) :
    step M p (enc x) ⟨"failClose", []⟩ =
      if x.nf + 1 ≤ p "maxN" ∧ (x.st = 2 ∨ x.st = 3) then some (enc { x with nf := x.nf + 1 }) else none := by
  by_cases h1 : x.nf + 1 ≤ p "maxN" <;> ir_simp [h1] <;> ite_close

theorem step_otherIntent (p : Params) (x : Rq) :
    step M p (enc x) ⟨"otherIntent", []⟩ =
      if x.unc + 1 ≤ p "maxN" ∧ x.unc = 0 then some (enc { x with unc := x.unc + 1 }) else none := by
  by_cases h1 : x.unc + 1 ≤ p "maxN" <;> ir_simp [h1] <;> ite_close

theorem step_otherSettle (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") :
    step M p (enc x) ⟨"otherSettle", []⟩ =
      if x.unc = 1 ∧ x.st ≠ 1 then some (enc { x with unc := x.unc - 1 }) else none := by
  by_cases hu : 1 ≤ x.unc <;> ir_simp [hu, hb] <;> ite_close

set_option maxHeartbeats 16000000 in
theorem step_update (p : Params) (x : Rq) (present : Bool) (newSt : Nat) (identSame : Bool) (nfNew : Nat)
    (prefixF : Bool) (ndNew : Nat) :
    step M p (enc x) ⟨"update", [boolV present, enumV newSt, boolV identSame, natV nfNew, boolV prefixF,
        natV ndNew]⟩ =
      if newSt < 6 ∧ nfNew ≤ p "maxN" ∧ ndNew ≤ p "maxN" ∧ present = true ∧
          (x.st = 4 ∨ ((x.st = 1 ∨ newSt = x.st) ∧ identSame = true)) ∧ x.nf ≤ nfNew ∧ prefixF = true ∧
          x.nd ≤ ndNew then some (enc x) else none := by
  by_cases h1 : newSt < 6 <;> by_cases h2 : nfNew ≤ p "maxN" <;> by_cases h3 : ndNew ≤ p "maxN" <;>
    cases present <;> cases identSame <;> cases prefixF <;> ir_simp [h1, h2, h3] <;> ite_close

set_option maxHeartbeats 16000000 in
theorem step_work (p : Params) (x : Rq) (newSt : Nat) (identical checksSame workSame versionOk : Bool) :
    step M p (enc x) ⟨"work", [enumV newSt, boolV identical, boolV checksSame, boolV workSame, boolV versionOk]⟩ =
      if newSt < 6 ∧ ((x.st = 4 ∧ identical = true) ∨ ((x.st = 1 ∨ x.st = 2 ∨ x.st = 3) ∧ checksSame = true) ∨
          ((x.st = 0 ∨ x.st = 4) ∧ (newSt = 1 ∨ newSt = 3) ∧ x.unc = 0 ∧ workSame = true ∧ versionOk = true))
      then some (enc x) else none := by
  by_cases h1 : newSt < 6 <;> cases identical <;> cases checksSame <;> cases workSame <;> cases versionOk <;>
    ir_simp [h1] <;> ite_close

theorem step_idKeep (p : Params) (x : Rq) (same : Bool) :
    step M p (enc x) ⟨"idKeep", [boolV same]⟩ =
      if x.iid = false ∨ same = true then some (enc x) else none := by
  cases same <;> cases hi : x.iid <;> ir_simp [hi] <;> ite_close

set_option maxHeartbeats 16000000 in
theorem step_valid (p : Params) (x : Rq) (vst : Nat) (actorPos commentPos outOk nfull nmet : Bool) :
    step M p (enc x) ⟨"valid", [enumV vst, boolV actorPos, boolV commentPos, boolV outOk, boolV nfull,
        boolV nmet]⟩ =
      if vst < 6 ∧ (vst = 1 ∨ vst = 2 ∨ vst = 3 ∨ vst = 4) ∧ actorPos = true ∧
          ((vst ≠ 2 ∧ vst ≠ 3) ∨ commentPos = true) ∧ outOk = true ∧ (nfull = false ∨ nmet = true)
      then some (enc x) else none := by
  by_cases h1 : vst < 6 <;> cases actorPos <;> cases commentPos <;> cases outOk <;> cases nfull <;>
    cases nmet <;> ir_simp [h1] <;> ite_close

theorem step_issueNeeded (p : Params) (x : Rq) (hasRecords idPos : Bool) :
    step M p (enc x) ⟨"issueNeeded", [boolV hasRecords, boolV idPos]⟩ =
      if hasRecords = false ∨ idPos = true then some (enc x) else none := by
  cases hasRecords <;> cases idPos <;> ir_simp [] <;> ite_close

end KanderFormal.Proofs.IssueResult
