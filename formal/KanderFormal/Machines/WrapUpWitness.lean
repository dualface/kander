import KanderFormal.Semantics

/-
The wrap-up Git witness machine (M12): `G/<group>/wrap-up-git/<dispatch>.json`.

One instance describes one wrap-up dispatch. The witness records that the Git
facts of the wrap-up held once; it is written once and never rewritten. Two
entrances write it: the completion move (`complete`, `verifyWrapUpCompletion`
and `wrapUpCompletion.stage`) and a reconcile that finds a completed wrap-up
without one (`reconcile`, `putWrapUpEvidence` through the pass).

The Git verification itself stays in the shell. `verified` is its result (a
verification that was not needed counts as passed by the kernel's own rule: the
shell first asks with `verified` false whether any is needed). `valid` is the
structural check of an existing record against its dispatch. `epochSame` is
whether the dispatch still holds the epoch the check ran for.
-/
namespace KanderFormal.Machines.WrapUpWitness

open KanderFormal

def f (n : String) : Expr := .field n
def a (n : String) : Expr := .arg n
def tt : Expr := .boolLit true
def andE (x y : Expr) : Expr := .and x y
def orE (x y : Expr) : Expr := .or x y
def notE (x : Expr) : Expr := .not x

def boolArg (n : String) : ArgDecl := { name := n, ty := .scalar .bool }

/-- The completion move writes the witness once, for the epoch it verified. -/
def completeEvent : EventDecl where
  name := "complete"
  args := [boolArg "epochSame", boolArg "verified"]
  guard := andE (notE (f "written")) (andE (a "epochSame") (a "verified"))
  assigns := [("written", tt)]

/-- A reconcile of a wrap-up member. An existing record must be valid. A
completed wrap-up that has its record is not verified again; any other needs a
passed verification, and a completed one then gets its record. -/
def reconcileEvent : EventDecl where
  name := "reconcile"
  args := [boolArg "completed", boolArg "valid", boolArg "verified"]
  guard := andE (orE (notE (f "written")) (a "valid"))
    (orE (a "verified") (andE (a "completed") (f "written")))
  assigns := [("written", orE (f "written") (a "completed"))]

def wrapUpWitnessMachine : Machine where
  name := "wrapupwitness"
  enums := []
  params := []
  fields := [{ name := "written", ty := .scalar .bool, init := .scalar (.bool false) }]
  events := [completeEvent, reconcileEvent]
  queries := [{ name := "recorded", ty := .base .bool, body := f "written" }]

end KanderFormal.Machines.WrapUpWitness
