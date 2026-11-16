import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.CoordinatorMember

/-
The coordinator member machine on an encoded state: every event evaluates to an
explicit `if` on the components of the state. The theorems of
`Proofs/CoordinatorMember.lean` are proved on these forms.

Columns: 0 early (backlog and todo), 1 working, 2 review, 3 done, 4 archived,
5 trash.
-/
namespace KanderFormal.Proofs.CoordinatorMember

open KanderFormal KanderFormal.Machines.CoordinatorMember KanderFormal.Proofs.Scalar

set_option maxHeartbeats 4000000

abbrev M := coordMemberMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. -/
structure Mb where
  col : Nat
  rev : Nat
  tseq : Nat
  dseq : Nat
  blocked : Bool
  confirmed : Bool

def enc (x : Mb) : State :=
  [("col", enumV x.col), ("rev", natV x.rev), ("tseq", natV x.tseq), ("dseq", natV x.dseq),
    ("blocked", boolV x.blocked), ("confirmed", boolV x.confirmed)]

theorem enc_inj {x y : Mb} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _, _⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

/-- What a conforming state fits. -/
def Fits (p : Params) (x : Mb) : Prop :=
  x.col < 6 ∧ x.rev ≤ p "maxRev" ∧ x.tseq ≤ p "maxSeq" ∧ x.dseq ≤ p "maxSeq"

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) : ∃ x, s = enc x ∧ Fits p x := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5), (_, v6)], hn =>
    simp [coordMemberMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "col" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "rev" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "tseq" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "dseq" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "blocked" v5 (by simp [lookup])
    obtain ⟨t6, ht6, c6⟩ := hc "confirmed" v6 (by simp [lookup])
    cases (rfl : M.fieldTy "col" = some (.scalar (.enum "col"))).symm.trans ht1
    cases (rfl : M.fieldTy "rev" = some (.scalar (.nat "maxRev"))).symm.trans ht2
    cases (rfl : M.fieldTy "tseq" = some (.scalar (.nat "maxSeq"))).symm.trans ht3
    cases (rfl : M.fieldTy "dseq" = some (.scalar (.nat "maxSeq"))).symm.trans ht4
    cases (rfl : M.fieldTy "blocked" = some (.scalar .bool)).symm.trans ht5
    cases (rfl : M.fieldTy "confirmed" = some (.scalar .bool)).symm.trans ht6
    obtain ⟨i1, rfl, h1⟩ := conforms_enum (rfl : M.enumSize "col" = some 6) c1
    obtain ⟨n2, rfl, h2⟩ := conforms_nat c2
    obtain ⟨n3, rfl, h3⟩ := conforms_nat c3
    obtain ⟨n4, rfl, h4⟩ := conforms_nat c4
    obtain ⟨b5, rfl⟩ := conforms_bool c5
    obtain ⟨b6, rfl⟩ := conforms_bool c6
    exact ⟨⟨i1, n2, n3, n4, b5, b6⟩, rfl, h1, h2, h3, h4⟩

theorem find_accept : M.findEvent "accept" = some acceptEvent := rfl
theorem find_acceptConfirmed : M.findEvent "acceptConfirmed" = some acceptConfirmedEvent := rfl
theorem find_acceptTerminal : M.findEvent "acceptTerminal" = some acceptTerminalEvent := rfl
theorem find_block : M.findEvent "block" = some blockEvent := rfl
theorem find_recoverReset : M.findEvent "recoverReset" = some recoverResetEvent := rfl
theorem find_recoverConfirm : M.findEvent "recoverConfirm" = some recoverConfirmEvent := rfl
theorem size_col : M.enumSize "col" = some 6 := rfl
theorem ctors_col : M.ctorsOf "col" = some ["early", "working", "review", "done", "archived", "trash"] := rfl
theorem ty_col : M.fieldTy "col" = some (.scalar (.enum "col")) := rfl
theorem ty_rev : M.fieldTy "rev" = some (.scalar (.nat "maxRev")) := rfl
theorem ty_tseq : M.fieldTy "tseq" = some (.scalar (.nat "maxSeq")) := rfl
theorem ty_dseq : M.fieldTy "dseq" = some (.scalar (.nat "maxSeq")) := rfl
theorem ty_blocked : M.fieldTy "blocked" = some (.scalar .bool) := rfl
theorem ty_confirmed : M.fieldTy "confirmed" = some (.scalar .bool) := rfl

/-- `archived` or `trash`. -/
abbrev Terminal (c : Nat) : Prop := c = 4 ∨ c = 5
/-- `done`, `archived`, or `trash`. -/
abbrev Finished (c : Nat) : Prop := c = 3 ∨ c = 4 ∨ c = 5
/-- `working` or `review`. -/
abbrev Active (c : Nat) : Prop := c = 1 ∨ c = 2

macro "cm_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_accept, find_acceptConfirmed, find_acceptTerminal, find_block, find_recoverReset,
      find_recoverConfirm, size_col, ctors_col, ty_col, ty_rev, ty_tseq, ty_dseq, ty_blocked, ty_confirmed, lookup,
      acceptEvent, acceptConfirmedEvent, acceptTerminalEvent, blockEvent, recoverResetEvent, recoverConfirmEvent,
      Terminal, Finished, Active, shortcutE, revOkE, terminalE, finishedE, activeE, colIs, boolArg, colArg, revArg, seqArg, eq, ge, bindArgs,
      Ty.conforms, STy.conforms, eval, evalCmp, natAdd, natSub, boolAnd, boolOr, boolNot, indexOf, enumV, boolV,
      natV, f, a, tt, ff, andE, orE, notE, evalAssigns, applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc,
      Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

theorem step_block (p : Params) (x : Mb) (hx : Fits p x) :
    step M p (enc x) ⟨"block", []⟩ = some (enc { x with blocked := true }) := by
  obtain ⟨c, r, t, d, b, k⟩ := x
  obtain ⟨h1, h2, h3, h4⟩ := hx
  cm_simp [h1, h2, h3, h4]

theorem step_accept (p : Params) (x : Mb) (hx : Fits p x) (nc nr nt nd : Nat) (cs ok mi : Bool) :
    step M p (enc x) ⟨"accept", [enumV nc, natV nr, natV nt, natV nd, boolV cs, boolV ok, boolV mi]⟩ =
      if nc < 6 ∧ nr ≤ p "maxRev" ∧ nt ≤ p "maxSeq" ∧ nd ≤ p "maxSeq" ∧ ok = true ∧ mi = false ∧ ¬Terminal nc ∧
          ¬(x.confirmed = true ∧ Finished nc ∧ cs = true) ∧ x.rev ≤ nr ∧ x.tseq ≤ nt then
        some (enc ⟨nc, nr, nt, nd, false, false⟩) else none := by
  obtain ⟨c, r, t, d, b, k⟩ := x
  obtain ⟨h1, h2, h3, h4⟩ := hx
  cases k <;> cases cs <;> by_cases g1 : nc < 6 <;> by_cases g2 : nr ≤ p "maxRev" <;> by_cases g3 : nt ≤ p "maxSeq" <;>
    by_cases g4 : nd ≤ p "maxSeq" <;> cm_simp [g1, g2, g3, g4] <;> ite_close

theorem step_acceptConfirmed (p : Params) (x : Mb) (hx : Fits p x) (nc nr : Nat) (cs : Bool) :
    step M p (enc x) ⟨"acceptConfirmed", [enumV nc, natV nr, boolV cs]⟩ =
      if nc < 6 ∧ nr ≤ p "maxRev" ∧ x.confirmed = true ∧ Finished nc ∧ cs = true ∧ x.rev ≤ nr then
        some (enc { x with col := nc, rev := nr, blocked := false }) else none := by
  obtain ⟨c, r, t, d, b, k⟩ := x
  obtain ⟨h1, h2, h3, h4⟩ := hx
  by_cases g1 : nc < 6 <;> by_cases g2 : nr ≤ p "maxRev" <;> cm_simp [g1, g2] <;> ite_close

theorem step_acceptTerminal (p : Params) (x : Mb) (hx : Fits p x) (nc nr nt nd : Nat) (hr cs ok mi : Bool) :
    step M p (enc x) ⟨"acceptTerminal", [enumV nc, natV nr, natV nt, natV nd, boolV hr, boolV cs, boolV ok,
        boolV mi]⟩ =
      if nc < 6 ∧ nr ≤ p "maxRev" ∧ nt ≤ p "maxSeq" ∧ nd ≤ p "maxSeq" ∧ ok = true ∧ mi = false ∧ Terminal nc ∧
          ¬(x.confirmed = true ∧ cs = true) ∧ x.rev ≤ nr ∧ x.tseq ≤ nt then
        some (enc ⟨nc, nr, nt, if hr = true then nd else x.dseq, false, x.confirmed⟩) else none := by
  obtain ⟨c, r, t, d, b, k⟩ := x
  obtain ⟨h1, h2, h3, h4⟩ := hx
  cases k <;> cases cs <;> cases hr <;> by_cases g1 : nc < 6 <;> by_cases g2 : nr ≤ p "maxRev" <;> by_cases g3 : nt ≤ p "maxSeq" <;>
    by_cases g4 : nd ≤ p "maxSeq" <;> cm_simp [g1, g2, g3, g4] <;> ite_close

theorem step_recoverReset (p : Params) (x : Mb) (hx : Fits p x) (cc nr nt nd : Nat) (ok : Bool) :
    step M p (enc x) ⟨"recoverReset", [enumV cc, natV nr, natV nt, natV nd, boolV ok]⟩ =
      if cc < 6 ∧ nr ≤ p "maxRev" ∧ nt ≤ p "maxSeq" ∧ nd ≤ p "maxSeq" ∧ x.blocked = true ∧ Active cc ∧
          ok = true then
        some (enc ⟨1, nr, nt, nd, false, false⟩) else none := by
  obtain ⟨c, r, t, d, b, k⟩ := x
  obtain ⟨h1, h2, h3, h4⟩ := hx
  by_cases g1 : cc < 6 <;> by_cases g2 : nr ≤ p "maxRev" <;> by_cases g3 : nt ≤ p "maxSeq" <;>
    by_cases g4 : nd ≤ p "maxSeq" <;> cm_simp [g1, g2, g3, g4] <;> ite_close

theorem step_recoverConfirm (p : Params) (x : Mb) (hx : Fits p x) (cc nr : Nat) (tr : Bool) (nt : Nat) (dr : Bool)
    (nd : Nat) :
    step M p (enc x) ⟨"recoverConfirm", [enumV cc, natV nr, boolV tr, natV nt, boolV dr, natV nd]⟩ =
      if cc < 6 ∧ nr ≤ p "maxRev" ∧ nt ≤ p "maxSeq" ∧ nd ≤ p "maxSeq" ∧ x.blocked = true ∧ Finished cc then
        some (enc ⟨cc, nr, if tr = true then nt else x.tseq, if dr = true then nd else x.dseq, false, true⟩)
      else none := by
  obtain ⟨c, r, t, d, b, k⟩ := x
  obtain ⟨h1, h2, h3, h4⟩ := hx
  cases tr <;> cases dr <;> by_cases g1 : cc < 6 <;> by_cases g2 : nr ≤ p "maxRev" <;> by_cases g3 : nt ≤ p "maxSeq" <;>
    by_cases g4 : nd ≤ p "maxSeq" <;> cm_simp [g1, g2, g3, g4] <;> ite_close

end KanderFormal.Proofs.CoordinatorMember
