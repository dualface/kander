# kander server

## Purpose and users

Run and administer the shared board service from the single `kander` executable.

## Syntax

```text
kander server start [--data DIR] [--listen ADDR] [--ttl D] [--renew D] [--foreground]
kander server stop
kander server restart
kander server status [--json]
kander server import [--data DIR] --project NAME [--remote URL] --from KANBAN_DIR
kander server maintain [--data DIR] --project NAME [--maintenance]
kander server projects [--data DIR] [--json]
```

## Arguments

- `start`: detach a background server, append stdout and stderr to `<data>/server.log`, and wait up to 10 seconds for health. An existing healthy server is reused. `--foreground` serves in this process, with JSON logs on stderr, until SIGINT, SIGTERM, or authenticated shutdown.
- `--data`: absolute or relative data directory. Default: `$XDG_DATA_HOME/kander-server` when XDG_DATA_HOME is absolute, otherwise `~/.local/share/kander-server`.
- `--listen`: explicit TCP listen address. Default: the configured loopback server URL's address, otherwise `127.0.0.1:7878`.
- `--ttl`, `--renew`: positive Go durations. Defaults: `90s`, `15s`. Renewal must be shorter than TTL.
- `status`: read the server's actual URL, version, PID, data directory, log path, project count, and listen/lease settings. `--json` prints those fields as JSON.
- `stop`: authenticate through a current-user private management key and gracefully shut down a loopback server. Wait up to 10 seconds. Already stopped is success.
- `restart`: read the running server's data directory, listen address and lease settings, stop it, then start it with those settings.
- `import`: explicitly import a paused v1 board into an empty service project under the exclusive data directory lock.
- `maintain`: offline legacy file-card maintenance. `--maintenance` acknowledges that all writers and executors are paused.
- `projects`: list the registry without writing or requiring a running server.

## Preconditions

`stop` and `restart` require a loopback URL. Status and control target `server.url`, defaulting to `http://127.0.0.1:7878`. Offline import and maintenance require that no server holds the data directory lock. Service routes retain the trusted-network boundary; only shutdown adds loopback and management-key authentication.

## Effects

Start creates a resident process unless `--foreground` is supplied. Stop uses HTTP, never signals or PID lookup. Restart preserves the reported settings. These commands never ask to start another service implicitly. The independent management key is stored in `admin-token.json`; it is never returned or logged.

## Output

Control commands report the service and its storage paths. Offline commands retain structured import/maintenance results and registry listing. Status fails when the service is unreachable. A failed background start reports the log path and its last 20 lines and terminates only the child it launched.

## Rejections

Unknown or extra arguments are usage errors. Invalid lease settings, refused shutdown authentication, unsafe paths and unavailable servers fail explicitly. Shutdown requests from non-loopback peers are refused even with a correct key.
