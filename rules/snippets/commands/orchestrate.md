# kander orchestrate

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" (syntax) and `KANDER-KANBAN-RULES.md` "Orchestrator Sessions" (preconditions)

## Purpose and users

Hands a confirmed plan with two or more cards, or with at least one task group, to a separate orchestrator session that advances and reports them in its own window. Used by the card creator under intake option 2; a plan of exactly one standalone card uses `start` instead.

## Syntax

```text
kander orchestrate [--agent <configured-agent>] [--launcher <launcher>] (--message TEXT | --message-file FILE) <task-id|task-group-id>...
```

## Arguments

- `task-id|task-group-id...`: the cards and groups in plan order; a group ID expands to its current members.
- `--message TEXT | --message-file FILE`: exactly one; the plan notes (order, parallel cards, groups and their merge-back steps, user decisions).
- `--agent`, `--launcher`: as for `start`; the default agent tier is `large`.

## Preconditions

- Only `backlog/` or `todo/` cards; every `backlog/` card must already pass the `todo` gate.
- No card named twice; every named group fully readable; the task group module enabled when a group is named.

## Effects

- Launches the orchestrator session with the cards and notes. Writes no board state; the orchestrator advances cards with `pick` and `start`. The creating session then stops following the plan.

## Output

- On success the agent, launcher, and container address. With `foreground` the command returns only after the orchestrator exits.

## Rejections

- A launch failure closes the container and changes no card.
- A missing message, both message forms, no task IDs, or an unknown option is a usage error.
