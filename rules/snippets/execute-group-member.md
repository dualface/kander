# Executing a Task Group Member

Source: moved from `KANDER-TASK-GROUP-RULES.md` "Git Branches and Worktrees" and the executing agent's part of "Delivering a Task Branch to the Group Branch".

This snippet extends `snippets/execute-single.md`: read that one first, then this one. It covers what a task group member does differently: its branch comes from the group branch, it delivers to the group branch and waits for `notify`, and it never integrates. When the member-card start entry of `snippets/orchestrate-start.md` "Entering Through a Member Card" makes this agent the orchestrator instead, read `snippets/orchestrate-start.md`.

## Git Branches and Worktrees

- A single card's executing agent creates a dedicated task branch and `<repo-root>/worktrees/<task-name>/` worktree per the Git rule file, with `develop` as the source branch.
- A task group uses two layers: the orchestrator holds the group branch `group/<task-group-id>` and the worktree `<repo-root>/worktrees/<task-group-id>/`, used to receive deliveries, run group-level verification and applicable reviews, and integrate. Each member's executing agent still creates its own task branch and worktree.
- In-group task branches are created from the latest group branch, with `group/<task-group-id>` as the source branch, not from `develop`; on the remote path fetch `origin/group/<task-group-id>` first. Other naming, paths, and user change protection follow the Git rule file.
- The executing agent changes only its own card's worktree and does its own rebases and conflict fixes there. The orchestrator operates only on the group worktree and the integration target and never modifies task branches or code on a member's behalf.

**Delivering a Task Branch to the Group Branch**

- After implementing, verifying, and committing by concern, the executing agent rebases its task branch onto the latest group branch, re-verifies, and updates the task branch per the Git rule file. Between deliveries it keeps the branch current per `snippets/git-sync.md` "Keeping a Task Branch Current" with the group branch as `SOURCE`, which also fixes the re-verification scope; the final delivery commit carries the full verification. Record in `IMPLEMENTATION` the task branch's final full SHA, the group branch SHA it is based on, and the verification result, then `move review` and end the current response turn. The executing agent never updates the group branch.
