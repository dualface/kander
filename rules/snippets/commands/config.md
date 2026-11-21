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
- With `--json`: one object, the merged effective configuration, and no server request. Among other keys it holds `issue.result_on_done` (`auto`, `ask`, or `off`; `auto` when missing), the completion handoff mode of `KANDER-ISSUE-RULES.md` "Completion Handoff". Remarks are `notices`: `agent_warning` for an agent warning, `config_ignored` for each setting this version ignores (an unknown key, a `models.*` entry of an unknown agent), and `agent_unusable` for each `agents` entry it cannot use and each role that names an agent it does not know. Stderr stays empty.

## Rejections

- An unknown option is a usage error.
