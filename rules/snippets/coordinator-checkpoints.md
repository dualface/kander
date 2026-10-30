# Coordinator Checkpoints

Source: moved from `KANDER-KANBAN-RULES.md` "Coordinator Checkpoints".

## Coordinator Checkpoints

```text
kander coordinator show <group-id>
kander coordinator claim <claim.json>
kander coordinator reconcile <observations.json>
```

These single-binary producers store group checkpoints through the existing transaction protocol. They read task, dispatch, and review originals and never notify, move cards, integrate Git, or grant execution authority; group workflow policy loads only when the task_groups module is enabled. `show` returns the committed schema, revision, coordinator authority, and member facts. `claim` requires group_id, expected_revision, expected_epoch, owner, unique token, basis, and members. `reconcile` requires group_id, expected_revision, authority, and a members object keyed by task ID. Each member names revision and, when bound, dispatch_id, epoch, base, and its completed delivery_commit. An optional absolute cwd selects a surviving repository for read-only Git verification, required for first unbound deliveries. Authority contains owner, token, and epoch from the successful claim. Retry the same JSON after a lost response; changed inputs or stale authority conflict. A pending transaction is never repaired by show or reconcile; use explicit init recovery under its maintenance preconditions. A checkpoint is a recovery cursor, never acceptance or integration authorization; confirmation comes from same-round receipts, including in snapshots.

Reconcile walks each member's execution chain and dispatch rounds from the position in its checkpoint mirror (`transition_seq`, `dispatch_seq`), so missed reconciles leave no gap. It accepts any change a producer recorded and refuses one no record carries. A member whose current execution is pending or unresolved stays waiting: reconcile succeeds and adopts no dispatch for it. A member in `archived/` or `trash/` is checked only for its result and its chain. A `TASK_GROUP` change is refused while the source or target group has a checkpoint. A checkpoint of schema 1 finds its position from its facts on the first reconcile and upgrades when that reconcile publishes.
