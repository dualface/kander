# Terminal Definitions

A terminal definition is a JSON file that turns a terminal multiplexer into a Kander launcher without Go code. The built-in `herdr`, `tmux` and `tmux-session` launchers are embedded definitions ([`herdr.json`](../internal/terminal/builtin/definitions/herdr.json), [`tmux.json`](../internal/terminal/builtin/definitions/tmux.json)) and run through the same `terminal.DeclarativeBackend` as user definitions. The operations a definition maps are the `terminal.Backend` operations described in [Terminal backends](terminal-backend.md).

## Fixed Boundaries

These points are the contract and are not extended:

- A step is one argv array. There is no shell interpolation and no scripting: no loops, expressions, variables beyond the listed placeholders, or embedded code.
- Output is parsed only by the three primitives of the shared output parser. Placeholder syntax, `{{` / `}}` escapes, and control-character rules are those of [Output parsing](output-parsing.md); this document does not restate them. Terminal definitions use only the whole-document form (`format` omitted or `json`) with `source` `stdout` or `stderr`.
- When an operation cannot be expressed with steps, conditions and the three primitives, the `Backend` operation set is split differently, or Go code is bound through a registered hook. The format does not gain a scripting capability.

## Files, Search Path and Precedence

| Source   | Location                                                     |
| -------- | ------------------------------------------------------------ |
| embedded | `internal/terminal/builtin/definitions/<name>.json` in the binary |
| global   | `<global share dir>/terminals/<name>.json` (`~/.local/share/kander/terminals`) |
| project  | `<main worktree>/.kander/share/terminals/<name>.json` of the Git repository of the current directory |

- Precedence is embedded < global < project. A file replaces the same-name definition of a lower source as a whole; fields are never merged, so a user copy cannot run half old and half new.
- The file name without `.json` must equal `name`. Files are read without following symbolic links or reparse points; a link, a non-regular file, invalid JSON or a failed validation is reported and ignored, and the lower-precedence definition (or the built-in launcher) stays in force.
- A launcher name must be unique: it may not be `auto`, a Go launcher (`foreground`, `console`), or a launcher of another definition.
- User definitions are loaded on first use and their launcher names join configuration validation, so `launcher` in `config.json` and `kander start --launcher` accept them. `kander doctor` lists every user definition file with its launchers, a replaced file, or the validation error with the file path.
- A definition runs its programs as the current user, at the same trust level as `agents.<name>.path` and the project `.kander-config.json`. It holds no credentials.

## Versioning

`schema_version` is `1`. It changes only on a breaking change; adding an optional field does not bump it. A file with an unknown version is rejected.

## Top-Level Fields

| Field          | Meaning |
| -------------- | ------- |
| `schema_version` | `1` |
| `name`         | Definition name, `^[a-z][a-z0-9-]{0,31}$`, equal to the file name |
| `binary`       | Command on `PATH` or an absolute path; `Backend.Executable` |
| `version_args` | argv printing the version (doctor, options panel); literal elements only, a placeholder is rejected |
| `capabilities` | `container` (must be `true`), `focus`, `pane_metadata`, `foreground_process`, `agent_identity`, `wait_output`, `session_report`; `POSIXOnly` follows each launcher's `requires.platform` |
| `address`      | Ordered `WINDOW` fields after `<launcher>:`, each `{"name": "session" | "container" | "pane", "pattern": <regex without capturing groups>}`; `container` and `pane` are required; the default pattern is `[^:\s]+` |
| `launchers`    | Launcher name to launcher object (below); one definition may provide several launchers |
| `errors`       | Error classification rules (below) |
| `hooks`        | Mount point to registered hook name (below) |
| `ops`          | Operation name to operation object (below) |

A capability flag requires its operation (`focus`, `set_session_marker` for `pane_metadata`, `wait_output`), and an operation whose capability is false is rejected. `session_report` requires the `report_session` hook. Callers pick the process-pane policies (liveness, notify, takeover) from `foreground_process` and `pane_metadata`, and the agent-pane policies from `agent_identity` (pane facts then carry `agent`, `agent_status`, `agent_session` and `container`, and `topology` lists pane IDs through `rows`).

`ParseAddress` applies the `address` patterns strictly. The read-only focus path (`ParseFocusAddress`) also accepts the same number of fields when each is a plain colon-free segment, which keeps legacy IDs without a prefix focusable.

## Launchers

```text
"launchers": {
  "<launcher>": {
    "auto_priority": <int, optional>,
    "requires": {
      "platform": "posix" | "windows" | "any",
      "binary": <bool>,
      "inside_session": <bool>,
      "env": [{"name": "<VAR>", "value": "<optional exact value>", "trim": <bool>,
               "before_binary": <bool>, "skip_auto": <bool>, "message": <message>}],
      "messages": {"platform": <message>, "binary": <message>, "env": <message>}
    },
    "started_lines": [{"when": <conditions>, "message": <message>}],
    "ops": {"<operation>": <operation object>}
  }
}
```

- `requires` is checked by `Prepare` before any container exists, in the order platform, the variables marked `before_binary`, binary on `PATH`, the remaining variables (each group in declaration order); a failure names what is missing. A variable with `trim` is compared without surrounding whitespace, and `{env.<VAR>}` expands to that trimmed value. A variable's own `message` replaces `messages.env` for it. The messages replace the generic diagnostics and may use `launcher`, `binary`, `platform` and `name` (the missing variable). `inside_session` is not a `Prepare` check.
- `auto` selects only launchers with `auto_priority > 0` and `inside_session: true` whose environment requirements hold, ignoring variables marked `skip_auto` (which `Prepare` still requires and reports), highest priority first, after the Go backends. The host policy stays in `internal/terminal`: `auto` resolves only among container launchers and never falls back to `foreground` or `console`, whose own preconditions (three TTY streams, native Windows) are Go code.
- `started_lines` render the launch report; the placeholders are `head`, `session`, `session_exists`, `workspace`, `project`, `container`, `pane` and `env.<VAR>`.
- `ops` replaces whole operations for this launcher; `tmux` and `tmux-session` share one definition this way.

## Operations

| Operation            | Inputs (placeholders)                                   | Result fields |
| -------------------- | ------------------------------------------------------- | ------------- |
| `prepare` (optional) | `project`, `project_key`, `command`                     | `session`, `session_exists` (`true`/`false`), `workspace` |
| `create_container`   | `session`, `session_exists`, `workspace`, `project`, `project_key`, `cwd`, `label` | `session`, `container`, `pane` (container and pane required) |
| `wait_ready` (optional; absent means ready) | `pane`                           | none |
| `run_command`        | `pane`, `command`, `posix` (`true`/`false`)             | none |
| `set_session_marker` | `pane`, `value`                                         | none |
| `pane_facts`         | `pane`                                                  | `command`, `in_mode`, `dead`, `session_marker`, `agent`, `agent_status`, `agent_session`, `container` |
| `read_output`        | `pane`                                                  | `text` |
| `wait_output`        | `pane`, `marker` (literal or `regex:` prefixed), `marker_literal` (the marker without a `regex:` prefix, else empty), `marker_regex` (the expression after `regex:`, else empty), `timeout_ms` | none |
| `deliver_text`       | `pane`, `text`                                          | none |
| `topology`           | `session`, `container`, `pane`                          | `session`, `container`, `pane_count`; optional `rows` whose matching rows' `pane` form the pane ID list |
| `container_exists`   | `session`, `container`, `pane`                          | exists when the steps succeed |
| `reverse_lookup`     | `agent`, `reference`, `process_name`                    | from `rows.result`: `session`, `container`, `pane` |
| `focus`              | `session`, `container`, `pane`                          | focus notice |
| `close_container`    | `session`, `container`, `pane`                          | none |

Every template also accepts `env.<VAR>` and the stored step results `step.<store>.<field>`. `project_key` is the directory label plus an eight-digit digest of the project path. `report_session` has no steps; it is the `report_session` hook.

An operation object is:

```text
{
  "steps": [<step>, ...],
  "candidates": <candidates, prepare and create_container only>,
  "rows": <rows, reverse_lookup (required) and topology (optional)>,
  "result": {"<result field>": "<template>"},
  "unavailable": [{"when": <conditions>, "message": <message>}],   // focus only
  "closed_when": <conditions over facts.*>                         // focus only
}
```

`steps` must not be empty unless `candidates` supplies them.

## Steps

```text
{
  "store": "<name>",
  "when": <conditions>,
  "argv": ["<template>", ...],          // or
  "fail": <message>,
  "output": {"source": "stdout" | "stderr", "parse": "<primitive>"},
  "fields": {"<field>": "<primitive>"},
  "expect": <conditions>,
  "gone_when": <conditions, pane_facts and container_exists only>,
  "poll": {"interval": "<duration>", "timeout": "<duration>" | "{timeout_ms}", "until": "matched" | "nonempty" | "json_field:<path>=<value>"},
  "on_error": "fail" | "continue" | "meta_missing",
  "timeout": "<duration>",
  "stop_on_success": <bool>,
  "messages": {"exec": <message>, "exit": <message>, "invalid": <message>,
               "not_json": <message>, "not_object": <message>, "missing_result": <message>,
               "gone": <message, with gone_when>}
}
```

- Steps run in order. A step whose `when` does not hold is skipped. A `fail` step ends the operation with its message. A runtime value rejected during argv expansion is a failure of that step, so `on_error` applies to it.
- `argv` elements are expanded one by one. An element whose placeholder value is empty is dropped together with an immediately preceding standalone flag, as in agent definitions. A runtime value containing CR, LF or NUL fails the step; TAB and braces are allowed in runtime values.
- `output` extracts the step text (the raw stdout without it). `fields` apply one primitive each to that text; a primitive that does not match leaves the field absent. An `output` that does not parse fails the step as an invalid response (`KindInvalidResponse`). A `json_field` output is classified like a JSON API response: output that is not JSON is `KindNotJSON`, a root that is not an object `KindNotObject`, and a missing path `KindMissingResult`, each with its own optional message falling back to `invalid`; a path holding an object or array yields that sub-document as compact JSON with sorted keys, so `fields` can read inside it.
- `store` records the step under `step.<store>.*`: the declared fields plus `ok` (`true`/`false`), `detail` and `text`. A later step with the same store name replaces the record, which is how a legacy fallback read supersedes a missing primary field.
- `expect` is checked after a successful command; when it does not hold the operation fails with the `invalid` message, whatever `on_error` says.
- `on_error: continue` records a failed command (or an output that does not parse) as `ok=false` with its `detail` and goes on; `meta_missing` does so only when `errors.meta_missing` classifies a non-zero exit. `fail` (default) ends the operation.
- `stop_on_success` skips the rest of the current step list after this step succeeds.
- `poll` reruns a succeeding command every `interval` until `until` holds on its text or `timeout` elapses (a failure). `matched` compares the `wait_output` marker; a failing command ends polling at once. `timeout` bounds the whole step.
- From the second command of an operation on, a cancelled or expired context ends the operation before the next command.

### Conditions

A condition set is one string or an array of string arrays: it holds when every condition of any inner array holds. Conditions are `prev_ok`, `prev_failed` (the most recent executed command), `field:<name>=<template>` (the value of a placeholder name equals the expanded template; a missing value is empty), `field_missing:<name>` (empty or absent), each optionally negated with a leading `!`. `when` may reference only stores of earlier steps.

### Messages

A message is a text template, `{"id": "<catalog id>", "args": [<message>, ...]}` rendered in the interface language, or `{"or": [<message>, ...]}` taking the first non-empty alternative. Unknown catalog IDs are rejected. Step messages may use `detail`, `error` and `output`. `detail` is the run error (including a rejected runtime value), the trimmed stderr or `exit N` for a non-zero exit, the parse error for an output that does not parse, `output does not match the definition` for a failed `expect`, and `timed out waiting until <until>` for an expired poll. `error` is the run error or the trimmed stderr, and `output` the trimmed stdout. Without a message, a run failure keeps the raw error text, and an exit or invalid-output failure its detail.

## Candidates

```text
"candidates": {
  "values": ["<template>", ...],
  "steps": [<step>, ...],
  "select": [{"when": <conditions>, "result": {"<result field>": "<template>"}}],
  "fallback": [<step>, ...]
}
```

Each value becomes `candidate` and runs the steps with fresh candidate stores; the first `select` entry that holds picks it and sets result fields. When no value is picked, the candidate stores are discarded and `fallback` runs (for example a `fail` step, or steps that create a new session). The embedded `tmux-session` definition chooses its per-project session this way: an absent session is created, a session owned by this project (primary marker, then legacy marker) is reused, and a session of another project moves to the next numbered name.

## Rows

`reverse_lookup` and `topology` scan one stored step output row by row:

```text
"rows": {
  "from": "<store>",
  "split": "lines" | "json_array:<dotted.path>",
  "fields": {"<field>": "<primitive>"},
  "expect": <conditions over row.*>,
  "checks": [{"when": <conditions over row.*>, "message": <message>}],
  "match": <conditions>,
  "result": {"session": "...", "container": "...", "pane": "..."},
  "messages": {"missing": <message>, "invalid": <message>, "none": <message>, "ambiguous": <message using count>}
}
```

- `split` defaults to `lines`, where blank lines are skipped. `json_array:<path>` takes the array at that path of the JSON output (a missing or non-array value fails with `missing`); each element becomes one row re-encoded as compact JSON with sorted keys, so `json_field` reads its members and a `regex` can check the encoded form (for example that `"agent":` is followed by a string or `null`).
- Every row must satisfy `expect`, otherwise the operation fails with `invalid`; then each of `checks`, in order, whose own message ends the operation when its condition does not hold.
- `reverse_lookup` maps `session`, `container` and `pane`; exactly one matching row is the result, while zero or several matches return `terminal.MatchError` with the count. `topology` maps only `pane` and collects the matching rows in order into `Topology.Panes`. `process_name` is resolved only after the collection steps succeed, so a collection failure is reported before an agent definition error.

## Error Classification

```text
"errors": {
  "gone": ["exit:<code>" | "stderr:<regex>" | "stdout_json:<path>=<value>" | "stderr_json:<path>=<value>", ...],
  "meta_missing": [...]
}
```

- A successful command whose output satisfies the step's `gone_when` (checked after `store`, before `expect`) is gone as well. Its detail is the step's optional `messages.gone`, or the neutral `the terminal reported the target as gone` without one. tmux 3.6 answers `display-message` for a closed pane or window with exit 0 and empty fields, so the embedded tmux definition declares `gone_when` on an empty answer with the gone message `the terminal answered the target with an empty response`.
- A non-zero exit matching `gone` makes `pane_facts` return the gone fact, whose detail is the step detail (trimmed stderr, or `exit N`), and `container_exists` return false; in other operations it is an ordinary failure. `stderr` rules see the trimmed stderr. JSON rules on the same dotted path share one resolved code: the first stream, in the order the rules name the streams, whose whole output holds a non-empty string at that path; a rule matches when that code equals its value. With `stderr_json` listed before `stdout_json`, a code in stderr decides even when stdout carries another one.
- `meta_missing` matters only for a step with `on_error: meta_missing`, which then continues with the field absent.
- Everything else is a command failure: `terminal.CommandError` with `KindExec` (the command could not run; deadline and cancellation stay detectable with `errors.Is`), `KindExit`, `KindNotJSON`, `KindNotObject`, `KindMissingResult` or `KindInvalidResponse`. Liveness, notify and takeover rely on this split to roll back, degrade or report.

## Host Policies

These stay in Go and are the same for every definition: `pane_facts`, `topology` and `reverse_lookup` run within the default probe budget when the caller has no deadline; launch-time operations run without a deadline; focus probes the pane first (probe failure, gone or `closed_when` end it), runs its steps (the container-level switch) through the same step machinery as other operations, then an optional `focus_pane` hook. A failed focus step, or a `fail` step, ends focus with `focus.switch_failed`; without a declared message its notice is `<subcommand>: <run error, stderr, stdout or exit status>`. A step failure that `on_error` continues past makes the result a switch with the `focus.tab_only` notice.

## Hooks

`hooks` binds a mount point to Go code registered with `terminal.RegisterHook` during package initialization; a definition naming an unregistered hook is rejected. A hook returns a three-state `terminal.HookResult`:

| Mount point      | ok                  | degraded                                   | failed |
| ---------------- | ------------------- | ------------------------------------------ | ------ |
| `report_session` | reported            | `ErrNoReportChannel` with the note (launch warns) | the hook error |
| `focus_pane` (after the `focus` steps) | switched | switched with the `focus.tab_only` notice | `focus.switch_failed` |

The `report_session` hook owns the session socket handshake, then reads the identity back through the selected backend's `pane_facts` operation. An unset `HERDR_SOCKET_PATH` is degraded (launch warns); a socket dial, handshake or read-back failure is failed. Launch's existing retry and best-effort policy remain unchanged.

The `focus_pane` hook runs only after the `focus` argv steps have switched the container. `ok` completes focus, `degraded` preserves successful container focus with a warning, and `failed` reports an error. The herdr implementation degrades on a socket failure, preserving the existing tab-only result.

### Hook Inventory

All shipped hook registrations live in `internal/terminal/builtin/hooks.go`; their implementations live in `internal/terminal/herdr`. The inventory test checks every registered built-in name against this table.

| Hook name | Mount point | Purpose | Built-in definition |
| --------- | ----------- | ------- | ------------------- |
| `herdr-socket-session` | `report_session` | Report and read back the agent session | `herdr.json` |
| `herdr-socket-focus` | `focus_pane` | Focus the pane after tab focus | `herdr.json` |

Go code is still required for socket or other non-command-line protocols, and for platform-specific process-tree handling. Definitions do not gain scripts to implement these operations.

The herdr definition uses `pane get` for identity facts and checks the returned pane ID. `pane read` belongs only to `read_output`, which callers use for output diagnostics and agent readiness. Text and exit commands use `agent prompt`. Literal and regular-expression waits use the native `pane wait-output` command and its recent-output source; new-shell readiness uses the visible source. Tab creation reads the workspace from the launcher's required environment. The Go herdr backend is removed; installation-path hints belong to the menu.

### Herdr Compatibility Limits

The herdr definition uses the original catalog messages for JSON failures, command failures and tab-close cleanup. Topology wraps list-response failures, while reverse lookup preserves their unwrapped detail; runner failures on either list path retain their original detail.

For a malformed response with a string-valued `result`, the definition rejects the pane as `KindInvalidResponse`; the former Go backend used `KindMissingResult`. Both stop takeover's exit wait before container close, with different diagnostics. A valid pane response and the normal missing-result, not-object and not-JSON classifications are unchanged.

The herdr launch requirements preserve the former backend's order and messages: `HERDR_ENV=1` is checked before PATH, then the workspace is trimmed and checked for emptiness. The workspace requirement is excluded from auto detection, so a missing workspace is diagnosed after selecting herdr. The prepare operation guards and returns that normalized workspace without running a command; tab creation also expands the trimmed environment value. Topology checks first that each row is an object, then that its IDs are present, preserving the separate diagnostics for these failures.

Reverse lookup's validity expressions scan the complete row. An unrelated nested `agent` or `value` with a non-string value can therefore reject an otherwise matching pane. The former backend checked only the top-level `agent` and `agent_session.value`; a path-aware type check would be needed to remove this stricter rejection.

## Example

A fragment of a two-field address definition:

```json
{
  "schema_version": 1,
  "name": "mymux",
  "binary": "mymux",
  "capabilities": {"container": true, "focus": false, "pane_metadata": true, "foreground_process": true},
  "address": [{"name": "container"}, {"name": "pane"}],
  "errors": {"gone": ["stderr:^no such pane"], "meta_missing": ["exit:4"]},
  "launchers": {"mymux": {"requires": {"platform": "posix", "binary": true, "inside_session": false}}},
  "ops": {
    "create_container": {
      "steps": [{"store": "new", "argv": ["new", "--cwd", "{cwd}", "--name", "{label}"],
                 "fields": {"container": "regex:^(\\S+)\\t", "pane": "regex:\\t(\\S+)"}}],
      "result": {"container": "{step.new.container}", "pane": "{step.new.pane}"}
    }
  }
}
```

A complete third-party definition with a fake terminal runs the full start, check, notify, takeover, focus and dismiss lifecycle in `cmd/kander/terminal_definition_e2e_test.go`.

## Submitting a new terminal definition

Run `kander terminal list` to inspect the embedded, global and project sources,
active launchers, overridden definitions and validation errors. `doctor` consumes
this same inventory and reports the same rejection diagnostic. List exits nonzero
when any definition is invalid. An invalid override is still visible even when
its lower-precedence definition remains active.

Before submitting a definition, run:

```sh
kander terminal test <name>
```

Attach the **complete output**, terminal version, operating system and definition
file to the contribution. `<name>` can identify a definition or one of its
launchers. Definitions are tried in inventory order (embedded, then global,
then project); within each definition a matching launcher name is tried before
the definition name, and a definition-name match uses its first launcher in
sorted order. The first match wins, so an earlier definition named `<name>` is
selected before a later definition that provides a launcher `<name>`. For tmux, run `terminal test tmux` inside tmux;
`terminal test tmux-session` can create its own session outside tmux. The normal
launcher prerequisites still apply. Any definition load error aborts the check before creating a container. This
includes malformed files whose names cannot be decoded; the check never silently
tests a fallback. Normal runtime fallback remains unchanged.

The checker creates a unique labelled container with a temporary working
directory. It invokes the Backend methods below through the same declarative
engine used by launch, liveness and delivery. Each operation emits `pass`, `fail`
or `skip`; command traces contain actual argv, exit code and JSON-escaped
stdout/stderr summaries (at most 1024 characters per stream). Hook operations use
their declared socket channels; only their CLI read-back has an argv trace.

| Backend methods | Check |
| --- | --- |
| Name, Capabilities, Executable, VersionArgs | Identity, capabilities, binary and version output |
| AutoDetect, Prepare | Environment selection and launch preflight |
| CreateContainer | Unique label, nonempty container and pane |
| OpaqueAddress, ParseAddress, ParseFocusAddress | Address round trip; invalid address rejected |
| StartedLines | Render the launch report |
| WaitReady, RunCommand | Wait for readiness; start a known `sh` foreground reader |
| WaitOutput, ReadOutput | Confirm a unique output marker assembled inside the pane |
| SetSessionMarker | Write a unique marker and compare the PaneFacts read-back |
| ReportSession | Report an isolated test identity and compare its read-back |
| PaneFacts | Live pane; foreground command, dead and copy-mode fields |
| Topology, ContainerExists | Owning container and exactly one pane; existence |
| ReverseLookup | Resolve the unique identity back to the created address |
| Focus | Focus the created pane with an attached client |
| DeliverText | Literal special characters plus Enter, confirmed by reader acknowledgement |
| CloseContainer | Close only the created container |
| PaneFacts, ContainerExists after close | Gone classification and false existence, without an ordinary error |

A reflection test requires every Backend method to have a check. Supplemental
metadata and post-close checks reuse the corresponding method. The inventory
evaluates capability requirements at runtime (`|` means OR, `&` means AND);
reverse lookup requires `PaneMetadata` or both `AgentIdentity` and `SessionReport`.
The optional requirements for WaitOutput, SetSessionMarker, ReportSession,
PaneFacts, ReverseLookup and Focus select success or unsupported assertions.
`Container` instead marks a prerequisite enforced by the Capabilities step:
when absent, that step fails and later steps are not executed. Checks labelled
`Container` therefore ignore the evaluated boolean; an empty expression imposes
no capability requirement. Absent pane metadata, native output waiting or
session reporting must produce `ErrUnsupported`. Without
`foreground_process`, PaneFacts must still succeed for a live pane, with empty
foreground fields; there is no separate foreground-query method to return
`ErrUnsupported`. Without a writable identity, reverse lookup must complete with
a zero-match `MatchError`. Unsupported Focus uses `focus.unsupported`. A normal
command failure is never accepted as an unsupported result.

`--skip-focus` skips supported focus checks. Without an attached tmux client,
the explicit outside-tmux/no-client result also produces a skip, not a failure.
Other focus failures fail the check. `--keep` skips CloseContainer and the
post-close checks, even after an earlier failure; it prints the retained address
and working directory. Remove the directory after manually closing that test
container. The default failure path attempts bounded cleanup and reports cleanup
errors; when cleanup fails it also prints the retained address. A failure always
exits nonzero. Steps following a failure are marked unexecuted.

The checker needs POSIX `sh` and `ps`; native Windows can list and validate
definitions but testing reports that no usable terminal is available. Before
waiting for literal input, the pane script reports its actual executable name
through `ps -p "$$" -o comm=`. PaneFacts and ReverseLookup compare against that
independent process marker, including platforms where `sh` execs another shell
image. The helper then reads text and prints acknowledgements; submitted text
is never executed as a shell command. It does not launch a real coding agent. Session reporting uses a
unique test reference on the newly created pane only; a terminal that requires
actual agent recognition may reject that check and must report the failure.

`go test ./internal/terminalcheck -run TestEmbedded -v` tests the final embedded
definitions. When tmux exists, it uses an independent socket, an isolated HOME,
and a seed session, then attaches a PTY client on Linux when available. It checks
both full focus and no-client skip without touching existing sessions. When
`bash` is installed, it also repeats the lifecycle with a `sh` trampoline that
execs `bash`, exercising a different foreground executable name. Herdr is
opt-in: only `KANDER_E2E_HERDR=1` with an available `HERDR_SOCKET_PATH` runs the
real lifecycle. Otherwise its test reports the precise skip reason. Preserve
that skip in contribution evidence rather than claiming a real herdr pass.
