import KanderFormal.Proofs.ReviewObligationModel

/-
The independent observer of review obligations.

The observer reads only the history of events (`Ev`). It never reads a field of
the machine state. It keeps its own record of the plan: every batch carries an
identity in the order batches were added, a dropped batch disappears with its
identity, and a closure remembers the cycle it was taken in. Debts carry their
provenance: a drop of required batches and a planned reset leave one debt per
role, discharged only by a closed batch added after the debt arose (its identity
is at least `since`). A reset with no plan leaves a no-plan record, which the
observer keeps across its release: while no role applies, the release discharges
it; once a role applies, only a closure of the current cycle does, release or
not. A planned reset with role history replaces that record with planned
recovery. A rebind owes a batch from the member's first open batch on.

This follows the "Changed rule" of the replaced prototype's PROVENANCE.md, with
production's role set: the plan's roles are the union over its batches, and the
record a drop leaves remembers the roles it took out of the plan.
-/
namespace KanderFormal.Proofs.ReviewObligation

/-- A batch as the observer records it. -/
structure OBatch where
  id : Nat
  /-- The cycle in which the batch was added. -/
  addedCycle : Nat
  has : Bool
  req : Nat → Bool
  closed : Bool
  closedCycle : Nat
  /-- The roles whose conclusion run reviewed the member's cycle at the close,
  bound to it. -/
  conc : Nat → Bool
  /-- The roles whose conclusion run covered the member's cycle at the close. -/
  covered : Nat → Bool

inductive DebtKind where
  | drop
  | recovery
  deriving DecidableEq

/-- One owed review: of `role`, by a closed batch with an identity of at least
`since`, in the cycle the debt arose in or a later one. -/
structure Debt where
  kind : DebtKind
  role : Nat
  since : Nat
  cycle : Nat

structure Obs where
  planned : Bool
  bound : Bool
  done : Bool
  cycle : Nat
  nextId : Nat
  batches : List OBatch
  debts : List Debt
  /-- The roles dropped batches required of the member. -/
  dropped : Nat → Bool
  npRecord : Bool
  npReleased : Bool
  /-- The roles a drop took out of the plan while the no-plan record stood. -/
  npRoles : Nat → Bool
  cycSince : Option Nat

def obsInit : Obs where
  planned := false
  bound := false
  done := false
  cycle := 0
  nextId := 0
  batches := []
  debts := []
  dropped := fun _ => false
  npRecord := false
  npReleased := false
  npRoles := fun _ => false
  cycSince := none

/-- Some live batch requires the role (of any member). -/
def liveReq (o : Obs) (r : Nat) : Bool := o.batches.any fun b => b.req r

/-- Some batch from position `k` on requires the role. -/
def tailReq (o : Obs) (k r : Nat) : Bool := (o.batches.drop k).any fun b => b.req r

/-- Some batch from position `k` on names the member and requires the role. -/
def tailMemberReq (o : Obs) (k r : Nat) : Bool := (o.batches.drop k).any fun b => b.has && b.req r

/-- The roles the planned record owes: those of its debts. -/
def recDebtRole (o : Obs) (r : Nat) : Bool := o.debts.any fun d => d.kind == .recovery && d.role == r

/-- The roles a planned reset owes. -/
def resetRoles (o : Obs) (r : Nat) : Bool :=
  (if o.npRecord then o.npRoles r else recDebtRole o r) || liveReq o r || o.dropped r

/-- One debt of `kind` for each of the first `nr` roles that `g` names. -/
def debtsFor (nr : Nat) (kind : DebtKind) (since cycle : Nat) (g : Nat → Bool) : List Debt :=
  (List.range nr).filterMap fun r => if g r then some ⟨kind, r, since, cycle⟩ else none

/-- The first open batch that names the member, if any. -/
def firstOpenId (o : Obs) : Option Nat := (o.batches.find? fun b => b.has && !b.closed).map OBatch.id

/-- The observer's record of a closure in `cycle` with conclusions `cs`. -/
def closeB (cycle : Nat) (cs cv : Nat → Bool) (x : OBatch) : OBatch :=
  { x with closed := true, closedCycle := cycle, conc := cs, covered := cv }

def observe (nr : Nat) (o : Obs) : Ev → Obs
  | .create => { o with planned := true, bound := true }
  | .addBatch m rs =>
    { o with
      batches := o.batches ++ [⟨o.nextId, o.cycle, m, rs, false, 0, fun _ => false, fun _ => false⟩]
      nextId := o.nextId + 1 }
  | .close k cs cv =>
    { o with
      batches := o.batches.modify k (closeB o.cycle cs cv) }
  | .newCycle => { o with cycle := o.cycle + 1, bound := false }
  | .reset =>
    if !o.planned then
      { o with cycle := o.cycle + 1, bound := false, npRecord := true, npReleased := false, npRoles := fun _ => false }
    else if (List.range nr).any (resetRoles o) then
      { o with
        cycle := o.cycle + 1
        bound := false
        debts := o.debts ++ debtsFor nr .recovery o.nextId (o.cycle + 1) (resetRoles o)
        npRecord := false }
    else { o with cycle := o.cycle + 1, bound := false }
  | .rebind =>
    { o with
      bound := true
      cycSince := if o.batches.any (·.has) then some ((firstOpenId o).getD o.nextId) else o.cycSince }
  | .drop k d =>
    { o with
      batches := o.batches.take k
      debts := if !d && !o.done then o.debts ++ debtsFor nr .drop o.nextId o.cycle (tailMemberReq o k) else o.debts
      dropped := fun r => o.dropped r || tailMemberReq o k r
      npRoles := if o.npRecord then fun r => o.npRoles r || tailReq o k r else o.npRoles }
  | .dropTakes _ => o
  | .releaseDrop => { o with debts := o.debts.filter fun d => d.kind != .drop }
  | .releaseRecovery => { o with npReleased := true }
  | .done _ => { o with done := true }

/-- A debt is paid by a closed batch that names the member, requires the role,
was added after the debt arose, closed in the debt's cycle or a later one, and
whose conclusion of the role covered the cycle current at the close. -/
def Paid (o : Obs) (d : Debt) : Prop :=
  ∃ b ∈ o.batches, d.since ≤ b.id ∧ b.has = true ∧ b.req d.role = true ∧ b.closed = true ∧
    d.cycle ≤ b.closedCycle ∧ b.covered d.role = true

/-- A role reviewed in the current cycle: a closed batch names the member,
requires the role, and its conclusion of the role reviewed this cycle. -/
def ReviewedNow (o : Obs) (r : Nat) : Prop :=
  ∃ b ∈ o.batches, b.has = true ∧ b.req r = true ∧ b.closed = true ∧ b.closedCycle = o.cycle ∧ b.conc r = true

/-- The no-plan record is settled: while no role applies, by the release; once
one does, by a review of every applying role in the current cycle. -/
def NoPlanSettled (nr : Nat) (o : Obs) : Prop :=
  o.npRecord = true →
    let applies := fun r => o.npRoles r || liveReq o r
    ((∀ r, r < nr → applies r = false) → o.npReleased = true) ∧
      ((∃ r, r < nr ∧ applies r = true) → ∀ r, r < nr → applies r = true → ReviewedNow o r)

/-- The observer finds no review owed. -/
def Clear (nr : Nat) (o : Obs) : Prop :=
  (∀ d ∈ o.debts, Paid o d) ∧ NoPlanSettled nr o ∧
    (∀ s, o.cycSince = some s → ∃ b ∈ o.batches, b.has = true ∧ s ≤ b.id)

/-! ## Histories -/

/-- A history the machine accepts, from the initial state, in order. -/
inductive Accepts (nr b : Nat) : List Ev → St → Prop
  | nil : Accepts nr b [] initSt
  | snoc {h : List Ev} {st : St} (e : Ev) : Accepts nr b h st → Guard nr b st e → Accepts nr b (h ++ [e]) (post nr st e)

/-- The observer after a history. -/
def obsOf (nr : Nat) (h : List Ev) : Obs := h.foldl (observe nr) obsInit

theorem obsOf_snoc (nr : Nat) (h : List Ev) (e : Ev) : obsOf nr (h ++ [e]) = observe nr (obsOf nr h) e := by
  simp [obsOf, List.foldl_append]

/-! ## Positions and identities -/

/-- The number of live batches with an identity below `s`: the position from
which a batch has an identity of at least `s`. -/
def posOf (l : List OBatch) (s : Nat) : Nat := (l.filter fun b => decide (b.id < s)).length

/-- Identities strictly increase along the plan. -/
def Sorted (l : List OBatch) : Prop := ∀ i j (hi : i < l.length) (hj : j < l.length), i < j → l[i].id < l[j].id

theorem posOf_le_length (l : List OBatch) (s : Nat) : posOf l s ≤ l.length := List.length_filter_le _ _

theorem posOf_take_le {l : List OBatch} {s k : Nat} : posOf (l.take k) s ≤ posOf l s :=
  ((List.take_sublist k l).filter _).length_le

theorem posOf_take_le_k {l : List OBatch} {s k : Nat} : posOf (l.take k) s ≤ k :=
  Nat.le_trans (posOf_le_length _ _) (List.length_take_le k l)

theorem posOf_all {l : List OBatch} {s : Nat} (h : ∀ b ∈ l, b.id < s) : posOf l s = l.length := by
  simp only [posOf]
  rw [List.filter_eq_self.mpr (fun b hb => by simpa using h b hb)]

theorem lt_posOf {l : List OBatch} {s i : Nat} (hs : Sorted l) (hi : i < l.length) (h : l[i].id < s) :
    i < posOf l s := by
  have hall : ∀ b ∈ l.take (i + 1), b.id < s := by
    intro b hb
    obtain ⟨j, hj, rfl⟩ := List.getElem_of_mem hb
    simp only [List.length_take] at hj
    simp only [List.getElem_take]
    rcases Nat.lt_or_ge j i with hlt | hge
    · exact Nat.lt_trans (hs j i (by omega) hi hlt) h
    · have : j = i := by omega
      subst this; exact h
  have h1 := posOf_all hall
  have h2 : posOf (l.take (i + 1)) s ≤ posOf l s := posOf_take_le
  simp only [List.length_take] at h1
  omega

/-- A position at or above `posOf s` holds an identity of at least `s`. -/
theorem le_id_of_posOf {l : List OBatch} {s i : Nat} (hs : Sorted l) (hi : i < l.length) (h : posOf l s ≤ i) :
    s ≤ l[i].id := by
  rcases Nat.lt_or_ge l[i].id s with hlt | hge
  · exact absurd (lt_posOf hs hi hlt) (by omega)
  · exact hge

theorem posOf_append_of_le {l : List OBatch} {b : OBatch} {s : Nat} (h : s ≤ b.id) :
    posOf (l ++ [b]) s = posOf l s := by
  simp [posOf, List.filter_append, show ¬ b.id < s by omega]

/-- When every position from `k` on holds an identity of at least `s`, `posOf s`
is at most `k`. -/
theorem posOf_le_of {l : List OBatch} {s k : Nat} (hk : k ≤ l.length)
    (h : ∀ i (hi : i < l.length), k ≤ i → s ≤ l[i].id) : posOf l s ≤ k := by
  have hd : (l.drop k).filter (fun b => decide (b.id < s)) = [] := by
    rw [List.filter_eq_nil_iff]
    intro b hb
    obtain ⟨j, hj, rfl⟩ := List.getElem_of_mem hb
    simp only [List.length_drop] at hj
    simp only [List.getElem_drop, decide_eq_true_eq, Nat.not_lt]
    exact h (k + j) (by omega) (by omega)
  have : posOf l s = posOf (l.take k) s := by
    simp only [posOf]
    conv => lhs; rw [← List.take_append_drop k l]
    rw [List.filter_append, hd, List.append_nil]
  rw [this]
  exact posOf_take_le_k

/-- The identity at position `k` has `posOf` at most `k`. -/
theorem posOf_at {l : List OBatch} {k : Nat} (hs : Sorted l) (hk : k < l.length) : posOf l l[k].id ≤ k :=
  posOf_le_of (Nat.le_of_lt hk) fun i hi hki => by
    rcases Nat.eq_or_lt_of_le hki with rfl | hlt
    · exact Nat.le_refl _
    · exact Nat.le_of_lt (hs k i hk hi hlt)

end KanderFormal.Proofs.ReviewObligation
