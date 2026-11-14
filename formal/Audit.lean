import Lean
import KanderFormal

/-
Axiom audit, run by scripts/formal-check.sh as `lake env lean Audit.lean`.

It fails when a registered theorem is missing, or when any declaration of a
KanderFormal module depends on an axiom other than propext, Classical.choice,
and Quot.sound. A `sorry` shows up as the axiom sorryAx and fails the audit; so
does a custom axiom or native_decide (Lean.ofReduceBool).
-/
open Lean Elab Command

def allowedAxioms : List Name := [``propext, ``Classical.choice, ``Quot.sound]

#eval show CommandElabM Unit from do
  let env ← getEnv
  let mut failures : Array String := #[]
  let registered := KanderFormal.Registry.genericTheorems ++
    KanderFormal.Registry.entries.flatMap (·.theorems)
  for n in registered do
    match env.find? n with
    | some (.thmInfo _) => pure ()
    | some _ => failures := failures.push s!"{n} is registered but is not a theorem"
    | none => failures := failures.push s!"{n} is registered but does not exist"
  for en in KanderFormal.Registry.entries do
    if en.theorems.isEmpty then
      failures := failures.push s!"machine {en.machine.name} registers no theorem"
  let mut checked : Nat := 0
  for (mod, idx) in env.header.moduleNames.zipIdx do
    if !(`KanderFormal).isPrefixOf mod then continue
    for c in env.header.moduleData[idx]!.constNames do
      checked := checked + 1
      let axioms ← liftCoreM (collectAxioms c)
      for a in axioms do
        if !allowedAxioms.contains a then
          failures := failures.push s!"{c} depends on axiom {a}"
  if failures.isEmpty then
    logInfo m!"axiom audit passed: {registered.length} registered theorems, {checked} declarations"
  else
    throwError ("axiom audit failed:\n" ++ "\n".intercalate failures.toList)
