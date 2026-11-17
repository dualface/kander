import KanderFormal.Proofs.ServerIntegrationSteps

/-
The events an accepted step of the server integration machine ran, with the
types of their arguments: every theorem about all events starts from
`accepted_ev`.
-/
namespace KanderFormal.Proofs.ServerIntegration

open KanderFormal KanderFormal.Machines.ServerIntegration KanderFormal.Proofs.Scalar

/-- One event of the machine with typed arguments. -/
inductive Ev where
  | claim (eg er : Nat) (roomGen roomRev sameExec actor actorMatch : Bool) (ag pu : Nat) (contained : Bool)
  | claimGuard (ticket actor facts : Bool)
  | require (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat) (ae ac : Bool)
  | requireOwner (hdr : Bool) (g tg : Nat) (client : Bool)
  | renew (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat) (ae ac facts : Bool)
  | check (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat) (ae ac : Bool) (er : Nat) (room : Bool)
  | checkOwner (hdr : Bool) (g tg : Nat) (client : Bool) (er : Nat) (room : Bool)
  | pushing (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat) (ae ac : Bool) (er : Nat)
      (room obsMatch obsSha : Bool)
  | confirm (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat) (ae ac : Bool) (er : Nat)
      (room obsSha obsAt edgesOk : Bool)
  | release (hdr : Bool) (g tg : Nat) (client : Bool) (er : Nat) (room decision obsEmpty obsSha : Bool)
  | guard (hdr : Bool) (g tg : Nat) (client al : Bool) (ag : Nat) (ae ac ticket facts : Bool)
  | guardRelease (hdr : Bool) (g tg : Nat) (client ticket : Bool)
  | completion (closed planNA noDelivery member planIsCommit : Bool)
  | wrapUp (notRequired idGiven idMatch : Bool) (ig : Nat)
      (srcMatch tgtMatch cwdMatch member closed planIsSrc reviewIsSrc : Bool)
  | expire
  | startup

def Ev.toEvent : Ev → Event
  | .claim eg er rg rr se ac am ag pu ct =>
    ⟨"claim", [natV eg, natV er, boolV rg, boolV rr, boolV se, boolV ac, boolV am, natV ag, enumV pu, boolV ct]⟩
  | .claimGuard t ac f => ⟨"claimGuard", [boolV t, boolV ac, boolV f]⟩
  | .require hdr g tg c al ag ae ac =>
    ⟨"require", [boolV hdr, natV g, natV tg, boolV c, boolV al, natV ag, boolV ae, boolV ac]⟩
  | .requireOwner hdr g tg c => ⟨"requireOwner", [boolV hdr, natV g, natV tg, boolV c]⟩
  | .renew hdr g tg c al ag ae ac fa =>
    ⟨"renew", [boolV hdr, natV g, natV tg, boolV c, boolV al, natV ag, boolV ae, boolV ac, boolV fa]⟩
  | .check hdr g tg c al ag ae ac er rm =>
    ⟨"check", [boolV hdr, natV g, natV tg, boolV c, boolV al, natV ag, boolV ae, boolV ac, natV er, boolV rm]⟩
  | .checkOwner hdr g tg c er rm => ⟨"checkOwner", [boolV hdr, natV g, natV tg, boolV c, natV er, boolV rm]⟩
  | .pushing hdr g tg c al ag ae ac er rm om os =>
    ⟨"pushing", [boolV hdr, natV g, natV tg, boolV c, boolV al, natV ag, boolV ae, boolV ac, natV er, boolV rm,
      boolV om, boolV os]⟩
  | .confirm hdr g tg c al ag ae ac er rm os oa ee =>
    ⟨"confirm", [boolV hdr, natV g, natV tg, boolV c, boolV al, natV ag, boolV ae, boolV ac, natV er, boolV rm,
      boolV os, boolV oa, boolV ee]⟩
  | .release hdr g tg c er rm de oe os =>
    ⟨"release", [boolV hdr, natV g, natV tg, boolV c, natV er, boolV rm, boolV de, boolV oe, boolV os]⟩
  | .guard hdr g tg c al ag ae ac t fa =>
    ⟨"guard", [boolV hdr, natV g, natV tg, boolV c, boolV al, natV ag, boolV ae, boolV ac, boolV t, boolV fa]⟩
  | .guardRelease hdr g tg c t => ⟨"guardRelease", [boolV hdr, natV g, natV tg, boolV c, boolV t]⟩
  | .completion cl pn nd me pc => ⟨"completion", [boolV cl, boolV pn, boolV nd, boolV me, boolV pc]⟩
  | .wrapUp nr ig idm ig' sm tm cm me cl ps rs =>
    ⟨"wrapUp", [boolV nr, boolV ig, boolV idm, natV ig', boolV sm, boolV tm, boolV cm, boolV me, boolV cl,
      boolV ps, boolV rs]⟩
  | .expire => ⟨"expire", []⟩
  | .startup => ⟨"startup", []⟩

theorem events_cases {ev : EventDecl} (h : ev ∈ M.events) :
    ev = claimEvent ∨ ev = claimGuardEvent ∨ ev = requireEvent ∨ ev = requireOwnerEvent ∨ ev = renewEvent ∨
      ev = checkEvent ∨ ev = checkOwnerEvent ∨ ev = pushingEvent ∨ ev = confirmEvent ∨ ev = releaseEvent ∨ ev = guardEvent ∨
      ev = guardReleaseEvent ∨ ev = completionEvent ∨ ev = wrapUpEvent ∨ ev = expireEvent ∨
      ev = startupEvent := by
  simpa [serverIntegrationMachine] using h

theorem bool_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (boolArg n :: ds) vs = some args) :
    ∃ b vs' args', vs = boolV b :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨b, rfl⟩ := conforms_bool c
  exact ⟨b, vs', a, rfl, h'⟩

theorem gen_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (genArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem rev_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (revArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem auth_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (authArg n :: ds) vs = some args) :
    ∃ g vs' args', vs = natV g :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨g, rfl, -⟩ := conforms_nat c
  exact ⟨g, vs', a, rfl, h'⟩

theorem purpose_cons {p : Params} {n : String} {ds : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (purposeArg n :: ds) vs = some args) :
    ∃ i vs' args', vs = enumV i :: vs' ∧ bindArgs M p ds vs' = some args' := by
  obtain ⟨v, vs', a, rfl, c, h', -⟩ := bindArgs_cons h
  obtain ⟨i, rfl, -⟩ := conforms_enum size_purpose c
  exact ⟨i, vs', a, rfl, h'⟩

theorem owner_args {p : Params} {rest : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (ownerArgs ++ rest) vs = some args) :
    ∃ hdr g tg c vs' args', vs = boolV hdr :: natV g :: natV tg :: boolV c :: vs' ∧
      bindArgs M p rest vs' = some args' := by
  obtain ⟨hdr, vs1, a1, rfl, h1⟩ := bool_cons (n := "hdr") h
  obtain ⟨g, vs2, a2, rfl, h2⟩ := gen_cons (n := "g") h1
  obtain ⟨tg, vs3, a3, rfl, h3⟩ := gen_cons (n := "tg") h2
  obtain ⟨c, vs4, a4, rfl, h4⟩ := bool_cons (n := "client") h3
  exact ⟨hdr, g, tg, c, vs4, a4, rfl, h4⟩

theorem lease_args {p : Params} {rest : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (leaseArgs ++ rest) vs = some args) :
    ∃ hdr g tg c al ag ae ac vs' args', vs = boolV hdr :: natV g :: natV tg :: boolV c :: boolV al :: natV ag ::
      boolV ae :: boolV ac :: vs' ∧ bindArgs M p rest vs' = some args' := by
  have h' : bindArgs M p (ownerArgs ++ (actorArgs ++ rest)) vs = some args := by
    simpa [leaseArgs, List.append_assoc] using h
  obtain ⟨hdr, g, tg, c, vs0, a0, rfl, h0⟩ := owner_args h'
  obtain ⟨al, vs1, a1, rfl, h1⟩ := bool_cons (n := "actorStanding") h0
  obtain ⟨ag, vs2, a2, rfl, h2⟩ := auth_cons (n := "actorGen") h1
  obtain ⟨ae, vs3, a3, rfl, h3⟩ := bool_cons (n := "actorExec") h2
  obtain ⟨ac, vs4, a4, rfl, h4⟩ := bool_cons (n := "actorClient") h3
  exact ⟨hdr, g, tg, c, al, ag, ae, ac, vs4, a4, rfl, h4⟩

theorem action_args {p : Params} {rest : List ArgDecl} {vs : List Val} {args : List (String × Val)}
    (h : bindArgs M p (actionArgs ++ rest) vs = some args) :
    ∃ hdr g tg c al ag ae ac er rm vs' args', vs = boolV hdr :: natV g :: natV tg :: boolV c :: boolV al ::
      natV ag :: boolV ae :: boolV ac :: natV er :: boolV rm :: vs' ∧ bindArgs M p rest vs' = some args' := by
  have h' : bindArgs M p (leaseArgs ++ ([revArg "expRev", boolArg "room"] ++ rest)) vs = some args := by
    simpa [actionArgs, List.append_assoc] using h
  obtain ⟨hdr, g, tg, c, al, ag, ae, ac, vs0, a0, rfl, h0⟩ := lease_args h'
  obtain ⟨er, vs1, a1, rfl, h1⟩ := rev_cons (n := "expRev") h0
  obtain ⟨rm, vs2, a2, rfl, h2⟩ := bool_cons (n := "room") h1
  exact ⟨hdr, g, tg, c, al, ag, ae, ac, er, rm, vs2, a2, rfl, h2⟩

/-- Every accepted event is one of the sixteen, with arguments of its declared
types. -/
theorem accepted_ev {p : Params} {s s' : State} {e : Event} (h : step M p s e = some s') :
    ∃ v : Ev, e = v.toEvent := by
  obtain ⟨ev, args, hmem, hname, hb⟩ := accepted_event h
  obtain ⟨n, vs⟩ := e
  simp only at hname hb
  subst hname
  rcases events_cases hmem with rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl | rfl |
    rfl
  · obtain ⟨eg, vs1, a1, rfl, h1⟩ := gen_cons hb
    obtain ⟨er, vs2, a2, rfl, h2⟩ := rev_cons h1
    obtain ⟨rg, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨rr, vs4, a4, rfl, h4⟩ := bool_cons h3
    obtain ⟨se, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨ac, vs6, a6, rfl, h6⟩ := bool_cons h5
    obtain ⟨am, vs7, a7, rfl, h7⟩ := bool_cons h6
    obtain ⟨ag, vs8, a8, rfl, h8⟩ := auth_cons h7
    obtain ⟨pu, vs9, a9, rfl, h9⟩ := purpose_cons h8
    obtain ⟨ct, vs10, a10, rfl, h10⟩ := bool_cons h9
    obtain ⟨rfl, -⟩ := bindArgs_nil h10
    exact ⟨.claim eg er rg rr se ac am ag pu ct, rfl⟩
  · obtain ⟨t, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨ac, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨fa, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    exact ⟨.claimGuard t ac fa, rfl⟩
  · obtain ⟨hdr, g, tg, c, al, ag, ae, ac, vs0, a0, rfl, h0⟩ := lease_args (rest := []) (by simpa [requireEvent] using hb)
    obtain ⟨rfl, -⟩ := bindArgs_nil h0
    exact ⟨.require hdr g tg c al ag ae ac, rfl⟩
  · obtain ⟨hdr, g, tg, c, vs0, a0, rfl, h0⟩ := owner_args (rest := []) (by simpa [requireOwnerEvent] using hb)
    obtain ⟨rfl, -⟩ := bindArgs_nil h0
    exact ⟨.requireOwner hdr g tg c, rfl⟩
  · obtain ⟨hdr, g, tg, c, al, ag, ae, ac, vs0, a0, rfl, h0⟩ := lease_args (by simpa [renewEvent] using hb)
    obtain ⟨fa, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact ⟨.renew hdr g tg c al ag ae ac fa, rfl⟩
  · obtain ⟨hdr, g, tg, c, al, ag, ae, ac, er, rm, vs0, a0, rfl, h0⟩ :=
      action_args (rest := []) (by simpa [checkEvent] using hb)
    obtain ⟨rfl, -⟩ := bindArgs_nil h0
    exact ⟨.check hdr g tg c al ag ae ac er rm, rfl⟩
  · obtain ⟨hdr, g, tg, c, vs0, a0, rfl, h0⟩ := owner_args (by simpa [checkOwnerEvent] using hb)
    obtain ⟨er, vs1, a1, rfl, h1⟩ := rev_cons h0
    obtain ⟨rm, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact ⟨.checkOwner hdr g tg c er rm, rfl⟩
  · obtain ⟨hdr, g, tg, c, al, ag, ae, ac, er, rm, vs0, a0, rfl, h0⟩ := action_args (by simpa [pushingEvent] using hb)
    obtain ⟨om, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨os, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact ⟨.pushing hdr g tg c al ag ae ac er rm om os, rfl⟩
  · obtain ⟨hdr, g, tg, c, al, ag, ae, ac, er, rm, vs0, a0, rfl, h0⟩ := action_args (by simpa [confirmEvent] using hb)
    obtain ⟨os, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨oa, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨ee, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨rfl, -⟩ := bindArgs_nil h3
    exact ⟨.confirm hdr g tg c al ag ae ac er rm os oa ee, rfl⟩
  · obtain ⟨hdr, g, tg, c, vs0, a0, rfl, h0⟩ := owner_args (by simpa [releaseEvent] using hb)
    obtain ⟨er, vs1, a1, rfl, h1⟩ := rev_cons h0
    obtain ⟨rm, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨de, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨oe, vs4, a4, rfl, h4⟩ := bool_cons h3
    obtain ⟨os, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    exact ⟨.release hdr g tg c er rm de oe os, rfl⟩
  · obtain ⟨hdr, g, tg, c, al, ag, ae, ac, vs0, a0, rfl, h0⟩ := lease_args (by simpa [guardEvent] using hb)
    obtain ⟨t, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨fa, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨rfl, -⟩ := bindArgs_nil h2
    exact ⟨.guard hdr g tg c al ag ae ac t fa, rfl⟩
  · obtain ⟨hdr, g, tg, c, vs0, a0, rfl, h0⟩ := owner_args (by simpa [guardReleaseEvent] using hb)
    obtain ⟨t, vs1, a1, rfl, h1⟩ := bool_cons h0
    obtain ⟨rfl, -⟩ := bindArgs_nil h1
    exact ⟨.guardRelease hdr g tg c t, rfl⟩
  · obtain ⟨cl, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨pn, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨nd, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨me, vs4, a4, rfl, h4⟩ := bool_cons h3
    obtain ⟨pc, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨rfl, -⟩ := bindArgs_nil h5
    exact ⟨.completion cl pn nd me pc, rfl⟩
  · obtain ⟨nr, vs1, a1, rfl, h1⟩ := bool_cons hb
    obtain ⟨ig, vs2, a2, rfl, h2⟩ := bool_cons h1
    obtain ⟨idm, vs3, a3, rfl, h3⟩ := bool_cons h2
    obtain ⟨ig', vs4, a4, rfl, h4⟩ := gen_cons h3
    obtain ⟨sm, vs5, a5, rfl, h5⟩ := bool_cons h4
    obtain ⟨tm, vs6, a6, rfl, h6⟩ := bool_cons h5
    obtain ⟨cm, vs7, a7, rfl, h7⟩ := bool_cons h6
    obtain ⟨me, vs8, a8, rfl, h8⟩ := bool_cons h7
    obtain ⟨cl, vs9, a9, rfl, h9⟩ := bool_cons h8
    obtain ⟨ps, vs10, a10, rfl, h10⟩ := bool_cons h9
    obtain ⟨rs, vs11, a11, rfl, h11⟩ := bool_cons h10
    obtain ⟨rfl, -⟩ := bindArgs_nil h11
    exact ⟨.wrapUp nr ig idm ig' sm tm cm me cl ps rs, rfl⟩
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact ⟨.expire, rfl⟩
  · obtain ⟨rfl, -⟩ := bindArgs_nil hb
    exact ⟨.startup, rfl⟩

end KanderFormal.Proofs.ServerIntegration
