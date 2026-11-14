import KanderFormal.Typing

/-
The proof obligation template. Every registered machine provides a
`SafetyCert`: an invariant that holds initially for every parameter value and
is preserved by every accepted event. `SafetyCert.reachable` then shows the
invariant holds in every reachable state, for every parameter value. Machine
specific safety properties are stated as corollaries of the invariant.
-/
namespace KanderFormal

structure SafetyCert (m : Machine) where
  Inv : Params → State → Prop
  init : ∀ p, Inv p (initial m)
  step : ∀ p s s' e, Inv p s → KanderFormal.step m p s e = some s' → Inv p s'

theorem SafetyCert.reachable {m : Machine} (c : SafetyCert m) {p : Params} {s : State}
    (h : Reachable m p s) : c.Inv p s := by
  induction h with
  | init => exact c.init p
  | step e _ hs ih => exact c.step p _ _ e ih hs

/-- Every reachable state of a well-typed machine conforms to its declared
types and bounds. -/
theorem reachable_conforms {m : Machine} (hwt : m.wellTyped = true) {p : Params} {s : State}
    (h : Reachable m p s) : m.StateConforms p s := by
  induction h with
  | init => exact initial_conforms hwt p
  | step _ _ hs ih => exact step_preserves_conforms ih hs

/-! ## Shape lemmas

A conforming value has the shape its declared type describes. Machine proofs
use these to turn a conforming state into explicit components. -/

theorem conforms_bool {m : Machine} {p : Params} {v : Val}
    (h : Ty.conforms m p (.scalar .bool) v = true) : ∃ b, v = boolV b := by
  match v with
  | .scalar (.bool b) => exact ⟨b, rfl⟩
  | .scalar (.enum _) | .scalar (.nat _) | .map _ | .set _ => simp [Ty.conforms, STy.conforms] at h

theorem conforms_nat {m : Machine} {p : Params} {b : String} {v : Val}
    (h : Ty.conforms m p (.scalar (.nat b)) v = true) : ∃ n, v = natV n ∧ n ≤ p b := by
  match v with
  | .scalar (.nat n) => simp [Ty.conforms, STy.conforms] at h; exact ⟨n, rfl, h⟩
  | .scalar (.enum _) | .scalar (.bool _) | .map _ | .set _ => simp [Ty.conforms, STy.conforms] at h

theorem conforms_enum {m : Machine} {p : Params} {e : String} {v : Val} {n : Nat}
    (hn : m.enumSize e = some n) (h : Ty.conforms m p (.scalar (.enum e)) v = true) :
    ∃ i, v = enumV i ∧ i < n := by
  match v with
  | .scalar (.enum i) => simp [Ty.conforms, STy.conforms, hn] at h; exact ⟨i, rfl, h⟩
  | .scalar (.nat _) | .scalar (.bool _) | .map _ | .set _ => simp [Ty.conforms, STy.conforms] at h

theorem enum_scalars {m : Machine} {p : Params} {e : String} {ne : Nat}
    (he : m.enumSize e = some ne) :
    ∀ {vs : List Scalar}, (∀ x ∈ vs, STy.conforms m p (.enum e) x = true) →
      ∃ is : List Nat, vs = is.map Scalar.enum ∧ ∀ i ∈ is, i < ne
  | [], _ => ⟨[], rfl, by simp⟩
  | x :: xs, hall => by
    have hx := hall x List.mem_cons_self
    obtain ⟨is, rfl, hlt⟩ := enum_scalars he fun y hy => hall y (List.mem_cons_of_mem _ hy)
    match x with
    | .enum i =>
      simp [STy.conforms, he] at hx
      refine ⟨i :: is, rfl, ?_⟩
      intro j hj
      rcases List.mem_cons.mp hj with rfl | hj
      · exact hx
      · exact hlt j hj
    | .bool _ | .nat _ => simp [STy.conforms] at hx

theorem conforms_map_enum {m : Machine} {p : Params} {k e : String} {v : Val} {nk ne : Nat}
    (hk : m.enumSize k = some nk) (he : m.enumSize e = some ne)
    (h : Ty.conforms m p (.map k (.enum e)) v = true) :
    ∃ is : List Nat, v = .map (is.map Scalar.enum) ∧ is.length = nk ∧ ∀ i ∈ is, i < ne := by
  match v with
  | .map vs =>
    simp only [Ty.conforms, hk, Option.some.injEq, Bool.and_eq_true, decide_eq_true_eq,
      List.all_eq_true] at h
    obtain ⟨hlen, hall⟩ := h
    obtain ⟨is, rfl, hlt⟩ := enum_scalars he hall
    exact ⟨is, rfl, by simpa using hlen.symm, hlt⟩
  | .scalar _ | .set _ => simp [Ty.conforms] at h

theorem conforms_set {m : Machine} {p : Params} {e : String} {v : Val} {n : Nat}
    (hn : m.enumSize e = some n) (h : Ty.conforms m p (.set e) v = true) :
    ∃ bs, v = .set bs ∧ bs.length = n := by
  match v with
  | .set bs => simp [Ty.conforms, hn] at h; exact ⟨bs, rfl, h.symm⟩
  | .scalar _ | .map _ => simp [Ty.conforms] at h

theorem bindArgs_nil {m : Machine} {p : Params} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs m p [] vs = some args) : vs = [] ∧ args = [] := by
  match vs with
  | [] => simp [bindArgs] at h; exact ⟨rfl, h⟩
  | _ :: _ => simp [bindArgs] at h

theorem bindArgs_cons {m : Machine} {p : Params} {d : ArgDecl} {ds : List ArgDecl} {vs : List Val}
    {args : List (String × Val)} (h : bindArgs m p (d :: ds) vs = some args) :
    ∃ v vs' args', vs = v :: vs' ∧ (Ty.scalar d.ty).conforms m p v = true ∧
      bindArgs m p ds vs' = some args' ∧ args = (d.name, v) :: args' := by
  match vs with
  | [] => simp [bindArgs] at h
  | v :: vs' =>
    simp only [bindArgs] at h
    split at h
    · rename_i hc
      simp only [Option.map_eq_some_iff] at h
      obtain ⟨args', ha, rfl⟩ := h
      exact ⟨v, vs', args', rfl, hc, ha, rfl⟩
    · cases h

end KanderFormal
