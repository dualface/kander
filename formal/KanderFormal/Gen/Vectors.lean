import KanderFormal.Registry
import KanderFormal.Gen.Json

/-
Differential vectors: the Lean semantics evaluated on every state reachable
within `depth - 1` events, for every event of the alphabet. The Go replay walks
every event sequence of length `depth` and looks up each step here, so every
step of every sequence is checked against Lean.

File format (JSON Lines): a header line
  {"machine":..,"depth":..,"params":[..],"events":[..]}
then one row per (parameter set, state, event):
  {"params":i,"state":..,"event":j,"accepted":..,"next":..,"queries":..}
`queries` holds the query results of `state`; `next` equals `state` when the
event is refused.
-/
namespace KanderFormal.Gen

open KanderFormal.Registry

def scalarDomain (m : Machine) (natDomain : List Nat) : STy → List Val
  | .bool => [boolV false, boolV true]
  | .enum e => (List.range ((m.enumSize e).getD 0)).map enumV
  | .nat _ => natDomain.map natV

def argTuples (m : Machine) (natDomain : List Nat) : List ArgDecl → List (List Val)
  | [] => [[]]
  | d :: ds =>
    (scalarDomain m natDomain d.ty).flatMap fun v => (argTuples m natDomain ds).map (v :: ·)

/-- Every event the replay tries, in event then argument order. -/
def alphabet (m : Machine) (natDomain : List Nat) : List Event :=
  m.events.flatMap fun ev => (argTuples m natDomain ev.args).map fun args => ⟨ev.name, args⟩

def paramsOf (ps : List (String × Nat)) : Params := fun x => (lookup x ps).getD 0

/-- States reachable within `n` events, in breadth-first discovery order. -/
def reachableWithin (m : Machine) (p : Params) (events : List Event) (n : Nat) : Array State := Id.run do
  let mut seen : Array State := #[initial m]
  let mut frontier : Array State := #[initial m]
  for _ in [0:n] do
    let mut next : Array State := #[]
    for s in frontier do
      for e in events do
        if let some s' := step m p s e then
          if !seen.contains s' then
            seen := seen.push s'
            next := next.push s'
    frontier := next
  return seen

def vectorLines (en : Entry) : List String := Id.run do
  let m := en.machine
  let b := en.vectors
  let events := alphabet m b.natDomain
  let header := jsonObj [("machine", jsonStr m.name), ("depth", toString b.depth),
    ("params", jsonArr (b.paramSets.map fun ps => encodeParams m (paramsOf ps))),
    ("events", jsonArr (events.map (encodeEvent m)))]
  let mut lines := #[header]
  for (pi, ps) in (List.range b.paramSets.length).zip b.paramSets do
    let p := paramsOf ps
    for s in reachableWithin m p events (b.depth - 1) do
      for (ei, e) in (List.range events.length).zip events do
        let (s', ok) := run m p s e
        lines := lines.push (jsonObj [("params", toString pi), ("state", encodeState m s),
          ("event", toString ei), ("accepted", if ok then "true" else "false"),
          ("next", encodeState m s'), ("queries", encodeQueries m p s)])
  return lines.toList

end KanderFormal.Gen
