# Git Workflow Rules

- Check the current working tree before any Git operation. Before creating a task worktree, also check the main worktree. Preserve user changes.

## Branches and Worktrees

- `git.branch_mode` (`kander config --json`) selects the **mainline branch**, the default integration target:
  - `main-develop` (default, also when missing): `develop` is the mainline branch and `main` is stable. `main` advances only from `develop` and only with explicit user confirmation; agents never push `main` automatically.
  - `main-only`: `main` is the mainline branch; no `develop` is used.
- A card's **integration target** is the mainline branch, or the branch named by its optional header line `- INTEGRATION_TARGET: <branch>`; `main` is never a target in `main-develop` mode. Where a rule names the mainline branch for a card or group, it means this target; `snippets/git-integrate.md` "Integration Target" lists the cases.
- Switch `git.branch_mode` only while no card is in `working/` or `review/` and no group branch is open.

**Initializing Branches**

- With `origin` and no local-only request: fetch first and require `origin/main`. In `main-develop` mode, when only `origin/develop` is missing, create it: from the latest `origin/main` when there is no local `develop`, otherwise from the local `develop` after confirming `origin/main` is its ancestor; then push it normally. If the check fails, stop and report.
- Without `origin`, or with local-only requested: require a local `main`; in `main-develop` mode, if `develop` is missing, create it from `main`.
- In `main-only` mode, never create `develop`; leave an existing one untouched.
- When `main` is missing, stop and report; never guess a substitute branch or rewrite history.

**Task Branches**

- File-changing tasks use a dedicated task branch and worktree at `<repo-root>/worktrees/<task-name>/`, where `<task-name>` is the branch name, a short kebab-case string. Never carry a task on `main`, `develop`, or a detached `HEAD`. Reuse the branch and worktree when already on this task's own.
- With `origin` and no local-only request: fetch, then create the task branch from the latest `origin/<integration target>`; if the fetch fails, stop and report. Without `origin`, or with local-only explicit: create from the local integration target and report that the remote is not synced.
- These points apply to single cards. When the task group module is enabled, the group branch is created from the integration target. Its worktree is named by the group ID, while the branch carries the `group/` prefix. In-group task branches are created from the group branch; see `snippets/orchestrate-start.md` "Group Integration Branch". Load that snippet only when task groups are enabled.
- In distributed mode (every v2 client), the task branch is the one Kander records in `TASK_BRANCH`, named `kander-task/<project-id>/<task-id>/g<generation>`. Delivery (`kander move <task-id> review --delivery-commit <SHA>`) and integration (`kander coordinator integrate`) do not need it on the common remote, so never push it after every commit. Kander's flow pushes it in two cases only: a `via_commit` that must be on the common remote (`snippets/git-integrate.md`) and a PR the user or project requires; a push the user asks for is the same. Every push uses that name, a PR or a `--force-with-lease` after a rebase included. Never push a ref below `refs/heads/kander/`: it holds Kander's immutable evidence refs only. A card started before this name records `kander/<project-id>/<task-id>/g<generation>`, which cannot be pushed as it is: push it with `git push origin <TASK_BRANCH>:refs/heads/kander-task/<TASK_BRANCH without the leading kander/>` every time.

## Local Change Protection

- All uncommitted changes are user assets: staged, unstaged, and untracked, related to the task or not. Never use `git restore`, `git checkout --`, `git reset --hard`, `git clean`, or equivalents to discard, overwrite, or delete them.
- When the main worktree has uncommitted changes and an operation that needs a clean tree must run (rebase, merge, fast-forward, branch switch), first `git stash push --include-untracked` and confirm the stash holds everything. Never mix these changes into task commits.
- Immediately after the operation, `git stash pop --index`. On conflict, keep both the operation result and the original changes, resolve item by item, and restore the original staging state. Until everything is confirmed restored, never drop the stash, clean files, declare completion, or leave; if lossless restoration is impossible, stop and report.

## Commit and Push

- Commit each independent concern separately once it is complete and verified; never mix unrelated changes.
- With a writable `origin` and no local-only request, push normally after every commit; the first push uses `git push -u origin <branch>`. In distributed mode, only as "Task Branches" says. When the user asks to push, check all uncommitted and unpushed state, commit only the changes authorized for this task, and preserve and report the rest.
- Without `origin`, or with local-only explicit: keep the local commits, skip the push, and report. When `origin` exists but is unreachable, unwritable, or pushing is forbidden, and local-only was not authorized: keep the branch and worktree, report, and stop integration.
- For a standalone kanban card with `rules.git=true`, authorization to execute the card includes integration into its integration target and cleanup. This holds whether the request executes an existing card or a confirmed plan creates and starts one, and it survives a resume of the same task. Once the task contract, verification, and applicable review gates pass, the executing agent integrates, pushes and syncs as applicable, cleans up, and completes the card without asking for a separate merge-back confirmation.
- Never rewrite the remote history of `main` or the mainline branch with plain `--force`. `--force-with-lease=<branch>:<just-fetched full SHA>` is permitted; when the lease fails, stop and report, never retry with `--force`. Integration still only fast-forwards. In `main-develop` mode, rewriting `main` needs the same confirmation as advancing it.
- Explicit user or project requirements to pause, wait for acceptance, use a PR, or retain the branch still apply. Enabling the Git module, or recording a card without authorization to execute it, grants no integration authority. Non-kanban tasks and task group integration need an explicit integration request or a user-confirmed plan that includes that step. In `main-develop` mode, advancing `main` always needs its own explicit confirmation; in `main-only` mode, integrating into `main` is mainline integration.

## Phase Rules

- Keeping a task branch current (fetch, overlap check, rebase, re-verification): `snippets/git-sync.md`.
- Integration and cleanup (authorization, the one-time review gate, direct integration and PRs, cleanup preconditions): `snippets/git-integrate.md`.
