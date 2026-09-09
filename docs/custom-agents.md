# Custom Execution Agents

The optional `agents` object in `config.json` is keyed by agent name. The built-in names are `codex`, `claude`, `grok`, and `cursor`. A new name starts with a lowercase letter, is at most 64 characters, and contains only lowercase letters, digits, `_`, and `-`. When this section is not configured, the existing parameters and configuration output remain unchanged.

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

`path` is a PATH name or an absolute path that exists and is executable; relative paths containing separators are not accepted. When specified explicitly it must be resolvable; when omitted it falls back in order to the built-in table, then the agent name. `process_name` is used for tmux foreground-process matching and defaults to the basename of the final path. When given explicitly, both must be non-empty and contain no control characters; to restore the default, delete the field. An npm trampoline can use `process_name: "node"`, and a renamed wrapper can set only `path`.

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

When both are declared, `args` takes precedence and completely replaces the dialect parameters. `args.start` is required; a template that supports resume must also have `args.resume`; empty arrays are allowed. Each element independently substitutes `{model}`, `{effort}`, and `{session}`, and substituted values are not parsed recursively. When any placeholder is empty, that element is dropped; if its immediately preceding original element is a standalone flag (starting with `-`, containing no placeholder or `=`), that flag is dropped as well. Other positional arguments are kept. Unknown placeholders, control characters, and empty elements are rejected.

The prompt does not enter the template; it is always appended by Kander at the end of the argv. Neither the allocate command nor the argument templates use shell interpolation; terminal launches still go through the existing platform argument encoding. The configuration can execute programs declared by the local user, which does not constitute a new inter-user privilege boundary.

## Session Policies

- `generated`: generates a UUID and saves it to the card's SESSION, for use by `{session}` and resume.
- `allocated`: first executes the `session.allocate` argv (the first element is the program), waiting at most 10 seconds; successful output must be a single ID, or a top-level JSON string field designated via `session.json_field`. An ID accepts only 1–128 letters, digits, `.`, `_`, `:`, and `-`. Program failure, invalid output, and timeout are all reported before the task is claimed.
- `none`: `resume` refuses explicitly; `notify` does not deliver directly and instead runs the start template through the recovery channel, re-reading the card context. The card keeps a UUID used only for terminal marking; `{session}` in the template is empty, the dialect parameters likewise omit session creation/resume options, and the UUID serves only terminal identity checks. `dismiss` still allows closing a terminal whose identity has been confirmed. `kander config`, the stderr of `config --json`, and `kander check` display a degradation notice. Persistent dispatch-back must still satisfy the existing stop facts and receipt gates, and does not use `none` to bypass the duplicate-execution guard.

A templated custom agent without a dialect must declare session explicitly. With a dialect, the default is inherited: Claude/Grok generate a UUID; Cursor invokes the configured program to run `create-chat`; Codex keeps the existing discovery mechanism of scanning CODEX_HOME rollouts (which likewise applies to a custom Codex dialect). `discovered` does not accept manual configuration. A template-less Codex dialect does not accept a `generated`/`allocated` override, and Cursor does not accept `generated`, because the corresponding start parameters cannot honor that identity source; inherit the default or provide a template. All dialects allow `none`, passing no session parameters at start.

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

"Task Execution and Models" can select a custom agent. Built-in agents that have not yet been probed successfully can also be selected in order to fill in the path of a renamed program; explicit paths are still validated on save. After each selected agent's model fields there are "Executable name" and "pane process name" inputs; when large and small tasks share the same agent, they are shown only once. Leaving them empty deletes the override; they are saved to `agents`. Dialects, templates, and session policies are edited only in JSON.

The reviewer roster still contains only the four built-in agents, which always use their read-only adapters. The review executable-name precedence is the `*_REVIEW_BIN` environment variable, then the review built-in program name; `agents.*.path` and `process_name` play no part at all in review selection. An execution agent's wrapper is therefore never automatically used for review.

`review_stages` is stored per task scale, matching `kanban_agents`:

```json
"review_stages": {
  "large": {"PM": "required", "QA": "auto", "CSA": "skip", "Hacker": "skip"},
  "small": {"PM": "auto", "QA": "auto", "CSA": "skip", "Hacker": "skip"}
}
```

A legacy flat `{role: mode}` object still loads and applies to both scales; saving rewrites it as the two-scale form. Missing scales or roles default to `auto`. The options panel's "Review and models" section edits large and small independently under each role. Agents resolve the third review-stage precedence tier from the card `SIZE`, and a mixed-size task-group batch uses the `large` scale.

A custom name with a compatible dialect reuses that dialect's exit command for `dismiss` and takeover cleanup, still requiring the identity and single-pane container checks to pass. A purely templated program with no compatible dialect declared has no inferable interactive exit command; `dismiss` refuses explicitly and keeps the container.
