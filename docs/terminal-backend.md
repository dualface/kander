# Terminal Backends

Kander reaches a terminal (herdr, tmux, or a direct process launcher) only through the `internal/terminal` package. This document describes the operation set, the address contract, the capability flags, and the rule that keeps callers off terminal command lines. herdr and tmux are implemented declaratively: the embedded definitions `internal/terminal/builtin/definitions/herdr.json` and `tmux.json` run through `terminal.DeclarativeBackend`, the same path as user terminal definitions (see [Terminal definitions](terminal-definitions.md)).

## The Rule

- Callers (`internal/launch`, `internal/liveness`, `internal/notify`, `internal/takeover`, `internal/focus`, `internal/menu`, `internal/tui`) never build a herdr or tmux argv and never execute those binaries. They look a backend up by launcher name (`terminal.Lookup`, `terminal.ParseWindow`, `terminal.ResolveAuto`, `terminal.FindBackend`) and call `terminal.Backend` methods.
- Callers degrade on `terminal.Capabilities`, not on launcher names. For example, "has a container" replaces `launcher == "herdr" || launcher == "tmux" || launcher == "tmux-session"`, and "reports agent identity" selects the herdr-style liveness and delivery policy.
- `internal/menu` still names specific launchers where the product UI is about one tool (install tmux, herdr installer, doctor hints). It uses exported constants (`builtin.Tmux`, `builtin.TmuxSession`, `builtin.Herdr`, `direct.Foreground`) instead of string literals, and lists launchers of loaded user definitions as extra choices.
- `internal/terminal` and its subpackages never import the callers above. `internal/board`, `internal/config`, `internal/fs`, `internal/process` and `internal/i18n` never import `internal/terminal`. `internal/terminal/imports_test.go` enforces both directions.

## Packages

| Package                      | Responsibility                                                                 |
| ---------------------------- | ------------------------------------------------------------------------------ |
| `internal/terminal`          | `Backend` interface, `Address`/`Target`/`PaneFacts`/`Topology` types, `CommandError` classification, runners, registry, auto resolution; the terminal definition format, its validation, `DeclarativeBackend`, the hook registry, and loading of user definitions from share directories |
| `internal/terminal/herdr`    | Only the socket session-report and pane-focus hooks used by `herdr.json` |
| `internal/terminal/direct`   | `foreground` and `console`: no container; every container operation returns `terminal.ErrUnsupported` |
| `internal/terminal/builtin`  | Registers the built-in Go backends and the embedded definitions (`definitions/herdr.json` provides `herdr`; `definitions/tmux.json` provides `tmux` and `tmux-session`) and the named socket hooks; import it for its side effect |
| `internal/terminal/terminaltest` | Fake terminal executable (the test binary itself) for declarative backend tests |

`internal/probe` keeps only generic process execution (`CaptureContext`, `CaptureWithEnv`), probe budgets (`WithDefaultTimeout`, `TimeoutContext`) and failure localization (`FailureDetail`).

## Registry and Launcher Names

- `terminal.Register` records a backend and calls `config.RegisterLauncherNames` with its name; `internal/terminal` itself registers `auto`. Registration is idempotent.
- `internal/config` keeps the six built-in names as the default set, so configuration validation works when `config` is used alone. Registered names are appended after the defaults (`config.LauncherNames`, `config.ValidLauncherName`). `config` never imports `terminal`.
- In the full binary the command packages import `internal/terminal/builtin`, so every Go backend and embedded definition is registered during package initialization, before `main` loads or validates configuration (`cmd/kander/launcher_registration_test.go`). Launchers of user definitions reach validation through `config.RegisterLauncherNameSource`, which loads the share directories on first use.
- Auto resolution tries container launchers by descending `auto_priority` (`herdr` 200, `tmux` 100), and skips POSIX-only backends on Windows.

## Address Contract

`WINDOW` is `<launcher>:<opaque>`. Only the backend encodes and decodes the opaque part:

| Backend                | Opaque part                    | Parsed `Address`                                  |
| ---------------------- | ------------------------------ | ------------------------------------------------- |
| herdr                  | `<tab-id>:<pane-id>` (both `w<n>:...`) | `Container`=tab, `Pane`=pane                |
| tmux                   | `<session-id>:<window-id>:<pane-id>`   | `Session`=session id, `Container`=window, `Pane` |
| tmux-session           | `<session-name>:<window-id>:<pane-id>` | `Session`=session name, `Container`=window, `Pane` |
| terminal definition    | the definition's `address` fields joined by `:` | the named fields |
| foreground / console   | none; WINDOW is the bare name  | not parseable                                     |

`terminal.FormatAddress` renders the complete value; `Backend.OpaqueAddress` renders the part without the launcher prefix (used for start and takeover reports). `ParseFocusAddress` additionally accepts herdr ids without a workspace prefix for the read-only focus path; a definition backend accepts each address field as a plain segment there.

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

Polling and multi-step operations: `CreateContainer` on tmux-session retries once as a new window when a concurrent start created the session; `PaneFacts` on tmux reads `@kander_session` and falls back to `@onevoke_session`; `ReportSession` is one attempt, and the caller retries within its budget; tmux has no native `WaitOutput`, so callers poll `ReadOutput`; `Focus` on tmux is three commands, and on herdr a tab focus plus a best-effort socket pane focus. For both terminals, command compositions are steps, candidates and conditions of their definitions; only herdr socket protocols remain Go hooks.

## Error Classification

- A gone pane is a fact (`PaneFacts.Gone` with the tool's diagnostic), not an error. Missing tmux user options are treated as an empty marker.
- A failed command returns `*terminal.CommandError`. `Kind` is `KindExec` (the command could not run, `Cause` holds the error), `KindExit` (non-zero exit, raw `Code`/`Stderr`; for a definition step also an expired poll, whose `Code` is 0), or one of the response kinds `KindNotJSON`, `KindNotObject`, `KindMissingResult`, `KindInvalidResponse`.
- `Message` is the diagnostic of the operation's primary caller, supplied by the definition's step messages. Other callers can render their own diagnostic from `Kind`, `Cause` and `Detail()`. Preserving an error kind alone does not preserve its user-visible message; the herdr tests assert the JSON, command and tab-cleanup messages separately. Remaining migration differences are recorded in [Terminal definitions](terminal-definitions.md#herdr-compatibility-limits).
- Deadline and cancellation errors stay detectable with `errors.Is`, so `probe.FailureDetail` still localizes them: Go backends return them unchanged, and a definition step wraps them in a `CommandError` of kind `KindExec` whose `Cause` is the context error.
- `terminal.ErrUnsupported` marks an operation the backend does not provide.
