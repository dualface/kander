import KanderFormal.Obligations
import KanderFormal.EvalLemmas
import KanderFormal.Machines.ReviewObligation

/-
The review obligation machine as Lean functions.

`St` holds the state with arrays and role sets as functions; `enc` encodes it
as a machine state. For every role list and parameter value, each event of the
machine definition, evaluated on an encoded state, gives the encoding of the
state `sstep` computes (`step_enc`), and no other result. The proofs about the
machine work on `St` and carry over through `enc`.
-/
namespace KanderFormal.Proofs.ReviewObligation

open KanderFormal KanderFormal.Machines.ReviewObligation

/-- The machine for the role list `roles`. -/
abbrev Mr (roles : List String) : Machine := obligationMachine roles

structure St where
  planned : Bool
  bound : Bool
  done : Bool
  count : Nat
  has : Nat → Bool
  /-- `req i r`: the batch at `i` requires role `r`. -/
  req : Nat → Nat → Bool
  closed : Nat → Bool
  cur : Nat → Nat → Bool
  cycNeed : Bool
  cycFrom : Nat
  dropRec : Nat → Bool
  dropFloor : Nat → Nat
  droppedRoles : Nat → Bool
  /-- 0 none, 1 written with no plan, 2 bound to the plan. -/
  recKind : Nat
  recRoles : Nat → Bool
  recFloor : Nat
  recReleased : Bool

def setV (n : Nat) (s : Nat → Bool) : Val := .set ((List.range n).map s)

/-- The encoding for `n` roles and arrays of length `b`. -/
def enc (n b : Nat) (st : St) : State := [
  ("planned", boolV st.planned), ("bound", boolV st.bound), ("done", boolV st.done),
  ("count", natV st.count),
  ("has", .arr ((List.range b).map fun i => boolV (st.has i))),
  ("req", .arr ((List.range b).map fun i => setV n (st.req i))),
  ("closed", .arr ((List.range b).map fun i => boolV (st.closed i))),
  ("cur", .arr ((List.range b).map fun i => setV n (st.cur i))),
  ("cycNeed", boolV st.cycNeed), ("cycFrom", natV st.cycFrom),
  ("dropRec", setV n st.dropRec),
  ("dropFloor", .map ((List.range n).map fun r => .nat (st.dropFloor r))),
  ("droppedRoles", setV n st.droppedRoles),
  ("rec", enumV st.recKind), ("recRoles", setV n st.recRoles),
  ("recFloor", natV st.recFloor), ("recReleased", boolV st.recReleased)]

/-- Two states with the same entries within `n` roles and `b` positions have the
same encoding. -/
structure Same (n b : Nat) (s t : St) : Prop where
  planned : s.planned = t.planned
  bound : s.bound = t.bound
  done : s.done = t.done
  count : s.count = t.count
  has : ∀ i, i < b → s.has i = t.has i
  req : ∀ i, i < b → ∀ r, r < n → s.req i r = t.req i r
  closed : ∀ i, i < b → s.closed i = t.closed i
  cur : ∀ i, i < b → ∀ r, r < n → s.cur i r = t.cur i r
  cycNeed : s.cycNeed = t.cycNeed
  cycFrom : s.cycFrom = t.cycFrom
  dropRec : ∀ r, r < n → s.dropRec r = t.dropRec r
  dropFloor : ∀ r, r < n → s.dropFloor r = t.dropFloor r
  droppedRoles : ∀ r, r < n → s.droppedRoles r = t.droppedRoles r
  recKind : s.recKind = t.recKind
  recRoles : ∀ r, r < n → s.recRoles r = t.recRoles r
  recFloor : s.recFloor = t.recFloor
  recReleased : s.recReleased = t.recReleased

theorem range_map_congr {α : Type} {n : Nat} {f g : Nat → α} (h : ∀ i, i < n → f i = g i) :
    (List.range n).map f = (List.range n).map g := by
  apply List.map_congr_left; intro i hi; exact h i (List.mem_range.mp hi)

theorem enc_same {n b : Nat} {s t : St} (h : Same n b s t) : enc n b s = enc n b t := by
  have set_eq : ∀ {f g : Nat → Bool}, (∀ r, r < n → f r = g r) → setV n f = setV n g :=
    fun hfg => by simp only [setV]; rw [range_map_congr hfg]
  have e1 : (List.range b).map (fun i => boolV (s.has i)) = (List.range b).map (fun i => boolV (t.has i)) :=
    range_map_congr fun i hi => by rw [h.has i hi]
  have e2 : (List.range b).map (fun i => setV n (s.req i)) = (List.range b).map (fun i => setV n (t.req i)) :=
    range_map_congr fun i hi => set_eq (h.req i hi)
  have e3 : (List.range b).map (fun i => boolV (s.closed i)) = (List.range b).map (fun i => boolV (t.closed i)) :=
    range_map_congr fun i hi => by rw [h.closed i hi]
  have e4 : (List.range b).map (fun i => setV n (s.cur i)) = (List.range b).map (fun i => setV n (t.cur i)) :=
    range_map_congr fun i hi => set_eq (h.cur i hi)
  have e5 : (List.range n).map (fun r => Scalar.nat (s.dropFloor r)) = (List.range n).map (fun r => Scalar.nat (t.dropFloor r)) :=
    range_map_congr fun r hr => by rw [h.dropFloor r hr]
  simp only [enc, h.planned, h.bound, h.done, h.count, h.cycNeed, h.cycFrom, h.recKind, h.recFloor, h.recReleased,
    set_eq h.dropRec, set_eq h.droppedRoles, set_eq h.recRoles, e1, e2, e3, e4, e5]

/-- The well-formedness every reachable state keeps: every position and floor
within the array length, the recovery kind in range. -/
structure WF (b : Nat) (st : St) : Prop where
  count : st.count ≤ b
  cycFrom : st.cycFrom ≤ b
  dropFloor : ∀ r, st.dropFloor r ≤ b
  recKind : st.recKind < 3
  recFloor : st.recFloor ≤ b

def initSt : St where
  planned := false
  bound := false
  done := false
  count := 0
  has := fun _ => false
  req := fun _ _ => false
  closed := fun _ => false
  cur := fun _ _ => false
  cycNeed := false
  cycFrom := 0
  dropRec := fun _ => false
  dropFloor := fun _ => 0
  droppedRoles := fun _ => false
  recKind := 0
  recRoles := fun _ => false
  recFloor := 0
  recReleased := false

theorem replicate_eq_range_map {α : Type} (n : Nat) (x : α) : List.replicate n x = (List.range n).map fun _ => x := by
  apply List.ext_getElem <;> simp

theorem initial_enc (roles : List String) (p : Params) :
    initial (Mr roles) p = enc roles.length (p "maxBatch") initSt := by
  simp only [initial, Mr, obligationMachine, List.map, initVal, enc, initSt, setV]
  have hc : ∀ {β : Type} (x : β), roles.map (fun _ => x) = (List.range roles.length).map (fun _ => x) := by
    intro β x; apply List.ext_getElem <;> simp
  simp [replicate_eq_range_map, boolV, natV, enumV, hc]

/-! ## Evaluation on an encoded state -/

/-- The environment of an event or query on the encoded state `st`. -/
def E (roles : List String) (p : Params) (st : St) (args : List (String × Val)) (bnd : List Val) : Env :=
  ⟨Mr roles, p, enc roles.length (p "maxBatch") st, args, bnd⟩

section
variable {roles : List String} {p : Params} {st : St} {args : List (String × Val)} {bnd : List Val}

theorem E_bound (v : Val) : { E roles p st args bnd with bound := v :: bnd } = E roles p st args (v :: bnd) := rfl

theorem size_role : (Mr roles).enumSize "role" = some roles.length := rfl

theorem setMem_setV {n r : Nat} {f : Nat → Bool} (h : r < n) :
    setMem (setV n f) (.scalar (.enum r)) = some (.scalar (.bool (f r))) := by
  simp [setMem, setV, h, boolV]

/-- Evaluates the field reads, literals, and scalar operators of an expression on
an encoded state. -/
macro "ev" : tactic => `(tactic| simp [eval, E, enc, lookup, setMem_setV, arrGet, mapGet, evalCmp, natAdd, boolAnd, boolOr,
  boolNot, indexOf, Machine.ctorsOf, Mr, obligationMachine, f, a, nat, tt, ff, eq, ne, lt, le, andE, orE, notE, iteE,
  recC, count, atI, roleAtI, inSet, boolV, natV, enumV])

theorem ev_atI_has {i : Nat} (hi : i < p "maxBatch") :
    eval (E roles p st args (natV i :: bnd)) (atI "has") = some (boolV (st.has i)) := by ev; simp [hi]

theorem ev_atI_closed {i : Nat} (hi : i < p "maxBatch") :
    eval (E roles p st args (natV i :: bnd)) (atI "closed") = some (boolV (st.closed i)) := by ev; simp [hi]

theorem ev_roleAtI_req {i r : Nat} (hi : i < p "maxBatch") (hr : r < roles.length) :
    eval (E roles p st args (natV i :: enumV r :: bnd)) (roleAtI "req") = some (boolV (st.req i r)) := by
  ev; simp [hi, setMem_setV hr]

theorem ev_roleAtI_cur {i r : Nat} (hi : i < p "maxBatch") (hr : r < roles.length) :
    eval (E roles p st args (natV i :: enumV r :: bnd)) (roleAtI "cur") = some (boolV (st.cur i r)) := by
  ev; simp [hi, setMem_setV hr]

end

/-! ## Helper expressions as Lean functions -/

/-- Some batch from `lo` to the end of the plan satisfies `g`. -/
def anyFrom (st : St) (lo : Nat) (g : Nat → Bool) : Bool := (indexRange lo st.count).any g

def anyReq (st : St) (lo r : Nat) : Bool := anyFrom st lo fun i => st.req i r
def memReq (st : St) (lo r : Nat) : Bool := anyFrom st lo fun i => st.has i && st.req i r
def closedReq (st : St) (lo r : Nat) : Bool := anyFrom st lo fun i => st.has i && (st.req i r && st.closed i)
def closedCur (st : St) (r : Nat) : Bool :=
  anyFrom st 0 fun i => st.has i && (st.req i r && (st.closed i && st.cur i r))

def recRoleIn (st : St) (r : Nat) : Bool :=
  (decide (st.recKind = 2) && st.recRoles r) || (decide (st.recKind = 1) && (st.recRoles r || anyReq st 0 r))
def recReviewed (st : St) (r : Nat) : Bool :=
  if st.recKind = 2 then closedReq st st.recFloor r else closedCur st r
def recMissing (st : St) (r : Nat) : Bool := recRoleIn st r && !recReviewed st r

/-- A role set is empty within `n` roles. -/
def noneOf (n : Nat) (g : Nat → Bool) : Bool := decide (∀ r, r < n → g r = false)

def recOwed (n : Nat) (st : St) : Bool :=
  !decide (st.recKind = 0) && (if noneOf n (recRoleIn st) then !st.recReleased else !noneOf n (recMissing st))

def dropOwedR (st : St) (mustClose : Bool) (r : Nat) : Bool :=
  st.dropRec r && !(if mustClose then closedReq st (st.dropFloor r) r else memReq st (st.dropFloor r) r)

def cycMissing (st : St) : Bool := st.cycNeed && !anyFrom st st.cycFrom st.has
def assignedS (st : St) : Bool := anyFrom st 0 st.has
def ownClosedS (st : St) : Bool := !anyFrom st 0 fun i => st.has i && !st.closed i

def doneReadyS (n : Nat) (st : St) : Bool :=
  st.planned && (st.bound && (!st.done && (assignedS st && (ownClosedS st && (!cycMissing st &&
    (!recOwed n st && noneOf n (dropOwedR st true)))))))

theorem setV_eq_iff {n : Nat} {f g : Nat → Bool} : setV n f = setV n g ↔ ∀ r, r < n → f r = g r := by
  simp only [setV, Val.set.injEq]
  constructor
  · intro h r hr
    have := congrArg (fun l => l[r]?) h
    simpa [hr] using this
  · intro h; exact range_map_congr h

theorem decide_setV_none {n : Nat} {g : Nat → Bool} :
    decide (setV n g = setV n fun _ => false) = noneOf n g := by
  simp only [noneOf]
  exact decide_eq_decide.mpr setV_eq_iff

section
variable {roles : List String} {p : Params} {st : St} {args : List (String × Val)} {bnd : List Val}

local notation "B" => p "maxBatch"
local notation "n" => roles.length

theorem ev_field_count : eval (E roles p st args bnd) count = some (natV st.count) := by ev
theorem ev_noRoles : eval (E roles p st args bnd) noRoles = some (setV n fun _ => false) := by
  unfold noRoles
  exact eval_setOf size_role fun i _ => by ev

theorem ev_anyFrom {lo body : Expr} {l : Nat} {g : Nat → Bool} (hc : st.count ≤ B)
    (hl : eval (E roles p st args bnd) lo = some (natV l))
    (hb : ∀ i, i < B → eval (E roles p st args (natV i :: bnd)) body = some (boolV (g i))) :
    eval (E roles p st args bnd) (.any lo count body) = some (boolV (anyFrom st l g)) :=
  eval_any hl ev_field_count fun i _ hi => hb i (by omega)

theorem ev_anyRequires {lo : Expr} {l r : Nat} (hc : st.count ≤ B) (hr : r < n)
    (hl : eval (E roles p st args (enumV r :: bnd)) lo = some (natV l)) :
    eval (E roles p st args (enumV r :: bnd)) (anyRequires lo) = some (boolV (anyReq st l r)) :=
  ev_anyFrom hc hl fun _ hi => ev_roleAtI_req hi hr

theorem ev_memberRequires {lo : Expr} {l r : Nat} (hc : st.count ≤ B) (hr : r < n)
    (hl : eval (E roles p st args (enumV r :: bnd)) lo = some (natV l)) :
    eval (E roles p st args (enumV r :: bnd)) (memberRequires lo) = some (boolV (memReq st l r)) :=
  ev_anyFrom hc hl fun _ hi => eval_and (ev_atI_has hi) (ev_roleAtI_req hi hr)

theorem ev_closedRequires {lo : Expr} {l r : Nat} (hc : st.count ≤ B) (hr : r < n)
    (hl : eval (E roles p st args (enumV r :: bnd)) lo = some (natV l)) :
    eval (E roles p st args (enumV r :: bnd)) (closedRequires lo) = some (boolV (closedReq st l r)) :=
  ev_anyFrom hc hl fun _ hi => eval_and (ev_atI_has hi) (eval_and (ev_roleAtI_req hi hr) (ev_atI_closed hi))

theorem ev_closedCurrent {r : Nat} (hc : st.count ≤ B) (hr : r < n) :
    eval (E roles p st args (enumV r :: bnd)) closedCurrent = some (boolV (closedCur st r)) :=
  ev_anyFrom hc (by ev) fun _ hi => eval_and (ev_atI_has hi) (eval_and (ev_roleAtI_req hi hr)
    (eval_and (ev_atI_closed hi) (ev_roleAtI_cur hi hr)))

theorem ev_inSet_dropRec {r : Nat} (hr : r < n) :
    eval (E roles p st args (enumV r :: bnd)) (inSet "dropRec") = some (boolV (st.dropRec r)) := by
  ev; simp [setMem_setV hr]
theorem ev_inSet_recRoles {r : Nat} (hr : r < n) :
    eval (E roles p st args (enumV r :: bnd)) (inSet "recRoles") = some (boolV (st.recRoles r)) := by
  ev; simp [setMem_setV hr]
theorem ev_inSet_droppedRoles {r : Nat} (hr : r < n) :
    eval (E roles p st args (enumV r :: bnd)) (inSet "droppedRoles") = some (boolV (st.droppedRoles r)) := by
  ev; simp [setMem_setV hr]

theorem ev_rec_none : eval (E roles p st args bnd) (eq (f "rec") (recC "none")) = some (boolV (decide (st.recKind = 0))) := by
  ev
theorem ev_rec_noPlan : eval (E roles p st args bnd) (eq (f "rec") (recC "noPlan")) = some (boolV (decide (st.recKind = 1))) := by
  ev
theorem ev_rec_planned : eval (E roles p st args bnd) (eq (f "rec") (recC "planned")) = some (boolV (decide (st.recKind = 2))) := by
  ev

theorem ev_recoveryRoleIn {r : Nat} (hc : st.count ≤ B) (hr : r < n) :
    eval (E roles p st args (enumV r :: bnd)) recoveryRoleIn = some (boolV (recRoleIn st r)) := by
  unfold recoveryRoleIn recRoleIn
  exact eval_or (eval_and ev_rec_planned (ev_inSet_recRoles hr))
    (eval_and ev_rec_noPlan (eval_or (ev_inSet_recRoles hr) (ev_anyRequires hc hr (by ev))))

theorem ev_field_recFloor : eval (E roles p st args bnd) (f "recFloor") = some (natV st.recFloor) := by ev
theorem ev_dropFloor_r {r : Nat} (hr : r < n) :
    eval (E roles p st args (enumV r :: bnd)) (.get (f "dropFloor") (.bvar 0)) = some (natV (st.dropFloor r)) := by
  ev; simp [hr]

theorem ev_recoveryReviewed {r : Nat} (hc : st.count ≤ B) (hr : r < n) :
    eval (E roles p st args (enumV r :: bnd)) recoveryReviewed = some (boolV (recReviewed st r)) := by
  unfold recoveryReviewed recReviewed iteE
  rw [eval_ite ev_rec_planned (ev_closedRequires hc hr ev_field_recFloor) (ev_closedCurrent hc hr)]
  by_cases h : st.recKind = 2 <;> simp [h]

theorem ev_recoveryRoles (hc : st.count ≤ B) :
    eval (E roles p st args bnd) recoveryRoles = some (setV n (recRoleIn st)) :=
  eval_setOf size_role fun _ hr => ev_recoveryRoleIn hc hr

theorem ev_recoveryMissing (hc : st.count ≤ B) :
    eval (E roles p st args bnd) recoveryMissing = some (setV n (recMissing st)) :=
  eval_setOf size_role fun _ hr => eval_and (ev_recoveryRoleIn hc hr) (eval_not (ev_recoveryReviewed hc hr))

theorem ev_recoveryOwed (hc : st.count ≤ B) :
    eval (E roles p st args bnd) recoveryOwed = some (boolV (recOwed n st)) := by
  unfold recoveryOwed recOwed iteE
  have hrr := eval_eq (ev_recoveryRoles (roles := roles) (args := args) (bnd := bnd) hc) ev_noRoles
  have hmm := eval_ne (ev_recoveryMissing (roles := roles) (args := args) (bnd := bnd) hc) ev_noRoles
  rw [decide_setV_none] at hrr
  rw [decide_setV_none] at hmm
  have hi : eval (E roles p st args bnd) (iteE (eq recoveryRoles noRoles) (notE (f "recReleased")) (ne recoveryMissing noRoles)) =
      some (boolV (if noneOf n (recRoleIn st) then !st.recReleased else !noneOf n (recMissing st))) := by
    refine (eval_ite hrr (eval_not (a := st.recReleased) (by ev)) hmm).trans ?_
    cases noneOf n (recRoleIn st) <;> rfl
  refine (eval_and (eval_ne (v := enumV st.recKind) (w := enumV 0) (by ev) (by ev)) hi).trans ?_
  simp [enumV]

theorem ev_dropOwed (mc : Bool) (hc : st.count ≤ B) :
    eval (E roles p st args bnd) (dropOwed mc) = some (setV n (dropOwedR st mc)) := by
  unfold dropOwed
  refine eval_setOf size_role fun r hr => ?_
  cases mc
  · exact eval_and (ev_inSet_dropRec hr) (eval_not (ev_memberRequires hc hr (ev_dropFloor_r hr)))
  · exact eval_and (ev_inSet_dropRec hr) (eval_not (ev_closedRequires hc hr (ev_dropFloor_r hr)))

theorem ev_cycleMissing (hc : st.count ≤ B) :
    eval (E roles p st args bnd) cycleMissing = some (boolV (cycMissing st)) :=
  eval_and (by ev) (eval_not (ev_anyFrom hc (by ev) fun _ hi => ev_atI_has hi))

theorem ev_assigned (hc : st.count ≤ B) :
    eval (E roles p st args bnd) assigned = some (boolV (assignedS st)) :=
  ev_anyFrom hc (by ev) fun _ hi => ev_atI_has hi

theorem ev_ownClosed (hc : st.count ≤ B) :
    eval (E roles p st args bnd) ownClosed = some (boolV (ownClosedS st)) :=
  eval_not (ev_anyFrom hc (by ev) fun _ hi => eval_and (ev_atI_has hi) (eval_not (ev_atI_closed hi)))

theorem ev_doneReady (hc : st.count ≤ B) :
    eval (E roles p st args bnd) doneReady = some (boolV (doneReadyS n st)) := by
  unfold doneReady doneReadyS
  have hd := eval_eq (ev_dropOwed (roles := roles) (args := args) (bnd := bnd) true hc) ev_noRoles
  rw [decide_setV_none] at hd
  exact eval_and (by ev) (eval_and (by ev) (eval_and (eval_not (by ev)) (eval_and (ev_assigned hc)
    (eval_and (ev_ownClosed hc) (eval_and (eval_not (ev_cycleMissing hc))
      (eval_and (eval_not (ev_recoveryOwed hc)) hd))))))

end

/-! ## Accepted events -/

theorem assigns_cons {env : Env} {f : String} {x : Expr} {as : List (String × Expr)} {us : List (String × Val)}
    (h : AssignsMatch env ((f, x) :: as) us) :
    ∃ v us', us = (f, v) :: us' ∧ eval env x = some v ∧ AssignsMatch env as us' := by
  cases h with
  | cons hv _ ht => exact ⟨_, _, rfl, hv, ht⟩

theorem assigns_nil {env : Env} {us : List (String × Val)} (h : AssignsMatch env [] us) : us = [] := by
  cases h; rfl

theorem set_of_list {bs : List Bool} {k : Nat} (h : bs.length = k) :
    Val.set bs = setV k (fun r => bs.getD r false) := by
  subst h
  simp only [setV, Val.set.injEq]
  apply List.ext_getElem <;> simp
  intro i hi; simp [List.getElem?_eq_getElem hi]

/-- Function update at one position. -/
def upd {α : Type} (g : Nat → α) (i : Nat) (x : α) : Nat → α := fun j => if j = i then x else g j

/-- Function fill from one position on. -/
def fillFn {α : Type} (g : Nat → α) (lo : Nat) (x : α) : Nat → α := fun j => if lo ≤ j then x else g j

theorem arr_put {α : Type} {b i : Nat} {g : Nat → α} {e : α → Val} {x : α} (h : i < b) :
    arrPut (.arr ((List.range b).map fun j => e (g j))) (natV i) (e x) =
      some (.arr ((List.range b).map fun j => e (upd g i x j))) := by
  simp only [arrPut, natV, List.length_map, List.length_range, h, ite_true, Option.some.injEq, Val.arr.injEq]
  rw [range_map_set]
  apply range_map_congr
  intro j _
  simp only [upd]
  split <;> rfl

theorem arr_fill {α : Type} {b lo : Nat} {g : Nat → α} {e : α → Val} {x : α} :
    fillFrom (.arr ((List.range b).map fun j => e (g j))) (natV lo) (e x) =
      some (.arr ((List.range b).map fun j => e (fillFn g lo x j))) := by
  simp only [fillFrom, natV, List.length_map, List.length_range, Option.some.injEq, Val.arr.injEq]
  rw [fill_range_map]
  apply range_map_congr
  intro j _
  simp only [fillFn]
  split <;> rfl

section
variable {roles : List String} {p : Params} {st : St} {args : List (String × Val)} {us : List (String × Val)}

local notation "B" => p "maxBatch"
local notation "n" => roles.length

theorem bool_true {x : Bool} (h : some (boolV x) = some (boolV true)) : x = true := by
  simpa [boolV] using h

theorem acc_create (hg : eval (E roles p st args []) createEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) createEvent.assigns = some us) :
    st.planned = false ∧ st.done = false ∧
      applyUpdates (enc n B st) us = enc n B { st with planned := true, bound := true } := by
  have hg' := bool_true ((eval_and (eval_not (a := st.planned) (by ev)) (eval_not (a := st.done) (by ev))).symm.trans hg)
  obtain ⟨v1, us1, rfl, h1, hm⟩ := assigns_cons (evalAssigns_spec hu)
  obtain ⟨v2, us2, rfl, h2, hm⟩ := assigns_cons hm
  rw [assigns_nil hm]
  simp only [eval, tt, Option.some.injEq] at h1 h2
  subst h1 h2
  simp only [Bool.and_eq_true, Bool.not_eq_true'] at hg'
  exact ⟨hg'.1, hg'.2, by simp [applyUpdates, enc, lookup]⟩

theorem ev_aput {env : Env} {x i v : Expr} {α : Type} {b k : Nat} {g : Nat → α} {e : α → Val} {y : α}
    (hx : eval env x = some (.arr ((List.range b).map fun j => e (g j)))) (hi : eval env i = some (natV k))
    (hv : eval env v = some (e y)) (hk : k < b) :
    eval env (.aput x i v) = some (.arr ((List.range b).map fun j => e (upd g k y j))) := by
  simp only [eval, hx, hi, hv, Option.bind_some]
  exact arr_put hk

theorem ev_afill {env : Env} {x lo v : Expr} {α : Type} {b k : Nat} {g : Nat → α} {e : α → Val} {y : α}
    (hx : eval env x = some (.arr ((List.range b).map fun j => e (g j)))) (hl : eval env lo = some (natV k))
    (hv : eval env v = some (e y)) :
    eval env (.afill x lo v) = some (.arr ((List.range b).map fun j => e (fillFn g k y j))) := by
  simp only [eval, hx, hl, hv, Option.bind_some]
  exact arr_fill

theorem ev_has : eval (E roles p st args []) (f "has") = some (.arr ((List.range B).map fun j => boolV (st.has j))) := by ev
theorem ev_req : eval (E roles p st args []) (f "req") = some (.arr ((List.range B).map fun j => setV n (st.req j))) := by ev
theorem ev_closed : eval (E roles p st args []) (f "closed") = some (.arr ((List.range B).map fun j => boolV (st.closed j))) := by ev
theorem ev_cur : eval (E roles p st args []) (f "cur") = some (.arr ((List.range B).map fun j => setV n (st.cur j))) := by ev
theorem ev_noRoles0 : eval (E roles p st args []) noRoles = some (setV n fun _ => false) := ev_noRoles
theorem ev_ff : eval (E roles p st args []) ff = some (boolV false) := rfl
theorem ev_tt : eval (E roles p st args []) tt = some (boolV true) := rfl

/-- `planned`, `bound`, `done` from a guard leaf. -/
theorem leaf {x : Expr} {a : Bool} (h1 : eval (E roles p st args []) x = some (boolV true))
    (h2 : eval (E roles p st args []) x = some (boolV a)) : a = true := (bool_eq_of h2 h1)

theorem ev_planned : eval (E roles p st args []) (f "planned") = some (boolV st.planned) := by ev
theorem ev_boundF : eval (E roles p st args []) (f "bound") = some (boolV st.bound) := by ev
theorem ev_done : eval (E roles p st args []) (f "done") = some (boolV st.done) := by ev
theorem ev_maxBatch : eval (E roles p st args []) (.param "maxBatch") = some (natV B) := rfl

def addBatchS (st : St) (m : Bool) (rs : Nat → Bool) : St :=
  { st with
    count := st.count + 1
    has := upd st.has st.count m
    req := upd st.req st.count rs
    closed := upd st.closed st.count false
    cur := upd st.cur st.count (fun _ => false) }

theorem acc_addBatch {m : Bool} {R : Nat → Bool} (hargs : args = [("member", boolV m), ("roles", setV n R)])
    (hg : eval (E roles p st args []) addBatchEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) addBatchEvent.assigns = some us) :
    st.planned = true ∧ st.count < B ∧ (st.done && m) = false ∧
      applyUpdates (enc n B st) us = enc n B (addBatchS st m R) := by
  subst hargs
  have hm : eval (E roles p st [("member", boolV m), ("roles", setV n R)] []) (a "member") = some (boolV m) := by ev
  have hR : eval (E roles p st [("member", boolV m), ("roles", setV n R)] []) (a "roles") = some (setV n R) := by ev
  obtain ⟨g1, g2⟩ := eval_and_true hg
  obtain ⟨g2, g3⟩ := eval_and_true g2
  have hp := leaf g1 ev_planned
  have hc := leaf g2 (eval_lt ev_field_count ev_maxBatch)
  have hd := leaf g3 (eval_not (eval_and ev_done hm))
  simp only [decide_eq_true_eq, Bool.not_eq_true'] at hc hd
  obtain ⟨v1, us1, rfl, h1, hm1⟩ := assigns_cons (evalAssigns_spec hu)
  obtain ⟨v2, us2, rfl, h2, hm2⟩ := assigns_cons hm1
  obtain ⟨v3, us3, rfl, h3, hm3⟩ := assigns_cons hm2
  obtain ⟨v4, us4, rfl, h4, hm4⟩ := assigns_cons hm3
  obtain ⟨v5, us5, rfl, h5, hm5⟩ := assigns_cons hm4
  rw [assigns_nil hm5]
  have e1 := eval_val_eq h1 (by simp [eval, ev_field_count, natAdd, natV, nat] : _ = some (natV (st.count + 1)))
  have e2 := eval_val_eq h2 (ev_aput ev_has ev_field_count hm hc)
  have e3 := eval_val_eq h3 (ev_aput (e := setV n) ev_req ev_field_count hR hc)
  have e4 := eval_val_eq h4 (ev_aput ev_closed ev_field_count ev_ff hc)
  have e5 := eval_val_eq h5 (ev_aput (e := setV n) ev_cur ev_field_count ev_noRoles0 hc)
  subst e1 e2 e3 e4 e5
  exact ⟨hp, hc, hd, by simp [applyUpdates, enc, lookup, addBatchS]⟩

theorem ev_aget {env : Env} {x i : Expr} {α : Type} {b k : Nat} {g : Nat → α} {e : α → Val}
    (hx : eval env x = some (.arr ((List.range b).map fun j => e (g j)))) (hi : eval env i = some (natV k))
    (hk : k < b) : eval env (.aget x i) = some (e (g k)) := by
  simp [eval, hx, hi, arrGet, natV, hk]

def closeS (st : St) (k : Nat) (cs : Nat → Bool) : St :=
  { st with
    closed := upd st.closed k true
    cur := upd st.cur k cs }

theorem acc_close {k : Nat} {cs : Nat → Bool} (hw : WF B st) (hargs : args = [("at", natV k), ("current", setV n cs)])
    (hg : eval (E roles p st args []) closeEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) closeEvent.assigns = some us) :
    st.planned = true ∧ st.bound = true ∧ k < st.count ∧ st.closed k = false ∧
      applyUpdates (enc n B st) us = enc n B (closeS st k cs) := by
  subst hargs
  have hk : eval (E roles p st [("at", natV k), ("current", setV n cs)] []) (a "at") = some (natV k) := by ev
  have hC : eval (E roles p st [("at", natV k), ("current", setV n cs)] []) (a "current") = some (setV n cs) := by ev
  obtain ⟨g1, g2⟩ := eval_and_true hg
  obtain ⟨g2, g3⟩ := eval_and_true g2
  obtain ⟨g3, g4⟩ := eval_and_true g3
  have hp := leaf g1 ev_planned
  have hb := leaf g2 ev_boundF
  have hlt := leaf g3 (eval_lt hk ev_field_count)
  simp only [decide_eq_true_eq] at hlt
  have hkb : k < B := Nat.lt_of_lt_of_le hlt hw.count
  have hcl := leaf g4 (eval_not (ev_aget ev_closed hk hkb))
  simp only [Bool.not_eq_true'] at hcl
  obtain ⟨v1, us1, rfl, h1, hm1⟩ := assigns_cons (evalAssigns_spec hu)
  obtain ⟨v2, us2, rfl, h2, hm2⟩ := assigns_cons hm1
  rw [assigns_nil hm2]
  have e1 := eval_val_eq h1 (ev_aput ev_closed hk ev_tt hkb)
  have e2 := eval_val_eq h2 (ev_aput (e := setV n) ev_cur hk hC hkb)
  subst e1 e2
  exact ⟨hp, hb, hlt, hcl, by simp [applyUpdates, enc, lookup, closeS]⟩

def newCycleS (st : St) : St :=
  { st with
    bound := false
    cur := fillFn st.cur 0 (fun _ => false) }

theorem acc_newCycle (hargs : args = [])
    (hg : eval (E roles p st args []) newCycleEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) newCycleEvent.assigns = some us) :
    st.done = false ∧ applyUpdates (enc n B st) us = enc n B (newCycleS st) := by
  subst hargs
  have hd := leaf hg (eval_not ev_done)
  simp only [Bool.not_eq_true'] at hd
  obtain ⟨v1, us1, rfl, h1, hm1⟩ := assigns_cons (evalAssigns_spec hu)
  obtain ⟨v2, us2, rfl, h2, hm2⟩ := assigns_cons hm1
  rw [assigns_nil hm2]
  have e1 := eval_val_eq h1 ev_ff
  have e2 := eval_val_eq h2 (ev_afill (e := setV n) ev_cur (by rfl : _ = some (natV 0)) ev_noRoles0)
  subst e1 e2
  exact ⟨hd, by simp [applyUpdates, enc, lookup, newCycleS]⟩

/-- The roles a reset owes under a plan. -/
def resetRoleS (st : St) (r : Nat) : Bool := st.recRoles r || (anyReq st 0 r || st.droppedRoles r)
def resetKeepsS (nr : Nat) (st : St) : Bool := st.planned && noneOf nr (resetRoleS st)

def resetS (nr : Nat) (st : St) : St :=
  { st with
    bound := false
    cur := fillFn st.cur 0 (fun _ => false)
    recKind := if st.planned then (if resetKeepsS nr st then st.recKind else 2) else 1
    recRoles := if st.planned then (if resetKeepsS nr st then st.recRoles else resetRoleS st) else (fun _ => false)
    recFloor := if st.planned then (if resetKeepsS nr st then st.recFloor else st.count) else 0
    recReleased := if resetKeepsS nr st then st.recReleased else false }

theorem ev_resetRoles (hc : st.count ≤ B) :
    eval (E roles p st args []) resetRoles = some (setV n (resetRoleS st)) :=
  eval_setOf size_role fun _ hr =>
    eval_or (ev_inSet_recRoles hr) (eval_or (ev_anyRequires hc hr (by ev)) (ev_inSet_droppedRoles hr))

theorem ev_resetKeeps (hc : st.count ≤ B) :
    eval (E roles p st args []) resetKeeps = some (boolV (resetKeepsS n st)) := by
  have h := eval_eq (ev_resetRoles (roles := roles) (args := args) hc) ev_noRoles0
  rw [decide_setV_none] at h
  exact eval_and ev_planned h

theorem acc_reset (hw : WF B st) (hargs : args = [])
    (hg : eval (E roles p st args []) resetEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) resetEvent.assigns = some us) :
    st.done = false ∧ applyUpdates (enc n B st) us = enc n B (resetS n st) := by
  subst hargs
  have hd := leaf hg (eval_not ev_done)
  simp only [Bool.not_eq_true'] at hd
  obtain ⟨v1, us1, rfl, h1, hm1⟩ := assigns_cons (evalAssigns_spec hu)
  obtain ⟨v2, us2, rfl, h2, hm2⟩ := assigns_cons hm1
  obtain ⟨v3, us3, rfl, h3, hm3⟩ := assigns_cons hm2
  obtain ⟨v4, us4, rfl, h4, hm4⟩ := assigns_cons hm3
  obtain ⟨v5, us5, rfl, h5, hm5⟩ := assigns_cons hm4
  obtain ⟨v6, us6, rfl, h6, hm6⟩ := assigns_cons hm5
  rw [assigns_nil hm6]
  have hk := ev_resetKeeps (roles := roles) (args := []) hw.count
  have e1 := eval_val_eq h1 ev_ff
  have e2 := eval_val_eq h2 (ev_afill (e := setV n) ev_cur (by rfl : _ = some (natV 0)) ev_noRoles0)
  have e3 := eval_val_eq h3 (eval_ite ev_planned (eval_ite hk (by ev : _ = some (enumV st.recKind)) (by ev : _ = some (enumV 2)))
    (by ev : _ = some (enumV 1)))
  have e4 := eval_val_eq h4 (eval_ite ev_planned (eval_ite hk (by ev : _ = some (setV n st.recRoles))
    (ev_resetRoles hw.count)) ev_noRoles0)
  have e5 := eval_val_eq h5 (eval_ite ev_planned (eval_ite hk (by ev : _ = some (natV st.recFloor)) ev_field_count)
    (by rfl : _ = some (natV 0)))
  have e6 := eval_val_eq h6 (eval_ite hk (by ev : _ = some (boolV st.recReleased)) ev_ff)
  subst e1 e2 e3 e4 e5 e6
  refine ⟨hd, ?_⟩
  cases hpl : st.planned <;> cases hks : resetKeepsS n st <;>
    simp [applyUpdates, enc, lookup, resetS, hpl, hks, enumV]

/-- The first open batch of the member, or the end of the plan. -/
def firstOpen (st : St) : Nat := ((indexRange 0 st.count).find? fun i => st.has i && !st.closed i).getD st.count

def rebindS (st : St) : St :=
  { st with
    bound := true
    cycNeed := st.cycNeed || assignedS st
    cycFrom := if assignedS st then firstOpen st else st.cycFrom }

theorem acc_rebind (hw : WF B st) (hargs : args = [])
    (hg : eval (E roles p st args []) rebindEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) rebindEvent.assigns = some us) :
    st.planned = true ∧ st.bound = false ∧ st.done = false ∧
      applyUpdates (enc n B st) us = enc n B (rebindS st) := by
  subst hargs
  obtain ⟨g1, g2⟩ := eval_and_true hg
  obtain ⟨g2, g3⟩ := eval_and_true g2
  have hp := leaf g1 ev_planned
  have hb := leaf g2 (eval_not ev_boundF)
  have hd := leaf g3 (eval_not ev_done)
  simp only [Bool.not_eq_true'] at hb hd
  obtain ⟨v1, us1, rfl, h1, hm1⟩ := assigns_cons (evalAssigns_spec hu)
  obtain ⟨v2, us2, rfl, h2, hm2⟩ := assigns_cons hm1
  obtain ⟨v3, us3, rfl, h3, hm3⟩ := assigns_cons hm2
  rw [assigns_nil hm3]
  have ha := ev_assigned (roles := roles) (args := []) (bnd := []) hw.count
  have hf : eval (E roles p st [] []) (.first (nat 0) count (andE (atI "has") (notE (atI "closed")))) =
      some (natV (firstOpen st)) :=
    eval_first (by rfl) ev_field_count fun i _ hi =>
      eval_and (ev_atI_has (Nat.lt_of_lt_of_le hi hw.count)) (eval_not (ev_atI_closed (Nat.lt_of_lt_of_le hi hw.count)))
  have e1 := eval_val_eq h1 ev_tt
  have e2 := eval_val_eq h2 (eval_or (by ev : _ = some (boolV st.cycNeed)) ha)
  have e3 := eval_val_eq h3 (eval_ite ha hf (by ev : _ = some (natV st.cycFrom)))
  subst e1 e2 e3
  refine ⟨hp, hb, hd, ?_⟩
  cases has : assignedS st <;> simp [applyUpdates, enc, lookup, rebindS, has]

/-- The drop of the batches from `k` on, with a decision when `d`. -/
def dropTakesS (st : St) (d : Bool) : Bool := !d && !st.done

def dropS (st : St) (k : Nat) (d : Bool) : St :=
  { st with
    count := k
    has := fillFn st.has k false
    req := fillFn st.req k (fun _ => false)
    closed := fillFn st.closed k false
    cur := fillFn st.cur k (fun _ => false)
    dropRec := fun r => st.dropRec r || (dropTakesS st d && memReq st k r)
    dropFloor := fun r => if (dropTakesS st d && memReq st k r) then k
      else if decide (st.dropFloor r ≤ k) then st.dropFloor r else k
    droppedRoles := fun r => st.droppedRoles r || memReq st k r
    cycFrom := if decide (st.cycFrom ≤ k) then st.cycFrom else k
    recRoles := if decide (st.recKind = 1) then (fun r => st.recRoles r || anyReq st k r) else st.recRoles
    recFloor := if (decide (st.recKind = 2) && decide (k < st.recFloor)) then k else st.recFloor }

theorem acc_drop {k : Nat} {d : Bool} (hw : WF B st) (hargs : args = [("keep", natV k), ("decision", boolV d)])
    (hg : eval (E roles p st args []) dropEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) dropEvent.assigns = some us) :
    st.planned = true ∧ 1 ≤ k ∧ k < st.count ∧ anyFrom st k st.closed = false ∧
      applyUpdates (enc n B st) us = enc n B (dropS st k d) := by
  subst hargs
  have hc := hw.count
  have hk : ∀ bnd, eval (E roles p st [("keep", natV k), ("decision", boolV d)] bnd) (a "keep") = some (natV k) := by
    intro bnd; ev
  have hdec : ∀ bnd, eval (E roles p st [("keep", natV k), ("decision", boolV d)] bnd) (a "decision") = some (boolV d) := by
    intro bnd; ev
  obtain ⟨g1, g2⟩ := eval_and_true hg
  obtain ⟨g2, g3⟩ := eval_and_true g2
  obtain ⟨g3, g4⟩ := eval_and_true g3
  have hp := leaf g1 ev_planned
  have h1k := leaf g2 (eval_le (by rfl : _ = some (natV 1)) (hk []))
  have hlt := leaf g3 (eval_lt (hk []) ev_field_count)
  have hcl := leaf g4 (eval_not (ev_anyFrom hc (hk []) fun _ hi => ev_atI_closed hi))
  simp only [decide_eq_true_eq, Bool.not_eq_true'] at h1k hlt hcl
  have htakes : ∀ r, r < n → eval (E roles p st [("keep", natV k), ("decision", boolV d)] [enumV r])
      (andE dropTakes droppedFor) = some (boolV (dropTakesS st d && memReq st k r)) := fun r hr =>
    eval_and (eval_and (eval_not (hdec _)) (eval_not (by ev))) (ev_memberRequires hc hr (hk _))
  obtain ⟨v1, us1, rfl, e1, hm⟩ := assigns_cons (evalAssigns_spec hu)
  obtain ⟨v2, us2, rfl, e2, hm⟩ := assigns_cons hm
  obtain ⟨v3, us3, rfl, e3, hm⟩ := assigns_cons hm
  obtain ⟨v4, us4, rfl, e4, hm⟩ := assigns_cons hm
  obtain ⟨v5, us5, rfl, e5, hm⟩ := assigns_cons hm
  obtain ⟨v6, us6, rfl, e6, hm⟩ := assigns_cons hm
  obtain ⟨v7, us7, rfl, e7, hm⟩ := assigns_cons hm
  obtain ⟨v8, us8, rfl, e8, hm⟩ := assigns_cons hm
  obtain ⟨v9, us9, rfl, e9, hm⟩ := assigns_cons hm
  obtain ⟨v10, us10, rfl, e10, hm⟩ := assigns_cons hm
  obtain ⟨v11, us11, rfl, e11, hm⟩ := assigns_cons hm
  rw [assigns_nil hm]
  have q1 := eval_val_eq e1 (hk [])
  have q2 := eval_val_eq e2 (ev_afill ev_has (hk []) ev_ff)
  have q3 := eval_val_eq e3 (ev_afill (e := setV n) ev_req (hk []) ev_noRoles0)
  have q4 := eval_val_eq e4 (ev_afill ev_closed (hk []) ev_ff)
  have q5 := eval_val_eq e5 (ev_afill (e := setV n) ev_cur (hk []) ev_noRoles0)
  have q6 := eval_val_eq e6 (eval_setOf size_role fun r hr => eval_or (ev_inSet_dropRec hr) (htakes r hr))
  have q7 := eval_val_eq e7 (eval_mapOf (g := fun r => .nat (if (dropTakesS st d && memReq st k r) then k
      else if decide (st.dropFloor r ≤ k) then st.dropFloor r else k)) size_role fun r hr => by
    refine (eval_ite (htakes r hr) (hk _) (eval_ite (eval_le (ev_dropFloor_r hr) (hk _)) (ev_dropFloor_r hr) (hk _))).trans ?_
    cases dropTakesS st d && memReq st k r <;> cases decide (st.dropFloor r ≤ k) <;> rfl)
  have q8 := eval_val_eq e8 (eval_setOf size_role fun r hr =>
    eval_or (ev_inSet_droppedRoles hr) (ev_memberRequires hc hr (hk _)))
  have q9 := eval_val_eq e9 (eval_ite (eval_le (by ev : _ = some (natV st.cycFrom)) (hk [])) (by ev : _ = some (natV st.cycFrom)) (hk []))
  have q10 := eval_val_eq e10 (eval_ite ev_rec_noPlan (eval_setOf size_role fun r hr =>
    eval_or (ev_inSet_recRoles hr) (ev_anyRequires hc hr (hk _))) (by ev : _ = some (setV n st.recRoles)))
  have q11 := eval_val_eq e11 (eval_ite (eval_and ev_rec_planned (eval_lt (hk []) ev_field_recFloor)) (hk [])
    ev_field_recFloor)
  subst q1 q2 q3 q4 q5 q6 q7 q8 q9 q10 q11
  refine ⟨hp, h1k, hlt, hcl, ?_⟩
  have c9 : (if decide (st.cycFrom ≤ k) = true then natV st.cycFrom else natV k) = natV (dropS st k d).cycFrom := by
    simp only [dropS]; split <;> rfl
  have c10 : (if decide (st.recKind = 1) = true then Val.set ((List.range n).map fun r => st.recRoles r || anyReq st k r)
      else setV n st.recRoles) = setV n (dropS st k d).recRoles := by
    simp only [dropS]; split <;> rfl
  have c11 : (if (decide (st.recKind = 2) && decide (k < st.recFloor)) = true then natV k else natV st.recFloor) =
      natV (dropS st k d).recFloor := by
    simp only [dropS]; split <;> rfl
  rw [c9, c10, c11]
  simp only [applyUpdates, enc, lookup, List.map]
  simp [dropS, setV]

def releaseDropS (st : St) : St :=
  { st with
    dropRec := fun _ => false
    dropFloor := fillFn st.dropFloor 0 0 }

theorem acc_releaseDrop (hargs : args = [])
    (hg : eval (E roles p st args []) releaseDropEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) releaseDropEvent.assigns = some us) :
    st.planned = true ∧ noneOf n st.dropRec = false ∧
      applyUpdates (enc n B st) us = enc n B (releaseDropS st) := by
  subst hargs
  obtain ⟨g1, g2⟩ := eval_and_true hg
  have hp := leaf g1 ev_planned
  have h := eval_ne (by ev : eval (E roles p st [] []) (f "dropRec") = some (setV n st.dropRec)) ev_noRoles0
  rw [show (!decide (setV n st.dropRec = setV n fun _ => false)) = !noneOf n st.dropRec by rw [decide_setV_none]] at h
  have hn := leaf g2 h
  simp only [Bool.not_eq_true'] at hn
  obtain ⟨v1, us1, rfl, h1, hm1⟩ := assigns_cons (evalAssigns_spec hu)
  obtain ⟨v2, us2, rfl, h2, hm2⟩ := assigns_cons hm1
  rw [assigns_nil hm2]
  have e1 := eval_val_eq h1 ev_noRoles0
  have hdf : eval (E roles p st [] []) (f "dropFloor") =
      some (.map ((List.range n).map fun r => .nat (st.dropFloor r))) := by ev
  have hfill : eval (E roles p st [] []) (.afill (f "dropFloor") (nat 0) (nat 0)) =
      some (.map ((List.range n).map fun r => .nat (fillFn st.dropFloor 0 0 r))) := by
    simp only [eval, hdf, nat, Option.bind_some, fillFrom, natV, Option.some.injEq, Val.map.injEq]
    apply List.ext_getElem <;> simp [fillFn]
  have e2 := eval_val_eq h2 hfill
  subst e1 e2
  exact ⟨hp, hn, by simp [applyUpdates, enc, lookup, releaseDropS]⟩

theorem acc_releaseRecovery (hw : WF B st) (hargs : args = [])
    (hg : eval (E roles p st args []) releaseRecoveryEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) releaseRecoveryEvent.assigns = some us) :
    st.planned = true ∧ st.recKind = 1 ∧ st.recReleased = false ∧ noneOf n (recRoleIn st) = true ∧
      applyUpdates (enc n B st) us = enc n B { st with recReleased := true } := by
  subst hargs
  obtain ⟨g1, g2⟩ := eval_and_true hg
  obtain ⟨g2, g3⟩ := eval_and_true g2
  obtain ⟨g3, g4⟩ := eval_and_true g3
  have hp := leaf g1 ev_planned
  have hk := leaf g2 ev_rec_noPlan
  have hr := leaf g3 (eval_not (by ev : _ = some (boolV st.recReleased)))
  have h := eval_eq (ev_recoveryRoles (roles := roles) (args := []) (bnd := []) hw.count) ev_noRoles0
  rw [decide_setV_none] at h
  have hn := leaf g4 h
  simp only [decide_eq_true_eq, Bool.not_eq_true'] at hk hr
  obtain ⟨v1, us1, rfl, h1, hm1⟩ := assigns_cons (evalAssigns_spec hu)
  rw [assigns_nil hm1]
  have e1 := eval_val_eq h1 ev_tt
  subst e1
  exact ⟨hp, hk, hr, hn, by simp [applyUpdates, enc, lookup]⟩

theorem acc_done {c : Bool} (hw : WF B st) (hargs : args = [("planClear", boolV c)])
    (hg : eval (E roles p st args []) doneEvent.guard = some (boolV true))
    (hu : evalAssigns (E roles p st args []) doneEvent.assigns = some us) :
    doneReadyS n st = true ∧ c = true ∧ applyUpdates (enc n B st) us = enc n B { st with done := true } := by
  subst hargs
  obtain ⟨g1, g2⟩ := eval_and_true hg
  have hr := leaf g1 (ev_doneReady hw.count)
  have hc := leaf g2 (by ev : _ = some (boolV c))
  obtain ⟨v1, us1, rfl, h1, hm1⟩ := assigns_cons (evalAssigns_spec hu)
  rw [assigns_nil hm1]
  have e1 := eval_val_eq h1 ev_tt
  subst e1
  exact ⟨hr, hc, by simp [applyUpdates, enc, lookup]⟩

end

/-! ## Events as Lean values -/

/-- The events of the machine with decoded arguments. -/
inductive Ev where
  | create
  | addBatch (m : Bool) (rs : Nat → Bool)
  | close (k : Nat) (cs : Nat → Bool)
  | newCycle
  | reset
  | rebind
  | drop (k : Nat) (d : Bool)
  | releaseDrop
  | releaseRecovery
  | done (c : Bool)

/-- Reads an event of the machine from its name and arguments alone. -/
def decodeEv (e : Event) : Option Ev :=
  match e.name, e.args with
  | "create", [] => some .create
  | "addBatch", [.scalar (.bool m), .set bs] => some (.addBatch m fun r => bs.getD r false)
  | "close", [.scalar (.nat k), .set bs] => some (.close k fun r => bs.getD r false)
  | "newCycle", [] => some .newCycle
  | "reset", [] => some .reset
  | "rebind", [] => some .rebind
  | "drop", [.scalar (.nat k), .scalar (.bool d)] => some (.drop k d)
  | "releaseDrop", [] => some .releaseDrop
  | "releaseRecovery", [] => some .releaseRecovery
  | "done", [.scalar (.bool c)] => some (.done c)
  | _, _ => none

/-- The guard of an event on a state, as the acceptance lemmas state it. -/
def Guard (nr b : Nat) (st : St) : Ev → Prop
  | .create => st.planned = false ∧ st.done = false
  | .addBatch m _ => st.planned = true ∧ st.count < b ∧ (st.done && m) = false
  | .close k _ => st.planned = true ∧ st.bound = true ∧ k < st.count ∧ st.closed k = false
  | .newCycle => st.done = false
  | .reset => st.done = false
  | .rebind => st.planned = true ∧ st.bound = false ∧ st.done = false
  | .drop k _ => st.planned = true ∧ 1 ≤ k ∧ k < st.count ∧ anyFrom st k st.closed = false
  | .releaseDrop => st.planned = true ∧ noneOf nr st.dropRec = false
  | .releaseRecovery => st.planned = true ∧ st.recKind = 1 ∧ st.recReleased = false ∧ noneOf nr (recRoleIn st) = true
  | .done c => doneReadyS nr st = true ∧ c = true

/-- The state after an accepted event. -/
def post (nr : Nat) (st : St) : Ev → St
  | .create => { st with planned := true, bound := true }
  | .addBatch m rs => addBatchS st m rs
  | .close k cs => closeS st k cs
  | .newCycle => newCycleS st
  | .reset => resetS nr st
  | .rebind => rebindS st
  | .drop k d => dropS st k d
  | .releaseDrop => releaseDropS st
  | .releaseRecovery => { st with recReleased := true }
  | .done _ => { st with done := true }

theorem events_cases {roles : List String} {ev : EventDecl} (h : ev ∈ (Mr roles).events) :
    ev = createEvent ∨ ev = addBatchEvent ∨ ev = closeEvent ∨ ev = newCycleEvent ∨ ev = resetEvent ∨
      ev = rebindEvent ∨ ev = dropEvent ∨ ev = releaseDropEvent ∨ ev = releaseRecoveryEvent ∨ ev = doneEvent := by
  simpa [Mr, obligationMachine] using h

theorem size_role' {roles : List String} : (Mr roles).enumSize "role" = some roles.length := rfl

theorem bool_arg {roles : List String} {p : Params} {v : Val} (h : Ty.conforms (Mr roles) p (.scalar .bool) v = true) :
    ∃ b, v = boolV b := conforms_bool h

theorem nat_arg {roles : List String} {p : Params} {v : Val}
    (h : Ty.conforms (Mr roles) p (.scalar (.nat "maxBatch")) v = true) : ∃ k, v = natV k ∧ k ≤ p "maxBatch" :=
  conforms_nat h

theorem set_arg {roles : List String} {p : Params} {v : Val} (h : Ty.conforms (Mr roles) p (.set "role") v = true) :
    ∃ bs : List Bool, v = .set bs ∧ bs.length = roles.length := by
  obtain ⟨bs, rfl, hl⟩ := conforms_set size_role' h
  exact ⟨bs, rfl, hl⟩

theorem findEvent_name {m : Machine} {n : String} {ev : EventDecl} (h : m.findEvent n = some ev) : ev.name = n := by
  unfold Machine.findEvent at h
  generalize m.events = l at h
  induction l with
  | nil => simp [lookup] at h
  | cons d l ih =>
    simp only [List.map, lookup] at h
    split at h
    · cases h; rename_i hn; exact hn.symm
    · exact ih h

/-- Every event the machine accepts on an encoded well-formed state decodes to
an `Ev`, its guard holds, and the result is the encoding of `post`. -/
theorem step_enc {roles : List String} {p : Params} {st : St} {e : Event} {s' : State}
    (hw : WF (p "maxBatch") st) (h : step (Mr roles) p (enc roles.length (p "maxBatch") st) e = some s') :
    ∃ ev, decodeEv e = some ev ∧ Guard roles.length (p "maxBatch") st ev ∧
      s' = enc roles.length (p "maxBatch") (post roles.length st ev) := by
  obtain ⟨ev, args, us, hev, hargs, hg, hu, rfl⟩ := step_accepted h
  have hname := findEvent_name hev
  obtain ⟨en, ea⟩ := e
  simp only at hname hargs
  subst hname
  have hmem := lookup_map_mem hev
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨rfl, rfl⟩ := bindArgs_nil hargs
    obtain ⟨h1, h2, h3⟩ := acc_create hg hu
    exact ⟨.create, rfl, ⟨h1, h2⟩, h3⟩
  · obtain ⟨v1, vs1, a1, rfl, c1, h1, rfl⟩ := bindArgs_cons hargs
    obtain ⟨v2, vs2, a2, rfl, c2, h2, rfl⟩ := bindArgs_cons h1
    obtain ⟨rfl, rfl⟩ := bindArgs_nil h2
    obtain ⟨m, rfl⟩ := bool_arg c1
    obtain ⟨bs, rfl, hl⟩ := set_arg c2
    obtain ⟨h1, h2, h3, h4⟩ := acc_addBatch (by rw [set_of_list hl]) hg hu
    exact ⟨_, rfl, ⟨h1, h2, h3⟩, h4⟩
  · obtain ⟨v1, vs1, a1, rfl, c1, h1, rfl⟩ := bindArgs_cons hargs
    obtain ⟨v2, vs2, a2, rfl, c2, h2, rfl⟩ := bindArgs_cons h1
    obtain ⟨rfl, rfl⟩ := bindArgs_nil h2
    obtain ⟨k, rfl, -⟩ := nat_arg c1
    obtain ⟨bs, rfl, hl⟩ := set_arg c2
    obtain ⟨h1, h2, h3, h4, h5⟩ := acc_close hw (by rw [set_of_list hl]) hg hu
    exact ⟨_, rfl, ⟨h1, h2, h3, h4⟩, h5⟩
  · obtain ⟨rfl, rfl⟩ := bindArgs_nil hargs
    obtain ⟨h1, h2⟩ := acc_newCycle rfl hg hu
    exact ⟨.newCycle, rfl, h1, h2⟩
  · obtain ⟨rfl, rfl⟩ := bindArgs_nil hargs
    obtain ⟨h1, h2⟩ := acc_reset hw rfl hg hu
    exact ⟨.reset, rfl, h1, h2⟩
  · obtain ⟨rfl, rfl⟩ := bindArgs_nil hargs
    obtain ⟨h1, h2, h3, h4⟩ := acc_rebind hw rfl hg hu
    exact ⟨.rebind, rfl, ⟨h1, h2, h3⟩, h4⟩
  · obtain ⟨v1, vs1, a1, rfl, c1, h1, rfl⟩ := bindArgs_cons hargs
    obtain ⟨v2, vs2, a2, rfl, c2, h2, rfl⟩ := bindArgs_cons h1
    obtain ⟨rfl, rfl⟩ := bindArgs_nil h2
    obtain ⟨k, rfl, -⟩ := nat_arg c1
    obtain ⟨d, rfl⟩ := bool_arg c2
    obtain ⟨h1, h2, h3, h4, h5⟩ := acc_drop hw rfl hg hu
    exact ⟨.drop k d, rfl, ⟨h1, h2, h3, h4⟩, h5⟩
  · obtain ⟨rfl, rfl⟩ := bindArgs_nil hargs
    obtain ⟨h1, h2, h3⟩ := acc_releaseDrop rfl hg hu
    exact ⟨.releaseDrop, rfl, ⟨h1, h2⟩, h3⟩
  · obtain ⟨rfl, rfl⟩ := bindArgs_nil hargs
    obtain ⟨h1, h2, h3, h4, h5⟩ := acc_releaseRecovery hw rfl hg hu
    exact ⟨.releaseRecovery, rfl, ⟨h1, h2, h3, h4⟩, h5⟩
  · obtain ⟨v1, vs1, a1, rfl, c1, h1, rfl⟩ := bindArgs_cons hargs
    obtain ⟨rfl, rfl⟩ := bindArgs_nil h1
    obtain ⟨c, rfl⟩ := bool_arg c1
    obtain ⟨h1, h2, h3⟩ := acc_done hw rfl hg hu
    exact ⟨.done c, rfl, ⟨h1, h2⟩, h3⟩

end KanderFormal.Proofs.ReviewObligation
