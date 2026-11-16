import KanderFormal.Semantics

/-
The differential vector alphabets of the issue result machines. They live in
their own file so that the registry stays below the file size limit.
-/
namespace KanderFormal.Registry

/-- The issue result vector alphabet. Each event is tried with every fact holding and with one fact failing; the
statuses are `none` 0, `uncertain` 1, `published` 2, `equivalent` 3, `rejected` 4, and `other` 5; the scan classes are
`open` 0 and `returned` 4 (an event that needs the scan blocked is tried with `refusal` 2). The update and work
checks try the new status, the failures, and the decisions of the first counts. -/
def issueResultEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let n := natV
  [ ⟨"read", [t, t]⟩, ⟨"read", [t, x]⟩, ⟨"read", [x, t]⟩,
    ⟨"reconcile", [t]⟩, ⟨"reconcile", [x]⟩, ⟨"reassess", []⟩,
    ⟨"intent", [x, x, x, t]⟩, ⟨"intent", [t, t, x, t]⟩, ⟨"intent", [t, x, x, t]⟩, ⟨"intent", [x, x, t, t]⟩,
    ⟨"intent", [x, x, x, x]⟩,
    ⟨"equivalent", [x, x, t]⟩, ⟨"equivalent", [t, t, t]⟩, ⟨"equivalent", [x, x, x]⟩,
    ⟨"publish", [t]⟩, ⟨"publish", [x]⟩, ⟨"reject", []⟩,
    ⟨"decide", [t, x, enumV 0, t]⟩, ⟨"decide", [x, x, enumV 0, t]⟩, ⟨"decide", [t, t, enumV 0, t]⟩,
    ⟨"decide", [t, x, enumV 1, t]⟩, ⟨"decide", [t, x, enumV 0, x]⟩,
    ⟨"keep", [t, t, enumV 0]⟩, ⟨"keep", [t, x, enumV 4]⟩, ⟨"keep", [t, x, enumV 0]⟩, ⟨"keep", [x, t, enumV 0]⟩,
    ⟨"failClose", []⟩, ⟨"otherIntent", []⟩, ⟨"otherSettle", []⟩,
    ⟨"update", [t, enumV 2, t, n 0, t, n 0]⟩, ⟨"update", [t, enumV 1, t, n 1, t, n 1]⟩,
    ⟨"update", [x, enumV 2, t, n 0, t, n 0]⟩, ⟨"update", [t, enumV 2, x, n 0, t, n 0]⟩,
    ⟨"update", [t, enumV 4, t, n 0, t, n 0]⟩, ⟨"update", [t, enumV 2, t, n 0, x, n 0]⟩,
    ⟨"work", [enumV 1, x, t, t, t]⟩, ⟨"work", [enumV 3, x, t, t, t]⟩, ⟨"work", [enumV 2, x, t, t, t]⟩,
    ⟨"work", [enumV 1, x, t, x, t]⟩, ⟨"work", [enumV 4, t, t, t, t]⟩, ⟨"work", [enumV 1, x, x, t, t]⟩,
    ⟨"idKeep", [t]⟩, ⟨"idKeep", [x]⟩,
    ⟨"valid", [enumV 1, t, x, t, x, x]⟩, ⟨"valid", [enumV 2, t, t, t, t, t]⟩, ⟨"valid", [enumV 2, t, x, t, x, x]⟩,
    ⟨"valid", [enumV 0, t, x, t, x, x]⟩, ⟨"valid", [enumV 1, t, x, t, t, x]⟩,
    ⟨"issueNeeded", [t, t]⟩, ⟨"issueNeeded", [t, x]⟩ ]

/-- The issue close decision vector alphabet. Statuses are `none` 0, `declined` 1, `uncertain` 2, `closed` 3,
`observed` 4, `superseded` 5, `rejected` 6, and `other` 7. Scan visits are `(decision, reconsider, newNo)`. -/
def decisionEvents : List Event :=
  let t := boolV true
  let x := boolV false
  [ ⟨"appendNo", []⟩, ⟨"appendYes", []⟩, ⟨"closeDone", []⟩, ⟨"closeRejected", []⟩,
    ⟨"reconcile", [t, t]⟩, ⟨"reconcile", [t, x]⟩, ⟨"reconcile", [x, x]⟩, ⟨"reconcile", [x, t]⟩,
    ⟨"visit", [enumV 1, x, x]⟩, ⟨"visit", [enumV 1, x, t]⟩, ⟨"visit", [enumV 1, t, x]⟩,
    ⟨"visit", [enumV 2, x, x]⟩, ⟨"visit", [enumV 2, t, x]⟩, ⟨"visit", [enumV 3, x, x]⟩, ⟨"visit", [enumV 3, t, x]⟩,
    ⟨"visit", [enumV 4, x, x]⟩, ⟨"visit", [enumV 5, x, x]⟩, ⟨"visit", [enumV 6, x, x]⟩, ⟨"visit", [enumV 6, t, x]⟩,
    ⟨"visit", [enumV 0, x, x]⟩, ⟨"visit", [enumV 7, x, x]⟩,
    ⟨"settle", [enumV 2, t]⟩, ⟨"settle", [enumV 3, t]⟩, ⟨"settle", [enumV 1, t]⟩, ⟨"settle", [enumV 2, x]⟩,
    ⟨"settle", [enumV 6, t]⟩, ⟨"settle", [enumV 0, t]⟩,
    ⟨"valid", [t, enumV 2]⟩, ⟨"valid", [t, enumV 3]⟩, ⟨"valid", [t, enumV 6]⟩, ⟨"valid", [t, enumV 1]⟩,
    ⟨"valid", [x, enumV 1]⟩, ⟨"valid", [x, enumV 2]⟩, ⟨"valid", [t, enumV 0]⟩, ⟨"valid", [x, enumV 7]⟩ ]

/-- The issue result resource vector alphabet: reads, saves with every fact failing in turn, and the replay. -/
def issueSaveEvents : List Event :=
  let t := boolV true
  let x := boolV false
  let n := natV
  [ ⟨"read", [t]⟩, ⟨"read", [x]⟩,
    ⟨"save", [t, n 0, t, t, t]⟩, ⟨"save", [t, n 1, t, t, t]⟩, ⟨"save", [t, n 2, t, t, t]⟩, ⟨"save", [t, n 3, t, t, t]⟩,
    ⟨"save", [x, n 0, t, t, t]⟩, ⟨"save", [x, n 1, t, t, t]⟩, ⟨"save", [t, n 0, x, t, t]⟩, ⟨"save", [t, n 1, x, t, t]⟩,
    ⟨"save", [t, n 0, t, x, t]⟩, ⟨"save", [t, n 1, t, x, t]⟩, ⟨"save", [t, n 0, t, t, x]⟩, ⟨"save", [t, n 1, t, t, x]⟩,
    ⟨"replay", []⟩ ]

end KanderFormal.Registry
