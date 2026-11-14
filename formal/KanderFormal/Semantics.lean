import KanderFormal.Dsl

/-
The semantics of a machine definition: expression evaluation, the `step`
function, reachability, and the generic theorems that hold for every machine.
-/
namespace KanderFormal

/-- The evaluation environment of one event or query. `state` is always the
pre-state of the event. -/
structure Env where
  m : Machine
  params : Params
  state : State
  args : List (String × Val)

def boolV (b : Bool) : Val := .scalar (.bool b)
def natV (n : Nat) : Val := .scalar (.nat n)
def enumV (i : Nat) : Val := .scalar (.enum i)

def evalCmp : CmpOp → Val → Val → Option Val
  | .eq, a, b => some (boolV (decide (a = b)))
  | .ne, a, b => some (boolV (!decide (a = b)))
  | .lt, .scalar (.nat a), .scalar (.nat b) => some (boolV (decide (a < b)))
  | .le, .scalar (.nat a), .scalar (.nat b) => some (boolV (decide (a ≤ b)))
  | .gt, .scalar (.nat a), .scalar (.nat b) => some (boolV (decide (a > b)))
  | .ge, .scalar (.nat a), .scalar (.nat b) => some (boolV (decide (a ≥ b)))
  | _, _, _ => none

def natAdd : Val → Val → Option Val
  | .scalar (.nat a), .scalar (.nat b) => some (natV (a + b))
  | _, _ => none

/-- Subtraction refuses instead of truncating at zero. -/
def natSub : Val → Val → Option Val
  | .scalar (.nat a), .scalar (.nat b) => if b ≤ a then some (natV (a - b)) else none
  | _, _ => none

def boolAnd : Val → Val → Option Val
  | .scalar (.bool a), .scalar (.bool b) => some (boolV (a && b))
  | _, _ => none

def boolOr : Val → Val → Option Val
  | .scalar (.bool a), .scalar (.bool b) => some (boolV (a || b))
  | _, _ => none

def boolNot : Val → Option Val
  | .scalar (.bool a) => some (boolV (!a))
  | _ => none

def mapGet : Val → Val → Option Val
  | .map vs, .scalar (.enum i) => vs[i]?.map .scalar
  | _, _ => none

def mapPut : Val → Val → Val → Option Val
  | .map vs, .scalar (.enum i), .scalar x => if i < vs.length then some (.map (vs.set i x)) else none
  | _, _, _ => none

def setMem : Val → Val → Option Val
  | .set bs, .scalar (.enum i) => bs[i]?.map boolV
  | _, _ => none

def setWith (b : Bool) : Val → Val → Option Val
  | .set bs, .scalar (.enum i) => if i < bs.length then some (.set (bs.set i b)) else none
  | _, _ => none

/-- Expression evaluation. `and` and `or` evaluate both operands; `ite`
evaluates only the selected branch. `none` means the expression is undefined
(an ill-typed operand, or a subtraction below zero), which refuses the event. -/
def eval (env : Env) : Expr → Option Val
  | .boolLit b => some (boolV b)
  | .enumLit e c => (env.m.ctorsOf e).bind (indexOf c) |>.map enumV
  | .natLit n => some (natV n)
  | .field f => lookup f env.state
  | .param x => some (natV (env.params x))
  | .arg a => lookup a env.args
  | .cmp op a b => (eval env a).bind fun x => (eval env b).bind fun y => evalCmp op x y
  | .add a b => (eval env a).bind fun x => (eval env b).bind fun y => natAdd x y
  | .sub a b => (eval env a).bind fun x => (eval env b).bind fun y => natSub x y
  | .and a b => (eval env a).bind fun x => (eval env b).bind fun y => boolAnd x y
  | .or a b => (eval env a).bind fun x => (eval env b).bind fun y => boolOr x y
  | .not a => (eval env a).bind boolNot
  | .ite c t e => (eval env c).bind fun x =>
      match x with
      | .scalar (.bool true) => eval env t
      | .scalar (.bool false) => eval env e
      | _ => none
  | .get a k => (eval env a).bind fun x => (eval env k).bind fun y => mapGet x y
  | .put a k v => (eval env a).bind fun x => (eval env k).bind fun y =>
      (eval env v).bind fun z => mapPut x y z
  | .mem s x => (eval env s).bind fun a => (eval env x).bind fun b => setMem a b
  | .insert s x => (eval env s).bind fun a => (eval env x).bind fun b => setWith true a b
  | .erase s x => (eval env s).bind fun a => (eval env x).bind fun b => setWith false a b

/-- Binds event arguments to their names, refusing a wrong count or a value that
does not conform to its declared type (for example a number above its bound). -/
def bindArgs (m : Machine) (p : Params) : List ArgDecl → List Val → Option (List (String × Val))
  | [], [] => some []
  | d :: ds, v :: vs =>
    if (Ty.scalar d.ty).conforms m p v then (bindArgs m p ds vs).map ((d.name, v) :: ·) else none
  | _, _ => none

/-- Evaluates every right-hand side in the pre-state environment and checks the
result against the target field's declared type. A value outside its bound is
refused, never wrapped or clamped. -/
def evalAssigns (env : Env) : List (String × Expr) → Option (List (String × Val))
  | [] => some []
  | (f, x) :: rest =>
    (eval env x).bind fun v =>
      (env.m.fieldTy f).bind fun t =>
        if t.conforms env.m env.params v then (evalAssigns env rest).map ((f, v) :: ·) else none

/-- Replaces each assigned field and keeps the order and the other fields. -/
def applyUpdates (s : State) (us : List (String × Val)) : State :=
  s.map fun (f, v) => (f, (lookup f us).getD v)

def initial (m : Machine) : State :=
  m.fields.map fun d => (d.name, d.init)

/-- One event: `some` with the post-state when accepted, `none` when refused. -/
def step (m : Machine) (p : Params) (s : State) (e : Event) : Option State :=
  (m.findEvent e.name).bind fun ev =>
    (bindArgs m p ev.args e.args).bind fun args =>
      let env : Env := ⟨m, p, s, args⟩
      (eval env ev.guard).bind fun g =>
        if g = boolV true then (evalAssigns env ev.assigns).map (applyUpdates s) else none

/-- The total form the generated Go `Step` implements: a refused event returns
the original state and `false`. -/
def run (m : Machine) (p : Params) (s : State) (e : Event) : State × Bool :=
  match step m p s e with
  | some s' => (s', true)
  | none => (s, false)

def evalQuery (m : Machine) (p : Params) (s : State) (q : QueryDecl) : Option Val :=
  eval ⟨m, p, s, []⟩ q.body

inductive Reachable (m : Machine) (p : Params) : State → Prop
  | init : Reachable m p (initial m)
  | step {s s' : State} (e : Event) : Reachable m p s → step m p s e = some s' → Reachable m p s'

/-! ## Helper lemmas -/

theorem lookup_mem {α : Type} {k : String} {v : α} :
    ∀ {l : List (String × α)}, lookup k l = some v → (k, v) ∈ l
  | [], h => by simp [lookup] at h
  | (k', v') :: rest, h => by
    unfold lookup at h
    split at h
    · cases h; subst_vars; exact List.mem_cons_self
    · exact List.mem_cons_of_mem _ (lookup_mem h)

theorem lookup_map {α β : Type} (k : String) (key : α → String) (g : α → β) :
    ∀ l : List α, lookup k (l.map fun d => (key d, g d)) = (lookup k (l.map fun d => (key d, d))).map g
  | [] => rfl
  | d :: rest => by
    simp only [List.map, lookup]
    split
    · rfl
    · exact lookup_map k key g rest

theorem lookup_applyUpdates (f : String) (us : List (String × Val)) :
    ∀ s : State, lookup f (applyUpdates s us) = (lookup f s).map fun v => (lookup f us).getD v
  | [] => rfl
  | (f', v) :: rest => by
    simp only [applyUpdates, List.map, lookup]
    split
    · subst_vars; rfl
    · exact lookup_applyUpdates f us rest

theorem applyUpdates_names (s : State) (us : List (String × Val)) :
    (applyUpdates s us).map Prod.fst = s.map Prod.fst := by
  simp [applyUpdates]

/-- Pointwise relation between assignments and their evaluated updates. -/
inductive AssignsMatch (env : Env) : List (String × Expr) → List (String × Val) → Prop
  | nil : AssignsMatch env [] []
  | cons {f : String} {x : Expr} {v : Val} {as : List (String × Expr)} {us : List (String × Val)} :
      eval env x = some v → (∃ t, env.m.fieldTy f = some t ∧ t.conforms env.m env.params v = true) →
      AssignsMatch env as us → AssignsMatch env ((f, x) :: as) ((f, v) :: us)

theorem lookup_some_of_mem {α : Type} {f : String} :
    ∀ {s : List (String × α)}, f ∈ s.map Prod.fst → ∃ w, lookup f s = some w
  | [], h => by simp at h
  | (g, u) :: rest, h => by
    simp only [lookup]
    split
    · exact ⟨u, rfl⟩
    · rename_i hne
      simp only [List.map, List.mem_cons] at h
      rcases h with h | h
      · exact absurd h hne
      · exact lookup_some_of_mem h

/-- Shape of a successful `evalAssigns`: the targets are kept in order, each
value is the evaluation of its right-hand side in `env`, and each value conforms
to the target's declared type. -/
theorem evalAssigns_spec {env : Env} :
    ∀ {as : List (String × Expr)} {us : List (String × Val)}, evalAssigns env as = some us →
      AssignsMatch env as us
  | [], us, h => by
    simp [evalAssigns] at h; subst h; exact AssignsMatch.nil
  | (f, x) :: rest, us, h => by
    simp only [evalAssigns, Option.bind_eq_some_iff] at h
    obtain ⟨v, hv, t, ht, h⟩ := h
    split at h
    · rename_i hc
      simp only [Option.map_eq_some_iff] at h
      obtain ⟨us', hrest, rfl⟩ := h
      exact AssignsMatch.cons hv ⟨t, ht, hc⟩ (evalAssigns_spec hrest)
    · cases h

theorem evalAssigns_conforms {env : Env} {as : List (String × Expr)} {us : List (String × Val)}
    (h : evalAssigns env as = some us) :
    ∀ f v, lookup f us = some v → ∃ t, env.m.fieldTy f = some t ∧ t.conforms env.m env.params v = true := by
  intro f v hl
  have hmem := lookup_mem hl
  have hspec := evalAssigns_spec h
  clear h hl
  induction hspec with
  | nil => simp at hmem
  | cons _ hconf _ ih =>
    rcases List.mem_cons.mp hmem with heq | hrest
    · cases heq; exact hconf
    · exact ih hrest

theorem evalAssigns_none_of_bad {env : Env} {f : String} {x : Expr} {v : Val} {t : Ty}
    (hv : eval env x = some v) (ht : env.m.fieldTy f = some t) (hbad : t.conforms env.m env.params v = false) :
    ∀ {as : List (String × Expr)}, (f, x) ∈ as → evalAssigns env as = none
  | [], hmem => by simp at hmem
  | (f', x') :: rest, hmem => by
    simp only [evalAssigns]
    rcases List.mem_cons.mp hmem with heq | hrest
    · cases heq; simp [hv, ht, hbad]
    · cases h1 : eval env x' with
      | none => simp
      | some v' =>
        cases h2 : env.m.fieldTy f' with
        | none => simp
        | some t' =>
          simp only [Option.bind_some]
          split
          · simp [evalAssigns_none_of_bad hv ht hbad hrest]
          · rfl

/-- The unique target of an assignment receives the evaluation of its own
right-hand side when the targets are distinct. -/
theorem lookup_of_assignsMatch {env : Env} {f : String} {x : Expr} :
    ∀ {as : List (String × Expr)} {us : List (String × Val)},
      AssignsMatch env as us →
      (as.map Prod.fst).Nodup → (f, x) ∈ as → ∃ v, eval env x = some v ∧ lookup f us = some v
  | [], [], _, _, hmem => by simp at hmem
  | (f', x') :: rest, (_, v) :: us, .cons hev _ ht, hnd, hmem => by
    simp only [List.map, List.nodup_cons] at hnd
    rcases List.mem_cons.mp hmem with heq | hrest
    · cases heq; exact ⟨v, hev, by simp [lookup]⟩
    · have hne : f ≠ f' := by
        intro h; subst h; exact hnd.1 (List.mem_map.mpr ⟨(f, x), hrest, rfl⟩)
      obtain ⟨w, hw, hl⟩ := lookup_of_assignsMatch ht hnd.2 hrest
      exact ⟨w, hw, by simp [lookup, hne, hl]⟩

theorem lookup_none_of_assignsMatch {env : Env} {f : String} :
    ∀ {as : List (String × Expr)} {us : List (String × Val)},
      AssignsMatch env as us →
      (∀ x, (f, x) ∉ as) → lookup f us = none
  | [], [], _, _ => rfl
  | (f', x') :: rest, (_, v) :: us, .cons _ _ ht, hnot => by
    have hne : f ≠ f' := by intro h; subst h; exact hnot x' List.mem_cons_self
    simp only [lookup, hne, ite_false]
    exact lookup_none_of_assignsMatch ht fun x hx => hnot x (List.mem_cons_of_mem _ hx)

/-! ## Generic theorems

These hold for every machine definition. The ones that need distinct assignment
targets take that as a hypothesis, which `Machine.wellTyped` guarantees
(`Typing.lean`). -/

/-- Unfolds an accepted step. -/
theorem step_accepted {m : Machine} {p : Params} {s s' : State} {e : Event}
    (h : step m p s e = some s') :
    ∃ ev args us, m.findEvent e.name = some ev ∧ bindArgs m p ev.args e.args = some args ∧
      eval ⟨m, p, s, args⟩ ev.guard = some (boolV true) ∧
      evalAssigns ⟨m, p, s, args⟩ ev.assigns = some us ∧ s' = applyUpdates s us := by
  simp only [step, Option.bind_eq_some_iff] at h
  obtain ⟨ev, hev, args, hargs, g, hg, h⟩ := h
  split at h
  · rename_i hgt
    subst hgt
    simp only [Option.map_eq_some_iff] at h
    obtain ⟨us, hus, rfl⟩ := h
    exact ⟨ev, args, us, hev, hargs, hg, hus, rfl⟩
  · cases h

/-- Generic theorem 1: a refused event leaves the state unchanged. -/
theorem run_refused_unchanged (m : Machine) (p : Params) (s : State) (e : Event) {s' : State}
    (h : run m p s e = (s', false)) : s' = s := by
  unfold run at h
  split at h <;> simp_all

/-- `run` reports acceptance exactly when `step` accepts. -/
theorem run_accepted_iff (m : Machine) (p : Params) (s s' : State) (e : Event) :
    run m p s e = (s', true) ↔ step m p s e = some s' := by
  unfold run
  split <;> simp_all

/-- Generic theorem 2: the same state, parameters, and event give one result. -/
theorem step_deterministic {m : Machine} {p : Params} {s a b : State} {e : Event}
    (ha : step m p s e = some a) (hb : step m p s e = some b) : a = b := by
  rw [ha] at hb; cases hb; rfl

theorem run_deterministic (m : Machine) (p : Params) (s : State) (e : Event) {r₁ r₂ : State × Bool}
    (h₁ : run m p s e = r₁) (h₂ : run m p s e = r₂) : r₁ = r₂ := by
  rw [← h₁, ← h₂]

/-- Generic theorem 3a: every assigned field receives the value of its
right-hand side evaluated in the pre-state `s`. -/
theorem step_assign_reads_prestate {m : Machine} {p : Params} {s s' : State} {e : Event}
    {ev : EventDecl} {f : String} {x : Expr}
    (h : step m p s e = some s') (hev : m.findEvent e.name = some ev)
    (hnd : (ev.assigns.map Prod.fst).Nodup) (hmem : (f, x) ∈ ev.assigns) (hf : f ∈ s.map Prod.fst) :
    ∃ args v, bindArgs m p ev.args e.args = some args ∧
      eval ⟨m, p, s, args⟩ x = some v ∧ lookup f s' = some v := by
  obtain ⟨ev', args, us, hev', hargs, _, hus, rfl⟩ := step_accepted h
  rw [hev] at hev'; cases hev'
  obtain ⟨v, hv, hl⟩ := lookup_of_assignsMatch (evalAssigns_spec hus) hnd hmem
  refine ⟨args, v, hargs, hv, ?_⟩
  obtain ⟨w, hw⟩ := lookup_some_of_mem hf
  rw [lookup_applyUpdates, hl, hw]
  rfl

/-- Generic theorem 3b: a field no assignment targets keeps its value. -/
theorem step_unassigned_unchanged {m : Machine} {p : Params} {s s' : State} {e : Event}
    {ev : EventDecl} {f : String}
    (h : step m p s e = some s') (hev : m.findEvent e.name = some ev)
    (hnot : ∀ x, (f, x) ∉ ev.assigns) : lookup f s' = lookup f s := by
  obtain ⟨ev', args, us, hev', _, _, hus, rfl⟩ := step_accepted h
  rw [hev] at hev'; cases hev'
  rw [lookup_applyUpdates, lookup_none_of_assignsMatch (evalAssigns_spec hus) hnot]
  cases lookup f s <;> rfl

/-- Generic theorem 4: an assignment whose value falls outside the declared
type, in particular above its parameter bound, refuses the whole event; the
value is never wrapped or clamped. -/
theorem step_refuses_out_of_bound {m : Machine} {p : Params} {s : State} {e : Event}
    {ev : EventDecl} {args : List (String × Val)} {f : String} {x : Expr} {v : Val} {t : Ty}
    (hev : m.findEvent e.name = some ev) (hargs : bindArgs m p ev.args e.args = some args)
    (hmem : (f, x) ∈ ev.assigns) (hv : eval ⟨m, p, s, args⟩ x = some v)
    (ht : m.fieldTy f = some t) (hbad : t.conforms m p v = false) :
    step m p s e = none := by
  have hnone := evalAssigns_none_of_bad (env := ⟨m, p, s, args⟩) hv ht hbad hmem
  simp only [step, hev, hargs, Option.bind_some]
  cases eval ⟨m, p, s, args⟩ ev.guard with
  | none => rfl
  | some g =>
    simp only [Option.bind_some]
    split
    · simp [hnone]
    · rfl

/-- Accepted steps keep a conforming state conforming: every field stays within
its declared type and bounds. -/
theorem step_preserves_conforms {m : Machine} {p : Params} {s s' : State} {e : Event}
    (hc : m.StateConforms p s) (h : step m p s e = some s') : m.StateConforms p s' := by
  obtain ⟨ev, args, us, _, _, _, hus, rfl⟩ := step_accepted h
  refine ⟨by rw [applyUpdates_names]; exact hc.1, ?_⟩
  intro f v hl
  rw [lookup_applyUpdates] at hl
  cases hs : lookup f s with
  | none => simp [hs] at hl
  | some w =>
    simp only [hs, Option.map_some, Option.some.injEq] at hl
    cases hu : lookup f us with
    | none => simp only [hu, Option.getD_none] at hl; subst hl; exact hc.2 f w hs
    | some u =>
      simp only [hu, Option.getD_some] at hl; subst hl
      exact evalAssigns_conforms hus f u hu

end KanderFormal
