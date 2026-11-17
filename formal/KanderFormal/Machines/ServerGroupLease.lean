import KanderFormal.Semantics

/-
The server group lease machine: the coordination lease of one task group on the
v2 server (`group-claims/<group>.json`, `internal/server/group_leases.go`,
`group_observation.go`, `groups.go`, `group_recovery.go`).

The state is the status of the lease record (`none` when no record exists),
its generation, and whether its lease has not expired (`live`). The server
clock is not modeled: a lease expiry is an environment event (`expire`) and
`live` is the comparison of the server clock with `ExpiresAt` that the shell
projects. The execution identities, the client, the token hash, the handoff
history, and the observation are not modeled: the shell passes the results of
comparing them as Boolean arguments (`sameExec`, `sameClient`).

A request presents a generation header (`g`) and a bearer token; `tg` is the
generation the token was derived for, and `hdr` says that the claim headers
parsed (`holder.invalid` is empty). The shell maps a header or token that
matches no generation of the record to a value other than `gen`, as it does for
the single card claim (`ServerClaim.lean`). `holder` is `requireGroup`: an
active record and the token and the header of the current generation. As for
the single card claim, the lease clock only decides whether another execution
may take the group over; `lapsed` is a view value that is never stored.

A group has no card, so the machine has no `held` field. The member cards of a
group write and recover through their own claim kernels
(`ServerClaim.lean`); the group events take the result of those kernels as the
Boolean arguments `members` and `member`.
-/
namespace KanderFormal.Machines.ServerGroupLease

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

/-- The lease of a record: active and not expired. -/
def valid : Expr := andE (statusIs "active") (f "live")

/-- `requireGroup` accepts: the headers parsed, the record is active, and the
generation header and the token belong to its generation. The lease clock is
not part of it. -/
def holder : Expr :=
  andE (a "hdr") (andE (statusIs "active") (andE (eq (a "tg") gen) (eq (a "g") gen)))

def genArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxGen") }
def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def holderArgs : List ArgDecl := [boolArg "hdr", genArg "g", genArg "tg"]

/-- A same-execution request reuses its lease: the same client presents the
current holder's headers and token. Any other claim of the lease's own
execution identity is refused, before the lease is looked at. Otherwise the
request takes the next generation when no lease is valid: it names the
generation it observed (`expected`), the generation has room below the last
`uint64` (`room`), and a previous record needs an explicit decision. -/
def claimGuard : Expr :=
  .ite (a "sameExec") (andE (a "sameClient") holder)
    (andE (notE valid) (andE (eq (a "expected") gen) (andE (a "room") (orE (statusIs "none") (a "decision")))))

/-- `claimGroup`: a new record, a takeover, or the reuse of an active lease,
valid or lapsed. The reuse keeps the status and the generation and makes the
lease live. -/
def claimEvent : EventDecl where
  name := "claim"
  args := holderArgs ++ [boolArg "sameExec", boolArg "sameClient", genArg "expected", boolArg "room",
    boolArg "decision"]
  guard := claimGuard
  assigns := [("status", .enumLit "status" "active"), ("gen", .ite (a "sameExec") gen (inc gen)), ("live", tt)]

/-- `renewGroup`, also after the lease lapsed. -/
def renewEvent : EventDecl where
  name := "renew"
  args := holderArgs
  guard := holder
  assigns := [("live", tt)]

/-- `observeGroup`: an observation of status `stopped` ends the lease. -/
def observeEvent : EventDecl where
  name := "observe"
  args := holderArgs ++ [boolArg "stopped"]
  guard := holder
  assigns := [("status", .ite (a "stopped") (.enumLit "status" "lost") (f "status")),
    ("live", .ite (a "stopped") ff (f "live"))]

/-- `releaseGroup`. -/
def releaseEvent : EventDecl where
  name := "release"
  args := holderArgs
  guard := holder
  assigns := [("status", .enumLit "status" "released")]

/-- `requireGroup` as a guard: the check before a group write and in its
`Before` callback, the integration actor, and the group check of the member
recovery. The record stays. -/
def requireEvent : EventDecl where
  name := "require"
  args := holderArgs
  guard := holder
  assigns := []

/-- The `After` check of a group write: the group holder again, and every member
card the transaction changed that was in `working` or `review` before it has a
current holder (`members`, the conjunction of the member claim kernels'
`write` events). -/
def writeAfterEvent : EventDecl where
  name := "writeAfter"
  args := holderArgs ++ [boolArg "members"]
  guard := andE holder (a "members")
  assigns := []

/-- The readiness of an owned recovery (the request names an observation of
status `stopped`): a member card in `working` or `review` (`claimed`) needs its
current holder (`member`) and a valid observation ticket (`ticket`), and the
member has no unfinished start (`pending`). -/
def recoverOwnedEvent : EventDecl where
  name := "recoverOwned"
  args := holderArgs ++ [boolArg "stopped", boolArg "claimed", boolArg "member", boolArg "ticket",
    boolArg "pending"]
  guard := andE holder (andE (a "stopped") (andE (orE (notE (a "claimed")) (andE (a "member") (a "ticket")))
    (notE (a "pending"))))
  assigns := []

/-- The request side of a lost member recovery: an execution identity, an
explicit decision, and an honest `unknown` observation of the predecessor,
presented by the group holder. The member's claim record moves through the
claim kernel's `audit` event. -/
def recoverLostEvent : EventDecl where
  name := "recoverLost"
  args := holderArgs ++ [boolArg "decision", boolArg "unknown", boolArg "execOk"]
  guard := andE holder (andE (a "decision") (andE (a "unknown") (a "execOk")))
  assigns := []

/-- Project startup: an active record gets a lease of at least one TTL from
the start, so the server's downtime never opens a takeover. -/
def startupEvent : EventDecl where
  name := "startup"
  args := []
  guard := statusIs "active"
  assigns := [("live", tt)]

/-- The check that the group lease still gives generation `g` an execution
(the integration actor check of a group). It changes nothing. -/
def standingEvent : EventDecl where
  name := "standing"
  args := [genArg "g"]
  guard := andE (statusIs "active") (eq gen (a "g"))
  assigns := []

/-- The environment: the server clock passes `ExpiresAt`. -/
def expireEvent : EventDecl where
  name := "expire"
  args := []
  guard := f "live"
  assigns := [("live", ff)]

def serverGroupLeaseMachine : Machine where
  name := "servergrouplease"
  enums := [{ name := "status", ctors := ["none", "active", "lost", "released", "lapsed"] }]
  params := ["maxGen"]
  fields := [
    { name := "status", ty := .scalar (.enum "status"), init := .scalar (.enum 0) },
    { name := "gen", ty := .scalar (.nat "maxGen"), init := .scalar (.nat 0) },
    { name := "live", ty := .scalar .bool, init := .scalar (.bool false) }
  ]
  events := [claimEvent, renewEvent, observeEvent, releaseEvent, requireEvent, writeAfterEvent,
    recoverOwnedEvent, recoverLostEvent, expireEvent, startupEvent, standingEvent]
  queries := [
    -- A valid lease: no other execution may take the group over.
    { name := "valid", ty := .base .bool, body := valid },
    -- Some generation has an execution: the record is active.
    { name := "hasStanding", ty := .base .bool, body := statusIs "active" },
    -- The status a view shows: an active record whose lease expired is `lapsed`.
    { name := "shown", ty := .base (.enum "status"),
      body := .ite (andE (statusIs "active") (notE (f "live"))) (.enumLit "status" "lapsed") (f "status") }
  ]

end KanderFormal.Machines.ServerGroupLease
