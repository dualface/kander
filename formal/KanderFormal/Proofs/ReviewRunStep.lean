import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ReviewRun

/-
The review run machine on an encoded state: every event evaluates to an
explicit `if` on the components of the state. The theorems of
`Proofs/ReviewRun.lean` are proved on these forms.
-/
namespace KanderFormal.Proofs.ReviewRun

open KanderFormal KanderFormal.Machines.ReviewRun KanderFormal.Proofs.Scalar

set_option maxHeartbeats 1000000

abbrev M := reviewRunMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `phase`: 0 none, 1 prepared, 2 launching,
3 running, 4 collecting, 5 cleaning, 6 finalized, 7 other. `launch`: 0 not
started, 1 unknown, 2 started, 3 other. `exec`: 0 incomplete, 1 ok, 2 not
started, 3 failed, 4 interrupted, 5 other. -/
structure Rn where
  phase : Nat
  launch : Nat
  exec : Nat
  exitZero : Bool
  prompt : Bool
  contract : Bool
  evidence : Bool
  originals : Bool
  published : Bool
  skipped : Bool

def enc (x : Rn) : State :=
  [("phase", enumV x.phase), ("launch", enumV x.launch), ("exec", enumV x.exec), ("exitZero", boolV x.exitZero),
    ("prompt", boolV x.prompt), ("contract", boolV x.contract), ("evidence", boolV x.evidence),
    ("originals", boolV x.originals), ("published", boolV x.published), ("skipped", boolV x.skipped)]

theorem enc_inj {x y : Rn} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _, _, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _, _, _, _, _, _⟩ := y
  simp [enc, enumV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.phase < 8 ∧ x.launch < 4 ∧ x.exec < 6 := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5), (_, v6), (_, v7), (_, v8), (_, v9), (_, v10)], hn =>
    simp [reviewRunMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "phase" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "launch" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "exec" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "exitZero" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "prompt" v5 (by simp [lookup])
    obtain ⟨t6, ht6, c6⟩ := hc "contract" v6 (by simp [lookup])
    obtain ⟨t7, ht7, c7⟩ := hc "evidence" v7 (by simp [lookup])
    obtain ⟨t8, ht8, c8⟩ := hc "originals" v8 (by simp [lookup])
    obtain ⟨t9, ht9, c9⟩ := hc "published" v9 (by simp [lookup])
    obtain ⟨t10, ht10, c10⟩ := hc "skipped" v10 (by simp [lookup])
    cases (rfl : M.fieldTy "phase" = some (.scalar (.enum "phase"))).symm.trans ht1
    cases (rfl : M.fieldTy "launch" = some (.scalar (.enum "launch"))).symm.trans ht2
    cases (rfl : M.fieldTy "exec" = some (.scalar (.enum "exec"))).symm.trans ht3
    cases (rfl : M.fieldTy "exitZero" = some (.scalar .bool)).symm.trans ht4
    cases (rfl : M.fieldTy "prompt" = some (.scalar .bool)).symm.trans ht5
    cases (rfl : M.fieldTy "contract" = some (.scalar .bool)).symm.trans ht6
    cases (rfl : M.fieldTy "evidence" = some (.scalar .bool)).symm.trans ht7
    cases (rfl : M.fieldTy "originals" = some (.scalar .bool)).symm.trans ht8
    cases (rfl : M.fieldTy "published" = some (.scalar .bool)).symm.trans ht9
    cases (rfl : M.fieldTy "skipped" = some (.scalar .bool)).symm.trans ht10
    obtain ⟨ph, rfl, hph⟩ := conforms_enum (rfl : M.enumSize "phase" = some 8) c1
    obtain ⟨la, rfl, hla⟩ := conforms_enum (rfl : M.enumSize "launch" = some 4) c2
    obtain ⟨ex, rfl, hex⟩ := conforms_enum (rfl : M.enumSize "exec" = some 6) c3
    obtain ⟨b4, rfl⟩ := conforms_bool c4
    obtain ⟨b5, rfl⟩ := conforms_bool c5
    obtain ⟨b6, rfl⟩ := conforms_bool c6
    obtain ⟨b7, rfl⟩ := conforms_bool c7
    obtain ⟨b8, rfl⟩ := conforms_bool c8
    obtain ⟨b9, rfl⟩ := conforms_bool c9
    obtain ⟨b10, rfl⟩ := conforms_bool c10
    exact ⟨⟨ph, la, ex, b4, b5, b6, b7, b8, b9, b10⟩, rfl, hph, hla, hex⟩

/-! ## Events on an encoded state -/

theorem find_create : M.findEvent "create" = some createEvent := rfl
theorem find_retry : M.findEvent "retry" = some retryEvent := rfl
theorem find_update : M.findEvent "update" = some updateEvent := rfl
theorem find_storePrompt : M.findEvent "storePrompt" = some storePromptEvent := rfl
theorem find_storeContract : M.findEvent "storeContract" = some storeContractEvent := rfl
theorem find_storeEvidence : M.findEvent "storeEvidence" = some storeEvidenceEvent := rfl
theorem find_finalize : M.findEvent "finalize" = some finalizeEvent := rfl
theorem find_stage : M.findEvent "stage" = some stageEvent := rfl
theorem find_skip : M.findEvent "skip" = some skipEvent := rfl
theorem find_verify : M.findEvent "verify" = some verifyEvent := rfl
theorem find_passSkipped : M.findEvent "passSkipped" = some passSkippedEvent := rfl
theorem size_phase : M.enumSize "phase" = some 8 := rfl
theorem size_launch : M.enumSize "launch" = some 4 := rfl
theorem size_exec : M.enumSize "exec" = some 6 := rfl
theorem ctors_phase : M.ctorsOf "phase" =
    some ["none", "prepared", "launching", "running", "collecting", "cleaning", "finalized", "other"] := rfl
theorem ctors_launch : M.ctorsOf "launch" = some ["notStarted", "unknown", "started", "other"] := rfl
theorem ctors_exec : M.ctorsOf "exec" = some ["incomplete", "ok", "notStarted", "failed", "interrupted", "other"] := rfl

theorem ty_phase : M.fieldTy "phase" = some (.scalar (.enum "phase")) := rfl
theorem ty_launch : M.fieldTy "launch" = some (.scalar (.enum "launch")) := rfl
theorem ty_exec : M.fieldTy "exec" = some (.scalar (.enum "exec")) := rfl
theorem ty_exitZero : M.fieldTy "exitZero" = some (.scalar .bool) := rfl
theorem ty_prompt : M.fieldTy "prompt" = some (.scalar .bool) := rfl
theorem ty_contract : M.fieldTy "contract" = some (.scalar .bool) := rfl
theorem ty_evidence : M.fieldTy "evidence" = some (.scalar .bool) := rfl
theorem ty_originals : M.fieldTy "originals" = some (.scalar .bool) := rfl
theorem ty_published : M.fieldTy "published" = some (.scalar .bool) := rfl
theorem ty_skipped : M.fieldTy "skipped" = some (.scalar .bool) := rfl

macro "rr_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_create, find_retry, find_update, find_storePrompt, find_storeContract,
      find_storeEvidence, find_finalize, find_stage, find_skip, find_verify, find_passSkipped, size_phase,
      size_launch, size_exec, ty_phase, ty_launch, ty_exec, ty_exitZero, ty_prompt, ty_contract,
      ty_evidence, ty_originals, ty_published, ty_skipped, ctors_phase, ctors_launch, ctors_exec, lookup, createEvent, retryEvent,
      updateEvent, storePromptEvent, storeContractEvent, storeEvidenceEvent, storeEvent, finalizeEvent,
      stageEvent, skipEvent, verifyEvent, passSkippedEvent, createFacts, retryFacts, finalizeFacts,
      phaseArg, launchArg, execArg, phaseIs, execIs, launchIs, exists_, open_, allArgs, boolArgs,
      bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr, boolNot, indexOf,
      enumV, boolV, natV, f, a, tt, ff, andE, orE, notE, eq, evalAssigns, applyUpdates, enc, or_assoc,
      and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_create (p : Params) (x : Rn) (i c b r : Bool) :
    step M p (enc x) ⟨"create", [boolV i, boolV c, boolV b, boolV r]⟩ =
      if x.phase = 0 ∧ i = true ∧ c = true ∧ b = true ∧ r = true then
        some (enc { x with phase := 1, launch := 0, exec := 0, exitZero := true }) else none := by
  by_cases h0 : x.phase = 0 <;> rr_simp [h0] <;> ite_close

theorem step_retry (p : Params) (x : Rn) (i c s b r : Bool) :
    step M p (enc x) ⟨"retry", [boolV i, boolV c, boolV s, boolV b, boolV r]⟩ =
      if x.phase ≠ 0 ∧ i = true ∧ c = true ∧ s = true ∧ b = true ∧ r = true then some (enc x) else none := by
  by_cases h0 : x.phase = 0 <;> rr_simp [h0] <;> ite_close


theorem step_update (p : Params) (x : Rn) (tp tl te : Nat) (ez si : Bool) :
    step M p (enc x) ⟨"update", [enumV tp, enumV tl, enumV te, boolV ez, boolV si]⟩ =
      if tp < 8 ∧ tl < 4 ∧ te < 6 ∧ x.phase ≠ 0 ∧ x.phase ≠ 6 ∧ tp ≠ 0 ∧ si = true then
        some (enc { x with phase := tp, launch := tl, exec := te, exitZero := ez }) else none := by
  by_cases h1 : tp < 8 <;> by_cases h2 : tl < 4 <;> by_cases h3 : te < 6 <;> by_cases h0 : x.phase = 0 <;>
    by_cases h6 : x.phase = 6 <;> by_cases h4 : tp = 0 <;> rr_simp [h1, h2, h3, h0, h6, h4] <;> ite_close

theorem step_storePrompt (p : Params) (x : Rn) (s : Bool) :
    step M p (enc x) ⟨"storePrompt", [boolV s]⟩ =
      if x.phase = 1 ∧ (x.prompt = false ∨ s = true) then some (enc { x with prompt := true }) else none := by
  by_cases h1 : x.phase = 1 <;> rr_simp [h1] <;> ite_close

theorem step_storeContract (p : Params) (x : Rn) (s : Bool) :
    step M p (enc x) ⟨"storeContract", [boolV s]⟩ =
      if x.phase = 1 ∧ (x.contract = false ∨ s = true) then some (enc { x with contract := true }) else none := by
  by_cases h1 : x.phase = 1 <;> rr_simp [h1] <;> ite_close

theorem step_storeEvidence (p : Params) (x : Rn) (s : Bool) :
    step M p (enc x) ⟨"storeEvidence", [boolV s]⟩ =
      if x.phase = 1 ∧ (x.evidence = false ∨ s = true) then some (enc { x with evidence := true }) else none := by
  by_cases h1 : x.phase = 1 <;> rr_simp [h1] <;> ite_close

/-- The state after an accepted `finalize`: an `ok` run whose report is invalid
becomes `failed` with a non-zero exit code. -/
def finalized (x : Rn) (l e : Nat) (ez inv : Bool) : Rn :=
  let ex := if e = 1 ∧ inv = true then 3 else e
  let z := if e = 1 ∧ inv = true then false else ez
  { x with phase := 6, launch := l, exec := ex, exitZero := z, originals := true }

theorem step_finalize (p : Params) (x : Rn) (l e : Nat) (ez ro rs inv cm si : Bool) :
    step M p (enc x) ⟨"finalize", [enumV l, enumV e, boolV ez, boolV ro, boolV rs, boolV inv, boolV cm, boolV si]⟩ =
      if l < 4 ∧ e < 6 ∧ x.phase ≠ 0 ∧ x.phase ≠ 6 ∧ si = true ∧ (e ≠ 1 ∨ ez = true ∧ l = 2 ∧ ro = true) ∧
          (e = 1 ∨ ez = false ∧ rs = true) ∧ cm = true then
        some (enc (finalized x l e ez inv)) else none := by
  by_cases h1 : l < 4 <;> by_cases h2 : e < 6 <;> by_cases h0 : x.phase = 0 <;> by_cases h6 : x.phase = 6 <;>
    by_cases he : e = 1 <;> by_cases hl : l = 2 <;> rr_simp [h1, h2, h0, h6, he, hl, finalized]
  all_goals (first | ite_close | (cases inv <;> rr_simp [] <;> ite_close))


theorem step_stage (p : Params) (x : Rn) (si g ex m ig : Bool) :
    step M p (enc x) ⟨"stage", [boolV si, boolV g, boolV ex, boolV m, boolV ig]⟩ =
      if x.phase = 6 ∧ si = true ∧ g = true ∧ x.skipped = false ∧ x.published = false ∧ ex = false ∧ m = true ∧
          ig = true then some (enc { x with published := true }) else none := by
  by_cases h6 : x.phase = 6 <;> rr_simp [h6] <;> ite_close

theorem step_skip (p : Params) (x : Rn) (si g ex : Bool) :
    step M p (enc x) ⟨"skip", [boolV si, boolV g, boolV ex]⟩ =
      if x.phase = 6 ∧ si = true ∧ g = true ∧ x.skipped = false ∧ x.published = false ∧ ex = true then
        some (enc { x with published := true, skipped := true }) else none := by
  by_cases h6 : x.phase = 6 <;> rr_simp [h6] <;> ite_close

theorem step_verify (p : Params) (x : Rn) (si g ig v : Bool) :
    step M p (enc x) ⟨"verify", [boolV si, boolV g, boolV ig, boolV v]⟩ =
      if x.phase = 6 ∧ si = true ∧ g = true ∧ x.published = true ∧ x.skipped = false ∧ ig = true ∧ v = true then
        some (enc x) else none := by
  by_cases h6 : x.phase = 6 <;> rr_simp [h6] <;> ite_close

theorem step_passSkipped (p : Params) (x : Rn) (si g : Bool) :
    step M p (enc x) ⟨"passSkipped", [boolV si, boolV g]⟩ =
      if x.phase = 6 ∧ si = true ∧ g = true ∧ x.skipped = true then some (enc x) else none := by
  by_cases h6 : x.phase = 6 <;> rr_simp [h6] <;> ite_close

end KanderFormal.Proofs.ReviewRun
