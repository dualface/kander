import KanderFormal.Proofs.Example
import KanderFormal.Proofs.ReviewObligation
import KanderFormal.Proofs.ReviewPlan
import KanderFormal.Proofs.CardLifecycle
import KanderFormal.Proofs.ExecutionLedger
import KanderFormal.Proofs.ServerClaim
import KanderFormal.Proofs.ServerGroupLease
import KanderFormal.Proofs.ServerRequestProposal
import KanderFormal.Proofs.ServerDelivery
import KanderFormal.Proofs.ServerProposal
import KanderFormal.Proofs.Dispatch
import KanderFormal.Proofs.DispatchForms
import KanderFormal.Proofs.DispatchSafety
import KanderFormal.Proofs.DispatchGrant
import KanderFormal.Proofs.ReviewRun
import KanderFormal.Proofs.ReviewFindings
import KanderFormal.Proofs.ReviewDisposition
import KanderFormal.Proofs.Coordinator
import KanderFormal.Proofs.CoordinatorMember
import KanderFormal.Proofs.WrapUpWitness
import KanderFormal.Proofs.IssueResult
import KanderFormal.Proofs.IssueResultDecision
import KanderFormal.Proofs.IssueResultSave
import KanderFormal.Alphabets.IssueResult
import KanderFormal.Alphabets.ServerIntegration

/-
The registry of machines: what the generator emits, the bounds of the
differential vectors, and the theorems the axiom audit checks. A new machine is
added here (see docs/formal-verification.md).
-/
namespace KanderFormal.Registry

open Lean (Name)

/-- Bounds of the differential vectors. The Go replay walks every event
sequence of length `depth` over `events` from the initial state of each
parameter set. -/
structure VectorBounds where
  depth : Nat
  paramSets : List (List (String × Nat))
  /-- Values tried for every `nat` event argument. -/
  natDomain : List Nat
  /-- When not empty, the event alphabet, replacing every event with every
  argument combination; for machines whose full alphabet is too large to walk. -/
  events : List Event := []
  deriving Inhabited

structure Entry where
  machine : Machine
  /-- The definition file, relative to `formal/`; its SHA-256 goes into the
  generated file header. -/
  source : String
  vectors : VectorBounds
  /-- Machine-specific theorems: well-typedness and the proof obligations. -/
  theorems : List Name
  deriving Inhabited

/-- Theorems that hold for every machine (`Semantics.lean`, `Typing.lean`,
`Obligations.lean`). -/
def genericTheorems : List Name := [
  ``KanderFormal.run_refused_unchanged,
  ``KanderFormal.run_accepted_iff,
  ``KanderFormal.step_deterministic,
  ``KanderFormal.run_deterministic,
  ``KanderFormal.step_assign_reads_prestate,
  ``KanderFormal.wellTyped_assign_reads_prestate,
  ``KanderFormal.step_unassigned_unchanged,
  ``KanderFormal.step_refuses_out_of_bound,
  ``KanderFormal.step_preserves_conforms,
  ``KanderFormal.initial_conforms,
  ``KanderFormal.SafetyCert.reachable,
  ``KanderFormal.reachable_conforms
]

/-- Role sets of the production roles (PMQA, Security). -/
def roles (pmqa security : Bool) : Val := .set [pmqa, security]

/-- The obligation vector alphabet: the events with the argument values that
reach every expression form within the depth. -/
def obligationEvents : List Event := [
  ⟨"create", []⟩,
  ⟨"addBatch", [boolV true, roles true false]⟩,
  ⟨"addBatch", [boolV true, roles false false]⟩,
  ⟨"addBatch", [boolV false, roles false true]⟩,
  ⟨"close", [natV 0, roles true true, roles true true]⟩,
  ⟨"close", [natV 0, roles false false, roles false false]⟩,
  ⟨"close", [natV 1, roles true false, roles true false]⟩,
  ⟨"close", [natV 1, roles false false, roles true false]⟩,
  ⟨"newCycle", []⟩,
  ⟨"reset", []⟩,
  ⟨"rebind", []⟩,
  ⟨"drop", [natV 1, boolV false]⟩,
  ⟨"drop", [natV 1, boolV true]⟩,
  ⟨"dropTakes", [boolV false]⟩,
  ⟨"dropTakes", [boolV true]⟩,
  ⟨"releaseDrop", []⟩,
  ⟨"releaseRecovery", []⟩,
  ⟨"done", [boolV true]⟩,
  ⟨"done", [boolV false]⟩
]

/-- The plan vector alphabet: each event with every fact holding, and with one
fact failing for the events whose facts the integrity theorem covers. -/
def planEvents : List Event :=
  let t := boolV true
  let x := boolV false
  [ ⟨"create", [natV 1, x, t, t, t, t]⟩,
    ⟨"create", [natV 2, t, t, t, t, t]⟩,
    ⟨"create", [natV 1, t, x, t, t, t]⟩,
    ⟨"extend", [t, x, x, x, x, x, x]⟩,
    ⟨"extend", [t, t, x, x, x, x, x]⟩,
    ⟨"extend", [x, x, x, x, t, x, t]⟩,
    ⟨"admit", [t, x, t, t]⟩,
    ⟨"admit", [t, x, t, x]⟩,
    ⟨"append", [x, x, t, t, t, t, t, t, t]⟩,
    ⟨"append", [x, t, t, t, t, t, t, t, t]⟩,
    ⟨"append", [t, x, t, t, t, t, t, t, t]⟩,
    ⟨"seal", [t, t, t]⟩,
    ⟨"rebind", [t, t, t]⟩,
    ⟨"sync", [t, t, t]⟩,
    ⟨"drop", [natV 1, t, t]⟩,
    ⟨"release", [t, t, t, t]⟩,
    ⟨"resolve", [t, t, t, t, t]⟩,
    ⟨"resolve", [t, t, t, t, x]⟩,
    ⟨"run", [natV 1]⟩,
    ⟨"advance", [natV 0, t, t, t, t, t, t]⟩,
    ⟨"advance", [natV 0, t, x, t, t, t, t]⟩,
    ⟨"aggregate", [natV 0, t, t, t]⟩,
    ⟨"close", [natV 0, t, t, t, t, t, t, t]⟩,
    ⟨"close", [natV 1, t, t, t, t, t, t, t]⟩,
    ⟨"close", [natV 0, t, x, t, t, t, t, t]⟩ ]


/-- The card lifecycle vector alphabet: every target column, the member flag
where it writes an exclusion, both outcomes of each integrity fact, and each
kind of contract change with and without a decision. -/
def cardEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let move (c : Nat) (m : Val) : Event := ⟨"move", [enumV c, m]⟩
  [ move 0 x, move 1 x, move 2 x, move 3 x, move 4 x, move 5 t, move 5 x, move 6 t,
    ⟨"rollback", [t, t]⟩, ⟨"rollback", [x, t]⟩, ⟨"rollback", [t, x]⟩,
    ⟨"dispatchMove", [t]⟩, ⟨"dispatchMove", [x]⟩,
    ⟨"recover", [t, t, t, t]⟩, ⟨"recover", [t, t, t, x]⟩,
    ⟨"update", [enumV 0, x]⟩, ⟨"update", [enumV 1, x]⟩, ⟨"update", [enumV 2, x]⟩, ⟨"update", [enumV 2, t]⟩ ]

/-- The execution ledger vector alphabet: each entrance at the revisions and
fences its first writes hold, with a stale revision or fence and a failing fact
for the theorems' refusals. -/
def ledgerEvents : List Event :=
  let t := boolV true
  let x := boolV false
  [ ⟨"claim", [natV 0, t, x, x, t]⟩, ⟨"claim", [natV 1, t, x, x, t]⟩, ⟨"claim", [natV 0, x, x, x, t]⟩,
    ⟨"claim", [natV 0, t, t, x, t]⟩, ⟨"start", [natV 0, t]⟩, ⟨"start", [natV 2, t]⟩, ⟨"start", [natV 0, x]⟩,
    ⟨"reclaim", [natV 2, t, t]⟩, ⟨"reclaim", [natV 2, x, t]⟩, ⟨"recover", [natV 1, t, t]⟩,
    ⟨"recover", [natV 2, t, t]⟩, ⟨"relocate", [natV 1, enumV 2]⟩, ⟨"relocate", [natV 2, enumV 1]⟩,
    ⟨"relocate", [natV 1, enumV 3]⟩, ⟨"relocate", [natV 0, enumV 0]⟩, ⟨"write", [natV 1]⟩,
    ⟨"confirm", [natV 1, t, x]⟩, ⟨"confirm", [natV 1, t, t]⟩, ⟨"confirm", [natV 2, t, x]⟩,
    ⟨"unresolve", [natV 1, natV 1, t, t]⟩, ⟨"unresolve", [natV 1, natV 0, t, x]⟩,
    ⟨"adopt", [natV 1, t, t]⟩, ⟨"takeover", [natV 1, t, t]⟩, ⟨"takeover", [natV 1, x, x]⟩,
    ⟨"rollback", [natV 1, natV 1, t, t, enumV 0, enumV 0]⟩,
    ⟨"rollback", [natV 2, natV 2, t, x, enumV 2, enumV 2]⟩,
    ⟨"field", [natV 1, t]⟩, ⟨"field", [natV 2, x]⟩, ⟨"restore", [natV 1, t, t]⟩, ⟨"restore", [natV 1, x, t]⟩ ]

/-- The server claim vector alphabet: holder requests of the first generations,
of no generation, and of an older one; terminations; takeovers with the
current, an older, and a newer observed generation; and the environment events. -/
def serverEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let g (n : Nat) : List Val := [natV n, natV n]
  [ ⟨"claim", [t]⟩, ⟨"claim", [x]⟩, ⟨"renew", g 1⟩, ⟨"renew", g 0⟩, ⟨"write", g 1 ++ [x]⟩, ⟨"write", g 2 ++ [x]⟩,
    ⟨"write", g 0 ++ [t]⟩, ⟨"write", [natV 0, natV 1, t]⟩,
    ⟨"leave", g 1 ++ [x]⟩, ⟨"leave", g 0 ++ [t]⟩, ⟨"leave", g 1 ++ [t]⟩, ⟨"release", g 1⟩, ⟨"rollback", g 1⟩,
    ⟨"takeover", [natV 1, t, x] ++ g 0⟩, ⟨"takeover", [natV 1, t, t] ++ g 1⟩,
    ⟨"takeover", [natV 0, t, x] ++ g 0⟩, ⟨"takeover", [natV 2, t, x] ++ g 0⟩,
    ⟨"expire", []⟩, ⟨"pending", [t]⟩, ⟨"pending", [x]⟩, ⟨"reconcile", [t]⟩, ⟨"reconcile", [x]⟩,
    ⟨"audit", [natV 1, t]⟩, ⟨"audit", [natV 0, t]⟩, ⟨"audit", [natV 1, x]⟩ ]

/-- The server group lease vector alphabet. Holder requests are `(hdr, g, tg)`:
the first and second generation, a token of another generation, and a header
that did not parse. Claims try a new record, takeovers with and without a
decision, at the observed generation and at a stale one, with and without room
below the last generation, and the reuse of a lease by the same client, by
another client, with a stale token, and with a header that did not parse. -/
def groupLeaseEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let h (hdr : Bool) (g tg : Nat) : List Val := [boolV hdr, natV g, natV tg]
  [ ⟨"claim", h true 0 0 ++ [x, x, natV 0, t, x]⟩, ⟨"claim", h true 0 0 ++ [x, x, natV 1, t, t]⟩,
    ⟨"claim", h true 0 0 ++ [x, x, natV 1, t, x]⟩, ⟨"claim", h true 0 0 ++ [x, x, natV 2, t, t]⟩,
    ⟨"claim", h true 0 0 ++ [x, x, natV 1, x, t]⟩,
    ⟨"claim", h true 1 1 ++ [t, t, natV 0, t, x]⟩, ⟨"claim", h true 2 2 ++ [t, t, natV 0, t, x]⟩,
    ⟨"claim", h true 1 1 ++ [t, x, natV 0, t, x]⟩, ⟨"claim", h false 1 1 ++ [t, t, natV 0, t, x]⟩,
    ⟨"renew", h true 1 1⟩, ⟨"renew", h true 2 2⟩, ⟨"renew", h true 1 2⟩,
    ⟨"observe", h true 1 1 ++ [t]⟩, ⟨"observe", h true 2 2 ++ [t]⟩, ⟨"observe", h true 1 1 ++ [x]⟩,
    ⟨"release", h true 1 1⟩, ⟨"release", h true 2 2⟩,
    ⟨"require", h true 1 1⟩, ⟨"require", h true 2 2⟩, ⟨"require", h false 1 1⟩,
    ⟨"writeAfter", h true 1 1 ++ [t]⟩, ⟨"writeAfter", h true 1 1 ++ [x]⟩,
    ⟨"recoverOwned", h true 1 1 ++ [t, x, t, t, x]⟩, ⟨"recoverOwned", h true 1 1 ++ [x, x, t, t, x]⟩,
    ⟨"recoverOwned", h true 1 1 ++ [t, t, t, x, x]⟩, ⟨"recoverOwned", h true 1 1 ++ [t, x, t, t, t]⟩,
    ⟨"recoverOwned", h true 1 1 ++ [t, t, x, t, x]⟩,
    ⟨"recoverLost", h true 1 1 ++ [t, t, t]⟩, ⟨"recoverLost", h true 1 1 ++ [x, t, t]⟩,
    ⟨"expire", []⟩ ]

/-- The server request vector alphabet. Holder requests are `(holder, cgen, answer, expected, sameNote, window,
card)` for the claim generations 1 and 2; the answers are `accepted`, `done`, `rejected`, and the two statuses that are
no answers (`open`, `superseded`). Creations try a plain request and a proposal at the first generations and
revisions, a proposal flag that disagrees with its revision, and every missing claim fact. -/
def requestEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let n := natV
  let answers : List (List Val) :=
    [ [t, n 1, enumV 2, n 0, t, t, n 1], [t, n 1, enumV 2, n 0, x, t, n 1], [t, n 1, enumV 3, n 0, t, t, n 1],
      [t, n 1, enumV 3, n 0, x, t, n 1], [t, n 1, enumV 4, n 0, t, t, n 1], [t, n 1, enumV 4, n 0, x, t, n 1],
      [t, n 1, enumV 2, n 0, t, x, n 1], [t, n 1, enumV 2, n 0, t, t, n 2], [x, n 1, enumV 2, n 0, t, t, n 1],
      [t, n 2, enumV 2, n 0, t, t, n 1], [t, n 1, enumV 2, n 1, t, t, n 1], [t, n 1, enumV 2, n 2, t, t, n 1],
      [t, n 1, enumV 1, n 0, t, t, n 1], [t, n 1, enumV 5, n 0, t, t, n 1] ]
  [ ⟨"bind", [t, n 1, n 1]⟩, ⟨"bind", [x, n 1, n 1]⟩, ⟨"bind", [t, n 0, n 1]⟩, ⟨"bind", [t, n 1, n 2]⟩,
    ⟨"create", [t, t, t, n 1, n 1, x, n 0]⟩, ⟨"create", [t, t, t, n 1, n 1, t, n 1]⟩,
    ⟨"create", [t, t, t, n 2, n 2, t, n 2]⟩, ⟨"create", [t, t, t, n 1, n 1, t, n 0]⟩,
    ⟨"create", [t, t, t, n 1, n 1, x, n 1]⟩, ⟨"create", [x, t, t, n 1, n 1, x, n 0]⟩,
    ⟨"create", [t, x, t, n 1, n 1, x, n 0]⟩, ⟨"create", [t, t, x, n 1, n 1, x, n 0]⟩,
    ⟨"create", [t, t, t, n 1, n 2, x, n 0]⟩,
    ⟨"recreate", [t, t, t, n 1, n 1, n 1, t]⟩, ⟨"recreate", [t, t, t, n 1, n 1, n 2, t]⟩,
    ⟨"recreate", [t, t, t, n 1, n 1, n 1, x]⟩, ⟨"recreate", [t, t, t, n 2, n 2, n 1, t]⟩,
    ⟨"recreate", [t, t, t, n 2, n 2, n 2, t]⟩, ⟨"recreate", [x, t, t, n 1, n 1, n 1, t]⟩,
    ⟨"recreate", [t, t, x, n 1, n 1, n 1, t]⟩, ⟨"recreate", [t, t, t, n 1, n 2, n 1, t]⟩ ] ++
  answers.map (fun args => ⟨"answerSame", args⟩) ++ answers.map (fun args => ⟨"answer", args⟩) ++
  [ ⟨"supersede", [n 1]⟩, ⟨"supersede", [n 2]⟩, ⟨"supersede", [n 3]⟩,
    ⟨"prepare", [t, t, n 1, t, n 1, t]⟩, ⟨"prepare", [x, t, n 1, t, n 1, t]⟩, ⟨"prepare", [t, x, n 1, t, n 1, t]⟩,
    ⟨"prepare", [t, t, n 2, t, n 1, t]⟩, ⟨"prepare", [t, t, n 1, x, n 1, t]⟩, ⟨"prepare", [t, t, n 1, t, n 2, t]⟩,
    ⟨"prepare", [t, t, n 1, t, n 1, x]⟩, ⟨"prepare", [t, t, n 2, t, n 2, t]⟩,
    ⟨"complete", [t, t, t, n 1]⟩, ⟨"complete", [x, t, t, n 1]⟩, ⟨"complete", [t, x, t, n 1]⟩,
    ⟨"complete", [t, t, x, n 1]⟩, ⟨"complete", [t, t, t, n 2]⟩ ]

/-- The server delivery vector alphabet. Publishes are `(holder, exec, baseline, gen, cgen, rev, card)` for the
first generations and revisions, with every fact failing in turn; guards are `(holder, exec, cgen, card)`. -/
def deliveryEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let n := natV
  [ ⟨"publish", [t, t, x, n 1, n 1, n 1, n 1]⟩, ⟨"publish", [t, t, t, n 1, n 1, n 1, n 1]⟩,
    ⟨"publish", [t, t, x, n 2, n 2, n 1, n 1]⟩, ⟨"publish", [t, t, x, n 1, n 1, n 2, n 2]⟩,
    ⟨"publish", [x, t, x, n 1, n 1, n 1, n 1]⟩, ⟨"publish", [t, x, x, n 1, n 1, n 1, n 1]⟩,
    ⟨"publish", [t, t, x, n 1, n 2, n 1, n 1]⟩, ⟨"publish", [t, t, x, n 1, n 1, n 1, n 2]⟩,
    ⟨"publish", [t, t, x, n 0, n 0, n 1, n 1]⟩, ⟨"publish", [t, t, x, n 1, n 1, n 0, n 0]⟩,
    ⟨"other", []⟩,
    ⟨"guard", [t, t, n 1, n 1]⟩, ⟨"guard", [t, t, n 2, n 1]⟩, ⟨"guard", [t, t, n 1, n 2]⟩,
    ⟨"guard", [t, t, n 2, n 2]⟩, ⟨"guard", [x, t, n 1, n 1]⟩, ⟨"guard", [t, x, n 1, n 1]⟩ ]

/-- The dispatch vector alphabet. Authorization pairs: `(1, 1)` is the first
round at its first epoch, `(1, 2)` its reauthorization, `(2, 1)` the next round,
and `(0, 0)` the empty pair. Each event is tried with every fact holding and with
one fact failing; the enum arguments try a working, a review, and another column. -/
def dispatchEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let a (r e : Nat) : List Val := [natV r, natV e]
  [ ⟨"prepare", [enumV 0, enumV 0, t, t]⟩, ⟨"prepare", [enumV 2, enumV 1, t, t]⟩,
    ⟨"prepare", [enumV 0, enumV 2, t, t]⟩, ⟨"prepare", [enumV 1, enumV 0, x, t]⟩,
    ⟨"prepare", [enumV 0, enumV 0, t, x]⟩,
    ⟨"retry", [t]⟩, ⟨"retry", [x]⟩,
    ⟨"attempt", a 1 1 ++ [t, t, t, t]⟩, ⟨"attempt", a 2 1 ++ [t, t, t, t]⟩, ⟨"attempt", a 1 1 ++ [t, t, t, x]⟩,
    ⟨"attempt", a 1 1 ++ [t, x, t, t]⟩, ⟨"attempt", a 1 1 ++ [x, t, t, t]⟩,
    ⟨"reauthorize", a 1 1 ++ [t, t, t, t, t]⟩, ⟨"reauthorize", a 1 1 ++ [t, t, t, x, x]⟩,
    ⟨"reauthorize", a 1 1 ++ [t, t, t, x, t]⟩, ⟨"reauthorize", a 1 1 ++ [t, x, t, t, t]⟩,
    ⟨"reauthorize", a 1 2 ++ [t, t, t, t, t]⟩,
    ⟨"accept", a 1 1 ++ [t, t, t]⟩, ⟨"accept", a 1 1 ++ [t, t, x]⟩, ⟨"accept", a 1 2 ++ [t, t, t]⟩,
    ⟨"accept", a 1 1 ++ [x, t, t]⟩,
    ⟨"complete", a 1 1 ++ [t, t, x, t, t, t, x, enumV 0]⟩, ⟨"complete", a 1 1 ++ [t, t, t, t, t, t, x, enumV 0]⟩,
    ⟨"complete", a 1 1 ++ [t, t, x, t, t, t, t, enumV 1]⟩, ⟨"complete", a 1 1 ++ [t, t, x, t, t, t, x, enumV 2]⟩,
    ⟨"complete", a 1 2 ++ [t, t, x, t, t, t, x, enumV 0]⟩, ⟨"complete", a 1 1 ++ [t, t, x, x, t, t, x, enumV 0]⟩,
    ⟨"end", a 1 1 ++ [t, t, t, t, t]⟩, ⟨"end", a 1 1 ++ [t, t, t, t, x]⟩, ⟨"end", a 1 1 ++ [t, t, t, x, t]⟩,
    ⟨"end", a 1 1 ++ [t, t, t, x, x]⟩, ⟨"end", a 2 1 ++ [t, t, t, t, t]⟩, ⟨"end", a 1 1 ++ [t, t, x, t, t]⟩,
    ⟨"cancel", [t, t, t]⟩, ⟨"cancel", [x, t, t]⟩, ⟨"cancel", [t, x, t]⟩,
    ⟨"retain", a 1 1 ++ [t, t, t, t]⟩, ⟨"retain", a 1 2 ++ [t, t, t, t]⟩, ⟨"retain", a 1 1 ++ [x, t, t, t]⟩,
    ⟨"exec", a 1 1 ++ [t, t, x]⟩, ⟨"exec", a 1 1 ++ [t, t, t]⟩, ⟨"exec", a 1 1 ++ [t, x, x]⟩,
    ⟨"exec", a 0 0 ++ [t, t, x]⟩, ⟨"exec", a 1 2 ++ [t, t, x]⟩,
    -- The wrap-up grant: a stopped, a reclaimed with and without a decision, and
    -- another exit, in a working and another column, with a stale exit, a stale
    -- revision, a failed integrity fact, and the epoch the grant raises to.
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 0, x, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 2, t, t, enumV 0, x, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 1, t, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 1, x, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 2, t, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 0, x, x, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [x, t, enumV 0, t, t, enumV 0, x, t, t]⟩,
    ⟨"authorizeWrapUp", a 1 1 ++ [t, t, enumV 0, t, t, enumV 0, x, t, x]⟩,
    ⟨"authorizeWrapUp", a 1 2 ++ [t, t, enumV 0, t, t, enumV 0, x, t, t]⟩,
    ⟨"attempt", a 1 2 ++ [t, t, t, t]⟩,
    -- The full-execution write and the record writes: a contract decision, a spec
    -- append that does and does not append, and a record path that is and is not
    -- free of other content.
    ⟨"full", []⟩,
    ⟨"wrapUpWrite", [t, t, t, x, x]⟩, ⟨"wrapUpWrite", [x, t, t, x, x]⟩, ⟨"wrapUpWrite", [x, t, x, t, t]⟩,
    ⟨"wrapUpWrite", [x, x, x, t, t]⟩, ⟨"wrapUpWrite", [x, x, x, x, t]⟩,
    -- The settlement of a completed binding from the three sources.
    ⟨"settle", a 1 1 ++ [t, enumV 0, t]⟩, ⟨"settle", a 1 1 ++ [t, enumV 1, t]⟩, ⟨"settle", a 1 1 ++ [t, enumV 2, t]⟩,
    ⟨"terminateSettle", [t, t, t]⟩, ⟨"terminateSettle", [x, t, t]⟩,
    ⟨"takeoverSettle", a 1 1 ++ [t, t, t, t]⟩, ⟨"takeoverSettle", a 1 2 ++ [t, t, t, t]⟩ ]

/-- The review run vector alphabet: each event with every fact holding and with
one fact failing, the phase updates the driver makes and the odd ones the entrance
accepts, the finalizations of an `ok` run (valid, invalid report, launch status
other than started, empty report, missing originals), a failure with and without
a reason, a launch failure, and an interruption. -/
def runEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let upd (p l e : Nat) (z s : Bool) : Event := ⟨"update", [enumV p, enumV l, enumV e, boolV z, boolV s]⟩
  let fin (l e : Nat) (z r why inv cm : Bool) : Event :=
    ⟨"finalize", [enumV l, enumV e, boolV z, boolV r, boolV why, boolV inv, boolV cm, t]⟩
  [ ⟨"create", [t, t, t, t]⟩, ⟨"create", [x, t, t, t]⟩, ⟨"create", [t, t, t, x]⟩,
    ⟨"retry", [t, t, t, t, t]⟩, ⟨"retry", [t, t, x, t, t]⟩, ⟨"retry", [t, t, t, x, t]⟩,
    upd 1 0 0 true true, upd 2 1 0 true true, upd 3 2 0 true true, upd 4 2 0 true true, upd 6 2 1 true true,
    upd 0 0 0 true true, upd 7 3 5 false true, upd 3 2 0 true false,
    ⟨"storePrompt", [t]⟩, ⟨"storePrompt", [x]⟩, ⟨"storeContract", [t]⟩, ⟨"storeEvidence", [t]⟩,
    fin 2 1 true true false false true, fin 2 1 true true false true true, fin 1 1 true true false false true,
    fin 2 1 true false false false true, fin 2 3 false false true false true, fin 2 3 false false false false true,
    fin 0 2 false false true false true, fin 1 4 false false true false true, fin 2 1 true true false false false,
    ⟨"finalize", [enumV 2, enumV 1, t, t, x, x, t, x]⟩,
    ⟨"stage", [t, t, x, t, t]⟩, ⟨"stage", [t, t, t, t, t]⟩, ⟨"stage", [t, x, x, t, t]⟩, ⟨"stage", [x, t, x, t, t]⟩,
    ⟨"stage", [t, t, x, x, t]⟩, ⟨"stage", [t, t, x, t, x]⟩,
    ⟨"skip", [t, t, t]⟩, ⟨"skip", [t, t, x]⟩, ⟨"skip", [t, x, t]⟩,
    ⟨"verify", [t, t, t, t]⟩, ⟨"verify", [t, t, t, x]⟩, ⟨"verify", [t, t, x, t]⟩,
    ⟨"passSkipped", [t, t]⟩, ⟨"passSkipped", [t, x]⟩ ]

/-- The review findings vector alphabet: a read-in of every schema with a
structured, an unstructured, a block-marked, and an unconsumable report, the
close of the batch, and each source and assignment event with every fact holding
and with one fact failing. -/
def findingsEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let opn (s : Nat) (st fe co : Val) : Event := ⟨"open", [enumV s, st, fe, co]⟩
  [ opn 0 x x t, opn 0 x t t, opn 0 t t t, opn 1 x x t, opn 1 t t t, opn 2 x x t, opn 2 x t t, opn 2 t t t,
    opn 2 x x x, opn 2 t x t,
    ⟨"close", []⟩,
    ⟨"interpret", [t, t, t]⟩, ⟨"interpret", [t, x, t]⟩, ⟨"interpret", [x, t, t]⟩, ⟨"interpret", [t, t, x]⟩,
    ⟨"mapLegacy", [t, t, t]⟩, ⟨"mapLegacy", [t, x, t]⟩, ⟨"mapLegacy", [x, t, t]⟩, ⟨"mapLegacy", [t, t, x]⟩,
    ⟨"assign", [t, t, t, t, t, t, t]⟩, ⟨"assign", [x, t, t, t, t, t, t]⟩, ⟨"assign", [t, x, t, t, t, t, t]⟩,
    ⟨"assign", [t, t, x, t, t, t, t]⟩, ⟨"assign", [t, t, t, x, t, t, t]⟩, ⟨"assign", [t, t, t, t, x, t, t]⟩,
    ⟨"assign", [t, t, t, t, t, x, t]⟩, ⟨"assign", [t, t, t, t, t, t, x]⟩ ]

/-- The facts of a record event with the fact at `i` (or none) failing. -/
def factsWith (n : Nat) (i : Option Nat) : List Val :=
  (List.range n).map fun k => boolV (i != some k)

/-- The review disposition vector alphabet: the assignment and the close, a
submission with every status for a gate and another tier, with a waiver for and
against a security role, with every relation of the record to the chain, and
with each fact failing; and a withdrawal with every relation to the chain and
each fact failing. -/
def dispositionEvents : List Event :=
  let sub (st du rf : Nat) (g se : Bool) (i : Option Nat) : Event :=
    ⟨"submit", [enumV st, enumV du, enumV rf, boolV g, boolV se] ++ factsWith 12 i⟩
  let wd (du rf : Nat) (i : Option Nat) : Event := ⟨"withdraw", [enumV du, enumV rf] ++ factsWith 12 i⟩
  [ ⟨"assign", []⟩, ⟨"close", []⟩,
    sub 0 0 0 true true none, sub 1 0 0 true true none, sub 1 0 0 false true none, sub 2 0 0 false true none,
    sub 3 0 0 false true none, sub 4 0 0 true true none, sub 4 0 0 true false none, sub 5 0 0 true true none,
    sub 5 0 0 false true none, sub 6 0 0 true true none, sub 7 0 0 true true none,
    sub 1 0 1 true true none, sub 1 0 2 true true none, sub 1 1 1 true true none, sub 1 2 1 true true none,
    sub 1 1 0 true true none ] ++
  (List.range 12).map (fun i => sub 1 0 0 true true (some i)) ++
  [ wd 0 0 none, wd 0 1 none, wd 0 2 none, wd 1 1 none, wd 2 1 none ] ++
  (List.range 12).map (fun i => wd 0 0 (some i))


/-- The coordinator checkpoint vector alphabet. Revisions and epochs are 0 to 2;
each event is tried with every fact holding and with one fact failing: a claim
of a new checkpoint, of an existing one at the right and at wrong revisions, with
the same token, without room, with other members, and as a replay; a reconcile
that changed and one that did not, with each count of blocked members, at the
right and at the previous revision as a retry, with a wrong authority, a
request-level failure, and each shell fact failing; and a recovery with each
fact failing. -/
def coordinatorEvents : List Event :=
  let claim (er ee : Nat) (sm ts rm mo mb : Bool) : Event :=
    ⟨"claim", [natV er, natV ee, boolV sm, boolV ts, boolV rm, boolV mo, boolV mb]⟩
  let rcn (os ts : Bool) (ea er : Nat) (sm mo mm rc rf ch : Bool) (ba : Nat) (rm : Bool) : Event :=
    ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc, boolV rf,
      boolV ch, natV ba, boolV rm]⟩
  let rcv (er ee : Nat) (rm kn mb ex mo : Bool) : Event :=
    ⟨"recover", [natV er, natV ee, boolV rm, boolV kn, boolV mb, boolV ex, boolV mo]⟩
  [ claim 0 0 false false true true true, claim 1 1 false false true true true, claim 2 1 false false true true true,
    claim 2 2 false false true true true, claim 1 1 false true true true true, claim 1 1 false false false true true,
    claim 1 1 false false true false true, claim 1 1 false false true true false, claim 0 0 true false true true true,
    claim 1 1 true false true true true, claim 0 1 false false true true true,
    rcn true true 1 1 false true true true false true 1 true, rcn true true 1 1 false true true true false false 0 true,
    rcn true true 1 2 false true true true false true 2 true, rcn true true 1 2 false true true true false true 0 true,
    rcn true true 1 0 true true true true false true 1 true, rcn true true 1 0 false true true true false true 1 true,
    rcn false true 1 1 false true true true false true 1 true, rcn true false 1 1 false true true true false true 1 true,
    rcn true true 2 1 false true true true false true 1 true, rcn true true 1 1 false false true true false true 1 true,
    rcn true true 1 1 false true false true false true 1 true, rcn true true 1 1 false true true false false true 1 true,
    rcn true true 1 1 false true true true true true 1 true, rcn true true 1 1 false true true true false true 1 false,
    rcv 1 1 true true true true true, rcv 2 1 true true true true true, rcv 2 2 true true true true true,
    rcv 0 0 true true true true true, rcv 1 1 false true true true true, rcv 1 1 true false true true true,
    rcv 1 1 true true false true true, rcv 1 1 true true true false true, rcv 1 1 true true true true false ]

/-- The coordinator member vector alphabet. Columns: 0 early, 1 working, 2 review,
3 done, 4 archived, 5 trash. The walking events are tried with the revision and
the chain position kept, advanced, and moved back, in a column of each kind, with
and without the confirmed cycle, a failing fact, and a missing handoff; the
recoveries with a card in every kind of column. -/
def memberEvents : List Event :=
  let acc (nc nr nt nd : Nat) (cs ok mi : Bool) : Event :=
    ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩
  let cnf (nc nr : Nat) (cs : Bool) : Event := ⟨"acceptConfirmed", [enumV nc, natV nr, boolV cs]⟩
  let trm (nc nr nt nd : Nat) (hr cs ok mi : Bool) : Event :=
    ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok, boolV mi]⟩
  let rst (cc nr nt nd : Nat) (ok : Bool) : Event := ⟨"recoverReset", [enumV cc, natV nr, natV nt, natV nd, boolV ok]⟩
  let rcf (cc nr : Nat) (tr : Bool) (nt : Nat) (dr : Bool) (nd : Nat) : Event :=
    ⟨"recoverConfirm", [enumV cc, natV nr, boolV tr, natV nt, boolV dr, natV nd]⟩
  [ acc 1 1 1 0 false true false, acc 2 2 1 1 false true false, acc 3 2 2 1 false true false,
    acc 3 3 2 1 true true false, acc 1 1 0 0 false true false, acc 1 0 1 0 false true false,
    acc 1 1 1 0 false false false, acc 1 1 1 0 false true true, acc 4 2 1 1 false true false,
    acc 0 1 1 0 false true false,
    cnf 3 2 true, cnf 4 3 true, cnf 3 2 false, cnf 1 2 true, cnf 3 0 true,
    trm 4 2 2 1 true false true false, trm 5 3 2 1 false false true false, trm 4 2 2 1 true true true false,
    trm 3 2 2 1 true false true false, trm 4 2 2 1 true false true true, trm 4 2 2 1 true false false false,
    trm 4 2 0 1 true false true false, trm 4 0 2 1 true false true false,
    ⟨"block", []⟩,
    rst 1 2 2 1 true, rst 2 3 2 1 true, rst 3 2 2 1 true, rst 0 2 2 1 true, rst 1 2 2 1 false,
    rcf 3 2 true 2 true 1, rcf 4 3 false 0 false 0, rcf 5 3 true 2 false 1, rcf 1 2 true 2 true 1,
    rcf 0 2 true 2 true 1 ]

def entries : List Entry := [
  { machine := Machines.exampleMachine
    source := "KanderFormal/Machines/Example.lean"
    vectors := { depth := 4, paramSets := [[("maxEpoch", 0)], [("maxEpoch", 1)], [("maxEpoch", 2)]],
                 natDomain := [0, 1, 2, 3] }
    theorems := [
      ``KanderFormal.Proofs.Example.wellTyped,
      ``KanderFormal.Proofs.Example.init,
      ``KanderFormal.Proofs.Example.step_inv,
      ``KanderFormal.Proofs.Example.reachable_inv,
      ``KanderFormal.Proofs.Example.reachable_sealed_passed
    ] },
  { machine := Machines.ReviewObligation.reviewObligationMachine
    source := "KanderFormal/Machines/ReviewObligation.lean"
    vectors := { depth := 5, paramSets := [[("maxBatch", 2)]], natDomain := [], events := obligationEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewObligation.wellTyped,
      ``KanderFormal.Proofs.ReviewObligation.step_enc,
      ``KanderFormal.Proofs.ReviewObligation.debt_safety,
      ``KanderFormal.Proofs.ReviewObligation.evidence_floor,
      ``KanderFormal.Proofs.ReviewObligation.release_once,
      ``KanderFormal.Proofs.ReviewObligation.reachable_inv,
      ``KanderFormal.Proofs.ReviewObligation.counter_dropReleased,
      ``KanderFormal.Proofs.ReviewObligation.counter_releaseDrop,
      ``KanderFormal.Proofs.ReviewObligation.counter_staleClose,
      ``KanderFormal.Proofs.ReviewObligation.witness_members,
      ``KanderFormal.Proofs.ReviewObligation.witness_twoResets,
      ``KanderFormal.Proofs.ReviewObligation.witness_releasedRole,
      ``KanderFormal.Proofs.ReviewObligation.completes_reachable
    ] },
  { machine := Machines.ReviewPlan.reviewPlanMachine
    source := "KanderFormal/Machines/ReviewPlan.lean"
    vectors := { depth := 5, paramSets := [[("maxBatch", 2)]], natDomain := [], events := planEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewPlan.wellTyped,
      ``KanderFormal.Proofs.ReviewPlan.integrity_refused,
      ``KanderFormal.Proofs.ReviewPlan.sealed_append,
      ``KanderFormal.Proofs.ReviewPlan.sealed_seal,
      ``KanderFormal.Proofs.ReviewPlan.sealed_admit,
      ``KanderFormal.Proofs.ReviewPlan.reachable_inv
    ] },
  { machine := Machines.CardLifecycle.cardLifecycleMachine
    source := "KanderFormal/Machines/CardLifecycle.lean"
    vectors := { depth := 5, paramSets := [[("maxDecision", 2)]], natDomain := [], events := cardEvents }
    theorems := [
      ``KanderFormal.Proofs.CardLifecycle.wellTyped,
      ``KanderFormal.Proofs.CardLifecycle.move_accepted_iff,
      ``KanderFormal.Proofs.CardLifecycle.pick_accepted_iff,
      ``KanderFormal.Proofs.CardLifecycle.rollback_spec,
      ``KanderFormal.Proofs.CardLifecycle.dispatchMove_spec,
      ``KanderFormal.Proofs.CardLifecycle.recover_spec,
      ``KanderFormal.Proofs.CardLifecycle.frozen_change,
      ``KanderFormal.Proofs.CardLifecycle.reachable_frozen_change,
      ``KanderFormal.Proofs.CardLifecycle.decisions_only_update,
      ``KanderFormal.Proofs.CardLifecycle.init,
      ``KanderFormal.Proofs.CardLifecycle.step_inv,
      ``KanderFormal.Proofs.CardLifecycle.reachable_inv
    ] },
  { machine := Machines.ExecutionLedger.executionLedgerMachine
    source := "KanderFormal/Machines/ExecutionLedger.lean"
    vectors := { depth := 4, paramSets := [[("maxSeq", 3), ("maxRev", 4)]], natDomain := [], events := ledgerEvents }
    theorems := [
      ``KanderFormal.Proofs.ExecutionLedger.wellTyped,
      ``KanderFormal.Proofs.ExecutionLedger.stale_revision_refused,
      ``KanderFormal.Proofs.ExecutionLedger.unresolve_stale,
      ``KanderFormal.Proofs.ExecutionLedger.stale_fence_refused,
      ``KanderFormal.Proofs.ExecutionLedger.init,
      ``KanderFormal.Proofs.ExecutionLedger.step_inv,
      ``KanderFormal.Proofs.ExecutionLedger.reachable_inv
    ] },
  { machine := Machines.ServerClaim.serverClaimMachine
    source := "KanderFormal/Machines/ServerClaim.lean"
    vectors := { depth := 5, paramSets := [[("maxGen", 3)]], natDomain := [], events := serverEvents }
    theorems := [
      ``KanderFormal.Proofs.ServerClaim.wellTyped,
      ``KanderFormal.Proofs.ServerClaim.holder_unique,
      ``KanderFormal.Proofs.ServerClaim.reachable_valid_held,
      ``KanderFormal.Proofs.ServerClaim.gen_monotone,
      ``KanderFormal.Proofs.ServerClaim.stale_holder_refused,
      ``KanderFormal.Proofs.ServerClaim.stale_terminate_refused,
      ``KanderFormal.Proofs.ServerClaim.stale_takeover_refused,
      ``KanderFormal.Proofs.ServerClaim.audit_iff,
      ``KanderFormal.Proofs.ServerClaim.audit_result,
      ``KanderFormal.Proofs.ServerClaim.audit_refused_valid,
      ``KanderFormal.Proofs.ServerClaim.audit_refused_stale,
      ``KanderFormal.Proofs.ServerClaim.init,
      ``KanderFormal.Proofs.ServerClaim.step_inv,
      ``KanderFormal.Proofs.ServerClaim.reachable_inv
    ] },
  { machine := Machines.ServerGroupLease.serverGroupLeaseMachine
    source := "KanderFormal/Machines/ServerGroupLease.lean"
    vectors := { depth := 5, paramSets := [[("maxGen", 3)]], natDomain := [], events := groupLeaseEvents }
    theorems := [
      ``KanderFormal.Proofs.ServerGroupLease.wellTyped,
      ``KanderFormal.Proofs.ServerGroupLease.require_iff,
      ``KanderFormal.Proofs.ServerGroupLease.holder_unique,
      ``KanderFormal.Proofs.ServerGroupLease.holder_required,
      ``KanderFormal.Proofs.ServerGroupLease.gen_monotone,
      ``KanderFormal.Proofs.ServerGroupLease.gen_change,
      ``KanderFormal.Proofs.ServerGroupLease.claim_reuse_keeps,
      ``KanderFormal.Proofs.ServerGroupLease.claim_same_exec_refused,
      ``KanderFormal.Proofs.ServerGroupLease.claim_reuse_needs_holder,
      ``KanderFormal.Proofs.ServerGroupLease.claim_takeover_spec,
      ``KanderFormal.Proofs.ServerGroupLease.claim_takeover_result,
      ``KanderFormal.Proofs.ServerGroupLease.claim_room_refused,
      ``KanderFormal.Proofs.ServerGroupLease.claim_decision_needed,
      ``KanderFormal.Proofs.ServerGroupLease.claim_valid_refused,
      ``KanderFormal.Proofs.ServerGroupLease.observe_stopped_spec,
      ``KanderFormal.Proofs.ServerGroupLease.observe_other_keeps,
      ``KanderFormal.Proofs.ServerGroupLease.release_spec,
      ``KanderFormal.Proofs.ServerGroupLease.ended_refuses_holder,
      ``KanderFormal.Proofs.ServerGroupLease.write_after_spec,
      ``KanderFormal.Proofs.ServerGroupLease.write_after_members_refused,
      ``KanderFormal.Proofs.ServerGroupLease.stale_refused,
      ``KanderFormal.Proofs.ServerGroupLease.recover_owned_spec,
      ``KanderFormal.Proofs.ServerGroupLease.recover_owned_refused,
      ``KanderFormal.Proofs.ServerGroupLease.recover_lost_spec,
      ``KanderFormal.Proofs.ServerGroupLease.init,
      ``KanderFormal.Proofs.ServerGroupLease.step_inv,
      ``KanderFormal.Proofs.ServerGroupLease.reachable_inv,
      ``KanderFormal.Proofs.ServerGroupLease.reachable_valid_gen
    ] },
  { machine := Machines.ServerRequest.serverRequestMachine
    source := "KanderFormal/Machines/ServerRequest.lean"
    vectors := { depth := 4, paramSets := [[("maxGen", 2), ("maxRev", 3)]], natDomain := [], events := requestEvents }
    theorems := [
      ``KanderFormal.Proofs.ServerRequest.wellTyped,
      ``KanderFormal.Proofs.ServerRequest.create_spec,
      ``KanderFormal.Proofs.ServerRequest.create_result,
      ``KanderFormal.Proofs.ServerRequest.create_refused,
      ``KanderFormal.Proofs.ServerRequest.bind_spec,
      ``KanderFormal.Proofs.ServerRequest.bind_refused,
      ``KanderFormal.Proofs.ServerRequest.flow,
      ``KanderFormal.Proofs.ServerRequest.settled_frozen,
      ``KanderFormal.Proofs.ServerRequest.answer_same_spec,
      ``KanderFormal.Proofs.ServerRequest.answer_same_keeps,
      ``KanderFormal.Proofs.ServerRequest.answer_same_accepted,
      ``KanderFormal.Proofs.ServerRequest.answer_spec,
      ``KanderFormal.Proofs.ServerRequest.answer_result,
      ``KanderFormal.Proofs.ServerRequest.settled_answer,
      ``KanderFormal.Proofs.ServerRequest.answer_exclusive,
      ``KanderFormal.Proofs.ServerRequest.answer_bumps,
      ``KanderFormal.Proofs.ServerRequest.answer_expected,
      ``KanderFormal.Proofs.ServerRequest.answer_accept_proposal,
      ``KanderFormal.Proofs.ServerRequest.answer_holder,
      ``KanderFormal.Proofs.ServerRequest.superseded_unanswerable,
      ``KanderFormal.Proofs.ServerRequest.stale_unanswerable,
      ``KanderFormal.Proofs.ServerRequest.non_holder_unanswerable,
      ``KanderFormal.Proofs.ServerRequest.supersede_spec,
      ``KanderFormal.Proofs.ServerRequest.supersede_result,
      ``KanderFormal.Proofs.ServerRequest.supersede_others,
      ``KanderFormal.Proofs.ServerRequest.supersede_then_unanswerable,
      ``KanderFormal.Proofs.ServerRequest.recreate_spec,
      ``KanderFormal.Proofs.ServerRequest.recreate_keeps,
      ``KanderFormal.Proofs.ServerRequest.recreate_refused,
      ``KanderFormal.Proofs.ServerRequest.proposal_immutable,
      ``KanderFormal.Proofs.ServerRequest.prepare_spec,
      ``KanderFormal.Proofs.ServerRequest.prepare_result,
      ``KanderFormal.Proofs.ServerRequest.prepare_bumps_accepted,
      ``KanderFormal.Proofs.ServerRequest.prepare_binds,
      ``KanderFormal.Proofs.ServerRequest.prepare_refused,
      ``KanderFormal.Proofs.ServerRequest.complete_spec,
      ``KanderFormal.Proofs.ServerRequest.complete_result,
      ``KanderFormal.Proofs.ServerRequest.complete_refused,
      ``KanderFormal.Proofs.ServerRequest.init,
      ``KanderFormal.Proofs.ServerRequest.step_inv,
      ``KanderFormal.Proofs.ServerRequest.reachable_inv,
      ``KanderFormal.Proofs.ServerRequest.reachable_revision,
      ``KanderFormal.Proofs.ServerRequest.reachable_answered,
      ``KanderFormal.Proofs.ServerRequest.reachable_proposal_bound
    ] },
  { machine := Machines.ServerDelivery.serverDeliveryMachine
    source := "KanderFormal/Machines/ServerDelivery.lean"
    vectors := { depth := 5, paramSets := [[("maxGen", 2), ("maxRev", 2)]], natDomain := [], events := deliveryEvents }
    theorems := [
      ``KanderFormal.Proofs.ServerDelivery.wellTyped,
      ``KanderFormal.Proofs.ServerDelivery.publish_spec,
      ``KanderFormal.Proofs.ServerDelivery.publish_result,
      ``KanderFormal.Proofs.ServerDelivery.publish_refused,
      ``KanderFormal.Proofs.ServerDelivery.publish_purpose,
      ``KanderFormal.Proofs.ServerDelivery.keeps_query,
      ``KanderFormal.Proofs.ServerDelivery.publish_replaces_keeps,
      ``KanderFormal.Proofs.ServerDelivery.publish_first_keeps_nothing,
      ``KanderFormal.Proofs.ServerDelivery.publish_latest,
      ``KanderFormal.Proofs.ServerDelivery.other_spec,
      ``KanderFormal.Proofs.ServerDelivery.guard_spec,
      ``KanderFormal.Proofs.ServerDelivery.guard_keeps,
      ``KanderFormal.Proofs.ServerDelivery.guard_refused,
      ``KanderFormal.Proofs.ServerDelivery.guard_after_publish,
      ``KanderFormal.Proofs.ServerDelivery.baseline_never_guards,
      ``KanderFormal.Proofs.ServerDelivery.init,
      ``KanderFormal.Proofs.ServerDelivery.step_inv,
      ``KanderFormal.Proofs.ServerDelivery.reachable_inv,
      ``KanderFormal.Proofs.ServerDelivery.reachable_bound,
      ``KanderFormal.Proofs.ServerDelivery.reachable_latest_record
    ] },
  { machine := Machines.ServerProposal.serverProposalMachine
    source := "KanderFormal/Machines/ServerProposal.lean"
    vectors := { depth := 5, paramSets := [[("maxCount", 2)]], natDomain := [] }
    theorems := [
      ``KanderFormal.Proofs.ServerProposal.wellTyped,
      ``KanderFormal.Proofs.ServerProposal.admit_spec,
      ``KanderFormal.Proofs.ServerProposal.admit_result,
      ``KanderFormal.Proofs.ServerProposal.admit_once,
      ``KanderFormal.Proofs.ServerProposal.admit_repeat_exclusive,
      ``KanderFormal.Proofs.ServerProposal.repeat_spec,
      ``KanderFormal.Proofs.ServerProposal.lookup_spec,
      ``KanderFormal.Proofs.ServerProposal.lookup_ambiguous,
      ``KanderFormal.Proofs.ServerProposal.ambiguous_not_admitted,
      ``KanderFormal.Proofs.ServerProposal.init,
      ``KanderFormal.Proofs.ServerProposal.step_inv,
      ``KanderFormal.Proofs.ServerProposal.reachable_inv,
      ``KanderFormal.Proofs.ServerProposal.reachable_unique
    ] },
  { machine := Machines.Dispatch.dispatchMachine
    source := "KanderFormal/Machines/Dispatch.lean"
    vectors := { depth := 4, paramSets := [[("maxRound", 3), ("maxEpoch", 3)]], natDomain := [],
                 events := dispatchEvents }
    theorems := [
      ``KanderFormal.Proofs.Dispatch.wellTyped,
      ``KanderFormal.Proofs.Dispatch.prepare_single,
      ``KanderFormal.Proofs.Dispatch.pair_monotone,
      ``KanderFormal.Proofs.Dispatch.prepare_strict,
      ``KanderFormal.Proofs.Dispatch.reauthorize_strict,
      ``KanderFormal.Proofs.Dispatch.fence_refused,
      ``KanderFormal.Proofs.Dispatch.accept_change,
      ``KanderFormal.Proofs.Dispatch.complete_change,
      ``KanderFormal.Proofs.Dispatch.complete_kind,
      ``KanderFormal.Proofs.Dispatch.accept_replay,
      ``KanderFormal.Proofs.Dispatch.complete_replay,
      ``KanderFormal.Proofs.Dispatch.terminal_stable,
      ``KanderFormal.Proofs.Dispatch.terminal_refused,
      ``KanderFormal.Proofs.Dispatch.end_spec,
      ``KanderFormal.Proofs.Dispatch.end_once,
      ``KanderFormal.Proofs.Dispatch.release_once,
      ``KanderFormal.Proofs.Dispatch.reauthorize_spec,
      ``KanderFormal.Proofs.Dispatch.cycle_refused,
      ``KanderFormal.Proofs.Dispatch.init,
      ``KanderFormal.Proofs.Dispatch.step_inv,
      ``KanderFormal.Proofs.Dispatch.reachable_inv,
      ``KanderFormal.Proofs.Dispatch.reachable_unbound,
      ``KanderFormal.Proofs.Dispatch.reachable_receipts,
      ``KanderFormal.Proofs.Dispatch.reachable_unbound_settled,
      ``KanderFormal.Proofs.Dispatch.authorize_spec,
      ``KanderFormal.Proofs.Dispatch.grant_refused,
      ``KanderFormal.Proofs.Dispatch.wrapUpWrite_spec,
      ``KanderFormal.Proofs.Dispatch.authorize_fences_previous,
      ``KanderFormal.Proofs.Dispatch.grant_complete_done,
      ``KanderFormal.Proofs.Dispatch.authorize_complete_done,
      ``KanderFormal.Proofs.Dispatch.settle_spec,
      ``KanderFormal.Proofs.Dispatch.terminate_settle_spec,
      ``KanderFormal.Proofs.Dispatch.takeover_settle_spec,
      ``KanderFormal.Proofs.Dispatch.settle_once,
      ``KanderFormal.Proofs.Dispatch.settled_fences,
      ``KanderFormal.Proofs.Dispatch.release_settle_exclusive,
      ``KanderFormal.Proofs.Dispatch.reachable_grant_kind,
      ``KanderFormal.Proofs.Dispatch.cycle_refused_grant
    ] },
  { machine := Machines.ReviewRun.reviewRunMachine
    source := "KanderFormal/Machines/ReviewRun.lean"
    vectors := { depth := 4, paramSets := [[]], natDomain := [], events := runEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewRun.wellTyped,
      ``KanderFormal.Proofs.ReviewRun.update_refused_finalized,
      ``KanderFormal.Proofs.ReviewRun.finalize_refused_finalized,
      ``KanderFormal.Proofs.ReviewRun.finalized_frozen,
      ``KanderFormal.Proofs.ReviewRun.archive_sequences_accepted,
      ``KanderFormal.Proofs.ReviewRun.finalize_ok,
      ``KanderFormal.Proofs.ReviewRun.reachable_ok_facts,
      ``KanderFormal.Proofs.ReviewRun.store_only_prepared,
      ``KanderFormal.Proofs.ReviewRun.artifacts_only_prepared,
      ``KanderFormal.Proofs.ReviewRun.originals_only_finalize,
      ``KanderFormal.Proofs.ReviewRun.retry_spec,
      ``KanderFormal.Proofs.ReviewRun.retry_refused,
      ``KanderFormal.Proofs.ReviewRun.create_refused,
      ``KanderFormal.Proofs.ReviewRun.stage_spec,
      ``KanderFormal.Proofs.ReviewRun.skip_spec,
      ``KanderFormal.Proofs.ReviewRun.publish_once,
      ``KanderFormal.Proofs.ReviewRun.publish_needs_finalized,
      ``KanderFormal.Proofs.ReviewRun.input_conflict_refused,
      ``KanderFormal.Proofs.ReviewRun.published_frozen,
      ``KanderFormal.Proofs.ReviewRun.init,
      ``KanderFormal.Proofs.ReviewRun.step_inv,
      ``KanderFormal.Proofs.ReviewRun.reachable_inv,
      ``KanderFormal.Proofs.ReviewRun.reachable_published_finalized
    ] },
  { machine := Machines.ReviewFindings.reviewFindingsMachine
    source := "KanderFormal/Machines/ReviewFindings.lean"
    vectors := { depth := 4, paramSets := [[]], natDomain := [], events := findingsEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewFindings.wellTyped,
      ``KanderFormal.Proofs.ReviewFindings.open_spec,
      ``KanderFormal.Proofs.ReviewFindings.open_refused_unconsumable,
      ``KanderFormal.Proofs.ReviewFindings.open_once,
      ``KanderFormal.Proofs.ReviewFindings.unopened_refused,
      ``KanderFormal.Proofs.ReviewFindings.interpret_refused_schema,
      ``KanderFormal.Proofs.ReviewFindings.mapLegacy_refused_schema,
      ``KanderFormal.Proofs.ReviewFindings.mapLegacy_refused_fenced,
      ``KanderFormal.Proofs.ReviewFindings.interpret_refused_structured,
      ``KanderFormal.Proofs.ReviewFindings.reachable_structured_no_source,
      ``KanderFormal.Proofs.ReviewFindings.reachable_one_source,
      ``KanderFormal.Proofs.ReviewFindings.interpret_spec,
      ``KanderFormal.Proofs.ReviewFindings.mapLegacy_spec,
      ``KanderFormal.Proofs.ReviewFindings.interpret_replay,
      ``KanderFormal.Proofs.ReviewFindings.interpret_conflict,
      ``KanderFormal.Proofs.ReviewFindings.mapLegacy_replay,
      ``KanderFormal.Proofs.ReviewFindings.mapLegacy_conflict,
      ``KanderFormal.Proofs.ReviewFindings.assign_replay,
      ``KanderFormal.Proofs.ReviewFindings.assign_conflict,
      ``KanderFormal.Proofs.ReviewFindings.written_frozen,
      ``KanderFormal.Proofs.ReviewFindings.assign_refused_unavailable,
      ``KanderFormal.Proofs.ReviewFindings.assign_refused_pending,
      ``KanderFormal.Proofs.ReviewFindings.reachable_pending_unassigned,
      ``KanderFormal.Proofs.ReviewFindings.assign_spec,
      ``KanderFormal.Proofs.ReviewFindings.assign_refused_coverage,
      ``KanderFormal.Proofs.ReviewFindings.assign_refused_foreign,
      ``KanderFormal.Proofs.ReviewFindings.assign_refused_excluded,
      ``KanderFormal.Proofs.ReviewFindings.closed_writes_refused,
      ``KanderFormal.Proofs.ReviewFindings.interpret_closed,
      ``KanderFormal.Proofs.ReviewFindings.init,
      ``KanderFormal.Proofs.ReviewFindings.step_inv,
      ``KanderFormal.Proofs.ReviewFindings.reachable_inv
    ] },
  { machine := Machines.ReviewDisposition.reviewDispositionMachine
    source := "KanderFormal/Machines/ReviewDisposition.lean"
    vectors := { depth := 4, paramSets := [[]], natDomain := [], events := dispositionEvents }
    theorems := [
      ``KanderFormal.Proofs.ReviewDisposition.wellTyped,
      ``KanderFormal.Proofs.ReviewDisposition.reachable_chain_assigned,
      ``KanderFormal.Proofs.ReviewDisposition.assign_once,
      ``KanderFormal.Proofs.ReviewDisposition.submit_spec,
      ``KanderFormal.Proofs.ReviewDisposition.submit_refused_unassigned,
      ``KanderFormal.Proofs.ReviewDisposition.submit_refused_gate_deferred,
      ``KanderFormal.Proofs.ReviewDisposition.submit_refused_nongate,
      ``KanderFormal.Proofs.ReviewDisposition.submit_refused_waiver_role,
      ``KanderFormal.Proofs.ReviewDisposition.submit_refused_other_status,
      ``KanderFormal.Proofs.ReviewDisposition.submit_refuses_withdrawn,
      ``KanderFormal.Proofs.ReviewDisposition.withdraw_spec,
      ``KanderFormal.Proofs.ReviewDisposition.withdraw_refused_not_excluded,
      ``KanderFormal.Proofs.ReviewDisposition.withdraw_refused_unnamed,
      ``KanderFormal.Proofs.ReviewDisposition.withdraw_refused_unassigned,
      ``KanderFormal.Proofs.ReviewDisposition.submit_refused_no_findings,
      ``KanderFormal.Proofs.ReviewDisposition.withdraw_refused_no_findings,
      ``KanderFormal.Proofs.ReviewDisposition.submit_chain_spec,
      ``KanderFormal.Proofs.ReviewDisposition.submit_stale_predecessor,
      ``KanderFormal.Proofs.ReviewDisposition.submit_replay,
      ``KanderFormal.Proofs.ReviewDisposition.submit_conflict,
      ``KanderFormal.Proofs.ReviewDisposition.withdraw_conflict,
      ``KanderFormal.Proofs.ReviewDisposition.submit_same_needs_chain,
      ``KanderFormal.Proofs.ReviewDisposition.withdraw_chain_spec,
      ``KanderFormal.Proofs.ReviewDisposition.closed_refused,
      ``KanderFormal.Proofs.ReviewDisposition.written_frozen,
      ``KanderFormal.Proofs.ReviewDisposition.init,
      ``KanderFormal.Proofs.ReviewDisposition.step_inv,
      ``KanderFormal.Proofs.ReviewDisposition.reachable_inv
    ] },
  { machine := Machines.Coordinator.coordinatorMachine
    source := "KanderFormal/Machines/Coordinator.lean"
    vectors := { depth := 4, paramSets := [[("maxRev", 3), ("maxEpoch", 3), ("members", 2)]], natDomain := [],
                 events := coordinatorEvents }
    theorems := [
      ``KanderFormal.Proofs.Coordinator.wellTyped,
      ``KanderFormal.Proofs.Coordinator.init,
      ``KanderFormal.Proofs.Coordinator.step_inv,
      ``KanderFormal.Proofs.Coordinator.reachable_inv,
      ``KanderFormal.Proofs.Coordinator.reachable_exists,
      ``KanderFormal.Proofs.Coordinator.claim_spec,
      ``KanderFormal.Proofs.Coordinator.reconcile_spec,
      ``KanderFormal.Proofs.Coordinator.recover_spec,
      ``KanderFormal.Proofs.Coordinator.claim_replay,
      ``KanderFormal.Proofs.Coordinator.claim_refused_cas,
      ``KanderFormal.Proofs.Coordinator.claim_refused_token,
      ``KanderFormal.Proofs.Coordinator.claim_refused_room,
      ``KanderFormal.Proofs.Coordinator.claim_refused_members,
      ``KanderFormal.Proofs.Coordinator.claim_first_refused,
      ``KanderFormal.Proofs.Coordinator.reconcile_refused_authority,
      ``KanderFormal.Proofs.Coordinator.reconcile_refused_none,
      ``KanderFormal.Proofs.Coordinator.reconcile_refused_revision,
      ``KanderFormal.Proofs.Coordinator.reconcile_refused_request_failure,
      ``KanderFormal.Proofs.Coordinator.recover_refused_cas,
      ``KanderFormal.Proofs.Coordinator.recover_refused_not_blocked,
      ``KanderFormal.Proofs.Coordinator.recover_refused_count,
      ``KanderFormal.Proofs.Coordinator.stale_retry_after_write,
      ``KanderFormal.Proofs.Coordinator.epoch_change,
      ``KanderFormal.Proofs.Coordinator.rev_monotone,
      ``KanderFormal.Proofs.Coordinator.reconcile_blocked_count,
      ``KanderFormal.Proofs.Coordinator.reconcile_all_blocked_accepted,
      ``KanderFormal.Proofs.Coordinator.reconcile_unchanged_no_write,
      ``KanderFormal.Proofs.Coordinator.wrapUpBlocked_eval,
      ``KanderFormal.Proofs.Coordinator.reachable_blocked_exists,
      ``KanderFormal.Proofs.Coordinator.recover_unblocks
    ] },
  { machine := Machines.CoordinatorMember.coordMemberMachine
    source := "KanderFormal/Machines/CoordinatorMember.lean"
    vectors := { depth := 4, paramSets := [[("maxRev", 3), ("maxSeq", 2)]], natDomain := [], events := memberEvents }
    theorems := [
      ``KanderFormal.Proofs.CoordinatorMember.wellTyped,
      ``KanderFormal.Proofs.CoordinatorMember.init,
      ``KanderFormal.Proofs.CoordinatorMember.step_inv,
      ``KanderFormal.Proofs.CoordinatorMember.reachable_inv,
      ``KanderFormal.Proofs.CoordinatorMember.reachable_confirmed_finished,
      ``KanderFormal.Proofs.CoordinatorMember.block_keeps_mirror,
      ``KanderFormal.Proofs.CoordinatorMember.accept_clears_block,
      ``KanderFormal.Proofs.CoordinatorMember.member_isolated,
      ``KanderFormal.Proofs.CoordinatorMember.member_step,
      ``KanderFormal.Proofs.CoordinatorMember.accept_monotone,
      ``KanderFormal.Proofs.CoordinatorMember.accept_refused_revision,
      ``KanderFormal.Proofs.CoordinatorMember.accept_refused_chain,
      ``KanderFormal.Proofs.CoordinatorMember.accept_dseq,
      ``KanderFormal.Proofs.CoordinatorMember.accept_dseq_monotone,
      ``KanderFormal.Proofs.CoordinatorMember.acceptTerminal_dseq,
      ``KanderFormal.Proofs.CoordinatorMember.accept_refused_missing,
      ``KanderFormal.Proofs.CoordinatorMember.missing_blocks_forever,
      ``KanderFormal.Proofs.CoordinatorMember.blocked_exit,
      ``KanderFormal.Proofs.CoordinatorMember.reset_spec,
      ``KanderFormal.Proofs.CoordinatorMember.confirm_spec,
      ``KanderFormal.Proofs.CoordinatorMember.recover_refused_not_blocked,
      ``KanderFormal.Proofs.CoordinatorMember.recover_refused_early,
      ``KanderFormal.Proofs.CoordinatorMember.recover_modes_exclusive,
      ``KanderFormal.Proofs.CoordinatorMember.recover_wrong_mode,
      ``KanderFormal.Proofs.CoordinatorMember.confirmed_accepts
    ] },
  { machine := Machines.WrapUpWitness.wrapUpWitnessMachine
    source := "KanderFormal/Machines/WrapUpWitness.lean"
    vectors := { depth := 5, paramSets := [[]], natDomain := [] }
    theorems := [
      ``KanderFormal.Proofs.WrapUpWitness.wellTyped,
      ``KanderFormal.Proofs.WrapUpWitness.written_frozen,
      ``KanderFormal.Proofs.WrapUpWitness.complete_once,
      ``KanderFormal.Proofs.WrapUpWitness.reconcile_no_rewrite,
      ``KanderFormal.Proofs.WrapUpWitness.complete_spec,
      ``KanderFormal.Proofs.WrapUpWitness.complete_refused,
      ``KanderFormal.Proofs.WrapUpWitness.reconcile_recorded_skips_git,
      ``KanderFormal.Proofs.WrapUpWitness.reconcile_needs_git,
      ``KanderFormal.Proofs.WrapUpWitness.reconcile_invalid_refused,
      ``KanderFormal.Proofs.WrapUpWitness.reconcile_writes
    ] },
  { machine := Machines.IssueResult.issueResultMachine
    source := "KanderFormal/Machines/IssueResult.lean"
    vectors := { depth := 4, paramSets := [[("maxN", 2), ("maxId", 2)]], natDomain := [], events := issueResultEvents }
    theorems := [
      ``KanderFormal.Proofs.IssueResult.wellTyped,
      ``KanderFormal.Proofs.IssueResult.update_spec,
      ``KanderFormal.Proofs.IssueResult.update_append_only,
      ``KanderFormal.Proofs.IssueResult.update_published_frozen,
      ``KanderFormal.Proofs.IssueResult.update_uncertain_any,
      ``KanderFormal.Proofs.IssueResult.update_rejected_spec,
      ``KanderFormal.Proofs.IssueResult.update_refused_removed,
      ``KanderFormal.Proofs.IssueResult.update_refused_failures,
      ``KanderFormal.Proofs.IssueResult.update_refused_prefix,
      ``KanderFormal.Proofs.IssueResult.update_refused_decisions,
      ``KanderFormal.Proofs.IssueResult.update_refused_regress,
      ``KanderFormal.Proofs.IssueResult.update_refused_identity,
      ``KanderFormal.Proofs.IssueResult.work_spec,
      ``KanderFormal.Proofs.IssueResult.work_new_record,
      ``KanderFormal.Proofs.IssueResult.work_rejected_only,
      ``KanderFormal.Proofs.IssueResult.work_kept,
      ``KanderFormal.Proofs.IssueResult.work_refused_other_uncertain,
      ``KanderFormal.Proofs.IssueResult.work_refused_status,
      ``KanderFormal.Proofs.IssueResult.work_refused_work,
      ``KanderFormal.Proofs.IssueResult.idKeep_spec,
      ``KanderFormal.Proofs.IssueResult.idKeep_refused,
      ``KanderFormal.Proofs.IssueResult.valid_spec,
      ``KanderFormal.Proofs.IssueResult.valid_refused,
      ``KanderFormal.Proofs.IssueResult.issueNeeded_spec,
      ``KanderFormal.Proofs.IssueResult.apply_refused_uncertain,
      ``KanderFormal.Proofs.IssueResult.apply_refused_uncertain_record,
      ``KanderFormal.Proofs.IssueResult.reassess_spec,
      ``KanderFormal.Proofs.IssueResult.intent_spec,
      ``KanderFormal.Proofs.IssueResult.intent_result,
      ``KanderFormal.Proofs.IssueResult.intent_refused,
      ``KanderFormal.Proofs.IssueResult.equivalent_spec,
      ``KanderFormal.Proofs.IssueResult.equivalent_result,
      ``KanderFormal.Proofs.IssueResult.publish_spec,
      ``KanderFormal.Proofs.IssueResult.publish_result,
      ``KanderFormal.Proofs.IssueResult.reject_spec,
      ``KanderFormal.Proofs.IssueResult.reject_result,
      ``KanderFormal.Proofs.IssueResult.decide_spec,
      ``KanderFormal.Proofs.IssueResult.decide_result,
      ``KanderFormal.Proofs.IssueResult.decide_refused,
      ``KanderFormal.Proofs.IssueResult.keep_spec,
      ``KanderFormal.Proofs.IssueResult.keep_keeps,
      ``KanderFormal.Proofs.IssueResult.keep_decide_exclusive,
      ``KanderFormal.Proofs.IssueResult.failClose_spec,
      ``KanderFormal.Proofs.IssueResult.failClose_result,
      ``KanderFormal.Proofs.IssueResult.reconcile_spec,
      ``KanderFormal.Proofs.IssueResult.reconcile_result,
      ``KanderFormal.Proofs.IssueResult.reconcile_refused,
      ``KanderFormal.Proofs.IssueResult.read_spec,
      ``KanderFormal.Proofs.IssueResult.read_result,
      ``KanderFormal.Proofs.IssueResult.read_refused,
      ``KanderFormal.Proofs.IssueResult.status_flow,
      ``KanderFormal.Proofs.IssueResult.published_frozen,
      ``KanderFormal.Proofs.IssueResult.rejected_only_replaced,
      ``KanderFormal.Proofs.IssueResult.client_update_ok,
      ``KanderFormal.Proofs.IssueResult.client_work_ok,
      ``KanderFormal.Proofs.IssueResult.read_idKeep_ok,
      ``KanderFormal.Proofs.IssueResult.props_valid,
      ``KanderFormal.Proofs.IssueResult.props_issueNeeded,
      ``KanderFormal.Proofs.IssueResult.reachable_client_save,
      ``KanderFormal.Proofs.IssueResult.init,
      ``KanderFormal.Proofs.IssueResult.step_inv,
      ``KanderFormal.Proofs.IssueResult.reachable_inv,
      ``KanderFormal.Proofs.IssueResult.reachable_one_uncertain,
      ``KanderFormal.Proofs.IssueResult.reachable_uncertain_counted,
      ``KanderFormal.Proofs.IssueResult.reachable_issue_identity,
      ``KanderFormal.Proofs.IssueResult.reachable_comment,
      ``KanderFormal.Proofs.IssueResult.reachable_resolved_met,
      ``KanderFormal.Proofs.IssueResult.reachable_no_decision
    ] },
  { machine := Machines.IssueResultDecision.issueResultDecisionMachine
    source := "KanderFormal/Machines/IssueResultDecision.lean"
    vectors := { depth := 4, paramSets := [[("maxId", 2)]], natDomain := [], events := decisionEvents }
    theorems := [
      ``KanderFormal.Proofs.IssueResultDecision.wellTyped,
      ``KanderFormal.Proofs.IssueResultDecision.flow,
      ``KanderFormal.Proofs.IssueResultDecision.settled_frozen,
      ``KanderFormal.Proofs.IssueResultDecision.declined_only_new,
      ``KanderFormal.Proofs.IssueResultDecision.append_spec,
      ``KanderFormal.Proofs.IssueResultDecision.close_spec,
      ``KanderFormal.Proofs.IssueResultDecision.reconcile_spec,
      ``KanderFormal.Proofs.IssueResultDecision.reconcile_result,
      ``KanderFormal.Proofs.IssueResultDecision.reconcile_refused_settled,
      ``KanderFormal.Proofs.IssueResultDecision.settle_spec,
      ``KanderFormal.Proofs.IssueResultDecision.settle_keeps,
      ``KanderFormal.Proofs.IssueResultDecision.settle_identity_refused,
      ``KanderFormal.Proofs.IssueResultDecision.settle_settled_refused,
      ``KanderFormal.Proofs.IssueResultDecision.settle_absent_refused,
      ``KanderFormal.Proofs.IssueResultDecision.valid_spec,
      ``KanderFormal.Proofs.IssueResultDecision.valid_refused,
      ``KanderFormal.Proofs.IssueResultDecision.scanAcc_first,
      ``KanderFormal.Proofs.IssueResultDecision.scan_first_blocker,
      ``KanderFormal.Proofs.IssueResultDecision.scan_open_iff,
      ``KanderFormal.Proofs.IssueResultDecision.cls_uncertain,
      ``KanderFormal.Proofs.IssueResultDecision.cls_declined,
      ``KanderFormal.Proofs.IssueResultDecision.cls_consent,
      ``KanderFormal.Proofs.IssueResultDecision.cls_other,
      ``KanderFormal.Proofs.IssueResultDecision.did_kept,
      ``KanderFormal.Proofs.IssueResultDecision.client_settle_ok,
      ``KanderFormal.Proofs.IssueResultDecision.props_valid,
      ``KanderFormal.Proofs.IssueResultDecision.visitAll_enc,
      ``KanderFormal.Proofs.IssueResultDecision.init,
      ``KanderFormal.Proofs.IssueResultDecision.step_inv,
      ``KanderFormal.Proofs.IssueResultDecision.reachable_inv
    ] },
  { machine := Machines.IssueResultSave.issueResultSaveMachine
    source := "KanderFormal/Machines/IssueResultSave.lean"
    vectors := { depth := 5, paramSets := [[("maxRev", 3)]], natDomain := [], events := issueSaveEvents }
    theorems := [
      ``KanderFormal.Proofs.IssueResultSave.wellTyped,
      ``KanderFormal.Proofs.IssueResultSave.save_spec,
      ``KanderFormal.Proofs.IssueResultSave.save_result,
      ``KanderFormal.Proofs.IssueResultSave.save_refused,
      ``KanderFormal.Proofs.IssueResultSave.save_ceiling,
      ``KanderFormal.Proofs.IssueResultSave.replay_keeps,
      ``KanderFormal.Proofs.IssueResultSave.read_spec,
      ``KanderFormal.Proofs.IssueResultSave.read_refused,
      ``KanderFormal.Proofs.IssueResultSave.rev_step,
      ``KanderFormal.Proofs.IssueResultSave.init,
      ``KanderFormal.Proofs.IssueResultSave.step_inv,
      ``KanderFormal.Proofs.IssueResultSave.reachable_inv
    ] },
  { machine := Machines.ServerIntegration.serverIntegrationMachine
    source := "KanderFormal/Machines/ServerIntegration.lean"
    vectors := { depth := 4, paramSets := [[("maxGen", 2), ("maxRev", 4), ("maxAuth", 2)]], natDomain := [], events := integrationEvents }
    theorems := serverIntegrationTheorems }
]

end KanderFormal.Registry
