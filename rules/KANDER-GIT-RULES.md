# Git Workflow Rules

- Check the current working tree before any Git operation.
- Before creating a task worktree, also check the main worktree. Preserve user changes.

## Branches and Worktrees

- `main` only advances from `develop` and requires explicit user confirmation; agents never push `main` automatically.
- Fixed `main` + `develop`: `main` is stable, `develop` is the only integration branch.

**Initializing Branches**

- Initialization: when `origin` exists and the user has not asked for local-only, fetch first and require `origin/main` to exist.
  - When only `origin/develop` is missing, create the remote branch:
    - If there is no local `develop`, create it from the latest `origin/main`.
    - If a local `develop` already exists, confirm that `origin/main` is its ancestor.
    - When satisfied, do a normal push to `origin/develop`; otherwise stop and report.
- When there is no `origin` or the user asked for local-only, require a local `main` to exist; if `develop` is missing, create it from `main`.
- When `main` is missing, stop and report; do not guess a substitute branch or rewrite history.

- File-changing tasks use a separate task branch and a dedicated worktree at `<repo-root>/worktrees/<task-name>/`.

  `<task-name>` is the same as the branch name, a short kebab-case string.

  Never carry a task on a stable branch, `develop`, or a detached `HEAD`.

  Reuse them when already on this task's dedicated branch and worktree.

- When `origin` exists and the user has not asked for local-only integration, fetch and then create the task branch from the latest `origin/develop`.

  If the fetch fails, stop creating and report.

  When there is no `origin` or local-only is explicit, create from the local `develop` and report that the remote is not synced.

- The base points above apply to single cards. When the task group module is enabled, the group branch is created from `develop` and in-group task branches are created from the group branch; the two-layer worktrees and the split of delivery and cleanup duties follow `KANDER-TASK-GROUP-RULES.md` "Group Integration Branch". Do not load a disabled task group module through this reference.

## Local Change Protection

- All uncommitted changes are user assets, including staged, unstaged, and untracked files.

  Never use `git restore`, `git checkout --`, `git reset --hard`, `git clean`, or equivalent operations to discard, overwrite, or delete them.

  Unrelated changes are no exception.

- When the main worktree has uncommitted changes and an operation that requires a clean working tree (rebase, merge, fast-forward, branch switch, etc.) must run, first `git stash push --include-untracked` and confirm the new stash holds all changes before operating.

  Do not mix them into task commits.

- Immediately after the operation, `git stash pop --index`. On conflict, keep both the operation result and the original changes, resolve item by item, and restore the original staging state. Until everything is confirmed restored, never delete the stash, clean files, declare completion, or leave; if lossless restoration is impossible, stop and report.

## Commit and Push

- Commit each independent concern separately once it is complete and verified; do not mix unrelated changes.
- When the task branch has a writable `origin` and the user has not asked for local-only, do a normal push after every commit; use `git push -u origin <branch>` for the first push.
- When the user asks to push, check all uncommitted and unpushed state; commit only the changes authorized for this task, and preserve and report the others.
- When there is no `origin` or local-only is explicit, keep the local commits, skip the push, and report. When `origin` exists but is unreachable, unwritable, or the user forbids pushing, and local-only was not authorized, keep the task branch and worktree, report, and stop integration.
- Never integrate the task branch into the source branch automatically unless the user explicitly asks for integration or a user-confirmed task plan explicitly includes that integration step. Do not ask again when authorization already exists; merely enabling the Git module, creating a card, or starting an agent does not constitute integration authorization.

## Integration and Cleanup

- Before integrating, verify the authorization above along with any existing PR, acceptance, and pause requirements from the user or project. Without authorization or with an unmet gate, keep the branch and worktree and report the specific pending items; a kanban single card stays in `working/`, and a task group keeps its actual state per the group rules.
- The source branch is the delivery target explicitly recorded when the task branch was created: `develop` for a single card, the corresponding group branch for an in-group task branch, and `develop` for a group branch. Integrate only into that target; the currently checked-out branch or a temporary rebase does not change the target.

- Before integrating, fetch the source branch on the remote path or read the local source branch on the local path, check whether the branch to deliver needs a rebase, and re-verify. If the fetch fails, stop and report.

  On rebase conflicts, prefer keeping the task branch's changes, but never replace whole files outright; resolve conflicts feature point by feature point. If this would cause a major change, ask the user to confirm.

  After a successful rebase, only the dedicated task branch may use `--force-with-lease` to update its already-pushed history; if the lease fails, stop and report, and do not overwrite remote changes.

**One-Time Review Gate**

- Review is triggered and executed per `KANDER-REVIEW-RULES.md` only when the review module is enabled or the user explicitly asks for a full review this time. Otherwise record N/A and do not load the disabled review module.
- An applicable review is a one-time gate before integration; the base is frozen during the review. A rebase after the review completes because the source branch advanced only redoes verification; re-review only when the user explicitly asks or substantive code conflicts were resolved by hand. Do not re-review when there are no conflicts or the conflicts are only in Markdown documents. The base chain and fix rounds for task group batches follow the group-level review rules.
- For a task group, this gate constrains merging the group branch back into `develop`; delivering a task branch to the group branch is a preparation step for the group-level review and does not require the group-level review to complete first.

**Direct Integration and PRs**

- Remote direct integration first pushes the complete commit normally: `git push origin <final delivery commit>:refs/heads/<source branch>`; do not advance the local source branch before this succeeds. When the remote rejects because it advanced concurrently, fetch, rebase the branch to deliver onto the latest `origin/<source branch>`, re-verify, and retry; an in-group task branch is handled by the original executing agent, and the orchestrator only dispatches back and receives.
- After a successful push, fetch and run `git merge --ff-only origin/<source branch>` in the worktree that owns the source branch. When the remote already contains the recorded final delivery commit, do only this sync; do not rebase again or re-integrate. If the local branch is missing, create it from that remote branch; if the sync fails due to local divergence, working tree, or other sync problems, preserve the working state and report "integrated on remote, local not synced", then handle only the sync problem afterward without resetting or discarding local commits.
- When there is no `origin` or local-only is explicit, run `git merge --ff-only <final delivery commit>` in the worktree that owns the source branch. If it fails because the local source branch has advanced, rebase the branch to deliver onto the latest local source branch, re-verify, and retry; do not push.
- Direct integration and local sync never generate merge commits. `main`, `develop`, and group branches must never have their remote history rewritten with `--force` or `--force-with-lease`. Save and restore user changes per "Local Change Protection" before and after operating on the main worktree.
- When the user or project requires a PR, follow its review, CI, merge method, and authorization requirements; do not bypass it with direct integration. Integration counts as complete only when the PR is merged, its target is the recorded source branch, and the final delivery is actually contained in the merge result; confirm squash/rebase merges by this evidence and do not require the pre-merge commit to remain an ancestor of the target branch.

**Cleanup Preconditions**

- Only after integration, verification, applicable push, and local sync all succeed, verify that the final delivery has entered the actual target. For direct integration use `git merge-base --is-ancestor <final delivery commit> <origin/source branch or local source branch>` with the full SHA after the integration rebase; for PRs use the merged criteria above. If this cannot be confirmed, preserve the working state and report.
- Once a single card meets the preconditions, clean up its task branch and worktree; clean up the remote task branch only on a non-local path. An in-group card keeps its working state after delivering to the group branch until the orchestrator confirms the whole group has entered `develop` and sends the wrap-up notice; the group worktree and group branch are cleaned up by the orchestrator after the whole group wraps up.
