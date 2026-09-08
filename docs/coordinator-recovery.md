# Orchestration Checkpoints and Recovery

`kander coordinator` is a single-shot controlled read/write entry point. It stores the facts needed for orchestration recovery; it neither runs the subscription loop, nor sends notifications, moves cards, changes Git refs, or deletes worktrees.

## Storage, Locks, and Authorization

The current record lives at `kanban/.kander/groups/<group-id>/checkpoint.json`. Each actual update simultaneously publishes a non-overwritable `checkpoints/<revision>.json` historical version. Both use board's same redo transaction, and readers are not exposed to the intermediate results of the preparation phase; after a crash, only an explicit `init` completes the transaction. The content digest detects accidental corruption; it is not a security boundary against malicious tampering by the same user.

The lock order is board, sorted group_id, sorted task_id, then the short-lived journal lock. claim/reconcile confirms the complete member set inside the board's exclusive lock, avoiding topology changes between discovering members and writing the checkpoint; the review and dispatch control-group locks are declared together with the member locks. The checkpoint revision is independent of task revisions, does not increment because of reads, and does not modify task revisions.

The coordinator authority contains `owner`, `token`, and `epoch`. A new session uses a unique token and the existing orchestration authorization basis, and CASes the current checkpoint revision/epoch; only one contender succeeds. An old epoch cannot write the checkpoint even if it holds the latest task revision. Retrying with identical claim JSON does not increment the epoch. This authority covers only the checkpoint; it is not a task takeover, integration, notification, or wrap-up-on-behalf authorization.

## Invocation

```text
kander coordinator show 20260908-example-group
kander coordinator claim /absolute/claim.json
kander coordinator reconcile /absolute/observations.json
```

First-claim example:

```json
{
  "group_id": "20260908-example-group",
  "expected_revision": 0,
  "expected_epoch": 0,
  "owner": "coordinator-session",
  "token": "unique-session-token",
  "basis": "the user authorized orchestrating this group; the corresponding decision is recorded",
  "members": ["20260908-example-task"]
}
```

A recovering session runs show first, fills in the actual revision/epoch, and switches to a new token. Do not preempt another active coordinator by claiming repeatedly. The member set is fixed; unknown, missing, or newly added in-group members are all explicitly refused, and the actual contract change must be handled first. New members of external dependency groups continue to be handled by subscription's dynamic-expansion protocol and do not masquerade as members of this group.

reconcile uses the authority returned by show; the member keys must be complete, and each revision comes from the latest `show --json`. The SHAs, revisions, and IDs below are illustrative only and cannot serve as real evidence:

```json
{
  "group_id": "20260908-example-group",
  "expected_revision": 1,
  "authority": {
    "owner": "coordinator-session",
    "token": "unique-session-token",
    "epoch": 1
  },
  "members": {
    "20260908-example-task": {
      "revision": 12,
      "dispatch_id": "fix-round-one",
      "epoch": 1,
      "base": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "delivery_commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    }
  }
}
```

An unfinished dispatch does not pass delivery_commit; on completion it must match the atomic completed receipt of the same ID and same epoch. Input cannot be inferred as passing from card state, event times, or similar-looking SHAs. A first delivery without a bound dispatch may provide delivery_commit on its own, but the card must be in review, the card must already record the task branch, and an absolute `cwd` must be provided to check the actual local task-branch HEAD; this only records the delivery and does not prove the group branch has received it.

## Reconciliation over the Same Facts

The initial snapshot, task-update, state-change, heartbeat/attention, and subscription restarts all use the same entry point. Events only trigger re-reads; the subscription process's sequence is not persisted, and seeing a review-working edge is not required.

Each member stores the observed revision, execution cycle, card-state facts, delivery SHA, dispatch ID/epoch/base/revision and pending confirmation/delivery/wrap-up, plus relative intent/integration/dedicated-authorization references. The review part stores the plan ID, batch ID, relative run references, and the existing validators' structural progress. A failed review references output.raw, without fabricating a report.md. The checkpoint does not copy review bodies, does not write dispositions on authors' behalf, and does not produce a semantic PASS.

The complete member set may include backlog/todo cards not yet started; an empty STARTED_AT corresponds to `awaiting_start: true`. When the launcher publishes working and OWNER/STARTED_AT, it only establishes a start attempt; the checkpoint keeps the cycle unbound along with the `start_attempt` ID until the controlled success artifact exists. Claiming again, and snapshots that missed working or the rollback, all use the same evidence path; result confirmation may happen at the same task revision, but is still subject to the checkpoint CAS and to artifact and task-fact validation. Old cards without a start-attempt artifact, and explicit move --owner, continue to be verified against the existing durable metadata; an existing non-empty cycle cannot be arbitrarily replaced. Old schema 1's backlog/todo empty cycles without the awaiting marker, and those without review/dispatch/delivery cursors, can still be upgraded under control; old history is not rewritten.

Start artifacts are published by board's existing transactions to `.kander/groups/00000000-start-group/<task-id>/`: `current.json` points to the latest attempt, `<attempt-id>/pending.json` stores the immutable attempt, and `result.json` stores the immutable `succeeded` or `rolled-back` result. The start metadata shares a transaction with pending; a legitimate launcher rollback shares a transaction with rolled-back. Success is recorded by launch calling `board.ConfirmTaskStart` after the launch: it writes only the result artifact, does not change the task revision/body, and allows the executor to have already written new records or entered review. The start-attempt ID and revision distinguish retries within the same minute; STARTED_AT alone must not be the only comparison.

The coordinator follows the board/group/task lock order and consumes these artifacts; each step of a retry chain must have the previous attempt's rolled-back result. When only metadata is seen, the success result is missing, or the exit is uncertain, remain awaiting start; do not infer delivery or authorize another executor. A controlled rollback may retract that attempt's provisional observations; after success is confirmed, rollback and arbitrary cycle replacement are refused. Confirmed facts and new task records are not erased because of a failed old launcher. When the artifacts, the current pointer, or the chain are missing/corrupted, stop explicitly; deleting the pointer while keeping the artifacts cannot downgrade the card to an old card. When the success-result write is interrupted, recover along the existing transaction; if the result has not yet been published, remain unknown — success cannot be fabricated from external window state.

An accepted or completed artifact clears the same round's pending confirmation; only completed with a matching delivery clears the same round's pending delivery/wrap-up. Recovery works when the executor completes before notify returns, when a fast round trip happens within one scan interval, or when the subscription/orchestration side restarts and sees only the final snapshot. Repeating the same observation re-verifies the artifacts and the actual Git, keeping the checkpoint revision and transaction count unchanged. A stale revision, old epoch, wrong round, or wrong delivery keeps the original record and reports an error.

The confirmation deadline comes from the dispatch artifact and cannot be extended because of other events or restarts. heartbeat means the subscription is alive; alive means the session exists; only the task revision and the dispatch receipt provide durable progress. No output does not trigger a restart, alive does not prove progress, and a timeout does not prove exit. The entry point does not automatically send to or recover executors.

## Wrap-up Evidence

board reuses the [Review disposition protocol](review-disposition.md)'s artifact, full-publication, role-requirement, author-disposition, batch-predecessor, mechanical-fix, batch-closure, and execution-cycle validation, then consumes the dispatch's integration binding. launch reuses the integration Git validation and calls review's `VerifyClosedReviewGit` to re-check the actual ancestry relationships and mechanical-change digests in the closed batch. The dependency is one-way: launch → review → board; board does not import review, launch, or notify.

A historical closed batch does not require the current HEAD to still sit on that batch's commits. A legitimate rebase is still validated by the existing patch-correspondence proof; undeclared rewrites and a mismatched final delivery fail. When the original evidence CWD no longer exists after cleanup, an absolute `cwd` pointing to a still-existing repository worktree may be passed to re-prove the same set of commits in the artifacts; the original paths and SHAs are not rewritten.

A completed fix can still be reconciled read-only after the batch target advances and the incremental re-review batch closes, including the recovery window where the wrap-up has not yet been created. The historical path checks the successful run, full publication, predecessor lineage, assignment, and author artifacts; it does not apply the "closed batches are immutable" write gate. Creating or sending a fix still requires an open batch and the current round; a historical completion receipt does not restore send permission. Repeated reconciliation re-verifies all artifacts; when they are missing or corrupted, the existing checkpoint is kept and an error is reported.

Members with no findings only consume published artifacts; no dispatch is made just to copy reports. When some of multiple cards are archived, relative attachments are located by task ID under the actual card state; only a same-round completed together with `done` or `archived/RESULT: completed` finishes that round's wrap-up. Completed cards are not rolled back, and integration is not redone.

Wrap-up on behalf can only consume the dispatch's dedicated wrap-up-only authorization. Reconciliation itself does not grant it. Missing SESSION, unknown delivery, still active, non-zero return, and confirmation or lease timeouts do not individually prove exit; even a confirmed exit must have the old execution epoch isolated by the original dedicated producer. The original author records remain unchanged.

## Failure and Platform Boundaries

The CLI uses a 30-second context, bounding the preparation phase's lock contention, member loop, and Git verification. Underlying OS opens/reads and the publish after the redo is committed still follow the existing transaction boundaries; the entire command cannot claim a hard wall-clock upper bound. Cancellation leaves no background workers waiting on locks; once publishing has begun, recoverable artifacts are kept, with no mid-way rollback guessing.

After EOF/output errors, re-reconcile the current facts once; when the facts are valid, rebuild the subscription only once. Only an explicit `board.transaction_pending` warrants, under the maintenance premise, one explicit init followed by another read; if it still fails, report. duplicate, reparse, real corruption, or unknown products immediately preserve evidence; they must not be deleted, renamed, or retried indefinitely. Lock-deadline exhaustion is a temporary inability to observe, not a task failure.

Tests separately cover board structure/transactions, real local Git, isolated child-process kills, and fake terminals/Agents. Native Windows and real tmux/herdr/Agent sessions require separate on-machine testing; cross builds and the fake CLI do not substitute for real-machine passes. For the original reproductions and regression mapping, see the [Reproduction acceptance mapping](recovery-regressions.md).
