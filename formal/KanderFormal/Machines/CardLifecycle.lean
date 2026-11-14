import KanderFormal.Semantics

/-
The card lifecycle machine (M1): the column of one card, whether its contract
is frozen, the number of its `CONTRACT_DECISIONS` records, and whether a member
exclusion record exists.

`move` is an ordinary move between columns, `kander pick` included (a pick is
the move into `todo`). Its guard is the transition table of
`internal/board/board.go` and nothing else: the gates of the target column (the
todo gate, the review evidence gate, dispatch staging, the reclaim decision)
belong to other decisions and stay outside this machine. The other column
changes are the start rollback, the same-column move of a dispatch, and the
reset of a coordinator recovery; each takes the integrity facts its shell
computes (an exact cursor, a dispatch authorization, a recovery CAS) as Boolean
arguments.

`update` is a body update of `spec.md`. Its argument `change` says what the
update changes in the frozen contract fields (`GOAL`, `USER_DECISIONS`,
`EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, `OUT_OF_SCOPE`, `SIZE`, task group
relations, pins): nothing, only top-level acceptance ticks, or anything else.
No other event changes those fields.
-/
namespace KanderFormal.Machines.CardLifecycle

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def ff : Expr := .boolLit false
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x

/-- Whether `x` is the column `c`. -/
def isCol (x : Expr) (c : String) : Expr := .cmp .eq x (.enumLit "column" c)

/-- Whether `x` is one of the columns `cs`. -/
def inCols (x : Expr) : List String → Expr
  | [] => ff
  | [c] => isCol x c
  | c :: cs => orE (isCol x c) (inCols x cs)

/-- The transition table of `internal/board/board.go`, as rows of a source
column and its target columns. -/
def table : List (String × List String) := [
  ("backlog", ["todo", "archived", "trash"]),
  ("todo", ["backlog", "working", "archived", "trash"]),
  ("working", ["review", "done", "archived", "trash"]),
  ("review", ["working", "done", "archived", "trash"]),
  ("done", ["archived", "trash"])
]

/-- Whether the move from `x` to `y` is a row of the table. -/
def inTable (x y : Expr) : Expr :=
  table.foldr (fun (src, dsts) acc => orE (andE (isCol x src) (inCols y dsts)) acc) ff

def column : Expr := f "column"
def held : Expr := inCols column ["working", "review"]

/-! ## Events -/

/-- An ordinary move or a pick. A move into `archived` or `trash` of a plan or
task group member (`member`) writes the exclusion record, except the move of a
done card into `archived`. Every move freezes the contract. -/
def moveEvent : EventDecl where
  name := "move"
  args := [{ name := "to", ty := .scalar (.enum "column") }, { name := "member", ty := .scalar .bool }]
  guard := inTable column (a "to")
  assigns := [
    ("column", a "to"),
    ("frozen", tt),
    ("excluded", orE (f "excluded") (andE (a "member")
      (andE (inCols (a "to") ["archived", "trash"])
        (notE (andE (isCol (a "to") "archived") (isCol column "done"))))))
  ]

/-- `RollbackDocumentWithTransaction`: the launcher that still owns its start
attempt at the exact cursor (`owns`) restores the card. A start returns the card
to `todo` (`toTodo`); a takeover keeps the column. -/
def rollbackEvent : EventDecl where
  name := "rollback"
  args := [{ name := "toTodo", ty := .scalar .bool }, { name := "owns", ty := .scalar .bool }]
  guard := andE (a "owns") (andE held (orE (notE (a "toTodo")) (isCol column "working")))
  assigns := [("column", .ite (a "toTodo") (.enumLit "column" "todo") column)]

/-- `MoveWithOptions` from `working` to `working` with a dispatch
authorization the dispatch machine accepted (`authorized`). -/
def dispatchMoveEvent : EventDecl where
  name := "dispatchMove"
  args := [{ name := "authorized", ty := .scalar .bool }]
  guard := andE (a "authorized") (isCol column "working")
  assigns := [("column", .enumLit "column" "working")]

/-- The reset of `RecoverCoordinatorWithTransaction`: a blocked member in
`working` or `review` returns to `working` with a new cycle, when the checkpoint
CAS matched (`cas`), the member is blocked (`blocked`), no dispatch binds the
card (`unbound`), and no start attempt is pending (`settled`). -/
def recoverEvent : EventDecl where
  name := "recover"
  args := [{ name := "cas", ty := .scalar .bool }, { name := "blocked", ty := .scalar .bool },
    { name := "unbound", ty := .scalar .bool }, { name := "settled", ty := .scalar .bool }]
  guard := andE (andE (a "cas") (andE (a "blocked") (andE (a "unbound") (a "settled")))) held
  assigns := [("column", .enumLit "column" "working")]

/-- A frozen-field change of a frozen contract with a decision reference leaves
a `CONTRACT_DECISIONS` record. -/
def recordsDecision : Expr :=
  andE (f "frozen") (andE (a "decision") (notE (.cmp .eq (a "change") (.enumLit "change" "none"))))

/-- `UpdateDocument` of `spec.md`. An archived card takes no update. A frozen
contract changes only through acceptance ticks in `working` or `review`, or
with a decision reference (`decision`). -/
def updateEvent : EventDecl where
  name := "update"
  args := [{ name := "change", ty := .scalar (.enum "change") }, { name := "decision", ty := .scalar .bool }]
  guard := andE (notE (isCol column "archived"))
    (orE (.cmp .eq (a "change") (.enumLit "change" "none"))
      (orE (notE (f "frozen"))
        (orE (a "decision") (andE (.cmp .eq (a "change") (.enumLit "change" "ticks")) held))))
  assigns := [("decisions", .ite recordsDecision (.add (f "decisions") (.natLit 1)) (f "decisions"))]

def cardLifecycleMachine : Machine where
  name := "cardlifecycle"
  enums := [
    { name := "column", ctors := ["backlog", "todo", "working", "review", "done", "archived", "trash"] },
    { name := "change", ctors := ["none", "ticks", "other"] }
  ]
  params := ["maxDecision"]
  fields := [
    { name := "column", ty := .scalar (.enum "column"), init := .scalar (.enum 0) },
    { name := "frozen", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "decisions", ty := .scalar (.nat "maxDecision"), init := .scalar (.nat 0) },
    { name := "excluded", ty := .scalar .bool, init := .scalar (.bool false) }
  ]
  events := [moveEvent, rollbackEvent, dispatchMoveEvent, recoverEvent, updateEvent]
  queries := [
    { name := "held", ty := .base .bool, body := held },
    { name := "frozenContract", ty := .base .bool, body := f "frozen" }
  ]

end KanderFormal.Machines.CardLifecycle
