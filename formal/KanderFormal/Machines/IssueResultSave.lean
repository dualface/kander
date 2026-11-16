import KanderFormal.Semantics

/-
The issue result resource machine: the revision of the central result record
of one issue on the v2 server (`issue-results/<hash>.json`,
`internal/server/issue_results.go`).

The state is the resource revision (0 while the server holds no record). The
work set, the stored record, and the new store are not modeled: the shell
passes the facts. `work` says that the request's work digest is the current
one, `expected` the revision the request expects (the shell maps it, with the
stored revision, to kernel values that are equal exactly when the real revisions
are), `room` that the revision is below the last `uint64`, `upd` that the new store passes the update and work relations
(`issueresult` events `update`, `work`, `idKeep`, `valid`, and `issueNeeded` for
every record), and `tx` that the transaction guard holds, before the write and
after it. The idempotent replay of a completed request is `replay`: the
receipt answers and nothing changes.
-/
namespace KanderFormal.Machines.IssueResultSave

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y
def zero : Expr := .natLit 0

def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def revArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxRev") }

/-- `readIssueResult`: a stored record has a resource revision. A record with
revision 0 is incomplete evidence. -/
def readEvent : EventDecl where
  name := "read"
  args := [boolArg "found"]
  guard := orE (notE (a "found")) (notE (eq (f "rev") zero))
  assigns := []

/-- `saveIssueResult`: the request's work digest is current, it expects the
stored revision, the revision has room, the new store passes both relations,
and the transaction guard holds. The revision rises by exactly one. -/
def saveEvent : EventDecl where
  name := "save"
  args := [boolArg "work", revArg "expected", boolArg "room", boolArg "upd", boolArg "tx"]
  guard := andE (a "work") (andE (eq (a "expected") (f "rev")) (andE (a "room") (andE (a "upd") (a "tx"))))
  assigns := [("rev", .add (f "rev") (.natLit 1))]

/-- `p.idempotent`: a completed request with the same key and input is answered
from its receipt. -/
def replayEvent : EventDecl where
  name := "replay"
  args := []
  guard := tt
  assigns := []

/-- The server holds a record of the issue: the resource has a revision. -/
def storedQuery : QueryDecl := { name := "stored", ty := .base .bool, body := notE (eq (f "rev") zero) }

def issueResultSaveMachine : Machine where
  name := "issueresultsave"
  enums := []
  params := ["maxRev"]
  fields := [
    { name := "rev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) }
  ]
  events := [readEvent, saveEvent, replayEvent]
  queries := [storedQuery]

end KanderFormal.Machines.IssueResultSave
