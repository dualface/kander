import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.IssueResultSave

/-
Proof obligations of the issue result resource machine: the compare-and-swap of
the central result record. The theorems hold for every value of `maxRev`.
-/
namespace KanderFormal.Proofs.IssueResultSave

open KanderFormal KanderFormal.Machines.IssueResultSave KanderFormal.Proofs.Scalar

abbrev M := issueResultSaveMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state: the resource revision. -/
structure Rq where
  rev : Nat

def enc (x : Rq) : State := [("rev", natV x.rev)]

theorem enc_inj {x y : Rq} (h : enc x = enc y) : x = y := by
  obtain ⟨_⟩ := x
  obtain ⟨_⟩ := y
  simp [enc, natV] at h
  subst h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) : ∃ x, s = enc x ∧ x.rev ≤ p "maxRev" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1)], hn =>
    simp [issueResultSaveMachine] at hn
    obtain rfl := hn
    obtain ⟨t1, ht1, c1⟩ := hc "rev" v1 (by simp [lookup])
    cases (rfl : M.fieldTy "rev" = some (.scalar (.nat "maxRev"))).symm.trans ht1
    obtain ⟨rv, rfl, hrv⟩ := conforms_nat c1
    exact ⟨⟨rv⟩, rfl, hrv⟩

/-! ## Events on an encoded state -/

theorem find_read : M.findEvent "read" = some readEvent := rfl
theorem find_save : M.findEvent "save" = some saveEvent := rfl
theorem find_replay : M.findEvent "replay" = some replayEvent := rfl
theorem ty_rev : M.fieldTy "rev" = some (.scalar (.nat "maxRev")) := rfl

macro "sv_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_read, find_save, find_replay, ty_rev, lookup, readEvent, saveEvent, replayEvent,
      boolArg, revArg, bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr, boolNot,
      indexOf, enumV, boolV, natV, f, a, tt, andE, orE, notE, eq, zero, evalAssigns, applyUpdates, enc, or_assoc,
      and_assoc, Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_read (p : Params) (x : Rq) (found : Bool) :
    step M p (enc x) ⟨"read", [boolV found]⟩ = if found = false ∨ x.rev ≠ 0 then some (enc x) else none := by
  cases found <;> sv_simp [] <;> ite_close

set_option maxHeartbeats 4000000 in
theorem step_save (p : Params) (x : Rq) (work : Bool) (expected : Nat) (room upd tx : Bool) :
    step M p (enc x) ⟨"save", [boolV work, natV expected, boolV room, boolV upd, boolV tx]⟩ =
      if expected ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧ work = true ∧ expected = x.rev ∧ room = true ∧
          upd = true ∧ tx = true then some (enc ⟨x.rev + 1⟩) else none := by
  by_cases h1 : expected ≤ p "maxRev" <;> by_cases h2 : x.rev + 1 ≤ p "maxRev" <;> cases work <;> cases room <;>
    cases upd <;> cases tx <;> sv_simp [h1, h2] <;> ite_close

theorem step_replay (p : Params) (x : Rq) : step M p (enc x) ⟨"replay", []⟩ = some (enc x) := by
  sv_simp []

theorem ite_some_enc {c : Prop} [Decidable c] {z : Rq} {s' : State}
    (h : (if c then some (enc z) else none) = some s') : c ∧ s' = enc z := by
  split at h
  · cases h; exact ⟨‹_›, rfl⟩
  · cases h

theorem ite_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : ¬c) : (if c then some v else none) = none := by
  simp [h]


/-! ## Accepted events -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) : ev = readEvent ∨ ev = saveEvent ∨ ev = replayEvent := by
  simpa [issueResultSaveMachine] using h

theorem bool_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArg n :: ds) vs = some args) :
    ∃ b vs' args', vs = boolV b :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨b, rfl⟩ := conforms_bool c
  exact ⟨b, vs', a, rfl, h'⟩

theorem rev_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (revArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

/-- Every accepted event is one of the three, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ fo, e = ⟨"read", [boolV fo]⟩) ∨ (∃ wk ex rm up tx, e = ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩) ∨
      e = ⟨"replay", []⟩ := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl
  · obtain ⟨fo, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact .inl ⟨fo, rfl⟩
  · obtain ⟨wk, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨ex, vs2, a2, rfl, h2⟩ := rev_cons h1
    obtain ⟨rm, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨up, vs4, a4, rfl, h4⟩ := bool_cons h3
    obtain ⟨tx, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    exact .inr (.inl ⟨wk, ex, rm, up, tx, rfl⟩)
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inr rfl)

/-- An accepted event never lowers the revision, and only a save raises it, by
exactly one. -/
theorem rev_step {p : Params} {x y : Rq} {e : Event} (h : step M p (enc x) e = some (enc y)) :
    y.rev = x.rev ∨ y.rev = x.rev + 1 := by
  rcases accepted_cases h with ⟨fo, rfl⟩ | ⟨wk, ex, rm, up, tx, rfl⟩ | rfl
  · rw [step_read] at h; have := enc_inj (ite_some_enc h).2.symm; subst this; exact .inl rfl
  · rw [step_save] at h; have := enc_inj (ite_some_enc h).2.symm; subst this; exact .inr rfl
  · rw [step_replay] at h; have := enc_inj (Option.some.inj h).symm; subst this; exact .inl rfl

/-! ## Theorem 7: the compare-and-swap -/

/-- Theorem 7: a save is accepted exactly when the request's work digest is the
current one, it expects the stored revision, the revision has room below the
last `uint64`, the new store passes the update and work relations, and the
transaction guard holds. -/
theorem save_spec (p : Params) (x : Rq) (wk : Bool) (ex : Nat) (rm up tx : Bool) :
    step M p (enc x) ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩ ≠ none ↔
      ex ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧ wk = true ∧ ex = x.rev ∧ rm = true ∧ up = true ∧ tx = true := by
  rw [step_save]
  by_cases h : ex ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧ wk = true ∧ ex = x.rev ∧ rm = true ∧ up = true ∧
    tx = true <;> simp [h]

/-- Theorem 7: an accepted save raises the revision by exactly one. -/
theorem save_result (p : Params) (x : Rq) (wk : Bool) (ex : Nat) (rm up tx : Bool) {s' : State}
    (h : step M p (enc x) ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩ = some s') :
    s' = enc ⟨x.rev + 1⟩ := by
  rw [step_save] at h
  exact (ite_some_enc h).2

/-- Theorem 7: a save is refused when the work digest is not the current one,
the request expects another revision, the revision has no room, the new store
fails the relations, or the transaction guard refuses. -/
theorem save_refused (p : Params) (x : Rq) (wk : Bool) (ex : Nat) (rm up tx : Bool) :
    (wk = false → step M p (enc x) ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩ = none) ∧
    (ex ≠ x.rev → step M p (enc x) ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩ = none) ∧
    (rm = false → step M p (enc x) ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩ = none) ∧
    (up = false → step M p (enc x) ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩ = none) ∧
    (tx = false → step M p (enc x) ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_save]; apply ite_none; intro c; obtain ⟨-, -, c3, c4, c5, c6, c7⟩ := c; simp_all)

/-- Theorem 7: a save at the last revision is refused, so the revision never
wraps. -/
theorem save_ceiling (p : Params) (x : Rq) (wk : Bool) (ex : Nat) (rm up tx : Bool) (h : x.rev = p "maxRev") :
    step M p (enc x) ⟨"save", [boolV wk, natV ex, boolV rm, boolV up, boolV tx]⟩ = none := by
  rw [step_save]; apply ite_none; intro c; obtain ⟨-, c2, -⟩ := c; omega

/-- Theorem 7: the replay of a completed request leaves the state as it is. -/
theorem replay_keeps (p : Params) (x : Rq) : step M p (enc x) ⟨"replay", []⟩ = some (enc x) := step_replay p x

/-- Theorem 7: a stored record has a resource revision: a record that exists
with revision 0 is refused, and any other read is accepted and changes
nothing. -/
theorem read_spec (p : Params) (x : Rq) (fo : Bool) :
    step M p (enc x) ⟨"read", [boolV fo]⟩ ≠ none ↔ (fo = false ∨ x.rev ≠ 0) := by
  rw [step_read]
  by_cases h : fo = false ∨ x.rev ≠ 0 <;> simp [h]

theorem read_refused (p : Params) (x : Rq) (h : x.rev = 0) :
    step M p (enc x) ⟨"read", [boolV true]⟩ = none := by
  rw [step_read]; apply ite_none; simp [h]

/-! ## The invariant -/

/-- Every reachable state conforms to its types. -/
def Inv (p : Params) (s : State) : Prop := M.StateConforms p s

theorem init (p : Params) : Inv p (initial M p) := initial_conforms wellTyped p

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := step_preserves_conforms hi h

def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

end KanderFormal.Proofs.IssueResultSave
