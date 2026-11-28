import KanderFormal.Proofs.StartOrderSteps

/-
Proof obligations of the start order machine. They hold for every value of
`maxGen`.
-/
namespace KanderFormal.Proofs.StartOrder

open KanderFormal KanderFormal.Machines.StartOrder KanderFormal.Proofs.Scalar

/-! ## The events an accepted step ran -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = createEvent ∨ ev = acceptEvent ∨ ev = claimEvent ∨ ev = confirmEvent ∨ ev = rollbackEvent ∨
      ev = supersedeEvent ∨ ev = failEvent ∨ ev = cancelEvent ∨ ev = expireEvent ∨ ev = hostLostEvent ∨
      ev = preemptEvent := by
  simpa [startOrderMachine] using h

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

/-- Every accepted event is one of the eleven, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    e = ⟨"create", []⟩ ∨
    (∃ t, e = ⟨"accept", [boolV t]⟩) ∨
    (∃ t k g, e = ⟨"claim", [boolV t, boolV k, natV g]⟩) ∨
    (∃ g, e = ⟨"confirm", [natV g]⟩) ∨
    (∃ g, e = ⟨"rollback", [natV g]⟩) ∨
    (∃ g, e = ⟨"supersede", [natV g]⟩) ∨
    (∃ t, e = ⟨"fail", [boolV t]⟩) ∨
    e = ⟨"cancel", []⟩ ∨
    (∃ d, e = ⟨"expire", [boolV d]⟩) ∨
    (∃ l, e = ⟨"hostLost", [boolV l]⟩) ∨
    e = ⟨"preempt", []⟩ := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inl rfl
  · obtain ⟨t, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inl ⟨t, rfl⟩)
  · obtain ⟨t, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨k, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨g, vs3, a3, rfl, h3⟩ := gen_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    exact .inr (.inr (.inl ⟨t, k, g, rfl⟩))
  · obtain ⟨g, vs1, a1, rfl, h1⟩ := gen_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inr (.inl ⟨g, rfl⟩)))
  · obtain ⟨g, vs1, a1, rfl, h1⟩ := gen_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inr (.inr (.inl ⟨g, rfl⟩))))
  · obtain ⟨g, vs1, a1, rfl, h1⟩ := gen_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inr (.inr (.inr (.inl ⟨g, rfl⟩)))))
  · obtain ⟨t, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨t, rfl⟩))))))
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl rfl)))))))
  · obtain ⟨d, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨d, rfl⟩))))))))
  · obtain ⟨l, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨l, rfl⟩)))))))))
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr rfl)))))))))

theorem ite_some_enc {c : Prop} [Decidable c] {z : Ord} {s' : State}
    (h : (if c then some (enc z) else none) = some s') : c ∧ s' = enc z := by
  split at h
  · cases h; exact ⟨‹_›, rfl⟩
  · cases h

/-- The result of every accepted step on an encoded state: the encoded state of
the step lemma, whose guard held. -/
theorem accepted_result {p : Params} {x : Ord} {e : Event} {s' : State} (h : step M p (enc x) e = some s') :
    (e = ⟨"create", []⟩ ∧ x.st = 0 ∧ s' = enc ⟨1, x.bgen⟩) ∨
    (∃ t, e = ⟨"accept", [boolV t]⟩ ∧ x.st = 1 ∧ t = true ∧ s' = enc ⟨2, x.bgen⟩) ∨
    (∃ t k g, e = ⟨"claim", [boolV t, boolV k, natV g]⟩ ∧ g ≤ p "maxGen" ∧ x.st = 2 ∧ x.bgen = 0 ∧
      t = true ∧ k = true ∧ g ≠ 0 ∧ s' = enc ⟨2, g⟩) ∨
    (∃ g, e = ⟨"confirm", [natV g]⟩ ∧ x.st = 2 ∧ x.bgen ≠ 0 ∧ x.bgen = g ∧ s' = enc ⟨3, x.bgen⟩) ∨
    (∃ g, e = ⟨"rollback", [natV g]⟩ ∧ x.st = 2 ∧ x.bgen ≠ 0 ∧ x.bgen = g ∧ s' = enc ⟨4, x.bgen⟩) ∨
    (∃ g, e = ⟨"supersede", [natV g]⟩ ∧ x.st = 2 ∧ x.bgen ≠ 0 ∧ x.bgen = g ∧ s' = enc ⟨4, x.bgen⟩) ∨
    (∃ t, e = ⟨"fail", [boolV t]⟩ ∧ x.st = 2 ∧ x.bgen = 0 ∧ t = true ∧ s' = enc ⟨4, x.bgen⟩) ∨
    (e = ⟨"cancel", []⟩ ∧ (x.st = 1 ∨ (x.st = 2 ∧ x.bgen = 0)) ∧ s' = enc ⟨5, x.bgen⟩) ∨
    (∃ d, e = ⟨"expire", [boolV d]⟩ ∧ x.st = 1 ∧ d = true ∧ s' = enc ⟨6, x.bgen⟩) ∨
    (∃ l, e = ⟨"hostLost", [boolV l]⟩ ∧ x.st = 2 ∧ x.bgen = 0 ∧ l = true ∧ s' = enc ⟨4, x.bgen⟩) ∨
    (e = ⟨"preempt", []⟩ ∧ (x.st = 1 ∨ x.st = 2) ∧ x.bgen = 0 ∧ s' = enc ⟨4, x.bgen⟩) := by
  rcases accepted_cases h with rfl | ⟨t, rfl⟩ | ⟨t, k, g, rfl⟩ | ⟨g, rfl⟩ | ⟨g, rfl⟩ | ⟨g, rfl⟩ | ⟨t, rfl⟩ |
      rfl | ⟨d, rfl⟩ | ⟨l, rfl⟩ | rfl
  · rw [step_create] at h
    obtain ⟨c, rfl⟩ := ite_some_enc h
    exact .inl ⟨rfl, c, rfl⟩
  · rw [step_accept] at h
    obtain ⟨⟨c1, c2⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inl ⟨t, rfl, c1, c2, rfl⟩)
  · rw [step_claim] at h
    obtain ⟨⟨c1, c2, c3, c4, c5, c6⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inl ⟨t, k, g, rfl, c1, c2, c3, c4, c5, c6, rfl⟩))
  · rw [step_confirm] at h
    obtain ⟨⟨-, c2, c3, c4⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inr (.inl ⟨g, rfl, c2, c3, c4, rfl⟩)))
  · rw [step_rollback] at h
    obtain ⟨⟨-, c2, c3, c4⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inr (.inr (.inl ⟨g, rfl, c2, c3, c4, rfl⟩))))
  · rw [step_supersede] at h
    obtain ⟨⟨-, c2, c3, c4⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inr (.inr (.inr (.inl ⟨g, rfl, c2, c3, c4, rfl⟩)))))
  · rw [step_fail] at h
    obtain ⟨⟨c1, c2, c3⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨t, rfl, c1, c2, c3, rfl⟩))))))
  · rw [step_cancel] at h
    obtain ⟨c, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨rfl, c, rfl⟩)))))))
  · rw [step_expire] at h
    obtain ⟨⟨c1, c2⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨d, rfl, c1, c2, rfl⟩))))))))
  · rw [step_hostLost] at h
    obtain ⟨⟨c1, c2, c3⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨l, rfl, c1, c2, c3, rfl⟩)))))))))
  · rw [step_preempt] at h
    obtain ⟨⟨c1, c2⟩, rfl⟩ := ite_some_enc h
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr ⟨rfl, c1, c2, rfl⟩)))))))))

/-! ## Theorem 1: a final order never changes -/

/-- Theorem 1: an order that started, failed, was cancelled, or expired refuses
every event, so no later claim, report, or sweep moves it. -/
theorem terminal_stable (p : Params) (x : Ord) (hx : 3 ≤ x.st) (e : Event) : step M p (enc x) e = none := by
  cases h : step M p (enc x) e with
  | none => rfl
  | some s' =>
    exfalso
    rcases accepted_result h with ⟨-, c, -⟩ | ⟨-, -, c, -⟩ | ⟨-, -, -, -, -, c, -⟩ | ⟨-, -, c, -⟩ | ⟨-, -, c, -⟩ |
        ⟨-, -, c, -⟩ | ⟨-, -, c, -⟩ | ⟨-, c, -⟩ | ⟨-, -, c, -⟩ | ⟨-, -, c, -⟩ | ⟨-, c, -⟩ <;> omega

/-! ## Theorem 2: an order binds at most one generation -/

/-- Theorem 2: the claim of an order binds a generation only while the order
is accepted, unbound, claimed by its target host for its task, with a
generation that is not zero. -/
theorem claim_spec (p : Params) (x : Ord) (t k : Bool) (g : Nat) :
    step M p (enc x) ⟨"claim", [boolV t, boolV k, natV g]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ x.st = 2 ∧ x.bgen = 0 ∧ t = true ∧ k = true ∧ g ≠ 0 := by
  rw [step_claim]
  by_cases h : g ≤ p "maxGen" ∧ x.st = 2 ∧ x.bgen = 0 ∧ t = true ∧ k = true ∧ g ≠ 0 <;> simp [h]

/-- Theorem 2: a claim of another host, of another task, or of an order that a
claim already bound, is refused. -/
theorem claim_refused (p : Params) (x : Ord) (t k : Bool) (g : Nat) :
    (t = false → step M p (enc x) ⟨"claim", [boolV t, boolV k, natV g]⟩ = none) ∧
    (k = false → step M p (enc x) ⟨"claim", [boolV t, boolV k, natV g]⟩ = none) ∧
    (x.bgen ≠ 0 → step M p (enc x) ⟨"claim", [boolV t, boolV k, natV g]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;> (rw [step_claim]; simp_all)

/-- Theorem 2: once an order is bound to a generation, every step keeps that
generation. -/
theorem bind_once {p : Params} {x : Ord} {e : Event} {s' : State} (hb : x.bgen ≠ 0)
    (h : step M p (enc x) e = some s') : ∃ y, s' = enc y ∧ y.bgen = x.bgen := by
  rcases accepted_result h with ⟨-, -, rfl⟩ | ⟨-, -, -, -, rfl⟩ | ⟨-, -, -, -, -, -, c, -⟩ |
      ⟨-, -, -, -, -, rfl⟩ | ⟨-, -, -, -, -, rfl⟩ | ⟨-, -, -, -, -, rfl⟩ | ⟨-, -, -, -, -, rfl⟩ | ⟨-, -, rfl⟩ |
      ⟨-, -, -, -, rfl⟩ | ⟨-, -, -, -, -, rfl⟩ | ⟨-, -, -, rfl⟩
  all_goals first | exact ⟨_, rfl, rfl⟩ | exact absurd c hb

/-! ## Theorem 3: only the bound generation's confirmation starts an order -/

/-- Theorem 3: a step reaches `started` only as the confirmation of the
generation the order is bound to, and that generation is not zero; a started
order takes no step at all (Theorem 1). -/
theorem started_only_confirm {p : Params} {x y : Ord} {e : Event} (h : step M p (enc x) e = some (enc y))
    (hy : y.st = 3) : ∃ g, e = ⟨"confirm", [natV g]⟩ ∧ x.bgen = g ∧ g ≠ 0 := by
  rcases accepted_result h with ⟨-, -, hs⟩ | ⟨-, -, -, -, hs⟩ | ⟨_t, _k, _g, -, -, -, -, -, -, -, hs⟩ |
      ⟨g, rfl, -, hg, hgx, -⟩ | ⟨-, -, -, -, -, hs⟩ | ⟨-, -, -, -, -, hs⟩ | ⟨-, -, -, -, -, hs⟩ | ⟨-, -, hs⟩ |
      ⟨-, -, -, -, hs⟩ | ⟨-, -, -, -, -, hs⟩ | ⟨-, -, -, hs⟩
  · obtain rfl := enc_inj hs; simp at hy
  · obtain rfl := enc_inj hs; simp at hy
  · obtain rfl := enc_inj hs; simp at hy
  · exact ⟨g, rfl, hgx, by omega⟩
  · obtain rfl := enc_inj hs; simp at hy
  · obtain rfl := enc_inj hs; simp at hy
  · obtain rfl := enc_inj hs; simp at hy
  · obtain rfl := enc_inj hs; simp at hy
  · obtain rfl := enc_inj hs; simp at hy
  · obtain rfl := enc_inj hs; simp at hy
  · obtain rfl := enc_inj hs; simp at hy

/-! ## Theorem 4: every open order has an exit -/

/-- Theorem 4: a pending order expires once the acceptance window passed. -/
theorem pending_expires (p : Params) (x : Ord) (hx : x.st = 1) :
    step M p (enc x) ⟨"expire", [boolV true]⟩ = some (enc ⟨6, x.bgen⟩) := by
  rw [step_expire]; simp [hx]

/-- Theorem 4: an accepted order no claim took fails once its host is lost. -/
theorem unbound_host_lost (p : Params) (x : Ord) (hx : x.st = 2) (hb : x.bgen = 0) :
    step M p (enc x) ⟨"hostLost", [boolV true]⟩ = some (enc ⟨4, x.bgen⟩) := by
  rw [step_hostLost]; simp [hx, hb]

/-- Theorem 4: an accepted order bound to a generation ends with that
generation: its confirmation starts it, its rollback or the end of its claim
fails it. -/
theorem bound_ends (p : Params) (x : Ord) (hx : x.st = 2) (hb : x.bgen ≠ 0) (hg : x.bgen ≤ p "maxGen") :
    step M p (enc x) ⟨"confirm", [natV x.bgen]⟩ = some (enc ⟨3, x.bgen⟩) ∧
    step M p (enc x) ⟨"rollback", [natV x.bgen]⟩ = some (enc ⟨4, x.bgen⟩) ∧
    step M p (enc x) ⟨"supersede", [natV x.bgen]⟩ = some (enc ⟨4, x.bgen⟩) := by
  refine ⟨?_, ?_, ?_⟩
  · rw [step_confirm]; simp [hx, hb, hg]
  · rw [step_rollback]; simp [hx, hb, hg]
  · rw [step_supersede]; simp [hx, hb, hg]

/-- The withdrawal of an order is possible exactly while no claim took it. -/
theorem cancel_spec (p : Params) (x : Ord) :
    step M p (enc x) ⟨"cancel", []⟩ ≠ none ↔ x.st = 1 ∨ (x.st = 2 ∧ x.bgen = 0) := by
  rw [step_cancel]
  by_cases h : x.st = 1 ∨ (x.st = 2 ∧ x.bgen = 0) <;> simp [h]

/-! ## Theorem 5: the invariant -/

/-- The safety properties of an order: an order that no host took or that ended
without a claim is unbound, and a started order is bound. -/
structure Props (x : Ord) : Prop where
  noneUnbound : x.st = 0 → x.bgen = 0
  pendingUnbound : x.st = 1 → x.bgen = 0
  startedBound : x.st = 3 → x.bgen ≠ 0
  cancelledUnbound : x.st = 5 → x.bgen = 0
  expiredUnbound : x.st = 6 → x.bgen = 0

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨fun _ => rfl, fun _ => rfl, by simp, fun _ => rfl, fun _ => rfl⟩

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, -, -⟩ := shape hc
  obtain ⟨P0, P1, -, -, -⟩ := hp x rfl
  intro y hy
  rcases accepted_result h with ⟨-, c, rfl⟩ | ⟨-, -, -, -, rfl⟩ | ⟨_t, _k, _g, -, -, -, -, -, -, -, rfl⟩ |
      ⟨-, -, -, b0, -, rfl⟩ | ⟨-, -, -, -, -, rfl⟩ | ⟨-, -, -, -, -, rfl⟩ | ⟨-, -, -, -, -, rfl⟩ | ⟨-, c, rfl⟩ |
      ⟨-, -, c, -, rfl⟩ | ⟨-, -, -, -, -, rfl⟩ | ⟨-, -, -, rfl⟩ <;>
    obtain rfl := enc_inj hy
  · exact ⟨by simp, fun _ => P0 c, by simp, by simp, by simp⟩
  · exact ⟨by simp, by simp, by simp, by simp, by simp⟩
  · exact ⟨by simp, by simp, by simp, by simp, by simp⟩
  · exact ⟨by simp, by simp, fun _ => b0, by simp, by simp⟩
  · exact ⟨by simp, by simp, by simp, by simp, by simp⟩
  · exact ⟨by simp, by simp, by simp, by simp, by simp⟩
  · exact ⟨by simp, by simp, by simp, by simp, by simp⟩
  · refine ⟨by simp, by simp, by simp, fun _ => ?_, by simp⟩
    rcases c with c | ⟨-, c⟩
    · exact P1 c
    · exact c
  · exact ⟨by simp, by simp, by simp, by simp, fun _ => P1 c⟩
  · exact ⟨by simp, by simp, by simp, by simp, by simp⟩
  · exact ⟨by simp, by simp, by simp, by simp, by simp⟩

/-- The start order machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 5: every reachable state satisfies the invariant, for every value
of `maxGen`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 5: a reachable started order is bound to a generation. -/
theorem reachable_started_bound {p : Params} {x : Ord} (hr : Reachable M p (enc x)) (hx : x.st = 3) :
    x.bgen ≠ 0 := ((reachable_inv hr).2 x rfl).startedBound hx

/-- Theorem 4, for reachable states: every order that is not final has an
event that makes it final. -/
theorem reachable_open_exit {p : Params} {x : Ord} (hr : Reachable M p (enc x)) (ho : x.st = 1 ∨ x.st = 2) :
    ∃ e y, step M p (enc x) e = some (enc y) ∧ 3 ≤ y.st := by
  obtain ⟨hc, -⟩ := reachable_inv hr
  obtain ⟨z, hz, -, hg⟩ := shape hc
  obtain rfl := enc_inj hz
  rcases ho with h1 | h2
  · exact ⟨_, _, pending_expires p x h1, by simp⟩
  · by_cases hb : x.bgen = 0
    · exact ⟨_, _, unbound_host_lost p x h2 hb, by simp⟩
    · exact ⟨_, _, (bound_ends p x h2 hb hg).1, by simp⟩

end KanderFormal.Proofs.StartOrder
