# Executing a Card

Source: moved from `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination" (the executing agent's part) and "Execution and Completion".

## Claiming, Starting, and Coordination

- Before touching code, obtain the unique entry in `working/`. The two claiming methods are mutually exclusive:

```sh
# Delegate to a new executing agent: start claims and launches atomically
kander start [--agent <configured-agent>] [--launcher <launcher>] <task-id>

# The current agent executes the card itself (explicit request, or intake option 1): claim and record ownership atomically
kander move <task-id> working --owner <agent>
```

- `kander move <task-id> working` applies only when the user explicitly asks the current agent to execute an existing card. Under the enabled intake guidance, choosing option 1, or a later start instruction for a card whose `DISCUSSION` carries an `EXECUTION_MODE: self` line (written by intake option 3), is that request. Option 2 starts cards with `start` (directly or through the orchestrator session of `snippets/orchestrate-sessions.md` "Orchestrator Sessions"). Never `move ... working` first and then `start`; `start` accepts only `todo` cards. Moving the entry on the same file system is the claiming primitive: only the one whose move succeeds obtains the task. After a failure, re-check; never create a replacement card, and add no lock service, database, or ID allocator.
- After claiming, only the executing owner modifies or moves the `working/` entry; coordinating and orchestrating agents supervise read-only, and after an explicit handover the new owner takes over.
- The temporary task file of `start` contains only the task ID and fixed requirements; the agent command line receives only one instruction to read that file. When `prompt_delivery.mode` is `pane`, that instruction is not an argv element: it is delivered after the agent TUI is ready. The executing agent first verifies the configuration through the entry, then reads this protocol, the card, and project rules, and prepares the working directory per the applicable flow; fill in `TASK_BRANCH` when a task branch is used. In the start task file of a task group, "exit" means ending this round of response, not exiting the interactive CLI or closing the container. When a non-interactive invocation ends naturally, later dispatch-back is recovered by `notify`, and an interactive session is kept until the user agrees to dismiss it.

## Execution and Completion

- A single card is delivered per the task contract, user and project rules, and the enabled modules. With the Git module off, develop, worktrees, commits, push, or merge back are not required; with the review module off, review is not required automatically; the user's own review, PR, or acceptance conditions still apply.
- With `rules.git=true`, authorizing execution of a standalone card also authorizes its merge-back to `develop` and cleanup. After acceptance criteria, verification, and applicable review gates are satisfied, the executing agent continues automatically through integration, applicable push, local sync, branch and worktree cleanup, and the completion commands below, following `KANDER-GIT-RULES.md`. It never asks for a separate merge-back confirmation or stops merely because the code is committed or review passed. Explicit pause, acceptance, PR, or branch-retention requirements remain binding. A single card stays in `working/` through this flow; `review/` is reserved for task groups.
- Confirm the actual working directory from the card records; record implementation, verification, and unresolved issues. After completing the contract and all applicable delivery steps, write `SUMMARY` or report.md through update. Then satisfy `snippets/review-evidence-gate.md` "Review Evidence Completion Gate" (a sealed plan with every batch closed, or the explicit N/A plan when no review applied). Then run `kander move <task-id> done --result completed` and `kander check`. When `rules.reporting=true`, report to the user from the template that move printed, filling every placeholder. An in-group card completes per `snippets/wrap-up.md` "Executing Agent Wrap-Up" with the targeted `kander check <task-id>`.
- On failure or pause, keep the actual state and record the blocker and the unblocking condition; write N/A for inapplicable Git or review steps, never unexecuted verification as passed.
- Task group members run review, integration, and wrap-up per `KANDER-TASK-GROUP-RULES.md` only when task_groups and git are enabled; those gates never apply to independent single cards. The fixed report format is read from `KANDER-REPORTING-RULES.md` only when `rules.reporting=true`; when off, use the user's own form without omitting the card result and execution records.
- A card in done is never moved back or reused for a problem found afterwards; create a new card that points at the original.
