import KanderFormal.Proofs.ServerRequest

/-
Proof obligations of the server request machine for dispatch proposals (their
creation twice, their formal preparation, and their completion) and the
invariant of every request. They hold for every value of `maxGen` and
`maxRev`.
-/
namespace KanderFormal.Proofs.ServerRequest

open KanderFormal KanderFormal.Machines.ServerRequest KanderFormal.Proofs.Scalar

/-! ## Theorem 5: a proposal is immutable -/

/-- Theorem 5: a second creation of the dispatch of a proposal returns the
proposal and changes nothing when the claim still holds (the card in `working`
or `review`, an active record, the claim's generation, a lease that has not
expired), the card is at the revision the proposal binds, the proposal targets
that generation, and the client, the window and the material are the same.
The request is accepted whatever its status. -/
theorem recreate_spec (p : Params) (x : Rq) (he sd : Bool) (tg ca : Nat) (sm : Bool) :
    step M p (enc x) ⟨"recreate", [boolV he, boolV sd, natV tg, natV ca, boolV sm]⟩ ≠ none ↔
      tg ≤ p "maxGen" ∧ ca ≤ p "maxRev" ∧ x.st ≠ 0 ∧ x.prop = true ∧
        he = true ∧ sd = true ∧ x.tgen = tg ∧ x.prev = ca ∧ sm = true := by
  rw [step_recreate]
  by_cases h : tg ≤ p "maxGen" ∧ ca ≤ p "maxRev" ∧ x.st ≠ 0 ∧ x.prop = true ∧
    he = true ∧ sd = true ∧ x.tgen = tg ∧ x.prev = ca ∧ sm = true <;> simp [h]

theorem recreate_keeps (p : Params) (x : Rq) (he sd : Bool) (tg ca : Nat) (sm : Bool) {s' : State}
    (h : step M p (enc x) ⟨"recreate", [boolV he, boolV sd, natV tg, natV ca, boolV sm]⟩ =
      some s') : s' = enc x := by
  rw [step_recreate] at h
  exact (ite_some_enc h).2

/-- Theorem 5: a second creation that differs in the target generation, the
bound card revision, or the client, window or material is refused, and so is
one the claim no longer holds for. -/
theorem recreate_refused (p : Params) (x : Rq) (he sd : Bool) (tg ca : Nat) (sm : Bool) :
    (x.tgen ≠ tg → step M p (enc x) ⟨"recreate", [boolV he, boolV sd, natV tg, natV ca, boolV sm]⟩ = none) ∧
    (x.prev ≠ ca → step M p (enc x) ⟨"recreate", [boolV he, boolV sd, natV tg, natV ca, boolV sm]⟩ = none) ∧
    (sm = false → step M p (enc x) ⟨"recreate", [boolV he, boolV sd, natV tg, natV ca, boolV sm]⟩ = none) ∧
    (sd = false → step M p (enc x) ⟨"recreate", [boolV he, boolV sd, natV tg, natV ca, boolV sm]⟩ = none) ∧
    (x.st = 0 → step M p (enc x) ⟨"recreate", [boolV he, boolV sd, natV tg, natV ca, boolV sm]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_recreate]; apply ite_none; intro c; obtain ⟨-, -, c3, -, -, c6, c7, c8, c9⟩ := c; simp_all)

/-- Theorem 5: an accepted step never changes the target generation, the
proposal flag, or the bound card revision of a request that exists. -/
theorem proposal_immutable {p : Params} {x y : Rq} {e : Event} {s' : State} (hx : x.st ≠ 0)
    (h : step M p (enc x) e = some s') (hy : s' = enc y) :
    y.tgen = x.tgen ∧ y.prop = x.prop ∧ y.prev = x.prev := by
  rcases accepted_cases h with ⟨tk, ir, ca, rfl⟩ | ⟨he, sd, tg, pr, ir, rfl⟩ |
    ⟨he, sd, tg, ca, sm, rfl⟩ | ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩ | ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩ |
    ⟨ng, rfl⟩ | ⟨un, ho, cg, wi, ca, ma, rfl⟩ | ⟨co, fo, im, cg, rfl⟩
  · rw [step_bind] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact ⟨rfl, rfl, rfl⟩
  · rw [step_create] at h; obtain ⟨hc, -⟩ := ite_some_enc h
    obtain ⟨-, -, -, h0, -⟩ := hc; exact absurd h0 hx
  · rw [step_recreate] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact ⟨rfl, rfl, rfl⟩
  · rw [step_answerSame] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨rfl, rfl, rfl⟩
  · rw [step_answer] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact ⟨rfl, rfl, rfl⟩
  · rw [step_supersede] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨rfl, rfl, rfl⟩
  · rw [step_prepare] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact ⟨rfl, rfl, rfl⟩
  · rw [step_complete] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨rfl, rfl, rfl⟩

/-- Theorem 5: the formal preparation of the dispatch of a proposal needs one
proposal with the dispatch ID (`unique`), the holder, a proposal that targets
the holder's generation and is open or accepted, an acceptance window that is
not over, the card at the bound revision, and material equal to the
proposal's bytes. -/
theorem prepare_spec (p : Params) (x : Rq) (un ho : Bool) (cg : Nat) (wi : Bool) (ca : Nat) (ma : Bool) :
    step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩ ≠ none ↔
      cg ≤ p "maxGen" ∧ ca ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧ un = true ∧ ho = true ∧ x.prop = true ∧
        x.tgen = cg ∧ (x.st = 1 ∨ x.st = 2) ∧ wi = true ∧ x.prev = ca ∧ ma = true := by
  rw [step_prepare]
  by_cases h : cg ≤ p "maxGen" ∧ ca ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧ un = true ∧ ho = true ∧
    x.prop = true ∧ x.tgen = cg ∧ (x.st = 1 ∨ x.st = 2) ∧ wi = true ∧ x.prev = ca ∧ ma = true <;> simp [h]

/-- Theorem 5: a prepared proposal is `accepted`, and its revision is one
higher, also when it already was accepted. -/
theorem prepare_result (p : Params) (x : Rq) (un ho : Bool) (cg : Nat) (wi : Bool) (ca : Nat) (ma : Bool)
    {s' : State}
    (h : step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩ = some s') :
    s' = enc { x with st := 2, rev := x.rev + 1 } := by
  rw [step_prepare] at h
  exact (ite_some_enc h).2

/-- Theorem 5: the revision of an accepted proposal grows again when it is
prepared, so a revision that grows does not mean the status changed. -/
theorem prepare_bumps_accepted {p : Params} {x y : Rq} {un ho : Bool} {cg : Nat} {wi : Bool} {ca : Nat}
    {ma : Bool} {s' : State} (hx : x.st = 2)
    (h : step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩ = some s')
    (hy : s' = enc y) : y.st = x.st ∧ y.rev = x.rev + 1 := by
  rw [prepare_result p x un ho cg wi ca ma h] at hy
  have := enc_inj hy
  subst this
  exact ⟨hx.symm, rfl⟩

/-- Theorem 5: a proposal is prepared only for the material it proposed, while
the card is at the revision it bound and the window is open. -/
theorem prepare_binds (p : Params) (x : Rq) (un ho : Bool) (cg : Nat) (wi : Bool) (ca : Nat) (ma : Bool)
    (h : step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩ ≠ none) :
    x.prev = ca ∧ wi = true ∧ ma = true ∧ un = true := by
  rw [prepare_spec] at h
  obtain ⟨-, -, -, hun, -, -, -, -, hwi, hprev, hma⟩ := h
  exact ⟨hprev, hwi, hma, hun⟩

/-- Theorem 5: the preparation is refused for an ambiguous dispatch ID (more
than one proposal), without the holder, for another generation, for a request
that is not open or accepted, after the window, for another card revision, and
for other material. -/
theorem prepare_refused (p : Params) (x : Rq) (un ho : Bool) (cg : Nat) (wi : Bool) (ca : Nat) (ma : Bool) :
    (un = false → step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩ =
      none) ∧
    (ho = false → step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩ =
      none) ∧
    (x.tgen ≠ cg → step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca,
      boolV ma]⟩ = none) ∧
    (x.st ≠ 1 ∧ x.st ≠ 2 → step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca,
      boolV ma]⟩ = none) ∧
    (wi = false → step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩ =
      none) ∧
    (x.prev ≠ ca → step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca,
      boolV ma]⟩ = none) ∧
    (ma = false → step M p (enc x) ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩ =
      none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_prepare]; apply ite_none; intro c
     obtain ⟨-, -, -, c4, c5, -, c7, c8, c9, c10, c11⟩ := c; simp_all; try omega)

/-! ## Theorem 6: completing a dispatch settles its proposal -/

/-- Theorem 6: the completion of a dispatch settles the accepted proposal of
that dispatch whose generation is the claim's, when the claim record exists. -/
theorem complete_spec (p : Params) (x : Rq) (co fo im : Bool) (cg : Nat) :
    step M p (enc x) ⟨"complete", [boolV co, boolV fo, boolV im, natV cg]⟩ ≠ none ↔
      cg ≤ p "maxGen" ∧ x.rev + 1 ≤ p "maxRev" ∧ co = true ∧ fo = true ∧ im = true ∧ x.prop = true ∧
        x.st = 2 ∧ x.tgen = cg := by
  rw [step_complete]
  by_cases h : cg ≤ p "maxGen" ∧ x.rev + 1 ≤ p "maxRev" ∧ co = true ∧ fo = true ∧ im = true ∧
    x.prop = true ∧ x.st = 2 ∧ x.tgen = cg <;> simp [h]

theorem complete_result (p : Params) (x : Rq) (co fo im : Bool) (cg : Nat) {s' : State}
    (h : step M p (enc x) ⟨"complete", [boolV co, boolV fo, boolV im, natV cg]⟩ = some s') :
    s' = enc { x with st := 3, rev := x.rev + 1 } := by
  rw [step_complete] at h
  exact (ite_some_enc h).2

/-- Theorem 6: no other request is settled: a dispatch that is not completed,
a missing claim record, another dispatch, a request that is not accepted, and
another generation all leave the request as it is. -/
theorem complete_refused (p : Params) (x : Rq) (co fo im : Bool) (cg : Nat) :
    (co = false → step M p (enc x) ⟨"complete", [boolV co, boolV fo, boolV im, natV cg]⟩ = none) ∧
    (fo = false → step M p (enc x) ⟨"complete", [boolV co, boolV fo, boolV im, natV cg]⟩ = none) ∧
    (im = false → step M p (enc x) ⟨"complete", [boolV co, boolV fo, boolV im, natV cg]⟩ = none) ∧
    (x.st ≠ 2 → step M p (enc x) ⟨"complete", [boolV co, boolV fo, boolV im, natV cg]⟩ = none) ∧
    (x.tgen ≠ cg → step M p (enc x) ⟨"complete", [boolV co, boolV fo, boolV im, natV cg]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_complete]; apply ite_none; intro c; obtain ⟨-, -, c3, c4, c5, -, c7, c8⟩ := c; simp_all)

/-! ## Theorem 9: the invariant -/

/-- The safety properties of a request: a missing request is empty; an
existing one has a revision that counts its creation and every answer; the
proposal flag and the bound card revision go together. -/
structure Props (x : Rq) : Prop where
  noneEmpty : x.st = 0 → x.rev = 0 ∧ x.tgen = 0 ∧ x.prop = false ∧ x.prev = 0
  exists_rev : x.st ≠ 0 → 1 ≤ x.rev
  answered : 2 ≤ x.st → 2 ≤ x.rev
  bound : x.prop = true ↔ x.prev ≠ 0

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, 0, false, 0⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨by simp, by simp, by simp, by simp⟩

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hs, hr, hg, hv⟩ := shape hc
  obtain ⟨P1, P2, P3, P4⟩ := hp x rfl
  intro y hy
  rcases accepted_cases h with ⟨tk, ir, ca, rfl⟩ | ⟨he, sd, tg, pr, ir, rfl⟩ |
    ⟨he, sd, tg, ca, sm, rfl⟩ | ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩ | ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩ |
    ⟨ng, rfl⟩ | ⟨un, ho, cg, wi, ca, ma, rfl⟩ | ⟨co, fo, im, cg, rfl⟩
  · rw [step_bind] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨P1, P2, P3, P4⟩
  · rw [step_create] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, -, -, hb⟩ := hc
    refine ⟨by simp, fun _ => by simp, fun h2 => by dsimp only at h2; omega, hb⟩
  · rw [step_recreate] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨P1, P2, P3, P4⟩
  · rw [step_answerSame] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨P1, P2, P3, P4⟩
  · rw [step_answer] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, -, ⟨hA, -, hA3, -, hA5, -, -⟩, -, hset⟩ := hc
    refine ⟨fun h0 => by dsimp only at h0; omega, fun _ => ?_, fun _ => ?_, P4⟩ <;> dsimp only <;>
      have := P2 hA3 <;> omega
  · rw [step_supersede] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, hst⟩ := hc
    refine ⟨fun h0 => by dsimp only at h0; omega, fun _ => ?_, fun _ => ?_, P4⟩ <;> dsimp only <;>
      have := P2 (by omega) <;> omega
  · rw [step_prepare] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, -, -, -, hst, -⟩ := hc
    refine ⟨fun h0 => by dsimp only at h0; omega, fun _ => ?_, fun _ => ?_, P4⟩ <;> dsimp only <;>
      have := P2 (by omega) <;> omega
  · rw [step_complete] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, -, -, hst, -⟩ := hc
    refine ⟨fun h0 => by dsimp only at h0; omega, fun _ => ?_, fun _ => ?_, P4⟩ <;> dsimp only <;>
      have := P2 (by omega) <;> omega

/-- The server request machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant, for every value
of `maxGen` and `maxRev`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 9: the revision of a request that exists is at least 1. -/
theorem reachable_revision {p : Params} {x : Rq} (hr : Reachable M p (enc x)) (hx : x.st ≠ 0) : 1 ≤ x.rev :=
  ((reachable_inv hr).2 x rfl).exists_rev hx

/-- Theorem 9: an answered request has been written at least twice, once to
create it and once for its answer. -/
theorem reachable_answered {p : Params} {x : Rq} (hr : Reachable M p (enc x)) (hx : 2 ≤ x.st) : 2 ≤ x.rev :=
  ((reachable_inv hr).2 x rfl).answered hx

/-- Theorem 9: an accepted or settled proposal binds a card revision. -/
theorem reachable_proposal_bound {p : Params} {x : Rq} (hr : Reachable M p (enc x)) (hp : x.prop = true) :
    x.prev ≠ 0 := (((reachable_inv hr).2 x rfl).bound).mp hp

end KanderFormal.Proofs.ServerRequest
