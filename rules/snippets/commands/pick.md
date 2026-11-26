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

## Examples

```text
# Move a card from backlog to todo
kander pick 20261118-example-task

# List the cards in backlog and choose one
kander pick
```

## Preconditions

- The same gate as `kander move <task-id> todo`: `SELF_REVIEW:` and, when required, `CARD_REVIEW:` records, complete contract sections, and an `INTEGRATION_TARGET` line, if present, that is not repeated and, when it has a value, holds a valid branch name (`snippets/git-integrate.md` "Integration Target").

## Effects

- `kander pick [task-id]` moves a `backlog/` card into `todo/` through the same gate as `kander move <task-id> todo`. After `todo/` the contract is frozen.

## Output

- The location of the card. With `--json`: `task_id`, `state` (`todo`), `location`, and `revision`. Without a task ID the command asks which card to pick, so `--json` is refused with `interactive_required` and exit status 2.

## Rejections

- A request card is refused with `confirmation_required`. Only its submitter can admit it to `todo/` with `kander confirm`.
- A card outside `backlog/`, a failed gate, or an unknown option.
