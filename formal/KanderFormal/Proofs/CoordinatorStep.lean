import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.Coordinator

/-
The coordinator checkpoint machine on an encoded state: every event evaluates to
an explicit `if` on the components of the state. The theorems of
`Proofs/Coordinator.lean` are proved on these forms.
-/
namespace KanderFormal.Proofs.Coordinator

open KanderFormal KanderFormal.Machines.Coordinator KanderFormal.Proofs.Scalar

set_option maxHeartbeats 4000000

abbrev M := coordinatorMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. -/
structure Gp where
  exists_ : Bool
  rev : Nat
  epoch : Nat
  last : Bool
  blocked : Nat

def enc (x : Gp) : State :=
  [("exists", boolV x.exists_), ("rev", natV x.rev), ("epoch", natV x.epoch), ("last", boolV x.last),
    ("blocked", natV x.blocked)]

theorem enc_inj {x y : Gp} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _, _⟩ := x
  obtain ⟨_, _, _, _, _⟩ := y
  simp [enc, boolV, natV] at h
  obtain ⟨rfl, rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.rev ≤ p "maxRev" ∧ x.epoch ≤ p "maxEpoch" ∧ x.blocked ≤ p "members" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5)], hn =>
    simp [coordinatorMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "exists" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "rev" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "epoch" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "last" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "blocked" v5 (by simp [lookup])
    cases (rfl : M.fieldTy "exists" = some (.scalar .bool)).symm.trans ht1
    cases (rfl : M.fieldTy "rev" = some (.scalar (.nat "maxRev"))).symm.trans ht2
    cases (rfl : M.fieldTy "epoch" = some (.scalar (.nat "maxEpoch"))).symm.trans ht3
    cases (rfl : M.fieldTy "last" = some (.scalar .bool)).symm.trans ht4
    cases (rfl : M.fieldTy "blocked" = some (.scalar (.nat "members"))).symm.trans ht5
    obtain ⟨b1, rfl⟩ := conforms_bool c1
    obtain ⟨n2, rfl, h2⟩ := conforms_nat c2
    obtain ⟨n3, rfl, h3⟩ := conforms_nat c3
    obtain ⟨b4, rfl⟩ := conforms_bool c4
    obtain ⟨n5, rfl, h5⟩ := conforms_nat c5
    exact ⟨⟨b1, n2, n3, b4, n5⟩, rfl, h2, h3, h5⟩

theorem find_claim : M.findEvent "claim" = some claimEvent := rfl
theorem find_reconcile : M.findEvent "reconcile" = some reconcileEvent := rfl
theorem find_recover : M.findEvent "recover" = some recoverEvent := rfl
theorem find_removeMember : M.findEvent "removeMember" = some removeMemberEvent := rfl
theorem ty_exists : M.fieldTy "exists" = some (.scalar .bool) := rfl
theorem ty_rev : M.fieldTy "rev" = some (.scalar (.nat "maxRev")) := rfl
theorem ty_epoch : M.fieldTy "epoch" = some (.scalar (.nat "maxEpoch")) := rfl
theorem ty_last : M.fieldTy "last" = some (.scalar .bool) := rfl
theorem ty_blocked : M.fieldTy "blocked" = some (.scalar (.nat "members")) := rfl

macro "cp_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_claim, find_reconcile, find_recover, find_removeMember, ty_exists, ty_rev, ty_epoch, ty_last, ty_blocked,
      lookup, claimEvent, reconcileEvent, recoverEvent, removeMemberEvent, replayE, boolArg, revArg, epochArg, countArg, eq, inc,
      bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, natSub, boolAnd, boolOr, boolNot, indexOf, boolV, natV,
      f, a, tt, ff, andE, orE, notE, evalAssigns, applyUpdates, enc, or_assoc, and_assoc, Bool.and_assoc,
      Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

/-- A claim whose hash equals the stored one. -/
def Replay (x : Gp) (sm : Bool) : Prop := x.exists_ = true ∧ sm = true

instance (x : Gp) (sm : Bool) : Decidable (Replay x sm) := by unfold Replay; infer_instance

/-- The conditions of a claim that is not a replay. -/
def ClaimOk (x : Gp) (er ee : Nat) (ts rm mo mb : Bool) : Prop :=
  er = x.rev ∧ ee = x.epoch ∧ rm = true ∧ ¬(x.exists_ = true ∧ ts = true) ∧
    (x.exists_ = false ∨ mo = true) ∧ mb = true

instance (x : Gp) (er ee : Nat) (ts rm mo mb : Bool) : Decidable (ClaimOk x er ee ts rm mo mb) := by
  unfold ClaimOk; infer_instance

theorem step_claim (p : Params) (x : Gp) (hr : x.rev ≤ p "maxRev") (he : x.epoch ≤ p "maxEpoch") (er ee : Nat) (sm ts rm mo mb : Bool) :
    step M p (enc x) ⟨"claim", [natV er, natV ee, boolV sm, boolV ts, boolV rm, boolV mo, boolV mb]⟩ =
      if er ≤ p "maxRev" ∧ ee ≤ p "maxEpoch" then
        (if Replay x sm then some (enc x)
        else if ClaimOk x er ee ts rm mo mb ∧ x.rev + 1 ≤ p "maxRev" ∧ x.epoch + 1 ≤ p "maxEpoch" then
          some (enc ⟨true, x.rev + 1, x.epoch + 1, false, x.blocked⟩) else none)
      else none := by
  obtain ⟨e, r, ep, l, b⟩ := x
  simp only at hr he
  by_cases h1 : er ≤ p "maxRev" <;> by_cases h2 : ee ≤ p "maxEpoch" <;> by_cases h3 : r + 1 ≤ p "maxRev" <;>
    by_cases h4 : ep + 1 ≤ p "maxEpoch" <;> cases e <;> cases sm <;>
    cp_simp [h1, h2, h3, h4, hr, he, Replay, ClaimOk] <;> ite_close

/-- The conditions of an accepted reconcile apart from the revision. -/
def RecOk (x : Gp) (os ts : Bool) (ea : Nat) (mo mm rc rf : Bool) : Prop :=
  x.exists_ = true ∧ os = true ∧ ts = true ∧ ea = x.epoch ∧ mo = true ∧ mm = true ∧ rc = true ∧ rf = false

instance (x : Gp) (os ts : Bool) (ea : Nat) (mo mm rc rf : Bool) : Decidable (RecOk x os ts ea mo mm rc rf) := by
  unfold RecOk; infer_instance

theorem step_reconcile (p : Params) (x : Gp) (hr : x.rev ≤ p "maxRev") (hb : x.blocked ≤ p "members") (os ts : Bool) (ea er : Nat) (sm mo mm rc rf ch : Bool) (ba : Nat)
    (rm : Bool) :
    step M p (enc x) ⟨"reconcile", [boolV os, boolV ts, natV ea, natV er, boolV sm, boolV mo, boolV mm, boolV rc,
        boolV rf, boolV ch, natV ba, boolV rm]⟩ =
      if ea ≤ p "maxEpoch" ∧ er ≤ p "maxRev" ∧ ba ≤ p "members" then
        (if RecOk x os ts ea mo mm rc rf ∧ (er = x.rev ∨ x.last = true ∧ sm = true) ∧ (ch = false ∨ rm = true) then
          (if ch = true then
            (if x.rev + 1 ≤ p "maxRev" then some (enc ⟨x.exists_, x.rev + 1, x.epoch, true, ba⟩) else none)
          else some (enc x))
        else none)
      else none := by
  obtain ⟨e, r, ep, l, b⟩ := x
  simp only at hr hb
  by_cases h1 : ea ≤ p "maxEpoch" <;> by_cases h2 : er ≤ p "maxRev" <;> by_cases h3 : ba ≤ p "members" <;>
    by_cases h4 : r + 1 ≤ p "maxRev" <;> cases e <;> cases ch <;>
    cp_simp [h1, h2, h3, h4, hr, hb, RecOk] <;> ite_close

/-- The conditions of an accepted recovery apart from the revision and epoch. -/
def RecvOk (x : Gp) (rm kn mb ex mo : Bool) : Prop :=
  x.exists_ = true ∧ rm = true ∧ kn = true ∧ mb = true ∧ ex = true ∧ mo = true ∧ 1 ≤ x.blocked

instance (x : Gp) (rm kn mb ex mo : Bool) : Decidable (RecvOk x rm kn mb ex mo) := by
  unfold RecvOk; infer_instance

theorem step_recover (p : Params) (x : Gp) (he : x.epoch ≤ p "maxEpoch") (hb : x.blocked ≤ p "members") (er ee : Nat) (rm kn mb ex mo : Bool) :
    step M p (enc x) ⟨"recover", [natV er, natV ee, boolV rm, boolV kn, boolV mb, boolV ex, boolV mo]⟩ =
      if er ≤ p "maxRev" ∧ ee ≤ p "maxEpoch" ∧ er = x.rev ∧ ee = x.epoch ∧ RecvOk x rm kn mb ex mo ∧
          x.rev + 1 ≤ p "maxRev" then
        some (enc ⟨x.exists_, x.rev + 1, x.epoch, false, x.blocked - 1⟩) else none := by
  obtain ⟨e, r, ep, l, b⟩ := x
  simp only at he hb
  by_cases h1 : er ≤ p "maxRev" <;> by_cases h2 : ee ≤ p "maxEpoch" <;> by_cases h3 : r + 1 ≤ p "maxRev" <;>
    by_cases h4 : 1 ≤ b <;> cases e <;>
    cp_simp [h1, h2, h3, h4, he, hb, RecvOk] <;> ite_close

/-- The conditions of an accepted removal apart from the revision, epoch, and
counts. -/
def RemOk (x : Gp) (rm kn un mb rem nd : Bool) : Prop :=
  x.exists_ = true ∧ rm = true ∧ kn = true ∧ un = true ∧ mb = true ∧ rem = true ∧ nd = true

instance (x : Gp) (rm kn un mb rem nd : Bool) : Decidable (RemOk x rm kn un mb rem nd) := by
  unfold RemOk; infer_instance

theorem step_removeMember (p : Params) (x : Gp) (he : x.epoch ≤ p "maxEpoch") (hb : x.blocked ≤ p "members") (er ee : Nat)
    (rm kn un mb rem nd : Bool) (ba : Nat) :
    step M p (enc x) ⟨"removeMember", [natV er, natV ee, boolV rm, boolV kn, boolV un, boolV mb, boolV rem, boolV nd,
        natV ba]⟩ =
      if er ≤ p "maxRev" ∧ ee ≤ p "maxEpoch" ∧ ba ≤ p "members" ∧ er = x.rev ∧ ee = x.epoch ∧
          RemOk x rm kn un mb rem nd ∧ ba ≤ x.blocked ∧ x.rev + 1 ≤ p "maxRev" then
        some (enc ⟨x.exists_, x.rev + 1, x.epoch, false, ba⟩) else none := by
  obtain ⟨e, r, ep, l, b⟩ := x
  simp only at he hb
  by_cases h1 : er ≤ p "maxRev" <;> by_cases h2 : ee ≤ p "maxEpoch" <;> by_cases h3 : r + 1 ≤ p "maxRev" <;>
    by_cases h4 : ba ≤ p "members" <;> by_cases h5 : ba ≤ b <;> cases e <;>
    cp_simp [h1, h2, h3, h4, h5, he, hb, RemOk] <;> ite_close

end KanderFormal.Proofs.Coordinator
