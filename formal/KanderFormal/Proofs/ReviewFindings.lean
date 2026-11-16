import KanderFormal.Proofs.ReviewFindingsCases

/-
Proof obligations of the review findings machine (M7 and the assignment of M8).
Every statement holds for every value of the (empty) parameter list and for
every run, because one instance describes one run and the number of findings and
members only enters through the Boolean facts the shell computes from the
persisted records (`covers`, `members`, `active`, `owners`, `lineage`, `same`).
-/
namespace KanderFormal.Proofs.ReviewFindings

open KanderFormal KanderFormal.Machines.ReviewFindings KanderFormal.Proofs.Scalar

set_option maxHeartbeats 1000000

/-- The findings of the run can be obtained (the query `available`). -/
def Avail (z : Fd) : Prop :=
  z.opened = true ∧
    (z.schema = 2 ∧ (z.interp = true ∨ z.structured = true) ∨
      z.schema = 1 ∧ z.structured = true ∨ z.schema = 0 ∧ (z.structured = true ∨ z.legacy = true))

/-! ## The invariant -/

/-- The safety properties of a run instance: before the run is read in, the
initial state; a structured report contains the block marker; an interpretation
exists only for a receiver run with an unstructured report; a legacy mapping
only for a legacy run whose report has no block marker (so a run has at most one
source); an assignment only for a run whose findings are available; a closed
batch belongs to a run that was read in. -/
structure Props (z : Fd) : Prop where
  fresh : z.opened = false → z = ⟨false, 0, false, false, false, false, false, false⟩
  fence : z.structured = true → z.fenced = true
  interp : z.interp = true → z.opened = true ∧ z.schema = 2 ∧ z.structured = false
  legacy : z.legacy = true → z.opened = true ∧ z.schema = 0 ∧ z.fenced = false
  assigned : z.assigned = true → Avail z
  closed : z.closed = true → z.opened = true

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ z, s = enc z → Props z

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro z hz
  have : z = ⟨false, 0, false, false, false, false, false, false⟩ := enc_inj (hz.symm.trans rfl)
  subst this
  exact ⟨fun _ => rfl, by simp, by simp, by simp, by simp [Avail], by simp⟩

macro "props_close" : tactic =>
  `(tactic| (refine ⟨?_, ?_, ?_, ?_, ?_, ?_⟩ <;> intros <;> simp_all [Avail] <;> try omega))

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨z, rfl, hsc⟩ := shape hc
  obtain ⟨P1, P2, P3, P4, P5, P6⟩ := hp z rfl
  intro y hy
  rcases accepted_cases h with ⟨sc, st, fe, co, rfl⟩ | rfl | ⟨v, sa, l, rfl⟩ | ⟨v, sa, l, rfl⟩ |
    ⟨pv, l, c, m, ac, o, sa, rfl⟩
  · rw [step_open] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; have := P1 hg.2.1; subst this; props_close
  · rw [step_close] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close
  · rw [step_interpret] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; obtain ⟨h0, h2, hv, hr⟩ := hg
    rcases hr with ⟨hi, -, -⟩ | ⟨hi, hc, hs, -⟩ <;> props_close
  · rw [step_mapLegacy] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close
  · rw [step_assign] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close

/-- The review findings machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

theorem reachable_props {p : Params} {z : Fd} (hr : Reachable M p (enc z)) : Props z := (reachable_inv hr).2 z rfl

/-! ## Reading the run in -/

/-- A run is read in once, for a consumable run, with a block marker when its
report is structured. -/
theorem open_spec {p : Params} {z y : Fd} {s' : State} {sc : Nat} {st fe co : Bool}
    (h : step M p (enc z) ⟨"open", [enumV sc, boolV st, boolV fe, boolV co]⟩ = some s') (hy : s' = enc y) :
    z.opened = false ∧ co = true ∧ (st = true → fe = true) ∧
      y = { z with opened := true, schema := sc, structured := st, fenced := fe } := by
  rw [step_open] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this
    obtain ⟨-, h0, hco, hf⟩ := hg
    exact ⟨h0, hco, by intro hs; rcases hf with hf | hf <;> simp_all, rfl⟩
  · cases h

theorem open_refused_unconsumable (p : Params) (z : Fd) (sc : Nat) (st fe : Bool) :
    step M p (enc z) ⟨"open", [enumV sc, boolV st, boolV fe, boolV false]⟩ = none := by
  rw [step_open]; simp

theorem open_once (p : Params) (z : Fd) (ho : z.opened = true) (sc : Nat) (st fe co : Bool) :
    step M p (enc z) ⟨"open", [enumV sc, boolV st, boolV fe, boolV co]⟩ = none := by
  rw [step_open]; simp [ho]

/-- No source or assignment is written before the run is read in. -/
theorem unopened_refused (p : Params) (z : Fd) (hn : z.opened = false) (v sa l : Bool) (pv c m ac o : Bool) :
    step M p (enc z) ⟨"interpret", [boolV v, boolV sa, boolV l]⟩ = none ∧
      step M p (enc z) ⟨"mapLegacy", [boolV v, boolV sa, boolV l]⟩ = none ∧
      step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV ac, boolV o, boolV sa]⟩ = none := by
  refine ⟨?_, ?_, ?_⟩
  · rw [step_interpret]; simp [hn]
  · rw [step_mapLegacy]; simp [hn]
  · rw [step_assign]; simp [hn]

/-! ## Theorem 1: one source per run, fixed by the schema and the report -/

/-- An interpretation is written only for a receiver run (schema 2): a legacy
report (schema 0) and a schema 1 report cannot use it. -/
theorem interpret_refused_schema (p : Params) (z : Fd) (hs : z.schema ≠ 2) (v sa l : Bool) :
    step M p (enc z) ⟨"interpret", [boolV v, boolV sa, boolV l]⟩ = none := by
  rw [step_interpret]; simp [hs]

/-- A legacy mapping is written only for a legacy run (schema 0): a schema 1 or
schema 2 report cannot use it. -/
theorem mapLegacy_refused_schema (p : Params) (z : Fd) (hs : z.schema ≠ 0) (v sa l : Bool) :
    step M p (enc z) ⟨"mapLegacy", [boolV v, boolV sa, boolV l]⟩ = none := by
  rw [step_mapLegacy]; simp [hs]

/-- A report that contains the block marker takes no legacy mapping. -/
theorem mapLegacy_refused_fenced (p : Params) (z : Fd) (hf : z.fenced = true) (v sa l : Bool) :
    step M p (enc z) ⟨"mapLegacy", [boolV v, boolV sa, boolV l]⟩ = none := by
  rw [step_mapLegacy]; simp [hf]

/-- A report with structured findings takes no new interpretation (a stored one
cannot exist for it, see `Props.interp`). -/
theorem interpret_refused_structured (p : Params) (z : Fd) (hs : z.structured = true) (hi : z.interp = false)
    (v sa l : Bool) : step M p (enc z) ⟨"interpret", [boolV v, boolV sa, boolV l]⟩ = none := by
  rw [step_interpret]; simp [hs, hi]

/-- On every reachable run, a structured report takes no interpretation at all
and a block-marked report no mapping. -/
theorem reachable_structured_no_source {p : Params} {z : Fd} (hr : Reachable M p (enc z))
    (v sa l : Bool) (hs : z.structured = true) :
    step M p (enc z) ⟨"interpret", [boolV v, boolV sa, boolV l]⟩ = none ∧
      step M p (enc z) ⟨"mapLegacy", [boolV v, boolV sa, boolV l]⟩ = none := by
  have P := reachable_props hr
  have hi : z.interp = false := by
    cases hi : z.interp
    · rfl
    · have := P.interp hi; simp_all
  exact ⟨interpret_refused_structured p z hs hi v sa l, mapLegacy_refused_fenced p z (P.fence hs) v sa l⟩

/-- A run has at most one source: no reachable run holds both an interpretation
and a legacy mapping, and a source exists only for the schema it belongs to. -/
theorem reachable_one_source {p : Params} {z : Fd} (hr : Reachable M p (enc z)) :
    ¬(z.interp = true ∧ z.legacy = true) ∧ (z.interp = true → z.schema = 2) ∧ (z.legacy = true → z.schema = 0) := by
  have P := reachable_props hr
  refine ⟨?_, fun hi => (P.interp hi).2.1, fun hl => (P.legacy hl).2.1⟩
  intro ⟨hi, hl⟩
  have := (P.interp hi).2.1
  have := (P.legacy hl).2.1
  omega

/-- An accepted interpretation: the run was read in as a receiver run, the text
and lineage hold, and either the interpretation was stored with the same content
and nothing changes, or the batch is open and the report is not structured and
the interpretation is stored. -/
theorem interpret_spec {p : Params} {z y : Fd} {s' : State} {v sa l : Bool}
    (h : step M p (enc z) ⟨"interpret", [boolV v, boolV sa, boolV l]⟩ = some s') (hy : s' = enc y) :
    z.opened = true ∧ z.schema = 2 ∧ v = true ∧ l = true ∧
      ((z.interp = true ∧ sa = true ∧ y = z) ∨
        (z.interp = false ∧ z.closed = false ∧ z.structured = false ∧ y = { z with interp := true })) := by
  rw [step_interpret] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this
    obtain ⟨h0, h2, hv, hr⟩ := hg
    refine ⟨h0, h2, hv, ?_, ?_⟩
    · rcases hr with hr | hr <;> simp_all
    · rcases hr with ⟨hi, hs, hl⟩ | ⟨hi, hc, hs, hl⟩
      · left; refine ⟨hi, hs, ?_⟩; cases z; simp_all
      · right; exact ⟨hi, hc, hs, rfl⟩
  · cases h

/-- An accepted legacy mapping: the batch is open, the run is a legacy run, the
report has no block marker, the text and lineage hold, and a stored mapping had
the same content. -/
theorem mapLegacy_spec {p : Params} {z y : Fd} {s' : State} {v sa l : Bool}
    (h : step M p (enc z) ⟨"mapLegacy", [boolV v, boolV sa, boolV l]⟩ = some s') (hy : s' = enc y) :
    z.opened = true ∧ z.closed = false ∧ z.schema = 0 ∧ v = true ∧ z.fenced = false ∧ l = true ∧
      (z.legacy = true → sa = true) ∧ y = { z with legacy := true } := by
  rw [step_mapLegacy] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this
    obtain ⟨h0, hc, h2, hv, hf, hl, hs⟩ := hg
    refine ⟨h0, hc, h2, hv, hf, hl, ?_, rfl⟩
    intro hz; rcases hs with hs | hs <;> simp_all
  · cases h

/-! ## Theorem 2: a written source or assignment never changes -/

/-- A stored interpretation with the same content, on a run whose lineage holds,
is accepted and changes nothing (whether or not the batch is closed). -/
theorem interpret_replay {p : Params} {z : Fd} (hr : Reachable M p (enc z)) (hi : z.interp = true) :
    step M p (enc z) ⟨"interpret", [boolV true, boolV true, boolV true]⟩ = some (enc z) := by
  have P := (reachable_props hr).interp hi
  have e : ({ z with interp := true } : Fd) = z := by cases z; simp_all
  rw [step_interpret]
  split
  · rw [e]
  · rename_i hg; exact absurd ⟨P.1, P.2.1, rfl, Or.inl ⟨hi, rfl, rfl⟩⟩ hg

/-- A different content is refused. -/
theorem interpret_conflict (p : Params) (z : Fd) (hi : z.interp = true) (v l : Bool) :
    step M p (enc z) ⟨"interpret", [boolV v, boolV false, boolV l]⟩ = none := by
  rw [step_interpret]; simp [hi]

theorem mapLegacy_replay {p : Params} {z : Fd} (hr : Reachable M p (enc z)) (hl : z.legacy = true)
    (hc : z.closed = false) :
    step M p (enc z) ⟨"mapLegacy", [boolV true, boolV true, boolV true]⟩ = some (enc z) := by
  have P := (reachable_props hr).legacy hl
  have e : ({ z with legacy := true } : Fd) = z := by cases z; simp_all
  rw [step_mapLegacy]
  split
  · rw [e]
  · rename_i hg; exact absurd ⟨P.1, hc, P.2.1, rfl, P.2.2, rfl, Or.inr rfl⟩ hg

theorem mapLegacy_conflict (p : Params) (z : Fd) (hl : z.legacy = true) (v l : Bool) :
    step M p (enc z) ⟨"mapLegacy", [boolV v, boolV false, boolV l]⟩ = none := by
  rw [step_mapLegacy]; simp [hl]

theorem assign_replay {p : Params} {z : Fd} (hr : Reachable M p (enc z)) (ha : z.assigned = true)
    (hc : z.closed = false) :
    step M p (enc z) ⟨"assign", [boolV true, boolV true, boolV true, boolV true, boolV true, boolV true, boolV true]⟩ =
      some (enc z) := by
  have P := (reachable_props hr).assigned ha
  have e : ({ z with assigned := true } : Fd) = z := by cases z; simp_all
  rw [step_assign]
  split
  · rw [e]
  · rename_i hg; exact absurd ⟨P.1, hc, P.2, rfl, rfl, rfl, rfl, rfl, rfl, Or.inr rfl⟩ hg

theorem assign_conflict (p : Params) (z : Fd) (ha : z.assigned = true) (pv l c m ac o : Bool) :
    step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV ac, boolV o, boolV false]⟩ = none := by
  rw [step_assign]; simp [ha]

/-- Once the run is read in, no accepted event changes its schema or the facts
about its report, and no accepted event clears a source, the assignment, or the
closing of the batch. -/
theorem written_frozen {p : Params} {z y : Fd} {e : Event} {s' : State} (h : step M p (enc z) e = some s')
    (hy : s' = enc y) (ho : z.opened = true) :
    y.opened = true ∧ y.schema = z.schema ∧ y.structured = z.structured ∧ y.fenced = z.fenced ∧
      (z.interp = true → y.interp = true) ∧ (z.legacy = true → y.legacy = true) ∧
      (z.assigned = true → y.assigned = true) ∧ (z.closed = true → y.closed = true) := by
  rcases accepted_cases h with ⟨sc, st, fe, co, rfl⟩ | rfl | ⟨v, sa, l, rfl⟩ | ⟨v, sa, l, rfl⟩ |
    ⟨pv, l, c, m, ac, o, sa, rfl⟩
  · rw [open_once p z ho] at h; cases h
  · rw [step_close] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [ho]
  · rw [step_interpret] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [ho]
  · rw [step_mapLegacy] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [ho]
  · rw [step_assign] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [ho]

/-! ## Theorems 3 and 4: assignment -/

/-- Theorem 3: a run whose findings are not available (a receiver run waiting
for its interpretation among them) takes no assignment. -/
theorem assign_refused_unavailable (p : Params) (z : Fd) (hn : ¬Avail z) (pv l c m ac o sa : Bool) :
    step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV ac, boolV o, boolV sa]⟩ = none := by
  rw [step_assign]
  split
  · rename_i hg; exfalso; apply hn; obtain ⟨h0, -, h2, -⟩ := hg; exact ⟨h0, h2⟩
  · rfl

theorem assign_refused_pending (p : Params) (z : Fd) (h2 : z.schema = 2) (hi : z.interp = false)
    (hs : z.structured = false) (pv l c m ac o sa : Bool) :
    step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV ac, boolV o, boolV sa]⟩ = none := by
  apply assign_refused_unavailable
  rintro ⟨-, hv⟩
  simp_all

/-- Theorem 3: on a reachable run, a pending run has no assignment. -/
theorem reachable_pending_unassigned {p : Params} {z : Fd} (hr : Reachable M p (enc z)) (h2 : z.schema = 2)
    (hi : z.interp = false) (hs : z.structured = false) : z.assigned = false := by
  have P := reachable_props hr
  cases ha : z.assigned
  · rfl
  · have := P.assigned ha; simp [Avail] at this; simp_all

/-- Theorem 4: an accepted assignment covers every finding, assigns only to run
members, assigns no excluded member, gives every assigned card an owner, and
had its provenance and lineage checked; a stored assignment had the same
content. -/
theorem assign_spec {p : Params} {z y : Fd} {s' : State} {pv l c m ac o sa : Bool}
    (h : step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV ac, boolV o, boolV sa]⟩ = some s')
    (hy : s' = enc y) :
    Avail z ∧ z.closed = false ∧ pv = true ∧ l = true ∧ c = true ∧ m = true ∧ ac = true ∧ o = true ∧
      (z.assigned = true → sa = true) ∧ y = { z with assigned := true } := by
  rw [step_assign] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this
    obtain ⟨h0, hc, hv, hpv, hl, hcv, hm, hac, ho, hs⟩ := hg
    refine ⟨⟨h0, hv⟩, hc, hpv, hl, hcv, hm, hac, ho, ?_, rfl⟩
    intro hz; rcases hs with hs | hs <;> simp_all
  · cases h

theorem assign_refused_coverage (p : Params) (z : Fd) (pv l m ac o sa : Bool) :
    step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV false, boolV m, boolV ac, boolV o, boolV sa]⟩ = none := by
  rw [step_assign]; simp

theorem assign_refused_foreign (p : Params) (z : Fd) (pv l c ac o sa : Bool) :
    step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV c, boolV false, boolV ac, boolV o, boolV sa]⟩ = none := by
  rw [step_assign]; simp

theorem assign_refused_excluded (p : Params) (z : Fd) (pv l c m o sa : Bool) :
    step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV false, boolV o, boolV sa]⟩ = none := by
  rw [step_assign]; simp

/-! ## Theorem 8: after the batch closes -/

/-- Once the batch is closed, no mapping and no assignment is accepted, and an
interpretation only as the replay of the stored one, which changes nothing. -/
theorem closed_writes_refused (p : Params) (z : Fd) (hc : z.closed = true) (v sa l pv c m ac o : Bool) :
    step M p (enc z) ⟨"mapLegacy", [boolV v, boolV sa, boolV l]⟩ = none ∧
      step M p (enc z) ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV ac, boolV o, boolV sa]⟩ = none := by
  refine ⟨?_, ?_⟩
  · rw [step_mapLegacy]; simp [hc]
  · rw [step_assign]; simp [hc]

theorem interpret_closed {p : Params} {z y : Fd} {s' : State} {v sa l : Bool} (hc : z.closed = true)
    (h : step M p (enc z) ⟨"interpret", [boolV v, boolV sa, boolV l]⟩ = some s') (hy : s' = enc y) :
    z.interp = true ∧ y = z := by
  obtain ⟨-, -, -, -, hr⟩ := interpret_spec h hy
  rcases hr with ⟨hi, -, hyz⟩ | ⟨-, hc', -⟩
  · exact ⟨hi, hyz⟩
  · simp_all

end KanderFormal.Proofs.ReviewFindings
