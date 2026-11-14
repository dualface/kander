import KanderFormal.Typing

/-
The Go emitter for the decision kernel (`zz_generated.go`).

Every compound expression is bound to a fresh temporary `tN`, so each emitted
statement has one operator and gofmt has nothing to rewrite. Natural numbers are
`uint64` inside the kernel and `uint32` in `Params`; since literals stay below
2^32 and there is no multiplication, no expression can overflow `uint64`.
-/
namespace KanderFormal.Gen

def capitalize (s : String) : String :=
  match s.toList with
  | [] => ""
  | c :: cs => String.ofList (c.toUpper :: cs)

def goEnum (e : String) : String := capitalize e
def goCtor (e c : String) : String := goEnum e ++ capitalize c
def goCount (e : String) : String := goEnum e ++ "Count"
def goSet (e : String) : String := goEnum e ++ "Set"

def goBase : EBase → String
  | .bool => "bool"
  | .enum e => goEnum e
  | .nat => "uint64"

def goType : ETy → String
  | .base b => goBase b
  | .map k v => "[" ++ goCount k ++ "]" ++ goBase v
  | .set e => goSet e
  | .arr t => "[]" ++ goType t

def tab (n : Nat) : String := String.ofList (List.replicate n '\t')

def pad (s : String) (w : Nat) : String := s ++ String.ofList (List.replicate (w - s.length) ' ')

/-- A struct body with gofmt's column alignment of field types. -/
def structLines (fields : List (String × String)) : List String :=
  let w := fields.foldl (fun acc (n, _) => max acc n.length) 0
  fields.map fun (n, t) => "\t" ++ pad n (w + 1) ++ t

def goScalarLit (m : Machine) : EBase → Scalar → String
  | _, .bool b => if b then "true" else "false"
  | .enum e, .enum i => goCtor e (((m.ctorsOf e).getD [])[i]?.getD "")
  | _, .enum i => toString i
  | _, .nat n => toString n

def goValLit (m : Machine) : ETy → Val → String
  | .base t, .scalar s => goScalarLit m t s
  | .map k t, .map vs =>
    "[" ++ goCount k ++ "]" ++ goBase t ++ "{" ++ ", ".intercalate (vs.map (goScalarLit m t)) ++ "}"
  | .set _, .set bs =>
    toString ((List.range bs.length).zip bs |>.foldl (fun acc (i, b) => if b then acc + 2 ^ i else acc) 0)
  | _, _ => "0"

/-! ## Expression emission -/

structure EmitCtx where
  m : Machine
  args : List ArgDecl
  /-- Go names and types of the bound variables in scope, innermost first. -/
  bvars : List (String × EBase) := []
  /-- How argument `a` is read, e.g. `ev.A`. -/
  argRef : String → String
  /-- The statement that refuses: `return s, false` in a step function. -/
  fail : String

structure EmitState where
  lines : Array String := #[]
  indent : Nat := 1
  next : Nat := 0

abbrev EmitM := ReaderT EmitCtx (StateT EmitState (Except String))

def emitLine (s : String) : EmitM Unit :=
  modify fun st => { st with lines := st.lines.push (tab st.indent ++ s) }

def withIndent (k : EmitM α) : EmitM α := do
  modify fun st => { st with indent := st.indent + 1 }
  let r ← k
  modify fun st => { st with indent := st.indent - 1 }
  return r

def fresh : EmitM String := do
  let st ← get
  set { st with next := st.next + 1 }
  return "t" ++ toString st.next

def emitFail : EmitM Unit := do
  withIndent (emitLine (← read).fail)

def bind (rhs : String) : EmitM String := do
  let t ← fresh
  emitLine (t ++ " := " ++ rhs)
  return t

def cmpOp : CmpOp → String
  | .eq => "==" | .ne => "!=" | .lt => "<" | .le => "<=" | .gt => ">" | .ge => ">="

def typeOf (e : Expr) : EmitM ETy := do
  let c ← read
  match infer c.m c.args (c.bvars.map Prod.snd) e with
  | some t => return t
  | none => throw s!"ill-typed expression {repr e}"

/-- Emits the statements that compute `e` and returns a Go atom holding it. -/
partial def emitExpr (e : Expr) : EmitM String := do
  let c ← read
  match e with
  | .boolLit b => return if b then "true" else "false"
  | .enumLit en ct => return goCtor en ct
  | .natLit n => return "uint64(" ++ toString n ++ ")"
  | .field f => return "s." ++ capitalize f
  | .param x => return "uint64(p." ++ capitalize x ++ ")"
  | .arg a => return c.argRef a
  | .cmp op a b => do
    let x ← emitExpr a; let y ← emitExpr b
    bind (x ++ " " ++ cmpOp op ++ " " ++ y)
  | .add a b => do
    let x ← emitExpr a; let y ← emitExpr b
    bind (x ++ " + " ++ y)
  | .sub a b => do
    let x ← emitExpr a; let y ← emitExpr b
    emitLine ("if " ++ x ++ " < " ++ y ++ " {"); emitFail; emitLine "}"
    bind (x ++ " - " ++ y)
  | .and a b => do
    let x ← emitExpr a; let y ← emitExpr b
    bind (x ++ " && " ++ y)
  | .or a b => do
    let x ← emitExpr a; let y ← emitExpr b
    bind (x ++ " || " ++ y)
  | .not a => do bind ("!" ++ (← emitExpr a))
  | .ite cnd th el => do
    let ty ← typeOf e
    let x ← emitExpr cnd
    let t ← fresh
    emitLine ("var " ++ t ++ " " ++ goType ty)
    emitLine ("if " ++ x ++ " {")
    withIndent do
      let y ← emitExpr th
      emitLine (t ++ " = " ++ y)
    emitLine "} else {"
    withIndent do
      let z ← emitExpr el
      emitLine (t ++ " = " ++ z)
    emitLine "}"
    return t
  | .get a k => do
    let x ← emitExpr a; let y ← emitExpr k
    bind (x ++ "[" ++ y ++ "]")
  | .put a k v => do
    let x ← emitExpr a; let y ← emitExpr k; let z ← emitExpr v
    let t ← bind x
    emitLine (t ++ "[" ++ y ++ "] = " ++ z)
    return t
  | .mem s x => do
    let a ← emitExpr s; let b ← emitExpr x
    bind (a ++ ".Has(" ++ b ++ ")")
  | .insert s x => do
    let a ← emitExpr s; let b ← emitExpr x
    bind (a ++ ".With(" ++ b ++ ")")
  | .erase s x => do
    let a ← emitExpr s; let b ← emitExpr x
    bind (a ++ ".Without(" ++ b ++ ")")
  | .aget a i => do
    let x ← emitExpr a; let y ← emitExpr i
    emitLine ("if " ++ y ++ " >= uint64(len(" ++ x ++ ")) {"); emitFail; emitLine "}"
    bind (x ++ "[" ++ y ++ "]")
  | .aput a i v => do
    let x ← emitExpr a; let y ← emitExpr i; let z ← emitExpr v
    emitLine ("if " ++ y ++ " >= uint64(len(" ++ x ++ ")) {"); emitFail; emitLine "}"
    let t ← bind ("slices.Clone(" ++ x ++ ")")
    emitLine (t ++ "[" ++ y ++ "] = " ++ z)
    return t
  | .afill a lo v => do
    let ty ← typeOf a
    let x ← emitExpr a; let y ← emitExpr lo; let z ← emitExpr v
    let t ← match ty with
      | .arr _ => bind ("slices.Clone(" ++ x ++ ")")
      | _ => bind x
    let i ← fresh
    emitLine ("for " ++ i ++ " := range " ++ t ++ " {")
    withIndent do
      emitLine ("if uint64(" ++ i ++ ") >= " ++ y ++ " {")
      withIndent (emitLine (t ++ "[" ++ i ++ "] = " ++ z))
      emitLine "}"
    emitLine "}"
    return t
  | .any lo hi body => do
    let l ← emitExpr lo; let h ← emitExpr hi
    let t ← bind "false"
    let i ← fresh
    emitLine ("for " ++ i ++ " := " ++ l ++ "; " ++ i ++ " < " ++ h ++ "; " ++ i ++ "++ {")
    withIndent do
      let b ← withReader (fun c => { c with bvars := (i, .nat) :: c.bvars }) (emitExpr body)
      emitLine (t ++ " = " ++ t ++ " || " ++ b)
    emitLine "}"
    return t
  | .first lo hi body => do
    let l ← emitExpr lo; let h ← emitExpr hi
    let t ← bind h
    let i ← fresh
    emitLine ("for " ++ i ++ " := " ++ l ++ "; " ++ i ++ " < " ++ h ++ "; " ++ i ++ "++ {")
    withIndent do
      let b ← withReader (fun c => { c with bvars := (i, .nat) :: c.bvars }) (emitExpr body)
      emitLine ("if " ++ b ++ " && " ++ t ++ " == " ++ h ++ " {")
      withIndent (emitLine (t ++ " = " ++ i))
      emitLine "}"
    emitLine "}"
    return t
  | .setOf en body => do
    let t ← fresh
    emitLine ("var " ++ t ++ " " ++ goSet en)
    let i ← fresh
    emitLine ("for " ++ i ++ " := " ++ goEnum en ++ "(0); " ++ i ++ " < " ++ goCount en ++ "; " ++ i ++ "++ {")
    withIndent do
      let b ← withReader (fun c => { c with bvars := (i, .enum en) :: c.bvars }) (emitExpr body)
      emitLine ("if " ++ b ++ " {")
      withIndent (emitLine (t ++ " = " ++ t ++ ".With(" ++ i ++ ")"))
      emitLine "}"
    emitLine "}"
    return t
  | .mapOf k body => do
    let ty ← typeOf e
    let t ← fresh
    emitLine ("var " ++ t ++ " " ++ goType ty)
    let i ← fresh
    emitLine ("for " ++ i ++ " := " ++ goEnum k ++ "(0); " ++ i ++ " < " ++ goCount k ++ "; " ++ i ++ "++ {")
    withIndent do
      let b ← withReader (fun c => { c with bvars := (i, .enum k) :: c.bvars }) (emitExpr body)
      emitLine (t ++ "[" ++ i ++ "] = " ++ b)
    emitLine "}"
    return t
  | .bvar k => do
    match c.bvars[k]? with
    | some (n, _) => return n
    | none => throw s!"unbound variable {k}"

/-- Refuses a value outside the declared bound (`Ty.conforms`). -/
def emitBoundCheck (atom : String) : Ty → EmitM Unit
  | .scalar (.nat b) => do
    emitLine ("if " ++ atom ++ " > uint64(p." ++ capitalize b ++ ") {"); emitFail; emitLine "}"
  | .map _ (.nat b) | .arr _ (.scalar (.nat b)) => do
    emitLine ("for _, v := range " ++ atom ++ " {")
    withIndent do
      emitLine ("if v > uint64(p." ++ capitalize b ++ ") {"); emitFail; emitLine "}"
    emitLine "}"
  | _ => pure ()

/-- Refuses an array whose length is not its parameter. -/
def emitLengthCheck (atom : String) : Ty → EmitM Unit
  | .arr b _ => do
    emitLine ("if uint64(len(" ++ atom ++ ")) != uint64(p." ++ capitalize b ++ ") {"); emitFail; emitLine "}"
  | _ => pure ()

def runEmit (c : EmitCtx) (k : EmitM α) : Except String (α × Array String) := do
  let (r, st) ← (k.run c).run {}
  return (r, st.lines)

/-! ## Declarations -/

/-- The enums of the sets a type mentions. -/
def tySets : Ty → List String
  | .set e => [e]
  | .arr _ t => tySets t
  | _ => []

def eTySets : ETy → List String
  | .set e => [e]
  | .arr t => eTySets t
  | _ => []

/-- The enums of the set comprehensions in an expression. -/
partial def exprSets : Expr → List String
  | .setOf e body => e :: exprSets body
  | .mapOf _ body => exprSets body
  | .cmp _ a b | .add a b | .sub a b | .and a b | .or a b | .get a b | .mem a b | .insert a b
  | .erase a b | .aget a b => exprSets a ++ exprSets b
  | .not a => exprSets a
  | .ite a b c | .put a b c | .aput a b c | .afill a b c | .any a b c | .first a b c =>
    exprSets a ++ exprSets b ++ exprSets c
  | _ => []

def machineExprs (m : Machine) : List Expr :=
  m.events.flatMap (fun ev => ev.guard :: ev.assigns.map Prod.snd) ++ m.queries.map QueryDecl.body

def setEnums (m : Machine) : List String :=
  let used := m.fields.flatMap (tySets ·.ty) ++ m.queries.flatMap (eTySets ·.ty) ++
    m.events.flatMap (fun ev => ev.args.flatMap (tySets ·.ty)) ++ (machineExprs m).flatMap exprSets
  m.enums.map EnumDecl.name |>.filter used.contains

/-- Whether the generated kernel clones arrays. -/
partial def usesClone : Expr → Bool
  | .aput _ _ _ | .afill _ _ _ => true
  | .cmp _ a b | .add a b | .sub a b | .and a b | .or a b | .get a b | .mem a b | .insert a b
  | .erase a b | .aget a b => usesClone a || usesClone b
  | .not a | .setOf _ a | .mapOf _ a => usesClone a
  | .ite a b c | .put a b c | .any a b c | .first a b c => usesClone a || usesClone b || usesClone c
  | _ => false

def enumDecl (d : EnumDecl) : List String :=
  let t := goEnum d.name
  ["// " ++ t ++ " enumerates the " ++ d.name ++ " values.", "type " ++ t ++ " uint8", "",
    "// " ++ t ++ " values.", "const ("] ++
  ((List.range d.ctors.length).zip d.ctors |>.map fun (i, c) =>
    "\t" ++ goCtor d.name c ++ (if i = 0 then " " ++ t ++ " = iota" else "")) ++
  [")", "", "// " ++ goCount d.name ++ " is the number of " ++ t ++ " values.",
    "const " ++ goCount d.name ++ " = " ++ toString d.ctors.length, ""]

def setDecl (e : String) : List String :=
  let s := goSet e
  let t := goEnum e
  ["// " ++ s ++ " is a set of " ++ t ++ " values, one bit per value.", "type " ++ s ++ " uint64", "",
    "// Has reports whether v is in the set.", "func (s " ++ s ++ ") Has(v " ++ t ++ ") bool {",
    "\treturn s&(1<<v) != 0", "}", "",
    "// With returns the set with v added.", "func (s " ++ s ++ ") With(v " ++ t ++ ") " ++ s ++ " {",
    "\treturn s | 1<<v", "}", "",
    "// Without returns the set with v removed.", "func (s " ++ s ++ ") Without(v " ++ t ++ ") " ++ s ++ " {",
    "\treturn s &^ (1 << v)", "}", ""]

def eventStruct (ev : EventDecl) : List String :=
  let n := capitalize ev.name
  ["// " ++ n ++ " is the " ++ ev.name ++ " event."] ++
  (if ev.args.isEmpty then ["type " ++ n ++ " struct{}"]
   else ["type " ++ n ++ " struct {"] ++
     structLines (ev.args.map fun a => (capitalize a.name, goType a.ty.erase)) ++ ["}"]) ++
  ["", "func (" ++ n ++ ") isEvent() {}", ""]

def argCheck (a : ArgDecl) : EmitM Unit := do
  let r := "ev." ++ capitalize a.name
  match a.ty with
  | .scalar (.enum e) => do emitLine ("if " ++ r ++ " >= " ++ goCount e ++ " {"); emitFail; emitLine "}"
  | .scalar (.nat b) => do emitLine ("if " ++ r ++ " > uint64(p." ++ capitalize b ++ ") {"); emitFail; emitLine "}"
  | .set e => do emitLine ("if " ++ r ++ ">>" ++ goCount e ++ " != 0 {"); emitFail; emitLine "}"
  | _ => pure ()

def stepFunc (m : Machine) (ev : EventDecl) : Except String (List String) := do
  let n := capitalize ev.name
  let c : EmitCtx := { m, args := ev.args, argRef := fun a => "ev." ++ capitalize a, fail := "return s, false" }
  let ((), body) ← runEmit c do
    for a in ev.args do argCheck a
    let g ← emitExpr ev.guard
    emitLine ("if !" ++ g ++ " {"); emitFail; emitLine "}"
    let mut results : Array (String × String) := #[]
    for (f, x) in ev.assigns do
      let a ← emitExpr x
      match m.fieldTy f with
      | some t => do emitLengthCheck a t; emitBoundCheck a t
      | none => throw s!"unknown field {f}"
      results := results.push (f, a)
    emitLine "next := s"
    for (f, a) in results do
      emitLine ("next." ++ capitalize f ++ " = " ++ a)
    emitLine "return next, true"
  return ["func step" ++ n ++ "(s State, ev " ++ n ++ ", p Params) (State, bool) {"] ++ body.toList ++ ["}", ""]

/-- A query whose body can be undefined returns `(value, ok)`. -/
partial def mayFail : Expr → Bool
  | .sub _ _ | .aget _ _ | .aput _ _ _ => true
  | .cmp _ a b | .add a b | .and a b | .or a b | .get a b | .mem a b | .insert a b | .erase a b =>
    mayFail a || mayFail b
  | .not a | .setOf _ a | .mapOf _ a => mayFail a
  | .ite a b c | .put a b c | .afill a b c | .any a b c | .first a b c => mayFail a || mayFail b || mayFail c
  | _ => false

def queryFunc (m : Machine) (q : QueryDecl) : Except String (List String) := do
  let n := capitalize q.name
  let fails := mayFail q.body
  let c : EmitCtx := { m, args := [], argRef := id, fail := "return result, false" }
  let (r, body) ← runEmit c (emitExpr q.body)
  let sig := if fails then "(result " ++ goType q.ty ++ ", ok bool)" else goType q.ty
  let doc := if fails then
      ["// " ++ n ++ " evaluates the " ++ q.name ++ " query; ok is false when it is undefined."]
    else ["// " ++ n ++ " evaluates the " ++ q.name ++ " query."]
  return doc ++ ["func " ++ n ++ "(s State, p Params) " ++ sig ++ " {"] ++ body.toList ++
    [tab 1 ++ "return " ++ r ++ (if fails then ", true" else ""), "}", ""]

/-- Every exported identifier must be distinct. -/
def checkNames (m : Machine) : Except String Unit := do
  let names := ["Params", "State", "Event", "Initial", "Step"] ++
    m.enums.flatMap (fun d => goEnum d.name :: goCount d.name :: d.ctors.map (goCtor d.name)) ++
    (setEnums m).map goSet ++ m.events.map (capitalize ·.name) ++ m.queries.map (capitalize ·.name)
  if !nodupB names then throw s!"generated Go identifiers collide in {m.name}: {names}"
  if m.events.isEmpty then throw s!"machine {m.name} declares no event"

def isZeroVal : Val → Bool
  | .scalar (.bool b) => !b
  | .scalar (.enum i) | .scalar (.nat i) => i == 0
  | .set bs => bs.all (!·)
  | _ => false

/-- The statements of `Initial` that set field `d`. -/
def initLines (m : Machine) (d : FieldDecl) : List String :=
  let f := "s." ++ capitalize d.name
  match d.ty with
  | .arr b t =>
    ["\t" ++ f ++ " = make(" ++ goType (.arr t.erase) ++ ", p." ++ capitalize b ++ ")"] ++
    (if isZeroVal d.init then [] else
      ["\tfor i := range " ++ f ++ " {", "\t\t" ++ f ++ "[i] = " ++ goValLit m t.erase d.init, "\t}"])
  | t => ["\t" ++ f ++ " = " ++ goValLit m t.erase d.init]

def header (sourcePath digest : String) : List String :=
  ["// Code generated by kander-formal; DO NOT EDIT.",
    "// Source: formal/" ++ sourcePath,
    "// Source SHA-256: " ++ digest, ""]

def kernelFile (m : Machine) (sourcePath digest : String) : Except String String := do
  if !m.wellTyped then throw s!"machine {m.name} is not well typed"
  checkNames m
  let steps ← m.events.mapM (stepFunc m)
  let queries ← m.queries.mapM (queryFunc m)
  let lines := header sourcePath digest ++
    ["// Package " ++ m.name ++ " is the decision kernel of the " ++ m.name ++ " state machine,",
      "// generated from its Lean definition.",
      "package " ++ m.name, ""] ++
    (if (machineExprs m).any usesClone then ["import \"slices\"", ""] else []) ++
    m.enums.flatMap enumDecl ++ (setEnums m).flatMap setDecl ++
    ["// Params holds the machine parameters.", "type Params struct {"] ++
    structLines (m.params.map fun x => (capitalize x, "uint32")) ++ ["}", "",
    "// State holds the machine state.", "type State struct {"] ++
    structLines (m.fields.map fun d => (capitalize d.name, goType d.ty.erase)) ++ ["}", "",
    "// Event is one of the event types of this package.", "type Event interface {", "\tisEvent()", "}", ""] ++
    m.events.flatMap eventStruct ++
    ["// Initial returns the initial state.", "func Initial(p Params) State {", "\tvar s State"] ++
    m.fields.flatMap (initLines m) ++
    ["\treturn s", "}", "",
      "// Step applies e to s. A refused event returns s unchanged and false.",
      "func Step(s State, e Event, p Params) (State, bool) {", "\tswitch ev := e.(type) {"] ++
    m.events.flatMap (fun ev => let n := capitalize ev.name
      ["\tcase " ++ n ++ ":", "\t\treturn step" ++ n ++ "(s, ev, p)"]) ++
    ["\t}", "\treturn s, false", "}", ""] ++
    steps.flatten ++ queries.flatten
  -- Drop the trailing blank line so the file ends with exactly one newline.
  return "\n".intercalate (lines.dropLast) ++ "\n"

end KanderFormal.Gen
