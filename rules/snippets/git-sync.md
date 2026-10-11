# Keeping a Task Branch Current

Source: moved from `KANDER-GIT-RULES.md` "Keeping a Task Branch Current".

## Keeping a Task Branch Current

- A task branch drifts from its source branch (`develop` for a single card, the group branch for an in-group card) the whole time it is open, and the eventual conflict grows with the drift. Fetch at every natural pause (before starting a work item, after finishing one, before requesting review, before delivery) and run the overlap check, with `SOURCE` the remote or local source branch:

```sh
git fetch -q origin
kander check overlap --source SOURCE --json
```

- `status` `action-required` names paths changed on both sides: rebase onto the latest source branch now, while the upstream change is small and its author is reachable. `status` `pass` (empty `paths`): rebase at the latest before the next delivery. A rebase onto an unchanged source branch is a no-op, so running it often is fine. Exit 2 or 3 means the check did not finish; never treat that as an empty overlap. Never substitute `comm`, process substitution, or `git diff --name-only` pipelines for this command.
- Never rebase while a review batch is open on this branch (`KANDER-REVIEW-RULES.md` "Review Base"); rebase before the review starts or after the batch closes.
- Re-verification after a clean mid-task rebase: compile the touched modules and run the targeted tests of the changed behavior; the full `KANDER-CODE-RULES.md` "Delivery Self-Check" item 6 runs once, at the final delivery commit. After a rebase with conflict resolution, also rerun the tests of every file touched by the resolution.
- When the source branch already contains a fix for the same problem, take the upstream version and drop this branch's duplicate commit.
- Only the dedicated task branch is rewritten this way, and only with `--force-with-lease` per `snippets/git-integrate.md` "Integration and Cleanup"; the group branch exception named there belongs to the orchestrator alone.
