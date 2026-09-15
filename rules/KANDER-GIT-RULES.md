# Git Workflow Rules

- Check the current working tree before any Git operation. Before creating a task worktree, also check the main worktree. Preserve user changes.

## Branches and Worktrees

- Fixed `main` + `develop`: `main` is stable, `develop` is the only integration branch. `main` advances only from `develop` and only with explicit user confirmation; agents never push `main` automatically.

**Initializing Branches**

- With `origin` and no local-only request: fetch first and require `origin/main`. When only `origin/develop` is missing, create it: from the latest `origin/main` when there is no local `develop`, otherwise from the local `develop` after confirming `origin/main` is its ancestor; then push it normally. If the check fails, stop and report.
- Without `origin`, or with local-only requested: require a local `main`; if `develop` is missing, create it from `main`.
- When `main` is missing, stop and report; do not guess a substitute branch or rewrite history.

**Task Branches**

- File-changing tasks use a dedicated task branch and worktree at `<repo-root>/worktrees/<task-name>/`, where `<task-name>` is the branch name, a short kebab-case string. Never carry a task on a stable branch, `develop`, or a detached `HEAD`. Reuse the branch and worktree when already on this task's own.
- With `origin` and no local-only request: fetch, then create the task branch from the latest `origin/develop`; if the fetch fails, stop and report. Without `origin`, or with local-only explicit: create from the local `develop` and report that the remote is not synced.
- These points apply to single cards. When the task group module is enabled, the group branch is created from `develop`, its worktree is named by the group ID while the branch carries the `group/` prefix, and in-group task branches are created from the group branch; see `KANDER-TASK-GROUP-RULES.md` "Group Integration Branch". Do not load a disabled task group module through this reference.

## Local Change Protection

- All uncommitted changes are user assets: staged, unstaged, and untracked, related to the task or not. Never use `git restore`, `git checkout --`, `git reset --hard`, `git clean`, or equivalents to discard, overwrite, or delete them.
- When the main worktree has uncommitted changes and an operation that needs a clean tree must run (rebase, merge, fast-forward, branch switch), first `git stash push --include-untracked` and confirm the stash holds everything. Do not mix these changes into task commits.
- Immediately after the operation, `git stash pop --index`. On conflict, keep both the operation result and the original changes, resolve item by item, and restore the original staging state. Until everything is confirmed restored, never drop the stash, clean files, declare completion, or leave; if lossless restoration is impossible, stop and report.

## Commit and Push

- Commit each independent concern separately once it is complete and verified; do not mix unrelated changes.
- With a writable `origin` and no local-only request, push normally after every commit; the first push uses `git push -u origin <branch>`. When the user asks to push, check all uncommitted and unpushed state, commit only the changes authorized for this task, and preserve and report the rest.
- Without `origin`, or with local-only explicit: keep the local commits, skip the push, and report. When `origin` exists but is unreachable, unwritable, or pushing is forbidden, and local-only was not authorized: keep the branch and worktree, report, and stop integration.
- For a standalone kanban card with `rules.git=true`, authorization to execute the card includes integration into `develop` and cleanup, whether the request executes an existing card or a confirmed plan creates and starts one, and it survives a resume of the same task. Once the task contract, verification, and applicable review gates pass, the executing agent integrates, pushes and syncs as applicable, cleans up, and completes the card without asking for a separate merge-back confirmation.
- Explicit user or project requirements to pause, wait for acceptance, use a PR, or retain the branch still apply. Enabling the Git module, or recording a card without authorization to execute it, grants no integration authority. Non-kanban tasks and task group integration need an explicit integration request or a user-confirmed plan that includes that step; do not ask again when authorization exists. Integration into `main` always needs its own explicit confirmation.

## Keeping a Task Branch Current

- A task branch drifts from its source branch (`develop` for a single card, the group branch for an in-group card) the whole time it is open, and the eventual conflict grows with the drift. Fetch at every natural pause (before starting a work item, after finishing one, before requesting review, before delivery) and run the overlap check, with `SOURCE` the remote or local source branch:

```sh
git fetch -q origin
kander check overlap --source SOURCE --json
```

- `status` `action-required` names paths changed on both sides: rebase onto the latest source branch now, while the upstream change is small and its author is reachable. `status` `pass` (empty `paths`): rebase at the latest before the next delivery. A rebase onto an unchanged source branch is a no-op, so running it often is fine. Exit 2 or 3 means the check did not finish; do not treat that as an empty overlap. Do not substitute `comm`, process substitution, or `git diff --name-only` pipelines for this command.
- Do not rebase while a review batch is open on this branch (`KANDER-REVIEW-RULES.md` "Review Base"); rebase before the review starts or after the batch closes.
- Re-verification after a clean mid-task rebase: compile the touched modules and run the targeted tests of the changed behavior; the full `KANDER-CODE-RULES.md` "Delivery Self-Check" item 6 runs once, at the final delivery commit. After a rebase with conflict resolution, also rerun the tests of every file touched by the resolution.
- When the source branch already contains a fix for the same problem, take the upstream version and drop this branch's duplicate commit.
- Only the dedicated task branch is rewritten this way, and only with `--force-with-lease` per "Integration and Cleanup".

## Integration and Cleanup

- Before integrating, verify the authorization above and any PR, acceptance, or pause requirement from the user or project. Without authorization or with an unmet gate, keep the branch and worktree and report the specific pending items; a kanban single card stays in `working/`, and a task group keeps its actual state per the group rules.
- The source branch is the delivery target recorded when the task branch was created (in the card's `IMPLEMENTATION` for a kanban card, otherwise in the task's own delivery record): `develop` for a single card or a group branch, the group branch for an in-group task branch. Integrate only into that target; the checked-out branch or a temporary rebase never changes it.
- Before integrating, fetch the source branch (remote path) or read the local source branch (local path), rebase if needed, and re-verify per "Keeping a Task Branch Current". If the fetch fails, stop and report. On rebase conflicts, prefer the task branch's changes but never replace whole files outright; resolve feature point by feature point, and ask the user when the resolution would be a major change. After a successful rebase, only the dedicated task branch may `--force-with-lease` its pushed history; if the lease fails, stop and report.

**One-Time Review Gate**

- Review is triggered and executed per `KANDER-REVIEW-RULES.md` only when the review module is enabled or the user explicitly asks for a full review this time. Otherwise record N/A and do not load the disabled module.
- An applicable review is a one-time gate before integration; the base is frozen during the review. For a single card, a rebase after the review because the source branch advanced only redoes verification; re-review only when the user asks or substantive code conflicts were resolved by hand, never for no conflicts or Markdown-only conflicts. For a task group, the bound wrap-up evidence accepts the rebased group line only when its complete patch equals the closed reviewed patch, so the integration rebase must apply cleanly and leave the patch unchanged; any conflict, Markdown included, or a changed patch is handled per `KANDER-TASK-GROUP-RULES.md` "Merge-Back and Cleanup Preconditions".
- For a task group, this gate constrains merging the group branch back into `develop`; delivering a task branch to the group branch prepares the group-level review and does not wait for it.

**Direct Integration and PRs**

- Remote direct integration first pushes the complete commit: `git push origin <final delivery commit>:refs/heads/<source branch>`; do not advance the local source branch before this succeeds. When the remote rejects because it advanced concurrently, fetch, rebase the branch to deliver onto the latest `origin/<source branch>`, re-verify, and retry; an in-group task branch is handled by its original executing agent, the orchestrator only dispatches back and receives.
- After a successful push, fetch and run `git merge --ff-only origin/<source branch>` in the worktree that owns the source branch. When the remote already contains the recorded final delivery commit, do only this sync. If the local branch is missing, create it from the remote branch. If the sync fails (local divergence, working tree, other), preserve the working state, report "integrated on remote, local not synced", and handle only the sync problem afterwards without resetting or discarding local commits.
- Without `origin`, or with local-only explicit: run `git merge --ff-only <final delivery commit>` in the worktree that owns the source branch. If it fails because the local source branch advanced, rebase the branch to deliver onto it, re-verify, and retry; do not push.
- Direct integration and local sync never create merge commits. `main`, `develop`, and group branches never have their remote history rewritten with `--force` or `--force-with-lease`. Save and restore user changes per "Local Change Protection" around any operation on the main worktree.
- When the user or project requires a PR, follow its review, CI, merge method, and authorization requirements; do not bypass it with direct integration. Integration is complete only when the PR is merged, its target is the recorded source branch, and the final delivery is contained in the merge result; confirm squash and rebase merges by this evidence, without requiring the pre-merge commit to remain an ancestor. For a task group, the durable wrap-up evidence additionally needs the Git mapping in `KANDER-TASK-GROUP-RULES.md` "Merge-Back and Cleanup Preconditions".

**Cleanup Preconditions**

- Only after integration, verification, applicable push, and local sync all succeed, verify that the final delivery entered the actual target: for direct integration `git merge-base --is-ancestor <final delivery commit> <origin/source branch or local source branch>` with the full SHA after the integration rebase, for PRs the merged criteria above. If this cannot be confirmed, preserve the working state and report.
- A single card that meets the preconditions cleans up its task branch and worktree; the remote task branch only on a non-local path. An in-group card keeps its working state after delivering to the group branch until the orchestrator confirms the whole group entered `develop` and sends the wrap-up notice; the orchestrator cleans up the group worktree and branch after the whole group wraps up.
