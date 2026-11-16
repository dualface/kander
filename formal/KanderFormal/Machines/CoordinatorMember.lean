import KanderFormal.Semantics

/-
The coordinator member mirror machine (M12): `CoordinatorMember` of one member
of one group.

One instance describes one member. The state is the column the mirror recorded,
the card revision, the execution chain position (`tseq`, `TransitionSeq`), the
dispatch round position (`dseq`, `DispatchSeq`), whether the member is blocked,
and whether a recovery confirmed it (`confirmed`). The execution cycle, the
review and dispatch projections, and the delivery commit stay in the shell:
the kernel decides acceptance from the positions and the facts below.

A reconcile judges the member alone: it accepts a new mirror (`accept`,
`acceptConfirmed`, `acceptTerminal`) or keeps the last one and blocks (`block`).
The instance depends on nothing outside itself, so a block of one member does
not change what another member's instance decides.

Facts the shell computes and the kernel does not: `ok` (every other check of the
path passed: handoffs, originals, review projection, dispatch facts, delivery,
Git), `missing` (the chain does not carry the cycle the mirror recorded, with no
handoff record: `missingHandoffError`), `cycSame` (the confirmed cycle equals the
card's current cycle), and, for a recovery, the checks of the card lifecycle and
execution ledger kernels (`ok`).

`early` is the backlog and todo columns.
-/
namespace KanderFormal.Machines.CoordinatorMember

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def ff : Expr := .boolLit false
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y
def ge (x y : Expr) : Expr := .cmp .ge x y

def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def colArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "col") }
def revArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxRev") }
def seqArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxSeq") }

def colIs (e : Expr) (c : String) : Expr := eq e (.enumLit "col" c)

/-- The column of an argument or field is `archived` or `trash`. -/
def terminalE (e : Expr) : Expr := orE (colIs e "archived") (colIs e "trash")
/-- `done`, `archived`, or `trash`. -/
def finishedE (e : Expr) : Expr := orE (colIs e "done") (terminalE e)
/-- `working` or `review`. -/
def activeE (e : Expr) : Expr := orE (colIs e "working") (colIs e "review")

/-- `memberConfirmed`: a finished card whose cycle is the one a recovery confirmed. -/
def shortcutE : Expr := andE (f "confirmed") (andE (finishedE (a "newCol")) (a "cycSame"))

/-- The member's card moved on: `s.Revision` is not below the mirror. -/
def revOkE : Expr := ge (a "newRev") (f "rev")

/-- A member that is neither finished-and-confirmed nor in `archived` or `trash`:
the walk of the execution chain and the dispatch rounds. The chain position must
not move back and the chain must carry the mirror's cycle. -/
def acceptEvent : EventDecl where
  name := "accept"
  args := [colArg "newCol", revArg "newRev", seqArg "newTseq", seqArg "newDseq", boolArg "cycSame", boolArg "ok",
    boolArg "missing"]
  guard := andE (a "ok") (andE (notE (a "missing")) (andE (notE (terminalE (a "newCol")))
    (andE (notE shortcutE) (andE revOkE (ge (a "newTseq") (f "tseq"))))))
  assigns := [("col", a "newCol"), ("rev", a "newRev"), ("tseq", a "newTseq"), ("dseq", a "newDseq"),
    ("blocked", ff), ("confirmed", ff)]

/-- A finished member whose confirmed cycle is still the card's: nothing is left
to check. Only the card facts and the block change. -/
def acceptConfirmedEvent : EventDecl where
  name := "acceptConfirmed"
  args := [colArg "newCol", revArg "newRev", boolArg "cycSame"]
  guard := andE (f "confirmed") (andE (finishedE (a "newCol")) (andE (a "cycSame") revOkE))
  assigns := [("col", a "newCol"), ("rev", a "newRev"), ("blocked", ff)]

/-- A member in `archived` or `trash` that is not confirmed: the chain and the end
the move recorded. The dispatch position changes only when the card has rounds. -/
def acceptTerminalEvent : EventDecl where
  name := "acceptTerminal"
  args := [colArg "newCol", revArg "newRev", seqArg "newTseq", seqArg "newDseq", boolArg "hasRounds",
    boolArg "cycSame", boolArg "ok", boolArg "missing"]
  guard := andE (a "ok") (andE (notE (a "missing")) (andE (terminalE (a "newCol"))
    (andE (notE (andE (f "confirmed") (a "cycSame"))) (andE revOkE (ge (a "newTseq") (f "tseq"))))))
  assigns := [("col", a "newCol"), ("rev", a "newRev"), ("tseq", a "newTseq"),
    ("dseq", .ite (a "hasRounds") (a "newDseq") (f "dseq")), ("blocked", ff)]

/-- The member kept its last accepted mirror and the request names why. -/
def blockEvent : EventDecl where
  name := "block"
  args := []
  guard := tt
  assigns := [("blocked", tt)]

/-- `stageRecoverReset`: a blocked member whose card is in `working` or `review`
gets a new execution cycle; the card returns to `working`. The positions are
those of the chain and the rounds after the reset. -/
def recoverResetEvent : EventDecl where
  name := "recoverReset"
  args := [colArg "cardCol", revArg "newRev", seqArg "newTseq", seqArg "newDseq", boolArg "ok"]
  guard := andE (f "blocked") (andE (activeE (a "cardCol")) (a "ok"))
  assigns := [("col", .enumLit "col" "working"), ("rev", a "newRev"), ("tseq", a "newTseq"),
    ("dseq", a "newDseq"), ("blocked", ff), ("confirmed", ff)]

/-- `confirmedMirror`: a blocked member whose card is finished is accepted as it
is. The card is not written. A chain or round list that cannot be read keeps its
position (`tseqRead`, `dseqRead`). -/
def recoverConfirmEvent : EventDecl where
  name := "recoverConfirm"
  args := [colArg "cardCol", revArg "newRev", boolArg "tseqRead", seqArg "newTseq", boolArg "dseqRead",
    seqArg "newDseq"]
  guard := andE (f "blocked") (finishedE (a "cardCol"))
  assigns := [("col", a "cardCol"), ("rev", a "newRev"),
    ("tseq", .ite (a "tseqRead") (a "newTseq") (f "tseq")),
    ("dseq", .ite (a "dseqRead") (a "newDseq") (f "dseq")), ("blocked", ff), ("confirmed", tt)]

def coordMemberMachine : Machine where
  name := "coordmember"
  enums := [{ name := "col", ctors := ["early", "working", "review", "done", "archived", "trash"] }]
  params := ["maxRev", "maxSeq"]
  fields := [
    { name := "col", ty := .scalar (.enum "col"), init := .scalar (.enum 0) },
    { name := "rev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) },
    { name := "tseq", ty := .scalar (.nat "maxSeq"), init := .scalar (.nat 0) },
    { name := "dseq", ty := .scalar (.nat "maxSeq"), init := .scalar (.nat 0) },
    { name := "blocked", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "confirmed", ty := .scalar .bool, init := .scalar (.bool false) }
  ]
  events := [acceptEvent, acceptConfirmedEvent, acceptTerminalEvent, blockEvent, recoverResetEvent,
    recoverConfirmEvent]
  queries := [
    { name := "isBlocked", ty := .base .bool, body := f "blocked" },
    { name := "isConfirmed", ty := .base .bool, body := f "confirmed" }
  ]

end KanderFormal.Machines.CoordinatorMember
