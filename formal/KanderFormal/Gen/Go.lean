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
  match infer c.m c.args e with
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

/-- Refuses a value outside the declared bound (`Ty.conforms`). -/
def emitBoundCheck (atom : String) : Ty → EmitM Unit
  | .scalar (.nat b) => do
    emitLine ("if " ++ atom ++ " > uint64(p." ++ capitalize b ++ ") {"); emitFail; emitLine "}"
  | .map _ (.nat b) => do
    emitLine ("for _, v := range " ++ atom ++ " {")
    withIndent do
      emitLine ("if v > uint64(p." ++ capitalize b ++ ") {"); emitFail; emitLine "}"
    emitLine "}"
  | _ => pure ()

def runEmit (c : EmitCtx) (k : EmitM α) : Except String (α × Array String) := do
  let (r, st) ← (k.run c).run {}
  return (r, st.lines)

/-! ## Declarations -/

def setEnums (m : Machine) : List String :=
  let fromFields := m.fields.filterMap fun d => match d.ty with | .set e => some e | _ => none
  let fromQueries := m.queries.filterMap fun q => match q.ty with | .set e => some e | _ => none
  m.enums.map EnumDecl.name |>.filter fun e => fromFields.contains e || fromQueries.contains e

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
     structLines (ev.args.map fun a => (capitalize a.name, goBase a.ty.erase)) ++ ["}"]) ++
  ["", "func (" ++ n ++ ") isEvent() {}", ""]

def argCheck (a : ArgDecl) : EmitM Unit := do
  let r := "ev." ++ capitalize a.name
  match a.ty with
  | .enum e => do emitLine ("if " ++ r ++ " >= " ++ goCount e ++ " {"); emitFail; emitLine "}"
  | .nat b => do emitLine ("if " ++ r ++ " > uint64(p." ++ capitalize b ++ ") {"); emitFail; emitLine "}"
  | .bool => pure ()

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
      | some t => emitBoundCheck a t
      | none => throw s!"unknown field {f}"
      results := results.push (f, a)
    emitLine "next := s"
    for (f, a) in results do
      emitLine ("next." ++ capitalize f ++ " = " ++ a)
    emitLine "return next, true"
  return ["func step" ++ n ++ "(s State, ev " ++ n ++ ", p Params) (State, bool) {"] ++ body.toList ++ ["}", ""]

/-- A query whose body can be undefined returns `(value, ok)`. -/
partial def mayFail : Expr → Bool
  | .sub _ _ => true
  | .cmp _ a b | .add a b | .and a b | .or a b | .get a b | .mem a b | .insert a b | .erase a b =>
    mayFail a || mayFail b
  | .not a => mayFail a
  | .ite a b c | .put a b c => mayFail a || mayFail b || mayFail c
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
    m.enums.flatMap enumDecl ++ (setEnums m).flatMap setDecl ++
    ["// Params holds the machine parameters.", "type Params struct {"] ++
    structLines (m.params.map fun x => (capitalize x, "uint32")) ++ ["}", "",
    "// State holds the machine state.", "type State struct {"] ++
    structLines (m.fields.map fun d => (capitalize d.name, goType d.ty.erase)) ++ ["}", "",
    "// Event is one of the event types of this package.", "type Event interface {", "\tisEvent()", "}", ""] ++
    m.events.flatMap eventStruct ++
    ["// Initial returns the initial state.", "func Initial(p Params) State {", "\tvar s State"] ++
    m.fields.map (fun d => "\ts." ++ capitalize d.name ++ " = " ++ goValLit m d.ty.erase d.init) ++
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
