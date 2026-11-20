import KanderFormal.Semantics

/-
The differential vector alphabet of the review run machine. It lives in its own
file so that the registry stays below the file size limit.
-/
namespace KanderFormal.Registry

/-- The review run vector alphabet: each event with every fact holding and with
one fact failing, the phase updates the driver makes and the odd ones the entrance
accepts, the finalizations of an `ok` run (valid, invalid report, launch status
other than started, empty report, missing originals), a failure with and without
a reason, a launch failure, an interruption, and the abandon with every fact
holding and with each of the seven facts failing. -/
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
    ⟨"passSkipped", [t, t]⟩, ⟨"passSkipped", [t, x]⟩,
    ⟨"abandon", [t, t, t, t, t, t, t]⟩, ⟨"abandon", [x, t, t, t, t, t, t]⟩, ⟨"abandon", [t, x, t, t, t, t, t]⟩,
    ⟨"abandon", [t, t, x, t, t, t, t]⟩, ⟨"abandon", [t, t, t, x, t, t, t]⟩, ⟨"abandon", [t, t, t, t, x, t, t]⟩,
    ⟨"abandon", [t, t, t, t, t, x, t]⟩, ⟨"abandon", [t, t, t, t, t, t, x]⟩ ]

end KanderFormal.Registry
