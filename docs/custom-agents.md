# Custom Execution Agents

The optional `agents` object in `config.json` is keyed by agent name. The built-in names are `codex`, `claude`, `grok`, and `cursor`. Those four are themselves definition files embedded at `internal/config/agents/` (`index.json` plus one JSON file per name), `schema_version` 1. A user `agents.<name>` overlay replaces individual fields of that embedded definition (path, process name, dialect, argv templates, session, `prompt_delivery`, review templates); fields that are omitted keep the embedded values. A new name starts with a lowercase letter, is at most 64 characters, and contains only lowercase letters, digits, `_`, and `-`. When this section is not configured, the existing parameters and configuration output remain unchanged.

Shared stdout/file/pane output parsing used by templates, review, and later terminal definitions is specified only in `docs/output-parsing.md` (delivered by task `20260909-process-output-parser-task`). This page does not repeat that field set.

A project may also commit `.kander-config.json` at the Git main worktree root (or, outside Git, the first file of that name found walking up from the current directory). Its keys overlay the scope `config.json` at runtime, including `agents` executable paths and argv templates. That is accepted at the same trust level as checking out and running the repository. The Options Global tab, `Save`/`Update`, doctor repair, and the installer still write only the scope config file. The Options Project tab writes only this overlay file; it never writes `.kander/config.json`. Unedited keys stay inherited.

## Executable Name and Process Name

```json
{
  "agents": {
    "claude": {"process_name": "node"},
    "codex": {"path": "kander-codex"}
  }
}
```

`path` is a PATH name or an absolute path that exists and is executable; relative paths containing separators are not accepted. When specified explicitly it must be resolvable; when omitted it falls back in order to the embedded definition's `path`, then the agent name. `process_name` is used for tmux foreground-process matching and defaults to the basename of the final path. When given explicitly, both must be non-empty and contain no control characters; to restore the default, delete the field. An npm trampoline can use `process_name: "node"`, and a renamed wrapper can set only `path`. Embedded definitions are not probed at config-load time; a missing CLI is reported by `start` and `doctor` only.

Start, resume, doctor, options-panel probing, tmux liveness checks, notification reverse lookup, and takeover cleanup all read this set of settings. Both saving and reading validate explicit paths; after deleting a wrapper, the configuration must be fixed or the executable restored. When doctor encounters an agent definition it cannot validate, it preserves the original file and reports, to avoid losing templates.

## Dialect or argv Templates

```json
{
  "agents": {
    "my-claude": {"path": "my-claude", "dialect": "claude"},
    "helper": {
      "path": "helper",
      "process_name": "helper",
      "args": {
        "start": ["--model", "{model}", "--effort", "{effort}", "--session-id", "{session}"],
        "resume": ["--resume", "{session}", "--model", "{model}"]
      },
      "session": {"mode": "generated"}
    }
  },
  "kanban_agents": {"large": "my-claude", "small": "helper"}
}
```

A custom agent must have `dialect` or `args`. Built-in agents automatically inherit their own dialect. `dialect` accepts the four built-in names and reuses the same model, effort, permission-bypass, and session parameters. Models still live under `models.kanban.<agent>`, using `large_model`, `small_model`, and the corresponding effort fields; the legacy shared `model` fallback remains compatible. A custom Cursor dialect follows Cursor's effort-less model fields.

When both are declared, `args` takes precedence and completely replaces the dialect parameters. `args.start` is required; a template that supports resume must also have `args.resume`; empty arrays are allowed. Each element independently substitutes `{model}`, `{effort}`, `{session}`, and `{session=}`, and substituted values are not parsed recursively. When `{model}`, `{effort}`, or `{session}` is empty, that element is dropped; if its immediately preceding original element is a standalone flag (starting with `-`, containing no placeholder or `=`), that flag is dropped as well. `{session=}` always substitutes, including an empty value, so a session flag can remain present. Other positional arguments are kept. Unknown placeholders, control characters, and empty elements are rejected. These primitives are the only substitution mechanism; there is no shell interpolation and no script hook.

`prompt_delivery` chooses how the task-file instruction reaches the CLI:

- `mode: argv` (the default, including the four built-in agents): Kander appends the prompt last. `ready` and `blocked` are rejected.
- `mode: pane`: the bare positional argument is not a prompt. After the existing container-shell ready wait and `pane run`, Kander waits again for the agent TUI using `ready.match` (`<literal>` or `regex:<pattern>`) and `ready.timeout_ms`. `blocked` entries are checked on every poll and win immediately if they match, even when the ready mark is also visible. After ready, the prompt is delivered with the notify primitives (herdr `agent prompt`, tmux `send-keys -l` plus a separate Enter), before the pane session marker is written. `start` and a `resume` takeover that creates a new container use this two-stage wait; `notify` to an already-running container does not wait for `ready`.

A pane-mode failure (`blocked` match, the delivery primitive rejecting the prompt, or a pure ready timeout) captures the pane output, closes this invocation's tab/window, and rolls the card back. It does not keep the container or write `WINDOW`/`SESSION`. The first two errors tell the operator to run that CLI once in a terminal to answer the dialog and then retry `kander start`; a pure timeout reports the captured pane output without that instruction. Pane mode is rejected before claiming when the resolved launcher is `foreground` or `console`.

Neither the allocate command nor the argument templates use shell interpolation; terminal launches still go through the existing platform argument encoding. The configuration can execute programs declared by the local user, which does not constitute a new inter-user privilege boundary.

## Session Policies

- `generated`: generates a UUID and saves it to the card's SESSION, for use by `{session}` and resume.
- `allocated`: first executes the `session.allocate` argv (the first element is the program), waiting at most 10 seconds; successful output must be a single ID, or a top-level JSON string field designated via `session.json_field`. An ID accepts only 1–128 letters, digits, `.`, `_`, `:`, and `-`. Program failure, invalid output, and timeout are all reported before the task is claimed.
- `hook:<name>`: names a registered Go hook when argv templates cannot express how the CLI creates or discovers a session. The built-in names are listed in the [Hook Catalog](#hook-catalog). An unregistered name is rejected at load time with the agent name and hook name. `discovered` is not accepted in hand-written configuration; Codex uses `hook:codex-rollout` instead.
- `none`: `resume` refuses explicitly; `notify` does not deliver directly and instead runs the start template through the recovery channel, re-reading the card context. The card keeps a UUID used only for terminal marking; `{session}` in the template is empty, the dialect parameters likewise omit session creation/resume options, and the UUID serves only terminal identity checks. `dismiss` still allows closing a terminal whose identity has been confirmed. `kander config`, the stderr of `config --json`, and `kander check` display a degradation notice. Persistent dispatch-back must still satisfy the existing stop facts and receipt gates, and does not use `none` to bypass the duplicate-execution guard.

A templated custom agent without a dialect must declare session explicitly. With a dialect, the default is inherited from that dialect's embedded `session` field: Claude/Grok generate a UUID; Cursor uses `hook:cursor-create-chat`; Codex uses `hook:codex-rollout`. Without explicit argv templates, a dialect using a session hook accepts that hook or `none`; a dialect whose hook allocates an ID before start also accepts an explicit `allocated` session. Thus Cursor still accepts `allocated`, while Codex rejects `generated` and `allocated`, and Cursor rejects `generated`. Dialects whose default is `generated` retain their existing session overrides. All dialects allow `none`, passing no session parameters at start.

Allocation example:

```json
"session": {
  "mode": "allocated",
  "allocate": ["helper", "create-session", "--json"],
  "json_field": "id"
}
```

A brand-new templated agent is recommended to launch via tmux / tmux-session, probed with the configured foreground name and session markers. herdr still relies on its own recognition of agent types; configuring path does not install a recognizer for herdr. foreground/console has no terminal address for `check` to probe and keeps the unknown classification.

## Panel and Review Boundaries

"Task Execution and Models" can select a custom agent. Built-in agents that have not yet been probed successfully can also be selected in order to fill in the path of a renamed program; explicit paths are still validated on save. After each selected agent's model fields there are "Executable name" and "pane process name" inputs; when large and small tasks share the same agent, they are shown only once. Leaving them empty deletes the override; they are saved to `agents`. Dialects, templates, session policies, and review templates are edited only in JSON.

A reviewer is any configured agent that declares `args.review` together with `review.*`. The four built-in names keep that pair in their embedded definitions. A custom name becomes a reviewer only by declaring that pair; inheriting `args.review` / `review.*` from a dialect does not make a path+dialect execution wrapper a reviewer. `args.review` and `review` must be declared together. Dialect defaults supply the built-in pair only when neither is declared; after the pair is declared, omitted review fields are not filled from the dialect. An agent that only has start/resume templates, or only `path` plus `dialect`, cannot be stored in `reviewers.<role>`; `kander config --json` reports the missing review template and `kander doctor` leaves that value unchanged.

Review invocation is declared, not hard-coded:

- `args.review` is an argv template. Placeholders are `{model}`, `{effort}`, `{root}` (the reviewed worktree), `{runtime}` (the private review directory), `{home}` (the review-private home), `{output}` (the result file), `{prompt_file}` (Kander's `prompt.txt`), `{prompt_file:<name>}` (an extra file from `review.prompt_files`), and `{instruction}` (the same one-line instruction historically written to stdin). Substitution is per element; an empty value drops that element and its immediately preceding standalone flag. Unknown placeholders, control characters, and empty elements are rejected. There is no shell interpolation.
- `review.env` maps environment names to the same style of templates, using `{model}`, `{effort}`, `{root}`, `{runtime}`, `{home}`, and `{output}`. Kander always sets `GIT_OPTIONAL_LOCKS=0` itself; that variable is not part of the definition.
- `review.cwd` is `root` or `runtime`. `review.home_env` names the environment variable that supplies the review home (falling back to `HOME` plus `.<agent>`). `review.home_policy` is `required` or `optional`: missing home is rejected, or allowed to continue. `review.output_name`, `review.inspection`, `review.spawns_helpers`, and `review.snapshot_spec` replace the former per-name settings (result file, prompt inspection text, helper-process accounting, and whether the task spec is snapshotted into the runtime).
- `review.stdin` is `instruction` (default) or `none`. `instruction` writes the one-line path instruction to stdin. `none` does not; the instruction must appear in argv as `{instruction}`. The two error combinations (`none` without `{instruction}`, or `instruction` plus `{instruction}`) are rejected at validation time.
- `review.prompt_files` is an optional list of `{name, path, template}` objects rendered into the runtime before launch (POSIX 0600, Windows protected DACL). `path` must be a canonical relative path; traversal, absolute paths, and reparse points are rejected. The template whitelist is `{inspection}`, `{prompt}` (the on-disk `prompt.txt` body), `{role}`, `{report_language}`, `{root}`, `{runtime}`, and `{output}`. Brace escaping follows `docs/output-parsing.md`.
- `review.output` is the shared output spec. Review only accepts `source` `stdout` or `file`. Do not repeat the field set or success-condition rules here; see `docs/output-parsing.md`. When `success` is omitted, Kander requires exit status 0 and a non-empty extracted report.
- `review.path` is the optional review executable. Built-in names ignore a user `agents.<name>.path` overlay and use `<NAME>_REVIEW_BIN`, then the embedded `path`. Custom names use `review.path` and otherwise fall back to `path`.

Read-only isolation for a custom reviewer is the definition author's responsibility. Kander still creates the result file, validates the parsed report, and keeps the review-private directories isolated.

`review_stages` is stored per task scale, matching `kanban_agents`:

```json
"review_stages": {
  "large": {"PM": "required", "QA": "auto", "CSA": "skip", "Hacker": "skip"},
  "small": {"PM": "auto", "QA": "auto", "CSA": "skip", "Hacker": "skip"}
}
```

A legacy flat `{role: mode}` object still loads and applies to both scales; saving rewrites it as the two-scale form. Missing scales or roles default to `auto`. The options panel's "Review and models" section edits large and small independently under each role. Agents resolve the third review-stage precedence tier from the card `SIZE`, and a mixed-size task-group batch uses the `large` scale.

`exit_command` is a single-line string without control characters and may be empty. Built-in Codex/Claude use `/exit`; Grok/Cursor use `/quit`. A custom name with a dialect inherits that field. A purely templated program that declares `exit_command` can be dismissed and cleaned up the same way, still requiring the identity and single-pane container checks to pass. Without `exit_command`, `dismiss` refuses explicitly, names the missing field, and keeps the container.

## Hook Catalog

These hooks live in one Go registry (`internal/config/session_hooks.go`). A definition may only reference a registered name.

| Name | Purpose | Built-in |
| ---- | ------- | -------- |
| `codex-rollout` | Scan CODEX_HOME rollouts; discover or wait for the id | `codex` |
| `cursor-create-chat` | Run `create-chat`; last nonempty stdout line is the id | `cursor` |

The following changes still require Go code:

- A new session identity source that cannot be expressed as `generated`, `allocated`, or `none` (add a named hook and list it here).
- A new interactive exit sequence that is not a single `exit_command` string.
- Merging agent hooks with terminal-side hooks (herdr socket and similar) into one registry.
