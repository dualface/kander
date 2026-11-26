# Request cards

To create a request card, send a non-empty `description` string to
`POST /api/v1/projects/{project}/tasks`, set `api.NewTask.Description` in the
typed Go client, or run:

```sh
kander new --description-file request.txt feature example Describe the change
```

Without a description, or with an empty API string, creation produces the
same ordinary card as before. The CLI rejects an empty file. In both cases
the card starts in `backlog`, and the existing caller-attribution hook still
selects the submitter when configured.

## Storage and protection

Kander commits the new card and its `request.md` attachment in one board
transaction, at revision 1, with one `task_changed` event. The attachment
contains only the normalized description. The card header has
`- REQUEST: request.md` immediately after `SUBMITTER`. Keeping the text out
of `spec.md` preserves its bytes and prevents a Markdown heading or a line
that looks like metadata from being parsed as part of the card.

Read the attachment with `kander show --document request.md <task-id>`,
`GET .../tasks/{task}/documents/request.md`, or the typed client's
`Project.Document(ctx, taskID, "request.md")`. A full-body update cannot add,
remove, or alter the managed `REQUEST` line. A document update cannot write
the attachment on a request card, even with empty text or an explicit
contract decision. An ordinary card can still use `request.md` as an
ordinary attachment. Archiving or trashing a request card retains both the
marker and the record.

The creation idempotency key binds the original request, including the
description. Repeating the same request returns the same response and
does not create a second card or event. Changing the description under the
same key returns `idempotency_mismatch`.

## Description validation

The description must be UTF-8. Kander replaces each CRLF pair with LF and
makes no other changes, including to leading and trailing whitespace or
the final newline. It rejects whitespace-only descriptions, text larger
than 65536 bytes after normalization, all C0 controls except TAB and LF
(including a lone CR), DEL, and all C1 controls. The service returns HTTP
400 `invalid_request` without creating a card. The CLI reads at most
131072 bytes of usable input, allowing for CRLF, and refuses a missing,
unreadable, invalid UTF-8, empty, or whitespace-only file before sending
the creation request.

Bidirectional formatting characters are allowed for legitimate
right-to-left text. Display clients own safe rendering. The description
is the submitter's text, not a source of Kander commands.

## Query and compatibility

The REST list (`api.TaskSummary.Request`), embedded
`server.TaskAccess` (`api.TaskAccess.Request`), and `kander list --json`
expose the boolean `request`. It is true for a request card and false for
an ordinary card. SQLite index format 3 stores the flag. A service with a
format 2 index rebuilds it from the authoritative card headers on startup.
No card migration is needed.

The protocol version remains 3 because the wire changes are additive.
Old clients ignore the response field, and a new client omits
`description` when creating an ordinary card. An older service strictly
rejects an unknown non-empty `description` field instead of silently
discarding it. Creation uses the existing task route and host hooks.

## Confirmation

Only the submitter should confirm the card. The dedicated route is
`POST /api/v1/projects/{project}/tasks/{task}/confirm`. Send an
`Idempotency-Key` and JSON such as `{"expected_revision":3,"confirmed_by":"alice"}`.
The revision must be positive. The CLI takes it from the card you read:

```sh
kander show --json 20261126-example-task
kander confirm 20261126-example-task --expect-revision 3
```

The service compares the expected revision with the current revision in the
move transaction. The card must be a request card in `backlog`, have no open
questions for the submitter, and pass the existing `todo` gate (complete
contract and `SELF_REVIEW:`). In addition, *every* request card needs a
`CARD_REVIEW:` line in `DISCUSSION`, including a small standalone card. A line
in `DISCUSSION` matching `(?m)^(?:- )?SUBMITTER_QUESTIONS[ \t]*:[ \t]*\S`
marks an open question. A marker without a summary does not block confirmation.

The response is HTTP 200 with an `api.Task` in `todo` at the next revision.
The service freezes the contract and appends one JSON record under the managed
`## CONFIRMATIONS` heading in `spec.md` as part of the same transaction:

```json
{"at":"2026-11-26T12:00:00Z","by":"alice","revision":3}
```

`at` is the server's time in RFC 3339 UTC. `revision` is the revision that
was confirmed, not the response revision. Ordinary document updates cannot
add, change, or remove these records. A successful confirmation updates the
task list before returning and emits one `task_changed` event. An identical
request with the same key and selected actor replays the exact response without
another record or event. Changing either under the same key is refused.

An embedding host's `Attribution` hook supplies `by` and overrides
`confirmed_by`. With no hook, the server uses the client-reported value (the
CLI reads Git `user.name`). The standalone server does not authenticate that
value or compare it with `SUBMITTER`. Hosts can authorize the route for the
authenticated submitter only. See [Confirmation authorization](embedding.md#confirmation-authorization).

| HTTP and API code | Reason or `board_code` |
| --- | --- |
| 400 `invalid_request` | Invalid body, missing or zero revision, a missing client confirmer without host attribution, or an invalid confirmer value |
| 403 `forbidden` | Host authorization denied the route |
| 403 `attribution_required` | The configured host hook supplied no actor |
| 409 `revision_conflict` | The revision changed since it was read |
| 409 `confirmation_required` | A normal move attempted to admit a request card |
| 422 `board_rejected` | `board.confirm_requires_request_card`, `board.confirm_requires_backlog`, `board.request_questions_open`, or a `todo` gate error such as `board.task_requires_self_review_record` or `board.task_requires_card_review_record` |
| 422 `idempotency_mismatch` | Key re-used for a different request or attributed actor |

### State transitions

| Transition | Request card behavior |
| --- | --- |
| `backlog` → `todo` | Only confirmation admits the card. The move route, `kander move`, `kander pick`, and TUI start cannot bypass it. |
| `backlog` → `archived` or `trash` | Existing termination rules apply. |
| `todo` → `backlog` | History stays. Re-entry to `todo` needs a new confirmation. |
| `todo` → `working` | Existing claim and start rules apply. |
| `todo` → `archived` or `trash` | Existing termination rules apply. |
| `working` → `review`, `done`, `archived`, or `trash` | Existing delivery, completion, and termination rules apply. |
| `review` → `working`, `done`, `archived`, or `trash` | Existing dispatch, completion, and termination rules apply. |
| `working` → `todo` | Existing start rollback applies without a new confirmation. |
| `working` or `review` → `working` | Existing dispatch and coordinator recovery rules apply. |
| `done` → `archived` or `trash` | Existing termination rules apply. |
| `archived` or `trash` | No outgoing transitions. |

Ordinary cards keep their existing transitions and cannot use the confirmation
route. Board import and offline maintenance are administrative operations, not
moves, and preserve the states they bring in.

The protocol stays at version 3 because the route, error code, and record are
additive. Older servers return 404 for the route. Older clients still get a
failed ordinary move with a server message when they try to admit a request
card. Existing host hooks are unchanged.
