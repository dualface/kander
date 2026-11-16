import KanderFormal.Proofs.ServerGroupLeaseSteps

/-
Proof obligations of the server group lease machine. They hold for every value
of `maxGen`.
-/
namespace KanderFormal.Proofs.ServerGroupLease

open KanderFormal KanderFormal.Machines.ServerGroupLease KanderFormal.Proofs.Scalar

/-! ## The events an accepted step ran -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = claimEvent ∨ ev = renewEvent ∨ ev = observeEvent ∨ ev = releaseEvent ∨ ev = requireEvent ∨
      ev = writeAfterEvent ∨ ev = recoverOwnedEvent ∨ ev = recoverLostEvent ∨ ev = expireEvent := by
  simpa [serverGroupLeaseMachine] using h

theorem bool_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArg n :: ds) vs = some args) :
    ∃ b vs' args', vs = boolV b :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨b, rfl⟩ := conforms_bool c
  exact ⟨b, vs', a, rfl, h'⟩

theorem nat_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (genArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

/-- The three leading arguments of a holder event, and the rest. -/
theorem holder_args {p : Params} {rest : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (holderArgs ++ rest) vs = some args) :
    ∃ hdr g tg vs' args', vs = boolV hdr :: natV g :: natV tg :: vs' ∧ bindArgs M p rest vs' = some args' := by
  obtain ⟨hdr, vs1, a1, rfl, h1⟩ := bool_cons (n := "hdr") h
  obtain ⟨g, vs2, a2, rfl, h2⟩ := nat_cons (n := "g") h1
  obtain ⟨tg, vs3, a3, rfl, h3⟩ := nat_cons (n := "tg") h2
  exact ⟨hdr, g, tg, vs3, a3, rfl, h3⟩

/-- Every accepted event is one of the nine, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ hdr g tg se sc ex ro de,
      e = ⟨"claim", [boolV hdr, natV g, natV tg, boolV se, boolV sc, natV ex, boolV ro, boolV de]⟩) ∨
    (∃ hdr g tg, e = ⟨"renew", [boolV hdr, natV g, natV tg]⟩) ∨
    (∃ hdr g tg st, e = ⟨"observe", [boolV hdr, natV g, natV tg, boolV st]⟩) ∨
    (∃ hdr g tg, e = ⟨"release", [boolV hdr, natV g, natV tg]⟩) ∨
    (∃ hdr g tg, e = ⟨"require", [boolV hdr, natV g, natV tg]⟩) ∨
    (∃ hdr g tg m, e = ⟨"writeAfter", [boolV hdr, natV g, natV tg, boolV m]⟩) ∨
    (∃ hdr g tg st cl m tk pe,
      e = ⟨"recoverOwned", [boolV hdr, natV g, natV tg, boolV st, boolV cl, boolV m, boolV tk, boolV pe]⟩) ∨
    (∃ hdr g tg de un ok, e = ⟨"recoverLost", [boolV hdr, natV g, natV tg, boolV de, boolV un, boolV ok]⟩) ∨
    e = ⟨"expire", []⟩ := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨hdr, g, tg, vs0, a0, rfl, h0⟩ := holder_args hb
    obtain ⟨se, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨sc, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨ex, vs3, a3, rfl, h3⟩ := nat_cons h2
    obtain ⟨ro, vs4, a4, rfl, h4⟩ := bool_cons h3
    obtain ⟨de, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    exact .inl ⟨hdr, g, tg, se, sc, ex, ro, de, rfl⟩
  · obtain ⟨hdr, g, tg, vs0, a0, rfl, h0⟩ := holder_args hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h0
    exact .inr (.inl ⟨hdr, g, tg, rfl⟩)
  · obtain ⟨hdr, g, tg, vs0, a0, rfl, h0⟩ := holder_args hb
    obtain ⟨st, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inl ⟨hdr, g, tg, st, rfl⟩))
  · obtain ⟨hdr, g, tg, vs0, a0, rfl, h0⟩ := holder_args hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h0
    exact .inr (.inr (.inr (.inl ⟨hdr, g, tg, rfl⟩)))
  · obtain ⟨hdr, g, tg, vs0, a0, rfl, h0⟩ := holder_args hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h0
    exact .inr (.inr (.inr (.inr (.inl ⟨hdr, g, tg, rfl⟩))))
  · obtain ⟨hdr, g, tg, vs0, a0, rfl, h0⟩ := holder_args hb
    obtain ⟨m, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inr (.inr (.inr (.inl ⟨hdr, g, tg, m, rfl⟩)))))
  · obtain ⟨hdr, g, tg, vs0, a0, rfl, h0⟩ := holder_args hb
    obtain ⟨st, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨cl, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨m, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨tk, vs4, a4, rfl, h4⟩ := bool_cons h3
    obtain ⟨pe, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨hdr, g, tg, st, cl, m, tk, pe, rfl⟩))))))
  · obtain ⟨hdr, g, tg, vs0, a0, rfl, h0⟩ := holder_args hb
    obtain ⟨de, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨un, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨ok, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨hdr, g, tg, de, un, ok, rfl⟩)))))))
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr rfl)))))))


/-! ## Reading an accepted step -/

theorem ite_ne_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : (if c then some v else none) ≠ none) : c := by
  refine Classical.byContradiction fun hc => ?_
  simp [hc] at h

theorem ite_some_enc {c : Prop} [Decidable c] {z : Gl} {s' : State}
    (h : (if c then some (enc z) else none) = some s') : c ∧ s' = enc z := by
  split at h
  · cases h; exact ⟨‹_›, rfl⟩
  · cases h

theorem ite_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : ¬c) : (if c then some v else none) = none := by
  simp [h]

/-! ## Theorem 1: at most one valid group holder -/

/-- `requireGroup` accepts exactly the current holder. -/
theorem require_iff (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) :
    step M p (enc x) ⟨"require", [boolV hdr, natV g, natV tg]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg := by
  rw [step_require]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg <;> simp [h]

/-- Theorem 1: in every state, `requireGroup` accepts at most one generation
and one token: two accepted requests name the same generation, the current one,
and the record is active with a lease that has not expired. -/
theorem holder_unique (p : Params) (x : Gl) {hdr1 g1 tg1 hdr2 g2 tg2 : _}
    (h1 : step M p (enc x) ⟨"require", [boolV hdr1, natV g1, natV tg1]⟩ ≠ none)
    (h2 : step M p (enc x) ⟨"require", [boolV hdr2, natV g2, natV tg2]⟩ ≠ none) :
    g1 = x.gen ∧ g2 = x.gen ∧ tg1 = x.gen ∧ tg2 = x.gen ∧ x.st = 1 ∧ x.live = true := by
  rw [require_iff] at h1 h2
  obtain ⟨-, -, -, hs1, ht1, hg1, hl1⟩ := h1
  obtain ⟨-, -, -, -, ht2, hg2, -⟩ := h2
  exact ⟨hg1, hg2, ht1, ht2, hs1, hl1⟩

/-- Every event that presents the claim headers of a holder is accepted only
for the current holder. -/
theorem holder_required (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) :
    (step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg]⟩ ≠ none → Holder x hdr g tg) ∧
    (∀ st, step M p (enc x) ⟨"observe", [boolV hdr, natV g, natV tg, boolV st]⟩ ≠ none → Holder x hdr g tg) ∧
    (step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg]⟩ ≠ none → Holder x hdr g tg) ∧
    (∀ m, step M p (enc x) ⟨"writeAfter", [boolV hdr, natV g, natV tg, boolV m]⟩ ≠ none → Holder x hdr g tg) ∧
    (∀ st cl m tk pe, step M p (enc x)
      ⟨"recoverOwned", [boolV hdr, natV g, natV tg, boolV st, boolV cl, boolV m, boolV tk, boolV pe]⟩ ≠ none →
        Holder x hdr g tg) ∧
    (∀ de un ok, step M p (enc x) ⟨"recoverLost", [boolV hdr, natV g, natV tg, boolV de, boolV un, boolV ok]⟩ ≠ none →
      Holder x hdr g tg) := by
  refine ⟨fun h => ?_, fun st h => ?_, fun h => ?_, fun m h => ?_, fun st cl m tk pe h => ?_,
    fun de un ok h => ?_⟩
  · rw [step_renew] at h; exact (ite_ne_none h).2.2
  · rw [step_observe] at h; exact (ite_ne_none h).2.2
  · rw [step_release] at h; exact (ite_ne_none h).2.2
  · rw [step_writeAfter] at h; exact (ite_ne_none h).2.2.1
  · rw [step_recoverOwned] at h; exact (ite_ne_none h).2.2.1
  · rw [step_recoverLost] at h; exact (ite_ne_none h).2.2.1

/-! ## Theorem 2: generations -/

/-- Theorem 2: no accepted event lowers the generation. -/
theorem gen_monotone {p : Params} {x y : Gl} {e : Event} {s' : State}
    (h : step M p (enc x) e = some s') (hy : s' = enc y) : x.gen ≤ y.gen := by
  rcases accepted_cases h with ⟨hdr, g, tg, se, sc, ex, ro, de, rfl⟩ | ⟨hdr, g, tg, rfl⟩ |
    ⟨hdr, g, tg, st, rfl⟩ | ⟨hdr, g, tg, rfl⟩ | ⟨hdr, g, tg, rfl⟩ | ⟨hdr, g, tg, m, rfl⟩ |
    ⟨hdr, g, tg, st, cl, m, tk, pe, rfl⟩ | ⟨hdr, g, tg, de, un, ok, rfl⟩ | rfl
  · rw [step_claim] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    cases se <;> simp
  · rw [step_renew] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; simp
  · rw [step_observe] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; simp
  · rw [step_release] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; simp
  · rw [step_require] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; simp
  · rw [step_writeAfter] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; simp
  · rw [step_recoverOwned] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; simp
  · rw [step_recoverLost] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; simp
  · rw [step_expire] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp

/-- Theorem 2: the generation changes only at a claim that takes the next
generation (a new record or a takeover), and then by exactly one. -/
theorem gen_change {p : Params} {x y : Gl} {e : Event} {s' : State}
    (h : step M p (enc x) e = some s') (hy : s' = enc y) (hne : y.gen ≠ x.gen) :
    ∃ hdr g tg sc ex ro de,
      e = ⟨"claim", [boolV hdr, natV g, natV tg, boolV false, boolV sc, natV ex, boolV ro, boolV de]⟩ ∧
        y.gen = x.gen + 1 := by
  rcases accepted_cases h with ⟨hdr, g, tg, se, sc, ex, ro, de, rfl⟩ | ⟨hdr, g, tg, rfl⟩ |
    ⟨hdr, g, tg, st, rfl⟩ | ⟨hdr, g, tg, rfl⟩ | ⟨hdr, g, tg, rfl⟩ | ⟨hdr, g, tg, m, rfl⟩ |
    ⟨hdr, g, tg, st, cl, m, tk, pe, rfl⟩ | ⟨hdr, g, tg, de, un, ok, rfl⟩ | rfl
  · rw [step_claim] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    cases se
    · exact ⟨hdr, g, tg, sc, ex, ro, de, rfl, rfl⟩
    · exact absurd rfl hne
  · rw [step_renew] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact absurd rfl hne
  · rw [step_observe] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact absurd rfl hne
  · rw [step_release] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact absurd rfl hne
  · rw [step_require] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact absurd rfl hne
  · rw [step_writeAfter] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact absurd rfl hne
  · rw [step_recoverOwned] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact absurd rfl hne
  · rw [step_recoverLost] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact absurd rfl hne
  · rw [step_expire] at h; split at h <;> cases h; have := enc_inj hy; subst this; exact absurd rfl hne

/-- Theorem 2: a reuse keeps the record. -/
theorem claim_reuse_keeps (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sc : Bool) (ex : Nat) (ro de : Bool) {s' : State}
    (h : step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV true, boolV sc, natV ex, boolV ro,
      boolV de]⟩ = some s') : s' = enc x := by
  rw [step_claim] at h
  obtain ⟨hc, rfl⟩ := ite_some_enc h
  obtain ⟨-, -, -, hh⟩ := hc
  simp only [ite_true] at hh
  obtain ⟨-, ⟨-, hs, -, -, hl⟩, -⟩ := hh
  obtain ⟨st, gn, lv⟩ := x
  simp_all

/-! ## Theorem 3: claims -/

/-- Theorem 3: a claim that names the lease's own execution identity and is not
the reuse of a valid lease by the same client is refused, whatever the other
facts say. -/
theorem claim_same_exec_refused (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sc : Bool) (ex : Nat)
    (ro de : Bool) (hno : ¬(sc = true ∧ x.st = 1 ∧ x.live = true)) :
    step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV true, boolV sc, natV ex, boolV ro,
      boolV de]⟩ = none := by
  rw [step_claim]
  apply ite_none
  intro hc
  obtain ⟨-, -, -, hh⟩ := hc
  simp only [ite_true] at hh
  obtain ⟨h1, ⟨-, h2, -, -, h3⟩, -⟩ := hh
  exact hno ⟨h1, h2, h3⟩

/-- Theorem 3: a reuse needs the current holder (`requireGroup` accepts) and the
same client. -/
theorem claim_reuse_needs_holder (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sc : Bool) (ex : Nat)
    (ro de : Bool) {s' : State}
    (h : step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV true, boolV sc, natV ex, boolV ro,
      boolV de]⟩ = some s') : sc = true ∧ Holder x hdr g tg := by
  rw [step_claim] at h
  obtain ⟨hc, -⟩ := ite_some_enc h
  obtain ⟨-, -, -, hh⟩ := hc
  simp only [ite_true] at hh
  exact ⟨hh.1, hh.2.1⟩

/-- Theorem 3: a takeover (another execution identity) needs a lease that is no
longer valid, the generation the request observed, and room below the last
`uint64` generation; a previous record also needs an explicit decision. It
takes the next generation. -/
theorem claim_takeover_spec (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sc : Bool) (ex : Nat)
    (ro de : Bool) :
    step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV false, boolV sc, natV ex, boolV ro,
      boolV de]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ex ≤ p "maxGen" ∧ ¬Valid x ∧ ex = x.gen ∧ ro = true ∧
        (x.st = 0 ∨ de = true) ∧ x.gen + 1 ≤ p "maxGen" := by
  rw [step_claim]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ex ≤ p "maxGen" ∧ ¬Valid x ∧ ex = x.gen ∧ ro = true ∧
    (x.st = 0 ∨ de = true) ∧ x.gen + 1 ≤ p "maxGen"
  · simp [h]
  · simp only [Bool.false_eq_true, ite_false]
    constructor
    · intro hh; exact absurd (ite_ne_none hh) (by intro ⟨a, b, c, d⟩; exact h ⟨a, b, c, d.1, d.2.1, d.2.2.1, d.2.2.2.1, d.2.2.2.2⟩)
    · intro hh; exact absurd hh h

theorem claim_takeover_result (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sc : Bool) (ex : Nat)
    (ro de : Bool) {s' : State}
    (h : step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV false, boolV sc, natV ex, boolV ro,
      boolV de]⟩ = some s') : s' = enc ⟨1, x.gen + 1, true⟩ := by
  rw [step_claim] at h
  obtain ⟨-, rfl⟩ := ite_some_enc h
  rfl

/-- Theorem 3: a claim that cannot take the next generation (`room` is false,
the generation is the last `uint64`) is refused. -/
theorem claim_room_refused (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sc : Bool) (ex : Nat) (de : Bool) :
    step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV false, boolV sc, natV ex, boolV false,
      boolV de]⟩ = none := by
  rw [step_claim]; apply ite_none; intro hc; obtain ⟨-, -, -, hh⟩ := hc; simp at hh

/-- Theorem 3: a takeover over a previous record needs an explicit decision. -/
theorem claim_decision_needed (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sc : Bool) (ex : Nat)
    (ro : Bool) (hx : x.st ≠ 0) :
    step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV false, boolV sc, natV ex, boolV ro,
      boolV false]⟩ = none := by
  rw [step_claim]; apply ite_none; intro hc; obtain ⟨-, -, -, hh⟩ := hc
  simp at hh; omega

/-- Theorem 3: a valid lease is never taken over. -/
theorem claim_valid_refused (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (sc : Bool) (ex : Nat)
    (ro de : Bool) (hv : Valid x) :
    step M p (enc x) ⟨"claim", [boolV hdr, natV g, natV tg, boolV false, boolV sc, natV ex, boolV ro,
      boolV de]⟩ = none := by
  rw [step_claim]; apply ite_none; intro hc; obtain ⟨-, -, -, hh⟩ := hc
  simp only [Bool.false_eq_true, ite_false] at hh
  exact hh.1 hv

/-! ## Theorem 4: observations and releases end the lease -/

/-- Theorem 4: an observation of status `stopped` needs the current holder and
makes the lease `lost` and expired; the lease is no longer valid. -/
theorem observe_stopped_spec (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) {s' : State}
    (h : step M p (enc x) ⟨"observe", [boolV hdr, natV g, natV tg, boolV true]⟩ = some s') :
    Holder x hdr g tg ∧ s' = enc ⟨2, x.gen, false⟩ := by
  rw [step_observe] at h
  obtain ⟨hc, rfl⟩ := ite_some_enc h
  exact ⟨hc.2.2, rfl⟩

/-- Theorem 4: any other observation keeps the lease. -/
theorem observe_other_keeps (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) {s' : State}
    (h : step M p (enc x) ⟨"observe", [boolV hdr, natV g, natV tg, boolV false]⟩ = some s') : s' = enc x := by
  rw [step_observe] at h
  obtain ⟨-, rfl⟩ := ite_some_enc h
  rfl

/-- Theorem 4: a release needs the current holder and makes the record
`released`. -/
theorem release_spec (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) {s' : State}
    (h : step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg]⟩ = some s') :
    Holder x hdr g tg ∧ s' = enc { x with st := 3 } := by
  rw [step_release] at h
  obtain ⟨hc, rfl⟩ := ite_some_enc h
  exact ⟨hc.2.2, rfl⟩

/-- Theorem 4: after an observation of status `stopped` or a release, no
request is accepted as the holder of the group. -/
theorem ended_refuses_holder (p : Params) (y : Gl) (hy : y.st = 2 ∨ y.st = 3) (hdr : Bool) (g tg : Nat) :
    step M p (enc y) ⟨"require", [boolV hdr, natV g, natV tg]⟩ = none := by
  rw [step_require]
  apply ite_none
  intro c
  obtain ⟨-, -, -, hs, -⟩ := c
  omega

/-! ## Theorem 5: group writes -/

/-- Theorem 5: the check after a group write accepts the current holder and
only when every member card of the transaction has its holder. -/
theorem write_after_spec (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (m : Bool) :
    step M p (enc x) ⟨"writeAfter", [boolV hdr, natV g, natV tg, boolV m]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ m = true := by
  rw [step_writeAfter]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ m = true <;> simp [h]

theorem write_after_members_refused (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) :
    step M p (enc x) ⟨"writeAfter", [boolV hdr, natV g, natV tg, boolV false]⟩ = none := by
  rw [step_writeAfter]; apply ite_none; intro hh; simp at hh

/-- Theorem 5: a request whose generation header or token is not the current
generation's is refused by every holder check. -/
theorem stale_refused (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (h : g ≠ x.gen ∨ tg ≠ x.gen) :
    step M p (enc x) ⟨"require", [boolV hdr, natV g, natV tg]⟩ = none ∧
    (∀ m, step M p (enc x) ⟨"writeAfter", [boolV hdr, natV g, natV tg, boolV m]⟩ = none) ∧
    step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg]⟩ = none ∧
    (∀ st, step M p (enc x) ⟨"observe", [boolV hdr, natV g, natV tg, boolV st]⟩ = none) ∧
    step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg]⟩ = none := by
  have hh : ¬Holder x hdr g tg := fun ⟨_, _, h2, h3, _⟩ => by omega
  refine ⟨?_, fun m => ?_, ?_, fun st => ?_, ?_⟩
  · rw [step_require]; apply ite_none; intro c; exact hh c.2.2
  · rw [step_writeAfter]; apply ite_none; intro c; exact hh c.2.2.1
  · rw [step_renew]; apply ite_none; intro c; exact hh c.2.2
  · rw [step_observe]; apply ite_none; intro c; exact hh c.2.2
  · rw [step_release]; apply ite_none; intro c; exact hh c.2.2

/-! ## Theorem 6: owned recovery -/

/-- Theorem 6: the readiness of an owned recovery accepts the current holder
with an observation of status `stopped`; a member card in `working` or `review`
needs its current holder and a valid observation ticket; the member has no
unfinished start. -/
theorem recover_owned_spec (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (st cl m tk pe : Bool) :
    step M p (enc x) ⟨"recoverOwned", [boolV hdr, natV g, natV tg, boolV st, boolV cl, boolV m, boolV tk,
      boolV pe]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ st = true ∧
        (cl = false ∨ m = true ∧ tk = true) ∧ pe = false := by
  rw [step_recoverOwned]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ st = true ∧
    (cl = false ∨ m = true ∧ tk = true) ∧ pe = false <;> simp [h]

/-- Theorem 6: an observation that is not `stopped` is refused in every state,
and so is a claimed member without its holder or its ticket, and a pending
start. -/
theorem recover_owned_refused (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) :
    (∀ cl m tk pe, step M p (enc x) ⟨"recoverOwned", [boolV hdr, natV g, natV tg, boolV false, boolV cl, boolV m,
      boolV tk, boolV pe]⟩ = none) ∧
    (∀ st tk pe, step M p (enc x) ⟨"recoverOwned", [boolV hdr, natV g, natV tg, boolV st, boolV true, boolV false,
      boolV tk, boolV pe]⟩ = none) ∧
    (∀ st m pe, step M p (enc x) ⟨"recoverOwned", [boolV hdr, natV g, natV tg, boolV st, boolV true, boolV m,
      boolV false, boolV pe]⟩ = none) ∧
    (∀ st cl m tk, step M p (enc x) ⟨"recoverOwned", [boolV hdr, natV g, natV tg, boolV st, boolV cl, boolV m,
      boolV tk, boolV true]⟩ = none) := by
  refine ⟨fun cl m tk pe => ?_, fun st tk pe => ?_, fun st m pe => ?_, fun st cl m tk => ?_⟩ <;>
    (rw [step_recoverOwned]; apply ite_none; intro c; simp_all)

/-! ## Theorem 7: lost member recovery, the group side -/

/-- Theorem 7: a lost member recovery needs the current holder of the group, an
explicit decision, an honest `unknown` observation, and a valid execution
identity; the lease record stays. The member's claim record moves through the
claim kernel's `audit` event (`ServerClaim.audit_spec`). -/
theorem recover_lost_spec (p : Params) (x : Gl) (hdr : Bool) (g tg : Nat) (de un ok : Bool) :
    step M p (enc x) ⟨"recoverLost", [boolV hdr, natV g, natV tg, boolV de, boolV un, boolV ok]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ de = true ∧ un = true ∧ ok = true := by
  rw [step_recoverLost]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x hdr g tg ∧ de = true ∧ un = true ∧ ok = true <;>
    simp [h]

/-! ## Theorem 9: the invariant -/

/-- The safety properties of a lease record: a record exists exactly when the
generation is positive, and a missing record has no lease. Together with the
definition of `Valid` they give: a valid lease belongs to an existing, active
record. -/
structure Props (x : Gl) : Prop where
  noneZero : x.st = 0 ↔ x.gen = 0
  noneDead : x.st = 0 → x.live = false

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, false⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨by simp, by simp⟩

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hs, hg⟩ := shape hc
  obtain ⟨P1, P2⟩ := hp x rfl
  intro y hy
  rcases accepted_cases h with ⟨hdr, g, tg, se, sc, ex, ro, de, rfl⟩ | ⟨hdr, g, tg, rfl⟩ |
    ⟨hdr, g, tg, st, rfl⟩ | ⟨hdr, g, tg, rfl⟩ | ⟨hdr, g, tg, rfl⟩ | ⟨hdr, g, tg, m, rfl⟩ |
    ⟨hdr, g, tg, st, cl, m, tk, pe, rfl⟩ | ⟨hdr, g, tg, de, un, ok, rfl⟩ | rfl
  · rw [step_claim] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    cases se
    · constructor <;> simp
    · obtain ⟨-, -, -, -, ⟨-, hs1, -, -, -⟩, -⟩ := hc
      constructor <;> simp_all
  · rw [step_renew] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, ⟨-, hs1, -, -, -⟩⟩ := hc
    constructor <;> simp_all
  · rw [step_observe] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, ⟨-, hs1, -, -, -⟩⟩ := hc
    cases st <;> constructor <;> simp_all
  · rw [step_release] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, ⟨-, hs1, -, -, -⟩⟩ := hc
    constructor <;> simp_all
  · rw [step_require] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨P1, P2⟩
  · rw [step_writeAfter] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨P1, P2⟩
  · rw [step_recoverOwned] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨P1, P2⟩
  · rw [step_recoverLost] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    exact ⟨P1, P2⟩
  · rw [step_expire] at h; split at h <;> cases h; have := enc_inj hy; subst this
    exact ⟨P1, fun h0 => rfl⟩

/-- The server group lease machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant, for every value of
`maxGen`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 9: a valid lease of a reachable state has a positive generation. -/
theorem reachable_valid_gen {p : Params} {x : Gl} (hr : Reachable M p (enc x)) (hv : Valid x) : 0 < x.gen := by
  have := ((reachable_inv hr).2 x rfl).noneZero
  obtain ⟨h1, -⟩ := hv
  omega

end KanderFormal.Proofs.ServerGroupLease
