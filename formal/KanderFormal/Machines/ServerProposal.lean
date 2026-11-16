import KanderFormal.Semantics

/-
The server proposal slot machine: the dispatch proposals of one dispatch ID
in one task on the v2 server (`requests/<task>/*.json`, the requests whose
`Dispatch.ID` is that ID; `internal/server/requests.go`, `dispatch_request.go`).

The state is the number of proposals the slot holds, capped by the parameter
`maxCount`: the shell counts the requests of the task that carry the dispatch
ID and projects `min(count, maxCount)`. A creation takes the slot (`admit`)
only when it is empty; a second creation of the same dispatch ID (`repeat`)
finds the first proposal and `recreate` of the request machine decides whether
it is the same one; the formal preparation (`lookup`) needs exactly one
proposal. The slot never holds two proposals when every creation goes through
`admit`: a hand-edited task directory can project a larger count, which the
slot refuses to admit into and to look up, and `repeat` still finds the first
proposal, as `createRequest` always did.
-/
namespace KanderFormal.Machines.ServerProposal

open KanderFormal

def f (n : String) : Expr := .field n
def eq (x y : Expr) : Expr := .cmp .eq x y
def ge (x y : Expr) : Expr := .cmp .ge x y

/-- `createRequest` found no proposal of the dispatch ID: it creates one. -/
def admitEvent : EventDecl where
  name := "admit"
  args := []
  guard := eq (f "count") (.natLit 0)
  assigns := [("count", .natLit 1)]

/-- `createRequest` found a proposal of the dispatch ID: the first one is
returned or refused as a mismatch. -/
def repeatEvent : EventDecl where
  name := "repeat"
  args := []
  guard := ge (f "count") (.natLit 1)
  assigns := []

/-- `inspectDispatchProposal`: the formal preparation needs the one proposal of
the dispatch ID. -/
def lookupEvent : EventDecl where
  name := "lookup"
  args := []
  guard := eq (f "count") (.natLit 1)
  assigns := []

def serverProposalMachine : Machine where
  name := "serverproposal"
  enums := []
  params := ["maxCount"]
  fields := [
    { name := "count", ty := .scalar (.nat "maxCount"), init := .scalar (.nat 0) }
  ]
  events := [admitEvent, repeatEvent, lookupEvent]
  queries := [
    -- The slot holds a proposal.
    { name := "taken", ty := .base .bool, body := ge (f "count") (.natLit 1) },
    -- The slot holds exactly one proposal.
    { name := "single", ty := .base .bool, body := eq (f "count") (.natLit 1) }
  ]

end KanderFormal.Machines.ServerProposal
