# kander resume

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Resuming the Original Session", "Taking Over with a New Session")

## Purpose and users

Wakes the original executing agent of a `working/` or `review/` card with a message, or, with `--agent`, hands the card to a brand-new session (a user-authorized takeover), or, with `--adopt`, keeps the running executor of an unfinished start. Used by the orchestrator only through `notify`, which chooses resumption itself; used directly by the user.

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
- `--launcher <launcher>`: as for `start`.
- `--dispatch-id`, `--kind fix|sync|wrap-up`, `--base <full-SHA>`, `--evidence-file <JSON>`: durable dispatch (`snippets/commands/dispatch.md`).

## Preconditions

- The card is in `review/` or `working/`. A plain `resume` needs a `SESSION` record and an execution a launcher started; a claimed or reclaimed card is refused before any dispatch binds. `resume --agent` needs neither.
- `resume --adopt` needs a current execution record in `pending` or `unresolved`, and an executor confirmed alive at the recorded address.
- A card whose agent differs from `EXEC_AGENT` refuses a plain resume (`snippets/commands/start.md` "Card Pins").

## Effects

**Resuming the Original Session**

- `resume` wakes the original agent by the card `SESSION`, preserving context. The launcher is the same as for `start`.
- `resume` does not move the card: the prompt for a `review/` card asks the woken agent to perform the `review -> working` move (plain for an unbound message, ID/epoch-bound for a durable dispatch) before handling the items. After launch the command validates that the new instance is alive; on an immediate exit it cleans up and exits non-zero with the agent output, and only after validation reports `resumed`.
- The full prompt is written to a UTF-8 temporary task file as described in `snippets/commands/start.md` "Start Parameters and Metadata". After the recovery or takeover channel succeeds, the new container address is written back to the card `WINDOW`.

**Taking Over with a New Session**

- By default `resume` keeps the original agent and context. An explicit `--agent <name>` means the user authorizes a takeover: even with the same name, a brand-new session is allocated and the old context is not migrated. The new agent rebuilds progress from the card, task worktree, Git state, and implementation records, then handles the message. It verifies delivery facts against the card, the task branch, and the review originals; it does not reset acceptance criteria, rewrite the contract, or reopen a finding closed by verified evidence unless it cites the specific gap (command, output, commit). Retracting a predecessor's completion claim is a separate `TAKEOVER_AUDIT` entry with the invalidating evidence; the predecessor's claim stays as history.
- The command overwrites `OWNER`/`SESSION`/`WINDOW` with the new agent, session, and launcher, and keeps `STARTED_AT`. In the same transaction it ends the current execution record and opens a new pending one; the new record runs once the agent is alive. A launch that fails restores the record it replaced. After the new agent is alive, it tries to exit and close the original container under the gate of `dismiss`. When that fails it prints `original container kept` with the reason and still succeeds.
- A takeover of a card with an unfinished durable dispatch rotates the epoch per `snippets/commands/dispatch.md`.
- A takeover of a `pending` or `unresolved` record ends that start attempt: the handoff record carries the decision, and a launcher that is still running steps aside with `superseded`.
- A takeover is a launcher, so its late failures follow `snippets/commands/start.md` "Late Failures": the same judgement, the same `started-unresolved` output, and the same exit status. A failed takeover without executor evidence restores the record it replaced, including `unresolved`.

**Foreground and Console Have No Native Resume**

- A session started by `foreground` or `console` has no address and no native resume. These cards get liveness (`kander check`), adoption of an unfinished start, and a takeover. Plain `resume` refuses a card whose `WINDOW` is `foreground` or `console` and whose execution record has a process identity, with the next step in the error. A card without an identity comes from an older binary and is not refused.

**Adopting an Unfinished Start**

- `resume --adopt` starts no executor. Use it when the start attempt has no result and the executor works. Then a takeover would replace a healthy executor.
- The command probes the recorded `SESSION` and `WINDOW`; foreground and console cards are probed by their process identity. It accepts only `alive` and `alive-orphaned`, and every other result refuses. A foreground or console start whose launcher died before it recorded the identity reads `unknown`, and a tmux pane with no session marker reads `unknown`, so those are refused too. Use a takeover in these cases.
- On success it writes `kind=adopt` with the decision and changes the record to `running`. The card stays unchanged. A launcher that is still running steps aside with `superseded`.

## Output

- `resumed`, or for a takeover `cleaned up original container: ...` then `taken over: ...`. A generated dispatch ID is printed before any send when the request is durable and the ID was omitted.
- `adopted: <task-id> window=<address>` for an adoption.
- `started-unresolved: ...` with exit status 3 for a takeover that failed late with executor evidence (`snippets/commands/start.md` "Late Failures"). `superseded: <task-id> ...` with exit status 0 for a takeover another execution overtook.

## Rejections

- A card outside `review/` or `working/`, a missing `SESSION`, a claimed or reclaimed card without `--agent` (the error names `snippets/failure-recovery.md`), a pin violation, an immediate agent exit, or a dispatch conflict.
- `--agent` without `--decision` on a `pending` or `unresolved` record.
- `--adopt` on a record in another status, or with an executor that is not confirmed alive.
- A missing or doubled message, a timeout of 60 seconds or less, a missing task ID, or an unknown option is a usage error. So are `--adopt` with any option except `--decision`, `--adopt` without `--decision`, and `--decision` without `--adopt` or `--agent`.
