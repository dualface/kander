import KanderFormal.Proofs.ServerRequestSteps

/-
The events an accepted step of the server request machine ran, and the helpers
that read an accepted `if`.
-/
namespace KanderFormal.Proofs.ServerRequest

open KanderFormal KanderFormal.Machines.ServerRequest KanderFormal.Proofs.Scalar

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = bindEvent ∨ ev = createEvent ∨ ev = recreateEvent ∨ ev = answerSameEvent ∨ ev = answerEvent ∨
      ev = supersedeEvent ∨ ev = prepareEvent ∨ ev = completeEvent := by
  simpa [serverRequestMachine] using h

theorem bool_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArg n :: ds) vs = some args) :
    ∃ b vs' args', vs = boolV b :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨b, rfl⟩ := conforms_bool c
  exact ⟨b, vs', a, rfl, h'⟩

theorem gen_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (genArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem rev_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (revArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem status_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (statusArg n :: ds) vs = some args) :
    ∃ i vs' args', vs = enumV i :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨i, rfl, -⟩ := conforms_enum (rfl : M.enumSize "status" = some 6) c
  exact ⟨i, vs', a, rfl, h'⟩

/-- Every accepted event is one of the eight, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ tk ir ca, e = ⟨"bind", [boolV tk, natV ir, natV ca]⟩) ∨
    (∃ he sd tg pr ir, e = ⟨"create", [boolV he, boolV sd, natV tg, boolV pr, natV ir]⟩) ∨
    (∃ he sd tg ca sm, e = ⟨"recreate", [boolV he, boolV sd, natV tg, natV ca, boolV sm]⟩) ∨
    (∃ ho cg an ex sn wi ca,
      e = ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩) ∨
    (∃ ho cg an ex sn wi ca,
      e = ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩) ∨
    (∃ ng, e = ⟨"supersede", [natV ng]⟩) ∨
    (∃ un ho cg wi ca ma, e = ⟨"prepare", [boolV un, boolV ho, natV cg, boolV wi, natV ca, boolV ma]⟩) ∨
    (∃ co fo im cg, e = ⟨"complete", [boolV co, boolV fo, boolV im, natV cg]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨tk, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨ir, vs2, a2, rfl, h2⟩ := rev_cons h1
    obtain ⟨ca, vs3, a3, rfl, h3⟩ := rev_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    exact .inl ⟨tk, ir, ca, rfl⟩
  · obtain ⟨he, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨sd, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨tg, vs3, a3, rfl, h3⟩ := gen_cons h2
    obtain ⟨pr, vs4, a4, rfl, h4⟩ := bool_cons h3
    obtain ⟨ir, vs5, a5, rfl, h5⟩ := rev_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    exact .inr (.inl ⟨he, sd, tg, pr, ir, rfl⟩)
  · obtain ⟨he, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨sd, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨tg, vs3, a3, rfl, h3⟩ := gen_cons h2
    obtain ⟨ca, vs4, a4, rfl, h4⟩ := rev_cons h3
    obtain ⟨sm, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    exact .inr (.inr (.inl ⟨he, sd, tg, ca, sm, rfl⟩))
  · obtain ⟨ho, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨cg, vs2, a2, rfl, h2⟩ := gen_cons h1
    obtain ⟨an, vs3, a3, rfl, h3⟩ := status_cons h2
    obtain ⟨ex, vs4, a4, rfl, h4⟩ := rev_cons h3
    obtain ⟨sn, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨wi, vs6, a6, rfl, h6⟩ := bool_cons h5
    obtain ⟨ca, vs7, a7, rfl, h7⟩ := rev_cons h6
    obtain ⟨rfl, -⟩ := bindArgs_nil h7
    exact .inr (.inr (.inr (.inl ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩)))
  · obtain ⟨ho, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨cg, vs2, a2, rfl, h2⟩ := gen_cons h1
    obtain ⟨an, vs3, a3, rfl, h3⟩ := status_cons h2
    obtain ⟨ex, vs4, a4, rfl, h4⟩ := rev_cons h3
    obtain ⟨sn, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨wi, vs6, a6, rfl, h6⟩ := bool_cons h5
    obtain ⟨ca, vs7, a7, rfl, h7⟩ := rev_cons h6
    obtain ⟨rfl, -⟩ := bindArgs_nil h7
    exact .inr (.inr (.inr (.inr (.inl ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩))))
  · obtain ⟨ng, vs1, a1, rfl, h1⟩ := gen_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inr (.inr (.inr (.inr (.inr (.inl ⟨ng, rfl⟩)))))
  · obtain ⟨un, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨ho, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨cg, vs3, a3, rfl, h3⟩ := gen_cons h2
    obtain ⟨wi, vs4, a4, rfl, h4⟩ := bool_cons h3
    obtain ⟨ca, vs5, a5, rfl, h5⟩ := rev_cons h4
    obtain ⟨ma, vs6, a6, rfl, h6⟩ := bool_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨un, ho, cg, wi, ca, ma, rfl⟩))))))
  · obtain ⟨co, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨fo, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨im, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨cg, vs4, a4, rfl, h4⟩ := gen_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr ⟨co, fo, im, cg, rfl⟩))))))

theorem ite_ne_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : (if c then some v else none) ≠ none) : c := by
  refine Classical.byContradiction fun hc => ?_
  simp [hc] at h

theorem ite_some_enc {c : Prop} [Decidable c] {z : Rq} {s' : State}
    (h : (if c then some (enc z) else none) = some s') : c ∧ s' = enc z := by
  split at h
  · cases h; exact ⟨‹_›, rfl⟩
  · cases h

theorem ite_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : ¬c) : (if c then some v else none) = none := by
  simp [h]

end KanderFormal.Proofs.ServerRequest
