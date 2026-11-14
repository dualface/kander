import KanderFormal.Obligations
import KanderFormal.Proofs.ReviewObligationSafety

/-
Proof obligations of the review obligation machine.

The theorems hold for every role list and every value of `maxBatch`, through
the model (`ReviewObligationModel.lean`), the independent observer
(`ReviewObligationObserver.lean`), and the simulation between them
(`ReviewObligationSafety.lean`).
-/
namespace KanderFormal.Proofs.ReviewObligation

open KanderFormal KanderFormal.Machines.ReviewObligation

abbrev M := reviewObligationMachine

theorem wellTyped : M.wellTyped = true := by decide

/-- A history of events the machine accepts, from the initial state. -/
inductive Hist (m : Machine) (p : Params) : List Event → State → Prop
  | nil : Hist m p [] (initial m p)
  | snoc {hs : List Event} {s s' : State} (e : Event) : Hist m p hs s → step m p s e = some s' → Hist m p (hs ++ [e]) s'

/-- The events of a history as the observer reads them. -/
def decoded (hs : List Event) : List Ev := hs.filterMap decodeEv

theorem hist_accepts {roles : List String} {p : Params} {hs : List Event} {s : State}
    (h : Hist (Mr roles) p hs s) :
    ∃ st, s = enc roles.length (p "maxBatch") st ∧ Accepts roles.length (p "maxBatch") (decoded hs) st := by
  induction h with
  | nil => exact ⟨initSt, initial_enc roles p, Accepts.nil⟩
  | snoc e _ hstep ih =>
    obtain ⟨st, rfl, ha⟩ := ih
    obtain ⟨ev, hd, hg, rfl⟩ := step_enc (accepts_rel ha).2 hstep
    refine ⟨_, rfl, ?_⟩
    simp only [decoded, List.filterMap_append, List.filterMap_cons, hd, List.filterMap_nil]
    exact Accepts.snoc ev ha hg

theorem hist_reachable {m : Machine} {p : Params} {hs : List Event} {s : State} (h : Hist m p hs s) :
    Reachable m p s := by
  induction h with
  | nil => exact Reachable.init
  | snoc e _ hstep ih => exact Reachable.step e ih hstep

theorem reachable_hist {m : Machine} {p : Params} {s : State} (h : Reachable m p s) : ∃ hs, Hist m p hs s := by
  induction h with
  | init => exact ⟨[], Hist.nil⟩
  | step e _ hstep ih => obtain ⟨hs, h⟩ := ih; exact ⟨_, Hist.snoc e h hstep⟩

theorem decode_done {e : Event} {ev : Ev} (hn : e.name = "done") (hd : decodeEv e = some ev) : ∃ c, ev = .done c := by
  obtain ⟨n, as⟩ := e
  simp only at hn
  subst hn
  simp only [decodeEv] at hd
  split at hd
  all_goals first
    | (injection hd with hd; subst hd; exact ⟨_, rfl⟩)
    | (exfalso; simp_all)

theorem decode_releaseRecovery {e : Event} {ev : Ev} (hn : e.name = "releaseRecovery") (hd : decodeEv e = some ev) :
    ev = .releaseRecovery := by
  obtain ⟨n, as⟩ := e
  simp only at hn
  subst hn
  simp only [decodeEv] at hd
  split at hd
  all_goals first
    | (injection hd with hd; subst hd; rfl)
    | (exfalso; simp_all)

/-- Theorem 1, debt safety: whenever the machine accepts a completion, the
independent observer of the history finds no review owed. For every role list
and every bound. -/
theorem debt_safety {roles : List String} {p : Params} {hs : List Event} {s s' : State} {e : Event}
    (h : Hist (Mr roles) p hs s) (hstep : step (Mr roles) p s e = some s') (hn : e.name = "done") :
    Clear roles.length (obsOf roles.length (decoded hs)) := by
  obtain ⟨st, rfl, ha⟩ := hist_accepts h
  obtain ⟨R, hw⟩ := accepts_rel ha
  obtain ⟨ev, hd, hg, -⟩ := step_enc hw hstep
  obtain ⟨c, rfl⟩ := decode_done hn hd
  exact clear_of_doneReady R hg.1

/-- Theorem 2, evidence floor safety: at an accepted completion every debt a drop
or a planned reset left is paid by a closed batch that names the member,
requires the role, and was added after the debt arose; and every role the no-plan
record applies to was reviewed by a conclusion of the current cycle. -/
theorem evidence_floor {roles : List String} {p : Params} {hs : List Event} {s s' : State} {e : Event}
    (h : Hist (Mr roles) p hs s) (hstep : step (Mr roles) p s e = some s') (hn : e.name = "done") :
    let o := obsOf roles.length (decoded hs)
    (∀ d ∈ o.debts, ∃ b ∈ o.batches, d.since ≤ b.id ∧ b.has = true ∧ b.req d.role = true ∧ b.closed = true) ∧
      (o.npRecord = true → ∀ r, r < roles.length → (o.npRoles r || liveReq o r) = true →
        ∃ b ∈ o.batches, b.has = true ∧ b.req r = true ∧ b.closed = true ∧ b.closedCycle = o.cycle ∧
          b.conc r = true) := by
  obtain ⟨hpaid, hnp, -⟩ := debt_safety h hstep hn
  refine ⟨hpaid, fun hr r hlt happ => (hnp hr).2 ⟨r, hlt, happ⟩ r hlt happ⟩

/-- Theorem 3: a released no-plan recovery record is not released again. -/
theorem release_once {roles : List String} {p : Params} {hs : List Event} {s s' : State} {e e' : Event}
    (h : Hist (Mr roles) p hs s) (hstep : step (Mr roles) p s e = some s') (hn : e.name = "releaseRecovery")
    (hn' : e'.name = "releaseRecovery") : step (Mr roles) p s' e' = none := by
  obtain ⟨st, rfl, ha⟩ := hist_accepts h
  obtain ⟨-, hw⟩ := accepts_rel ha
  obtain ⟨ev, hd, hg, rfl⟩ := step_enc hw hstep
  obtain rfl := decode_releaseRecovery hn hd
  cases h2 : step (Mr roles) p (enc roles.length (p "maxBatch") (post roles.length st .releaseRecovery)) e' with
  | none => rfl
  | some s'' =>
    obtain ⟨ev', hd', hg', -⟩ := step_enc (wf_post hw hg) h2
    rw [decode_releaseRecovery hn' hd'] at hg'
    exact absurd hg'.2.2.1 (by simp [post])

/-- Theorem 6: the invariant holds in every reachable state. A reachable state
is the encoding of a well-formed model state, related to the observer of a
history that reaches it. -/
def Inv (roles : List String) (p : Params) (s : State) : Prop :=
  ∃ hs st, Hist (Mr roles) p hs s ∧ s = enc roles.length (p "maxBatch") st ∧
    WF (p "maxBatch") st ∧ Rel roles.length st (obsOf roles.length (decoded hs))

def cert (roles : List String) : SafetyCert (Mr roles) where
  Inv := Inv roles
  init := fun p => ⟨[], initSt, Hist.nil, initial_enc roles p, wf_init _, rel_init _⟩
  step := fun p s s' e ⟨hs, st, hh, hs', _, _⟩ hstep => by
    have h' := Hist.snoc e hh hstep
    obtain ⟨st', rfl, ha⟩ := hist_accepts h'
    obtain ⟨R, hw⟩ := accepts_rel ha
    exact ⟨_, st', h', rfl, hw, R⟩

theorem reachable_inv {roles : List String} {p : Params} {s : State} (h : Reachable (Mr roles) p s) :
    Inv roles p s := (cert roles).reachable h

/-! ## The traces of the replaced prototype (PROVENANCE.md)

Concrete histories over the production roles (PMQA, Security) with
`maxBatch = 3`, checked by evaluation. A role set argument lists PMQA then
Security. -/

/-- Runs a list of events; `none` as soon as one is refused. -/
def runList (m : Machine) (p : Params) : State → List Event → Option State
  | s, [] => some s
  | s, e :: es => (step m p s e).bind fun s' => runList m p s' es

theorem runList_hist {m : Machine} {p : Params} :
    ∀ {es hs : List Event} {s s' : State}, Hist m p hs s → runList m p s es = some s' → Hist m p (hs ++ es) s'
  | [], _, _, _, h, hr => by simp only [runList, Option.some.injEq] at hr; subst hr; simpa using h
  | e :: es, hs, s, s', h, hr => by
    simp only [runList, Option.bind_eq_some_iff] at hr
    obtain ⟨s1, h1, h2⟩ := hr
    have := runList_hist (Hist.snoc e h h1) h2
    simpa using this

def p3 : Params := fun _ => 3
def rolesV (pmqa security : Bool) : Val := .set [pmqa, security]
def evReset : Event := ⟨"reset", []⟩
def evCreate : Event := ⟨"create", []⟩
def evAdd (member : Bool) (roles : Val) : Event := ⟨"addBatch", [boolV member, roles]⟩
def evClose (at_ : Nat) (current : Val) : Event := ⟨"close", [natV at_, current]⟩
def evRebind : Event := ⟨"rebind", []⟩
def evDrop (keep : Nat) (decision : Bool) : Event := ⟨"drop", [natV keep, boolV decision]⟩
def evReleaseDrop : Event := ⟨"releaseDrop", []⟩
def evReleaseRecovery : Event := ⟨"releaseRecovery", []⟩
def evDone : Event := ⟨"done", [boolV true]⟩

/-- The run accepts every event of the prefix and refuses the completion. -/
def refusesDone (es : List Event) : Bool :=
  match runList M p3 (initial M p3) es with
  | some s => (step M p3 s evDone).isNone
  | none => false

/-- The run accepts every event of the history, the completion included. -/
def completes (es : List Event) : Bool := (runList M p3 (initial M p3) (es ++ [evDone])).isSome

/-- reset → planNA → close → releaseRecovery → appendRequired → dropReleased:
the drop remembers PMQA on the no-plan record, so the completion is refused. -/
def counterDropReleased : List Event :=
  [evReset, evCreate, evAdd true (rolesV false false), evClose 0 (rolesV false false), evReleaseRecovery,
    evAdd true (rolesV true false), evDrop 1 true]

/-- The same with a drop and a later release of the drop obligation. -/
def counterReleaseDrop : List Event :=
  [evReset, evCreate, evAdd true (rolesV false false), evClose 0 (rolesV false false), evReleaseRecovery,
    evAdd true (rolesV true false), evDrop 1 false, evReleaseDrop]

theorem counter_dropReleased : refusesDone counterDropReleased = true := by decide
theorem counter_releaseDrop : refusesDone counterReleaseDrop = true := by decide

/-- Two members of one plan, each with its own kernel: the first is in the
first batch, the second in the second; both complete. -/
def witnessMemberA : List Event :=
  [evCreate, evAdd true (rolesV true false), evAdd false (rolesV true false), evClose 0 (rolesV true false),
    evClose 1 (rolesV false false)]
def witnessMemberB : List Event :=
  [evCreate, evAdd false (rolesV true false), evAdd true (rolesV true false), evClose 0 (rolesV false false),
    evClose 1 (rolesV true false)]

/-- A member reset twice after a required review pays the renewed debt with a
review of the new cycle and completes. -/
def witnessTwoResets : List Event :=
  [evCreate, evAdd true (rolesV true false), evClose 0 (rolesV true false), evReset, evReset, evRebind,
    evAdd true (rolesV true false), evClose 1 (rolesV true false)]

/-- A released no-plan recovery acquires a role, takes its review, and completes:
reset → planNA → close → releaseRecovery → appendRequired → close → done. -/
def witnessReleasedRole : List Event :=
  [evReset, evCreate, evAdd true (rolesV false false), evClose 0 (rolesV false false), evReleaseRecovery,
    evAdd true (rolesV true false), evClose 1 (rolesV true false)]

theorem witness_members : completes witnessMemberA = true ∧ completes witnessMemberB = true := by decide
theorem witness_twoResets : completes witnessTwoResets = true := by decide
theorem witness_releasedRole : completes witnessReleasedRole = true := by decide

/-- A completing history is a history of the machine: its states are reachable,
and debt safety applies to it. -/
theorem completes_reachable {es : List Event} (h : completes es = true) :
    ∃ s, Hist M p3 (es ++ [evDone]) s := by
  simp only [completes, Option.isSome_iff_exists] at h
  obtain ⟨s, hs⟩ := h
  exact ⟨s, by simpa using runList_hist Hist.nil hs⟩

end KanderFormal.Proofs.ReviewObligation
