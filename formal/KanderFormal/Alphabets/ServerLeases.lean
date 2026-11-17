import KanderFormal.Semantics

/-
The differential vector alphabets of the server claim and group lease
machines. They live in their own file so that the registry stays below the
file size limit.
-/
namespace KanderFormal.Registry

/-- The server claim vector alphabet: holder requests of the first generations,
of no generation, and of an older one; terminations; takeovers with the
current, an older, and a newer observed generation; the environment events;
the startup lease; and the standing checks of the first two generations. -/
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
    ⟨"audit", [natV 1, t]⟩, ⟨"audit", [natV 0, t]⟩, ⟨"audit", [natV 1, x]⟩,
    ⟨"startup", []⟩, ⟨"standing", [natV 1]⟩, ⟨"standing", [natV 2]⟩ ]

/-- The server group lease vector alphabet. Holder requests are `(hdr, g, tg)`:
the first and second generation, a token of another generation, and a header
that did not parse. Claims try a new record, takeovers with and without a
decision, at the observed generation and at a stale one, with and without room
below the last generation, and the reuse of a lease by the same client, by
another client, with a stale token, and with a header that did not parse. The
environment events, the startup lease, and the standing checks of the first two
generations close it. -/
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
    ⟨"expire", []⟩, ⟨"startup", []⟩, ⟨"standing", [natV 1]⟩, ⟨"standing", [natV 2]⟩ ]

end KanderFormal.Registry
