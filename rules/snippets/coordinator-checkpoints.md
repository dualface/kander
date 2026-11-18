# Coordinator Checkpoints

Source: moved from `KANDER-KANBAN-RULES.md` "Coordinator Checkpoints".

## Coordinator Checkpoints

```text
kander coordinator show <group-id>
kander coordinator claim <claim.json>
kander coordinator reconcile <observations.json>
kander coordinator recover <request.json>
kander coordinator remove-member <request.json>
```

These single-binary producers store group checkpoints through the existing transaction protocol. They read task, dispatch, and review originals. `show`, `claim`, and `reconcile` never notify, move cards, integrate Git, or grant execution authority; a `recover` reset moves one `review/` card to `working/` and does none of the others; group workflow policy loads only when the task_groups module is enabled. `show` returns the committed schema, revision, coordinator authority, and member facts. `claim` requires group_id, expected_revision, expected_epoch, owner, unique token, basis, and members. `reconcile` requires group_id, expected_revision, authority, and a members object keyed by task ID. Each member names revision and, when bound, dispatch_id, epoch, base, and its completed delivery_commit. An optional absolute cwd selects a surviving repository for read-only Git verification, required for first unbound deliveries. Authority contains owner, token, and epoch from the successful claim. Retry the same JSON after a lost response; changed inputs or stale authority conflict. A pending transaction is never repaired by show or reconcile; use explicit init recovery under its maintenance preconditions. A checkpoint is a recovery cursor, never acceptance or integration authorization; confirmation comes from same-round receipts, including in snapshots.

Reconcile walks each member's execution chain and dispatch rounds from the position in its checkpoint mirror (`transition_seq`, `dispatch_seq`), so missed reconciles leave no gap. It accepts any change a producer recorded and refuses one no record carries. A member whose current execution is pending or unresolved stays waiting: reconcile succeeds and stores no dispatch for it. A member in `archived/` or `trash/` is checked only for its result and its chain. A `TASK_GROUP` change is refused while the source or target group has a checkpoint; the one way out for an unstarted member is `remove-member` below. A checkpoint of schema 1 finds its position from its facts on the first reconcile and upgrades when that reconcile publishes.

Reconcile judges each member alone. A member that fails stays at its last mirror with `blocked.reason`, and the others advance. A later reconcile that accepts the member clears the field. `result` and the exit status say `complete` (0), `partial` (3), or `failed` (1): see `snippets/commands/coordinator.md`. `kander subscribe` reports the set in `blocked`, with a `member-blocked` event when it changes. A stale card revision fails the whole request instead: read fresh revisions and retry. Reconcile a blocked member again before anything else, because most blocks end there. While a member is blocked, no wrap-up dispatch of the group starts.

A wrap-up that completed is verified against Git once, when its `move done` runs, and that result is immutable evidence. Reconcile then checks only the structure of the evidence. A wrap-up without evidence, from an older binary, is verified by the next reconcile; a failure blocks the member until a confirmation. To re-check a completed delivery by hand, use `kander check delivery`.

`kander coordinator recover` is the last resort. Never run it on your own judgment or to get past a failure: report the blocked member, its reason, and the mode to the user, and run it only with the user's decision reference. See `snippets/failure-recovery.md` "Blocked Group Members".

`kander coordinator remove-member` takes one unstarted member out of a group that has a checkpoint, so the others go on without waiting for it. Its request names group_id, member, expected_revision, expected_epoch, decision, and reason, all required. Run it only with the user's decision reference. The member must be in `backlog/` or `todo/` with no start, claim, dispatch, or review of any kind, at least one other member must stay, and no remaining member may list it in `PREREQUISITES`. The card becomes a standalone card (`TASK_GROUP` empty, with the decision recorded in its `CONTRACT_DECISIONS`); the checkpoint revision rises by one and the epoch stays. Read the new member set with `show` and keep reconciling with it; a new claim names only the remaining members. A started member and a new member of a checkpointed group are not supported.
