import KanderFormal.Semantics

/-
The server claim machine: the claim record of one task on the v2 server
(`internal/server/claims.go`).

The state is whether the card is in `working` or `review` (`held`), the status
of its claim record (`none` when no record exists), its generation, and whether
its lease has not expired (`live`). The server clock is not modeled: a lease
expiry is an environment event (`expire`) with no timing assumption, and
`live` is the comparison of the server clock with `ExpiresAt` that the shell
projects.

A request presents a generation header (`g`) and a bearer token; `tg` is the
generation the token was derived for (the shell maps a token that matches no
generation of the record to a value other than `gen`). A request of an older
execution carries its own generation and token, so `g` and `tg` are both that
older generation. `holder` is `requireHolder`: an active record, the token and
the header of the current generation, and a live lease.
-/
namespace KanderFormal.Machines.ServerClaim

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def ff : Expr := .boolLit false
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y
def inc (x : Expr) : Expr := .add x (.natLit 1)

def statusIs (c : String) : Expr := eq (f "status") (.enumLit "status" c)
def gen : Expr := f "gen"

/-- `requireHolder`. -/
def holder : Expr :=
  andE (statusIs "active") (andE (eq (a "tg") gen) (andE (eq (a "g") gen) (f "live")))

/-- The `CodeLeaseInvalid` answers of `requireHolder`: no active record, or the
token of the current generation with another header or an expired lease. -/
def leaseInvalid : Expr :=
  orE (notE (statusIs "active")) (andE (eq (a "tg") gen) (orE (notE (eq (a "g") gen)) (notE (f "live"))))

def genArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxGen") }
def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def holderArgs : List ArgDecl := [genArg "g", genArg "tg"]

/-- `project.claim`: a card outside `working` and `review` gets the next
generation with a live lease. A previous record needs a fresh execution
identity below the last generation (`fresh`). -/
def claimEvent : EventDecl where
  name := "claim"
  args := [boolArg "fresh"]
  guard := andE (notE (f "held")) (orE (statusIs "none") (a "fresh"))
  assigns := [("held", tt), ("status", .enumLit "status" "active"), ("gen", inc gen), ("live", tt)]

/-- `project.renew`: the holder extends its lease. -/
def renewEvent : EventDecl where
  name := "renew"
  args := holderArgs
  guard := holder
  assigns := [("live", tt)]

/-- Every other write that presents a claim (`requireHolder`, and
`mutationGuard` inside the write's transaction): a document, a move inside
`working` and `review`, a launcher fact, a delivery, a review, or a dispatch
request. A termination (`terminate`) also passes on a lease the server answers
`CodeLeaseInvalid` for. The claim record stays. -/
def writeEvent : EventDecl where
  name := "write"
  args := holderArgs ++ [boolArg "terminate"]
  guard := orE holder (andE (a "terminate") leaseInvalid)
  assigns := []

/-- `project.move` out of `working` or `review`. The holder may move; a
termination (`terminate`, into `archived` or `trash`) also passes on a lease
the server answers `CodeLeaseInvalid` for. A record is released. -/
def leaveEvent : EventDecl where
  name := "leave"
  args := holderArgs ++ [boolArg "terminate"]
  guard := andE (f "held") (orE holder (andE (a "terminate") leaseInvalid))
  assigns := [("held", ff),
    ("status", .ite (statusIs "none") (.enumLit "status" "none") (.enumLit "status" "released"))]

/-- `ExecutionRelease`: the holder gives up a lease whose executor stopped. -/
def releaseEvent : EventDecl where
  name := "release"
  args := holderArgs
  guard := holder
  assigns := [("status", .enumLit "status" "lost"), ("live", ff)]

/-- `ExecutionRollback`: the holder of a failed launcher start returns the card
to `todo` and releases the record. -/
def rollbackEvent : EventDecl where
  name := "rollback"
  args := holderArgs
  guard := holder
  assigns := [("held", ff), ("status", .enumLit "status" "released")]

/-- `project.takeover`: a claimed card gets the next generation for a new
execution. The request names the generation it observed (`expected`) and a
fresh execution identity below the last generation (`fresh`). A valid lease is
never preempted: only its holder may restart its own stopped executor
(`restart`: the same client and a fresh stopped observation). -/
def takeoverEvent : EventDecl where
  name := "takeover"
  args := [genArg "expected", boolArg "fresh", boolArg "restart"] ++ holderArgs
  guard := andE (f "held") (andE (eq (a "expected") gen) (andE (a "fresh")
    (orE (notE (andE (statusIs "active") (f "live"))) (andE (a "restart") holder))))
  assigns := [("status", .enumLit "status" "active"), ("gen", inc gen), ("live", tt)]

/-- The environment: the server clock passes `ExpiresAt`. -/
def expireEvent : EventDecl where
  name := "expire"
  args := []
  guard := f "live"
  assigns := [("live", ff)]

/-- The environment: an earlier binary wrote a pending record of the next
generation and stopped before or after its board move (`moved`). -/
def pendingEvent : EventDecl where
  name := "pending"
  args := [boolArg "moved"]
  guard := notE (f "held")
  assigns := [("held", a "moved"), ("status", .enumLit "status" "pending"), ("gen", inc gen), ("live", ff)]

/-- `reconcileClaims` at startup: a pending record whose move committed for its
owner (`owner`) becomes active with a live lease; any other pending record, and
an active record of a card that left `working` and `review`, is released. -/
def reconcileEvent : EventDecl where
  name := "reconcile"
  args := [boolArg "owner"]
  guard := tt
  assigns := [
    ("status", .ite (andE (statusIs "pending") (andE (f "held") (a "owner"))) (.enumLit "status" "active")
      (.ite (orE (statusIs "pending") (andE (statusIs "active") (notE (f "held"))))
        (.enumLit "status" "released") (f "status"))),
    ("live", orE (f "live") (andE (statusIs "pending") (andE (f "held") (a "owner"))))
  ]

def serverClaimMachine : Machine where
  name := "serverclaim"
  enums := [{ name := "status", ctors := ["none", "pending", "active", "lost", "released"] }]
  params := ["maxGen"]
  fields := [
    { name := "held", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "status", ty := .scalar (.enum "status"), init := .scalar (.enum 0) },
    { name := "gen", ty := .scalar (.nat "maxGen"), init := .scalar (.nat 0) },
    { name := "live", ty := .scalar .bool, init := .scalar (.bool false) }
  ]
  events := [claimEvent, renewEvent, writeEvent, leaveEvent, releaseEvent, rollbackEvent, takeoverEvent,
    expireEvent, pendingEvent, reconcileEvent]
  queries := [
    { name := "valid", ty := .base .bool, body := andE (statusIs "active") (f "live") }
  ]

end KanderFormal.Machines.ServerClaim
