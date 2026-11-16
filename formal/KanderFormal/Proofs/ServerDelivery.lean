import KanderFormal.Proofs.ServerDeliverySteps

/-
Proof obligations of the server delivery machine. They hold for every value of
`maxGen` and `maxRev`.
-/
namespace KanderFormal.Proofs.ServerDelivery

open KanderFormal KanderFormal.Machines.ServerDelivery KanderFormal.Proofs.Scalar

/-! ## The events an accepted step ran -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = publishEvent ∨ ev = otherEvent ∨ ev = guardEvent := by
  simpa [serverDeliveryMachine] using h

theorem bool_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArg n :: ds) vs = some args) :
    ∃ b vs' args', vs = boolV b :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨b, rfl⟩ := conforms_bool c
  exact ⟨b, vs', a, rfl, h'⟩

theorem gen_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (genArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem rev_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (revArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

/-- Every accepted event is one of the three, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ ho ex ba g cg r ca,
      e = ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r, natV ca]⟩) ∨
    e = ⟨"other", []⟩ ∨
    (∃ ho ex cg ca, e = ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl
  · obtain ⟨ho, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨ex, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨ba, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨g, vs4, a4, rfl, h4⟩ := gen_cons h3
    obtain ⟨cg, vs5, a5, rfl, h5⟩ := gen_cons h4
    obtain ⟨r, vs6, a6, rfl, h6⟩ := rev_cons h5
    obtain ⟨ca, vs7, a7, rfl, h7⟩ := rev_cons h6
    obtain ⟨rfl, -⟩ := bindArgs_nil h7
    exact .inl ⟨ho, ex, ba, g, cg, r, ca, rfl⟩
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inl rfl)
  · obtain ⟨ho, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨ex, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨cg, vs3, a3, rfl, h3⟩ := gen_cons h2
    obtain ⟨ca, vs4, a4, rfl, h4⟩ := rev_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    exact .inr (.inr ⟨ho, ex, cg, ca, rfl⟩)

theorem ite_ne_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : (if c then some v else none) ≠ none) : c := by
  refine Classical.byContradiction fun hc => ?_
  simp [hc] at h

theorem ite_some_enc {c : Prop} [Decidable c] {z : Dl} {s' : State}
    (h : (if c then some (enc z) else none) = some s') : c ∧ s' = enc z := by
  split at h
  · cases h; exact ⟨‹_›, rfl⟩
  · cases h

theorem ite_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : ¬c) : (if c then some v else none) = none := by
  simp [h]

/-! ## Theorem 1: only the holder publishes -/

/-- Theorem 1: a delivery is published only by the current holder (its
execution identity and generation are the holder's) for the card's current
revision, with a generation and a revision that are not zero. -/
theorem publish_spec (p : Params) (x : Dl) (ho ex ba : Bool) (g cg r ca : Nat) :
    step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r, natV ca]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ cg ≤ p "maxGen" ∧ r ≤ p "maxRev" ∧ ca ≤ p "maxRev" ∧
        ho = true ∧ ex = true ∧ g ≠ 0 ∧ g = cg ∧ r ≠ 0 ∧ r = ca := by
  rw [step_publish]
  by_cases h : g ≤ p "maxGen" ∧ cg ≤ p "maxGen" ∧ r ≤ p "maxRev" ∧ ca ≤ p "maxRev" ∧
    ho = true ∧ ex = true ∧ g ≠ 0 ∧ g = cg ∧ r ≠ 0 ∧ r = ca <;> simp [h]

/-- Theorem 1: a published delivery binds the holder's generation and the card
revision of its publish, replaces the commit's record with the purpose it
names, and becomes the task's latest delivery. -/
theorem publish_result (p : Params) (x : Dl) (ho ex ba : Bool) (g cg r ca : Nat) {s' : State}
    (h : step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r, natV ca]⟩ =
      some s') : s' = enc ⟨if ba = true then 2 else 1, g, r, 1⟩ := by
  rw [step_publish] at h
  exact (ite_some_enc h).2

/-- Theorem 1: a request of another execution, of another generation, of a
non-holder, or for another card revision is refused. -/
theorem publish_refused (p : Params) (x : Dl) (ho ex ba : Bool) (g cg r ca : Nat) :
    (ho = false → step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r,
      natV ca]⟩ = none) ∧
    (ex = false → step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r,
      natV ca]⟩ = none) ∧
    (g ≠ cg → step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r,
      natV ca]⟩ = none) ∧
    (r ≠ ca → step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r,
      natV ca]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_publish]; apply ite_none; intro c; obtain ⟨-, -, -, -, c5, c6, c7, c8, c9, c10⟩ := c; simp_all)

/-- Theorem 1: the evidence of a baseline is a record like any other for the
publish rules, and has the purpose `baseline`. -/
theorem publish_purpose (p : Params) (x : Dl) (ho ex ba : Bool) (g cg r ca : Nat) {s' : State}
    (h : step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r, natV ca]⟩ =
      some s') : ∃ y, s' = enc y ∧ (y.pur = 2 ↔ ba = true) := by
  refine ⟨_, publish_result p x ho ex ba g cg r ca h, ?_⟩
  cases ba <;> simp

/-! ## Theorem 2: history and the latest index -/

/-- Theorem 2: the query `keeps` says exactly whether the commit has a record,
that is whether a publish replaces a version. -/
theorem keeps_query (p : Params) (x : Dl) :
    evalQuery M p (enc x) keepsQuery = some (boolV (decide (x.pur ≠ 0))) := by
  simp [evalQuery, keepsQuery, eval, recIs, eq, notE, f, enc, lookup, indexOf, evalCmp, boolNot, enumV,
    boolV, ne_eq, M, serverDeliveryMachine, Machine.ctorsOf, lookup]

/-- Theorem 2: a publish that replaces a record has the query `keeps` true
before it, so the shell moves the previous version into the history. -/
theorem publish_replaces_keeps (p : Params) (x : Dl) (ho ex ba : Bool) (g cg r ca : Nat)
    (_ : step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r, natV ca]⟩ ≠ none)
    (hx : x.pur ≠ 0) : evalQuery M p (enc x) keepsQuery = some (boolV true) := by
  rw [keeps_query]; simp [hx]

/-- Theorem 2: the first publish of a commit keeps nothing. -/
theorem publish_first_keeps_nothing (p : Params) (x : Dl) (hx : x.pur = 0) :
    evalQuery M p (enc x) keepsQuery = some (boolV false) := by
  rw [keeps_query]; simp [hx]

/-- Theorem 2: after a publish the task's latest delivery is the published
record. -/
theorem publish_latest (p : Params) (x : Dl) (ho ex ba : Bool) (g cg r ca : Nat) {s' : State}
    (h : step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV ba, natV g, natV cg, natV r, natV ca]⟩ =
      some s') : evalQuery M p s' latestHereQuery = some (boolV true) := by
  rw [publish_result p x ho ex ba g cg r ca h]
  simp [evalQuery, latestHereQuery, eval, latestIs, eq, f, enc, lookup, indexOf, evalCmp, enumV, boolV, M,
    serverDeliveryMachine, Machine.ctorsOf]

/-- Theorem 2: the publish of another commit moves the latest index away and
keeps this commit's record. -/
theorem other_spec (p : Params) (x : Dl) :
    step M p (enc x) ⟨"other", []⟩ = some (enc { x with latest := 2 }) := step_other p x

/-! ## Theorem 3: the delivery guard -/

/-- Theorem 3: the move from `working` to `review` accepts only a delivery
record that belongs to the current holder, the holder's generation and the
card's current revision. -/
theorem guard_spec (p : Params) (x : Dl) (ho ex : Bool) (cg ca : Nat) :
    step M p (enc x) ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩ ≠ none ↔
      cg ≤ p "maxGen" ∧ ca ≤ p "maxRev" ∧ ho = true ∧ x.pur = 1 ∧ x.rgen = cg ∧ ex = true ∧ x.rrev = ca := by
  rw [step_guard]
  by_cases h : cg ≤ p "maxGen" ∧ ca ≤ p "maxRev" ∧ ho = true ∧ x.pur = 1 ∧ x.rgen = cg ∧ ex = true ∧
    x.rrev = ca <;> simp [h]

/-- Theorem 3: the guard keeps the record. -/
theorem guard_keeps (p : Params) (x : Dl) (ho ex : Bool) (cg ca : Nat) {s' : State}
    (h : step M p (enc x) ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩ = some s') : s' = enc x := by
  rw [step_guard] at h
  exact (ite_some_enc h).2

/-- Theorem 3: a baseline, a commit without a record, a record of another
generation, of another execution, or of another card revision never passes the
guard. -/
theorem guard_refused (p : Params) (x : Dl) (ho ex : Bool) (cg ca : Nat) :
    (x.pur = 2 → step M p (enc x) ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩ = none) ∧
    (x.pur = 0 → step M p (enc x) ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩ = none) ∧
    (x.rgen ≠ cg → step M p (enc x) ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩ = none) ∧
    (x.rrev ≠ ca → step M p (enc x) ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩ = none) ∧
    (ho = false → step M p (enc x) ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩ = none) ∧
    (ex = false → step M p (enc x) ⟨"guard", [boolV ho, boolV ex, natV cg, natV ca]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_guard]; apply ite_none; intro c; obtain ⟨-, -, c3, c4, c5, c6, c7⟩ := c; simp_all)

/-- Theorem 3: a delivery published by the holder passes the guard exactly
while the holder's generation and the card revision are still the ones it was
published for, so an earlier version cannot authorize later content. -/
theorem guard_after_publish (p : Params) (x : Dl) (ho ex : Bool) (g cg r ca : Nat) {s' : State}
    (h : step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV false, natV g, natV cg, natV r, natV ca]⟩ =
      some s') (ho' ex' : Bool) (cg' ca' : Nat) :
    step M p s' ⟨"guard", [boolV ho', boolV ex', natV cg', natV ca']⟩ ≠ none ↔
      cg' ≤ p "maxGen" ∧ ca' ≤ p "maxRev" ∧ ho' = true ∧ ex' = true ∧ g = cg' ∧ r = ca' := by
  rw [publish_result p x ho ex false g cg r ca h, guard_spec]
  simp only [Bool.false_eq_true, ite_false]
  constructor
  · rintro ⟨h1, h2, h3, -, h5, h6, h7⟩; exact ⟨h1, h2, h3, h6, h5, h7⟩
  · rintro ⟨h1, h2, h3, h4, h5, h6⟩; exact ⟨h1, h2, h3, trivial, h5, h4, h6⟩

/-- Theorem 3: a baseline never passes the guard, even from its holder. -/
theorem baseline_never_guards (p : Params) (x : Dl) (ho ex : Bool) (g cg r ca : Nat) {s' : State}
    (h : step M p (enc x) ⟨"publish", [boolV ho, boolV ex, boolV true, natV g, natV cg, natV r, natV ca]⟩ =
      some s') (ho' ex' : Bool) (cg' ca' : Nat) :
    step M p s' ⟨"guard", [boolV ho', boolV ex', natV cg', natV ca']⟩ = none := by
  rw [publish_result p x ho ex true g cg r ca h]
  exact (guard_refused p ⟨2, g, r, 1⟩ ho' ex' cg' ca').1 rfl

/-! ## Theorem 4: the invariant -/

/-- The safety properties of a commit's record: a commit without a record has
no generation, no revision, and is not the latest; a record binds a generation
and a revision that are not zero. -/
structure Props (x : Dl) : Prop where
  noneEmpty : x.pur = 0 → x.rgen = 0 ∧ x.rrev = 0 ∧ x.latest ≠ 1
  bound : x.pur ≠ 0 → x.rgen ≠ 0 ∧ x.rrev ≠ 0

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, 0, 0⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨by simp, by simp⟩

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hs, hg⟩ := shape hc
  obtain ⟨P1, P2⟩ := hp x rfl
  intro y hy
  rcases accepted_cases h with ⟨ho, ex, ba, g, cg, r, ca, rfl⟩ | rfl | ⟨ho, ex, cg, ca, rfl⟩
  · rw [step_publish] at h
    obtain ⟨hc, rfl⟩ := ite_some_enc h
    have := enc_inj hy
    subst this
    obtain ⟨-, -, -, -, -, -, hg0, -, hr0, -⟩ := hc
    cases ba <;> exact ⟨by simp, fun _ => ⟨hg0, hr0⟩⟩
  · rw [step_other] at h
    cases h
    have := enc_inj hy
    subst this
    exact ⟨fun h0 => ⟨(P1 h0).1, (P1 h0).2.1, by simp⟩, P2⟩
  · rw [step_guard] at h
    obtain ⟨-, rfl⟩ := ite_some_enc h
    have := enc_inj hy
    subst this
    exact ⟨P1, P2⟩

/-- The server delivery machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 4: every reachable state satisfies the invariant, for every value
of `maxGen` and `maxRev`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 4: a record of a reachable state binds a generation and a card
revision that are not zero. -/
theorem reachable_bound {p : Params} {x : Dl} (hr : Reachable M p (enc x)) (hx : x.pur ≠ 0) :
    x.rgen ≠ 0 ∧ x.rrev ≠ 0 := ((reachable_inv hr).2 x rfl).bound hx

/-- Theorem 4: the latest index points at a commit only when that commit has a
record. -/
theorem reachable_latest_record {p : Params} {x : Dl} (hr : Reachable M p (enc x)) (hl : x.latest = 1) :
    x.pur ≠ 0 := fun h0 => ((reachable_inv hr).2 x rfl).noneEmpty h0 |>.2.2 hl

end KanderFormal.Proofs.ServerDelivery
