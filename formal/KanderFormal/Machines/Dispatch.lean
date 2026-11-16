import KanderFormal.Semantics

/-
The dispatch round machine: the durable dispatch rounds of one card
(`internal/board/dispatch*.go`).

The state is the slot a card offers one round of dispatch: the sequence number
of the latest round (`round`, 0 before the first `prepare`), the authorization
epoch of that round (`epoch`), whether the card binding (`DISPATCH_ID` and
`EXECUTION_EPOCH`) is set (`bound`), the phase and kind of the round, and
whether its release was written (`released`), and whether the record holds the
receipt of its acceptance and the receipt of its completion (`acceptedReceipt`,
`completedReceipt`; the content of a receipt is not modeled). The round number abstracts the
dispatch ID: the global uniqueness of IDs belongs to the shell. After a
release the record keeps its pair `(round, epoch)` while the card binding is
empty, so the pair of the dispatch record never decreases; the card's own
epoch is `if bound then epoch else 0`.

The authorization pair a request presents is `(authRound, authEpoch)`: the
round the presented dispatch ID names (0 for an empty ID) and the presented
epoch. `fenced` compares it with the card binding, as `a != authFrom(text)`
does. Facts the shell computes are Boolean arguments: the server clock against
the acceptance deadline (`before`), the cycle of the round against the plan
cycle (`cycle`), revision checks (`cas`, `rev`), the integrity of the stored
receipts and originals (`rec`, `evidence`, `disp`, `same`), and the epoch
ceiling of the uint64 representation (`room`).

The wrap-up grant (`grant`) is the cleanup-and-records-only authority a round of
kind wrap-up can hold from `AuthorizeDispatchWrapUp`; it is set once, fences the
previous epoch, and refuses `attempt`, `reauthorize`, a second grant, and the
full-execution and non-record writes. A settlement (`settled`) ends a completed
binding without releasing it: the round stays completed and keeps its receipt,
and only the card binding goes away. Both flags describe the current round and
reset when `prepare` starts the next one. A grant clears the receipts of its
round, as a reauthorization does.
-/
namespace KanderFormal.Machines.Dispatch

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y
def ne (x y : Expr) : Expr := .cmp .ne x y
def inc (x : Expr) : Expr := .add x (.natLit 1)
def zero : Expr := .natLit 0

/-- Conjunction of a list of guards. -/
def all : List Expr → Expr
  | [] => tt
  | [x] => x
  | x :: xs => andE x (all xs)

def phaseIs (c : String) : Expr := eq (f "phase") (.enumLit "phase" c)
def kindIs (c : String) : Expr := eq (f "kind") (.enumLit "kind" c)
def phaseLit (c : String) : Expr := .enumLit "phase" c
def exitIs (c : String) : Expr := eq (a "outcome") (.enumLit "exit" c)

/-- The round is open: prepared, uncertain, or accepted. -/
def isOpen : Expr := orE (phaseIs "prepared") (orE (phaseIs "unknown") (phaseIs "accepted"))
/-- A terminal phase of the dispatch state: completed, failed, or cancelled. -/
def isTerm : Expr := orE (phaseIs "completed") (orE (phaseIs "failed") (phaseIs "cancelled"))
/-- A failed or cancelled round: the legacy terminal phases a release can still end. -/
def isLegacy : Expr := orE (phaseIs "failed") (phaseIs "cancelled")
/-- The round has not been accepted yet. -/
def isPending : Expr := orE (phaseIs "prepared") (phaseIs "unknown")

def authRound : Expr := a "authRound"
def authEpoch : Expr := a "authEpoch"

/-- The presented authorization pair names the card binding: the card has one,
and the pair is its non-empty round and epoch. -/
def fenced : Expr :=
  all [f "bound", ne authRound zero, ne authEpoch zero, eq authRound (f "round"), eq authEpoch (f "epoch")]

def imp (x y : Expr) : Expr := orE (notE x) y

def natArg (n b : String) : ArgDecl := { name := n, ty := .scalar (.nat b) }
def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def enumArg (n e : String) : ArgDecl := { name := n, ty := .scalar (.enum e) }
def authArgs : List ArgDecl := [natArg "authRound" "maxRound", natArg "authEpoch" "maxEpoch"]

/-- `prepareDispatch` creating a round: the card is in `working` or `review`
(`col`), no start is unfinished (`free`), the card has no binding or its
binding is terminal, and the epoch is below the last one (`room`). The new
epoch is the card's epoch plus one, which is 1 on a card without binding. -/
def prepareEvent : EventDecl where
  name := "prepare"
  args := [enumArg "kind" "kind", enumArg "col" "col", boolArg "free", boolArg "room"]
  guard := all [ne (a "col") (.enumLit "col" "other"), a "free", orE (notE (f "bound")) isTerm, a "room"]
  assigns := [("round", inc (f "round")),
    ("epoch", .ite (f "bound") (inc (f "epoch")) (.natLit 1)),
    ("bound", tt), ("phase", phaseLit "prepared"), ("kind", a "kind"), ("released", .boolLit false),
    ("acceptedReceipt", .boolLit false), ("completedReceipt", .boolLit false),
    ("grant", .boolLit false), ("settled", .boolLit false)]

/-- `prepareDispatch` of an ID the registry knows: the same input returns the
stored round and changes nothing. -/
def retryEvent : EventDecl where
  name := "retry"
  args := [boolArg "same"]
  guard := a "same"
  assigns := []

/-- `BeginDispatchAttempt`: an uncertain send of a round that was prepared or
already attempted, before its acceptance deadline. A wrap-up grant never
launches or delivers. -/
def attemptEvent : EventDecl where
  name := "attempt"
  args := authArgs ++ [boolArg "cas", boolArg "cycle", boolArg "evidence", boolArg "before"]
  guard := all [notE (f "grant"), a "cas", fenced, a "cycle", a "evidence", isPending, a "before"]
  assigns := [("phase", phaseLit "unknown")]

/-- `ReauthorizeDispatch`: the next epoch of an open round. An accepted round
needs a fresh confirmed exit (`fresh`); any other needs the old deadline to be
in the future (`before`). A wrap-up grant is never upgraded to full execution. -/
def reauthorizeEvent : EventDecl where
  name := "reauthorize"
  args := authArgs ++ [boolArg "cas", boolArg "room", boolArg "cycle", boolArg "fresh", boolArg "before"]
  guard := all [notE (f "grant"), a "cas", fenced, isOpen, a "room", a "cycle",
    .ite (phaseIs "accepted") (a "fresh") (a "before")]
  assigns := [("epoch", inc (f "epoch")), ("phase", phaseLit "prepared"),
    ("acceptedReceipt", .boolLit false), ("completedReceipt", .boolLit false)]

/-- `stageDispatchMove` into `working`: acceptance of a pending round before its
deadline, or the replay of an accepted or completed one. -/
def acceptEvent : EventDecl where
  name := "accept"
  args := authArgs ++ [boolArg "rec", boolArg "cycle", boolArg "before"]
  guard := all [fenced, a "rec", a "cycle",
    orE (phaseIs "accepted") (orE (phaseIs "completed") (andE isPending (a "before")))]
  assigns := [("phase", .ite isPending (phaseLit "accepted") (f "phase")),
    ("acceptedReceipt", .ite isPending tt (f "acceptedReceipt"))]

/-- `stageDispatchMove` into `review` or `done`: completion of an accepted round
of a card in `working`, or the replay of an identical stored receipt. -/
def completeEvent : EventDecl where
  name := "complete"
  args := authArgs ++ [boolArg "rec", boolArg "cycle", boolArg "toDone", boolArg "commit",
    boolArg "evidence", boolArg "disp", boolArg "same", enumArg "col" "col"]
  guard := all [fenced, a "rec", a "cycle", eq (kindIs "wrapUp") (a "toDone"), a "commit", a "evidence",
    a "disp",
    orE (andE (phaseIs "completed") (a "same")) (andE (phaseIs "accepted") (eq (a "col") (.enumLit "col" "working")))]
  assigns := [("phase", phaseLit "completed"), ("completedReceipt", tt)]

/-- `EndDispatch`: a failed or cancelled end that also releases the binding. Ending
an accepted round needs a decision; a failed or cancelled round only releases,
with the same state and a decision. -/
def endEvent : EventDecl where
  name := "end"
  args := authArgs ++ [boolArg "cas", boolArg "room", boolArg "cycle", boolArg "decided", boolArg "toFailed"]
  guard := all [a "cas", fenced, notE (phaseIs "completed"), notE (f "released"), a "room", a "cycle",
    imp (phaseIs "accepted") (a "decided"),
    imp isLegacy (andE (a "decided") (eq (phaseIs "failed") (a "toFailed")))]
  assigns := [("phase", .ite (a "toFailed") (phaseLit "failed") (phaseLit "cancelled")),
    ("bound", .boolLit false), ("released", tt)]

/-- `stageDispatchTermination` on a move into `archived` or `trash` without a
settlement: an open round is cancelled, a failed or cancelled one stays. -/
def cancelEvent : EventDecl where
  name := "cancel"
  args := [boolArg "reasoned", boolArg "authOk", boolArg "cycle"]
  guard := all [f "bound", a "reasoned", a "authOk", a "cycle", orE isOpen isLegacy]
  assigns := [("phase", .ite isOpen (phaseLit "cancelled") (f "phase"))]

/-- `stageTakeoverDispatch` in mode `retain`: an open round stays bound to its
authorization. -/
def retainEvent : EventDecl where
  name := "retain"
  args := authArgs ++ [boolArg "decided", boolArg "rev", boolArg "rec", boolArg "cycle"]
  guard := all [fenced, a "decided", a "rev", a "rec", a "cycle", isOpen]
  assigns := []

/-- `requireExecution`: a write of the executor. A card with no binding takes a
write that presents none; otherwise the pair is the binding's, the round is
open, and an accepted round or a live deadline backs it. An author write needs
the round accepted. -/
def execEvent : EventDecl where
  name := "exec"
  args := authArgs ++ [boolArg "rec", boolArg "before", boolArg "author"]
  guard := orE (all [notE (f "bound"), eq authRound zero, eq authEpoch zero])
    (all [fenced, a "rec", orE (phaseIs "accepted") (a "before"), isOpen, imp (a "author") (phaseIs "accepted")])
  assigns := []

/-- `AuthorizeDispatchWrapUp`: the cleanup-and-records-only grant of an open round
of kind wrap-up, on a card in `working` or `review` (`col`), that holds no grant
yet. The previous executor must have stopped: a fresh confirmed exit (`fresh`)
whose outcome is `stopped`, or `reclaimed` with a decision. The grant raises the
epoch by one, which fences the previous executor, and returns the round to
`prepared` with its receipts cleared. -/
def authorizeWrapUpEvent : EventDecl where
  name := "authorizeWrapUp"
  args := authArgs ++ [boolArg "cas", boolArg "room", enumArg "col" "col", boolArg "named", boolArg "cycle",
    enumArg "outcome" "exit", boolArg "decided", boolArg "fresh", boolArg "evidence"]
  guard := all [a "cas", fenced, kindIs "wrapUp", isOpen, notE (f "grant"), a "room",
    ne (a "col") (.enumLit "col" "other"), a "named", a "cycle",
    orE (exitIs "stopped") (andE (exitIs "reclaimed") (a "decided")), a "fresh", a "evidence"]
  assigns := [("epoch", inc (f "epoch")), ("phase", phaseLit "prepared"), ("acceptedReceipt", .boolLit false),
    ("completedReceipt", .boolLit false), ("grant", tt)]

/-- `requireFullExecution`: a write that needs full execution (a start rollback,
an execution identity write, an author conclusion). A card that no wrap-up grant
holds takes it; a grant owns no identity and no author conclusion. -/
def fullEvent : EventDecl where
  name := "full"
  args := []
  guard := orE (notE (f "bound")) (notE (f "grant"))
  assigns := []

/-- `validateWrapUpUpdate`: a document write under a wrap-up grant may append a
non-empty `WRAP_UP_RECORDS` section to `spec.md` (`appends`), or write the
dedicated record path (`path`) that holds no other content (`same`), and never
carries a contract decision (`contract`). A card that no grant holds takes any
write. -/
def wrapUpWriteEvent : EventDecl where
  name := "wrapUpWrite"
  args := [boolArg "contract", boolArg "spec", boolArg "appends", boolArg "path", boolArg "same"]
  guard := orE (notE (andE (f "bound") (f "grant")))
    (andE (notE (a "contract")) (.ite (a "spec") (a "appends") (andE (a "path") (a "same"))))
  assigns := []

/-- `SettleDispatch`: a direct exit of a completed binding that is not settled yet,
for a card in `working` or `review`. The round stays completed; the card binding
goes away. -/
def settleEvent : EventDecl where
  name := "settle"
  args := authArgs ++ [boolArg "cas", enumArg "col" "col", boolArg "cycle"]
  guard := all [a "cas", fenced, phaseIs "completed", notE (f "settled"),
    ne (a "col") (.enumLit "col" "other"), a "cycle"]
  assigns := [("bound", .boolLit false), ("settled", tt)]

/-- `stageDispatchTermination` on a move into `archived` or `trash` of a completed
binding: the binding is settled instead of cancelled. Production does not check
for a settlement here, because a settlement clears the binding in the same
transaction (`settled` implies no binding, see `Props`). -/
def terminateSettleEvent : EventDecl where
  name := "terminateSettle"
  args := [boolArg "reasoned", boolArg "authOk", boolArg "cycle"]
  guard := all [f "bound", a "reasoned", a "authOk", a "cycle", phaseIs "completed"]
  assigns := [("bound", .boolLit false), ("settled", tt)]

/-- `stageTakeoverDispatch` in mode `settle-completed`: the handoff settles a
completed round that is not settled yet. -/
def takeoverSettleEvent : EventDecl where
  name := "takeoverSettle"
  args := authArgs ++ [boolArg "decided", boolArg "rev", boolArg "rec", boolArg "cycle"]
  guard := all [fenced, a "decided", a "rev", a "rec", a "cycle", phaseIs "completed", notE (f "settled")]
  assigns := [("bound", .boolLit false), ("settled", tt)]

def dispatchMachine : Machine where
  name := "dispatch"
  enums := [
    { name := "phase", ctors := ["none", "prepared", "unknown", "accepted", "completed", "failed", "cancelled"] },
    { name := "kind", ctors := ["fix", "sync", "wrapUp"] },
    { name := "col", ctors := ["working", "review", "other"] },
    { name := "exit", ctors := ["stopped", "reclaimed", "other"] }
  ]
  params := ["maxRound", "maxEpoch"]
  fields := [
    { name := "round", ty := .scalar (.nat "maxRound"), init := .scalar (.nat 0) },
    { name := "epoch", ty := .scalar (.nat "maxEpoch"), init := .scalar (.nat 0) },
    { name := "bound", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "phase", ty := .scalar (.enum "phase"), init := .scalar (.enum 0) },
    { name := "kind", ty := .scalar (.enum "kind"), init := .scalar (.enum 0) },
    { name := "released", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "acceptedReceipt", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "completedReceipt", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "grant", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "settled", ty := .scalar .bool, init := .scalar (.bool false) }
  ]
  events := [prepareEvent, retryEvent, attemptEvent, reauthorizeEvent, acceptEvent, completeEvent, endEvent,
    cancelEvent, retainEvent, execEvent, authorizeWrapUpEvent, fullEvent, wrapUpWriteEvent, settleEvent,
    terminateSettleEvent, takeoverSettleEvent]
  queries := [
    { name := "holds", ty := .base .bool, body := andE (f "bound") isOpen },
    { name := "granted", ty := .base .bool, body := andE (f "bound") (f "grant") }
  ]

end KanderFormal.Machines.Dispatch
