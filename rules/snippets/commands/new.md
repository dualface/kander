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
- `title...`: the words of the title, joined by single spaces. A word that starts with `-` must come after `--`, as in `kander new chore slug -- -dash title`.

## Examples

```text
# Create a small card
kander new feature example-slug Add an example

# Create a large card in a given language
kander new --large --language en bug example-slug Fix the example
```

## Preconditions

- A located board; the task ID must be unique across the board.

## Effects

- Writes `<task-id>/spec.md` with the metadata and the contract template; `new` writes `SIZE` and, for a small card, the IMPLEMENTATION/SUMMARY sections. The command creates the template only. The caller completes the card through `update` per `snippets/card-content.md`, then follows "Post-Creation Self-Review".
- Leaves `ASSIGNEE` empty. The client reports the current directory's `git config user.name`, or an empty value if unavailable. The service uses that value for `SUBMITTER` unless the embedding host supplies caller attribution, in which case its value takes precedence. The same rule applies to `kander issue import`.

## Output

- The location of the new card. With `--json`: `task_id`, `state` (`backlog`), `location`, and `revision`.

## Rejections

- A missing type, slug, or title, an unknown option, or an invalid `--language` value is a usage error; an invalid configured `agent_language` is an ordinary failure.
- A configured attribution hook that does not identify the caller refuses creation with `attribution_required`. The selected `SUBMITTER` must be a single line without control characters and at most 256 UTF-8 bytes after trimming leading and trailing whitespace. An invalid value returns `board.person_field_invalid`.
