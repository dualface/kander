# kander server

## Purpose and users

Runs and administers the shared board service from the single `kander` executable.

## Syntax

```text
kander server start [--data DIR] [--listen ADDR] [--ttl D] [--renew D] [--foreground]
kander server stop
kander server restart
kander server status
kander server import [--data DIR] --project NAME [--remote URL] [--from KANBAN_DIR]
kander server maintain [--data DIR] --project NAME [--maintenance]
kander server projects [--data DIR]
```

## Arguments

- `start`: detach a background server, append stdout and stderr to `<data>/server.log`, and wait up to 10 seconds for health. An existing healthy server is reused. `--foreground` serves in this process, with JSON logs on stderr, until SIGINT, SIGTERM, or authenticated shutdown.
- `--data`: absolute or relative data directory. Default: `$XDG_DATA_HOME/kander-server` when XDG_DATA_HOME is absolute, otherwise `~/.local/share/kander-server`.
- `--listen`: explicit TCP listen address. Default: the configured loopback server URL's address, otherwise `127.0.0.1:7878`.
- `--ttl`, `--renew`: positive Go durations. Defaults: `90s`, `15s`. Renewal must be shorter than TTL.
- `status`: read the server's actual URL, version, PID, data directory, log path, project count, and listen/lease settings.
- `stop`: authenticate through a current-user private management key and gracefully shut down a loopback server. Wait up to 10 seconds. Already stopped is success. `stop` leaves no mark: the next command that opens a session starts the server again (see `init`), so stop and restart the server only while no Agent runs, and pause every executor before an offline `maintain`.
- `restart`: read the running server's data directory, listen address and lease settings, stop it, then start it with those settings.
- `import`: explicitly import a paused v1 board into an empty service project. When no process holds the data directory lock, it imports offline under that lock. When a local server serves the data directory, that server performs the import and keeps serving. `--from` defaults to the board v1 commands locate: `KANBAN_DIR`, then the main worktree's `kanban/`, then an upward search.
- `maintain`: offline legacy file-card maintenance. `--maintenance` acknowledges that all writers and executors are paused.
- `projects`: list the registry without writing or requiring a running server.
- `--foreground`: with `start`, serve in this process instead of detaching.
- `--project NAME`: the project name that `import` and `maintain` act on.
- `--remote URL`: with `import`, the Git remote URL that the first registration of the project name binds.
- `--from KANBAN_DIR`: with `import`, the paused v1 board to import.
- `--maintenance`: with `maintain`, acknowledge that all writers and executors are paused.

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

# Maintain legacy file cards of a project offline
kander server maintain --project demo --maintenance

# List the projects of the registry
kander server projects
```

## Preconditions

`stop` and `restart` require a loopback URL. Status and control target `server.url`, defaulting to `http://127.0.0.1:7878`. Maintenance requires that no server holds the data directory lock. Import requires either a free lock or a loopback `server.url` whose server reports the same data directory. Service routes retain the trusted-network boundary; only shutdown and import add loopback and management-key authentication.

## Effects

Start creates a resident process unless `--foreground` is supplied. Stop uses HTTP, never signals or PID lookup. Restart preserves the reported settings. These commands never ask to start another service implicitly. The independent management key is stored in `admin-token.json`; it is never returned or logged.

## Output

Control commands report the service and its storage paths. Import prints the same structured result offline and online, including the absolute `source` it used. Offline commands retain structured maintenance results and registry listing. Status fails when the service is unreachable. A failed background start reports the log path and its last 20 lines and terminates only the child it launched.

With `--json`:
- `start`, `restart`, and `status`: one object, the server info: `url`, `version`, `pid`, `data_dir`, `log_path`, `projects`, `listen`, `ttl`, `renew`. A server that is not running makes `status` fail with the code `server_not_running`.
- `start --foreground`: stdout stays empty until the server can serve, then one line holds `listen`, `data_dir`, `pid`, and `version`. The JSON log lines stay on stderr. A failure after that line leaves it in place and ends stderr with one error object.
- `stop`: `stopped` (boolean, false when no server was running) and `target`.
- `import`: the import result, with no progress output. Fields: `project` (`id`, `name`, `remote` when set, `created_at`, `board_path` when set), `source` (the absolute source path of this request, also for an earlier import of the same snapshot), `files` and `tasks` (counts), `snapshot` (the digest of the source board; the published board equals it except for the `SIZE` lines in `defaulted_size`), `existing` (`true` only when the snapshot was imported before; otherwise left out), `defaulted_size` (an array of the cards whose `SIZE` line the import defaulted; left out when empty), and `warnings` (an array of `task_ids` and `message`: a board check problem accepted because every card it concerns is closed; left out when empty). `defaulted_size` and `warnings` are the same for an earlier import of the same snapshot.
- `maintain`: the maintenance result: `project` (the same object as in the import result), `changed_tasks` (the number of file cards converted to directory cards, 0 when there was nothing to convert), and `snapshot` (the digest of the board after the migration).
- `projects`: `projects`, an array of `id`, `name`, `remote`, `created_at`, `board_path`.

## Rejections

Unknown or extra arguments are usage errors. Invalid lease settings, refused shutdown authentication, unsafe paths and unavailable servers fail explicitly. Shutdown and import requests from non-loopback peers are refused even with a correct key. Import fails without changing data when its source board cannot be located, when the lock is held but no server answers at `server.url`, when `server.url` is not a loopback URL, when the server serves another data directory, or when the running server predates online import (restart it first).
