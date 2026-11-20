# kander notify

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Notify and Recovery" and the probe classification)

## Purpose and users

Dispatches items back to the original executing agent. It is the single interface the orchestrator uses for review findings (`--kind fix`), task branch synchronization (`--kind sync`), and post-integration wrap-up (`--kind wrap-up`). The orchestrator does not call `resume` separately; resumption is chosen inside `notify`.

## Syntax

```text
kander notify [--pane HERDR-PANE-ID] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
```

## Arguments

- `task-id`: the card.
- `--message TEXT | --message-file FILE`: exactly one, non-empty, written in the card's `LANGUAGE`.
- `--timeout SECONDS`: greater than 60; default 120.
- `--pane HERDR-PANE-ID`: explicit delivery target; no reverse lookup is performed.
- `--dispatch-id`, `--kind fix|sync|wrap-up`, `--base <full-SHA>`, `--evidence-file <JSON>`: durable dispatch (`snippets/commands/dispatch.md`).
- `fix|sync|review|stop`: with `request`, the kind of work the proposal asks for.
- `message...`: with `request`, the text of the proposal.
- `list`, `show`, `answer`: the subcommands of `request`: list the requests of a card, print one request, and answer one as the live holder.
- `request-id`: the request that `request show` prints or `request answer` answers.
- `accepted|done|rejected`: the answer of the holder.
- `note...`: an optional note of the answer.

## Examples

```text
# Send a plain message to the agent of a card
kander notify --message "Please continue" 20261118-example-task

# Dispatch review findings back to the agent
kander notify --kind fix --dispatch-id dispatch-1 --message-file /tmp/message.md --evidence-file /tmp/evidence.json 20261118-example-task

# Propose a fix to the holder of a card
kander request 20261118-example-task fix "Fix the review findings"

# List the requests of a card
kander request list 20261118-example-task

# Print one request as JSON
kander request show 20261118-example-task request-1

# Answer a request as the holder
kander request answer 20261118-example-task request-1 done "Fixed in the latest commit"
```

## Preconditions

- The card is in `review/` or `working/`. A durable `fix` or `wrap-up` request carries its evidence file per `snippets/commands/dispatch.md`.
- The start attempt has ended. `notify` shares this check with a plain `resume` (`snippets/commands/resume.md` "Preconditions").

## Effects

**Delivery and Recovery**

- Delivery goes to the explicit `--pane` override, else the card `WINDOW`, else a session reverse lookup. The target must prove the card's session identity and be idle, done, or working (a working agent handles the message after its current turn). Any other live state is busy per the probe classification below and never recovers, while an unusable target falls back to recovery. Session markers avoid misdelivery within same-user permissions and are not a security boundary.
- The agent receives only one line starting with `# kander-notify:` (also recognized as `# onevoke-notify:`) naming the message file; the file starts with the acknowledgement line the agent outputs exactly before anything else. A successful delivery returns success without moving the card; the message for a `review/` card is prefixed with the requirement to perform the `review -> working` move first (plain for an unbound message, ID/epoch-bound for a durable dispatch). A marker timeout only warns "delivered, not confirmed within timeout" and recovers no second process.
- No direct channel, or a failed probe or delivery make the command recover the original session internally; when both paths fail it exits non-zero with both reasons and the card is unchanged. A card on `foreground` or `console` has no direct channel and no recovery (`snippets/commands/resume.md` "Foreground and Console Have No Native Resume"), and a recovery through either launcher is refused too, so the command fails with the next step and no second executor starts.
- Neither `notify` nor `resume` moves the card. In the unbound compatibility flow, the notified or woken executing agent itself performs `review -> working` with a plain `kander move <task-id> working` before handling the items; that move is the legacy "received and started" acknowledgement. Durable dispatch instead requires its atomic accepted receipt.

**Probe Classification**

- When `notify` probes the recorded `WINDOW`, it distinguishes four target classes. A ready target: the agent is idle, done, or working. A working agent receives the message and handles it after its current turn, so the acknowledgement may not arrive within `--timeout`. A busy target: the agent is alive but in any other state, such as blocked on a dialog. The probe is retried within `--timeout`, then the command returns non-zero with "target agent busy, not delivered" and changes nothing. A stale address: the pane is gone or its identity mismatches. A unique session reverse lookup rewrites the address and delivers; old tmux cards with an empty `WINDOW` get no lookup. A failed lookup: 0 or multiple hits. Recovery runs through the existing chain and reports both reasons. This classification takes precedence over `snippets/failure-recovery.md` "Failure Recovery"; an explicit `--pane` override does no lookup.

**Codex Identity Repair**

- A missing Codex direct identity may be repaired automatically before normal notify or unaccepted durable dispatch delivery. The repair needs a backend and platform that can prove the current pane's foreground process and its open task-bound rollout. That rollout must have a session header identifying the physical launch directory. Kander rechecks process/file identity, card revision and execution epoch, reports through the backend, verifies read-back, and persists an old agent-only SESSION through a controlled transaction. If a previous report took effect before card persistence failed, retry verifies the existing direct ID against the same current-process evidence and only finishes card binding; conflicting evidence stays uncertain and cannot trigger recovery. The repair shares the existing notification/acceptance budget and cancellation; existing conflicts, unknown kinds and unresolved native path identities are never overwritten. Unsupported or ambiguous evidence preserves the executor and pending dispatch with a specific diagnostic. Repair may update verified identity metadata even when normal readiness still blocks delivery; it never bypasses blocked state, extends a dispatch deadline, fabricates receipts, installs/replays hooks, or starts a second executor.

## Output

- Delivery and confirmation results; a generated dispatch ID before any send when omitted; after recovery or takeover the new container address, which is also written back to `WINDOW`. The object is the collaboration request (`id`, `task_id`, `kind`, `message`, `status`, `target_generation`, `revision`, `created_at`, `updated_at`, and when present `note`, `dispatch`, `card_revision`), with and without `--json`.

## Rejections

- A card claimed or reclaimed with `--owner`: it has no executor session. The refusal comes before any dispatch binds and names `snippets/failure-recovery.md`.
- A card whose start attempt has not ended (current execution record `pending` or `unresolved`): the refusal comes before any dispatch binds, and the error names `kander resume <task-id> --adopt --decision <reference>` and `kander resume <task-id> --agent <agent> --decision <reference>` (`snippets/commands/start.md` "Late Failures"). For an unmarked pane or an old card shape it names only the takeover (`snippets/commands/resume.md` "Adopting an Unfinished Start").
- "target agent busy, not delivered" after the probe retries; both reasons when direct delivery and recovery fail; dispatch conflicts per `snippets/commands/dispatch.md`.
- A card in `review/` that belongs to a task group or carries a binding, with no dispatch option: the error gives the `--kind` usage. `notify` never turns such a message into a fix dispatch.
- A missing or doubled message, a timeout of 60 seconds or less, a missing task ID, or an unknown option is a usage error.

## Distributed requests

With a configured server, notify queues a generation-bound collaboration request for the owning worker and prints its JSON state. A queued or accepted request is not a business dispatch receipt. Dispatch options freeze the typed intent and inspected card revision; reuse the ID to inspect/retry the same material, and use a new ID after changing it. Relative acceptance windows use the service clock and are bounded to 24 hours. A retry retains the original deadline.

The request kind follows the dispatch. A notify without dispatch options is a `note`. A notify with dispatch options carries the kind of its dispatch: `fix`, `sync`, or `wrap-up` (`--kind`, default `fix`). A non-holder `kander dispatch prepare` proposal and a `kander resume` that turns into a notify follow the same mapping. A `note` has no dispatch attached: the executing agent handles the message within the card contract, never runs `kander dispatch prepare` for it, and never uses it as the grant of a dispatch-bound move. A note still carries status, requirements and answers, and can send an idle executing agent back to its unmet acceptance criteria. Formal fix, sync and wrap-up rounds come only from a request with a dispatch. The message must not be blank and must not exceed 64 KiB; the client checks both before it sends. The server must be upgraded before the clients: a server that predates the `note` and `wrap-up` kinds refuses them, and the client then says to upgrade the server first.

The worker resolves the target pane first, which types nothing, and reserves delivery only after that succeeded and while that version and generation remain current. Stale or expired proposals are rejected without typing. Formal preparation by the holder verifies the original proposal and acknowledges it in the dispatch transaction. A plain `kander move <task-id> working` of a task group card in `review/` is refused while the request kernel would still accept such a proposal for `kander dispatch prepare`: the holder runs `kander dispatch prepare` for it, or answers it `rejected`, first (`snippets/commands/dispatch.md`). Inspect the full request with `kander request show <task-id> <request-id>`; do not act on a superseded request or one for another execution generation.

When a new proposal is a `fix` dispatch, the server checks its `evidence.fix` before it creates the request, with the check that `kander dispatch prepare` runs at the holder (`snippets/commands/dispatch.md`). A failed check creates no proposal: the command exits nonzero and prints every problem at once, one line each. A line that belongs to a finding starts with `<run_id>/<finding_id>`, names the field, and gives the provided and the expected value; every missing author original is one line with `finding.run_id`, `finding.finding_id`, `record_id`, `author` and `artifact.path`. The lines never carry the text, the evidence or the basis of a finding. Correct the evidence file and run `kander notify` again without `--dispatch-id`, or with a new dispatch ID: the refused ID keeps its stored refusal, so changed evidence under it is an `idempotency_mismatch`. The holder's `prepare` still checks again and refuses what changed after the creation.

**Holder steps for a proposal**

A request of kind `fix`, `sync`, or `wrap-up` carries a `dispatch` object. It is a proposal, not a dispatch: no dispatch record exists until the holder prepares it, so `kander dispatch show` finds nothing before step 3. The holder, which is the execution agent that holds the credential of the task, handles it in this order:

1. Read the request: `kander request show <task-id> <request-id>`.
2. Write the `dispatch` object of the request, unchanged, to a UTF-8 file with an absolute path, including `created_at` and `confirm_by`. The server compares the intent with the proposal byte for byte, so a copy with another time or field is refused.
3. Run `kander dispatch prepare <absolute-UTF8-intent.json>`. Its output holds `authorization.dispatch_id` and `authorization.epoch`; the request is acknowledged in the same transaction. When it reports "proposal names another card version", follow the Next step in the error.
4. Run `kander move <task-id> working --dispatch-id <id> --execution-epoch <epoch>` with those two values. Its JSON has `replayed`; start the work only for `replayed=false`. For `replayed=true` report the receipt that exists and do not repeat the work.
5. When the work of a `fix` or `sync` proposal is done, run `kander move <task-id> review --dispatch-id <id> --execution-epoch <epoch> --delivery-commit <full-SHA>`. A `wrap-up` proposal ends with `move done --result completed` instead, as `snippets/wrap-up.md` "Executing Agent Wrap-Up" describes; `move review` is refused for it.

A bare `kander move` without these options does not acknowledge the proposal and writes no dispatch receipt; `kander dispatch show` then finds no dispatch, only the request that proposed it. If the proposal should not be done, answer it with `kander request answer <task-id> <request-id> rejected [note...]` instead.

When the resolution fails (the pane reports no session identity, the agent is blocked, the terminal is gone), nothing is typed and the request keeps its state: `open`, or `accepted` with the reservation note when its reservation answer was lost or its local list entry could not be saved. The worker tries again per execution after 30, 60, 120 and 240 seconds, then every 300 seconds, and writes each failure to its `worker.log`. While requests wait behind such a failure, the execution observation reason ends with `delivery blocked before sending since <UTC time>; <n> request(s) waiting (first <request-id>): <reason>`; read it in `claim.observation.reason` of `kander list --json` or in `kander check`. A proposal with an acceptance window gets one or two more attempts in the last 15 to 30 seconds of the window, and one that expires during the block is rejected with the same facts in its note. A late attempt can deliver a proposal with less than 30 seconds of its window left: if `kander dispatch prepare` reports that the acceptance window has elapsed, answer the request `rejected` and propose again with a new dispatch ID. A work round stops starting resolutions once they have used 30 seconds; an execution that waits is judged again at its next work step. A typing that fails after the reservation stays uncertain and is not repeated.

If the worker does not see the answer of its own reservation, it resumes that request on a later renewal and types it once; no action is needed. A request that stays `accepted` with the note `automatic delivery reserved before typing; outcome is uncertain until observed` is either blocked or an uncertain terminal attempt that the worker never repeats. Read the execution observation reason first (`kander list --json`, `claim.observation.reason`), then inspect the pane and the request. A request left `accepted` by an older worker carries the older note `automatic delivery reserved; outcome is uncertain until observed`: the worker does not resume it. The holder inspects it with `kander request show <task-id> <request-id>`, and the sender re-sends it when needed. A reserved proposal whose acceptance window elapsed or whose card version changed is rejected without typing.

Before transport the client saves a private operation receipt. A lost response is reconciled against the original request after restart, including when a successor now holds the task. It never silently retargets pending material. An uncertain terminal attempt remains recorded and is not automatically repeated. A pane override must match the current private local execution; foreign pane addresses are refused before any terminal operation.

## Request command

Proposes a piece of work to the live holder of a card through the shared service. Any client may propose; only the live holder answers. `request list` and `request show` read the requests of a card, and `request answer` lets the holder accept, finish, or reject one. The forms are under "Additional Syntax".

## Additional Syntax

```text
kander request <task-id> fix|sync|review|stop|note <message...>
kander request list <task-id>
kander request show <task-id> <request-id>
kander request answer <task-id> <request-id> accepted|done|rejected [note...]
```

The free text of `<message...>` and `[note...]` is positional: a word that starts with `-` must come after `--`, as in `kander request <task-id> fix -- -dash message`. These v2 collaboration forms share this contract. They use the same service project and never select a local board. A request has a stable identity, revision, and target generation. Any client may propose work. Only the live holder answers with its private execution proof and the observed request revision. `show` reads the exact proposal for an executor's read-only reconciliation, and always prints it as JSON. With `--json`, `request` and `request answer` print the request object of `notify`, `request list` prints `requests` (an array of those objects), and `request show` prints the request object. A transport retry recovers the frozen request, and a changed message is a different logical proposal.

A worker that observes its agent stopped keeps renewing the lease but leaves proposals queued. It never delivers to that stopped scene or launches a replacement automatically. The holder must explicitly continue or release that execution. A handoff supersedes old-generation proposals; a successor needs a new explicit proposal. Request acceptance is transport acknowledgement, not formal dispatch business acceptance or proof of completion. A completing move that releases the claim settles the request of its dispatch as `done`; see `snippets/commands/dispatch.md` "Distributed wrap-up evidence".
