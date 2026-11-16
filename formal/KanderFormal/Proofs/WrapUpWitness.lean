import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.WrapUpWitness

/-
Proof obligations of the wrap-up Git witness machine (M12, theorem 8). The
machine has no parameter and one scalar field, so every statement holds for every
dispatch and every group. The witness directory of a group is removed with the
group by the archive cleanup (M14); the statements are about a witness while its
group exists.
-/
namespace KanderFormal.Proofs.WrapUpWitness

open KanderFormal KanderFormal.Machines.WrapUpWitness KanderFormal.Proofs.Scalar

set_option maxHeartbeats 2000000

abbrev M := wrapUpWitnessMachine

theorem wellTyped : M.wellTyped = true := by decide

structure Wt where
  written : Bool

def enc (x : Wt) : State := [("written", boolV x.written)]

theorem enc_inj {x y : Wt} (h : enc x = enc y) : x = y := by
  obtain ⟨_⟩ := x
  obtain ⟨_⟩ := y
  simp [enc, boolV] at h
  subst h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) : ∃ x, s = enc x := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1)], hn =>
    simp [wrapUpWitnessMachine] at hn
    subst hn
    obtain ⟨t1, ht1, c1⟩ := hc "written" v1 (by simp [lookup])
    cases (rfl : M.fieldTy "written" = some (.scalar .bool)).symm.trans ht1
    obtain ⟨b1, rfl⟩ := conforms_bool c1
    exact ⟨⟨b1⟩, rfl⟩

theorem find_complete : M.findEvent "complete" = some completeEvent := rfl
theorem find_reconcile : M.findEvent "reconcile" = some reconcileEvent := rfl
theorem ty_written : M.fieldTy "written" = some (.scalar .bool) := rfl

macro "ww_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_complete, find_reconcile, ty_written, lookup, completeEvent, reconcileEvent, boolArg,
      bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr, boolNot, indexOf, boolV, f, a,
      tt, andE, orE, notE, evalAssigns, applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc,
      map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_complete (p : Params) (x : Wt) (es v : Bool) :
    step M p (enc x) ⟨"complete", [boolV es, boolV v]⟩ =
      if x.written = false ∧ es = true ∧ v = true then some (enc ⟨true⟩) else none := by
  obtain ⟨w⟩ := x
  cases w <;> cases es <;> cases v <;> ww_simp [] <;> ite_close

theorem step_reconcile (p : Params) (x : Wt) (c va v : Bool) :
    step M p (enc x) ⟨"reconcile", [boolV c, boolV va, boolV v]⟩ =
      if (x.written = false ∨ va = true) ∧ (v = true ∨ c = true ∧ x.written = true) then
        some (enc ⟨x.written || c⟩) else none := by
  obtain ⟨w⟩ := x
  cases w <;> cases c <;> cases va <;> cases v <;> ww_simp [] <;> ite_close

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) : ev = completeEvent ∨ ev = reconcileEvent := by
  simpa [wrapUpWitnessMachine] using h

theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ es v, e = ⟨"complete", [boolV es, boolV v]⟩) ∨ (∃ c va v, e = ⟨"reconcile", [boolV c, boolV va, boolV v]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl
  · obtain ⟨bs, rfl, hl⟩ := args_bools (m := M) (p := p) (ds := [boolArg "epochSame", boolArg "verified"])
      (by intro d hd; simp at hd; rcases hd with rfl | rfl <;> rfl) hb
    match bs, hl with
    | [es, v], _ => exact .inl ⟨es, v, rfl⟩
  · obtain ⟨bs, rfl, hl⟩ := args_bools (m := M) (p := p) (ds := [boolArg "completed", boolArg "valid", boolArg "verified"])
      (by intro d hd; simp at hd; rcases hd with rfl | rfl | rfl <;> rfl) hb
    match bs, hl with
    | [c, va, v], _ => exact .inr ⟨c, va, v, rfl⟩

/-! ## The invariant: conformance is all the state needs -/

def cert : SafetyCert M where
  Inv := fun p s => M.StateConforms p s
  init := fun p => initial_conforms wellTyped p
  step := fun _ _ _ _ hi h => step_preserves_conforms hi h

theorem reachable_conforms' {p : Params} {s : State} (h : Reachable M p s) : M.StateConforms p s :=
  cert.reachable h

/-! ## Theorem 8: a witness is written once and never changes -/

/-- Theorem 8: no accepted event clears a written witness, and a written witness
is never written again: the completion move refuses it, and a reconcile of a
written witness leaves the state as it is. -/
theorem written_frozen {p : Params} {z y : Wt} {e : Event} {s' : State} (h : step M p (enc z) e = some s')
    (hy : s' = enc y) : z.written = true → y.written = true := by
  rcases accepted_cases h with ⟨es, v, rfl⟩ | ⟨c, va, v, rfl⟩
  · rw [step_complete] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_reconcile] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp_all

/-- Theorem 8: a second write is refused: the completion move on a written
witness. -/
theorem complete_once (p : Params) (z : Wt) (hw : z.written = true) (es v : Bool) :
    step M p (enc z) ⟨"complete", [boolV es, boolV v]⟩ = none := by
  rw [step_complete]; simp [hw]

/-- Theorem 8: a reconcile of a written witness changes nothing. -/
theorem reconcile_no_rewrite {p : Params} {z : Wt} {s' : State} {c va v : Bool}
    (hw : z.written = true)
    (h : step M p (enc z) ⟨"reconcile", [boolV c, boolV va, boolV v]⟩ = some s') : s' = enc z := by
  rw [step_reconcile] at h
  split at h
  · cases h; obtain ⟨w⟩ := z; simp at hw; subst hw; rfl
  · cases h

/-- The completion move writes the witness only for the epoch it verified, after
a passed verification, and only once. -/
theorem complete_spec {p : Params} {z y : Wt} {s' : State} {es v : Bool}
    (h : step M p (enc z) ⟨"complete", [boolV es, boolV v]⟩ = some s') (hy : s' = enc y) :
    z.written = false ∧ es = true ∧ v = true ∧ y.written = true := by
  rw [step_complete] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this; exact ⟨hg.1, hg.2.1, hg.2.2, rfl⟩
  · cases h

/-- Theorem 8: a stale epoch or a failed verification writes nothing. -/
theorem complete_refused (p : Params) (z : Wt) (es v : Bool) (h : es = false ∨ v = false) :
    step M p (enc z) ⟨"complete", [boolV es, boolV v]⟩ = none := by
  rw [step_complete]; rcases h with rfl | rfl <;> simp

/-- A completed wrap-up that has its valid witness is not verified again. -/
theorem reconcile_recorded_skips_git (p : Params) (z : Wt) (hw : z.written = true) :
    step M p (enc z) ⟨"reconcile", [boolV true, boolV true, boolV false]⟩ = some (enc z) := by
  rw [step_reconcile]; obtain ⟨w⟩ := z; simp at hw; subst hw; simp

/-- Any other wrap-up needs a passed verification. -/
theorem reconcile_needs_git (p : Params) (z : Wt) (c va : Bool) (h : z.written = false ∨ c = false) :
    step M p (enc z) ⟨"reconcile", [boolV c, boolV va, boolV false]⟩ = none := by
  rw [step_reconcile]
  rcases h with h | h <;> simp [h]

/-- A written witness that does not match its dispatch is refused, whatever the
verification says. -/
theorem reconcile_invalid_refused (p : Params) (z : Wt) (hw : z.written = true) (c v : Bool) :
    step M p (enc z) ⟨"reconcile", [boolV c, boolV false, boolV v]⟩ = none := by
  rw [step_reconcile]; simp [hw]

/-- A completed wrap-up that passed verification gets its witness. -/
theorem reconcile_writes {p : Params} {z y : Wt} {s' : State} {va : Bool}
    (h : step M p (enc z) ⟨"reconcile", [boolV true, boolV va, boolV true]⟩ = some s') (hy : s' = enc y) :
    y.written = true := by
  rw [step_reconcile] at h
  split at h
  · cases h; have := enc_inj hy; subst this; simp
  · cases h

end KanderFormal.Proofs.WrapUpWitness
