# kander resume

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Resuming the Original Session", "Taking Over with a New Session")

## Purpose and users

Wakes the original executing agent of a `working/` or `review/` card with a message, or, with `--agent`, hands the card to a brand-new session (a user-authorized takeover). Used by the orchestrator only through `notify`, which chooses resumption itself; used directly by the user.

## Syntax

```text
kander resume [--agent <configured-agent>] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--launcher <launcher>] [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
```

## Arguments

- `task-id`: the card.
- `--message TEXT | --message-file FILE`: exactly one, non-empty.
- `--timeout SECONDS`: a finite number greater than 60; default 120.
- `--agent <configured-agent>`: takeover by a new session (see "Taking Over with a New Session").
- `--launcher <launcher>`: as for `start`.
- `--dispatch-id`, `--kind fix|sync|wrap-up`, `--base <full-SHA>`, `--evidence-file <JSON>`: durable dispatch (`snippets/commands/dispatch.md`).

## Preconditions

- The card is in `review/` or `working/` and has a `SESSION` record; cards without one cannot be resumed, and cards that never went through `start` cannot be taken over.
- A card whose agent differs from `EXEC_AGENT` refuses a plain resume (`snippets/commands/start.md` "Card Pins").

## Effects

**Resuming the Original Session**

- `resume` wakes the original agent by the card `SESSION`, preserving context. The launcher is the same as for `start`.
- `resume` does not move the card: the prompt for a `review/` card asks the woken agent to perform the `review -> working` move (plain for an unbound message, ID/epoch-bound for a durable dispatch) before handling the items. After launch the command validates that the new instance is alive; on an immediate exit it cleans up and exits non-zero with the agent output, and only after validation reports `resumed`.
- The full prompt is written to a UTF-8 temporary task file as described in `snippets/commands/start.md` "Start Parameters and Metadata". After the recovery or takeover channel succeeds, the new container address is written back to the card `WINDOW`.

**Taking Over with a New Session**

- By default `resume` keeps the original agent and context. An explicit `--agent <name>` means the user authorizes a takeover: even with the same name, a brand-new session is allocated and the old context is not migrated. The new agent rebuilds progress from the card, task worktree, Git state, and implementation records, then handles the message. It verifies delivery facts against the card, the task branch, and the review originals; it does not reset acceptance criteria, rewrite the contract, or reopen a finding closed by verified evidence unless it cites the specific gap (command, output, commit). Retracting a predecessor's completion claim is a separate `TAKEOVER_AUDIT` entry with the invalidating evidence; the predecessor's claim stays as history.
- The command overwrites `OWNER`/`SESSION`/`WINDOW` with the new agent, session, and launcher, and keeps `STARTED_AT`. After the new agent is alive, it tries to exit and close the original container under the gate of `dismiss`. When that fails it prints `original container kept` with the reason and still succeeds.
- A takeover of a card with an unfinished durable dispatch rotates the epoch per `snippets/commands/dispatch.md`.

## Output

- `resumed`, or for a takeover `cleaned up original container: ...` then `taken over: ...`. A generated dispatch ID is printed before any send when the request is durable and the ID was omitted.

## Rejections

- A card outside `review/` or `working/`, a missing `SESSION`, a pin violation, an immediate agent exit, or a dispatch conflict.
- A missing or doubled message, a timeout of 60 seconds or less, a missing task ID, or an unknown option is a usage error.
