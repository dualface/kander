import KanderFormal.Proofs.ServerRequestCases

/-
Proof obligations of the server request machine: creation, answers, handoff
supersession, and the invariant. The theorems about dispatch proposals are in
`ServerRequestProposal.lean`. They hold for every value of `maxGen` and
`maxRev`.
-/
namespace KanderFormal.Proofs.ServerRequest

open KanderFormal KanderFormal.Machines.ServerRequest KanderFormal.Proofs.Scalar

/-! ## Theorem 1: a request goes to the current holder -/

/-- Theorem 1: a request is created only for a card in `working` or `review`
whose claim record is active, for the claim's generation, while the lease has
not expired. A request of a proposal binds a card revision that is not zero. -/
theorem create_spec (p : Params) (x : Rq) (he ac lv : Bool) (cg tg : Nat) (pr : Bool) (ir : Nat) :
    step M p (enc x) ⟨"create", [boolV he, boolV ac, boolV lv, natV cg, natV tg, boolV pr, natV ir]⟩ ≠ none ↔
      cg ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ir ≤ p "maxRev" ∧ 1 ≤ p "maxRev" ∧ x.st = 0 ∧
        he = true ∧ ac = true ∧ tg = cg ∧ lv = true ∧ (pr = true ↔ ir ≠ 0) := by
  rw [step_create]
  by_cases h : cg ≤ p "maxGen" ∧ tg ≤ p "maxGen" ∧ ir ≤ p "maxRev" ∧ 1 ≤ p "maxRev" ∧ x.st = 0 ∧
    he = true ∧ ac = true ∧ tg = cg ∧ lv = true ∧ (pr = true ↔ ir ≠ 0) <;> simp [h]

/-- Theorem 1: a created request is open at revision 1 and targets the
generation it was created for. -/
theorem create_result (p : Params) (x : Rq) (he ac lv : Bool) (cg tg : Nat) (pr : Bool) (ir : Nat) {s' : State}
    (h : step M p (enc x) ⟨"create", [boolV he, boolV ac, boolV lv, natV cg, natV tg, boolV pr, natV ir]⟩ =
      some s') : s' = enc ⟨1, 1, tg, pr, ir⟩ := by
  rw [step_create] at h
  exact (ite_some_enc h).2

/-- Theorem 1: no request is created for a request that exists, a card
outside `working` and `review`, a record that is not active, another
generation, or an expired lease. -/
theorem create_refused (p : Params) (x : Rq) (he ac lv : Bool) (cg tg : Nat) (pr : Bool) (ir : Nat) :
    (x.st ≠ 0 → step M p (enc x) ⟨"create", [boolV he, boolV ac, boolV lv, natV cg, natV tg, boolV pr,
      natV ir]⟩ = none) ∧
    (he = false → step M p (enc x) ⟨"create", [boolV he, boolV ac, boolV lv, natV cg, natV tg, boolV pr,
      natV ir]⟩ = none) ∧
    (ac = false → step M p (enc x) ⟨"create", [boolV he, boolV ac, boolV lv, natV cg, natV tg, boolV pr,
      natV ir]⟩ = none) ∧
    (tg ≠ cg → step M p (enc x) ⟨"create", [boolV he, boolV ac, boolV lv, natV cg, natV tg, boolV pr,
      natV ir]⟩ = none) ∧
    (lv = false → step M p (enc x) ⟨"create", [boolV he, boolV ac, boolV lv, natV cg, natV tg, boolV pr,
      natV ir]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_create]; apply ite_none; intro c; obtain ⟨-, -, -, -, c5, c6, c7, c8, c9, -⟩ := c; simp_all)

/-- Theorem 1: the check of a proposal's card version accepts only an inspected
revision that is not zero and is the card's. It keeps the request. -/
theorem bind_spec (p : Params) (x : Rq) (tk : Bool) (ir ca : Nat) :
    step M p (enc x) ⟨"bind", [boolV tk, natV ir, natV ca]⟩ ≠ none ↔
      ir ≤ p "maxRev" ∧ ca ≤ p "maxRev" ∧ tk = true ∧ ir ≠ 0 ∧ ir = ca := by
  rw [step_bind]
  by_cases h : ir ≤ p "maxRev" ∧ ca ≤ p "maxRev" ∧ tk = true ∧ ir ≠ 0 ∧ ir = ca <;> simp [h]

theorem bind_refused (p : Params) (x : Rq) (tk : Bool) (ir ca : Nat) :
    (tk = false → step M p (enc x) ⟨"bind", [boolV tk, natV ir, natV ca]⟩ = none) ∧
    (ir = 0 → step M p (enc x) ⟨"bind", [boolV tk, natV ir, natV ca]⟩ = none) ∧
    (ir ≠ ca → step M p (enc x) ⟨"bind", [boolV tk, natV ir, natV ca]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_bind]; apply ite_none; intro c; obtain ⟨-, -, c3, c4, c5⟩ := c; simp_all)

/-! ## Theorem 2: answers -/

/-- Theorem 2: a request status moves only forward: a request is created
`open`; an open request becomes `accepted`, `done`, `rejected` or `superseded`;
an accepted request becomes `done`, `rejected` or `superseded`; nothing else
changes a status. -/
theorem flow {p : Params} {x y : Rq} {e : Event} {s' : State} (hs : x.st < 6)
    (h : step M p (enc x) e = some s') (hy : s' = enc y) :
    y.st = x.st ∨ (x.st = 0 ∧ y.st = 1) ∨ (x.st = 1 ∧ 2 ≤ y.st) ∨ (x.st = 2 ∧ 3 ≤ y.st) := by
  rcases accepted_cases h with ⟨tk, ir, ca, rfl⟩ | ⟨he, ac, lv, cg, tg, pr, ir, rfl⟩ |
    ⟨he, ac, lv, cg, tg, ca, sm, rfl⟩ | ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩ | ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩ |
    ⟨ng, rfl⟩ | ⟨un, ho, cg, wi, ca, ma, rfl⟩ | ⟨co, fo, im, cg, rfl⟩
  · rw [step_bind] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact .inl rfl
  · rw [step_create] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, h0, -⟩ := hc; exact .inr (.inl ⟨h0, rfl⟩)
  · rw [step_recreate] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact .inl rfl
  · rw [step_answerSame] at h; obtain ⟨-, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this; exact .inl rfl
  · rw [step_answer] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, -, ⟨hA, -, hA3, -, hA5, -, -⟩, -, hset⟩ := hc
    dsimp only
    omega
  · rw [step_supersede] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, hst⟩ := hc; dsimp only; omega
  · rw [step_prepare] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, -, -, -, hst, -⟩ := hc; dsimp only; omega
  · rw [step_complete] at h; obtain ⟨hc, rfl⟩ := ite_some_enc h; have := enc_inj hy; subst this
    obtain ⟨-, -, -, -, -, -, hst, -⟩ := hc; dsimp only; omega

/-- Theorem 2: a request that is `done`, `rejected` or `superseded` never
changes again, whatever event arrives. -/
theorem settled_frozen {p : Params} {x : Rq} {e : Event} {s' : State} (hs : x.st < 6) (hx : 3 ≤ x.st)
    (h : step M p (enc x) e = some s') : s' = enc x := by
  rcases accepted_cases h with ⟨tk, ir, ca, rfl⟩ | ⟨he, ac, lv, cg, tg, pr, ir, rfl⟩ |
    ⟨he, ac, lv, cg, tg, ca, sm, rfl⟩ | ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩ | ⟨ho, cg, an, ex, sn, wi, ca, rfl⟩ |
    ⟨ng, rfl⟩ | ⟨un, ho, cg, wi, ca, ma, rfl⟩ | ⟨co, fo, im, cg, rfl⟩
  · rw [step_bind] at h; exact (ite_some_enc h).2
  · rw [step_create] at h; obtain ⟨hc, -⟩ := ite_some_enc h
    obtain ⟨-, -, -, -, h0, -⟩ := hc; omega
  · rw [step_recreate] at h; exact (ite_some_enc h).2
  · rw [step_answerSame] at h; exact (ite_some_enc h).2
  · rw [step_answer] at h; obtain ⟨hc, -⟩ := ite_some_enc h
    obtain ⟨-, -, -, -, -, ⟨-, -, hA3, -, hA5, -, -⟩, -, hset⟩ := hc; omega
  · rw [step_supersede] at h; obtain ⟨hc, -⟩ := ite_some_enc h
    obtain ⟨-, -, -, hst⟩ := hc; omega
  · rw [step_prepare] at h; obtain ⟨hc, -⟩ := ite_some_enc h
    obtain ⟨-, -, -, -, -, -, -, hst, -⟩ := hc; omega
  · rw [step_complete] at h; obtain ⟨hc, -⟩ := ite_some_enc h
    obtain ⟨-, -, -, -, -, -, hst, -⟩ := hc; omega

/-- Theorem 2: an answer that repeats the status and the note of a request is
accepted and changes nothing, once the holder, the generation, and the expected
revision pass (a settled request included). -/
theorem answer_same_spec (p : Params) (x : Rq) (ho : Bool) (cg an ex : Nat) (sn wi : Bool) (ca : Nat) :
    step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠ none ↔
      cg ≤ p "maxGen" ∧ an < 6 ∧ ex ≤ p "maxRev" ∧ ca ≤ p "maxRev" ∧
        AnsChecks x ho cg an ex wi ca ∧ SameAns x an sn := by
  rw [step_answerSame]
  by_cases h : cg ≤ p "maxGen" ∧ an < 6 ∧ ex ≤ p "maxRev" ∧ ca ≤ p "maxRev" ∧
    AnsChecks x ho cg an ex wi ca ∧ SameAns x an sn <;> simp [h]

theorem answer_same_keeps (p : Params) (x : Rq) (ho : Bool) (cg an ex : Nat) (sn wi : Bool) (ca : Nat)
    {s' : State}
    (h : step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi,
      natV ca]⟩ = some s') : s' = enc x := by
  rw [step_answerSame] at h
  exact (ite_some_enc h).2

/-- Theorem 2: after the holder, the generation, and the expected revision
check, an answer that equals the answered, accepted, done, or rejected request
is accepted. -/
theorem answer_same_accepted (p : Params) (x : Rq) (ho : Bool) (cg ex : Nat) (sn wi : Bool) (ca : Nat)
    (hcg : cg ≤ p "maxGen") (hex : ex ≤ p "maxRev") (hca : ca ≤ p "maxRev") (hx : 2 ≤ x.st ∧ x.st ≤ 4)
    (hho : ho = true) (hg : x.tgen = cg) (he : ex = 0 ∨ ex = x.rev) (hsn : sn = true) :
    step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV x.st, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠
      none := by
  rw [answer_same_spec]
  refine ⟨hcg, by omega, hex, hca, ⟨by omega, hho, by omega, hg, by omega, he, ?_⟩, rfl, hsn⟩
  left
  rintro ⟨-, h1, -⟩
  omega

/-- Theorem 2: a new answer to a request that is not settled changes its
status to the answer and increases its revision by exactly one. -/
theorem answer_spec (p : Params) (x : Rq) (ho : Bool) (cg an ex : Nat) (sn wi : Bool) (ca : Nat) :
    step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠ none ↔
      cg ≤ p "maxGen" ∧ an < 6 ∧ ex ≤ p "maxRev" ∧ ca ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
        AnsChecks x ho cg an ex wi ca ∧ ¬SameAns x an sn ∧ ¬(x.st = 3 ∨ x.st = 4) := by
  rw [step_answer]
  by_cases h : cg ≤ p "maxGen" ∧ an < 6 ∧ ex ≤ p "maxRev" ∧ ca ≤ p "maxRev" ∧ x.rev + 1 ≤ p "maxRev" ∧
    AnsChecks x ho cg an ex wi ca ∧ ¬SameAns x an sn ∧ ¬(x.st = 3 ∨ x.st = 4) <;> simp [h]

theorem answer_result (p : Params) (x : Rq) (ho : Bool) (cg an ex : Nat) (sn wi : Bool) (ca : Nat) {s' : State}
    (h : step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ =
      some s') : s' = enc { x with st := an, rev := x.rev + 1 } := by
  rw [step_answer] at h
  exact (ite_some_enc h).2

/-- Theorem 2: a settled request cannot change its answer: a new answer to a
`done` or `rejected` request is refused, and a repeated one is accepted only
when it names the same status and the same note. -/
theorem settled_answer (p : Params) (x : Rq) (hx : x.st = 3 ∨ x.st = 4) (ho : Bool) (cg an ex : Nat)
    (sn wi : Bool) (ca : Nat) :
    step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ = none ∧
    (step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠
      none → an = x.st ∧ sn = true) := by
  refine ⟨?_, fun h => ?_⟩
  · rw [step_answer]; apply ite_none; intro c; obtain ⟨-, -, -, -, -, -, -, hset⟩ := c; exact hset hx
  · rw [answer_same_spec] at h
    obtain ⟨-, -, -, -, -, hs⟩ := h
    exact hs

/-- Theorem 2: an answer is a repeated one or a new one, never both. -/
theorem answer_exclusive (p : Params) (x : Rq) (ho : Bool) (cg an ex : Nat) (sn wi : Bool) (ca : Nat)
    (h : step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi,
      natV ca]⟩ ≠ none) :
    step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ = none := by
  rw [answer_same_spec] at h
  obtain ⟨-, -, -, -, -, hs⟩ := h
  rw [step_answer]; apply ite_none; intro c
  obtain ⟨-, -, -, -, -, -, hns, -⟩ := c
  exact hns hs

/-- Theorem 2: every new answer changes the status or the note, and increases
the revision by one; the status becomes the answer. The converse does not
hold: the preparation of an accepted proposal also increases its revision
(`prepare_bumps_accepted`). -/
theorem answer_bumps {p : Params} {x y : Rq} {ho : Bool} {cg an ex : Nat} {sn wi : Bool} {ca : Nat} {s' : State}
    (h : step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ =
      some s') (hy : s' = enc y) : y.rev = x.rev + 1 ∧ y.st = an := by
  rw [answer_result p x ho cg an ex sn wi ca h] at hy
  have := enc_inj hy
  subst this
  exact ⟨rfl, rfl⟩

/-- Theorem 2: an expected revision that is given (not zero) must be the
request's, for a repeated and for a new answer. -/
theorem answer_expected (p : Params) (x : Rq) (ho : Bool) (cg an ex : Nat) (sn wi : Bool) (ca : Nat) :
    (step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠ none →
      ex = 0 ∨ ex = x.rev) ∧
    (step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠
      none → ex = 0 ∨ ex = x.rev) := by
  refine ⟨fun h => ?_, fun h => ?_⟩
  · rw [answer_spec] at h; obtain ⟨-, -, -, -, -, ⟨-, -, -, -, -, he, -⟩, -⟩ := h; exact he
  · rw [answer_same_spec] at h; obtain ⟨-, -, -, -, ⟨-, -, -, -, -, he, -⟩, -⟩ := h; exact he

/-- Theorem 2: the first acceptance of an open proposal needs an acceptance
window that is not over and the card at the revision the proposal binds. -/
theorem answer_accept_proposal (p : Params) (x : Rq) (ho : Bool) (cg an ex : Nat) (sn wi : Bool) (ca : Nat)
    (hp : x.prop = true) (hs : x.st = 1) (ha : an = 2) :
    step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠ none →
      wi = true ∧ x.prev = ca := by
  intro h
  rw [answer_spec] at h
  obtain ⟨-, -, -, -, -, ⟨-, -, -, -, -, -, hc⟩, -⟩ := h
  rcases hc with hc | hc
  · exact absurd ⟨hp, hs, ha⟩ hc
  · exact hc

/-! ## Theorem 3: the holder answers -/

/-- Theorem 3: only the holder answers (the result of `requireHolder`), and
only a request that targets the holder's generation and that has not been
superseded. -/
theorem answer_holder (p : Params) (x : Rq) (ho : Bool) (cg an ex : Nat) (sn wi : Bool) (ca : Nat) :
    (step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠ none →
      ho = true ∧ x.tgen = cg ∧ x.st ≠ 5 ∧ x.st ≠ 0) ∧
    (step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ ≠
      none → ho = true ∧ x.tgen = cg ∧ x.st ≠ 5 ∧ x.st ≠ 0) := by
  refine ⟨fun h => ?_, fun h => ?_⟩
  · rw [answer_spec] at h
    obtain ⟨-, -, -, -, -, ⟨-, hho, hn0, hg, hn5, -, -⟩, -⟩ := h
    exact ⟨hho, hg, hn5, hn0⟩
  · rw [answer_same_spec] at h
    obtain ⟨-, -, -, -, ⟨-, hho, hn0, hg, hn5, -, -⟩, -⟩ := h
    exact ⟨hho, hg, hn5, hn0⟩

/-- Theorem 3: a superseded request cannot be answered, by anyone. -/
theorem superseded_unanswerable (p : Params) (x : Rq) (hx : x.st = 5) (ho : Bool) (cg an ex : Nat)
    (sn wi : Bool) (ca : Nat) :
    step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ = none ∧
    step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ =
      none := by
  refine ⟨?_, ?_⟩
  · rw [step_answer]; apply ite_none; intro c; obtain ⟨-, -, -, -, -, ⟨-, -, -, -, hn5, -, -⟩, -⟩ := c; exact hn5 hx
  · rw [step_answerSame]; apply ite_none; intro c; obtain ⟨-, -, -, -, ⟨-, -, -, -, hn5, -, -⟩, -⟩ := c; exact hn5 hx

/-- Theorem 3: a request that targets another generation than the holder's
cannot be answered. -/
theorem stale_unanswerable (p : Params) (x : Rq) (hx : x.tgen ≠ cg) (ho : Bool) (an ex : Nat) (sn wi : Bool)
    (ca : Nat) :
    step M p (enc x) ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ = none ∧
    step M p (enc x) ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ =
      none := by
  refine ⟨?_, ?_⟩
  · rw [step_answer]; apply ite_none; intro c; obtain ⟨-, -, -, -, -, ⟨-, -, -, hg, -, -, -⟩, -⟩ := c; exact hx hg
  · rw [step_answerSame]; apply ite_none; intro c; obtain ⟨-, -, -, -, ⟨-, -, -, hg, -, -, -⟩, -⟩ := c; exact hx hg

/-- Theorem 3: without the holder (`requireHolder` refused) nothing is
answered. -/
theorem non_holder_unanswerable (p : Params) (x : Rq) (cg an ex : Nat) (sn wi : Bool) (ca : Nat) :
    step M p (enc x) ⟨"answer", [boolV false, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ = none ∧
    step M p (enc x) ⟨"answerSame", [boolV false, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ =
      none := by
  refine ⟨?_, ?_⟩
  · rw [step_answer]; apply ite_none; intro c; obtain ⟨-, -, -, -, -, ⟨-, hho, -⟩, -⟩ := c; simp at hho
  · rw [step_answerSame]; apply ite_none; intro c; obtain ⟨-, -, -, -, ⟨-, hho, -⟩, -⟩ := c; simp at hho

/-! ## Theorem 4: a handoff supersedes older requests -/

/-- Theorem 4: when the generation rises to `ngen`, a request of an older
generation that is open or accepted becomes superseded and its revision
grows by one; every other request is refused, so it stays. -/
theorem supersede_spec (p : Params) (x : Rq) (ng : Nat) :
    step M p (enc x) ⟨"supersede", [natV ng]⟩ ≠ none ↔
      ng ≤ p "maxGen" ∧ x.rev + 1 ≤ p "maxRev" ∧ x.tgen < ng ∧ (x.st = 1 ∨ x.st = 2) := by
  rw [step_supersede]
  by_cases h : ng ≤ p "maxGen" ∧ x.rev + 1 ≤ p "maxRev" ∧ x.tgen < ng ∧ (x.st = 1 ∨ x.st = 2) <;> simp [h]

theorem supersede_result (p : Params) (x : Rq) (ng : Nat) {s' : State}
    (h : step M p (enc x) ⟨"supersede", [natV ng]⟩ = some s') :
    s' = enc { x with st := 5, rev := x.rev + 1 } := by
  rw [step_supersede] at h
  exact (ite_some_enc h).2

/-- Theorem 4: a request of the new generation or a later one, and a request
that is not open or accepted, is not superseded. -/
theorem supersede_others (p : Params) (x : Rq) (ng : Nat) :
    (ng ≤ x.tgen → step M p (enc x) ⟨"supersede", [natV ng]⟩ = none) ∧
    (x.st = 0 ∨ 3 ≤ x.st → step M p (enc x) ⟨"supersede", [natV ng]⟩ = none) := by
  refine ⟨fun hn => ?_, fun hn => ?_⟩ <;>
    (rw [step_supersede]; apply ite_none; intro c; obtain ⟨-, -, c3, c4⟩ := c; omega)

/-- Theorem 4: a request that was superseded is not answered afterwards. -/
theorem supersede_then_unanswerable (p : Params) (x : Rq) (ng : Nat) {s' : State}
    (h : step M p (enc x) ⟨"supersede", [natV ng]⟩ = some s') (ho : Bool) (cg an ex : Nat) (sn wi : Bool)
    (ca : Nat) :
    step M p s' ⟨"answer", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ = none ∧
    step M p s' ⟨"answerSame", [boolV ho, natV cg, enumV an, natV ex, boolV sn, boolV wi, natV ca]⟩ = none := by
  rw [supersede_result p x ng h]
  exact superseded_unanswerable p _ rfl ho cg an ex sn wi ca

end KanderFormal.Proofs.ServerRequest
