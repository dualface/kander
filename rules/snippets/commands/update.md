# kander update

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" and `KANDER-KANBAN-RULES.md` "Entries and Documents"

## Purpose and users

Publishes a card document (the body `spec.md`, `plan.md`, `report.md`, or an ordinary attachment) under compare-and-swap. The only way an agent edits a card; direct file writes are forbidden.

## Syntax

```text
kander update <task-id> --document <relative-path> --file <UTF8-input> --expect-revision <revision> [--contract-decision-file <UTF8-decision>] [--dispatch-id <id> --execution-epoch <epoch>]
```

## Arguments

- `task-id`: the card.
- `--document <relative-path>`: `spec.md` for the body, or another canonical relative path without traversal, symlinks, or reparse points.
- `--file <UTF8-input>`: the replacement content, prepared in a separate UTF-8 file.
- `--expect-revision <revision>`: the revision read from `kander show --json`.
- `--contract-decision-file <UTF8-decision>`: a user-authorized contract change after `todo/`; Kander records the decision and the old and new frozen fields in `CONTRACT_DECISIONS`.
- `--dispatch-id <id> --execution-epoch <epoch>`: required on a card that carries a `DISPATCH_ID`.

## Preconditions

- Obtain the revision from `kander show --json <task-id>` first; prepare the replacement in a separate file.
- After `todo/`, the frozen fields (`GOAL`, `USER_DECISIONS`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, `OUT_OF_SCOPE`, `SIZE`, task group relations) change only with `--contract-decision-file`. One exception: in `working/` or `review/`, an update whose only frozen difference toggles line-start markers of top-level acceptance items between `- [ ] ` and `- [x] ` needs no decision file and records no `CONTRACT_DECISIONS` entry.

## Effects

- Full-body replacements preserve `LANGUAGE`, `OWNER`, `SESSION`, `WINDOW`, the timestamps, `RESULT`, execution metadata, and managed indexes (`REVIEWS`, `*_RESOLVED`, `CONTRACT_DECISIONS`, `LIFECYCLE_DECISION`).
- Never writes managed `reviews/`, `dispatches/`, manifests, indexes, or control records; dedicated producers own them.
- Legacy file cards are read-only: the command fails before side effects and directs the caller to `kander init`.

## Output

- The new revision.

## Rejections

- A revision conflict: read and reconcile the current document before retrying.
- A frozen-field change without a decision file, a managed path, a non-canonical path, an archived card, or a bound card without the grant.
- A missing task ID, an option without a value, or an unknown option is a usage error.
