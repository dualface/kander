import KanderFormal.Proofs.ReviewObligationRel

/-
Preservation of the simulation relation by the events that move floors or take
batches out of the plan (reset, rebind, drop), the well-formedness of every
post-state, and the induction over accepted histories. The relation itself and
the events that keep identities and positions in step are in
`ReviewObligationRel.lean`.
-/
namespace KanderFormal.Proofs.ReviewObligation

section
variable {nr b : Nat} {st : St} {o : Obs}

theorem mem_debtsFor {nr' : Nat} {k : DebtKind} {s c : Nat} {g : Nat → Bool} {d : Debt} :
    d ∈ debtsFor nr' k s c g ↔ d.kind = k ∧ d.role < nr' ∧ g d.role = true ∧ d.since = s ∧ d.cycle = c := by
  simp only [debtsFor, List.mem_filterMap, List.mem_range]
  constructor
  · rintro ⟨r, hr, h⟩
    split at h
    · cases h; rename_i hg; exact ⟨rfl, hr, hg, rfl, rfl⟩
    · cases h
  · rintro ⟨h1, h2, h3, h4, h5⟩
    exact ⟨d.role, h2, by simp [h3]; cases d; simp_all⟩

theorem anyRole_debtsFor {nr' : Nat} {k : DebtKind} {s c : Nat} {g : Nat → Bool} {r : Nat} :
    ((debtsFor nr' k s c g).any fun d => d.kind == .recovery && d.role == r) =
      (decide (k = .recovery) && decide (r < nr') && g r) := by
  apply Bool.eq_iff_iff.mpr
  simp only [List.any_eq_true, mem_debtsFor, Bool.and_eq_true, beq_iff_eq, decide_eq_true_eq]
  constructor
  · rintro ⟨d, ⟨h1, h2, h3, -⟩, h5, rfl⟩; exact ⟨⟨h1 ▸ h5, h2⟩, h3⟩
  · rintro ⟨⟨h1, h2⟩, h3⟩; exact ⟨⟨k, r, s, c⟩, ⟨rfl, h2, h3, rfl, rfl⟩, h1, rfl⟩

theorem resetRole_eq (R : Rel nr st o) (hw : WF b st) {r : Nat} (hr : r < nr) : resetRoleS st r = resetRoles o r := by
  simp only [resetRoleS, resetRoles, anyReq_iff_live R hr, R.dropped r hr, Bool.or_assoc]
  congr 1
  have h := hw.recKind
  · rcases (show st.recKind = 0 ∨ st.recKind = 1 ∨ st.recKind = 2 by omega) with h0 | h1 | h2
    · have hn : o.npRecord = false := by
        cases hh : o.npRecord
        · rfl
        · have := R.np.mpr hh; omega
      simp [hn, (R.rec0 h0 r hr).1, (R.rec0 h0 r hr).2]
    · simp [R.np.mp h1, R.npRoles h1 r hr]
    · have hn : o.npRecord = false := by
        cases hh : o.npRecord
        · rfl
        · have := R.np.mpr hh; omega
      simp [hn, R.recRoles h2 r hr]

theorem keeps_eq (R : Rel nr st o) (hw : WF b st) :
    noneOf nr (resetRoleS st) = !((List.range nr).any (resetRoles o)) := by
  apply Bool.eq_iff_iff.mpr
  simp only [noneOf, decide_eq_true_eq, Bool.not_eq_true', List.any_eq_false, List.mem_range]
  constructor
  · intro h r hr; rw [← resetRole_eq R hw hr]; simp [h r hr]
  · intro h r hr; rw [resetRole_eq R hw hr]; simpa using h r hr

/-- The cycle part of a reset: no closure reviews the new cycle. -/
theorem cur_reset (R : Rel nr st o) {i : Nat} (h : i < o.batches.length) {r : Nat} :
    fillFn st.cur 0 (fun _ => false) i r =
      (o.batches[i].closed && decide (o.batches[i].closedCycle = o.cycle + 1) && o.batches[i].conc r) := by
  have := R.cycles _ (List.getElem_mem h)
  have hne : o.batches[i].closedCycle ≠ o.cycle + 1 := by omega
  simp [fillFn, hne]

theorem rel_reset (R : Rel nr st o) (hw : WF b st) : Rel nr (post nr st .reset) (observe nr o .reset) := by
  have hk := keeps_eq R hw
  have hpl := R.planned
  cases hp : o.planned
  · -- no plan: a no-plan record with no role, not released
    have hstp : st.planned = false := by rw [hpl, hp]
    have hnd : o.debts = [] := by
      cases hd : o.debts
      · rfl
      · have := R.debtPlanned (by simp [hd]); simp [hp] at this
    simp only [post, resetS, observe, hp, hstp, resetKeepsS, Bool.not_false, Bool.false_and, Bool.false_eq_true,
      ite_true, ite_false]
    constructor
    case planned => simp
    case bound => rfl
    case done => exact R.done
    case count => exact R.count
    case has => exact R.has
    case req => exact R.req
    case closed => exact R.closed
    case cur => exact fun i h r _ => cur_reset R h
    case sorted => exact R.sorted
    case ids => exact R.ids
    case cycles => exact fun x hx => Nat.le_succ_of_le (R.cycles x hx)
    case dropped => exact R.dropped
    case np => simp
    case npRoles => exact fun _ _ _ => rfl
    case npReleased => exact fun _ => rfl
    case recRoles => exact fun h => by simp at h
    case rec0 => exact fun h => by simp at h
    case npNoDebt => exact fun _ r => by simp [recDebtRole, hnd]
    case recDebt => simp [hnd]
    case dropDebt => simp [hnd]
    case debtRoles => simp [hnd]
    case debtSince => simp [hnd]
    case debtPlanned => simp [hnd]
    case cyc => exact R.cyc
    case covered => exact R.covered
    case added => exact fun x hx => ⟨Nat.le_succ_of_le (R.added x hx).1, (R.added x hx).2⟩
    case debtCycle => simp [hnd]
    case debtAdded => simp [hnd]
  · have hst : st.planned = true := by rw [hpl, hp]
    cases ha : (List.range nr).any (resetRoles o)
    · -- under a plan with no role: the record stays as it is
      have hkp : resetKeepsS nr st = true := by simp [resetKeepsS, hst, hk, ha]
      simp only [post, resetS, observe, hp, hst, hkp, ha, Bool.not_true, Bool.false_eq_true, ite_false, ite_true]
      constructor
      case planned => simp
      case bound => rfl
      case done => exact R.done
      case count => exact R.count
      case has => exact R.has
      case req => exact R.req
      case closed => exact R.closed
      case cur => exact fun i h r _ => cur_reset R h
      case sorted => exact R.sorted
      case ids => exact R.ids
      case cycles => exact fun x hx => Nat.le_succ_of_le (R.cycles x hx)
      case dropped => exact R.dropped
      case np => exact R.np
      case npRoles => exact R.npRoles
      case npReleased => exact R.npReleased
      case recRoles => exact R.recRoles
      case rec0 => exact R.rec0
      case npNoDebt => exact R.npNoDebt
      case recDebt => exact R.recDebt
      case dropDebt => exact R.dropDebt
      case debtRoles => exact R.debtRoles
      case debtSince => exact R.debtSince
      case debtPlanned => exact fun _ => rfl
      case cyc => exact R.cyc
      case covered => exact R.covered
      case added => exact fun x hx => ⟨Nat.le_succ_of_le (R.added x hx).1, (R.added x hx).2⟩
      case debtCycle => exact fun d hd => Nat.le_succ_of_le (R.debtCycle d hd)
      case debtAdded => exact R.debtAdded
    · -- under a plan with roles: planned recovery from the end of the plan
      have hkp : resetKeepsS nr st = false := by simp [resetKeepsS, hst, hk, ha]
      simp only [post, resetS, observe, hp, hst, hkp, ha, Bool.not_true, Bool.false_eq_true, ite_false, ite_true]
      have hrd : ∀ r, r < nr → recDebtRole o r = true → resetRoles o r = true := by
        intro r hr h
        cases hn : o.npRecord
        · simp [resetRoles, hn, h]
        · rw [R.npNoDebt hn r] at h; cases h
      constructor
      case planned => simp
      case bound => rfl
      case done => exact R.done
      case count => exact R.count
      case has => exact R.has
      case req => exact R.req
      case closed => exact R.closed
      case cur => exact fun i h r _ => cur_reset R h
      case sorted => exact R.sorted
      case ids => exact R.ids
      case cycles => exact fun x hx => Nat.le_succ_of_le (R.cycles x hx)
      case dropped => exact R.dropped
      case np => simp
      case npRoles => exact fun h => by simp at h
      case npReleased => exact fun h => by simp at h
      case recRoles =>
        intro _ r hr
        have h1 := hrd r hr
        simp only [recDebtRole, List.any_append, anyRole_debtsFor] at h1 ⊢
        rw [resetRole_eq R hw hr]
        cases h2 : o.debts.any (fun d => d.kind == .recovery && d.role == r) <;> simp_all
      case rec0 => exact fun h => by simp at h
      case npNoDebt => exact fun h => by simp at h
      case recDebt =>
        intro d hd _
        rcases List.mem_append.mp hd with hd | hd
        · have h1 := posOf_le_length o.batches d.since
          have h2 := R.count
          exact ⟨rfl, by simp only; omega⟩
        · obtain ⟨-, -, -, hs, -⟩ := mem_debtsFor.mp hd
          have h2 := R.count
          exact ⟨rfl, by simp only; rw [hs, posOf_all R.ids]; omega⟩
      case dropDebt =>
        intro d hd hk'
        rcases List.mem_append.mp hd with hd | hd
        · exact R.dropDebt d hd hk'
        · obtain ⟨h1, -⟩ := mem_debtsFor.mp hd
          rw [h1] at hk'; cases hk'
      case debtRoles =>
        intro d hd
        rcases List.mem_append.mp hd with hd | hd
        · exact R.debtRoles d hd
        · exact (mem_debtsFor.mp hd).2.1
      case debtSince =>
        intro d hd
        rcases List.mem_append.mp hd with hd | hd
        · exact R.debtSince d hd
        · exact Nat.le_of_eq (mem_debtsFor.mp hd).2.2.2.1
      case debtPlanned => exact fun _ => rfl
      case cyc => exact R.cyc
      case covered => exact R.covered
      case added => exact fun x hx => ⟨Nat.le_succ_of_le (R.added x hx).1, (R.added x hx).2⟩
      case debtCycle =>
        intro d hd
        rcases List.mem_append.mp hd with hd | hd
        · exact Nat.le_succ_of_le (R.debtCycle d hd)
        · exact Nat.le_of_eq (mem_debtsFor.mp hd).2.2.2.2
      case debtAdded =>
        intro d hd x hx hs
        rcases List.mem_append.mp hd with hd | hd
        · exact R.debtAdded d hd x hx hs
        · have := (mem_debtsFor.mp hd).2.2.2.1
          have := R.ids x hx
          omega

/-- `anyFrom` over the machine's positions from `k` is `any` over the
observer's batches from `k`. -/
theorem anyFrom_drop (R : Rel nr st o) {k : Nat} {g : Nat → Bool} {p : OBatch → Bool}
    (hgp : ∀ i (h : i < o.batches.length), g i = p o.batches[i]) :
    anyFrom st k g = (o.batches.drop k).any p := by
  apply Bool.eq_iff_iff.mpr
  rw [anyFrom_iff, List.any_eq_true]
  constructor
  · rintro ⟨i, hk, hi, h⟩
    rw [R.count] at hi
    refine ⟨o.batches[i], ?_, by rwa [← hgp i hi]⟩
    rw [List.mem_iff_getElem]
    exact ⟨i - k, by simp; omega, by simp [List.getElem_drop, show k + (i - k) = i by omega]⟩
  · rintro ⟨x, hx, h⟩
    obtain ⟨j, hj, rfl⟩ := List.getElem_of_mem hx
    simp only [List.length_drop] at hj
    simp only [List.getElem_drop] at h
    exact ⟨k + j, Nat.le_add_right _ _, by rw [R.count]; omega, by rw [hgp _ (by omega)]; exact h⟩

theorem memReq_eq (R : Rel nr st o) {k r : Nat} (hr : r < nr) : memReq st k r = tailMemberReq o k r :=
  anyFrom_drop R fun i h => by simp [R.has i h, R.req i h r hr]

theorem tailReq_eq (R : Rel nr st o) {k r : Nat} (hr : r < nr) : anyReq st k r = tailReq o k r :=
  anyFrom_drop R fun i h => by simp [R.req i h r hr]

/-- The machine's first open batch of the member is the observer's. -/
theorem firstOpen_rel (R : Rel nr st o) :
    (∃ k, ∃ hk : k < o.batches.length, firstOpenId o = some o.batches[k].id ∧ firstOpen st = k) ∨
      (firstOpenId o = none ∧ firstOpen st = st.count) := by
  have hg : ∀ i (h : i < o.batches.length), (st.has i && !st.closed i) = (o.batches[i].has && !o.batches[i].closed) :=
    fun i h => by rw [R.has i h, R.closed i h]
  cases hf : o.batches.find? (fun x => x.has && !x.closed) with
  | none =>
    right
    refine ⟨by simp [firstOpenId, hf], ?_⟩
    have hall := List.find?_eq_none.mp hf
    simp only [firstOpen]
    rw [List.find?_eq_none.mpr]
    · rfl
    · intro i hi
      obtain ⟨h1, h2⟩ := mem_indexRange.mp hi
      rw [R.count] at h2
      rw [hg i h2]
      exact hall _ (List.getElem_mem h2)
  | some x =>
    left
    obtain ⟨hx, k, hk, rfl, hbefore⟩ := List.find?_eq_some_iff_getElem.mp hf
    refine ⟨k, hk, by simp [firstOpenId, hf], ?_⟩
    simp only [firstOpen]
    have : (indexRange 0 st.count).find? (fun i => st.has i && !st.closed i) = some k := by
      rw [List.find?_eq_some_iff_getElem]
      refine ⟨by rw [hg k hk]; exact hx, k, by simp [indexRange, R.count]; omega, by simp [indexRange], ?_⟩
      intro j hj
      have hjl : j < o.batches.length := by omega
      simp only [indexRange, List.getElem_range', Nat.zero_add, Nat.one_mul]
      rw [hg j hjl]
      exact hbefore j hj
    rw [this]; rfl

theorem rel_rebind (R : Rel nr st o) :
    Rel nr (post nr st .rebind) (observe nr o .rebind) := by
  have hasg : assignedS st = o.batches.any (·.has) := by
    have := anyFrom_drop R (k := 0) (g := st.has) (p := (·.has)) fun i h => R.has i h
    simpa [assignedS] using this
  constructor
  case planned => exact R.planned
  case bound => rfl
  case done => exact R.done
  case count => exact R.count
  case has => exact R.has
  case req => exact R.req
  case closed => exact R.closed
  case cur => exact R.cur
  case sorted => exact R.sorted
  case ids => exact R.ids
  case cycles => exact R.cycles
  case dropped => exact R.dropped
  case np => exact R.np
  case npRoles => exact R.npRoles
  case npReleased => exact R.npReleased
  case recRoles => exact R.recRoles
  case rec0 => exact R.rec0
  case npNoDebt => exact R.npNoDebt
  case recDebt => exact R.recDebt
  case dropDebt => exact R.dropDebt
  case debtRoles => exact R.debtRoles
  case debtSince => exact R.debtSince
  case debtPlanned => exact R.debtPlanned
  case covered => exact R.covered
  case added => exact R.added
  case debtCycle => exact R.debtCycle
  case debtAdded => exact R.debtAdded
  case cyc =>
    intro s hs
    simp only [observe, post, rebindS] at hs ⊢
    cases ha : o.batches.any (·.has)
    · rw [ha] at hs
      simp only [Bool.false_eq_true, ite_false] at hs
      obtain ⟨h1, h2, h3⟩ := R.cyc s hs
      simp [hasg, ha, h1, h2, h3]
    · rw [ha] at hs
      simp only [ite_true, Option.some.injEq] at hs
      subst hs
      refine ⟨by simp [hasg, ha], ?_, ?_⟩
      · simp only [hasg, ha, ite_true]
        rcases firstOpen_rel R with ⟨k, hk, h1, h2⟩ | ⟨h1, h2⟩
        · rw [h1, h2]; exact posOf_at R.sorted hk
        · rw [h1, h2, R.count]; simp [posOf_all R.ids]
      · rcases firstOpen_rel R with ⟨k, hk, h1, -⟩ | ⟨h1, -⟩
        · rw [h1]; exact Nat.le_of_lt (R.ids _ (List.getElem_mem hk))
        · rw [h1]; exact Nat.le_refl _

theorem rel_drop {k : Nat} {d : Bool} (R : Rel nr st o) (hg : Guard nr b st (.drop k d)) :
    Rel nr (post nr st (.drop k d)) (observe nr o (.drop k d)) := by
  obtain ⟨hpl, -, hk, -⟩ := hg
  have hc := R.count
  have hkl : k < o.batches.length := by omega
  have htake : ∀ s, posOf (o.batches.take k) s ≤ posOf o.batches s ∧ posOf (o.batches.take k) s ≤ k :=
    fun _ => ⟨posOf_take_le, posOf_take_le_k⟩
  have hdone := R.done
  constructor
  case planned => exact R.planned
  case bound => exact R.bound
  case done => exact R.done
  case count => simp [post, dropS, observe]; omega
  case has =>
    intro i h
    simp only [observe, List.length_take] at h
    simp [post, dropS, observe, fillFn, show ¬ k ≤ i by omega, R.has i (by omega)]
  case req =>
    intro i h r hr
    simp only [observe, List.length_take] at h
    simp [post, dropS, observe, fillFn, show ¬ k ≤ i by omega, R.req i (by omega) r hr]
  case closed =>
    intro i h
    simp only [observe, List.length_take] at h
    simp [post, dropS, observe, fillFn, show ¬ k ≤ i by omega, R.closed i (by omega)]
  case cur =>
    intro i h r hr
    simp only [observe, List.length_take] at h
    simp [post, dropS, observe, fillFn, show ¬ k ≤ i by omega, R.cur i (by omega) r hr]
  case sorted =>
    intro i j hi hj hij
    simp only [observe, List.length_take] at hi hj
    simp only [observe, List.getElem_take]
    exact R.sorted i j _ _ hij
  case ids => exact fun x hx => R.ids x (List.mem_of_mem_take hx)
  case cycles => exact fun x hx => R.cycles x (List.mem_of_mem_take hx)
  case dropped =>
    intro r hr
    simp [post, dropS, observe, R.dropped r hr, memReq_eq R hr]
  case np => exact R.np
  case npRoles =>
    intro h r hr
    simp only [post, dropS] at h ⊢
    have hn := R.np.mp h
    simp [observe, h, hn, R.npRoles h r hr, tailReq_eq R hr]
  case npReleased => exact R.npReleased
  case recRoles =>
    intro h r hr
    simp only [post, dropS] at h ⊢
    have h1 : decide (st.recKind = 1) = false := by simp; omega
    simp only [h1, Bool.false_eq_true, ite_false]
    rw [R.recRoles h r hr]
    simp only [observe, recDebtRole]
    split <;> simp [List.any_append, anyRole_debtsFor]
  case rec0 =>
    intro h r hr
    simp only [post, dropS] at h ⊢
    obtain ⟨h1, h2⟩ := R.rec0 h r hr
    refine ⟨by simp [h, h1], ?_⟩
    simp only [observe, recDebtRole] at h2 ⊢
    split <;> simp [List.any_append, anyRole_debtsFor, h2]
  case npNoDebt =>
    intro h r
    have h2 := R.npNoDebt h r
    simp only [observe, recDebtRole] at h2 h ⊢
    split <;> simp [List.any_append, anyRole_debtsFor, h2]
  case recDebt =>
    intro e he hke
    have he' : e ∈ o.debts := by
      simp only [observe] at he
      split at he
      · rcases List.mem_append.mp he with he | he
        · exact he
        · rw [(mem_debtsFor.mp he).1] at hke; cases hke
      · exact he
    obtain ⟨h1, h2⟩ := R.recDebt e he' hke
    refine ⟨by simp [post, dropS, h1], ?_⟩
    obtain ⟨t1, t2⟩ := htake e.since
    show posOf (o.batches.take k) e.since ≤ (dropS st k d).recFloor
    simp only [dropS, h1, decide_true, Bool.true_and]
    split <;> omega
  case dropDebt =>
    intro e he hke
    simp only [observe] at he
    split at he
    · rename_i htk
      rcases List.mem_append.mp he with he | he
      · obtain ⟨h1, h2⟩ := R.dropDebt e he hke
        obtain ⟨t1, t2⟩ := htake e.since
        refine ⟨by simp [post, dropS, h1], ?_⟩
        show posOf (o.batches.take k) e.since ≤ (dropS st k d).dropFloor e.role
        simp only [dropS]
        split <;> (try split) <;> omega
      · obtain ⟨-, hr, hm, hs, -⟩ := mem_debtsFor.mp he
        have hmr : memReq st k e.role = true := by rw [memReq_eq R hr]; exact hm
        have ht : dropTakesS st d = true := by
          simp only [Bool.and_eq_true, Bool.not_eq_true'] at htk
          simp [dropTakesS, htk.1, hdone, htk.2]
        refine ⟨by simp [post, dropS, ht, hmr], ?_⟩
        show posOf (o.batches.take k) e.since ≤ (dropS st k d).dropFloor e.role
        simp only [dropS, ht, hmr, Bool.and_self, ite_true]
        exact posOf_take_le_k
    · obtain ⟨h1, h2⟩ := R.dropDebt e he hke
      obtain ⟨t1, t2⟩ := htake e.since
      refine ⟨by simp [post, dropS, h1], ?_⟩
      show posOf (o.batches.take k) e.since ≤ (dropS st k d).dropFloor e.role
      simp only [dropS]
      split <;> (try split) <;> omega
  case debtRoles =>
    intro e he
    simp only [observe] at he
    split at he
    · rcases List.mem_append.mp he with he | he
      · exact R.debtRoles e he
      · exact (mem_debtsFor.mp he).2.1
    · exact R.debtRoles e he
  case debtSince =>
    intro e he
    simp only [observe] at he
    split at he
    · rcases List.mem_append.mp he with he | he
      · exact R.debtSince e he
      · exact Nat.le_of_eq (mem_debtsFor.mp he).2.2.2.1
    · exact R.debtSince e he
  case debtPlanned =>
    intro _
    simp only [observe]
    rw [← R.planned]; exact hpl
  case cyc =>
    intro s hs
    obtain ⟨h1, h2, h3⟩ := R.cyc s hs
    obtain ⟨t1, t2⟩ := htake s
    refine ⟨h1, ?_, h3⟩
    show posOf (o.batches.take k) s ≤ (dropS st k d).cycFrom
    simp only [dropS]
    split <;> omega
  case covered => exact fun x hx => R.covered x (List.mem_of_mem_take hx)
  case added => exact fun x hx => R.added x (List.mem_of_mem_take hx)
  case debtCycle =>
    intro e he
    simp only [observe] at he
    split at he
    · rcases List.mem_append.mp he with he | he
      · exact R.debtCycle e he
      · exact Nat.le_of_eq (mem_debtsFor.mp he).2.2.2.2
    · exact R.debtCycle e he
  case debtAdded =>
    intro e he x hx hs
    have hx' := List.mem_of_mem_take hx
    simp only [observe] at he
    split at he
    · rcases List.mem_append.mp he with he | he
      · exact R.debtAdded e he x hx' hs
      · have := (mem_debtsFor.mp he).2.2.2.1
        have := R.ids x hx'
        omega
    · exact R.debtAdded e he x hx' hs


theorem wf_post {e : Ev} (hw : WF b st) (hg : Guard nr b st e) : WF b (post nr st e) := by
  obtain ⟨w1, w2, w3, w4, w5⟩ := hw
  cases e with
  | create => exact ⟨w1, w2, w3, w4, w5⟩
  | addBatch m rs => exact ⟨hg.2.1, w2, w3, w4, w5⟩
  | close k cs => exact ⟨w1, w2, w3, w4, w5⟩
  | newCycle => exact ⟨w1, w2, w3, w4, w5⟩
  | reset =>
    refine ⟨w1, w2, w3, ?_, ?_⟩ <;> simp only [post, resetS] <;> split <;> (try split) <;> omega
  | rebind =>
    refine ⟨w1, ?_, w3, w4, w5⟩
    simp only [post, rebindS]
    split
    · simp only [firstOpen]
      cases hf : (indexRange 0 st.count).find? (fun i => st.has i && !st.closed i) with
      | none => exact w1
      | some r => have := (find?_indexRange_some hf).2.1; simp only [Option.getD_some]; omega
    · exact w2
  | drop k d =>
    obtain ⟨-, -, hk, -⟩ := hg
    refine ⟨?_, ?_, fun r => ?_, w4, ?_⟩ <;> simp only [post, dropS] <;> (try split) <;> (try split) <;>
      first | omega | exact w3 r | (have := w3 r; omega)
  | dropTakes d => exact ⟨w1, w2, w3, w4, w5⟩
  | releaseDrop => exact ⟨w1, w2, fun r => by simp [post, releaseDropS, fillFn], w4, w5⟩
  | releaseRecovery => exact ⟨w1, w2, w3, w4, w5⟩
  | done c => exact ⟨w1, w2, w3, w4, w5⟩

theorem rel_post {e : Ev} (R : Rel nr st o) (hw : WF b st) (hg : Guard nr b st e) :
    Rel nr (post nr st e) (observe nr o e) := by
  cases e with
  | create => exact rel_create R
  | addBatch m rs => exact rel_addBatch R
  | close k cs => exact rel_close R hg
  | newCycle => exact rel_newCycle R
  | reset => exact rel_reset R hw
  | rebind => exact rel_rebind R
  | drop k d => exact rel_drop R hg
  | dropTakes d => exact R
  | releaseDrop => exact rel_releaseDrop R
  | releaseRecovery => exact rel_releaseRecovery R
  | done c => exact rel_done R

end

/-- Along every accepted history the machine state is related to the observer
of that history, and well formed. -/
theorem accepts_rel {nr b : Nat} {h : List Ev} {st : St} (ha : Accepts nr b h st) :
    Rel nr st (obsOf nr h) ∧ WF b st := by
  induction ha with
  | nil => exact ⟨rel_init nr, wf_init b⟩
  | snoc e _ hg ih =>
    rw [obsOf_snoc]
    exact ⟨rel_post ih.1 ih.2 hg, wf_post ih.2 hg⟩

end KanderFormal.Proofs.ReviewObligation
