# kander service

## Purpose and users

Registers the local Kander background processes with the host's service manager, so they keep running and come back after a reboot. Linux uses a systemd user service, macOS a launchd LaunchAgent, and Windows a scheduled task; a worker that runs as a service stays without leases. Used by the user.

## Syntax

```text
kander service install server [--listen ADDR] [--data DIR] [--ttl D] [--renew D]
kander service install worker
kander service uninstall <server|worker>
kander service status [server|worker]
```

## Arguments

- `server`, `worker`: the service. `status` without one covers both.
- `install`: write the service definition, register it with the service manager, and make sure the service runs. The definition starts the global `kander` (a Homebrew install: its link in the Homebrew bin directory, never the Cellar target) with `server start --foreground` and the server options, or with `worker`, and sets `KANDER_MANAGED=1`, plus `KANDER_CLIENT_HOME` when the command runs with it set. It writes no token, `PATH`, or other variable.
- `uninstall`: stop the service and remove its definition.
- `status`: report each service: whether it is installed, the state its service manager reports, the definition, its command line and client home, and the process that runs.
- `--listen`, `--data`, `--ttl`, `--renew`: the options of the server, those of `kander server start`, with the same defaults. `--data` is written as an absolute path.

## Examples

```text
# Run the worker of this host as a service
kander service install worker

# Run the server as a service on another address
kander service install server --listen 127.0.0.1:7979

# Show both services as JSON
kander service status --json

# Remove the server service
kander service uninstall server

# Remove the worker service
kander service uninstall worker
```

## Preconditions

- The global `kander` installed by `kander install` or Homebrew. A Microsoft Store install is refused.
- Linux: a systemd user instance that answers `systemctl --user`. Without one `install` is refused and the automatic start by `kander` commands stays in charge; such a host cannot take starts directed at it. macOS: launchd for the gui domain of the user. Windows: Task Scheduler.

## Effects

- Linux: `~/.config/systemd/user/kander-server.service` and `kander-worker.service` (below `$XDG_CONFIG_HOME` when it is absolute), enabled for `default.target`, with `Restart=always`; the worker unit has `KillMode=process`, so a stop or restart of the worker leaves the agents and terminal servers it started running. When linger is off for the user, `install` succeeds and prints the `loginctl enable-linger <user>` hint: without linger the service starts at the first login and stops at the last logout. It never enables linger.
- macOS: `~/Library/LaunchAgents/io.github.dualface.kander.server.plist` and `io.github.dualface.kander.worker.plist`, loaded into the `gui/<uid>` domain, with `RunAtLoad`, `KeepAlive`, and `AbandonProcessGroup`.
- Windows: the tasks `\kander-server` and `\kander-worker`, registered from an XML definition: started at the logon of the user, so a service runs only after the user logged in; restarted on failure every minute; no execution time limit. The task runs `cmd.exe` through `conhost.exe --headless`, which sets the variables and appends the output to the log. Kander stops a task's process through the stop request of the worker or the authenticated stop of the server, never through `schtasks /End`.
- The output goes to the logs of a background start: `<data>/server.log` and `<client home>/worker.log`.
- Every definition carries the Kander marker. `install` and `uninstall` refuse a definition without it at the same location and leave it unchanged.
- `install` first stops a server or worker that `kander` had started in the background: the server with the authenticated stop of `kander server stop`, the worker with the stop request `worker.stop`, waiting up to 30 seconds for `worker.lock` to be free. A server counts when it answers at the new listen address, or at the address the server of the new data directory recorded and serves that directory. The leases and files of the client home stay. A definition that did not change is not rewritten, and a running service is left alone; a changed one is rewritten and the service restarts, also when its process still runs at the listen address or for the client home of the definition it replaces. `install` waits up to 20 seconds until the worker holds its lock or the server answers.
- While the server service of the client home of this process is installed, `kander server start`, `stop`, and `restart` and the automatic start of the server go through the service manager; so does the automatic start of the worker while the worker service of the client home is installed. Both client homes unset count as the same home. `kander server start --foreground` and a process that runs with `KANDER_MANAGED=1` never do.
- A managed worker on Linux and macOS whose binary was replaced exits after it released its lock, and the service manager starts the new binary. On Windows it hands over as a background worker does, and its successor stays managed.
- `uninstall` stops the service and removes its definition; on Windows it deletes the task and stops the process of the service through the Kander protocol. `kander uninstall` unregisters both services first.

## Output

- Text: one line per change, the command line of the definition, and for `status` each service with its details and warnings: an executable that does not exist, one that is not the global `kander`, a running process the service manager did not start, a definition Kander did not write.
- With `--json`, `install` prints `service`, `manager` (`systemd`, `launchd`, or `schtasks`), `location`, `executable`, `args`, `client_home`, `changed`, `stopped`, `restarted`, and `linger` (`yes`, `no`, or `unknown` on systemd). `uninstall` prints `service`, `location`, and `removed`. `status` prints `services`: per service `service`, `manager`, `location`, `installed`, `foreign`, `executable`, `args`, `client_home`, `state`, `running`, `pid`, `managed`, `missing_executable`, and `warnings`.
- Exit 0 on success, 1 on a failure, 2 for a usage error. A definition without the Kander marker fails with the code `service_foreign_definition`.

## Rejections

- A missing or unknown service name, or a second one, is a usage error.
- `--listen`, `--data`, `--ttl`, or `--renew` with `install worker` is an unknown option, and a lease renewal not shorter than its TTL is a usage error.
