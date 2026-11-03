# dsh Launcher

The `dsh` launcher runs a card's executing agent inside a DeepSeek Harness (dsh) host process: dsh Web (`dsh web`) or dsh Desktop. The agent is a child of the dsh host, in a terminal the `kander-dsh` plugin opens, and the plugin's Kander view in dsh is where the user watches it. Kander drives that terminal through the embedded terminal definition [`dsh.json`](../internal/terminal/builtin/definitions/dsh.json), the same way it drives tmux or herdr.

The launcher is POSIX-only (Linux and macOS). It never takes part in `auto`: whether a dsh host with the plugin runs cannot be read from the environment. Select it with `launcher: dsh` in the configuration or `kander start --launcher dsh <task-id>`.

## Parts

| Part | Location | Role |
| --- | --- | --- |
| Terminal definition | `internal/terminal/builtin/definitions/dsh.json` | Maps every `terminal.Backend` operation to one `curl --unix-socket` call on the plugin socket |
| Plugin, host half | `integrations/kander-dsh/lib/` | Pane multiplexer, socket server, carrier election, JavaScript interface for the page routes |
| Plugin, page half | `integrations/kander-dsh/` (card `20261103-dsh-kander-view-task`) | The Kander view: board, agent terminal tabs, HTTP routes |

Host half modules:

| Module | Responsibility |
| --- | --- |
| `index.js` | dsh plugin entry (`apply(ctx, config)`): builds one `KanderHost`, stops it with the plugin |
| `host.js` | `KanderHost`: composes the parts below and exposes the JavaScript interface |
| `mux.js` | One container per pane, one PTY per pane through `ctx.subprocess.spawnTerminal`, a headless screen per pane (vendored `@xterm/headless` 5.5.0) for `read_output` and terminal query replies, an output ring for subscribers |
| `carrier.js` | Which host of a `DSH_HOME` holds the socket |
| `ops.js` | The socket operations |
| `protocol.js` | Socket protocol version, HTTP framing, error classes |
| `ids.js` | Persistent pane and container IDs |
| `foreground.js` | Foreground process name: `/proc/<pgid>/comm` on Linux, `ps -o comm= -p <pgid>` elsewhere |
| `paths.js` | The state directory and its permissions |

## Panes, Containers and IDs

A pane is one PTY that the dsh host opens with `ctx.subprocess.spawnTerminal`. A container holds exactly one pane, and every agent launch creates a new container. `WINDOW` is `dsh:<container>:<pane>`, for example `dsh:c7-81234:p7-81234`.

IDs are `p<n>-<pid>` and `c<n>-<pid>`, where `<pid>` is the dsh host pid. A number belongs to the process that creates the empty file `<state dir>/taken/<n>` with `O_EXCL`, so two processes never get the same number, even two hosts that both serve for an instant during a handover. `<state dir>/sequence` (mode 0600) records the last number and only says where to start looking: a stale or regressed record costs a few failed creations, never a reused number. The marks 64 numbers below the newest are removed. So within one `DSH_HOME`, IDs never repeat across the hosts or across host restarts, and an old card's `WINDOW` never names a later pane. A `sequence` file that does not hold a number makes pane creation fail instead of restarting at 1.

The pane runs `/bin/sh -c 'cd -- <startup dir> && exec <agent command>'` with `/` as its initial directory, like tmux. When the program exits, the pane stays as `dead` until `close_container`. The program is told `TERM=xterm-256color`, and the pane's headless screen answers its terminal queries (cursor position, device attributes) through the pane's ordered input queue, whether or not a page shows the pane.

`close_container` returns only after dsh confirmed that the pane's terminal session ended. When dsh cannot confirm it, the operation fails (409) and the pane stays, so `kander` reports the failed cleanup and can close it again.

## Environment of the Agent

The agent inherits the dsh host's environment, not the environment of the `kander` process that started it, as with the tmux server. dsh removes every `DSH_*` name and every name matching `KEY|PASSWORD|SECRET|TOKEN` (case-insensitive) from its children. The plugin adds:

- `DSH_HOME`, when the host has it, so a `kander` run by the agent finds the same socket;
- an empty value for `TMUX`, `TMUX_PANE`, `HERDR_ENV`, `HERDR_PANE_ID`, `LUVUS_ENV`, `LUVUS_PANE_ID` and `HC_TERMINAL_ID`, so a `kander` inside the pane does not pick a launcher of a terminal the dsh host was started from;
- the host variables named in the plugin configuration `env` (an array of names), for an agent CLI that reads its credentials from the environment.

Start the dsh host with the `PATH` the agents need.

## Carrier Host

Several dsh hosts can share one `DSH_HOME`, for example dsh Web and dsh Desktop (separate profiles under the same home). They share one socket, `$DSH_HOME/kander/mux.sock` (`~/.dsh/kander/mux.sock` when `DSH_HOME` is unset or empty). The host whose server the socket path names is the carrier host; only it hosts dsh-launcher panes.

- A starting host probes the socket with `hello`. When a server answers, the host stays a non-carrier and writes `<socket> is held by another dsh host (pid N)` to the dsh host log (the cordis logger of the plugin).
- A host publishes its socket without replacing anything: it binds a temporary name `.mux-<pid>-<n>` in the same directory, sets it to 0600, and hard-links it onto the path. `link` fails while any socket is at the path, so two hosts never both publish.
- When the path names a socket nobody serves (a host killed with SIGKILL leaves one), the host renames it aside and links its own. It removes the moved file only when it is the stale inode it inspected; a socket another host published in between is linked back. A path that is not a socket is never replaced; the host logs it and stays a non-carrier.
- Every host repeats the check every second. A non-carrier therefore takes the socket over without a restart once the carrier exits.
- A carrier whose path no longer names its own socket (the link-back above found the path taken again) closes its server, ends its panes, logs it, and competes again. Kander can no longer reach those panes and reads them as stopped; ending them keeps a `resume` from running a second executor beside a hidden one.
- A carrier that stops removes its socket. Closing a server unlinks only the temporary name node bound, so a host never removes a socket that belongs to another host.

`KanderHost.carrierStatus()` returns `{carrier, socket, reason, holderPid}`; `reason` is `carrier`, `held`, `lost`, `blocked`, `unreachable`, `starting` or `stopped`. A non-carrier lists no panes, and its Kander view shows dsh agents by status only.

## Socket Protocol

Every operation is `POST /op/<name>` on the unix socket, with a form-encoded body (`curl --data-urlencode`), answered by one JSON object. The current protocol version is `1`.

| Status | Body | Meaning |
| --- | --- | --- |
| 200 | the result fields | success |
| 400 | `{"error":"<message>"}` | malformed request (missing field, non-POSIX command) |
| 404 | `{"error":"gone"}` | the pane or container does not exist; the definition classifies it as gone |
| 404 | `{"error":"unknown op"}` | the plugin has no such operation |
| 405 | `{"error":"use POST"}` | another method |
| 409 | `{"error":"<message>"}` | the operation failed, for example a write to an exited pane |

| Operation | Fields | Result |
| --- | --- | --- |
| `hello` | `protocol` (Kander's version) | `protocol` (the plugin's, as a string), `upgrade` (`""`, `kander` or `plugin`), `pid` |
| `create_container` | `cwd`, `label`, `project` | `container`, `pane` |
| `run_command` | `pane`, `cwd_quoted`, `command`, `posix` (`true` only) | `{}` |
| `set_session_marker` | `pane`, `value` | `{}` |
| `pane_facts` | `pane` | `command` (foreground process name), `in_mode` (`0`), `dead` (`0`/`1`), `session_marker` |
| `read_output` | `pane` | `text`: the visible screen, trailing empty lines removed |
| `deliver_text` | `pane`, `text` | `{}` |
| `enter` | `pane` | `{}`: writes a carriage return |
| `topology` | `pane` | `container`, `pane_count` (`1`) |
| `container_exists` | `container` | `container` |
| `list` | none | `panes`: `[{pane, container, command, dead, marker}]` |
| `focus` | `pane` | `{}`: records the focus request |
| `close_container` | `container` | `{}`; an unknown container is not an error |

The definition checks the protocol at the start of `create_container`, before any pane exists, so `kander start` and `kander resume` (which opens a new pane) both fail early:

- the plugin answers `upgrade: kander` (its protocol is newer): `upgrade kander`;
- any other protocol than the definition's own: `upgrade the kander-dsh plugin`;
- no answer (no host, or a plugin without `hello`): the error names the socket and asks to start dsh Web or dsh Desktop with the plugin.

Writes to one pane reach the program in call order; `deliver_text` writes the text and the definition sends `enter` 500 ms later, as the tmux definition does. `focus` cannot switch the dsh UI from the server side; it records `{pane, container, cwd, at}` (`at` in epoch milliseconds), which the Kander view reads with `KanderHost.lastFocus()`.

## JavaScript Interface

`KanderHost` (`lib/host.js`) is the interface the page routes of the plugin use:

| Method | Returns |
| --- | --- |
| `isCarrier()` | whether this host holds the socket |
| `carrierStatus()` | `{carrier, socket, reason, holderPid}` |
| `listPanes()` | `[{pane, container, label, project, cwd, exited, command, inputWaiting}]`; `project` is the main worktree Kander sent with `create_container` |
| `subscribe(pane, listener)` | an unsubscribe function; `listener` receives the buffered output, each new chunk, then `null` after the program exited |
| `write(pane, data)` | a promise; writes terminal input |
| `resize(pane, cols, rows)` | a promise; positive integers only |
| `lastFocus()` | the latest focus request or `null` |

The pane's screen already answers the program's terminal queries. A page that shows the pane in its own terminal emulator must forward only the user's input to `write`, not that emulator's query replies, or the program receives every answer twice.

`inputWaiting` is `true` when `inspectForeground()` of the dsh subprocess service returns `inputWaiting: true` for the pane (its foreground process group reads terminal input), `false` when it returns `false`, and `null` when it returns no foreground group or fails. An unknown pane throws `Gone`.

## Lifecycle

- The host stops: its panes' programs end with it (the plugin terminates them on dispose; a killed host closes their PTYs). While no host runs, `kander check` reports `unknown` (the socket does not answer).
- A host starts again, or another host takes over: the old pane IDs are unknown to it, so `kander check` reports `stopped`, and `kander resume` opens a new pane.
- `kander dismiss` sends the agent's exit command and closes the container.

## Installation

The plugin is not published to npm. Install it from a checkout of this repository into each dsh profile that should run it; quit that dsh host first.

dsh Web:

```sh
dsh plugin --profile web add /path/to/kander/integrations/kander-dsh
dsh web
```

dsh Desktop is the Electron package of the full dsh Web and loads plugins the same way, from its own profile:

```sh
dsh plugin --profile desktop add /path/to/kander/integrations/kander-dsh
```

Then start dsh Desktop. A plugin installed in both profiles serves both hosts; the first host to start is the carrier. The Desktop installation is not verified on the development machine (no dsh Desktop there).

Kander side: `curl` on `PATH` and the same `DSH_HOME` as the dsh host (or neither sets it). Check the setup with `kander terminal test dsh` while a dsh host with the plugin runs.

## Security

The socket can start processes as the current user, type into agents and read their screens. Its trust boundary is that of the tmux and herdr sockets: the current user and the user's own processes. The state directory is created or tightened to 0700, must be a real directory owned by the user, and the socket is 0600 before it becomes reachable. The plugin listens on no network address. A malicious process of the same user is not defended against.

## Verification

- `node --test integrations/kander-dsh/test/` (Node 22 or later): socket operations, gone classification, close failures, terminal query replies, carrier election among hosts that start together on a missing or stale socket, takeover and step-down, pane ID uniqueness across processes, protocol version, foreground name on Linux and macOS (simulated `ps`), permissions.
- `go test ./internal/terminal/builtin -run Dsh`: definition shape, socket path, argv, protocol failures, gone rules.
- CI runs the plugin tests on Linux and macOS (`platform-checks.yml`, job `kander-dsh`).

## Not Supported

Windows; dsh sessions or jobs for agents (a blank session is hidden by dsh, and a finished job starts a model turn); forwarding terminals from a non-carrier host to the carrier; a reminder when dsh Desktop quits with running agents; `kander attach`.
