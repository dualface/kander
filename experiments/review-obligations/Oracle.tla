---- MODULE Oracle ----
EXTENDS Naturals, FiniteSets

\* Handwritten history observer. It never reads the generated obligation fields
\* or the generated done predicate. roleSeen survives discharge and release.
ObserverInit == [debts |-> {}, roleSeen |-> FALSE,
                 dropAfter |-> 0, resetAfter |-> 0, cycleAfter |-> 0]

Observe(event, s, o) ==
    CASE event \in {"planRequired", "appendRequired"} ->
            [o EXCEPT !.roleSeen = TRUE]
    [] event = "reset" ->
            [o EXCEPT
                !.debts = o.debts \cup
                    (IF s.plan = 0 THEN {"unplanned"} ELSE {"cycle"}) \cup
                    (IF s.plan = 1 /\ o.roleSeen THEN {"reset"} ELSE {}),
                !.resetAfter = IF s.plan = 1 /\ o.roleSeen THEN s.batch ELSE o.resetAfter,
                !.cycleAfter = IF s.plan = 1 THEN s.batch ELSE o.cycleAfter]
    [] event = "drop" /\ s.batchRequired = 1 ->
            [o EXCEPT !.debts = o.debts \cup {"drop"}, !.dropAfter = s.batch]
    [] event = "close" ->
            [o EXCEPT !.debts = o.debts \ {
                kind \in o.debts :
                    (kind = "cycle" /\ s.batch > o.cycleAfter /\ s.batchCycle = s.cycle)
                 \/ (s.batchRequired = 1 /\
                       ((kind = "drop" /\ s.batch > o.dropAfter)
                     \/ (kind = "reset" /\ s.batch > o.resetAfter /\ s.batchCycle = s.cycle)
                     \/ (kind = "unplanned" /\ s.batchCycle = s.cycle))) }]
    [] event = "releaseDrop" -> [o EXCEPT !.debts = o.debts \ {"drop"}]
    [] event = "releaseRecovery" /\ ~o.roleSeen ->
            [o EXCEPT !.debts = o.debts \ {"unplanned"}]
    [] OTHER -> o
====
