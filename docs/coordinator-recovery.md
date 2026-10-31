# Orchestration Checkpoints and Recovery

`kander coordinator` is a single-shot controlled read/write entry point. It stores the facts needed for orchestration recovery; it neither runs the subscription loop, nor sends notifications, moves cards, changes Git refs, or deletes worktrees.

## Storage, Locks, and Authorization

The current record lives at `kanban/.kander/groups/<group-id>/checkpoint.json`. Each actual update simultaneously publishes a non-overwritable `checkpoints/<revision>.json` historical version. Both use board's same redo transaction, and readers are not exposed to the intermediate results of the preparation phase; after a crash, only an explicit `init` completes the transaction. The content digest detects accidental corruption; it is not a security boundary against malicious tampering by the same user.

The lock order is board, sorted group_id, sorted task_id, then the short-lived journal lock. claim/reconcile confirms the complete member set inside the board's exclusive lock, avoiding topology changes between discovering members and writing the checkpoint; the review and dispatch control-group locks are declared together with the member locks. The checkpoint revision is independent of task revisions, does not increment because of reads, and does not modify task revisions.

reconcile never runs Git while it holds a board, group, task, or journal lock, so a subscription read is not blocked by Git subprocesses. It runs in three steps:

1. Read the facts. Under shared locks over the same board, group, and task scope, it runs the complete member reconciliation and collects the Git facts each member needs (first-delivery branch heads, the integration of an open wrap-up or of a completed wrap-up with no evidence, and closed review batches). It writes nothing. A failure of the request itself stops here.
2. Verify Git. With every lock released, it verifies the collected facts in member order and keeps each verdict. A cancelled context stops the command.
3. Re-check and commit. Under the exclusive scope it runs the same reconciliation again, gives each member the verdict of step 2, then publishes the checkpoint and any new wrap-up evidence.

When the board changed between steps 1 and 3, step 3 fails and keeps the checkpoint: a changed member revision returns `board.transaction_conflict`; a checkpoint advanced by a different request returns `board.coordinator_conflict` ("checkpoint CAS"); a Git fact that differs from the verified one behind an unchanged member revision returns `board.coordinator_conflict`. When the same request was committed in between, step 3 follows the replay path and returns that committed checkpoint. Git refs are not protected by board locks, before or after this change.

The coordinator authority contains `owner`, `token`, and `epoch`. A new session uses a unique token and the existing orchestration authorization basis, and CASes the current checkpoint revision/epoch; only one contender succeeds. An old epoch cannot write the checkpoint even if it holds the latest task revision. Retrying with identical claim JSON does not increment the epoch. This authority covers only the checkpoint; it is not a task takeover, integration, notification, or wrap-up-on-behalf authorization.

## Invocation

```text
kander coordinator show 20260908-example-group
kander coordinator claim /absolute/claim.json
kander coordinator reconcile /absolute/observations.json
kander coordinator recover /absolute/request.json
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

The coordinator follows the board/group/task lock order and consumes these artifacts; each step of a retry chain must have the previous attempt's rolled-back result. When only metadata is seen, the success result is missing, or the exit is uncertain, remain awaiting start; do not infer delivery or authorize another executor. A controlled rollback may retract that attempt's provisional observations; after success is confirmed, rollback and arbitrary cycle replacement are refused. An explicit released-binding reclaim is the evidence-backed exception described below. Confirmed facts and new task records are not erased because of a failed old launcher. When the artifacts, the current pointer, or the chain are missing/corrupted, stop explicitly; deleting the pointer while keeping the artifacts cannot downgrade the card to an old card. When the success-result write is interrupted, recover along the existing transaction; if the result has not yet been published, remain unknown — success cannot be fabricated from external window state.

An accepted or completed artifact clears the same round's pending confirmation; only completed with a matching delivery clears the same round's pending delivery/wrap-up. Recovery works when the executor completes before notify returns, when a fast round trip happens within one scan interval, or when the subscription/orchestration side restarts and sees only the final snapshot. Repeating the same observation re-verifies the artifacts, keeping the checkpoint revision and transaction count unchanged. A stale card revision fails the request and keeps the checkpoint. An old epoch, wrong round, or wrong delivery blocks that member; see [Member isolation](#member-isolation).

The confirmation deadline comes from the dispatch artifact and cannot be extended because of other events or restarts. heartbeat means the subscription is alive; alive means the session exists; only the task revision and the dispatch receipt provide durable progress. No output does not trigger a restart, alive does not prove progress, and a timeout does not prove exit. The entry point does not automatically send to or recover executors.

## Wrap-up Evidence

board reuses the [Review disposition protocol](review-disposition.md)'s artifact, full-publication, role-requirement, author-disposition, batch-predecessor, mechanical-fix, batch-closure, and execution-cycle validation, then consumes the dispatch's integration binding. launch reuses the integration Git validation and calls review's `VerifyClosedReviewGit` to re-check the actual ancestry relationships and mechanical-change digests in the closed batch. The dependency is one-way: launch → review → board; board does not import review, launch, or notify. launch installs its check in board with `SetWrapUpGitVerifier`.

Git is verified while a wrap-up is open, and once more when it completes. `kander move <id> done` of a bound wrap-up verifies before it takes any lock and refuses the move on a failure, so the card stays in `working/`. In the transaction of the move it writes the immutable `kanban/.kander/groups/<group-id>/wrap-up-git/<dispatch-id>.json`. The record binds the dispatch, the epoch, and the integration. A later epoch than the verified one fails the move. A group with no checkpoint writes no record. After that, reconcile checks only the structure of the record and calls no Git, so a rewrite of `develop` no longer fails the group. `kander check delivery` still reports it. A completed wrap-up with no record, from an older binary, is verified once by the next reconcile, which writes the record. When that verification fails, the member is blocked and only a confirmation ends it. Archive cleanup removes the records with the checkpoint.

A historical closed batch does not require the current HEAD to still sit on that batch's commits. A legitimate rebase is still validated by the existing patch-correspondence proof; undeclared rewrites and a mismatched final delivery fail. When the original evidence CWD no longer exists after cleanup, an absolute `cwd` pointing to a still-existing repository worktree may be passed to re-prove the same set of commits in the artifacts; the original paths and SHAs are not rewritten. A completion with no `cwd` falls back to the main worktree.

A completed fix can still be reconciled read-only after the batch target advances and the incremental re-review batch closes, including the recovery window where the wrap-up has not yet been created. The historical path checks the successful run, full publication, predecessor lineage, assignment, and author artifacts; it does not apply the "closed batches are immutable" write gate. Creating or sending a fix still requires an open batch and the current round; a historical completion receipt does not restore send permission. Repeated reconciliation re-verifies all artifacts; when they are missing or corrupted, the existing checkpoint is kept and an error is reported.

Members with no findings only consume published artifacts; no dispatch is made just to copy reports. When some of multiple cards are archived, relative attachments are located by task ID under the actual card state; only a same-round completed together with `done` or `archived/RESULT: completed` finishes that round's wrap-up. Completed cards are not rolled back, and integration is not redone.

Wrap-up on behalf can only consume the dispatch's dedicated wrap-up-only authorization. Reconciliation itself does not grant it. Missing SESSION, unknown delivery, still active, non-zero return, and confirmation or lease timeouts do not individually prove exit; even a confirmed exit must have the old execution epoch isolated by the original dedicated producer. The original author records remain unchanged.

## Failure and Platform Boundaries

The CLI uses a 30-second context, bounding the lock contention of both locked steps, both member loops, and the unlocked Git verification. Underlying OS opens/reads and the publish after the redo is committed still follow the existing transaction boundaries; the entire command cannot claim a hard wall-clock upper bound. Cancellation leaves no background workers waiting on locks; once publishing has begun, recoverable artifacts are kept, with no mid-way rollback guessing.

After EOF/output errors, re-reconcile the current facts once; when the facts are valid, rebuild the subscription only once. Only an explicit `board.transaction_pending` warrants, under the maintenance premise, one explicit init followed by another read; if it still fails, report. duplicate, reparse, real corruption, or unknown products immediately preserve evidence; they must not be deleted, renamed, or retried indefinitely. Lock-deadline exhaustion is a temporary inability to observe, not a task failure.

Tests separately cover board structure/transactions, real local Git, isolated child-process kills, and fake terminals/Agents. Native Windows and real tmux/herdr/Agent sessions require separate on-machine testing; cross builds and the fake CLI do not substitute for real-machine passes. For the original reproductions and regression mapping, see the [Reproduction acceptance mapping](recovery-regressions.md).

## Execution Ledger and Chain Walk

Every change of `OWNER`, `SESSION`, `WINDOW` or the execution cycle writes a producer record in the transaction of the change. The records live in the start control group `kanban/.kander/groups/00000000-start-group/<task-id>/`:

| Path | Content |
| --- | --- |
| `current.json` | The current execution record, schema 2. A replaceable pointer. |
| `<record-id>/pending.json`, `result.json` | Immutable originals, as for a start attempt. A claim, reclaim or recover has only `result.json`. |
| `<record-id>/unresolved.json`, `handed-off.json` | Immutable originals of those statuses. |
| `<record-id>/process.json` | The one-time process identity of a record. |
| `transitions/<seq>.json` | Immutable transition records. `seq` is gapless. |

A record has a `kind` (`start`, `claim`, `takeover`, `reclaim`, `recover`) and a `status` (`pending`, `running`, `unresolved`, `rolled-back`, `handed-off`). A transition has a `kind` (`start`, `confirm`, `rollback`, `unresolve`, `adopt`, `handoff`, `claim`, `reclaim`, `recover`, `session-repair`, `window-update`, `process-identity`), its from and to records, cycles, and the old and new `OWNER`, `SESSION` and `WINDOW`. `kander update` cannot reach these paths. The packages that launch and notify write the card fields only through the board entrances; see [Launch, delivery, and terminal cleanup](launch-and-delivery.md).

Reconcile no longer infers transitions from snapshots. For each member it:

1. Reads the chain and requires every link to continue the previous one.
2. Requires the chain to end at the card: the last link names the card's cycle, `OWNER`, `SESSION` and `WINDOW`. This snapshot comparison is a consistency assertion only. A difference means a change no producer recorded, and reconcile fails with `pending start facts changed` or `execution cycle changed without lifecycle handoff`.
3. Finds the mirror's position (`transition_seq`) and accepts everything after it. A skipped reconcile only makes this segment longer.
4. Reads the card's dispatch rounds from `dispatch_seq`, the count of rounds the mirror has passed. Each round before the bound one must be released, settled, or replaced by the next round.

A member whose current record is `pending` or `unresolved` stays waiting: reconcile succeeds and stores no `dispatch` in the mirror, not even the one it held. `dispatch_seq` stops before an open round, so the walk validates that round once a durable result ends the attempt. `PrepareDispatch` refuses such a member, so no new round can bind it. Adoption or takeover ends the wait ([Late Failures](launch-and-delivery.md#late-failures)). A member in `archived/` or `trash/` takes a separate branch that checks only the `RESULT` field and the chain, then copies the last round into the mirror as a record. An archived card whose evidence cleanup removed the records is accepted on its prune receipt. Such a member no longer repeats Git verification.

Coordinator claims and reconciles write the current checkpoint schema. A schema 1 mirror has no position, so the first reconcile selects one by its facts: the observed start attempt, or the last link that reaches its cycle. No match fails with the same errors as before.

A card from before the ledger has no transitions. A reader synthesizes its chain in memory from start attempts, claim lines, and `LIFECYCLE_DECISION` handoffs, and never writes that chain back. The first producer write on such a card continues the sequence after the synthesized links. A stuck schema 1 `pending` attempt opens to a takeover. Old and new binaries cannot share a board: an old binary rejects schema 2 records.

## Member Isolation

Reconcile judges each member alone, so one failure no longer stops the group. A member that fails keeps its last mirror unchanged and gets `blocked.reason`; the others advance, and the checkpoint is published. A later reconcile judges the member again from that mirror and clears the field when it passes. A stale observation therefore ends with the next correct one, and only a lasting failure needs a recovery.

These fail the whole request and keep the checkpoint, because they say nothing about one member:

- a stale card revision (`board.transaction_conflict`) and an unfinished transaction;
- a cancelled or expired context;
- stale coordinator authority, a checkpoint CAS failure, a wrong member set, and board membership integrity;
- Git facts that changed between the verification and the commit.

`coordinator reconcile` reports `result` with the checkpoint: `complete` (exit 0), `partial` (exit 3), or `failed` (exit 1, every member blocked). A refused request prints no JSON and exits 1. `board.CoordinatorBlockedError` is the Go form of `failed`, returned beside the committed checkpoint.

`PrepareDispatch`, `BeginDispatchAttempt` and `AuthorizeDispatchWrapUp` refuse a wrap-up while any member of its group is blocked, so `dispatch prepare`, `notify --kind wrap-up` and `dispatch authorize-wrap-up` all stop there, also on a same-ID retry. An accepted wrap-up that already runs can still complete. A block does not stop other kinds of dispatch.

Checkpoint schema 3 adds `blocked`, `confirmed` and `recoveries`. Schema 1 and 2 stay readable and upgrade on the first write.

## Audited Recovery

`kander coordinator recover <absolute-request.json>` ends a block that stays. It loosens checks, so it is a last resort: the orchestrator never runs it on its own, and Kander records the decision reference without verifying it.

```json
{
  "group_id": "20260908-example-group",
  "member": "20260908-example-task",
  "expected_revision": 7,
  "expected_epoch": 2,
  "decision": "user decision in session 2026-10-31",
  "reason": "the plain reclaim of an old binary left no handoff"
}
```

Every field is required, and the member must be blocked. The request runs under the exclusive board lock with a CAS on the checkpoint revision and the coordinator epoch, so concurrent recoveries have one winner. A retry is not idempotent: it fails on the revision. After a lost response, read `recoveries` with `show`.

The column of the member selects the mode:

| Column | Mode | Writes |
| --- | --- | --- |
| `backlog`, `todo` | refused | nothing |
| `working`, `review` | reset | card, ledger, recovery review record, mirror, record |
| `done`, `archived`, `trash` | confirm | mirror, record |

A reset refuses a member with any dispatch binding, including a completed one, and launch refuses an executor that reads `alive` or `alive-orphaned`. Other probe results proceed: `unknown` never counts as alive. launch probes before the command takes a lock, and board requires the card revision that was probed.

A reset writes `STARTED_AT`, a new claim, an execution record of kind `recover`, and a transition of kind `recover` with the decision reference, and retires the old record as `handed-off`. The transition continues the chain, not the card, so it also records a gap from a change no producer recorded, such as the plain reclaim of a binary from before #20. A `review/` card moves to `working/`, because its delivery belongs to the old cycle. `OWNER`, `SESSION`, and `WINDOW` stay; the takeover replaces them and closes the old container. A record of kind `recover` has no session of its owner, so plain `resume`, `notify`, and `dismiss` refuse it and name `resume --agent`.

The mirror rebinds to the end of the ledger, with no delivery, dispatch, review, handoff, or release cursor. Nothing else invalidates evidence: the new cycle no longer matches the plan, so the done gate needs `rebind_cycles` as after a reclaim. Other members keep their evidence.

The reset also writes `recovery-reviews/<task-id>.json` in the review control group. It names the union of the non-N/A roles of all batches of the plan, with historical `PM`/`QA` folding to `PMQA` and `CSA`/`Hacker` to `Security`, and the batch position at the recovery. Until a closed batch from that position on requires each role for this member, `review progress` lists `recovery-review:<task-id>` and the done gate refuses. An N/A batch never discharges it. The record binds the member, not a cycle, so a later reclaim does not end it. A sealed plan takes a batch while the record is owed, so an N/A batch that closed first is no dead end. Without this record a recovery could launder a plan into N/A, which the rule for a reclaim allows.

A confirmation writes no card, no ledger, and no review evidence, and needs neither a binding check nor a probe. It sets `confirmed` on the mirror with the recovery ID and the cycle of the card. For that cycle reconcile only updates the revision and the column, and reads nothing else, so a damaged ledger or a failed Git verification stays behind it. The mirror takes the end of the ledger when that is readable. A confirmation loosens no gate of any card.

Each recovery writes the immutable `groups/<group-id>/recoveries/<recovery-id>.json` and adds the same object to `recoveries` in the checkpoint. The record binds the checkpoint revision and the coordinator epoch, and holds the decision, the reason, the blocked reason, and the cycles. Reconcile requires each listed entry to equal its record, and each `confirmed` to name a listed confirmation. `kander update` cannot reach the checkpoint directory, and archive cleanup removes it with the group.

## Released Bindings and Authorized Cycle Handoffs

A checkpoint may still name an old dispatch after `dispatch fail|cancel` or `dispatch settle`
removed the card's active binding. Reconciliation accepts an unbound observation only after
verifying that round's terminal state, immutable termination/release originals, card release record, epoch,
base, kind and revision progression. The checkpoint clears its active `dispatch` and retains
a card-relative `released_dispatches` reference. A bare failed/cancelled state string or
missing original cannot authorize the change. Legacy terminal bindings are first released
through the public command with a decision reference; the release preserves the old terminal
facts instead of fabricating a new termination original.

`move working --owner --decision` then publishes the protected `LIFECYCLE_DECISION` handoff.
Both coordinator cycle consumers verify its old/new cycles, released dispatch, nonempty user
decision reference and increasing card revisions. A cycle that never held a dispatch has no
release to cite, so its reclaim writes a dispatchless handoff with an empty `dispatch_id`. A
release is the only way to clear a binding and a bound card rejects `--owner`, so a cycle with
a dispatch always has its own release. The producer and the consumers apply one check
(`verifyDispatchlessHandoff`): no release names the old cycle, and every completed or accepted
dispatch before the handoff was followed by a release or has a settlement original. A plain reclaim that an older binary
already wrote has no handoff and stays rejected; this change does not repair such a checkpoint. A successful launch original remains
unchanged: its original cycle can reach the new cycle only through those verified handoff
links. The same path supports a manually claimed card without a launch original. Reconciliation
updates the member cycle, retains the start-attempt identity and handoff history, and clears
any previous cycle's delivery from the active cursor. A new delivery still requires the normal
Git or completed-receipt evidence; release and reclaim alone do not establish delivery.

After reclaim, `review progress` reports `requirements-needed` and the complete
`rebind_cycles` map. Extend the existing plan with that map, then reconcile the existing
checkpoint. Its old review batches and originals remain binding. Recovery also accepts a
checkpoint that missed both release and reclaim, validating the same links in one observation.
Optional checkpoint fields preserve schema-1 compatibility; old checkpoints need no rewrite
before reconciliation. Replays revalidate retained release and handoff evidence. Missing or
changed originals, absent handoff decisions, a substituted cycle or regressing revisions
reject without changing the checkpoint. These references record user decisions; they do not
grant the coordinator independent authority to terminate or reclaim an executor.
