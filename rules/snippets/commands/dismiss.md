# kander dismiss

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Dismissal and Terminal Cleanup")

## Purpose and users

Exits the executing agent of a finished card and closes its container. Used by the orchestrator after the user confirmed the dismiss question, and by the user.

## Syntax

```text
kander dismiss [--timeout SECONDS] <task-id>
```

## Arguments

- `task-id`: a card in `done/` or `archived/`.
- `--timeout SECONDS`: greater than 60; default 120.

## Preconditions

- `dismiss` accepts only `done/` or `archived/` cards. It locates the pane by the card `WINDOW` (or a unique session reverse lookup). It requires the agent and session to match, the agent to be idle, and the container to hold only that pane. The agent definition must declare an `exit_command`.

## Effects

- Changes no card body or state. Sends the `exit_command` declared on the agent definition and closes the tab or window only after the agent process has exited.

## Output

- The result of the exit and close steps.

## Rejections

- A card claimed or reclaimed with `--owner`: it has no executor session to dismiss.
- Any identity, state, ownership, delivery, exit confirmation, or close failure, or a timeout, returns non-zero and preserves the working state; the command never force-kills or degrades to closing the container. `foreground`/`console` have no container: report an error without partial action. An agent without `exit_command` is refused.
- A missing task ID, a timeout of 60 seconds or less, or an unknown option is a usage error.
