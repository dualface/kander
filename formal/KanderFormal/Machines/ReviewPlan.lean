import KanderFormal.Semantics

/-
The review plan machine (M9 and M10): what a plan decides as a whole.

One instance describes one plan: whether it exists, whether it is sealed, its
batches by position, which of them closed, and which a run or an advance
reached. The per-member obligations live in the review obligation machine
(`ReviewObligation.lean`); where a plan decision depends on them, the event
takes the member kernels' query results as an argument (for example `owed`, the
`cycleBatchOwed` of some member of an appended batch).

Every other argument is an integrity fact the hand-written shell computes
from the persisted records: a compare-and-swap that matched, a hash, a chain,
Git evidence, or a conclusion of the finding and disposition records (M7, M8).
The machine computes none of them; it refuses an event when one of them does
not hold.
-/
namespace KanderFormal.Machines.ReviewPlan

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def nat (n : Nat) : Expr := .natLit n
def tt : Expr := .boolLit true
def ff : Expr := .boolLit false
def lt (x y : Expr) : Expr := .cmp .lt x y
def le (x y : Expr) : Expr := .cmp .le x y
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def count : Expr := f "count"

/-- The conjunction of Boolean arguments. -/
def allArgs : List String → Expr
  | [] => tt
  | [x] => a x
  | x :: xs => andE (a x) (allArgs xs)

def boolArgs (names : List String) : List ArgDecl := names.map fun n => { name := n, ty := .scalar .bool }

def atArg : ArgDecl := { name := "at", ty := .scalar (.nat "maxBatch") }

/-- The batch at `at` exists and is open. -/
def openAt : Expr := andE (lt (a "at") count) (notE (.aget (f "closed") (a "at")))

/-! ## Plan creation -/

def createFacts : List String := ["coversAll", "chain", "noUnplannedRuns", "membersFree"]

/-- `CreateReviewPlan`: a plan with `batches` batches. A sealed plan must cover
every member; the batch chain must be explicit and continuous, no run of a
member may sit in an unplanned batch, and no member may already have a plan. -/
def createEvent : EventDecl where
  name := "create"
  args := [{ name := "batches", ty := .scalar (.nat "maxBatch") }, { name := "seal", ty := .scalar .bool }] ++
    boolArgs createFacts
  guard := andE (notE (f "planned")) (andE (le (nat 1) (a "batches"))
    (andE (orE (notE (a "seal")) (a "coversAll")) (allArgs (createFacts.drop 1))))
  assigns := [("planned", tt), ("sealed", a "seal"), ("count", a "batches")]

/-! ## Plan extension (`ExtendReviewPlanWithTransaction`, `ResolveReviewBatch`) -/

def ops : List String := ["rebind", "sync", "batch", "drop", "release", "resolve"]

/-- At most one of the Boolean arguments holds. -/
def atMostOne : List String → Expr
  | [] => tt
  | x :: xs => andE (notE (andE (a x) (xs.foldr (fun y acc => orE (a y) acc) ff))) (atMostOne xs)

/-- The shape of an extension request: at least one operation, no two of the
exclusive ones (`batch` stands for a batch, a seal, or both), a decision only
with a drop or a release, and a resolution with nothing else. -/
def extendEvent : EventDecl where
  name := "extend"
  args := boolArgs (ops ++ ["decision"])
  guard := andE (ops.foldr (fun x acc => orE (a x) acc) ff)
    (andE (atMostOne ops)
      (andE (orE (notE (a "decision")) (orE (a "drop") (a "release")))
        (orE (notE (a "resolve")) (notE (a "decision")))))
  assigns := []

/-- The admission of an extension to the plan: the CAS matched, and a sealed
plan takes only a rebind, a target sync, a drop, a release, or a batch without
a seal for a member whose rebound cycle has none or whose recovery still owes a
review (`owed`). -/
def admitEvent : EventDecl where
  name := "admit"
  args := boolArgs ["cas", "otherOp", "batchOnly", "owed"]
  guard := andE (f "planned") (andE (a "cas")
    (orE (notE (f "sealed")) (orE (a "otherOp") (andE (a "batchOnly") (a "owed")))))
  assigns := []

def appendFacts : List String :=
  ["cas", "binding", "membersActive", "previousClosed", "requirements", "allAssigned", "noDropOwed"]

/-- A batch is appended, and sealed with it when `seal`. A sealed plan takes a
batch only for a member whose rebound cycle has none or whose recovery still
owes a review (`owed`), and never with another seal. The seal needs every
member assigned and no drop obligation without a batch. -/
def appendEvent : EventDecl where
  name := "append"
  args := boolArgs (["seal", "owed"] ++ appendFacts)
  guard := andE (f "planned") (andE (lt count (.param "maxBatch"))
    (andE (allArgs (appendFacts.take 5))
      (andE (orE (notE (f "sealed")) (andE (notE (a "seal")) (a "owed")))
        (orE (notE (a "seal")) (andE (a "allAssigned") (a "noDropOwed"))))))
  assigns := [
    ("count", .add count (nat 1)),
    ("closed", .aput (f "closed") count ff),
    ("touched", .aput (f "touched") count ff),
    ("sealed", orE (f "sealed") (a "seal"))
  ]

/-- The plan is sealed alone. -/
def sealEvent : EventDecl where
  name := "seal"
  args := boolArgs ["cas", "allAssigned", "noDropOwed"]
  guard := andE (f "planned") (andE (notE (f "sealed")) (allArgs ["cas", "allAssigned", "noDropOwed"]))
  assigns := [("sealed", tt)]

/-- `rebind_cycles` names exactly the members whose cycle changed, and none of
them is terminal. -/
def rebindEvent : EventDecl where
  name := "rebind"
  args := boolArgs ["cas", "exact", "membersActive"]
  guard := andE (f "planned") (allArgs ["cas", "exact", "membersActive"])
  assigns := []

/-- `sync_targets` moves recorded targets to the runtime batches; every cycle
must be current. -/
def syncEvent : EventDecl where
  name := "sync"
  args := boolArgs ["cas", "cyclesCurrent", "targets"]
  guard := andE (f "planned") (allArgs ["cas", "cyclesCurrent", "targets"])
  assigns := []

/-- `drop_batches` removes the trailing `n` batches, which no run, advance, or
closure reached; at least one batch stays, and the plan reopens. -/
def dropEvent : EventDecl where
  name := "drop"
  args := [{ name := "n", ty := .scalar (.nat "maxBatch") }] ++ boolArgs ["cas", "trailing"]
  guard := andE (f "planned") (andE (allArgs ["cas", "trailing"]) (andE (le (nat 1) (a "n"))
    (andE (lt (a "n") count)
      (notE (.any (.sub count (a "n")) count
        (orE (.aget (f "closed") (.bvar 0)) (.aget (f "touched") (.bvar 0))))))))
  assigns := [
    ("count", .sub count (a "n")),
    ("sealed", ff),
    ("closed", .afill (f "closed") (.sub count (a "n")) ff),
    ("touched", .afill (f "touched") (.sub count (a "n")) ff)
  ]

/-- `release_obligation` with a user decision, for plan members whose kernels
accept the release. -/
def releaseEvent : EventDecl where
  name := "release"
  args := boolArgs ["cas", "decision", "members", "accepted"]
  guard := andE (f "planned") (allArgs ["cas", "decision", "members", "accepted"])
  assigns := []

def resolveFacts : List String := ["cas", "fresh", "covered", "published", "git"]

/-- `resolve_batch` accepts an unplanned batch: the CAS matched, the batch was
not resolved before, a closed planned batch covers it and every finding has a
result, the runs' publications verify, and the Git evidence binds. -/
def resolveEvent : EventDecl where
  name := "resolve"
  args := boolArgs resolveFacts
  guard := andE (f "planned") (allArgs resolveFacts)
  assigns := []

/-! ## Batches (`AdvanceReviewBatch`, `aggregateReviewBatch`, `CloseReviewBatch`) -/

/-- A review run reached the open batch at `at`. -/
def runEvent : EventDecl where
  name := "run"
  args := [atArg]
  guard := andE (f "planned") openAt
  assigns := [("touched", .aput (f "touched") (a "at") tt)]

def advanceFacts : List String := ["revision", "target", "noSuccessor", "attribution", "settled", "dispositions"]

/-- The open batch at `at` takes a verified delivery: the batch revision and
target CAS matched, no successor batch exists, the deliveries belong to its
members, its dispatches settled, and every assigned finding has an author
record. -/
def advanceEvent : EventDecl where
  name := "advance"
  args := atArg :: boolArgs advanceFacts
  guard := andE (f "planned") (andE openAt (allArgs advanceFacts))
  assigns := [("touched", .aput (f "touched") (a "at") tt)]

def aggregateFacts : List String := ["published", "lineage", "ledger"]

/-- The view of the batch at `at` is built: every run's publication verifies,
every finding lineage holds, and every disposition ledger reads. -/
def aggregateEvent : EventDecl where
  name := "aggregate"
  args := atArg :: boolArgs aggregateFacts
  guard := andE (f "planned") (andE (lt (a "at") count) (allArgs aggregateFacts))
  assigns := []

def closeFacts : List String :=
  ["revision", "viewHash", "conclusions", "git", "current", "previousClosed", "bound"]

/-- The open batch at `at` closes: the batch revision and the view hash matched,
the role conclusions and the handled findings hold (`ReviewClosureEdges`), the
Git evidence binds, the conclusions reviewed the current cycles, the previous
batch closed, and every member is bound to its current cycle. -/
def closeEvent : EventDecl where
  name := "close"
  args := atArg :: boolArgs closeFacts
  guard := andE (f "planned") (andE openAt (allArgs closeFacts))
  assigns := [("closed", .aput (f "closed") (a "at") tt)]

/-! ## Machine -/

def reviewPlanMachine : Machine where
  name := "reviewplan"
  enums := []
  params := ["maxBatch"]
  fields := [
    { name := "planned", ty := .scalar .bool, init := boolV false },
    { name := "sealed", ty := .scalar .bool, init := boolV false },
    { name := "count", ty := .scalar (.nat "maxBatch"), init := natV 0 },
    { name := "closed", ty := .arr "maxBatch" (.scalar .bool), init := boolV false },
    { name := "touched", ty := .arr "maxBatch" (.scalar .bool), init := boolV false }
  ]
  events := [createEvent, extendEvent, admitEvent, appendEvent, sealEvent, rebindEvent, syncEvent, dropEvent,
    releaseEvent, resolveEvent, runEvent, advanceEvent, aggregateEvent, closeEvent]
  queries := [
    { name := "allClosed", ty := .base .bool,
      body := notE (.any (nat 0) count (notE (.aget (f "closed") (.bvar 0)))) },
    { name := "untouched", ty := .base .bool,
      body := .cmp .eq (f "touched") (.afill (f "touched") (nat 0) ff) },
    { name := "anyClosed", ty := .base .bool,
      body := .cmp .ne (f "closed") (.afill (f "closed") (nat 0) ff) }
  ]

end KanderFormal.Machines.ReviewPlan
