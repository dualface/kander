# Executing Agent Wrap-Up

Source: moved from `KANDER-TASK-GROUP-RULES.md` "Executing Agent Wrap-Up".

This snippet is read when a `notify --kind wrap-up` dispatch arrives.

**Executing Agent Wrap-Up**

- On a wrap-up notice, the executing agent first moves back to `working/` with the ID/epoch-bound move command, then confirms per `snippets/git-integrate.md` "Integration and Cleanup" that its changes are in the integration target. For direct integration, the check is `git merge-base --is-ancestor` with the final group HEAD plus the notice's statement that this card's changes are included. For a PR, the check is the merged criteria and the `source_commit`/`rebased_base` mapping given in the notice. Never judge ancestry with the pre-rebase SHA. If not satisfied, preserve the working state and report to the orchestrator.
- Once satisfied, delete this card's worktree, local task branch, and applicable remote task branch, without touching the group worktree or branch. Complete the summary's review and wrap-up parts (role conclusions or N/A, applicable rounds, final commit, group branch and integration target results). Publish records through the controlled update entrance. Run the completion command from the dispatch prompt, `kander move <task-id> done --result completed --dispatch-id <id> --execution-epoch <epoch> --delivery-commit <source_commit>`, and the targeted `kander check <task-id>`. `<source_commit>` is the full SHA of the wrap-up evidence, never this card's own final commit: read it as `intent.evidence.wrap_up.git.source_commit` (equal to `intent.base`) in `kander dispatch show <task-id> <dispatch-id>`, or as `source_commit` in `kander show --document dispatches/<dispatch-id>/integration.json <task-id>`. A wrap-up completion takes no `--disposition`; the `integration.json` path is read-only evidence, not a `--disposition` argument. Report per the applicable `KANDER-REPORTING-RULES.md` template or the user's format. End the turn and keep the interactive CLI session waiting for the dismiss decision.
- The orchestrator does not modify the delivery, acceptance, and verification records written before the card moved into `review/`.
