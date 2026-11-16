import KanderFormal.Semantics

/-
The review disposition machine (M8): the author records for one finding and one
member card.

One instance describes one (finding, card) pair of a run. The assignment of the
run (`reviewfindings`, event `assign`) assigns the pair or not; the author then
appends records to the pair's chain, each pointing at the last one, and an
orchestrator may withdraw the finding of a member that left the batch. The
machine decides which records the chain accepts: the pair must be assigned, the
status must be allowed for the tier of the finding and the role of the run, a
withdrawal needs the exclusion of the member, a record with the identifier of a
stored one is either the same record (accepted, nothing is written) or a
conflict, and a new record names the last record of the chain as its
predecessor. A closed batch takes no new record.

The run-level facts (`ledger`: the assignment exists; `available`: the findings
of the run can be obtained) come from the `reviewfindings` machine through the
shell, as Boolean arguments.

`status` is the persisted status string: `other` is any string outside the seven
values, which `validateDisposition` refuses. `gate` is whether the tier of the
finding is `blocking`, `high`, or `medium` (`mustFix`), and `security` whether the
role of the run is `Security` or a legacy security role. `dup` is the scan of the
ledger for the identifier of the presented record (`none`, an identical record,
or a different one); `ref` is the presented `previous_record_id` against the
chain (empty, the last record, any other).

Every other Boolean argument is a fact the hand-written shell computes from the
persisted records and the presented record (an ID shape, the author, the report
hash, the fix and waiver evidence, the execution authorization and the card
revision). The machine computes none of them and refuses an event when one does
not hold.
-/
namespace KanderFormal.Machines.ReviewDisposition

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y

def statusIs (c : String) : Expr := eq (a "status") (.enumLit "status" c)
def dupIs (c : String) : Expr := eq (a "dup") (.enumLit "dup" c)
def refIs (c : String) : Expr := eq (a "ref") (.enumLit "ref" c)

/-- The conjunction of Boolean arguments. -/
def allArgs : List String → Expr
  | [] => tt
  | [x] => a x
  | x :: xs => andE (a x) (allArgs xs)

def boolArgs (names : List String) : List ArgDecl := names.map fun n => { name := n, ty := .scalar .bool }

def statusArg : ArgDecl := { name := "status", ty := .scalar (.enum "status") }
def dupArg : ArgDecl := { name := "dup", ty := .scalar (.enum "dup") }
def refArg : ArgDecl := { name := "ref", ty := .scalar (.enum "ref") }

/-- The batch of the run is open. -/
def writableE : Expr := notE (f "closed")

/-- The chain rule of a new record. An identical stored record is accepted
(and must belong to this chain); a different one is a conflict; a new record
names the last record of the chain, or nothing when the chain is empty. -/
def chainE : Expr :=
  andE (notE (dupIs "conflict"))
    (andE (orE (notE (dupIs "same")) (f "chain"))
      (orE (dupIs "same") (orE (andE (refIs "empty") (notE (f "chain"))) (andE (refIs "last") (f "chain")))))

/-- The statuses a record of a gate tier or another tier may take, apart from
`withdrawn`, which only `withdraw` writes. -/
def statusE : Expr :=
  andE (notE (statusIs "withdrawn")) (andE (notE (statusIs "other"))
    (orE (andE (a "gate") (notE (statusIs "deferred")))
      (andE (notE (a "gate")) (orE (statusIs "fixed") (orE (statusIs "deferred") (statusIs "rejected"))))))

/-- A waiver is for a security role. -/
def waiverRoleE : Expr := orE (notE (statusIs "waived")) (a "security")

/-! ## Assignment -/

/-- The pair is assigned when the run's assignment is written. -/
def assignEvent : EventDecl where
  name := "assign"
  args := []
  guard := notE (f "assigned")
  assigns := [("assigned", tt)]

/-- The batch of the run closes. -/
def closeEvent : EventDecl where
  name := "close"
  args := []
  guard := notE (f "closed")
  assigns := [("closed", tt)]

/-! ## Records (`SubmitReviewDispositionWithTransaction`, `withdrawReviewFinding`) -/

def submitFacts : List String :=
  ["expect", "exec", "full", "current", "ledger", "available", "shape", "authorOk", "fieldsOk", "bound",
    "evidenceOk", "waiverOk"]

/-- An author record that is not a withdrawal: the card is the working card of
the current owner under its execution authorization, the assignment and the
findings exist, the record is well formed, the status is allowed for the tier,
a waiver is for a security role, and the chain rule holds. -/
def submitEvent : EventDecl where
  name := "submit"
  args := [statusArg, dupArg, refArg] ++ boolArgs (["gate", "security"] ++ submitFacts)
  guard := andE writableE (andE (f "assigned") (andE (allArgs submitFacts) (andE statusE (andE waiverRoleE chainE))))
  assigns := [("chain", tt)]

def withdrawFacts : List String :=
  ["expect", "declared", "excluded", "named", "ledger", "available", "shape", "authorOk", "fieldsOk", "bound",
    "evidenceOk", "waiverOk"]

/-- The withdrawal of a finding of a member that left the batch: the member is
excluded, the withdrawal names its exclusion, and the record is well formed and
follows the chain rule. -/
def withdrawEvent : EventDecl where
  name := "withdraw"
  args := [dupArg, refArg] ++ boolArgs withdrawFacts
  guard := andE writableE (andE (f "assigned") (andE (allArgs withdrawFacts) chainE))
  assigns := [("chain", tt)]

/-! ## Machine -/

def reviewDispositionMachine : Machine where
  name := "reviewdisposition"
  enums := [
    { name := "status", ctors := ["confirmed", "fixed", "rejected", "unverifiable", "waived", "deferred", "withdrawn", "other"] },
    { name := "dup", ctors := ["none", "same", "conflict"] },
    { name := "ref", ctors := ["empty", "last", "other"] }
  ]
  params := []
  fields := [
    { name := "assigned", ty := .scalar .bool, init := boolV false },
    { name := "chain", ty := .scalar .bool, init := boolV false },
    { name := "closed", ty := .scalar .bool, init := boolV false }
  ]
  events := [assignEvent, closeEvent, submitEvent, withdrawEvent]
  queries := [
    { name := "writable", ty := .base .bool, body := writableE },
    { name := "recorded", ty := .base .bool, body := f "chain" }
  ]

end KanderFormal.Machines.ReviewDisposition
