import KanderFormal.Obligations
import KanderFormal.EvalLemmas
import KanderFormal.Machines.ReviewPlan

/-
Proof obligations of the review plan machine. They hold for every value of
`maxBatch`.
-/
namespace KanderFormal.Proofs.ReviewPlan

open KanderFormal KanderFormal.Machines.ReviewPlan

abbrev M := reviewPlanMachine

theorem wellTyped : M.wellTyped = true := by decide

/-! ## Reading an accepted guard -/

theorem allArgs_true {env : Env} : ∀ {ns : List String}, eval env (allArgs ns) = some (boolV true) →
    ∀ n ∈ ns, lookup n env.args = some (boolV true)
  | [], _, n, hn => by simp at hn
  | [x], h, n, hn => by
    simp only [List.mem_singleton] at hn; subst hn
    simpa [allArgs, a, eval] using h
  | x :: y :: ys, h, n, hn => by
    obtain ⟨h1, h2⟩ := eval_and_true h
    rcases List.mem_cons.mp hn with rfl | hn
    · simpa [a, eval] using h1
    · exact allArgs_true h2 n hn

/-- The guard and arguments of an accepted event. -/
theorem accepted_args {p : Params} {s s' : State} {name : String} {vs : List Val} {ev : EventDecl}
    (hev : M.findEvent name = some ev) (h : step M p s ⟨name, vs⟩ = some s') :
    ∃ args, bindArgs M p ev.args vs = some args ∧ eval ⟨M, p, s, args, []⟩ ev.guard = some (boolV true) := by
  obtain ⟨ev', args, -, hev', hargs, hg, -, -⟩ := step_accepted h
  rw [hev] at hev'; cases hev'
  exact ⟨args, hargs, hg⟩

/-- The arguments bound to declarations, in order. -/
theorem bind_pairs {m : Machine} {p : Params} :
    ∀ {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}, bindArgs m p ds vs = some args →
      args = (ds.map ArgDecl.name).zip vs
  | [], vs, args, h => by
    obtain ⟨rfl, rfl⟩ := bindArgs_nil h; rfl
  | d :: ds, vs, args, h => by
    obtain ⟨v, vs', args', rfl, -, h', rfl⟩ := bindArgs_cons h
    simp [bind_pairs h']

/-! ## Theorem 4: integrity facts -/

theorem find_advance : M.findEvent "advance" = some advanceEvent := rfl
theorem find_close : M.findEvent "close" = some closeEvent := rfl
theorem find_resolve : M.findEvent "resolve" = some resolveEvent := rfl

/-- An accepted advance had every integrity fact: the batch revision and target
CAS, no successor batch, the attribution, settled dispatches, and recorded
author dispositions. -/
theorem advance_facts {p : Params} {s s' : State} {at_ v1 v2 v3 v4 v5 v6 : Val}
    (h : step M p s ⟨"advance", [at_, v1, v2, v3, v4, v5, v6]⟩ = some s') :
    v1 = boolV true ∧ v2 = boolV true ∧ v3 = boolV true ∧ v4 = boolV true ∧ v5 = boolV true ∧ v6 = boolV true := by
  obtain ⟨args, hb, hg⟩ := accepted_args find_advance h
  have hf := allArgs_true (eval_and_true (eval_and_true hg).2).2
  rw [bind_pairs hb] at hf
  simp [advanceEvent, advanceFacts, boolArgs, atArg, lookup] at hf
  simp_all

/-- An accepted close had every integrity fact. -/
theorem close_facts {p : Params} {s s' : State} {at_ v1 v2 v3 v4 v5 v6 v7 : Val}
    (h : step M p s ⟨"close", [at_, v1, v2, v3, v4, v5, v6, v7]⟩ = some s') :
    v1 = boolV true ∧ v2 = boolV true ∧ v3 = boolV true ∧ v4 = boolV true ∧ v5 = boolV true ∧
      v6 = boolV true ∧ v7 = boolV true := by
  obtain ⟨args, hb, hg⟩ := accepted_args find_close h
  have hf := allArgs_true (eval_and_true (eval_and_true hg).2).2
  rw [bind_pairs hb] at hf
  simp [closeEvent, closeFacts, boolArgs, atArg, lookup] at hf
  simp_all

/-- An accepted resolution had every integrity fact. -/
theorem resolve_facts {p : Params} {s s' : State} {v1 v2 v3 v4 v5 : Val}
    (h : step M p s ⟨"resolve", [v1, v2, v3, v4, v5]⟩ = some s') :
    v1 = boolV true ∧ v2 = boolV true ∧ v3 = boolV true ∧ v4 = boolV true ∧ v5 = boolV true := by
  obtain ⟨args, hb, hg⟩ := accepted_args find_resolve h
  have hf := allArgs_true (eval_and_true hg).2
  rw [bind_pairs hb] at hf
  simp [resolveEvent, resolveFacts, boolArgs, lookup] at hf
  simp_all

/-- Theorem 4: when any integrity fact of an advance, a close, or a resolution
does not hold, the event is refused and the state stays as it was. -/
theorem integrity_refused {p : Params} {s : State} {e : Event}
    (h : (e.name = "advance" ∧ ∃ at_ vs, e.args = at_ :: vs ∧ vs.length = 6 ∧ boolV false ∈ vs) ∨
      (e.name = "close" ∧ ∃ at_ vs, e.args = at_ :: vs ∧ vs.length = 7 ∧ boolV false ∈ vs) ∨
      (e.name = "resolve" ∧ e.args.length = 5 ∧ boolV false ∈ e.args)) :
    step M p s e = none ∧ run M p s e = (s, false) := by
  have hn : step M p s e = none := by
    cases hs : step M p s e with
    | none => rfl
    | some s' =>
      exfalso
      obtain ⟨n, as⟩ := e
      rcases h with ⟨rfl, at_, vs, rfl, hl, hf⟩ | ⟨rfl, at_, vs, rfl, hl, hf⟩ | ⟨rfl, hl, hf⟩
      · match vs, hl with
        | [v1, v2, v3, v4, v5, v6], _ =>
          obtain ⟨h1, h2, h3, h4, h5, h6⟩ := advance_facts hs
          simp [h1, h2, h3, h4, h5, h6, boolV] at hf
      · match vs, hl with
        | [v1, v2, v3, v4, v5, v6, v7], _ =>
          obtain ⟨h1, h2, h3, h4, h5, h6, h7⟩ := close_facts hs
          simp [h1, h2, h3, h4, h5, h6, h7, boolV] at hf
      · match as, hl with
        | [v1, v2, v3, v4, v5], _ =>
          obtain ⟨h1, h2, h3, h4, h5⟩ := resolve_facts hs
          simp [h1, h2, h3, h4, h5, boolV] at hf
  exact ⟨hn, by simp [run, hn]⟩

theorem assigns_cons' {env : Env} {fn : String} {x : Expr} {as : List (String × Expr)} {us : List (String × Val)}
    (h : AssignsMatch env ((fn, x) :: as) us) :
    ∃ v us', us = (fn, v) :: us' ∧ eval env x = some v ∧ AssignsMatch env as us' := by
  cases h with
  | cons hv _ ht => exact ⟨_, _, rfl, hv, ht⟩

/-! ## Theorem 5: a sealed plan takes no batch outside the rule -/

theorem find_append : M.findEvent "append" = some appendEvent := rfl
theorem find_seal : M.findEvent "seal" = some sealEvent := rfl
theorem find_admit : M.findEvent "admit" = some admitEvent := rfl

theorem eval_sealed {p : Params} {s : State} {args : List (String × Val)} (h : lookup "sealed" s = some (boolV true)) :
    eval ⟨M, p, s, args, []⟩ (notE (f "sealed")) = some (boolV false) := by
  unfold notE f
  simp only [eval]
  rw [h]
  rfl

theorem arg_true {p : Params} {s : State} {args : List (String × Val)} {n : String} {c : Bool}
    (h : eval ⟨M, p, s, args, []⟩ (a n) = some (boolV c)) : lookup n args = some (boolV c) := by
  simpa [a, eval] using h

/-- Theorem 5: a sealed plan takes a batch only for a member whose rebound cycle
has none or whose recovery still owes a review (`owed`), and never with another
seal. -/
theorem sealed_append {p : Params} {s s' : State} {vs : List Val} (hs : lookup "sealed" s = some (boolV true))
    (h : step M p s ⟨"append", vs⟩ = some s') : ∃ rest, vs = boolV false :: boolV true :: rest := by
  obtain ⟨args, hb, hg⟩ := accepted_args find_append h
  have hb' := bind_pairs hb
  obtain ⟨-, g⟩ := eval_and_true hg
  obtain ⟨-, g⟩ := eval_and_true g
  obtain ⟨-, g⟩ := eval_and_true g
  obtain ⟨g, -⟩ := eval_and_true g
  obtain ⟨x, y, hx, hy, hc⟩ := eval_or_parts g
  rw [eval_sealed hs] at hx
  cases Option.some.inj hx
  simp only [Bool.false_or] at hc
  subst hc
  obtain ⟨u, w, hu, hw, hc⟩ := eval_and_parts hy
  have hu' := eval_not_parts hu
  have h1 := arg_true hu'
  have h2 := arg_true hw
  have hc' : u = true ∧ w = true := by simpa using hc.symm
  obtain ⟨rfl, rfl⟩ := hc'
  rw [hb'] at h1 h2
  match vs with
  | v1 :: v2 :: rest =>
    simp [appendEvent, boolArgs, appendFacts, lookup] at h1 h2
    exact ⟨rest, by rw [← h1, ← h2]⟩
  | [] => simp [lookup] at h1
  | [_] => simp [appendEvent, boolArgs, appendFacts, lookup] at h2

/-- A sealed plan refuses another seal. -/
theorem sealed_seal {p : Params} {s : State} {vs : List Val} (hs : lookup "sealed" s = some (boolV true)) :
    step M p s ⟨"seal", vs⟩ = none := by
  cases h : step M p s ⟨"seal", vs⟩ with
  | none => rfl
  | some s' =>
    obtain ⟨args, -, hg⟩ := accepted_args find_seal h
    obtain ⟨-, g⟩ := eval_and_true hg
    obtain ⟨g, -⟩ := eval_and_true g
    rw [eval_sealed hs] at g
    simp [boolV] at g

/-- The admission of an extension to a sealed plan: only a rebind, a sync, a
drop, a release, or a batch without a seal that is owed. -/
theorem sealed_admit {p : Params} {s s' : State} {cas otherOp batchOnly owed : Val}
    (hs : lookup "sealed" s = some (boolV true)) (h : step M p s ⟨"admit", [cas, otherOp, batchOnly, owed]⟩ = some s') :
    otherOp = boolV true ∨ (batchOnly = boolV true ∧ owed = boolV true) := by
  obtain ⟨args, hb, hg⟩ := accepted_args find_admit h
  have hargs : args = [("cas", cas), ("otherOp", otherOp), ("batchOnly", batchOnly), ("owed", owed)] := by
    rw [bind_pairs hb]; rfl
  subst hargs
  obtain ⟨-, g⟩ := eval_and_true hg
  obtain ⟨-, g⟩ := eval_and_true g
  obtain ⟨x, y, hx, hy, hc⟩ := eval_or_parts g
  rw [eval_sealed hs] at hx
  cases Option.some.inj hx
  simp only [Bool.false_or] at hc
  subst hc
  obtain ⟨u, w, hu, hw, hc⟩ := eval_or_parts hy
  have hu' := arg_true hu
  simp only [lookup] at hu'
  cases u
  · simp only [Bool.false_or] at hc
    obtain ⟨b1, b2, h1, h2, hc⟩ := eval_and_parts (hc ▸ hw)
    have h1' := arg_true h1
    have h2' := arg_true h2
    simp only [lookup] at h1' h2'
    have hc' : b1 = true ∧ b2 = true := by simpa using hc.symm
    obtain ⟨rfl, rfl⟩ := hc'
    exact .inr ⟨by simpa using h1', by simpa using h2'⟩
  · exact .inl (by simpa using hu')

/-! ## Theorem 6: invariant -/

/-- A state without a plan is the initial state: no batch, closure, or seal
exists without a plan. Reachable states also conform to their declared types. -/
def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ (lookup "planned" s = some (boolV false) → s = initial M p)

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = createEvent ∨ ev = extendEvent ∨ ev ∈ [admitEvent, appendEvent, sealEvent, rebindEvent, syncEvent, dropEvent,
      releaseEvent, resolveEvent, runEvent, advanceEvent, aggregateEvent, closeEvent] := by
  simpa [M, reviewPlanMachine] using h

theorem planned_guard {ev : EventDecl}
    (h : ev ∈ [admitEvent, appendEvent, sealEvent, rebindEvent, syncEvent, dropEvent, releaseEvent, resolveEvent,
      runEvent, advanceEvent, aggregateEvent, closeEvent]) :
    ∃ g, ev.guard = .and (f "planned") g ∧ ∀ x, ("planned", x) ∉ ev.assigns := by
  simp only [List.mem_cons, List.not_mem_nil, or_false] at h
  rcases h with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl <;>
    exact ⟨_, rfl, by simp [admitEvent, appendEvent, sealEvent, rebindEvent, syncEvent, dropEvent, releaseEvent,
      resolveEvent, runEvent, advanceEvent, aggregateEvent, closeEvent]⟩

theorem lookup_names {s : State} {f : String} (hn : s.map Prod.fst = M.fields.map FieldDecl.name)
    (hf : f ∈ M.fields.map FieldDecl.name) : ∃ v, lookup f s = some v :=
  lookup_some_of_mem (hn ▸ hf)

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s) (h : step M p s e = some s') :
    Inv p s' := by
  obtain ⟨hc, hinit⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨ev, args, us, hev, hargs, hg, hu, rfl⟩ := step_accepted h
  have hmem := lookup_map_mem hev
  rcases events_cases hmem with rfl | rfl | hin
  · -- create records the plan
    intro hp
    have hm := evalAssigns_spec hu
    simp only [createEvent] at hm
    obtain ⟨v1, us1, rfl, h1, -⟩ := assigns_cons' hm
    obtain ⟨w, hw⟩ := lookup_names hc.1 (f := "planned") (by decide)
    rw [lookup_applyUpdates, hw] at hp
    simp only [eval, tt] at h1
    cases h1
    simp [lookup, boolV] at hp
  · -- an extension shape changes nothing
    intro hp
    have hm := evalAssigns_spec hu
    simp only [extendEvent] at hm
    cases hm
    have : applyUpdates s [] = s := by simp [applyUpdates, lookup]
    rw [this] at hp ⊢
    exact hinit hp
  · intro hp
    obtain ⟨g, hgd, hna⟩ := planned_guard hin
    rw [hgd] at hg
    obtain ⟨h1, -⟩ := eval_and_true hg
    have hs : lookup "planned" s = some (boolV true) := by simpa [f, eval] using h1
    rw [step_unassigned_unchanged h hev hna, hs] at hp
    simp [boolV] at hp

def cert : SafetyCert M where
  Inv := Inv
  init := fun p => ⟨initial_conforms wellTyped p, fun _ => rfl⟩
  step := step_inv

/-- Theorem 6: every reachable state of the plan machine satisfies the
invariant, for every value of `maxBatch`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

end KanderFormal.Proofs.ReviewPlan
