# kander subscribe

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Reading the event stream", "Subscription Events")

## Purpose and users

Streams board events for a task group and its watched external cards or groups as one JSON object per line. Used by the orchestrator as its only periodic view of the executing agents; it never polls the board with `list`, `check`, or card reads.

## Syntax

```text
kander subscribe [--follow] [--refresh SECONDS] [--heartbeat SECONDS] (<task-group> <task-id>... [--watch <task-id|task-group-id>]... | --watch <task-id|task-group-id> [--watch <task-id|task-group-id>]...)
```

## Arguments

- `--follow`: keep writing events until the command is stopped, a terminal event, or a failure; it never exits because all cards are done. Without it the command writes the `snapshot`, then exits with status 0 after the first wake-up event: `state-change`, `task-update`, `membership-change`, `member-blocked`, `heartbeat`, `snapshot-reset`, `coordinator-change`, `claim-change`, or a `dispatch-attention` that names a dispatch (ID and epoch) not already `confirmation_overdue` in this subscription's `snapshot`. It writes every event of that refresh before it exits. A terminal event, an output failure, or a signal ends it as under `--follow`.
- `<task-group> <task-id>...`: the group and its complete member set; the command validates ownership.
- `--watch <task-id|task-group-id>`: one external card or group, repeated once per direct reference and re-expanded on each observation; alone it monitors standalone cards without a group. Each reference needs its own `--watch`.
- `--refresh SECONDS`: default 1.
- `--heartbeat SECONDS`: default 900; the heartbeat starts after the snapshot is queued and restarts only after each heartbeat is queued.

## Examples

```text
# Wait for the first event of a group and its members, then exit
kander subscribe 20261118-example-group 20261118-example-task

# Watch a standalone card and keep streaming until stopped
kander subscribe --follow --watch 20261118-example-task
```

## Preconditions

- `subscribe` requires an explicit group ID and non-empty member IDs and validates ownership. A nonexistent or empty target, a duplicate member, or overlapping expansions fail before subscribing.

## Effects

**Waking Up**

- Never end the turn, switch away, or report the cards as watched without a wake-up source: something the runner is certain to deliver to this session at the next event. A background process counts only when the runner reports what wakes you: its exit for a single-event subscription, each output line for `--follow`. A process whose exit or output the runner does not report is not a wake-up source; ending the turn on it stalls the orchestration until someone wakes the session by hand.
- The wait loop is the default. Start `kander subscribe` without `--follow`, with standard output in a new file. It exits with status 0 after the first wake-up event. Read the file, handle every line, then start the next subscription with a new file.
  - A runner that wakes the session when a background process exits (Claude Code: background Bash, `run_in_background`, which reports nothing before the exit) runs each subscription in the background.
  - A runner with no background wake-up runs each subscription in the foreground within the turn, with `--heartbeat` shorter than its command timeout.
- Use `--follow` only when the runner reports each new output line of a background process (Claude Code: the Monitor tool). When the runner ends or expires that watch, the subscription ends with it: handle that as an exit.
- On each restart of the wait loop, pass `--heartbeat` as the full interval after a `heartbeat` event, and after any other event as the seconds left until the next heartbeat is due, at least the smaller of the interval and 15 (a shorter heartbeat reports unfinished probes as `pending`). The next heartbeat is due at the last `heartbeat`, or at the start of the first subscription of the loop when none came yet, plus the interval. A `--follow` subscription keeps its `--heartbeat` for its whole life, so it always gets the full interval.
- Each restart begins with a new `snapshot`. Compare it with the saved card states, `task_revisions`, `blocked`, `membership_versions`, overdue dispatches, `claims` (generation, status, and the status of the agent observation), and `coordinators` (revision, epoch, and the lease generation and status), and handle each difference as the matching `state-change`, `task-update`, `member-blocked`, `membership-change`, `dispatch-attention`, `claim-change`, or `coordinator-change`; a `snapshot-reset` asks for the same full comparison. Extract only the fields you compare; never read a whole `snapshot` line into the conversation (a line may reach 1 MiB).
- Stop a subscription only through the runner's handle for it or its own process ID, never by a command-line pattern such as `pkill -f`, which also hits other sessions and the agent's own shell. Never pipe it through a shell filter such as `grep -m1`.

**Reading the event stream**

A subscription is a `kander subscribe` process plus a file, run as the wait loop of "Waking Up". The loop, in order:

1. Start `kander subscribe ...` as "Waking Up" says, with standard output connected to a new file.
2. Read the file from the cursor, handle every complete line, and advance the cursor.
3. Wait for the wake-up of "Waking Up": the process exit in the default mode, a report of new output under `--follow`.
4. While the process runs under `--follow`, go to step 2. When any process ends, apply the "Exit" judgment below: it decides between a new subscription at step 1, the end of the loop, and a reconnect.

- Start: `kander subscribe` writes one JSON object per line to standard output. In the default mode it exits with status 0 after the first wake-up event; with `--follow` it does not exit while it is watching. Start it through an argv array, with no shell command string. When the runner accepts only a command string, pass each argument as its own quoted word and never concatenate card or user data into that string. The file is a new UTF-8 file that this process alone writes, in the system temporary directory or the agent's own scratch directory, never in the kanban board or a task worktree. If the runner backgrounds the process and captures standard output in its own log file, that log file is the event stream. Standard error is diagnostics, not events.
- Read: the cursor is the byte offset, or the count of complete lines already handled. Each read takes only the content after the cursor. Accept a line only when it ends with a newline and parses as one JSON object. Leave a partial trailing line unread until its newline arrives. Advance the cursor only after that line is handled.
- Handle: the first line is `snapshot`. Later lines are `state-change`, `task-update`, `heartbeat`, `membership-change`, `dispatch-attention`, `member-blocked`, `snapshot-reset`, `coordinator-change`, `claim-change`, or a terminal `membership-unknown` or `read-error`. Apply the event rules in "Subscription Events". Never skip a line because the same fact is also on the board.
- Wait: the wake-up is the one named in "Waking Up". Within a turn, a bounded wait that re-reads only this file past the cursor is allowed. Never poll the board on a timer, and never replace the file with `kander list`, `kander check`, or repeated card reads. No new complete line until the next heartbeat is normal, not a failure. The heartbeat interval is the only periodic check of the executing agents.
- A live process, a background handle, or the runner returning after it backgrounds the process is not an event, and a file that nobody reads does not stop the process. Only reading the file delivers the events. Unread complete lines are pending work: every continuation first handles them.
- Exit, the only judgment of how a subscription ended (every other rule refers here): first read the last complete lines, discard a partial trailing line, and handle the events. Then judge in this order. (1) You stopped it on purpose, in either mode, including the EOF that follows: the loop ends; do not restart, reconnect, or reconcile on account of it. (2) Default mode, exit status 0, and this subscription's file holds a wake-up event after the `snapshot` (in the lines just read or in earlier ones): it is the wake-up, not a failure; continue the wait loop at step 1 with a new file. (3) Anything else is a failure: a terminal event, a nonzero exit, a status-0 exit with no wake-up event (a signal also causes it), an output failure, an EOF, or a `--follow` watch the runner ended. Reconnect per `snippets/orchestrate-sessions.md` "Handling Blocked Executing Agents" and reconcile the new snapshot; a task group also reconciles per `snippets/orchestrate-start.md` "Durable Coordinator Recovery".
- A new process writes a new file, and its cursor starts at that file's `snapshot`. Reuse a path only after truncating it. Never append two subscriptions into one file. When the watched set changes, the next default-mode subscription names the new set; stop a still-running `--follow` subscription first, then start the new process and a new file.

**Subscription Events**

- `subscribe` prints one JSON object per line with `schema_version: 1`, a fresh `subscription_id`, `seq` from 1 (local to the subscription, never a restart cursor), `observed_at`, `event`, `group_id`, `tasks`, and `task_revisions` captured with the committed states and documents.
- The initial event is `snapshot`. State differences emit `state-change` with `changed` items (`task_id`, `from`, `to`); revision differences without state differences emit `task-update`; both may carry `updated` task IDs. A same-refresh review-working-review round trip is visible by revision, but revision alone proves neither completion nor dispatch acceptance. Restart with a new snapshot and compare saved revisions; there is no replay log. Legacy unversioned cards have revision 0.
- With `--watch`, events retain `watch_references`, expand external IDs into `watched`, and include all monitored tasks. Group references carry complete `memberships` and deterministic `membership_versions` (a hash of the sorted member IDs, `empty` for a group emptied after startup).
- Membership changes emit `membership-change`: additions join immediately; removals and ownership changes that remove a member from its former watched group carry `removed` and set `reconciliation_required: true` for the rest of the subscription. Never infer satisfied dependencies from a shrinking set; re-check the contract, membership versions, and actual delivery. `membership_complete: true` and `read_status: committed` describe a complete observation, not authorization to release. Scan problems fail closed with a terminal `membership-unknown` event (`membership_complete: false`, `reconciliation_required: true`, nonzero exit) whose task values are the previous complete snapshot, never fresh facts.
- A board lock wait that exhausts the 2-second read budget does not end the subscription at once: it emits nothing and retries after `--refresh` seconds until such timeouts have lasted 30 seconds without a successful read. A terminal `read-error` carries `read_status: maintenance` (lock contention lasted 30 seconds, usually ordinary writer contention) or `read_status: recoverable` (a known prepared transaction); both set the incomplete flags and exit nonzero without repair. `maintenance` never calls for init: handle it per `snippets/orchestrate-start.md` (reconcile once from current facts, and reopen the subscription once when the committed facts are valid). Only `recoverable` enters the explicit init recovery.
- `dispatches`, keyed by task ID, carries the current dispatch summary from the same snapshot. The summary holds `dispatch_id`, `task_id`, `kind`, `epoch`, `state`, dispatch `revision`, original `created_at`, the current epoch's effective `confirm_by`, `age_seconds` measured from creation, and durable `accepted`/`completed` receipts with card revisions and delivery/disposition references. Message bodies are omitted; unbound cards have no invented receipts; superseded IDs stay queryable with `dispatch show`. A same-refresh review-working-review/done round trip remains provable by its same-ID receipts in the next event or restart snapshot; a completion receipt is not proof of Git integration or review closure.
- `confirmation_pending` is true only for prepared/delivery-unknown; `confirmation_overdue` additionally means the current epoch's deadline elapsed. Accepted work has no inferred completion deadline. The deadline wakes the subscription independently of refresh and heartbeat and never resets on same-epoch retries, unrelated events, or restart. One exception: while the subscription retries after lock wait timeouts, it neither wakes early nor emits `heartbeat` or `dispatch-attention`; the next successful read applies the usual rules. At expiry, `dispatch-attention` carries the overdue task IDs in `attention`, current dispatch facts, available `liveness`, and `reconciliation_required=true`; attention never performs recovery, retry, card mutation, or dependency release. Missing or corrupt dispatch evidence for a monitored card fails the read.
- `blocked`, keyed by task ID, carries the reason of each member the last coordinator reconcile did not accept, for the subscribed group and the watched groups. A change of the set emits `member-blocked`; an event with no `blocked` field means no member is blocked. Handle it per `snippets/failure-recovery.md` "Blocked Group Members".
- Heartbeats are independent of state, task-update, and membership events. When monitored `working/` cards or pending-dispatch `review/` cards exist, the heartbeat carries `liveness` items. Each item has `agent`, `status`, `channel`, `detail`, and `revision`, `identity`, `observed_at`, `age_seconds`, `runtime_state`, `observation_valid`, `collection_state` (`pending`, `complete`, `not-observed`), `collecting`, `stale`, optional `new_window`, using the classifier and result set of `snippets/commands/check.md`. Revision or identity mismatch discards the cached result. Pending or uncollected results stay unknown. An observation older than heartbeat plus 10 seconds is stale and unknown. Ordinary review and completed cards get no liveness. Probes run in bounded batches, so a heartbeat may report cards still `pending`.
- Output is bounded; overflow, oversized lines, broken pipes, or write timeouts terminate the subscription with an explicit error. Consumers discard partial terminal lines and reconcile a new snapshot after reconnecting. Ctrl+C, termination signals, and caller cancellation stop all owned work. A `--follow` subscription does not exit when all cards are done; a default subscription exits after its first wake-up event, and no option exits because all cards are done.

## Output

- One JSON object per line on standard output, as described above. Standard error carries diagnostics only. With `--json` the lines are the same and every diagnostic on standard error is one JSON line: `{"notice":{"code":...,"message":...}}` for a remark, `{"error":{"code":...,"message":...}}` for a failure. A failure before the first line leaves standard output empty and writes one error object. A failure after output started keeps the lines already written and ends standard error with one error object; the exit status is the same as without `--json`.

## Rejections

- A nonexistent or empty target, a duplicate member, overlapping expansions, or an ownership mismatch fails before subscribing.
- A missing group or member list without `--watch`, a missing `--watch` value, or an unknown option is a usage error.

## Distributed project

In a distributed project, membership, revisions, dispatches and blocked members come from the service snapshot. The client resumes the service event cursor and performs no local board scan. Worker observations describe agent presence; an expired lease alone does not prove the agent stopped. A cursor reset emits `snapshot-reset` after a complete refresh, and generation or status changes emit `claim-change`. These schema-1 events include the service cursor and public claims.

Distributed subscriptions also emit `coordinator-change` with the selected groups' checkpoint revisions, authority epochs and public lease generations. Coordinator renewal timestamps alone do not emit a change.

`snapshot-reset`, `coordinator-change`, and `claim-change` are wake-up events: each asks the consumer to compare the new facts, so a default subscription exits after the refresh that wrote one.
