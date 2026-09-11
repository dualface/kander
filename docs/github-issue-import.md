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
imported, and `I` imports with comments. Selecting a row also loads the issue
content on its own; the "Issue snapshot cache" section describes that path. The
overlay marks imported issues with
their task ID and state, and appends "update available" when the remote
`updated_at` is newer than the fetched snapshot. It never overwrites the card on
its own. The `s` key starts a takeover session instead of opening a form; the
"Takeover session" section describes that path.

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

## Issue snapshot cache

The issues overlay keeps a machine-local copy of every issue content it fetched,
so a reopened overlay paints the last known body and comments before the network
answers:

```
<board>/.kander/caches/issues/v1/<sha256(source_key)>.json
```

The file name is the hash of the canonical source key only; no remote text
reaches the path. The record wraps the same schema-versioned `ImportSnapshot`
the card attachments use and adds the cache version, the source key, the fetch
time and a content digest over the issue update time plus body and comments:

```json
{
  "schema_version": 1,
  "source_key": "github://github.com/owner/repo/issues/42",
  "fetched_at": "2026-09-11T03:00:00Z",
  "digest": "…",
  "snapshot": { }
}
```

A read validates the cache version, the source key and the requested issue, then
runs the stored record through the same sanitizer and bounds as a fresh provider
reply. A missing file, a file above 1 MiB, a damaged record, a mismatched
identity and content that fails a bound are all a miss: the cache never reports
an error and never blocks the list, and the network stays the only authority.

Writes are atomic and private (0600 in a private directory). The cache is
bounded at 200 records and 16 MiB in total, flat across repositories like the
file-name layout; a write prunes the oldest `fetched_at` records until both
bounds hold, evicting a record it cannot read first. A snapshot whose record
would exceed 1 MiB is not cached at all.

Selecting a row requests the issue content in the background, debounced across
one UI tick and limited to one request at a time; a cache hit paints
immediately. When the refresh returns the same content, the scroll position is
kept; when the content changed, the cache is rewritten, the pane repaints and a
short notice reports the update. The cache lives below `kanban/`, is never
committed, and carries no credentials.

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

## Takeover session

The overlay's `s` key no longer edits a card contract. It confirms one takeover
session, started through the same path as the CLI, whose agent investigates the
issue and agrees on the plan with the user before anything is written:

- An issue without a local card opens a confirmation dialog with the resolved
  agent and launcher; `y`/`Enter` starts the session. The dialog is only a
  confirmation: no card is created or moved.
- An issue whose card is already bound jumps to that card on the board and
  reports its state, because that card already carries the contract. A card
  still in `backlog` first offers both exits: `y`/`Enter` jumps, `s` starts the
  same session pointed at that card so its contract can be completed.
- The TUI starts only background launchers (`herdr`, `tmux`, `tmux-session`);
  `foreground` and `console` report that the CLI must be used instead. Every
  request runs through the overlay's background slot and is dropped when its
  dialog sequence or selected issue no longer matches.

```
kander issue triage NUMBER [--card TASK_ID] [--repo HOST/OWNER/REPO]
                           [--agent NAME] [--launcher NAME]
```

Each attempt fetches the issue again and rewrites its evidence below the private
board cache:

```
kanban/.kander/caches/triage/<owner>-<repo>-<number>/issue.json
kanban/.kander/caches/triage/<owner>-<repo>-<number>/issue.md
```

The JSON file is the same bounded, sanitized snapshot the card attachments use;
the Markdown file renders that record for reading. An issue that exceeds the
import bounds is rejected instead of truncated. The evidence is machine-local,
never committed, and carries no credentials; re-running a takeover replaces it,
so a session never works from a stale copy.

The session itself reads and writes no board state. Its prompt is built from the
confirmed identity, the local evidence paths and the installed rules: it carries
the rule-loading instruction, the language directive, a minimal protocol, and
never inlines the issue title, body or comments. The agent reads the evidence as
untrusted data, investigates the issue, and confirms findings and scope with the
user. Only after that agreement does it create the card with
`kander issue import NUMBER`, which keeps the issue binding and the contract
sections described above; a session started with `--card` continues the bound
card instead and never imports a duplicate. The card `SIZE` and whether the work
splits into several cards are the agent's judgement.

A failed start closes the container it created and removes its task file, so it
never leaves a half-started window or prompt behind; the evidence files stay for
the next attempt. The tool never writes `CARD_REVIEW:` and never fills in a
`SELF_REVIEW` conclusion, and an imported card never carries an auto-generated
review record, so a takeover cannot mint the independent record: the normal
`backlog → todo` gate applies unchanged, and large cards and task-group members
stay blocked until an independent agent has actually reviewed the card and its
record was added.

Card files stay the only trusted input for the agent; the generated task file
keeps only the identity, the evidence paths and the fixed requirements, so
private issue text and dynamic attachment paths never enter the shell, `argv`,
the environment, or the top-level task-file instructions.

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
