# Card Transactions, Controlled Updates, and Recovery

This document defines Kander's card storage model, file-level transaction protocol, lock ordering hierarchy, and crash-recovery guarantees.

Task identity is anchored on `task_id`; file paths are merely the runtime lookup result. The `internal/board` package relocates paths under lock and never uses cached paths for blind asynchronous writes.

---

## 1. Core Principles & Transaction Commands

All card modifications go through controlled CLI commands or the `board.WithTransaction` API:

```sh
# Inspection & Document updates
kander show --json <task-id>
kander update <task-id> --document spec.md --file <UTF8-input> --expect-revision <revision>
kander update <task-id> --document plan.md --file <UTF8-input> --expect-revision <revision>

# Lifecycle state transitions
kander move <task-id> working --owner <agent>
kander move <task-id> done --result completed
kander move <task-id> archived --result cancelled --reason <reason> --decision <decision-ref>
kander move <task-id> trash --result trashed --reason <reason> --decision <decision-ref>
```

### Invariants

1. **Card Identity**: Cards are stored as directory cards containing `spec.md` and optional attachments.
2. **Optimistic Locking (CAS)**: Mutations require an `--expect-revision`. Every successful write increments the task revision by 1.
3. **State Source of Truth**: State is solely determined by the card's residence directory (`backlog/`, `todo/`, `working/`, `review/`, `done/`, `archived/`, `trash/`).
4. **Contract Freeze**: After moving past `todo`, task size, groups, and core contracts cannot be modified without an explicit `--contract-decision-file` revision record.

---

## 2. Lock Hierarchy & Isolation

To prevent deadlocks across concurrent multi-task operations, locks must be acquired in a strict, deterministic sequence:

```text
1. board.lock          (Shared for reads/writes, Exclusive for new/move/recovery)
       |
       v
2. group locks         (Sorted lexicographically by group ID)
       |
       v
3. task locks          (Sorted lexicographically by task ID)
       |
       v
4. journal.lock        (Brief Exclusive lock: atomic pending->committed moves & cleanup)
```

### Control Files Layout (`kanban/.kander/`)

| Path | Purpose |
|---|---|
| `locks/board.lock` | Global board mutex (POSIX `flock` / Windows `LockFileEx`). |
| `locks/<task-id>.lock` | Stable inode/handle per task; readers share, writers are exclusive. |
| `locks/journal.lock` | Protects atomic write-ahead log operations and directory partitioning. |
| `versions/<task-id>.json` | Tracks `{revision, operation_id, contract_frozen}`. |
| `operations/pending/` | Holds durable pre-commit transaction records. |
| `operations/committed/` | Holds committed historical transaction records. |

---

## 3. Two-Phase Commit & Crash Recovery

Multi-file updates (e.g. updating task specs, moving state folders, writing review sidecars) are written via a Two-Phase Commit (2PC) write-ahead log.

```text
[1. Prepare Phase]
   Write operation JSON -> operations/pending/<operation-id>.json (phase: "prepared")
   FS sync (Atomic replacement via internal/fs)

[2. Apply Phase]
   Create attachment dirs -> Write files -> Relocate card directories -> Update versions

[3. Commit Phase]
   Under journal.lock:
   Rewrite marker to phase: "committed"
   Atomic rename: pending/<id>.json -> committed/<id>.json
```

### Crash Scenarios & `kander init` Redo

If a crash or kill signal interrupts an operation:
- **Pending Record with phase `"prepared"`**: Reader operations will fail closed with a `pending-recovery` error. Readers never automatically repair.
- **Recovery Command**: Running `kander init` acquires the exclusive `board.lock`, reads the pending journal, and replays unfinished file copies and directory relocations until complete.
- **Pending Record with phase `"committed"`**: The commit succeeded before the final rename. `kander init` completes the rename into `committed/` without re-applying old disk images.

---

## 4. Journal Retention & Partition Migration

- **Committed Retention**: After each commit and recovery cycle, `kander` runs best-effort pruning under `journal.lock`. The constant `committedJournalRetention = 100` preserves the 100 most recent records (by modification time) to bound scan overhead.
- **Migration Protection**: Any record with an active `.kander/migrations/<operation-id>` staging directory is preserved regardless of the 100-record threshold.
- **Partition Layout**: Modern Kander partitions operations into `pending/` and `committed/`. Legacy flat `operations/<id>.json` layouts trigger a prompt to run `kander init --maintenance`.

---

## 5. Review & Durable Dispatch Extensions

1. **Review Artifacts**: The `Transaction.PutBytes` API stores binary attachments losslessly. In the recovery journal, non-UTF-8 files are encoded with `"binary": true` and base64 payloads.
2. **Durable Dispatch Authorization**: Updates originating from an active dispatch must supply `--dispatch-id` and `--execution-epoch`. Card updates validate both the operation revision and execution authorization before committing.
