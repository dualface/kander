# kander host

## Purpose and users

Lists the hosts that report to the configured server, with the agents and launchers each reports per project. Used by the user and by an orchestrator that picks a host for `kander start --host`.

## Syntax

```text
kander host list
```

## Arguments

- `list`: every host that ever sent a heartbeat to this server, by host name.

## Examples

```text
# List the hosts of the server
kander host list

# The same as JSON
kander host list --json
```

## Preconditions

- The server of the configuration (`server.url`, otherwise the default loopback server). No project binding is needed.

## Effects

- Read-only. The server decides on its own clock: a host whose last heartbeat is older than 60 seconds is offline.
- A host reports only the projects whose registration on that host passes its check: the directory exists, it is a Git checkout, its configuration binds the same server and project, and its `origin` is the Git remote the server registered for the project. `kander init` registers a checkout, and `kander doctor` scans the agents and launchers (`snippets/commands/doctor.md`).

## Output

- Text: one line per host with its name, `online` or `offline`, the last heartbeat, the worker version, and whether it takes orders, plus a conflict mark; then one line per reported project with its agents and launchers.
- With `--json`: `hosts`, per host `client_id`, `name`, `worker_id`, `version`, `accepting`, `order_protocol`, `projects` (per project `project`, `agents`, `launchers`), `last_heartbeat`, `online`, and `conflict` (`client_id_conflict` while two workers of one client ID sent heartbeats within 60 seconds of each other).

## Rejections

- Any argument after `list`, or an unknown option, is a usage error.
