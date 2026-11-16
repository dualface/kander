import KanderFormal.Proofs.IssueResultDecisionSteps

/-
Proof obligations of the issue close decision machine: the status flow of one
decision, the scan that `DecideResult` makes over the earlier decisions of a
record, and the update relation of one decision. The theorems hold for every
number of decisions in a record: the scan theorem is stated for a list of any
length.
-/
namespace KanderFormal.Proofs.IssueResultDecision

open KanderFormal KanderFormal.Machines.IssueResultDecision KanderFormal.Proofs.Scalar

theorem ite_some_enc {c : Prop} [Decidable c] {z : Rq} {s' : State}
    (h : (if c then some (enc z) else none) = some s') : c ∧ s' = enc z := by
  split at h
  · cases h; exact ⟨‹_›, rfl⟩
  · cases h

theorem ite_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : ¬c) : (if c then some v else none) = none := by
  simp [h]

/-! ## Accepted events -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = appendNoEvent ∨ ev = appendYesEvent ∨ ev = closeDoneEvent ∨ ev = closeRejectedEvent ∨
      ev = reconcileEvent ∨ ev = visitEvent ∨ ev = settleEvent ∨ ev = validEvent := by
  simpa [issueResultDecisionMachine] using h

theorem bool_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArg n :: ds) vs = some args) :
    ∃ b vs' args', vs = boolV b :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨b, rfl⟩ := conforms_bool c
  exact ⟨b, vs', a, rfl, h'⟩

theorem id_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (idArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem dstat_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (dstatArg n :: ds) vs = some args) :
    ∃ i vs' args', vs = enumV i :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨i, rfl, -⟩ := conforms_enum (rfl : M.enumSize "dstat" = some 8) c
  exact ⟨i, vs', a, rfl, h'⟩

/-- Every accepted event is one of the eight, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ nd, e = ⟨"appendNo", [natV nd]⟩) ∨ (∃ nd, e = ⟨"appendYes", [natV nd]⟩) ∨ e = ⟨"closeDone", []⟩ ∨
    e = ⟨"closeRejected", []⟩ ∨
    (∃ c sm, e = ⟨"reconcile", [boolV c, boolV sm]⟩) ∨
    (∃ d rc nn, e = ⟨"visit", [enumV d, boolV rc, boolV nn]⟩) ∨
    (∃ nd id, e = ⟨"settle", [enumV nd, boolV id]⟩) ∨ (∃ y v, e = ⟨"valid", [boolV y, enumV v]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨nd, vs1, a1, rfl, h1⟩ := id_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1; exact .inl ⟨nd, rfl⟩
  · obtain ⟨nd, vs1, a1, rfl, h1⟩ := id_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1; exact .inr (.inl ⟨nd, rfl⟩)
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb; exact .inr (.inr (.inl rfl))
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb; exact .inr (.inr (.inr (.inl rfl)))
  · obtain ⟨c, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨sm, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact .inr (.inr (.inr (.inr (.inl ⟨c, sm, rfl⟩))))
  · obtain ⟨d, vs1, a1, rfl, h1⟩ := dstat_cons hb
    obtain ⟨rc, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨nn, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    exact .inr (.inr (.inr (.inr (.inr (.inl ⟨d, rc, nn, rfl⟩)))))
  · obtain ⟨nd, vs1, a1, rfl, h1⟩ := dstat_cons hb
    obtain ⟨id, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨nd, id, rfl⟩))))))
  · obtain ⟨y, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨v, vs2, a2, rfl, h2⟩ := dstat_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr ⟨y, v, rfl⟩))))))

/-! ## Theorem 4: the status of a decision -/

/-- Theorem 4: a decision starts as `declined` (a refusal) or `uncertain` (a
consent), an `uncertain` consent becomes `closed`, `observed`, `superseded`, or
`rejected`, and nothing else changes a status. -/
theorem flow {p : Params} {x y : Rq} {e : Event} (hx : x.acc < 5) (h : step M p (enc x) e = some (enc y)) :
    y.ds = x.ds ∨ (x.ds = 0 ∧ (y.ds = 1 ∨ y.ds = 2)) ∨ (x.ds = 2 ∧ 3 ≤ y.ds ∧ y.ds ≤ 6) := by
  rcases accepted_cases h with ⟨ndn, rfl⟩ | ⟨ndn, rfl⟩ | rfl | rfl | ⟨c, sm, rfl⟩ | ⟨d, rc, nn, rfl⟩ | ⟨nd, id, rfl⟩ | ⟨v, w, rfl⟩
  · rw [step_appendNo] at h; obtain ⟨hc, hy⟩ := ite_some_enc h; have := enc_inj hy.symm; subst this
    exact .inr (.inl ⟨hc.2.1, .inl rfl⟩)
  · rw [step_appendYes] at h; obtain ⟨hc, hy⟩ := ite_some_enc h; have := enc_inj hy.symm; subst this
    exact .inr (.inl ⟨hc.2.1, .inr rfl⟩)
  · rw [step_closeDone] at h; obtain ⟨hc, hy⟩ := ite_some_enc h; have := enc_inj hy.symm; subst this
    exact .inr (.inr ⟨hc, by simp, by simp⟩)
  · rw [step_closeRejected] at h; obtain ⟨hc, hy⟩ := ite_some_enc h; have := enc_inj hy.symm; subst this
    exact .inr (.inr ⟨hc, by simp, by simp⟩)
  · rw [step_reconcile] at h; obtain ⟨hc, hy⟩ := ite_some_enc h; have := enc_inj hy.symm; subst this
    refine .inr (.inr ⟨hc.1, ?_, ?_⟩) <;> dsimp only <;> split <;> omega
  · rw [step_visit p x hx] at h
    split at h
    · have := enc_inj (Option.some.inj h); subst this; exact .inl rfl
    · cases h
  · rw [step_settle] at h; obtain ⟨-, hy⟩ := ite_some_enc h; have := enc_inj hy.symm; subst this; exact .inl rfl
  · rw [step_valid] at h; obtain ⟨-, hy⟩ := ite_some_enc h; have := enc_inj hy.symm; subst this; exact .inl rfl

/-- Theorem 4: a decision that is `declined`, `closed`, `observed`,
`superseded`, or `rejected` never changes status again. -/
theorem settled_frozen {p : Params} {x y : Rq} {e : Event} (hx : x.acc < 5) (hs : x.ds ≠ 0 ∧ x.ds ≠ 2)
    (h : step M p (enc x) e = some (enc y)) : y.ds = x.ds := by
  rcases flow hx h with h1 | ⟨h1, -⟩ | ⟨h1, -⟩ <;> omega

/-- Theorem 4: a consent never becomes a refusal, and a refusal comes only from
a new record: `declined` is the status of a decision that was `declined` or
did not exist. -/
theorem declined_only_new {p : Params} {x y : Rq} {e : Event} (hx : x.acc < 5)
    (h : step M p (enc x) e = some (enc y)) (hy : y.ds = 1) : x.ds = 1 ∨ x.ds = 0 := by
  rcases flow hx h with h1 | ⟨h1, -⟩ | ⟨h1, h2, h3⟩ <;> omega

/-- Theorem 4: a decision is appended only where none exists, with an identity
that is not zero. -/
theorem append_spec (p : Params) (x : Rq) (nd : Nat) :
    (step M p (enc x) ⟨"appendNo", [natV nd]⟩ ≠ none ↔ nd ≤ p "maxId" ∧ x.ds = 0 ∧ nd ≠ 0) ∧
    (step M p (enc x) ⟨"appendYes", [natV nd]⟩ ≠ none ↔ nd ≤ p "maxId" ∧ x.ds = 0 ∧ nd ≠ 0) ∧
    (∀ s', step M p (enc x) ⟨"appendNo", [natV nd]⟩ = some s' → s' = enc { x with ds := 1, did := nd }) ∧
    (∀ s', step M p (enc x) ⟨"appendYes", [natV nd]⟩ = some s' → s' = enc { x with ds := 2, did := nd }) := by
  refine ⟨?_, ?_, ?_, ?_⟩
  · rw [step_appendNo]; by_cases h : nd ≤ p "maxId" ∧ x.ds = 0 ∧ nd ≠ 0 <;> simp [h]
  · rw [step_appendYes]; by_cases h : nd ≤ p "maxId" ∧ x.ds = 0 ∧ nd ≠ 0 <;> simp [h]
  · intro s' h; rw [step_appendNo] at h; exact (ite_some_enc h).2
  · intro s' h; rw [step_appendYes] at h; exact (ite_some_enc h).2

/-- Theorem 4: the close outcome is recorded only for an `uncertain` consent. -/
theorem close_spec (p : Params) (x : Rq) :
    (step M p (enc x) ⟨"closeDone", []⟩ ≠ none ↔ x.ds = 2) ∧
    (step M p (enc x) ⟨"closeRejected", []⟩ ≠ none ↔ x.ds = 2) ∧
    (∀ s', step M p (enc x) ⟨"closeDone", []⟩ = some s' → s' = enc { x with ds := 3 }) ∧
    (∀ s', step M p (enc x) ⟨"closeRejected", []⟩ = some s' → s' = enc { x with ds := 6 }) := by
  refine ⟨?_, ?_, ?_, ?_⟩
  · rw [step_closeDone]; by_cases h : x.ds = 2 <;> simp [h]
  · rw [step_closeRejected]; by_cases h : x.ds = 2 <;> simp [h]
  · intro s' h; rw [step_closeDone] at h; exact (ite_some_enc h).2
  · intro s' h; rw [step_closeRejected] at h; exact (ite_some_enc h).2

/-! ## Theorem 6: reconciliation -/

/-- Theorem 6: `InspectResult` settles only an `uncertain` consent: a closed
remote makes it `observed`, otherwise a state version that differs makes it
`superseded`. Nothing else about the decision changes. -/
theorem reconcile_spec (p : Params) (x : Rq) (c sm : Bool) :
    step M p (enc x) ⟨"reconcile", [boolV c, boolV sm]⟩ ≠ none ↔ x.ds = 2 ∧ (c = true ∨ sm = false) := by
  rw [step_reconcile]
  by_cases h : x.ds = 2 ∧ (c = true ∨ sm = false) <;> simp [h]

theorem reconcile_result (p : Params) (x : Rq) (c sm : Bool) {s' : State}
    (h : step M p (enc x) ⟨"reconcile", [boolV c, boolV sm]⟩ = some s') :
    s' = enc { x with ds := if c = true then 4 else 5 } := by
  rw [step_reconcile] at h; exact (ite_some_enc h).2

/-- Theorem 6: a settled decision (not `uncertain`) is not reconciled again. -/
theorem reconcile_refused_settled (p : Params) (x : Rq) (c sm : Bool) (h : x.ds ≠ 2) :
    step M p (enc x) ⟨"reconcile", [boolV c, boolV sm]⟩ = none := by
  rw [step_reconcile]; apply ite_none; intro hc; exact h hc.1

/-! ## Theorem 4: the update relation of one decision -/

/-- Theorem 4: a decision of the old store passes when the new store keeps its
state version, decision, and user reference, and a decision that is not
`uncertain` keeps its status. -/
theorem settle_spec (p : Params) (x : Rq) (nd : Nat) (id : Bool) :
    step M p (enc x) ⟨"settle", [enumV nd, boolV id]⟩ ≠ none ↔
      nd < 8 ∧ id = true ∧ x.ds ≠ 0 ∧ (x.ds = 2 ∨ nd = x.ds) := by
  rw [step_settle]
  by_cases h : nd < 8 ∧ id = true ∧ x.ds ≠ 0 ∧ (x.ds = 2 ∨ nd = x.ds) <;> simp [h]

theorem settle_keeps (p : Params) (x : Rq) (nd : Nat) (id : Bool) {s' : State}
    (h : step M p (enc x) ⟨"settle", [enumV nd, boolV id]⟩ = some s') : s' = enc x := by
  rw [step_settle] at h; exact (ite_some_enc h).2

/-- Theorem 4: a decision whose identity changed is refused. -/
theorem settle_identity_refused (p : Params) (x : Rq) (nd : Nat) :
    step M p (enc x) ⟨"settle", [enumV nd, boolV false]⟩ = none := by
  rw [step_settle]; apply ite_none; intro c; obtain ⟨-, c2, -⟩ := c; simp at c2

/-- Theorem 4: a settled decision (not `uncertain`) cannot change status in the
new store, and a consent that is settled cannot be `declined`. -/
theorem settle_settled_refused (p : Params) (x : Rq) (nd : Nat) (id : Bool) (hx : x.ds ≠ 2) (hn : nd ≠ x.ds) :
    step M p (enc x) ⟨"settle", [enumV nd, boolV id]⟩ = none := by
  rw [step_settle]; apply ite_none; intro c; obtain ⟨-, -, -, c4⟩ := c; omega

/-- Theorem 4: the new store has the decision (a decision cannot be removed). -/
theorem settle_absent_refused (p : Params) (nd : Nat) (id : Bool) :
    step M p (enc ⟨0, 0, 0⟩) ⟨"settle", [enumV nd, boolV id]⟩ = none := by
  rw [step_settle]; apply ite_none; intro c; obtain ⟨-, -, c3, -⟩ := c; simp at c3

/-- Theorem 4: a decision is valid when its status is known and has the kind of
its decision: a consent (`yes`) is `uncertain`, `closed`, `observed`,
`superseded`, or `rejected`, and a refusal (`no`) is `declined`. -/
theorem valid_spec (p : Params) (x : Rq) (y : Bool) (v : Nat) :
    step M p (enc x) ⟨"valid", [boolV y, enumV v]⟩ ≠ none ↔
      v < 8 ∧ ((y = true ∧ 2 ≤ v ∧ v ≤ 6) ∨ (y = false ∧ v = 1)) := by
  rw [step_valid]
  by_cases h : v < 8 ∧ ((y = true ∧ 2 ≤ v ∧ v ≤ 6) ∨ (y = false ∧ v = 1)) <;> simp [h]

/-- Theorem 4: a consent cannot become a refusal, and a refusal cannot have the
status of a consent. -/
theorem valid_refused (p : Params) (x : Rq) :
    step M p (enc x) ⟨"valid", [boolV true, enumV 1]⟩ = none ∧
    (∀ v, 2 ≤ v → step M p (enc x) ⟨"valid", [boolV false, enumV v]⟩ = none) ∧
    step M p (enc x) ⟨"valid", [boolV true, enumV 0]⟩ = none ∧
    step M p (enc x) ⟨"valid", [boolV true, enumV 7]⟩ = none ∧
    step M p (enc x) ⟨"valid", [boolV false, enumV 7]⟩ = none := by
  refine ⟨?_, ?_, ?_, ?_, ?_⟩
  · rw [step_valid]; apply ite_none; intro c; simp at c
  · intro v hv; rw [step_valid]; apply ite_none; intro c; simp at c; omega
  · rw [step_valid]; apply ite_none; intro c; simp at c
  · rw [step_valid]; apply ite_none; intro c; simp at c
  · rw [step_valid]; apply ite_none; intro c; simp at c

/-! ## Theorem 5: the scan of the earlier decisions -/

/-- The accumulator after one more decision: the first blocker decides. -/
def stepAcc (rc nn : Bool) (acc d : Nat) : Nat := if acc = 0 then cls d rc nn else acc

/-- The accumulator after the decisions of a list, in order. -/
def scanAcc (rc nn : Bool) : Nat → List Nat → Nat
  | acc, [] => acc
  | acc, d :: ds => scanAcc rc nn (stepAcc rc nn acc d) ds

theorem cls_lt (d : Nat) (rc nn : Bool) : cls d rc nn < 5 := by
  unfold cls; split <;> (try split) <;> (try split) <;> omega

theorem stepAcc_lt {rc nn : Bool} {acc : Nat} (d : Nat) (h : acc < 5) : stepAcc rc nn acc d < 5 := by
  unfold stepAcc; split
  · exact cls_lt d rc nn
  · exact h

/-- One visit on an encoded state: the accumulator keeps a blocker and takes the
class of the decision otherwise. -/
theorem visit_enc (p : Params) (x : Rq) (hx : x.acc < 5) (d : Nat) (rc nn : Bool) (hd : d < 8) :
    step M p (enc x) ⟨"visit", [enumV d, boolV rc, boolV nn]⟩ = some (enc { x with acc := stepAcc rc nn x.acc d }) := by
  rw [step_visit p x hx]; simp [hd, stepAcc]

/-- The visits of a list of decisions, in order, from a state. -/
def visitAll (p : Params) (rc nn : Bool) : State → List Nat → Option State
  | s, [] => some s
  | s, d :: ds => (step M p s ⟨"visit", [enumV d, boolV rc, boolV nn]⟩).bind fun s' => visitAll p rc nn s' ds

theorem visitAll_enc (p : Params) (rc nn : Bool) :
    ∀ (l : List Nat) (x : Rq), x.acc < 5 → (∀ d ∈ l, d < 8) →
      visitAll p rc nn (enc x) l = some (enc { x with acc := scanAcc rc nn x.acc l })
  | [], x, _, _ => by simp [visitAll, scanAcc]
  | d :: ds, x, hx, hl => by
    have hd : d < 8 := hl d List.mem_cons_self
    simp only [visitAll, scanAcc]
    rw [visit_enc p x hx d rc nn hd, Option.bind_some]
    exact visitAll_enc p rc nn ds { x with acc := stepAcc rc nn x.acc d } (stepAcc_lt d hx)
      (fun e he => hl e (List.mem_cons_of_mem _ he))

/-- Theorem 5: the first decision that blocks decides the scan, and no later
decision changes the result. This holds for a record with any number of
decisions. -/
theorem scanAcc_first (rc nn : Bool) :
    ∀ (l : List Nat) (acc : Nat), scanAcc rc nn acc l =
      if acc ≠ 0 then acc else
        match l.find? (fun d => decide (cls d rc nn ≠ 0)) with
        | none => 0
        | some d => cls d rc nn
  | [], acc => by by_cases h : acc = 0 <;> simp [scanAcc, h]
  | d :: ds, acc => by
    simp only [scanAcc, stepAcc]
    by_cases h : acc = 0
    · subst h
      by_cases hc : cls d rc nn = 0
      · rw [if_pos rfl, hc, scanAcc_first rc nn ds 0]; simp [List.find?_cons, hc]
      · rw [if_pos rfl, scanAcc_first rc nn ds (cls d rc nn)]; simp [List.find?_cons, hc]
    · rw [if_neg h, scanAcc_first rc nn ds acc]; simp [h]

/-- Theorem 5: the machine's visits of a list of decisions, from the initial
accumulator, give the class of the first decision that blocks, or `open` when
none does. -/
theorem scan_first_blocker (p : Params) (rc nn : Bool) (ds : Nat) (l : List Nat) (hl : ∀ d ∈ l, d < 8) :
    visitAll p rc nn (enc ⟨ds, 0, 0⟩) l = some (enc ⟨ds, match l.find? (fun d => decide (cls d rc nn ≠ 0)) with
      | none => 0
      | some d => cls d rc nn, 0⟩) := by
  rw [visitAll_enc p rc nn l ⟨ds, 0, 0⟩ (by show 0 < 5; decide) hl]
  simp [scanAcc_first]

theorem scanAcc_zero (rc nn : Bool) :
    ∀ (l : List Nat) (acc : Nat), scanAcc rc nn acc l = 0 ↔ acc = 0 ∧ ∀ d ∈ l, cls d rc nn = 0
  | [], acc => by simp [scanAcc]
  | d :: ds, acc => by
    simp only [scanAcc]
    rw [scanAcc_zero rc nn ds (stepAcc rc nn acc d)]
    by_cases h : acc = 0 <;> simp [stepAcc, h]

/-- Theorem 5: the scan passes exactly when no decision blocks. -/
theorem scan_open_iff (rc nn : Bool) (l : List Nat) : scanAcc rc nn 0 l = 0 ↔ ∀ d ∈ l, cls d rc nn = 0 := by
  simpa using scanAcc_zero rc nn l 0

/-- Theorem 5: an `uncertain` decision always blocks, with the class
`uncertain`. -/
theorem cls_uncertain (rc nn : Bool) : cls 2 rc nn = 1 := by simp [cls]

/-- Theorem 5: a refusal blocks when the user does not reconsider: the scan
returns the record when the new decision is a refusal too, and refuses
otherwise. With reconsideration a refusal never blocks. -/
theorem cls_declined (nn : Bool) :
    cls 1 false nn = (if nn = true then 4 else 2) ∧ cls 1 true nn = 0 := by
  constructor <;> simp [cls]

/-- Theorem 5: a consent that is `closed`, `observed`, or `superseded` always
blocks; a `rejected` consent blocks unless the user reconsiders. -/
theorem cls_consent (rc nn : Bool) :
    cls 3 rc nn = 3 ∧ cls 4 rc nn = 3 ∧ cls 5 rc nn = 3 ∧ cls 6 false nn = 3 ∧ cls 6 true nn = 0 := by
  refine ⟨?_, ?_, ?_, ?_, ?_⟩ <;> simp [cls]

/-- Theorem 5: a missing or unknown status blocks nothing. -/
theorem cls_other (rc nn : Bool) : cls 0 rc nn = 0 ∧ cls 7 rc nn = 0 := by
  constructor <;> simp [cls]

/-! ## The invariant -/

/-- A decision never has the status `other`. -/
def Inv (p : Params) (s : State) : Prop := M.StateConforms p s ∧ ∀ x, s = enc x → x.ds ≠ 7

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨0, 0, 0⟩ := enc_inj (hx.symm.trans rfl)
  subst this; simp

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hd, ha, hdi⟩ := shape hc
  have hx := hp x rfl
  intro y hy
  rw [hy] at h
  rcases flow ha h with h1 | ⟨h1, h2⟩ | ⟨h1, h2, h3⟩ <;> omega

def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-! ## Theorem 6: every save satisfies the update relation of a decision -/

/-- The facts of `ValidateResultStoreUpdate` for a decision that went from `x`
to `y`, read off the two decisions: the new store has the decision with its
status, and its state version, decision, and user reference are the old ones. -/
def settleFacts (x y : Rq) : Event := ⟨"settle", [enumV y.ds, boolV (decide (x.did = y.did))]⟩

/-- The facts of `ValidateResultStore` for the new decision: its kind is the one
its status belongs to (a refusal is `declined`). -/
def validFacts (y : Rq) : Event := ⟨"valid", [boolV (decide (y.ds ≠ 1)), enumV y.ds]⟩

/-- Theorem 6: the identity of a decision that exists never changes. -/
theorem did_kept {p : Params} {x y : Rq} {e : Event} (hx : x.acc < 5) (hs : x.ds ≠ 0)
    (h : step M p (enc x) e = some (enc y)) : y.did = x.did := by
  rcases accepted_cases h with ⟨nd, rfl⟩ | ⟨nd, rfl⟩ | rfl | rfl | ⟨c, sm, rfl⟩ | ⟨d, rc, nn, rfl⟩ | ⟨nd, id, rfl⟩ |
    ⟨v, w, rfl⟩
  · rw [step_appendNo] at h; obtain ⟨hc, -⟩ := ite_some_enc h; omega
  · rw [step_appendYes] at h; obtain ⟨hc, -⟩ := ite_some_enc h; omega
  · rw [step_closeDone] at h; have := enc_inj (ite_some_enc h).2.symm; subst this; rfl
  · rw [step_closeRejected] at h; have := enc_inj (ite_some_enc h).2.symm; subst this; rfl
  · rw [step_reconcile] at h; have := enc_inj (ite_some_enc h).2.symm; subst this; rfl
  · rw [step_visit p x hx] at h
    split at h
    · have := enc_inj (Option.some.inj h); subst this; rfl
    · cases h
  · rw [step_settle] at h; have := enc_inj (ite_some_enc h).2.symm; subst this; rfl
  · rw [step_valid] at h; have := enc_inj (ite_some_enc h).2.symm; subst this; rfl

/-- Theorem 6: after a close outcome or a reconciliation of a decision of the
old store, the new decision passes the update relation: it keeps its identity
and a settled status. The identity fact is read off the two decisions. -/
theorem client_settle_ok {p : Params} {x y : Rq} {e : Event} (hi : Inv p (enc x)) (hs : x.ds ≠ 0)
    (h : step M p (enc x) e = some (enc y)) : step M p (enc x) (settleFacts x y) ≠ none := by
  obtain ⟨z, hz, hd, ha, hdi⟩ := shape hi.1
  have := enc_inj hz.symm; subst this
  have hy := step_inv p _ _ _ hi h
  obtain ⟨z, hz, hd', ha', hdi'⟩ := shape hy.1
  have := enc_inj hz.symm; subst this
  unfold settleFacts
  rw [settle_spec]
  have hk := did_kept ha hs h
  have hf := flow ha h
  simp only [decide_eq_true_eq]
  refine ⟨hd', by omega, hs, ?_⟩
  omega

/-- Theorem 6: a decision of a reachable state is structurally valid. -/
theorem props_valid {p : Params} {y : Rq} (hi : Inv p (enc y)) (h0 : y.ds ≠ 0) :
    step M p (enc y) (validFacts y) ≠ none := by
  obtain ⟨z, hz, hd, ha, hdi⟩ := shape hi.1
  have := enc_inj hz.symm; subst this
  have h7 := hi.2 z rfl
  unfold validFacts
  rw [valid_spec]
  simp only [decide_eq_true_eq]
  refine ⟨hd, ?_⟩
  by_cases h1 : z.ds = 1
  · exact .inr ⟨by simp [h1], h1⟩
  · exact .inl ⟨by simp [h1], by omega, by omega⟩

end KanderFormal.Proofs.IssueResultDecision
