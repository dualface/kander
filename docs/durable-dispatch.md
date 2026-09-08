# Durable Dispatch Protocol

Dispatch identity is determined by `dispatch_id`, not inferred from terminal echo or column changes. `prepared` only proves the intent has been persisted to disk; before sending it enters `delivery-unknown`. Only the executor side's controlled `move working` produces `accepted`; `move review` or `move done` commits the business receipt, card state, and revision in the same transaction.

## Creation, Reading, and Retry

```sh
kander notify <task-id> --kind fix --base <40-character-SHA> \
  --evidence-file <evidence.json> --message-file <UTF8-file>
kander notify <task-id> --dispatch-id <id> --message-file <same-UTF8-file>
kander resume <task-id> --dispatch-id <id> --message-file <same-UTF8-file>
kander dispatch show <task-id> <id>
```

`kind` is `fix`, `sync`, or `wrap-up`. Review cards in a task group automatically use durable mode, defaulting to fix when kind is omitted; working cards or cards without a group opt into durable mode via an explicit `--kind` / `--dispatch-id` / `--base`. When the ID is omitted, it is generated and printed before any send. A new intent missing base uses the Git HEAD of the current working directory; a retry missing base/kind uses the original intent, and must not replace the original baseline with a new HEAD.

The caller may also prepare UTF-8 JSON first, then run `kander dispatch prepare <intent.json>`. The fields are `dispatch_id` (optional), `task_id`, `kind`, `message`, `base`, optional `references`, `created_at`, and `confirm_by`. Each reference is expressed as `{ "task_id": "...", "path": "reviews/run/report.md" }`; absolute state-directory paths are not stored. The generic `references` still only indicates attachment locations and cannot substitute for the required `evidence.fix` / `evidence.wrap_up` semantic bindings below. A new fix/wrap-up without a binding is refused at creation and at send; sync does not accept either of these bindings. Historical unbound intents can still be read and reconciled, without fabricating review or integration evidence, and cannot continue to be sent as new fix/wrap-up dispatches.

Repeated creation with the same ID, same task, same message, same baseline, and same kind/references does not write a second intent and does not increment the card revision. Different inputs conflict explicitly. Time defaults are applied only at first creation; a retry omitting times keeps the original values. `confirm_by` is the acceptance deadline, defaulting to 120 seconds after creation; new intents from notify/resume use this invocation's `--timeout`. It is not a work-completion deadline; accepted work may complete after that deadline.

Readers check the durable receipt first. When accepted/completed already exists, return it directly without re-sending or re-launching. When there is no receipt, a same-ID retry re-collects the facts: the same-ID instruction may be re-sent to the currently unique, receivable same session; only a stopped observation with valid identity permits recovering the original session. unknown, invalidated observations, missing identity, and busy do not authorize recovery. A send call that reports an error may still have delivered; keep delivery-unknown and the message file, and do not launch a recovery instance right afterwards. Once a recovery launch has attempted to hand the command to the Agent, keep the window, WINDOW, and task files even if subsequent launch, marking, or liveness verification fails; it may have already accepted the work and must not be blindly cleaned up. When a placeholder container was created but no send has been attempted, this attempt's resources may still be cleaned up. Deadline expiry returns non-zero, does not reset the deadline, and does not claim accepted.

An explicit `resume --agent` carries over the existing user-authorized takeover semantics. Taking over a non-terminal dispatch increments the execution epoch and preserves the previous epoch's original state and receipts; the original message, baseline, and confirmation deadline are unchanged. The takeover message cannot be swapped in under the same ID; when a new message is needed or the intent has already expired, the caller first explicitly disposes of the old intent, then creates a new ID. The entry point for canceling/failing an intent is `kander dispatch cancel|fail <task-id> <dispatch-id> <dispatch-revision> <reason>`; it records the dispatch disposition, does not cancel, archive, or move the task card, and does not automatically authorize takeover.

## fix Review Artifact Binding

The `--evidence-file` of `notify` / `resume` reads strict JSON. `dispatch prepare` instead places the same object in the intent's `evidence` field. The IDs and commits in the example must come from a real review and assignment; the following only shows the field shape.

```json
{
  "fix": {
    "batch_id": "batch-one",
    "findings": [
      {"run_id": "pm-round-two", "finding_id": "PM-02", "previous_run_id": "pm-round-one"}
    ],
    "authors": [
      {
        "finding": {"run_id": "pm-round-one", "finding_id": "PM-01"},
        "record_id": "author-one",
        "author": "codex",
        "artifact": {
          "task_id": "20260908-example-task",
          "path": "reviews/pm-round-one/dispositions/author-one.json"
        }
      }
    ]
  }
}
```

Creation validates that the batch's current target equals the dispatch base, that the run belongs to that batch/task and is fully published, that the run's predecessor ID matches, and that the finding is a structured blocking/high/medium item with an explicit assignment to this card. A predecessor round already superseded by a successor run can no longer be dispatched. Before each actual send, the original artifacts are re-read and re-checked within the transaction that creates the send attempt, without relying on stale card paths. Cross-card copies, assignments, mappings, author dispositions, and report hashes follow the review module's structural validation.

`authors` references all of this card's author artifacts that already exist for this item and its explicit finding lineage, preserving each one's author, run/finding, record ID, and relative path. This round's dispositions that do not yet exist are not required to be submitted in advance; when there are no prior dispositions, authors may be omitted. New dispositions appearing after creation do not rewrite the original dispatch binding. A same-ID retry should omit the evidence-file and inherit the original binding directly; providing a different binding conflicts. Missing artifacts, impersonated authors, and arbitrary cross-round references are all refused. Legacy unstructured reports must first go through the review's `map-legacy` explicit mapping, then assignment; findings are not guessed from IDs mentioned in the body text.

## wrap-up Integration Binding and Dedicated Authorization

An ordinary wrap-up likewise uses the evidence-file; the fields are:

```json
{
  "wrap_up": {
    "git": {
      "cwd": "/absolute/group-or-main-worktree",
      "source_commit": "<40-character-final-group-SHA>",
      "target_commit": "<40-character-integrated-develop-SHA>",
      "target_ref": "refs/remotes/origin/develop",
      "author": "coordinator",
      "basis": "the user authorized integration; the normal push and local sync are complete"
    }
  }
}
```

The tool reads the original `review_base` and final `review_target` from this card's closed review plan, validates the review range, that source is an ancestor of target, and that target is contained in the specified develop ref, and re-reads the ref at the end of validation to reject concurrent changes. By default it also requires source to equal the closed review_target, and does not accept unreviewed commits being mixed in. Only `refs/heads/develop` or `refs/remotes/origin/develop` are accepted; a remote ref only proves the locally fetched remote snapshot, and the orchestration side must still complete the fetch and actual sync first. It does not automatically fetch, integrate, delete worktrees, or confirm user authorization. When a legitimate rebase rewrites commits, `rebased_base` (the new develop baseline before replay) may be provided explicitly. The tool validates that the original baseline precedes the new baseline and the new baseline precedes source, and compares the full Git patches of the original review range and the replayed range. Only blob hashes and hunk line-number offsets are normalized; whitespace, context, file modes, and binary changes are preserved. A patch-id that ignores whitespace is not used. Mismatched patches are refused and must go through the existing conflict validation/review flow; PR merges or equivalence are not accepted on the basis of descriptions.

`dispatch_id`, `task_id`, `verified_at`, `review_base`, `review_target`, and the relative `artifact` are filled in by the creation entry point; explicitly provided identities and review targets must still match. The intent and `dispatches/<id>/integration.json` are published in the same transaction. board validates only structure, review targets, and copies, and does not run Git; the full binary's launch integration layer validates the real Git relationships. Validation runs again before sending; the completion receipt must carry the bound source_commit, and it cannot be replaced with an arbitrary SHA or author disposition. The binding is independent of card moves.

The original orchestration wrap-up-on-behalf exception is preserved, but a non-zero return, a missing SESSION, a confirmation timeout, or deadline expiry cannot on its own prove the original executor has exited. Reconcile with the same ID first, then verify the facts; only a confirmed exit, or a stopped re-confirmed after a previous legitimate user-authorized reclaim, may request a dedicated epoch:

```sh
kander dispatch show <task-id> <dispatch-id>
kander dispatch authorize-wrap-up <request.json>
```

```json
{
  "task_id": "20260908-example-task",
  "dispatch_id": "wrap-one",
  "expected_revision": 2,
  "author": "coordinator",
  "reason": "the original executor has exited; wrapping up on behalf under the existing exception"
}
```

`expected_revision` is the dispatch revision. When there is no dispatch record, attach a complete `intent` to the request (kind must be wrap-up, and the explicit ID/task must match the outer fields); the intent is created first, then observed — permission is not granted out of thin air. A previous legitimate reclaim may additionally fill in `reclaim_decision`, referencing an already existing user authorization; this field does not trigger a reclaim and does not substitute for the stopped observation. Missing SESSION, unknown, and alive/drifted are all refused; a verifiable exit fact must first be established through the existing authorization flow. The tool does not broaden takeover permissions and does not infer consent from having waited long enough.

The wrap-up-on-behalf entry point first reads the current receipts inside the delivery lock: completed, or an existing dedicated grant with the same author/reason, reconciles directly without granting again. Otherwise it collects a fresh stopped observation consistent with the current card identity, carrying the card revision and dispatch revision for CAS. delivery-unknown/accepted must both go through this verification; if a concurrent acceptance or WINDOW change occurs, the CAS rejects the stale facts. The authorization transaction first saves `execution-<old-epoch>.json`, raises the epoch, and publishes `wrap-up-authority-<epoch>.json`; the old executor cannot use the latest revision to update the body, WINDOW, author dispositions, or completion receipts. The new dedicated grant has its own independent 120-second acceptance deadline; the original intent's deadline is not rewritten. The confirm_by in the board snapshot used by subscription outputs the actual acceptance deadline of the current epoch, avoiding misreporting a just-granted wrap-up authority as expired. This is not proving exit by deadline expiry.

After obtaining the dedicated grant, go through the atomic receipt entry point `move working`, and start cleanup only when replayed=false. Only user-authorized cleanup and appended records are allowed; code changes, Agent launches, notification delivery, escalating to an ordinary takeover authorization, rewriting the original author's dispositions, or rewriting runtime identity are not. Ordinary update accepts only appending `## WRAP_UP_RECORDS` while preserving the full original spec text, or the first write of `wrap-up/<dispatch-id>-<epoch>.md` (a retry with identical content may read and write; different content cannot overwrite). The original OWNER, SESSION, and existing author records remain unchanged; the actual on-behalf author is recorded in the grant. Completion still goes through done's original gates; fabricated author conclusions must not be used to scrape a pass.

After the worktree is cleaned up, reconciling already-stored intents/authorizations/receipts under the same ID does not require the original Git CWD to still exist; continuing to send, or granting anew, must still re-validate Git. The dedicated authorization only constrains processes that respect the controlled entry points; it cannot stop local processes that bypass the tool to edit code or operate on Git or the filesystem directly. The executor must respect the cleanup-and-records-only scope of the authorization.

## Executor-Side Atomic Receipts

```sh
kander move <task-id> working --dispatch-id <id> --execution-epoch <epoch>
kander update <task-id> --document spec.md --file <UTF8-file> \
  --expect-revision <revision> --dispatch-id <id> --execution-epoch <epoch>
kander move <task-id> review --dispatch-id <id> --execution-epoch <epoch> \
  --delivery-commit <final-40-character-SHA> --disposition <relative-artifact>
kander move <task-id> done --result completed --dispatch-id <id> \
  --execution-epoch <epoch> --delivery-commit <final-40-character-SHA>
```

The acceptance command returns JSON containing `dispatch` and `replayed`. Only `replayed=false` starts this round's work. A repeated acceptance returns the original receipt with `replayed=true`, without re-running the work or incrementing the revision; even if the card quickly moved back to review after the first acceptance, that acceptance can still be confirmed. An old ID/epoch cannot accept a new round, nor can a freshly re-read newer revision be used to bypass the authorization check.

The completion target for fix/sync is review; for wrap-up it is done. The full delivery SHA is required; where applicable, include the disposition's relative attachment path, and the attachment must exist on this card. This proves the references are committed atomically with the completion; it does not claim the references or an arbitrary SHA as Git integration verification. done still goes through the original summary/report, review plan, and batch-closure gates. A completion retry returns the original receipt only with the same target and same evidence.

A review author's disposition JSON likewise carries `authorization: { "dispatch_id": "...", "epoch": 1 }`, preventing an old executor from submitting dispositions with a new revision; historical unbound records do not gain this field. The stated artifact and round bindings are validated before a fix is sent; a wrap-up-only authorization cannot submit author dispositions.

Each card keeps only one valid execution authorization. A new intent can only replace a completed, failed, or canceled intent; an explicit takeover can rotate the epoch of a non-terminal intent. Ordinary update is refused before acceptance, after the end, with a missing ID/epoch, or with an old epoch. WINDOW and launch/notify rollbacks carry the version cursor and authorization from when the operation started; they cannot borrow a new execution round's cursor, nor write consumed old body text back. No kanban or card lock is held for the duration of an entire Agent session. archive/trash lifecycle decisions that explicitly carry reason/decision may still operate on a bound card; the unfinished authorization is canceled in the same transaction, and cannot be used to keep executing work.

## Storage and Recovery

`internal/board` defines pure types and the controlled API; notify/launch/window consume it through public entry points, and no board-to-notify dependency exists.

- `dispatches/<id>/intent.json` stores the creation artifact; `state.json` stores the current version.
- `accepted-<epoch>.json` / `completed-<epoch>.json` store the per-epoch atomic receipts; a takeover additionally stores `execution-<epoch>.json`.
- `.kander/groups/00000000-dispatch-group/<id>.json` is the reserved board-wide ID registry, preventing the same ID from being reused by another task. Creation orders locks as board, group, task, then the short-lived journal lock.
- Artifacts, state, and receipts are managed by dedicated producers; ordinary update refuses to rewrite them. All publishing reuses the redo journal and `internal/fs` of [Card transactions](card-transactions.md), and after a card moves, the artifacts follow the card.
- Each dispatch has an independent, OS-managed delivery lock, acquired before the board lock. It covers only send/recovery and this confirmation wait; receipt writes do not acquire this lock. The lock is released when the sending process is killed, and does not occupy the execution session's lifetime. Process creation, kernel I/O, lock waits, and process reaping remain subject to the operating system.
- When publishing is interrupted, readers report pending, do not read intermediate state, and do not auto-repair. Run `kander init` under the maintenance conditions to redo the transaction, then reconcile with the original ID. Unknown files and conflicts are preserved; evidence is not deleted to manufacture success.

The context entry point for batched liveness probing (see [Probe deadlines and cancellation](probe-deadlines.md)) provides a total probing budget (default 10 seconds), with the acceptance deadline as the parent deadline; identity validity is checked by `ValidFor`. Readiness of the same session is checked again before delivery; alive is separate from ready, accepted, and completed. Sending and the confirmation wait share the durable deadline. Real terminal markers serve only as transport diagnostics; prompt text appearing on screen does not constitute evidence of work having started.

## Compatibility and Capability Boundaries

Unbound-mode ordinary messages to working cards, and cards without a group, continue to use the existing message flow, without fabricating historical accepted/completed receipts. An ordinary notify to a bound working card allows only direct-delivery information and does not recover processes through the old mode; resume must explicitly carry the dispatch ID. When a recovery launch succeeds and the confirmation budget is not yet exhausted, resume may return a genuine delivery-unknown (not yet accepted) status; the caller must still read the business receipt. When the confirmation budget is exhausted and a re-read still finds no acceptance/completion receipt, a non-zero pending must be returned; mere foreground/console liveness does not satisfy the acceptance condition, and the unknown executor and payload remain preserved. foreground's process wait continues after the delivery lock is released, and cannot lock out same-ID reconciliation for the duration of the session. Durable mode takes precedence over the old rules' wording that treated review-working column changes or terminal markers as confirmation. A bound card must not return to the old protocol via unauthorized update/move. Old binaries do not understand this protocol; upgrade/recovery must still follow the existing maintenance-window rules, and must not run in parallel with old writers that bypass the protocol.

The guarantees are limited to local processes that respect the controlled entry points: a repeated acceptance does not produce a second receipt; a new epoch rejects old controlled writes. An on-disk protocol cannot make arbitrary external side effects — Git, network, file edits — exactly-once. If the executor dies after acceptance, midway through an external operation, an existing accepted is not mistaken for completed; subsequent decisions must reconstruct the actual work progress and cannot blindly replay.

Linux regressions use temporary directories, a fake CLI, and child-process kill/restart, covering intent creation, acceptance, review/done completion, interruption before and after sending, same-ID reconciliation, and concurrent rollback. Windows cross-compilation only verifies the build; native Windows lock/DACL/reparse/process-reaping behavior and real tmux/herdr/Agent behavior each require their own on-machine evidence.
