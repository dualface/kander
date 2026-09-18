# Terminal Backends

Kander controls terminal sessions (such as `herdr`, `tmux`, or direct process launchers) exclusively through the `internal/terminal` package.

This document describes the `terminal.Backend` operation interface, the `WINDOW` address contract, backend capability flags, and the architectural isolation rule that keeps callers off raw terminal command lines.

---

## 1. The Iron Rule of Terminal Isolation

To ensure stability across heterogeneous terminal multiplexers and OS platforms, Kander enforces strict architectural boundaries:

```text
[ launch / liveness / notify / takeover / focus / tui ]
                         |
                         | (Backend Interface & Capabilities)
                         v
                internal/terminal
                         |
       +-----------------+-----------------+
       |                                   |
[ Declarative Backends ]          [ Direct Backends ]
 (herdr, tmux, tmux-session)       (foreground, console)
```

1. **No Raw Terminal Commands**: Callers (`launch`, `liveness`, `notify`, `takeover`, `focus`, `tui`) **never** construct command-line argument lists or execute terminal binaries directly. They resolve a backend via `terminal.Lookup`, `terminal.ParseWindow`, or `terminal.ResolveAuto`, and invoke `terminal.Backend` methods.
2. **Feature Degradation via Capabilities**: Callers degrade based on `terminal.Capabilities`, not string comparisons on launcher names. For example, checking for container support tests `caps.Has(terminal.CapContainer)` rather than matching `"herdr"` or `"tmux"`.
3. **Strict Dependency Hierarchy**: `internal/terminal` never imports higher-level orchestration packages (`launch`, `liveness`, `notify`, `tui`, etc.).

---

## 2. Package Organization

| Package | Role |
|---|---|
| `internal/terminal` | Defines `Backend` interface, `Address`, `Target`, `PaneFacts`, `Topology`, and `DeclarativeBackend`. |
| `internal/terminal/builtin` | Registers built-in declarative definitions (`definitions/herdr.json`, `tmux.json`) and Go hooks. |
| `internal/terminal/herdr` | Specialized socket hooks for herdr session reporting and pane focusing. |
| `internal/terminal/direct` | Containerless backends (`foreground`, `console`). Container operations return `terminal.ErrUnsupported`. |
| `internal/terminal/terminaltest`| Test harnesses running the test binary as a mock terminal. |

---

## 3. The `WINDOW` Address Contract

Task cards record their associated terminal address in the `WINDOW` field formatted as:

$$\text{WINDOW} = \langle\text{launcher}\rangle{:}\langle\text{opaque}\rangle$$

Only the owning backend parses and formats the opaque component:

| Launcher | Opaque Format | Parsed `Address` Structure |
|---|---|---|
| `herdr` | `<tab-id>:<pane-id>` | `Container` = tab (`w0:...`), `Pane` = pane |
| `tmux` | `<session-id>:<window-id>:<pane-id>` | `Session` = `$0`, `Container` = `@1`, `Pane` = `%1` |
| `tmux-session` | `<session-name>:<window-id>:<pane-id>` | `Session` = `kander-...`, `Container` = `@1`, `Pane` = `%1` |
| Declarative user definition | Colon-separated values matching definition schema | Maps to named definition address fields |
| `foreground` / `console` | *(none)* | Bare launcher name; not parseable |

---

## 4. Capability Flags

Backends declare supported features via `terminal.Capabilities`:

| Capability | Description | herdr | tmux / tmux-session | foreground / console |
|---|---|:---:|:---:|:---:|
| `Container` | Can spawn isolated tabs/windows | Yes | Yes | No |
| `Focus` | Can switch active window/pane focus | Yes | Yes | No |
| `PaneMetadata` | Can read window/pane variables | No | Yes | No |
| `ForegroundProcess` | Can inspect active foreground PID/name | No | Yes | No |
| `AgentIdentity` | Supports agent-reported identity checks | Yes | No | No |
| `SessionReport` | Supports bidirectional session handshake | Yes | No | No |
| `WaitOutput` | Native output pattern matching | Yes | No *(polled)* | No |
| `POSIXOnly` | Restricted to POSIX platforms | No | Yes | No |
| `Detached` | Runs as an unmanaged detached process | No | No | console only |

---

## 5. Operations Reference

Every operation receives a `terminal.Conn` containing the resolved binary path and execution runner:
- `ProbeRunner`: Bounded by a deadline or default timeout, owns the child process tree.
- `SpawnRunner`: Enforces context deadlines without process reaping.

| Operation | Purpose |
|---|---|
| `Prepare` | Resolves target binary, platform requirements, and environment variables. |
| `CreateContainer` | Allocates a new tab/window and returns its `Address`. |
| `WaitReady` | Waits until the container shell/pane is responsive. |
| `RunCommand` | Dispatches an initial startup command line to the pane. |
| `SetSessionMarker` | Statically tags a pane with a Kander session reference. |
| `ReportSession` | Actively reports agent startup identity to the multiplexer socket. |
| `PaneFacts` | Inspects pane liveness, foreground process, and marker metadata. |
| `ReadOutput` | Captures plain text from the pane screen buffer. |
| `WaitOutput` | Bounded wait for a specific text pattern to appear on screen. |
| `DeliverText` | Sends interactive input followed by Enter into the pane. |
| `Focus` | Brings the container tab and pane to the user's foreground. |
| `CloseContainer` | Cleanly shuts down the container tab or window. |

---

## 6. Error Classification

Operations return structured errors mapped to `*terminal.CommandError`:

1. **`KindExec`**: The command binary could not be started (e.g. missing executable or context deadline).
2. **`KindExit`**: Non-zero exit code or expired poll step. Contains raw `Code` and `Stderr`.
3. **Response Syntax Errors**: `KindNotJSON`, `KindNotObject`, `KindMissingResult`, `KindInvalidResponse` identify malformed output from declarative command steps.
4. **Missing Container**: A vanished pane or tab is returned as a structured fact (`PaneFacts.Gone = true`), never as an unhandled error.
5. **Unsupported Operation**: Calling an unsupported feature returns `terminal.ErrUnsupported`.
