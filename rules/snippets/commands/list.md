# kander list

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Lists cards, optionally for one state. Used by card creators and the user; orchestrators monitor through `subscribe`, never by polling `list`.

## Syntax

```text
kander list [--mobile] [backlog|todo|working|review|done|archived|trash]
```

## Arguments

- `state`: one of `backlog`, `todo`, `working`, `review`, `done`, `archived`, `trash`.
- `--mobile`: narrow layout.

## Preconditions

- A located board.

## Effects

- Read-only.

## Output

- One row per card: state, size, time, task ID, title.

## Rejections

- An unknown state, more than one state, or an unknown option is a usage error.
