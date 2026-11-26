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
- `--timeout SECONDS`: how long to wait for the agent to exit, in seconds, more than 60; default 120.

## Examples

```text
# Exit the agent of a finished card and close its container
kander dismiss 20261118-example-task

# Wait up to 180 seconds for the agent to exit
kander dismiss --timeout 180 20261118-example-task
```

## Preconditions

- `dismiss` accepts only `done/` or `archived/` cards. It locates the pane by the card `WINDOW` (or a unique session reverse lookup). It requires the agent and session to match, the agent to be idle, and the container to hold only that pane. The agent definition must declare an `exit_command`.

## Effects

- Changes no card body or state. Sends the `exit_command` declared on the agent definition and closes the tab or window only after the agent process has exited.

## Output

- The result of the exit and close steps. With `--json`: `task_id` and `outcome`, `dismissed` or `target_gone` (the container was already gone).

## Rejections

- A card claimed or reclaimed with `--owner`: it has no executor session to dismiss.
- Any identity, state, ownership, delivery, exit confirmation, or close failure, or a timeout, returns non-zero and preserves the working state; the command never force-kills or degrades to closing the container. `foreground`/`console` have no container: report an error without partial action. An agent without `exit_command` is refused.
- A missing task ID, a timeout of 60 seconds or less, or an unknown option is a usage error.

## Distributed cleanup

With a configured server, dismissal reads the final task and execution record through REST and requires the exact owning host's private routing. Removing an execution secret keeps a separate token-free record of its final session, window and workspace. The record grants no task write or renewal authority.

- A process with no execution binding, on the owning host, may dismiss.
- A task executor process (`KANDER_EXECUTION_ID` set, `KANDER_COORDINATOR_EXECUTION_ID` empty) that is not the card's own execution is refused before any terminal operation, as is a process on another host or one bound to a predecessor.
- An orchestrator process started by `kander orchestrate` (both variables set to the same execution ID) may dismiss a member of its task group. It must hold, on the owning host, the valid group credential of the group named by the card's `TASK_GROUP`: present, not released, not lost, of kind group, and bound to this server, client and project. A card with no `TASK_GROUP` is refused for it. Every other check below still applies.
- Run the dismissal while the group lease is valid, before `kander coordinator release`: the release ends the lease. The orchestrator is refused whenever its local group credential is no longer valid: the worker removes it after it confirmed the release, or marks it lost after another orchestrator took the group over. An orchestrator that ran a card outside any task group cannot dismiss that execution; the user runs `kander dismiss <task-id>` in a shell with neither `KANDER_EXECUTION_ID` nor `KANDER_COORDINATOR_EXECUTION_ID` set.
- Each refusal that needs the retained execution keeps the sentence `This operation requires the exact retained execution on its owning host` and appends the failed condition with its values (execution IDs, client IDs, group ID, revision, generation, `WINDOW`, `SESSION`). The message never contains a credential token.

A card and execution record whose `SESSION` is only the agent of the retained session (for example `codex`, retained as `codex <id>`), in the same `WINDOW`, belong to an execution started before starts recorded the discovered session; dismissal accepts that pair and identifies the executor by the retained session. Every other difference is still refused as a scene or record mismatch.

The client checks final state, unchanged version and generation, and the exact local pane/session. It then runs every pre-send check of the pane (delivery probe, agent status, session match, and the container holding only that pane) and records a private exit intent only after they all pass, immediately before the first exit command is sent. When a pre-send check fails, for example because the agent waits on a permission dialog (`blocked`), nothing was sent and the receipt stays prepared: run `kander dismiss` again once the pane accepts delivery. A failure after the first send keeps the exit intent, because the agent may have received the exit command. It never reverse-searches a foreign or stale pane. When the recorded terminal no longer exists, the receipt is recorded as complete, one line names the task and the `WINDOW`, and the exit code is 0; a later run prints the ordinary dismissed line. A successful receipt is replayed without sending another exit command. An uncertain retry only recognizes a container already gone; otherwise it reports the retained receipt and leaves inspection to the operator. Dismissal writes no local board and preserves worktrees.
