import KanderFormal.Semantics

/-
The review run machine (M6): the execution of one review run and its
publication to one member card.

One instance describes one run as seen by one member. The run-level fields
(`phase`, `launch`, `exec`, `exitZero`, the three input artifacts, `originals`)
are the same in the instance of every member, and the shell steps every
instance with each run-level event; `published` and `skipped` are the member's
own. A run's publication is complete when every instance has `published`, which
the shell computes from the queries `complete` and `consumable`.

`phase`, `launch`, and `exec` are the persisted strings of `ReviewRun`
(`internal/board/review_run.go`). A string that is not one of the persisted
values is `other`: `UpdateReviewRun` stores any phase, launch status, and
execution status it is given, and the shell projects an unknown value as
`other`. The event `update` carries the target values and writes them as given,
as the entrance does.

Every argument that is a Boolean is an integrity fact the hand-written shell
computes from the persisted records (an ID shape, a card language, a batch
binding, a hash, a manifest, an index line). The machine computes none of them
and refuses an event when one does not hold. `sameInput` is the equality of the
frozen `ReviewInput` with the run the caller presents.
-/
namespace KanderFormal.Machines.ReviewRun

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def ff : Expr := .boolLit false
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x
def eq (x y : Expr) : Expr := .cmp .eq x y

def phaseIs (c : String) : Expr := eq (f "phase") (.enumLit "phase" c)
def execIs (c : String) : Expr := eq (a "exec") (.enumLit "exec" c)
def launchIs (c : String) : Expr := eq (a "launch") (.enumLit "launch" c)

/-- The conjunction of Boolean arguments. -/
def allArgs : List String → Expr
  | [] => tt
  | [x] => a x
  | x :: xs => andE (a x) (allArgs xs)

def boolArgs (names : List String) : List ArgDecl := names.map fun n => { name := n, ty := .scalar .bool }

/-- A run exists. -/
def exists_ : Expr := notE (phaseIs "none")

/-- A run that exists and is not finalized. -/
def open_ : Expr := andE exists_ (notE (phaseIs "finalized"))

/-! ## Preparation (`PrepareReviewRunWithTransaction`) -/

def createFacts : List String := ["ids", "cards", "batch", "role"]

/-- A new run: the identity, the cards, the batch binding with its advance and
requirements, and the role with its previous run all hold. The run starts
`prepared`, not started, `incomplete`, with exit code 0. -/
def createEvent : EventDecl where
  name := "create"
  args := boolArgs createFacts
  guard := andE (phaseIs "none") (allArgs createFacts)
  assigns := [("phase", .enumLit "phase" "prepared"), ("launch", .enumLit "launch" "notStarted"),
    ("exec", .enumLit "exec" "incomplete"), ("exitZero", tt)]

def retryFacts : List String := ["ids", "cards", "sameInput", "batchKnown", "reqOk"]

/-- The same ID again: the identity, the remaining members' cards, the frozen
input (schema 1 and equal), the batch, and the requirements hold. Nothing is
written and the reviewer is not started again. -/
def retryEvent : EventDecl where
  name := "retry"
  args := boolArgs retryFacts
  guard := andE exists_ (allArgs retryFacts)
  assigns := []

/-! ## Execution (`UpdateReviewRun`, `StoreReviewArtifact`, `FinalizeReviewRun`) -/

def phaseArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "phase") }
def launchArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "launch") }
def execArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "exec") }

/-- `UpdateReviewRun`: a run that exists and is not finalized takes any phase
(a rollback included), launch status, execution status, and exit code the
caller presents; the frozen input stays the same. -/
def updateEvent : EventDecl where
  name := "update"
  args := [phaseArg "toPhase", launchArg "launch", execArg "exec"] ++ boolArgs ["exitZero", "sameInput"]
  guard := andE open_ (andE (notE (eq (a "toPhase") (.enumLit "phase" "none"))) (a "sameInput"))
  assigns := [("phase", a "toPhase"), ("launch", a "launch"), ("exec", a "exec"), ("exitZero", a "exitZero")]

/-- `StoreReviewArtifact` for one of the three input artifacts: only a
`prepared` run takes it, and an artifact that already exists must hold the same
bytes (`same`). -/
def storeEvent (name field : String) : EventDecl where
  name := name
  args := boolArgs ["same"]
  guard := andE (phaseIs "prepared") (orE (notE (f field)) (a "same"))
  assigns := [(field, tt)]

def storePromptEvent : EventDecl := storeEvent "storePrompt" "prompt"
def storeContractEvent : EventDecl := storeEvent "storeContract" "contract"
def storeEvidenceEvent : EventDecl := storeEvent "storeEvidence" "evidence"

def finalizeFacts : List String := ["exitZero", "reportOk", "reason", "invalid", "complete", "sameInput"]

/-- `FinalizeReviewRun`. A run that exists and is not finalized takes the
launch status, execution status, and exit code the caller presents, and its
originals are written. An `ok` run needs launch status `started`, exit code 0,
and a report; any other status needs a non-zero exit code and a failure
reason. A structured report that does not parse, or whose lineage fails
(`invalid`), turns an `ok` run into `failed` with a non-zero exit code. All
required originals must exist (`complete`). -/
def finalizeEvent : EventDecl where
  name := "finalize"
  args := [launchArg "launch", execArg "exec"] ++ boolArgs finalizeFacts
  guard := andE open_ (andE (a "sameInput")
    (andE (orE (notE (execIs "ok")) (andE (a "exitZero") (andE (launchIs "started") (a "reportOk"))))
      (andE (orE (execIs "ok") (andE (notE (a "exitZero")) (a "reason"))) (a "complete"))))
  assigns := [("phase", .enumLit "phase" "finalized"), ("launch", a "launch"),
    ("exec", .ite (andE (execIs "ok") (a "invalid")) (.enumLit "exec" "failed") (a "exec")),
    ("exitZero", .ite (andE (execIs "ok") (a "invalid")) ff (a "exitZero")),
    ("originals", tt)]

/-! ## Publication (`PublishReviewRun`, `PublishReviewRunAtomic`, `stageReviewRunCard`) -/

/-- A member that is not excluded and has no copy yet takes the copy, the
manifest, and the index line, and is marked published. The run is finalized,
the frozen input is the same, the card belongs to the run's group, the card
passes the working or review check, and the batch binding, structure,
originals, and sidecar verify. -/
def stageEvent : EventDecl where
  name := "stage"
  args := boolArgs ["sameInput", "groupOk", "excluded", "memberOk", "integrity"]
  guard := andE (phaseIs "finalized") (andE (a "sameInput") (andE (a "groupOk")
    (andE (notE (f "skipped")) (andE (notE (f "published"))
      (andE (notE (a "excluded")) (andE (a "memberOk") (a "integrity")))))))
  assigns := [("published", tt)]

/-- A member that left the batch during the run takes no copy and is marked
published and skipped. -/
def skipEvent : EventDecl where
  name := "skip"
  args := boolArgs ["sameInput", "groupOk", "excluded"]
  guard := andE (phaseIs "finalized") (andE (a "sameInput") (andE (a "groupOk")
    (andE (notE (f "skipped")) (andE (notE (f "published")) (a "excluded")))))
  assigns := [("published", tt), ("skipped", tt)]

/-- A member that is already published (and not skipped) is verified against
its copy; nothing is written. -/
def verifyEvent : EventDecl where
  name := "verify"
  args := boolArgs ["sameInput", "groupOk", "integrity", "verified"]
  guard := andE (phaseIs "finalized") (andE (a "sameInput") (andE (a "groupOk")
    (andE (f "published") (andE (notE (f "skipped")) (andE (a "integrity") (a "verified"))))))
  assigns := []

/-- A skipped member passes unchanged. -/
def passSkippedEvent : EventDecl where
  name := "passSkipped"
  args := boolArgs ["sameInput", "groupOk"]
  guard := andE (phaseIs "finalized") (andE (a "sameInput") (andE (a "groupOk") (f "skipped")))
  assigns := []

/-! ## Machine -/

def reviewRunMachine : Machine where
  name := "reviewrun"
  enums := [
    { name := "phase", ctors := ["none", "prepared", "launching", "running", "collecting", "cleaning", "finalized", "other"] },
    { name := "launch", ctors := ["notStarted", "unknown", "started", "other"] },
    { name := "exec", ctors := ["incomplete", "ok", "notStarted", "failed", "interrupted", "other"] }
  ]
  params := []
  fields := [
    { name := "phase", ty := .scalar (.enum "phase"), init := enumV 0 },
    { name := "launch", ty := .scalar (.enum "launch"), init := enumV 0 },
    { name := "exec", ty := .scalar (.enum "exec"), init := enumV 0 },
    { name := "exitZero", ty := .scalar .bool, init := boolV false },
    { name := "prompt", ty := .scalar .bool, init := boolV false },
    { name := "contract", ty := .scalar .bool, init := boolV false },
    { name := "evidence", ty := .scalar .bool, init := boolV false },
    { name := "originals", ty := .scalar .bool, init := boolV false },
    { name := "published", ty := .scalar .bool, init := boolV false },
    { name := "skipped", ty := .scalar .bool, init := boolV false }
  ]
  events := [createEvent, retryEvent, updateEvent, storePromptEvent, storeContractEvent, storeEvidenceEvent,
    finalizeEvent, stageEvent, skipEvent, verifyEvent, passSkippedEvent]
  queries := [
    { name := "final", ty := .base .bool, body := phaseIs "finalized" },
    { name := "complete", ty := .base .bool, body := andE (phaseIs "finalized") (f "published") },
    { name := "consumable", ty := .base .bool,
      body := andE (phaseIs "finalized") (andE (f "published") (eq (f "exec") (.enumLit "exec" "ok"))) }
  ]

end KanderFormal.Machines.ReviewRun
