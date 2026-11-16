import KanderFormal.Semantics

/-
The issue close decision machine: one close decision of a result record
(`ResultCloseRecord`) and the scan of the earlier decisions that `DecideResult`
makes before it appends a new one (`internal/issue/result_sync.go`).

`ds` is the status of the decision: `none` when the record has no such
decision, `declined` for a refusal (the decision `no`), and every other status
belongs to a consent (the decision `yes`). The kind of a decision therefore
never has to be compared with its status: a consent cannot be `declined`, and a
refusal has no other status. `other` stands for a status string the store does
not know.

`did` is the identity of the decision's state version, decision, and user
reference (0 while there is no decision); the identity of a text is a hash of
it, so equal texts have equal identities, and the only event that writes it is
the append of the decision. `acc` is the accumulator of the scan. `DecideResult` walks the decisions that
bind the remote's current state version in order, and `visit` takes one of them.
The first decision that blocks decides the result and no later decision changes
it. The shell visits every decision of the record in order, so the statement
holds for a record with any number of decisions. The scan starts from the
initial accumulator `open`; the decision under `ds` takes no part in it.

The check events `settle` and `valid` change nothing. They are the decisions of
`ValidateResultStoreUpdate` and `ValidateResultStore` for one decision of the old
store and the facts of the new one.
-/
namespace KanderFormal.Machines.IssueResultDecision

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y
def ne (x y : Expr) : Expr := .cmp .ne x y

def dsIs (c : String) : Expr := eq (f "ds") (.enumLit "dstat" c)
def dsC (c : String) : Expr := .enumLit "dstat" c
def decIs (c : String) : Expr := eq (a "dec") (.enumLit "dstat" c)
def accIs (c : String) : Expr := eq (f "acc") (.enumLit "scan" c)
def scanC (c : String) : Expr := .enumLit "scan" c

def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def dstatArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "dstat") }
def idArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxId") }
def zero : Expr := .natLit 0

/-- A decision that consents (`yes`): any status but `declined`, `none`, and
`other`. -/
def consentStatus (x : Expr) : Expr :=
  orE (eq x (.enumLit "dstat" "uncertain")) (orE (eq x (.enumLit "dstat" "closed"))
    (orE (eq x (.enumLit "dstat" "observed")) (orE (eq x (.enumLit "dstat" "superseded"))
      (eq x (.enumLit "dstat" "rejected")))))

/-- `DecideResult`: a refusal is recorded (`no`, status `declined`). -/
def appendNoEvent : EventDecl where
  name := "appendNo"
  args := [idArg "ndid"]
  guard := andE (dsIs "none") (ne (a "ndid") zero)
  assigns := [("ds", dsC "declined"), ("did", a "ndid")]

/-- `DecideResult`: a consent is recorded (`yes`) before the close request;
its outcome is not known yet. -/
def appendYesEvent : EventDecl where
  name := "appendYes"
  args := [idArg "ndid"]
  guard := andE (dsIs "none") (ne (a "ndid") zero)
  assigns := [("ds", dsC "uncertain"), ("did", a "ndid")]

/-- `DecideResult`: the provider closed the issue. -/
def closeDoneEvent : EventDecl where
  name := "closeDone"
  args := []
  guard := dsIs "uncertain"
  assigns := [("ds", dsC "closed")]

/-- `DecideResult`: the provider definitely rejected the close request. -/
def closeRejectedEvent : EventDecl where
  name := "closeRejected"
  args := []
  guard := dsIs "uncertain"
  assigns := [("ds", dsC "rejected")]

/-- `InspectResult`: an `uncertain` consent is settled by the complete remote
observation. A closed remote makes it `observed`; otherwise a state version that
differs (`same` false) makes it `superseded`. A closed remote and the same
version leave nothing to reconcile, so the shell does not ask. -/
def reconcileEvent : EventDecl where
  name := "reconcile"
  args := [boolArg "closed", boolArg "same"]
  guard := andE (dsIs "uncertain") (orE (a "closed") (notE (a "same")))
  assigns := [("ds", .ite (a "closed") (dsC "observed") (dsC "superseded"))]

/-- The class of the decision `dec` as a blocker of a new decision. A decision
that is `uncertain` blocks. With `reconsider` false, a refusal blocks (it
returns the record when the new decision is a refusal too, `newNo`, and refuses
otherwise), and so does every consent. With `reconsider` true, only a consent
that is not `rejected` blocks. -/
def classify : Expr :=
  .ite (decIs "uncertain") (scanC "uncertain")
    (.ite (andE (decIs "declined") (notE (a "reconsider")))
      (.ite (a "newNo") (scanC "returned") (scanC "refusal"))
      (.ite (andE (consentStatus (a "dec")) (notE (andE (decIs "rejected") (a "reconsider"))))
        (scanC "consent") (scanC "open")))

/-- `DecideResult`: the scan takes one earlier decision of the current state
version. The first blocker decides and later decisions change nothing. -/
def visitEvent : EventDecl where
  name := "visit"
  args := [dstatArg "dec", boolArg "reconsider", boolArg "newNo"]
  guard := tt
  assigns := [("acc", .ite (accIs "open") classify (f "acc"))]

/-- `ValidateResultStoreUpdate`, one decision of the old store: it keeps its
state version, decision, and user reference (`idSame`), and a decision that is
not `uncertain` keeps its status. -/
def settleEvent : EventDecl where
  name := "settle"
  args := [dstatArg "newDs", boolArg "idSame"]
  guard := andE (a "idSame") (andE (notE (dsIs "none")) (orE (dsIs "uncertain") (eq (a "newDs") (f "ds"))))
  assigns := []

/-- `ValidateResultStore`, one decision: its status is known, and a consent
(`yes`) and a refusal (`no`) have the statuses of their kind. -/
def validEvent : EventDecl where
  name := "valid"
  args := [boolArg "yes", dstatArg "vstat"]
  guard := orE (andE (a "yes") (consentStatus (a "vstat")))
    (andE (notE (a "yes")) (eq (a "vstat") (.enumLit "dstat" "declined")))
  assigns := []

/-- The scan found a decision that blocks. -/
def blockedQuery : QueryDecl := { name := "blocked", ty := .base .bool, body := notE (accIs "open") }

/-- The decision is settled: it has a final status. -/
def settledQuery : QueryDecl :=
  { name := "settled", ty := .base .bool, body := orE (dsIs "declined") (orE (dsIs "closed")
    (orE (dsIs "observed") (orE (dsIs "superseded") (dsIs "rejected")))) }

def issueResultDecisionMachine : Machine where
  name := "issueresultdecision"
  enums := [{ name := "dstat", ctors := ["none", "declined", "uncertain", "closed", "observed", "superseded",
      "rejected", "other"] },
    { name := "scan", ctors := ["open", "uncertain", "refusal", "consent", "returned"] }]
  params := ["maxId"]
  fields := [
    { name := "ds", ty := .scalar (.enum "dstat"), init := .scalar (.enum 0) },
    { name := "acc", ty := .scalar (.enum "scan"), init := .scalar (.enum 0) },
    { name := "did", ty := .scalar (.nat "maxId"), init := .scalar (.nat 0) }
  ]
  events := [appendNoEvent, appendYesEvent, closeDoneEvent, closeRejectedEvent, reconcileEvent, visitEvent,
    settleEvent, validEvent]
  queries := [blockedQuery, settledQuery]

end KanderFormal.Machines.IssueResultDecision
