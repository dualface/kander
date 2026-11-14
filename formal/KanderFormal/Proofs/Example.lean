import KanderFormal.Obligations
import KanderFormal.Machines.Example

/-
Proof obligations of the example machine.
-/
namespace KanderFormal.Proofs.Example

open KanderFormal KanderFormal.Machines

abbrev M := exampleMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The only state shape the machine's declared types allow. -/
def mk (ph ep i j : Nat) (x y z : Bool) : State :=
  [("phase", enumV ph), ("epoch", natV ep), ("reviews", .map [.enum i, .enum j]),
    ("reviewed", .set [x, y]), ("sealed", boolV z)]

theorem mk_inj {ph ep i j x y z ph' ep' i' j' x' y' z'} :
    mk ph ep i j x y z = mk ph' ep' i' j' x' y' z' →
      ph = ph' ∧ ep = ep' ∧ i = i' ∧ j = j' ∧ x = x' ∧ y = y' ∧ z = z' := by
  simp only [mk, enumV, natV, boolV, List.cons.injEq, Prod.mk.injEq, Val.scalar.injEq, Scalar.enum.injEq,
    Scalar.nat.injEq, Scalar.bool.injEq, Val.map.injEq, Val.set.injEq, and_true, true_and]
  intro h; simp_all

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ ph ep i j x y z, s = mk ph ep i j x y z ∧ ph < 3 ∧ ep ≤ p "maxEpoch" ∧ i < 3 ∧ j < 3 := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4), (_, v5)], hn =>
    simp [exampleMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "phase" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "epoch" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "reviews" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "reviewed" v4 (by simp [lookup])
    obtain ⟨t5, ht5, c5⟩ := hc "sealed" v5 (by simp [lookup])
    cases (rfl : M.fieldTy "phase" = some (.scalar (.enum "phase"))).symm.trans ht1
    cases (rfl : M.fieldTy "epoch" = some (.scalar (.nat "maxEpoch"))).symm.trans ht2
    cases (rfl : M.fieldTy "reviews" = some (.map "role" (.enum "verdict"))).symm.trans ht3
    cases (rfl : M.fieldTy "reviewed" = some (.set "role")).symm.trans ht4
    cases (rfl : M.fieldTy "sealed" = some (.scalar .bool)).symm.trans ht5
    obtain ⟨ph, rfl, hph⟩ := conforms_enum (rfl : M.enumSize "phase" = some 3) c1
    obtain ⟨ep, rfl, hep⟩ := conforms_nat c2
    obtain ⟨is, rfl, hlen, hlt⟩ := conforms_map_enum (rfl : M.enumSize "role" = some 2)
      (rfl : M.enumSize "verdict" = some 3) c3
    obtain ⟨bs, rfl, hbl⟩ := conforms_set (rfl : M.enumSize "role" = some 2) c4
    obtain ⟨z, rfl⟩ := conforms_bool c5
    match is, bs, hlen, hbl with
    | [i, j], [x, y], _, _ =>
      exact ⟨ph, ep, i, j, x, y, z, rfl, hph, hep, hlt i (by simp), hlt j (by simp)⟩

/-- The safety properties of the example machine, over the explicit components
of a state: phase `ph` (0 idle, 1 claimed, 2 done), epoch `ep`, verdicts `i`
and `j` of the two roles (0 none, 1 pass, 2 fail), reviewed flags `x` and `y`,
and the sealed flag `z`. -/
structure Props (ph ep i j : Nat) (x y z : Bool) : Prop where
  /-- A claimed or finished card has a positive epoch. -/
  claimedEpoch : ph ≠ 0 → 0 < ep
  /-- The card is sealed exactly when it is done. -/
  sealedDone : z = true ↔ ph = 2
  /-- A finished card has a passing verdict from every role. -/
  donePassed : ph = 2 → i = 1 ∧ j = 1
  /-- A role with a verdict is in the reviewed set. -/
  pmqaReviewed : i ≠ 0 → x = true
  securityReviewed : j ≠ 0 → y = true

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ ph ep i j x y z, s = mk ph ep i j x y z → Props ph ep i j x y z

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = claimEvent ∨ ev = reviewEvent ∨ ev = releaseEvent ∨ ev = finishEvent ∨ ev = bumpEvent := by
  simpa [exampleMachine] using h

theorem init : Inv p (initial M) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro ph ep i j x y z h
  obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (h.symm.trans (rfl : initial M = mk 0 0 0 0 false false false)).symm
  exact ⟨by simp, by simp, by simp, by simp, by simp⟩

theorem ctors_phase : M.ctorsOf "phase" = some ["idle", "claimed", "done"] := rfl
theorem ctors_role : M.ctorsOf "role" = some ["pmqa", "security"] := rfl
theorem ctors_verdict : M.ctorsOf "verdict" = some ["none", "pass", "fail"] := rfl
theorem size_role : M.enumSize "role" = some 2 := rfl
theorem fieldTy_phase : M.fieldTy "phase" = some (.scalar (.enum "phase")) := rfl
theorem fieldTy_epoch : M.fieldTy "epoch" = some (.scalar (.nat "maxEpoch")) := rfl
theorem fieldTy_reviews : M.fieldTy "reviews" = some (.map "role" (.enum "verdict")) := rfl
theorem fieldTy_reviewed : M.fieldTy "reviewed" = some (.set "role") := rfl
theorem fieldTy_sealed : M.fieldTy "sealed" = some (.scalar .bool) := rfl

/-- Evaluates guards and assignments against the example machine's
declarations. -/
macro "eval_simp" " at " h:ident : tactic =>
  `(tactic| simp [eval, evalCmp, evalAssigns, natAdd, mapGet, mapPut, setWith, boolAnd, lookup, mk,
      indexOf, enumV, natV, boolV, Ty.conforms, STy.conforms, Machine.enumSize, ctors_phase, ctors_role,
      ctors_verdict, fieldTy_phase, fieldTy_epoch, fieldTy_reviews, fieldTy_reviewed, fieldTy_sealed,
      claimEvent, reviewEvent, releaseEvent, finishEvent, bumpEvent] at $h:ident)

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨ph, ep, i, j, x, y, z, rfl, hph, hep, hi, hj⟩ := shape hc
  have P := hp ph ep i j x y z rfl
  obtain ⟨ev, args, us, hev, hargs, hg, hus, rfl⟩ := step_accepted h
  intro ph' ep' i' j' x' y' z' heq
  have hmem := lookup_map_mem hev
  clear h hp hc hev
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl
  · -- claim: idle to claimed, next epoch, verdicts and reviewed set cleared
    obtain ⟨-, rfl⟩ := bindArgs_nil hargs
    eval_simp at hg
    subst hg
    eval_simp at hus
    obtain ⟨-, rfl⟩ := hus
    have hpost : applyUpdates (mk 0 ep i j x y z)
        [("phase", enumV 1), ("epoch", natV (ep + 1)), ("reviews", .map [.enum 0, .enum 0]),
          ("reviewed", .set [false, false])] =
        mk 1 (ep + 1) 0 0 false false z := by
      simp [applyUpdates, mk, lookup]
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (hpost.symm.trans heq)
    exact ⟨by omega, by simp [P.sealedDone], by simp, by simp, by simp⟩
  · -- review: record the verdict of one role while claimed
    obtain ⟨v1, vs1, a1, -, c1, h1, rfl⟩ := bindArgs_cons hargs
    obtain ⟨v2, vs2, a2, rfl, c2, h2, rfl⟩ := bindArgs_cons h1
    obtain ⟨-, rfl⟩ := bindArgs_nil h2
    obtain ⟨r, rfl, hr⟩ := conforms_enum size_role c1
    obtain ⟨b, rfl⟩ := conforms_bool c2
    eval_simp at hg
    subst hg
    cases b
    · -- pass = false
      match r, hr with
      | 0, _ =>
        eval_simp at hus
        obtain ⟨-, rfl⟩ := hus
        have hpost : applyUpdates (mk 1 ep i j x y z)
            [("reviews", .map [.enum 2, .enum j]), ("reviewed", .set [true, y])] =
            mk 1 ep 2 j true y z := by
          simp [applyUpdates, mk, lookup]
        obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (hpost.symm.trans heq)
        exact ⟨fun _ => P.claimedEpoch (by omega), by simp [P.sealedDone], by omega, by simp,
          P.securityReviewed⟩
      | 1, _ =>
        eval_simp at hus
        obtain ⟨-, rfl⟩ := hus
        have hpost : applyUpdates (mk 1 ep i j x y z)
            [("reviews", .map [.enum i, .enum 2]), ("reviewed", .set [x, true])] =
            mk 1 ep i 2 x true z := by
          simp [applyUpdates, mk, lookup]
        obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (hpost.symm.trans heq)
        exact ⟨fun _ => P.claimedEpoch (by omega), by simp [P.sealedDone], by omega, P.pmqaReviewed,
          by simp⟩
      | _ + 2, h => omega
    · -- pass = true
      match r, hr with
      | 0, _ =>
        eval_simp at hus
        obtain ⟨-, rfl⟩ := hus
        have hpost : applyUpdates (mk 1 ep i j x y z)
            [("reviews", .map [.enum 1, .enum j]), ("reviewed", .set [true, y])] =
            mk 1 ep 1 j true y z := by
          simp [applyUpdates, mk, lookup]
        obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (hpost.symm.trans heq)
        exact ⟨fun _ => P.claimedEpoch (by omega), by simp [P.sealedDone], by omega, by simp,
          P.securityReviewed⟩
      | 1, _ =>
        eval_simp at hus
        obtain ⟨-, rfl⟩ := hus
        have hpost : applyUpdates (mk 1 ep i j x y z)
            [("reviews", .map [.enum i, .enum 1]), ("reviewed", .set [x, true])] =
            mk 1 ep i 1 x true z := by
          simp [applyUpdates, mk, lookup]
        obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (hpost.symm.trans heq)
        exact ⟨fun _ => P.claimedEpoch (by omega), by simp [P.sealedDone], by omega, P.pmqaReviewed,
          by simp⟩
      | _ + 2, h => omega
  · -- release: claimed back to idle
    obtain ⟨-, rfl⟩ := bindArgs_nil hargs
    eval_simp at hg
    subst hg
    eval_simp at hus
    obtain ⟨-, rfl⟩ := hus
    have hpost : applyUpdates (mk 1 ep i j x y z)
        [("phase", enumV 0)] =
        mk 0 ep i j x y z := by
      simp [applyUpdates, mk, lookup]
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (hpost.symm.trans heq)
    exact ⟨by simp, by simp [P.sealedDone], by simp, P.pmqaReviewed, P.securityReviewed⟩
  · -- finish: claimed with two passing verdicts to done, sealed
    obtain ⟨-, rfl⟩ := bindArgs_nil hargs
    eval_simp at hg
    obtain ⟨rfl, rfl, rfl⟩ := hg
    eval_simp at hus
    obtain ⟨-, rfl⟩ := hus
    have hpost : applyUpdates (mk 1 ep 1 1 x y z)
        [("phase", enumV 2), ("sealed", boolV true)] =
        mk 2 ep 1 1 x y true := by
      simp [applyUpdates, mk, lookup]
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (hpost.symm.trans heq)
    exact ⟨fun _ => P.claimedEpoch (by omega), by simp, by simp, P.pmqaReviewed, P.securityReviewed⟩
  · -- bump: raise the epoch while idle
    obtain ⟨v1, vs1, a1, -, c1, h1, rfl⟩ := bindArgs_cons hargs
    obtain ⟨-, rfl⟩ := bindArgs_nil h1
    obtain ⟨n, rfl, hn⟩ := conforms_nat c1
    eval_simp at hg
    obtain ⟨rfl, -, -⟩ := hg
    eval_simp at hus
    obtain ⟨-, rfl⟩ := hus
    have hpost : applyUpdates (mk 0 ep i j x y z)
        [("epoch", natV n)] =
        mk 0 n i j x y z := by
      simp [applyUpdates, mk, lookup]
    obtain ⟨rfl, rfl, rfl, rfl, rfl, rfl, rfl⟩ := mk_inj (hpost.symm.trans heq)
    exact ⟨by simp, by simp [P.sealedDone], by simp, P.pmqaReviewed, P.securityReviewed⟩

/-- The example machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := fun _ => init
  step := step_inv

/-- Every reachable state of the example machine satisfies the invariant, for
every value of `maxEpoch`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Corollary: a reachable sealed card is done and passed by every role. -/
theorem reachable_sealed_passed {p : Params} {ph ep i j : Nat} {x y : Bool}
    (h : Reachable M p (mk ph ep i j x y true)) : ph = 2 ∧ i = 1 ∧ j = 1 := by
  have P := (reachable_inv h).2 ph ep i j x y true rfl
  have hd := P.sealedDone.mp rfl
  exact ⟨hd, P.donePassed hd⟩
