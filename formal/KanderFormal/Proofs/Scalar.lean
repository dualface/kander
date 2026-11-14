import KanderFormal.Obligations

/-
Helpers for machines whose fields and arguments are all scalars: the event an
accepted step ran, and the shape of its arguments.
-/
namespace KanderFormal.Proofs.Scalar

open KanderFormal

/-- A lookup in a list keyed by `key` returns an element with that key. -/
theorem lookup_map_key {α : Type} {key : α → String} {k : String} {d : α} :
    ∀ {l : List α}, lookup k (l.map fun x => (key x, x)) = some d → key d = k
  | [], h => by simp [lookup] at h
  | x :: xs, h => by
    simp only [List.map, lookup] at h
    split at h
    · cases h; rename_i hk; exact hk.symm
    · exact lookup_map_key h

/-- An accepted step ran a declared event of the same name. -/
theorem accepted_event {m : Machine} {p : Params} {s s' : State} {e : Event}
    (h : step m p s e = some s') :
    ∃ ev args, ev ∈ m.events ∧ ev.name = e.name ∧ bindArgs m p ev.args e.args = some args := by
  obtain ⟨ev, args, -, hev, hargs, -, -, -⟩ := step_accepted h
  exact ⟨ev, args, lookup_map_mem hev, lookup_map_key hev, hargs⟩

/-- The arguments of an event with one Boolean argument. -/
theorem args_b {m : Machine} {p : Params} {n : String} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs m p [{ name := n, ty := .scalar .bool }] vs = some args) : ∃ b, vs = [boolV b] := by
  obtain ⟨v, vs', _, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨rfl, -⟩ := bindArgs_nil h'
  obtain ⟨b, rfl⟩ := conforms_bool c
  exact ⟨b, rfl⟩

/-- The arguments of an event with a list of Boolean arguments. -/
theorem args_bools {m : Machine} {p : Params} :
    ∀ {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)},
      (∀ d ∈ ds, d.ty = .scalar .bool) → bindArgs m p ds vs = some args →
      ∃ bs : List Bool, vs = bs.map boolV ∧ bs.length = ds.length
  | [], vs, args, _, h => by
    obtain ⟨rfl, -⟩ := bindArgs_nil h
    exact ⟨[], rfl, rfl⟩
  | d :: ds, vs, args, hb, h => by
    obtain ⟨v, vs', _, rfl, c, h', -⟩ := bindArgs_cons h
    rw [hb d List.mem_cons_self] at c
    obtain ⟨b, rfl⟩ := conforms_bool c
    obtain ⟨bs, rfl, hl⟩ := args_bools (fun d hd => hb d (List.mem_cons_of_mem _ hd)) h'
    exact ⟨b :: bs, rfl, by simp [hl]⟩

/-! ## Normalizing nested refusals -/

theorem map_ite {α β : Type} (g : α → β) (c : Prop) [Decidable c] (x y : Option α) :
    Option.map g (if c then x else y) = if c then Option.map g x else Option.map g y := by
  split <;> rfl

theorem ite_ite_none {α : Type} (c d : Prop) [Decidable c] [Decidable d] (x : Option α) :
    (if c then (if d then x else none) else none) = if c ∧ d then x else none := by
  by_cases hc : c <;> by_cases hd : d <;> simp [hc, hd]

theorem ite_and_self {α : Type} (c : Prop) [Decidable c] (x : Option α) :
    (if c then (if c then x else none) else none) = if c then x else none := by
  by_cases hc : c <;> simp [hc]

end KanderFormal.Proofs.Scalar
