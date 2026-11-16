import KanderFormal.Proofs.DispatchSafety

/-
Proof obligations of the dispatch round machine for the wrap-up grant and the
settlement of a completed binding (theorems 10 and 11). They hold for every
value of `maxRound` and `maxEpoch`.
-/
namespace KanderFormal.Proofs.Dispatch

open KanderFormal KanderFormal.Machines.Dispatch KanderFormal.Proofs.Scalar

/-! ## Theorem 10: the wrap-up grant -/

/-- A refused event is reported as refused and leaves the state unchanged. -/
def Refused (p : Params) (x : Dp) (e : Event) : Prop := run M p (enc x) e = (enc x, false)

theorem refused_of_none {p : Params} {x : Dp} {e : Event} (h : step M p (enc x) e = none) : Refused p x e := by
  unfold Refused run
  simp [h]

/-- Theorem 10 (1): `authorizeWrapUp` is accepted only for an open round of kind
wrap-up on a card in `working` or `review` (`cl` is 0 or 1) that holds no grant,
with a fresh confirmed exit (`fr`) whose outcome is `stopped` (`ot` is 0), or
`reclaimed` (`ot` is 1) with a decision (`dc`). It raises the epoch by one,
returns the round to `prepared` with both receipts cleared, and sets the grant. -/
theorem authorize_spec {p : Params} {x y : Dp} {ar ae cl ot : Nat} {cs rm nm cy dc fr ev : Bool} {s' : State}
    (h : step M p (enc x) ⟨"authorizeWrapUp", [natV ar, natV ae, boolV cs, boolV rm, enumV cl, boolV nm,
        boolV cy, enumV ot, boolV dc, boolV fr, boolV ev]⟩ = some s')
    (hy : s' = enc y) :
    x.kd = 2 ∧ Open x ∧ ¬Term x ∧ x.grant = false ∧ (cl = 0 ∨ cl = 1) ∧ fr = true ∧ (ot = 0 ∨ ot = 1 ∧ dc = true) ∧
      y.epoch = x.epoch + 1 ∧ y.ph = 1 ∧ y.racc = false ∧ y.rcmp = false ∧ y.grant = true ∧ y.round = x.round ∧
      y.bound = true ∧ y.kd = x.kd := by
  rw [step_authorize] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, hc3, -, -, hf, hk, ho, hg, -, hcl, -, -, hot, hfr, -, -⟩ := hc
  refine ⟨hk, ho, ?_, hg, by omega, hfr, hot, rfl, rfl, rfl, rfl, rfl, rfl, hf.1, rfl⟩
  unfold Open at ho
  unfold Term
  omega

/-- Theorem 10 (2): while a round holds a wrap-up grant, `attempt`, `reauthorize`,
and a second `authorizeWrapUp` are refused, and so are a full-execution write and
a write with a contract decision on the card the grant binds. A refused event
leaves the state unchanged. -/
theorem grant_refused (p : Params) (x : Dp) (hg : x.grant = true) :
    (∀ ar ae cs cy ev bf, Refused p x ⟨"attempt", [natV ar, natV ae, boolV cs, boolV cy, boolV ev, boolV bf]⟩) ∧
    (∀ ar ae cs rm cy fr bf, Refused p x ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy,
        boolV fr, boolV bf]⟩) ∧
    (∀ ar ae cs rm cl nm cy ot dc fr ev, Refused p x ⟨"authorizeWrapUp", [natV ar, natV ae, boolV cs, boolV rm,
        enumV cl, boolV nm, boolV cy, enumV ot, boolV dc, boolV fr, boolV ev]⟩) ∧
    (x.bound = true → Refused p x ⟨"full", []⟩) ∧
    (x.bound = true → ∀ sp ap pa sm,
      Refused p x ⟨"wrapUpWrite", [boolV true, boolV sp, boolV ap, boolV pa, boolV sm]⟩) := by
  refine ⟨?_, ?_, ?_, ?_, ?_⟩
  · intro ar ae cs cy ev bf; apply refused_of_none; rw [step_attempt]; simp [hg]
  · intro ar ae cs rm cy fr bf; apply refused_of_none; rw [step_reauthorize]; simp [hg]
  · intro ar ae cs rm cl nm cy ot dc fr ev; apply refused_of_none; rw [step_authorize]; simp [hg]
  · intro hb; apply refused_of_none; rw [step_full]; simp [hb, hg]
  · intro hb sp ap pa sm; apply refused_of_none; rw [step_wrapUpWrite]; simp [hb, hg]

/-- Theorem 10 (2): a write the grant allows is a record: no contract decision, and
either an append to `spec.md` or the dedicated record path, which holds no other
content. -/
theorem wrapUpWrite_spec {p : Params} {x y : Dp} {ct sp ap pa sm : Bool} {s' : State}
    (h : step M p (enc x) ⟨"wrapUpWrite", [boolV ct, boolV sp, boolV ap, boolV pa, boolV sm]⟩ = some s')
    (hy : s' = enc y) (hb : x.bound = true) (hg : x.grant = true) :
    ct = false ∧ (sp = true → ap = true) ∧ (sp = false → pa = true ∧ sm = true) ∧ y = x := by
  rw [step_wrapUpWrite] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  simp only [hb, hg] at hc
  simp at hc
  obtain ⟨hct, hr⟩ := hc
  refine ⟨hct, ?_, ?_, rfl⟩ <;> cases sp <;> simp_all

/-- Theorem 10 (3): after a grant, the requests that present the epoch before it
are refused: `accept`, `complete`, `end`, and the writes of the executor. -/
theorem authorize_fences_previous {p : Params} {x y : Dp} {ar ae cl ot : Nat} {cs rm nm cy dc fr ev : Bool}
    {s' : State}
    (h : step M p (enc x) ⟨"authorizeWrapUp", [natV ar, natV ae, boolV cs, boolV rm, enumV cl, boolV nm,
        boolV cy, enumV ot, boolV dc, boolV fr, boolV ev]⟩ = some s')
    (hy : s' = enc y) (br : Nat) (rc cy' bf : Bool) :
    step M p (enc y) ⟨"accept", [natV br, natV x.epoch, boolV rc, boolV cy', boolV bf]⟩ = none ∧
    (∀ td cm ev dp sm cl, step M p (enc y) ⟨"complete", [natV br, natV x.epoch, boolV rc, boolV cy', boolV td,
        boolV cm, boolV ev, boolV dp, boolV sm, enumV cl]⟩ = none) ∧
    (∀ cs rm dc tf, step M p (enc y) ⟨"end", [natV br, natV x.epoch, boolV cs, boolV rm, boolV cy', boolV dc,
        boolV tf]⟩ = none) ∧
    (∀ au, step M p (enc y) ⟨"exec", [natV br, natV x.epoch, boolV rc, boolV bf, boolV au]⟩ = none) := by
  obtain ⟨-, -, -, -, -, -, -, he, -, -, -, -, -, hb, -⟩ := authorize_spec h hy
  obtain ⟨a1, a2, a3, a4⟩ := fence_refused p y br x.epoch (fun ⟨_, _, h⟩ => by omega) rc cy' bf
  exact ⟨a1, a2, a3, fun au => a4 au (Or.inl hb)⟩

/-- Theorem 10 (3): a round that holds a grant completes only by a move into
`done`, in every state that satisfies the invariant. -/
theorem grant_complete_done {p : Params} {x : Dp} (hp : Props x) (hg : x.grant = true) {ar ae : Nat}
    {rc cy td cm ev dp sm : Bool} {cl : Nat} {s' : State}
    (h : step M p (enc x) ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td, boolV cm, boolV ev,
        boolV dp, boolV sm, enumV cl]⟩ = some s') : td = true :=
  (complete_kind h).1 (hp.grantKind hg).1

/-- Theorem 10 (3): right after the grant, a `complete` that presents the epoch of
the grant is accepted only as a move into `done`. -/
theorem authorize_complete_done {p : Params} {x y : Dp} {ar ae cl ot : Nat}
    {cs rm nm cy dc fr ev : Bool} {s' : State}
    (h : step M p (enc x) ⟨"authorizeWrapUp", [natV ar, natV ae, boolV cs, boolV rm, enumV cl, boolV nm,
        boolV cy, enumV ot, boolV dc, boolV fr, boolV ev]⟩ = some s')
    (hy : s' = enc y) {br be : Nat} {rc cy' td cm ev' dp sm : Bool} {cl' : Nat} {s'' : State}
    (h2 : step M p (enc y) ⟨"complete", [natV br, natV be, boolV rc, boolV cy', boolV td, boolV cm, boolV ev',
        boolV dp, boolV sm, enumV cl']⟩ = some s'') : td = true := by
  obtain ⟨hk, -, -, -, -, -, -, -, -, -, -, -, -, -, hk'⟩ := authorize_spec h hy
  exact (complete_kind h2).1 (by omega)

/-! ## Theorem 11: settlement -/

/-- Theorem 11 (4): `SettleDispatch` settles the current binding of a completed
round that is not settled, of a card in `working` or `review` (`cl` is 0 or 1).
The round stays completed and keeps its pair; the binding goes away. -/
theorem settle_spec {p : Params} {x y : Dp} {ar ae cl : Nat} {cs cy : Bool} {s' : State}
    (h : step M p (enc x) ⟨"settle", [natV ar, natV ae, boolV cs, enumV cl, boolV cy]⟩ = some s')
    (hy : s' = enc y) :
    Fenced x ar ae ∧ x.ph = 4 ∧ x.settled = false ∧ (cl = 0 ∨ cl = 1) ∧
      y.ph = 4 ∧ y.bound = false ∧ y.settled = true ∧ y.round = x.round ∧ y.epoch = x.epoch ∧ y.rel = x.rel := by
  rw [step_settle] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, hc3, -, hf, h4, hs, hcl, -⟩ := hc
  exact ⟨hf, h4, hs, by omega, h4, rfl, rfl, rfl, rfl, rfl⟩

/-- Theorem 11 (4): a move into `archived` or `trash` of a completed binding
settles it, in every state that satisfies the invariant: the round is not settled
yet because a settlement clears the binding. -/
theorem terminate_settle_spec {p : Params} {x y : Dp} (hp : Props x) {rs ao cy : Bool} {s' : State}
    (h : step M p (enc x) ⟨"terminateSettle", [boolV rs, boolV ao, boolV cy]⟩ = some s')
    (hy : s' = enc y) :
    x.bound = true ∧ x.ph = 4 ∧ x.settled = false ∧
      y.ph = 4 ∧ y.bound = false ∧ y.settled = true ∧ y.round = x.round ∧ y.epoch = x.epoch ∧ y.rel = x.rel := by
  rw [step_terminateSettle] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨hb, -, -, -, h4⟩ := hc
  refine ⟨hb, h4, ?_, h4, rfl, rfl, rfl, rfl, rfl⟩
  cases hq : x.settled
  · rfl
  · have := (hp.settledSpec hq).2.1; simp_all

/-- Theorem 11 (4): the settlement of a takeover handoff settles the same way. -/
theorem takeover_settle_spec {p : Params} {x y : Dp} {ar ae : Nat} {dc rv rc cy : Bool} {s' : State}
    (h : step M p (enc x) ⟨"takeoverSettle", [natV ar, natV ae, boolV dc, boolV rv, boolV rc, boolV cy]⟩ =
      some s')
    (hy : s' = enc y) :
    Fenced x ar ae ∧ x.ph = 4 ∧ x.settled = false ∧
      y.ph = 4 ∧ y.bound = false ∧ y.settled = true ∧ y.round = x.round ∧ y.epoch = x.epoch ∧ y.rel = x.rel := by
  rw [step_takeoverSettle] at h
  obtain ⟨hc, rfl⟩ := ite_some_inv h
  have := enc_inj hy
  subst this
  obtain ⟨-, -, hf, -, -, -, -, h4, hs⟩ := hc
  exact ⟨hf, h4, hs, h4, rfl, rfl, rfl, rfl, rfl⟩

/-- Theorem 11 (4): a round settles at most once: in a settled round all three
settlements are refused. -/
theorem settle_once (p : Params) (x : Dp) (hp : Props x) (hs : x.settled = true) :
    (∀ ar ae cs cl cy, Refused p x ⟨"settle", [natV ar, natV ae, boolV cs, enumV cl, boolV cy]⟩) ∧
    (∀ rs ao cy, Refused p x ⟨"terminateSettle", [boolV rs, boolV ao, boolV cy]⟩) ∧
    (∀ ar ae dc rv rc cy, Refused p x ⟨"takeoverSettle", [natV ar, natV ae, boolV dc, boolV rv, boolV rc,
        boolV cy]⟩) := by
  have hb := (hp.settledSpec hs).2.1
  refine ⟨?_, ?_, ?_⟩
  · intro ar ae cs cl cy; apply refused_of_none; rw [step_settle]; simp [hs]
  · intro rs ao cy; apply refused_of_none; rw [step_terminateSettle]; simp [hb]
  · intro ar ae dc rv rc cy; apply refused_of_none; rw [step_takeoverSettle]; simp [hs]

/-- Theorem 11 (5): in a settled round, the requests that present the pair of the
round (`ar` is its round) are refused: `accept`, `complete`, `end`, and the writes
of the executor, with the card binding gone. -/
theorem settled_fences (p : Params) (x : Dp) (hp : Props x) (hs : x.settled = true) (ar ae : Nat)
    (hr : ar = x.round) (rc cy bf : Bool) :
    Refused p x ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩ ∧
    (∀ td cm ev dp sm cl, Refused p x ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td,
        boolV cm, boolV ev, boolV dp, boolV sm, enumV cl]⟩) ∧
    (∀ cs rm dc tf, Refused p x ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc,
        boolV tf]⟩) ∧
    (∀ au, Refused p x ⟨"exec", [natV ar, natV ae, boolV rc, boolV bf, boolV au]⟩) := by
  obtain ⟨h4, hb, -⟩ := hp.settledSpec hs
  have hr0 : x.round ≠ 0 := fun h0 => by have := hp.phaseRound.2 h0; omega
  obtain ⟨a1, a2, a3, a4⟩ := fence_refused p x ar ae (fun ⟨h, _, _⟩ => by simp_all) rc cy bf
  exact ⟨refused_of_none a1, fun td cm ev dp sm cl => refused_of_none (a2 td cm ev dp sm cl),
    fun cs rm dc tf => refused_of_none (a3 cs rm dc tf), fun au => refused_of_none (a4 au (Or.inr (Or.inl (by omega))))⟩

/-- Theorem 11 (6): a release and a settlement exclude each other: no reachable
round is both, a released round refuses every settlement, and a settled round
refuses `end`. -/
theorem release_settle_exclusive {p : Params} {x : Dp} (hr : Reachable M p (enc x)) :
    ¬(x.rel = true ∧ x.settled = true) ∧
    (x.rel = true →
      (∀ ar ae cs cl cy, Refused p x ⟨"settle", [natV ar, natV ae, boolV cs, enumV cl, boolV cy]⟩) ∧
      (∀ rs ao cy, Refused p x ⟨"terminateSettle", [boolV rs, boolV ao, boolV cy]⟩) ∧
      (∀ ar ae dc rv rc cy, Refused p x ⟨"takeoverSettle", [natV ar, natV ae, boolV dc, boolV rv, boolV rc,
          boolV cy]⟩)) ∧
    (x.settled = true → ∀ ar ae cs rm cy dc tf,
      Refused p x ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc, boolV tf]⟩) := by
  have hp := (reachable_inv hr).2 x rfl
  refine ⟨fun ⟨h1, h2⟩ => by have := (hp.settledSpec h2).2.2; simp_all, fun hrl => ?_, fun hs => ?_⟩
  · obtain ⟨hb, hl⟩ := hp.releasedUnbound hrl
    unfold Leg at hl
    refine ⟨?_, ?_, ?_⟩
    · intro ar ae cs cl cy; apply refused_of_none; rw [step_settle]; simp [Fenced, hb]
    · intro rs ao cy; apply refused_of_none; rw [step_terminateSettle]; simp [hb]
    · intro ar ae dc rv rc cy; apply refused_of_none; rw [step_takeoverSettle]; simp [Fenced, hb]
  · obtain ⟨-, hb, -⟩ := hp.settledSpec hs
    intro ar ae cs rm cy dc tf
    apply refused_of_none; rw [step_end]; simp [Fenced, hb]

/-- Theorem 11 (7): a grant belongs to a round of kind wrap-up, in every
reachable state. -/
theorem reachable_grant_kind {p : Params} {x : Dp} (hr : Reachable M p (enc x)) (hg : x.grant = true) :
    x.kd = 2 := (((reachable_inv hr).2 x rfl).grantKind hg).1

/-- Theorem 8 (extended): with a plan cycle that differs from the round's,
`authorizeWrapUp`, `settle`, `terminateSettle`, and `takeoverSettle` are refused. -/
theorem cycle_refused_grant (p : Params) (x : Dp) (ar ae : Nat) (b1 b2 b3 b4 b5 b6 : Bool) (cl ot : Nat)
    (rs ao : Bool) :
    step M p (enc x) ⟨"authorizeWrapUp", [natV ar, natV ae, boolV b1, boolV b2, enumV cl, boolV b3, boolV false,
        enumV ot, boolV b4, boolV b5, boolV b6]⟩ = none ∧
    step M p (enc x) ⟨"settle", [natV ar, natV ae, boolV b1, enumV cl, boolV false]⟩ = none ∧
    step M p (enc x) ⟨"terminateSettle", [boolV rs, boolV ao, boolV false]⟩ = none ∧
    step M p (enc x) ⟨"takeoverSettle", [natV ar, natV ae, boolV b1, boolV b2, boolV b3, boolV false]⟩ = none := by
  refine ⟨?_, ?_, ?_, ?_⟩
  · rw [step_authorize]; simp
  · rw [step_settle]; simp
  · rw [step_terminateSettle]; simp
  · rw [step_takeoverSettle]; simp

end KanderFormal.Proofs.Dispatch
