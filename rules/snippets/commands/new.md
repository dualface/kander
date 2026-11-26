# kander new

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" and `KANDER-KANBAN-RULES.md` "Task Scale and Grouping"

## Purpose and users

Creates a card in `backlog/` from the template. Used by the card creator; cards are created only through `new`, never by writing files.

## Syntax

```text
kander new [--large] [--language <agent language>] <feature|bug|chore|research> <slug> <title...>
```

## Additional Syntax

```text
kander new [--large] [--language <agent language>] [--description-file <UTF8-file>] <feature|bug|chore|research> <slug> <title...>
```

## Arguments

- `type`: `feature`, `bug`, `chore`, or `research`.
- `slug`: groups of lowercase ASCII letters and digits joined by single hyphens, with no hyphen at either end; the task ID becomes `YYYYMMDD-<slug>-task`.
- `--large`: `SIZE: large` (large-task documents, `report.md` required for completion). Default is `small`.
- `--language <agent language>`: the card `LANGUAGE`; default is the configured `agent_language`.
- `--description-file <UTF8-file>`: read the submitter's description from this file, at most once. The file must contain non-blank UTF-8 text and be at most 131072 bytes before normalization.
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
- With `--description-file`, creates a request card. Kander writes the description to the protected attachment `request.md` and adds `- REQUEST: request.md` immediately after `SUBMITTER` in the same operation. It replaces CRLF with LF and otherwise preserves the text. Omitting the option creates an ordinary card.
- Leaves `ASSIGNEE` empty. The client reports the current directory's `git config user.name`, or an empty value if unavailable. The service uses that value for `SUBMITTER` unless the embedding host supplies caller attribution, in which case its value takes precedence. The same rule applies to `kander issue import`.

## Output

- The location of the new card. With `--json`: `task_id`, `state` (`backlog`), `location`, and `revision`.

## Rejections

- A missing type, slug, or title, an unknown option, or an invalid `--language` value is a usage error; an invalid configured `agent_language` is an ordinary failure.
- An unreadable, empty, blank, oversized, or non-UTF-8 description file fails without sending a creation request. The service rejects a description that exceeds 65536 bytes after CRLF normalization or contains a C0 control other than TAB or LF, DEL, or any C1 control. A lone CR is not allowed.
- A configured attribution hook that does not identify the caller refuses creation with `attribution_required`. The selected `SUBMITTER` must be a single line without control characters and at most 256 UTF-8 bytes after trimming leading and trailing whitespace. An invalid value returns `board.person_field_invalid`.
