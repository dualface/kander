# kander server

## Purpose and users

Runs and administers the shared board service from the single `kander` executable.

## Syntax

```text
kander server start [--data DIR] [--listen ADDR] [--ttl D] [--renew D] [--foreground]
kander server stop
kander server restart
kander server status
kander server import [--data DIR] --project NAME [--git-remote GIT-URL] [--from V1-BOARD-DIR] [--skip-conflicts]
kander server maintain [--data DIR] --project NAME [--maintenance]
kander server projects [--data DIR]
kander server gc [--data DIR] [--project NAME] [--dry-run] [--yes] [--older-than D] [--log-keep SIZE]
```

## Arguments

- `start`: detach a background server, append stdout and stderr to `<data>/server.log`, and wait up to 10 seconds for health. An existing healthy server is reused. `--foreground` serves in this process, with JSON logs on stderr, until SIGINT, SIGTERM, or authenticated shutdown.
- `--data`: absolute or relative data directory. Default: `$XDG_DATA_HOME/kander-server` when XDG_DATA_HOME is absolute, otherwise `~/.local/share/kander-server`.
- `--listen`: explicit TCP listen address. Default: the configured loopback server URL's address, otherwise `127.0.0.1:7878`.
- `--ttl`, `--renew`: positive Go durations. Defaults: `90s`, `15s`. Renewal must be shorter than TTL.
- `status`: read the server's actual URL, version, PID, data directory, log path, project count, and listen/lease settings.
- `stop`: authenticate through a current-user private management key and gracefully shut down a loopback server. Wait up to 10 seconds. Already stopped is success. `stop` leaves no mark: the next command that opens a session starts the server again (see `init`), so stop and restart the server only while no Agent runs, and pause every executor before an offline `maintain`.
- `restart`: read the running server's data directory, listen address and lease settings, stop it, then start it with those settings.
- `import`: explicitly import a paused v1 board into an empty or existing service project. Requests to that project wait until publication finishes. Other projects stay available. The target server is the effective `server.url` of the current checkout, which `kander init --server <URL>` or the configuration sets; no `import` argument selects it. With a loopback `server.url` it imports offline when the data directory lock is free, or uses the local server that holds it. With a remote `server.url` it streams the client's board to that server and never opens the local server data directory. Omit `--data` for remote import. Remote upload uses the trusted-network boundary. A server with default limits accepts 1,000,000 files, 1,000,000 directories, 128 GiB of file contents, 256 MiB per file, a 256 MiB manifest and two concurrent uploads; an embedding host may set lower or higher limits, and the client leaves the limits to the server. The server receives the request within 30 minutes plus the declared file bytes at 8 MiB/s, and ends it when file bodies stop arriving for 5 minutes. A local import waits at most 5 minutes for the source board lock; its scan and copy have no fixed deadline and stop only on cancellation, a lost client or a server shutdown. Once a streamed answer has started, the server sends progress at least once per second and the client treats 2 minutes without a line as a lost connection; before that, an upload is bounded by one minute more than the receive deadline and a local online import by 6 minutes for the response headers. `--from` defaults to the board v1 commands locate: `KANBAN_DIR`, then the main worktree's `kanban/`, then an upward search.
- `maintain`: offline legacy file-card maintenance. `--maintenance` acknowledges that all writers and executors are paused.
- `projects`: list the registry without writing or requiring a running server.
- `gc`: reclaim server data on the server host: compact settled review runs (see the review command contract), delete idempotency receipts nothing replays any more, and trim `<data>/server.log`. It first computes the plan and prints it by category with counts and bytes, then asks y/N (default N) on a terminal, then runs. When a loopback server serves the data directory, the gc runs inside that server through its administration route; otherwise it takes the data directory lock without waiting and runs in this process. A held lock sends it to the server at `server.url` or at the address the server recorded in the data directory, asked again for up to 30 seconds while the server starts; each probe of one address waits at most 3 seconds, so an address that never answers does not hold up the other one, and a discovery whose last probe timed out fails with `gc_timeout`. With `--project` it covers that project only and leaves `server.log` alone.
- `--foreground`: with `start`, serve in this process instead of detaching.
- `--project NAME`: the service project name that `import` and `maintain` act on, the name a bound checkout holds in `project.name`. Required; it is not read from the configuration. With `gc` it is optional and selects one registered project.
- `--git-remote GIT-URL`: with `import`, the Git remote URL of the project's repository. It is not the address of the Kander server (see `import`). A project without a remote is bound to it; a project bound to another remote refuses the import. Use the exact text that `git remote get-url origin` prints in the checkouts of the project, or omit it: the first command run from such a checkout binds the project.
- `--from V1-BOARD-DIR`: with `import`, the directory of the paused v1 board (the `kanban/` directory itself); a relative path is resolved in the current directory. Default: see `import`.
- `--skip-conflicts`: with `import`, keep the destination versions of conflicting cards and shared records and merge the rest. Without it, any conflict rejects the entire import.
- `--maintenance`: with `maintain`, acknowledge that all writers and executors are paused.
- `--dry-run`: with `gc`, print the plan and change no data file.
- `--yes`: with `gc`, run without asking. Without a terminal, or with `--json`, `gc` needs `--yes` or `--dry-run`.
- `--older-than D`: with `gc`, the retention period: a Go duration or a whole number of days such as `7d`; at least `1h`. Default: `7d`.
- `--log-keep SIZE`: with `gc`, the tail of `server.log` to keep: a number of bytes, optionally followed by `KiB`, `MiB` or `GiB`; at least `1MiB`. Default: `64MiB`.

## Examples

```text
# Start a background server
kander server start --data /tmp/kander-data --listen 127.0.0.1:7878

# Print the state of the running server as JSON
kander server status --json

# Stop the running server
kander server stop

# Restart the server with its current settings
kander server restart

# Import a paused v1 board into an empty project
kander server import --project demo --from /tmp/kanban

# Import into the configured remote or local server, keeping destination versions on conflict
kander server import --project demo --from /tmp/historical-kanban --skip-conflicts

# Import a v1 board and bind the project to its Git remote
kander server import --project demo --from /tmp/kanban --git-remote https://example.com/demo.git

# Maintain legacy file cards of a project offline
kander server maintain --project demo --maintenance

# List the projects of the registry
kander server projects

# Show what a server gc would reclaim
kander server gc --dry-run

# Reclaim without asking, keeping two weeks and 128 MiB of log
kander server gc --yes --older-than 14d --log-keep 128MiB
```

## Preconditions

`stop` and `restart` require a loopback URL. Status and control target `server.url`, defaulting to `http://127.0.0.1:7878`. Maintenance requires that no server holds the data directory lock. Local import requires either a free lock or a loopback server that reports the same data directory. Remote import requires a server that supports uploads. It uses the service's trusted-network boundary and does not require a management key. Shutdown, the local online import endpoint and the gc endpoint require a loopback peer with the management key. `gc` runs offline only when the data directory lock is free; it never waits for the lock.

## Effects

With the server service of the client home installed (`snippets/commands/service.md`), `start`, `stop`, and `restart` ask the service manager instead and use the options of the service definition; on Windows `stop` stays the authenticated stop and `restart` stops that way and runs the task. `start --foreground`, which the service runs, and a process with `KANDER_MANAGED=1` never do.

Import adds whole cards and their shared evidence without overwriting destination files. Identical tasks in the same state are skipped after applying the missing-`SIZE` default to the source. Shared identities include task-group checkpoints, review runs, batches and plans, dispatches, execution ledgers, archive cleanup, issue results, task versions and committed journal operations. Source locks and caches are excluded only from merges. Unknown source control records and problems involving active cards reject the staged result. The leftover of an interrupted atomic write, a file named `.<name>.<pid>.<n>.tmp` below `.kander/` or inside a card directory, is not copied and does not reject the import: each one adds a warning that names its board-relative path, its target and whether their contents are the same, differ, or the target does not exist; it stays in the source board. Publication and its per-snapshot receipt recover together after interruption. An import that fails or stops before publication removes its staging, and the server log records the original cause. Server startup removes staging that no publication intent names and every leftover upload staging directory.

`gc` removes data that cannot be restored, and only in terminal state: a review run is compacted only when it is settled and older than the retention period; a done idempotency receipt of `observation:` or `group-observation:` goes after 24 hours, any other done receipt only after the retention period and once a late retry of it can no longer write: a `claim:`, `takeover:` or `confirm:` receipt when its card is in `done`, `archived` or `trash`, any other card receipt when its card is `archived`. A successful `group-release:` receipt (HTTP 200) goes after the retention period once the board has at least one card of that task group and every such card is `archived` or in `trash`. Pending receipts, the other group receipts and failed releases, run receipts, receipts bound to nothing reliable, and every receipt of a board with a transaction that waits for recovery stay. `server.log` keeps its last `--log-keep` bytes from the next line start; the head goes to `server.log.1.gz` (one generation, replaced in one atomic step). A project that an import holds or whose import waits for recovery is skipped and reported, and a held lock is reported, never waited for: a board, card, or journal lock that another process holds ends the compaction or the receipt part of that project with one error naming `board.lock_busy`, while a project lock that a request holds ends the receipt part with an error saying the project was busy. A gc keeps running in the server when its client disconnects or times out, and a second gc is refused while one runs. Running `gc` again finishes what an interrupted gc left: the receipt batch it recorded, a batch of members per project lock hold, and the log tail it saved. A recorded member that ends without a removal (already gone, replaced, damaged, or no longer due) counts in `receipts.settled`, not in `files`; a plan with only such members still runs and removes the record.

Relative archive paths use `/` on all platforms. The server validates a complete upload before starting the same merge used for local import. An upload interrupted before completion publishes nothing. If interrupted after completion, rerun to see whether the server published it.

Start creates a resident process unless `--foreground` is supplied. Stop uses HTTP, never signals or PID lookup. Restart preserves the reported settings. These commands never ask to start another service implicitly. The independent management key is stored in `admin-token.json`; it is never returned or logged.

## Output

Control commands report the service and its storage paths. Import prints the same structured result offline and online, including the absolute client-side `source` it used. Progress is on stderr. Remote results omit `project.board_path`. Offline commands retain structured maintenance results and registry listing. Status fails when the service is unreachable. A failed background start reports the log path and its last 20 lines and terminates only the child it launched.

With `--json`:
- `start`, `restart`, and `status`: one object, the server info: `url`, `version`, `pid`, `data_dir`, `log_path`, `projects`, `listen`, `ttl`, `renew`. A server that is not running makes `status` fail with the code `server_not_running`.
- `start --foreground`: stdout stays empty until the server can serve, then one line holds `listen`, `data_dir`, `pid`, and `version`. The JSON log lines stay on stderr. A failure after that line leaves it in place and ends stderr with one error object.
- `stop`: `stopped` (boolean, false when no server was running) and `target`.
- `import`: the import result, with no progress output. It includes `project` (`id`, `name`, `remote` when set, `created_at`, `board_path` when set), the request's absolute `source`, the source file and task counts (`files`, `tasks`), and the source `snapshot` digest. For merges, `added_tasks` counts cards actually added, `existing_tasks` lists identical tasks, and `skipped_conflicts` lists the skipped `{kind, id}` pairs. Zero and empty values are omitted. `defaulted_size` lists cards whose size was defaulted and `warnings` lists the skipped leftovers of interrupted writes, then the accepted check problems of closed cards, as `{task_ids, message}`; `task_ids` is `[]` for a leftover below `.kander/`. Both are omitted when empty. `existing: true` returns the first result of that snapshot, including any merge fields and warnings.
- `maintain`: the maintenance result: `project` (the same object as in the import result), `changed_tasks` (the number of file cards converted to directory cards, 0 when there was nothing to convert), and `snapshot` (the digest of the board after the migration).
- `projects`: `projects`, an array of `id`, `name`, `remote`, `created_at`, `board_path`.
- `gc` (with `--yes` or `--dry-run`): `dry_run`, `projects` (array of `name`, `compaction` with `runs`, `files`, `bytes` and `kept`, `receipts` with `files`, `bytes`, `settled` (omitted when 0) and `kept`, optional `warnings` and `errors`), `log` (`path`, `size`, `bytes`, optional `recovered` and `kept`; omitted with `--project`), and optional `errors`. Each `kept` entry is `{id, reason}`: a candidate that was kept.

`gc` exits 0 when every candidate was reclaimed or there was none, 1 when the user cancelled, a candidate was kept, or the run met an error (an unreachable server, a failed read or removal, a timeout), and 2 for a usage error, a missing confirmation, or a refused request. The plan goes to standard output and the y/N prompt to standard error.

## Rejections

Unknown or extra arguments are usage errors. With the server service installed, `start` with a `--data` or `--listen` that differs from the service definition fails and names `kander service install server` to change the service. `gc` refuses with exit 2 and changes nothing when `--older-than` or `--log-keep` is invalid or below its minimum, the project name is invalid or not registered, confirmation is missing without a terminal, `--json` comes without `--yes` or `--dry-run` (`interactive_required`), the administration check fails (HTTP 403), or the server already runs a gc (`gc_running`, HTTP 409). A merge conflict without `--skip-conflicts` returns `board_rejected` (HTTP 422) and lists the conflicting task and record IDs. Invalid lease settings, refused shutdown authentication, unsafe paths and unavailable servers fail explicitly. Shutdown and local online import requests from non-loopback peers are refused even with a correct key. Local import fails when the source cannot be located, the lock is held but no server answers, or the server serves another data directory. Remote import refuses `--data` with exit 2 before contacting the server. Malformed uploads, unsafe paths, links, limit violations and archive mismatches do not publish a board. A limit violation returns HTTP 413 naming the limit, the declared value and the limit value; the client adds that an older server may have lower limits and should be upgraded. If the remote server answers 404 or 405 for the upload route, upgrade it and retry. An import that stops before publication because the source lock wait took longer than 5 minutes, the client went away, or the server is shutting down returns `import_canceled` (HTTP 503): nothing was published, and the same command can run again.
