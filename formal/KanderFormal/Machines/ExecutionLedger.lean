import KanderFormal.Semantics

/-
The execution ledger machine (M2, board side): who executes one card.

The state is the column of the card as the ledger sees it (`todo` stands for
`backlog` and `todo`, `closed` for `done`, `archived`, and `trash`), the kind and
status of the current execution record, the position of that record among the
records of the card (`record`, 0 before the first), the number of transitions
of the chain (`seq`), the card revision (`rev`), the number of board claim
decisions (`claim` lines of `LIFECYCLE_DECISION`, `cycle`), and the card
revision the current record was written at (`recRev`).

A writer names the execution it acts for in one of two ways. A move, a start,
a rollback, and a recovery hold a card revision (`at`) and compare it with the
card. The other entrances hold a record fence (`fence`, the position of the
record their cursor is bound to) and compare it with the current record; they
never compare the card revision, so a fast executor's own updates do not fail
its launcher's confirmation. The integrity facts a shell computes (an owner
that matches, a verified release, a recovery CAS) are Boolean arguments.
-/
namespace KanderFormal.Machines.ExecutionLedger

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def nat (n : Nat) : Expr := .natLit n
def tt : Expr := .boolLit true
def ff : Expr := .boolLit false
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y
def inc (x : Expr) : Expr := .add x (nat 1)

/-- The conjunction of a list of conditions. -/
def allOf : List Expr → Expr
  | [] => tt
  | [x] => x
  | x :: xs => andE x (allOf xs)

def isCol (x : Expr) (c : String) : Expr := eq x (.enumLit "col" c)
def statusIs (c : String) : Expr := eq (f "status") (.enumLit "status" c)
def kindIs (c : String) : Expr := eq (f "kind") (.enumLit "kind" c)
def col : Expr := f "col"
def held : Expr := orE (isCol col "working") (isCol col "review")

/-- The writer's card revision is the card's. -/
def atRev : Expr := eq (a "at") (f "rev")
/-- The writer's fence is the current record. -/
def fenced : Expr := andE (eq (a "fence") (f "record")) (.cmp .ge (f "record") (nat 1))

/-- No record, or a record that no longer owns the card. -/
def ended : Expr := orE (statusIs "none") (orE (statusIs "rolledBack") (statusIs "handedOff"))
/-- A start attempt no launcher, adoption, or takeover ended yet. -/
def unfinished : Expr := orE (statusIs "pending") (statusIs "unresolved")

def atArg : ArgDecl := { name := "at", ty := .scalar (.nat "maxRev") }
def fenceArg : ArgDecl := { name := "fence", ty := .scalar (.nat "maxSeq") }
def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }

/-- The assignments of a new current record of `kind` and `status`, written at
the next card revision. `claims` is the number of claim lines it adds. -/
def newRecord (kind status : String) (claims : Expr) : List (String × Expr) := [
  ("col", .enumLit "col" "working"),
  ("kind", .enumLit "kind" kind),
  ("status", .enumLit "status" status),
  ("record", inc (f "record")),
  ("seq", inc (f "seq")),
  ("rev", inc (f "rev")),
  ("cycle", .add (f "cycle") claims),
  ("recRev", inc (f "rev"))
]

/-! ## Claims, starts, reclaims, and recoveries (revision-checked) -/

/-- `stageExecutionMove` from `todo` to `working`: a manual claim (`owner`, which
adds a claim line) or a plain move. No release may bind the cycle when an owner
claims, a decision reference is never accepted here, and the previous record
must have ended (`chain`: the shell's check of a legacy prefix and the finished
revision). -/
def claimEvent : EventDecl where
  name := "claim"
  args := [atArg, boolArg "owner", boolArg "decision", boolArg "released", boolArg "chain"]
  guard := allOf [isCol col "todo", atRev, notE (a "decision"), notE (andE (a "owner") (a "released")), ended,
    a "chain"]
  assigns := newRecord "claim" "running" (.ite (a "owner") (nat 1) (nat 0))

/-- `BeginTaskStart`: a launcher claims a `todo` card with a pending start. -/
def startEvent : EventDecl where
  name := "start"
  args := [atArg, boolArg "chain"]
  guard := allOf [isCol col "todo", atRev, ended, a "chain"]
  assigns := newRecord "start" "pending" (nat 1)

/-- `stageReclaim` and `stageExecutionMove` from `review` to `working` with an
owner: a decision reference, and either a verified release of the old cycle or
the proof that the old cycle never held a dispatch (`proof`). -/
def reclaimEvent : EventDecl where
  name := "reclaim"
  args := [atArg, boolArg "decision", boolArg "proof"]
  guard := allOf [isCol col "review", atRev, a "decision", a "proof"]
  assigns := newRecord "reclaim" "running" (nat 1)

/-- `stageRecoverReset`: a member in `working` or `review` gets a new cycle and a
running recover record, when no start is pending, no dispatch binds the card
(`unbound`), and the checkpoint CAS matched for a blocked member (`cas`). -/
def recoverEvent : EventDecl where
  name := "recover"
  args := [atArg, boolArg "unbound", boolArg "cas"]
  guard := allOf [held, atRev, notE (statusIs "pending"), a "unbound", a "cas"]
  assigns := newRecord "recover" "running" (nat 1)

/-! ## Moves and writes that leave the records alone (revision-checked) -/

/-- A move the ledger records no execution for: `working` to `review`, a
`review` card back to `working` without an owner (a dispatch round), a move
into `done`, `archived`, or `trash`, and the moves between `backlog` and
`todo`. -/
def relocateEvent : EventDecl where
  name := "relocate"
  args := [atArg, { name := "to", ty := .scalar (.enum "col") }]
  guard := andE atRev (orE (andE (isCol col "working") (isCol (a "to") "review"))
    (orE (andE (isCol col "review") (isCol (a "to") "working"))
      (orE (andE (notE (isCol col "closed")) (isCol (a "to") "closed"))
        (orE (andE (isCol col "todo") (isCol (a "to") "todo"))
          (andE (isCol col "closed") (isCol (a "to") "closed"))))))
  assigns := [("col", a "to"), ("rev", inc (f "rev"))]

/-- Any other card write that changes no execution fact (`UpdateDocument`). -/
def writeEvent : EventDecl where
  name := "write"
  args := [atArg]
  guard := atRev
  assigns := [("rev", inc (f "rev"))]

/-! ## Fenced entrances -/

/-- `ConfirmTaskStart` and `ConfirmTaskStartSession`: the launcher of a pending
record confirms it (`match`: OWNER, and for a session confirmation the
provisional SESSION, are the record's). A session confirmation (`session`)
writes the card, which then must be in `working` or `review`. A running record
is a replay and changes nothing. -/
def confirmEvent : EventDecl where
  name := "confirm"
  args := [fenceArg, boolArg "match", boolArg "session"]
  guard := allOf [fenced, a "match",
    orE (statusIs "running") (andE (statusIs "pending") (orE (notE (a "session")) held))]
  assigns := [
    ("status", .enumLit "status" "running"),
    ("seq", .ite (statusIs "pending") (inc (f "seq")) (f "seq")),
    ("rev", .ite (andE (statusIs "pending") (a "session")) (inc (f "rev")) (f "rev"))
  ]

/-- `UnresolveExecution`: the launcher (`launcher`) of a pending record ends it
as unresolved; its cursor may not be older than the record. A container address
the card lacks (`window`) is written while the card is in `working` or
`review`. -/
def unresolveEvent : EventDecl where
  name := "unresolve"
  args := [fenceArg, atArg, boolArg "launcher", boolArg "window"]
  guard := allOf [fenced, a "launcher", statusIs "pending", .cmp .le (f "recRev") (a "at")]
  assigns := [
    ("status", .enumLit "status" "unresolved"),
    ("seq", inc (f "seq")),
    ("rev", .ite (andE (a "window") held) (inc (f "rev")) (f "rev"))
  ]

/-- `AdoptExecution`: an unfinished start whose executor was proved alive keeps
it, with a decision reference, while the card still shows the attempt's
identity (`identity`) in `working` or `review`. -/
def adoptEvent : EventDecl where
  name := "adopt"
  args := [fenceArg, boolArg "decision", boolArg "identity"]
  guard := allOf [fenced, a "decision", unfinished, held, a "identity"]
  assigns := [("status", .enumLit "status" "running"), ("seq", inc (f "seq"))]

/-- `BeginTakeover`: the current record is handed off to a new pending takeover
record in the same cycle. An unfinished start needs a decision reference. A
takeover that changes OWNER, SESSION, or WINDOW (`changes`) writes the card,
which then must be in `working` or `review`. -/
def takeoverEvent : EventDecl where
  name := "takeover"
  args := [fenceArg, boolArg "decision", boolArg "changes"]
  guard := allOf [fenced, notE ended, orE (notE unfinished) (a "decision"), orE (notE (a "changes")) held]
  assigns := [
    ("kind", .enumLit "kind" "takeover"),
    ("status", .enumLit "status" "pending"),
    ("record", inc (f "record")),
    ("seq", inc (f "seq")),
    ("rev", .ite (a "changes") (inc (f "rev")) (f "rev")),
    ("recRev", .ite (a "changes") (inc (f "rev")) (f "rev"))
  ]

/-- `stageExecutionRollback` of the launcher that owns a pending record, at its
exact cursor. A start returns the card to `todo` (`toTodo`), rolls the record
back, and removes its claim line. A takeover reinstates the record it replaced
as a new record with that record's kind and status (`restoredKind`,
`restoredStatus`, an unfinished status; only a start or a takeover is ever
pending) and keeps the column. -/
def rollbackEvent : EventDecl where
  name := "rollback"
  args := [fenceArg, atArg, boolArg "launcher", boolArg "toTodo",
    { name := "restoredKind", ty := .scalar (.enum "kind") },
    { name := "restoredStatus", ty := .scalar (.enum "status") }]
  guard := allOf [fenced, atRev, a "launcher", statusIs "pending", .cmp .le (f "recRev") (a "at"),
    orE (allOf [kindIs "start", a "toTodo", isCol col "working"])
      (allOf [kindIs "takeover", notE (a "toTodo"), held,
        notE (eq (a "restoredKind") (.enumLit "kind" "none")),
        orE (eq (a "restoredStatus") (.enumLit "status" "pending"))
          (orE (eq (a "restoredStatus") (.enumLit "status" "running"))
            (eq (a "restoredStatus") (.enumLit "status" "unresolved"))),
        orE (notE (eq (a "restoredStatus") (.enumLit "status" "pending")))
          (orE (eq (a "restoredKind") (.enumLit "kind" "start")) (eq (a "restoredKind") (.enumLit "kind" "takeover")))])]
  assigns := [
    ("col", .ite (a "toTodo") (.enumLit "col" "todo") col),
    ("kind", .ite (a "toTodo") (f "kind") (a "restoredKind")),
    ("status", .ite (a "toTodo") (.enumLit "status" "rolledBack") (a "restoredStatus")),
    ("record", .ite (a "toTodo") (f "record") (inc (f "record"))),
    ("seq", inc (f "seq")),
    ("rev", inc (f "rev")),
    ("cycle", .ite (a "toTodo") (.sub (f "cycle") (nat 1)) (f "cycle")),
    ("recRev", .ite (a "toTodo") (f "recRev") (inc (f "rev")))
  ]

/-- `RecordExecutionWindow`, `RecordExecutionSession`, `RecordExecutionProcess`,
and the window restore of a rollback by another caller: one fact of the current
record changes. A write of the card (`text`) needs a card in `working` or
`review`; a process identity changes no card text. -/
def fieldEvent : EventDecl where
  name := "field"
  args := [fenceArg, boolArg "text"]
  guard := andE fenced (orE (notE (a "text")) held)
  assigns := [("seq", inc (f "seq")), ("rev", .ite (a "text") (inc (f "rev")) (f "rev"))]

def executionLedgerMachine : Machine where
  name := "executionledger"
  enums := [
    { name := "col", ctors := ["todo", "working", "review", "closed"] },
    { name := "status", ctors := ["none", "pending", "running", "unresolved", "rolledBack", "handedOff"] },
    { name := "kind", ctors := ["none", "start", "claim", "takeover", "reclaim", "recover"] }
  ]
  params := ["maxSeq", "maxRev"]
  fields := [
    { name := "col", ty := .scalar (.enum "col"), init := .scalar (.enum 0) },
    { name := "kind", ty := .scalar (.enum "kind"), init := .scalar (.enum 0) },
    { name := "status", ty := .scalar (.enum "status"), init := .scalar (.enum 0) },
    { name := "record", ty := .scalar (.nat "maxSeq"), init := .scalar (.nat 0) },
    { name := "seq", ty := .scalar (.nat "maxSeq"), init := .scalar (.nat 0) },
    { name := "rev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) },
    { name := "cycle", ty := .scalar (.nat "maxSeq"), init := .scalar (.nat 0) },
    { name := "recRev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) }
  ]
  events := [claimEvent, startEvent, reclaimEvent, recoverEvent, relocateEvent, writeEvent, confirmEvent,
    unresolveEvent, adoptEvent, takeoverEvent, rollbackEvent, fieldEvent]
  queries := [
    { name := "ended", ty := .base .bool, body := ended },
    { name := "startUnfinished", ty := .base .bool, body := unfinished }
  ]

end KanderFormal.Machines.ExecutionLedger
