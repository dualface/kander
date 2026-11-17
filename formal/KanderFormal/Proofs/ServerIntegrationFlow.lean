import KanderFormal.Proofs.ServerIntegration

/-
Proof obligations of the server integration machine, part two: the three
actions and the phases (theorem 4), the rejected events (theorem 8), and the
invariant (theorem 9). They hold for every value of `maxGen`, `maxRev`, and
`maxAuth`.
-/
namespace KanderFormal.Proofs.ServerIntegration

open KanderFormal KanderFormal.Machines.ServerIntegration KanderFormal.Proofs.Scalar

/-! ## Theorem 4: phases -/

/-- Theorem 4: `pushing` needs the owner with its actor, the revision of the
record, a request that needs a push (it does not declare the source contained),
the phase `reserved`, and an observed target equal to the target the claim
inspected. -/
theorem pushing_spec (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (rm om os : Bool) :
    step M p (enc x) ⟨"pushing", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, natV er, boolV rm, boolV om, boolV os]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ er ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
        Owner x hdr g tg client ∧ Actor x al ag ae ac ∧ er = x.rev ∧ er ≠ 0 ∧ rm = true ∧ x.ct = false ∧
        x.ph = 1 ∧ om = true := by
  rw [step_pushing]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ er ≤ p "maxRev" ∧
    x.rev + 1 ≤ p "maxRev" ∧ Owner x hdr g tg client ∧ Actor x al ag ae ac ∧ er = x.rev ∧ er ≠ 0 ∧
    rm = true ∧ x.ct = false ∧ x.ph = 1 ∧ om = true <;> simp [h]

/-- Theorem 4: `pushing` moves `reserved` to `pushing`, writes the next
revision, and records whether the observed target is a commit SHA. -/
theorem pushing_result (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (rm om os : Bool) {s' : State}
    (h : step M p (enc x) ⟨"pushing", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, natV er, boolV rm, boolV om, boolV os]⟩ = some s') :
    s' = enc { x with ph := 2, rev := x.rev + 1, ob := os } := by
  rw [step_pushing] at h
  obtain ⟨-, rfl⟩ := ite_some_enc h
  rfl

/-- Theorem 4: a request that declares the source contained needs no push: it is
refused `pushing`, and so is any record not in phase `reserved` and any observed
target other than the inspected one. -/
theorem pushing_refused (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (rm om os : Bool) (h : x.ct = true ∨ x.ph ≠ 1 ∨ om = false) :
    step M p (enc x) ⟨"pushing", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, natV er, boolV rm, boolV om, boolV os]⟩ = none := by
  rw [step_pushing]; apply ite_none; intro c
  obtain ⟨-, -, -, -, -, -, -, -, -, -, h11, h12, h13⟩ := c
  rcases h with h | h | h <;> simp_all

/-- Theorem 4: `confirm` needs the owner with its actor, the revision of the
record, a push intent (`pushing`) or a contained request still in `reserved`,
and fresh exact remote facts (an observed target that is a commit SHA, an
observation time, and the exact edge). -/
theorem confirm_spec (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (rm os oa ee : Bool) :
    step M p (enc x) ⟨"confirm", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, natV er, boolV rm, boolV os, boolV oa, boolV ee]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ er ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
        Owner x hdr g tg client ∧ Actor x al ag ae ac ∧ er = x.rev ∧ er ≠ 0 ∧ rm = true ∧
        ((x.ph = 1 ∧ x.ct = true) ∨ x.ph = 2) ∧ os = true ∧ oa = true ∧ ee = true := by
  rw [step_confirm]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ er ≤ p "maxRev" ∧
    x.rev + 1 ≤ p "maxRev" ∧ Owner x hdr g tg client ∧ Actor x al ag ae ac ∧ er = x.rev ∧ er ≠ 0 ∧
    rm = true ∧ ((x.ph = 1 ∧ x.ct = true) ∨ x.ph = 2) ∧ os = true ∧ oa = true ∧ ee = true <;> simp [h]

/-- Theorem 4: `confirm` makes the phase `integrated` and the lease `released`,
writes the next revision, and records a commit SHA as the observed target. -/
theorem confirm_result (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (rm os oa ee : Bool) {s' : State}
    (h : step M p (enc x) ⟨"confirm", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, natV er, boolV rm, boolV os, boolV oa, boolV ee]⟩ = some s') :
    s' = enc { x with st := 3, ph := 3, live := false, rev := x.rev + 1, ob := true } := by
  rw [step_confirm] at h
  obtain ⟨-, rfl⟩ := ite_some_enc h
  rfl

/-- Theorem 4: a record in phase `reserved` that does not declare the source
contained, and any record in another phase than `reserved` and `pushing`, is
refused `confirm`; so is a confirmation without a commit SHA, an observation
time, or the exact edge. -/
theorem confirm_refused (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (rm os oa ee : Bool)
    (h : ¬((x.ph = 1 ∧ x.ct = true) ∨ x.ph = 2) ∨ os = false ∨ oa = false ∨ ee = false) :
    step M p (enc x) ⟨"confirm", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, natV er, boolV rm, boolV os, boolV oa, boolV ee]⟩ = none := by
  rw [step_confirm]; apply ite_none; intro c
  obtain ⟨-, -, -, -, -, -, -, -, -, -, h11, h12, h13, h14⟩ := c
  rcases h with h | h | h | h <;> simp_all

/-- Theorem 4: `release` needs the owner only (the actor's lease is not
required), the revision of the record, a decision, and an observed target that
is empty or a commit SHA. -/
theorem release_spec (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client : Bool) (er : Nat)
    (rm de oe os : Bool) :
    step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg, boolV client, natV er, boolV rm, boolV de,
      boolV oe, boolV os]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ er ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
        Owner x hdr g tg client ∧ er = x.rev ∧ er ≠ 0 ∧ rm = true ∧ de = true ∧ (oe = true ∨ os = true) := by
  rw [step_release]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ er ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
    Owner x hdr g tg client ∧ er = x.rev ∧ er ≠ 0 ∧ rm = true ∧ de = true ∧ (oe = true ∨ os = true) <;> simp [h]

/-- Theorem 4: `release` makes the phase `uncertain` when the record was
`pushing` and `released` otherwise, and the lease `released`; it writes the next
revision. -/
theorem release_result (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client : Bool) (er : Nat)
    (rm de oe os : Bool) {s' : State}
    (h : step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg, boolV client, natV er, boolV rm, boolV de,
      boolV oe, boolV os]⟩ = some s') :
    s' = enc ⟨3, x.gen, x.rev + 1, false, (if x.ph = 2 then 5 else 4), x.pu, x.ct, x.au, os⟩ := by
  rw [step_release] at h
  obtain ⟨-, rfl⟩ := ite_some_enc h
  rfl

/-- Theorem 4: a release without a decision, or with an observed target that is
neither empty nor a commit SHA, is refused. -/
theorem release_refused (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client : Bool) (er : Nat)
    (rm os : Bool) :
    (∀ oe, step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg, boolV client, natV er, boolV rm,
      boolV false, boolV oe, boolV os]⟩ = none) ∧
    (step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg, boolV client, natV er, boolV rm,
      boolV true, boolV false, boolV false]⟩ = none) := by
  constructor
  · intro oe; rw [step_release]; apply ite_none; intro c; simp at c
  · rw [step_release]; apply ite_none; intro c; simp at c

/-! ## Theorem 8: refused events -/

/-- Theorem 8: a refused event changes nothing: `run` reports the refusal and
returns the state it was given. -/
theorem refused_unchanged (p : Params) (s : State) (e : Event) (h : step M p s e = none) :
    run M p s e = (s, false) := by
  unfold run
  rw [h]

/-! ## Theorem 9: the invariant -/

/-- The safety properties of an integration record. -/
structure Props (x : It) : Prop where
  noneGen : x.st = 0 ↔ x.gen = 0
  noneRev : x.st = 0 ↔ x.rev = 0
  genRev : x.gen ≤ x.rev
  notLost : x.st ≠ 2
  noneClean : x.st = 0 → x.live = false ∧ x.ph = 0 ∧ x.pu = 0 ∧ x.ct = false ∧ x.au = 0 ∧ x.ob = false
  activePhase : x.st = 1 ↔ (x.ph = 1 ∨ x.ph = 2)
  releasedPhase : x.st = 3 ↔ (x.ph = 3 ∨ x.ph = 4 ∨ x.ph = 5)
  releasedDead : x.st = 3 → x.live = false
  integratedObs : x.ph = 3 → x.ob = true
  pushingPush : x.ph = 2 → x.ct = false
  purposeSet : x.st ≠ 0 → x.pu ≠ 0

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, 0, false, 0, 0, false, 0, false⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp, by simp⟩

theorem props_step {x y : It} (P : Props x) (ht : Trans x y) : Props y := by
  obtain ⟨a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11⟩ := P
  cases ht with
  | keep => exact ⟨a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11⟩
  | renew hs hl =>
    refine ⟨a1, a2, a3, a4, ?_, a6, a7, ?_, a9, a10, a11⟩ <;> simp_all
  | expire hl =>
    refine ⟨a1, a2, a3, a4, ?_, a6, a7, ?_, a9, a10, a11⟩ <;> simp_all
  | claim pu ag ct hv hpu =>
    refine ⟨?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_⟩ <;> simp_all
  | pushing ob hs hl hr hph hct =>
    refine ⟨?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_⟩ <;> simp_all <;> omega
  | confirm hs hl hr hph =>
    refine ⟨?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_⟩ <;> simp_all <;> omega
  | release ob hs hl hr =>
    by_cases hp : x.ph = 2 <;> refine ⟨?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_, ?_⟩ <;> simp_all <;> omega

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, -⟩ := shape hc
  obtain ⟨v, rfl, y, hy, ht, -⟩ := accepted_trans' h
  intro z hz
  have := enc_inj (hz ▸ hy : enc z = enc y)
  subst this
  exact props_step (hp x rfl) ht

/-- The server integration machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 9: every reachable state satisfies the invariant, for every value of
`maxGen`, `maxRev`, and `maxAuth`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 9: a valid lease of a reachable state belongs to a record in phase
`reserved` or `pushing`, with a positive generation and revision. -/
theorem reachable_valid {p : Params} {x : It} (hr : Reachable M p (enc x)) (hv : Valid x) :
    (x.ph = 1 ∨ x.ph = 2) ∧ 0 < x.gen ∧ 0 < x.rev := by
  have P := (reachable_inv hr).2 x rfl
  obtain ⟨h1, -⟩ := hv
  have := P.activePhase.1 h1
  have := P.noneGen
  have := P.noneRev
  refine ⟨‹_›, ?_, ?_⟩ <;> omega

/-- Theorem 9: a record in phase `integrated`, `released`, or `uncertain` of a
reachable state has its lease released, so it is not valid. -/
theorem reachable_ended_released {p : Params} {x : It} (hr : Reachable M p (enc x))
    (h : x.ph = 3 ∨ x.ph = 4 ∨ x.ph = 5) : x.st = 3 ∧ x.live = false ∧ ¬Valid x := by
  have P := (reachable_inv hr).2 x rfl
  have h3 := P.releasedPhase.2 h
  exact ⟨h3, P.releasedDead h3, fun ⟨a, _⟩ => by omega⟩

/-- Theorem 9: an `integrated` record of a reachable state observed a commit
SHA. -/
theorem reachable_integrated_obs {p : Params} {x : It} (hr : Reachable M p (enc x)) (h : x.ph = 3) :
    x.ob = true := ((reachable_inv hr).2 x rfl).integratedObs h

/-- The phases a transition between two records of a reachable state allows: the
generation grows and the phase is `reserved` (a claim), or the phase stays, or
it follows `reserved → pushing`, `reserved → integrated`, `reserved → released`,
`pushing → integrated`, or `pushing → uncertain`. -/
theorem trans_phase_flow {x y : It} (P : Props x) (ht : Trans x y) :
    (y.gen = x.gen + 1 ∧ y.ph = 1) ∨ y.ph = x.ph ∨ (x.ph = 1 ∧ (y.ph = 2 ∨ y.ph = 3 ∨ y.ph = 4)) ∨
      (x.ph = 2 ∧ (y.ph = 3 ∨ y.ph = 5)) := by
  have hap := P.activePhase
  cases ht with
  | keep => simp
  | renew => simp
  | expire => simp
  | claim => simp
  | pushing ob hs hl hr hph hct => simp [hph]
  | confirm hs hl hr hph =>
    rcases hph with ⟨h1, -⟩ | h2
    · simp [h1]
    · simp [h2]
  | release ob hs hl hr =>
    have := hap.1 hs
    by_cases hp : x.ph = 2 <;> simp_all <;> omega

/-- Theorem 4: from a reachable state, an accepted event follows the phase flow
of `trans_phase_flow`. -/
theorem phase_flow {p : Params} {x y : It} {e : Event} {s' : State} (hr : Reachable M p (enc x))
    (h : step M p (enc x) e = some s') (hy : s' = enc y) :
    (y.gen = x.gen + 1 ∧ y.ph = 1) ∨ y.ph = x.ph ∨ (x.ph = 1 ∧ (y.ph = 2 ∨ y.ph = 3 ∨ y.ph = 4)) ∨
      (x.ph = 2 ∧ (y.ph = 3 ∨ y.ph = 5)) := by
  obtain ⟨v, -, z, hz, ht, -⟩ := accepted_trans' h
  have := enc_inj (hy ▸ hz : enc y = enc z)
  subst this
  exact trans_phase_flow ((reachable_inv hr).2 x rfl) ht

/-- Theorem 4: once a record is `integrated`, `released`, or `uncertain`, only a
new claim changes it: an accepted event that does not take a new generation
leaves the record as it is. -/
theorem terminal_frozen {p : Params} {x y : It} {e : Event} {s' : State} (hr : Reachable M p (enc x))
    (hph : x.ph = 3 ∨ x.ph = 4 ∨ x.ph = 5) (h : step M p (enc x) e = some s') (hy : s' = enc y)
    (hn : y.gen = x.gen) : y = x := by
  have P := (reachable_inv hr).2 x rfl
  have h3 := P.releasedPhase.2 hph
  have hd := P.releasedDead h3
  obtain ⟨v, -, z, hz, ht, -⟩ := accepted_trans' h
  have := enc_inj (hy ▸ hz : enc y = enc z)
  subst this
  have hap := P.activePhase
  cases ht with
  | keep => rfl
  | renew hs hl => omega
  | expire hl => simp_all
  | claim pu ag ct hv hpu => simp at hn
  | pushing ob hs hl hr hph hct => omega
  | confirm hs hl hr hph => omega
  | release ob hs hl hr => omega

/-- Theorem 4: a record in a final phase keeps its generation and revision
against every event that does not take a new generation. -/
theorem terminal_keeps_counters {p : Params} {x y : It} {e : Event} {s' : State} (hr : Reachable M p (enc x))
    (hph : x.ph = 3 ∨ x.ph = 4 ∨ x.ph = 5) (h : step M p (enc x) e = some s') (hy : s' = enc y)
    (hn : y.gen = x.gen) : y.rev = x.rev := by
  have := terminal_frozen hr hph h hy hn
  subst this
  rfl

end KanderFormal.Proofs.ServerIntegration
