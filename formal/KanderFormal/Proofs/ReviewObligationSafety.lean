import KanderFormal.Proofs.ReviewObligationObserver

/-
The simulation between the machine and the independent observer, and the debt
safety it gives: whenever the machine accepts a completion, the observer finds
no review owed.

`Rel` relates a machine state to the observer state of the same history. Every
relation about roles is stated for the `nr` roles of the enum. The relation on
floors is an inequality: the machine's position of a floor is at least the
position from which the observer's identities discharge the debt, so a closed
batch the machine counts always pays the observer's debt.
-/
namespace KanderFormal.Proofs.ReviewObligation

structure Rel (nr : Nat) (st : St) (o : Obs) : Prop where
  planned : st.planned = o.planned
  bound : st.bound = o.bound
  done : st.done = o.done
  count : st.count = o.batches.length
  has : ∀ i (h : i < o.batches.length), st.has i = o.batches[i].has
  req : ∀ i (h : i < o.batches.length) r, r < nr → st.req i r = o.batches[i].req r
  closed : ∀ i (h : i < o.batches.length), st.closed i = o.batches[i].closed
  cur : ∀ i (h : i < o.batches.length) r, r < nr →
    st.cur i r = (o.batches[i].closed && decide (o.batches[i].closedCycle = o.cycle) && o.batches[i].conc r)
  sorted : Sorted o.batches
  ids : ∀ x ∈ o.batches, x.id < o.nextId
  cycles : ∀ x ∈ o.batches, x.closedCycle ≤ o.cycle
  dropped : ∀ r, r < nr → st.droppedRoles r = o.dropped r
  np : st.recKind = 1 ↔ o.npRecord = true
  npRoles : st.recKind = 1 → ∀ r, r < nr → st.recRoles r = o.npRoles r
  npReleased : st.recKind = 1 → st.recReleased = o.npReleased
  recRoles : st.recKind = 2 → ∀ r, r < nr → st.recRoles r = recDebtRole o r
  rec0 : st.recKind = 0 → ∀ r, r < nr → st.recRoles r = false ∧ recDebtRole o r = false
  npNoDebt : o.npRecord = true → ∀ r, recDebtRole o r = false
  recDebt : ∀ d ∈ o.debts, d.kind = .recovery → st.recKind = 2 ∧ posOf o.batches d.since ≤ st.recFloor
  dropDebt : ∀ d ∈ o.debts, d.kind = .drop → st.dropRec d.role = true ∧ posOf o.batches d.since ≤ st.dropFloor d.role
  debtRoles : ∀ d ∈ o.debts, d.role < nr
  debtSince : ∀ d ∈ o.debts, d.since ≤ o.nextId
  debtPlanned : o.debts ≠ [] → o.planned = true
  cyc : ∀ s, o.cycSince = some s → st.cycNeed = true ∧ posOf o.batches s ≤ st.cycFrom ∧ s ≤ o.nextId

theorem rel_init (nr : Nat) : Rel nr initSt obsInit where
  planned := rfl
  bound := rfl
  done := rfl
  count := rfl
  has := fun _ h => by simp [obsInit] at h
  req := fun _ h => by simp [obsInit] at h
  closed := fun _ h => by simp [obsInit] at h
  cur := fun _ h => by simp [obsInit] at h
  sorted := fun _ _ h => by simp [obsInit] at h
  ids := by simp [obsInit]
  cycles := by simp [obsInit]
  dropped := fun _ _ => rfl
  np := by simp [initSt, obsInit]
  npRoles := by simp [initSt]
  npReleased := by simp [initSt]
  recRoles := by simp [initSt]
  rec0 := fun _ _ _ => ⟨rfl, by simp [recDebtRole, obsInit]⟩
  npNoDebt := by simp [obsInit]
  recDebt := by simp [obsInit]
  dropDebt := by simp [obsInit]
  debtRoles := by simp [obsInit]
  debtSince := by simp [obsInit]
  debtPlanned := by simp [obsInit]
  cyc := by simp [obsInit]

theorem wf_init (b : Nat) : WF b initSt := ⟨Nat.zero_le _, Nat.zero_le _, fun _ => Nat.zero_le _, by decide, Nat.zero_le _⟩

/-! ## Reading the machine through the relation -/

section
variable {nr : Nat} {st : St} {o : Obs}

theorem anyFrom_iff {lo : Nat} {g : Nat → Bool} :
    anyFrom st lo g = true ↔ ∃ i, lo ≤ i ∧ i < st.count ∧ g i = true := any_indexRange

theorem anyReq_iff_live (R : Rel nr st o) {r : Nat} (hr : r < nr) : anyReq st 0 r = liveReq o r := by
  apply Bool.eq_iff_iff.mpr
  rw [anyReq, anyFrom_iff, liveReq, List.any_eq_true]
  constructor
  · rintro ⟨i, -, hi, h⟩
    rw [R.count] at hi
    exact ⟨o.batches[i], List.getElem_mem hi, by rwa [← R.req i hi r hr]⟩
  · rintro ⟨x, hx, h⟩
    obtain ⟨i, hi, rfl⟩ := List.getElem_of_mem hx
    exact ⟨i, Nat.zero_le _, by rw [R.count]; exact hi, by rwa [R.req i hi r hr]⟩

/-- A batch at a machine position at or above the position of `s` pays a debt
from `s` on. -/
theorem paid_of (R : Rel nr st o) {d : Debt} {fl : Nat} (hfl : posOf o.batches d.since ≤ fl) (hr : d.role < nr)
    (h : anyFrom st fl (fun i => st.has i && (st.req i d.role && st.closed i)) = true) : Paid o d := by
  obtain ⟨i, hi1, hi2, hg⟩ := anyFrom_iff.mp h
  rw [R.count] at hi2
  simp only [Bool.and_eq_true] at hg
  obtain ⟨h1, h2, h3⟩ := hg
  refine ⟨o.batches[i], List.getElem_mem hi2, le_id_of_posOf R.sorted hi2 (Nat.le_trans hfl hi1), ?_, ?_, ?_⟩
  · rwa [← R.has i hi2]
  · rwa [← R.req i hi2 _ hr]
  · rwa [← R.closed i hi2]

theorem noneOf_false {g : Nat → Bool} (h : noneOf nr g = true) {r : Nat} (hr : r < nr) : g r = false := by
  simp only [noneOf, decide_eq_true_eq] at h; exact h r hr

/-- The payoff of the simulation: a state ready for completion leaves the
observer nothing owed. -/
theorem clear_of_doneReady (R : Rel nr st o) (h : doneReadyS nr st = true) : Clear nr o := by
  simp only [doneReadyS, Bool.and_eq_true, Bool.not_eq_true'] at h
  obtain ⟨-, -, -, -, -, hcyc, hrec, hdrop⟩ := h
  refine ⟨fun d hd => ?_, fun hnp => ?_, fun s hs => ?_⟩
  · have hr := R.debtRoles d hd
    cases hk : d.kind
    · obtain ⟨hrec', hfl⟩ := R.dropDebt d hd hk
      have := noneOf_false hdrop hr
      simp only [dropOwedR, hrec', Bool.true_and, Bool.not_eq_false', ite_true] at this
      exact paid_of R hfl hr this
    · obtain ⟨h2, hfl⟩ := R.recDebt d hd hk
      have hin : recRoleIn st d.role = true := by
        have : recDebtRole o d.role = true := List.any_eq_true.mpr ⟨d, hd, by simp [hk]⟩
        simp [recRoleIn, h2, R.recRoles h2 d.role hr, this]
      have hnone : noneOf nr (recRoleIn st) = false := by
        simp only [noneOf, decide_eq_false_iff_not]
        intro hall; have := hall _ hr; simp [hin] at this
      simp only [recOwed, hnone, Bool.false_eq_true, ite_false, Bool.and_eq_false_iff, Bool.not_eq_false'] at hrec
      have hrec2 : noneOf nr (recMissing st) = true := by
        rcases hrec with h0 | h1
        · simp [h2] at h0
        · exact h1
      have := noneOf_false hrec2 hr
      simp only [recMissing, hin, Bool.true_and, Bool.not_eq_false', recReviewed, h2, ite_true] at this
      exact paid_of R hfl hr this
  · have h1 : st.recKind = 1 := R.np.mpr hnp
    have happ : ∀ r, r < nr → recRoleIn st r = (o.npRoles r || liveReq o r) := fun r hr => by
      simp [recRoleIn, h1, R.npRoles h1 r hr, anyReq_iff_live R hr]
    refine ⟨fun hall => ?_, fun _ r hr hap => ?_⟩
    · have hnone : noneOf nr (recRoleIn st) = true := by
        simp only [noneOf, decide_eq_true_eq]; intro r hr; rw [happ r hr]; exact hall r hr
      simp only [recOwed, hnone, ite_true, Bool.and_eq_false_iff, Bool.not_eq_false'] at hrec
      rw [← R.npReleased h1]
      rcases hrec with h0 | h0
      · simp [h1] at h0
      · simpa using h0
    · have hin : recRoleIn st r = true := by rw [happ r hr]; exact hap
      have hnone : noneOf nr (recRoleIn st) = false := by
        simp only [noneOf, decide_eq_false_iff_not]
        intro hall; have := hall _ hr; simp [hin] at this
      simp only [recOwed, hnone, Bool.false_eq_true, ite_false, Bool.and_eq_false_iff, Bool.not_eq_false'] at hrec
      have hrec2 : noneOf nr (recMissing st) = true := by
        rcases hrec with h0 | h0
        · simp [h1] at h0
        · exact h0
      have := noneOf_false hrec2 hr
      simp only [recMissing, hin, Bool.true_and, Bool.not_eq_false', recReviewed, h1] at this
      simp only [show (1 : Nat) ≠ 2 by decide, ite_false, closedCur] at this
      obtain ⟨i, -, hi, hg⟩ := anyFrom_iff.mp this
      rw [R.count] at hi
      simp only [Bool.and_eq_true] at hg
      obtain ⟨ha, hq, hc, hu⟩ := hg
      rw [R.cur i hi r hr] at hu
      simp only [Bool.and_eq_true, decide_eq_true_eq] at hu
      exact ⟨o.batches[i], List.getElem_mem hi, by rwa [← R.has i hi], by rwa [← R.req i hi r hr],
        hu.1.1, hu.1.2, hu.2⟩
  · obtain ⟨hn, hfl, -⟩ := R.cyc s hs
    simp only [cycMissing, hn, Bool.true_and, Bool.not_eq_false'] at hcyc
    obtain ⟨i, hi1, hi2, hg⟩ := anyFrom_iff.mp hcyc
    rw [R.count] at hi2
    exact ⟨o.batches[i], List.getElem_mem hi2, by rwa [← R.has i hi2],
      le_id_of_posOf R.sorted hi2 (Nat.le_trans hfl hi1)⟩

end

/-! ## The relation is kept by every accepted event -/

section
variable {nr b : Nat} {st : St} {o : Obs}

theorem rel_create (R : Rel nr st o) : Rel nr (post nr st .create) (observe nr o .create) where
  planned := rfl
  bound := rfl
  done := R.done
  count := R.count
  has := R.has
  req := R.req
  closed := R.closed
  cur := R.cur
  sorted := R.sorted
  ids := R.ids
  cycles := R.cycles
  dropped := R.dropped
  np := R.np
  npRoles := R.npRoles
  npReleased := R.npReleased
  recRoles := R.recRoles
  rec0 := R.rec0
  npNoDebt := R.npNoDebt
  recDebt := R.recDebt
  dropDebt := R.dropDebt
  debtRoles := R.debtRoles
  debtSince := R.debtSince
  debtPlanned := fun _ => rfl
  cyc := R.cyc

theorem rel_releaseRecovery (R : Rel nr st o) : Rel nr (post nr st .releaseRecovery) (observe nr o .releaseRecovery) :=
  { R with npReleased := fun _ => rfl }

theorem rel_done {c : Bool} (R : Rel nr st o) : Rel nr (post nr st (.done c)) (observe nr o (.done c)) :=
  { R with done := rfl }

theorem rel_releaseDrop (R : Rel nr st o) : Rel nr (post nr st .releaseDrop) (observe nr o .releaseDrop) where
  planned := R.planned
  bound := R.bound
  done := R.done
  count := R.count
  has := R.has
  req := R.req
  closed := R.closed
  cur := R.cur
  sorted := R.sorted
  ids := R.ids
  cycles := R.cycles
  dropped := R.dropped
  np := R.np
  npRoles := R.npRoles
  npReleased := R.npReleased
  recRoles := fun h r hr => by
    simp only [post, releaseDropS] at h ⊢
    rw [R.recRoles h r hr]
    simp only [recDebtRole, observe, List.any_filter]
    apply Bool.eq_iff_iff.mpr
    simp only [List.any_eq_true, Bool.and_eq_true, beq_iff_eq]
    constructor
    · rintro ⟨d, hd, h1, h2⟩; exact ⟨d, hd, by simp [h1], h1, h2⟩
    · rintro ⟨d, hd, -, h1, h2⟩; exact ⟨d, hd, h1, h2⟩
  rec0 := fun h r hr => by
    refine ⟨(R.rec0 h r hr).1, ?_⟩
    have := (R.rec0 h r hr).2
    simp only [recDebtRole, observe, List.any_filter, List.any_eq_false, Bool.and_eq_true, beq_iff_eq] at this ⊢
    intro d hd ⟨_, h1, h2⟩
    exact this d hd ⟨h1, h2⟩
  npNoDebt := fun h r => by
    have := R.npNoDebt h r
    simp only [recDebtRole, observe, List.any_filter, List.any_eq_false, Bool.and_eq_true, beq_iff_eq] at this ⊢
    intro d hd ⟨_, h1, h2⟩
    exact this d hd ⟨h1, h2⟩
  recDebt := fun d hd hk => by
    simp only [observe, List.mem_filter] at hd
    exact R.recDebt d hd.1 hk
  dropDebt := fun d hd hk => by
    simp only [observe, List.mem_filter, hk] at hd
    simp at hd
  debtRoles := fun d hd => R.debtRoles d (List.mem_filter.mp hd).1
  debtSince := fun d hd => R.debtSince d (List.mem_filter.mp hd).1
  debtPlanned := fun h => R.debtPlanned (by
    intro hn; apply h; simp [observe, hn])
  cyc := R.cyc

theorem rel_newCycle (R : Rel nr st o) : Rel nr (post nr st .newCycle) (observe nr o .newCycle) where
  planned := R.planned
  bound := rfl
  done := R.done
  count := R.count
  has := R.has
  req := R.req
  closed := R.closed
  cur := fun i h r hr => by
    simp only [observe] at h ⊢
    have := R.cycles _ (List.getElem_mem h)
    simp only [post, newCycleS, fillFn, Nat.zero_le, ite_true]
    have hne : o.batches[i].closedCycle ≠ o.cycle + 1 := by omega
    simp [hne]
  sorted := R.sorted
  ids := R.ids
  cycles := fun x hx => Nat.le_succ_of_le (R.cycles x hx)
  dropped := R.dropped
  np := R.np
  npRoles := R.npRoles
  npReleased := R.npReleased
  recRoles := R.recRoles
  rec0 := R.rec0
  npNoDebt := R.npNoDebt
  recDebt := R.recDebt
  dropDebt := R.dropDebt
  debtRoles := R.debtRoles
  debtSince := R.debtSince
  debtPlanned := R.debtPlanned
  cyc := R.cyc

theorem snoc_cases {α : Type} {l : List α} {x : α} {i : Nat} (h : i < (l ++ [x]).length) :
    (∃ hi : i < l.length, (l ++ [x])[i] = l[i]) ∨ (i = l.length ∧ (l ++ [x])[i] = x) := by
  rcases Nat.lt_or_ge i l.length with hi | hi
  · exact .inl ⟨hi, List.getElem_append_left hi⟩
  · have : i = l.length := by simp at h; omega
    subst this
    exact .inr ⟨rfl, by simp⟩

theorem rel_addBatch {m : Bool} {rs : Nat → Bool} (R : Rel nr st o) :
    Rel nr (post nr st (.addBatch m rs)) (observe nr o (.addBatch m rs)) := by
  have hc := R.count
  constructor
  case planned => exact R.planned
  case bound => exact R.bound
  case done => exact R.done
  case count => simp [post, addBatchS, observe, hc]
  case has =>
    intro i h
    simp only [observe, List.length_append, List.length_singleton] at h
    by_cases hi : i < o.batches.length
    · simp [observe, post, addBatchS, upd, hc, hi, Nat.ne_of_lt hi, R.has i hi]
    · have : i = o.batches.length := by omega
      subst this; simp [observe, post, addBatchS, upd, hc]
  case req =>
    intro i h r hr
    simp only [observe, List.length_append, List.length_singleton] at h
    by_cases hi : i < o.batches.length
    · simp [observe, post, addBatchS, upd, hc, hi, Nat.ne_of_lt hi, R.req i hi r hr]
    · have : i = o.batches.length := by omega
      subst this; simp [observe, post, addBatchS, upd, hc]
  case closed =>
    intro i h
    simp only [observe, List.length_append, List.length_singleton] at h
    by_cases hi : i < o.batches.length
    · simp [observe, post, addBatchS, upd, hc, hi, Nat.ne_of_lt hi, R.closed i hi]
    · have : i = o.batches.length := by omega
      subst this; simp [observe, post, addBatchS, upd, hc]
  case cur =>
    intro i h r hr
    simp only [observe, List.length_append, List.length_singleton] at h
    by_cases hi : i < o.batches.length
    · simp [observe, post, addBatchS, upd, hc, hi, Nat.ne_of_lt hi, R.cur i hi r hr]
    · have : i = o.batches.length := by omega
      subst this; simp [observe, post, addBatchS, upd, hc]
  case sorted =>
    intro i j hi hj hij
    simp only [observe, List.length_append, List.length_singleton] at hi hj
    simp only [observe, List.getElem_append]
    split
    · split
      · exact R.sorted i j _ _ hij
      · have hj' : j - o.batches.length = 0 := by omega
        simp only [hj', List.getElem_cons_zero]
        exact R.ids _ (List.getElem_mem _)
    · omega
  case ids =>
    intro x hx
    simp only [observe, List.mem_append, List.mem_singleton] at hx
    rcases hx with hx | rfl
    · exact Nat.lt_succ_of_lt (R.ids x hx)
    · exact Nat.lt_succ_self _
  case cycles =>
    intro x hx
    simp only [observe, List.mem_append, List.mem_singleton] at hx
    rcases hx with hx | rfl
    · exact R.cycles x hx
    · exact Nat.zero_le _
  case dropped => exact R.dropped
  case np => exact R.np
  case npRoles => exact R.npRoles
  case npReleased => exact R.npReleased
  case recRoles => exact R.recRoles
  case rec0 => exact R.rec0
  case npNoDebt => exact R.npNoDebt
  case recDebt =>
    intro d hd hk
    obtain ⟨h1, h2⟩ := R.recDebt d hd hk
    exact ⟨h1, by simp only [observe]; rw [posOf_append_of_le (R.debtSince d hd)]; exact h2⟩
  case dropDebt =>
    intro d hd hk
    obtain ⟨h1, h2⟩ := R.dropDebt d hd hk
    exact ⟨h1, by simp only [observe]; rw [posOf_append_of_le (R.debtSince d hd)]; exact h2⟩
  case debtRoles => exact R.debtRoles
  case debtSince => exact fun d hd => Nat.le_succ_of_le (R.debtSince d hd)
  case debtPlanned => exact R.debtPlanned
  case cyc =>
    intro s hs
    obtain ⟨h1, h2, h3⟩ := R.cyc s hs
    exact ⟨h1, by simp only [observe]; rw [posOf_append_of_le h3]; exact h2, Nat.le_succ_of_le h3⟩

theorem posOf_eq_ids {l l' : List OBatch} (h : l.map OBatch.id = l'.map OBatch.id) (s : Nat) :
    posOf l s = posOf l' s := by
  have e : ∀ m : List OBatch, posOf m s = ((m.map OBatch.id).filter fun x => decide (x < s)).length := by
    intro m; simp [posOf, List.filter_map, Function.comp_def]
  rw [e, e, h]

theorem modify_ids {l : List OBatch} {k : Nat} {g : OBatch → OBatch} (hg : ∀ x, (g x).id = x.id) :
    (l.modify k g).map OBatch.id = l.map OBatch.id := by
  apply List.ext_getElem
  · simp
  · intro i h1 h2
    simp only [List.getElem_map, List.getElem_modify]
    split <;> simp [hg]

theorem rel_close {k : Nat} {cs : Nat → Bool} (R : Rel nr st o) (hg : Guard nr b st (.close k cs)) :
    Rel nr (post nr st (.close k cs)) (observe nr o (.close k cs)) := by
  obtain ⟨-, -, hk, -⟩ := hg
  have hc := R.count
  have hids := modify_ids (l := o.batches) (k := k) (g := closeB o.cycle cs) (fun _ => rfl)
  have hpos := posOf_eq_ids hids
  constructor
  case planned => exact R.planned
  case bound => exact R.bound
  case done => exact R.done
  case count => simp [post, closeS, observe, hc]
  case has =>
    intro i h
    simp only [observe, List.length_modify] at h
    simp only [observe, post, closeS, List.getElem_modify]
    split <;> simp [R.has i h, closeB]
  case req =>
    intro i h r hr
    simp only [observe, List.length_modify] at h
    simp only [observe, post, closeS, List.getElem_modify]
    split <;> simp [R.req i h r hr, closeB]
  case closed =>
    intro i h
    simp only [observe, List.length_modify] at h
    simp only [observe, post, closeS, List.getElem_modify, upd]
    split
    · rename_i hki; subst hki; simp [closeB]
    · rename_i hki; simp [Ne.symm hki, R.closed i h]
  case cur =>
    intro i h r hr
    simp only [observe, List.length_modify] at h
    simp only [observe, post, closeS, List.getElem_modify, upd]
    split
    · rename_i hki; subst hki; simp [closeB]
    · rename_i hki; simp [Ne.symm hki, R.cur i h r hr]
  case sorted =>
    intro i j hi hj hij
    simp only [observe, List.length_modify] at hi hj
    have hi' : ((o.batches.modify k (closeB o.cycle cs))[i]'(by simpa using hi)).id = o.batches[i].id := by
      simp only [List.getElem_modify]; split <;> rfl
    have hj' : ((o.batches.modify k (closeB o.cycle cs))[j]'(by simpa using hj)).id = o.batches[j].id := by
      simp only [List.getElem_modify]; split <;> rfl
    simp only [observe]
    rw [hi', hj']
    exact R.sorted i j hi hj hij
  case ids =>
    intro x hx
    simp only [observe] at hx
    obtain ⟨i, hi, rfl⟩ := List.getElem_of_mem hx
    have hi2 : i < o.batches.length := by simpa using hi
    have : ((o.batches.modify k (closeB o.cycle cs))[i]'hi).id = o.batches[i].id := by
      simp only [List.getElem_modify]; split <;> rfl
    rw [this]; exact R.ids _ (List.getElem_mem hi2)
  case cycles =>
    intro x hx
    simp only [observe] at hx
    obtain ⟨i, hi, rfl⟩ := List.getElem_of_mem hx
    have hi2 : i < o.batches.length := by simpa using hi
    simp only [List.getElem_modify]
    split
    · exact Nat.le_refl _
    · exact R.cycles _ (List.getElem_mem hi2)
  case dropped => exact R.dropped
  case np => exact R.np
  case npRoles => exact R.npRoles
  case npReleased => exact R.npReleased
  case recRoles => exact R.recRoles
  case rec0 => exact R.rec0
  case npNoDebt => exact R.npNoDebt
  case recDebt =>
    intro d hd hk
    obtain ⟨h1, h2⟩ := R.recDebt d hd hk
    exact ⟨h1, by simp only [observe]; rw [hpos]; exact h2⟩
  case dropDebt =>
    intro d hd hk
    obtain ⟨h1, h2⟩ := R.dropDebt d hd hk
    exact ⟨h1, by simp only [observe]; rw [hpos]; exact h2⟩
  case debtRoles => exact R.debtRoles
  case debtSince => exact R.debtSince
  case debtPlanned => exact R.debtPlanned
  case cyc =>
    intro s hs
    obtain ⟨h1, h2, h3⟩ := R.cyc s hs
    exact ⟨h1, by simp only [observe]; rw [hpos]; exact h2, h3⟩

theorem mem_debtsFor {nr' : Nat} {k : DebtKind} {s : Nat} {g : Nat → Bool} {d : Debt} :
    d ∈ debtsFor nr' k s g ↔ d.kind = k ∧ d.role < nr' ∧ g d.role = true ∧ d.since = s := by
  simp only [debtsFor, List.mem_filterMap, List.mem_range]
  constructor
  · rintro ⟨r, hr, h⟩
    split at h
    · cases h; rename_i hg; exact ⟨rfl, hr, hg, rfl⟩
    · cases h
  · rintro ⟨h1, h2, h3, h4⟩
    exact ⟨d.role, h2, by simp [h3]; cases d; simp_all⟩

theorem anyRole_debtsFor {nr' : Nat} {k : DebtKind} {s : Nat} {g : Nat → Bool} {r : Nat} :
    ((debtsFor nr' k s g).any fun d => d.kind == .recovery && d.role == r) =
      (decide (k = .recovery) && decide (r < nr') && g r) := by
  apply Bool.eq_iff_iff.mpr
  simp only [List.any_eq_true, mem_debtsFor, Bool.and_eq_true, beq_iff_eq, decide_eq_true_eq]
  constructor
  · rintro ⟨d, ⟨h1, h2, h3, -⟩, h5, rfl⟩; exact ⟨⟨h1 ▸ h5, h2⟩, h3⟩
  · rintro ⟨⟨h1, h2⟩, h3⟩; exact ⟨⟨k, r, s⟩, ⟨rfl, h2, h3, rfl⟩, h1, rfl⟩

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
        · obtain ⟨-, -, -, hs⟩ := mem_debtsFor.mp hd
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
        · exact Nat.le_of_eq (mem_debtsFor.mp hd).2.2.2
      case debtPlanned => exact fun _ => rfl
      case cyc => exact R.cyc

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
      · obtain ⟨-, hr, hm, hs⟩ := mem_debtsFor.mp he
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
      · exact Nat.le_of_eq (mem_debtsFor.mp he).2.2.2
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
