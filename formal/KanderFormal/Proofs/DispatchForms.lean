import KanderFormal.Proofs.Dispatch

/-
The forms of an accepted step of the dispatch round machine: which of the
sixteen events an accepted step ran, and what it did to the components of the
state. The proof obligations are in `Proofs/DispatchSafety.lean` and
`Proofs/DispatchGrant.lean`.
-/
namespace KanderFormal.Proofs.Dispatch

open KanderFormal KanderFormal.Machines.Dispatch KanderFormal.Proofs.Scalar

/-! ## The events an accepted step ran -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = prepareEvent ∨ ev = retryEvent ∨ ev = attemptEvent ∨ ev = reauthorizeEvent ∨ ev = acceptEvent ∨
      ev = completeEvent ∨ ev = endEvent ∨ ev = cancelEvent ∨ ev = retainEvent ∨ ev = execEvent ∨
      ev = authorizeWrapUpEvent ∨ ev = fullEvent ∨ ev = wrapUpWriteEvent ∨ ev = settleEvent ∨ ev = terminateSettleEvent ∨
      ev = takeoverSettleEvent := by
  simpa [dispatchMachine] using h

theorem nat_arg {p : Params} {b : String} {v : Val} (h : Ty.conforms M p (.scalar (.nat b)) v = true) :
    ∃ n, v = natV n := by
  obtain ⟨n, rfl, -⟩ := conforms_nat h; exact ⟨n, rfl⟩

/-- Every accepted event is one of the sixteen, with arguments of its declared types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
      (∃ kd cl fr rm, e = ⟨"prepare", [enumV kd, enumV cl, boolV fr, boolV rm]⟩) ∨
      (∃ sm, e = ⟨"retry", [boolV sm]⟩) ∨
      (∃ ar ae cs cy ev bf, e = ⟨"attempt", [natV ar, natV ae, boolV cs, boolV cy, boolV ev, boolV bf]⟩) ∨
      (∃ ar ae cs rm cy fr bf, e = ⟨"reauthorize", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV fr, boolV bf]⟩) ∨
      (∃ ar ae rc cy bf, e = ⟨"accept", [natV ar, natV ae, boolV rc, boolV cy, boolV bf]⟩) ∨
      (∃ ar ae rc cy td cm ev dp sm cl, e = ⟨"complete", [natV ar, natV ae, boolV rc, boolV cy, boolV td, boolV cm, boolV ev, boolV dp, boolV sm, enumV cl]⟩) ∨
      (∃ ar ae cs rm cy dc tf, e = ⟨"end", [natV ar, natV ae, boolV cs, boolV rm, boolV cy, boolV dc, boolV tf]⟩) ∨
      (∃ rs ao cy, e = ⟨"cancel", [boolV rs, boolV ao, boolV cy]⟩) ∨
      (∃ ar ae dc rv rc cy, e = ⟨"retain", [natV ar, natV ae, boolV dc, boolV rv, boolV rc, boolV cy]⟩) ∨
      (∃ ar ae rc bf au, e = ⟨"exec", [natV ar, natV ae, boolV rc, boolV bf, boolV au]⟩) ∨
      (∃ ar ae cs rm cl nm cy ot dc fr ev, e = ⟨"authorizeWrapUp", [natV ar, natV ae, boolV cs, boolV rm, enumV cl, boolV nm, boolV cy, enumV ot, boolV dc, boolV fr, boolV ev]⟩) ∨
      (e = ⟨"full", []⟩) ∨
      (∃ ct sp ap pa sm, e = ⟨"wrapUpWrite", [boolV ct, boolV sp, boolV ap, boolV pa, boolV sm]⟩) ∨
      (∃ ar ae cs cl cy, e = ⟨"settle", [natV ar, natV ae, boolV cs, enumV cl, boolV cy]⟩) ∨
      (∃ rs ao cy, e = ⟨"terminateSettle", [boolV rs, boolV ao, boolV cy]⟩) ∨
      (∃ ar ae dc rv rc cy, e = ⟨"takeoverSettle", [natV ar, natV ae, boolV dc, boolV rv, boolV rc, boolV cy]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · -- prepare
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    obtain ⟨kd, rfl, -⟩ := conforms_enum size_kind c0
    obtain ⟨cl, rfl, -⟩ := conforms_enum size_col c1
    obtain ⟨fr, rfl⟩ := conforms_bool c2
    obtain ⟨rm, rfl⟩ := conforms_bool c3
    exact .inl ⟨kd, cl, fr, rm, rfl⟩
  · -- retry
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h0
    obtain ⟨sm, rfl⟩ := conforms_bool c0
    exact .inr (.inl ⟨sm, rfl⟩)
  · -- attempt
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨cs, rfl⟩ := conforms_bool c2
    obtain ⟨cy, rfl⟩ := conforms_bool c3
    obtain ⟨ev, rfl⟩ := conforms_bool c4
    obtain ⟨bf, rfl⟩ := conforms_bool c5
    exact .inr (.inr (.inl ⟨ar, ae, cs, cy, ev, bf, rfl⟩))
  · -- reauthorize
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨cs, rfl⟩ := conforms_bool c2
    obtain ⟨rm, rfl⟩ := conforms_bool c3
    obtain ⟨cy, rfl⟩ := conforms_bool c4
    obtain ⟨fr, rfl⟩ := conforms_bool c5
    obtain ⟨bf, rfl⟩ := conforms_bool c6
    exact .inr (.inr (.inr (.inl ⟨ar, ae, cs, rm, cy, fr, bf, rfl⟩)))
  · -- accept
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨rc, rfl⟩ := conforms_bool c2
    obtain ⟨cy, rfl⟩ := conforms_bool c3
    obtain ⟨bf, rfl⟩ := conforms_bool c4
    exact .inr (.inr (.inr (.inr (.inl ⟨ar, ae, rc, cy, bf, rfl⟩))))
  · -- complete
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨v7, vs7, _, rfl, c7, h7, -⟩ := bindArgs_cons h6
    obtain ⟨v8, vs8, _, rfl, c8, h8, -⟩ := bindArgs_cons h7
    obtain ⟨v9, vs9, _, rfl, c9, h9, -⟩ := bindArgs_cons h8
    obtain ⟨rfl, -⟩ := bindArgs_nil h9
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨rc, rfl⟩ := conforms_bool c2
    obtain ⟨cy, rfl⟩ := conforms_bool c3
    obtain ⟨td, rfl⟩ := conforms_bool c4
    obtain ⟨cm, rfl⟩ := conforms_bool c5
    obtain ⟨ev, rfl⟩ := conforms_bool c6
    obtain ⟨dp, rfl⟩ := conforms_bool c7
    obtain ⟨sm, rfl⟩ := conforms_bool c8
    obtain ⟨cl, rfl, -⟩ := conforms_enum size_col c9
    exact .inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, rc, cy, td, cm, ev, dp, sm, cl, rfl⟩)))))
  · -- end
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨cs, rfl⟩ := conforms_bool c2
    obtain ⟨rm, rfl⟩ := conforms_bool c3
    obtain ⟨cy, rfl⟩ := conforms_bool c4
    obtain ⟨dc, rfl⟩ := conforms_bool c5
    obtain ⟨tf, rfl⟩ := conforms_bool c6
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, cs, rm, cy, dc, tf, rfl⟩))))))
  · -- cancel
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    obtain ⟨rs, rfl⟩ := conforms_bool c0
    obtain ⟨ao, rfl⟩ := conforms_bool c1
    obtain ⟨cy, rfl⟩ := conforms_bool c2
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨rs, ao, cy, rfl⟩)))))))
  · -- retain
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨dc, rfl⟩ := conforms_bool c2
    obtain ⟨rv, rfl⟩ := conforms_bool c3
    obtain ⟨rc, rfl⟩ := conforms_bool c4
    obtain ⟨cy, rfl⟩ := conforms_bool c5
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, dc, rv, rc, cy, rfl⟩))))))))
  · -- exec
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨rc, rfl⟩ := conforms_bool c2
    obtain ⟨bf, rfl⟩ := conforms_bool c3
    obtain ⟨au, rfl⟩ := conforms_bool c4
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, rc, bf, au, rfl⟩)))))))))
  · -- authorizeWrapUp
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨v6, vs6, _, rfl, c6, h6, -⟩ := bindArgs_cons h5
    obtain ⟨v7, vs7, _, rfl, c7, h7, -⟩ := bindArgs_cons h6
    obtain ⟨v8, vs8, _, rfl, c8, h8, -⟩ := bindArgs_cons h7
    obtain ⟨v9, vs9, _, rfl, c9, h9, -⟩ := bindArgs_cons h8
    obtain ⟨v10, vs10, _, rfl, c10, h10, -⟩ := bindArgs_cons h9
    obtain ⟨rfl, -⟩ := bindArgs_nil h10
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨cs, rfl⟩ := conforms_bool c2
    obtain ⟨rm, rfl⟩ := conforms_bool c3
    obtain ⟨cl, rfl, -⟩ := conforms_enum size_col c4
    obtain ⟨nm, rfl⟩ := conforms_bool c5
    obtain ⟨cy, rfl⟩ := conforms_bool c6
    obtain ⟨ot, rfl, -⟩ := conforms_enum size_exit c7
    obtain ⟨dc, rfl⟩ := conforms_bool c8
    obtain ⟨fr, rfl⟩ := conforms_bool c9
    obtain ⟨ev, rfl⟩ := conforms_bool c10
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, cs, rm, cl, nm, cy, ot, dc, fr, ev, rfl⟩))))))))))
  · -- full
    obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl rfl)))))))))))
  · -- wrapUpWrite
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    obtain ⟨ct, rfl⟩ := conforms_bool c0
    obtain ⟨sp, rfl⟩ := conforms_bool c1
    obtain ⟨ap, rfl⟩ := conforms_bool c2
    obtain ⟨pa, rfl⟩ := conforms_bool c3
    obtain ⟨sm, rfl⟩ := conforms_bool c4
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ct, sp, ap, pa, sm, rfl⟩))))))))))))
  · -- settle
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨cs, rfl⟩ := conforms_bool c2
    obtain ⟨cl, rfl, -⟩ := conforms_enum size_col c3
    obtain ⟨cy, rfl⟩ := conforms_bool c4
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ar, ae, cs, cl, cy, rfl⟩)))))))))))))
  · -- terminateSettle
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    obtain ⟨rs, rfl⟩ := conforms_bool c0
    obtain ⟨ao, rfl⟩ := conforms_bool c1
    obtain ⟨cy, rfl⟩ := conforms_bool c2
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨rs, ao, cy, rfl⟩))))))))))))))
  · -- takeoverSettle
    obtain ⟨v0, vs0, _, rfl, c0, h0, -⟩ := bindArgs_cons hb
    obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h0
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨v4, vs4, _, rfl, c4, h4, -⟩ := bindArgs_cons h3
    obtain ⟨v5, vs5, _, rfl, c5, h5, -⟩ := bindArgs_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    obtain ⟨ar, rfl⟩ := nat_arg c0
    obtain ⟨ae, rfl⟩ := nat_arg c1
    obtain ⟨dc, rfl⟩ := conforms_bool c2
    obtain ⟨rv, rfl⟩ := conforms_bool c3
    obtain ⟨rc, rfl⟩ := conforms_bool c4
    obtain ⟨cy, rfl⟩ := conforms_bool c5
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (⟨ar, ae, dc, rv, rc, cy, rfl⟩)))))))))))))))

/-! ## The forms of an accepted step -/

/-- What an accepted event does to the components, event by event. -/
def Forms (x y : Dp) : Prop :=
  y = x ∨
  (y.round = x.round + 1 ∧ y.bound = true ∧ y.ph = 1 ∧ y.rel = false ∧ (x.bound = false ∨ Term x) ∧
      y.epoch = (if x.bound = true then x.epoch + 1 else 1) ∧ y.racc = false ∧ y.rcmp = false ∧
      y.grant = false ∧ y.settled = false) ∨
  (y = { x with ph := 2 } ∧ x.bound = true ∧ x.round ≠ 0 ∧ Pend x) ∨
  (y = { x with epoch := x.epoch + 1, ph := 1, racc := false, rcmp := false } ∧ x.bound = true ∧ x.round ≠ 0 ∧
      Open x) ∨
  (y = { x with ph := if Pend x then 3 else x.ph, racc := if Pend x then true else x.racc } ∧ x.bound = true ∧
      x.round ≠ 0 ∧ (Pend x ∨ x.ph = 3 ∨ x.ph = 4)) ∨
  (y = { x with ph := 4, rcmp := true } ∧ x.bound = true ∧ x.round ≠ 0 ∧ (x.ph = 3 ∨ x.ph = 4)) ∨
  (y.round = x.round ∧ y.epoch = x.epoch ∧ y.bound = false ∧ y.rel = true ∧ (y.ph = 5 ∨ y.ph = 6) ∧
      y.kd = x.kd ∧ x.bound = true ∧ x.round ≠ 0 ∧ x.ph ≠ 4 ∧ x.rel = false ∧ (Leg x → y.ph = x.ph) ∧
      y.racc = x.racc ∧ y.rcmp = x.rcmp ∧ y.grant = x.grant ∧ y.settled = x.settled) ∨
  (y = { x with ph := if Open x then 6 else x.ph } ∧ x.bound = true ∧ (Open x ∨ Leg x)) ∨
  (y = { x with epoch := x.epoch + 1, ph := 1, racc := false, rcmp := false, grant := true } ∧ x.bound = true ∧ x.round ≠ 0 ∧ Open x ∧
      x.kd = 2 ∧ x.grant = false) ∨
  (y = { x with bound := false, settled := true } ∧ x.bound = true ∧ x.ph = 4)

theorem ite_some_inv {c : Prop} [Decidable c] {a b : State} (h : (if c then some a else none) = some b) :
    c ∧ a = b := by
  by_cases hc : c <;> simp [hc] at h <;> exact ⟨hc, h⟩

theorem forms {p : Params} {x y : Dp} {s' : State} {e : Event} (h : step M p (enc x) e = some s')
    (hy : s' = enc y) : Forms x y := by
  rcases accepted_cases h with ⟨kd, cl, fr, rm, rfl⟩ | ⟨sm, rfl⟩ | ⟨ar, ae, cs, cy, ev, bf, rfl⟩ |
    ⟨ar, ae, cs, rm, cy, fr, bf, rfl⟩ | ⟨ar, ae, rc, cy, bf, rfl⟩ |
    ⟨ar, ae, rc, cy, td, cm, ev, dp, sm, cl, rfl⟩ | ⟨ar, ae, cs, rm, cy, dc, tf, rfl⟩ | ⟨rs, ao, cy, rfl⟩ |
    ⟨ar, ae, dc, rv, rc, cy, rfl⟩ | ⟨ar, ae, rc, bf, au, rfl⟩ |
    ⟨ar, ae, cs, rm, cl, nm, cy, ot, dc, fr, ev, rfl⟩ | rfl | ⟨ct, sp, ap, pa, sm, rfl⟩ |
    ⟨ar, ae, cs, cl, cy, rfl⟩ | ⟨rs, ao, cy, rfl⟩ | ⟨ar, ae, dc, rv, rc, cy, rfl⟩
  · rw [step_prepare] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; left; cases hb : x.bound <;> simp_all
  · rw [step_retry] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this; left; rfl
  · rw [step_attempt] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; left; simp_all [Fenced] <;> omega
  · rw [step_reauthorize] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; left; simp_all [Fenced] <;> omega
  · rw [step_accept] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; left; simp_all [Fenced]
    obtain ⟨-, -, -, -, -, h3⟩ := hc
    rcases h3 with h3 | h3 | ⟨h3, -⟩ <;> simp [h3]
  · rw [step_complete] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; left; simp_all [Fenced] <;> omega
  · rw [step_end] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; right; left; simp_all [Fenced]
    intro hleg
    obtain ⟨-, -, -, -, -, -, -, -, -, hl⟩ := hc
    rcases hl with hl | ⟨-, hl⟩
    · exact absurd hleg hl
    · unfold Leg at hleg; cases tf <;> simp at hl ⊢ <;> omega
  · rw [step_cancel] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; right; right; simp_all
  · rw [step_retain] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this; left; rfl
  · rw [step_exec] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this; left; rfl
  · rw [step_authorize] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; right; right; right; left; simp_all [Fenced] <;> omega
  · rw [step_full] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this; left; rfl
  · rw [step_wrapUpWrite] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this; left; rfl
  · rw [step_settle] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; right; right; right; right; simp_all [Fenced] <;> omega
  · rw [step_terminateSettle] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; right; right; right; right; simp_all
  · rw [step_takeoverSettle] at h; obtain ⟨hc, rfl⟩ := ite_some_inv h; have := enc_inj hy; subst this
    right; right; right; right; right; right; right; right; right; simp_all [Fenced] <;> omega

end KanderFormal.Proofs.Dispatch
