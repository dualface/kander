# Task Group Orchestration Rules

Task group orchestration is enabled only when `rules.task_groups=true` and `rules.git=true`. See `KANDER-AGENTS.md` for precedence and the loading flow.

Task groups always use the `KANDER-GIT-RULES.md` rules; a custom group integration contract with Git disabled is not supported.

- First read the enabled `KANDER-GIT-RULES.md` and the command protocol `KANDER-KANBAN-RULES.md`. If the configuration does not satisfy the dependencies, keep the cards and workspaces and report which switches need adjusting.
- When there is no `origin` or the user explicitly asks for local-only, skip every push and remote branch cleanup in this file and report truthfully that the remote is not synced.
- The orchestration plan must spell out the steps for delivering task branches to the group branch and for merging each group back into `develop` in dependency order. Integration authorization follows `KANDER-GIT-RULES.md` "Integration and Cleanup"; when a confirmed plan explicitly includes these steps, do not ask again, and merely starting cards does not count as integration authorization.

- Review batches, dispatching findings back, and review gates apply only when `rules.review=true` or the user explicitly asks for a full review this time.

  Otherwise do not load `KANDER-REVIEW-RULES.md`; after implementation and verification, enter the group integration flow, which must still satisfy integration authorization and the project's delivery gates; record review as N/A and do not build a review base chain.

- The fixed user-facing report format is read from `KANDER-REPORTING-RULES.md` only when `rules.reporting=true`. When disabled, report progress, deliveries, and unresolved items truthfully; in-card records and state gates still apply.
- After an external dependency reaches done, also confirm that its delivery is actually usable on the current group branch; a single card completed through the user's own Git flow does not guarantee its changes are in develop. When this cannot be confirmed, report the actual gap and do not merge it back on its behalf.

## Task IDs

- A task ID is `YYYYMMDD-short-slug-task`, unique across the whole board; the file card's entry name without `.md`, or the directory card's directory name, is the task ID.
- `short-slug` consists of lowercase ASCII letters, digits, and the hyphens separating them; it does not start or end with a hyphen and contains no consecutive hyphens.

## Task Splitting and Task Groups

- Unless the user explicitly asks to keep a single card, split the task into small cards that can be accepted independently as far as possible. Even when there is only one overall goal, check whether independently acceptable sub-goals can be split out; stop splitting when independent acceptance is no longer possible, and do not force splits to pad the count.
- Each card focuses on one small, independently acceptable goal and spells out `EXPECTED_OUTCOME` and `ACCEPTANCE_CRITERIA`.

- When a goal is split into multiple task cards:
  - These task cards form a task group.
  - A task group is generally recommended to have no more than 3 task cards.
  - If a task group exceeds 5 task cards, try to split it further into multiple task groups; the counts are suggestions and do not replace judgment about goals and dependencies.
  - A task group ID is `YYYYMMDD-short-slug-group`, unique across the whole board.
  - Each member card's `- TASK_GROUP:` metadata holds the ID of its group; non-members leave it empty. A task group is a relationship between cards and adds no board entry or state.

- Prefer splitting into small cards; when no further independently acceptable small cards can be split out and a large card is truly needed, choose the form per `KANDER-KANBAN-RULES.md` "Task Scale and Grouping". Clarify shared interfaces or data contracts first; when they need separate delivery, create a contract card as a prerequisite.
- When creating a group, list all members and the dependency graph, and rule out missing references, dependency cycles, and overlapping responsibilities; once in `todo/`, task group relationships are frozen per the kanban protocol. The `任务组: ...` line recorded in `DISCUSSION` on old cards remains compatible; no bulk rewrite is required.
- After the member cards are created, complete `KANDER-KANBAN-RULES.md` "Post-Creation Self-Review" card by card (member cards belong to a group and must also obtain the `CARD_REVIEW:` record from an independent card review; the same independent agent may review the whole group at once), then check whether the whole group fully covers the user's goal, whether the boundaries between cards overlap or leave gaps, and whether the prerequisite deliveries satisfy the constraints and acceptance of the later cards. Fix any problem found and re-check first; only advance to `todo/` after it passes.

## Dependencies Between Task Cards

- An in-group card's dependencies on task cards or task groups are written in a standalone code block at the start of that card's `DISCUSSION`. The field name must be `PREREQUISITES`, with IDs separated by ASCII commas; card IDs and group IDs may be mixed:

```text
PREREQUISITES: 20260905-contract-task,20260905-foundation-group
```

- When there are no dependencies, use the same position and code block format:

```text
PREREQUISITES: N/A
```

- An old card missing this line is treated as having no dependencies. A group reference expands to all current members of that group; referencing the card's own group or forming a cross-group dependency cycle is not allowed.
- An in-group prerequisite card satisfies the dependency only when it has reached `review/` or `done/` and the orchestrator has confirmed that its latest delivery commit is contained in the current group branch; the `review/` state alone does not release it.
- Out-of-group cards and all members expanded from group references must reach `done/`, and their deliveries must be confirmed usable on the current `develop`. Non-group cards do not enable the automatic dependency resolution contract above; their delivery preconditions are verified by the executing agent against the task contract.

## Running Task Cards in Parallel

- If task cards can be run in parallel, note it explicitly.
- Run in parallel only cards that are ready at the same time and whose modified resources can be isolated; when the same resource cannot be isolated, record the dependency and run serially, and never share an execution worktree.

## Git Branches and Worktrees

- For a single card, the executing agent creates a dedicated task branch and a `<repo-root>/worktrees/<task-name>/` worktree per the Git rule file, with `develop` as the source branch.
- A task group uses two layers of isolation: the orchestrator holds the group branch `group/<task-group-id>` and the dedicated worktree `<repo-root>/worktrees/<task-group-id>/`, used to receive deliveries, group-level verification, applicable reviews, and final integration; each member card's executing agent still creates its own task branch and worktree.
- In-group task branches are created from the latest group branch, with `group/<task-group-id>` as the source branch, not from `develop`. On the remote path, fetch `origin/group/<task-group-id>` first; other branch naming, paths, and user change protection requirements follow the Git rule file.
- The executing agent changes only its own card's worktree; the orchestrator operates only on the group worktree and the integration target. The executing agent's rebases and conflict fixes are done in its own card's workspace; the orchestrator never modifies task branches or code on its behalf.

## Group Integration Branch

**Creation and Reuse**

- The orchestrator first confirms the whole group's external dependencies per "Starting and Subscribing" and initializes `main` and `develop` per the Git rule file. Once all external dependencies are satisfied, create the group branch and group worktree before starting the first card.
- When `origin` exists and it is not local-only, fetch, create from the latest `origin/develop`, and push normally: `git push -u origin group/<task-group-id>`. Otherwise create from the latest local `develop` and report that the remote is not synced. If the fetch or creation fails, preserve the working state and do not start member cards.
- Record the group branch, the group worktree path, and the full `develop` SHA used for creation. That SHA is the creation anchor and doubles as the first batch's review base only when review applies. When resuming orchestration, verify and reuse the existing group working state and records; do not overwrite the branch or regenerate the anchor.
- Do not rebase the group branch before final integration, and do not rewrite delivered group history. The base chain for applicable review batches is maintained per the review rule file.

**Delivering a Task Branch to the Group Branch**

- After implementing, verifying, and committing by concern, the executing agent rebases its card's task branch onto the latest group branch, re-verifies, and updates the task branch per the Git rule file. Record in `IMPLEMENTATION` the task branch's final full SHA, the group branch SHA it is based on, and the verification result, then `move review` and end the current response turn. The executing agent never updates the group branch.
- On receiving the `review/` state, the orchestrator reads the delivery record and verifies integration authorization and that the task branch head matches the recorded final commit. On the remote path, fetch first and verify the remote branch; on the local path, verify the local branch. Deliver that commit to the group branch per the Git rule file "Direct Integration and PRs" and sync the group worktree; on the remote path, push the final commit first and only ff the local group branch after success. When the same delivery commit is already contained, only verify and sync; do not integrate again. Without authorization, keep `review/` and the working state, report, and do not release dependencies.
- When the group branch has advanced earlier and the task branch cannot ff, the orchestrator calls `kander notify <task-id> --message-file <sync requirements>` exactly once to dispatch the original executing agent to rebase, resolve conflicts, and re-verify. The orchestrator does not modify that card's worktree or commits; if `notify` exits non-zero, stop and report. After the new delivery returns to `review/`, verify again; do not carry the old delivery's conclusion forward.
- Only after the orchestrator confirms that the card's latest delivery commit is in the actual local or remote group branch and the group worktree is synced to that head does it add the card to the pending review set or release its direct successor cards. If the push, ff, or verification fails, keep `review/` and the working state and do not release dependencies. If the group branch shows unexpected changes or divergence, report; do not overwrite the remote or rewrite reviewed history.
- Deliveries are received serially by the orchestrator without generating merge commits. When review applies, arrange the batch before receiving deliveries; each batch covers all unreviewed deliveries from its base to this batch's HEAD, and out-of-batch changes are not mixed into the group branch. Other `review/` cards queue until the current batch completes; in-batch fixes are received after the Reviewer exits and then re-reviewed, so that the review target matches the batch contract. When review does not apply, receive directly in dependency order.

**Merge-Back and Cleanup Preconditions**

- Once all of a group's deliveries are in the group branch, all members have reached `review/`, and applicable reviews are complete, the orchestrator checks integration authorization per the Git rule file "Integration and Cleanup", rebases onto the latest `develop`, re-verifies, and merges back. Successor groups in the same orchestration are unlocked by this prerequisite group's `done/` and actual delivery; do not wait for all groups to finish before integrating.
- The orchestrator records the group HEAD before and after integration and the mapping to each card's commit. When `develop` advances, the one-time review gate applies; substantive code conflicts are dispatched to the original executing agent via `notify`, the orchestrator does not fix them on its behalf, and the fixed result is re-reviewed per the applicable review.
- Before cleanup, confirm the final group changes have entered the actual local or remote `develop`: for direct integration, verify with `git merge-base --is-ancestor` using the post-rebase group HEAD; when the user or project requires a PR, use the PR merged criteria from the Git rule file. If not satisfied, keep the group and task working state and report.
- Once satisfied, dispatch the original executing agents to clean up their own card's working state per "Integration and Wrap-Up". After all of the group's cards enter `done/`, the orchestrator removes the group worktree, the local group branch, and the applicable remote group branch; on failure, keep only the working state that still remains and do not roll back completed cards.

## Task Orchestration

- For a task with only a single task card:

  - Starting, manual claiming, and the tracking responsibilities of different launchers follow `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination".
  - The agent running the task card is called the executing agent; it is responsible for verifying delivery preconditions, creating the dedicated worktree, implementing, verifying, running applicable reviews, and integrating back into the source branch under obtained authorization. Without integration authorization or with an unmet delivery gate, keep `working/`, record the pending items, and report.

- For a task group:

  - The current agent becomes the orchestrator agent, schedules all groups and cards by dependency, integrates and wraps up each group under obtained authorization once its gates are satisfied, then unlocks successor groups.
  - After the entire orchestration completes, summarize per the unified gate in "Integration and Wrap-Up" and ask whether to dismiss the executing agents.

### Roles and Responsibilities in a Task Group

- The orchestrator agent is responsible for:

  - Validating dependencies
  - Creating and cleaning up the group branch and group worktree
  - Fast-forwarding executing agents' task branch deliveries onto the group branch and releasing dependencies after verification
  - Subscribing to notifications
  - Arranging reviews
  - Summarizing review results
  - Dispatching findings back
  - Group integration
  - Wrap-up

- The orchestrator agent does not:

  - Implement subtasks
  - Modify subtask code, worktrees, or commits; "Orchestrator Wrap-Up on Behalf" allows cleanup and records only
  - Monitor other agents' output

- The executing agent of an in-group card is responsible for:

  - Preparing its card's workspace, implementing, verifying, committing by concern, and rebasing, re-verifying, and updating the task branch per "Delivering a Task Branch to the Group Branch".
  - Writing the delivery, acceptance, and verification parts of `IMPLEMENTATION` and `SUMMARY`, and filling in `TASK_BRANCH` and the final delivery SHA.
  - Running `kander move <task-id> review`, then ending the current response turn, keeping the interactive agent CLI session, and not exiting the process or closing the terminal container on its own. "Exit" in the task file is interpreted the same way; when a non-interactive invocation ends naturally, later recovery is left to `notify`.
  - Waiting for `notify`:
    - If a wrap-up notice arrives, complete the card's wrap-up, report, end the current response turn, and keep the interactive CLI session.
    - Otherwise act on the notice's instructions.

- The executing agent of an in-group card does not:

  - Trigger reviews
  - Update the group branch or merge the group branch back into `develop`
  - Clean up its card's worktree and task branch before receiving the wrap-up notice

### Starting and Subscribing

- Before starting, the orchestrator reads all of the group's cards:
  - Verify IDs, dependencies, contracts, and modification scopes, parse every card's full `PREREQUISITES`, and classify the direct references into in-group cards, out-of-group cards, and out-of-group task groups.
  - Run `kander check <all member task-ids>...` as a targeted check of references and dependency cycles. Missing cards, missing group references, dependency cycles, or resource conflicts that cannot be isolated block the affected cards from starting; preserve the working state and report.
  - Existing environment problems that do not affect start conditions are only recorded and need not be fixed first. When the agent, launcher, configuration dependencies, board, or required Git workspace is unavailable, still block and do not bypass the `start` pre-checks.
  - Use `kander move <task-id> todo` to move confirmed `backlog` cards into `todo/`; cards already in later states stay as they are.

**Waiting for External Dependencies**

- While out-of-group dependencies are unmet, do not create the group branch or start cards; the whole not-yet-started group stays in `todo/`.
- Cards in later states during recovery keep their state.
- First tell the user the gap and the dependency targets, then wait with `kander subscribe <task-group> <all member task-ids>... --watch <out-of-group task-id|task-group-id>...`; `--watch` may be repeated per direct reference.
- After an out-of-group card reaches `done/` or all members of a referenced group reach `done/`, re-check that the delivery is actually usable on `develop`.
- When a watched card enters `archived/` or `trash/`, stop waiting and hand the decision to the user; do not judge the dependency satisfied on your own.
- State events trigger targeted re-checks of external dependencies. With no `working/` members of this group, the 15-minute heartbeat only confirms the subscription is alive; when resuming orchestration with `working/` members of this group still present, check those members' `liveness` per "Handling State Changes" and do not take over out-of-group agents.
- Once all external dependencies are satisfied, stop this group's waiting subscription, create the group working state from the then-latest `develop` per "Creation and Reuse", record the creation anchor, and then start. When the same orchestrator also owns the prerequisite group, keep advancing the prerequisite group; do not stop its scheduling because of the successor group's waiting subscription.
- With no external blocker, run this directly before starting the first card.

**Starting Ready Cards**

- Start each ready `todo` card with `kander start <task-id>`; the agent is taken from configuration by card scale, the launcher defaults to the Kander configuration, and `--launcher` overrides only this run.
- Use only one launcher within a task group; when the chosen launcher is unavailable on the current platform, report the blocker.
- In the first round, start cards with no in-group `PREREQUISITES` (whose out-of-group prerequisites have all been satisfied through the flow above); afterward, start only cards whose in-group prerequisite cards are all in `review/` or `done/` and whose latest deliveries have entered the current group branch.
- Start cards that are ready at the same time and have no resource conflicts in parallel; never start early by skipping dependencies.
- The task file for `start` is written by the command with the task ID and fixed requirements; the orchestrator does not append the card body or fabricate another task context.
- The executing agent learns that it follows the task group flow from the card's `TASK_GROUP` field and these rules.
- Immediately after the first card starts successfully, tell the user: this session is the task group orchestrator and must be kept until all task groups in this orchestration have run in dependency order; do not end the current session; ending early loses dependency validation, ordered starts, applicable group-level reviews, and integration. The orchestrator session lasts until this orchestration succeeds or the user explicitly terminates it.

- After starting the first batch, the orchestrator blocks reading the line-by-line JSON from `kander subscribe <task-group> <task-id>...`: first a `snapshot`, then a `state-change` per state change, containing the group ID, the states before and after the change, and a snapshot of the whole group.

  Use the initial snapshot to catch up on moves made before subscribing; do not rely on historical events.

**Handling State Changes**

- On receiving a `state-change`, the orchestrator runs `kander check <task-id>...` only on the changed card and the direct successor cards whose dependencies may be released by it entering `review`/`done`, reads those cards, and verifies dependencies. `review/` cards in the initial snapshot get the same delivery verification; do not skip steps because historical events were missed.
- A card entering `review/` is first received per "Delivering a Task Branch to the Group Branch"; only after success decide whether successor cards are ready. This delivery verification is still required when review is disabled.
- Newly ready cards are still started with `kander start` in the established order.
- A `review/` card whose delivery has been received enters the pending review set when review applies; otherwise it waits until the group integration gate is satisfied.
- A card that has been dispatched for wrap-up closes its wrap-up loop when it enters `done/`.
- After a new member starts or a card is dispatched back via `notify`, restart the subscription with parameters naming the explicit set of members that still need monitoring, and continue judging from the new initial snapshot.

- When the subscription sees no state change for 15 minutes, it emits a `heartbeat`.

  The orchestrator reads the `liveness` carried by the event directly: `alive` keeps waiting, `stopped` or `drifted` is handled per "Failure Recovery", and `unknown` is reported as undeterminable together with the details.

  Do not run a board-wide `kander check`, do not re-read unrelated cards, and do not patrol with capture-pane on your own.

  Agent messages or user input may trigger an extra liveness check of the same scope without changing the semantics of the next heartbeat.

- Between state events, keep blocking on the subscription output; adding short-period polling on your own is forbidden. Only a state event, a heartbeat, or an explicit failure/exit of the subscription process triggers handling; lack of output and long waits are not anomalies by themselves.

### Review Batches and Dispatch-Back

This section runs only when review applies. A dispatch-back solely for task branch sync or integration conflicts is handled per "Group Integration Branch" and does not enable the review module.

- The orchestrator batches `review/` cards by module, milestone, or dependency chain, receives the batch's deliveries per "Delivering a Task Branch to the Group Branch", and then reviews; neither whole-group nor one-card batches are mandated.

  Determine the CWD, base, task context, and role flow per `KANDER-REVIEW-RULES.md` "Group-Level Review for Task Groups".

  Each batch is independent; a later batch's base is the commit at which the previous batch completed review, and only in-batch fixes get incremental re-review.

- Findings are attributed by the orchestrator according to the cards' modification scopes: whichever card's `GOAL`/`OUT_OF_SCOPE`/actual changes a finding hits, that card gets it.

  Cross-card integration findings go to the card whose modification scope they hit; when none matches, the orchestrator creates a small fix card and adds it to this group (fill in `TASK_GROUP` and `PREREQUISITES`, then `pick` and `start`).

**Dispatching Findings Back**

- A dispatch-back calls `kander notify <task-id> --message-file <findings>` exactly once and checks the exit code; channel selection, recovery, and window/document rollback are handled inside the command.
- A non-zero exit means stop and report to the user.
- The file states the reviewer role, tier, the findings verbatim, and facts known to the orchestrator; it does not contain the orchestrator's own conclusions.
- On receiving the notice, the original executing agent first runs `kander move <task-id> working` itself to move back to `working/`, then continues with context: verify each finding, fix, commit, rebase onto the group branch head and re-verify, update the task branch, write in `IMPLEMENTATION` the previous round's finding list, handling conclusions, and the latest delivery SHA, then `move review` and end the current response turn.
- From a successful `notify` return until the card's `review -> working` `state-change` is observed, the card is "dispatched, pending confirmation": the orchestrator does not add it to the pending review set, release its dependencies, or dispatch again; if it still has not moved after a reasonable wait, handle per "Failure Recovery".
- On receiving the `state-change`, the orchestrator first receives and syncs the fix delivery per "Delivering a Task Branch to the Group Branch", then aggregates each card's list into the review context and triggers the incremental re-review.
- Findings the executing agent judges invalid or outside the contract return to the orchestrator together with the reasoning; the orchestrator must not rewrite them and includes them in the unresolved items for the user to re-check per `KANDER-REVIEW-RULES.md` "Main Agent Verification Duty".
- When a card dispatched back via `notify` has not entered `review/` and the executing agent has exited, or the same finding fails to close in two rounds, record the current state and report to the user per "Failure Recovery"; do not reassign and do not let the orchestrator fix it on its behalf.

### Integration and Wrap-Up

- Once this group meets "Merge-Back and Cleanup Preconditions", the orchestrator completes the authorized integration and local sync per the Git rule file "Direct Integration and PRs"; record N/A when review is disabled.

  Do not merge back into `develop` while any card of this group is still in `working/` or has an unreceived delivery; this does not stop the orchestrator from receiving other ready cards' deliveries per "Group Integration Branch".

- When integration fails (rebase, verification, push, or ff incomplete) or the user asks to pause or not merge back: all in-group cards stay in `review/`, the group branch, task branches, and worktrees are kept, and the orchestrator records the blocker and the conditions for release and reports card by card per the enabled `KANDER-REPORTING-RULES.md` template (report truthfully when disabled), with the last line's state written as `review (blocked)`.

  Use `notify` to dispatch back only when there are genuine dispatch items (task branch sync, rebase conflicts, review findings, wrap-up after successful integration), and the original executing agent moves itself back to `working/`; the orchestrator must not move cards by hand to signal a blocker.

- After successful integration, dispatch the original agents to wrap up in dependency order; in parallel when there is no conflict.

  Use only `kander notify <task-id> --message-file <wrap-up notice>`.

  The file states how the group branch was integrated into `develop` and the full SHA, this card's final commit (with the before/after mapping when a rebase rewrote it), role conclusions or N/A, applicable batches and fix rounds, this card's unresolved items, and the "Executing Agent Wrap-Up" checklist. For PR integration, also give the PR identifier, merged status, and target branch evidence.

  The orchestrator does not modify card bodies or move cards by hand.

  When dispatch is impossible, apply the on-behalf exception.

**Executing Agent Wrap-Up**

- An executing agent receiving a wrap-up notice first runs `kander move <task-id> working` itself to move back to `working/`, then wraps up.
- The executing agent confirms per `KANDER-GIT-RULES.md` "Integration and Cleanup" that its card's changes are in `develop`: for direct integration, verify `git merge-base --is-ancestor` with the final group HEAD and confirm from the notice that this card's changes are included.
- Do not judge ancestry with the pre-rebase old SHA; for PRs, confirm with the merged criteria in the Git rule file "Integration and Cleanup".
- If not satisfied, preserve the working state and report to the orchestrator.
- Once satisfied, delete this card's worktree, local task branch, and applicable remote task branch, without deleting the group worktree or group branch; complete the summary's review and wrap-up parts (role conclusions or N/A, applicable rounds, final commit, group branch and `develop` integration results), fill in `RESULT: completed`, run `kander move <task-id> done`, report per the applicable `KANDER-REPORTING-RULES.md` template or the user's format, end the current response turn, and keep the interactive agent CLI session waiting for the user to decide whether to dismiss.
- The orchestrator does not modify the delivery, acceptance, and verification records written before the card moved into `review/`.

**Orchestrator Wrap-Up on Behalf**

- When dispatch is impossible or the wrap-up does not close, the orchestrator completes the full wrap-up of that card and reports as the one who finished it. Applicable conditions:
  - The card has no usable `WINDOW`/`SESSION` record.
  - `notify` exited non-zero.
  - The executing agent exited after the dispatch-back and the card still has not entered `done/`.

  State "wrap-up done by the orchestrator on behalf" and the reason in the `Wrap-Up` part of the card's completion report and in the group-level summary.

- This is the explicit exception to "not fixed by the orchestrator on its behalf" in "Review Batches and Dispatch-Back", and applies only to wrap-up: wrap-up changes no code, it only cleans up and records.
- Findings that require code changes still must not be fixed by the orchestrator on its behalf.

**Cleanup Failures**

- When cleanup fails partway (a card's branch or worktree deletion fails): cards that have finished wrap-up and entered `done/` stay in `done/` and are not rolled back.
- The failed card stops in its actual state at that time (`working/` if wrap-up was dispatched, `review/` if not), keeping only the worktree and branch that still remain; the group branch is kept.
- Report card by card by real state; for the failed card, state the failed step, the error, and the conditions for release, with the state written as `working (blocked)` or `review (blocked)`.
- The code is already in `develop`, so do not re-integrate; after release, only continue the unfinished wrap-up.

- After all members of a group enter `done/`, stop that group's subscription, delete the group worktree, local group branch, and applicable remote group branch, and continue scheduling the groups in this orchestration that are not yet complete. After all groups in this orchestration finish wrap-up, run `kander check` once without targets.

  Success counts only when the full check passes.

  When any card enters `archived/` or `trash/`, wait for the user to change the group contract or terminate the whole group.

- At the end of orchestration, summarize the execution order, parallelism, applicable review batches and rounds or N/A, and integration results, then list per card the known defects, verification gaps, and follow-up tasks. Only when review applies, additionally classify and record unresolved review items per `KANDER-REVIEW-RULES.md` "Conclusions and Failure Handling"; do not load the disabled review module for this.

  Write "None" when there are no unresolved items; give the task ID separately for each item.

  On termination, issue the reports for unfinished cards one by one per the enabled `KANDER-REPORTING-RULES.md` template (report truthfully when disabled), then list the termination decision.

- Only when all in-group cards of this orchestration have entered `done/`, all group worktrees and group branches are deleted, `kander check` has passed, and all group-level summaries are complete does the orchestrator ask the user once whether to exit the executing agents responsible for these task cards and close the corresponding herdr tabs or tmux windows. Use numbered options, such as `1. Dismiss the executing agents and close the corresponding containers`, `2. Keep the sessions and containers`; a text reply that clearly expresses the same intent is also valid.

  Without explicit confirmation, perform no exit or close action.

  If the user declines, keep all sessions and terminal containers, and the orchestrator ends this orchestration.

- After the user confirms, the orchestrator calls `kander dismiss <task-id>` exactly once for each in-group task card; it does not send exit instructions to agents directly, does not close tabs/windows directly, and does not bypass the command's identity, topology, and graceful exit gates.

  Each card's dismissal result is independent: when a card returns non-zero, keep its working state and record the reason, and continue with the remaining in-group cards.

  After all attempts finish, summarize the results per card.

  This is optional terminal cleanup after the task group completes; failure does not roll back completed cards, does not restore deleted branches or worktrees, and does not change the completion reports and group-level conclusions already issued.
