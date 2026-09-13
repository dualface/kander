# Terminal Definitions

A terminal definition is a JSON file that turns a terminal multiplexer into a Kander launcher without Go code. The built-in `tmux` and `tmux-session` launchers are an embedded definition ([`internal/terminal/builtin/definitions/tmux.json`](../internal/terminal/builtin/definitions/tmux.json)) and run through the same `terminal.DeclarativeBackend` as user definitions. The operations a definition maps are the `terminal.Backend` operations described in [Terminal backends](terminal-backend.md).

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
- A launcher name must be unique: it may not be `auto`, a Go launcher (`herdr`, `foreground`, `console`), or a launcher of another definition.
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
| `version_args` | argv printing the version (doctor, options panel); no placeholders |
| `capabilities` | `container` (must be `true`), `focus`, `pane_metadata`, `foreground_process`, `wait_output`, `session_report`; `POSIXOnly` follows each launcher's `requires.platform` |
| `address`      | Ordered `WINDOW` fields after `<launcher>:`, each `{"name": "session" | "container" | "pane", "pattern": <regex without capturing groups>}`; `container` and `pane` are required; the default pattern is `[^:\s]+` |
| `launchers`    | Launcher name to launcher object (below); one definition may provide several launchers |
| `errors`       | Error classification rules (below) |
| `hooks`        | Mount point to registered hook name (below) |
| `ops`          | Operation name to operation object (below) |

A capability flag requires its operation (`focus`, `set_session_marker` for `pane_metadata`, `wait_output`), and an operation whose capability is false is rejected. `session_report` requires the `report_session` hook. Callers pick the process-pane policies (liveness, notify, takeover) from `foreground_process` and `pane_metadata`.

## Launchers

```text
"launchers": {
  "<launcher>": {
    "auto_priority": <int, optional>,
    "requires": {
      "platform": "posix" | "windows" | "any",
      "binary": <bool>,
      "inside_session": <bool>,
      "env": [{"name": "<VAR>", "value": "<optional exact value>"}],
      "messages": {"platform": <message>, "binary": <message>, "env": <message>}
    },
    "started_lines": [{"when": <conditions>, "message": <message>}],
    "ops": {"<operation>": <operation object>}
  }
}
```

- `requires` is checked by `Prepare` before any container exists, in the order platform, binary on `PATH`, environment; a failure names what is missing. The optional messages replace the generic diagnostics and may use `launcher`, `binary`, `platform` and `name` (the missing variable).
- `auto` selects only launchers with `auto_priority > 0` and `inside_session: true` whose environment requirements hold, highest priority first, after the Go backends. The host policy stays in `internal/terminal`: `auto` resolves only among container launchers and never falls back to `foreground` or `console`, whose own preconditions (three TTY streams, native Windows) are Go code.
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
| `pane_facts`         | `pane`                                                  | `command`, `in_mode`, `dead`, `session_marker` |
| `read_output`        | `pane`                                                  | `text` |
| `wait_output`        | `pane`, `marker` (literal or `regex:` prefixed), `timeout_ms` | none |
| `deliver_text`       | `pane`, `text`                                          | none |
| `topology`           | `session`, `container`, `pane`                          | `session`, `container`, `pane_count` |
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
  "rows": <rows, reverse_lookup only>,
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
  "poll": {"interval": "<duration>", "timeout": "<duration>" | "{timeout_ms}", "until": "matched" | "nonempty" | "json_field:<path>=<value>"},
  "on_error": "fail" | "continue" | "meta_missing",
  "timeout": "<duration>",
  "stop_on_success": <bool>,
  "messages": {"exec": <message>, "exit": <message>, "invalid": <message>}
}
```

- Steps run in order. A step whose `when` does not hold is skipped. A `fail` step ends the operation with its message.
- `argv` elements are expanded one by one. An element whose placeholder value is empty is dropped together with an immediately preceding standalone flag, as in agent definitions. A runtime value containing CR, LF or NUL fails the step; TAB and braces are allowed in runtime values.
- `output` extracts the step text (the raw stdout without it). `fields` apply one primitive each to that text; a primitive that does not match leaves the field absent. An `output` that does not parse fails the step as an invalid response.
- `store` records the step under `step.<store>.*`: the declared fields plus `ok` (`true`/`false`), `detail` and `text`. A later step with the same store name replaces the record, which is how a legacy fallback read supersedes a missing primary field.
- `expect` is checked after a successful command; when it does not hold the operation fails with the `invalid` message, whatever `on_error` says.
- `on_error: continue` records a failed command (or an output that does not parse) as `ok=false` with its `detail` and goes on; `meta_missing` does so only when `errors.meta_missing` classifies a non-zero exit. `fail` (default) ends the operation.
- `stop_on_success` skips the rest of the current step list after this step succeeds.
- `poll` reruns a succeeding command every `interval` until `until` holds on its text or `timeout` elapses (a failure). `matched` compares the `wait_output` marker; a failing command ends polling at once. `timeout` bounds the whole step.
- From the second command of an operation on, a cancelled or expired context ends the operation before the next command.

### Conditions

A condition set is one string or an array of string arrays: it holds when every condition of any inner array holds. Conditions are `prev_ok`, `prev_failed` (the most recent executed command), `field:<name>=<template>` (the value of a placeholder name equals the expanded template; a missing value is empty), `field_missing:<name>` (empty or absent), each optionally negated with a leading `!`. `when` may reference only stores of earlier steps.

### Messages

A message is a text template, `{"id": "<catalog id>", "args": [<message>, ...]}` rendered in the interface language, or `{"or": [<message>, ...]}` taking the first non-empty alternative. Unknown catalog IDs are rejected. Step messages may use `detail` (run error, trimmed stderr, or `exit N`), `error` (run error or trimmed stderr) and `output` (trimmed stdout). Without a message, a run failure keeps the raw error text, and an exit or invalid-output failure its detail.

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

`reverse_lookup` collects its candidates from one stored step output, one line per row:

```text
"rows": {
  "from": "<store>",
  "fields": {"<field>": "<primitive>"},
  "expect": <conditions over row.*>,
  "match": <conditions>,
  "result": {"session": "...", "container": "...", "pane": "..."},
  "messages": {"invalid": <message>, "none": <message>, "ambiguous": <message using count>}
}
```

Blank lines are skipped; a row failing `expect` fails the lookup; exactly one matching row is the result, while zero or several matches return `terminal.MatchError` with the count. `process_name` is resolved only after the collection steps succeed, so a collection failure is reported before an agent definition error.

## Error Classification

```text
"errors": {
  "gone": ["exit:<code>" | "stderr:<regex>" | "stdout_json:<path>=<value>" | "stderr_json:<path>=<value>", ...],
  "meta_missing": [...]
}
```

- A non-zero exit matching `gone` makes `pane_facts` return the gone fact (with the trimmed stderr) and `container_exists` return false; in other operations it is an ordinary failure. `stderr` rules see the trimmed stderr; JSON rules read a string field of the whole stream.
- `meta_missing` matters only for a step with `on_error: meta_missing`, which then continues with the field absent.
- Everything else is a command failure: `terminal.CommandError` with `KindExec` (the command could not run; deadline and cancellation stay detectable with `errors.Is`), `KindExit`, or `KindInvalidResponse`. Liveness, notify and takeover rely on this split to roll back, degrade or report.

## Host Policies

These stay in Go and are the same for every definition: `pane_facts`, `topology` and `reverse_lookup` run within the default probe budget when the caller has no deadline; launch-time operations run without a deadline; focus probes the pane first (probe failure, gone or `closed_when` end it), runs its steps with the focus step diagnostics, then an optional `focus` hook.

## Hooks

`hooks` binds a mount point to Go code registered with `terminal.RegisterHook` during package initialization; a definition naming an unregistered hook is rejected. A hook returns a three-state `terminal.HookResult`:

| Mount point      | ok                  | degraded                                   | failed |
| ---------------- | ------------------- | ------------------------------------------ | ------ |
| `report_session` | reported            | `ErrNoReportChannel` with the note (launch warns) | the hook error |
| `focus`          | switched            | switched with the `focus.tab_only` notice  | `focus.switch_failed` |

The hooks of specific terminals (for example the herdr socket) belong to those terminals, not to this format.

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
