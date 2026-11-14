/-
The state machine definition language.

A machine is a plain Lean value (a deep embedding): enums, natural number
parameters, typed state fields with initial values, events with typed
arguments, a guard and simultaneous assignments, and derived queries. The
semantics lives in `Semantics.lean`, the type checker in `Typing.lean`, and the
Go emitter reads the same value.
-/
namespace KanderFormal

/-- Declared scalar types. A `nat` carries the name of the parameter that is its
inclusive upper bound. -/
inductive STy where
  | bool
  | enum (e : String)
  | nat (bound : String)
  deriving DecidableEq, Repr, Inhabited

/-- Declared field types: a scalar, a fixed-size map indexed by an enum, a
finite set of enum elements, or an array of `p bound` elements for parameter
`bound`. An array element is a scalar or a set (`Machine.TyOk`). -/
inductive Ty where
  | scalar (t : STy)
  | map (key : String) (val : STy)
  | set (elem : String)
  | arr (bound : String) (elem : Ty)
  deriving DecidableEq, Repr, Inhabited

/-- Runtime scalars. An enum value is the index of its constructor. -/
inductive Scalar where
  | bool (b : Bool)
  | enum (i : Nat)
  | nat (n : Nat)
  deriving DecidableEq, Repr, Inhabited

/-- Runtime values. A map holds one scalar per key constructor, in constructor
order; a set holds one membership bit per element constructor; an array holds
one value per index. -/
inductive Val where
  | scalar (s : Scalar)
  | map (vs : List Scalar)
  | set (bs : List Bool)
  | arr (vs : List Val)
  deriving Repr, Inhabited

/- `Val` is nested through `List`, which the `DecidableEq` deriving handler does
not support, so the instance is written out. -/
mutual
def Val.decEq : (a b : Val) → Decidable (a = b)
  | .scalar x, .scalar y => if h : x = y then isTrue (h ▸ rfl) else isFalse (by intro e; cases e; exact h rfl)
  | .map x, .map y => if h : x = y then isTrue (h ▸ rfl) else isFalse (by intro e; cases e; exact h rfl)
  | .set x, .set y => if h : x = y then isTrue (h ▸ rfl) else isFalse (by intro e; cases e; exact h rfl)
  | .arr xs, .arr ys =>
    match Val.decEqList xs ys with
    | isTrue h => isTrue (h ▸ rfl)
    | isFalse h => isFalse (by intro e; cases e; exact h rfl)
  | .scalar _, .map _ | .scalar _, .set _ | .scalar _, .arr _
  | .map _, .scalar _ | .map _, .set _ | .map _, .arr _
  | .set _, .scalar _ | .set _, .map _ | .set _, .arr _
  | .arr _, .scalar _ | .arr _, .map _ | .arr _, .set _ => isFalse (by intro e; cases e)
def Val.decEqList : (a b : List Val) → Decidable (a = b)
  | [], [] => isTrue rfl
  | x :: xs, y :: ys =>
    match Val.decEq x y, Val.decEqList xs ys with
    | isTrue h1, isTrue h2 => isTrue (h1 ▸ h2 ▸ rfl)
    | isFalse h, _ => isFalse (by intro e; cases e; exact h rfl)
    | _, isFalse h => isFalse (by intro e; cases e; exact h rfl)
  | [], _ :: _ | _ :: _, [] => isFalse (by intro e; cases e)
end

instance : DecidableEq Val := Val.decEq

/-- Expression types: declared types with the parameter bounds erased. -/
inductive EBase where
  | bool
  | enum (e : String)
  | nat
  deriving DecidableEq, Repr, Inhabited

inductive ETy where
  | base (b : EBase)
  | map (key : String) (val : EBase)
  | set (elem : String)
  | arr (elem : ETy)
  deriving DecidableEq, Repr, Inhabited

def STy.erase : STy → EBase
  | .bool => .bool
  | .enum e => .enum e
  | .nat _ => .nat

def Ty.erase : Ty → ETy
  | .scalar t => .base t.erase
  | .map k v => .map k v.erase
  | .set e => .set e
  | .arr _ t => .arr t.erase

inductive CmpOp where
  | eq | ne | lt | le | gt | ge
  deriving DecidableEq, Repr, Inhabited

inductive Expr where
  | boolLit (b : Bool)
  | enumLit (e c : String)
  | natLit (n : Nat)
  | field (f : String)
  | param (p : String)
  | arg (a : String)
  | cmp (op : CmpOp) (a b : Expr)
  | add (a b : Expr)
  | sub (a b : Expr)
  | and (a b : Expr)
  | or (a b : Expr)
  | not (a : Expr)
  | ite (c t e : Expr)
  | get (m k : Expr)
  | put (m k v : Expr)
  | mem (s x : Expr)
  | insert (s x : Expr)
  | erase (s x : Expr)
  /-- Array read and functional update; an index outside the array is
  undefined. -/
  | aget (a i : Expr)
  | aput (a i v : Expr)
  /-- The array or map `a` with every entry at index `lo` or above set to `v`. -/
  | afill (a lo v : Expr)
  /-- Whether `body` holds for some `i` with `lo ≤ i < hi`; `bvar 0` is `i`. -/
  | any (lo hi body : Expr)
  /-- The least `i` with `lo ≤ i < hi` for which `body` holds, else `hi`;
  `bvar 0` is `i`. -/
  | first (lo hi body : Expr)
  /-- The set of constructors `c` of enum `e` for which `body` holds;
  `bvar 0` is `c`. -/
  | setOf (e : String) (body : Expr)
  /-- The map from each constructor `c` of enum `k` to `body`; `bvar 0` is `c`. -/
  | mapOf (k : String) (body : Expr)
  /-- The variable bound by the `k`-th enclosing `any`, `first`, `setOf`, or `mapOf`,
  counting from 0 at the innermost. -/
  | bvar (k : Nat)
  deriving Repr, Inhabited

structure EnumDecl where
  name : String
  ctors : List String
  deriving Repr, Inhabited

/-- `init` is the initial value; for an array field it is the value of every
element, since the length depends on the parameters (`initial`). -/
structure FieldDecl where
  name : String
  ty : Ty
  init : Val
  deriving Repr, Inhabited

/-- An event argument is a scalar or a set (`Machine.eventOk`). -/
structure ArgDecl where
  name : String
  ty : Ty
  deriving Repr, Inhabited

structure EventDecl where
  name : String
  args : List ArgDecl
  guard : Expr
  /-- Simultaneous assignments: every right-hand side reads the pre-state. -/
  assigns : List (String × Expr)
  deriving Repr, Inhabited

structure QueryDecl where
  name : String
  ty : ETy
  body : Expr
  deriving Repr, Inhabited

structure Machine where
  name : String
  enums : List EnumDecl
  params : List String
  fields : List FieldDecl
  events : List EventDecl
  queries : List QueryDecl
  deriving Repr, Inhabited

/-- Parameter values. Any function is a valid choice, so a statement quantified
over `Params` holds for every parameter value. -/
abbrev Params := String → Nat

/-- A state maps each field name to its value, in field declaration order. -/
abbrev State := List (String × Val)

/-- An event instance: the event name and its arguments in declaration order. -/
structure Event where
  name : String
  args : List Val
  deriving DecidableEq, Repr, Inhabited

/-- Association list lookup returning the first match. -/
def lookup {α : Type} (k : String) : List (String × α) → Option α
  | [] => none
  | (k', v) :: rest => if k = k' then some v else lookup k rest

def indexOf (c : String) : List String → Option Nat
  | [] => none
  | c' :: rest => if c = c' then some 0 else (indexOf c rest).map (· + 1)

namespace Machine

def ctorsOf (m : Machine) (e : String) : Option (List String) :=
  lookup e (m.enums.map fun d => (d.name, d.ctors))

def enumSize (m : Machine) (e : String) : Option Nat :=
  (m.ctorsOf e).map List.length

def findField (m : Machine) (f : String) : Option FieldDecl :=
  lookup f (m.fields.map fun d => (d.name, d))

def fieldTy (m : Machine) (f : String) : Option Ty :=
  (m.findField f).map FieldDecl.ty

def findEvent (m : Machine) (e : String) : Option EventDecl :=
  lookup e (m.events.map fun d => (d.name, d))

end Machine

def STy.conforms (m : Machine) (p : Params) : STy → Scalar → Bool
  | .bool, .bool _ => true
  | .enum e, .enum i =>
    match m.enumSize e with
    | some n => decide (i < n)
    | none => false
  | .nat b, .nat n => decide (n ≤ p b)
  | _, _ => false

/-- A value conforms to a declared type when it has the right shape, every enum
index is in range, every natural number is within its parameter bound, and every
array has exactly its parameter's length. -/
def Ty.conforms (m : Machine) (p : Params) : Ty → Val → Bool
  | .scalar t, .scalar s => t.conforms m p s
  | .map k t, .map vs => decide (m.enumSize k = some vs.length) && vs.all (t.conforms m p)
  | .set e, .set bs => decide (m.enumSize e = some bs.length)
  | .arr b t, .arr vs => decide (vs.length = p b) && vs.all (Ty.conforms m p t)
  | _, _ => false

/-- A state conforms to a machine when it lists exactly the declared fields in
order and every field value conforms to its declared type. -/
def Machine.StateConforms (m : Machine) (p : Params) (s : State) : Prop :=
  s.map Prod.fst = m.fields.map FieldDecl.name ∧
    ∀ f v, lookup f s = some v → ∃ t, m.fieldTy f = some t ∧ t.conforms m p v = true

end KanderFormal
