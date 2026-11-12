---- MODULE Oracle ----
EXTENDS Naturals, FiniteSets

\* Handwritten history observer. It never reads the generated obligation fields
\* or the generated done predicate. roleSeen survives discharge and release.
ObserverInit == [debts |-> {}, roleSeen |-> FALSE,
                 dropAfter |-> 0, resetAfter |-> 0, cycleAfter |-> 0,
                 unplannedRecord |-> FALSE, reviewedCycle |-> 0]

Observe(event, s, o) ==
    CASE event \in {"planRequired", "appendRequired"} ->
            [o EXCEPT !.roleSeen = TRUE,
                !.debts = o.debts \cup
                    (IF o.unplannedRecord /\ o.reviewedCycle # s.cycle
                     THEN {"unplanned"} ELSE {})]
    [] event = "reset" ->
            [o EXCEPT
                !.debts = (o.debts \ (IF s.plan = 1 /\ o.roleSeen
                                      THEN {"unplanned"} ELSE {})) \cup
                    (IF s.plan = 0 THEN {"unplanned"} ELSE {"cycle"}) \cup
                    (IF s.plan = 1 /\ o.roleSeen THEN {"reset"} ELSE {}),
                !.unplannedRecord = IF s.plan = 0 THEN TRUE
                                    ELSE IF o.roleSeen THEN FALSE ELSE o.unplannedRecord,
                !.resetAfter = IF s.plan = 1 /\ o.roleSeen THEN s.batch ELSE o.resetAfter,
                !.cycleAfter = IF s.plan = 1 THEN s.batch ELSE o.cycleAfter]
    [] event = "drop" /\ s.batchRequired = 1 ->
            [o EXCEPT !.debts = o.debts \cup {"drop"}, !.dropAfter = s.batch]
    [] event = "close" ->
            [o EXCEPT
                !.reviewedCycle = IF s.batchRequired = 1 THEN s.batchCycle ELSE o.reviewedCycle,
                !.debts = o.debts \ {
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
