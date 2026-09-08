# Kanban Write Guard (kander guard-write)

A common source of duplicate cards on the kanban board is creating new direct children under the state directories (`backlog/`, `todo/`, `working/`, `review/`, `done/`, `archived/`, `trash/`). The only legitimate creation entry point is `kander new`; when an Agent writes using the old path of an already-migrated card, editing tools silently recreate the file, resurrecting a cross-state copy at the original location.

`kander guard-write` errors out explicitly when it finds an old path at check time, for the host project's pre-write hooks (PreToolUse, etc.) to invoke. A race still exists between the check and the subsequent external write, so it is only an auxiliary check. Agents must write cards through the controlled update transactions of [Card transactions](card-transactions.md); a hook pass must not be treated as an atomic write guarantee.

## Commands

```sh
kander guard-write <path>
```

- Exit code `0`: pass.
- Exit code `1`: reject, with the reason on stderr; when the same ID already exists in another state directory, the card's current state is pointed out.
- Exit code `2`: usage error.

Decision rules:

| Target path | Result |
| --- | --- |
| Not under the current board's `kanban/` | Pass |
| Inside the board but not state-directory content | Pass |
| Direct child of a state directory, file or directory already exists | Pass (normal editing) |
| Same-state legacy `<task-id>.md` spelling, directory card already exists | Reject, pointing to the current spec.md path and the update entry point |
| Direct child of a state directory, currently nonexistent | Reject (new cards only through `kander new`; writing to an old path resurrects a copy) |
| File inside a directory card (`spec.md`, `plan.md`, `report.md`, etc.), directory card entry exists | Pass |
| File inside a directory card, that ID is still a same-state legacy `.md` | Reject, prompting to pause writes and run `kander init` first |
| File inside a directory card, directory card entry does not exist | Reject (the write would silently recreate the whole directory card) |

Board location follows the existing order: `KANBAN_DIR` -> `kanban/` in the current Git repository's main worktree -> upward search. When no board can be located (a non-Git project with `KANBAN_DIR` unset), it passes and does not block non-kanban projects; other location errors still fail.

The guard only prevents accidental writes by same-user Agents; it is not a security boundary against malicious processes.

## Claude Code Integration

The repository provides the sample script [`scripts/guard-kanban-write.sh`](../scripts/guard-kanban-write.sh) (depends on `jq`; passes when `jq` is missing or parsing fails). Register the PreToolUse hook in the project's `.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {"type": "command", "command": "sh scripts/guard-kanban-write.sh"}
        ]
      }
    ]
  }
}
```

The script reads the hook JSON from stdin and hands `tool_input.file_path` to `kander guard-write`; on rejection, it blocks the current tool call with exit code 2 and echoes the reason back to the Agent.

Write mechanisms in `Bash` such as redirection, heredocs, and `sed -i` cannot have their target paths reliably reconstructed from the tool arguments, so the hook does not cover them; Kander's internal writes also do not go through this hook. Concurrency and recovery guarantees are carried by the per-task-ID controlled write transactions.

## Codex Integration

Codex's PreToolUse hook (`.codex/hooks.json`) can use the same script; the recommended matcher covers `apply_patch|Edit|Write`. The field names of the incoming JSON depend on the Codex version in use; add the corresponding path-extraction logic to the script when necessary.

## Windows

The `kander guard-write` subcommand itself is available cross-platform. The sample hook script is POSIX shell; a native Windows environment needs to wrap the invocation itself according to the hook mechanism of the Agent in use.
