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
- A legacy file card has only `spec.md`; reading any other document fails until `kander init` migrates the card.

## Effects

- Read-only. The revision, the entry, and the document text come from one read-only transaction, so the revision always describes the text.
- Reads only regular files inside the card directory. A symlink or reparse point, as the file or as any parent directory, is rejected and never followed.

## Output

- `kander show` prints the current state and absolute path before the card body; `--json` returns the committed `text`, `revision`, `operation_id`, and `entry` location.
- With `--document`, `kander show` prints only the document text, byte for byte, without the state and path header. The text of `spec.md` equals the `text` of `kander show --json`.
- With `--document` and `--json`, one JSON object and a newline with exactly `path` (the requested relative path), `text`, `revision` (the card revision, the one `kander update --expect-revision` expects), and `entry` (the same form as in `kander show --json`).

## Rejections

- A missing task ID, a task ID that is an option, more than one task ID, a `--document` without a value, or a repeated `--document` is a usage error; an unknown card is an error.
- A path that is not canonical, or a hidden, absolute, or traversing path, fails with `board.transaction_invalid`.
- A missing document fails with `board.document_not_found` and names the path. A directory, a symlink, or a document that is not valid UTF-8 fails; nothing is printed.
- A document other than `spec.md` of a legacy file card fails with `board.transaction_directory_required`.
