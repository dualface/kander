# Task decision records

A v2 card may contain audited answers to questions raised while it is being
executed. The server writes decision records. They are separate from the
editable `USER_DECISIONS` and execution notes in the card contract. A human
record attributes the answer to the recorder. A Leader record attributes the
answer to the Leader, with the basis for the delegation recorded separately.

## Mode and routing

The managed `DECISION_MODE` header is `leader` or `assignee`. A card
without the field uses `leader`. With `assignee`, Leader writes are
refused with HTTP 409 and `decision_reserved`. The human route still
uses the embedding host's policy to decide who may write. A mode change
does not need a contract decision file or transfer a task's claim.

All routes below are under `/api/v1/projects/{project}/tasks/{task}`.
The host authorization hook receives each matched method and pattern
separately. A host can allow a signed-in web user to use the human write
route while denying that route to agent or Leader sessions. Without
a host authorization hook, these operations follow the standalone
server's normal trust model. A terminal answer by itself is not a
decision record.

| Method and suffix | Request and response |
| --- | --- |
| `POST /decisions/human` | Records an answer by the recorder. Returns the full record (201). |
| `POST /decisions/leader` | Records a delegated answer. Requires the user's grant. Returns the full record (201). |
| `GET /decisions` | Returns `{"decisions":[...]}` in publication order. |
| `POST /decision-mode` | Takes `{"mode":"leader"}` or `{"mode":"assignee"}`. Returns the task ID, mode, and new revision. |

Write routes require an `Idempotency-Key` but no execution claim. Replaying
a successful write with the same key and input returns the original result
without another revision or event. The resolved recorder is part of the
idempotent write identity. The task list can filter exactly by
`?decision_mode=leader` or `?decision_mode=assignee`. List rows and the
in-process `Server.TaskAccess` both expose the effective mode.

## Record contents

The input to either decision write uses the following fields.

```json
{
  "question": "Which environment should we test?",
  "options": [{"key": "staging", "label": "Staging"}],
  "answer": "staging",
  "recorded_by": "leader-account",
  "channel": "pane",
  "authorization": {
    "granted_by": "project-owner",
    "wording": "Choose a test environment for this task.",
    "date": "2026-11-25"
  }
}
```

The server adds `schema` (currently 1), `id`, `task_id`, `created_at`
(RFC 3339 UTC), and `kind` from the route (`human` or `leader`).
`authorization` is required for Leader records and must be omitted for
human records. Its date is a calendar date (YYYY-MM-DD). The grantor is
supplied from the original delegation, not inferred from the current
recorder's session. These are the Leader's submitted authorization facts,
not an independent verification of the grant.

With no attribution hook, the server uses the non-empty reported recorder.
If a hook returns a recorder, the server ignores the client value. If the
hook cannot identify the recorder, the server refuses the write (403,
`attribution_required`). The CLI uses the current Git `user.name` as its
fallback when the JSON file does not name one.

The question and answer are each limited to 4096 UTF-8 bytes. Supply up to
32 options, with unique keys of up to 128 bytes and labels of up to 512 bytes.
Use an empty array if there were no predefined options. Answers may be option
keys or free text. Verbatim grant wording is limited to 8192 bytes. Recorder
and grantor must be single-line identities without control characters, at
most 256 bytes after trimming surrounding whitespace for validation. Channel
is one of `web`, `cli`, `pane`, or `notify`.

## Storage and CLI

The card revision, `decisions/<id>.json`, its index entry in `DECISIONS`,
and the completed receipt publish in one board transaction. The write emits
`task_changed` and updates the rebuildable task index. Ordinary document
writes cannot add or change these managed fields, sections, or paths.
Archive cleanup leaves the decision files and index intact. Archived and
trashed cards reject decision writes and mode changes.

Use `kander decision record`, `kander decision list`, and
`kander decision mode`. See the [command contract](../rules/snippets/commands/decision.md)
for syntax. `kander show --document decisions/<id>.json <task-id>`
returns an individual record, including after archive.
