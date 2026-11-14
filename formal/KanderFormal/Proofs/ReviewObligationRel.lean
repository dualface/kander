import KanderFormal.Proofs.ReviewObligationObserver

/-
The simulation between the machine and the independent observer, and the debt
safety it gives: whenever the machine accepts a completion, the observer finds
no review owed. This file holds the relation and its preservation by the events
that keep batch identities and positions in step (create, the releases, done,
new cycle, add batch, close); `ReviewObligationSafety.lean` completes it with
the events that move floors or take batches out (reset, rebind, drop).

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
  covered : ∀ x ∈ o.batches, x.closed = true → x.has = true → ∀ r, r < nr → x.req r = true → x.covered r = true
  added : ∀ x ∈ o.batches, x.addedCycle ≤ o.cycle ∧ (x.closed = true → x.addedCycle ≤ x.closedCycle)
  debtCycle : ∀ d ∈ o.debts, d.cycle ≤ o.cycle
  debtAdded : ∀ d ∈ o.debts, ∀ x ∈ o.batches, d.since ≤ x.id → d.cycle ≤ x.addedCycle

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
  covered := by simp [obsInit]
  added := by simp [obsInit]
  debtCycle := by simp [obsInit]
  debtAdded := by simp [obsInit]

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
theorem paid_of (R : Rel nr st o) {d : Debt} (hd : d ∈ o.debts) {fl : Nat} (hfl : posOf o.batches d.since ≤ fl)
    (hr : d.role < nr) (h : anyFrom st fl (fun i => st.has i && (st.req i d.role && st.closed i)) = true) :
    Paid o d := by
  obtain ⟨i, hi1, hi2, hg⟩ := anyFrom_iff.mp h
  rw [R.count] at hi2
  simp only [Bool.and_eq_true] at hg
  obtain ⟨h1, h2, h3⟩ := hg
  have hm := List.getElem_mem hi2
  have hid := le_id_of_posOf R.sorted hi2 (Nat.le_trans hfl hi1)
  have ha : o.batches[i].has = true := by rwa [← R.has i hi2]
  have hq : o.batches[i].req d.role = true := by rwa [← R.req i hi2 _ hr]
  have hc : o.batches[i].closed = true := by rwa [← R.closed i hi2]
  exact ⟨o.batches[i], hm, hid, ha, hq, hc,
    Nat.le_trans (R.debtAdded d hd _ hm hid) ((R.added _ hm).2 hc), R.covered _ hm hc ha d.role hr hq⟩

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
      exact paid_of R hd hfl hr this
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
      exact paid_of R hd hfl hr this
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
  covered := R.covered
  added := R.added
  debtCycle := R.debtCycle
  debtAdded := R.debtAdded

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
  covered := R.covered
  added := R.added
  debtCycle := fun d hd => R.debtCycle d (List.mem_filter.mp hd).1
  debtAdded := fun d hd => R.debtAdded d (List.mem_filter.mp hd).1

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
  covered := R.covered
  added := fun x hx => ⟨Nat.le_succ_of_le (R.added x hx).1, (R.added x hx).2⟩
  debtCycle := fun d hd => Nat.le_succ_of_le (R.debtCycle d hd)
  debtAdded := R.debtAdded

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
  case covered =>
    intro x hx
    simp only [observe, List.mem_append, List.mem_singleton] at hx
    rcases hx with hx | rfl
    · exact R.covered x hx
    · intro hc; simp at hc
  case added =>
    intro x hx
    simp only [observe, List.mem_append, List.mem_singleton] at hx
    rcases hx with hx | rfl
    · exact R.added x hx
    · exact ⟨Nat.le_refl _, by simp⟩
  case debtCycle => exact R.debtCycle
  case debtAdded =>
    intro d hd x hx hs
    simp only [observe, List.mem_append, List.mem_singleton] at hx
    rcases hx with hx | rfl
    · exact R.debtAdded d hd x hx hs
    · exact R.debtCycle d hd

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

theorem rel_close {k : Nat} {cs cv : Nat → Bool} (R : Rel nr st o) (hg : Guard nr b st (.close k cs cv)) :
    Rel nr (post nr st (.close k cs cv)) (observe nr o (.close k cs cv)) := by
  obtain ⟨-, -, hk, -, hcov⟩ := hg
  have hc := R.count
  have hids := modify_ids (l := o.batches) (k := k) (g := closeB o.cycle cs cv) (fun _ => rfl)
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
    have hi' : ((o.batches.modify k (closeB o.cycle cs cv))[i]'(by simpa using hi)).id = o.batches[i].id := by
      simp only [List.getElem_modify]; split <;> rfl
    have hj' : ((o.batches.modify k (closeB o.cycle cs cv))[j]'(by simpa using hj)).id = o.batches[j].id := by
      simp only [List.getElem_modify]; split <;> rfl
    simp only [observe]
    rw [hi', hj']
    exact R.sorted i j hi hj hij
  case ids =>
    intro x hx
    simp only [observe] at hx
    obtain ⟨i, hi, rfl⟩ := List.getElem_of_mem hx
    have hi2 : i < o.batches.length := by simpa using hi
    have : ((o.batches.modify k (closeB o.cycle cs cv))[i]'hi).id = o.batches[i].id := by
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
  case covered =>
    intro x hx
    simp only [observe] at hx
    obtain ⟨i, hi, rfl⟩ := List.getElem_of_mem hx
    have hi2 : i < o.batches.length := by simpa using hi
    simp only [List.getElem_modify]
    split
    · rename_i hki
      subst hki
      intro _ hh r hr hq
      simp only [closeB] at hh hq ⊢
      exact hcov (by rw [R.has _ hi2]; exact hh) r hr (by rw [R.req _ hi2 r hr]; exact hq)
    · exact R.covered _ (List.getElem_mem hi2)
  case added =>
    intro x hx
    simp only [observe] at hx
    obtain ⟨i, hi, rfl⟩ := List.getElem_of_mem hx
    have hi2 : i < o.batches.length := by simpa using hi
    have ha := R.added _ (List.getElem_mem hi2)
    simp only [List.getElem_modify]
    split
    · exact ⟨ha.1, fun _ => ha.1⟩
    · exact ha
  case debtCycle => exact R.debtCycle
  case debtAdded =>
    intro d hd x hx hs
    simp only [observe] at hx
    obtain ⟨i, hi, rfl⟩ := List.getElem_of_mem hx
    have hi2 : i < o.batches.length := by simpa using hi
    have e : ((o.batches.modify k (closeB o.cycle cs cv))[i]'hi).id = o.batches[i].id ∧
        ((o.batches.modify k (closeB o.cycle cs cv))[i]'hi).addedCycle = o.batches[i].addedCycle := by
      simp only [List.getElem_modify]; split <;> exact ⟨rfl, rfl⟩
    rw [e.1] at hs
    rw [e.2]
    exact R.debtAdded d hd _ (List.getElem_mem hi2) hs

end

end KanderFormal.Proofs.ReviewObligation
