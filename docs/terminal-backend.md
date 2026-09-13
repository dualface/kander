# Terminal Backends

Kander reaches a terminal (herdr, tmux, or a direct process launcher) only through the `internal/terminal` package. This document describes the operation set, the address contract, the capability flags, and the rule that keeps callers off terminal command lines.

## The Rule

- Callers (`internal/launch`, `internal/liveness`, `internal/notify`, `internal/takeover`, `internal/focus`, `internal/menu`, `internal/tui`) never build a herdr or tmux argv and never execute those binaries. They look a backend up by launcher name (`terminal.Lookup`, `terminal.ParseWindow`, `terminal.ResolveAuto`, `terminal.FindBackend`) and call `terminal.Backend` methods.
- Callers degrade on `terminal.Capabilities`, not on launcher names. For example, "has a container" replaces `launcher == "herdr" || launcher == "tmux" || launcher == "tmux-session"`, and "reports agent identity" selects the herdr-style liveness and delivery policy.
- `internal/menu` still names specific launchers where the product UI is about one tool (install tmux, herdr installer, doctor hints). It uses the constants exported by the backend packages (`tmux.Name`, `herdr.Name`, `direct.Foreground`) instead of string literals.
- `internal/terminal` and its subpackages never import the callers above. `internal/board`, `internal/config`, `internal/fs`, `internal/process` and `internal/i18n` never import `internal/terminal`. `internal/terminal/imports_test.go` enforces both directions.

## Packages

| Package                      | Responsibility                                                                 |
| ---------------------------- | ------------------------------------------------------------------------------ |
| `internal/terminal`          | `Backend` interface, `Address`/`Target`/`PaneFacts`/`Topology` types, `CommandError` classification, runners, registry, auto resolution |
| `internal/terminal/tmux`     | The `tmux` and `tmux-session` launchers; one implementation, `tmux-session` addresses a per-project session by name |
| `internal/terminal/herdr`    | The `herdr` launcher, including the herdr socket session report and pane focus |
| `internal/terminal/direct`   | `foreground` and `console`: no container; every container operation returns `terminal.ErrUnsupported` |
| `internal/terminal/builtin`  | Registers the built-in backends; import it for its side effect                |

`internal/probe` keeps only generic process execution (`CaptureContext`, `CaptureWithEnv`), probe budgets (`WithDefaultTimeout`, `TimeoutContext`) and failure localization (`FailureDetail`).

## Registry and Launcher Names

- `terminal.Register` records a backend and calls `config.RegisterLauncherNames` with its name; `internal/terminal` itself registers `auto`. Registration is idempotent.
- `internal/config` keeps the six built-in names as the default set, so configuration validation works when `config` is used alone. Registered names are appended after the defaults (`config.LauncherNames`, `config.ValidLauncherName`). `config` never imports `terminal`.
- In the full binary the command packages import `internal/terminal/builtin`, so every backend is registered during package initialization, before `main` loads or validates configuration (`cmd/kander/launcher_registration_test.go`).
- Auto resolution follows registration order (`herdr` first, then `tmux`) and skips POSIX-only backends on Windows.

## Address Contract

`WINDOW` is `<launcher>:<opaque>`. Only the backend encodes and decodes the opaque part:

| Backend                | Opaque part                    | Parsed `Address`                                  |
| ---------------------- | ------------------------------ | ------------------------------------------------- |
| herdr                  | `<tab-id>:<pane-id>` (both `w<n>:...`) | `Container`=tab, `Pane`=pane                |
| tmux                   | `<session-id>:<window-id>:<pane-id>`   | `Session`=session id, `Container`=window, `Pane` |
| tmux-session           | `<session-name>:<window-id>:<pane-id>` | `Session`=session name, `Container`=window, `Pane` |
| foreground / console   | none; WINDOW is the bare name  | not parseable                                     |

`terminal.FormatAddress` renders the complete value; `Backend.OpaqueAddress` renders the part without the launcher prefix (used for start and takeover reports). `ParseFocusAddress` additionally accepts herdr ids without a workspace prefix for the read-only focus path.

## Capabilities

| Flag                | herdr | tmux / tmux-session | foreground | console |
| ------------------- | ----- | ------------------- | ---------- | ------- |
| `Container`         | yes   | yes                 |            |         |
| `Focus`             | yes   | yes                 |            |         |
| `PaneMetadata`      |       | yes                 |            |         |
| `ForegroundProcess` |       | yes                 |            |         |
| `AgentIdentity`     | yes   |                     |            |         |
| `SessionReport`     | yes   |                     |            |         |
| `WaitOutput`        | yes   |                     |            |         |
| `POSIXOnly`         |       | yes                 |            |         |
| `Detached`          |       |                     |            | yes     |

## Operations

Every operation takes a `terminal.Conn` (resolved executable plus runner) so the caller keeps the process and deadline semantics of its call site: `ProbeRunner` bounds a command by the caller deadline or the default probe budget and owns the process tree; `SpawnRunner` runs a plain child and only enforces a deadline present on the context; `SpawnRunnerWithin` bounds each command separately.

| Operation          | Input                               | Output                          |
| ------------------ | ----------------------------------- | ------------------------------- |
| `Prepare`          | project, command, platform, PATH lookup, environment, TTY | `Target` (program, session, workspace) |
| `CreateContainer`  | `Target`, cwd, label                | `Address` of the new container and pane |
| `WaitReady`        | pane                                | ready or error                  |
| `RunCommand`       | pane, one-line command, POSIX flag  | started or error                |
| `SetSessionMarker` | pane, session reference             | recorded or error               |
| `ReportSession`    | pane, agent, reference, deadline    | reported and read back, `ErrNoReportChannel`, or error |
| `PaneFacts`        | pane                                | `PaneFacts` (gone, process facts, marker, agent identity, container) |
| `ReadOutput`       | pane                                | pane text                       |
| `WaitOutput`       | pane, match, timeout ms             | matched or error                |
| `DeliverText`      | pane, text                          | delivered (text plus Enter) or error |
| `Topology`         | address                             | recorded session, container, pane count or pane list |
| `ContainerExists`  | address                             | exists / gone                   |
| `ReverseLookup`    | agent, reference, lazy process name | unique `Address`, or `*MatchError` with the match count |
| `Focus`            | address                             | `FocusResult` (success, message id, arguments) |
| `CloseContainer`   | address                             | closed or error                 |
| `StartedLines`     | report head, target, address        | launch report lines             |

Polling and multi-step operations: `CreateContainer` on tmux-session retries once as a new window when a concurrent start created the session; `PaneFacts` on tmux reads `@kander_session` and falls back to `@onevoke_session`; `ReportSession` is one attempt, and the caller retries within its budget; tmux has no native `WaitOutput`, so callers poll `ReadOutput`; `Focus` on tmux is three commands, and on herdr a tab focus plus a best-effort socket pane focus.

## Error Classification

- A gone pane is a fact (`PaneFacts.Gone` with the tool's diagnostic), not an error. Missing tmux user options are treated as an empty marker.
- A failed command returns `*terminal.CommandError`. `Kind` is `KindExec` (the command could not run, `Cause` holds the error), `KindExit` (non-zero exit, raw `Code`/`Stderr`), or one of the response kinds `KindNotJSON`, `KindNotObject`, `KindMissingResult`, `KindInvalidResponse`.
- `Message` is the diagnostic of the operation's primary caller. Other callers render their own existing diagnostic from `Kind`, `Cause` and `Detail()`, so user-visible messages stay the same at every call site.
- Deadline and cancellation errors are returned unchanged, so `probe.FailureDetail` still localizes them.
- `terminal.ErrUnsupported` marks an operation the backend does not provide.
