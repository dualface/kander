import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ReviewFindings

/-
The review findings machine on an encoded state: every event evaluates to an
explicit `if` on the components of the state. The theorems of
`Proofs/ReviewFindings.lean` are proved on these forms.
-/
namespace KanderFormal.Proofs.ReviewFindings

open KanderFormal KanderFormal.Machines.ReviewFindings KanderFormal.Proofs.Scalar

set_option maxHeartbeats 1000000

abbrev M := reviewFindingsMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `schema`: 0 legacy, 1 report, 2 receiver. -/
structure Fd where
  opened : Bool
  schema : Nat
  structured : Bool
  fenced : Bool
  interp : Bool
  legacy : Bool
  assigned : Bool
  closed : Bool

def enc (x : Fd) : State :=
  [("opened", boolV x.opened), ("schema", enumV x.schema), ("structured", boolV x.structured),
    ("fenced", boolV x.fenced), ("interp", boolV x.interp), ("legacy", boolV x.legacy),
    ("assigned", boolV x.assigned), ("closed", boolV x.closed)]

theorem enc_inj {x y : Fd} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _, _, _, _⟩ := y
  simp [enc, enumV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.schema < 3 := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5), (_, v6), (_, v7), (_, v8)], hn =>
    simp [reviewFindingsMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "opened" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "schema" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "structured" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "fenced" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "interp" v5 (by simp [lookup])
    obtain ⟨t6, ht6, c6⟩ := hc "legacy" v6 (by simp [lookup])
    obtain ⟨t7, ht7, c7⟩ := hc "assigned" v7 (by simp [lookup])
    obtain ⟨t8, ht8, c8⟩ := hc "closed" v8 (by simp [lookup])
    cases (rfl : M.fieldTy "opened" = some (.scalar .bool)).symm.trans ht1
    cases (rfl : M.fieldTy "schema" = some (.scalar (.enum "schema"))).symm.trans ht2
    cases (rfl : M.fieldTy "structured" = some (.scalar .bool)).symm.trans ht3
    cases (rfl : M.fieldTy "fenced" = some (.scalar .bool)).symm.trans ht4
    cases (rfl : M.fieldTy "interp" = some (.scalar .bool)).symm.trans ht5
    cases (rfl : M.fieldTy "legacy" = some (.scalar .bool)).symm.trans ht6
    cases (rfl : M.fieldTy "assigned" = some (.scalar .bool)).symm.trans ht7
    cases (rfl : M.fieldTy "closed" = some (.scalar .bool)).symm.trans ht8
    obtain ⟨b1, rfl⟩ := conforms_bool c1
    obtain ⟨sc, rfl, hsc⟩ := conforms_enum (rfl : M.enumSize "schema" = some 3) c2
    obtain ⟨b3, rfl⟩ := conforms_bool c3
    obtain ⟨b4, rfl⟩ := conforms_bool c4
    obtain ⟨b5, rfl⟩ := conforms_bool c5
    obtain ⟨b6, rfl⟩ := conforms_bool c6
    obtain ⟨b7, rfl⟩ := conforms_bool c7
    obtain ⟨b8, rfl⟩ := conforms_bool c8
    exact ⟨⟨b1, sc, b3, b4, b5, b6, b7, b8⟩, rfl, hsc⟩

/-! ## Events on an encoded state -/

theorem find_open : M.findEvent "open" = some openEvent := rfl
theorem find_close : M.findEvent "close" = some closeEvent := rfl
theorem find_interpret : M.findEvent "interpret" = some interpretEvent := rfl
theorem find_mapLegacy : M.findEvent "mapLegacy" = some mapLegacyEvent := rfl
theorem find_assign : M.findEvent "assign" = some assignEvent := rfl
theorem size_schema : M.enumSize "schema" = some 3 := rfl
theorem ctors_schema : M.ctorsOf "schema" = some ["legacy", "report", "receiver"] := rfl

theorem ty_opened : M.fieldTy "opened" = some (.scalar .bool) := rfl
theorem ty_schema : M.fieldTy "schema" = some (.scalar (.enum "schema")) := rfl
theorem ty_structured : M.fieldTy "structured" = some (.scalar .bool) := rfl
theorem ty_fenced : M.fieldTy "fenced" = some (.scalar .bool) := rfl
theorem ty_interp : M.fieldTy "interp" = some (.scalar .bool) := rfl
theorem ty_legacy : M.fieldTy "legacy" = some (.scalar .bool) := rfl
theorem ty_assigned : M.fieldTy "assigned" = some (.scalar .bool) := rfl
theorem ty_closed : M.fieldTy "closed" = some (.scalar .bool) := rfl

macro "rf_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_open, find_close, find_interpret, find_mapLegacy, find_assign, size_schema,
      ty_opened, ty_schema, ty_structured, ty_fenced, ty_interp, ty_legacy, ty_assigned, ty_closed, ctors_schema,
      lookup, openEvent, closeEvent, interpretEvent, mapLegacyEvent, assignEvent, openFacts, sourceFacts,
      assignFacts, schemaArg, schemaIs, writableE, availableE, allArgs, boolArgs, bindArgs, Ty.conforms,
      STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr, boolNot, indexOf, enumV, boolV, natV, f, a, tt,
      andE, orE, notE, eq, evalAssigns, applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc,
      map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_open (p : Params) (x : Fd) (sc : Nat) (st fe co : Bool) :
    step M p (enc x) ⟨"open", [enumV sc, boolV st, boolV fe, boolV co]⟩ =
      if sc < 3 ∧ x.opened = false ∧ co = true ∧ (st = false ∨ fe = true) then
        some (enc { x with opened := true, schema := sc, structured := st, fenced := fe }) else none := by
  by_cases h1 : sc < 3 <;> by_cases h0 : x.opened = true <;> rf_simp [h1, h0] <;> ite_close

theorem step_close (p : Params) (x : Fd) :
    step M p (enc x) ⟨"close", []⟩ =
      if x.opened = true ∧ x.closed = false then some (enc { x with closed := true }) else none := by
  by_cases h0 : x.opened = true <;> rf_simp [h0] <;> ite_close

theorem step_interpret (p : Params) (x : Fd) (v s l : Bool) :
    step M p (enc x) ⟨"interpret", [boolV v, boolV s, boolV l]⟩ =
      if x.opened = true ∧ x.schema = 2 ∧ v = true ∧
          (x.interp = true ∧ s = true ∧ l = true ∨
            x.interp = false ∧ x.closed = false ∧ x.structured = false ∧ l = true) then
        some (enc { x with interp := true }) else none := by
  by_cases h0 : x.opened = true <;> by_cases h2 : x.schema = 2 <;> rf_simp [h0, h2] <;> ite_close

theorem step_mapLegacy (p : Params) (x : Fd) (v s l : Bool) :
    step M p (enc x) ⟨"mapLegacy", [boolV v, boolV s, boolV l]⟩ =
      if x.opened = true ∧ x.closed = false ∧ x.schema = 0 ∧ v = true ∧ x.fenced = false ∧ l = true ∧
          (x.legacy = false ∨ s = true) then
        some (enc { x with legacy := true }) else none := by
  by_cases h0 : x.opened = true <;> by_cases h2 : x.schema = 0 <;> rf_simp [h0, h2] <;> ite_close

theorem step_assign (p : Params) (x : Fd) (pv l c m ac o s : Bool) :
    step M p (enc x) ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV ac, boolV o, boolV s]⟩ =
      if x.opened = true ∧ x.closed = false ∧
          (x.schema = 2 ∧ (x.interp = true ∨ x.structured = true) ∨
            x.schema = 1 ∧ x.structured = true ∨ x.schema = 0 ∧ (x.structured = true ∨ x.legacy = true)) ∧
          pv = true ∧ l = true ∧ c = true ∧ m = true ∧ ac = true ∧ o = true ∧ (x.assigned = false ∨ s = true) then
        some (enc { x with assigned := true }) else none := by
  by_cases h0 : x.opened = true <;> by_cases h2 : x.schema = 2 <;> by_cases h1 : x.schema = 1 <;>
    by_cases h3 : x.schema = 0 <;> rf_simp [h0, h2, h1, h3] <;> ite_close

end KanderFormal.Proofs.ReviewFindings
