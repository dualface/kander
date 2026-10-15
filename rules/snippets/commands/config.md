# kander config

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract"

## Purpose and users

Prints the merged effective configuration of the current scope. Every agent runs `kander config --json` at session start per `KANDER-AGENTS.md` "Every Session" and before each round of Kander work.

## Syntax

```text
kander config [--json]
```

## Arguments

- `--json`: print the configuration as one JSON object instead of the summary.

## Preconditions

- A readable configuration for the current scope. When reading or validating it fails, stop the affected Kander operations and report; never guess switch values.

## Effects

- Read-only.

## Output

- Without `--json`: the summary lines plus, when a project `.kander-config.json` exists, that file's absolute path.
- With `--json`: the merged effective configuration. Among other keys it holds `issue.result_on_done` (`auto`, `ask`, or `off`; `auto` when missing), the completion handoff mode of `KANDER-ISSUE-RULES.md` "Completion Handoff".

## Rejections

- An unknown option is a usage error.
