import KanderFormal.Proofs.DispatchForms

/-
Proof obligations of the dispatch round machine (theorems 1 to 9): rounds, the
authorization pair, acceptance, replays, terminal phases, ending a round,
reauthorization, cycles, and the invariant. They hold for every value of
`maxRound` and `maxEpoch`.
-/
namespace KanderFormal.Proofs.Dispatch

open KanderFormal KanderFormal.Machines.Dispatch KanderFormal.Proofs.Scalar

/-! ## Theorem 1: one non-terminal binding -/

/-- Theorem 1: a `prepare` that creates a round is accepted only when the card
has no binding or its binding is terminal, so a card never holds two open
rounds. -/
theorem prepare_single {p : Params} {x : Dp} {kd cl : Nat} {free room : Bool} {s' : State}
    (h : step M p (enc x) ⟨"prepare", [enumV kd, enumV cl, boolV free, boolV room]⟩ = some s') :
    x.bound = false ∨ Term x := by
  rw [step_prepare] at h
  obtain ⟨hc, -⟩ := ite_some_inv h
  exact hc.2.2.2.2.1

/-! ## Theorem 2: the authorization pair -/

/-- The pair of the dispatch record. -/
def PairLe (x y : Dp) : Prop := x.round < y.round ∨ x.round = y.round ∧ x.epoch ≤ y.epoch

/-- Theorem 2 (monotone): no accepted event lowers the pair `(round, epoch)` of
the dispatch record in lexicographic order. -/
theorem pair_monotone {p : Params} {x y : Dp} {s' : State} {e : Event} (h : step M p (enc x) e = some s')
    (hy : s' = enc y) : PairLe x y := by
  unfold PairLe
  rcases forms h hy with h0 | ⟨hr, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨hr, he, -⟩ | ⟨h1, -⟩ |
    ⟨h1, -⟩ | ⟨h1, -⟩
  · subst h0; right; simp
  · left; omega
  · subst h1; right; simp
  · subst h1; right; simp
  · subst h1; right; simp
  · subst h1; right; simp
  · right; omega
  · subst h1; right; simp
  · subst h1; right; simp
  · subst h1; right; simp

/-- Theorem 2 (strict): a `prepare` that creates a round raises the round and
sets the epoch to the card's epoch plus one, which is 1 on a card without
binding. -/
theorem prepare_strict {p : Params} {x y : Dp} {kd cl : Nat} {free room : Bool} {s' : State}
    (h : step M p (enc x) ⟨"prepare", [enumV kd, enumV cl, boolV free, boolV room]⟩ = some s')
    (hy : s' = enc y) : y.round = x.round + 1 ∧ y.epoch = (if x.bound = true then x.epoch + 1 else 1) := by
  rw [step_prepare] at h
  obtain ⟨-, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  simp

/-- Theorem 2 (strict): a `reauthorize` keeps the round and raises the epoch by
one. -/
theorem reauthorize_strict {p : Params} {x y : Dp} {ar ae : Nat} {cs rm cy fr bf : Bool} {s' : State}
    (h : step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV fr,
        boolV bf]⟩ = some s')
    (hy : s' = enc y) : y.round = x.round ∧ y.epoch = x.epoch + 1 ∧ y.ph = 1 := by
  rw [step_reauthorize] at h
  obtain ⟨-, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  simp

/-- Theorem 2 (fencing): `accept`, `complete`, and the other writes that present
an authorization pair different from the card binding are refused. A refused
event leaves the state unchanged (`run_refused_unchanged`). The binding of a
card without one is the empty pair. -/
theorem fence_refused (p : Params) (x : Dp) (ar ae : Nat)
    (hne : ¬(x.bound = true ∧ ar = x.round ∧ ae = x.epoch)) (rc cy bf : Bool) :
    step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩ = none ∧
    (∀ td cm ev dp sm cl, step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td,
        boolV cm, boolV ev, boolV dp, boolV sm, enumV cl]⟩ = none) ∧
    (∀ cs rm dc tf, step M p (enc x) ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc,
        boolV tf]⟩ = none) ∧
    (∀ au, x.bound = true ∨ ar ≠ 0 ∨ ae ≠ 0 →
      step M p (enc x) ⟨"exec", [natV ar, natV ae, boolV rc, boolV bf, boolV au]⟩ = none) := by
  have hf : ¬Fenced x ar ae := fun ⟨h1, h2, h3, h4, h5⟩ => hne ⟨h1, h4, h5⟩
  refine ⟨?_, ?_, ?_, ?_⟩
  · rw [step_accept]; simp [hf]
  · intro td cm ev dp sm cl; rw [step_complete]; simp [hf]
  · intro cs rm dc tf; rw [step_end]; simp [hf]
  · intro au hb; rw [step_exec]
    simp only [hf, false_and, or_false]
    split
    · rename_i hc
      obtain ⟨-, -, hb0, h0, h0'⟩ := hc
      rcases hb with hb | hb | hb <;> simp_all
    · rfl

/-! ## Theorem 3: acceptance and completion -/

/-- Theorem 3: an `accept` that changes the phase starts from a prepared or
uncertain round, before its acceptance deadline, and ends in `accepted` with
its receipt. -/
theorem accept_change {p : Params} {x y : Dp} {ar ae : Nat} {rc cy bf : Bool} {s' : State}
    (h : step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩ = some s')
    (hy : s' = enc y) (hne : y.ph ≠ x.ph) : Pend x ∧ bf = true ∧ y.ph = 3 ∧ y.racc = true := by
  rw [step_accept] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, -, -, -, h3⟩ := hc
  by_cases hp : Pend x
  · rcases h3 with h3 | h3 | ⟨-, h3⟩
    · exfalso; unfold Pend at hp; omega
    · exfalso; unfold Pend at hp; omega
    · simp [hp, h3]
  · simp [hp] at hne

/-- Theorem 3: a `complete` that changes the phase starts from an accepted round
of a card in `working` (`col` is `working`, index 0) and ends in `completed` with
its receipt. -/
theorem complete_change {p : Params} {x y : Dp} {ar ae : Nat} {rc cy td cm ev dp sm : Bool} {cl : Nat}
    {s' : State}
    (h : step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td, boolV cm, boolV ev,
        boolV dp, boolV sm, enumV cl]⟩ = some s')
    (hy : s' = enc y) (hne : y.ph ≠ x.ph) : x.ph = 3 ∧ cl = 0 ∧ y.ph = 4 ∧ y.rcmp = true := by
  rw [step_complete] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, -, -, -, -, -, -, -, -, h3⟩ := hc
  rcases h3 with ⟨h3, -⟩ | ⟨h3, h4⟩
  · simp [h3] at hne
  · exact ⟨h3, h4, rfl, rfl⟩

/-- Theorem 3: a round of kind `wrap-up` (index 2) completes only by a move into
`done`, and any other kind only by a move into `review`. -/
theorem complete_kind {p : Params} {x : Dp} {ar ae : Nat} {rc cy td cm ev dp sm : Bool} {cl : Nat}
    {s' : State}
    (h : step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td, boolV cm, boolV ev,
        boolV dp, boolV sm, enumV cl]⟩ = some s') :
    (x.kd = 2 → td = true) ∧ (x.kd ≠ 2 → td = false) := by
  rw [step_complete] at h
  obtain ⟨hc, -⟩ := ite_some_inv h
  have hk := hc.2.2.2.2.2.2.1
  cases td <;> simp at hk ⊢ <;> omega

/-! ## Theorem 4: replays -/

/-- Theorem 4: an `accept` of an accepted or completed round is accepted as a
replay and leaves the state unchanged, whatever the deadline says. -/
theorem accept_replay (p : Params) (x : Dp) (ar ae : Nat) (rc cy bf : Bool)
    (hb1 : ar ≤ p "maxRound") (hb2 : ae ≤ p "maxEpoch") (hf : Fenced x ar ae) (hr : rc = true) (hc : cy = true)
    (hp : x.ph = 3 ∨ x.ph = 4) :
    step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩ = some (enc x) := by
  rw [step_accept]
  have hn : ¬Pend x := by unfold Pend; omega
  simp [hb1, hb2, hf, hr, hc, hp, hn]

/-- Theorem 4: a `complete` of a completed round that carries the identical stored
receipt is accepted as a replay and leaves the state unchanged. -/
theorem complete_replay (p : Params) (x : Dp) (ar ae : Nat) (td : Bool) (cl : Nat)
    (hb1 : ar ≤ p "maxRound") (hb2 : ae ≤ p "maxEpoch") (hcl : cl < 3) (hf : Fenced x ar ae)
    (hk : decide (x.kd = 2) = td) (hp : x.ph = 4) (hr : x.rcmp = true) :
    step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV true, boolV true, boolV td, boolV true, boolV true,
        boolV true, boolV true, enumV cl]⟩ = some (enc x) := by
  rw [step_complete]
  have : { x with ph := 4, rcmp := true } = x := by cases x; simp_all
  simp [hb1, hb2, hcl, hf, hk, hp, this]

/-! ## Theorem 5: terminal phases -/

/-- Theorem 5: within one round, a terminal phase never changes: only `prepare`
leaves it, by creating the next round. -/
theorem terminal_stable {p : Params} {x y : Dp} {s' : State} {e : Event} (h : step M p (enc x) e = some s')
    (hy : s' = enc y) (ht : Term x) (hr : y.round = x.round) : y.ph = x.ph := by
  unfold Term at ht
  rcases forms h hy with h0 | ⟨h1, -⟩ | ⟨h1, -, -, hp⟩ | ⟨h1, -, -, ho⟩ | ⟨h1, -, -, hp⟩ | ⟨h1, -, -, hp⟩ |
    ⟨-, -, -, -, -, -, -, -, h4, -, hl, -⟩ | ⟨h1, -, ho⟩ | ⟨h1, -, -, ho, -⟩ | ⟨h1, -, -⟩
  · subst h0; rfl
  · omega
  · exfalso; unfold Pend at hp; omega
  · exfalso; unfold Open at ho; omega
  · subst h1; have : ¬Pend x := by unfold Pend; omega
    simp [this]
  · subst h1; simp; omega
  · apply hl; unfold Leg; omega
  · subst h1; have : ¬Open x := by unfold Open; omega
    simp [this]
  · exfalso; unfold Open at ho; omega
  · subst h1; rfl

/-- Theorem 5: `attempt` and `reauthorize` refuse a terminal round. -/
theorem terminal_refused (p : Params) (x : Dp) (ht : Term x) (ar ae : Nat) (cs cy ev bf rm fr : Bool) :
    step M p (enc x) ⟨"attempt", [natV ar, natV ae, boolV cs, boolV cy, boolV ev, boolV bf]⟩ = none ∧
    step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV fr, boolV bf]⟩ =
      none := by
  have hp : ¬Pend x := by unfold Term at ht; unfold Pend; omega
  have ho : ¬Open x := by unfold Term at ht; unfold Open; omega
  constructor
  · rw [step_attempt]; simp [hp]
  · rw [step_reauthorize]; simp [ho]

/-! ## Theorem 6: ending a round -/

/-- Theorem 6: an accepted `end` of an accepted round carries a decision, a
failed or cancelled round is released only with a decision and the same phase,
and the release unbinds the card. -/
theorem end_spec {p : Params} {x y : Dp} {ar ae : Nat} {cs rm cy dc tf : Bool} {s' : State}
    (h : step M p (enc x) ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc, boolV tf]⟩ =
      some s')
    (hy : s' = enc y) :
    (x.ph = 3 → dc = true) ∧ (Leg x → dc = true ∧ y.ph = x.ph) ∧ y.bound = false ∧ y.rel = true ∧
      x.rel = false ∧ y.round = x.round ∧ y.epoch = x.epoch := by
  rw [step_end] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, -, -, -, hr, -, -, h3, hl⟩ := hc
  refine ⟨fun h => ?_, fun hleg => ?_, rfl, rfl, hr, rfl, rfl⟩
  · rcases h3 with h3 | h3
    · exact absurd h h3
    · exact h3
  · rcases hl with hl | ⟨hd, hi⟩
    · exact absurd hleg hl
    · refine ⟨hd, ?_⟩
      unfold Leg at hleg
      cases tf <;> simp at hi ⊢ <;> omega

/-- Theorem 6: a round whose release was written is never ended again. -/
theorem end_once (p : Params) (x : Dp) (hr : x.rel = true) (ar ae : Nat) (cs rm cy dc tf : Bool) :
    step M p (enc x) ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc, boolV tf]⟩ = none := by
  rw [step_end]; simp [hr]

/-- Theorem 6: only `prepare` clears a release, and it starts the next round: a
round is released at most once. -/
theorem release_once {p : Params} {x y : Dp} {s' : State} {e : Event} (h : step M p (enc x) e = some s')
    (hy : s' = enc y) (hr : x.rel = true) (hs : y.round = x.round) : y.rel = true := by
  rcases forms h hy with h0 | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨h1, -⟩ | ⟨-, -, -, hr', -⟩ | ⟨h1, -⟩ |
    ⟨h1, -⟩ | ⟨h1, -⟩
  · subst h0; exact hr
  · omega
  · subst h1; exact hr
  · subst h1; exact hr
  · subst h1; exact hr
  · subst h1; exact hr
  · exact hr'
  · subst h1; exact hr
  · subst h1; exact hr
  · subst h1; exact hr

/-! ## Theorem 7: reauthorization -/

/-- Theorem 7: an accepted `reauthorize` of an accepted round carries fresh
stopped evidence; it raises the epoch by one, returns the round to `prepared`,
and clears the receipts of its acceptance and completion. -/
theorem reauthorize_spec {p : Params} {x y : Dp} {ar ae : Nat} {cs rm cy fr bf : Bool} {s' : State}
    (h : step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV fr,
        boolV bf]⟩ = some s')
    (hy : s' = enc y) :
    (x.ph = 3 → fr = true) ∧ (x.ph ≠ 3 → bf = true) ∧ y.epoch = x.epoch + 1 ∧ y.ph = 1 ∧
      y.round = x.round ∧ y.racc = false ∧ y.rcmp = false := by
  rw [step_reauthorize] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, -, -, -, -, -, -, h8, -⟩ := hc
  refine ⟨fun h3 => ?_, fun h3 => ?_, rfl, rfl, rfl, rfl, rfl⟩
  · simpa [h3] using h8
  · simpa [h3] using h8

/-! ## Theorem 8: cycles -/

/-- Theorem 8: with a plan cycle that differs from the round's, `attempt`,
`reauthorize`, `accept`, `complete`, `end`, `cancel`, and `retain` are refused;
`prepare` and `exec` carry no cycle. -/
theorem cycle_refused (p : Params) (x : Dp) (ar ae : Nat) (b1 b2 b3 b4 : Bool) (td cm ev dp sm : Bool)
    (cl : Nat) (rs ao : Bool) :
    step M p (enc x) ⟨"attempt", [natV ar, natV ae, boolV b1, boolV false, boolV b2, boolV b3]⟩ = none ∧
    step M p (enc x) ⟨"reauthorize", [natV ar, natV ae, boolV b1, boolV b2, boolV false, boolV b3, boolV b4]⟩ =
      none ∧
    step M p (enc x) ⟨"accept", [natV ar, natV ae, boolV b1, boolV false, boolV b2]⟩ = none ∧
    step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV b1, boolV false, boolV td, boolV cm, boolV ev,
        boolV dp, boolV sm, enumV cl]⟩ = none ∧
    step M p (enc x) ⟨"end", [natV ar, natV ae, boolV b1, boolV b2, boolV false, boolV b3, boolV b4]⟩ = none ∧
    step M p (enc x) ⟨"cancel", [boolV rs, boolV ao, boolV false]⟩ = none ∧
    step M p (enc x) ⟨"retain", [natV ar, natV ae, boolV b1, boolV b2, boolV b3, boolV false]⟩ = none := by
  refine ⟨?_, ?_, ?_, ?_, ?_, ?_, ?_⟩
  · rw [step_attempt]; simp
  · rw [step_reauthorize]; simp
  · rw [step_accept]; simp
  · rw [step_complete]; simp
  · rw [step_end]; simp
  · rw [step_cancel]; simp
  · rw [step_retain]; simp

/-! ## Theorem 9: the invariant -/

/-- The safety properties of the slot: the phase and the epoch exist exactly
once a round exists, a binding names a round, a release belongs to a failed or
cancelled round and an empty binding, an empty binding is the initial state, a
released round, or a settled one, the receipts follow the phase, a grant belongs
to a wrap-up round that exists, and a settlement belongs to a completed round
that is neither bound nor released. -/
structure Props (x : Dp) : Prop where
  phaseRound : x.ph = 0 ↔ x.round = 0
  epochRound : x.epoch = 0 ↔ x.round = 0
  boundLive : x.bound = true → x.ph ≠ 0
  releasedUnbound : x.rel = true → x.bound = false ∧ Leg x
  unbound : x.bound = false → x.ph = 0 ∨ x.rel = true ∨ x.settled = true
  idleReceipts : (x.ph = 0 ∨ x.ph = 1 ∨ x.ph = 2) → x.racc = false ∧ x.rcmp = false
  acceptedReceipts : x.ph = 3 → x.racc = true ∧ x.rcmp = false
  completedReceipts : x.ph = 4 → x.racc = true ∧ x.rcmp = true
  endedReceipts : (x.ph = 5 ∨ x.ph = 6) → x.rcmp = false
  grantKind : x.grant = true → x.kd = 2 ∧ x.ph ≠ 0
  settledSpec : x.settled = true → x.ph = 4 ∧ x.bound = false ∧ x.rel = false

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, false, 0, 0, false, false, false, false, false⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp⟩

macro "props_close" : tactic =>
  `(tactic| (refine ⟨?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_⟩ <;> intros <;> simp_all [Pend, Open, Leg] <;> omega))

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hlt, -⟩ := shape hc
  obtain ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9, P10, P11⟩ := hp x rfl
  intro y hy
  have hrel : x.bound = true → x.rel = false := fun hb => by
    cases hq : x.rel
    · rfl
    · have := (P4 hq).1; simp_all
  have hsl : x.bound = true → x.settled = false := fun hb => by
    cases hq : x.settled
    · rfl
    · have := (P11 hq).2.1; simp_all
  rcases forms h hy with h0 | ⟨hr, hb, hph, hrl, -, he, hra, hrc, hg, hs⟩ | ⟨h1, hb, hr, hp⟩ | ⟨h1, hb, hr, ho⟩ |
    ⟨h1, hb, hr, hp⟩ | ⟨h1, hb, hr, hr'⟩ |
    ⟨hr, he, hb, hrl, hph, hkd, hxb, hxr, hx4, hxrel, -, hra, hrc, hg, hs⟩ | ⟨h1, hb, ho⟩ |
    ⟨h1, hb, hr, ho, hk, hng⟩ | ⟨h1, hb, h4⟩
  · subst h0; exact ⟨P1, P2, P3, P4, P5, P6, P7, P8, P9, P10, P11⟩
  · have : y.epoch ≠ 0 := by rw [he]; split <;> omega
    refine ⟨by omega, by omega, by simp [hph], by simp [hb, hrl], by simp [hb], ?_, ?_, ?_, ?_, ?_, ?_⟩ <;>
      intros <;> simp_all
  · subst h1; have := hrel hb; have := hsl hb; props_close
  · subst h1; have := hrel hb; have := hsl hb; props_close
  · subst h1; have := hrel hb; have := hsl hb
    by_cases hq : x.ph = 1 ∨ x.ph = 2 <;> props_close
  · subst h1; have := hrel hb; have := hsl hb
    have hx : x.ph = 3 ∨ x.ph = 4 := hr'
    rcases hx with hq | hq <;> props_close
  · have hx : x.ph ≠ 0 := P3 hxb
    have hleg := P9
    have := hsl hxb
    have hq : x.ph = 1 ∨ x.ph = 2 ∨ x.ph = 3 ∨ x.ph = 5 ∨ x.ph = 6 := by omega
    refine ⟨by omega, by omega, by simp [hb], fun _ => ⟨hb, by unfold Leg; omega⟩, fun _ => Or.inr (Or.inl hrl),
      ?_, ?_, ?_, ?_, ?_, ?_⟩ <;>
      intros <;> rcases hq with hq | hq | hq | hq | hq <;> simp_all <;> omega
  · subst h1; have := hrel hb; have := hsl hb
    have hx : x.ph = 1 ∨ x.ph = 2 ∨ x.ph = 3 ∨ x.ph = 5 ∨ x.ph = 6 := by
      unfold Open Leg at ho; omega
    rcases hx with hx | hx | hx | hx | hx <;> props_close
  · subst h1; have := hrel hb; have := hsl hb; props_close
  · subst h1; have := hrel hb; props_close

/-- The dispatch round machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant, for every value of
`maxRound` and `maxEpoch`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 9: in every reachable state the receipts follow the phase: none while
a round is prepared or uncertain, the acceptance receipt once accepted, both once
completed, and never a completion receipt on a failed or cancelled round. -/
theorem reachable_receipts {p : Params} {x : Dp} (hr : Reachable M p (enc x)) :
    (x.ph = 0 ∨ x.ph = 1 ∨ x.ph = 2 → x.racc = false ∧ x.rcmp = false) ∧
    (x.ph = 3 → x.racc = true ∧ x.rcmp = false) ∧ (x.ph = 4 → x.racc = true ∧ x.rcmp = true) ∧
    (x.ph = 5 ∨ x.ph = 6 → x.rcmp = false) := by
  have h := (reachable_inv hr).2 x rfl
  exact ⟨h.idleReceipts, h.acceptedReceipts, h.completedReceipts, h.endedReceipts⟩

/-- Theorem 9: in every reachable state a card without binding and without a
settlement has no round or a released one, so `accept`, `complete`, `attempt`,
`reauthorize`, `end`, `cancel`, and `retain` are refused there. The extra premise
only names the states the settlement of a completed binding adds
(`reachable_unbound_settled`); for the states without one the statement is the
one of the machine without settlements. -/
theorem reachable_unbound {p : Params} {x : Dp} (hr : Reachable M p (enc x)) (hb : x.bound = false)
    (hs : x.settled = false) : x.ph = 0 ∨ x.rel = true := by
  rcases ((reachable_inv hr).2 x rfl).unbound hb with h | h | h
  · exact Or.inl h
  · exact Or.inr h
  · simp_all

/-- Theorem 9: in every reachable state a card without binding has no round, a
released one, or a settled one. -/
theorem reachable_unbound_settled {p : Params} {x : Dp} (hr : Reachable M p (enc x))
    (hb : x.bound = false) : x.ph = 0 ∨ x.rel = true ∨ x.settled = true :=
  ((reachable_inv hr).2 x rfl).unbound hb

end KanderFormal.Proofs.Dispatch
