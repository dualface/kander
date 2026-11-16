import KanderFormal.Proofs.ReviewDispositionStep

/-
The events an accepted step of the review disposition machine ran: one of the
four declared events, with arguments of their declared types.
-/
namespace KanderFormal.Proofs.ReviewDisposition

open KanderFormal KanderFormal.Machines.ReviewDisposition KanderFormal.Proofs.Scalar

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = assignEvent ∨ ev = closeEvent ∨ ev = submitEvent ∨ ev = withdrawEvent := by
  simpa [reviewDispositionMachine] using h

theorem enum_arg {p : Params} {n : String} {k : Nat} {v : Val} (hk : M.enumSize n = some k)
    (h : Ty.conforms M p (.scalar (.enum n)) v = true) : ∃ i, v = enumV i := by
  obtain ⟨i, rfl, -⟩ := conforms_enum hk h; exact ⟨i, rfl⟩

/-- A list of Boolean arguments of the declared length. -/
theorem bool_list {p : Params} {names : List String} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArgs names) vs = some args) : ∃ bs : List Bool, vs = bs.map boolV ∧ bs.length = names.length := by
  obtain ⟨bs, rfl, hl⟩ := args_bools (m := M) (p := p) (ds := boolArgs names)
    (by intro d hd; simp [boolArgs] at hd; obtain ⟨n, -, rfl⟩ := hd; rfl) h
  exact ⟨bs, rfl, by simpa [boolArgs] using hl⟩

/-- Every accepted event is one of the four, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    e = ⟨"assign", []⟩ ∨ e = ⟨"close", []⟩ ∨
      (∃ st du rf g se e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12,
        e = ⟨"submit", [enumV st, enumV du, enumV rf, boolV g, boolV se, boolV e1, boolV e2, boolV e3, boolV e4,
          boolV e5, boolV e6, boolV e7, boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩) ∨
      (∃ du rf e1 e2 e3 e4 e5 e6 e7 e8 e9 e10 e11 e12,
        e = ⟨"withdraw", [enumV du, enumV rf, boolV e1, boolV e2, boolV e3, boolV e4, boolV e5, boolV e6, boolV e7,
          boolV e8, boolV e9, boolV e10, boolV e11, boolV e12]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inl rfl
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inl rfl)
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["gate", "security"] ++ submitFacts) h3
    obtain ⟨st, rfl⟩ := enum_arg size_status c1
    obtain ⟨du, rfl⟩ := enum_arg size_dup c2
    obtain ⟨rf, rfl⟩ := enum_arg size_ref c3
    match bs, hl with
    | [g, se, e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12], _ =>
      exact .inr (.inr (.inl ⟨st, du, rf, g, se, e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12, rfl⟩))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨bs, rfl, hl⟩ := bool_list (names := withdrawFacts) h2
    obtain ⟨du, rfl⟩ := enum_arg size_dup c1
    obtain ⟨rf, rfl⟩ := enum_arg size_ref c2
    match bs, hl with
    | [e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12], _ =>
      exact .inr (.inr (.inr ⟨du, rf, e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12, rfl⟩))

end KanderFormal.Proofs.ReviewDisposition
