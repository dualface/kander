import KanderFormal.Semantics

/-
The review obligation machine (M11, with the per-member effects of M9 and M10).

One instance describes one plan member: the batches of its plan as the member
sees them, what a reset left on it, what a drop of required batches left on it,
and whether its rebound cycle still needs a batch. Members share no state here;
the plan machine (`ReviewPlan.lean`) holds what the plan decides as a whole.

The definition is written for any role enum. The registered machine uses the
production roles; the proofs hold for every role list.

Fields:
- `planned`: the member has a review plan (its task-plans pointer exists).
- `bound`: the plan records the member's current execution cycle.
- `done`: the member takes no new obligation: it reached done, or left.
- `count`: the number of batches of the plan.
- `has`, `req`, `closed`, `cur`: per batch position, whether the batch names the
  member, the roles the batch requires (of any member), whether it closed, and
  the roles whose conclusion run reviewed the member's current cycle.
- `cycNeed`, `cycFrom`: the rebound cycle needs a batch from position `cycFrom`
  on (`CycleBatchFrom`).
- `dropRec`, `dropFloor`: the roles of the drop obligations naming the member,
  and per role the position from which a batch discharges them (the largest
  `FromBatch`).
- `droppedRoles`: the roles dropped batches required of the member
  (`DroppedRoles`), kept after a decision or a release.
- `rec`, `recRoles`, `recFloor`, `recReleased`: the recovery record of a reset:
  none, one written with no plan, or one bound to the plan; its roles, its
  position, and whether a user decision released it.
-/
namespace KanderFormal.Machines.ReviewObligation

open KanderFormal

/-! ## Expression helpers -/

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def nat (n : Nat) : Expr := .natLit n
def tt : Expr := .boolLit true
def ff : Expr := .boolLit false
def eq (x y : Expr) : Expr := .cmp .eq x y
def ne (x y : Expr) : Expr := .cmp .ne x y
def lt (x y : Expr) : Expr := .cmp .lt x y
def le (x y : Expr) : Expr := .cmp .le x y
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def iteE (c x y : Expr) : Expr := .ite c x y
def recC (c : String) : Expr := .enumLit "recovery" c

/-- The empty role set. -/
def noRoles : Expr := .setOf "role" ff
/-- `count`, the end of the plan. -/
def count : Expr := f "count"

/-- Inside an `any` over batch positions nested in a role binder: the position
is `bvar 0` and the role is `bvar 1`. -/
def atI (arr : String) : Expr := .aget (f arr) (.bvar 0)
def roleAtI (arr : String) : Expr := .mem (atI arr) (.bvar 1)

/-- Inside a role binder: some batch from `lo` to the end of the plan requires
the role (of any member). -/
def anyRequires (lo : Expr) : Expr := .any lo count (roleAtI "req")

/-- Inside a role binder: some batch from `lo` on names the member and requires
the role. -/
def memberRequires (lo : Expr) : Expr := .any lo count (andE (atI "has") (roleAtI "req"))

/-- Inside a role binder: some closed batch from `lo` on names the member and
requires the role. -/
def closedRequires (lo : Expr) : Expr :=
  .any lo count (andE (atI "has") (andE (roleAtI "req") (atI "closed")))

/-- Inside a role binder: some closed batch names the member, requires the role,
and its conclusion run of the role reviewed the member's current cycle. -/
def closedCurrent : Expr :=
  .any (nat 0) count (andE (atI "has") (andE (roleAtI "req") (andE (atI "closed") (roleAtI "cur"))))

/-- Inside a role binder: the role is in the set field `s`. -/
def inSet (s : String) : Expr := .mem (f s) (.bvar 0)

/-! ## Queries -/

/-- The roles the plan requires of any member (`planRequiredRoles`). -/
def planRoles : Expr := .setOf "role" (anyRequires (nat 0))

/-- The roles a recovery record owes (`recoveryRoles`): its own for a record
bound to the plan, with the plan's for a record written with no plan. -/
def recoveryRoleIn : Expr :=
  orE (andE (eq (f "rec") (recC "planned")) (inSet "recRoles"))
    (andE (eq (f "rec") (recC "noPlan")) (orE (inSet "recRoles") (anyRequires (nat 0))))
def recoveryRoles : Expr := .setOf "role" recoveryRoleIn

/-- Inside a role binder: a closed batch reviewed the role for the recovery. A
record bound to the plan counts batches from its position; one written with no
plan counts conclusions of the current cycle (`recoveryReviewOwed`). -/
def recoveryReviewed : Expr :=
  iteE (eq (f "rec") (recC "planned")) (closedRequires (f "recFloor")) closedCurrent

/-- The roles a recovery still misses. -/
def recoveryMissing : Expr := .setOf "role" (andE recoveryRoleIn (notE recoveryReviewed))

/-- A recovery still needs an actual review; a record with no role needs the
user decision that releases it. -/
def recoveryOwed : Expr :=
  andE (ne (f "rec") (recC "none"))
    (iteE (eq recoveryRoles noRoles) (notE (f "recReleased")) (ne recoveryMissing noRoles))

/-- The roles of the drop obligations no batch discharges. With `mustClose` a
batch counts once it closed; the seal also counts a planned batch, since done
still waits for it (`dropObligationsOwed`). -/
def dropOwed (mustClose : Bool) : Expr :=
  .setOf "role" (andE (inSet "dropRec")
    (notE ((if mustClose then closedRequires else memberRequires) (.get (f "dropFloor") (.bvar 0)))))

/-- The rebound cycle has no batch from its position on (`cycleBatchMissing`). -/
def cycleMissing : Expr := andE (f "cycNeed") (notE (.any (f "cycFrom") count (atI "has")))

/-- Some batch names the member. -/
def assigned : Expr := .any (nat 0) count (atI "has")

/-- Every batch that names the member closed. -/
def ownClosed : Expr := notE (.any (nat 0) count (andE (atI "has") (notE (atI "closed"))))

/-- The member's own part of the completion gate. -/
def doneReady : Expr :=
  andE (f "planned") (andE (f "bound") (andE (notE (f "done")) (andE assigned (andE ownClosed
    (andE (notE cycleMissing) (andE (notE recoveryOwed) (eq (dropOwed true) noRoles)))))))

/-! ## Events -/

/-- The plan is created; it records the member's current cycle. -/
def createEvent : EventDecl where
  name := "create"
  args := []
  guard := andE (notE (f "planned")) (notE (f "done"))
  assigns := [("planned", tt), ("bound", tt)]

/-- A batch is appended at the end of the plan: `member` tells whether it names
the member, `roles` are the roles it requires. A member that reached done joins
no batch. -/
def addBatchEvent : EventDecl where
  name := "addBatch"
  args := [{ name := "member", ty := .scalar .bool }, { name := "roles", ty := .set "role" }]
  guard := andE (f "planned") (andE (lt count (.param "maxBatch")) (notE (andE (f "done") (a "member"))))
  assigns := [
    ("count", .add count (nat 1)),
    ("has", .aput (f "has") count (a "member")),
    ("req", .aput (f "req") count (a "roles")),
    ("closed", .aput (f "closed") count ff),
    ("cur", .aput (f "cur") count noRoles)
  ]

/-- The batch at `at` closes; `current` are the roles whose conclusion run
reviewed the member's current cycle. Every member must be bound to its current
cycle. -/
def closeEvent : EventDecl where
  name := "close"
  args := [{ name := "at", ty := .scalar (.nat "maxBatch") }, { name := "current", ty := .set "role" }]
  guard := andE (f "planned") (andE (f "bound") (andE (lt (a "at") count) (notE (.aget (f "closed") (a "at")))))
  assigns := [
    ("closed", .aput (f "closed") (a "at") tt),
    ("cur", .aput (f "cur") (a "at") (a "current"))
  ]

/-- A new execution cycle: no closed conclusion reviews it, and the plan must be
rebound. -/
def newCycleEvent : EventDecl where
  name := "newCycle"
  args := []
  guard := notE (f "done")
  assigns := [("bound", ff), ("cur", .afill (f "cur") (nat 0) noRoles)]

/-- The roles a reset owes under a plan: those of the old record, those the plan
requires, and those dropped batches required of the member. Inside a role
binder. -/
def resetRoleIn : Expr := orE (inSet "recRoles") (orE (anyRequires (nat 0)) (inSet "droppedRoles"))
def resetRoles : Expr := .setOf "role" resetRoleIn
def resetKeeps : Expr := andE (f "planned") (eq resetRoles noRoles)

/-- A coordinator reset: a new cycle, and the recovery record
(`stageRecoveryReview`). With no plan the record has no role and is not
released; under a plan it takes the roles from the current end of the plan on,
unless there is no role, which keeps the old record as it is. -/
def resetEvent : EventDecl where
  name := "reset"
  args := []
  guard := notE (f "done")
  assigns := [
    ("bound", ff),
    ("cur", .afill (f "cur") (nat 0) noRoles),
    ("rec", iteE (f "planned") (iteE resetKeeps (f "rec") (recC "planned")) (recC "noPlan")),
    ("recRoles", iteE (f "planned") (iteE resetKeeps (f "recRoles") resetRoles) noRoles),
    ("recFloor", iteE (f "planned") (iteE resetKeeps (f "recFloor") count) (nat 0)),
    ("recReleased", iteE resetKeeps (f "recReleased") ff)
  ]

/-- The plan records the member's new cycle. A batch from the member's first
open batch on reviews it, or from the end of the plan when every batch of the
member closed; a member of no batch keeps its old position
(`cycleBatchStart`). -/
def rebindEvent : EventDecl where
  name := "rebind"
  args := []
  guard := andE (f "planned") (andE (notE (f "bound")) (notE (f "done")))
  assigns := [
    ("bound", tt),
    ("cycNeed", orE (f "cycNeed") assigned),
    ("cycFrom", iteE assigned (.first (nat 0) count (andE (atI "has") (notE (atI "closed")))) (f "cycFrom"))
  ]

/-- Inside a role binder: a dropped batch (from `keep` on) names the member and
requires the role. -/
def droppedFor : Expr := memberRequires (a "keep")
/-- Inside a role binder: a dropped batch requires the role. -/
def droppedAny : Expr := anyRequires (a "keep")
/-- The drop leaves obligations: no user decision came with it, and the member
still takes obligations. -/
def dropTakes : Expr := andE (notE (a "decision")) (notE (f "done"))

/-- The batches from `keep` on, none of them closed, leave the plan
(`dropPlanBatches`). Without a decision each role they required of the member
stays owed from the new end of the plan; every older obligation moves there
too. The member remembers the roles either way. A recovery record bound to the
plan moves to the new end; one written with no plan keeps the roles the drop
took out of the plan. -/
def dropEvent : EventDecl where
  name := "drop"
  args := [{ name := "keep", ty := .scalar (.nat "maxBatch") }, { name := "decision", ty := .scalar .bool }]
  guard := andE (f "planned") (andE (le (nat 1) (a "keep")) (andE (lt (a "keep") count)
    (notE (.any (a "keep") count (atI "closed")))))
  assigns := [
    ("count", a "keep"),
    ("has", .afill (f "has") (a "keep") ff),
    ("req", .afill (f "req") (a "keep") noRoles),
    ("closed", .afill (f "closed") (a "keep") ff),
    ("cur", .afill (f "cur") (a "keep") noRoles),
    ("dropRec", .setOf "role" (orE (inSet "dropRec") (andE dropTakes droppedFor))),
    ("dropFloor", .mapOf "role" (iteE (andE dropTakes droppedFor) (a "keep")
      (iteE (le (.get (f "dropFloor") (.bvar 0)) (a "keep")) (.get (f "dropFloor") (.bvar 0)) (a "keep")))),
    ("droppedRoles", .setOf "role" (orE (inSet "droppedRoles") droppedFor)),
    ("cycFrom", iteE (le (f "cycFrom") (a "keep")) (f "cycFrom") (a "keep")),
    ("recRoles", iteE (eq (f "rec") (recC "noPlan")) (.setOf "role" (orE (inSet "recRoles") droppedAny)) (f "recRoles")),
    ("recFloor", iteE (andE (eq (f "rec") (recC "planned")) (lt (a "keep") (f "recFloor"))) (a "keep") (f "recFloor"))
  ]

/-- A user decision releases the drop obligations naming the member. It refuses
a member with none, so a decision never stands with no effect. -/
def releaseDropEvent : EventDecl where
  name := "releaseDrop"
  args := []
  guard := andE (f "planned") (ne (f "dropRec") noRoles)
  assigns := [("dropRec", noRoles), ("dropFloor", .afill (f "dropFloor") (nat 0) (nat 0))]

/-- A user decision releases a recovery record written with no plan, while the
plan requires no role of it. A released record is not released again. -/
def releaseRecoveryEvent : EventDecl where
  name := "releaseRecovery"
  args := []
  guard := andE (f "planned") (andE (eq (f "rec") (recC "noPlan")) (andE (notE (f "recReleased"))
    (eq recoveryRoles noRoles)))
  assigns := [("recReleased", tt)]

/-- The member completes. `planClear` is what the gate checks beyond this
member: the plan is sealed, every other batch closed or void, and no other
member owes anything. -/
def doneEvent : EventDecl where
  name := "done"
  args := [{ name := "planClear", ty := .scalar .bool }]
  guard := andE doneReady (a "planClear")
  assigns := [("done", tt)]

/-! ## Machine -/

def obligationMachine (roles : List String) : Machine where
  name := "reviewobligation"
  enums := [
    { name := "role", ctors := roles },
    { name := "recovery", ctors := ["none", "noPlan", "planned"] }
  ]
  params := ["maxBatch"]
  fields := [
    { name := "planned", ty := .scalar .bool, init := boolV false },
    { name := "bound", ty := .scalar .bool, init := boolV false },
    { name := "done", ty := .scalar .bool, init := boolV false },
    { name := "count", ty := .scalar (.nat "maxBatch"), init := natV 0 },
    { name := "has", ty := .arr "maxBatch" (.scalar .bool), init := boolV false },
    { name := "req", ty := .arr "maxBatch" (.set "role"), init := .set (roles.map fun _ => false) },
    { name := "closed", ty := .arr "maxBatch" (.scalar .bool), init := boolV false },
    { name := "cur", ty := .arr "maxBatch" (.set "role"), init := .set (roles.map fun _ => false) },
    { name := "cycNeed", ty := .scalar .bool, init := boolV false },
    { name := "cycFrom", ty := .scalar (.nat "maxBatch"), init := natV 0 },
    { name := "dropRec", ty := .set "role", init := .set (roles.map fun _ => false) },
    { name := "dropFloor", ty := .map "role" (.nat "maxBatch"), init := .map (roles.map fun _ => .nat 0) },
    { name := "droppedRoles", ty := .set "role", init := .set (roles.map fun _ => false) },
    { name := "rec", ty := .scalar (.enum "recovery"), init := enumV 0 },
    { name := "recRoles", ty := .set "role", init := .set (roles.map fun _ => false) },
    { name := "recFloor", ty := .scalar (.nat "maxBatch"), init := natV 0 },
    { name := "recReleased", ty := .scalar .bool, init := boolV false }
  ]
  events := [createEvent, addBatchEvent, closeEvent, newCycleEvent, resetEvent, rebindEvent, dropEvent,
    releaseDropEvent, releaseRecoveryEvent, doneEvent]
  queries := [
    { name := "planRoles", ty := .set "role", body := planRoles },
    { name := "recoveryRoles", ty := .set "role", body := recoveryRoles },
    { name := "recoveryMissing", ty := .set "role", body := recoveryMissing },
    { name := "recoveryOwed", ty := .base .bool, body := recoveryOwed },
    { name := "dropOwedClosed", ty := .set "role", body := dropOwed true },
    { name := "dropOwedPlanned", ty := .set "role", body := dropOwed false },
    { name := "cycleMissing", ty := .base .bool, body := cycleMissing },
    { name := "cycleBatchOwed", ty := .base .bool, body := orE cycleMissing recoveryOwed },
    { name := "assigned", ty := .base .bool, body := assigned },
    { name := "doneReady", ty := .base .bool, body := doneReady },
    { name := "takesObligations", ty := .base .bool, body := notE (f "done") },
    { name := "resetKeepsRecord", ty := .base .bool, body := resetKeeps },
    { name := "recoveryReleasable", ty := .base .bool,
      body := andE (eq (f "rec") (recC "noPlan")) (notE (f "recReleased")) }
  ]

/-- The production review roles: PMQA and Security (`PinRole` folds the
historical four roles into them). -/
def productionRoles : List String := ["pmqa", "security"]

def reviewObligationMachine : Machine := obligationMachine productionRoles

end KanderFormal.Machines.ReviewObligation
