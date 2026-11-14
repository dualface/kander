import KanderFormal.Semantics

/-
The type checker. `Machine.wellTyped` is a decidable check; the Go emitter
refuses a machine that fails it, and every registered machine proves it.
-/
namespace KanderFormal

/-- Nat literals stay below 2^32 so that generated Go arithmetic on `uint64`
never overflows (see docs/formal-verification.md). -/
def natLitLimit : Nat := 4294967296

/-- Enums are encoded as `uint8` and sets as `uint64` bit sets in Go. -/
def enumCtorLimit : Nat := 64

def isLowerAlpha (c : Char) : Bool := 'a' ≤ c && c ≤ 'z'
def isAlnum (c : Char) : Bool := isLowerAlpha c || ('A' ≤ c && c ≤ 'Z') || ('0' ≤ c && c ≤ '9')

/-- DSL names are lowerCamel identifiers; the emitter capitalizes them. -/
def validIdent (s : String) : Bool :=
  match s.toList with
  | [] => false
  | c :: cs => isLowerAlpha c && cs.all isAlnum

def nodupB : List String → Bool
  | [] => true
  | x :: xs => !xs.contains x && nodupB xs

namespace Machine

def hasEnum (m : Machine) (e : String) : Bool := (m.ctorsOf e).isSome

def hasParam (m : Machine) (p : String) : Bool := m.params.contains p

def STyOk (m : Machine) : STy → Bool
  | .bool => true
  | .enum e => m.hasEnum e
  | .nat b => m.hasParam b

def TyOk (m : Machine) : Ty → Bool
  | .scalar t => m.STyOk t
  | .map k v => m.hasEnum k && m.STyOk v
  | .set e => m.hasEnum e

def EBaseOk (m : Machine) : EBase → Bool
  | .enum e => m.hasEnum e
  | _ => true

def ETyOk (m : Machine) : ETy → Bool
  | .base b => m.EBaseOk b
  | .map k v => m.hasEnum k && m.EBaseOk v
  | .set e => m.hasEnum e

end Machine

def tBool : ETy := .base .bool
def tNat : ETy := .base .nat

/-- Infers the type of an expression; `none` for an ill-typed expression.
`args` are the declared arguments of the enclosing event. -/
def infer (m : Machine) (args : List ArgDecl) : Expr → Option ETy
  | .boolLit _ => some tBool
  | .enumLit e c => if ((m.ctorsOf e).getD []).contains c then some (.base (.enum e)) else none
  | .natLit n => if n < natLitLimit then some tNat else none
  | .field f => (m.fieldTy f).map Ty.erase
  | .param x => if m.hasParam x then some tNat else none
  | .arg a => (lookup a (args.map fun d => (d.name, d.ty))).map fun t => .base t.erase
  | .cmp op a b =>
    match infer m args a, infer m args b with
    | some ta, some tb =>
      if ta ≠ tb then none
      else match op with
        | .eq | .ne => some tBool
        | _ => if ta = tNat then some tBool else none
    | _, _ => none
  | .add a b | .sub a b =>
    match infer m args a, infer m args b with
    | some (.base .nat), some (.base .nat) => some tNat
    | _, _ => none
  | .and a b | .or a b =>
    match infer m args a, infer m args b with
    | some (.base .bool), some (.base .bool) => some tBool
    | _, _ => none
  | .not a =>
    match infer m args a with
    | some (.base .bool) => some tBool
    | _ => none
  | .ite c t e =>
    match infer m args c, infer m args t, infer m args e with
    | some (.base .bool), some tt, some te => if tt = te then some tt else none
    | _, _, _ => none
  | .get a k =>
    match infer m args a, infer m args k with
    | some (.map k' v), some (.base (.enum e)) => if k' = e then some (.base v) else none
    | _, _ => none
  | .put a k v =>
    match infer m args a, infer m args k, infer m args v with
    | some (.map k' w), some (.base (.enum e)), some (.base w') =>
      if k' = e ∧ w = w' then some (.map k' w) else none
    | _, _, _ => none
  | .mem s x =>
    match infer m args s, infer m args x with
    | some (.set e), some (.base (.enum e')) => if e = e' then some tBool else none
    | _, _ => none
  | .insert s x | .erase s x =>
    match infer m args s, infer m args x with
    | some (.set e), some (.base (.enum e')) => if e = e' then some (.set e) else none
    | _, _ => none

/-- Parameters fixed at zero: a value that conforms here conforms for every
parameter value (`Ty.conforms_zero`). -/
def zeroParams : Params := fun _ => 0

namespace Machine

def eventOk (m : Machine) (ev : EventDecl) : Bool :=
  validIdent ev.name &&
  nodupB (ev.args.map ArgDecl.name) &&
  ev.args.all (fun a => validIdent a.name && m.STyOk a.ty) &&
  infer m ev.args ev.guard == some tBool &&
  nodupB (ev.assigns.map Prod.fst) &&
  ev.assigns.all fun (f, x) =>
    match m.fieldTy f with
    | some t => infer m ev.args x == some t.erase
    | none => false

def queryOk (m : Machine) (q : QueryDecl) : Bool :=
  validIdent q.name && m.ETyOk q.ty && infer m [] q.body == some q.ty

def wellTyped (m : Machine) : Bool :=
  validIdent m.name &&
  nodupB (m.enums.map EnumDecl.name) &&
  m.enums.all (fun d => validIdent d.name && !d.ctors.isEmpty &&
    decide (d.ctors.length ≤ enumCtorLimit) && nodupB d.ctors && d.ctors.all validIdent) &&
  nodupB m.params && m.params.all validIdent &&
  nodupB (m.fields.map FieldDecl.name) &&
  m.fields.all (fun d => validIdent d.name && m.TyOk d.ty && d.ty.conforms m zeroParams d.init) &&
  nodupB (m.events.map EventDecl.name) && m.events.all m.eventOk &&
  nodupB (m.queries.map QueryDecl.name) && m.queries.all m.queryOk

end Machine

/-! ## Consequences of well-typedness used by the generic theorems -/

theorem nodupB_nodup : ∀ {l : List String}, nodupB l = true → l.Nodup
  | [], _ => List.nodup_nil
  | x :: xs, h => by
    simp only [nodupB, Bool.and_eq_true, Bool.not_eq_true'] at h
    refine List.nodup_cons.mpr ⟨?_, nodupB_nodup h.2⟩
    intro hx
    have := List.contains_iff_mem.mpr hx
    simp_all

theorem lookup_map_mem {α : Type} {key : α → String} {k : String} {d : α} :
    ∀ {l : List α}, lookup k (l.map fun x => (key x, x)) = some d → d ∈ l
  | [], h => by simp [lookup] at h
  | x :: xs, h => by
    simp only [List.map, lookup] at h
    split at h
    · cases h; exact List.mem_cons_self
    · exact List.mem_cons_of_mem _ (lookup_map_mem h)

theorem wellTyped_events {m : Machine} (h : m.wellTyped = true) :
    ∀ ev ∈ m.events, m.eventOk ev = true := by
  simp only [Machine.wellTyped, Bool.and_eq_true, List.all_eq_true] at h
  exact h.1.1.2

/-- Every event of a well-typed machine assigns each field at most once. -/
theorem wellTyped_assigns_nodup {m : Machine} {e : String} {ev : EventDecl}
    (h : m.wellTyped = true) (hev : m.findEvent e = some ev) : (ev.assigns.map Prod.fst).Nodup := by
  have hok := wellTyped_events h ev (lookup_map_mem hev)
  simp only [Machine.eventOk, Bool.and_eq_true] at hok
  exact nodupB_nodup hok.1.2

theorem STy.conforms_zero {m : Machine} {p : Params} :
    ∀ {t : STy} {s : Scalar}, t.conforms m zeroParams s = true → t.conforms m p s = true
  | .bool, .bool _, _ => rfl
  | .enum _, .enum _, h => h
  | .nat _, .nat n, h => by
    simp only [STy.conforms, zeroParams, decide_eq_true_eq, Nat.le_zero] at h ⊢
    omega
  | .bool, .enum _, h | .bool, .nat _, h | .enum _, .bool _, h | .enum _, .nat _, h
  | .nat _, .bool _, h | .nat _, .enum _, h => by simp [STy.conforms] at h

theorem Ty.conforms_zero {m : Machine} {p : Params} :
    ∀ {t : Ty} {v : Val}, t.conforms m zeroParams v = true → t.conforms m p v = true
  | .scalar _, .scalar _, h => STy.conforms_zero h
  | .map _ _, .map vs, h => by
    simp only [Ty.conforms, Bool.and_eq_true, List.all_eq_true] at h ⊢
    exact ⟨h.1, fun x hx => STy.conforms_zero (h.2 x hx)⟩
  | .set _, .set _, h => h
  | .scalar _, .map _, h | .scalar _, .set _, h | .map _ _, .scalar _, h | .map _ _, .set _, h
  | .set _, .scalar _, h | .set _, .map _, h => by simp [Ty.conforms] at h

/-- The initial state of a well-typed machine conforms for every parameter
value. -/
theorem initial_conforms {m : Machine} (h : m.wellTyped = true) (p : Params) :
    m.StateConforms p (initial m) := by
  refine ⟨by simp [initial], ?_⟩
  intro f v hl
  simp only [initial] at hl
  rw [lookup_map f FieldDecl.name FieldDecl.init] at hl
  simp only [Option.map_eq_some_iff] at hl
  obtain ⟨d, hd, rfl⟩ := hl
  refine ⟨d.ty, by simp [Machine.fieldTy, Machine.findField, hd], ?_⟩
  simp only [Machine.wellTyped, Bool.and_eq_true, List.all_eq_true] at h
  have hdok := h.1.1.1.1.2 d (lookup_map_mem hd)
  exact Ty.conforms_zero hdok.2

/-- Generic theorem 3 for well-typed machines: assignments read the pre-state. -/
theorem wellTyped_assign_reads_prestate {m : Machine} {p : Params} {s s' : State} {e : Event}
    {ev : EventDecl} {f : String} {x : Expr} (hwt : m.wellTyped = true)
    (h : step m p s e = some s') (hev : m.findEvent e.name = some ev)
    (hmem : (f, x) ∈ ev.assigns) (hf : f ∈ s.map Prod.fst) :
    ∃ args v, bindArgs m p ev.args e.args = some args ∧
      eval ⟨m, p, s, args⟩ x = some v ∧ lookup f s' = some v :=
  step_assign_reads_prestate h hev (wellTyped_assigns_nodup hwt hev) hmem hf

end KanderFormal
