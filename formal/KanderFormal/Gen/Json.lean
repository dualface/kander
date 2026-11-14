import KanderFormal.Semantics

/-
Canonical JSON encoding of parameters, states, events, and query results. The
generated Go test encodes its own values the same way and compares the bytes,
so the format is fixed: no whitespace, object keys in declaration order, enum
values as constructor names, sets as arrays in constructor order.
-/
namespace KanderFormal.Gen

def jsonStr (s : String) : String := "\"" ++ s ++ "\""

def jsonObj (kvs : List (String × String)) : String :=
  "{" ++ ",".intercalate (kvs.map fun (k, v) => jsonStr k ++ ":" ++ v) ++ "}"

def jsonArr (xs : List String) : String := "[" ++ ",".intercalate xs ++ "]"

def ctorName (m : Machine) (e : String) (i : Nat) : String :=
  ((m.ctorsOf e).getD [])[i]?.getD s!"?{i}"

def encodeScalar (m : Machine) : EBase → Scalar → String
  | _, .bool b => if b then "true" else "false"
  | .enum e, .enum i => jsonStr (ctorName m e i)
  | _, .enum i => toString i
  | _, .nat n => toString n

def encodeVal (m : Machine) : ETy → Val → String
  | .base t, .scalar s => encodeScalar m t s
  | .map k t, .map vs =>
    jsonObj ((List.range vs.length).zip vs |>.map fun (i, x) => (ctorName m k i, encodeScalar m t x))
  | .set e, .set bs =>
    jsonArr ((List.range bs.length).zip bs |>.filterMap fun (i, b) => if b then some (jsonStr (ctorName m e i)) else none)
  | _, _ => "null"

def encodeState (m : Machine) (s : State) : String :=
  jsonObj (m.fields.map fun d => (d.name, ((lookup d.name s).map (encodeVal m d.ty.erase)).getD "null"))

def encodeEvent (m : Machine) (e : Event) : String :=
  let args := match m.findEvent e.name with
    | some ev => (ev.args.zip e.args).map fun (d, v) => (d.name, encodeVal m (.base d.ty.erase) v)
    | none => []
  jsonObj [("event", jsonStr e.name), ("args", jsonObj args)]

def encodeParams (m : Machine) (p : Params) : String :=
  jsonObj (m.params.map fun x => (x, toString (p x)))

def encodeQueries (m : Machine) (p : Params) (s : State) : String :=
  jsonObj (m.queries.map fun q => (q.name, ((evalQuery m p s q).map (encodeVal m q.ty)).getD "null"))

end KanderFormal.Gen
