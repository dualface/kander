# Keeping a Task Branch Current

Source: moved from `KANDER-GIT-RULES.md` "Keeping a Task Branch Current".

## Keeping a Task Branch Current

- At each natural pause (before and after a work item, before review, and before delivery), check the task branch against its source: the integration target for a single card or the group branch for a member. Set `source_branch` to the recorded branch name. With `origin` and remote delivery:

```sh
git fetch -q origin && kander check overlap --source "refs/remotes/origin/$source_branch" --json
```

Without `origin`, or with explicit local-only delivery:

```sh
kander check overlap --source "refs/heads/$source_branch" --json
```

- A failed remote fetch stops the remote path. `action-required` lists paths changed on both sides: inspect them now and sync overlapping work in the task worktree. With `pass`, independent commits may be delivered from diverged branches. Exit 2 or 3 means the check did not finish. Never substitute path-list shell pipelines for the overlap check.
- Never rebase while a review batch is open on this branch (`KANDER-REVIEW-RULES.md` "Review Base"); synchronize before the review starts if needed. After the batch closes, integration follows `snippets/git-integrate.md` "One-Time Review Gate", which may require a merge commit instead.
- Re-verification after a clean mid-task merge or rebase: compile the touched modules and run the targeted tests of the changed behavior; the full `KANDER-CODE-RULES.md` "Delivery Self-Check" item 6 runs once, at the final delivery commit. After resolving a conflict, also rerun the tests of every file touched by the resolution.
- When the source branch already contains a fix for the same problem, take the upstream version and drop this branch's duplicate commit.
- Only the dedicated task branch is rewritten this way, and only with `--force-with-lease`; if the lease fails, stop and report. The group branch exception in `snippets/git-integrate.md` "Direct Integration and PRs" belongs to the orchestrator alone.
