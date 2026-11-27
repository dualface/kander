# kander config

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract"

## Purpose and users

Prints the merged effective configuration of the current scope. Every agent runs `kander config --json` at session start per `KANDER-AGENTS.md` "Every Session" and before each round of Kander work.

## Syntax

```text
kander config
```

## Arguments

None.

## Examples

```text
# Print the effective configuration as JSON
kander config --json

# Print the effective configuration as text
kander config
```

## Preconditions

- A readable configuration for the current scope. When reading or validating it fails, stop the affected Kander operations and report; never guess switch values.

## Effects

- Read-only.

## Output

- Without `--json`: the summary lines plus, when a project `.kander-config.json` exists, that file's absolute path.
- Without `--json`, the same remarks are one line each on stderr; stdout is the configuration without that content: a built-in agent an unusable `agents` entry took out of service shows with its models, and a role that names an unknown agent shows the value it has when the key is absent.
- Without `--json`, for a bound project: the server URL and project name in force, and the project's board path on the server host. The path comes from one read-only request to the bound server, bounded to 3 seconds; it prints as unavailable when the server cannot answer or the project is unbound or unregistered, and never changes the exit code.
- Without `--json`: a project policy line that says where the five project policy values come from (see "Project policy" below).
- With `--json`: one object, the merged effective configuration, and no server request. Among other keys it holds `issue.result_on_done` (`auto`, `ask`, or `off`; `auto` when missing), the completion handoff mode of `KANDER-ISSUE-RULES.md` "Completion Handoff", and the top-level object `project_policy` (see "Project policy" below). Remarks are `notices`: `agent_warning` for an agent warning, `config_ignored` for each setting this version ignores (an unknown key, a `models.*` entry of an unknown agent), `agent_unusable` for each `agents` entry it cannot use and each role that names an agent it does not know, and the project policy remarks below. Stderr stays empty.

## Project policy

The server keeps one project policy per project (`snippets/commands/policy.md`): `git.branch_mode`, `review_stages`, `rules`, `agent_language`, and `issue.result_on_done`. `kander config` never asks the server for it. It reads the local policy cache that the last command that opened the project wrote, keyed by the server and the project.

- With a valid cache of a policy whose revision is above 0, those five values in the output are the cached policy values, whatever `config.json` and `.kander-config.json` say, and `project_policy` is `{"epoch": "...", "revision": N, "source": "server_cache"}`.
- Otherwise the five values are the local ones and `project_policy.source` is `local`, with one notice: `project_policy_cache_missing` when no cache exists yet, `project_policy_unset` when the server answered that the project has no policy (run `kander policy init`), and `project_policy_cache_invalid` when the cache cannot be read or does not validate. The next command that opens the project replaces an invalid cache. A checkout without a project binding has `project_policy` `{"revision": 0, "source": "local"}` and no notice.
- A key of the cached policy that this version does not know (a rule module of a newer version, for example) is ignored with a `project_policy_ignored` notice. A rule module this version knows and the policy leaves out takes its default.
- Each value of `config.json` or `.kander-config.json` that the policy replaces with another value is one `project_policy_override` notice: the JSON path, the file, the file value, and the policy value. The files are not changed, and no command writes a policy value into them.

## Rejections

- An unknown option is a usage error.
