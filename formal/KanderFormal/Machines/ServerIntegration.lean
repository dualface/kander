import KanderFormal.Semantics

/-
The server integration machine: the integration record of one target ref on the
v2 server (`integrations/<id>/current.json`, `internal/server/integration.go`,
`integration_validation.go`, `integration_completion.go`), and the completion
evidence that the published receipt of an integration gives a task or a wrap-up
dispatch.

The state is the status of the record (`none` when no record exists, `lost` only
as the status a stored record of an unknown kind projects to), its generation and
revision, whether its lease has not expired (`live`), its phase (`none`,
`reserved`, `pushing`, `integrated`, `released`, `uncertain`), the purpose of its
request, whether the request declares the source already contained
(`contained`), the generation of the integration actor's lease it was claimed
under (`auth`, `AuthorityGeneration`), and whether the observed target of the
record is a commit SHA (`obs`). The server clock is not modeled: a lease expiry
is an environment event (`expire`) and `live` is the comparison of the server
clock with `ExpiresAt` that the shell projects. The execution identities, the
client, the token hash, the target ref, the commits and the edge sets are not
modeled: the shell passes the results of comparing them as Boolean arguments.

A request presents a generation header (`g`) and a bearer token; `tg` is the
generation the token was derived for, and `hdr` says that the claim headers
parsed (`holder.invalid` is empty). `client` says that the presenting client is
the record's client. The shell maps a header or token that matches no generation
of the record to a value other than `gen`, as it does for the claims
(`ServerClaim.lean`). `owner` is `requireIntegrationOwner`: an active record, the
header of the current generation, a lease that has not expired, the record's
client, and the token of the current generation. The integration actor is the
single-card holder or the group holder (`ServerClaim.lean`,
`ServerGroupLease.lean`); the actor arguments are the facts of its current lease
(`currentIntegrationActor`): the lease is active and has not expired
(`actorLive`), it is the generation recorded in `auth` (`actorGen`), and its
execution identity and client are the record's (`actorExec`, `actorClient`).

An action names the revision it inspected (`expRev`), which must be the
record's and not zero, and the revision must have room for one more (`room`);
revisions and generations are `uint64`, and the shell maps them into the kernel
range the way the claim kernel does (`internal/server/claim_kernel.go`).

The completion evidence events (`completion`, `wrapUp`) read a published
integration receipt, which has the shape of the record. The shell projects the
receipt into the state (or the empty state when no receipt exists) and passes the
facts about the task and the review as arguments.
-/
namespace KanderFormal.Machines.ServerIntegration

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def ff : Expr := .boolLit false
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y
def ne (x y : Expr) : Expr := .cmp .ne x y
def inc (x : Expr) : Expr := .add x (.natLit 1)
def zero : Expr := .natLit 0

def statusIs (c : String) : Expr := eq (f "status") (.enumLit "status" c)
def phaseIs (c : String) : Expr := eq (f "phase") (.enumLit "phase" c)
def purposeIs (c : String) : Expr := eq (f "purpose") (.enumLit "purpose" c)
def gen : Expr := f "gen"
def rev : Expr := f "rev"
def auth : Expr := f "auth"

/-- A valid lease: an active record that has not expired. -/
def valid : Expr := andE (statusIs "active") (f "live")

/-- `requireIntegrationOwner` accepts: the headers parsed, the record is active
with a lease that has not expired, the generation header and the token belong to
its generation, and the client is the record's. -/
def owner : Expr :=
  andE (a "hdr") (andE (statusIs "active") (andE (eq (a "g") gen) (andE (f "live")
    (andE (a "client") (eq (a "tg") gen)))))

/-- `currentIntegrationActor` accepts: the actor's lease is active and has not
expired, it is the generation the record was claimed under, and its execution
identity and client are the record's. -/
def actor : Expr :=
  andE (a "actorLive") (andE (eq (a "actorGen") auth) (andE (a "actorExec") (a "actorClient")))

/-- `requireIntegration`: the owner whose actor still holds its lease. -/
def lease : Expr := andE owner actor

/-- The action names the inspected revision, which is not zero, and the revision
has room for one more. -/
def revOk : Expr := andE (eq (a "expRev") rev) (andE (ne (a "expRev") zero) (a "room"))

def genArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxGen") }
def revArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxRev") }
def authArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxAuth") }
def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def purposeArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "purpose") }

def ownerArgs : List ArgDecl := [boolArg "hdr", genArg "g", genArg "tg", boolArg "client"]
def actorArgs : List ArgDecl :=
  [boolArg "actorLive", authArg "actorGen", boolArg "actorExec", boolArg "actorClient"]
def leaseArgs : List ArgDecl := ownerArgs ++ actorArgs
def actionArgs : List ArgDecl := leaseArgs ++ [revArg "expRev", boolArg "room"]

/-- `claimIntegration` before its transaction: a target whose lease is not valid
(a record in any phase, including an expired `reserved` or `pushing`). The
request names the generation and the revision it inspected, the record has room
for one more of each, and the execution identity differs from the record's
(`sameExec`). The integration actor is accepted (`actor`, the result of
`requireGroup` or `requireHolder`) and presents the request's client and
execution identity (`actorMatch`). The new record has the next generation and
revision, phase `reserved`, the request's purpose and containment flag, and the
actor's generation as its authority; it has no observed target. -/
def claimEvent : EventDecl where
  name := "claim"
  args := [genArg "expGen", revArg "expRev", boolArg "roomGen", boolArg "roomRev", boolArg "sameExec",
    boolArg "actor", boolArg "actorMatch", authArg "actorGen", purposeArg "purpose", boolArg "contained"]
  guard := andE (notE valid) (andE (eq (a "expGen") gen) (andE (eq (a "expRev") rev)
    (andE (a "roomGen") (andE (a "roomRev") (andE (notE (a "sameExec")) (andE (a "actor")
      (andE (a "actorMatch") (ne (a "purpose") (.enumLit "purpose" "none")))))))))
  assigns := [("status", .enumLit "status" "active"), ("gen", inc gen), ("rev", inc rev), ("live", tt),
    ("phase", .enumLit "phase" "reserved"), ("purpose", a "purpose"), ("contained", a "contained"),
    ("auth", a "actorGen"), ("obs", ff)]

/-- The check inside the claim transaction (`guard` and its `After`): the
observation ticket is valid for the inspected revision and generation, the actor
is accepted again, and the integration facts hold. The record stays. -/
def claimGuardEvent : EventDecl where
  name := "claimGuard"
  args := [boolArg "ticket", boolArg "actor", boolArg "facts"]
  guard := andE (a "ticket") (andE (a "actor") (a "facts"))
  assigns := []

/-- `requireIntegration` as a guard. The record stays. -/
def requireEvent : EventDecl where
  name := "require"
  args := leaseArgs
  guard := lease
  assigns := []

/-- `requireIntegrationOwner` as a guard, for the check of a `release`. The
record stays. -/
def requireOwnerEvent : EventDecl where
  name := "requireOwner"
  args := ownerArgs
  guard := owner
  assigns := []

/-- `renewIntegration` inside its transaction: `requireIntegration` again and the
integration facts. The renewal refreshes the lease and keeps the revision, the
generation, and the phase. -/
def renewEvent : EventDecl where
  name := "renew"
  args := leaseArgs ++ [boolArg "facts"]
  guard := andE lease (a "facts")
  assigns := [("live", tt)]

/-- The checks of `integrationAction` that precede the switch on the action
(the lease, the inspected revision, and the room for one more revision), for an
action name the server does not know. The record stays. -/
def checkEvent : EventDecl where
  name := "check"
  args := actionArgs
  guard := andE lease revOk
  assigns := []

/-- The checks of `integrationAction` that precede the switch on the action when
the action is `release` (the owner, the inspected revision, and the room for one
more revision). The record stays. -/
def checkOwnerEvent : EventDecl where
  name := "checkOwner"
  args := ownerArgs ++ [revArg "expRev", boolArg "room"]
  guard := andE owner revOk
  assigns := []

/-- Action `pushing`: the request needs a push, the phase is `reserved`, and the
observed target is the target the claim inspected (`obsMatch`). -/
def pushingEvent : EventDecl where
  name := "pushing"
  args := actionArgs ++ [boolArg "obsMatch", boolArg "obsSha"]
  guard := andE lease (andE revOk (andE (notE (f "contained")) (andE (phaseIs "reserved") (a "obsMatch"))))
  assigns := [("phase", .enumLit "phase" "pushing"), ("rev", inc rev), ("obs", a "obsSha")]

/-- Action `confirm`: a push intent, or a contained request in phase `reserved`,
and fresh exact remote facts (the observed target is a commit SHA, `obsSha`, with
an observation time, `obsAt`, and the edges are exactly the integrated source to
the observed target, `edgesOk`). The lease is released. -/
def confirmEvent : EventDecl where
  name := "confirm"
  args := actionArgs ++ [boolArg "obsSha", boolArg "obsAt", boolArg "edgesOk"]
  guard := andE lease (andE revOk
    (andE (orE (andE (phaseIs "reserved") (f "contained")) (phaseIs "pushing"))
      (andE (a "obsSha") (andE (a "obsAt") (a "edgesOk")))))
  assigns := [("status", .enumLit "status" "released"), ("phase", .enumLit "phase" "integrated"),
    ("live", ff), ("rev", inc rev), ("obs", tt)]

/-- Action `release`: the owner (the actor's lease is not required), a decision,
and an observed target that is empty or a commit SHA (`obsEmpty`, `obsSha`). A
record in phase `pushing` becomes `uncertain`, any other `released`. -/
def releaseEvent : EventDecl where
  name := "release"
  args := ownerArgs ++ [revArg "expRev", boolArg "room", boolArg "decision", boolArg "obsEmpty",
    boolArg "obsSha"]
  guard := andE owner (andE revOk (andE (a "decision") (orE (a "obsEmpty") (a "obsSha"))))
  assigns := [("status", .enumLit "status" "released"),
    ("phase", .ite (phaseIs "pushing") (.enumLit "phase" "uncertain") (.enumLit "phase" "released")),
    ("live", ff), ("rev", inc rev), ("obs", a "obsSha")]

/-- The check inside the transaction of `pushing` and `confirm` (`guard` and its
`After`): `requireIntegration` again, a valid observation ticket (bound to the
revision and generation), and the integration facts. The record stays. -/
def guardEvent : EventDecl where
  name := "guard"
  args := leaseArgs ++ [boolArg "ticket", boolArg "facts"]
  guard := andE lease (andE (a "ticket") (a "facts"))
  assigns := []

/-- The check inside the transaction of `release`: the owner again and a valid
observation ticket. The record stays. -/
def guardReleaseEvent : EventDecl where
  name := "guardRelease"
  args := ownerArgs ++ [boolArg "ticket"]
  guard := andE owner (a "ticket")
  assigns := []

/-- `completionIntegration` (the move of a card into `done` on a project with a
common remote): the integration is not applicable (the review is closed, its
plan target is `N/A`, and no delivery beyond the baseline exists), or the
published receipt of the task's generation (the state) is `integrated`,
`released`, for the purpose `deliver`, observed a commit SHA, lists the task's
generation with a delivery commit (`member`), and the review is still closed with
the receipt's commit as its plan target. The state stays. -/
def completionEvent : EventDecl where
  name := "completion"
  args := [boolArg "closed", boolArg "planNA", boolArg "noDelivery", boolArg "member",
    boolArg "planIsCommit"]
  guard := orE (andE (a "closed") (andE (a "planNA") (a "noDelivery")))
    (andE (phaseIs "integrated") (andE (statusIs "released") (andE (purposeIs "deliver") (andE (f "obs")
      (andE (a "member") (andE (a "closed") (a "planIsCommit")))))))
  assigns := []

/-- `wrapUpIntegration` (a wrap-up dispatch other than `end` and `settle`): no
evidence is needed on a project without a common remote when the evidence names
no integration (`notRequired`). Otherwise the evidence names the integration, the
execution and a generation that is not zero (`idGiven`, `igen`), and the
published execution (the state) matches it item by item: the integration ID
(`idMatch`), phase `integrated`, status `released`, the generation, the purpose
`deliver`, the source commit (`srcMatch`), the observed target (`tgtMatch`) and
the project (`cwdMatch`). The task's generation is a member with a delivery
commit (`member`), and the review is closed with the source commit as its plan
target and as the evidence's review target (`planIsSrc`, `reviewIsSrc`). The
state stays. -/
def wrapUpEvent : EventDecl where
  name := "wrapUp"
  args := [boolArg "notRequired", boolArg "idGiven", boolArg "idMatch", genArg "igen", boolArg "srcMatch",
    boolArg "tgtMatch", boolArg "cwdMatch", boolArg "member", boolArg "closed", boolArg "planIsSrc",
    boolArg "reviewIsSrc"]
  guard := orE (a "notRequired")
    (andE (a "idGiven") (andE (ne (a "igen") zero) (andE (a "idMatch") (andE (phaseIs "integrated")
      (andE (statusIs "released") (andE (eq (a "igen") gen) (andE (purposeIs "deliver") (andE (a "srcMatch")
        (andE (a "tgtMatch") (andE (a "cwdMatch") (andE (a "member") (andE (a "closed")
          (andE (a "planIsSrc") (a "reviewIsSrc"))))))))))))))
  assigns := []

/-- The environment: the server clock passes `ExpiresAt`. -/
def expireEvent : EventDecl where
  name := "expire"
  args := []
  guard := f "live"
  assigns := [("live", ff)]

def serverIntegrationMachine : Machine where
  name := "serverintegration"
  enums := [
    { name := "status", ctors := ["none", "active", "lost", "released"] },
    { name := "phase", ctors := ["none", "reserved", "pushing", "integrated", "released", "uncertain"] },
    { name := "purpose", ctors := ["none", "initialize", "assemble", "deliver"] }
  ]
  params := ["maxGen", "maxRev", "maxAuth"]
  fields := [
    { name := "status", ty := .scalar (.enum "status"), init := .scalar (.enum 0) },
    { name := "gen", ty := .scalar (.nat "maxGen"), init := .scalar (.nat 0) },
    { name := "rev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) },
    { name := "live", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "phase", ty := .scalar (.enum "phase"), init := .scalar (.enum 0) },
    { name := "purpose", ty := .scalar (.enum "purpose"), init := .scalar (.enum 0) },
    { name := "contained", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "auth", ty := .scalar (.nat "maxAuth"), init := .scalar (.nat 0) },
    { name := "obs", ty := .scalar .bool, init := .scalar (.bool false) }
  ]
  events := [claimEvent, claimGuardEvent, requireEvent, requireOwnerEvent, renewEvent, checkEvent,
    checkOwnerEvent, pushingEvent, confirmEvent, releaseEvent, guardEvent, guardReleaseEvent, completionEvent, wrapUpEvent, expireEvent]
  queries := [
    -- A valid lease: an active record that has not expired.
    { name := "valid", ty := .base .bool, body := valid },
    -- The status a view shows: an active record whose lease expired is `lost`.
    { name := "shown", ty := .base (.enum "status"),
      body := .ite (andE (statusIs "active") (notE (f "live"))) (.enumLit "status" "lost") (f "status") }
  ]

end KanderFormal.Machines.ServerIntegration
