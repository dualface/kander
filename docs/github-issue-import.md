# GitHub issue import

`kander issue import NUMBER` turns one GitHub issue into a normal Kander card in
`backlog/`, with the untouched issue text stored beside it. The import is
idempotent, bounded, and published in a single board transaction, so a card can
never appear without its source.

```
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments]
                           [--type feature|bug|chore|research] [--large]
                           [--language LANG] [--json]
```

- `--repo` accepts the same `[HOST/]OWNER/REPO` references as the other issue
  commands and is resolved with the same canonical identity confirmation.
- `--comments` also stores the issue discussion. Comments are never fetched
  otherwise.
- `--type` overrides the label mapping (see below); the default is derived from
  the labels and falls back to `feature`.
- `--large` sets `SIZE: large` only. An imported card is always a directory card,
  so a large card can grow `plan.md` and `report.md` later.
- `--language` sets the card's `LANGUAGE`; without it the configured
  `agent_language` is frozen into the card.
- `--json` prints one machine-readable line with `task_id`, `state`, `path`,
  `existing`, `source_key`, `source_url`, and `comments_loaded`.

On the terminal board the same import is available from the issues overlay:
`i` imports the selected issue, or jumps to the local card when it is already
imported, and `I` imports with comments. The overlay marks imported issues with
their task ID and appends "update available" when the remote `updated_at` is
newer than the fetched snapshot. It never overwrites the card on its own.

## Identity and idempotency

The binding key of an imported card is the canonical source key

```
github://HOST/OWNER/NAME/issues/NUMBER
```

built from the repository identity that `kander issue repo` confirmed. The
`source_url` in the snapshot is rebuilt from the same identity; a URL supplied by
the provider is never stored.

The source-key check and the card publication run inside the same exclusive-board
transaction. Repeating an import, importing the same issue from two processes at
once, or importing an issue whose readable task ID is already taken all end with
exactly one card:

- an existing binding returns that card (`existing: true`) without fetching the
  issue again;
- a readable ID collision appends `-<8 hex digits of the source-key hash>`;
- an unrecoverable card is refused instead of silently duplicated.

A card whose import snapshot cannot be decoded stops the import with an error
that points at `kander check`; the card must be repaired or removed first. The
uniqueness check matches `source_key` regardless of the snapshot schema version,
so a card written by a newer Kander still owns its issue and an older binary
never publishes a second card for it.

## Attachments

Every imported card carries two attachments next to `spec.md`:

| Path | Content |
| ---- | ------- |
| `source/github-issue.json` | The schema-versioned machine record of the issue |
| `source/github-issue.md` | The same record rendered for reading |

The JSON snapshot has this shape (schema version 1):

```json
{
  "schema_version": 1,
  "source_key": "github://github.com/owner/repo/issues/42",
  "source_url": "https://github.com/owner/repo/issues/42",
  "fetched_at": "2026-09-11T03:00:00Z",
  "repository": {
    "host": "github.com",
    "owner": "owner",
    "name": "repo",
    "url": "https://github.com/owner/repo",
    "private": false
  },
  "issue": {
    "number": 42,
    "title": "…",
    "state": "open",
    "author": "…",
    "labels": ["bug"],
    "assignees": ["…"],
    "created_at": "2026-09-01T00:00:00Z",
    "updated_at": "2026-09-10T00:00:00Z",
    "body": "…"
  },
  "comments_loaded": true,
  "comments": [
    {"author": "…", "created_at": "2026-09-02T00:00:00Z", "body": "…"}
  ]
}
```

Fields are sanitized and bounded before they are written: terminal escape
sequences, C0/C1 control characters, bidi overrides and invalid UTF-8 are
removed from remote text, and no token or sensitive response header is recorded.
The issue link (`source_url`) is rebuilt from the confirmed identity; the
snapshot also records the `repository.url` that `Repository.Validate` accepted
for that identity, so it carries the same host and path. An issue that exceeds a
bound is rejected with a remediation hint; the snapshot is never truncated.

## Limits

| Bound | Value |
| ----- | ----- |
| Issue body | 512 KiB |
| One comment | 64 KiB |
| Comments | 50 |
| Body plus comments per snapshot | 1 MiB |

## Card contract

The card sections are authored from the confirmed identity and the issue number,
not from the remote text: `GOAL`, `USER_DECISIONS`, `EXPECTED_OUTCOME`,
`ACCEPTANCE_CRITERIA`, `THREAT_MODEL`, `OUT_OF_SCOPE`, and `DISCUSSION` state the
import scope, the untrusted-data rule, and the items the issue cannot decide.
Contracts that do not fit ask the reader to confirm them before the card leaves
`backlog`.

The issue title reaches the card only as its `H1` heading, after the sanitizer
reduced it to a single line. The body and comments stay in the attachments.
Apart from that heading, remote text is never spliced into the card structure:
an issue cannot add a section, rewrite a metadata field, or place
`SELF_REVIEW`/`CARD_REVIEW` records, which would otherwise satisfy review gates.
The imported card never carries an auto-generated review record and always
starts in `backlog`; the normal `backlog → todo` gate applies unchanged.

## Label mapping

The first matching label decides the TYPE, and `--type` overrides it:

| Label (or `type/…`, `kind/…`, `type:…`, `kind:…` prefix) | TYPE |
| -------------------------------------------------------- | ---- |
| `bug`, `defect`, `regression` | `bug` |
| `feature`, `enhancement`, `request` | `feature` |
| `chore`, `docs`, `documentation`, `maintenance`, `cleanup` | `chore` |
| `research`, `investigation`, `spike`, `question` | `research` |
| anything else, or no label | `feature` |

## Handoff to a running task

On the terminal board the issues overlay's `s` key turns the selected issue into
a running task. It first imports the issue when no card exists yet, or locates
the existing backlog card; a card that already left `backlog` is only selected
on the board and the form refuses to start it again.

The form edits `TYPE`, `SIZE`, `GOAL`, `USER_DECISIONS`, `EXPECTED_OUTCOME`,
`ACCEPTANCE_CRITERIA`, `THREAT_MODEL`, `OUT_OF_SCOPE`, and `DISCUSSION`, and
shows `LANGUAGE` as read-only: changing the language means cancelling and
importing the issue again. Validation refuses empty sections, leftover
placeholders, headings or metadata lines inside section bodies, an acceptance
list without an item, and a `GOAL` that dropped the requirement to read
`source/github-issue.md`. Nothing is written while the draft is invalid, so
cancelling or fixing the draft leaves the backlog card and its source
attachment untouched.

After the contract passes validation the form shows the four post-creation
self-review points from the Kanban rules, requires an explicit attestation of
each point, and asks the creator to type the conclusion. One controlled update
publishes the edited contract together with a `SELF_REVIEW: <conclusion>` line
in `DISCUSSION`. The tool never fills that conclusion in, never claims to have
verified the creator's judgement, and never writes a `CARD_REVIEW:` line; large
cards and task-group members stay blocked at the `todo` gate until an
independent agent has actually reviewed the card and its record was added.

The update carries the revision the form read. If another write changed the
card first, the save fails with a revision conflict and the form reloads the
card so the user can reconcile the draft; nothing is overwritten.

When the read-only gate check passes, a final confirmation shows the issue, the
task ID, the agent, the launcher, and the `backlog → todo → working`
transition. Only an explicit confirmation moves the card through the controlled
`backlog → todo` step and then calls the same start path as the board's `s`
key; the card is never moved to `working` first. The TUI starts only background
launchers (`herdr`, `tmux`, `tmux-session`); `foreground` and `console` report
that the CLI must be used instead. A failed start reports the card state that
was actually observed and how to retry, and keeps the issue and the card.

Card files stay the only trusted input. The agent still reads the source
attachment through the card's `GOAL`; the generated task file keeps only the
task ID, the fixed requirements, and the paths, so private issue text and
dynamic attachment paths never enter the shell, `argv`, the environment, or the
top-level task-file instructions.

## Failure and recovery

Publication is one board operation: the card directory, both attachments, the
`spec.md` entry, and the revision update are committed together. If the process
stops in the middle, the journal keeps the operation as `prepared` and ordinary
board commands refuse to run: `kander list`, `kander show`, `kander move`, and
the TUI report `board.transaction_pending`, naming the pending operation, instead
of showing or changing a half-published card. The replay happens in
`kander init` (`MigrateCards` → `recoverMigrationRecords`), not in ordinary
commands. Replay is idempotent, so an interrupted import ends as one complete
card and a repeat `kander issue import` then returns it as `existing`.

## Threat model

An issue can be written by anyone, so its title, body, comments, author names,
and links are untrusted:

- **Card-structure injection** — the title is sanitized to a single line before
  it becomes the card heading, and the contract bodies reject headings,
  metadata and record markers before publication.
- **Prompt injection** — the attachments carry an explicit untrusted-data
  banner and the card's `THREAT_MODEL` section requires treating them as
  evidence, never as instructions.
- **Terminal control** — every rendered and stored remote string is sanitized
  before it can reach the screen, the JSON, or the Markdown.
- **Path traversal** — attachment names are validated card-relative paths; `..`,
  absolute paths, and reparse points are rejected by the board layer.
- **Resource exhaustion** — bodies, comment sizes, comment counts, and the total
  snapshot are bounded, and an over-limit issue publishes nothing.

Import never fetches a link, attachment, or referenced code, and never executes
anything from the issue.
