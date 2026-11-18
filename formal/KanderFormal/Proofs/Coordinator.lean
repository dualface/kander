import KanderFormal.Proofs.CoordinatorCases

/-
Proof obligations of the coordinator checkpoint machine (M12). Every statement
holds for every value of `maxRev`, `maxEpoch`, and `members`, so for every
revision, epoch, and number of members: the members enter the state only as the
count of blocked ones, and the facts about them (`changed`, `blockedAfter`,
`reqFail`, `memberBlocked`, `memberOk`) are arguments the shell computes from the
member instances (`coordmember`).

The group directory is removed with its checkpoint by the archive cleanup (M14).
Statements about the epoch and the revision are about one checkpoint while its
group exists; after a cleanup a claim creates a new checkpoint.
-/
namespace KanderFormal.Proofs.Coordinator

open KanderFormal KanderFormal.Machines.Coordinator KanderFormal.Proofs.Scalar

set_option maxHeartbeats 4000000

/-- The bounds a conforming state keeps. -/
def Fits (p : Params) (z : Gp) : Prop :=
  z.rev ≤ p "maxRev" ∧ z.epoch ≤ p "maxEpoch" ∧ z.blocked ≤ p "members"

/-! ## The invariant -/

/-- Without a checkpoint the state is the initial one; with one the epoch is at
least 1 and never above the revision. -/
structure Props (z : Gp) : Prop where
  none : z.exists_ = false → z.rev = 0 ∧ z.epoch = 0 ∧ z.last = false ∧ z.blocked = 0
  some : z.exists_ = true → 1 ≤ z.epoch ∧ z.epoch ≤ z.rev

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ z, s = enc z → Props z

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro z hz
  have : z = ⟨false, 0, 0, false, 0⟩ := enc_inj (hz.symm.trans rfl)
  subst this
  exact ⟨fun _ => ⟨rfl, rfl, rfl, rfl⟩, by simp⟩

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨z, rfl, hr, he, hb⟩ := shape hc
  obtain ⟨P1, P2⟩ := hp z rfl
  intro y hy
  rcases accepted_cases h with ⟨er, ee, sm, ts, rm, mo, mb, rfl⟩ |
    ⟨os, ts, ea, er, sm, mo, mm, rc, rf, ch, ba, rm, rfl⟩ | ⟨er, ee, rm, kn, mb, ex, mo, rfl⟩ |
    ⟨er, ee, rm, kn, un, mb, rem, nd, ba, rfl⟩
  · rw [step_claim p z hr he] at h
    split at h
    · split at h
      · cases h; have := enc_inj hy; subst this; exact ⟨P1, P2⟩
      · split at h
        · rename_i hg; cases h; have := enc_inj hy; subst this
          obtain ⟨⟨h1, h2, -⟩, -⟩ := hg
          by_cases hz : z.exists_ = true
          · obtain ⟨a1, a2⟩ := P2 hz; exact ⟨by simp, fun _ => ⟨by simp, by simp; omega⟩⟩
          · have hz' : z.exists_ = false := by simpa using hz
            obtain ⟨b1, b2, -⟩ := P1 hz'
            exact ⟨by simp, fun _ => ⟨by simp [b2], by simp [b1, b2]⟩⟩
        · cases h
    · cases h
  · rw [step_reconcile p z hr hb] at h
    split at h
    · split at h
      · rename_i hg
        obtain ⟨⟨hex, -⟩, -⟩ := hg
        split at h
        · split at h
          · cases h; have := enc_inj hy; subst this
            obtain ⟨a1, a2⟩ := P2 hex
            exact ⟨by simp [hex], fun _ => ⟨by simpa using a1, by simp; omega⟩⟩
          · cases h
        · cases h; have := enc_inj hy; subst this; exact ⟨P1, P2⟩
      · cases h
    · cases h
  · rw [step_recover p z he hb] at h
    split at h
    · rename_i hg
      cases h; have := enc_inj hy; subst this
      obtain ⟨-, -, -, -, ⟨hex, -⟩, -⟩ := hg
      obtain ⟨a1, a2⟩ := P2 hex
      exact ⟨by simp [hex], fun _ => ⟨by simpa using a1, by simp; omega⟩⟩
    · cases h
  · rw [step_removeMember p z he hb] at h
    split at h
    · rename_i hg
      cases h; have := enc_inj hy; subst this
      obtain ⟨-, -, -, -, -, ⟨hex, -⟩, -⟩ := hg
      obtain ⟨a1, a2⟩ := P2 hex
      exact ⟨by simp [hex], fun _ => ⟨by simpa using a1, by simp; omega⟩⟩
    · cases h

def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

theorem reachable_props {p : Params} {z : Gp} (hr : Reachable M p (enc z)) : Props z := (reachable_inv hr).2 z rfl

theorem reachable_fits {p : Params} {z : Gp} (hr : Reachable M p (enc z)) : Fits p z := by
  obtain ⟨z', hz, h1, h2, h3⟩ := shape (reachable_inv hr).1
  have := enc_inj hz; subst this; exact ⟨h1, h2, h3⟩

/-- Theorem 9: `last` and `blocked` belong to an existing checkpoint, and the
epoch of an existing checkpoint is at least 1 and not above its revision. -/
theorem reachable_exists {p : Params} {z : Gp} (hr : Reachable M p (enc z)) :
    (z.exists_ = false → z.rev = 0 ∧ z.epoch = 0 ∧ z.last = false ∧ z.blocked = 0) ∧
      (z.exists_ = true → 1 ≤ z.epoch ∧ z.epoch ≤ z.rev) := by
  obtain ⟨P1, P2⟩ := reachable_props hr; exact ⟨P1, P2⟩

/-! ## Theorem 1: what each accepted write does to the checkpoint -/

/-- An accepted claim replays (nothing changes), or it raises the revision and
the epoch by one, forgets the last request, and keeps the blocked count. It
needed the revision and epoch it names, room below the ceilings, a token other
than the current one, and the same members. -/
theorem claim_spec {p : Params} {z y : Gp} {s' : State} {er ee : Nat} {sm ts rm mo mb : Bool} (hz : Fits p z)
    (h : step M p (enc z) ⟨"claim", [natV er, natV ee, boolV sm, boolV ts, boolV rm, boolV mo, boolV mb]⟩ = some s')
    (hy : s' = enc y) :
    (Replay z sm ∧ y = z) ∨
      (¬Replay z sm ∧ ClaimOk z er ee ts rm mo mb ∧ y = ⟨true, z.rev + 1, z.epoch + 1, false, z.blocked⟩) := by
  rw [step_claim p z hz.1 hz.2.1] at h
  split at h
  · split at h
    · rename_i hr; cases h; exact .inl ⟨hr, (enc_inj hy).symm⟩
    · rename_i hr; split at h
      · rename_i hg; cases h; exact .inr ⟨hr, hg.1, (enc_inj hy).symm⟩
      · cases h
  · cases h

/-- An accepted reconcile of mirrors that did not change writes nothing; one of
changed mirrors raises the revision by one, remembers the request, and records the
blocked count the mirrors now have. Either needed the authority (owner, token,
epoch), a revision that is the checkpoint's or the retry of the last request, the
member set and group membership, the recovery records, and no request-level
failure. -/
theorem reconcile_spec {p : Params} {z y : Gp} {s' : State} {os ts : Bool} {ea er : Nat}
    {sm mo mm rc rf ch : Bool} {ba : Nat} {rm : Bool} (hz : Fits p z)
    (h : step M p (enc z) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV rf, boolV ch, natV ba, boolV rm]⟩ = some s') (hy : s' = enc y) :
    RecOk z os ts ea mo mm rc rf ∧ (er = z.rev ∨ z.last = true ∧ sm = true) ∧
      (ch = false → y = z) ∧
      (ch = true → rm = true ∧ y = ⟨z.exists_, z.rev + 1, z.epoch, true, ba⟩) := by
  rw [step_reconcile p z hz.1 hz.2.2] at h
  split at h
  · split at h
    · rename_i hg
      by_cases hch : ch = true
      · rw [if_pos hch] at h
        split at h
        · cases h
          have := enc_inj hy; subst this
          refine ⟨hg.1, hg.2.1, by simp [hch], fun _ => ⟨?_, rfl⟩⟩
          rcases hg.2.2 with hc | hc
          · simp [hch] at hc
          · exact hc
        · cases h
      · have hch' : ch = false := by simpa using hch
        rw [if_neg hch] at h
        cases h
        have := enc_inj hy; subst this
        exact ⟨hg.1, hg.2.1, fun _ => rfl, fun h' => absurd h' hch⟩
    · cases h
  · cases h

/-- An accepted recovery needed the revision and epoch it names, an existing
blocked member the shell found at the observed card revision and accepted for
recovery, and room; it raises the revision by one, forgets the last request, and
leaves one member less blocked. -/
theorem recover_spec {p : Params} {z y : Gp} {s' : State} {er ee : Nat} {rm kn mb ex mo : Bool} (hz : Fits p z)
    (h : step M p (enc z) ⟨"recover", [natV er, natV ee, boolV rm, boolV kn, boolV mb, boolV ex, boolV mo]⟩ = some s')
    (hy : s' = enc y) :
    er = z.rev ∧ ee = z.epoch ∧ RecvOk z rm kn mb ex mo ∧
      y = ⟨z.exists_, z.rev + 1, z.epoch, false, z.blocked - 1⟩ := by
  rw [step_recover p z hz.2.1 hz.2.2] at h
  split at h
  · rename_i hg; cases h; exact ⟨hg.2.2.1, hg.2.2.2.1, hg.2.2.2.2.1, (enc_inj hy).symm⟩
  · cases h

/-- An accepted removal needed the revision and epoch it names, an existing
checkpoint, room below the ceiling, a member the shell found in the checkpoint
and on the board, unstarted, with at least one member left and no remaining
member that lists it as a prerequisite, and a blocked count that is not higher
than before. It raises the revision by one, keeps the epoch, forgets the last
request, and records the blocked count the remaining mirrors have. -/
theorem removeMember_spec {p : Params} {z y : Gp} {s' : State} {er ee : Nat} {rm kn un mb rem nd : Bool} {ba : Nat}
    (hz : Fits p z)
    (h : step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV kn, boolV un, boolV mb, boolV rem,
        boolV nd, natV ba]⟩ = some s')
    (hy : s' = enc y) :
    er = z.rev ∧ ee = z.epoch ∧ RemOk z rm kn un mb rem nd ∧ ba ≤ z.blocked ∧
      y = ⟨z.exists_, z.rev + 1, z.epoch, false, ba⟩ := by
  rw [step_removeMember p z hz.2.1 hz.2.2] at h
  split at h
  · rename_i hg
    cases h
    exact ⟨hg.2.2.2.1, hg.2.2.2.2.1, hg.2.2.2.2.2.1, hg.2.2.2.2.2.2.1, (enc_inj hy).symm⟩
  · cases h

/-- Theorem 1: a claim of the same hash returns the checkpoint as it is, whatever
revision and epoch it names. -/
theorem claim_replay (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (hr : er ≤ p "maxRev")
    (he : ee ≤ p "maxEpoch") (hx : z.exists_ = true) (ts rm mo mb : Bool) :
    step M p (enc z) ⟨"claim", [natV er, natV ee, boolV true, boolV ts, boolV rm, boolV mo, boolV mb]⟩ =
      some (enc z) := by
  rw [step_claim p z hz.1 hz.2.1]; simp [hr, he, Replay, hx]

/-- Theorem 1: a claim that is not a replay and names another revision or another
epoch than the checkpoint's is refused. -/
theorem claim_refused_cas (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (hc : er ≠ z.rev ∨ ee ≠ z.epoch)
    (ts rm mo mb : Bool) :
    step M p (enc z) ⟨"claim", [natV er, natV ee, boolV false, boolV ts, boolV rm, boolV mo, boolV mb]⟩ = none := by
  rw [step_claim p z hz.1 hz.2.1]; simp [Replay, ClaimOk]
  intro _ _; rcases hc with hc | hc <;> intro h <;> first | exact absurd h hc | intro h2; first | exact absurd h2 hc | skip

/-- Theorem 1: a claim that is not a replay, with the token of the current
authority, is refused. -/
theorem claim_refused_token (p : Params) (z : Gp) (hz : Fits p z) (hx : z.exists_ = true) (er ee : Nat)
    (rm mo mb : Bool) :
    step M p (enc z) ⟨"claim", [natV er, natV ee, boolV false, boolV true, boolV rm, boolV mo, boolV mb]⟩ = none := by
  rw [step_claim p z hz.1 hz.2.1]; simp [Replay, ClaimOk, hx]

/-- Theorem 1: no ceiling room, no claim. -/
theorem claim_refused_room (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (ts mo mb : Bool) :
    step M p (enc z) ⟨"claim", [natV er, natV ee, boolV false, boolV ts, boolV false, boolV mo, boolV mb]⟩ = none := by
  rw [step_claim p z hz.1 hz.2.1]; simp [Replay, ClaimOk]

/-- Theorem 1: an existing checkpoint takes no claim that changes its members. -/
theorem claim_refused_members (p : Params) (z : Gp) (hz : Fits p z) (hx : z.exists_ = true) (er ee : Nat)
    (ts rm mb : Bool) :
    step M p (enc z) ⟨"claim", [natV er, natV ee, boolV false, boolV ts, boolV rm, boolV false, boolV mb]⟩ = none := by
  rw [step_claim p z hz.1 hz.2.1]; simp [Replay, ClaimOk, hx]

/-- Theorem 1: the first claim of a group names revision 0 and epoch 0; any other
claim of a group that has no checkpoint is refused. -/
theorem claim_first_refused {p : Params} {z : Gp} (hr : Reachable M p (enc z)) (hx : z.exists_ = false) (er ee : Nat)
    (hc : er ≠ 0 ∨ ee ≠ 0) (sm ts rm mo mb : Bool) :
    step M p (enc z) ⟨"claim", [natV er, natV ee, boolV sm, boolV ts, boolV rm, boolV mo, boolV mb]⟩ = none := by
  obtain ⟨b1, b2, -⟩ := (reachable_props hr).none hx
  have hz := reachable_fits hr
  rw [step_claim p z hz.1 hz.2.1]
  cases sm <;> simp [Replay, ClaimOk, hx, b1, b2] <;>
    (intro _ _ h1 h2; rcases hc with hc | hc <;> simp_all)

/-- Theorem 1: a reconcile whose authority (owner, token, epoch) is not the
checkpoint's, in any part, is refused. -/
theorem reconcile_refused_authority (p : Params) (z : Gp) (hz : Fits p z) (os ts : Bool) (ea er : Nat)
    (hc : os = false ∨ ts = false ∨ ea ≠ z.epoch) (sm mo mm rc rf ch : Bool) (ba : Nat) (rm : Bool) :
    step M p (enc z) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV rf, boolV ch, natV ba, boolV rm]⟩ = none := by
  rw [step_reconcile p z hz.1 hz.2.2]
  have : ¬(RecOk z os ts ea mo mm rc rf) := by
    intro ⟨_, h1, h2, h3, _⟩; rcases hc with hc | hc | hc <;> simp_all
  simp [this]

/-- Theorem 1: no checkpoint, no reconcile. -/
theorem reconcile_refused_none (p : Params) (z : Gp) (hz : Fits p z) (hx : z.exists_ = false)
    (os ts : Bool) (ea er : Nat) (sm mo mm rc rf ch : Bool) (ba : Nat) (rm : Bool) :
    step M p (enc z) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV rf, boolV ch, natV ba, boolV rm]⟩ = none := by
  rw [step_reconcile p z hz.1 hz.2.2]
  have : ¬(RecOk z os ts ea mo mm rc rf) := by intro ⟨h, _⟩; simp_all
  simp [this]

/-- Theorem 1: a reconcile that read another revision than the checkpoint's is
accepted only as the retry of the last request; with no remembered request, or
another request, it is refused. -/
theorem reconcile_refused_revision (p : Params) (z : Gp) (hz : Fits p z) (os ts : Bool) (ea er : Nat)
    (hr : er ≠ z.rev) (hn : z.last = false ∨ sm = false) (mo mm rc rf ch : Bool) (ba : Nat) (rm : Bool) :
    step M p (enc z) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV rf, boolV ch, natV ba, boolV rm]⟩ = none := by
  rw [step_reconcile p z hz.1 hz.2.2]
  have : ¬((er = z.rev ∨ z.last = true ∧ sm = true)) := by
    intro h; rcases h with h | h <;> rcases hn with hn | hn <;> simp_all
  simp [this]

/-- Theorem 1 and 3: a member that failed at request level (a stale card
revision, a cancelled or timed-out run, changed Git facts, a pending
transaction) makes the whole request refused. -/
theorem reconcile_refused_request_failure (p : Params) (z : Gp) (hz : Fits p z) (os ts : Bool) (ea er : Nat)
    (sm mo mm rc ch : Bool) (ba : Nat) (rm : Bool) :
    step M p (enc z) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV true, boolV ch, natV ba, boolV rm]⟩ = none := by
  rw [step_reconcile p z hz.1 hz.2.2]
  have : ¬(RecOk z os ts ea mo mm rc true) := by intro ⟨_, _, _, _, _, _, _, h⟩; simp at h
  simp [this]

/-- Theorem 1: a recovery that names another revision or epoch than the
checkpoint's is refused (the request carries no token). -/
theorem recover_refused_cas (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (hc : er ≠ z.rev ∨ ee ≠ z.epoch)
    (rm kn mb ex mo : Bool) :
    step M p (enc z) ⟨"recover", [natV er, natV ee, boolV rm, boolV kn, boolV mb, boolV ex, boolV mo]⟩ = none := by
  rw [step_recover p z hz.2.1 hz.2.2]
  have : ¬(er = z.rev ∧ ee = z.epoch) := by intro ⟨h1, h2⟩; rcases hc with hc | hc <;> simp_all
  simp [this]
  intro _ _ h1 h2; exact absurd ⟨h1, h2⟩ this

/-- Theorem 6: a recovery names a blocked member: the shell's member fact, and at
least one blocked member in the count. -/
theorem recover_refused_not_blocked (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (rm kn ex mo : Bool) :
    step M p (enc z) ⟨"recover", [natV er, natV ee, boolV rm, boolV kn, boolV false, boolV ex, boolV mo]⟩ = none := by
  rw [step_recover p z hz.2.1 hz.2.2]; simp [RecvOk]

theorem recover_refused_count (p : Params) (z : Gp) (hz : Fits p z) (hb : z.blocked = 0) (er ee : Nat)
    (rm kn mb ex mo : Bool) :
    step M p (enc z) ⟨"recover", [natV er, natV ee, boolV rm, boolV kn, boolV mb, boolV ex, boolV mo]⟩ = none := by
  rw [step_recover p z hz.2.1 hz.2.2]; simp [RecvOk, hb]

/-- Theorem 1: a removal that names another revision or epoch than the
checkpoint's is refused. -/
theorem removeMember_refused_cas (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (hc : er ≠ z.rev ∨ ee ≠ z.epoch)
    (rm kn un mb rem nd : Bool) (ba : Nat) :
    step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV kn, boolV un, boolV mb, boolV rem, boolV nd,
        natV ba]⟩ = none := by
  rw [step_removeMember p z hz.2.1 hz.2.2]
  have : ¬(er = z.rev ∧ ee = z.epoch) := by intro ⟨h1, h2⟩; rcases hc with hc | hc <;> simp_all
  simp
  intro _ _ _ h1 h2; exact absurd ⟨h1, h2⟩ this

/-- Theorem 1: a removal of a group that has no checkpoint is refused. -/
theorem removeMember_refused_none (p : Params) (z : Gp) (hz : Fits p z) (hx : z.exists_ = false) (er ee : Nat)
    (rm kn un mb rem nd : Bool) (ba : Nat) :
    step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV kn, boolV un, boolV mb, boolV rem, boolV nd,
        natV ba]⟩ = none := by
  rw [step_removeMember p z hz.2.1 hz.2.2]; simp [RemOk, hx]

/-- Theorem 1: a removal of a card that is not a member of the checkpoint, or
whose group on the board is another one, is refused. -/
theorem removeMember_refused_member (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (rm un mb rem nd : Bool)
    (ba : Nat) :
    (step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV false, boolV un, boolV mb, boolV rem,
        boolV nd, natV ba]⟩ = none) ∧
    (step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV true, boolV un, boolV false, boolV rem,
        boolV nd, natV ba]⟩ = none) := by
  rw [step_removeMember p z hz.2.1 hz.2.2, step_removeMember p z hz.2.1 hz.2.2]; simp [RemOk]

/-- Theorem 1: a removal of a member that has started is refused. -/
theorem removeMember_refused_started (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (rm kn mb rem nd : Bool)
    (ba : Nat) :
    step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV kn, boolV false, boolV mb, boolV rem,
        boolV nd, natV ba]⟩ = none := by
  rw [step_removeMember p z hz.2.1 hz.2.2]; simp [RemOk]

/-- Theorem 1: a removal of the last member is refused: the group would have no
member left to continue it. -/
theorem removeMember_refused_last (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat) (rm kn un mb nd : Bool)
    (ba : Nat) :
    step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV kn, boolV un, boolV mb, boolV false,
        boolV nd, natV ba]⟩ = none := by
  rw [step_removeMember p z hz.2.1 hz.2.2]; simp [RemOk]

/-- Theorem 1: a removal of a card a remaining member lists as a prerequisite is
refused, so no group is held up by a card outside it. -/
theorem removeMember_refused_dependents (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat)
    (rm kn un mb rem : Bool) (ba : Nat) :
    step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV kn, boolV un, boolV mb, boolV rem,
        boolV false, natV ba]⟩ = none := by
  rw [step_removeMember p z hz.2.1 hz.2.2]; simp [RemOk]

/-- Theorem 1: a removal with no room below the revision ceiling is refused. -/
theorem removeMember_refused_room (p : Params) (z : Gp) (hz : Fits p z) (er ee : Nat)
    (kn un mb rem nd : Bool) (ba : Nat) :
    step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV false, boolV kn, boolV un, boolV mb, boolV rem,
        boolV nd, natV ba]⟩ = none := by
  rw [step_removeMember p z hz.2.1 hz.2.2]; simp [RemOk]

/-- Theorem 7: a removal never raises the blocked count, and a removal that
records the same count as the checkpoint has keeps the group's wrap-up decision
as it was. -/
theorem removeMember_blocked_not_higher {p : Params} {z y : Gp} {s' : State} {er ee : Nat}
    {rm kn un mb rem nd : Bool} {ba : Nat} (hz : Fits p z)
    (h : step M p (enc z) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV kn, boolV un, boolV mb, boolV rem,
        boolV nd, natV ba]⟩ = some s')
    (hy : s' = enc y) : y.blocked ≤ z.blocked ∧ y.exists_ = z.exists_ := by
  obtain ⟨-, -, -, hb, rfl⟩ := removeMember_spec hz h hy
  exact ⟨hb, rfl⟩

/-- Theorem 1: a claim or a recovery forgets the last request, so a request that
read an older revision is refused afterwards. -/
theorem stale_retry_after_write {p : Params} {z y : Gp} {s' : State} {e : Event} (hz : Fits p z)
    (h : step M p (enc z) e = some s') (hy : s' = enc y) (hw : y.rev ≠ z.rev) (hl : y.last = false)
    (hy' : Fits p y) (os ts : Bool) (ea er : Nat) (hr : er ≠ y.rev) (sm mo mm rc rf ch : Bool) (ba : Nat) (rm : Bool) :
    step M p (enc y) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV rf, boolV ch, natV ba, boolV rm]⟩ = none :=
  reconcile_refused_revision p y hy' os ts ea er hr (Or.inl hl) mo mm rc rf ch ba rm

/-! ## Theorem 2: the epoch -/

/-- Theorem 2: no accepted event lowers the epoch, and an event that changes it is
a claim that raises it by exactly one. -/
theorem epoch_change {p : Params} {z y : Gp} {e : Event} {s' : State} (hz : Fits p z)
    (h : step M p (enc z) e = some s') (hy : s' = enc y) :
    z.epoch ≤ y.epoch ∧ (y.epoch ≠ z.epoch →
      (∃ er ee sm ts rm mo mb,
        e = ⟨"claim", [natV er, natV ee, boolV sm, boolV ts, boolV rm, boolV mo, boolV mb]⟩) ∧
        y.epoch = z.epoch + 1) := by
  rcases accepted_cases h with ⟨er, ee, sm, ts, rm, mo, mb, rfl⟩ |
    ⟨os, ts, ea, er, sm, mo, mm, rc, rf, ch, ba, rm, rfl⟩ | ⟨er, ee, rm, kn, mb, ex, mo, rfl⟩ |
    ⟨er, ee, rm, kn, un, mb, rem, nd, ba, rfl⟩
  · rcases claim_spec hz h hy with ⟨-, rfl⟩ | ⟨-, -, rfl⟩
    · simp
    · exact ⟨by simp, fun _ => ⟨⟨er, ee, sm, ts, rm, mo, mb, rfl⟩, rfl⟩⟩
  · obtain ⟨-, -, h1, h2⟩ := reconcile_spec hz h hy
    cases ch
    · have := h1 rfl; subst this; simp
    · obtain ⟨-, rfl⟩ := h2 rfl; simp
  · obtain ⟨-, -, -, rfl⟩ := recover_spec hz h hy; simp
  · obtain ⟨-, -, -, -, rfl⟩ := removeMember_spec hz h hy; simp

/-- Theorem 2: every accepted event keeps or raises the revision, and never
lowers it. -/
theorem rev_monotone {p : Params} {z y : Gp} {e : Event} {s' : State} (hz : Fits p z)
    (h : step M p (enc z) e = some s') (hy : s' = enc y) : z.rev ≤ y.rev := by
  rcases accepted_cases h with ⟨er, ee, sm, ts, rm, mo, mb, rfl⟩ |
    ⟨os, ts, ea, er, sm, mo, mm, rc, rf, ch, ba, rm, rfl⟩ | ⟨er, ee, rm, kn, mb, ex, mo, rfl⟩ |
    ⟨er, ee, rm, kn, un, mb, rem, nd, ba, rfl⟩
  · rcases claim_spec hz h hy with ⟨-, rfl⟩ | ⟨-, -, rfl⟩ <;> simp
  · obtain ⟨-, -, h1, h2⟩ := reconcile_spec hz h hy
    cases ch
    · have := h1 rfl; subst this; simp
    · obtain ⟨-, rfl⟩ := h2 rfl; simp
  · obtain ⟨-, -, -, rfl⟩ := recover_spec hz h hy; simp
  · obtain ⟨-, -, -, -, rfl⟩ := removeMember_spec hz h hy; simp

/-! ## Theorem 3: the blocked members of a group -/

/-- Theorem 3: a reconcile that wrote a checkpoint records the blocked count the
mirrors have; one that wrote nothing keeps it. -/
theorem reconcile_blocked_count {p : Params} {z y : Gp} {s' : State} {os ts : Bool} {ea er : Nat}
    {sm mo mm rc rf : Bool} {ba : Nat} {rm : Bool} (hz : Fits p z)
    (h : step M p (enc z) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV rf, boolV true, natV ba, boolV rm]⟩ = some s') (hy : s' = enc y) : y.blocked = ba := by
  obtain ⟨-, -, -, h2⟩ := reconcile_spec hz h hy
  obtain ⟨-, rfl⟩ := h2 rfl; rfl

/-- Theorem 3: a reconcile that left every member blocked is accepted: the
checkpoint is written with the count equal to the member count, and the shell then
reports `CoordinatorBlockedError`. The refusal is never the kernel's. -/
theorem reconcile_all_blocked_accepted (p : Params) (z : Gp) (hz : Fits p z) (os ts : Bool) (ea er : Nat)
    (hx : RecOk z os ts ea true true true false) (hr : er = z.rev) (hroom : z.rev + 1 ≤ p "maxRev")
    (he : ea ≤ p "maxEpoch") (hrr : er ≤ p "maxRev") (sm : Bool) :
    step M p (enc z) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV true, boolV true,
        boolV true, boolV false, boolV true, natV (p "members"), boolV true]⟩ =
      some (enc ⟨z.exists_, z.rev + 1, z.epoch, true, p "members"⟩) := by
  rw [step_reconcile p z hz.1 hz.2.2]
  have hr' : z.rev ≤ p "maxRev" := hz.1
  simp [hx, hr, hroom, he, hr']

/-- Theorem 3: a reconcile whose mirrors did not change writes nothing, whatever
the blocked count it computed. -/
theorem reconcile_unchanged_no_write {p : Params} {z y : Gp} {s' : State} {os ts : Bool} {ea er : Nat}
    {sm mo mm rc rf : Bool} {ba : Nat} {rm : Bool} (hz : Fits p z)
    (h : step M p (enc z) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV rf, boolV false, natV ba, boolV rm]⟩ = some s') (hy : s' = enc y) : y = z := by
  obtain ⟨-, -, h1, -⟩ := reconcile_spec hz h hy
  exact h1 rfl

/-! ## Theorem 7: a group with a blocked member -/

/-- The query `wrapUpBlocked` is the decision of `refuseBlockedWrapUp`. -/
theorem wrapUpBlocked_eval (p : Params) (z : Gp) :
    evalQuery M p (enc z) (M.queries[0]!) =
      some (boolV (decide (z.exists_ = true ∧ 1 ≤ z.blocked))) := by
  obtain ⟨e, r, ep, l, b⟩ := z
  cases e <;> simp [evalQuery, M, coordinatorMachine, eval, evalCmp, boolAnd, enc, boolV, natV, f, andE, lookup]

/-- Theorem 7: in every reachable state with a blocked member a checkpoint
exists, so the wrap-up refusal reads the checkpoint of the group. -/
theorem reachable_blocked_exists {p : Params} {z : Gp} (hr : Reachable M p (enc z)) (hb : 1 ≤ z.blocked) :
    z.exists_ = true := by
  cases hx : z.exists_
  · obtain ⟨-, -, -, h4⟩ := (reachable_props hr).none hx
    omega
  · rfl

/-- Theorem 7: a recovery of the only blocked member leaves a group with none, so
a wrap-up is possible again; a reconcile that leaves one blocked keeps it refused. -/
theorem recover_unblocks {p : Params} {z y : Gp} {s' : State} {er ee : Nat} {rm kn mb ex mo : Bool} (hz : Fits p z)
    (h : step M p (enc z) ⟨"recover", [natV er, natV ee, boolV rm, boolV kn, boolV mb, boolV ex, boolV mo]⟩ = some s')
    (hy : s' = enc y) : y.blocked + 1 = z.blocked := by
  obtain ⟨-, -, hg, rfl⟩ := recover_spec hz h hy
  obtain ⟨-, -, -, -, -, -, hb⟩ := hg
  simp; omega

end KanderFormal.Proofs.Coordinator
