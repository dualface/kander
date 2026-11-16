import KanderFormal.Proofs.CoordinatorMemberCases

/-
Proof obligations of the coordinator member machine (M12). Every statement holds
for every value of `maxRev` and `maxSeq`, so for every card revision and every
chain and round position: one instance is one member, and it has no input from
another member.

Assumptions the statements name: the execution chain only appends (M2) and the
dispatch rounds of a card only append (M3); the group directory is removed with
its checkpoint by the archive cleanup (M14), and a claim then starts new mirrors.
-/
namespace KanderFormal.Proofs.CoordinatorMember

open KanderFormal KanderFormal.Machines.CoordinatorMember KanderFormal.Proofs.Scalar

set_option maxHeartbeats 4000000

/-! ## The invariant -/

/-- A confirmed member is in a finished column. -/
structure Props (z : Mb) : Prop where
  confirmed : z.confirmed = true → Finished z.col

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ z, s = enc z → Props z

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro z hz
  have : z = ⟨0, 0, 0, 0, false, false⟩ := enc_inj (hz.symm.trans rfl)
  subst this
  exact ⟨by simp⟩

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨z, rfl, hx⟩ := shape hc
  obtain ⟨P1⟩ := hp z rfl
  intro y hy
  rcases accepted_cases h with ⟨nc, nr, nt, nd, cs, ok, mi, rfl⟩ | ⟨nc, nr, cs, rfl⟩ |
    ⟨nc, nr, nt, nd, hr, cs, ok, mi, rfl⟩ | rfl | ⟨cc, nr, nt, nd, ok, rfl⟩ | ⟨cc, nr, tr, nt, dr, nd, rfl⟩
  · rw [step_accept p z hx] at h; split at h <;> cases h; have := enc_inj hy; subst this; exact ⟨by simp⟩
  · rw [step_acceptConfirmed p z hx] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; exact ⟨fun _ => hg.2.2.2.1⟩
  · rw [step_acceptTerminal p z hx] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; exact ⟨fun _ => by rcases hg.2.2.2.2.2.2.1 with h | h <;> simp [h, Finished]⟩
  · rw [step_block p z hx] at h; cases h; have := enc_inj hy; subst this; exact ⟨P1⟩
  · rw [step_recoverReset p z hx] at h; split at h <;> cases h; have := enc_inj hy; subst this; exact ⟨by simp⟩
  · rw [step_recoverConfirm p z hx] at h; split at h <;> cases h; have := enc_inj hy; subst this
    rename_i hg; exact ⟨fun _ => hg.2.2.2.2.2⟩

def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

theorem reachable_props {p : Params} {z : Mb} (hr : Reachable M p (enc z)) : Props z := (reachable_inv hr).2 z rfl

/-- Theorem 9: a confirmed member of a reachable state is in a finished column. -/
theorem reachable_confirmed_finished {p : Params} {z : Mb} (hr : Reachable M p (enc z)) (hc : z.confirmed = true) :
    Finished z.col := (reachable_props hr).confirmed hc

theorem reachable_fits {p : Params} {z : Mb} (hr : Reachable M p (enc z)) : Fits p z := by
  obtain ⟨z', hz, h⟩ := shape (reachable_inv hr).1
  have := enc_inj hz; subst this; exact h

/-! ## Theorem 3: a member is judged alone -/

/-- Theorem 3: a block keeps the last accepted mirror: it changes only `blocked`. -/
theorem block_keeps_mirror (p : Params) (z : Mb) (hz : Fits p z) :
    step M p (enc z) ⟨"block", []⟩ = some (enc { z with blocked := true }) := step_block p z hz

/-- Theorem 3: every accepting reconcile event clears the block; a block keeps
the rest. -/
theorem accept_clears_block {p : Params} {z y : Mb} {s' : State} {e : Event} (hz : Fits p z)
    (h : step M p (enc z) e = some s') (hy : s' = enc y)
    (hacc : (∃ nc nr nt nd cs ok mi,
        e = ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩) ∨
      (∃ nc nr cs, e = ⟨"acceptConfirmed", [enumV nc, natV nr, boolV cs]⟩) ∨
      (∃ nc nr nt nd hr cs ok mi,
        e = ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok, boolV mi]⟩)) :
    y.blocked = false := by
  rcases hacc with ⟨nc, nr, nt, nd, cs, ok, mi, rfl⟩ | ⟨nc, nr, cs, rfl⟩ | ⟨nc, nr, nt, nd, hr, cs, ok, mi, rfl⟩
  · rw [step_accept p z hz] at h; split at h <;> cases h; have := enc_inj hy; subst this; rfl
  · rw [step_acceptConfirmed p z hz] at h; split at h <;> cases h; have := enc_inj hy; subst this; rfl
  · rw [step_acceptTerminal p z hz] at h; split at h <;> cases h; have := enc_inj hy; subst this; rfl

/-- The states of a list of members after each member took its own event. -/
def runAll (p : Params) (ss : List State) (es : List Event) : List State :=
  List.zipWith (fun s e => (run M p s e).1) ss es

/-- Theorem 3: the next state of member `i` depends only on the state and the
event of member `i`. Another member's event (a block, an acceptance, a recovery)
changes nothing of it. -/
theorem member_isolated (p : Params) (ss : List State) (es : List Event) (i j : Nat) (hij : i ≠ j) (e' : Event) :
    (runAll p ss es)[i]? = (runAll p ss (es.set j e'))[i]? := by
  simp [runAll, List.getElem?_zipWith, List.getElem?_set_ne (Ne.symm hij)]

/-- Theorem 3: the result of member `i` is the kernel step on its own state and
event. -/
theorem member_step (p : Params) (ss : List State) (es : List Event) (i : Nat) (s : State) (e : Event)
    (hs : ss[i]? = some s) (he : es[i]? = some e) : (runAll p ss es)[i]? = some (run M p s e).1 := by
  simp [runAll, List.getElem?_zipWith, hs, he]

/-! ## Theorem 4: positions never move back -/

/-- Theorem 4: an accepted reconcile never lowers the card revision, and never
lowers the chain position (the guard `newTseq ≥ tseq` is the explicit one of v2). -/
theorem accept_monotone {p : Params} {z y : Mb} {s' : State} {e : Event} (hz : Fits p z)
    (h : step M p (enc z) e = some s') (hy : s' = enc y)
    (hacc : (∃ nc nr nt nd cs ok mi,
        e = ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩) ∨
      (∃ nc nr cs, e = ⟨"acceptConfirmed", [enumV nc, natV nr, boolV cs]⟩) ∨
      (∃ nc nr nt nd hr cs ok mi,
        e = ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok, boolV mi]⟩)) :
    z.rev ≤ y.rev ∧ z.tseq ≤ y.tseq := by
  rcases hacc with ⟨nc, nr, nt, nd, cs, ok, mi, rfl⟩ | ⟨nc, nr, cs, rfl⟩ | ⟨nc, nr, nt, nd, hr, cs, ok, mi, rfl⟩
  · rw [step_accept p z hz] at h; split at h
    · rename_i hg; cases h; have := enc_inj hy; subst this; exact ⟨hg.2.2.2.2.2.2.2.2.1, hg.2.2.2.2.2.2.2.2.2⟩
    · cases h
  · rw [step_acceptConfirmed p z hz] at h; split at h
    · rename_i hg; cases h; have := enc_inj hy; subst this; exact ⟨hg.2.2.2.2.2, Nat.le_refl _⟩
    · cases h
  · rw [step_acceptTerminal p z hz] at h; split at h
    · rename_i hg; cases h; have := enc_inj hy; subst this; exact ⟨hg.2.2.2.2.2.2.2.2.1, hg.2.2.2.2.2.2.2.2.2⟩
    · cases h

/-- Theorem 4: a member whose card revision went below the mirror's is refused by
all three accepting events. -/
theorem accept_refused_revision (p : Params) (z : Mb) (hz : Fits p z) (nr : Nat) (hlt : nr < z.rev)
    (nc nt nd : Nat) (cs ok mi hr : Bool) :
    step M p (enc z) ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩ = none ∧
    step M p (enc z) ⟨"acceptConfirmed", [enumV nc, natV nr, boolV cs]⟩ = none ∧
    step M p (enc z) ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok,
      boolV mi]⟩ = none := by
  refine ⟨?_, ?_, ?_⟩
  · rw [step_accept p z hz]; split
    · rename_i hg; omega
    · rfl
  · rw [step_acceptConfirmed p z hz]; split
    · rename_i hg; omega
    · rfl
  · rw [step_acceptTerminal p z hz]; split
    · rename_i hg; omega
    · rfl

/-- Theorem 4: a chain that is shorter than the mirror's position (the chain
regressed) is refused by the events that walk it. -/
theorem accept_refused_chain (p : Params) (z : Mb) (hz : Fits p z) (nt : Nat) (hlt : nt < z.tseq)
    (nc nr nd : Nat) (cs ok mi hr : Bool) :
    step M p (enc z) ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩ = none ∧
    step M p (enc z) ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok,
      boolV mi]⟩ = none := by
  refine ⟨?_, ?_⟩
  · rw [step_accept p z hz]; split
    · rename_i hg; omega
    · rfl
  · rw [step_acceptTerminal p z hz]; split
    · rename_i hg; omega
    · rfl

/-- Theorem 4, under the assumption that the dispatch rounds of a card only
append (M3): an accepted reconcile that walks the rounds writes the number of
rounds it found, and when that number is not below the mirror's, the dispatch
position did not move back. The kernel adds no refusal for a smaller position,
because v2 has none. -/
theorem accept_dseq {p : Params} {z y : Mb} {s' : State} {nc nr nt nd : Nat} {cs ok mi : Bool} (hz : Fits p z)
    (h : step M p (enc z) ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩ = some s')
    (hy : s' = enc y) : y.dseq = nd := by
  rw [step_accept p z hz] at h; split at h
  · cases h; have := enc_inj hy; subst this; rfl
  · cases h

theorem accept_dseq_monotone {p : Params} {z y : Mb} {s' : State} {nc nr nt nd : Nat} {cs ok mi : Bool}
    (hz : Fits p z)
    (h : step M p (enc z) ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩ = some s')
    (hy : s' = enc y) (hrounds : z.dseq ≤ nd) : z.dseq ≤ y.dseq := by
  rw [accept_dseq hz h hy]; exact hrounds

/-- Theorem 4: a member in `archived` or `trash` changes its round position only
when the card has rounds; and then, under the same assumption, it does not move
back. -/
theorem acceptTerminal_dseq {p : Params} {z y : Mb} {s' : State} {nc nr nt nd : Nat} {hr cs ok mi : Bool}
    (hz : Fits p z)
    (h : step M p (enc z) ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok,
      boolV mi]⟩ = some s') (hy : s' = enc y) :
    (hr = true → y.dseq = nd) ∧ (hr = false → y.dseq = z.dseq) := by
  rw [step_acceptTerminal p z hz] at h; split at h
  · cases h; have := enc_inj hy; subst this; cases hr <;> simp
  · cases h

/-! ## Theorem 5: a missing handoff blocks until a recovery -/

/-- Theorem 5: while the chain does not carry the cycle the mirror recorded, with
no handoff record (`missing`), both walking events refuse the member whatever
else holds. -/
theorem accept_refused_missing (p : Params) (z : Mb) (hz : Fits p z) (nc nr nt nd : Nat) (cs ok hr : Bool) :
    step M p (enc z) ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV true]⟩ = none ∧
    step M p (enc z) ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok,
      boolV true]⟩ = none := by
  refine ⟨?_, ?_⟩
  · rw [step_accept p z hz]; simp
  · rw [step_acceptTerminal p z hz]; simp

/-- The events a reconcile of a member whose chain misses a handoff runs: a walk
with `missing` set, or the block that follows its refusal. -/
def MissingEvent (e : Event) : Prop :=
  (∃ nc nr nt nd cs ok,
      e = ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV true]⟩) ∨
    (∃ nc nr nt nd hr cs ok,
      e = ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok, boolV true]⟩) ∨
    e = ⟨"block", []⟩

/-- Theorem 5: a blocked member whose chain misses a handoff stays exactly as it
is under every sequence of such reconciles: each walk is refused and each block
changes nothing more. Because the chain only appends (M2), the mirror that the
block kept still fails the next reconcile the same way, so only a recovery ends
it. -/
theorem missing_blocks_forever (p : Params) (z : Mb) (hz : Fits p z) (hb : z.blocked = true) :
    ∀ es : List Event, (∀ e ∈ es, MissingEvent e) →
      es.foldl (fun s e => (run M p s e).1) (enc z) = enc z
  | [], _ => rfl
  | e :: es, h => by
    have he := h e List.mem_cons_self
    have hstep : (run M p (enc z) e).1 = enc z := by
      rcases he with ⟨nc, nr, nt, nd, cs, ok, rfl⟩ | ⟨nc, nr, nt, nd, hr, cs, ok, rfl⟩ | rfl
      · have := (accept_refused_missing p z hz nc nr nt nd cs ok false).1
        simp [run, this]
      · have := (accept_refused_missing p z hz nc nr nt nd cs ok hr).2
        simp [run, this]
      · simp only [run, step_block p z hz]
        obtain ⟨c, r, t, d, b, k⟩ := z
        simp at hb; subst hb; rfl
    simp only [List.foldl_cons, hstep]
    exact missing_blocks_forever p z hz hb es fun e he => h e (List.mem_cons_of_mem _ he)

/-- Theorem 5: the ways a block ends. A blocked member becomes unblocked only
through an acceptance with no missing handoff, or a recovery. -/
theorem blocked_exit {p : Params} {z y : Mb} {s' : State} {e : Event} (hz : Fits p z)
    (h : step M p (enc z) e = some s') (hy : s' = enc y) (hb : z.blocked = true) (hn : y.blocked = false) :
    (∃ nc nr nt nd cs ok, e = ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV false]⟩) ∨
    (∃ nc nr cs, e = ⟨"acceptConfirmed", [enumV nc, natV nr, boolV cs]⟩) ∨
    (∃ nc nr nt nd hr cs ok,
      e = ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok, boolV false]⟩) ∨
    (∃ cc nr nt nd ok, e = ⟨"recoverReset", [enumV cc, natV nr, natV nt, natV nd, boolV ok]⟩) ∨
    (∃ cc nr tr nt dr nd, e = ⟨"recoverConfirm", [enumV cc, natV nr, boolV tr, natV nt, boolV dr, natV nd]⟩) := by
  rcases accepted_cases h with ⟨nc, nr, nt, nd, cs, ok, mi, rfl⟩ | ⟨nc, nr, cs, rfl⟩ |
    ⟨nc, nr, nt, nd, hr, cs, ok, mi, rfl⟩ | rfl | ⟨cc, nr, nt, nd, ok, rfl⟩ | ⟨cc, nr, tr, nt, dr, nd, rfl⟩
  · rw [step_accept p z hz] at h; split at h
    · rename_i hg
      have : mi = false := hg.2.2.2.2.2.1
      subst this; exact .inl ⟨nc, nr, nt, nd, cs, ok, rfl⟩
    · cases h
  · exact .inr (.inl ⟨nc, nr, cs, rfl⟩)
  · rw [step_acceptTerminal p z hz] at h; split at h
    · rename_i hg
      have : mi = false := hg.2.2.2.2.2.1
      subst this; exact .inr (.inr (.inl ⟨nc, nr, nt, nd, hr, cs, ok, rfl⟩))
    · cases h
  · rw [step_block p z hz] at h; cases h
    have := enc_inj hy; subst this; simp at hn
  · exact .inr (.inr (.inr (.inl ⟨cc, nr, nt, nd, ok, rfl⟩)))
  · exact .inr (.inr (.inr (.inr ⟨cc, nr, tr, nt, dr, nd, rfl⟩)))

/-! ## Theorem 6: recovery -/

/-- Theorem 6: an accepted reset was a blocked member whose card is in `working`
or `review`, with the checks of the lifecycle and ledger kernels passed; the
member is at `working` with a new cycle (an unconfirmed mirror), the positions of
the chain and rounds after the reset, and no block. -/
theorem reset_spec {p : Params} {z y : Mb} {s' : State} {cc nr nt nd : Nat} {ok : Bool} (hz : Fits p z)
    (h : step M p (enc z) ⟨"recoverReset", [enumV cc, natV nr, natV nt, natV nd, boolV ok]⟩ = some s')
    (hy : s' = enc y) :
    z.blocked = true ∧ Active cc ∧ ok = true ∧ y = ⟨1, nr, nt, nd, false, false⟩ := by
  rw [step_recoverReset p z hz] at h; split at h
  · rename_i hg; cases h; exact ⟨hg.2.2.2.2.1, hg.2.2.2.2.2.1, hg.2.2.2.2.2.2, (enc_inj hy).symm⟩
  · cases h

/-- Theorem 6: an accepted confirmation was a blocked member whose card is in
`done`, `archived`, or `trash`; the mirror is rebound to that column, confirmed,
unblocked; a chain or round list that was not read keeps its position. -/
theorem confirm_spec {p : Params} {z y : Mb} {s' : State} {cc nr : Nat} {tr : Bool} {nt : Nat} {dr : Bool} {nd : Nat}
    (hz : Fits p z)
    (h : step M p (enc z) ⟨"recoverConfirm", [enumV cc, natV nr, boolV tr, natV nt, boolV dr, natV nd]⟩ = some s')
    (hy : s' = enc y) :
    z.blocked = true ∧ Finished cc ∧
      y = ⟨cc, nr, if tr = true then nt else z.tseq, if dr = true then nd else z.dseq, false, true⟩ := by
  rw [step_recoverConfirm p z hz] at h; split at h
  · rename_i hg; cases h; exact ⟨hg.2.2.2.2.1, hg.2.2.2.2.2, (enc_inj hy).symm⟩
  · cases h

/-- Theorem 6: only a blocked member is recovered. -/
theorem recover_refused_not_blocked (p : Params) (z : Mb) (hz : Fits p z) (hn : z.blocked = false)
    (cc nr nt nd : Nat) (ok : Bool) (tr dr : Bool) :
    step M p (enc z) ⟨"recoverReset", [enumV cc, natV nr, natV nt, natV nd, boolV ok]⟩ = none ∧
    step M p (enc z) ⟨"recoverConfirm", [enumV cc, natV nr, boolV tr, natV nt, boolV dr, natV nd]⟩ = none := by
  refine ⟨?_, ?_⟩
  · rw [step_recoverReset p z hz]; simp [hn]
  · rw [step_recoverConfirm p z hz]; simp [hn]

/-- Theorem 6: a member whose card is in `backlog` or `todo` is recovered by
neither mode. -/
theorem recover_refused_early (p : Params) (z : Mb) (hz : Fits p z) (nr nt nd : Nat) (ok tr dr : Bool) :
    step M p (enc z) ⟨"recoverReset", [enumV 0, natV nr, natV nt, natV nd, boolV ok]⟩ = none ∧
    step M p (enc z) ⟨"recoverConfirm", [enumV 0, natV nr, boolV tr, natV nt, boolV dr, natV nd]⟩ = none := by
  refine ⟨?_, ?_⟩
  · rw [step_recoverReset p z hz]; simp [Active]
  · rw [step_recoverConfirm p z hz]; simp [Finished]

/-- Theorem 6: a reset is only for a card in `working` or `review`, a
confirmation only for a finished card: the two modes never both apply. -/
theorem recover_modes_exclusive (p : Params) (z : Mb) (hz : Fits p z) (cc nr nt nd : Nat) (ok tr dr : Bool) :
    ¬ (Active cc ∧ Finished cc) := by
  intro ⟨h1, h2⟩; omega

/-- Theorem 6: a reset refuses a finished card and a confirmation refuses a card
in `working` or `review`. -/
theorem recover_wrong_mode (p : Params) (z : Mb) (hz : Fits p z) (nr nt nd : Nat) (ok tr dr : Bool)
    (cc : Nat) :
    (Finished cc → step M p (enc z) ⟨"recoverReset", [enumV cc, natV nr, natV nt, natV nd, boolV ok]⟩ = none) ∧
    (Active cc → step M p (enc z) ⟨"recoverConfirm", [enumV cc, natV nr, boolV tr, natV nt, boolV dr, natV nd]⟩ = none) := by
  refine ⟨fun hf => ?_, fun ha => ?_⟩
  · rw [step_recoverReset p z hz]; split
    · rename_i hg; have := hg.2.2.2.2.2.1; omega
    · rfl
  · rw [step_recoverConfirm p z hz]; split
    · rename_i hg; have := hg.2.2.2.2.2; omega
    · rfl

/-- Theorem 6: a confirmation leaves the member accepted as it is: while its
card stays in a finished column of the confirmed cycle, a reconcile takes the
short path with no other check, whatever else fails. -/
theorem confirmed_accepts (p : Params) (z : Mb) (hz : Fits p z) (hc : z.confirmed = true) (nc nr : Nat)
    (hnc : nc < 6) (hf : Finished nc) (hnr : nr ≤ p "maxRev") (hge : z.rev ≤ nr) :
    step M p (enc z) ⟨"acceptConfirmed", [enumV nc, natV nr, boolV true]⟩ =
      some (enc { z with col := nc, rev := nr, blocked := false }) := by
  rw [step_acceptConfirmed p z hz]; simp [hc, hf, hnc, hnr, hge]

end KanderFormal.Proofs.CoordinatorMember
