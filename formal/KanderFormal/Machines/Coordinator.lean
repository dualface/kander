import KanderFormal.Semantics

/-
The coordinator checkpoint machine (M12): the group-level record of one task
group, `G/<group>/checkpoint.json`.

One instance describes one group. The members are not part of this state: a
member's mirror is the `coordmember` machine (one instance per member), and the
witnesses of wrap-up Git checks are the `wrapupwitness` machine (one instance
per dispatch). The group event takes what depends on the members as arguments
the shell computes from them: whether the mirrors changed (`changed`), how many
members are blocked after the request (`blockedAfter`), and whether a member
refused with a request-level failure (`reqFail`). Because no member state enters
the group state, every statement below holds for every number of members.

The state is whether the checkpoint exists, its revision, the epoch of the
authority (`CoordinatorAuthority`), whether it holds a `LastRequestHash` (`last`),
and how many members are blocked. The owner and token of the authority, and the
hashes, are not modeled: the shell passes the result of comparing them
(`ownerSame`, `tokenSame`, `same`) as Boolean arguments.

Revision and epoch arguments are the values a request names (`expRev`,
`expEpoch`, `argEpoch`); the machine only compares them with the state. `room`
is the shell's fact that no `uint64` ceiling is reached.
-/
namespace KanderFormal.Machines.Coordinator

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

def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def revArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxRev") }
def epochArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxEpoch") }
def countArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "members") }

/-- A claim whose hash equals the stored `ClaimHash` returns the checkpoint as
it is. -/
def replayE : Expr := andE (f "exists") (a "same")

/-- `ClaimCoordinatorWithTransaction`: a claim replays, or creates the checkpoint
or replaces its authority. The expected revision and epoch must equal the
checkpoint's (zero for no checkpoint), no `uint64` ceiling may be reached, the
new token must differ from the current one, an existing checkpoint keeps its
members, and the group membership must match. Revision and epoch rise by one
and the last request is forgotten. -/
def claimEvent : EventDecl where
  name := "claim"
  args := [revArg "expRev", epochArg "expEpoch", boolArg "same", boolArg "tokenSame", boolArg "room",
    boolArg "membersOk", boolArg "membership"]
  guard := orE replayE
    (andE (eq (a "expRev") (f "rev")) (andE (eq (a "expEpoch") (f "epoch")) (andE (a "room")
      (andE (notE (andE (f "exists") (a "tokenSame")))
        (andE (orE (notE (f "exists")) (a "membersOk")) (a "membership"))))))
  assigns := [("exists", tt), ("rev", .ite replayE (f "rev") (inc (f "rev"))),
    ("epoch", .ite replayE (f "epoch") (inc (f "epoch"))), ("last", .ite replayE (f "last") ff)]

/-- `reconcileCheckpoint`: the request presents the authority (owner, token,
epoch) and the revision it read. The authority must equal the checkpoint's; a
request at another revision is a retry only when it is the last request. Every
shell fact must hold and no member may fail at request level. A request whose
mirrors did not change writes nothing; otherwise the revision rises by one, the
request is remembered, and the blocked count is the one the mirrors now have. -/
def reconcileEvent : EventDecl where
  name := "reconcile"
  args := [boolArg "ownerSame", boolArg "tokenSame", epochArg "argEpoch", revArg "expRev", boolArg "same",
    boolArg "membersOk", boolArg "membership", boolArg "recoveries", boolArg "reqFail", boolArg "changed",
    countArg "blockedAfter", boolArg "room"]
  guard := andE (f "exists") (andE (a "ownerSame") (andE (a "tokenSame") (andE (eq (a "argEpoch") (f "epoch"))
    (andE (orE (eq (a "expRev") (f "rev")) (andE (f "last") (a "same")))
      (andE (a "membersOk") (andE (a "membership") (andE (a "recoveries")
        (andE (notE (a "reqFail")) (orE (notE (a "changed")) (a "room"))))))))))
  assigns := [("rev", .ite (a "changed") (inc (f "rev")) (f "rev")),
    ("last", orE (f "last") (a "changed")),
    ("blocked", .ite (a "changed") (a "blockedAfter") (f "blocked"))]

/-- `RecoverCoordinatorWithTransaction`: the request names the revision and epoch
it was decided on. The member must exist, be blocked, and be accepted by its own
recovery (`memberOk`) at the card revision the caller observed (`expect`). The
revision rises by one, the last request is forgotten, and one member less is
blocked. -/
def recoverEvent : EventDecl where
  name := "recover"
  args := [revArg "expRev", epochArg "expEpoch", boolArg "room", boolArg "known", boolArg "memberBlocked",
    boolArg "expect", boolArg "memberOk"]
  guard := andE (f "exists") (andE (eq (a "expRev") (f "rev")) (andE (eq (a "expEpoch") (f "epoch"))
    (andE (a "room") (andE (a "known") (andE (a "memberBlocked") (andE (a "expect")
      (andE (a "memberOk") (.cmp .ge (f "blocked") (.natLit 1)))))))))
  assigns := [("rev", inc (f "rev")), ("last", ff), ("blocked", .sub (f "blocked") (.natLit 1))]

/-- `RemoveCoordinatorMemberWithTransaction`: the request names the revision and
epoch it was decided on. The member must be in the checkpoint (`known`), belong
to the group on the board (`membership`), be unstarted (`unstarted`), leave at
least one member behind (`remaining`), and be named by no remaining member's
`PREREQUISITES` (`noDependents`). The revision rises by one, the epoch stays, the
last request is forgotten, and the blocked count is the one the remaining mirrors
have, which can only be lower (`blockedAfter`). -/
def removeMemberEvent : EventDecl where
  name := "removeMember"
  args := [revArg "expRev", epochArg "expEpoch", boolArg "room", boolArg "known", boolArg "unstarted",
    boolArg "membership", boolArg "remaining", boolArg "noDependents", countArg "blockedAfter"]
  guard := andE (f "exists") (andE (eq (a "expRev") (f "rev")) (andE (eq (a "expEpoch") (f "epoch"))
    (andE (a "room") (andE (a "known") (andE (a "unstarted") (andE (a "membership") (andE (a "remaining")
      (andE (a "noDependents") (.cmp .le (a "blockedAfter") (f "blocked"))))))))))
  assigns := [("rev", inc (f "rev")), ("last", ff), ("blocked", a "blockedAfter")]

def coordinatorMachine : Machine where
  name := "coordinator"
  enums := []
  params := ["maxRev", "maxEpoch", "members"]
  fields := [
    { name := "exists", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "rev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) },
    { name := "epoch", ty := .scalar (.nat "maxEpoch"), init := .scalar (.nat 0) },
    { name := "last", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "blocked", ty := .scalar (.nat "members"), init := .scalar (.nat 0) }
  ]
  events := [claimEvent, reconcileEvent, recoverEvent, removeMemberEvent]
  queries := [
    -- `refuseBlockedWrapUp`: a wrap-up is refused while a checkpoint has a blocked member.
    { name := "wrapUpBlocked", ty := .base .bool, body := andE (f "exists") (.cmp .ge (f "blocked") (.natLit 1)) },
    -- A reconcile that left every member blocked reports `CoordinatorBlockedError`.
    { name := "allBlocked", ty := .base .bool, body := andE (f "exists") (eq (f "blocked") (.param "members")) },
    { name := "retryable", ty := .base .bool, body := f "last" }
  ]

end KanderFormal.Machines.Coordinator
