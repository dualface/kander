import KanderFormal.Proofs.IssueResultSteps

/-
The events an accepted step of the issue result machine ran, as one inductive
type, so that a proof can case on the event and get its arguments by name.
-/
namespace KanderFormal.Proofs.IssueResult

open KanderFormal KanderFormal.Machines.IssueResult KanderFormal.Proofs.Scalar

theorem ite_some_enc {c : Prop} [Decidable c] {z : Rq} {s' : State}
    (h : (if c then some (enc z) else none) = some s') : c ∧ s' = enc z := by
  split at h
  · cases h; exact ⟨‹_›, rfl⟩
  · cases h

theorem ite_none {c : Prop} [Decidable c] {α : Type} {v : α} (h : ¬c) : (if c then some v else none) = none := by
  simp [h]

/-- An event of the machine with its arguments: statuses are the indices of
`rstat`, scan classes the indices of `scan`. -/
inductive Ev where
  | read (remoteOk same : Bool) (nw : Nat)
  | reconcile (found : Bool) (ncid : Nat)
  | reassess
  | intent (nfull nmet dup bodyOk : Bool) (nbod nchk : Nat) (nsound : Bool)
  | equivalent (nfull nmet found : Bool) (nbod ncid nchk : Nat) (nsound : Bool)
  | publish (ok : Bool) (ncid : Nat)
  | reject (nfh : Nat)
  | decide (digestOk closed : Bool) (scan : Nat) (fieldsOk : Bool)
  | keep (digestOk closed : Bool) (scan : Nat)
  | failClose (nfh : Nat)
  | otherIntent
  | otherSettle
  | update (present : Bool) (newSt : Nat) (identSame : Bool) (nfNew : Nat) (prefixF : Bool) (ndNew : Nat)
  | work (newSt : Nat) (identical checksSame workSame versionOk : Bool)
  | idKeep (same : Bool)
  | valid (vst : Nat) (actorPos commentPos outOk nfull nmet : Bool)
  | issueNeeded (hasRecords idPos : Bool)

def Ev.toEvent : Ev → Event
  | .read ro sm nw => ⟨"read", [boolV ro, boolV sm, natV nw]⟩
  | .reconcile fo nc => ⟨"reconcile", [boolV fo, natV nc]⟩
  | .reassess => ⟨"reassess", []⟩
  | .intent nf nm dp bo nb nk ns => ⟨"intent", [boolV nf, boolV nm, boolV dp, boolV bo, natV nb, natV nk, boolV ns]⟩
  | .equivalent nf nm fo nb nc nk ns =>
    ⟨"equivalent", [boolV nf, boolV nm, boolV fo, natV nb, natV nc, natV nk, boolV ns]⟩
  | .publish ok nc => ⟨"publish", [boolV ok, natV nc]⟩
  | .reject nh => ⟨"reject", [natV nh]⟩
  | .decide dg cl sc fk => ⟨"decide", [boolV dg, boolV cl, enumV sc, boolV fk]⟩
  | .keep dg cl sc => ⟨"keep", [boolV dg, boolV cl, enumV sc]⟩
  | .failClose nh => ⟨"failClose", [natV nh]⟩
  | .otherIntent => ⟨"otherIntent", []⟩
  | .otherSettle => ⟨"otherSettle", []⟩
  | .update pr ns is nfn pf ndn => ⟨"update", [boolV pr, enumV ns, boolV is, natV nfn, boolV pf, natV ndn]⟩
  | .work ns id cs ws vo => ⟨"work", [enumV ns, boolV id, boolV cs, boolV ws, boolV vo]⟩
  | .idKeep sm => ⟨"idKeep", [boolV sm]⟩
  | .valid vs ap cp oo nf nm => ⟨"valid", [enumV vs, boolV ap, boolV cp, boolV oo, boolV nf, boolV nm]⟩
  | .issueNeeded hr ip => ⟨"issueNeeded", [boolV hr, boolV ip]⟩

/-- The events a client entrance (`InspectResult`, `ApplyResult`,
`DecideResult`, and `readResult` in front of them) runs. The other events are
environment events of other records and the checks of the update relations. -/
def Ev.client : Ev → Bool
  | .read .. | .reconcile .. | .reassess | .intent .. | .equivalent .. | .publish .. | .reject .. | .decide ..
  | .keep .. | .failClose .. => true
  | _ => false

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = readEvent ∨ ev = reconcileEvent ∨ ev = reassessEvent ∨ ev = intentEvent ∨ ev = equivalentEvent ∨
      ev = publishEvent ∨ ev = rejectEvent ∨ ev = decideEvent ∨ ev = keepEvent ∨ ev = failCloseEvent ∨
      ev = otherIntentEvent ∨ ev = otherSettleEvent ∨ ev = updateEvent ∨ ev = workEvent ∨ ev = idKeepEvent ∨
      ev = validEvent ∨ ev = issueNeededEvent := by
  simpa [issueResultMachine] using h

theorem bool_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArg n :: ds) vs = some args) :
    ∃ b vs' args', vs = boolV b :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨b, rfl⟩ := conforms_bool c
  exact ⟨b, vs', a, rfl, h'⟩

theorem num_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (numArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem id_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (idArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem rstat_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (rstatArg n :: ds) vs = some args) :
    ∃ i vs' args', vs = enumV i :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨i, rfl, -⟩ := conforms_enum (rfl : M.enumSize "rstat" = some 6) c
  exact ⟨i, vs', a, rfl, h'⟩

theorem scan_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (scanArg n :: ds) vs = some args) :
    ∃ i vs' args', vs = enumV i :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨i, rfl, -⟩ := conforms_enum (rfl : M.enumSize "scan" = some 5) c
  exact ⟨i, vs', a, rfl, h'⟩

/-- Every accepted event is one of the seventeen, with arguments of its
declared types. -/
theorem accepted_ev {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    ∃ ev : Ev, e = ev.toEvent := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl |
    rfl | rfl | rfl
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := bool_cons h1
    obtain ⟨a3, vs3, r3, rfl, h3⟩ := id_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    exact ⟨.read a1 a2 a3, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := id_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact ⟨.reconcile a1 a2, rfl⟩
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact ⟨.reassess, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := bool_cons h1
    obtain ⟨a3, vs3, r3, rfl, h3⟩ := bool_cons h2
    obtain ⟨a4, vs4, r4, rfl, h4⟩ := bool_cons h3
    obtain ⟨a5, vs5, r5, rfl, h5⟩ := id_cons h4
    obtain ⟨a6, vs6, r6, rfl, h6⟩ := id_cons h5
    obtain ⟨a7, vs7, r7, rfl, h7⟩ := bool_cons h6
    obtain ⟨rfl, -⟩ := bindArgs_nil h7
    exact ⟨.intent a1 a2 a3 a4 a5 a6 a7, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := bool_cons h1
    obtain ⟨a3, vs3, r3, rfl, h3⟩ := bool_cons h2
    obtain ⟨a4, vs4, r4, rfl, h4⟩ := id_cons h3
    obtain ⟨a5, vs5, r5, rfl, h5⟩ := id_cons h4
    obtain ⟨a6, vs6, r6, rfl, h6⟩ := id_cons h5
    obtain ⟨a7, vs7, r7, rfl, h7⟩ := bool_cons h6
    obtain ⟨rfl, -⟩ := bindArgs_nil h7
    exact ⟨.equivalent a1 a2 a3 a4 a5 a6 a7, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := id_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact ⟨.publish a1 a2, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := id_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact ⟨.reject a1, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := bool_cons h1
    obtain ⟨a3, vs3, r3, rfl, h3⟩ := scan_cons h2
    obtain ⟨a4, vs4, r4, rfl, h4⟩ := bool_cons h3
    obtain ⟨rfl, -⟩ := bindArgs_nil h4
    exact ⟨.decide a1 a2 a3 a4, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := bool_cons h1
    obtain ⟨a3, vs3, r3, rfl, h3⟩ := scan_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    exact ⟨.keep a1 a2 a3, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := id_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact ⟨.failClose a1, rfl⟩
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact ⟨.otherIntent, rfl⟩
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact ⟨.otherSettle, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := rstat_cons h1
    obtain ⟨a3, vs3, r3, rfl, h3⟩ := bool_cons h2
    obtain ⟨a4, vs4, r4, rfl, h4⟩ := num_cons h3
    obtain ⟨a5, vs5, r5, rfl, h5⟩ := bool_cons h4
    obtain ⟨a6, vs6, r6, rfl, h6⟩ := num_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    exact ⟨.update a1 a2 a3 a4 a5 a6, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := rstat_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := bool_cons h1
    obtain ⟨a3, vs3, r3, rfl, h3⟩ := bool_cons h2
    obtain ⟨a4, vs4, r4, rfl, h4⟩ := bool_cons h3
    obtain ⟨a5, vs5, r5, rfl, h5⟩ := bool_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    exact ⟨.work a1 a2 a3 a4 a5, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact ⟨.idKeep a1, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := rstat_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := bool_cons h1
    obtain ⟨a3, vs3, r3, rfl, h3⟩ := bool_cons h2
    obtain ⟨a4, vs4, r4, rfl, h4⟩ := bool_cons h3
    obtain ⟨a5, vs5, r5, rfl, h5⟩ := bool_cons h4
    obtain ⟨a6, vs6, r6, rfl, h6⟩ := bool_cons h5
    obtain ⟨rfl, -⟩ := bindArgs_nil h6
    exact ⟨.valid a1 a2 a3 a4 a5 a6, rfl⟩
  · obtain ⟨a1, vs1, r1, rfl, h1⟩ := bool_cons hb
    obtain ⟨a2, vs2, r2, rfl, h2⟩ := bool_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact ⟨.issueNeeded a1 a2, rfl⟩

end KanderFormal.Proofs.IssueResult
