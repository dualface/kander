# kander notify

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Notify and Recovery" and the probe classification)

## Purpose and users

The single interface for the orchestrator to dispatch items back to the original executing agent: review findings (`--kind fix`), task branch synchronization (`--kind sync`), and post-integration wrap-up (`--kind wrap-up`). The orchestrator does not call `resume` separately; resumption is chosen inside `notify`.

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

## Preconditions

- The card is in `review/` or `working/`. A durable `fix` or `wrap-up` request carries its evidence file per `snippets/commands/dispatch.md`.

## Effects

**Delivery and Recovery**

- Delivery goes to the explicit `--pane` override, else the card `WINDOW`, else a session reverse lookup. The target must prove the card's session identity and be idle, done, or working (a working agent handles the message after its current turn). Any other live state is busy per the probe classification below and never recovers, while an unusable target falls back to recovery. Session markers avoid misdelivery within same-user permissions and are not a security boundary.
- The agent receives only one line starting with `# kander-notify:` (also recognized as `# onevoke-notify:`) naming the message file; the file starts with the acknowledgement line the agent outputs exactly before anything else. A successful delivery returns success without moving the card; the message for a `review/` card is prefixed with the requirement to perform the `review -> working` move first (plain for an unbound message, ID/epoch-bound for a durable dispatch). A marker timeout only warns "delivered, not confirmed within timeout" and recovers no second process.
- Only foreground/console, no direct channel, or a failed probe or delivery make the command recover the original session internally; when both paths fail it exits non-zero with both reasons and the card is unchanged.
- Neither `notify` nor `resume` moves the card. In the unbound compatibility flow, the notified or woken executing agent itself performs `review -> working` with a plain `kander move <task-id> working` before handling the items; that move is the legacy "received and started" acknowledgement. Durable dispatch instead requires its atomic accepted receipt.

**Probe Classification**

- When `notify` probes the recorded `WINDOW`, it distinguishes four target classes. A ready target: the agent is idle, done, or working. A working agent receives the message and handles it after its current turn, so the acknowledgement may not arrive within `--timeout`. A busy target: the agent is alive but in any other state, such as blocked on a dialog. The probe is retried within `--timeout`, then the command returns non-zero with "target agent busy, not delivered" and changes nothing. A stale address: the pane is gone or its identity mismatches. A unique session reverse lookup rewrites the address and delivers; old tmux cards with an empty `WINDOW` get no lookup. A failed lookup: 0 or multiple hits. Recovery runs through the existing chain and reports both reasons. This classification takes precedence over `snippets/failure-recovery.md` "Failure Recovery"; an explicit `--pane` override does no lookup.

**Codex Identity Repair**

- A missing Codex direct identity may be repaired automatically before normal notify or unaccepted durable dispatch delivery. The repair needs a backend and platform that can prove the current pane's foreground process and its open task-bound rollout. That rollout must have a session header identifying the physical launch directory. Kander rechecks process/file identity, card revision and execution epoch, reports through the backend, verifies read-back, and persists an old agent-only SESSION through a controlled transaction. If a previous report took effect before card persistence failed, retry verifies the existing direct ID against the same current-process evidence and only finishes card binding; conflicting evidence stays uncertain and cannot trigger recovery. The repair shares the existing notification/acceptance budget and cancellation; existing conflicts, unknown kinds and unresolved native path identities are never overwritten. Unsupported or ambiguous evidence preserves the executor and pending dispatch with a specific diagnostic. Repair may update verified identity metadata even when normal readiness still blocks delivery; it never bypasses blocked state, extends a dispatch deadline, fabricates receipts, installs/replays hooks, or starts a second executor.

## Output

- Delivery and confirmation results; a generated dispatch ID before any send when omitted; after recovery or takeover the new container address, which is also written back to `WINDOW`.

## Rejections

- A card claimed or reclaimed with `--owner`: it has no executor session. The refusal comes before any dispatch binds and names `snippets/failure-recovery.md`.
- A card whose start attempt has not ended (current execution record `pending` or `unresolved`): the error names adoption and takeover (`snippets/commands/start.md` "Late Failures").
- "target agent busy, not delivered" after the probe retries; both reasons when direct delivery and recovery fail; dispatch conflicts per `snippets/commands/dispatch.md`.
- A missing or doubled message, a timeout of 60 seconds or less, a missing task ID, or an unknown option is a usage error.
