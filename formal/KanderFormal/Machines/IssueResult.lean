import KanderFormal.Semantics

/-
The issue result machine: one result record of a work set and the summary of
the store around it (`internal/issue/result_*.go`, `internal/server/issue_results.go`).

The instance is one record of the store, the record the entrance works on, plus
three facts about the whole store: how many records are `uncertain`, whether the
store knows the numeric issue identity, and nothing else. Records of one store
do not depend on each other except through these facts, so every statement about
this record holds for every record of a store of any size. The record that an
event does not name is changed only by the events `otherIntent` and
`otherSettle`, which describe the same events on another record.

`st` is the persisted status (`none` when the store has no record for the
version, `other` for a status string the store does not know). `full` is
`FullyResolved`, `met` whether every outcome of the record is `met`, `cpos`
whether `CommentID` is positive, `nf` the number of failures, and `nd` the number
of close decisions. The text of a record (body, author, comment ID, the work
digest, evidence version, commits, checks) is not modeled: the shell compares it
and passes the result as a Boolean argument. The remote identities are positive
by the provider contract, so a record that gets a remote comment has `cpos`.

The identity witnesses make the comparison facts of the check events derivable
from the transitions of the client events, instead of facts the shell assumes.
`bod` is the identity of the record's body and author, `cid` of its remote
comment (0 while it has none), `chk` of its verification evidence, `wk` of the
work set it was assessed against, `cw` the identity of the current work set the
store was read against, `fh` the identity of its failure log and `fp` that of
the log before the last failure was appended, and `sound` that its author is
positive, its outcomes are known, and its version is the one its assessment
computes. Identities are abstract: an event that writes a new body
brings a new identity. The shell assigns identities from a table, so that, within
one operation, two values have the same identity exactly when they are equal
(never a digest, which could collide); the events only keep or replace an
identity, so the facts read off two records are the comparisons of their
values.

The check events `update`, `work`, `idKeep`, `valid`, and `issueNeeded` change
nothing. They are the decisions of `ValidateResultStoreUpdate`,
`ValidateResultStoreWork`, and `ValidateResultStore` for one record of the old
store and the facts of the new record, and the shell asks them for every record
of the stores it compares.
-/
namespace KanderFormal.Machines.IssueResult

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
def le (x y : Expr) : Expr := .cmp .le x y
def zero : Expr := .natLit 0
def one : Expr := .natLit 1
def inc (x : Expr) : Expr := .add x one
def dec (x : Expr) : Expr := .sub x one

def stIs (c : String) : Expr := eq (f "st") (.enumLit "rstat" c)
def stC (c : String) : Expr := .enumLit "rstat" c
def newIs (c : String) : Expr := eq (a "newSt") (.enumLit "rstat" c)
def vstIs (c : String) : Expr := eq (a "vst") (.enumLit "rstat" c)
def scanIs (c : String) : Expr := eq (a "scan") (.enumLit "scan" c)

def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def numArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxN") }
def idArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxId") }
def rstatArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "rstat") }
def scanArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "scan") }

/-- The record is `published` or `equivalent`: a remote comment carries it. -/
def pubOrEq : Expr := orE (stIs "published") (stIs "equivalent")

/-- The store has no live record for the version: absent or `rejected`. A new
record may be written there. -/
def fresh : Expr := orE (stIs "none") (stIs "rejected")

/-- The record is not new: it exists and is not `rejected`. -/
def kept : Expr := orE (stIs "uncertain") pubOrEq

/-- `DecideResult` acts on a record that is `published` or `equivalent`, belongs
to the current work (`digestOk`), and is fully resolved. -/
def gate : Expr := andE pubOrEq (andE (a "digestOk") (f "full"))

/-- `readResult`: the remote identity and state are valid (`remoteOk`), and the
store's issue identity, when it has one, is the remote's (`same`); the store
then has the identity. -/
def readEvent : EventDecl where
  name := "read"
  args := [boolArg "remoteOk", boolArg "same", idArg "nw"]
  guard := andE (a "remoteOk") (andE (orE (notE (f "iid")) (a "same")) (ne (a "nw") zero))
  assigns := [("iid", tt), ("cw", a "nw")]

/-- `InspectResult`: an `uncertain` record whose comment appears in the complete
remote observation (`found`: same author, same body, positive ID) is `published`
and records the comment ID. -/
def reconcileEvent : EventDecl where
  name := "reconcile"
  args := [boolArg "found", idArg "ncid"]
  guard := andE (stIs "uncertain") (andE (a "found") (ne (a "ncid") zero))
  assigns := [("st", stC "published"), ("cpos", tt), ("unc", dec (f "unc")), ("cid", a "ncid")]

/-- `ApplyResult`: a record that is not `rejected` is assessed again. No record
may be `uncertain`. The record keeps its status. -/
def reassessEvent : EventDecl where
  name := "reassess"
  args := []
  guard := andE (eq (f "unc") zero) pubOrEq
  assigns := [("wk", f "cw")]

/-- `ApplyResult`: a new record, or one that replaces a `rejected` record, is
written as `uncertain` before the remote comment is posted. No record may be
`uncertain`; the completion evidence is not already published (`dup`); the body
is valid (`bodyOk`); a fully resolved assessment has only `met` outcomes. -/
def intentEvent : EventDecl where
  name := "intent"
  args := [boolArg "nfull", boolArg "nmet", boolArg "dup", boolArg "bodyOk", idArg "nbod", idArg "nchk",
    boolArg "nsound"]
  guard := andE (f "iid") (andE (eq (f "unc") zero) (andE fresh (andE (notE (a "dup"))
    (andE (a "bodyOk") (andE (orE (notE (a "nfull")) (a "nmet")) (andE (ne (a "nbod") zero)
      (a "nsound")))))))
  assigns := [("st", stC "uncertain"), ("unc", inc (f "unc")), ("full", a "nfull"), ("met", a "nmet"),
    ("cpos", ff), ("nd", zero), ("bod", a "nbod"), ("cid", zero), ("chk", a "nchk"), ("wk", f "cw"),
    ("sound", tt)]

/-- `ApplyResult`: the agent names an existing comment (`found` in the complete
observation) as equivalent. The new record is `equivalent` and has no
decisions. -/
def equivalentEvent : EventDecl where
  name := "equivalent"
  args := [boolArg "nfull", boolArg "nmet", boolArg "found", idArg "nbod", idArg "ncid", idArg "nchk",
    boolArg "nsound"]
  guard := andE (f "iid") (andE (eq (f "unc") zero) (andE fresh (andE (a "found")
    (andE (orE (notE (a "nfull")) (a "nmet")) (andE (ne (a "nbod") zero) (andE (ne (a "ncid") zero)
      (a "nsound")))))))
  assigns := [("st", stC "equivalent"), ("cpos", tt), ("full", a "nfull"), ("met", a "nmet"), ("nd", zero),
    ("bod", a "nbod"), ("cid", a "ncid"), ("chk", a "nchk"), ("wk", f "cw"), ("sound", tt)]

/-- `ApplyResult`: the provider accepted the comment and returned the same
author and body (`ok`). -/
def publishEvent : EventDecl where
  name := "publish"
  args := [boolArg "ok", idArg "ncid"]
  guard := andE (stIs "uncertain") (andE (a "ok") (ne (a "ncid") zero))
  assigns := [("st", stC "published"), ("cpos", tt), ("unc", dec (f "unc")), ("cid", a "ncid")]

/-- `ApplyResult`: the provider definitely rejected the comment. The record is
`rejected` and gets one more failure. -/
def rejectEvent : EventDecl where
  name := "reject"
  args := [idArg "nfh"]
  guard := andE (stIs "uncertain") (ne (a "nfh") zero)
  assigns := [("st", stC "rejected"), ("unc", dec (f "unc")), ("nf", inc (f "nf")), ("fp", f "fh"), ("fh", a "nfh")]

/-- `DecideResult`: the decision is appended after the record gate, a remote
that is still open, a scan of the earlier decisions that blocks nothing, and
valid decision fields. -/
def decideEvent : EventDecl where
  name := "decide"
  args := [boolArg "digestOk", boolArg "closed", scanArg "scan", boolArg "fieldsOk"]
  guard := andE gate (andE (notE (a "closed")) (andE (scanIs "open") (a "fieldsOk")))
  assigns := [("nd", inc (f "nd"))]

/-- `DecideResult`: the record is returned and nothing is written, because the
remote is already closed, or the earlier refusal stands and the new decision is
the same refusal. -/
def keepEvent : EventDecl where
  name := "keep"
  args := [boolArg "digestOk", boolArg "closed", scanArg "scan"]
  guard := andE gate (orE (a "closed") (scanIs "returned"))
  assigns := []

/-- `DecideResult`: the provider definitely rejected the close request; the
record gets one more failure. -/
def failCloseEvent : EventDecl where
  name := "failClose"
  args := [idArg "nfh"]
  guard := andE pubOrEq (ne (a "nfh") zero)
  assigns := [("nf", inc (f "nf")), ("fp", f "fh"), ("fh", a "nfh")]

/-- Another record of the store becomes `uncertain`. -/
def otherIntentEvent : EventDecl where
  name := "otherIntent"
  args := []
  guard := eq (f "unc") zero
  assigns := [("unc", inc (f "unc"))]

/-- The `uncertain` record of the store is another record, and it leaves that
status. -/
def otherSettleEvent : EventDecl where
  name := "otherSettle"
  args := []
  guard := andE (eq (f "unc") one) (notE (stIs "uncertain"))
  assigns := [("unc", dec (f "unc"))]

/-- `ValidateResultStoreUpdate`, one record of the old store: the new store has
it (`present`); a record that is not `rejected` keeps its status unless it was
`uncertain`, and keeps its body, author, and comment (`identSame`); the failures
only grow (`nfNew`, `prefixF`); the decisions are not fewer (`ndNew`). -/
def updateEvent : EventDecl where
  name := "update"
  args := [boolArg "present", rstatArg "newSt", boolArg "identSame", numArg "nfNew", boolArg "prefixF",
    numArg "ndNew"]
  guard := andE (a "present") (andE (orE (stIs "rejected") (andE (orE (stIs "uncertain")
    (eq (a "newSt") (f "st"))) (a "identSame"))) (andE (le (f "nf") (a "nfNew")) (andE (a "prefixF")
    (le (f "nd") (a "ndNew")))))
  assigns := []

/-- `ValidateResultStoreWork`, one record of the new store: a `rejected` record
that did not change passes (`identical`); a record that is not new keeps its
verification evidence (`checksSame`); a new record (absent or `rejected` in the
old store) is `uncertain` or `equivalent`, no other record of the old store is
`uncertain`, and it matches the current work (`workSame`, `versionOk`). -/
def workEvent : EventDecl where
  name := "work"
  args := [rstatArg "newSt", boolArg "identical", boolArg "checksSame", boolArg "workSame",
    boolArg "versionOk"]
  guard := orE (andE (stIs "rejected") (a "identical")) (orE (andE kept (a "checksSame"))
    (andE fresh (andE (orE (newIs "uncertain") (newIs "equivalent")) (andE (eq (f "unc") zero)
      (andE (a "workSame") (a "versionOk"))))))
  assigns := []

/-- `ValidateResultStoreUpdate`: a store that knows its issue identity keeps it
(`same`). -/
def idKeepEvent : EventDecl where
  name := "idKeep"
  args := [boolArg "same"]
  guard := orE (notE (f "iid")) (a "same")
  assigns := []

/-- `ValidateResultStore`, one record: its status is known, its author is
positive, a `published` or `equivalent` record has a positive comment ID, its
outcomes are valid, and a fully resolved record has only `met` outcomes. -/
def validEvent : EventDecl where
  name := "valid"
  args := [rstatArg "vst", boolArg "actorPos", boolArg "commentPos", boolArg "outOk", boolArg "nfull",
    boolArg "nmet"]
  guard := andE (orE (vstIs "uncertain") (orE (vstIs "published") (orE (vstIs "equivalent") (vstIs "rejected"))))
    (andE (a "actorPos") (andE (orE (andE (notE (vstIs "published")) (notE (vstIs "equivalent")))
      (a "commentPos")) (andE (a "outOk") (orE (notE (a "nfull")) (a "nmet")))))
  assigns := []

/-- `ValidateResultStore`: a store with records has a positive issue
identity. -/
def issueNeededEvent : EventDecl where
  name := "issueNeeded"
  args := [boolArg "hasRecords", boolArg "idPos"]
  guard := orE (notE (a "hasRecords")) (a "idPos")
  assigns := []

/-- A fully resolved, `published` or `equivalent` record: the only kind
`DecideResult` takes a decision on. -/
def resolvedQuery : QueryDecl := { name := "resolved", ty := .base .bool, body := andE pubOrEq (f "full") }

/-- The record waits for the remote outcome. -/
def pendingQuery : QueryDecl := { name := "pending", ty := .base .bool, body := stIs "uncertain" }

def issueResultMachine : Machine where
  name := "issueresult"
  enums := [{ name := "rstat", ctors := ["none", "uncertain", "published", "equivalent", "rejected", "other"] },
    { name := "scan", ctors := ["open", "uncertain", "refusal", "consent", "returned"] }]
  params := ["maxN", "maxId"]
  fields := [
    { name := "st", ty := .scalar (.enum "rstat"), init := .scalar (.enum 0) },
    { name := "unc", ty := .scalar (.nat "maxN"), init := .scalar (.nat 0) },
    { name := "iid", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "full", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "met", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "cpos", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "nf", ty := .scalar (.nat "maxN"), init := .scalar (.nat 0) },
    { name := "nd", ty := .scalar (.nat "maxN"), init := .scalar (.nat 0) },
    { name := "bod", ty := .scalar (.nat "maxId"), init := .scalar (.nat 0) },
    { name := "cid", ty := .scalar (.nat "maxId"), init := .scalar (.nat 0) },
    { name := "chk", ty := .scalar (.nat "maxId"), init := .scalar (.nat 0) },
    { name := "wk", ty := .scalar (.nat "maxId"), init := .scalar (.nat 0) },
    { name := "cw", ty := .scalar (.nat "maxId"), init := .scalar (.nat 0) },
    { name := "fh", ty := .scalar (.nat "maxId"), init := .scalar (.nat 0) },
    { name := "fp", ty := .scalar (.nat "maxId"), init := .scalar (.nat 0) },
    { name := "sound", ty := .scalar .bool, init := .scalar (.bool false) }
  ]
  events := [readEvent, reconcileEvent, reassessEvent, intentEvent, equivalentEvent, publishEvent, rejectEvent,
    decideEvent, keepEvent, failCloseEvent, otherIntentEvent, otherSettleEvent, updateEvent, workEvent, idKeepEvent,
    validEvent, issueNeededEvent]
  queries := [resolvedQuery, pendingQuery]

end KanderFormal.Machines.IssueResult
