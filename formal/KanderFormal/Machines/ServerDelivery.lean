import KanderFormal.Semantics

/-
The server delivery machine: the evidence record of one commit of a task on
the v2 server (`deliveries/<task>/<commit>.json` and the task's
`deliveries/<task>/latest.json`, `internal/server/deliveries.go`).

The state is the purpose of the commit's record (`none` when no record exists),
the generation and the card revision it binds, and whether the task's latest
delivery index points at this commit's record (`latest`: `none` without an
index, `here`, or `other` for another commit). The claim record and the card
are not modeled: the shell projects their facts as arguments (`cgen` is the
generation of the claim record, `card` the revision of the card). The execution
identity is compared by the shell (`exec`), as are the fields of the record
(`ref`, `remote`, the clock) that `publishDelivery` validates before it asks the
kernel. `holder` is the result of `requireHolder` (the claim kernel's `write`
event); the shell does not reach a delivery event without it.

A publish of a commit that has a record moves the record's previous version
into `delivery-history/<task>/` (`keeps`), and the latest index always points at
the commit that was published last. Another commit's publish moves the index
away (`other`).
-/
namespace KanderFormal.Machines.ServerDelivery

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def andE (x y : Expr) : Expr := .and x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y
def ne (x y : Expr) : Expr := .cmp .ne x y
def zero : Expr := .natLit 0

def recIs (c : String) : Expr := eq (f "rec") (.enumLit "rec" c)
def latestIs (c : String) : Expr := eq (f "latest") (.enumLit "latest" c)

def genArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxGen") }
def revArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxRev") }
def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }

/-- `publishDelivery`: the holder publishes evidence of its own execution and
generation for the card's current revision. A `baseline` purpose records the
commit the task branch started from. The record replaces the commit's earlier
one and the latest index points at it. -/
def publishEvent : EventDecl where
  name := "publish"
  args := [boolArg "holder", boolArg "exec", boolArg "baseline", genArg "gen", genArg "cgen", revArg "rev",
    revArg "card"]
  guard := andE (a "holder") (andE (a "exec") (andE (ne (a "gen") zero) (andE (eq (a "gen") (a "cgen"))
    (andE (ne (a "rev") zero) (eq (a "rev") (a "card"))))))
  assigns := [("rec", .ite (a "baseline") (.enumLit "rec" "baseline") (.enumLit "rec" "delivery")),
    ("rgen", a "gen"), ("rrev", a "rev"), ("latest", .enumLit "latest" "here")]

/-- The environment: the evidence of another commit of the task is published
and becomes the latest. -/
def otherEvent : EventDecl where
  name := "other"
  args := []
  guard := tt
  assigns := [("latest", .enumLit "latest" "other")]

/-- `deliveryGuard`: the move of a task from `working` to `review` accepts only
the commit's evidence of the holder's execution and generation for the card's
current revision, and never a baseline. -/
def guardEvent : EventDecl where
  name := "guard"
  args := [boolArg "holder", boolArg "exec", genArg "cgen", revArg "card"]
  guard := andE (a "holder") (andE (recIs "delivery") (andE (eq (f "rgen") (a "cgen")) (andE (a "exec")
    (eq (f "rrev") (a "card")))))
  assigns := []

/-- The commit has a record, so a publish moves its previous version into the
history. -/
def keepsQuery : QueryDecl := { name := "keeps", ty := .base .bool, body := notE (recIs "none") }

/-- The task's latest delivery is this commit's record. -/
def latestHereQuery : QueryDecl := { name := "isLatest", ty := .base .bool, body := latestIs "here" }

/-- The commit's record is a delivery, not a baseline. -/
def deliveredQuery : QueryDecl := { name := "delivered", ty := .base .bool, body := recIs "delivery" }

def serverDeliveryMachine : Machine where
  name := "serverdelivery"
  enums := [{ name := "rec", ctors := ["none", "delivery", "baseline"] },
    { name := "latest", ctors := ["none", "here", "other"] }]
  params := ["maxGen", "maxRev"]
  fields := [
    { name := "rec", ty := .scalar (.enum "rec"), init := .scalar (.enum 0) },
    { name := "rgen", ty := .scalar (.nat "maxGen"), init := .scalar (.nat 0) },
    { name := "rrev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) },
    { name := "latest", ty := .scalar (.enum "latest"), init := .scalar (.enum 0) }
  ]
  events := [publishEvent, otherEvent, guardEvent]
  queries := [keepsQuery, latestHereQuery, deliveredQuery]

end KanderFormal.Machines.ServerDelivery
