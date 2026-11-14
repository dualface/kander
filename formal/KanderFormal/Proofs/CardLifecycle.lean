import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.CardLifecycle

/-
Proof obligations of the card lifecycle machine. They hold for every value of
`maxDecision`.

A conforming state is the encoding `enc x` of a `Card`. Each event has a lemma
that evaluates `step` on an encoded state to an explicit `if`; the theorems
are proved on those lemmas.
-/
namespace KanderFormal.Proofs.CardLifecycle

open KanderFormal KanderFormal.Machines.CardLifecycle KanderFormal.Proofs.Scalar

abbrev M := cardLifecycleMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. Columns are numbered in declaration
order: 0 backlog, 1 todo, 2 working, 3 review, 4 done, 5 archived, 6 trash. -/
structure Card where
  col : Nat
  frozen : Bool
  dec : Nat
  exc : Bool

def enc (x : Card) : State :=
  [("column", enumV x.col), ("frozen", boolV x.frozen), ("decisions", natV x.dec), ("excluded", boolV x.exc)]

theorem enc_inj {x y : Card} (h : enc x = enc y) : x = y := by
  obtain ⟨c, fr, d, ex⟩ := x
  obtain ⟨c', fr', d', ex'⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.col < 7 ∧ x.dec ≤ p "maxDecision" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4)], hn =>
    simp [cardLifecycleMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "column" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "frozen" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "decisions" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "excluded" v4 (by simp [lookup])
    cases (rfl : M.fieldTy "column" = some (.scalar (.enum "column"))).symm.trans ht1
    cases (rfl : M.fieldTy "frozen" = some (.scalar .bool)).symm.trans ht2
    cases (rfl : M.fieldTy "decisions" = some (.scalar (.nat "maxDecision"))).symm.trans ht3
    cases (rfl : M.fieldTy "excluded" = some (.scalar .bool)).symm.trans ht4
    obtain ⟨c, rfl, hcol⟩ := conforms_enum (rfl : M.enumSize "column" = some 7) c1
    obtain ⟨fr, rfl⟩ := conforms_bool c2
    obtain ⟨d, rfl, hd⟩ := conforms_nat c3
    obtain ⟨ex, rfl⟩ := conforms_bool c4
    exact ⟨⟨c, fr, d, ex⟩, rfl, hcol, hd⟩

/-! ## The transition table -/

/-- The transition table of `internal/board/board.go`. -/
def Table (c t : Nat) : Prop :=
  c = 0 ∧ (t = 1 ∨ t = 5 ∨ t = 6) ∨ c = 1 ∧ (t = 0 ∨ t = 2 ∨ t = 5 ∨ t = 6) ∨
    c = 2 ∧ (t = 3 ∨ t = 4 ∨ t = 5 ∨ t = 6) ∨ c = 3 ∧ (t = 2 ∨ t = 4 ∨ t = 5 ∨ t = 6) ∨
    c = 4 ∧ (t = 5 ∨ t = 6)

instance (c t : Nat) : Decidable (Table c t) := by unfold Table; infer_instance

/-- A target column of the table is a column. -/
theorem table_lt {c t : Nat} (h : Table c t) : t < 7 := by unfold Table at h; omega

/-! ## Events on an encoded state -/

macro "card_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, M, cardLifecycleMachine, Machine.findEvent, lookup, moveEvent, rollbackEvent,
      dispatchMoveEvent, recoverEvent, updateEvent, recordsDecision, bindArgs, Ty.conforms, STy.conforms,
      Machine.enumSize, Machine.ctorsOf, inTable, table, inCols, isCol, column, held, eval, evalCmp, natAdd,
      boolAnd, boolOr, boolNot, indexOf, enumV, boolV, natV, f, a, tt, ff, andE, orE, notE, evalAssigns,
      Machine.fieldTy, Machine.findField, applyUpdates, enc, Table, or_assoc, and_assoc, Bool.and_assoc, Bool.or_assoc, $ts,*])

/-- The excluded flag after a move into `to`. -/
def excludedAfter (x : Card) (to : Nat) (m : Bool) : Bool :=
  x.exc || (m && (decide (to = 5) || decide (to = 6)) && (!decide (to = 5) || !decide (x.col = 4)))

theorem step_move (p : Params) (x : Card) (to : Nat) (m : Bool) :
    step M p (enc x) ⟨"move", [enumV to, boolV m]⟩ =
      if Table x.col to then some (enc ⟨to, true, x.dec, excludedAfter x to m⟩) else none := by
  by_cases ht : to < 7
  · by_cases htab : Table x.col to
    · simp only [Table] at htab; card_simp [ht, htab, excludedAfter]
    · simp only [Table] at htab; card_simp [ht, htab]
  · have : ¬Table x.col to := fun h => ht (table_lt h)
    simp only [this, ite_false]
    simp [step, M, cardLifecycleMachine, Machine.findEvent, lookup, moveEvent, bindArgs, Ty.conforms,
      STy.conforms, Machine.enumSize, Machine.ctorsOf, enumV, ht]

theorem lt3 {c : Nat} (h : c < 3) : c = 0 ∨ c = 1 ∨ c = 2 := by omega

theorem step_rollback (p : Params) (x : Card) (hc : x.col < 7) (toTodo owns : Bool) :
    step M p (enc x) ⟨"rollback", [boolV toTodo, boolV owns]⟩ =
      if owns = true ∧ (x.col = 2 ∨ x.col = 3) ∧ (toTodo = false ∨ x.col = 2) then
        some (enc { x with col := if toTodo then 1 else x.col }) else none := by
  by_cases h2 : x.col = 2 <;> by_cases h3 : x.col = 3 <;> cases toTodo <;> cases owns <;>
    card_simp [h2, h3, hc] <;> omega

theorem step_dispatchMove (p : Params) (x : Card) (authorized : Bool) :
    step M p (enc x) ⟨"dispatchMove", [boolV authorized]⟩ =
      if authorized = true ∧ x.col = 2 then some (enc { x with col := 2 }) else none := by
  by_cases h2 : x.col = 2 <;> cases authorized <;> card_simp [h2]

theorem step_recover (p : Params) (x : Card) (cas blocked unbound settled : Bool) :
    step M p (enc x) ⟨"recover", [boolV cas, boolV blocked, boolV unbound, boolV settled]⟩ =
      if cas = true ∧ blocked = true ∧ unbound = true ∧ settled = true ∧ (x.col = 2 ∨ x.col = 3) then
        some (enc { x with col := 2 }) else none := by
  card_simp []

/-- An update of a frozen contract with a frozen-field change and a decision
reference leaves a `CONTRACT_DECISIONS` record. -/
def records (x : Card) (change : Nat) (decision : Bool) : Bool :=
  x.frozen && decision && !decide (change = 0)

/-- The guard of an update. -/
def UpdateOk (x : Card) (change : Nat) (decision : Bool) : Prop :=
  x.col ≠ 5 ∧ (change = 0 ∨ x.frozen = false ∨ decision = true ∨ change = 1 ∧ (x.col = 2 ∨ x.col = 3))

instance (x : Card) (change : Nat) (decision : Bool) : Decidable (UpdateOk x change decision) := by
  unfold UpdateOk; infer_instance

theorem step_update (p : Params) (x : Card) (hd : x.dec ≤ p "maxDecision") (change : Nat) (decision : Bool) :
    step M p (enc x) ⟨"update", [enumV change, boolV decision]⟩ =
      if change < 3 ∧ UpdateOk x change decision ∧ (records x change decision = true → x.dec + 1 ≤ p "maxDecision") then
        some (enc { x with dec := if records x change decision then x.dec + 1 else x.dec }) else none := by
  by_cases hch : change < 3
  · by_cases h0 : change = 0 <;> cases hf : x.frozen <;> cases decision <;>
      card_simp [hch, hd, h0, hf, records, UpdateOk] <;> by_cases h5 : x.col = 5 <;> simp [h5]
  · simp [hch, step, M, cardLifecycleMachine, Machine.findEvent, lookup, moveEvent, rollbackEvent,
      dispatchMoveEvent, recoverEvent, updateEvent, bindArgs, Ty.conforms, STy.conforms, Machine.enumSize,
      Machine.ctorsOf, enumV]

/-! ## The events an accepted step ran -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = moveEvent ∨ ev = rollbackEvent ∨ ev = dispatchMoveEvent ∨ ev = recoverEvent ∨ ev = updateEvent := by
  simpa [cardLifecycleMachine] using h

/-- Every accepted event is one of the five, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ to m, e = ⟨"move", [enumV to, boolV m]⟩) ∨ (∃ t o, e = ⟨"rollback", [boolV t, boolV o]⟩) ∨
      (∃ au, e = ⟨"dispatchMove", [boolV au]⟩) ∨
      (∃ b1 b2 b3 b4, e = ⟨"recover", [boolV b1, boolV b2, boolV b3, boolV b4]⟩) ∨
      (∃ ch d, e = ⟨"update", [enumV ch, boolV d]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    obtain ⟨to, rfl, -⟩ := conforms_enum (rfl : M.enumSize "column" = some 7) c1
    obtain ⟨m, rfl⟩ := conforms_bool c2
    exact .inl ⟨to, m, rfl⟩
  · obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [rollbackEvent]) hb
    match bs, hl with
    | [t, o], _ => exact .inr (.inl ⟨t, o, rfl⟩)
  · obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [dispatchMoveEvent]) hb
    match bs, hl with
    | [au], _ => exact .inr (.inr (.inl ⟨au, rfl⟩))
  · obtain ⟨bs, rfl, hl⟩ := args_bools (by simp [recoverEvent]) hb
    match bs, hl with
    | [b1, b2, b3, b4], _ => exact .inr (.inr (.inr (.inl ⟨b1, b2, b3, b4, rfl⟩)))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    obtain ⟨ch, rfl, -⟩ := conforms_enum (rfl : M.enumSize "change" = some 3) c1
    obtain ⟨d, rfl⟩ := conforms_bool c2
    exact .inr (.inr (.inr (.inr ⟨ch, d, rfl⟩)))

/-! ## Theorem 1: the transition table -/

/-- Theorem 1: an ordinary move is accepted exactly when its source and target
columns are a row of the transition table, in every state. -/
theorem move_accepted_iff (p : Params) (x : Card) (to : Nat) (m : Bool) :
    (step M p (enc x) ⟨"move", [enumV to, boolV m]⟩).isSome ↔ Table x.col to := by
  rw [step_move]; by_cases h : Table x.col to <;> simp [h]

/-- Theorem 1 for `kander pick`, the move into `todo`. -/
theorem pick_accepted_iff (p : Params) (x : Card) (m : Bool) :
    (step M p (enc x) ⟨"move", [enumV 1, boolV m]⟩).isSome ↔ x.col = 0 := by
  rw [move_accepted_iff]; simp [Table]

/-! ## Theorem 2: the other column changes -/

/-- Theorem 2: an accepted start rollback came from the launcher that owns the
attempt, on a card in `working` or `review`; a start rollback (`toTodo`) left
`working` and ends in `todo`, any other keeps the column. -/
theorem rollback_spec {p : Params} {x : Card} {s' : State} {toTodo owns : Bool} (hc : x.col < 7)
    (h : step M p (enc x) ⟨"rollback", [boolV toTodo, boolV owns]⟩ = some s') :
    owns = true ∧ (x.col = 2 ∨ x.col = 3) ∧ (toTodo = true → x.col = 2) ∧
      s' = enc { x with col := if toTodo then 1 else x.col } := by
  rw [step_rollback p x hc] at h
  split at h
  · rename_i hg; cases h
    obtain ⟨h1, h2, h3⟩ := hg
    exact ⟨h1, h2, fun ht => by cases toTodo <;> simp_all, rfl⟩
  · cases h

/-- Theorem 2: an accepted same-column move of a dispatch had its authorization
and stays in `working`. -/
theorem dispatchMove_spec {p : Params} {x : Card} {s' : State} {au : Bool}
    (h : step M p (enc x) ⟨"dispatchMove", [boolV au]⟩ = some s') :
    au = true ∧ x.col = 2 ∧ s' = enc x := by
  rw [step_dispatchMove] at h
  split at h
  · rename_i hg; cases h; obtain ⟨h1, h2⟩ := hg
    obtain ⟨c, fr, d, ex⟩ := x
    simp only at h2; subst h2
    exact ⟨h1, rfl, rfl⟩
  · cases h

/-- Theorem 2: an accepted recovery reset had its CAS, a blocked member, no
dispatch binding, and no pending start, on a card in `working` or `review`, and
ends in `working`. -/
theorem recover_spec {p : Params} {x : Card} {s' : State} {cas blocked unbound settled : Bool}
    (h : step M p (enc x) ⟨"recover", [boolV cas, boolV blocked, boolV unbound, boolV settled]⟩ = some s') :
    cas = true ∧ blocked = true ∧ unbound = true ∧ settled = true ∧ (x.col = 2 ∨ x.col = 3) ∧
      s' = enc { x with col := 2 } := by
  rw [step_recover] at h
  split at h
  · rename_i hg; cases h; exact ⟨hg.1, hg.2.1, hg.2.2.1, hg.2.2.2.1, hg.2.2.2.2, rfl⟩
  · cases h

/-! ## Theorem 6: contract freezing -/

/-- Theorem 6: on a frozen contract, an accepted update that changes a frozen
field either only ticks acceptance items in `working` or `review`, or carries a
decision reference and adds one `CONTRACT_DECISIONS` record. -/
theorem frozen_change {p : Params} {x : Card} {s' : State} {change : Nat} {decision : Bool}
    (hd : x.dec ≤ p "maxDecision") (hf : x.frozen = true) (hch : change ≠ 0)
    (h : step M p (enc x) ⟨"update", [enumV change, boolV decision]⟩ = some s') :
    (change = 1 ∧ (x.col = 2 ∨ x.col = 3) ∧ decision = false ∧ s' = enc x) ∨
      (decision = true ∧ s' = enc { x with dec := x.dec + 1 }) := by
  rw [step_update p x hd] at h
  split at h
  · rename_i hg; cases h
    obtain ⟨-, ⟨-, hok⟩, -⟩ := hg
    cases decision
    · rcases hok with h0 | h0 | h0 | ⟨h1, hcol⟩
      · exact absurd h0 hch
      · simp [hf] at h0
      · cases h0
      · left
        obtain ⟨c, fr, d, ex⟩ := x
        simp only at hf hcol; subst hf
        exact ⟨h1, hcol, rfl, by simp [records]⟩
    · right; exact ⟨rfl, by simp [records, hf, hch]⟩
  · cases h

/-- Only an update changes the number of contract decision records. -/
theorem decisions_only_update {p : Params} {x : Card} {s' : State} {e : Event} (hc : x.col < 7)
    (h : step M p (enc x) e = some s') (hu : e.name ≠ "update") : ∃ y : Card, s' = enc y ∧ y.dec = x.dec := by
  rcases accepted_cases h with ⟨to, m, rfl⟩ | ⟨t, o, rfl⟩ | ⟨au, rfl⟩ | ⟨b1, b2, b3, b4, rfl⟩ | ⟨ch, d, rfl⟩
  · rw [step_move] at h; split at h <;> cases h; exact ⟨_, rfl, rfl⟩
  · rw [step_rollback p x hc] at h; split at h <;> cases h; exact ⟨_, rfl, rfl⟩
  · rw [step_dispatchMove] at h; split at h <;> cases h; exact ⟨_, rfl, rfl⟩
  · rw [step_recover] at h; split at h <;> cases h; exact ⟨_, rfl, rfl⟩
  · exact absurd rfl hu

/-! ## Theorem 7: the invariant -/

/-- The safety properties of a card: a card outside `backlog` has a frozen
contract, only a frozen contract has decision records, and an excluded member
is in `archived` or `trash`. -/
structure Props (x : Card) : Prop where
  frozenOut : x.col ≠ 0 → x.frozen = true
  decisionsFrozen : x.frozen = false → x.dec = 0
  excludedFinal : x.exc = true → x.col = 5 ∨ x.col = 6

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, false, 0, false⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨by simp, by simp, by simp⟩

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hcol, hd⟩ := shape hc
  have P := hp x rfl
  intro y hy
  rcases accepted_cases h with ⟨to, m, rfl⟩ | ⟨t, o, rfl⟩ | ⟨au, rfl⟩ | ⟨b1, b2, b3, b4, rfl⟩ | ⟨ch, d, rfl⟩
  · rw [step_move] at h
    split at h
    · rename_i htab; cases h; have := enc_inj hy; subst this
      refine ⟨fun _ => rfl, by simp, fun he => ?_⟩
      cases hx : x.exc
      · simp [excludedAfter, hx] at he; simp; omega
      · rcases P.excludedFinal hx with h5 | h6
        · simp [Table, h5] at htab
        · simp [Table, h6] at htab
    · cases h
  · obtain ⟨-, h23, -, rfl⟩ := rollback_spec hcol h
    cases enc_inj hy
    have hfr := P.frozenOut (by omega)
    have hex : x.exc = false := by
      cases hx : x.exc
      · rfl
      · have := P.excludedFinal hx; omega
    refine ⟨fun _ => hfr, fun h => by simp_all, fun h => by simp_all⟩
  · obtain ⟨-, h2, rfl⟩ := dispatchMove_spec h
    cases enc_inj hy; exact P
  · obtain ⟨-, -, -, -, h23, rfl⟩ := recover_spec h
    cases enc_inj hy
    have hfr := P.frozenOut (by omega)
    refine ⟨fun _ => hfr, fun h => by simp_all, fun he => ?_⟩
    have := P.excludedFinal he; omega
  · rw [step_update p x hd] at h
    split at h
    · cases h; cases enc_inj hy
      refine ⟨P.frozenOut, fun hf => ?_, P.excludedFinal⟩
      simp only at hf
      simp [records, hf, P.decisionsFrozen hf]
    · cases h

/-- The card lifecycle machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 7: every reachable state satisfies the invariant, for every value
of `maxDecision`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 6 on reachable states: once a card left `backlog`, its contract is
frozen, so an update that changes a frozen field only ticks acceptance items in
`working` or `review`, or adds a decision record. -/
theorem reachable_frozen_change {p : Params} {x : Card} {s' : State} {change : Nat} {decision : Bool}
    (hr : Reachable M p (enc x)) (hout : x.col ≠ 0) (hch : change ≠ 0)
    (h : step M p (enc x) ⟨"update", [enumV change, boolV decision]⟩ = some s') :
    (change = 1 ∧ (x.col = 2 ∨ x.col = 3) ∧ decision = false ∧ s' = enc x) ∨
      (decision = true ∧ s' = enc { x with dec := x.dec + 1 }) := by
  obtain ⟨hc, hp⟩ := reachable_inv hr
  obtain ⟨y, hy, -, hd⟩ := shape hc
  cases enc_inj hy
  exact frozen_change hd ((hp _ rfl).frozenOut hout) hch h

end KanderFormal.Proofs.CardLifecycle
