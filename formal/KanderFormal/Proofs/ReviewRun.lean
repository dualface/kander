import KanderFormal.Proofs.ReviewRunCases

/-
Proof obligations of the review run machine (M6). Every statement holds for
every value of the (empty) parameter list and for every run, because one
instance describes one run as seen by one member and the shell steps the run
events on every member's instance.
-/
namespace KanderFormal.Proofs.ReviewRun

open KanderFormal KanderFormal.Machines.ReviewRun KanderFormal.Proofs.Scalar

set_option maxHeartbeats 1000000

/-- A new run is refused when one fact fails or the run already exists. -/
theorem create_refused {p : Params} {z : Rn} {i c b r : Bool}
    (h : z.phase ≠ 0 ∨ i = false ∨ c = false ∨ b = false ∨ r = false) :
    step M p (enc z) ⟨"create", [boolV i, boolV c, boolV b, boolV r]⟩ = none := by
  rw [step_create]
  split
  · rename_i hg; obtain ⟨h0, hi, hc, hb, hr⟩ := hg; rcases h with h | h | h | h | h <;> simp_all
  · rfl

/-- A store of an input artifact is refused unless the run is `prepared`. -/
theorem store_only_prepared (p : Params) (z : Rn) (hn : z.phase ≠ 1) (sm : Bool) :
    step M p (enc z) ⟨"storePrompt", [boolV sm]⟩ = none ∧
      step M p (enc z) ⟨"storeContract", [boolV sm]⟩ = none ∧
      step M p (enc z) ⟨"storeEvidence", [boolV sm]⟩ = none := by
  refine ⟨?_, ?_, ?_⟩
  · rw [step_storePrompt]; simp [hn]
  · rw [step_storeContract]; simp [hn]
  · rw [step_storeEvidence]; simp [hn]

/-! ## Theorem 1: a finalized run is frozen, and the driver's sequences are accepted -/

/-- `UpdateReviewRun` refuses a finalized run, and `FinalizeReviewRun` does too. -/
theorem update_refused_finalized (p : Params) (x : Rn) (hf : x.phase = 6) (tp tl te : Nat) (ez si : Bool) :
    step M p (enc x) ⟨"update", [enumV tp, enumV tl, enumV te, boolV ez, boolV si]⟩ = none := by
  rw [step_update]; simp [hf]

theorem finalize_refused_finalized (p : Params) (x : Rn) (hf : x.phase = 6) (l e : Nat)
    (ez ro rs inv cm si : Bool) :
    step M p (enc x) ⟨"finalize", [enumV l, enumV e, boolV ez, boolV ro, boolV rs, boolV inv, boolV cm, boolV si]⟩ =
      none := by
  rw [step_finalize]; simp [hf]

/-- Theorem 1: once a run is finalized, no accepted event changes its phase,
launch status, execution result, exit code, input artifacts, or originals. -/
theorem finalized_frozen {p : Params} {x y : Rn} {e : Event} {s' : State} (hf : x.phase = 6)
    (h : step M p (enc x) e = some s') (hy : s' = enc y) :
    y.phase = 6 ∧ y.launch = x.launch ∧ y.exec = x.exec ∧ y.exitZero = x.exitZero ∧ y.prompt = x.prompt ∧
      y.contract = x.contract ∧ y.evidence = x.evidence ∧ y.originals = x.originals := by
  rcases accepted_cases h with ⟨i, c, b, r, rfl⟩ | ⟨i, c, sa, b, r, rfl⟩ | ⟨tp, tl, te, ez, si, rfl⟩ | ⟨sm, rfl⟩ |
    ⟨sm, rfl⟩ | ⟨sm, rfl⟩ | ⟨l, ex, ez, ro, rs, inv, cm, si, rfl⟩ | ⟨si, g, ex, m, ig, rfl⟩ | ⟨si, g, ex, rfl⟩ |
    ⟨si, g, ig, v, rfl⟩ | ⟨si, g, rfl⟩
  · rw [create_refused (Or.inl (by omega))] at h; cases h
  · rw [step_retry] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [hf]
  · rw [update_refused_finalized p x hf] at h; cases h
  · rw [(store_only_prepared p _ (by omega) sm).1] at h; cases h
  · rw [(store_only_prepared p _ (by omega) sm).2.1] at h; cases h
  · rw [(store_only_prepared p _ (by omega) sm).2.2] at h; cases h
  · rw [finalize_refused_finalized p x hf] at h; cases h
  · rw [step_stage] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [hf]
  · rw [step_skip] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [hf]
  · rw [step_verify] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [hf]
  · rw [step_passSkipped] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [hf]

/-- Runs a list of events, stopping at the first refusal. -/
def runSeq (p : Params) : State → List Event → Option State
  | s, [] => some s
  | s, e :: es => (step M p s e).bind fun s' => runSeq p s' es

def t : Val := boolV true
def x : Val := boolV false

/-- The events of the `archiveExecution` driver after a run is prepared: the
phase updates (`launching` with an unknown launch status, `running` with a
started one), then one finalization. -/
def launching : Event := ⟨"update", [enumV 2, enumV 1, enumV 0, t, t]⟩
def running : Event := ⟨"update", [enumV 3, enumV 2, enumV 0, t, t]⟩
def prepared : Event := ⟨"create", [t, t, t, t]⟩

/-- A finalization with launch status `l`, execution status `e`, an exit code of
zero or not, and a failure reason or not. -/
def finalizing (l e : Nat) (zero reason : Bool) : Event :=
  ⟨"finalize", [enumV l, enumV e, boolV zero, boolV zero, boolV reason, x, t, t]⟩

/-- The call sequences `archiveExecution` makes on a run: the normal sequence
(prepared, launching, running, finalize), a launch failure, which finalizes from
`launching` with the launch status reset to not started, and the recovery of an
unfinished run, which finalizes it as interrupted from `prepared`, `launching`,
and `running`. The sequences end with an `ok` finalization, a failure, a launch
failure, and an interruption. -/
def archiveSequences : List (List Event) := [
  [prepared, launching, running, finalizing 2 1 true false],
  [prepared, launching, running, finalizing 2 3 false true],
  [prepared, launching, finalizing 0 2 false true],
  [prepared, finalizing 0 4 false true],
  [prepared, launching, finalizing 1 4 false true],
  [prepared, launching, running, finalizing 2 4 false true],
  [prepared, launching, running, ⟨"finalize", [enumV 2, enumV 1, t, t, x, t, t, t]⟩]
]

/-- Theorem 1: every step of every sequence the driver makes is accepted. -/
theorem archive_sequences_accepted (p : Params) :
    ∀ seq ∈ archiveSequences, (runSeq p (initial M p) seq).isSome = true := by
  intro seq hseq
  simp only [archiveSequences, List.mem_cons, List.not_mem_nil, or_false] at hseq
  rcases hseq with rfl | rfl | rfl | rfl | rfl | rfl | rfl <;> rfl

/-! ## Theorem 2: an accepted `ok` finalization has its facts -/

/-- Theorem 2: a finalization that leaves the run `ok` had launch status
`started`, exit code 0, and a non-empty report. -/
theorem finalize_ok {p : Params} {z y : Rn} {s' : State} {l e : Nat} {ez ro rs inv cm si : Bool}
    (h : step M p (enc z) ⟨"finalize", [enumV l, enumV e, boolV ez, boolV ro, boolV rs, boolV inv, boolV cm, boolV si]⟩ =
      some s') (hy : s' = enc y) (hok : y.exec = 1) :
    y.launch = 2 ∧ y.exitZero = true ∧ ro = true := by
  rw [step_finalize] at h
  split at h
  · rename_i hg
    cases h
    have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, -, hok1, -, -⟩ := hg
    simp only [finalized] at hok ⊢
    by_cases hv : e = 1 ∧ inv = true
    · simp [hv] at hok
    · simp [hv] at hok
      subst hok
      simp at hok1
      simp [hok1] at hv ⊢
      simp_all
  · cases h

/-! ## Theorem 3: input artifacts and originals -/

/-- Theorem 3: an accepted step that changes an input artifact ran on a
`prepared` run. -/
theorem artifacts_only_prepared {p : Params} {z y : Rn} {e : Event} {s' : State}
    (h : step M p (enc z) e = some s') (hy : s' = enc y)
    (hc : y.prompt ≠ z.prompt ∨ y.contract ≠ z.contract ∨ y.evidence ≠ z.evidence) : z.phase = 1 := by
  rcases accepted_cases h with ⟨i, c, b, r, rfl⟩ | ⟨i, c, sa, b, r, rfl⟩ | ⟨tp, tl, te, ez, si, rfl⟩ | ⟨sm, rfl⟩ |
    ⟨sm, rfl⟩ | ⟨sm, rfl⟩ | ⟨l, ex, ez, ro, rs, inv, cm, si, rfl⟩ | ⟨si, g, ex, m, ig, rfl⟩ | ⟨si, g, ex, rfl⟩ |
    ⟨si, g, ig, v, rfl⟩ | ⟨si, g, rfl⟩
  · rw [step_create] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_retry] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_update] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_storePrompt] at h; split at h <;> cases h; rename_i hg; exact hg.1
  · rw [step_storeContract] at h; split at h <;> cases h; rename_i hg; exact hg.1
  · rw [step_storeEvidence] at h; split at h <;> cases h; rename_i hg; exact hg.1
  · rw [step_finalize] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp [finalized] at hc
  · rw [step_stage] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_skip] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_verify] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_passSkipped] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc

/-- Theorem 3: the originals are written only by an accepted finalization, from
a run that was not finalized. -/
theorem originals_only_finalize {p : Params} {z y : Rn} {e : Event} {s' : State}
    (h : step M p (enc z) e = some s') (hy : s' = enc y)
    (hc : y.originals ≠ z.originals) :
    ∃ l ex ez ro rs inv cm si,
      e = ⟨"finalize", [enumV l, enumV ex, boolV ez, boolV ro, boolV rs, boolV inv, boolV cm, boolV si]⟩ ∧
        z.originals = false ∧ y.originals = true ∧ z.phase ≠ 6 := by
  rcases accepted_cases h with ⟨i, c, b, r, rfl⟩ | ⟨i, c, sa, b, r, rfl⟩ | ⟨tp, tl, te, ez, si, rfl⟩ | ⟨sm, rfl⟩ |
    ⟨sm, rfl⟩ | ⟨sm, rfl⟩ | ⟨l, ex, ez, ro, rs, inv, cm, si, rfl⟩ | ⟨si, g, ex, m, ig, rfl⟩ | ⟨si, g, ex, rfl⟩ |
    ⟨si, g, ig, v, rfl⟩ | ⟨si, g, rfl⟩
  · rw [step_create] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_retry] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_update] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_storePrompt] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_storeContract] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_storeEvidence] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_finalize] at h
    split at h
    · rename_i hg
      cases h
      have := enc_inj hy; subst this
      refine ⟨l, ex, ez, ro, rs, inv, cm, si, rfl, ?_, rfl, hg.2.2.2.1⟩
      cases hz : z.originals
      · rfl
      · simp [finalized, hz] at hc
    · cases h
  · rw [step_stage] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_skip] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_verify] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc
  · rw [step_passSkipped] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp at hc

/-! ## Theorem 4: the retry branch of `Prepare` -/

/-- Theorem 4: an accepted retry changes nothing; it is accepted only for an
existing run whose identity, cards, frozen input, batch, and requirements hold. -/
theorem retry_spec {p : Params} {z : Rn} {s' : State} {i c sa b r : Bool}
    (h : step M p (enc z) ⟨"retry", [boolV i, boolV c, boolV sa, boolV b, boolV r]⟩ = some s') :
    s' = enc z ∧ z.phase ≠ 0 ∧ i = true ∧ c = true ∧ sa = true ∧ b = true ∧ r = true := by
  rw [step_retry] at h
  split at h
  · rename_i hg; cases h; exact ⟨rfl, hg⟩
  · cases h

/-- Theorem 4: a retry is refused when the run does not exist or any fact fails. -/
theorem retry_refused {p : Params} {z : Rn} {i c sa b r : Bool}
    (h : z.phase = 0 ∨ i = false ∨ c = false ∨ sa = false ∨ b = false ∨ r = false) :
    step M p (enc z) ⟨"retry", [boolV i, boolV c, boolV sa, boolV b, boolV r]⟩ = none := by
  rw [step_retry]
  split
  · rename_i hg; obtain ⟨h0, hi, hc, hs, hb, hr⟩ := hg; rcases h with h | h | h | h | h | h <;> simp_all
  · rfl

/-! ## Theorems 5 and 6: per-card publication -/

/-- Theorem 5: a member that takes a copy was not published, not skipped, and not
excluded, and is published afterwards without a skip. The shell writes the copy,
the manifest, and the index line exactly when this event is accepted. -/
theorem stage_spec {p : Params} {z y : Rn} {s' : State} {si g ex m ig : Bool}
    (h : step M p (enc z) ⟨"stage", [boolV si, boolV g, boolV ex, boolV m, boolV ig]⟩ = some s')
    (hy : s' = enc y) :
    z.published = false ∧ z.skipped = false ∧ ex = false ∧ z.phase = 6 ∧ y = { z with published := true } := by
  rw [step_stage] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this
    obtain ⟨h6, -, -, hs, hp, hx, -, -⟩ := hg
    exact ⟨hp, hs, hx, h6, rfl⟩
  · cases h

/-- Theorem 5: a member that left the batch is published and skipped without a
copy. -/
theorem skip_spec {p : Params} {z y : Rn} {s' : State} {si g ex : Bool}
    (h : step M p (enc z) ⟨"skip", [boolV si, boolV g, boolV ex]⟩ = some s') (hy : s' = enc y) :
    z.published = false ∧ z.skipped = false ∧ ex = true ∧ z.phase = 6 ∧
      y = { z with published := true, skipped := true } := by
  rw [step_skip] at h
  split at h
  · rename_i hg; cases h; have := enc_inj hy; subst this
    obtain ⟨h6, -, -, hs, hp, hx⟩ := hg
    exact ⟨hp, hs, hx, h6, rfl⟩
  · cases h

/-- Theorem 5: a card is published at most once: a published member takes no
second copy and is not skipped afterwards. -/
theorem publish_once (p : Params) (z : Rn) (hp : z.published = true) (si g ex m ig : Bool) :
    step M p (enc z) ⟨"stage", [boolV si, boolV g, boolV ex, boolV m, boolV ig]⟩ = none ∧
      step M p (enc z) ⟨"skip", [boolV si, boolV g, boolV ex]⟩ = none := by
  refine ⟨?_, ?_⟩
  · rw [step_stage]; simp [hp]
  · rw [step_skip]; simp [hp]

/-- Theorem 6: only a finalized run publishes, verifies, or passes a member. -/
theorem publish_needs_finalized (p : Params) (z : Rn) (hn : z.phase ≠ 6) (si g ex m ig ig' v : Bool) :
    step M p (enc z) ⟨"stage", [boolV si, boolV g, boolV ex, boolV m, boolV ig]⟩ = none ∧
      step M p (enc z) ⟨"skip", [boolV si, boolV g, boolV ex]⟩ = none ∧
      step M p (enc z) ⟨"verify", [boolV si, boolV g, boolV ig', boolV v]⟩ = none ∧
      step M p (enc z) ⟨"passSkipped", [boolV si, boolV g]⟩ = none := by
  refine ⟨?_, ?_, ?_, ?_⟩
  · rw [step_stage]; simp [hn]
  · rw [step_skip]; simp [hn]
  · rw [step_verify]; simp [hn]
  · rw [step_passSkipped]; simp [hn]

/-- The frozen `ReviewInput` is part of every publication step: a different
input is refused. -/
theorem input_conflict_refused (p : Params) (z : Rn) (g ex m ig ig' v : Bool) :
    step M p (enc z) ⟨"stage", [boolV false, boolV g, boolV ex, boolV m, boolV ig]⟩ = none ∧
      step M p (enc z) ⟨"skip", [boolV false, boolV g, boolV ex]⟩ = none ∧
      step M p (enc z) ⟨"verify", [boolV false, boolV g, boolV ig', boolV v]⟩ = none ∧
      step M p (enc z) ⟨"passSkipped", [boolV false, boolV g]⟩ = none ∧
      (∀ tp tl te ez, step M p (enc z) ⟨"update", [enumV tp, enumV tl, enumV te, boolV ez, boolV false]⟩ = none) ∧
      (∀ l e ez ro rs inv cm, step M p (enc z)
        ⟨"finalize", [enumV l, enumV e, boolV ez, boolV ro, boolV rs, boolV inv, boolV cm, boolV false]⟩ = none) := by
  refine ⟨?_, ?_, ?_, ?_, ?_, ?_⟩
  · rw [step_stage]; simp
  · rw [step_skip]; simp
  · rw [step_verify]; simp
  · rw [step_passSkipped]; simp
  · intro tp tl te ez; rw [step_update]; simp
  · intro l e ez ro rs inv cm; rw [step_finalize]; simp

/-! ## The invariant -/

/-- The safety properties of a run instance: the initial state before the run
exists; a skipped member is published; a published member belongs to a finalized
run; the originals exist only for a finalized run, and an `ok` run with
originals has launch status `started` and exit code 0. -/
structure Props (z : Rn) : Prop where
  fresh : z.phase = 0 → z = ⟨0, 0, 0, false, false, false, false, false, false, false⟩
  skippedPub : z.skipped = true → z.published = true
  pubFinal : z.published = true → z.phase = 6
  origFinal : z.originals = true → z.phase = 6
  okFacts : z.originals = true → z.exec = 1 → z.launch = 2 ∧ z.exitZero = true

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ z, s = enc z → Props z

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro z hz
  have : z = ⟨0, 0, 0, false, false, false, false, false, false, false⟩ := enc_inj (hz.symm.trans rfl)
  subst this
  exact ⟨fun _ => rfl, by simp, by simp, by simp, by simp⟩

macro "props_close" : tactic =>
  `(tactic| (refine ⟨?_, ?_, ?_, ?_, ?_⟩ <;> intros <;> simp_all [finalized] <;> omega))

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨z, rfl, hph, hl, hex⟩ := shape hc
  obtain ⟨P1, P2, P3, P4, P5⟩ := hp z rfl
  intro y hy
  rcases accepted_cases h with ⟨i, c, b, r, rfl⟩ | ⟨i, c, sa, b, r, rfl⟩ | ⟨tp, tl, te, ez, si, rfl⟩ | ⟨sm, rfl⟩ |
    ⟨sm, rfl⟩ | ⟨sm, rfl⟩ | ⟨l, ex, ez, ro, rs, inv, cm, si, rfl⟩ | ⟨si, g, ex, m, ig, rfl⟩ | ⟨si, g, ex, rfl⟩ |
    ⟨si, g, ig, v, rfl⟩ | ⟨si, g, rfl⟩
  · rw [step_create] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; obtain ⟨h0, -⟩ := hg; have := P1 h0; subst this; props_close
  · rw [step_retry] at h; split at h <;> cases h; have := enc_inj hy; subst this; exact ⟨P1, P2, P3, P4, P5⟩
  · rw [step_update] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close
  · rw [step_storePrompt] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close
  · rw [step_storeContract] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close
  · rw [step_storeEvidence] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close
  · rw [step_finalize] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; by_cases hv : ex = 1 ∧ inv = true <;> by_cases hx : ex = 1 <;> props_close
  · rw [step_stage] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close
  · rw [step_skip] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; props_close
  · rw [step_verify] at h; split at h <;> cases h; have := enc_inj hy; subst this; exact ⟨P1, P2, P3, P4, P5⟩
  · rw [step_passSkipped] at h; split at h <;> cases h; have := enc_inj hy; subst this; exact ⟨P1, P2, P3, P4, P5⟩

/-- The review run machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 8: every reachable state satisfies the invariant. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 6 on reachable states: a published member belongs to a finalized
run, and a skipped member is published. -/
theorem reachable_published_finalized {p : Params} {z : Rn} (hr : Reachable M p (enc z)) :
    (z.published = true → z.phase = 6) ∧ (z.skipped = true → z.published = true) :=
  ⟨((reachable_inv hr).2 z rfl).pubFinal, ((reachable_inv hr).2 z rfl).skippedPub⟩

/-- Theorem 2 on reachable states: a run with originals that is `ok` was
finalized with launch status `started` and exit code 0. -/
theorem reachable_ok_facts {p : Params} {z : Rn} (hr : Reachable M p (enc z)) (ho : z.originals = true)
    (he : z.exec = 1) : z.launch = 2 ∧ z.exitZero = true :=
  ((reachable_inv hr).2 z rfl).okFacts ho he

/-! ## Theorem 7: a published member is frozen -/

/-- Theorem 7: once a member is published, no accepted event changes its
instance, and so none changes the run once every member is published. -/
theorem published_frozen {p : Params} {z y : Rn} {e : Event} {s' : State} (hr : Reachable M p (enc z))
    (hp : z.published = true) (h : step M p (enc z) e = some s') (hy : s' = enc y) : y = z := by
  have hf : z.phase = 6 := ((reachable_inv hr).2 z rfl).pubFinal hp
  rcases accepted_cases h with ⟨i, c, b, r, rfl⟩ | ⟨i, c, sa, b, r, rfl⟩ | ⟨tp, tl, te, ez, si, rfl⟩ | ⟨sm, rfl⟩ |
    ⟨sm, rfl⟩ | ⟨sm, rfl⟩ | ⟨l, ex, ez, ro, rs, inv, cm, si, rfl⟩ | ⟨si, g, ex, m, ig, rfl⟩ | ⟨si, g, ex, rfl⟩ |
    ⟨si, g, ig, v, rfl⟩ | ⟨si, g, rfl⟩
  · rw [create_refused (Or.inl (by omega))] at h; cases h
  · rw [step_retry] at h; split at h <;> cases h; exact (enc_inj hy).symm
  · rw [update_refused_finalized p z hf] at h; cases h
  · rw [(store_only_prepared p _ (by omega) sm).1] at h; cases h
  · rw [(store_only_prepared p _ (by omega) sm).2.1] at h; cases h
  · rw [(store_only_prepared p _ (by omega) sm).2.2] at h; cases h
  · rw [finalize_refused_finalized p z hf] at h; cases h
  · rw [step_stage] at h; split at h <;> cases h; simp_all
  · rw [step_skip] at h; split at h <;> cases h; simp_all
  · rw [step_verify] at h; split at h <;> cases h; exact (enc_inj hy).symm
  · rw [step_passSkipped] at h; split at h <;> cases h; exact (enc_inj hy).symm

end KanderFormal.Proofs.ReviewRun
