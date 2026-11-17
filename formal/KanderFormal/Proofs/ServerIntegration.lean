import KanderFormal.Proofs.ServerIntegrationTrans

/-
Proof obligations of the server integration machine, part one: the lease
(theorem 1), the claim (theorem 2), revisions and generations (theorem 3), the
facts and the observation ticket (theorem 5), and the completion evidence
(theorems 6 and 7). They hold for every value of `maxGen`, `maxRev`, and
`maxAuth`. Part two (`ServerIntegrationFlow.lean`) proves the phases and the
invariant.
-/
namespace KanderFormal.Proofs.ServerIntegration

open KanderFormal KanderFormal.Machines.ServerIntegration KanderFormal.Proofs.Scalar

/-! ## Theorem 1: at most one valid integration lease -/

/-- `requireIntegrationOwner` accepts exactly the owner of the record. -/
theorem require_owner_iff (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client : Bool) :
    step M p (enc x) ⟨"requireOwner", [boolV hdr, natV g, natV tg, boolV client]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Owner x hdr g tg client := by
  rw [step_requireOwner]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Owner x hdr g tg client <;> simp [h]

/-- `requireIntegration` accepts exactly the owner whose integration actor still
stands: the generation the record was claimed under, accepted by the actor's
`standing` event, and the execution identity and client of the record. -/
theorem require_iff (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) :
    step M p (enc x) ⟨"require", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
        boolV ac]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧ Actor x al ag ae ac := by
  rw [step_require]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧
    Actor x al ag ae ac <;> simp [h]

/-- Theorem 1: in every state, `requireIntegrationOwner` accepts at most one
generation and one token: two accepted requests name the same generation, the
current one, and the record is active. The lease clock plays no part. -/
theorem owner_unique (p : Params) (x : It) {hdr1 g1 tg1 c1 hdr2 g2 tg2 c2 : _}
    (h1 : step M p (enc x) ⟨"requireOwner", [boolV hdr1, natV g1, natV tg1, boolV c1]⟩ ≠ none)
    (h2 : step M p (enc x) ⟨"requireOwner", [boolV hdr2, natV g2, natV tg2, boolV c2]⟩ ≠ none) :
    g1 = x.gen ∧ g2 = x.gen ∧ tg1 = x.gen ∧ tg2 = x.gen ∧ x.st = 1 := by
  rw [require_owner_iff] at h1 h2
  obtain ⟨-, -, -, hs1, hg1, -, ht1⟩ := h1
  obtain ⟨-, -, -, -, hg2, -, ht2⟩ := h2
  exact ⟨hg1, hg2, ht1, ht2, hs1⟩

/-- Theorem 1: every event that presents the owner headers is accepted only for
the owner of the record. -/
theorem owner_required (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) :
    (step M p (enc x) ⟨"require", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
        boolV ac]⟩ ≠ none → Owner x hdr g tg client) ∧
    (∀ fa, step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
        boolV ac, boolV fa]⟩ ≠ none → Owner x hdr g tg client) ∧
    (∀ er rm, step M p (enc x) ⟨"check", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag,
        boolV ae, boolV ac, natV er, boolV rm]⟩ ≠ none → Owner x hdr g tg client) ∧
    (∀ er rm, step M p (enc x) ⟨"checkOwner", [boolV hdr, natV g, natV tg, boolV client, natV er,
        boolV rm]⟩ ≠ none → Owner x hdr g tg client) ∧
    (∀ er rm om os, step M p (enc x) ⟨"pushing", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag,
        boolV ae, boolV ac, natV er, boolV rm, boolV om, boolV os]⟩ ≠ none → Owner x hdr g tg client) ∧
    (∀ er rm os oa ee, step M p (enc x) ⟨"confirm", [boolV hdr, natV g, natV tg, boolV client, boolV al,
        natV ag, boolV ae, boolV ac, natV er, boolV rm, boolV os, boolV oa, boolV ee]⟩ ≠ none →
      Owner x hdr g tg client) ∧
    (∀ er rm de oe os, step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg, boolV client, natV er,
        boolV rm, boolV de, boolV oe, boolV os]⟩ ≠ none → Owner x hdr g tg client) ∧
    (∀ t fa, step M p (enc x) ⟨"guard", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
        boolV ac, boolV t, boolV fa]⟩ ≠ none → Owner x hdr g tg client) ∧
    (∀ t, step M p (enc x) ⟨"guardRelease", [boolV hdr, natV g, natV tg, boolV client, boolV t]⟩ ≠ none →
      Owner x hdr g tg client) := by
  refine ⟨fun h => ?_, fun fa h => ?_, fun er rm h => ?_, fun er rm h => ?_, fun er rm om os h => ?_,
    fun er rm os oa ee h => ?_, fun er rm de oe os h => ?_, fun t fa h => ?_, fun t h => ?_⟩
  · rw [step_require] at h; exact (ite_ne_none h).2.2.2.1
  · rw [step_renew] at h; exact (ite_ne_none h).2.2.2.1
  · rw [step_check] at h; exact (ite_ne_none h).2.2.2.2.1
  · rw [step_checkOwner] at h; exact (ite_ne_none h).2.2.2.1
  · rw [step_pushing] at h; exact (ite_ne_none h).2.2.2.2.2.1
  · rw [step_confirm] at h; exact (ite_ne_none h).2.2.2.2.2.1
  · rw [step_release] at h; exact (ite_ne_none h).2.2.2.2.1
  · rw [step_guard] at h; exact (ite_ne_none h).2.2.2.1
  · rw [step_guardRelease] at h; exact (ite_ne_none h).2.2.1

/-- Theorem 1: every event that is guarded by `requireIntegration` also needs
the integration actor to stand. A `release` needs the owner only, and its
arguments carry no actor. -/
theorem actor_required (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) :
    (step M p (enc x) ⟨"require", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
        boolV ac]⟩ ≠ none → Actor x al ag ae ac) ∧
    (∀ fa, step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
        boolV ac, boolV fa]⟩ ≠ none → Actor x al ag ae ac) ∧
    (∀ er rm, step M p (enc x) ⟨"check", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag,
        boolV ae, boolV ac, natV er, boolV rm]⟩ ≠ none → Actor x al ag ae ac) ∧
    (∀ er rm om os, step M p (enc x) ⟨"pushing", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag,
        boolV ae, boolV ac, natV er, boolV rm, boolV om, boolV os]⟩ ≠ none → Actor x al ag ae ac) ∧
    (∀ er rm os oa ee, step M p (enc x) ⟨"confirm", [boolV hdr, natV g, natV tg, boolV client, boolV al,
        natV ag, boolV ae, boolV ac, natV er, boolV rm, boolV os, boolV oa, boolV ee]⟩ ≠ none →
      Actor x al ag ae ac) ∧
    (∀ t fa, step M p (enc x) ⟨"guard", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
        boolV ac, boolV t, boolV fa]⟩ ≠ none → Actor x al ag ae ac) := by
  refine ⟨fun h => ?_, fun fa h => ?_, fun er rm h => ?_, fun er rm om os h => ?_,
    fun er rm os oa ee h => ?_, fun t fa h => ?_⟩
  · rw [step_require] at h; exact (ite_ne_none h).2.2.2.2
  · rw [step_renew] at h; exact (ite_ne_none h).2.2.2.2.1
  · rw [step_check] at h; exact (ite_ne_none h).2.2.2.2.2.1
  · rw [step_pushing] at h; exact (ite_ne_none h).2.2.2.2.2.2.1
  · rw [step_confirm] at h; exact (ite_ne_none h).2.2.2.2.2.2.1
  · rw [step_guard] at h; exact (ite_ne_none h).2.2.2.2.1

/-- Theorem 1: a request whose generation header or token is not the record's
generation is refused by every owner check. -/
theorem stale_refused (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (h : g ≠ x.gen ∨ tg ≠ x.gen) :
    step M p (enc x) ⟨"requireOwner", [boolV hdr, natV g, natV tg, boolV client]⟩ = none ∧
    step M p (enc x) ⟨"require", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac]⟩ = none ∧
    (∀ t, step M p (enc x) ⟨"guardRelease", [boolV hdr, natV g, natV tg, boolV client, boolV t]⟩ = none) := by
  have ho : ¬Owner x hdr g tg client := fun ⟨_, _, h2, _, h3⟩ => by omega
  refine ⟨?_, ?_, fun t => ?_⟩
  · rw [step_requireOwner]; apply ite_none; intro c; exact ho c.2.2
  · rw [step_require]; apply ite_none; intro c; exact ho c.2.2.2.1
  · rw [step_guardRelease]; apply ite_none; intro c; exact ho c.2.2.1

/-! ## Theorem 2: claims -/

/-- Theorem 2: a claim is accepted only when no lease is valid (the record may
be in any phase, including an expired `reserved` or `pushing`), the request
names the generation and the revision of the record, both have room for one
more, the execution identity differs from the record's, the integration actor
is accepted and presents the request's client and execution identity, and the
purpose is one of the three. -/
theorem claim_spec (p : Params) (x : It) (eg er : Nat) (roomGen roomRev sameExec actor actorMatch : Bool)
    (ag pu : Nat) (contained : Bool) :
    step M p (enc x) ⟨"claim", [natV eg, natV er, boolV roomGen, boolV roomRev, boolV sameExec, boolV actor,
        boolV actorMatch, natV ag, enumV pu, boolV contained]⟩ ≠ none ↔
      eg ≤ p "maxGen" ∧ er ≤ p "maxRev" ∧ ag ≤ p "maxAuth" ∧ pu < 4 ∧ x.gen + 1 ≤ p "maxGen" ∧
        x.rev + 1 ≤ p "maxRev" ∧ ¬Valid x ∧ eg = x.gen ∧ er = x.rev ∧ roomGen = true ∧ roomRev = true ∧
        sameExec = false ∧ actor = true ∧ actorMatch = true ∧ pu ≠ 0 := by
  rw [step_claim]
  by_cases h : eg ≤ p "maxGen" ∧ er ≤ p "maxRev" ∧ ag ≤ p "maxAuth" ∧ pu < 4 ∧ x.gen + 1 ≤ p "maxGen" ∧
    x.rev + 1 ≤ p "maxRev" ∧ ¬Valid x ∧ eg = x.gen ∧ er = x.rev ∧ roomGen = true ∧ roomRev = true ∧
    sameExec = false ∧ actor = true ∧ actorMatch = true ∧ pu ≠ 0 <;> simp [h]

/-- Theorem 2: an accepted claim writes the next generation and the next
revision, phase `reserved`, a live lease, the request's purpose and containment
flag, the actor's generation as the authority, and no observed target. -/
theorem claim_result (p : Params) (x : It) (eg er : Nat) (roomGen roomRev sameExec actor actorMatch : Bool)
    (ag pu : Nat) (contained : Bool) {s' : State}
    (h : step M p (enc x) ⟨"claim", [natV eg, natV er, boolV roomGen, boolV roomRev, boolV sameExec,
      boolV actor, boolV actorMatch, natV ag, enumV pu, boolV contained]⟩ = some s') :
    s' = enc ⟨1, x.gen + 1, x.rev + 1, true, 1, pu, contained, ag, false⟩ := by
  rw [step_claim] at h
  obtain ⟨-, rfl⟩ := ite_some_enc h
  rfl

/-- Theorem 2: a lease that is not valid, whatever phase the record is in, is
claimed when the request is current and the actor is accepted. -/
theorem claim_accepts (p : Params) (x : It) (eg er : Nat) (ag pu : Nat) (contained : Bool)
    (hv : ¬Valid x) (hg : eg = x.gen) (hr : er = x.rev) (hpu : pu ≠ 0) (hpu4 : pu < 4)
    (hb : eg ≤ p "maxGen" ∧ er ≤ p "maxRev" ∧ ag ≤ p "maxAuth" ∧ x.gen + 1 ≤ p "maxGen" ∧
      x.rev + 1 ≤ p "maxRev") :
    step M p (enc x) ⟨"claim", [natV eg, natV er, boolV true, boolV true, boolV false, boolV true,
      boolV true, natV ag, enumV pu, boolV contained]⟩ ≠ none := by
  rw [claim_spec]
  obtain ⟨b1, b2, b3, b4, b5⟩ := hb
  exact ⟨b1, b2, b3, hpu4, b4, b5, hv, hg, hr, rfl, rfl, rfl, rfl, rfl, hpu⟩

/-- Theorem 2: an expired `reserved` or `pushing` record is claimed like any
other record whose lease is not valid. -/
theorem claim_accepts_expired (p : Params) (x : It) (ag pu : Nat) (contained : Bool)
    (hph : x.ph = 1 ∨ x.ph = 2) (hl : x.live = false) (hpu : pu ≠ 0) (hpu4 : pu < 4)
    (hb : x.gen ≤ p "maxGen" ∧ x.rev ≤ p "maxRev" ∧ ag ≤ p "maxAuth" ∧ x.gen + 1 ≤ p "maxGen" ∧
      x.rev + 1 ≤ p "maxRev") :
    step M p (enc x) ⟨"claim", [natV x.gen, natV x.rev, boolV true, boolV true, boolV false, boolV true,
      boolV true, natV ag, enumV pu, boolV contained]⟩ ≠ none := by
  have hv : ¬Valid x := fun ⟨_, h⟩ => by simp [hl] at h
  have := hph
  exact claim_accepts p x x.gen x.rev ag pu contained hv rfl rfl hpu hpu4 hb

/-- Theorem 2: a valid lease is never claimed. -/
theorem claim_valid_refused (p : Params) (x : It) (eg er : Nat) (rg rr se ac am : Bool) (ag pu : Nat)
    (ct : Bool) (hv : Valid x) :
    step M p (enc x) ⟨"claim", [natV eg, natV er, boolV rg, boolV rr, boolV se, boolV ac, boolV am, natV ag,
      enumV pu, boolV ct]⟩ = none := by
  rw [step_claim]; apply ite_none; intro c; exact c.2.2.2.2.2.2.1 hv

/-- Theorem 2: a claim that does not name the record's generation or revision is
refused. -/
theorem claim_stale_refused (p : Params) (x : It) (eg er : Nat) (rg rr se ac am : Bool) (ag pu : Nat)
    (ct : Bool) (h : eg ≠ x.gen ∨ er ≠ x.rev) :
    step M p (enc x) ⟨"claim", [natV eg, natV er, boolV rg, boolV rr, boolV se, boolV ac, boolV am, natV ag,
      enumV pu, boolV ct]⟩ = none := by
  rw [step_claim]; apply ite_none; intro c
  obtain ⟨-, -, -, -, -, -, -, hg, hr, -⟩ := c
  omega

/-- Theorem 2: a claim without room for one more generation or revision is
refused. -/
theorem claim_room_refused (p : Params) (x : It) (eg er : Nat) (se ac am : Bool) (ag pu : Nat) (ct : Bool) :
    (∀ rr, step M p (enc x) ⟨"claim", [natV eg, natV er, boolV false, boolV rr, boolV se, boolV ac, boolV am,
      natV ag, enumV pu, boolV ct]⟩ = none) ∧
    (∀ rg, step M p (enc x) ⟨"claim", [natV eg, natV er, boolV rg, boolV false, boolV se, boolV ac, boolV am,
      natV ag, enumV pu, boolV ct]⟩ = none) := by
  constructor
  · intro rr; rw [step_claim]; apply ite_none; intro c; simp at c
  · intro rg; rw [step_claim]; apply ite_none; intro c; simp at c

/-- Theorem 2: a claim with the execution identity of the record, or whose
actor is not accepted or does not present the request's client and execution
identity, is refused. -/
theorem claim_identity_refused (p : Params) (x : It) (eg er : Nat) (rg rr : Bool) (ag pu : Nat) (ct : Bool) :
    (∀ ac am, step M p (enc x) ⟨"claim", [natV eg, natV er, boolV rg, boolV rr, boolV true, boolV ac,
      boolV am, natV ag, enumV pu, boolV ct]⟩ = none) ∧
    (∀ se am, step M p (enc x) ⟨"claim", [natV eg, natV er, boolV rg, boolV rr, boolV se, boolV false,
      boolV am, natV ag, enumV pu, boolV ct]⟩ = none) ∧
    (∀ se ac, step M p (enc x) ⟨"claim", [natV eg, natV er, boolV rg, boolV rr, boolV se, boolV ac,
      boolV false, natV ag, enumV pu, boolV ct]⟩ = none) := by
  refine ⟨fun ac am => ?_, fun se am => ?_, fun se ac => ?_⟩ <;> (rw [step_claim]; apply ite_none; intro c; simp at c)

/-! ## Theorem 3: revisions and generations -/

/-- Theorem 3: a renewal refreshes the lease and keeps the revision, the
generation, the phase, and the authority. -/
theorem renew_keeps (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac fa : Bool) {s' : State}
    (h : step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, boolV fa]⟩ = some s') : s' = enc { x with live := true } := by
  rw [step_renew] at h
  obtain ⟨-, rfl⟩ := ite_some_enc h
  rfl

/-- Theorem 3: every accepted action names the revision of the record, which is
not zero, and the revision has room for one more. -/
theorem action_cas (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) (er : Nat) (rm : Bool) :
    (∀ om os, step M p (enc x) ⟨"pushing", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag,
        boolV ae, boolV ac, natV er, boolV rm, boolV om, boolV os]⟩ ≠ none →
      er = x.rev ∧ er ≠ 0 ∧ rm = true ∧ x.rev + 1 ≤ p "maxRev") ∧
    (∀ os oa ee, step M p (enc x) ⟨"confirm", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag,
        boolV ae, boolV ac, natV er, boolV rm, boolV os, boolV oa, boolV ee]⟩ ≠ none →
      er = x.rev ∧ er ≠ 0 ∧ rm = true ∧ x.rev + 1 ≤ p "maxRev") ∧
    (∀ de oe os, step M p (enc x) ⟨"release", [boolV hdr, natV g, natV tg, boolV client, natV er, boolV rm,
        boolV de, boolV oe, boolV os]⟩ ≠ none →
      er = x.rev ∧ er ≠ 0 ∧ rm = true ∧ x.rev + 1 ≤ p "maxRev") := by
  refine ⟨fun om os h => ?_, fun os oa ee h => ?_, fun de oe os h => ?_⟩
  · rw [step_pushing] at h
    obtain ⟨-, -, -, -, h5, -, -, h8, h9, h10, -⟩ := ite_ne_none h
    exact ⟨h8, h9, h10, h5⟩
  · rw [step_confirm] at h
    obtain ⟨-, -, -, -, h5, -, -, h8, h9, h10, -⟩ := ite_ne_none h
    exact ⟨h8, h9, h10, h5⟩
  · rw [step_release] at h
    obtain ⟨-, -, -, h4, -, h6, h7, h8, -⟩ := ite_ne_none h
    exact ⟨h6, h7, h8, h4⟩

/-- Theorem 3: the revision changes only at a claim or an accepted action, and
then by exactly one. -/
theorem rev_change {p : Params} {x y : It} {v : Ev} {s' : State}
    (h : step M p (enc x) v.toEvent = some s') (hy : s' = enc y) (hne : y.rev ≠ x.rev) :
    y.rev = x.rev + 1 ∧ v.bumpsRev := by
  obtain ⟨z, hz, -, hr⟩ := accepted_trans h
  have := enc_inj (hy ▸ hz : enc y = enc z)
  subst this
  cases v <;> simp only [Ev.res] at hr <;> subst hr <;> simp_all [Ev.bumpsRev]

/-- Theorem 3: the generation and the authority generation change only at a
claim, and the generation then grows by exactly one. -/
theorem gen_auth_change {p : Params} {x y : It} {v : Ev} {s' : State}
    (h : step M p (enc x) v.toEvent = some s') (hy : s' = enc y) (hne : y.gen ≠ x.gen ∨ y.au ≠ x.au) :
    y.gen = x.gen + 1 ∧ v.isClaim := by
  obtain ⟨z, hz, -, hr⟩ := accepted_trans h
  have := enc_inj (hy ▸ hz : enc y = enc z)
  subst this
  cases v <;> simp only [Ev.res] at hr <;> subst hr <;> simp_all [Ev.isClaim]

/-- Theorem 3: the authority generation of a new record is the generation of
the integration actor's lease (`actorGen`), and it is not changed by anything
else. -/
theorem claim_authority (p : Params) (x : It) (eg er : Nat) (rg rr se ac am : Bool) (ag pu : Nat) (ct : Bool)
    {s' : State} {y : It}
    (h : step M p (enc x) ⟨"claim", [natV eg, natV er, boolV rg, boolV rr, boolV se, boolV ac, boolV am, natV ag,
      enumV pu, boolV ct]⟩ = some s') (hy : s' = enc y) : y.au = ag := by
  rw [claim_result p x eg er rg rr se ac am ag pu ct h] at hy
  have := enc_inj hy.symm
  subst this
  rfl

/-! ## Theorem 5: facts and observation tickets -/

/-- Theorem 5: the check inside the claim transaction needs a valid observation
ticket, the accepted actor, and the integration facts. -/
theorem claim_guard_spec (p : Params) (x : It) (t ac fa : Bool) :
    step M p (enc x) ⟨"claimGuard", [boolV t, boolV ac, boolV fa]⟩ ≠ none ↔
      t = true ∧ ac = true ∧ fa = true := by
  rw [step_claimGuard]
  by_cases h : t = true ∧ ac = true ∧ fa = true <;> simp [h]

/-- Theorem 5: a renewal needs `requireIntegration` and the integration facts. -/
theorem renew_spec (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac fa : Bool) :
    step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae, boolV ac,
      boolV fa]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧ Actor x al ag ae ac ∧
        fa = true := by
  rw [step_renew]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧
    Actor x al ag ae ac ∧ fa = true <;> simp [h]

/-- Theorem 5: the check inside the transaction of `pushing` and `confirm` needs
`requireIntegration`, a valid observation ticket, and the integration facts. -/
theorem guard_spec (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac t fa : Bool) :
    step M p (enc x) ⟨"guard", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae, boolV ac,
      boolV t, boolV fa]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧ Actor x al ag ae ac ∧
        t = true ∧ fa = true := by
  rw [step_guard]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ag ≤ p "maxAuth" ∧ Owner x hdr g tg client ∧
    Actor x al ag ae ac ∧ t = true ∧ fa = true <;> simp [h]

/-- Theorem 5: the check inside the transaction of `release` needs the owner and
a valid observation ticket, and neither the actor's lease nor the facts. -/
theorem guard_release_spec (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client t : Bool) :
    step M p (enc x) ⟨"guardRelease", [boolV hdr, natV g, natV tg, boolV client, boolV t]⟩ ≠ none ↔
      g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Owner x hdr g tg client ∧ t = true := by
  rw [step_guardRelease]
  by_cases h : g ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ Owner x hdr g tg client ∧ t = true <;> simp [h]

/-- Theorem 5: a request without the integration facts or without a valid
observation ticket is refused by every check inside a transaction that needs
them. -/
theorem facts_ticket_refused (p : Params) (x : It) (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat)
    (ae ac : Bool) :
    (∀ ac' fa, step M p (enc x) ⟨"claimGuard", [boolV false, boolV ac', boolV fa]⟩ = none) ∧
    (∀ t ac', step M p (enc x) ⟨"claimGuard", [boolV t, boolV ac', boolV false]⟩ = none) ∧
    (step M p (enc x) ⟨"renew", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae, boolV ac,
      boolV false]⟩ = none) ∧
    (∀ fa, step M p (enc x) ⟨"guard", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, boolV false, boolV fa]⟩ = none) ∧
    (∀ t, step M p (enc x) ⟨"guard", [boolV hdr, natV g, natV tg, boolV client, boolV al, natV ag, boolV ae,
      boolV ac, boolV t, boolV false]⟩ = none) ∧
    (step M p (enc x) ⟨"guardRelease", [boolV hdr, natV g, natV tg, boolV client, boolV false]⟩ = none) := by
  refine ⟨fun ac' fa => ?_, fun t ac' => ?_, ?_, fun fa => ?_, fun t => ?_, ?_⟩ <;>
    first
    | (rw [step_claimGuard]; apply ite_none; intro c; simp at c)
    | (rw [step_renew]; apply ite_none; intro c; simp at c)
    | (rw [step_guard]; apply ite_none; intro c; simp at c)
    | (rw [step_guardRelease]; apply ite_none; intro c; simp at c)

/-! ## Theorem 6: the completion evidence of a task -/

/-- Theorem 6: the evidence of a move into `done` is accepted exactly when the
integration is not applicable, or the receipt (the state) is `integrated`,
`released`, for the purpose `deliver`, and observed a commit SHA, lists the
task's generation with a delivery commit, and the review is closed with the
receipt's commit as its plan target. -/
theorem completion_spec (p : Params) (x : It) (cl pn nd me pc : Bool) :
    step M p (enc x) ⟨"completion", [boolV cl, boolV pn, boolV nd, boolV me, boolV pc]⟩ ≠ none ↔
      (cl = true ∧ pn = true ∧ nd = true) ∨
        (x.ph = 3 ∧ x.st = 3 ∧ x.pu = 3 ∧ x.ob = true ∧ me = true ∧ cl = true ∧ pc = true) := by
  rw [step_completion]
  by_cases h : (cl = true ∧ pn = true ∧ nd = true) ∨
    (x.ph = 3 ∧ x.st = 3 ∧ x.pu = 3 ∧ x.ob = true ∧ me = true ∧ cl = true ∧ pc = true) <;> simp [h]

/-- Theorem 6: unless the integration is not applicable, the evidence needs the
published receipt of a final delivery. -/
theorem completion_needs_receipt (p : Params) (x : It) (cl pn nd me pc : Bool)
    (hna : ¬(cl = true ∧ pn = true ∧ nd = true))
    (h : step M p (enc x) ⟨"completion", [boolV cl, boolV pn, boolV nd, boolV me, boolV pc]⟩ ≠ none) :
    x.ph = 3 ∧ x.st = 3 ∧ x.pu = 3 ∧ x.ob = true ∧ me = true ∧ cl = true ∧ pc = true := by
  rw [completion_spec] at h
  rcases h with h | h
  · exact absurd h hna
  · exact h

/-- Theorem 6: a receipt whose phase is not `integrated`, whose lease is not
`released`, or whose purpose is not `deliver` is never evidence, unless the
integration is not applicable. -/
theorem completion_wrong_receipt_refused (p : Params) (x : It) (cl pn nd me pc : Bool)
    (hna : ¬(cl = true ∧ pn = true ∧ nd = true)) (hw : x.ph ≠ 3 ∨ x.st ≠ 3 ∨ x.pu ≠ 3 ∨ x.ob = false) :
    step M p (enc x) ⟨"completion", [boolV cl, boolV pn, boolV nd, boolV me, boolV pc]⟩ = none := by
  rw [step_completion]; apply ite_none; intro c
  rcases c with c | c
  · exact hna c
  · obtain ⟨h1, h2, h3, h4, -⟩ := c
    rcases hw with hw | hw | hw | hw <;> simp_all

/-! ## Theorem 7: the integration evidence of a wrap-up -/

/-- Theorem 7: the evidence of a wrap-up dispatch is accepted exactly when it
needs no integration, or it names the integration, the execution and a
generation that is not zero, and the published execution (the state) matches it
item by item. -/
theorem wrapup_spec (p : Params) (x : It) (nr idg idm : Bool) (ig : Nat) (sm tm cm me cl ps rs : Bool) :
    step M p (enc x) ⟨"wrapUp", [boolV nr, boolV idg, boolV idm, natV ig, boolV sm, boolV tm, boolV cm,
      boolV me, boolV cl, boolV ps, boolV rs]⟩ ≠ none ↔
      ig ≤ p "maxGen" ∧ (nr = true ∨
        (idg = true ∧ ig ≠ 0 ∧ idm = true ∧ x.ph = 3 ∧ x.st = 3 ∧ ig = x.gen ∧ x.pu = 3 ∧ sm = true ∧
          tm = true ∧ cm = true ∧ me = true ∧ cl = true ∧ ps = true ∧ rs = true)) := by
  rw [step_wrapUp]
  by_cases h : ig ≤ p "maxGen" ∧ (nr = true ∨
    (idg = true ∧ ig ≠ 0 ∧ idm = true ∧ x.ph = 3 ∧ x.st = 3 ∧ ig = x.gen ∧ x.pu = 3 ∧ sm = true ∧
      tm = true ∧ cm = true ∧ me = true ∧ cl = true ∧ ps = true ∧ rs = true)) <;> simp [h]

/-- Theorem 7: when the evidence needs an integration, an accepted wrap-up
evidence names a published execution that is `integrated`, `released`, of the
generation the evidence names, for the purpose `deliver`, and matches the
source commit, the target, and the project; the task's generation is a member
with a delivery commit, and the review is closed on the source commit. -/
theorem wrapup_needs_evidence (p : Params) (x : It) (idg idm : Bool) (ig : Nat) (sm tm cm me cl ps rs : Bool)
    (h : step M p (enc x) ⟨"wrapUp", [boolV false, boolV idg, boolV idm, natV ig, boolV sm, boolV tm, boolV cm,
      boolV me, boolV cl, boolV ps, boolV rs]⟩ ≠ none) :
    idg = true ∧ ig ≠ 0 ∧ idm = true ∧ x.ph = 3 ∧ x.st = 3 ∧ ig = x.gen ∧ x.pu = 3 ∧ sm = true ∧
      tm = true ∧ cm = true ∧ me = true ∧ cl = true ∧ ps = true ∧ rs = true := by
  rw [wrapup_spec] at h
  obtain ⟨-, h | h⟩ := h
  · simp at h
  · exact h

/-- Theorem 7: a project without a common remote needs no integration evidence
when the dispatch names no integration. -/
theorem wrapup_not_required (p : Params) (x : It) (idg idm : Bool) (ig : Nat) (sm tm cm me cl ps rs : Bool)
    (hb : ig ≤ p "maxGen") :
    step M p (enc x) ⟨"wrapUp", [boolV true, boolV idg, boolV idm, natV ig, boolV sm, boolV tm, boolV cm,
      boolV me, boolV cl, boolV ps, boolV rs]⟩ ≠ none := by
  rw [wrapup_spec]; exact ⟨hb, .inl rfl⟩

end KanderFormal.Proofs.ServerIntegration
