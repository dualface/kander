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
 (dsh, herdr, herdr-core, luvus,   (foreground, console)
  tmux, tmux-session)
```

1. **No Raw Terminal Commands**: Callers (`launch`, `liveness`, `notify`, `takeover`, `focus`, `tui`) **never** construct command-line argument lists or execute terminal binaries directly. They resolve a backend via `terminal.Lookup`, `terminal.ParseWindow`, or `terminal.ResolveAuto`, and invoke `terminal.Backend` methods.
2. **Feature Degradation via Capabilities**: Callers degrade based on `terminal.Capabilities`, not string comparisons on launcher names. For example, checking for container support tests `caps.Has(terminal.CapContainer)` rather than matching `"herdr"` or `"tmux"`.
3. **Strict Dependency Hierarchy**: `internal/terminal` never imports higher-level orchestration packages (`launch`, `liveness`, `notify`, `tui`, etc.).

---

## 2. Package Organization

| Package | Role |
|---|---|
| `internal/terminal` | Defines `Backend` interface, `Address`, `Target`, `PaneFacts`, `Topology`, `DeclarativeBackend`, and the optional extensions `ProcessInspector`, `AgentLister` and `ContainerPlacer`. |
| `internal/terminal/builtin` | Registers built-in declarative definitions (`definitions/dsh.json`, `herdr.json`, `herdr-core.json`, `luvus.json`, `tmux.json`) and Go hooks. |
| `internal/terminal/herdr` | Specialized socket hooks for herdr session reporting and pane focusing. Both dial one control channel from `HERDR_SOCKET_PATH`: a unix socket on POSIX, the named pipe `\\.\pipe\` plus that path on Windows. |
| `internal/terminal/herdrcore` | The herdr-core `place_container` hook: the caller's space, a new tab ID, and the tab name and last place in its space through the `herdr-app` layout document. |
| `internal/terminal/direct` | Containerless backends (`foreground`, `console`). Container operations return `terminal.ErrUnsupported`. |
| `internal/terminal/terminaltest`| Test harnesses running the test binary as a mock terminal. |

---

## 3. The `WINDOW` Address Contract

Task cards record their associated terminal address in the `WINDOW` field formatted as:

$$\text{WINDOW} = \langle\text{launcher}\rangle{:}\langle\text{opaque}\rangle$$

Only the owning backend parses and formats the opaque component:

| Launcher | Opaque Format | Parsed `Address` Structure |
|---|---|---|
| `dsh` | `<container>:<pane>` | `Container` = `c7-1234`, `Pane` = `p7-1234` (sequence number and dsh host pid) |
| `herdr` | `<tab-id>:<pane-id>` | `Container` = tab (`w0:...`), `Pane` = pane |
| `herdr-core` | `<terminal>` | `Container` = `Pane` = hc terminal address (`t8`) |
| `luvus` | `<pane-id>:<pane-id>` | `Container` = `Pane` = luvus pane id (`7`) |
| `tmux` | `<session-id>:<window-id>:<pane-id>` | `Session` = `$0`, `Container` = `@1`, `Pane` = `%1` |
| `tmux-session` | `<session-name>:<window-id>:<pane-id>` | `Session` = `kander-...`, `Container` = `@1`, `Pane` = `%1` |
| Declarative user definition | Colon-separated values matching definition schema | Maps to named definition address fields |
| `foreground` / `console` | *(none)* | Bare launcher name; not parseable |

---

## 4. Capability Flags

Backends declare supported features via `terminal.Capabilities`:

| Capability | Description | dsh | herdr | herdr-core | luvus | tmux / tmux-session | foreground / console |
|---|---|:---:|:---:|:---:|:---:|:---:|:---:|
| `Container` | Can spawn isolated tabs/windows | Yes | Yes | Yes | Yes | Yes | No |
| `Focus` | Can switch active window/pane focus | Yes *(recorded for the Kander view)* | Yes | No | Yes | Yes | No |
| `PaneMetadata` | Can read window/pane variables | Yes | No | Yes *(label)* | No | Yes | No |
| `ForegroundProcess` | Can inspect active foreground PID/name | Yes | No | No | No | Yes | No |
| `AgentIdentity` | Supports agent-reported identity checks | No | Yes | Yes | Yes | No | No |
| `ProgramStatus` | Identity and status come from OSC 7501 reports | No | No | Yes | No | No | No |
| `SessionReport` | Supports bidirectional session handshake | No | Yes | No | No | No | No |
| `WaitOutput` | Native output pattern matching | No *(polled)* | Yes | No *(polled)* | No *(polled)* | No *(polled)* | No |
| `POSIXOnly` | Restricted to POSIX platforms | Yes | No | Yes | Yes | Yes | No |
| `Detached` | Runs as an unmanaged detached process | No | No | No | No | No | console only |

`ProgramStatus` refines `AgentIdentity`: the pane names no agent, only the `status.app` its program reported through OSC 7501 (`PaneFacts.AgentApp`). Callers compare a pane with the expected agent through `PaneFacts.MatchAgent`, which is tri-state: a match, another agent, or undecided while the pane has not reported (or the agent's declaration cannot be read). Undecided is never a stopped observation. `terminal.AddressFreeBackend` (the backend of a `--pane` override or a card without `WINDOW`) skips program status backends, because their terminals carry the session only in the marker Kander wrote at launch.

---

## 5. Operations Reference

Every operation receives a `terminal.Conn` containing the resolved binary path and execution runner:
- `ProbeRunner`: Bounded by a deadline or default timeout, owns the child process tree.
- `SpawnRunner`: Enforces context deadlines without process reaping.

A definition step that declares `unset_env` does not use `Conn.Run`; it runs through `Conn.RunEnv`, or `ProbeEnvRunner` when that is nil, so no runner can leave a listed variable in the child environment (see [Terminal definitions](terminal-definitions.md#steps)).

| Operation | Purpose |
|---|---|
| `Prepare` | Resolves target binary, platform requirements, and environment variables. |
| `CreateContainer` | Allocates a new tab/window and returns its `Address`. |
| `WaitReady` | Waits up to the given millisecond budget until the container shell/pane is responsive. |
| `RunCommand` | Dispatches an initial startup command line to the pane. It also receives the startup directory, quoted by the caller for a POSIX pane shell. tmux enters that directory before `exec`; other backends ignore it. |
| `PaneDirectory` | Optional `PaneDirectoryReader` extension: the working directory of the pane's foreground process. A launch uses it to confirm the pane entered the startup directory. |
| `SetSessionMarker` | Statically tags a pane with a Kander session reference. |
| `ReportSession` | Actively reports agent startup identity to the multiplexer socket. |
| `PaneFacts` | Inspects pane liveness, foreground process, marker metadata, and the reported session identity (`AgentSession` value plus `AgentSessionKind`). |
| `ReadOutput` | Captures plain text from the pane screen buffer. |
| `WaitOutput` | Bounded wait for a specific text pattern to appear on screen. |
| `DeliverText` | Sends interactive input followed by Enter into the pane. |
| `Focus` | Brings the container tab and pane to the user's foreground. |
| `CloseContainer` | Cleanly shuts down the container tab or window. |

---

## 6. Session Identity Resolution

`ProcessInspector` is an optional extension, independent of the required `Backend` interface. `ProcessFacts(ctx, conn, pane)` returns foreground process IDs and names observed for exactly that pane. Old Go backends and definitions remain valid; a declarative backend without `process_facts` returns `ErrUnsupported`. The embedded herdr and hc operations validate pane ownership before exposing process facts. hc exposes its observed foreground group leader, not a complete process tree; absent foreground information yields no evidence. It does not enable agent-session repair on a program-status backend. The launch layer combines them with Linux process incarnation and open-file evidence to repair only missing Codex identity; terminal process facts alone cannot prove a session match.

`AgentLister` is a second optional extension, for `AgentIdentity` backends. `AgentList(ctx, conn)` reports every agent pane (`AgentPane`: pane, agent, status, session value and kind, plus optional program-status app and session marker) in one call within the default probe budget; a declarative backend without `agent_list` returns `ErrUnsupported`. A pane missing from the list is not proof that it is gone, so callers confirm it with `PaneFacts`. `liveness.ObserveAgentStatus` uses it for the TUI agent status. `AgentPane.Facts` preserves the program-status flag even before the app reports and shares status normalization with single-pane observations.

`Target.Title` is the display title of a new container, such as a tab name. The launch layer sets it for no launch: task agents (start, resume takeover, notify recovery), orchestrators, chat, issue triage and refinement all leave it empty, so every tab is named by the container label (`kb-<card title>`, `orchestrate-<first task ID>`, `chat-<time>`, `issue-<repository>-<number>`). An empty title means the container label. The field stays for definitions and placers: a definition sees it as `{title}` in `create_container`, and `place_container` receives it as `title`.

`ContainerPlacer` is a third optional extension. `PlaceContainer(conn, address, target, label)` runs right after `CreateContainer` succeeds and returns a warning string, empty when nothing went wrong or nothing needed placing. It is separate from `CreateContainer` because that returns no warnings; placement never fails a launch. `launchAgent` appends a non-empty warning to the launch result warnings, or prints it to stderr when the caller collects none. A declarative backend implements it through the `place_container` hook and does nothing without one; Go backends such as `direct` do not implement it. `kander terminal test` calls it on the container it created, like a launch, so with `herdr-core` inside an Herdr App terminal the test terminal also opens a tab, which closes with the terminal.

`SessionReport.Context` carries caller cancellation through the declarative hook, socket handshake and read-back. A nil context preserves the existing deadline-only startup reporting behavior. Session reporting remains best-effort on startup; notification repair requires successful read-back and revalidation before ordinary delivery.

Backends with `AgentIdentity` report the pane's session as a kind plus a value (`AgentSessionKind`, `AgentSession`). A missing kind is a legacy direct id. `terminal.MatchAgentSession(ctx, agent, kind, value, reference)` is the single tri-state comparison every consumer shares:

- **Match**: the reported identity resolves to the recorded card reference. `id` (or absent kind) compares verbatim — ids are compared as strings, never parsed as UUIDs.
- **Differ**: a resolved identity that names a different session.
- **Uncertain**: the identity is missing, of an unknown kind, or could not be resolved — evidence of neither a match nor a mismatch, and never a `stopped` observation.

`kind: "path"` resolves through the agent definition's `session.file` declaration: the file is opened only through the `internal/fs` safe boundary (no symlinks, reparse points, or special files; the POSIX leaf opens non-blocking so a FIFO fails instead of hanging), the first line is read within the declared `max_bytes`, and the JSON header must carry the declared `id_field` and, when configured, the matching `type_field`/`type_value`. Unsupported formats, unreadable or missing files, oversized headers, malformed headers, and agents without a declaration all return Uncertain with a diagnosable detail. File content is never executed, never scanned beyond the bounded first line, and checks the caller's context before and after the read inside the existing probe budget.

`ReverseLookup` evaluates every row the terminal reports (not just the recorded tab/workspace): rows that pass `expect`, `checks`, and the declarative `match` (agent filter) become candidates whose session identity is resolved the same way. Exactly one resolved match is the answer; zero decided matches return `terminal.MatchError{Matches:0}`; several decided matches return `MatchError` with the count; any undecidable same-agent candidate returns `terminal.IncompleteLookupError` — an unfinished lookup that callers must treat as unknown, never as a unique match or a proven absence.

---

## 7. Error Classification

Operations return structured errors mapped to `*terminal.CommandError`:

1. **`KindExec`**: The command binary could not be started (e.g. missing executable or context deadline).
2. **`KindExit`**: Non-zero exit code or expired poll step. Contains raw `Code` and `Stderr`.
3. **Response Syntax Errors**: `KindNotJSON`, `KindNotObject`, `KindMissingResult`, `KindInvalidResponse` identify malformed output from declarative command steps.
4. **Missing Container**: A vanished pane or tab is returned as a structured fact (`PaneFacts.Gone = true`), never as an unhandled error.
5. **Unsupported Operation**: Calling an unsupported feature returns `terminal.ErrUnsupported`.
6. **Lookup Outcomes**: `terminal.MatchError` (completed lookup, zero or several matches) and `terminal.IncompleteLookupError` (undecidable candidates, lookup not finished) keep absence, ambiguity, and undecidability distinct.

---

## 8. The Built-In luvus Launcher

The embedded [`luvus.json`](../internal/terminal/builtin/definitions/luvus.json) provides the `luvus` launcher for running Kander from a [luvus](https://github.com/RizRiyz/luvus) pane. It is POSIX-only.

- **Prerequisites**: `luvus` on `PATH`, `LUVUS_ENV=1` and a non-empty `LUVUS_PANE_ID` (both set by luvus in its panes). `auto` picks luvus with priority 150, between herdr (200) and tmux (100), when `LUVUS_ENV=1`.
- **Minimum version**: the first luvus release whose `pane split` accepts `--cwd` (upstream PR #480); luvus 0.14.3 and earlier do not. The luvus server must also run that version: restart it after upgrading.
- **Container**: `pane split <LUVUS_PANE_ID> --no-focus --cwd <cwd>`, then `pane move --new-tab`, `pane name` with the card label, and `pane focus` back to the calling pane. The container is the pane itself, so `WINDOW` is `luvus:<pane>:<pane>`.
- **Directory check**: an older CLI silently ignores `--cwd`, and an older server ignores the directory a newer CLI sends; the split still succeeds in the anchor pane's directory. `create_container` therefore reads the new pane back with `agent get` (polling briefly until the directory is reported) and accepts it only when the reported directory equals the requested one or its symlink-resolved form. Otherwise it closes the new pane and fails with an upgrade message, so `start` rolls the card back to `todo/` and no agent starts.
- **Session identity**: luvus reports an agent's session through its own integration hooks. Run `luvus integration install <agent>` once per agent so `pane_facts` and reverse lookup carry the session id. Kander binds no session hook of its own.
- **Known limits**: the calling pane briefly loses focus while the new pane moves to its tab. Agents without a luvus integration hook (for example cursor and pi) report no session: liveness can still confirm the agent and status of the recorded pane, but direct `notify` delivery is unavailable and a reverse lookup of a moved pane stays incomplete (`unknown`), never `stopped`. A plain shell pane reports the shell name as its agent. Inside nested tmux and luvus, `auto` follows the priorities above.
- A user definition named `luvus.json` in the global or project share directory replaces this embedded definition as a whole; remove an older hand-written copy to use the built-in one.

---

## 9. The Built-In herdr-core Launcher

The embedded [`herdr-core.json`](../internal/terminal/builtin/definitions/herdr-core.json) provides the `herdr-core` launcher. herdr-core is the terminal runtime behind Herdr App; its CLI is `hc`. It is a separate runtime from the herdr TUI, and the two do not share agents. The launcher is POSIX-only and runs every operation through `hc … --json`, except that a `place_container` hook (Go) puts each new terminal into its own named tab. Validated against hc 0.1.20; full-list operations use the public JSON-RPC transport `hc connect --json`.

- **Prerequisites**: `hc` on `PATH` and a running core. Kander never installs `hc`. On Unix every `hc` command, `hc server status` included, starts a missing core (hc 0.1.17), so a launch may start it; `kander doctor` therefore runs only `hc --version` and checks the core by connecting to its socket, resolved the way `hc` does: `HC_SOCKET`, else `socket` in `$XDG_CONFIG_HOME/herdr-core/config.toml`, else `<XDG_RUNTIME_DIR or /run/user/<uid>>/herdr-core/<HC_SESSION or default>.sock`, else the session's state directory. A `socket` setting it cannot read is reported as undecidable. An explicit `--launcher herdr-core` works from any terminal. `auto` picks herdr-core with priority 120, between luvus (150) and tmux (100), when `HC_TERMINAL_ID` is non-empty: a herdr or luvus pane running inside an hc terminal is the inner environment, and `TMUX` can leak into every terminal of a core started from tmux.
- **Agents**: herdr-core identifies agents only by their OSC 7501 (Program Status Protocol) reports. Every start entrance refuses, before any side effect, an agent whose `program_status` declaration is missing or says it does not report (see [Custom execution agents](custom-agents.md#program-status-osc-7501)). There is no fallback for such agents.
- **Container**: `hc run --bg --no-parent --rm --json --cwd <startup dir> --name <label_name> --label kander.window=<label> -- /usr/bin/env PATH=… KANBAN_DIR=… KANDER_CONFIG=… KANDER_LANG=… /bin/sh`. `--no-parent` keeps the terminal alive when the calling terminal ends; `--rm` removes it when the agent exits. The core's own environment does not carry the caller's, so the definition forwards these variables of the Kander process (an unset one is left out); the agent itself starts by its resolved absolute path. `WINDOW` is `herdr-core:<terminal>`.
- **Placement**: Herdr App writes the labels `herdr.space` and `herdr.tab` on a terminal to say which space and tab it belongs to; a terminal without them is not shown in any tab. `create_container` only runs `hc run`; the launch layer then calls `terminal.ContainerPlacer`, whose `place_container` hook (`herdr-core-place-tab`, implemented in `internal/terminal/herdrcore`) gives every new terminal a tab of its own in the caller's space. It reads the calling terminal named by `HC_TERMINAL_ID` with `hc get --json` and takes only the `herdr.space` label (made of `[A-Za-z0-9_-]`); the caller's `herdr.tab` and `where` labels are not copied. It then labels the new terminal with that space and a new random tab ID (`tb` plus 32 lowercase hexadecimal characters), which Herdr App turns into a new tab of that space and names `where` for. Without `HC_TERMINAL_ID`, or when the caller has no valid `herdr.space`, nothing is labelled and the terminal stays outside any tab, as before. The tab is named by the display title (`Target.Title`, or the container label when it is empty; each is taken with control characters removed and surrounding white space trimmed, the first non-empty one is used, and when both are empty the hook stops after labelling, leaves the position Herdr App chose and reads and writes no layout): the hook waits for the tab to show up in the `herdr-app` layout document and writes the title into that tab's `name` and, in the same versioned write, moves that tab to the end of its space's `tabs`, because Herdr App inserts a new tab after the active one and not at the end (see [Terminal definitions](terminal-definitions.md#hooks)). A later reordering by Herdr App is not countered. Every problem is a launch warning, never a failure: the terminal keeps its default tab name or sits outside any tab. Hc has no way to tell that Herdr App is offline, so a caller with `herdr.space` while the App is not connected to the core costs each launch a bounded wait before the warning: the tab is polled for 5 seconds, each `hc` call is capped at 3 seconds, and the whole hook at 15 seconds. These labels and the layout document are a private convention of Herdr App, which Kander does not probe.
- **Start**: `run_command` sends `cd -- <dir> && exec <agent command>` to the shell with `hc send`. After the session is known, `set_session_marker` stores it in the terminal label `kander.session`; `pane_facts` reads it back as the session identity, and `reverse_lookup` calls `terminal.list` through `hc connect --json` and compares each local live terminal's actual label. A remote `machine/t7` is never rebound as a local terminal.
- **State**: `pane_facts` reads `hc get`. A `finished` or `lost` terminal, or one hc no longer knows, is gone, whatever its last status says. A live terminal maps `status.state` onto the agent status: `idle`, `working`, `blocked` and `done` unchanged, `error` as `blocked` with the reason `error`; a `null` status (no report yet) leaves the agent undecided, so liveness reads `unknown` and notify waits. Kander never derives a herdr-core agent state from the screen, the output, or the foreground process.
- **Batch observation**: `agent_list` sends one literal JSON-RPC `terminal.list` request through `hc connect --json`, followed by EOF. Its response carries the full `status.app`, `status.state` and `labels.kander.session`; `hc ls --json` instead flattens status, may substitute a program name for an unreported app, and omits labels. The response ID and version must match and no error may be present. Only local live terminals are returned. Invalid or duplicate rows fail the batch; missing panes are confirmed individually with `get`.
- **Process evidence**: optional `ProcessInspector` reads `get.foreground` (PID and name). This is independent of the `ForegroundProcess` capability, which remains off: hc agent identity and readiness still require OSC reports.
- **Delivery and output**: `send` has a 90-second command budget to cover hc's default 60-second draft hold and submission checks; an earlier caller deadline still applies. Longer hc draft settings can outlast this bounded budget. `hc` wraps text sent from inside an hc terminal in an `<hc from="…">` tag, which turns an agent slash command such as `/exit` into plain prose. The `deliver_text` step therefore declares `unset_env: ["HC_TERMINAL_ID"]`, so `hc` does not see the sending terminal and delivers the text unchanged; `HC_TERMINAL_SOCKET` and `HC_SCOPE` stay, so `hc` still finds the core. The `run_command` send goes to a shell, which hc does not tag, and keeps the full environment. Cancellation is an error and is never retried here. `started: false` means no working transition was observed, not that text was unsent; notification acknowledgement still checks its marker. Reads use hc's default visible-screen mode (`lines=0`), which sends no history-scroll input. `wait --output` only watches output after the call and supports literal matches, so it cannot replace Kander's post-send history/regex checks; `WaitOutput` remains off and those callers retain polling.
- **Close**: `hc kill` then `hc rm`; `not_found` counts as already closed, because an exited agent took its `--rm` terminal with it.
- **Not supported**: focus (herdr-core has no API to switch the Herdr App view), native Agent session reporting, Windows, and remote machines (`hc machine`, `machine/t7` addresses).

---

## 10. The Built-In dsh Launcher

The embedded [`dsh.json`](../internal/terminal/builtin/definitions/dsh.json) provides the `dsh` launcher: the executing agent runs as a child of a DeepSeek Harness (dsh) host process, in a terminal that the `kander-dsh` plugin ([`integrations/kander-dsh/`](../integrations/kander-dsh/)) opens. It is POSIX-only and never chosen by `auto`, because it depends on a running dsh host that no environment variable reveals; select it with `launcher: dsh` or `kander start --launcher dsh`. Every operation is one `curl --unix-socket` call on the plugin socket `$DSH_HOME/kander/mux.sock` (`~/.dsh/kander/mux.sock` without `DSH_HOME`). `create_container` first checks the socket protocol version, so `start` and `resume` fail before any pane exists when the plugin and Kander disagree, and the error names the side to upgrade. `WINDOW` is `dsh:<container>:<pane>`. Architecture, protocol, installation and limits are in [dsh launcher](dsh-launcher.md).
