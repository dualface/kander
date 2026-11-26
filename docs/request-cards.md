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
discarding it. The routes and host hooks are unchanged.
