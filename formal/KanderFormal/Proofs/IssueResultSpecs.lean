import KanderFormal.Proofs.IssueResultInv

/-
The specification theorems of the issue result machine: the update relation of
a record (theorems 1 to 4), the client events (theorems 5 and 6). All of them
hold for every value of `maxN`.
-/
namespace KanderFormal.Proofs.IssueResult

open KanderFormal KanderFormal.Machines.IssueResult KanderFormal.Proofs.Scalar

/-! ## Theorems 1 and 2: the update relation of a record -/

/-- Theorem 1: a record of the old store passes `ValidateResultStoreUpdate` when
the new store has it (`present`); a record that is not `rejected` keeps its
status unless it was `uncertain`, and keeps its body, author, and comment
(`identSame`); the failures only grow (`nfNew`, `prefixF`); the close decisions
are not fewer (`ndNew`). -/
theorem update_spec (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (is : Bool) (nfn : Nat) (pf : Bool) (ndn : Nat) :
    step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩ ≠ none ↔
      ns < 6 ∧ nfn ≤ p "maxN" ∧ ndn ≤ p "maxN" ∧ pr = true ∧
        (x.st = 4 ∨ ((x.st = 1 ∨ ns = x.st) ∧ is = true)) ∧ x.nf ≤ nfn ∧ pf = true ∧ x.nd ≤ ndn := by
  rw [step_update]
  by_cases h : ns < 6 ∧ nfn ≤ p "maxN" ∧ ndn ≤ p "maxN" ∧ pr = true ∧
    (x.st = 4 ∨ ((x.st = 1 ∨ ns = x.st) ∧ is = true)) ∧ x.nf ≤ nfn ∧ pf = true ∧ x.nd ≤ ndn <;> simp [h]

theorem update_keeps (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (is : Bool) (nfn : Nat) (pf : Bool) (ndn : Nat)
    {s' : State}
    (h : step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩ = some s') :
    s' = enc x := by
  rw [step_update] at h; exact (ite_some_enc h).2

/-- Theorem 1: the history is append-only. The new store has the record, its
failures extend the old ones, and it has at least as many close decisions. -/
theorem update_append_only (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (is : Bool) (nfn : Nat) (pf : Bool)
    (ndn : Nat)
    (h : step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩ ≠ none) :
    pr = true ∧ x.nf ≤ nfn ∧ pf = true ∧ x.nd ≤ ndn := by
  obtain ⟨-, -, -, h4, -, h6, h7, h8⟩ := (update_spec p x pr ns is nfn pf ndn).mp h
  exact ⟨h4, h6, h7, h8⟩

/-- Theorem 2: a `published` or `equivalent` record keeps its status, body,
author, and comment. -/
theorem update_published_frozen (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (is : Bool) (nfn : Nat) (pf : Bool)
    (ndn : Nat) (hx : x.st = 2 ∨ x.st = 3)
    (h : step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩ ≠ none) :
    ns = x.st ∧ is = true := by
  obtain ⟨-, -, -, -, h5, -⟩ := (update_spec p x pr ns is nfn pf ndn).mp h
  rcases h5 with h5 | ⟨h5 | h5, h6⟩
  · omega
  · omega
  · exact ⟨h5, h6⟩

/-- Theorem 2: an `uncertain` record may become any valid status, including
`uncertain` with other assessment fields, as long as its body, author, and
comment stay. -/
theorem update_uncertain_any (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (nfn : Nat) (ndn : Nat) (hx : x.st = 1)
    (hns : ns < 6) (hf : nfn ≤ p "maxN") (hd : ndn ≤ p "maxN") (hp : pr = true) (hnf : x.nf ≤ nfn)
    (hnd : x.nd ≤ ndn) :
    step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV true, natV nfn, boolV true, natV ndn]⟩ ≠ none := by
  rw [update_spec]; exact ⟨hns, hf, hd, hp, .inr ⟨.inl hx, rfl⟩, hnf, rfl, hnd⟩

/-- Theorem 2: a `rejected` record has no status constraint in the update
relation: only its presence, its failures, and its decisions are compared. The
work relation lets it stay as it is or be replaced as a new record. -/
theorem update_rejected_spec (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (is : Bool) (nfn : Nat) (pf : Bool)
    (ndn : Nat) (hx : x.st = 4) :
    step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩ ≠ none ↔
      ns < 6 ∧ nfn ≤ p "maxN" ∧ ndn ≤ p "maxN" ∧ pr = true ∧ x.nf ≤ nfn ∧ pf = true ∧ x.nd ≤ ndn := by
  rw [update_spec]; simp [hx]

/-- Theorem 1: a store that has a record and a failure history can only be
replaced by a store that has the record. -/
theorem update_refused_removed (p : Params) (x : Rq) (ns : Nat) (is : Bool) (nfn : Nat) (pf : Bool) (ndn : Nat) :
    step M p (enc x) ⟨"update", [boolV false, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩ = none := by
  rw [step_update]; apply ite_none; intro c; obtain ⟨-, -, -, c4, -⟩ := c; simp at c4

theorem update_refused_failures (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (is : Bool) (nfn : Nat) (ndn : Nat)
    (hn : nfn < x.nf) :
    step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV true, natV ndn]⟩ = none := by
  rw [step_update]; apply ite_none; intro c; obtain ⟨-, -, -, -, -, c6, -⟩ := c; omega

theorem update_refused_prefix (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (is : Bool) (nfn : Nat) (ndn : Nat) :
    step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV false, natV ndn]⟩ = none := by
  rw [step_update]; apply ite_none; intro c; obtain ⟨-, -, -, -, -, -, c7, -⟩ := c; simp at c7

theorem update_refused_decisions (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (is : Bool) (nfn : Nat) (pf : Bool)
    (ndn : Nat) (hn : ndn < x.nd) :
    step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩ = none := by
  rw [step_update]; apply ite_none; intro c; obtain ⟨-, -, -, -, -, -, -, c8⟩ := c; omega

/-- Theorem 2: a `published` or `equivalent` record whose status changed, and a
record that is not `rejected` whose body, author, or comment changed, is
refused. -/
theorem update_refused_regress (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (nfn : Nat) (pf : Bool) (ndn : Nat)
    (hx : x.st = 2 ∨ x.st = 3) (hn : ns ≠ x.st) (is : Bool) :
    step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩ = none := by
  rw [step_update]; apply ite_none; intro c; obtain ⟨-, -, -, -, c5, -⟩ := c; rcases c5 with c5 | ⟨c5, -⟩ <;> omega

theorem update_refused_identity (p : Params) (x : Rq) (pr : Bool) (ns : Nat) (nfn : Nat) (pf : Bool) (ndn : Nat)
    (hx : x.st ≠ 4) :
    step M p (enc x) ⟨"update", [boolV pr, enumV ns, boolV false, natV nfn, boolV pf, natV ndn]⟩ = none := by
  rw [step_update]; apply ite_none; intro c; obtain ⟨-, -, -, -, c5, -⟩ := c; rcases c5 with c5 | ⟨-, c5⟩ <;> simp_all

/-! ## Theorems 2 and 3: the work relation of a record -/

/-- Theorem 3: a record of the new store passes `ValidateResultStoreWork` when
it is a `rejected` record that did not change; or it is not new and keeps its
verification evidence; or it is new (absent or `rejected` before) and
`uncertain` or `equivalent`, no other record of the old store is `uncertain`,
and it matches the current work set. -/
theorem work_spec (p : Params) (x : Rq) (ns : Nat) (id cs ws vo : Bool) :
    step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV ws, boolV vo]⟩ ≠ none ↔
      ns < 6 ∧ ((x.st = 4 ∧ id = true) ∨ ((x.st = 1 ∨ x.st = 2 ∨ x.st = 3) ∧ cs = true) ∨
        ((x.st = 0 ∨ x.st = 4) ∧ (ns = 1 ∨ ns = 3) ∧ x.unc = 0 ∧ ws = true ∧ vo = true)) := by
  rw [step_work]
  by_cases h : ns < 6 ∧ ((x.st = 4 ∧ id = true) ∨ ((x.st = 1 ∨ x.st = 2 ∨ x.st = 3) ∧ cs = true) ∨
        ((x.st = 0 ∨ x.st = 4) ∧ (ns = 1 ∨ ns = 3) ∧ x.unc = 0 ∧ ws = true ∧ vo = true)) <;> simp [h]

theorem work_keeps (p : Params) (x : Rq) (ns : Nat) (id cs ws vo : Bool) {s' : State}
    (h : step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV ws, boolV vo]⟩ = some s') :
    s' = enc x := by
  rw [step_work] at h; exact (ite_some_enc h).2

/-- Theorem 3: a new record (absent or `rejected` before, and not an unchanged
`rejected` record) is `uncertain` or `equivalent`, no other record was
`uncertain`, and it matches the current work set (digest, evidence, members,
commits) and the version computed from the current assessment. -/
theorem work_new_record (p : Params) (x : Rq) (ns : Nat) (id cs ws vo : Bool) (hx : x.st = 0 ∨ x.st = 4)
    (hid : ¬(x.st = 4 ∧ id = true))
    (h : step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV ws, boolV vo]⟩ ≠ none) :
    (ns = 1 ∨ ns = 3) ∧ x.unc = 0 ∧ ws = true ∧ vo = true := by
  obtain ⟨-, h2⟩ := (work_spec p x ns id cs ws vo).mp h
  rcases h2 with h2 | ⟨h2, -⟩ | h2
  · exact absurd h2 hid
  · omega
  · exact h2.2

/-- Theorem 2: a `rejected` record stays exactly as it is, or is replaced by a
new record. -/
theorem work_rejected_only (p : Params) (x : Rq) (ns : Nat) (id cs ws vo : Bool) (hx : x.st = 4)
    (h : step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV ws, boolV vo]⟩ ≠ none) :
    id = true ∨ ((ns = 1 ∨ ns = 3) ∧ x.unc = 0 ∧ ws = true ∧ vo = true) := by
  obtain ⟨-, h2⟩ := (work_spec p x ns id cs ws vo).mp h
  rcases h2 with ⟨-, h2⟩ | ⟨h2, -⟩ | h2
  · exact .inl h2
  · omega
  · exact .inr h2.2

/-- Theorem 3: a record that is not new keeps its verification evidence. -/
theorem work_kept (p : Params) (x : Rq) (ns : Nat) (id cs ws vo : Bool) (hx : x.st = 1 ∨ x.st = 2 ∨ x.st = 3)
    (h : step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV ws, boolV vo]⟩ ≠ none) : cs = true := by
  obtain ⟨-, h2⟩ := (work_spec p x ns id cs ws vo).mp h
  rcases h2 with ⟨h2, -⟩ | ⟨-, h2⟩ | h2
  · omega
  · exact h2
  · omega

/-- Theorem 3: a new record is refused when another record of the old store is
`uncertain`. -/
theorem work_refused_other_uncertain (p : Params) (x : Rq) (ns : Nat) (id cs ws vo : Bool) (hx : x.st = 0 ∨ x.st = 4)
    (hu : x.unc ≠ 0) (hid : id = false ∨ x.st = 0) :
    step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV ws, boolV vo]⟩ = none := by
  rw [step_work]; apply ite_none; intro c; obtain ⟨-, c2⟩ := c
  rcases c2 with ⟨c2, c3⟩ | ⟨c2, -⟩ | ⟨-, -, c4, -⟩
  · rcases hid with hid | hid <;> simp_all
  · omega
  · exact hu c4

/-- Theorem 3: a new record that is not `uncertain` or `equivalent` is refused
(a publication needs its preceding uncertain intent). -/
theorem work_refused_status (p : Params) (x : Rq) (ns : Nat) (id cs ws vo : Bool) (hx : x.st = 0 ∨ x.st = 4)
    (hs : ns ≠ 1 ∧ ns ≠ 3) (hid : id = false ∨ x.st = 0) :
    step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV ws, boolV vo]⟩ = none := by
  rw [step_work]; apply ite_none; intro c; obtain ⟨-, c2⟩ := c
  rcases c2 with ⟨c2, c3⟩ | ⟨c2, -⟩ | ⟨-, c3, -⟩
  · rcases hid with hid | hid <;> simp_all
  · omega
  · omega

/-- Theorem 3: a new record that differs from the current work set or from the
current assessment is refused. -/
theorem work_refused_work (p : Params) (x : Rq) (ns : Nat) (id cs vo : Bool) (hx : x.st = 0 ∨ x.st = 4)
    (hid : id = false ∨ x.st = 0) :
    step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV false, boolV vo]⟩ = none ∧
    step M p (enc x) ⟨"work", [enumV ns, boolV id, boolV cs, boolV true, boolV false]⟩ = none := by
  refine ⟨?_, ?_⟩ <;> (rw [step_work]; apply ite_none; intro c; obtain ⟨-, c2⟩ := c
                       rcases c2 with ⟨c2, c3⟩ | ⟨c2, -⟩ | ⟨-, -, -, c5, c6⟩
                       · rcases hid with hid | hid <;> simp_all
                       · omega
                       · simp_all)

/-- Theorem 1: a store that knew its issue identity keeps it. -/
theorem idKeep_spec (p : Params) (x : Rq) (same : Bool) :
    step M p (enc x) ⟨"idKeep", [boolV same]⟩ ≠ none ↔ (x.iid = false ∨ same = true) := by
  rw [step_idKeep]
  by_cases h : x.iid = false ∨ same = true <;> simp [h]

theorem idKeep_refused (p : Params) (x : Rq) (h : x.iid = true) :
    step M p (enc x) ⟨"idKeep", [boolV false]⟩ = none := by
  rw [step_idKeep]; apply ite_none; simp [h]

/-- Theorem 1: a record is valid when its status is one the store writes, its
author is positive, a `published` or `equivalent` record has a positive comment
ID, its outcomes are valid, and a fully resolved record has only `met`
outcomes. -/
theorem valid_spec (p : Params) (x : Rq) (vs : Nat) (ap cp oo nf nm : Bool) :
    step M p (enc x) ⟨"valid", [enumV vs, boolV ap, boolV cp, boolV oo, boolV nf, boolV nm]⟩ ≠ none ↔
      vs < 6 ∧ (vs = 1 ∨ vs = 2 ∨ vs = 3 ∨ vs = 4) ∧ ap = true ∧ ((vs ≠ 2 ∧ vs ≠ 3) ∨ cp = true) ∧ oo = true ∧
        (nf = false ∨ nm = true) := by
  rw [step_valid]
  by_cases h : vs < 6 ∧ (vs = 1 ∨ vs = 2 ∨ vs = 3 ∨ vs = 4) ∧ ap = true ∧ ((vs ≠ 2 ∧ vs ≠ 3) ∨ cp = true) ∧
    oo = true ∧ (nf = false ∨ nm = true) <;> simp [h]

theorem valid_refused (p : Params) (x : Rq) (vs : Nat) (ap cp oo nf nm : Bool) :
    ((vs = 0 ∨ vs = 5) →
      step M p (enc x) ⟨"valid", [enumV vs, boolV ap, boolV cp, boolV oo, boolV nf, boolV nm]⟩ = none) ∧
    (ap = false →
      step M p (enc x) ⟨"valid", [enumV vs, boolV ap, boolV cp, boolV oo, boolV nf, boolV nm]⟩ = none) ∧
    ((vs = 2 ∨ vs = 3) → cp = false →
      step M p (enc x) ⟨"valid", [enumV vs, boolV ap, boolV cp, boolV oo, boolV nf, boolV nm]⟩ = none) ∧
    (oo = false →
      step M p (enc x) ⟨"valid", [enumV vs, boolV ap, boolV cp, boolV oo, boolV nf, boolV nm]⟩ = none) ∧
    (nf = true → nm = false →
      step M p (enc x) ⟨"valid", [enumV vs, boolV ap, boolV cp, boolV oo, boolV nf, boolV nm]⟩ = none) := by
  refine ⟨fun h => ?_, fun h => ?_, fun h1 h2 => ?_, fun h => ?_, fun h1 h2 => ?_⟩ <;>
    (rw [step_valid]; apply ite_none; intro c; obtain ⟨-, c2, c3, c4, c5, c6⟩ := c; simp_all <;> omega)

/-- Theorem 1: a store with records has a positive issue identity. -/
theorem issueNeeded_spec (p : Params) (x : Rq) (hr ip : Bool) :
    step M p (enc x) ⟨"issueNeeded", [boolV hr, boolV ip]⟩ ≠ none ↔ (hr = false ∨ ip = true) := by
  rw [step_issueNeeded]
  by_cases h : hr = false ∨ ip = true <;> simp [h]

/-! ## Theorem 5: the client entrances -/

/-- Theorem 5: `ApplyResult` writes nothing while any record of the store is
`uncertain`: no new record, no equivalent record, and no reassessment. -/
theorem apply_refused_uncertain (p : Params) (x : Rq) (hu : x.unc ≠ 0) :
    (∀ nf nm dp bo, step M p (enc x) ⟨"intent", [boolV nf, boolV nm, boolV dp, boolV bo]⟩ = none) ∧
    (∀ nf nm fo, step M p (enc x) ⟨"equivalent", [boolV nf, boolV nm, boolV fo]⟩ = none) ∧
    step M p (enc x) ⟨"reassess", []⟩ = none := by
  refine ⟨fun nf nm dp bo => ?_, fun nf nm fo => ?_, ?_⟩
  · rw [step_intent]; apply ite_none; intro c; obtain ⟨-, -, c3, -⟩ := c; exact hu c3
  · rw [step_equivalent]; apply ite_none; intro c; obtain ⟨-, c2, -⟩ := c; exact hu c2
  · rw [step_reassess]; apply ite_none; intro c; exact hu c.1

/-- Theorem 5: on a reachable state, an `uncertain` record blocks every
`ApplyResult` write. -/
theorem apply_refused_uncertain_record {p : Params} {x : Rq} (hr : Reachable M p (enc x)) (hs : x.st = 1) :
    (∀ nf nm dp bo, step M p (enc x) ⟨"intent", [boolV nf, boolV nm, boolV dp, boolV bo]⟩ = none) ∧
    (∀ nf nm fo, step M p (enc x) ⟨"equivalent", [boolV nf, boolV nm, boolV fo]⟩ = none) ∧
    step M p (enc x) ⟨"reassess", []⟩ = none :=
  apply_refused_uncertain p x (by have := reachable_uncertain_counted hr hs; omega)

/-- Theorem 5: a reassessment keeps a `published` or `equivalent` record. -/
theorem reassess_spec (p : Params) (x : Rq) :
    step M p (enc x) ⟨"reassess", []⟩ ≠ none ↔ x.unc = 0 ∧ (x.st = 2 ∨ x.st = 3) := by
  rw [step_reassess]
  by_cases h : x.unc = 0 ∧ (x.st = 2 ∨ x.st = 3) <;> simp [h]

/-- Theorem 5: a new record is written `uncertain` before the remote comment is
posted, only when no record is `uncertain`, the completion evidence is not
already published, and the body is valid. It carries no decision. -/
theorem intent_spec (p : Params) (x : Rq) (nf nm dp bo : Bool) :
    step M p (enc x) ⟨"intent", [boolV nf, boolV nm, boolV dp, boolV bo]⟩ ≠ none ↔
      x.unc + 1 ≤ p "maxN" ∧ x.iid = true ∧ x.unc = 0 ∧ (x.st = 0 ∨ x.st = 4) ∧ dp = false ∧ bo = true ∧
        (nf = false ∨ nm = true) := by
  rw [step_intent]
  by_cases h : x.unc + 1 ≤ p "maxN" ∧ x.iid = true ∧ x.unc = 0 ∧ (x.st = 0 ∨ x.st = 4) ∧ dp = false ∧
    bo = true ∧ (nf = false ∨ nm = true) <;> simp [h] <;> omega

theorem intent_result (p : Params) (x : Rq) (nf nm dp bo : Bool) {s' : State}
    (h : step M p (enc x) ⟨"intent", [boolV nf, boolV nm, boolV dp, boolV bo]⟩ = some s') :
    s' = enc { x with st := 1, unc := x.unc + 1, full := nf, met := nm, cpos := false, nd := 0 } := by
  rw [step_intent] at h; exact (ite_some_enc h).2

theorem intent_refused (p : Params) (x : Rq) (nf nm : Bool) :
    (∀ bo, step M p (enc x) ⟨"intent", [boolV nf, boolV nm, boolV true, boolV bo]⟩ = none) ∧
    (∀ dp, step M p (enc x) ⟨"intent", [boolV nf, boolV nm, boolV dp, boolV false]⟩ = none) ∧
    (x.st = 1 ∨ x.st = 2 ∨ x.st = 3 → ∀ dp bo,
      step M p (enc x) ⟨"intent", [boolV nf, boolV nm, boolV dp, boolV bo]⟩ = none) := by
  refine ⟨fun bo => ?_, fun dp => ?_, fun hs dp bo => ?_⟩ <;>
    (rw [step_intent]; apply ite_none; intro c; obtain ⟨-, -, -, c4, c5, c6, -⟩ := c
     first | omega | (simp at c5) | (simp at c6))

/-- Theorem 5: the agent names an existing comment as the equivalent result: the
new record is `equivalent`, and it has a comment. -/
theorem equivalent_spec (p : Params) (x : Rq) (nf nm fo : Bool) :
    step M p (enc x) ⟨"equivalent", [boolV nf, boolV nm, boolV fo]⟩ ≠ none ↔
      x.iid = true ∧ x.unc = 0 ∧ (x.st = 0 ∨ x.st = 4) ∧ fo = true ∧ (nf = false ∨ nm = true) := by
  rw [step_equivalent]
  by_cases h : x.iid = true ∧ x.unc = 0 ∧ (x.st = 0 ∨ x.st = 4) ∧ fo = true ∧ (nf = false ∨ nm = true) <;>
    simp [h]

theorem equivalent_result (p : Params) (x : Rq) (nf nm fo : Bool) {s' : State}
    (h : step M p (enc x) ⟨"equivalent", [boolV nf, boolV nm, boolV fo]⟩ = some s') :
    s' = enc { x with st := 3, cpos := true, full := nf, met := nm, nd := 0 } := by
  rw [step_equivalent] at h; exact (ite_some_enc h).2

/-- Theorem 5: a publication is recorded only for an `uncertain` record after
the provider returned the same author and body; a definite rejection of an
`uncertain` record makes it `rejected` and adds one failure. -/
theorem publish_spec (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") (ok : Bool) :
    step M p (enc x) ⟨"publish", [boolV ok]⟩ ≠ none ↔ x.st = 1 ∧ ok = true ∧ 1 ≤ x.unc := by
  rw [step_publish p x hb]
  by_cases h : x.st = 1 ∧ ok = true ∧ 1 ≤ x.unc <;> simp [h]

theorem publish_result (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") (ok : Bool) {s' : State}
    (h : step M p (enc x) ⟨"publish", [boolV ok]⟩ = some s') :
    s' = enc { x with st := 2, cpos := true, unc := x.unc - 1 } := by
  rw [step_publish p x hb] at h; exact (ite_some_enc h).2

theorem reject_spec (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") :
    step M p (enc x) ⟨"reject", []⟩ ≠ none ↔ x.nf + 1 ≤ p "maxN" ∧ x.st = 1 ∧ 1 ≤ x.unc := by
  rw [step_reject p x hb]
  by_cases h : x.nf + 1 ≤ p "maxN" ∧ x.st = 1 ∧ 1 ≤ x.unc <;> simp [h]

theorem reject_result (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") {s' : State}
    (h : step M p (enc x) ⟨"reject", []⟩ = some s') :
    s' = enc { x with st := 4, unc := x.unc - 1, nf := x.nf + 1 } := by
  rw [step_reject p x hb] at h; exact (ite_some_enc h).2

/-- Theorem 5: `DecideResult` takes a decision only on a `published` or
`equivalent` record of the current work set that is fully resolved, only while
the remote is open, only when the scan of the earlier decisions blocks nothing,
and only with valid decision fields. The record then has one more
decision. -/
theorem decide_spec (p : Params) (x : Rq) (dg cl : Bool) (sc : Nat) (fk : Bool) :
    step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ ≠ none ↔
      sc < 5 ∧ x.nd + 1 ≤ p "maxN" ∧ (x.st = 2 ∨ x.st = 3) ∧ dg = true ∧ x.full = true ∧ cl = false ∧ sc = 0 ∧
        fk = true := by
  rw [step_decide]
  by_cases h : sc < 5 ∧ x.nd + 1 ≤ p "maxN" ∧ (x.st = 2 ∨ x.st = 3) ∧ dg = true ∧ x.full = true ∧ cl = false ∧
    sc = 0 ∧ fk = true <;> simp [h]

theorem decide_result (p : Params) (x : Rq) (dg cl : Bool) (sc : Nat) (fk : Bool) {s' : State}
    (h : step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ = some s') :
    s' = enc { x with nd := x.nd + 1 } := by
  rw [step_decide] at h; exact (ite_some_enc h).2

/-- Theorem 5: a record that is not `published` or `equivalent`, belongs to
another work set, or is not fully resolved takes no decision; neither does a
closed remote, a blocked scan, or invalid fields. -/
theorem decide_refused (p : Params) (x : Rq) (dg cl : Bool) (sc : Nat) (fk : Bool) :
    ((x.st ≠ 2 ∧ x.st ≠ 3) →
      step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ = none) ∧
    (dg = false → step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ = none) ∧
    (x.full = false → step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ = none) ∧
    (cl = true → step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ = none) ∧
    (sc ≠ 0 → step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ = none) ∧
    (fk = false → step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ = none) := by
  refine ⟨fun h => ?_, fun h => ?_, fun h => ?_, fun h => ?_, fun h => ?_, fun h => ?_⟩ <;>
    (rw [step_decide]; apply ite_none; intro c; obtain ⟨-, -, c3, c4, c5, c6, c7, c8⟩ := c; simp_all <;> omega)

/-- Theorem 5: when the remote is already closed, or the earlier refusal stands
and the new decision is a refusal too, the record is returned and nothing is
written. The two ways exclude the append. -/
theorem keep_spec (p : Params) (x : Rq) (dg cl : Bool) (sc : Nat) :
    step M p (enc x) ⟨"keep", [boolV dg, boolV cl, enumV sc]⟩ ≠ none ↔
      sc < 5 ∧ (x.st = 2 ∨ x.st = 3) ∧ dg = true ∧ x.full = true ∧ (cl = true ∨ sc = 4) := by
  rw [step_keep]
  by_cases h : sc < 5 ∧ (x.st = 2 ∨ x.st = 3) ∧ dg = true ∧ x.full = true ∧ (cl = true ∨ sc = 4) <;> simp [h]

theorem keep_keeps (p : Params) (x : Rq) (dg cl : Bool) (sc : Nat) {s' : State}
    (h : step M p (enc x) ⟨"keep", [boolV dg, boolV cl, enumV sc]⟩ = some s') : s' = enc x := by
  rw [step_keep] at h; exact (ite_some_enc h).2

theorem keep_decide_exclusive (p : Params) (x : Rq) (dg cl : Bool) (sc : Nat) (fk : Bool)
    (h1 : step M p (enc x) ⟨"keep", [boolV dg, boolV cl, enumV sc]⟩ ≠ none)
    (h2 : step M p (enc x) ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩ ≠ none) : False := by
  obtain ⟨-, -, -, -, h5⟩ := (keep_spec p x dg cl sc).mp h1
  obtain ⟨-, -, -, -, -, h6, h7, -⟩ := (decide_spec p x dg cl sc fk).mp h2
  rcases h5 with h5 | h5 <;> simp_all

/-- Theorem 5: a close request that the provider definitely rejected adds one
failure to a `published` or `equivalent` record. -/
theorem failClose_spec (p : Params) (x : Rq) :
    step M p (enc x) ⟨"failClose", []⟩ ≠ none ↔ x.nf + 1 ≤ p "maxN" ∧ (x.st = 2 ∨ x.st = 3) := by
  rw [step_failClose]
  by_cases h : x.nf + 1 ≤ p "maxN" ∧ (x.st = 2 ∨ x.st = 3) <;> simp [h]

theorem failClose_result (p : Params) (x : Rq) {s' : State} (h : step M p (enc x) ⟨"failClose", []⟩ = some s') :
    s' = enc { x with nf := x.nf + 1 } := by
  rw [step_failClose] at h; exact (ite_some_enc h).2

/-! ## Theorem 6: reconciliation and the issue identity -/

/-- Theorem 6: `InspectResult` makes an `uncertain` record `published` only
when the complete remote observation has its comment, and records that comment;
nothing else about the record changes. -/
theorem reconcile_spec (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") (fo : Bool) :
    step M p (enc x) ⟨"reconcile", [boolV fo]⟩ ≠ none ↔ x.st = 1 ∧ fo = true ∧ 1 ≤ x.unc := by
  rw [step_reconcile p x hb]
  by_cases h : x.st = 1 ∧ fo = true ∧ 1 ≤ x.unc <;> simp [h]

theorem reconcile_result (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") (fo : Bool) {s' : State}
    (h : step M p (enc x) ⟨"reconcile", [boolV fo]⟩ = some s') :
    s' = enc { x with st := 2, cpos := true, unc := x.unc - 1 } := by
  rw [step_reconcile p x hb] at h; exact (ite_some_enc h).2

theorem reconcile_refused (p : Params) (x : Rq) (hb : x.unc ≤ p "maxN") (fo : Bool) (hs : x.st ≠ 1) :
    step M p (enc x) ⟨"reconcile", [boolV fo]⟩ = none := by
  rw [step_reconcile p x hb]; apply ite_none; intro c; exact hs c.1

/-- Theorem 6: `readResult` writes the issue identity of the remote into a store
that has none, and refuses a remote with another identity. -/
theorem read_spec (p : Params) (x : Rq) (ro sm : Bool) :
    step M p (enc x) ⟨"read", [boolV ro, boolV sm]⟩ ≠ none ↔ ro = true ∧ (x.iid = false ∨ sm = true) := by
  rw [step_read]
  by_cases h : ro = true ∧ (x.iid = false ∨ sm = true) <;> simp [h]

theorem read_result (p : Params) (x : Rq) (ro sm : Bool) {s' : State}
    (h : step M p (enc x) ⟨"read", [boolV ro, boolV sm]⟩ = some s') : s' = enc { x with iid := true } := by
  rw [step_read] at h; exact (ite_some_enc h).2

theorem read_refused (p : Params) (x : Rq) (hi : x.iid = true) (ro : Bool) :
    step M p (enc x) ⟨"read", [boolV ro, boolV false]⟩ = none := by
  rw [step_read]; apply ite_none; intro c; simp_all

end KanderFormal.Proofs.IssueResult
