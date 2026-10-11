# kander subscribe

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Reading the event stream", "Subscription Events")

## Purpose and users

Streams board events for a task group and its watched external cards or groups as one JSON object per line. Used by the orchestrator as its only periodic view of the executing agents; it never polls the board with `list`, `check`, or card reads.

## Syntax

```text
kander subscribe [--refresh SECONDS] [--heartbeat SECONDS] (<task-group> <task-id>... [--watch <task-id|task-group-id>...] | --watch <task-id|task-group-id>...)
```

## Arguments

- `<task-group> <task-id>...`: the group and its complete member set; the command validates ownership.
- `--watch <task-id|task-group-id>...`: external cards or groups, repeated per direct reference and re-expanded on each observation; alone it monitors standalone cards without a group.
- `--refresh SECONDS`: default 1.
- `--heartbeat SECONDS`: default 900; the heartbeat starts after the snapshot is queued and restarts only after each heartbeat is queued.

## Preconditions

- `subscribe` requires an explicit group ID and non-empty member IDs and validates ownership. A nonexistent or empty target, a duplicate member, or overlapping expansions fail before subscribing.

## Effects

**Reading the event stream**

A subscription is a background process plus a file. The loop, in order:

1. Start `kander subscribe ...` in the background with standard output connected to a new file.
2. Read the file from the cursor, handle every complete line, and advance the cursor.
3. Wait for new output in that file.
4. Go to step 2. Leave the loop only by stopping the subscription or when the process exits.

- Start: `kander subscribe` writes one JSON object per line to standard output and does not exit while it is watching. Start it through an argv array, with no shell command string. When the runner accepts only a command string, pass each argument as its own quoted word and never concatenate card or user data into that string. The file is a new UTF-8 file that this process alone writes, in the system temporary directory or the agent's own scratch directory, never in the kanban board or a task worktree. If the runner backgrounds the process and captures standard output in its own log file, that log file is the event stream. Standard error is diagnostics, not events.
- Read: the cursor is the byte offset, or the count of complete lines already handled. Each read takes only the content after the cursor. Accept a line only when it ends with a newline and parses as one JSON object. Leave a partial trailing line unread until its newline arrives. Advance the cursor only after that line is handled.
- Handle: the first line is `snapshot`. Later lines are `state-change`, `task-update`, `heartbeat`, `membership-change`, `dispatch-attention`, or a terminal `membership-unknown` or `read-error`. Apply the event rules in "Subscription Events". Never skip a line because the same fact is also on the board.
- Wait: when the runner reports new output from the background process, that report is the wake-up. When the runner gives no such report, a bounded wait that re-reads only this file past the cursor is allowed. Never poll the board on a timer, and never replace the file with `kander list`, `kander check`, or repeated card reads. No new complete line until the next heartbeat is normal, not a failure. The heartbeat interval is the only periodic check of the executing agents.
- A live process, a background handle, or the runner returning after it backgrounds the process is not an event, and a file that nobody reads does not stop the process. Only reading the file delivers the events. Unread complete lines are pending work: every continuation first handles them, and ending the turn, switching away, or reporting the cards as watched while they are unread misses the subscription.
- Exit: read any last complete line, discard a partial trailing line, then reconnect per `KANDER-KANBAN-RULES.md` "Handling Blocked Executing Agents" and reconcile the new snapshot; a task group also reconciles per `KANDER-TASK-GROUP-RULES.md` "Durable Coordinator Recovery".
- A new process writes a new file, and its cursor starts at that file's `snapshot`. Reuse a path only after truncating it. Never append two subscriptions into one file. When the watched set changes, stop the process first, then start a new process and a new file.

**Subscription Events**

- `subscribe` prints one JSON object per line with `schema_version: 1`, a fresh `subscription_id`, `seq` from 1 (local to the subscription, never a restart cursor), `observed_at`, `event`, `group_id`, `tasks`, and `task_revisions` captured with the committed states and documents.
- The initial event is `snapshot`. State differences emit `state-change` with `changed` items (`task_id`, `from`, `to`); revision differences without state differences emit `task-update`; both may carry `updated` task IDs. A same-refresh review-working-review round trip is visible by revision, but revision alone proves neither completion nor dispatch acceptance. Restart with a new snapshot and compare saved revisions; there is no replay log. Legacy unversioned cards have revision 0.
- With `--watch`, events retain `watch_references`, expand external IDs into `watched`, and include all monitored tasks. Group references carry complete `memberships` and deterministic `membership_versions` (a hash of the sorted member IDs, `empty` for a group emptied after startup).
- Membership changes emit `membership-change`: additions join immediately; removals and ownership changes that remove a member from its former watched group carry `removed` and set `reconciliation_required: true` for the rest of the subscription. Never infer satisfied dependencies from a shrinking set; re-check the contract, membership versions, and actual delivery. `membership_complete: true` and `read_status: committed` describe a complete observation, not authorization to release. Scan problems fail closed with a terminal `membership-unknown` event (`membership_complete: false`, `reconciliation_required: true`, nonzero exit) whose task values are the previous complete snapshot, never fresh facts.
- A terminal `read-error` carries `read_status: maintenance` (lock contention, possibly ordinary writer contention) or `read_status: recoverable` (a known prepared transaction); both set the incomplete flags and exit nonzero without repair. Follow the explicit init maintenance protocol.
- `dispatches`, keyed by task ID, carries the current dispatch summary from the same snapshot. The summary holds `dispatch_id`, `task_id`, `kind`, `epoch`, `state`, dispatch `revision`, original `created_at`, the current epoch's effective `confirm_by`, `age_seconds` measured from creation, and durable `accepted`/`completed` receipts with card revisions and delivery/disposition references. Message bodies are omitted; unbound cards have no invented receipts; superseded IDs stay queryable with `dispatch show`. A same-refresh review-working-review/done round trip remains provable by its same-ID receipts in the next event or restart snapshot; a completion receipt is not proof of Git integration or review closure.
- `confirmation_pending` is true only for prepared/delivery-unknown; `confirmation_overdue` additionally means the current epoch's deadline elapsed. Accepted work has no inferred completion deadline. The deadline wakes the subscription independently of refresh and heartbeat and never resets on same-epoch retries, unrelated events, or restart. At expiry, `dispatch-attention` carries the overdue task IDs in `attention`, current dispatch facts, available `liveness`, and `reconciliation_required=true`; attention never performs recovery, retry, card mutation, or dependency release. Missing or corrupt dispatch evidence for a monitored card fails the read.
- Heartbeats are independent of state, task-update, and membership events. When monitored `working/` cards or pending-dispatch `review/` cards exist, the heartbeat carries `liveness` items. Each item has `agent`, `status`, `channel`, `detail`, and `revision`, `identity`, `observed_at`, `age_seconds`, `runtime_state`, `observation_valid`, `collection_state` (`pending`, `complete`, `not-observed`), `collecting`, `stale`, optional `new_window`, using check's four-state classifier. Revision or identity mismatch discards the cached result. Pending or uncollected results stay unknown. An observation older than heartbeat plus 10 seconds is stale and unknown. Ordinary review and completed cards get no liveness. Probes run in bounded batches, so a heartbeat may report cards still `pending`.
- Output is bounded; overflow, oversized lines, broken pipes, or write timeouts terminate the subscription with an explicit error. Consumers discard partial terminal lines and reconcile a new snapshot after reconnecting. Ctrl+C, termination signals, and caller cancellation stop all owned work; the command does not exit automatically when all cards are done.

## Output

- One JSON object per line on standard output, as described above. Standard error carries diagnostics only.

## Rejections

- A nonexistent or empty target, a duplicate member, overlapping expansions, or an ownership mismatch fails before subscribing.
- A missing group or member list without `--watch`, a missing `--watch` value, or an unknown option is a usage error.
