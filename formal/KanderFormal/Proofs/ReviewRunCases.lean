import KanderFormal.Proofs.ReviewRunStep

/-
The events an accepted step of the review run machine ran: one of the eleven
declared events, with arguments of their declared types.
-/
namespace KanderFormal.Proofs.ReviewRun

open KanderFormal KanderFormal.Machines.ReviewRun KanderFormal.Proofs.Scalar

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = createEvent ∨ ev = retryEvent ∨ ev = updateEvent ∨ ev = storePromptEvent ∨ ev = storeContractEvent ∨
      ev = storeEvidenceEvent ∨ ev = finalizeEvent ∨ ev = stageEvent ∨ ev = skipEvent ∨ ev = verifyEvent ∨
      ev = passSkippedEvent := by
  simpa [reviewRunMachine] using h

theorem enum_arg {p : Params} {n : String} {k : Nat} {v : Val} (hk : M.enumSize n = some k)
    (h : Ty.conforms M p (.scalar (.enum n)) v = true) : ∃ i, v = enumV i := by
  obtain ⟨i, rfl, -⟩ := conforms_enum hk h; exact ⟨i, rfl⟩

/-- A list of Boolean arguments of the declared length. -/
theorem bool_list {p : Params} {names : List String} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArgs names) vs = some args) : ∃ bs : List Bool, vs = bs.map boolV ∧ bs.length = names.length := by
  obtain ⟨bs, rfl, hl⟩ := args_bools (m := M) (p := p) (ds := boolArgs names)
    (by intro d hd; simp [boolArgs] at hd; obtain ⟨n, -, rfl⟩ := hd; rfl) h
  exact ⟨bs, rfl, by simpa [boolArgs] using hl⟩

/-- Every accepted event is one of the eleven, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ i c b r, e = ⟨"create", [boolV i, boolV c, boolV b, boolV r]⟩) ∨
      (∃ i c sa b r, e = ⟨"retry", [boolV i, boolV c, boolV sa, boolV b, boolV r]⟩) ∨
      (∃ tp tl te ez si, e = ⟨"update", [enumV tp, enumV tl, enumV te, boolV ez, boolV si]⟩) ∨
      (∃ sm, e = ⟨"storePrompt", [boolV sm]⟩) ∨ (∃ sm, e = ⟨"storeContract", [boolV sm]⟩) ∨
      (∃ sm, e = ⟨"storeEvidence", [boolV sm]⟩) ∨
      (∃ l ex ez ro rs inv cm si,
        e = ⟨"finalize", [enumV l, enumV ex, boolV ez, boolV ro, boolV rs, boolV inv, boolV cm, boolV si]⟩) ∨
      (∃ si g ex m ig, e = ⟨"stage", [boolV si, boolV g, boolV ex, boolV m, boolV ig]⟩) ∨
      (∃ si g ex, e = ⟨"skip", [boolV si, boolV g, boolV ex]⟩) ∨
      (∃ si g ig v, e = ⟨"verify", [boolV si, boolV g, boolV ig, boolV v]⟩) ∨
      (∃ si g, e = ⟨"passSkipped", [boolV si, boolV g]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := createFacts) hb
    match bs, hl with
    | [i, c, b, r], _ => exact .inl ⟨i, c, b, r, rfl⟩
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := retryFacts) hb
    match bs, hl with
    | [i, c, sa, b, r], _ => exact .inr (.inl ⟨i, c, sa, b, r, rfl⟩)
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["exitZero", "sameInput"]) h3
    obtain ⟨tp, rfl⟩ := enum_arg size_phase c1
    obtain ⟨tl, rfl⟩ := enum_arg size_launch c2
    obtain ⟨te, rfl⟩ := enum_arg size_exec c3
    match bs, hl with
    | [ez, si], _ => exact .inr (.inr (.inl ⟨tp, tl, te, ez, si, rfl⟩))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["same"]) hb
    match bs, hl with
    | [sm], _ => exact .inr (.inr (.inr (.inl ⟨sm, rfl⟩)))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["same"]) hb
    match bs, hl with
    | [sm], _ => exact .inr (.inr (.inr (.inr (.inl ⟨sm, rfl⟩))))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["same"]) hb
    match bs, hl with
    | [sm], _ => exact .inr (.inr (.inr (.inr (.inr (.inl ⟨sm, rfl⟩)))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨bs, rfl, hl⟩ := bool_list (names := finalizeFacts) h2
    obtain ⟨l, rfl⟩ := enum_arg size_launch c1
    obtain ⟨ex, rfl⟩ := enum_arg size_exec c2
    match bs, hl with
    | [ez, ro, rs, inv, cm, si], _ => exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨l, ex, ez, ro, rs, inv, cm, si, rfl⟩))))))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["sameInput", "groupOk", "excluded", "memberOk", "integrity"]) hb
    match bs, hl with
    | [si, g, ex, m, ig], _ =>
      exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨si, g, ex, m, ig, rfl⟩)))))))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["sameInput", "groupOk", "excluded"]) hb
    match bs, hl with
    | [si, g, ex], _ => exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨si, g, ex, rfl⟩))))))))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["sameInput", "groupOk", "integrity", "verified"]) hb
    match bs, hl with
    | [si, g, ig, v], _ =>
      exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨si, g, ig, v, rfl⟩)))))))))
  · obtain ⟨bs, rfl, hl⟩ := bool_list (names := ["sameInput", "groupOk"]) hb
    match bs, hl with
    | [si, g], _ => exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr ⟨si, g, rfl⟩)))))))))

end KanderFormal.Proofs.ReviewRun
