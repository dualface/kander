---- MODULE Model ----
EXTENDS Naturals, FiniteSets, TLC, Oracle
CONSTANTS MemberCount, MaxCycle, MaxBatch, Mutation
G == INSTANCE Generated
Members == 1..MemberCount
VARIABLES state, observer, last, frameOK
vars == <<state, observer, last, frameOK>>

Init == /\ state = [m \in Members |-> G!Initial]
        /\ observer = [m \in Members |-> ObserverInit]
        /\ last = [member |-> 0, event |-> "init"]
        /\ frameOK = TRUE

Step(m, event) ==
    /\ G!Enabled(event, state[m])
    /\ state' = [state EXCEPT ![m] = G!Apply(event, state[m])]
    /\ observer' = [observer EXCEPT ![m] = Observe(event, state[m], observer[m])]
    /\ last' = [member |-> m, event |-> event]
    /\ frameOK' = frameOK /\
         (\A other \in Members \ {m} :
              state'[other] = state[other] /\ observer'[other] = observer[other])

Next == \E m \in Members, event \in G!Events : Step(m, event)
Spec == Init /\ [][Next]_vars

DebtSafety == \A m \in Members : state[m].done = 1 => observer[m].debts = {}
BoundSafety == \A m \in Members : state[m].done = 1 => state[m].bound = state[m].cycle
MemberIsolation == frameOK
TypeOK == \A m \in Members :
             /\ state[m].cycle \in 1..MaxCycle
             /\ state[m].bound \in 1..MaxCycle
             /\ state[m].batch \in 0..MaxBatch
             /\ \A f \in DOMAIN state[m] : state[m][f] \in 0..3

\* Deliberately false invariants used only to obtain positive reachability traces.
NoCompletion == ~ (\A m \in Members : state[m].done = 1)
NoReviewedRecoveryCompletion == ~ (\A m \in Members :
    state[m].done = 1 /\ state[m].cycle = MaxCycle /\ state[m].recoverOwed = 1)
====
