# kander show

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract"

## Purpose and users

Prints one card. Every agent that edits a card first obtains its current revision here (`KANDER-KANBAN-RULES.md` "Entries and Documents").

## Syntax

```text
kander show [--json] <task-id>
```

## Arguments

- `task-id`: the card.
- `--json`: machine-readable output.

## Preconditions

- A located board that contains the card.

## Effects

- Read-only.

## Output

- `kander show` prints the current state and absolute path before the card body; `--json` returns the committed `text`, `revision`, `operation_id`, and `entry` location.

## Rejections

- A missing task ID is a usage error; an unknown card is an error.
