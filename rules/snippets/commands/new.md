# kander new

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" and `KANDER-KANBAN-RULES.md` "Task Scale and Grouping"

## Purpose and users

Creates a card in `backlog/` from the template. Used by the card creator; cards are created only through `new`, never by writing files.

## Syntax

```text
kander new [--large] [--language <agent language>] <feature|bug|chore|research> <slug> <title...>
```

## Arguments

- `type`: `feature`, `bug`, `chore`, or `research`.
- `slug`: lowercase ASCII letters, digits, and hyphens; the task ID becomes `YYYYMMDD-<slug>-task`.
- `title...`: the remaining words form the title.
- `--large`: `SIZE: large` (large-task documents, `report.md` required for completion). Default is `small`.
- `--language <agent language>`: the card `LANGUAGE`; default is the configured `agent_language`.

## Preconditions

- A located board; the task ID must be unique across the board.

## Effects

- Writes `<task-id>/spec.md` with the metadata and the contract template; `new` writes `SIZE` and, for a small card, the IMPLEMENTATION/SUMMARY sections. The caller fills in `GOAL`, `EXPECTED_OUTCOME`, and `ACCEPTANCE_CRITERIA` from confirmed content, in the card's `LANGUAGE`, through `update`.

## Output

- The absolute path of the new card directory.

## Rejections

- A missing type, slug, or title, an unknown option, or an invalid `--language` value is a usage error; an invalid configured `agent_language` is an ordinary failure.
