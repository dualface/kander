# Custom Execution Agents

The optional `agents` object in `config.json` is keyed by agent name. The built-in names are `codex`, `claude`, `grok`, `cursor`, `pi`, `devin`, `opencode`, `kimi`, `droid`, and `agy`. Those built-in names are themselves definition files embedded at `internal/config/agents/` (`index.json` plus one JSON file per name), `schema_version` 1. A user `agents.<name>` overlay replaces individual fields of that embedded definition (path, process name, dialect, argv templates, session, `prompt_delivery`, review templates); fields that are omitted keep the embedded values. A new name starts with a lowercase letter, is at most 64 characters, and contains only lowercase letters, digits, `_`, and `-`. When this section is not configured, the existing parameters and configuration output remain unchanged.

Shared stdout/file/pane output parsing used by templates, review, and later terminal definitions is specified only in `docs/output-parsing.md` (delivered by task `20260909-process-output-parser-task`). This page does not repeat that field set.

A project may also commit `.kander-config.json` at the Git main worktree root (or, outside Git, the first file of that name found walking up from the current directory). Its keys overlay the scope `config.json` at runtime, including `agents` executable paths and argv templates. That is accepted at the same trust level as checking out and running the repository. The Options Global tab, `Save`/`Update`, doctor repair, and the installer still write only the scope config file. The Options Project tab writes only this overlay file; it never writes `.kander/config.json`. Unedited keys stay inherited.

The background worker (`kander worker`) runs in the client home, outside every project, and serves the executions of every project of that home. It reads the agent definitions of an execution (process name, session settings, program status, session file, and exit command) with the overlay of the directory that execution records: the main worktree of the checkout it was started from (the start directory itself outside Git), or, when an older record has none, its execution worktree. When the recorded directory is empty, or no longer exists, the worker uses the scope `config.json` and logs a warning for a missing directory; it never searches for an overlay above a missing directory. An overlay that is not valid fails those reads, as it does for a command run in the project.

## Executable Name and Process Name

```json
{
  "agents": {
    "claude": {"process_name": "node"},
    "codex": {"path": "kander-codex"}
  }
}
```

`path` is a command name looked up on PATH or an absolute path; relative paths containing separators are not accepted. When omitted it falls back in order to the embedded definition's `path`, then the agent name. `process_name` is used for tmux foreground-process matching and defaults to the basename of the final path. When given explicitly, both must be non-empty and contain no control characters; to restore the default, delete the field. An npm trampoline can use `process_name: "node"`, and a renamed wrapper can set only `path`. No program is probed at config-load time, neither `path`, `review.path`, nor `session.allocate[0]`, for embedded and user definitions alike: an agent whose program does not resolve leaves the configuration valid and every command that does not use it working. A missing program is reported where it is used: `start` and `resume` report that `path` is not in PATH, session allocation reports the same for `session.allocate[0]`, and `kander review` reports that the reviewer CLI is unavailable when the review program (chosen as described under `review.path` in [Panel and Review Boundaries](#panel-and-review-boundaries)) does not resolve. `doctor` shows the agent as not installed when `path` does not resolve; when only the review program is missing it lists the agent without the review capability and reports each role configured with that reviewer as unavailable. `doctor` does not probe `session.allocate[0]`.

A built-in name may also set `config_dir` to relocate the agent's global config directory, which holds its global rules file (and Pi's extension), for example `{"agents": {"codex": {"config_dir": "~/work/codex-home"}}}`. It is not the Kander rules root. The value is an absolute path or starts with `~` or `~/`; a relative value, control characters, the key on a custom name, or the key on `cursor`, `droid`, or `agy` (whose directories are fixed, see below) fail validation, and removing the key makes the configuration valid again. Only the scope `config.json` takes effect; see [Installation and permission boundaries](install-and-permissions.md) for the precedence over the agent's own environment variables.

Start, resume, doctor, options-panel probing, tmux liveness checks, notification reverse lookup, and takeover cleanup all read this set of settings. Both saving and reading check only the form of explicit paths; after deleting a wrapper, the configuration stays valid and the agent fails when it is used, until the configuration is fixed or the executable restored. When doctor encounters an agent definition it cannot validate, it preserves the original file and reports, to avoid losing templates.

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

A custom agent must have `dialect` or `args`. Built-in agents automatically inherit their own dialect. `dialect` accepts the built-in names and reuses the same model, effort, permission-bypass, and session parameters. Models still live under `models.kanban.<agent>`, using `large_model`, `small_model`, and the corresponding effort fields; the legacy shared `model` fallback remains compatible. A custom Cursor dialect follows Cursor's effort-less model fields. On a custom name, an entry that breaks this rule, names an unknown dialect, or uses `install_rules` or `config_dir` is an unavailable agent, not a load failure (see [Configuration written by a newer version](config-forward-compat.md)).

When both are declared, `args` takes precedence and completely replaces the dialect parameters. `args.start` is required; a template that supports resume must also have `args.resume`; empty arrays are allowed. Each element independently substitutes `{model}`, `{effort}`, `{session}`, and `{session=}`, and substituted values are not parsed recursively. When `{model}`, `{effort}`, or `{session}` is empty, that element is dropped; if its immediately preceding original element is a standalone flag (starting with `-`, containing no placeholder or `=`), that flag is dropped as well. `{session=}` always substitutes, including an empty value, so a session flag can remain present. Other positional arguments are kept. Unknown placeholders, control characters, and empty elements are rejected. These primitives are the only substitution mechanism; there is no shell interpolation and no script hook.

`prompt_delivery` chooses how the task-file instruction reaches the CLI:

- `mode: argv` (the default, and the delivery of every built-in agent except Kimi and Droid): Kander appends the prompt last. `ready` and `blocked` are rejected.
- `mode: pane`: the bare positional argument is not a prompt. After the existing container-shell ready wait and `pane run`, Kander waits again for the agent TUI using `ready.match` (`<literal>` or `regex:<pattern>`) and `ready.timeout_ms` (raised to 120000 when `extended_launch_timeouts` is on and the declared value is lower). `blocked` entries are checked on every poll and win immediately if they match, even when the ready mark is also visible. After ready, on a backend that reports pane agent identity (herdr), Kander also waits within the same ready budget until the backend names an agent in the pane, because herdr routes `agent prompt` only to panes it has recognized; a pane still unrecognized at the deadline is delivered to anyway, so the primitive reports its own error. The prompt is then delivered with the notify primitives (herdr or luvus `agent prompt`; tmux `send-keys -l`, a 500ms wait, then a separate Enter), before the pane session marker is written. `start` and a `resume` takeover that creates a new container use this two-stage wait; `notify` to an already-running container does not wait for `ready`.

A pane-mode failure (`blocked` match, the delivery primitive rejecting the prompt, or a pure ready timeout) captures the pane output, closes this invocation's tab/window, and rolls the card back. It does not keep the container or write `WINDOW`/`SESSION`. The first two errors tell the operator to run that CLI once in a terminal to answer the dialog and then retry `kander start`; a pure timeout reports the captured pane output without that instruction. Pane mode is rejected before claiming when the resolved launcher is `foreground` or `console`.

A card pin (`EXEC_EFFORT`, `REVIEW_<ROLE>_EFFORT`) is accepted only when the agent's `args.start` or `args.review` template contains `{effort}`; otherwise the card is rejected instead of silently dropping the value. An execution model pin (`EXEC_MODEL`) is accepted only when `args.start` contains `{model}`, under the same rule. An agent whose `args.start` lacks `{model}` records `model=N/A` in `EXEC_RESOLVED`, and the options panel offers no execution or Chat model field for it; its effort field stays when it takes an effort. Pinned models and efforts must be one ASCII token of letters, digits, and `._:/@+[]-_` that does not start with `-`, so a card value cannot turn into an option once a terminal launcher renders the argv into a shell command line. Pinned agent names follow the same naming rule as configured agents and must name a loaded definition.

Neither the allocate command nor the argument templates use shell interpolation; terminal launches still go through the existing platform argument encoding. The configuration can execute programs declared by the local user, which does not constitute a new inter-user privilege boundary.

## Session Policies

### Interactive session names

An agent can declare `rename_command`, for example `"/rename {name}"`. It
must be a single line containing the literal placeholder `{name}`, with no
control characters, Unicode format characters, or Unicode line or paragraph
separators. Kander rejects invalid values in both built-in and user definitions.
A missing field inherits the built-in command of the effective dialect, if
there is one. A scope setting replaces that default, and a project overlay
replaces the scope setting. An explicit empty string disables renaming without
changing how that agent normally receives its prompt.

Kander substitutes the filtered terminal label for every `{name}`. Unicode
letters (L*), combining marks (M*) and decimal digits (Nd), plus `-`, `_` and
`.`, are retained. Control and format characters (Cc and Cf) are removed.
Everything else becomes `-`. Repeated hyphens collapse and leading and trailing
hyphens are removed. This does not change the terminal label itself.

`rename_delivery` can describe the interactive composer separately from
`prompt_delivery`. It uses the same `ready` and `blocked` structures and
supports literal matches or the `regex:` prefix. Omitting the object inherits
the dialect's object, or reuses the baseline pane delivery rules if the
dialect has none. Its optional `complete` screen match can also contain
`{name}` (escaped as literal text inside a regular expression). Without a
completion match, Kander looks for the ready composer again after a short
pause without a new rename draft. Screen matches see the original backend
output, including terminal line endings. After successful delivery, Kander can
exclude one command echo next to a fresh acknowledgement from the draft
check. An acknowledgement that stays in place cannot hide an adjacent draft.
If a new acknowledgement uses the same row as an older one, the retained
viewport lines must show that the older match moved higher on the screen.
If the number of matches does not increase, a repeated acknowledgement and
its echo must appear below the retained viewport lines after an older match
scrolls out. Both checks apply when one older match moves and another scrolls
out. After a delivery error, no acknowledgement can hide a visible rename
draft.
`after_argv: true` preserves the
initial prompt argument. Use it only for a client verified to handle the
rename command locally without interfering with that first prompt.
Keep the default when the client must be renamed while idle, before it
receives its first prompt. Match the available composer in `ready.match`
for launchers without OSC 7501 status. Supply `blocked` matches for startup
dialogs that may appear.

These are trusted CLI declarations. Verify the command and readiness against
the actual CLI version before enabling renaming for an agent. In particular,
confirm that the CLI treats the command as local, not as a message to the
model. See [launch and delivery](launch-and-delivery.md#interactive-session-naming).

### Session identity

- `generated`: generates a UUID and saves it to the card's SESSION, for use by `{session}` and resume.
- `allocated`: first executes the `session.allocate` argv (the first element is the program), waiting at most 10 seconds; successful output must be a single ID, or a top-level JSON string field designated via `session.json_field`. An ID accepts only 1–128 letters, digits, `.`, `_`, `:`, and `-`. Program failure, invalid output, and timeout are all reported before the task is claimed.
- `hook:<name>`: names a registered Go hook when argv templates cannot express how the CLI creates or discovers a session. The built-in names are listed in the [Hook Catalog](#hook-catalog). An unregistered name (or any other unregistered `session.mode`) does not fail the load: the agent becomes unavailable, the rest of the configuration applies, and using the agent fails with the agent name and the mode; see [Configuration written by a newer version](config-forward-compat.md). Codex uses `hook:codex-rollout`.
- `discovered`: the agent CLI enumerates its own sessions, so Kander can identify the exact session a launch created without a Go hook. Before launch Kander snapshots the enumerated ids; after launch it polls the enumeration until exactly one id is new relative to the snapshot and satisfies every declared `match`. Zero new sessions at the deadline, several new sessions, an enumerate failure, malformed output, an invalid id, or an exceeded bound all fail the launch and roll the card back — there is no "latest session" or `--continue` fallback. `session.discovery` is required for this mode and rejected under any other mode. Fields:
  - `args`: argv appended to the agent program and spawned directly, never through a shell; placeholders are not expanded here and braces are rejected.
  - `format`: `json` (a JSON array of objects), `jsonl` (one object per line), or `lines` (one bare id per line). `lines` takes no `id_field` or `match`.
  - `id_field`: for `json`/`jsonl`, the field whose string value is the session id. An id accepts only 1–128 letters, digits, `.`, `_`, `:`, and `-`.
  - `match`: optional `{field, equals}` pairs; a record must have each field as a string equal to `equals` after placeholder expansion. `{cwd}` expands to the launch project directory and `{task_id}` to the card id; `{cwd}` comparisons are canonicalized on both sides so a symlinked spelling still binds. A missing or non-string field never matches, which prevents cross-project binding.
  - `timeout_ms`: total post-launch discovery budget, 1000–120000, default 10000. With `extended_launch_timeouts` on, a lower value is raised to 120000 for the pre-launch snapshot and the post-launch discovery.
  - `interval_ms`: pause between enumerations, 20–10000 and below the timeout, default 100.
  - `max_bytes`: enumerate output bound, 4096–16777216, default 1048576; overflowing output is rejected.
- `none`: `resume` refuses explicitly; `notify` does not deliver directly and instead runs the start template through the recovery channel, re-reading the card context. The card keeps a UUID used only for terminal marking; `{session}` in the template is empty, the dialect parameters likewise omit session creation/resume options, and the UUID serves only terminal identity checks. `dismiss` still allows closing a terminal whose identity has been confirmed. `kander config`, the stderr of `config --json`, and `kander check` display a degradation notice. Persistent dispatch-back must still satisfy the existing stop facts and receipt gates, and does not use `none` to bypass the duplicate-execution guard.

A templated custom agent without a dialect must declare session explicitly. With a dialect, the default is inherited from that dialect's embedded `session` field: Claude/Grok/Pi generate a UUID; Cursor uses `hook:cursor-create-chat`; Codex uses `hook:codex-rollout`; Devin, OpenCode, and Kimi discover their session via `session.discovery`; Droid uses `hook:droid-sessions`; Antigravity (`agy`) uses `hook:agy-transcript`. Without explicit argv templates, a dialect whose session produces an agent-native id accepts only that same mode or `none`; a dialect whose hook allocates an ID before start also accepts an explicit `allocated` session. Thus Cursor still accepts `allocated`; Codex, Devin, OpenCode, Kimi, Droid, and Antigravity reject `generated` and `allocated`; Cursor rejects `generated`. Dialects whose default is `generated` retain their existing session overrides. All dialects allow `none`, passing no session parameters at start.

Allocation example:

```json
"session": {
  "mode": "allocated",
  "allocate": ["helper", "create-session", "--json"],
  "json_field": "id"
}
```

### Session File (`session.file`)

Optional; `session.file` declares the on-disk session file format so a terminal-reported `kind: "path"` session identity resolves to the real session id inside the file. The declaration itself is orthogonal to `mode`, though the overlay inheritance described below is not: it applies only while the overlay `mode` matches the embedded `mode`. Agents without it keep path identities undecidable — an undecidable candidate is never proof of a match or a mismatch, so liveness reports unknown instead of stopped, and delivery, exit, and close paths refuse rather than act on an unconfirmed pane. Declaring `session.file` also tells Kander the agent's own integration reports the path-kind identity, so herdr launches send no proactive `pane.report_agent_session` id report for that agent; declare it only when such a native report exists. Only the pi built-in declares it today; herdr reports pi sessions as `kind: "path"` whose value is the session JSONL path while the card records the UUID passed by `--session-id`.

```json
"session": {
  "mode": "generated",
  "file": {
    "format": "jsonl_header",
    "id_field": "id",
    "type_field": "type",
    "type_value": "session",
    "max_bytes": 8192
  }
}
```

- `format`: `jsonl_header` is the only supported layout — the session id lives in the first JSON line of the file.
- `id_field`: the top-level header field whose string value is the session id, required and non-empty.
- `type_field` / `type_value`: optional header discriminator, required as a pair; a header whose field does not equal the value is not a session record and stays undecidable.
- `max_bytes`: bound on the header read, default 8192, at most 1048576; a longer header line is undecidable.

The file is read only through the `internal/fs` safe boundary (regular files only, no symlinks or reparse points, non-blocking open), only the bounded first line is parsed as data, and content is never executed. The `session` object keeps its existing overlay semantics with one exception: an overlay `session` replaces the embedded object as a whole, but an omitted `file` is inherited from the embedded definition of the resolved dialect when the overlay `mode` equals the embedded `mode`. The dialect names the CLI being driven and the session file format follows that CLI, so a wrapper agent that declares `dialect: pi` inherits the pi declaration the same way the pi overlay does. An explicit overlay `file` always wins, an overlay whose `mode` differs from the embedded `mode` inherits nothing, and an overlay without `session` keeps inheriting the whole embedded session object. Inheritance is read-time only: the stored overlay is never rewritten. `kander doctor` reports every agent whose overlay relies on this inheritance as an informational hint, and an interactive repair offers to store the embedded declaration into the scope `config.json` — the default keeps inheriting, and a session overlay that lives in the project `.kander-config.json` is only reported, never written into the scope file.

Discovery example (the built-in Devin definition uses exactly this shape; OpenCode uses `args: ["session", "list", "--format", "json"]` and matches `directory` against `{cwd}`; Kimi uses `args: ["session", "list", "--json", "--all"]` and matches `workDir` against `{cwd}`):

```json
"session": {
  "mode": "discovered",
  "discovery": {
    "args": ["list", "--format", "json"],
    "format": "json",
    "id_field": "id",
    "match": [{"field": "working_directory", "equals": "{cwd}"}],
    "timeout_ms": 30000
  }
}
```

The built-in OpenCode definition targets the upstream CLI shape verified on `opencode` 1.18.x: top-level `--model`, `--session`, `--prompt`, and `--auto` on the interactive entry (the TUI has no `--variant`, so effort applies only to reviews), `opencode session list --format json` records carrying an `id` and a `directory` equal to the launch directory, and `opencode run --pure --format json` emitting NDJSON events whose `step_finish` record reports `part.reason`. Its default kanban/review/chat model is `opencode-go/deepseek-v4.1-flash`, and the review pins `OPENCODE_PERMISSION` to a deny-first read-only policy described in [Review Runtime Isolation](review-runtime-isolation.md). Kander does not install OpenCode, configure providers, or enumerate its model catalog.

The built-in Kimi definition targets the upstream CLI shape verified on `kimi` 0.43.x: `--auto` and `--model` on the interactive entry (the CLI has no effort flag), `kimi session list --json --all` records carrying an `id` and a `workDir` equal to the launch directory, `--session <id>` for exact resume, and `kimi --prompt <instruction> --output-format stream-json` emitting NDJSON rows whose terminal `session.resume_hint` record marks a completed run. Kimi is the one built-in agent on `prompt_delivery.mode: pane`: the task prompt is never an argv element; Kander waits for the TUI status line (`context:`), delivers the prompt into the pane, and treats the first-run `Trust this folder?` dialog as a visible blocked state for the user to answer manually. The review renders a `kimi-reviewer.md` agent file into the runtime, passed through `--agent-file`, restricting the reviewer to `Read`/`Grep`/`Glob`. Model fields stay empty: Kimi resolves the model from its own `config.toml`, and Kander does not invent an alias. Kander does not install Kimi, configure providers, or enumerate its model catalog.

The built-in Droid definition targets the Factory CLI shape verified on `droid` 0.235.x: the interactive entry takes `--auto high` and `--resume <id>` but has no model or effort flag, so the kanban and Chat model fields stay empty and Droid resolves both from its own settings; reviews pass `--model` only when `models.review.droid.model` is set and leave reasoning effort to the model's default. Droid has no command that enumerates sessions, so its session uses `hook:droid-sessions`: before launch Kander records the `<id>.jsonl` file names under `~/.factory/sessions/*/`, and after launch it binds the one new file whose first line is a `session_start` record carrying that id and a `cwd` equal to the launch directory, under the same exactly-one rule as `discovered`; `--resume <id>` then resumes exactly. Droid also uses `prompt_delivery.mode: pane`: Kander waits for the TUI help hint (`? for help`) and delivers the prompt into the pane. The first-run `Trust this folder?` dialog is a blocked state for the user to answer by running `droid` once in that directory, because a session whose folder is trusted after launch falls back to `Auto (Off)` and stops on every action. Reviews run `droid exec --output-format json` at its default read-only autonomy, under which any write ends the run with `is_error`; the JSON envelope carries the final message in `result`. Kander does not install Droid, sign in, or enumerate its model catalog.

The built-in Antigravity CLI definition (`agy`) follows the reference design of GitHub PR #29, which verified it on `agy` 1.5.13. The interactive entry is `agy --model <m> --effort <e> --prompt-interactive <instruction>`: the instruction is the last argv element (`prompt_delivery.mode: argv`), and `--conversation <id>` leads the resume form. Model fields stay empty, so agy picks its own default model; the effort defaults are `high` (large), `medium` (small), and `high` for review and Chat. agy has no command that enumerates conversations, so its session uses `hook:agy-transcript`: before launch Kander records the conversation directory names below `~/.gemini/antigravity-cli/brain/` (no variable relocates it), and after launch it binds the one new conversation whose `.system_generated/logs/transcript.jsonl` starts with a `USER_EXPLICIT` / `USER_INPUT` record whose request, between `<USER_REQUEST>` and `</USER_REQUEST>`, equals the launch instruction exactly. The instruction names the temporary task file of this launch, so a conversation of another launch, even in the same workspace, never matches; two matches fail at once, and no match fails after 30 seconds (the wait is raised to 120 seconds by `extended_launch_timeouts`). The transcript is data written by agy: Kander opens it without following links, reads at most 256 KiB of its first line, parses it as JSON, and only compares strings. A `/plan ` prefix on the request is not accepted, because a task launch does not use plan mode; it can be accepted again only with proof that a plain `--prompt-interactive` launch writes it. `resume --adopt` cannot rediscover the session of an unmarked pane for agy when the card has no session reference, as for Droid. Its global rules file is `~/.gemini/AGENTS.md` and its project file is `AGENTS.md`; the reference design is the only source of the global path, and it has not been verified against a real agy. Reviews run `agy --mode plan --output-format json --print <instruction ...>` with `--settings` denying `write_file(*)`, `command(*)`, `mcp(*)`, `read_url(*)`, and `execute_url(*)`; the runtime directory and the installed rules directory are added to the workspace with `--add-dir`, so a reviewer can read the rules directory (the global rules file references it). The review accepts the JSON envelope only when `status` is `SUCCESS` and `response`, the final message, is not empty. An agy that does not know `--settings` fails the review at start. Kander does not install agy, sign in, upgrade it, or enumerate its model catalog.

A brand-new templated agent is recommended to launch via tmux / tmux-session, probed with the configured foreground name and session markers. herdr still relies on its own recognition of agent types; configuring path does not install a recognizer for herdr. foreground/console has no terminal address for `check` to probe and keeps the unknown classification.

## Program Status (OSC 7501)

`program_status` declares whether an agent reports its status to the terminal through OSC 7501 (the Program Status Protocol) and which `status.app` value it reports:

```json
"program_status": {"reports": true, "app": "claude-code"}
```

- `reports` is a boolean. `app` is required, non-empty and single-line when `reports` is `true`, and must be omitted when it is `false`.
- Every built-in agent declares it. claude (`claude-code`), codex (`codex`), pi (`pi`), grok (`grok-build`), kimi (`kimi-code`) and devin (`devin`) report; the app values were observed in herdr-core. cursor, opencode, droid and agy declare `reports: false`: cursor sends no report, and opencode, droid and agy were not observed.
- A custom agent may set its own declaration in `agents.<name>`. Without one it inherits the declaration of its dialect, like `prompt_delivery`; a templated agent without a dialect has none.
- Only a program status launcher (`herdr-core`) reads it. That launcher identifies an agent by the app its terminal reports, so it refuses an agent whose declaration is missing or says `reports: false`, before any side effect of start, resume, takeover, notify recovery, orchestrate, issue triage, refinement, issue result or chat. `kander doctor` lists the configured execution agents (both task scales and the chat agent) that would be refused. Other launchers ignore the field.

## Chat Agent and Model

TUI chat sessions use the independent `chat_agent`, which must name a usable execution agent. Its settings live under `models.chat.<agent>`: `model` is available for every agent, while `effort` is available only when that agent's argv template supports it (Cursor therefore accepts only `model`). For example:

```json
{
  "chat_agent": "helper",
  "models": {
    "chat": {
      "helper": {"model": "helper-chat", "effort": "high"}
    }
  }
}
```

When `chat_agent` is absent, it follows the effective `kanban_agents.large`. A missing Chat model falls back to that agent's `models.kanban.<agent>.large_model`, then its legacy shared `model`; a missing Chat effort falls back to `large_effort`. Project overlays prefer their explicit `models.chat` fields, then scope Chat fields, then the effective large-task fields.

## Panel and Review Boundaries

"Task Execution and Models" can select custom agents independently for large tasks, small tasks, and Chat, and edit their model fields. Switching Chat Agent presents that agent's effective large-task model and effort; Project edits copy Chat fields sparsely, so only explicitly edited values override inheritance. Built-in agents that have not yet been probed successfully remain selectable so they can still be chosen for work. Executable `path`, `process_name`, dialects, argv templates, session policies, and review templates are edited only in JSON (`config.json` or the project overlay). Options saves never rewrite the `agents` section, so a TUI save cannot clobber hand-edited agent definitions.

Changing a reviewer in Options adopts that agent's `models.review` model and effort together. Role settings record their owner in `models.review_roles.<role>.large_agent` or `small_agent`; overrides apply only to that agent. Empty values in a bound scale inherit directly from that agent's defaults, skipping the legacy shared role keys. Legacy entries without an owner retain their shared-key fallback for the configured reviewer; explicitly running another reviewer uses that agent's defaults. Agent bindings prevent cross-agent reuse; they do not validate model availability or supported effort levels.

Project role-model edits also pin the corresponding reviewer. A project reviewer change cannot inherit another agent's Global role settings, including when an individual model or effort field is restored to inheritance. Restoring the reviewer itself removes that scale's project model, effort, and owner together. The other task scale and roles remain independent. The optional owner fields are written by the updated Options panel and require a binary that recognizes them; older configurations remain readable.

Compatible legacy shared Project values continue to fill empty Global scale fields when the Global scale acquires an owner binding. A nonempty scale value still takes precedence over a legacy shared value. Opening the review page displays effective values without overwriting stored overrides owned by a different reviewer; an explicit model edit or reviewer switch establishes the new ownership.

A reviewer is any configured agent that declares `args.review` together with `review.*`. The built-in names keep that pair in their embedded definitions. A custom name becomes a reviewer only by declaring that pair; inheriting `args.review` / `review.*` from a dialect does not make a path+dialect execution wrapper a reviewer. `args.review` and `review` must be declared together. Dialect defaults supply the built-in pair only when neither is declared; after the pair is declared, omitted review fields are not filled from the dialect. An agent that only has start/resume templates, or only `path` plus `dialect`, cannot be stored in `reviewers.<role>`; `kander config --json` reports the missing review template and `kander doctor` leaves that value unchanged.

Review invocation is declared, not hard-coded:

- `args.review` is an argv template. Placeholders are `{model}`, `{effort}`, `{root}` (the reviewed worktree), `{runtime}` (the private review directory), `{home}` (the review-private home), `{output}` (the result file), `{prompt_file}` (Kander's `prompt.txt`), `{prompt_file:<name>}` (an extra file from `review.prompt_files`), `{instruction}` (the same one-line instruction historically written to stdin), and `{rules_dir}` (the installed Kander rules directory, an absolute path). `{rules_dir}` is empty when that directory does not exist, so its element and the flag before it are dropped; it is valid only in `args.review`, never in `review.env` or `review.prompt_files`. Substitution is per element; an empty value drops that element and its immediately preceding standalone flag. Unknown placeholders, control characters, and empty elements are rejected. There is no shell interpolation.
- `review.env` maps environment names to the same style of templates, using `{model}`, `{effort}`, `{root}`, `{runtime}`, `{home}`, and `{output}`. Kander always sets `GIT_OPTIONAL_LOCKS=0` itself; that variable is not part of the definition.
- `review.cwd` is `root` or `runtime`. `review.home_env` names the environment variable that supplies the review home (falling back to `HOME` plus `.<agent>`). `review.home_policy` is `required` or `optional`: missing home is rejected, or allowed to continue. `review.output_name`, `review.inspection`, `review.spawns_helpers`, and `review.snapshot_spec` replace the former per-name settings (result file, prompt inspection text, helper-process accounting, and whether the task spec is snapshotted into the runtime). The generated runtime path `review-contract.md` is reserved: `output_name` cannot equal it, and a `prompt_files[].path` cannot equal or descend from it. These checks are case-insensitive for cross-platform consistency.
- `review.stdin` is `instruction` (default) or `none`. `instruction` writes the one-line path instruction to stdin. `none` does not; the instruction must appear in argv as `{instruction}`. The two error combinations (`none` without `{instruction}`, or `instruction` plus `{instruction}`) are rejected at validation time.
- `review.prompt_files` is an optional list of `{name, path, template}` objects rendered into the runtime before launch (POSIX 0600, Windows protected DACL). `path` must be a canonical relative path; traversal, absolute paths, and reparse points are rejected. The template whitelist is `{inspection}`, `{prompt}` (the on-disk `prompt.txt` bootstrap, which names the separate `review-contract.md`), `{role}`, `{report_language}`, `{root}`, `{runtime}`, and `{output}`. Brace escaping follows `docs/output-parsing.md`.
- `review.output` is the shared output spec. Review only accepts `source` `stdout` or `file`. Do not repeat the field set or success-condition rules here; see `docs/output-parsing.md`. When `success` is omitted, Kander requires exit status 0 and a non-empty extracted report.
- `review.path` is the optional review executable. Built-in names ignore a user `agents.<name>.path` overlay and use `<NAME>_REVIEW_BIN`, then the embedded `path`. Custom names use `review.path` and otherwise fall back to `path`.

Read-only isolation for a custom reviewer is the definition author's responsibility. Its tools must be able to run read-only Git in the review worktree because the evidence lists commit ranges, logs, and changed files without including diffs. Kander still creates the result file, validates the parsed report, and keeps the review-private directories isolated.

`review_stages` is stored per task scale, matching `kanban_agents`:

```json
"review_stages": {
  "large": {"PMQA": "required", "Security": "skip"},
  "small": {"PMQA": "auto", "Security": "skip"}
}
```

A legacy flat `{role: mode}` object still loads and applies to both scales; saving rewrites it as the two-scale form. Missing `PMQA` and `Security` default to `auto`, including when the scale or whole section is absent. Leftover `PM`/`QA`/`CSA`/`Hacker` keys fold on load into the two current roles (strongest stage; first non-default reviewer; first non-empty model field) and are not written back. The options panel's "Review stages" section edits large and small independently under each role; "Review and models" edits the reviewer agent and model/effort only. Agents resolve the third review-stage precedence tier from the card `SIZE`, and a mixed-size task-group batch uses the `large` scale.

`exit_command` is a single-line string without control characters and may be empty. Built-in Codex/Claude/Devin/OpenCode/Kimi/Agy use `/exit`; Grok/Cursor/Pi/Droid use `/quit`. A custom name with a dialect inherits that field. A purely templated program that declares `exit_command` can be dismissed and cleaned up the same way, still requiring the identity and single-pane container checks to pass. Without `exit_command`, `dismiss` refuses explicitly, names the missing field, and keeps the container.

## Embedded-Only Fields

The embedded definitions also carry fields a user `agents.<name>` overlay cannot declare — the loader ignores them as unknown keys (and reports them), so an overlay can neither add, change, nor remove them, and omitting them changes nothing. `rules_target` and `rules_integration` name the agent rules file each install scope integrates and how the reference is written; `config_home` declares the default global config directory (`default`, relative to the home directory, containing every global target) and the environment variables that relocate it (`env`, in precedence order, each with `name`, an optional `join` subdirectory, and `absolute_only`), or `fixed: true` with no `env` when the agent only reads its default directory (cursor, whose global target is the always-applied rule `.cursor/rules/kander.mdc`, because cursor-agent finds global rules only under `<ancestor>/.cursor/rules/` and never reads `~/.cursor/AGENTS.md`; droid, whose global target is `.factory/AGENTS.md`, because Droid reads personal rules only from fixed home directories; and agy, whose global target is `.gemini/AGENTS.md`, because Antigravity CLI reads its global rules file from the fixed `~/.gemini` directory); `rules_extension` names an optional Kander-shipped agent extension:

```json
"rules_extension": {
  "source": "kander-rules.ts",
  "global": ".pi/agent/extensions/kander-rules.ts",
  "project": ".pi/extensions/kander-rules.ts"
}
```

`source` is a file embedded under `internal/config/agents/extensions/`; `global` is the default install target relative to the user's home, `project` the target relative to the Git main worktree. A global target follows the agent's resolved config directory (`config_dir`, then `config_home.env`, then `config_home.default`), so the rules reference and the extension move together; project targets remain unchanged. This resolution does not make the embedded fields user-configurable. Only the embedded `pi.json` declares an extension today. Install and `kander doctor` write the file for the covered scope unless the agent's `install_rules` switch is off, refuse any symlink or reparse point on the target chain, classify an existing file as installed, missing, outdated, or locally modified by its digest, and never overwrite a modified copy. See [Installation and permission boundaries](install-and-permissions.md) for the write and reporting rules.

`review_windows_read_groups` lists local groups that get read-only access to the review runtime on Windows, for reviewers whose sandbox runs commands under separate accounts:

```json
"review_windows_read_groups": ["CodexSandboxUsers"]
```

Each entry is a plain group name without `\`, `/`, or `@`, unique case-insensitively. At review time a name must resolve to a local group of this machine's account domain; builtin and well-known groups fail the review and an unresolved name is skipped. On Windows such a reviewer also gets the task spec snapshotted into the runtime regardless of `review.snapshot_spec`. Only the embedded `codex.json` declares it; POSIX ignores it. See [Review runtime isolation](review-runtime-isolation.md#runtime-directory-contract).

On Windows, Kander appends the review worktree to the reviewer's `GIT_CONFIG_PARAMETERS` as a single `safe.directory` entry. This allows the sandbox account to pass Git's ownership check for that worktree without changing persistent Git configuration.

`review.last_message_report` is a boolean inside the embedded `review` object. When it is `true`, the generated `review-contract.md` of that reviewer adds the output contract that makes the final message the complete report (`rules/templates/review/last-message-output.md`). The built-in `claude`, `cursor`, `grok`, `kimi`, `droid`, and `agy` definitions declare it; the others do not, and an absent field means `false`:

```json
"review": {
  "last_message_report": true
}
```

The value is read from the built-in definition of the agent with exactly that name, through `config.EmbeddedReviewLastMessageReport`. It never comes from a user overlay and is not inherited through `dialect`. A user definition that overrides `agents.claude.review` therefore keeps the output contract, and a custom agent with `dialect: claude` does not get it; such an agent that needs the convention writes it in its own prompt text. The field lives only on the embedded wrapper of the review object, not on `AgentReview`, so a user `agents.<name>.review.last_message_report` is an ignored key: the configuration stays valid, `kander config` and `kander doctor` list the key as ignored, and the contract does not change. Opening the field to user definitions is a separate decision that needs a minimum-version note in `CHANGELOG.md`.

`review.git_inspection` is a boolean read only from the embedded definition of the exact reviewer name. It defaults to `true`, including for custom names, and is not inherited through `dialect`. The built-in `agy`, `grok`, `kimi`, and `opencode` definitions set it to `false`. New runs with those names are rejected before preparation even if a user overlay enables shell access. Setting the field in a user overlay has no effect. Same-ID recovery for existing runs remains available.

### Rules integration providers

`rules_integration` selects a reference style provider, and the target file selects a file format provider. `internal/install` branches on neither a style name nor a file extension; callers ask the provider, and the providers only decide what is written, never whether or where a file may be written (the `install_rules` gate, the shared-file check, the symlink and project-escape checks, and the atomic write stay with the callers). Providers are named after the mechanism, not the agent, so several definitions share one.

| Kind | Provider | Selected by | What it owns |
| ---- | -------- | ----------- | ------------ |
| Reference style | `markdown-reference` | `rules_integration` | the `## Kander Rules Entry` block, detection by entry path or embedded entry content, bare path load commands |
| Reference style | `claude-import` | `rules_integration` | the `@<path>` import line and its detection, `@<path>` load commands, written first in project scope, a project rules file that is a symlink not pointing at the entry is read as a file |
| File format | `markdown` | any target without a registered extension | nothing: no prefix, symlinks allowed |
| File format | `mdc-rule` | the `.mdc` extension | the `alwaysApply: true` frontmatter check and prefix, no symlinks, and the frontmatter alone counts as only Kander content on uninstall |

The built-in definitions produce three combinations: `claude-import` with `markdown` (Claude), `markdown-reference` with `mdc-rule` (Cursor's global `.cursor/rules/kander.mdc`), and `markdown-reference` with `markdown` (every other agent, and Cursor's project `AGENTS.md`).

The reference style names are registered twice: `config.RulesIntegrations` lists the values `internal/config` accepts, and `referenceStyles` in `internal/install` holds the implementations. A consistency test fails when the two differ. To add a way of referencing the entry:

1. Add the name to `config.RulesIntegrations` and use it as `rules_integration` in the embedded definition.
2. Add `rules_style_<mechanism>.go` in `internal/install` with a type that implements `referenceStyle` (`Name`, `Block`, `Present`, `LoadCommand`, `ProjectWriteFirst`, `ProjectSymlinkMayDiffer`) and register it in `referenceStyles`. `Name()` must return exactly the value added to `config.RulesIntegrations` in step 1: `referenceStyleFor` selects the style by comparing it with the definition's `rules_integration`, and the consistency test compares the two lists.
3. Add scenarios for the new style to the golden transcripts under `internal/install/testdata/`; the existing transcripts must stay byte for byte unchanged unless the written output changes on purpose.

To add a file format, implement `fileFormat` (`Name`, `SymlinkRefusal`, `Inspect`, `Prefix`, `OnlyKander`; `Name()` only identifies the format and is not matched against any configuration value) in `rules_format_<name>.go` and register its extension in `fileFormatsByExtension`, the only place a target is matched by extension. See [Installation and permission boundaries](install-and-permissions.md#rules-integration-providers).

## Hook Catalog

These hooks are registered twice, and the two registrations must agree. `internal/config/session_hooks.go` lists each name with its capability flags (`DiscoverAfterStart`, `ResolveEmptyReference`, `AllocateBeforeStart`, `PersistAfterStart`) so configuration loading can validate a definition without importing launch. `internal/launch` holds the implementation as a session provider, one `session_provider_<mechanism>.go` file per mechanism, registered by its `session.mode` value. A definition may only reference a registered name, and a consistency test in `internal/launch` fails when a hook has no provider, a provider names an unregistered hook, or its capabilities differ from the flags. `ResolveEmptyReference` is the exception: only `internal/liveness` consumes it, and no provider implements it.

| Name | Purpose | Built-in |
| ---- | ------- | -------- |
| `codex-rollout` | Scan CODEX_HOME rollouts; discover or wait for the id | `codex` |
| `cursor-create-chat` | Run `create-chat`; last nonempty stdout line is the id | `cursor` |
| `droid-sessions` | Snapshot `~/.factory/sessions/*/<id>.jsonl` names before launch; bind the one new `session_start` header whose `cwd` is the launch directory | `droid` |
| `agy-transcript` | Snapshot the conversation directory names below `~/.gemini/antigravity-cli/brain/` before launch; bind the one new conversation whose transcript's first user request equals the launch instruction | `agy` |

The following changes still require Go code:

- A new session identity source that cannot be expressed as `generated`, `allocated`, `none`, or `discovered`. Add a session provider and a hook:
  1. In `internal/config/session_hooks.go`, add the hook to `registeredSessionHooks` with its capability flags.
  2. In `internal/launch`, add `session_provider_<mechanism>.go` with a type that implements `sessionProvider` (`Mode()` returns `hook:<name>`, `Resumable()` says whether a later launch can resume the session) and registers itself from `init` through `registerSessionProvider`. Name the provider after the mechanism, not the agent, so several definitions can share it.
  3. Implement only the capabilities the mechanism has: `sessionAllocator` (an id before the agent starts; `sessionAllocatorChecker` adds a check, run before the allocation is recorded, that the allocator can start) and `sessionDiscoverer` (`Snapshot` before the launch, `Scan` of the candidates after it, the wait, the poll interval, the two localized error keys, and whether the id is written back to the card on every launcher; a remote start writes a discovered id back on a launcher that marks the pane or reports the session whatever the provider says). `sessionMarkerRepairer` marks a provider whose launch session can be found again from static facts. A provider without a capability gets the behavior of an agent that never had it. This branch has no local resume, adoption or session repair, so the resolver, adopter and prover capabilities of the develop branch do not exist here.
  4. Do not write a polling loop: a discoverer only supplies the scan, and `waitForSessionCandidates` runs the one loop (exactly one candidate returns, several fail at once, none retries until the wait ends). The `sessionLaunch` a provider receives carries the task, the launch directory, the launch time, the agent program, the definition, the launch budget, and the instruction text this launch sends to the agent, for providers that match a session by its first prompt.
  5. List the hook in the table above.
- A new interactive exit sequence that is not a single `exit_command` string.
- Merging agent hooks with terminal-side hooks (herdr socket and similar) into one registry.

## Review Roles

Current runnable roles are PMQA (stage one: contract checks then quality checks) and Security (stage two: trust-boundary analysis then exploit-chain checks). Both default to `auto`. `skip` records N/A. Leftover four-role configuration keys fold on load without migration warnings; saves write only PMQA and Security.

This fragment requires PMQA and skips Security:

```json
"review_stages": {
  "large": {"PMQA": "required", "Security": "skip"},
  "small": {"PMQA": "auto", "Security": "skip"}
}
```

Batch requirements for new work use the exact two-key set shown in [review evidence](review-evidence.md#roles-and-historical-compatibility). Historical four-key and six-key batches remain readable and closable.
