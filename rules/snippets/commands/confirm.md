# kander confirm

## Purpose and users

Confirms a request card at the revision the submitter read. It is not `kander review confirm`, which confirms a prepared review publication. Only the submitter personally runs this command. Agents and Leaders do not run it, even on the submitter's behalf. An embedding host can authorize the dedicated route separately and limit how it is called, for example to browser sessions.

## Syntax

```text
kander confirm <task-id> --expect-revision <revision>
```

## Arguments

- `<task-id>`: the request card to confirm.
- `--expect-revision <revision>`: required positive revision read from `kander show --json <task-id>`.

## Examples

```text
# Confirm the revision shown when you read the request card
kander confirm 20261126-example-task --expect-revision 3
```

## Preconditions

- A bound project, a non-blank Git `user.name`, and a request card in `backlog/` at the expected revision.
- The normal `todo/` gate passes. Every request card also needs a `CARD_REVIEW:` line in `DISCUSSION`, regardless of size or group membership.
- No non-blank `SUBMITTER_QUESTIONS:` marker in `DISCUSSION` (`snippets/card-content.md`).

## Effects

- Moves the card to `todo/`, freezes its contract, and appends a managed `CONFIRMATIONS` record with the confirmer, server time, and confirmed revision in one transaction.
- A return to `backlog/` preserves the history but requires a fresh confirmation. `move` and `pick` cannot admit a request card to `todo/`.

## Output

- The card location. With `--json`, the fields are `task_id`, `state` (`todo`), `location`, `revision` (the new revision), and `confirmed_by` (the actor in the server's latest record).

## Rejections

- Missing or invalid argument or revision: usage error, before sending the request.
- Missing Git `user.name`: no request is sent.
- Stale revision: `revision_conflict`. Read the current card before deciding again.
- Ordinary card, state other than `backlog/`, open questions, or failed `todo/` gate: `board_rejected`.
- Host denial: `forbidden`. A configured host hook that supplies no actor: `attribution_required`. Invalid confirmer: `invalid_request`.
