# Card Transactions, Controlled Updates, and Recovery

The task ID is the identity; a path is only the result of this lookup. `board` relocates while holding the lock and does not use cached paths for unconditional writes after asynchronous completion. State is still determined solely by the seven state directories; the control directory keeps no second source of state truth.

## Commands

```sh
kander show --json <task-id>
kander update <task-id> --document spec.md --file <UTF8-input> --expect-revision <revision>
kander update <task-id> --document plan.md --file <UTF8-input> --expect-revision <revision>
kander move <task-id> working --owner codex
kander move <task-id> done --result completed
kander move <task-id> archived --result cancelled --reason <reason> --decision <user-decision-reference>
kander move <task-id> trash --result trashed --reason <reason> --decision <user-decision-reference>
```

The `show --json` object contains `entry` (TaskID/State/Path/Document/Kind), `revision`, `operation_id`, and `text`. A legacy card reads as revision 0 on first read and has no operation ID yet; each successful mutation increments it by 1, and creating a new card also counts as one commit. Clients must not derive versions from timestamps. The input file is a standalone UTF-8 file; do not edit the existing card directly and then call update.

New cards are always directories; both small and large use `--document spec.md` and may write ordinary attachments. Legacy file cards are read-only; mutations require running the init migration before any side effects. Ordinary attachments support relative directories, but not path escapes, the hidden control directory, case-variant body aliases, trailing dots/spaces, symbolic links, or reparse points. Machine artifacts such as `reviews/`, `dispatches/`, and manifest/index/checkpoint files are written by dedicated producers.

Full-text updates preserve LANGUAGE, the managed identity/time/result fields, and the machine index. TASK_BRANCH, IMPLEMENTATION, and SUMMARY may be updated through the body. After todo, the contract, task group, dependencies, and SIZE must not be modified arbitrarily; moving back to backlog does not lift the freeze either. An explicit user decision may revise them with `--contract-decision-file <UTF8-decision>` under the same expected revision; the protected CONTRACT_DECISIONS section of the body stores the time, the original decision text, and the fields before and after the change. The tool only records the basis; it does not infer or validate user intent.

A manual claim's `--owner` writes OWNER/STARTED_AT together. On completion, first update the summary/report, then move done --result completed; RESULT/FINISHED_AT and the state commit atomically. Termination requires result, reason, and decision; duplicate additionally requires `--duplicate-of`. Moving completed from done to archived also requires a user decision reference. move accepts `--expect-revision`.

## Locks and Cross-Package API

Control files live in `kanban/.kander/` and do not move with cards:

- `locks/board.lock`: shared for ordinary reads and writes; exclusive for new, move, migration, and recovery.
- `locks/<group-id>.lock`, `locks/<task-id>.lock`: sort group IDs first, then task IDs. Readers share, writers are exclusive. Lock files are stable inodes/handles, never replaced or deleted.
- `locks/journal.lock`: acquired briefly after the board/group/task locks; shared for global journal enumeration and reads, exclusive for atomic prepared/committed publication, partition migration, and retention cleanup. The lock covers from temporary-file creation until all read/write handles are closed; while holding this lock, no board/group/task locks are acquired.
- `versions/<task-id>.json`: `{revision, operation_id, contract_frozen}`. A legacy card with the file missing is equivalent to revision 0.
- `operations/pending/<operation-id>.json`: a durable record written before the writes; after the commit completes, it is atomically moved into `operations/committed/`, with the record fields and before/after images unchanged. Normal disk reads parse only pending records and legacy root-directory records not yet sorted into partitions, and do not open committed content; they still enumerate each partition, checking file names, object types, duplicate IDs, and reparse points.
- `groups/<group-id>/...`: group control documents for later producers; not scanned as cards.

POSIX implements shared/exclusive locking with flock; Windows implements it with LockFileEx, and new locks and control files receive private permissions/DACLs at creation through internal/fs. Paths are validated component by component, and lock handles are held until the commit/read finishes. Asynchronous Agent/terminal operations do not hold file locks for long periods.

`ScanWithWarnings` / `ReadSnapshotWithWarnings` accept an operation-level `WarningLog`, collect log notices as deduplicated messages, and do not run external callbacks while holding locks. The version cursor of a returned Entry continues to carry that log; subsequent reads, launch migration, metadata write-back, successful original-artifact writes, and failure rollback use the same collector. Default entry points keep the CLI stderr notices; BoardPayload/TaskPayload return messages through the optional `warnings` field and launch Start/PreviewStart through Warnings, displayed by the TUI notification bar, confirmation dialogs, and pendingWork's result display; background calls do not write to the terminal directly.

`board.ReadSnapshot(root,id)` returns a consistent body, location, and version. `Scan`/`ScanTargets` return Entries carrying operation-local version cursors; `ReadDocument` rejects stale Entries. `MoveEntry` and window's compatibility functions keep their call forms, but writes require a valid Entry version cursor; a hand-constructed Entry does not constitute write authorization.

`board.WithTransaction(root, LockScope, callback)` is the multi-file producer entry point. LockScope declares Tasks, Groups, ExclusiveBoard, and ReadOnly in one go. Inside the callback, use only Transaction methods; do not nest calls to board APIs that would re-acquire locks:

- `Snapshot` / `Expect`: relocate while holding locks, validating the state and the expected revision.
- `Read` / `Put`: read the same committed snapshot; stage a body or an attachment. Each file in one transaction is staged only once, and each card's revision increases only once.
- `ReadGroup` / `PutGroup`: control files protected by the declared group locks; the producer validates its own schema and the expected document version.
- `Relocate`: stage a state-directory rename under ExclusiveBoard.

Put may create an attachment's parent directories, but does not create the root directory of an existing task. Dedicated producers may write managed directories, while the update command applies additional body/attachment protection. When the callback returns an error, no data has been published yet. Multi-task lock ordering avoids reverse-order bulk-write deadlocks; long-running reviews acquire these locks only briefly at publication time.

`window.WriteDocument` delegates to `board.WriteManagedDocument`; on launch failure, `RollbackDocument` restores the original text and original state within a single transaction. The operation cursor advances only with its own successful writes. When a new execution record, a migration, or another command changes the revision, the old rollback conflicts explicitly; it does not overwrite the new content and does not resurrect old paths. A later execution-round protocol may add a durable epoch on top of Expect; this layer's current concurrency tokens are the revision and the expected state.

## Recovery Format and Visibility

A schema 1 operation record contains:

```json
{
  "schema": 1,
  "operation_id": "<random 128-bit identifier>",
  "phase": "prepared",
  "revisions": {"<task-id>": 2},
  "groups": [],
  "directories": [],
  "files": [{"path": "working/<task-id>/report.md", "after": "report text"}],
  "entries": []
}
```

A missing `files.before` means create-only; a present value means the file must exist and match the old content. File contents are stored as strings, with JSON escaping arbitrary UTF-8 text. In `entries`, from/to are board-relative paths, and kind is the legacy schema 1 physical-form encoding (small means file, large means directory), not the task size of Entry.Kind; a missing from means new, and text stores the creation body or the expected body after the move. All paths must belong to the tasks/groups listed in the record.

Commit order: the prepared record is written to pending and persisted; attachment directories; files; entry creation/migration; versions; the committed marker is atomically rewritten under the journal exclusive lock, then the record is atomically moved into the committed partition. When interrupted after the phase rewrite but before the rename, a committed record inside pending means the data and versions are already committed; init only completes the rename and does not re-apply the old images. Files and versions are atomically replaced through internal/fs, creation uses create-only semantics, and renames reject existing targets. Recovery testing at this stage targets process kill/restart; it does not claim to provide filesystem durability guarantees after arbitrary hardware power loss.

Readers holding locks never see the in-flight multi-file intermediate state. If the process is interrupted and releases its locks, the prepared record makes read commands report an explicit pending-recovery error. Readers do not repair automatically. Scan/ScanTargets capture entries and bodies within the same lock; list/TUI/subscription/dependency checks consume committed snapshots through Board.Document, do not mix new paths with old bodies because of a migration that happens afterwards, and do not return partial member sets. A new scan still fails explicitly on unfinished transactions. `kander init` acquires the board exclusive lock and then completes the redo according to the record; already-completed steps are confirmed by matching content/versions, unfinished steps continue, and recovery is repeatable. Unknown versions, content conflicts, duplicate entries, and reparse points all fail closed and preserve the scene. When a new card's directory has been created but the spec is missing, the body is completed only under a valid creation record.

Directory migration adds `purpose: "migration"` and `migrations: [{from,to,before,after,rewrite,original}]` to the same schema 1 record; the staging directory is `.kander/migrations/<operation-id>/<task-id>/`. rewrite/original are the explicit temporary-replacement and original-text-backup names bound by the operation ID; schema 1 records missing these two fields are interpreted under the same fixed naming protocol. Recovery accepts only the complete original text and exact prefixes of After, and does not adopt randomly named unknown residue. Committed records that have a corresponding operation staging directory (including empty parent directories) are always retained for verification; unregistered artifacts error out and are retained. New records use `link_relocation: true`; the migrations of one such record contain the whole batch's legacy-file mapping, and files contain the SIZE and link adjustments for existing directories' spec/Markdown attachments; each changed card registers one revision. Before publication and before recovery, After is re-verified against the original text and the registered mapping; mixing in other body modifications is forbidden. Legacy schema 1 records without this flag keep the original SIZE-only plan semantics. For the maintenance window, recovery order, and boundaries, see [Directory cards](directory-cards.md).

## Journal Partition Migration and Retention

Under the legacy layout `operations/<operation-id>.json`, all unsorted records are still parsed by content, never assumed committed based on the file name, and a prompt to run `kander init` is given. Under the board exclusive lock and the existing maintenance window, init first checks the full record set and the migration staging evidence, then renames records into partitions one by one according to phase, and afterwards recovers prepared records. When legacy-layout records exist and the board has working/review cards, all writers must be paused first, then confirmed with `init --maintenance`. Renaming does not change record bytes or modification times; after an interruption, it continues from the actual partitions, and repeated execution reports 0 records sorted. Duplicate IDs across partitions, unknown files/directories, illegal names, and reparse points all fail closed and preserve the scene. Only ordinary temporary files matching the internal atomic-write naming are ignored and retained. Old binaries do not recognize the partition directories; coordinate a write stop before upgrading, and do not mix old and new writers.

After each successful commit and after init recovery completes, best-effort cleanup runs under the journal exclusive lock. The constant `committedJournalRetention = 100` keeps the most recent 100 committed records, to bound steady-state storage and directory-scan cost while retaining recent diagnostic history. Recency is determined by file modification time in descending order, with ties at the same time broken by file name in descending order; sorting legacy records preserves their original times. Additionally, all records whose `.kander/migrations/<operation-id>` still exists are retained (including recovery-verification original artifacts carrying Migrations), so the total can exceed 100 when migration staging evidence is present. Cleanup reads only file metadata through safe handles and does not parse redo images; deletion applies only to committed, never to pending or legacy root-directory records. Single-file deletion can be interrupted and retried, with no new recovery format needed. A cleanup failure only warns; the committed result and the command exit code are unchanged; init's record-integrity and recovery errors still fail as usual.

## Validation and Capability Boundaries

Tests cover concurrent new with the same ID, update/move races, reverse-order bulk locks, concurrent appends, cross-file and group-control publication, original-text rollback racing a new revision, regressions for resurrecting old-path small cards, managed fields and body aliases, lifecycle, and user-approved contract revisions. Child processes are killed at the prepared, attachment-directory, first-file, all-files, rename, revision, committed, after-phase-rewrite-before-archiving, journal-sorting, and cleanup boundaries, and recovery and reader isolation are verified after restart.

Transactions protect local processes that follow the command protocol; they do not isolate arbitrary processes that modify files directly. When upgrading, coordinate a write stop for old Agents/old binaries before enabling the controlled entry points. `guard-write` only gives a notice at the instant of the check; it cannot turn an external edit plus the check into one transaction. When a genuine duplicate is found, both copies are kept and an explicit error is reported; the tool does not guess which is the primary copy or delete automatically.

## Review Original-Artifact Publication

Review archiving reuses this protocol. `Transaction.PutBytes` lets dedicated producers store binary attachments losslessly; spec.md still requires UTF-8. A FileChange with invalid UTF-8 uses `binary: true` and `before_bytes`/`after_bytes` (base64) in the JSON journal; text records keep the existing format. After decoding, recovery executes through the same fs atomic writes, with creation and replacement semantics unchanged. For runs, batches, and per-card manifests, see [Review evidence archive](review-evidence.md).

## Durable Dispatch Authorization

Body and attachment updates bound to a dispatch must additionally carry `--dispatch-id` and `--execution-epoch`; the current revision cannot substitute for execution authorization. WINDOW/launch rollback validates both the operation-local version and the authorization, and accept/complete receipts share this document's transactions with state/revision. Intent and receipt original artifacts are managed by the board producer and must not be rewritten by ordinary update; for commands and recovery, see [Durable dispatch protocol](durable-dispatch.md).
