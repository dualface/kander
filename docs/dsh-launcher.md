# dsh Launcher

The `dsh` launcher runs a card's executing agent inside a DeepSeek Harness (dsh) host process: dsh Web (`dsh web`) or dsh Desktop. The agent is a child of the dsh host, in a terminal the `kander-dsh` plugin opens, and the plugin's Kander view in dsh is where the user watches it. Kander drives that terminal through the embedded terminal definition [`dsh.json`](../internal/terminal/builtin/definitions/dsh.json), the same way it drives tmux or herdr.

The launcher is POSIX-only (Linux and macOS). It never takes part in `auto`: whether a dsh host with the plugin runs cannot be read from the environment. Select it with `launcher: dsh` in the configuration or `kander start --launcher dsh <task-id>`.

## Parts

| Part | Location | Role |
| --- | --- | --- |
| Terminal definition | `internal/terminal/builtin/definitions/dsh.json` | Maps every `terminal.Backend` operation to one `curl --unix-socket` call on the plugin socket |
| Plugin, host half | `integrations/kander-dsh/lib/` | Pane multiplexer, socket server, carrier election, JavaScript interface for the page routes |
| Plugin, page half | `integrations/kander-dsh/lib/client.js`, `boards.js`, `routes.js` | The Kander view: board, agent terminal tabs, HTTP routes; see [dsh Kander view](dsh-kander-view.md) |

Host half modules:

| Module | Responsibility |
| --- | --- |
| `index.js` | dsh plugin entry (`apply(ctx, config)`): builds one `KanderHost`, stops it with the plugin, and registers the view routes |
| `host.js` | `KanderHost`: composes the parts below and exposes the JavaScript interface |
| `mux.js` | One container per pane, one PTY per pane through `ctx.subprocess.spawnTerminal`, a headless screen per pane (vendored `@xterm/headless` 5.5.0) for `read_output` and terminal query replies, an output ring for subscribers |
| `carrier.js` | Which host of a `DSH_HOME` holds the socket |
| `lock.js` | The carrier lock: an exclusive file lock on a file description the dsh host itself keeps open, released only when the host releases it or dies |
| `processes.js` | Whether a process exists, for removing names left by dead hosts |
| `ops.js` | The socket operations |
| `protocol.js` | Socket protocol version, HTTP framing, error classes |
| `ids.js` | Persistent pane and container IDs |
| `foreground.js` | Foreground process name: `/proc/<pgid>/comm` on Linux, `ps -o comm= -p <pgid>` elsewhere |
| `paths.js` | The state directory and its permissions |

## Panes, Containers and IDs

A pane is one PTY that the dsh host opens with `ctx.subprocess.spawnTerminal`. A container holds exactly one pane, and every agent launch creates a new container. `WINDOW` is `dsh:<container>:<pane>`, for example `dsh:c7-81234:p7-81234`.

IDs are `p<n>-<pid>` and `c<n>-<pid>`, where `<pid>` is the dsh host pid. `<n>` is read from `<state dir>/sequence` (mode 0600), incremented, and written back before it is used. Only the carrier creates panes and the carrier lock lets one host at a time be the carrier (see "Carrier Host"), so the read and the write never interleave between hosts; a host also never goes below the last number it took itself. So within one `DSH_HOME`, IDs never repeat across the hosts or across host restarts, and an old card's `WINDOW` never names a later pane. A `sequence` file that does not hold a number makes pane creation fail instead of restarting at 1.

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

- The carrier is the host that holds the carrier lock `<state dir>/carrier.lock` (mode 0600), an exclusive `flock(2)`-style lock on a file description the dsh host process keeps open itself. The lock lasts exactly as long as that descriptor: it ends when the host releases it or dies, never earlier, so a host that still runs never stops being the carrier behind its own back, and a paused host keeps it until it resumes or dies. No host ever judges whether a lock was left over. Node has no file locking, so on Linux the host opens the file and lets `flock -n -x 3` lock that descriptor (passed as fd 3); `flock` exits at once and the lock stays with the host. macOS opens the file with `O_EXLOCK | O_NONBLOCK`, which takes the same lock in `open(2)`. No helper process waits for the lock.
- Every other host tries the lock once a second. It reads the socket's `hello` and writes `<socket> is held by another dsh host (pid N); this host does not host kander panes and waits for the carrier lock` to the dsh host log (the cordis logger of the plugin). Once the carrier releases the lock or dies, the next try succeeds, so that host becomes the carrier within a second, without a restart. A host that is not the carrier stops at once.
- Holding the lock makes the carrier the only host that touches the socket path, so a socket it finds there is left over (a host killed with SIGKILL leaves one) and is replaced. A path that is not a socket is never replaced; the host logs it, releases the lock, and tries again a second later. The carrier binds its server to a temporary name `.mux-<pid>-<n>` in the same directory, sets it to 0600, and links it onto the path, so the socket is private before it becomes reachable. It also removes the temporary names of hosts that are gone.
- The carrier checks the path every second; a path changed or removed from outside is linked to its socket again.
- A carrier that stops removes its socket, closes its server, ends its panes, and only then releases the lock, so the next carrier never starts beside panes that are still running. Closing a server unlinks only its temporary name, never the path.

`KanderHost.carrierStatus()` returns `{carrier, socket, reason, holderPid}`; `reason` is `carrier`, `held`, `waiting`, `blocked`, `unreachable`, `starting` or `stopped`. A non-carrier lists no panes, and its Kander view shows dsh agents by status only.

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

On Linux the dsh host needs `flock` (util-linux) on its `PATH`; macOS needs no tool. Kander side: `curl` on `PATH` and the same `DSH_HOME` as the dsh host (or neither sets it). Check the setup with `kander terminal test dsh` while a dsh host with the plugin runs.

## Security

The socket can start processes as the current user, type into agents and read their screens. Its trust boundary is that of the tmux and herdr sockets: the current user and the user's own processes. The state directory is created or tightened to 0700, must be a real directory owned by the user, and the socket is 0600 before it becomes reachable. The plugin listens on no network address. A malicious process of the same user is not defended against.

## Verification

- `node --test integrations/kander-dsh/test/` (Node 22 or later): socket operations, gone classification, close failures, terminal query replies, carrier election among hosts that start together on a missing or stale socket, repeated handovers with one carrier at a time, a paused carrier process that keeps the carrier until it dies, a non-carrier that stops at once, a carrier that ends its panes before the handover, the carrier lock across processes (mutual exclusion, kept by a stopped holder, released on holder death, a missing flock), pane ID continuity, protocol version, foreground name on Linux and macOS (simulated `ps`), permissions.
- `go test ./internal/terminal/builtin -run Dsh`: definition shape, socket path, argv, protocol failures, gone rules.
- CI runs the plugin tests on Linux and macOS (`platform-checks.yml`, job `kander-dsh`).

## Not Supported

Windows; dsh sessions or jobs for agents (a blank session is hidden by dsh, and a finished job starts a model turn); forwarding terminals from a non-carrier host to the carrier; a reminder when dsh Desktop quits with running agents; `kander attach`.
