# Git Workflow Rules

- Check the current working tree before any Git operation. Before creating a task worktree, also check the main worktree. Preserve user changes.

## Branches and Worktrees

- Fixed `main` + `develop`: `main` is stable, `develop` is the only integration branch. `main` advances only from `develop` and only with explicit user confirmation; agents never push `main` automatically.

**Initializing Branches**

- With `origin` and no local-only request: fetch first and require `origin/main`. When only `origin/develop` is missing, create it: from the latest `origin/main` when there is no local `develop`, otherwise from the local `develop` after confirming `origin/main` is its ancestor; then push it normally. If the check fails, stop and report.
- Without `origin`, or with local-only requested: require a local `main`; if `develop` is missing, create it from `main`.
- When `main` is missing, stop and report; never guess a substitute branch or rewrite history.

**Task Branches**

- File-changing tasks use a dedicated task branch and worktree at `<repo-root>/worktrees/<task-name>/`, where `<task-name>` is the branch name, a short kebab-case string. Never carry a task on a stable branch, `develop`, or a detached `HEAD`. Reuse the branch and worktree when already on this task's own.
- With `origin` and no local-only request: fetch, then create the task branch from the latest `origin/develop`; if the fetch fails, stop and report. Without `origin`, or with local-only explicit: create from the local `develop` and report that the remote is not synced.
- These points apply to single cards. When the task group module is enabled, the group branch is created from `develop`. Its worktree is named by the group ID, while the branch carries the `group/` prefix. In-group task branches are created from the group branch; see `KANDER-TASK-GROUP-RULES.md` "Group Integration Branch". Never load a disabled task group module through this reference.

## Local Change Protection

- All uncommitted changes are user assets: staged, unstaged, and untracked, related to the task or not. Never use `git restore`, `git checkout --`, `git reset --hard`, `git clean`, or equivalents to discard, overwrite, or delete them.
- When the main worktree has uncommitted changes and an operation that needs a clean tree must run (rebase, merge, fast-forward, branch switch), first `git stash push --include-untracked` and confirm the stash holds everything. Never mix these changes into task commits.
- Immediately after the operation, `git stash pop --index`. On conflict, keep both the operation result and the original changes, resolve item by item, and restore the original staging state. Until everything is confirmed restored, never drop the stash, clean files, declare completion, or leave; if lossless restoration is impossible, stop and report.

## Commit and Push

- Commit each independent concern separately once it is complete and verified; never mix unrelated changes.
- With a writable `origin` and no local-only request, push normally after every commit; the first push uses `git push -u origin <branch>`. When the user asks to push, check all uncommitted and unpushed state, commit only the changes authorized for this task, and preserve and report the rest.
- Without `origin`, or with local-only explicit: keep the local commits, skip the push, and report. When `origin` exists but is unreachable, unwritable, or pushing is forbidden, and local-only was not authorized: keep the branch and worktree, report, and stop integration.
- For a standalone kanban card with `rules.git=true`, authorization to execute the card includes integration into `develop` and cleanup. This holds whether the request executes an existing card or a confirmed plan creates and starts one, and it survives a resume of the same task. Once the task contract, verification, and applicable review gates pass, the executing agent integrates, pushes and syncs as applicable, cleans up, and completes the card without asking for a separate merge-back confirmation.
- Explicit user or project requirements to pause, wait for acceptance, use a PR, or retain the branch still apply. Enabling the Git module, or recording a card without authorization to execute it, grants no integration authority. Non-kanban tasks and task group integration need an explicit integration request or a user-confirmed plan that includes that step; never ask again when authorization exists. Integration into `main` always needs its own explicit confirmation.

## Phase Rules

- Keeping a task branch current (fetch, overlap check, rebase, re-verification): `snippets/git-sync.md`.
- Integration and cleanup (authorization, the one-time review gate, direct integration and PRs, cleanup preconditions): `snippets/git-integrate.md`.
