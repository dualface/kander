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

## Distributed cleanup

With a configured server, dismissal reads the final task and execution record through REST and requires the exact owning host's private routing. Removing an execution secret keeps a separate token-free record of its final session, window and workspace. The record grants no task write or renewal authority. Another host or a process bound to a predecessor refuses cleanup before any terminal operation.

The client checks final state, unchanged version and generation, and the exact local pane/session before recording a private exit intent. It never reverse-searches a foreign or stale pane. A successful receipt is replayed without sending another exit command. An uncertain retry only recognizes a container already gone; otherwise it reports the retained receipt and leaves inspection to the operator. Dismissal writes no local board and preserves worktrees.
