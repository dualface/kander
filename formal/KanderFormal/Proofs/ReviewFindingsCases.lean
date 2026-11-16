import KanderFormal.Proofs.ReviewFindingsStep

/-
The events an accepted step of the review findings machine ran: one of the five
declared events, with arguments of their declared types.
-/
namespace KanderFormal.Proofs.ReviewFindings

open KanderFormal KanderFormal.Machines.ReviewFindings KanderFormal.Proofs.Scalar

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = openEvent ∨ ev = closeEvent ∨ ev = interpretEvent ∨ ev = mapLegacyEvent ∨ ev = assignEvent := by
  simpa [reviewFindingsMachine] using h

theorem enum_arg {p : Params} {n : String} {k : Nat} {v : Val} (hk : M.enumSize n = some k)
    (h : Ty.conforms M p (.scalar (.enum n)) v = true) : ∃ i, v = enumV i := by
  obtain ⟨i, rfl, -⟩ := conforms_enum hk h; exact ⟨i, rfl⟩

/-- A list of Boolean arguments of the declared length. -/
theorem bool_list {p : Params} {names : List String} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArgs names) vs = some args) : ∃ bs : List Bool, vs = bs.map boolV ∧ bs.length = names.length := by
  obtain ⟨bs, rfl, hl⟩ := args_bools (m := M) (p := p) (ds := boolArgs names)
    (by intro d hd; simp [boolArgs] at hd; obtain ⟨n, -, rfl⟩ := hd; rfl) h
  exact ⟨bs, rfl, by simpa [boolArgs] using hl⟩

/-- Every accepted event is one of the five, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ sc st fe co, e = ⟨"open", [enumV sc, boolV st, boolV fe, boolV co]⟩) ∨
      e = ⟨"close", []⟩ ∨
      (∃ v sa l, e = ⟨"interpret", [boolV v, boolV sa, boolV l]⟩) ∨
      (∃ v sa l, e = ⟨"mapLegacy", [boolV v, boolV sa, boolV l]⟩) ∨
      (∃ pv l c m ac o sa, e = ⟨"assign", [boolV pv, boolV l, boolV c, boolV m, boolV ac, boolV o, boolV sa]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨bs, rfl, hl⟩ := bool_list (names := openFacts) h1
    obtain ⟨sc, rfl⟩ := enum_arg size_schema c1
    match bs, hl with
    | [st, fe, co], _ => exact .inl ⟨sc, st, fe, co, rfl⟩
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inl rfl)
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := sourceFacts) hb
    match bs, hl with
    | [v, sa, l], _ => exact .inr (.inr (.inl ⟨v, sa, l, rfl⟩))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := sourceFacts) hb
    match bs, hl with
    | [v, sa, l], _ => exact .inr (.inr (.inr (.inl ⟨v, sa, l, rfl⟩)))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := assignFacts) hb
    match bs, hl with
    | [pv, l, c, m, ac, o, sa], _ => exact .inr (.inr (.inr (.inr ⟨pv, l, c, m, ac, o, sa, rfl⟩)))

end KanderFormal.Proofs.ReviewFindings
