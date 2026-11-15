# kander show

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract"

## Purpose and users

Prints one card, or with `--document` one document of it. Every agent that edits a card first obtains its current revision here (`KANDER-KANBAN-RULES.md` "Entries and Documents"). It is also the only way to read a card document other than `spec.md`.

## Syntax

```text
kander show [--json] [--document <relative-path>] <task-id>
```

## Arguments

- `task-id`: the card.
- `--json`: machine-readable output.
- `--document <relative-path>`: print this document of the card instead of its body: `spec.md`, `plan.md`, `report.md`, an ordinary attachment, or a file below `reviews/` or `dispatches/`. The path follows the rules of `kander update --document`: relative and canonical, with no `.` or `..` segment, no backslash or colon, no segment that starts with `.` or ends with a space or a dot, and `spec.md` in lower case. Given at most once. An empty value is a path error, not a usage error.

## Preconditions

- A located board that contains the card, in any state.
- A legacy file card has only `spec.md`; any other document reports as missing until offline `kander server maintain --project NAME` migrates the card.

## Effects

- Read-only. The server reads the card revision before the document, so a document that changes in between moves the revision and `kander update --expect-revision` rejects the stale one.
- Reads only regular files inside the card directory. A symlink or reparse point, as the file or as any parent directory, is rejected and never followed.

## Output

- `kander show` prints the current state and absolute path before the card body; `--json` returns the committed `text`, `revision`, `operation_id`, and `entry` location.
- With `--document`, `kander show` prints only the document text, byte for byte, without the state and path header. The text of `spec.md` equals the `text` of `kander show --json`.
- With `--document` and `--json`, one JSON object and a newline with `path` (the requested relative path), `text`, `sha256` (the hex digest of `text`), and `expected_revision` (the card revision, the one `kander update --expect-revision` expects).

## Rejections

- A missing task ID, a task ID that is an option, more than one task ID, a `--document` without a value, or a repeated `--document` is a usage error; an unknown card is an error.
- A path that is not canonical, or a hidden, absolute, or traversing path, is rejected by the server.
- A missing document fails with a not-found error that names the path. A directory, a symlink, or a document that is not valid UTF-8 fails; nothing is printed.
