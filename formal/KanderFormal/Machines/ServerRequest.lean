import KanderFormal.Semantics

/-
The server request machine: one collaboration request of a task on the v2
server (`requests/<task>/<id>.json`, `internal/server/requests.go`,
`dispatch_request.go`). A dispatch proposal is a request with `prop` set.

The state is the status of the request (`none` when no record exists), its
revision (the request's own, never the card's), the generation it targets,
whether it is a dispatch proposal, and the card revision a proposal binds
(`prev`, zero for any other request). The server clock, the claim record, and
the card are not modeled: the shell projects their facts as arguments. `cgen`
is the generation of the claim record, and `held`, `active` and `live` say that
the card is in `working` or `review`, that the record is active, and that its
lease has not expired. `card` is the card revision a check compares with the
revision a request binds. Comparisons of strings and JSON (the client, the
acceptance window, the immutable dispatch material, the note) are Boolean
arguments (`same`, `sameNote`, `material`, `match`, `window`). `holder` is the
result of `requireHolder` (the claim kernel's `write` event); the shell does not
reach a request event without it.

Revisions and generations are compared and the request revision is increased
by one; nothing else is computed. The shell maps them into the kernel range the
way the claim kernel does (`internal/server/claim_kernel.go`).

An answer is a request status (`accepted`, `done` or `rejected`), so one enum
describes the status and the answers; any other value is refused. A repeated
answer is a separate event (`answerSame`) from a changing one (`answer`): the
server returns the request unchanged for the first and writes it for the
second.
-/
namespace KanderFormal.Machines.ServerRequest

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
def lt (x y : Expr) : Expr := .cmp .lt x y
def inc (x : Expr) : Expr := .add x (.natLit 1)
def zero : Expr := .natLit 0

def statusIs (c : String) : Expr := eq (f "status") (.enumLit "status" c)
def ansIs (c : String) : Expr := eq (a "ans") (.enumLit "status" c)

def genArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxGen") }
def revArg (n : String) : ArgDecl := { name := n, ty := .scalar (.nat "maxRev") }
def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }
def statusArg (n : String) : ArgDecl := { name := n, ty := .scalar (.enum "status") }

/-- A request the holder can still answer: open or accepted. -/
def actionable : Expr := orE (statusIs "open") (statusIs "accepted")

/-- A request that has its final answer: done or rejected. -/
def settled : Expr := orE (statusIs "done") (statusIs "rejected")

/-- The claim facts of creating a request (`createRequest`): the card is in
`working` or `review`, the claim record is active, the request targets the
claim's generation, and the lease has not expired. -/
def claimOk : Expr :=
  andE (a "held") (andE (a "active") (andE (eq (a "target") (a "cgen")) (a "live")))

/-- `createRequest`: a dispatch proposal binds the inspected card version (the
card revision, not zero, and the task of its material). The check runs before
the claim is looked at. -/
def bindEvent : EventDecl where
  name := "bind"
  args := [boolArg "taskOk", revArg "inRev", revArg "card"]
  guard := andE (a "taskOk") (andE (ne (a "inRev") zero) (eq (a "inRev") (a "card")))
  assigns := []

/-- `createRequest`: a new request is `open` at revision 1, for the current
holder's generation. A proposal binds a card revision; any other request keeps
none (`prop` and a revision that is not zero go together). -/
def createEvent : EventDecl where
  name := "create"
  args := [boolArg "held", boolArg "active", boolArg "live", genArg "cgen", genArg "target", boolArg "prop",
    revArg "inRev"]
  guard := andE (statusIs "none") (andE claimOk (eq (a "prop") (ne (a "inRev") zero)))
  assigns := [("status", .enumLit "status" "open"), ("rev", .natLit 1), ("tgen", a "target"), ("prop", a "prop"),
    ("prev", a "inRev")]

/-- `createRequest`: the dispatch of a proposal already identifies a request
(`same` compares the client, the acceptance window, and the dispatch material).
The immutable proposal is returned and nothing changes. -/
def recreateEvent : EventDecl where
  name := "recreate"
  args := [boolArg "held", boolArg "active", boolArg "live", genArg "cgen", genArg "target", revArg "card",
    boolArg "same"]
  guard := andE (ne (f "status") (.enumLit "status" "none")) (andE (f "prop") (andE claimOk
    (andE (eq (f "tgen") (a "target")) (andE (eq (f "prev") (a "card")) (a "same")))))
  assigns := []

/-- The checks of `answerRequest` before the answer is compared with the
request: the answer is `accepted`, `done` or `rejected`; the holder answers a
request that targets its generation and is not `superseded`; a given expected
revision (not zero) is the request's; the first acceptance of an open proposal
needs an acceptance window that is not over and the card at the bound
revision. -/
def answerChecks : Expr :=
  andE (orE (ansIs "accepted") (orE (ansIs "done") (ansIs "rejected")))
    (andE (a "holder") (andE (ne (f "status") (.enumLit "status" "none")) (andE (eq (f "tgen") (a "cgen"))
      (andE (ne (f "status") (.enumLit "status" "superseded"))
        (andE (orE (eq (a "expected") zero) (eq (a "expected") (f "rev")))
          (orE (notE (andE (f "prop") (andE (statusIs "open") (ansIs "accepted"))))
            (andE (a "window") (eq (f "prev") (a "card")))))))))

/-- The answer repeats the request's status and note. -/
def sameAnswer : Expr := andE (eq (a "ans") (f "status")) (a "sameNote")

def answerArgs : List ArgDecl :=
  [boolArg "holder", genArg "cgen", statusArg "ans", revArg "expected", boolArg "sameNote", boolArg "window",
    revArg "card"]

/-- `answerRequest`: the same answer is returned, nothing changes (even for a
settled request). -/
def answerSameEvent : EventDecl where
  name := "answerSame"
  args := answerArgs
  guard := andE answerChecks sameAnswer
  assigns := []

/-- `answerRequest`: a new answer to a request that is not settled changes its
status and note, and increases its revision by one. -/
def answerEvent : EventDecl where
  name := "answer"
  args := answerArgs
  guard := andE answerChecks (andE (notE sameAnswer) (notE settled))
  assigns := [("status", a "ans"), ("rev", inc (f "rev"))]

/-- `stageSupersedeRequests`: a handoff or recovery to generation `ngen`
supersedes a request of an older generation that is open or accepted. -/
def supersedeEvent : EventDecl where
  name := "supersede"
  args := [genArg "ngen"]
  guard := andE (lt (f "tgen") (a "ngen")) actionable
  assigns := [("status", .enumLit "status" "superseded"), ("rev", inc (f "rev"))]

/-- `inspectDispatchProposal` and `stagePreparedProposal`: the formal
preparation of the proposal of a dispatch. The inspection and the staging run
this event with the facts of the inspection. `unique` says that one proposal
has the dispatch ID; `material` that the formal dispatch equals the proposal's
bytes. The proposal becomes `accepted`, also when it already was, and its
revision grows. -/
def prepareEvent : EventDecl where
  name := "prepare"
  args := [boolArg "unique", boolArg "holder", genArg "cgen", boolArg "window", revArg "card", boolArg "material"]
  guard := andE (a "unique") (andE (a "holder") (andE (f "prop") (andE (eq (f "tgen") (a "cgen"))
    (andE actionable (andE (a "window") (andE (eq (f "prev") (a "card")) (a "material")))))))
  assigns := [("status", .enumLit "status" "accepted"), ("rev", inc (f "rev"))]

/-- `stageCompletedProposal`: the completion move of a dispatch settles its
accepted proposal of the claim's generation (`found`, the claim record exists;
`completed`, the dispatch is completed; `match`, the request's dispatch is it). -/
def completeEvent : EventDecl where
  name := "complete"
  args := [boolArg "completed", boolArg "found", boolArg "match", genArg "cgen"]
  guard := andE (a "completed") (andE (a "found") (andE (a "match") (andE (f "prop")
    (andE (statusIs "accepted") (eq (f "tgen") (a "cgen"))))))
  assigns := [("status", .enumLit "status" "done"), ("rev", inc (f "rev"))]

/-- The holder can still answer the request. -/
def actionableQuery : QueryDecl := { name := "actionable", ty := .base .bool, body := actionable }

/-- The request has its final answer. -/
def settledQuery : QueryDecl := { name := "settled", ty := .base .bool, body := settled }

def serverRequestMachine : Machine where
  name := "serverrequest"
  enums := [{ name := "status", ctors := ["none", "open", "accepted", "done", "rejected", "superseded"] }]
  params := ["maxGen", "maxRev"]
  fields := [
    { name := "status", ty := .scalar (.enum "status"), init := .scalar (.enum 0) },
    { name := "rev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) },
    { name := "tgen", ty := .scalar (.nat "maxGen"), init := .scalar (.nat 0) },
    { name := "prop", ty := .scalar .bool, init := .scalar (.bool false) },
    { name := "prev", ty := .scalar (.nat "maxRev"), init := .scalar (.nat 0) }
  ]
  events := [bindEvent, createEvent, recreateEvent, answerSameEvent, answerEvent, supersedeEvent, prepareEvent,
    completeEvent]
  queries := [actionableQuery, settledQuery]

end KanderFormal.Machines.ServerRequest
