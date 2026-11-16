import KanderFormal.Proofs.CoordinatorStep

/-
The events an accepted step of the coordinator checkpoint machine ran: one of
the three declared events, with arguments of their declared types.
-/
namespace KanderFormal.Proofs.Coordinator

open KanderFormal KanderFormal.Machines.Coordinator KanderFormal.Proofs.Scalar

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = claimEvent ∨ ev = reconcileEvent ∨ ev = recoverEvent := by
  simpa [coordinatorMachine] using h

theorem nat_arg {p : Params} {b : String} {v : Val} (h : Ty.conforms M p (.scalar (.nat b)) v = true) :
    ∃ n, v = natV n ∧ n ≤ p b := by
  obtain ⟨n, rfl, hn⟩ := conforms_nat h; exact ⟨n, rfl, hn⟩

theorem bool_arg {p : Params} {v : Val} (h : Ty.conforms M p (.scalar .bool) v = true) : ∃ b, v = boolV b := by
  obtain ⟨b, rfl⟩ := conforms_bool h; exact ⟨b, rfl⟩

/-- Every accepted event is one of the three, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ er ee sm ts rm mo mb, e = ⟨"claim", [natV er, natV ee, boolV sm, boolV ts, boolV rm, boolV mo, boolV mb]⟩) ∨
    (∃ os ts ea er sm mo mm rc rf ch ba rm,
      e = ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc, boolV rf,
        boolV ch, natV ba, boolV rm]⟩) ∨
    (∃ er ee rm kn mb ex mo, e = ⟨"recover", [natV er, natV ee, boolV rm, boolV kn, boolV mb, boolV ex, boolV mo]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨v7, vs7, _, rfl, c7, h7, -⟩ := bindArgs_cons h6
    obtain ⟨rfl, -⟩ := bindArgs_nil h7
    obtain ⟨er, rfl, -⟩ := nat_arg c1
    obtain ⟨ee, rfl, -⟩ := nat_arg c2
    obtain ⟨sm, rfl⟩ := bool_arg c3
    obtain ⟨ts, rfl⟩ := bool_arg c4
    obtain ⟨rm, rfl⟩ := bool_arg c5
    obtain ⟨mo, rfl⟩ := bool_arg c6
    obtain ⟨mb, rfl⟩ := bool_arg c7
    exact .inl ⟨er, ee, sm, ts, rm, mo, mb, rfl⟩
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨v7, vs7, _, rfl, c7, h7, -⟩ := bindArgs_cons h6
    obtain ⟨v8, vs8, _, rfl, c8, h8, -⟩ := bindArgs_cons h7
    obtain ⟨v9, vs9, _, rfl, c9, h9, -⟩ := bindArgs_cons h8
    obtain ⟨v10, vs10, _, rfl, c10, h10, -⟩ := bindArgs_cons h9
    obtain ⟨v11, vs11, _, rfl, c11, h11, -⟩ := bindArgs_cons h10
    obtain ⟨v12, vs12, _, rfl, c12, h12, -⟩ := bindArgs_cons h11
    obtain ⟨rfl, -⟩ := bindArgs_nil h12
    obtain ⟨os, rfl⟩ := bool_arg c1
    obtain ⟨ts, rfl⟩ := bool_arg c2
    obtain ⟨ea, rfl, -⟩ := nat_arg c3
    obtain ⟨er, rfl, -⟩ := nat_arg c4
    obtain ⟨sm, rfl⟩ := bool_arg c5
    obtain ⟨mo, rfl⟩ := bool_arg c6
    obtain ⟨mm, rfl⟩ := bool_arg c7
    obtain ⟨rc, rfl⟩ := bool_arg c8
    obtain ⟨rf, rfl⟩ := bool_arg c9
    obtain ⟨ch, rfl⟩ := bool_arg c10
    obtain ⟨ba, rfl, -⟩ := nat_arg c11
    obtain ⟨rm, rfl⟩ := bool_arg c12
    exact .inr (.inl ⟨os, ts, ea, er, sm, mo, mm, rc, rf, ch, ba, rm, rfl⟩)
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨v7, vs7, _, rfl, c7, h7, -⟩ := bindArgs_cons h6
    obtain ⟨rfl, -⟩ := bindArgs_nil h7
    obtain ⟨er, rfl, -⟩ := nat_arg c1
    obtain ⟨ee, rfl, -⟩ := nat_arg c2
    obtain ⟨rm, rfl⟩ := bool_arg c3
    obtain ⟨kn, rfl⟩ := bool_arg c4
    obtain ⟨mb, rfl⟩ := bool_arg c5
    obtain ⟨ex, rfl⟩ := bool_arg c6
    obtain ⟨mo, rfl⟩ := bool_arg c7
    exact .inr (.inr ⟨er, ee, rm, kn, mb, ex, mo, rfl⟩)

end KanderFormal.Proofs.Coordinator
