# kander assign

## Purpose and users

Changes the person responsible for one task, without changing the executing agent.

## Syntax

```text
kander assign <task-id> [<assignee>] [--expect-revision <revision>]
```

## Arguments

- `task-id`: the card.
- `assignee`: the new value of `ASSIGNEE`. Omit it to clear the assignment. The value is a single line with no control characters, at most 256 UTF-8 bytes after trimming leading and trailing whitespace.
- `--expect-revision <revision>`: refuse the write unless this is the current card revision from `kander show --json`.

## Examples

```text
# Assign the card only if its revision is still 7
kander assign 20261118-example-task "Example User" --expect-revision 7

# Clear the assignment
kander assign 20261118-example-task
```

## Preconditions

- The server project contains a writable card outside `archived` and `trash`. The embedding host, if present, can deny the request through its authorization hook.

## Effects

- Writes only the `ASSIGNEE` header field of the card, advances its revision, and emits a `task_changed` event. No claim or contract decision is needed.

## Output

- Without `--json`: the new revision. With `--json`: `task_id`, `assignee` (empty if cleared), and `revision`.

## Rejections

- An invalid value returns `board.person_field_invalid`. A stale revision or policy refusal also rejects the write.
