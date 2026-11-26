# kander resume

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Resuming the Original Session", "Taking Over with a New Session")

## Purpose and users

Wakes the original executing agent of a `working/` or `review/` card with a message. With `--agent` it hands the card to a brand-new session (a user-authorized takeover); with `--adopt` it keeps the running executor of an unfinished start. Used by the orchestrator only through `notify`, which chooses resumption itself; used directly by the user.

## Syntax

```text
kander resume [--agent <configured-agent> [--decision <reference>]] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--launcher <launcher>] [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
kander resume --adopt --decision <reference> <task-id>
```

## Arguments

- `task-id`: the card.
- `--message TEXT | --message-file FILE`: exactly one, non-empty.
- `--timeout SECONDS`: a finite number greater than 60; default 120.
- `--agent <configured-agent>`: takeover by a new session (see "Taking Over with a New Session").
- `--adopt`: keep the running executor and start nothing (see "Adopting an Unfinished Start"). It takes only the task ID and `--decision`.
- `--decision <reference>`: the user decision. Required with `--adopt`, and with `--agent` when the current execution record is `pending` or `unresolved`. `--agent` on a `running` record may omit it. Kander records the reference and does not verify it.
- `--launcher <launcher>`: as for `start`. Follow `snippets/commands/start.md` "Choosing the Launcher".
- `--dispatch-id`, `--kind fix|sync|wrap-up`, `--base <full-SHA>`, `--evidence-file <JSON>`: durable dispatch (`snippets/commands/dispatch.md`).
- `--release`: with `--decision`, ends the lease of a stopped local execution without changing the card content; it takes only the task ID and `--decision`. See "Additional Syntax".

## Examples

```text
# Wake the agent of a card with a message
kander resume --message "Please continue" 20261118-example-task

# Take a card over with a new session
kander resume --agent codex --decision user-2026-11-19 --message "Continue the work" 20261118-example-task

# Keep the running executor of an unfinished start
kander resume --adopt --decision user-2026-11-19 20261118-example-task

# Release the stopped execution of a card
kander resume --release --decision user-2026-11-19 20261118-example-task
```

## Preconditions

- The card is in `review/` or `working/`. A plain `resume` needs a `SESSION` record and an execution a launcher started; a claimed or reclaimed card is refused before any dispatch binds. `resume --agent` needs neither.
- A plain `resume`, with or without a dispatch option, needs a finished start. With a current execution record in `pending` or `unresolved` it is refused before any dispatch binds: a second process on that session would rewrite `WINDOW`, and adoption and takeover would then use the wrong pane. Nothing starts, and the card and the record stay unchanged. While the launcher runs, wait for it. Otherwise use `kander resume <task-id> --adopt --decision <reference>` or `kander resume <task-id> --agent <agent> --decision <reference>`. The error, and the same refusal of a session repair by `notify`, names adoption only when it can work. It names only the takeover for an old card shape and for an unmarked pane ("Adopting an Unfinished Start"); for an unmarked pane it first tells you to stop the old executor in `WINDOW` by hand.
- `resume --adopt` needs a current execution record in `pending` or `unresolved`, a card that still shows the `OWNER` and `SESSION` of that start attempt, and an executor confirmed alive at the recorded address.
- A card whose agent differs from `EXEC_AGENT` refuses a plain resume (`snippets/commands/start.md` "Card Pins").

## Effects

**Resuming the Original Session**

- `resume` wakes the original agent by the card `SESSION`, preserving context. The launcher is the same as for `start`.
- `resume` does not move the card: the prompt for a `review/` card asks the woken agent to perform the `review -> working` move (plain for an unbound message, ID/epoch-bound for a durable dispatch) before handling the items. After launch the command validates that the new instance is alive; on an immediate exit it cleans up and exits non-zero with the agent output, and only after validation reports `resumed`.
- The full prompt is written to a UTF-8 temporary task file as described in `snippets/commands/start.md` "Start Parameters and Metadata". After the recovery or takeover channel succeeds, the new container address is written back to the card `WINDOW`.

**Taking Over with a New Session**

- By default `resume` keeps the original agent and context. An explicit `--agent <name>` means the user authorizes a takeover: even with the same name, a brand-new session is allocated and the old context is not migrated. The new agent rebuilds progress from the card, task worktree, Git state, and implementation records, then handles the message. It verifies delivery facts against the card, the task branch, and the review originals; it does not reset acceptance criteria, rewrite the contract, or reopen a finding closed by verified evidence unless it cites the specific gap (command, output, commit). Retracting a predecessor's completion claim is a separate `TAKEOVER_AUDIT` entry with the invalidating evidence; the predecessor's claim stays as history.
- The command overwrites `OWNER`/`SESSION`/`WINDOW` with the new agent, session, and launcher, and keeps `STARTED_AT`. In the same transaction it ends the current execution record and opens a new pending one; the new record runs once the agent is alive. A launch that fails restores the record it replaced. Before the new agent starts, it searches a stale `WINDOW` by session, and it resolves a Codex card with no reference first, because the new session would match afterwards. After the new agent is alive, it tries to exit and close the original container under the gate of `dismiss`: the container of the drifted pane when the search found one, otherwise the recorded one. The new agent's address is never a target. When that fails, or when the search did not finish or found several matches, it prints `original container kept` with the reason and still succeeds. A kept drifted pane is named by its own address.
- A takeover of a card with an unfinished durable dispatch rotates the epoch per `snippets/commands/dispatch.md`.
- A takeover of a `pending` or `unresolved` record ends that start attempt: the handoff record carries the decision, and a launcher that is still running steps aside with `superseded`. This is the way out when the starter and the executor have ended. A task group member in `review/` takes this takeover without a dispatch option. With a `running` record, that member refuses `resume --agent` without a dispatch option.
- A takeover is a launcher, so its late failures follow `snippets/commands/start.md` "Late Failures": the same judgement, the same `started-unresolved` output, and the same exit status. A failed takeover without executor evidence restores the record it replaced, including `unresolved`.

**Foreground and Console Have No Native Resume**

- A session started by `foreground` or `console` has no address and no native resume. These cards get liveness (`kander check`), adoption of an unfinished start, and a takeover. Plain `resume` refuses a card whose `WINDOW` is `foreground` or `console`, and a relaunch through either launcher, with the next step in the error. It does not matter whether a process identity was recorded. `resume --agent` is not refused.

**Adopting an Unfinished Start**

- `resume --adopt` starts no executor. Use it when the start attempt has no result and the executor works. Then a takeover would replace a healthy executor.
- The command probes the recorded executor. Foreground and console use the exact local process incarnation. A pane normally needs an `alive` observation. An initial `unknown` can enter marker repair only when the probe found the expected agent running in an unmarked pane. An unmarked pane on an agent-reporting launcher may already read `alive`.
- On a launcher with pane metadata, an existing `SESSION` reference supplies the marker without a rollout scan or a change to `SESSION`. For a bare `codex` session, adoption scans once for a rollout whose working directory and first actual user message match the launch directory and task prompt. Codex-injected startup context does not count as a user message. Its creation time must be at or after the card's `STARTED_AT`, parsed in the client's local zone and shifted back 26 hours. Exactly one matching session is required. When a later execution uses a shared project directory instead of a generation-specific worktree, its local launch receipt must prove the exact task file named in the prompt. A missing receipt or a prompt from an earlier execution cannot prove the session. An existing different marker is never overwritten.
- Before repair writes, adoption reads the card and record again. If the attempt or card changed, it refuses and points to `kander check <task-id>`. Otherwise it writes the missing marker first. A bare Codex session then becomes `codex <id>` in the card and execution record through `session-repair`, then in the local credential. If a write fails midway, rerun `--adopt`: it verifies the existing marker and finishes the remaining writes without a second `session-repair`.
- Adoption refuses when no session can be proven, including zero or multiple Codex matches, a failed scan, a conflicting marker, or a bare session for an agent whose session cannot be rediscovered. If it cannot prove the session or write the marker, stop the old executor by hand before taking over. Agents configured with `session.mode: none` do not need a session marker. Launchers without pane metadata do not enter marker repair.
- An old card shape is refused before any probe runs. A card upgraded from 1.0.0 can keep its start attempt in `pending` after a 1.0.0 takeover rewrote `OWNER` or `SESSION`. The record no longer names the card's executor, so adoption cannot end it. The localized error names the takeover, and the record stays `pending`. A card that leaves `working/` and `review/` while the adoption is being written fails that write with the same error.
- On success it writes `kind=adopt` with the decision and changes the record to `running`. Except for a `session-repair` replacement of `SESSION`, the card stays unchanged. A launcher that is still running steps aside with `superseded`.

## Output

- `resumed`, or for a takeover `cleaned up original container: ...` then `taken over: ...`. A generated dispatch ID is printed before any send when the request is durable and the ID was omitted.
- `adopted: <task-id> window=<address>` for an adoption, preceded by `session-marker-written: <task-id> window=<address> session=<session>` when adoption wrote the missing marker.
- `started-unresolved: ...` with exit status 3 for a takeover that failed late with executor evidence (`snippets/commands/start.md` "Late Failures"). `superseded: <task-id> ...` with exit status 0 for a takeover another execution overtook.
- With `--json`: a message to a live executor is the collaboration request object of `notify`. A launch (takeover or continuation) is `action` (`resume`), `task_id`, `size`, `agent`, `launcher`, `address`, `replayed`, `superseded`, and `request` (the dispatch request queued for the new agent, when there is one); `launch_unresolved` and `interactive_required` are as for `start`. `--adopt` and `--release` are `action` (`adopt` or `release`) and `task_id`.

## Rejections

- A task group `review/` card without a dispatch option, unless the takeover ends an unfinished start.
- A card outside `review/` or `working/`, a missing `SESSION`, a claimed or reclaimed card without `--agent` (the error names `snippets/failure-recovery.md`), a pin violation, an immediate agent exit, or a dispatch conflict.
- A plain `resume` on a `pending` or `unresolved` record: the error names adoption and takeover, or only the takeover for an old card shape or for an unmarked pane that adoption cannot mark. That judgement uses static facts only (a session in `SESSION`, or a `codex` card); the error never runs session discovery and writes nothing.
- `--agent` without `--decision` on a `pending` or `unresolved` record.
- `--adopt` on a record in another status, on a card whose `OWNER` or `SESSION` differs from the start attempt, or with an executor that is not confirmed alive. The last two name the takeover as the next step.
- `--adopt` when a pane session cannot be proven or the marker write fails. The card and credential stay unchanged. The error names the takeover, after stopping the old executor by hand.
- `--adopt` when the card or the attempt changed after the probe and before the marker write. No marker is written; the error tells you to run `kander check <task-id>` and act on its result.
- A missing or doubled message, a timeout of 60 seconds or less, a missing task ID, or an unknown option is a usage error. So are `--adopt` with any option except `--decision`, `--adopt` without `--decision`, and `--decision` without `--adopt` or `--agent`.

## Distributed adoption

With a configured server, `resume --adopt --decision <reference>` uses only this host's private credential for the fixed execution. It fetches the current record and a short-lived service-clock observation ticket, then observes the exact local process incarnation or terminal session. When repair is needed, the client proves the session, rechecks the attempt and card, writes the marker, and writes any needed `SESSION` replacement to the server before updating the local credential. After repair it fetches a fresh revision and ticket and probes again. The final observation must confirm the executor alive. The server checks the ticket, generation and submitted revision in the adoption transaction. A foreign host, lost credential, changed identity, unconfirmed final presence or stale ticket refuses adoption. This path never reads a local board or treats a lease timeout as a stopped executor.

Remote starts retain a durable record/revision cursor. Server restart and later launcher writes cannot erase executor-progress evidence. A late or uncertain launch failure preserves the executor and its task file and records an unresolved start; it does not restore the pre-start document over executor work. A superseded launcher touches no successor. Inspect the same execution before any recovery; repeating `start` cannot create a second executor for a claimed task.

## Additional Syntax

```text
kander resume --release --decision <reference> <task-id>
```

## Distributed restart and explicit release

`resume <task> --release --decision <reference>` ends a stopped local execution's lease without changing the card content or assigning another host. It accepts no message, launcher, agent, adoption, or dispatch options. The client must hold the current private credential and prove the executor stopped using a service-clock observation ticket and exact card revision. The server commits release, audit evidence and the exact receipt together. The task becomes lost until an explicit takeover.

A healthy worker renews working/review ownership even after its agent stops. A fresh `--agent` takeover always needs a decision. An active lease can only be handed off by its own client with private holder proof and a fresh stopped observation. A lapsed (expired) or explicitly released lease permits another client's deliberate takeover. A lapsed lease stays with its holder until then: the worker keeps the agent running through a service outage and renews when the service is back. Every handoff uses a new execution identity, generation, branch and worktree; old requests are superseded.

Plain resume queues a collaboration request while the executor is alive or unknown. A locally proven stopped executor with a finished start can resume its native session through a controlled handoff. The client privately snapshots index changes, worktree changes and non-ignored untracked regular files, verifies the source before handoff, and restores into the new generation. The predecessor remains intact. Partial restore or concurrent source edits block automatic retry. Native resume still refuses foreground/console. Unfinished starts require explicit adoption or takeover.

Bound dispatch context is inspected and retained without business replay. A completed binding is settled atomically with handoff. A newly requested round is queued for the confirmed successor; the launch prompt instructs it to wait read-only for that generation's exact proposal and accept its formal authorization before work.

The implicit native decision and launch input are durable before handoff. On an unknown response, retry the same command to reconcile that attempt. Recovery never turns a settled no-launch attempt into another launch; after inspecting its receipt, a new attempt needs an explicit new `--decision`. A completed retry prints a historical receipt and does not claim that the agent is alive. A later confirmed stop of that exact successful execution permits a new intentional native continuation.
