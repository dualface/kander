import KanderFormal.Semantics

/-
Evaluation lemmas for the binder and array forms. Machine proofs use them to
turn the evaluation of an expression on an encoded state into an ordinary Lean
value, one helper expression at a time.
-/
namespace KanderFormal

theorem boolsAt_map {f : Nat → Option Val} {g : Nat → Bool} :
    ∀ {l : List Nat}, (∀ i ∈ l, f i = some (boolV (g i))) → boolsAt f l = some (l.map g)
  | [], _ => rfl
  | i :: is, h => by
    simp only [boolsAt, h i List.mem_cons_self, boolV, Option.bind_some, List.map_cons]
    rw [boolsAt_map fun j hj => h j (List.mem_cons_of_mem _ hj)]
    rfl

theorem scalarsAt_map {f : Nat → Option Val} {g : Nat → Scalar} :
    ∀ {l : List Nat}, (∀ i ∈ l, f i = some (.scalar (g i))) → scalarsAt f l = some (l.map g)
  | [], _ => rfl
  | i :: is, h => by
    simp only [scalarsAt, h i List.mem_cons_self, Option.bind_some, List.map_cons]
    rw [scalarsAt_map fun j hj => h j (List.mem_cons_of_mem _ hj)]
    rfl

theorem mem_indexRange {i lo hi : Nat} : i ∈ indexRange lo hi ↔ lo ≤ i ∧ i < hi := by
  simp only [indexRange, List.mem_range'_1]
  omega

theorem any_indexRange {lo hi : Nat} {g : Nat → Bool} :
    (indexRange lo hi).any g = true ↔ ∃ i, lo ≤ i ∧ i < hi ∧ g i = true := by
  simp only [List.any_eq_true, mem_indexRange]
  constructor
  · rintro ⟨i, ⟨h1, h2⟩, h3⟩; exact ⟨i, h1, h2, h3⟩
  · rintro ⟨i, h1, h2, h3⟩; exact ⟨i, ⟨h1, h2⟩, h3⟩

theorem any_indexRange_false {lo hi : Nat} {g : Nat → Bool} :
    (indexRange lo hi).any g = false ↔ ∀ i, lo ≤ i → i < hi → g i = false := by
  rw [← Bool.not_eq_true, any_indexRange]
  constructor
  · intro h i h1 h2; cases hg : g i
    · rfl
    · exact absurd ⟨i, h1, h2, hg⟩ h
  · rintro h ⟨i, h1, h2, h3⟩; rw [h i h1 h2] at h3; cases h3

/-- `firstSet` over a list of indices with flags from `g`. -/
theorem firstSet_map {dflt : Nat} {g : Nat → Bool} :
    ∀ (l : List Nat), firstSet dflt l (l.map g) = (l.find? g).getD dflt
  | [] => rfl
  | i :: is => by
    simp only [List.map_cons, firstSet, List.find?_cons]
    cases g i <;> simp [firstSet_map is]

theorem find?_range'_some {g : Nat → Bool} :
    ∀ {n lo r : Nat}, (List.range' lo n).find? g = some r →
      lo ≤ r ∧ r < lo + n ∧ g r = true ∧ ∀ j, lo ≤ j → j < r → g j = false
  | 0, _, _, h => by simp at h
  | n + 1, lo, r, h => by
    rw [List.range'_succ, List.find?_cons] at h
    cases hg : g lo
    · rw [hg] at h
      obtain ⟨h1, h2, h3, h4⟩ := find?_range'_some (n := n) h
      refine ⟨by omega, by omega, h3, fun j hj1 hj2 => ?_⟩
      rcases Nat.eq_or_lt_of_le hj1 with rfl | hlt
      · exact hg
      · exact h4 j hlt hj2
    · rw [hg] at h
      simp only [Option.some.injEq] at h
      subst h
      exact ⟨Nat.le_refl _, by omega, hg, fun j h1 h2 => by omega⟩

theorem find?_indexRange_some {lo hi r : Nat} {g : Nat → Bool} (h : (indexRange lo hi).find? g = some r) :
    lo ≤ r ∧ r < hi ∧ g r = true ∧ ∀ j, lo ≤ j → j < r → g j = false := by
  obtain ⟨h1, h2, h3, h4⟩ := find?_range'_some h
  exact ⟨h1, by omega, h3, h4⟩

theorem find?_indexRange_none {lo hi : Nat} {g : Nat → Bool} (h : (indexRange lo hi).find? g = none) :
    ∀ j, lo ≤ j → j < hi → g j = false := by
  intro j h1 h2
  have := List.find?_eq_none.mp h j (mem_indexRange.mpr ⟨h1, h2⟩)
  simpa using this

/-! ## Binder forms -/

theorem eval_any {env : Env} {lo hi body : Expr} {l h : Nat} {g : Nat → Bool}
    (hl : eval env lo = some (natV l)) (hh : eval env hi = some (natV h))
    (hb : ∀ i, l ≤ i → i < h → eval { env with bound := natV i :: env.bound } body = some (boolV (g i))) :
    eval env (.any lo hi body) = some (boolV ((indexRange l h).any g)) := by
  have hb' : ∀ i ∈ indexRange l h,
      eval { env with bound := Val.scalar (Scalar.nat i) :: env.bound } body = some (boolV (g i)) :=
    fun i hi => hb i (mem_indexRange.mp hi).1 (mem_indexRange.mp hi).2
  simp only [eval, hl, hh, natV, Option.bind_some]
  rw [boolsAt_map hb']
  simp [List.any_map]

theorem eval_first {env : Env} {lo hi body : Expr} {l h : Nat} {g : Nat → Bool}
    (hl : eval env lo = some (natV l)) (hh : eval env hi = some (natV h))
    (hb : ∀ i, l ≤ i → i < h → eval { env with bound := natV i :: env.bound } body = some (boolV (g i))) :
    eval env (.first lo hi body) = some (natV (((indexRange l h).find? g).getD h)) := by
  have hb' : ∀ i ∈ indexRange l h,
      eval { env with bound := Val.scalar (Scalar.nat i) :: env.bound } body = some (boolV (g i)) :=
    fun i hi => hb i (mem_indexRange.mp hi).1 (mem_indexRange.mp hi).2
  simp only [eval, hl, hh, natV, Option.bind_some]
  rw [boolsAt_map hb']
  simp [firstSet_map]

theorem eval_setOf {env : Env} {e : String} {body : Expr} {n : Nat} {g : Nat → Bool}
    (hn : env.m.enumSize e = some n)
    (hb : ∀ i, i < n → eval { env with bound := enumV i :: env.bound } body = some (boolV (g i))) :
    eval env (.setOf e body) = some (.set ((List.range n).map g)) := by
  have hb' : ∀ i ∈ List.range n,
      eval { env with bound := enumV i :: env.bound } body = some (boolV (g i)) :=
    fun i hi => hb i (List.mem_range.mp hi)
  simp only [eval, hn, Option.bind_some]
  rw [boolsAt_map hb']
  rfl

theorem eval_mapOf {env : Env} {k : String} {body : Expr} {n : Nat} {g : Nat → Scalar}
    (hn : env.m.enumSize k = some n)
    (hb : ∀ i, i < n → eval { env with bound := enumV i :: env.bound } body = some (.scalar (g i))) :
    eval env (.mapOf k body) = some (.map ((List.range n).map g)) := by
  have hb' : ∀ i ∈ List.range n,
      eval { env with bound := enumV i :: env.bound } body = some (.scalar (g i)) :=
    fun i hi => hb i (List.mem_range.mp hi)
  simp only [eval, hn, Option.bind_some]
  rw [scalarsAt_map hb']
  rfl

/-! ## Boolean forms -/

theorem eval_and {env : Env} {x y : Expr} {a b : Bool} (hx : eval env x = some (boolV a))
    (hy : eval env y = some (boolV b)) : eval env (.and x y) = some (boolV (a && b)) := by
  simp [eval, hx, hy, boolAnd, boolV]

theorem eval_or {env : Env} {x y : Expr} {a b : Bool} (hx : eval env x = some (boolV a))
    (hy : eval env y = some (boolV b)) : eval env (.or x y) = some (boolV (a || b)) := by
  simp [eval, hx, hy, boolOr, boolV]

theorem eval_not {env : Env} {x : Expr} {a : Bool} (hx : eval env x = some (boolV a)) :
    eval env (.not x) = some (boolV (!a)) := by
  simp [eval, hx, boolNot, boolV]

theorem eval_ite {env : Env} {c x y : Expr} {a : Bool} {v w : Val} (hc : eval env c = some (boolV a))
    (hx : eval env x = some v) (hy : eval env y = some w) :
    eval env (.ite c x y) = some (if a then v else w) := by
  cases a <;> simp [eval, hc, hx, hy, boolV]

theorem eval_eq {env : Env} {x y : Expr} {v w : Val} (hx : eval env x = some v) (hy : eval env y = some w) :
    eval env (.cmp .eq x y) = some (boolV (decide (v = w))) := by
  simp [eval, hx, hy, evalCmp]

theorem eval_ne {env : Env} {x y : Expr} {v w : Val} (hx : eval env x = some v) (hy : eval env y = some w) :
    eval env (.cmp .ne x y) = some (boolV (!decide (v = w))) := by
  simp [eval, hx, hy, evalCmp]

theorem eval_le {env : Env} {x y : Expr} {a b : Nat} (hx : eval env x = some (natV a))
    (hy : eval env y = some (natV b)) : eval env (.cmp .le x y) = some (boolV (decide (a ≤ b))) := by
  simp [eval, hx, hy, evalCmp, natV]

theorem eval_lt {env : Env} {x y : Expr} {a b : Nat} (hx : eval env x = some (natV a))
    (hy : eval env y = some (natV b)) : eval env (.cmp .lt x y) = some (boolV (decide (a < b))) := by
  simp [eval, hx, hy, evalCmp, natV]

theorem eval_and_true {env : Env} {x y : Expr} (h : eval env (.and x y) = some (boolV true)) :
    eval env x = some (boolV true) ∧ eval env y = some (boolV true) := by
  simp only [eval, Option.bind_eq_some_iff] at h
  obtain ⟨a, ha, b, hb, h⟩ := h
  match a, b, h with
  | .scalar (.bool true), .scalar (.bool true), _ => exact ⟨ha, hb⟩
  | .scalar (.bool false), .scalar (.bool _), h => simp [boolAnd, boolV] at h
  | .scalar (.bool true), .scalar (.bool false), h => simp [boolAnd, boolV] at h

theorem eval_and_parts {env : Env} {x y : Expr} {c : Bool} (h : eval env (.and x y) = some (boolV c)) :
    ∃ a b, eval env x = some (boolV a) ∧ eval env y = some (boolV b) ∧ c = (a && b) := by
  simp only [eval, Option.bind_eq_some_iff] at h
  obtain ⟨u, hu, v, hv, h⟩ := h
  match u, v, h with
  | .scalar (.bool a), .scalar (.bool b), h => exact ⟨a, b, hu, hv, by simp_all [boolAnd, boolV]⟩

theorem eval_or_parts {env : Env} {x y : Expr} {c : Bool} (h : eval env (.or x y) = some (boolV c)) :
    ∃ a b, eval env x = some (boolV a) ∧ eval env y = some (boolV b) ∧ c = (a || b) := by
  simp only [eval, Option.bind_eq_some_iff] at h
  obtain ⟨u, hu, v, hv, h⟩ := h
  match u, v, h with
  | .scalar (.bool a), .scalar (.bool b), h => exact ⟨a, b, hu, hv, by simp_all [boolOr, boolV]⟩

theorem eval_not_parts {env : Env} {x : Expr} {c : Bool} (h : eval env (.not x) = some (boolV c)) :
    eval env x = some (boolV (!c)) := by
  simp only [eval, Option.bind_eq_some_iff] at h
  obtain ⟨u, hu, h⟩ := h
  match u, h with
  | .scalar (.bool a), h => simp [boolNot, boolV] at h; rw [hu]; subst h; simp [boolV]

theorem eval_val_eq {env : Env} {x : Expr} {v w : Val} (h1 : eval env x = some v) (h2 : eval env x = some w) :
    v = w := by
  rw [h1] at h2; exact Option.some.inj h2

theorem bool_eq_of {env : Env} {x : Expr} {a b : Bool} (h1 : eval env x = some (boolV a))
    (h2 : eval env x = some (boolV b)) : a = b := by
  have := eval_val_eq h1 h2; simpa [boolV] using this

/-! ## Arrays built from a function -/

theorem range_map_set {α : Type} {n i : Nat} {g : Nat → α} {x : α} :
    ((List.range n).map g).set i x = (List.range n).map (fun j => if j = i then x else g j) := by
  apply List.ext_getElem <;> simp
  intro j hj
  rw [List.getElem_set]
  split <;> simp_all [eq_comm]

theorem fill_range_map {α : Type} {n lo : Nat} {g : Nat → α} {x : α} :
    ((List.range n).zip ((List.range n).map g)).map (fun (i, v) => if lo ≤ i then x else v) =
      (List.range n).map (fun j => if lo ≤ j then x else g j) := by
  apply List.ext_getElem <;> simp

end KanderFormal
