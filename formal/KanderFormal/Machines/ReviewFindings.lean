import KanderFormal.Semantics

/-
The review findings machine (M7 and the assignment of M8): where the findings
of one review run come from and how they are assigned to the member cards.

One instance describes one run. The run is read in once (`open`), after which
its findings come from one of three sources: the structured block of its report,
an interpretation (`review interpret`, schema 2 reports only), or a legacy
mapping (`review map-legacy`, schema 0 reports only). The assignment of the
findings to the member cards is written once. The machine decides which source
may be written, that a written source or assignment never changes, that a run
with no findings yet (a pending interpretation) cannot be assigned, and that a
closed batch takes no new write.

`schema` is the persisted `FindingsSchema` of the run: `legacy` is any value up
to 0, `receiver` is 2 (`FindingsSchemaReceiver`), and `report` is every other
value. `structured` is whether the report parses as a structured findings block
and `fenced` whether it contains the block marker at all, so `structured`
implies `fenced`; the shell computes both from the report original and the
machine takes the implication as a condition of `open`.

Every argument that is a Boolean is a fact the hand-written shell computes from
the persisted records (a text validation, a lineage check, a comparison with the
stored record, the coverage and membership of an assignment). The machine
computes none of them and refuses an event when one does not hold. `same` is the
equality of the presented record with the stored one, ignoring the recording
time (for an assignment, after the owners are frozen again from the current
cards).
-/
namespace KanderFormal.Machines.ReviewFindings

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y

def schemaIs (c : String) : Expr := eq (f "schema") (.enumLit "schema" c)

/-- The conjunction of Boolean arguments. -/
def allArgs : List String → Expr
  | [] => tt
  | [x] => a x
  | x :: xs => andE (a x) (allArgs xs)

def boolArgs (names : List String) : List ArgDecl := names.map fun n => { name := n, ty := .scalar .bool }

/-- The run is read in and its batch is open for writes. -/
def writableE : Expr := andE (f "opened") (notE (f "closed"))

/-- The findings of the run can be obtained: a receiver run has an
interpretation or a parsable report, a report run a parsable report, and a
legacy run a parsable report or a mapping. -/
def availableE : Expr :=
  andE (f "opened")
    (orE (andE (schemaIs "receiver") (orE (f "interp") (f "structured")))
      (orE (andE (schemaIs "report") (f "structured"))
        (andE (schemaIs "legacy") (orE (f "structured") (f "legacy")))))

/-- A receiver run whose report has no structured findings and that has no
interpretation yet: its findings cannot be consumed. -/
def pendingE : Expr :=
  andE (f "opened") (andE (schemaIs "receiver") (andE (notE (f "interp")) (notE (f "structured"))))

/-- A new interpretation may be written: a receiver run whose report is not
structured. -/
def interpWritableE : Expr :=
  andE (f "opened") (andE (schemaIs "receiver") (andE (notE (f "structured")) (notE (f "interp"))))

/-- The run is a legacy run: only these take a legacy mapping. -/
def legacySchemaE : Expr := andE (f "opened") (schemaIs "legacy")

/-! ## Reading the run in -/

def schemaArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "schema") }

def openFacts : List String := ["structured", "fenced", "consumable"]

/-- A run that is consumable (successful and published) is read in once with its
schema and the two facts about its report. A structured report contains the
block marker. -/
def openEvent : EventDecl where
  name := "open"
  args := [schemaArg "toSchema"] ++ boolArgs openFacts
  guard := andE (notE (f "opened")) (andE (a "consumable") (orE (notE (a "structured")) (a "fenced")))
  assigns := [("opened", tt), ("schema", a "toSchema"), ("structured", a "structured"), ("fenced", a "fenced")]

/-- The batch of the run closes. -/
def closeEvent : EventDecl where
  name := "close"
  args := []
  guard := andE (f "opened") (notE (f "closed"))
  assigns := [("closed", tt)]

/-! ## Sources (`InterpretReviewWithTransaction`, `MapLegacyReviewWithTransaction`) -/

def sourceFacts : List String := ["valid", "same", "lineage"]

/-- `InterpretReview`: a receiver run takes an interpretation whose text is valid
(`valid` includes its schema), whose findings pass the lineage check, and that
is either already stored with the same content (accepted whether or not the
batch is closed, nothing is written) or new on an open batch, for a report with
no structured findings. -/
def interpretEvent : EventDecl where
  name := "interpret"
  args := boolArgs sourceFacts
  guard := andE (f "opened") (andE (schemaIs "receiver") (andE (a "valid")
    (orE (andE (f "interp") (andE (a "same") (a "lineage")))
      (andE (notE (f "interp")) (andE (notE (f "closed")) (andE (notE (f "structured")) (a "lineage")))))))
  assigns := [("interp", tt)]

/-- `MapLegacyReview`: an open batch of a legacy run takes a mapping whose text
is valid, whose report has no block marker, and whose findings pass the lineage
check; a mapping that is already stored must have the same content. -/
def mapLegacyEvent : EventDecl where
  name := "mapLegacy"
  args := boolArgs sourceFacts
  guard := andE writableE (andE (schemaIs "legacy") (andE (a "valid") (andE (notE (f "fenced"))
    (andE (a "lineage") (orE (notE (f "legacy")) (a "same"))))))
  assigns := [("legacy", tt)]

/-! ## Assignment (`AssignReviewFindingsWithTransaction`) -/

def assignFacts : List String := ["provenance", "lineage", "covers", "members", "active", "owners", "same"]

/-- `AssignReviewFindings`: an open batch whose findings are available takes the
assignment when its provenance holds, the findings pass the lineage check, every
finding is assigned, only to run members, none of them excluded, and each with
an owner; an assignment that is already stored must have the same content. -/
def assignEvent : EventDecl where
  name := "assign"
  args := boolArgs assignFacts
  guard := andE writableE (andE availableE (andE (a "provenance") (andE (a "lineage") (andE (a "covers")
    (andE (a "members") (andE (a "active") (andE (a "owners") (orE (notE (f "assigned")) (a "same")))))))))
  assigns := [("assigned", tt)]

/-! ## Machine -/

def reviewFindingsMachine : Machine where
  name := "reviewfindings"
  enums := [
    { name := "schema", ctors := ["legacy", "report", "receiver"] }
  ]
  params := []
  fields := [
    { name := "opened", ty := .scalar .bool, init := boolV false },
    { name := "schema", ty := .scalar (.enum "schema"), init := enumV 0 },
    { name := "structured", ty := .scalar .bool, init := boolV false },
    { name := "fenced", ty := .scalar .bool, init := boolV false },
    { name := "interp", ty := .scalar .bool, init := boolV false },
    { name := "legacy", ty := .scalar .bool, init := boolV false },
    { name := "assigned", ty := .scalar .bool, init := boolV false },
    { name := "closed", ty := .scalar .bool, init := boolV false }
  ]
  events := [openEvent, closeEvent, interpretEvent, mapLegacyEvent, assignEvent]
  queries := [
    { name := "available", ty := .base .bool, body := availableE },
    { name := "pending", ty := .base .bool, body := pendingE },
    { name := "writable", ty := .base .bool, body := writableE },
    { name := "interpWritable", ty := .base .bool, body := interpWritableE },
    { name := "legacySchema", ty := .base .bool, body := legacySchemaE }
  ]

end KanderFormal.Machines.ReviewFindings
