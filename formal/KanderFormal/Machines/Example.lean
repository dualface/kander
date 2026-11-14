import KanderFormal.Dsl

/-
The example machine. It exists only to exercise the toolchain: every value
type, every expression form, a nat event argument, and a query that can fail.
Production code does not use it.

A card is claimed for an epoch, collects one verdict per review role, and is
sealed when every role passed.
-/
namespace KanderFormal.Machines

open KanderFormal

def claimEvent : EventDecl where
  name := "claim"
  args := []
  guard := .cmp .eq (.field "phase") (.enumLit "phase" "idle")
  assigns := [
    ("phase", .enumLit "phase" "claimed"),
    ("epoch", .add (.field "epoch") (.natLit 1)),
    ("reviews", .put (.put (.field "reviews") (.enumLit "role" "pmqa") (.enumLit "verdict" "none"))
      (.enumLit "role" "security") (.enumLit "verdict" "none")),
    ("reviewed", .erase (.erase (.field "reviewed") (.enumLit "role" "pmqa")) (.enumLit "role" "security"))
  ]

def reviewEvent : EventDecl where
  name := "review"
  args := [{ name := "role", ty := .enum "role" }, { name := "pass", ty := .bool }]
  guard := .cmp .eq (.field "phase") (.enumLit "phase" "claimed")
  assigns := [
    ("reviews", .put (.field "reviews") (.arg "role")
      (.ite (.arg "pass") (.enumLit "verdict" "pass") (.enumLit "verdict" "fail"))),
    ("reviewed", .insert (.field "reviewed") (.arg "role"))
  ]

def releaseEvent : EventDecl where
  name := "release"
  args := []
  guard := .cmp .eq (.field "phase") (.enumLit "phase" "claimed")
  assigns := [("phase", .enumLit "phase" "idle")]

def finishEvent : EventDecl where
  name := "finish"
  args := []
  guard := .and (.cmp .eq (.field "phase") (.enumLit "phase" "claimed"))
    (.and (.cmp .eq (.get (.field "reviews") (.enumLit "role" "pmqa")) (.enumLit "verdict" "pass"))
      (.cmp .eq (.get (.field "reviews") (.enumLit "role" "security")) (.enumLit "verdict" "pass")))
  assigns := [("phase", .enumLit "phase" "done"), ("sealed", .boolLit true)]

def bumpEvent : EventDecl where
  name := "bump"
  args := [{ name := "to", ty := .nat "maxEpoch" }]
  guard := .and (.cmp .eq (.field "phase") (.enumLit "phase" "idle"))
    (.and (.cmp .gt (.arg "to") (.field "epoch")) (.cmp .ge (.arg "to") (.natLit 1)))
  assigns := [("epoch", .arg "to")]

def exampleMachine : Machine where
  name := "example"
  enums := [
    { name := "phase", ctors := ["idle", "claimed", "done"] },
    { name := "role", ctors := ["pmqa", "security"] },
    { name := "verdict", ctors := ["none", "pass", "fail"] }
  ]
  params := ["maxEpoch"]
  fields := [
    { name := "phase", ty := .scalar (.enum "phase"), init := .scalar (.enum 0) },
    { name := "epoch", ty := .scalar (.nat "maxEpoch"), init := .scalar (.nat 0) },
    { name := "reviews", ty := .map "role" (.enum "verdict"), init := .map [.enum 0, .enum 0] },
    { name := "reviewed", ty := .set "role", init := .set [false, false] },
    { name := "sealed", ty := .scalar .bool, init := .scalar (.bool false) }
  ]
  events := [claimEvent, reviewEvent, releaseEvent, finishEvent, bumpEvent]
  queries := [
    { name := "canFinish", ty := .base .bool
      body := .and (.cmp .eq (.field "phase") (.enumLit "phase" "claimed"))
        (.and (.and (.mem (.field "reviewed") (.enumLit "role" "pmqa"))
            (.mem (.field "reviewed") (.enumLit "role" "security")))
          (.not (.or (.cmp .ne (.get (.field "reviews") (.enumLit "role" "pmqa")) (.enumLit "verdict" "pass"))
            (.cmp .ne (.get (.field "reviews") (.enumLit "role" "security")) (.enumLit "verdict" "pass"))))) },
    { name := "headroom", ty := .base .nat
      body := .sub (.param "maxEpoch") (.field "epoch") },
    { name := "fresh", ty := .base .bool
      body := .or (.cmp .le (.field "epoch") (.natLit 0)) (.cmp .lt (.field "epoch") (.natLit 1)) },
    { name := "overdrawn", ty := .base .nat
      body := .sub (.field "epoch") (.natLit 1) }
  ]

end KanderFormal.Machines
