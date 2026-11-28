import KanderFormal.Semantics

/-
The start order machine: one request of a host that another host starts a todo
card (`start-orders/<order>.json` of a project on the v2 server,
`internal/server/start_orders.go`).

The state is the order's status `st` and the generation the order is bound to,
`bgen` (0 while no claim took the order). The shell projects every generation
an event compares into the kernel's range: 0 stays 0, the generation of the
operation maps to 1, and any other generation maps to 2, so the kernel only
compares, as the delivery kernel does.

The facts the shell judges are arguments: `target` (the client that acts is the
order's target host), `task` (the claim names the order's task), `due` (the
order waited longer than the acceptance window on the server clock) and `lost`
(the target host sent no heartbeat within the online window). A claim of the
task by another execution while the order is open ends it (`preempt`). Once a
claim bound the order, only the bound generation decides: its confirmation
(`confirm`), its launch rollback (`rollback`), or the end of its claim by a
takeover, a release or a termination (`supersede`). Every status other than
`none`, `pending` and `accepted` is terminal.
-/
namespace KanderFormal.Machines.StartOrder

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def eq (x y : Expr) : Expr := .cmp .eq x y
def ne (x y : Expr) : Expr := .cmp .ne x y
def zero : Expr := .natLit 0

def stIs (c : String) : Expr := eq (f "st") (.enumLit "st" c)
def st (c : String) : Expr := .enumLit "st" c
def unbound : Expr := eq (f "bgen") zero
/-- The order is bound to generation `g`, which is not zero. -/
def boundTo (g : Expr) : Expr := andE (ne (f "bgen") zero) (eq (f "bgen") g)

def genArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxGen") }
def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }

/-- A requester creates the order; it waits for its target host. -/
def createEvent : EventDecl where
  name := "create"
  args := []
  guard := stIs "none"
  assigns := [("st", st "pending")]

/-- The target host takes the order. -/
def acceptEvent : EventDecl where
  name := "accept"
  args := [boolArg "target"]
  guard := andE (stIs "pending") (a "target")
  assigns := [("st", st "accepted")]

/-- The claim of the start the target host runs for the order binds the new
generation. -/
def claimEvent : EventDecl where
  name := "claim"
  args := [boolArg "target", boolArg "task", genArg "gen"]
  guard := andE (stIs "accepted") (andE unbound (andE (a "target") (andE (a "task") (ne (a "gen") zero))))
  assigns := [("bgen", a "gen")]

/-- The bound generation confirmed its start. -/
def confirmEvent : EventDecl where
  name := "confirm"
  args := [genArg "gen"]
  guard := andE (stIs "accepted") (boundTo (a "gen"))
  assigns := [("st", st "started")]

/-- The bound generation rolled its launch back. -/
def rollbackEvent : EventDecl where
  name := "rollback"
  args := [genArg "gen"]
  guard := andE (stIs "accepted") (boundTo (a "gen"))
  assigns := [("st", st "failed")]

/-- The claim of the bound generation ended before its confirmation: a
takeover, a release, or a termination. -/
def supersedeEvent : EventDecl where
  name := "supersede"
  args := [genArg "gen"]
  guard := andE (stIs "accepted") (boundTo (a "gen"))
  assigns := [("st", st "failed")]

/-- The target host reports a failure before any claim. -/
def failEvent : EventDecl where
  name := "fail"
  args := [boolArg "target"]
  guard := andE (stIs "accepted") (andE unbound (a "target"))
  assigns := [("st", st "failed")]

/-- Anybody withdraws an order no claim took yet. -/
def cancelEvent : EventDecl where
  name := "cancel"
  args := []
  guard := orE (stIs "pending") (andE (stIs "accepted") unbound)
  assigns := [("st", st "cancelled")]

/-- No host took the order within the acceptance window. -/
def expireEvent : EventDecl where
  name := "expire"
  args := [boolArg "due"]
  guard := andE (stIs "pending") (a "due")
  assigns := [("st", st "expired")]

/-- The target host took the order and went silent before any claim. -/
def hostLostEvent : EventDecl where
  name := "hostLost"
  args := [boolArg "lost"]
  guard := andE (stIs "accepted") (andE unbound (a "lost"))
  assigns := [("st", st "failed")]

/-- Another execution claimed the task while the order waited for its claim. -/
def preemptEvent : EventDecl where
  name := "preempt"
  args := []
  guard := andE (orE (stIs "pending") (stIs "accepted")) unbound
  assigns := [("st", st "failed")]

/-- The order is not finished: it waits for its host or for its claim's start. -/
def openQuery : QueryDecl := { name := "open", ty := .base .bool, body := orE (stIs "pending") (stIs "accepted") }

/-- The order can be withdrawn. -/
def cancellableQuery : QueryDecl :=
  { name := "cancellable", ty := .base .bool, body := orE (stIs "pending") (andE (stIs "accepted") unbound) }

def startOrderMachine : Machine where
  name := "startorder"
  enums := [{ name := "st", ctors := ["none", "pending", "accepted", "started", "failed", "cancelled", "expired"] }]
  params := ["maxGen"]
  fields := [
    { name := "st", ty := .scalar (.enum "st"), init := .scalar (.enum 0) },
    { name := "bgen", ty := .scalar (.nat "maxGen"), init := .scalar (.nat 0) }
  ]
  events := [createEvent, acceptEvent, claimEvent, confirmEvent, rollbackEvent, supersedeEvent, failEvent,
    cancelEvent, expireEvent, hostLostEvent, preemptEvent]
  queries := [openQuery, cancellableQuery]

end KanderFormal.Machines.StartOrder
