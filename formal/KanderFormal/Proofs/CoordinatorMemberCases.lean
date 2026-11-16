import KanderFormal.Proofs.CoordinatorMemberStep

/-
The events an accepted step of the coordinator member machine ran: one of the
six declared events, with arguments of their declared types.
-/
namespace KanderFormal.Proofs.CoordinatorMember

open KanderFormal KanderFormal.Machines.CoordinatorMember KanderFormal.Proofs.Scalar

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = acceptEvent ∨ ev = acceptConfirmedEvent ∨ ev = acceptTerminalEvent ∨ ev = blockEvent ∨
      ev = recoverResetEvent ∨ ev = recoverConfirmEvent := by
  simpa [coordMemberMachine] using h

theorem nat_arg {p : Params} {b : String} {v : Val} (h : Ty.conforms M p (.scalar (.nat b)) v = true) :
    ∃ n, v = natV n ∧ n ≤ p b := by
  obtain ⟨n, rfl, hn⟩ := conforms_nat h; exact ⟨n, rfl, hn⟩

theorem bool_arg {p : Params} {v : Val} (h : Ty.conforms M p (.scalar .bool) v = true) : ∃ b, v = boolV b := by
  obtain ⟨b, rfl⟩ := conforms_bool h; exact ⟨b, rfl⟩

theorem col_arg {p : Params} {v : Val} (h : Ty.conforms M p (.scalar (.enum "col")) v = true) :
    ∃ i, v = enumV i ∧ i < 6 := by
  obtain ⟨i, rfl, hi⟩ := conforms_enum size_col h; exact ⟨i, rfl, hi⟩

/-- Every accepted event is one of the six, with arguments of its declared types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ nc nr nt nd cs ok mi,
      e = ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩) ∨
    (∃ nc nr cs, e = ⟨"acceptConfirmed", [enumV nc, natV nr, boolV cs]⟩) ∨
    (∃ nc nr nt nd hr cs ok mi,
      e = ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok, boolV mi]⟩) ∨
    e = ⟨"block", []⟩ ∨
    (∃ cc nr nt nd ok, e = ⟨"recoverReset", [enumV cc, natV nr, natV nt, natV nd, boolV ok]⟩) ∨
    (∃ cc nr tr nt dr nd, e = ⟨"recoverConfirm", [enumV cc, natV nr, boolV tr, natV nt, boolV dr, natV nd]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨v7, vs7, _, rfl, c7, h7, -⟩ := bindArgs_cons h6
    obtain ⟨rfl, -⟩ := bindArgs_nil h7
    obtain ⟨nc, rfl, -⟩ := col_arg c1
    obtain ⟨nr, rfl, -⟩ := nat_arg c2
    obtain ⟨nt, rfl, -⟩ := nat_arg c3
    obtain ⟨nd, rfl, -⟩ := nat_arg c4
    obtain ⟨cs, rfl⟩ := bool_arg c5
    obtain ⟨ok, rfl⟩ := bool_arg c6
    obtain ⟨mi, rfl⟩ := bool_arg c7
    exact .inl ⟨nc, nr, nt, nd, cs, ok, mi, rfl⟩
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    obtain ⟨nc, rfl, -⟩ := col_arg c1
    obtain ⟨nr, rfl, -⟩ := nat_arg c2
    obtain ⟨cs, rfl⟩ := bool_arg c3
    exact .inr (.inl ⟨nc, nr, cs, rfl⟩)
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨v7, vs7, _, rfl, c7, h7, -⟩ := bindArgs_cons h6
    obtain ⟨v8, vs8, _, rfl, c8, h8, -⟩ := bindArgs_cons h7
    obtain ⟨rfl, -⟩ := bindArgs_nil h8
    obtain ⟨nc, rfl, -⟩ := col_arg c1
    obtain ⟨nr, rfl, -⟩ := nat_arg c2
    obtain ⟨nt, rfl, -⟩ := nat_arg c3
    obtain ⟨nd, rfl, -⟩ := nat_arg c4
    obtain ⟨hr, rfl⟩ := bool_arg c5
    obtain ⟨cs, rfl⟩ := bool_arg c6
    obtain ⟨ok, rfl⟩ := bool_arg c7
    obtain ⟨mi, rfl⟩ := bool_arg c8
    exact .inr (.inr (.inl ⟨nc, nr, nt, nd, hr, cs, ok, mi, rfl⟩))
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inr (.inr (.inl rfl)))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    obtain ⟨cc, rfl, -⟩ := col_arg c1
    obtain ⟨nr, rfl, -⟩ := nat_arg c2
    obtain ⟨nt, rfl, -⟩ := nat_arg c3
    obtain ⟨nd, rfl, -⟩ := nat_arg c4
    obtain ⟨ok, rfl⟩ := bool_arg c5
    exact .inr (.inr (.inr (.inr (.inl ⟨cc, nr, nt, nd, ok, rfl⟩))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    obtain ⟨cc, rfl, -⟩ := col_arg c1
    obtain ⟨nr, rfl, -⟩ := nat_arg c2
    obtain ⟨tr, rfl⟩ := bool_arg c3
    obtain ⟨nt, rfl, -⟩ := nat_arg c4
    obtain ⟨dr, rfl⟩ := bool_arg c5
    obtain ⟨nd, rfl, -⟩ := nat_arg c6
    exact .inr (.inr (.inr (.inr (.inr ⟨cc, nr, tr, nt, dr, nd, rfl⟩))))

end KanderFormal.Proofs.CoordinatorMember
