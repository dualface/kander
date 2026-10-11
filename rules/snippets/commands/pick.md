# kander pick

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract"

## Purpose and users

Moves a `backlog/` card into `todo/`. Used by the card creator right after the card checks, and by the orchestrator just before each `start`.

## Syntax

```text
kander pick [task-id]
```

## Arguments

- `task-id`: the card; without it, the command lists the `backlog/` cards and asks which to pick. Automation passes the ID.

## Preconditions

- The same gate as `kander move <task-id> todo`: `SELF_REVIEW:` and, when required, `CARD_REVIEW:` records and complete contract sections.

## Effects

- `kander pick [task-id]` moves a `backlog/` card into `todo/` through the same gate as `kander move <task-id> todo`. After `todo/` the contract is frozen.

## Output

- The new path of the card.

## Rejections

- A card outside `backlog/`, a failed gate, or an unknown option.
