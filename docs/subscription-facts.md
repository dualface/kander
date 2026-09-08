# Subscription Committed Facts and Member Sets

`kander subscribe <task-group> <task-id>... [--watch <task-id|task-group-id>...]` outputs versioned JSON Lines. Member IDs are fixed, `--watch`'s original group references are preserved, and each observation re-expands them. Subscription only reports facts; it does not perform dependency release, business confirmation, automatic recovery, or kanban repair.

## Events and revision

All lines keep `event`, `group_id`, and `tasks`; state changes keep `changed` with `task_id/from/to`. The new fields are as follows:

| Field | Meaning |
| --- | --- |
| `schema_version` | Currently 1 |
| `subscription_id` | A random identifier newly created for each subscription |
| `seq` | A sequence number for this subscription, incrementing from 1; not a cross-process cursor |
| `observed_at` | The completion time of the coordinated snapshot read, UTC RFC3339 |
| `task_revisions` | The committed revisions from the same snapshot as `tasks` |
| `read_status` | `committed`, `recoverable`, `maintenance`, or `invalid` |
| `membership_complete` | Whether this observation's member set and the watched cards were fully readable |
| `reconciliation_required` | The consumer must re-verify first; this must not be used to auto-release |

The initial line is `snapshot`. A different state sends `state-change`; the same state with a different revision sends `task-update`. The `updated` of both lists the watched cards whose revision changed. A state change in the same round may carry revision changes at the same time, without sending a duplicate `task-update`.

A controlled update to the body or attachments in the same state advances the revision; a `review -> working -> review` within one refresh produces a `task-update` even though the final state is the same. A restart re-sends the current `snapshot`; consumers compare the updates that have happened using their saved revisions, and cannot use a new process's `seq` to backfill historical events. A revision only proves that a controlled update happened, not that some dispatch round has completed; the current dispatch's durable receipts are provided by `dispatches` described below, and the interface has no event replay log. Cards from old versions without an established version record have revision 0, and editing files directly while bypassing transactions is not guaranteed to advance the revision.

Heartbeats are timed independently, defaulting to refresh=1 second and heartbeat=900 seconds. Only `working` cards in the current watch set are collected, plus `review` cards whose durable dispatch is still awaiting confirmation; scanning does not wait for probing or output. An event's `observed_at` denotes the card snapshot time and does not masquerade as the probe completion time. For the bounded lifecycle of probing and output, see the section below.

## Dispatch Facts and Confirmation Deadlines

Snapshots, heartbeats, and related change events may carry `dispatches`, keyed by task ID. Each summary contains:

- `dispatch_id`, `task_id`, `kind`, `epoch`, `state`, and the dispatch's own `revision`.
- `created_at` preserves the original intent creation time; `age_seconds` is computed from that time at snapshot time.
- `confirm_by` is the effective acceptance deadline of the current epoch: an ordinary authorization takes the original intent's deadline; a wrap-up-only dedicated grant takes its own independent deadline, and the original intent's deadline is not rewritten.
- The `accepted` / `completed` atomic receipts, containing the time, card revision, state, and the applicable delivery/disposition references.
- `confirmation_pending`: true only for prepared/delivery-unknown; `confirmation_overdue` means these two states have reached the current epoch's effective acceptance deadline. After accepted it is not a completion timeout, and the confirmation deadline is no longer used.

These fields are read within the same group-shared lock as the same event's card states, bodies, and `task_revisions`; the summary reports only the current execution authorization and does not expose message bodies. Unbound old cards omit the corresponding entries; historical receipts cannot be fabricated. When a same-ID `review -> working -> review/done` completes within one refresh, even if notify has not yet returned, the next event or the first snapshot after a restart still contains accepted/completed, with no need to catch the working edge. After a new dispatch replaces the current authorization, the old ID can still be queried via `kander dispatch show`; the summary is not a history list. A completion receipt proves the controlled completion operation and cannot substitute for review batch closure or Git integration verification.

Each scan reads the current epoch's `confirm_by` and wakes independently on that absolute deadline; same-epoch retries, subscription restarts, and unrelated state, body, or membership changes and heartbeats do not extend it. Discovering an already-expired deadline for the first time also immediately sends `dispatch-attention`; `attention` lists the timed-out task IDs, and `reconciliation_required=true` requires the consumer to verify. The event attaches the current dispatch summaries and the liveness cache, and starts bounded probing when necessary; while a batch is already in flight, one merged request is retained and scheduled after it completes, without stalling the scan. After a probe batch completes, dispatches still overdue send another attention event, carrying the latest available observations. Subsequent heartbeats keep collecting. When there is no dispatch progress, `confirmation_pending=true` is kept even with `liveness.status=alive`; the liveness observation age and the dispatch age are output separately. unknown, incomplete facts, and deadline arrival do not automatically recover, re-send, rewrite cards, or release dependencies.

Only working and pending review participate in probing; ordinary review and already-completed review are not probed. Missing/conflicting dispatch artifacts or receipts make the watched card's facts unavailable, following the existing terminating error events; they cannot be treated as unbound. During task-group expansion, a dispatch error on an unrelated card does not masquerade as a membership error; errors within the watch set still fail closed.

## Dynamic Group References and Incomplete Facts

With `--watch`, each line's `watch_references` preserves the original references, and `watched` denotes the expanded external tasks; when the external set is empty, `watched` is omitted. `tasks` and `task_revisions` contain this group's explicit members and the current external set. Group references additionally carry `memberships` and `membership_versions`. A membership version is a deterministic SHA-256 of the sorted member-ID set; an empty group in an already-started subscription uses `empty`. The version does not change because of unrelated body or state updates, and it is not a globally incrementing counter.

A member-set change sends `membership-change`, carrying the complete new set and version. Newly added members enter the watch immediately. When a removal or regrouping causes members to leave the originally watched group, `removed` preserves the departed IDs (even if they still belong to another watched group), and `reconciliation_required=true` persists across this subscription's subsequent events; a shrinking member set must not be interpreted as dependencies having been satisfied. Consumers must re-verify the contract, the before/after membership versions, and the actual deliveries. An empty group is refused at initial subscription; a group going empty within an existing subscription still sends the membership change, and it is not treated as successful completion.

Duplicates among member IDs, explicit external IDs, and group expansions are refused. Group member resolution and `check`'s dependency expansion reuse the board-layer results, compatible with the legacy discussion-area group fields. Group references require board-wide membership information: when scan problems, unreadable bodies, and the like make membership undeterminable, the terminating event `membership-unknown` is sent with `membership_complete=false` and `reconciliation_required=true`, followed by a non-zero exit. If that line carries task facts, they are only the last successful snapshot and cannot be consumed as a new complete snapshot. An initial failure uses an empty mapping.

When watching only explicit task IDs, a targeted scan is used, and unrelated known problems do not trigger extra reads or Agent probing. Group expansion must check all possible members; an entry whose membership cannot be read must not be assumed to belong to an unrelated group.

## Coordinated Reads and Recovery

`board.ScanContext` / `ScanTargetsContext` read states, bodies, and revisions under the same group of shared locks; `Board.Revision` and `Board.Document` preserve the snapshot values. The old `Scan` / `ScanTargets` interfaces remain available, keeping blocking reads. `ScanDispatchesContext` optionally captures the current dispatches, and `Board.CurrentDispatch` returns a summary consistent with the body and revision; the old scan entry points do not gain dispatch reads. `Board.GroupMembership` returns members, versions, and completeness problems; when group dependencies exist, `TaskDependenciesOf` and `check` do not accept partial expansion results.

Each subscription fact read shares a 2-second deadline. Kanban maintenance-lock, task write-lock, and journal-lock contention all obey this deadline; no background lock-waiting goroutines are left behind. On exhaustion, `read-error` with `read_status=maintenance` is sent, followed by a non-zero exit; that status may also indicate normal write contention, and it does not claim it is confirmed that someone ran init. On recognizing a prepared but uncommitted transaction, `read-error` with `read_status=recoverable` is sent immediately and the process exits, leaving the operator to recover explicitly under the transaction maintenance contract. Duplicate cards, reparse, and persistent corruption fail closed and are not auto-repaired.

The deadline covers lock contention and the cancellation checks between the read phases; file opens, kernel I/O, and closes remain under operating-system control, with no promise of hard real-time termination. POSIX uses non-blocking flock attempts, Windows uses LockFileEx with FAIL_IMMEDIATELY; the existing blocking write locks and safe-path entry points are unchanged.

## Bounded Probing and Output Lifecycle

Scanning, probing, and output each hold their own runtime resources. Scanning does not wait for the external Agent CLI; the first liveness collection batch starts after the initial snapshot is enqueued, and subsequent heartbeats or dispatch confirmation deadlines start the next batch once the previous one has ended. Each invocation reuses `ClassifyTasksContext`'s default total budget of 10 seconds and concurrency of 4. A subscription holds at most one in-flight collection batch, one completed-result slot, and one cache, and does not pile up more probe batches because of refresh or heartbeat backlog.

Heartbeats carry over `agent/status/channel/detail` and add the following fields:

- `revision` and `identity`: this card revision and the request identity; a result is consumable only when both match.
- `observed_at`, `age_seconds`: the real collection end time and the age at heartbeat generation; the time is omitted when there was no collection.
- `runtime_state`, `observation_valid`: reuse the batch collection facts; ready and business progress are not derived from alive.
- `collection_state`: `pending` means the current revision has no result, `complete` means a collection was attempted, `not-observed` means the batch did not actually collect; `collecting` independently indicates whether the subscription has a batch running.
- `stale`: true when the cache age exceeds heartbeat plus 10 seconds, at which point status/runtime_state become unknown and observation_valid becomes false; the original observation time and age are preserved and do not masquerade as a fresh observation.
- `new_window`: preserved when there is a reverse-lookup address suggestion; not written back to the card.

When the revision or SESSION/WINDOW/OWNER/STARTED_AT changes, old results are discarded and pending/unknown is output. Even while other cards keep changing, heartbeats still report the current cache on their independent clock. pending is allowed while a slow batch has not ended; that kind of unknown must not be treated as the Agent having stopped. Result coverage and the input set are both O(number of watched cards), not an unbounded history queue; after a single batch's total budget is exhausted, undequeued items stay unknown.

Output has a single worker, with at most 16 lines queued plus one more line being written out; each line is at most 1 MiB (including the newline). Each line's 2-second deadline is counted from enqueue and includes queueing time. A full queue, an oversized single line, a short write, a broken pipe, or a write timeout each report an explicit error and end the subscription, rather than dropping events and continuing to fake a complete stream. Exit may leave lines not yet written out or a partial final line; consumers parse only complete JSON lines and re-verify snapshot/revision after reconnecting. The heartbeat interval starts after the snapshot/heartbeat is enqueued, without waiting for the consumer to read; slow consumers keep FIFO within the limits. All members being done does not auto-exit; the consumer still decides when to stop. stderr writes for error diagnostics are likewise limited to 2 seconds; even when stdout/stderr point at the same stopped-reading pipe, there is no second unbounded wait because of diagnostics. When the diagnostic cannot be written out, failure is signaled by a non-zero exit, waiting in the worst case one extra diagnostic deadline beyond the output failure.

`SubscribeContext` accepts the caller's context; the old `Subscribe` keeps the stop-channel wrapper, and closing stop returns success. The CLI treats Ctrl+C as a normal exit, and POSIX also handles SIGTERM/SIGHUP; Windows handles the console Ctrl+Break, close, logoff, and shutdown notifications that Go exposes. A forced kill does not run cleanup hooks. All normal exit paths cancel and wait for the probe worker, the output worker, the stop-channel adapter, and the platform cancellation callbacks, abandoning no blocked goroutines. Read-lock contention also reuses the caller's context and the original 2-second read deadline.

A custom `io.Writer` must implement `ContextWriter.WriteContext`, returning after cancellation and leaving no background writes behind; an ordinary non-cancelable Writer is refused before the first write. `bytes.Buffer`, `strings.Builder`, and `io.Discard` remain compatible. During the call, the output target is owned exclusively by the subscription; the caller must not concurrently write, close, or modify descriptor attributes. An implementation that violates the ContextWriter contract cannot obtain the exit guarantee.

The platform adaptation boundaries are as follows:

- POSIX: nonblocking is enabled on the passed-in file descriptor, checking cancellation/deadline every 5ms on EAGAIN; a full pipe is not waited on, the original file status flags are restored after the writer joins, and the caller's file is not closed. Descriptor duplicates share these flags, so the caller must also constrain the use of aliases.
- Windows: deadline-capable overlapped files use the Go poller and write deadlines; synchronous handles write on a pinned OS thread, are canceled with CancelSynchronousIo, and the cancellation thread is waited on until it joins. The race between the cancellation request and entering the write call is covered by 5ms retries. The console keeps Go's Unicode write-out. [Microsoft's cancellation contract](https://learn.microsoft.com/en-us/windows/win32/api/ioapiset/nf-ioapiset-cancelsynchronousio) does not guarantee that every kind of kernel I/O completes immediately; the implementation retains and waits for real completion and does not fake reclamation.

File opens, ordinary disk/network filesystem I/O, driver responses, process creation/reaping, and system scheduling remain under OS control; the deadlines above are not a hard real-time guarantee for uncancelable kernel operations. JSON encoding happens before the single-line size check and still requires temporary space proportional to the current watch set. Automated tests use a temporary kanban and a fake CLI; real terminals, real tmux/herdr/Agent, native Windows, and cross-compilation results must each be recorded separately.
