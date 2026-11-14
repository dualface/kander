import KanderFormal.Obligations
import KanderFormal.Proofs.Scalar
import KanderFormal.Machines.ServerClaim

/-
Proof obligations of the server claim machine. They hold for every value of
`maxGen`.
-/
namespace KanderFormal.Proofs.ServerClaim

open KanderFormal KanderFormal.Machines.ServerClaim KanderFormal.Proofs.Scalar

abbrev M := serverClaimMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- The explicit components of a state. `st`: 0 none, 1 pending, 2 active,
3 lost, 4 released. -/
structure Cl where
  held : Bool
  st : Nat
  gen : Nat
  live : Bool

def enc (x : Cl) : State :=
  [("held", boolV x.held), ("status", enumV x.st), ("gen", natV x.gen), ("live", boolV x.live)]

theorem enc_inj {x y : Cl} (h : enc x = enc y) : x = y := by
  obtain ⟨_, _, _, _⟩ := x
  obtain ⟨_, _, _, _⟩ := y
  simp [enc, enumV, natV, boolV] at h
  obtain ⟨rfl, rfl, rfl, rfl⟩ := h
  rfl

theorem shape {p : Params} {s : State} (h : M.StateConforms p s) :
    ∃ x, s = enc x ∧ x.st < 5 ∧ x.gen ≤ p "maxGen" := by
  obtain ⟨hn, hc⟩ := h
  match s, hn with
  | [(_, v1), (_, v2), (_, v3), (_, v4)], hn =>
    simp [serverClaimMachine] at hn
    obtain ⟨rfl, rfl, rfl, rfl⟩ := hn
    obtain ⟨t1, ht1, c1⟩ := hc "held" v1 (by simp [lookup])
    obtain ⟨t2, ht2, c2⟩ := hc "status" v2 (by simp [lookup])
    obtain ⟨t3, ht3, c3⟩ := hc "gen" v3 (by simp [lookup])
    obtain ⟨t4, ht4, c4⟩ := hc "live" v4 (by simp [lookup])
    cases (rfl : M.fieldTy "held" = some (.scalar .bool)).symm.trans ht1
    cases (rfl : M.fieldTy "status" = some (.scalar (.enum "status"))).symm.trans ht2
    cases (rfl : M.fieldTy "gen" = some (.scalar (.nat "maxGen"))).symm.trans ht3
    cases (rfl : M.fieldTy "live" = some (.scalar .bool)).symm.trans ht4
    obtain ⟨hd, rfl⟩ := conforms_bool c1
    obtain ⟨st, rfl, hst⟩ := conforms_enum (rfl : M.enumSize "status" = some 5) c2
    obtain ⟨g, rfl, hg⟩ := conforms_nat c3
    obtain ⟨lv, rfl⟩ := conforms_bool c4
    exact ⟨⟨hd, st, g, lv⟩, rfl, hst, hg⟩

/-! ## Events on an encoded state -/

theorem find_claim : M.findEvent "claim" = some claimEvent := rfl
theorem find_renew : M.findEvent "renew" = some renewEvent := rfl
theorem find_write : M.findEvent "write" = some writeEvent := rfl
theorem find_leave : M.findEvent "leave" = some leaveEvent := rfl
theorem find_release : M.findEvent "release" = some releaseEvent := rfl
theorem find_rollback : M.findEvent "rollback" = some rollbackEvent := rfl
theorem find_takeover : M.findEvent "takeover" = some takeoverEvent := rfl
theorem find_expire : M.findEvent "expire" = some expireEvent := rfl
theorem find_pending : M.findEvent "pending" = some pendingEvent := rfl
theorem find_reconcile : M.findEvent "reconcile" = some reconcileEvent := rfl
theorem ctors_status : M.ctorsOf "status" = some ["none", "pending", "active", "lost", "released"] := rfl
theorem size_status : M.enumSize "status" = some 5 := rfl
theorem ty_held : M.fieldTy "held" = some (.scalar .bool) := rfl
theorem ty_status : M.fieldTy "status" = some (.scalar (.enum "status")) := rfl
theorem ty_gen : M.fieldTy "gen" = some (.scalar (.nat "maxGen")) := rfl
theorem ty_live : M.fieldTy "live" = some (.scalar .bool) := rfl

macro "cl_simp" "[" ts:Lean.Parser.Tactic.simpLemma,* "]" : tactic =>
  `(tactic| simp [step, find_claim, find_renew, find_write, find_leave, find_release, find_rollback, find_takeover,
      find_expire, find_pending, find_reconcile, ctors_status, size_status, ty_held, ty_status, ty_gen, ty_live,
      lookup, claimEvent, renewEvent, writeEvent, leaveEvent, releaseEvent, rollbackEvent, takeoverEvent,
      expireEvent, pendingEvent, reconcileEvent, holder, leaseInvalid, genArg, boolArg, holderArgs, statusIs,
      gen, inc, eq, bindArgs, Ty.conforms, STy.conforms, eval, evalCmp, natAdd, boolAnd, boolOr, boolNot, indexOf,
      enumV, boolV, natV, f, a, tt, ff, andE, orE, notE, evalAssigns, applyUpdates, enc, or_assoc, and_assoc,
      Bool.and_assoc, Bool.or_assoc, map_ite, ite_and_self, ite_ite_none, $ts,*])

macro "ite_close" : tactic =>
  `(tactic| ((try split) <;> (try split) <;> first | rfl | (exfalso; simp_all; done) | (simp_all; done) |
    (exfalso; simp_all; omega)))

/-- `requireHolder` accepts the request. -/
def Holder (x : Cl) (g tg : Nat) : Prop := x.st = 2 ∧ tg = x.gen ∧ g = x.gen ∧ x.live = true

instance (x : Cl) (g tg : Nat) : Decidable (Holder x g tg) := by unfold Holder; infer_instance

/-- `requireHolder` answers `CodeLeaseInvalid`. -/
def LeaseInvalid (x : Cl) (g tg : Nat) : Prop := x.st ≠ 2 ∨ tg = x.gen ∧ (g ≠ x.gen ∨ x.live = false)

instance (x : Cl) (g tg : Nat) : Decidable (LeaseInvalid x g tg) := by unfold LeaseInvalid; infer_instance

/-- A valid claim record: active with a lease that has not expired. -/
def Valid (x : Cl) : Prop := x.st = 2 ∧ x.live = true

instance (x : Cl) : Decidable (Valid x) := by unfold Valid; infer_instance

theorem step_claim (p : Params) (x : Cl) (fresh : Bool) :
    step M p (enc x) ⟨"claim", [boolV fresh]⟩ =
      if x.held = false ∧ (x.st = 0 ∨ fresh = true) ∧ x.gen + 1 ≤ p "maxGen" then
        some (enc ⟨true, 2, x.gen + 1, true⟩) else none := by
  cases hh : x.held <;> cl_simp [hh] <;> ite_close

/-- The holder events that change nothing but the lease, or the claim. -/
theorem step_renew (p : Params) (x : Cl) (g tg : Nat) :
    step M p (enc x) ⟨"renew", [natV g, natV tg]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x g tg then some (enc { x with live := true }) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> cl_simp [h1, h2, Holder] <;> ite_close

theorem step_write (p : Params) (x : Cl) (g tg : Nat) (terminate : Bool) :
    step M p (enc x) ⟨"write", [natV g, natV tg, boolV terminate]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ (Holder x g tg ∨ terminate = true ∧ LeaseInvalid x g tg) then
        some (enc x) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;>
    cl_simp [h1, h2, Holder, LeaseInvalid] <;> ite_close

theorem step_leave (p : Params) (x : Cl) (g tg : Nat) (terminate : Bool) :
    step M p (enc x) ⟨"leave", [natV g, natV tg, boolV terminate]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ x.held = true ∧
          (Holder x g tg ∨ terminate = true ∧ LeaseInvalid x g tg) then
        some (enc ⟨false, if x.st = 0 then 0 else 4, x.gen, x.live⟩) else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> by_cases h0 : x.st = 0 <;>
    cl_simp [h1, h2, h0, Holder, LeaseInvalid] <;> ite_close

theorem step_release (p : Params) (x : Cl) (g tg : Nat) :
    step M p (enc x) ⟨"release", [natV g, natV tg]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x g tg then some (enc ⟨x.held, 3, x.gen, false⟩)
      else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> cl_simp [h1, h2, Holder] <;> ite_close

theorem step_rollback (p : Params) (x : Cl) (g tg : Nat) :
    step M p (enc x) ⟨"rollback", [natV g, natV tg]⟩ =
      if g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Holder x g tg then some (enc ⟨false, 4, x.gen, x.live⟩)
      else none := by
  by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;> cl_simp [h1, h2, Holder] <;> ite_close

theorem step_takeover (p : Params) (x : Cl) (expected g tg : Nat)
    (fresh restart : Bool) :
    step M p (enc x) ⟨"takeover", [natV expected, boolV fresh, boolV restart, natV g, natV tg]⟩ =
      if expected ≤ p "maxGen" ∧ g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ x.held = true ∧ expected = x.gen ∧
          fresh = true ∧ (¬Valid x ∨ restart = true ∧ Holder x g tg) ∧ x.gen + 1 ≤ p "maxGen" then
        some (enc ⟨x.held, 2, x.gen + 1, true⟩) else none := by
  by_cases h0 : expected ≤ p "maxGen" <;> by_cases h1 : g ≤ p "maxGen" <;> by_cases h2 : tg ≤ p "maxGen" <;>
    cl_simp [h0, h1, h2, Holder, Valid] <;> ite_close

theorem step_expire (p : Params) (x : Cl) :
    step M p (enc x) ⟨"expire", []⟩ = if x.live = true then some (enc { x with live := false }) else none := by
  cases hl : x.live <;> cl_simp [hl]

theorem step_pending (p : Params) (x : Cl) (moved : Bool) :
    step M p (enc x) ⟨"pending", [boolV moved]⟩ =
      if x.held = false ∧ x.gen + 1 ≤ p "maxGen" then some (enc ⟨moved, 1, x.gen + 1, false⟩) else none := by
  cases hh : x.held <;> cl_simp [hh] <;> ite_close

/-- The status after a startup reconciliation. -/
def reconciled (x : Cl) (owner : Bool) : Nat :=
  if x.st = 1 ∧ x.held = true ∧ owner = true then 2
  else if x.st = 1 ∨ x.st = 2 ∧ x.held = false then 4 else x.st

theorem step_reconcile (p : Params) (x : Cl) (hs : x.st < 5) (owner : Bool) :
    step M p (enc x) ⟨"reconcile", [boolV owner]⟩ =
      some (enc ⟨x.held, reconciled x owner, x.gen,
        x.live || (decide (x.st = 1) && x.held && owner)⟩) := by
  cases hh : x.held <;> cases owner <;> by_cases h1 : x.st = 1 <;> by_cases h2 : x.st = 2 <;>
    cl_simp [hh, h1, h2, hs, reconciled]

/-! ## The events an accepted step ran -/

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = claimEvent ∨ ev = renewEvent ∨ ev = writeEvent ∨ ev = leaveEvent ∨ ev = releaseEvent ∨
      ev = rollbackEvent ∨ ev = takeoverEvent ∨ ev = expireEvent ∨ ev = pendingEvent ∨ ev = reconcileEvent := by
  simpa [serverClaimMachine] using h

theorem nat_arg {p : Params} {b : String} {v : Val} (h : Ty.conforms M p (.scalar (.nat b)) v = true) :
    ∃ n, v = natV n := by
  obtain ⟨n, rfl, -⟩ := conforms_nat h; exact ⟨n, rfl⟩

/-- The two generation arguments of a holder event. -/
theorem holder_args {p : Params} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p holderArgs vs = some args) : ∃ g tg, vs = [natV g, natV tg] := by
  obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons h
  obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
  obtain ⟨rfl, -⟩ := bindArgs_nil h2
  obtain ⟨g, rfl⟩ := nat_arg c1
  obtain ⟨tg, rfl⟩ := nat_arg c2
  exact ⟨g, tg, rfl⟩

/-- Every accepted event is one of the ten, with arguments of its declared
types. -/
theorem accepted_cases {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    (∃ fr, e = ⟨"claim", [boolV fr]⟩) ∨ (∃ g tg, e = ⟨"renew", [natV g, natV tg]⟩) ∨
      (∃ g tg t, e = ⟨"write", [natV g, natV tg, boolV t]⟩) ∨
      (∃ g tg t, e = ⟨"leave", [natV g, natV tg, boolV t]⟩) ∨
      (∃ g tg, e = ⟨"release", [natV g, natV tg]⟩) ∨ (∃ g tg, e = ⟨"rollback", [natV g, natV tg]⟩) ∨
      (∃ ex fr rs g tg, e = ⟨"takeover", [natV ex, boolV fr, boolV rs, natV g, natV tg]⟩) ∨
      e = ⟨"expire", []⟩ ∨ (∃ m, e = ⟨"pending", [boolV m]⟩) ∨ (∃ o, e = ⟨"reconcile", [boolV o]⟩) := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl
  · obtain ⟨fr, rfl⟩ := args_b hb; exact .inl ⟨fr, rfl⟩
  · obtain ⟨g, tg, rfl⟩ := holder_args hb; exact .inr (.inl ⟨g, tg, rfl⟩)
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    obtain ⟨g, rfl⟩ := nat_arg c1
    obtain ⟨tg, rfl⟩ := nat_arg c2
    obtain ⟨t, rfl⟩ := conforms_bool c3
    exact .inr (.inr (.inl ⟨g, tg, t, rfl⟩))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    obtain ⟨g, rfl⟩ := nat_arg c1
    obtain ⟨tg, rfl⟩ := nat_arg c2
    obtain ⟨t, rfl⟩ := conforms_bool c3
    exact .inr (.inr (.inr (.inl ⟨g, tg, t, rfl⟩)))
  · obtain ⟨g, tg, rfl⟩ := holder_args hb; exact .inr (.inr (.inr (.inr (.inl ⟨g, tg, rfl⟩))))
  · obtain ⟨g, tg, rfl⟩ := holder_args hb; exact .inr (.inr (.inr (.inr (.inr (.inl ⟨g, tg, rfl⟩)))))
  · obtain ⟨v1, vs1, _, rfl, c1, h1, -⟩ := bindArgs_cons hb
    obtain ⟨v2, vs2, _, rfl, c2, h2, -⟩ := bindArgs_cons h1
    obtain ⟨v3, vs3, _, rfl, c3, h3, -⟩ := bindArgs_cons h2
    obtain ⟨g, tg, rfl⟩ := holder_args h3
    obtain ⟨ex, rfl⟩ := nat_arg c1
    obtain ⟨fr, rfl⟩ := conforms_bool c2
    obtain ⟨rs, rfl⟩ := conforms_bool c3
    exact .inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨ex, fr, rs, g, tg, rfl⟩))))))
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb; exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl rfl)))))))
  · obtain ⟨m, rfl⟩ := args_b hb; exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inl ⟨m, rfl⟩))))))))
  · obtain ⟨o, rfl⟩ := args_b hb; exact .inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr (.inr ⟨o, rfl⟩))))))))

/-! ## Theorem 3: at most one valid claim -/

/-- Theorem 3: in every state, `requireHolder` accepts at most one generation
and one token: two accepted holder writes name the same generation, the
current one. -/
theorem holder_unique (p : Params) (x : Cl) {g1 tg1 g2 tg2 : Nat}
    (h1 : step M p (enc x) ⟨"write", [natV g1, natV tg1, boolV false]⟩ ≠ none)
    (h2 : step M p (enc x) ⟨"write", [natV g2, natV tg2, boolV false]⟩ ≠ none) :
    g1 = x.gen ∧ g2 = x.gen ∧ tg1 = x.gen ∧ tg2 = x.gen := by
  rw [step_write] at h1 h2
  split at h1
  · split at h2
    · rename_i a b
      obtain ⟨-, -, ⟨-, ha2, ha3, -⟩ | ⟨ha, -⟩⟩ := a
      · obtain ⟨-, -, ⟨-, hb2, hb3, -⟩ | ⟨hb, -⟩⟩ := b
        · exact ⟨ha3, hb3, ha2, hb2⟩
        · cases hb
      · cases ha
    · exact absurd rfl h2
  · exact absurd rfl h1

/-! ## Theorem 4: generations -/

/-- Theorem 4: no accepted event lowers the generation; a takeover raises it by
one. -/
theorem gen_monotone {p : Params} {x y : Cl} {e : Event} {s' : State} (hs : x.st < 5)
    (h : step M p (enc x) e = some s') (hy : s' = enc y) :
    x.gen ≤ y.gen ∧ (e.name = "takeover" → y.gen = x.gen + 1) := by
  rcases accepted_cases h with ⟨fr, rfl⟩ | ⟨g, tg, rfl⟩ | ⟨g, tg, w, rfl⟩ | ⟨g, tg, t, rfl⟩ | ⟨g, tg, rfl⟩ |
    ⟨g, tg, rfl⟩ | ⟨ex, fr, rs, g, tg, rfl⟩ | rfl | ⟨m, rfl⟩ | ⟨o, rfl⟩
  · rw [step_claim p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_renew] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_write] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_leave p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_release p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_rollback p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_takeover p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_expire] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_pending] at h; split at h <;> cases h; have := enc_inj hy; subst this; simp
  · rw [step_reconcile p x hs] at h; cases h; have := enc_inj hy; subst this; simp

/-! ## Theorem 5: requests of an older generation -/

/-- Theorem 5: a holder event (a renewal, any other holder write, a lease
release, a launch rollback, and a move out of `working` or `review` that is not
a termination) whose generation header or token is not the current
generation's is refused. A refused event leaves the state unchanged
(`run_refused_unchanged`). -/
theorem stale_holder_refused (p : Params) (x : Cl) (g tg : Nat)
    (h : g ≠ x.gen ∨ tg ≠ x.gen) :
    step M p (enc x) ⟨"renew", [natV g, natV tg]⟩ = none ∧
      step M p (enc x) ⟨"write", [natV g, natV tg, boolV false]⟩ = none ∧
      step M p (enc x) ⟨"release", [natV g, natV tg]⟩ = none ∧
      step M p (enc x) ⟨"rollback", [natV g, natV tg]⟩ = none ∧
      step M p (enc x) ⟨"leave", [natV g, natV tg, boolV false]⟩ = none := by
  have hh : ¬Holder x g tg := fun ⟨_, h2, h3, _⟩ => by omega
  refine ⟨?_, ?_, ?_, ?_, ?_⟩
  · rw [step_renew]; simp [hh]
  · rw [step_write]; simp [hh]
  · rw [step_release p x]; simp [hh]
  · rw [step_rollback p x]; simp [hh]
  · rw [step_leave p x]; simp [hh]

/-- Theorem 5 for terminations: a move into `archived` or `trash`, and the
write check of its transaction, whose token belongs to another generation is
refused while a valid claim exists. -/
theorem stale_terminate_refused (p : Params) (x : Cl) (g tg : Nat)
    (hv : Valid x) (h : tg ≠ x.gen) (t : Bool) :
    step M p (enc x) ⟨"leave", [natV g, natV tg, boolV t]⟩ = none ∧
      step M p (enc x) ⟨"write", [natV g, natV tg, boolV t]⟩ = none := by
  have hh : ¬Holder x g tg := fun ⟨_, h2, _, _⟩ => h h2
  have hl : ¬LeaseInvalid x g tg := by
    obtain ⟨h1, h2⟩ := hv
    intro hl; rcases hl with hl | ⟨hl, -⟩ <;> contradiction
  exact ⟨by rw [step_leave p x]; simp [hh, hl], by rw [step_write]; simp [hh, hl]⟩

/-- Theorem 5 for takeovers: a takeover that observed another generation is
refused. -/
theorem stale_takeover_refused (p : Params) (x : Cl) (expected g tg : Nat)
    (fresh restart : Bool) (h : expected ≠ x.gen) :
    step M p (enc x) ⟨"takeover", [natV expected, boolV fresh, boolV restart, natV g, natV tg]⟩ = none := by
  rw [step_takeover p x]; simp [h]

/-! ## Theorem 7: the invariant -/

/-- The safety properties of a claim record: a valid record belongs to a card
in `working` or `review`, a record exists exactly when the generation is
positive, and a pending record has no lease. -/
structure Props (x : Cl) : Prop where
  validHeld : Valid x → x.held = true
  noneZero : x.st = 0 ↔ x.gen = 0
  pendingDead : x.st = 1 → x.live = false

def Inv (p : Params) (s : State) : Prop :=
  M.StateConforms p s ∧ ∀ x, s = enc x → Props x

theorem init (p : Params) : Inv p (initial M p) := by
  refine ⟨initial_conforms wellTyped p, ?_⟩
  intro x hx
  have : x = ⟨false, 0, 0, false⟩ := enc_inj (hx.symm.trans rfl)
  subst this
  exact ⟨fun h => by simp [Valid] at h, by simp, by simp⟩

macro "props_close" : tactic =>
  `(tactic| (constructor <;> simp_all [Valid, Holder, LeaseInvalid, reconciled] <;> omega))

theorem step_inv (p : Params) (s s' : State) (e : Event) (hi : Inv p s)
    (h : KanderFormal.step M p s e = some s') : Inv p s' := by
  obtain ⟨hc, hp⟩ := hi
  refine ⟨step_preserves_conforms hc h, ?_⟩
  obtain ⟨x, rfl, hs, hg⟩ := shape hc
  obtain ⟨P1, P2, P3⟩ := hp x rfl
  intro y hy
  rcases accepted_cases h with ⟨fr, rfl⟩ | ⟨g, tg, rfl⟩ | ⟨g, tg, w, rfl⟩ | ⟨g, tg, t, rfl⟩ | ⟨g, tg, rfl⟩ |
    ⟨g, tg, rfl⟩ | ⟨ex, fr, rs, g, tg, rfl⟩ | rfl | ⟨m, rfl⟩ | ⟨o, rfl⟩
  · rw [step_claim p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_renew] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_write] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_leave p x] at h; split at h <;> cases h; have := enc_inj hy; subst this
    by_cases h0 : x.st = 0 <;> props_close
  · rw [step_release p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_rollback p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_takeover p x] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_expire] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_pending] at h; split at h <;> cases h; have := enc_inj hy; subst this; props_close
  · rw [step_reconcile p x hs] at h; cases h; have := enc_inj hy; subst this
    cases hh : x.held <;> cases o <;> by_cases h1 : x.st = 1 <;> by_cases h2 : x.st = 2 <;> props_close

/-- The server claim machine's proof obligation certificate. -/
def cert : SafetyCert M where
  Inv := Inv
  init := init
  step := step_inv

/-- Theorem 7: every reachable state satisfies the invariant, for every value
of `maxGen`. -/
theorem reachable_inv {p : Params} {s : State} (h : Reachable M p s) : Inv p s := cert.reachable h

/-- Theorem 3 on reachable states: a valid claim record belongs to a claimed
card, and only its generation passes `requireHolder`. -/
theorem reachable_valid_held {p : Params} {x : Cl} (hr : Reachable M p (enc x)) (hv : Valid x) :
    x.held = true := ((reachable_inv hr).2 x rfl).validHeld hv

end KanderFormal.Proofs.ServerClaim
