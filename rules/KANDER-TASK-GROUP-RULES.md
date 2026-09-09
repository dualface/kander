# Task Group Orchestration Rules

Task group orchestration is enabled only when `rules.task_groups=true` and `rules.git=true`. See `KANDER-AGENTS.md` for precedence and the loading flow.

Task groups always use the `KANDER-GIT-RULES.md` rules; a custom group integration contract with Git disabled is not supported.

- First read the enabled `KANDER-GIT-RULES.md` and the command protocol `KANDER-KANBAN-RULES.md`. If the configuration does not satisfy the dependencies, keep the cards and workspaces and report which switches need adjusting.
- When there is no `origin` or the user explicitly asks for local-only, skip every push and remote branch cleanup in this file and report truthfully that the remote is not synced.
- The orchestration plan must spell out the steps for delivering task branches to the group branch and for merging each group back into `develop` in dependency order. Integration authorization follows `KANDER-GIT-RULES.md` "Integration and Cleanup"; when a confirmed plan explicitly includes these steps, do not ask again, and merely starting cards does not count as integration authorization. A plan confirmed through `KANDER-TASK-INTAKE-RULES.md` that names the group and its merge-back step is such a confirmed plan, whether the user chose to start immediately or to create the cards first and start later.
- The three clauses below apply to the whole file, whether or not review applies:
  - Every `notify` that dispatches a round in this file carries the `--kind` of that round (`sync`, `fix`, `wrap-up`) and, for `fix` and `wrap-up`, the `--evidence-file`; a `--message-file` alone is the legacy spelling and is not sufficient. Status messages and takeovers for `working/` cards follow `KANDER-KANBAN-RULES.md` "Failure Recovery", which distinguishes unbound cards (a plain message without `--kind`) from bound cards (same-ID handling, or a disposed round and a new dispatch). "Once" or "exactly once" in this file means one logical dispatch ID: same-ID reconciliation is allowed, exactly-once external side effects are not promised. Column transitions remain scheduling hints; verify the dispatch receipt before treating a bound round as accepted or complete.
  - Wherever this file requires a card to have entered `done/`, a card in `archived/` with `RESULT: completed` counts the same; wherever it requires a card to have reached `review/`, a card already in `done/` or in `archived/` with `RESULT: completed` counts the same.
  - Whenever this file says the executing agent "moves back to `working/`", it means the ID/epoch-bound move command from the generated dispatch prompt (`KANDER-KANBAN-RULES.md` "Durable Dispatch"), not a plain `kander move <task-id> working`; a plain move accepts no dispatch and authorizes no work.

- Review batches, dispatching findings back, and review gates apply only when `rules.review=true` or the user explicitly asks for a full review this time.

  Otherwise do not load `KANDER-REVIEW-RULES.md`; after implementation and verification, enter the group integration flow, which must still satisfy integration authorization and the project's delivery gates; record review as N/A, do not build a review base chain, and satisfy `KANDER-KANBAN-RULES.md` "Review Evidence Completion Gate" with the explicit N/A plan before any member completes.

- The fixed user-facing report format is read from `KANDER-REPORTING-RULES.md` only when `rules.reporting=true`. When disabled, report progress, deliveries, and unresolved items truthfully; in-card records and state gates still apply.
- External dependencies are satisfied only per "Dependencies Between Task Cards", including its `develop` containment check. External deliveries enter a group only through the group branch's creation anchor: the group branch is created after all external dependencies of all members are satisfied, so it never needs a rebase to pick them up.

## Task IDs

- A task ID is `YYYYMMDD-short-slug-task`, unique across the whole board; the file card's entry name without `.md`, or the directory card's directory name, is the task ID.
- `short-slug` consists of lowercase ASCII letters, digits, and the hyphens separating them; it does not start or end with a hyphen and contains no consecutive hyphens.

## Task Splitting and Task Groups

- Unless the user explicitly asks for a single card or for a task group, the agent decides which one to use; do not put the choice to the user as a question. Presenting the decision inside a plan for confirmation, as `KANDER-TASK-INTAKE-RULES.md` requires, is not such a question. Judge by the number of goals in the request: a single goal becomes a single card, even when the work is large; multiple goals become one card per goal. A goal is a result the user can accept on its own; a step that only makes sense as part of another goal is not a separate goal and does not get its own card. The only exception is the contract card in this section: a shared interface or data contract that must be delivered before its dependents may take its own card even though the user does not accept it on its own. Do not split a single goal to pad the count, and do not merge several goals into one card to save cards. State the choice and its reason in one sentence: in the plan when intake guidance is on, otherwise when reporting the created cards.
- Each card focuses on one goal and spells out `EXPECTED_OUTCOME` and `ACCEPTANCE_CRITERIA`.

- When the request is split into multiple task cards:
  - Cards form a task group only when they are related: one depends on another, or they share an integration contract that must be verified together. Goals with no dependency between them and no shared contract are created as independent single cards, not as a group, even when they come from the same request; a group therefore never holds unrelated goals.
  - A task group is generally recommended to have no more than 3 task cards.
  - If a task group exceeds 5 task cards, split it further into multiple task groups; how to cut the group still follows the goals and dependencies, not the count alone.
  - Precedence between the two guidelines above: the goal count decides how many cards exist; the counts only decide how the cards are grouped. Never merge two independently acceptable goals into one card to stay under 3, and never split one goal to reach a count. A group of 4 or 5 cards is acceptable only when its dependency chain is linear or its members share one integration contract; record the reason in the `DISCUSSION` of the first card in dependency order. A group of 4 or 5 cards that meets neither condition is split into multiple groups the same way as a group of more than 5.
  - A task group ID is `YYYYMMDD-short-slug-group`, unique across the whole board.
  - Each member card's `- TASK_GROUP:` metadata holds the ID of its group; non-members leave it empty. A task group is a relationship between cards and adds no board entry or state.
  - Member cards of one group share the same `LANGUAGE`; create them with the same `--language` or under the same configuration.

- Card count follows the goal count above, not size; when a goal is large, keep it as one card and choose SIZE per `KANDER-KANBAN-RULES.md` "Task Scale and Grouping". Clarify shared interfaces or data contracts first; when they need separate delivery, create a contract card as a prerequisite, which is the exception to the one-card-per-goal rule stated at the top of this section.
- When creating a group, list all members and the dependency graph, and rule out missing references, dependency cycles, and overlapping responsibilities; once in `todo/`, task group relationships are frozen per the kanban protocol. The legacy Chinese `任务组: ...` line recorded in `DISCUSSION` on old cards remains compatible as a literal marker; no bulk rewrite is required.
- After the member cards are created, complete `KANDER-KANBAN-RULES.md` "Post-Creation Self-Review" card by card (member cards belong to a group and must also obtain the `CARD_REVIEW:` record from an independent card review; the same independent agent may review all member cards in one session, reading only those cards and the user's original requirement), then check whether the group's members fully cover the goals assigned to that group, and the created cards together fully cover the user's request, whether the boundaries between cards overlap or leave gaps, and whether the prerequisite deliveries satisfy the constraints and acceptance of the later cards. Fix any problem found and re-check first; only advance to `todo/` after it passes. `kander pick <task-id>` and `kander move <task-id> todo` are the same transition (`KANDER-KANBAN-RULES.md` "Command Contract"); the orchestrator performs it in "Starting and Subscribing", or the creator performs it right after the checks when the user asked to start immediately.

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
- Out-of-group cards and all members expanded from group references must reach `done/`, or `archived/` with `RESULT: completed`, and their deliveries must be confirmed contained in the current `develop`. An `archived/` card with any other result, or a `trash/` card, never satisfies a dependency; hand the decision to the user. Non-group cards do not enable the automatic dependency resolution contract above; their delivery preconditions are verified by the executing agent against the task contract.
- Out-of-group references of any member block the whole group, not only the referencing card, because the group branch is created once from `develop` and is not rebased before final integration. When one late member would hold up the others for long, move that member into a successor group that depends on the external target instead of starting the group early. This regrouping is a planning decision made before the cards enter `todo/`; afterwards it is a frozen-relation change that needs an explicit user decision per `KANDER-KANBAN-RULES.md`.

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
- When the group branch has advanced earlier and the task branch cannot ff, the orchestrator calls `kander notify <task-id> --kind sync --message-file <sync requirements>` once per logical dispatch (see "Durable Dispatch Identity") to dispatch the original executing agent to rebase, resolve conflicts, and re-verify. The orchestrator does not modify that card's worktree or commits; if `notify` exits non-zero, stop and report. After the new delivery returns to `review/`, verify again; do not carry the old delivery's conclusion forward.
- Only after the orchestrator confirms that the card's latest delivery commit is in the actual local or remote group branch and the group worktree is synced to that head does it add the card to the pending review set or release its direct successor cards. If the push, ff, or verification fails, keep `review/` and the working state and do not release dependencies. If the group branch shows unexpected changes or divergence, report; do not overwrite the remote or rewrite reviewed history.
- Deliveries are received serially by the orchestrator without generating merge commits. When review applies, no batch opens before every member of the group has started, because closing a batch needs the review plan and the plan can be created only then; until that point deliveries are received freely in dependency order. Afterwards receive only while no batch is open; each batch, when opened, covers all deliveries received since the previous closed target up to this batch's HEAD, and no other change is mixed into the group branch while it runs, because closing it requires the worktree clean at its final target. Other `review/` cards queue until the current batch closes; in-batch fixes are received after the Reviewer exits and then re-reviewed, so that the review target matches the batch contract. When review does not apply, receive directly in dependency order.

**Topology Freeze**

- While any review batch of a group is open, the orchestrator does not re-split the group, add members, move cards between groups, change a card's owner, or accept a contract change. A user request for any of these first closes the open batch (its remaining required roles still run on the current target, and findings the pending change supersedes are disposed as `rejected` with the user's quoted decision and stay on the unresolved list; a batch in which a reviewer has run is never abandoned, because every planned batch must close before its members can complete), then applies the change, then starts a new batch in the same plan, which must still be unsealed and which reviews only the work after the closed target; re-reviewing the closed range under the new contract, a change that alters the base chain, or a change that arrives after the plan is sealed, continues as new cards per `KANDER-REVIEW-RULES.md` "Group-Level Review for Task Groups". Applying such a change mid-batch invalidates the batch and must be reported as wasted rounds, not silently absorbed. The review plan fixes the group's members and worktree when it is created: it cannot take a new member later, and it cannot follow a card into another group, so work discovered after that point becomes a successor group or an independent card, and a re-split after the plan exists means terminating the affected cards under the user's decision and creating new ones; report both as topology changes.

**Merge-Back and Cleanup Preconditions**

- Once all of a group's deliveries are in the group branch, all members have reached at least `review/`, and applicable reviews are complete, the orchestrator checks integration authorization per the Git rule file "Integration and Cleanup", rebases onto the latest `develop`, re-verifies, and merges back. Successor groups in the same orchestration are unlocked once every member of this prerequisite group has entered `done/` (or `archived/` with `RESULT: completed`) and its final group HEAD is confirmed in `develop`; do not wait for all groups to finish before integrating.
- The orchestrator records the group HEAD before and after integration and the mapping to each card's commit. When `develop` advances, the one-time review gate applies. The bound wrap-up evidence accepts the rebased group line only when its complete patch equals the closed reviewed patch, so the integration rebase must apply cleanly. After a rebase that applied cleanly, compare the complete patch of the reviewed range with the rebased range before pushing, normalizing only index lines and hunk offsets (the same comparison the wrap-up evidence applies); a differing patch, for example from changed context lines upstream, is handled exactly like a conflict. When any commit conflicts or the patch differs, abort the rebase, keep the reviewed group HEAD and the whole group state, and report to the user with numbered options: `1. Integrate the reviewed group HEAD unchanged through a PR or a user-authorized merge into develop, which keeps the reviewed patch intact`, `2. Stop integration and keep the group state`. The orchestrator does not resolve group-branch conflicts itself and does not dispatch executing agents to rewrite delivered group history.
- Before cleanup, confirm the final group changes have entered the actual local or remote `develop`: for direct integration, verify with `git merge-base --is-ancestor` using the post-rebase group HEAD; when the user or project requires a PR, use the PR merged criteria from the Git rule file. The wrap-up evidence still needs a Git mapping, and it binds the range from the first planned batch's base to the target recorded in the plan for the last batch (`KANDER-KANBAN-RULES.md` "Review Evidence Completion Gate"), which equals the closed final target only when no fix advanced the last batch after it was recorded: when `develop` contains the group history unchanged (fast-forward without rebase, or a merge commit), `source_commit` is that recorded target and the orchestrator verifies the closed final target's ancestry separately; when history was rewritten (the integration rebase followed by fast-forward, or a squash or rebase merge), `source_commit` is the last replayed commit on `develop` and `rebased_base` is the parent of the first replayed commit, and the evidence passes only when the complete patch of the replayed range equals the recorded range's patch; because that comparison covers only the recorded range, when the last batch advanced after being recorded, stop and report instead of wrapping up. Choose the PR merge method with that in mind. If not satisfied, keep the group and task working state and report.
- Once satisfied, dispatch the original executing agents to clean up their own card's working state per "Integration and Wrap-Up"; the group worktree and branches are removed per "Cleanup Failures" after all of the group's cards enter `done/`.

## Task Orchestration

**Entering Through a Member Card**

- A request to start a specific task card must first read that card's `TASK_GROUP` and `PREREQUISITES` before claiming it. When `TASK_GROUP` is non-empty, also read every member of that group and resolve the complete dependency graph; do not treat the requested card as a standalone card.
- Before starting the requested member, verify each prerequisite using "Dependencies Between Task Cards". Completion for this check means the state and Git containment required there: an in-group predecessor must have its latest delivery accepted into the group branch, while an out-of-group card or group must be complete in `develop`. A column name by itself is not sufficient evidence.
- Before assuming orchestration, inspect the durable coordinator record and any group working state. Reconcile an existing record under "Durable Coordinator Recovery". Another live coordinator remains the orchestrator; do not displace its claim, create a competing orchestration, or launch another copy of a member.
- If any prerequisite is not yet satisfied, or the group has no recorded group working state from "Creation and Reuse", the current agent automatically becomes the orchestrator once the coordinator claim permits it. Treat the start request as an instruction to start or continue the group orchestration, not as permission to bypass a dependency. Do not ask the user to choose another member or to restate the request.
- The new orchestrator follows "Starting and Subscribing": validate the whole group, wait for external dependencies, create or reuse the group working state, start ready predecessors first, then start their successors as deliveries release them. It continues through group review, authorized integration, wrap-up, and successor groups under the existing gates. It never starts the originally requested card before that card becomes ready.
- If the group already has a recorded working state and the requested card's prerequisites are satisfied, use the reconciled coordinator state before starting the card. Do not create a second group branch or launch the same member twice.

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
  - Use `kander move <task-id> todo` (or `kander pick`) to move confirmed `backlog` cards into `todo/`; the transition requires the `SELF_REVIEW:` and `CARD_REVIEW:` records from "Task Splitting and Task Groups". Cards already in later states stay as they are.

**Waiting for External Dependencies**

- While out-of-group dependencies are unmet, do not create the group branch or start cards; the whole not-yet-started group stays in `todo/`.
- Cards in later states during recovery keep their state.
- First tell the user the gap and the dependency targets, then wait with `kander subscribe <task-group> <all member task-ids>... --watch <out-of-group task-id|task-group-id>...`; `--watch` may be repeated per direct reference. `--watch` is used only for targets outside this orchestration; a prerequisite group owned by the same orchestrator releases its successor per "Merge-Back and Cleanup Preconditions" without a separate waiting subscription.
- After an out-of-group card reaches `done/` or all members of a referenced group reach `done/`, apply the `develop` containment check of "Dependencies Between Task Cards".
- When a watched card enters `archived/` with `RESULT: completed`, treat it as `done/` and run the same `develop` containment check. When it enters `archived/` with any other result, or `trash/`, stop waiting and hand the decision to the user; do not judge the dependency satisfied on your own.
- State events trigger targeted re-checks of external dependencies. With no `working/` members of this group, the 15-minute heartbeat only confirms the subscription is alive; when resuming orchestration with `working/` members of this group still present, check those members' `liveness` per "Handling State Changes" and do not take over out-of-group agents.
- Once all external dependencies are satisfied, stop this group's waiting subscription, create the group working state from the then-latest `develop` per "Creation and Reuse", record the creation anchor, and then start. When the same orchestrator also owns the prerequisite group, keep advancing it; a successor's waiting subscription for its other external targets does not stop that scheduling.
- With no external blocker, create the group working state per "Creation and Reuse" directly before starting the first card.

**Starting Ready Cards**

- Start each ready `todo` card with `kander start <task-id>`; the agent is taken from configuration by card scale, the launcher defaults to the Kander configuration, and `--launcher` overrides only this run.
- Use only one launcher within a task group; when the chosen launcher is unavailable on the current platform, report the blocker.
- In the first round, start cards with no in-group `PREREQUISITES` (whose out-of-group prerequisites have all been satisfied through the flow above); afterward, start only cards whose in-group prerequisite cards are all in `review/` or `done/` and whose latest deliveries have entered the current group branch.
- Start cards that are ready at the same time and have no resource conflicts in parallel; never start early by skipping dependencies.
- The task file for `start` is written by the command with the task ID and fixed requirements; the orchestrator does not append the card body or fabricate another task context.
- The executing agent learns that it follows the task group flow from the card's `TASK_GROUP` field and these rules.
- Immediately after the first card starts successfully, tell the user: this session is the task group orchestrator and must be kept until all task groups in this orchestration have run in dependency order; do not end the current session; ending early loses dependency validation, ordered starts, applicable group-level reviews, and integration. The orchestrator session lasts until this orchestration succeeds or the user explicitly terminates it.

- After starting the first round of cards, the orchestrator blocks reading the line-by-line JSON from `kander subscribe <task-group> <task-id>...`: first a `snapshot`, then a `state-change` per state change, containing the group ID, the states before and after the change, and a snapshot of the whole group.

  Use the initial snapshot to catch up on moves made before subscribing; do not rely on historical events.

**Handling State Changes**

- Consume versioned subscription facts as observations and feed each of them into the coordinator checkpoint per "Durable Coordinator Recovery". A `task-update` requires re-reading the affected delivery record even when its state is unchanged; revision is not a dispatch completion receipt. On restart, compare the new snapshot with saved task revisions and membership versions, not the process-local sequence number.
- A `membership-change` requires re-checking the complete dependency set. A removed or regrouped member never automatically satisfies its former obligation. When `reconciliation_required` is true, reconcile the contract and delivery before releasing dependencies.
- A terminal `membership-unknown` or `read-error`, or `membership_complete: false`, stops dependency release for the affected subscription. Preserve the last facts as history, report the diagnostic, and follow explicit recovery requirements; do not infer completion from omitted tasks or automatically repair the board.

- On receiving a `state-change`, the orchestrator runs `kander check <task-id>...` only on the changed card and the direct successor cards whose dependencies may be released by it entering `review`/`done`, reads those cards, and verifies dependencies. `review/` cards in the initial snapshot get the same delivery verification; do not skip steps because historical events were missed.
- A card entering `review/` is first received per "Delivering a Task Branch to the Group Branch"; only after success decide whether successor cards are ready. This delivery verification is still required when review is disabled.
- Newly ready cards are still started with `kander start` in the established order.
- A `review/` card whose delivery has been received enters the pending review set when review applies; otherwise it waits until the group integration gate is satisfied.
- A bound wrap-up closes only when the same dispatch ID/epoch has a completed receipt binding the final delivery and integration evidence, and the card is `done/` or already `archived/` with `RESULT: completed`. A snapshot can prove this after a fast round trip or restart; the column alone cannot.
- After a new member starts or a card is dispatched back via `notify`, restart the subscription with parameters naming the explicit set of members that still need monitoring, and continue judging from the new initial snapshot.

- The subscription emits a `heartbeat` every 15 minutes by default, independently of state changes. The interval restarts after each heartbeat is queued. Slow probes do not block scanning or heartbeat production; output backpressure beyond the bounded queue/write deadline terminates explicitly. Reconnect and reconcile the new snapshot after such an exit.

  The orchestrator reads the `liveness` carried by the event directly, checking revision/identity, observation age, validity and collection state. Pending, uncollected and stale observations remain `unknown`; `alive` proves presence only and does not extend confirmation deadlines or prove progress, `stopped` or `drifted` is handled per `KANDER-KANBAN-RULES.md` "Failure Recovery" (unbound and bound cards differ there), and `unknown` is reported as undeterminable together with the details.

  While handling heartbeats and state events, do not run a board-wide `kander check`, do not re-read unrelated cards, and do not patrol with capture-pane on your own; the single untargeted `kander check` in "Integration and Wrap-Up" runs only after every group has wrapped up.

  Agent messages or user input may trigger an extra liveness check of the same scope without changing the semantics of the next heartbeat.

- Between state events, keep blocking on the subscription output; adding short-period polling on your own is forbidden. Only a state event, a heartbeat, or an explicit failure/exit of the subscription process triggers handling; lack of output and long waits are not anomalies by themselves.

### Durable Coordinator Recovery

- Before recording group progress, use `kander coordinator claim <claim.json>` with the explicit full member set, a unique session token, owner and authority basis. Initial expected revision/epoch are zero. Recovery reads `coordinator show <group-id>` and CAS-claims its current revision/epoch with a new token only under existing coordination authority. Never reclaim repeatedly to override another live writer. The token fences checkpoint writes only; it grants no task takeover, integration, transport or cleanup authority.
- Read the committed checkpoint, current task revisions and original dispatches, then call `kander coordinator reconcile <observations.json>`. Supply the same full member set and each expected dispatch ID/epoch/base; completed rounds additionally name the exact receipt delivery SHA. Use this path for initial snapshots, task updates, state changes, heartbeat/attention events and subscriber restarts. Events wake the consumer; their process-local sequence and state strings are not completion evidence.
- A checkpoint preserves observed task revisions, execution cycles, membership version, deliveries, batch/run references and pending confirmation/delivery/wrap-up work. It is not a second task state or a review original. Same-request retries revalidate evidence and preserve the committed revision; stale epochs, revisions, wrong rounds, unknown members or damaged originals stop the affected reconciliation.
- Include waiting members in the full claim. Launcher metadata represents a start attempt until its durable success result exists; reconciliation keeps that member waiting and grants no delivery from the attempt alone. Consume controlled rollback evidence to restore waiting and to follow retries, including when a rollback snapshot was missed. Preserve attempt identities, task/checkpoint CAS and immutable history across coordinator restarts. Success confirmation preserves newer executor work; confirmed cycles cannot be silently replaced. Missing or corrupt attempt/result evidence stops recovery, and an unknown launcher outcome must not be promoted to success. Legacy cards and explicit manual claims retain their existing metadata checks.
- First deliveries without dispatch receipts require the recorded task branch, `review/`, an explicit delivery SHA and its actual branch HEAD in the supplied absolute `cwd`. This records a delivery only; group reception and dependency release still require their independent Git verification and existing authorization. Never invent a dispatch receipt for a legacy card.
- Reconcile EOF/output failure once from current facts and reopen the subscription once when committed facts are valid. Only a diagnosed managed pending transaction may enter the existing explicit `init` maintenance recovery, at most once before rereading; if it persists, report and preserve evidence. Do not retry duplicate entries, reparse points, corrupt records or unknown artifacts. Lock contention/deadline exhaustion is unavailable observation, not stopped execution; report and retain the cursor. No output by itself is not a failure and starts no recovery.
- Keep subscription heartbeat, dispatch confirmation deadline and task progress separate. Receipt acceptance ends confirmation waiting; it does not prove delivery. Other events do not renew a dispatch deadline. Compare saved task/dispatch revisions when a heartbeat or attention event arrives; an alive but unchanged member may need a progress report, never an automatic new executor or fabricated completion.
- During wrap-up recovery, reconcile the same dispatch and consume its bound integration artifact, all required role conclusions, immutable author dispositions, sealed plan and exact closed batch chain. Recheck actual Git ancestry and mechanical evidence in the recorded repository, or supply an absolute surviving worktree `cwd` when cleanup removed the old one. The evidence's commit identities remain unchanged. No-finding members need no notification merely to copy a report.
- Partly archived completed members retain their original references and completed receipts. Continue only unfinished work; do not reintegrate or repeat completed cleanup. A checkpoint does not grant on-behalf wrap-up: consume only the dedicated fenced `dispatch authorize-wrap-up` grant under the existing exception. Absent SESSION, uncertain delivery, nonzero status, expiry and alive observations remain insufficient. A changed member set or execution cycle requires explicit reconciliation of the governing contract; never silently drop members or reset an epoch to bypass evidence.

### Review Batches and Dispatch-Back

- Bind each role invocation to all batch members with repeated `--task` flags. Keep one batch ID across its fix rounds, distinct run IDs per role/invocation, and explicit predecessor run IDs for incremental re-review. Follow the minimal tool protocol for fixed requirements, CAS target advancement and same-run publication recovery.
- Before treating a role report as available, confirm all its card publications are complete. Execution success alone is not semantic PASS; failed or interrupted evidence and incomplete publication cannot close a batch.

This section runs only when review applies. A dispatch-back solely for task branch sync or integration conflicts is handled per "Group Integration Branch" and does not enable the review module.

- The orchestrator decides batch boundaries by choosing when to receive deliveries: a batch is opened at a point the orchestrator picks (a module, a milestone, or a dependency-chain step), never before every member has started (see "Group Integration Branch"), and it then covers every delivery received on the group branch since the previous closed target. Deliveries the orchestrator wants in a later batch stay queued in `review/` unreceived. Deferral never delays a successor: a delivery whose in-group successor is otherwise ready is received as soon as no batch is open, and it then belongs to the next batch. Neither whole-group nor one-card batches are mandated; the constraint is only that a received delivery is never left out of the batch that follows it.

  Determine the CWD, base, task context, and role flow per `KANDER-REVIEW-RULES.md` "Group-Level Review for Task Groups".

  Each batch is independent; a later batch's base is the closed target of the previous batch, and only in-batch fixes get incremental re-review. A batch in which a reviewer has run is never abandoned; it is closed per "Topology Freeze" before the next batch names it as predecessor.

- Default batching: at batch time, every delivery received on the group branch since the previous closed target joins the same batch. A batch with a single card arises only when that delivery was the only one received between the previous batch's closure and this batch's opening, for example when a dependency chain made the successor deliver only after that closure. Serial per-card batches for independent cards are forbidden; holding independent ready deliveries unreceived merely to review them one by one counts as such a serial batch.

- Findings are attributed by the orchestrator according to the cards' modification scopes: whichever card's `GOAL`/`OUT_OF_SCOPE`/actual changes a finding hits, that card gets it.

  Cross-card integration findings are assigned through `review assign` to every card of the batch whose modification scope they hit; a card outside the batch cannot be assigned, so when a finding also hits a card reviewed in an earlier batch, report that membership gap and plan that card's part as a later batch or a successor card. When no batch member's scope matches, the finding is beyond every member's contract: attribute it to the in-batch card whose delivery introduced the interaction (the later delivery in dependency order, or the later-received delivery when they are independent). Its executing agent disposes it as `rejected` beyond contract with a follow-up card suggested, unless it reaches `blocking` or `high`, which goes to the user per `KANDER-REVIEW-RULES.md` "Main Agent Verification Duty". If the user decides to include it, that is a contract change of that member: close the open batch per "Topology Freeze", apply the contract decision, then open a new batch in the still unsealed plan. The orchestrator never adds a fix card to a running group, because the review plan cannot take a new member; such work becomes a successor group or an independent card.

**Durable Dispatch Identity**

- Use `notify --kind fix --evidence-file <JSON>` for findings, `--kind sync` for task-branch synchronization, and `--kind wrap-up --evidence-file <JSON>` after integration. Bind the actual review run/finding/assignment and existing author originals for fix, and verified develop integration for wrap-up, per the command protocol. Record the printed dispatch ID, frozen baseline and original message; retries reuse the same ID and payload.
- Read `kander dispatch show <task-id> <dispatch-id>` to reconcile uncertainty. State changes and terminal echo do not replace accepted/completed receipts. Before working, the executing owner uses the ID/epoch move command from the generated prompt; a replayed receipt does not authorize duplicate work. Completion carries the same grant and final delivery/evidence references.
- When a notify returns nonzero, preserve the actual dispatch state. Do not invent a new round or separately invoke resume to escape uncertainty. Report the reason; a same-ID retry follows the command protocol's persisted deadline, readiness and stopped/unknown rules.

**Dispatching Findings Back**

- A dispatch-back calls `kander notify <task-id> --kind fix --evidence-file <JSON> --message-file <findings>` once per logical dispatch and checks the exit code; channel selection, recovery, and window/document rollback are handled inside the command. A dispatch-back carries the card's gate findings (`blocking`, `high`, `medium`, including `[mechanical]`) to fix, bound in the evidence, and its non-blocking findings (`low`, `recommend`, `suggest`) in the message for disposition only, each with role and tier, per `KANDER-REVIEW-RULES.md` "Preconditions and Execution"; a card with only non-blocking findings gets the `--kind sync` disposition-only dispatch described there. Non-blocking items never open a fix round, and a dispatch that lists no concrete items or asks the author to "triage" is forbidden.
- A non-zero exit means stop and report to the user.
- The file states the reviewer role, tier, the findings verbatim, and facts known to the orchestrator; it does not contain the orchestrator's own conclusions. It is written in the target card's `LANGUAGE`.
- On receiving the notice, the original executing agent first moves back to `working/` with the ID/epoch-bound move command from the dispatch prompt, then continues with context: verify each finding, fix, commit, rebase onto the group branch head and re-verify, update the task branch, submit the dispositions through `review disposition`, write in `IMPLEMENTATION` one entry with the counts per status, the card-relative record paths and the latest delivery SHA (never the finding list itself), then `move review` and end the current response turn.
- A dispatch remains pending confirmation until its immutable accepted or completed receipt is verified for the expected ID, epoch and base. The initial snapshot and every later event use the same reconciliation path. Missing a `review -> working` edge does not block a proven receipt; observing an edge or terminal echo does not prove it. Until the receipt exists, do not release dependencies, queue its fix delivery for review, or create another logical dispatch. Respect the original confirmation deadline and inspect attention/liveness facts without inferring exit from timeout.
- After reconciling the same round's completed receipt and final delivery, receive and sync the fix delivery per "Delivering a Task Branch to the Group Branch", then consume the author originals and trigger applicable incremental re-review. An already-contained delivery is verified and synced only; duplicate, lost or reordered events never create a new fix round.
- Findings the executing agent judges invalid or outside the contract return to the orchestrator together with the reasoning; the orchestrator must not rewrite them and includes them in the unresolved items for the user to re-check per `KANDER-REVIEW-RULES.md` "Main Agent Verification Duty".
- When a card dispatched back via `notify` has not entered `review/` and the executing agent has exited, or the same finding fails to close in two rounds, apply the round cap in `KANDER-REVIEW-RULES.md` "Conclusions and Failure Handling": record the current state and report to the user with numbered options per `KANDER-KANBAN-RULES.md` "Failure Recovery"; do not reassign on your own and do not let the orchestrator fix it on its behalf, and a user-decided takeover follows that section.

### Integration and Wrap-Up

- Once this group meets "Merge-Back and Cleanup Preconditions", the orchestrator completes the authorized integration and local sync per the Git rule file "Direct Integration and PRs"; record N/A when review is disabled.

  Do not merge back into `develop` while any card of this group is still in `working/` or has an unreceived delivery; this does not stop the orchestrator from receiving other ready cards' deliveries per "Group Integration Branch".

- When integration fails (rebase, verification, push, or ff incomplete) or the user asks to pause or not merge back: all in-group cards stay in `review/`, the group branch, task branches, and worktrees are kept, and the orchestrator records the blocker and the conditions for release and reports card by card per the enabled `KANDER-REPORTING-RULES.md` template (report truthfully when disabled), with the last line's state written as `review (blocked)`.

  Use `notify` to dispatch back only when there are genuine dispatch items (task branch sync, rebase conflicts, review findings, wrap-up after successful integration), and the original executing agent moves itself back to `working/`; the orchestrator must not move cards by hand to signal a blocker.

- After successful integration, dispatch the original agents to wrap up in dependency order; in parallel when there is no conflict.

  Use only `kander notify <task-id> --kind wrap-up --evidence-file <JSON> --base <source_commit> --message-file <wrap-up notice>` for the normal wrap-up dispatch, passing the evidence's `source_commit` as `--base` because the default base is the HEAD of the current directory; the only other entrance that creates a wrap-up intent is `dispatch authorize-wrap-up` under "Orchestrator Wrap-Up on Behalf", and a same-ID `resume --agent` follows the command protocol's user-authorization rule.

  The file states how the group branch was integrated into `develop` and the full SHA, this card's final commit (with the before/after mapping when a rebase rewrote it), role conclusions or N/A, applicable batches and fix rounds, this card's unresolved items, and the "Executing Agent Wrap-Up" checklist. For PR integration, also give the PR identifier, merged status, and target branch evidence.

  The orchestrator does not modify card bodies or move cards by hand.

  When dispatch is impossible, go through the gate in "Orchestrator Wrap-Up on Behalf"; impossibility alone grants nothing.

**Executing Agent Wrap-Up**

- An executing agent receiving a wrap-up notice first moves back to `working/` with the ID/epoch-bound move command from the dispatch prompt, then wraps up.
- The executing agent confirms per `KANDER-GIT-RULES.md` "Integration and Cleanup" that its card's changes are in `develop`: for direct integration, verify `git merge-base --is-ancestor` with the final group HEAD and confirm from the notice that this card's changes are included.
- Do not judge ancestry with the pre-rebase old SHA; for PRs, confirm with the merged criteria in the Git rule file "Integration and Cleanup" and the `source_commit`/`rebased_base` mapping given in the wrap-up notice.
- If not satisfied, preserve the working state and report to the orchestrator.
- Once satisfied, delete this card's worktree, local task branch, and applicable remote task branch, without deleting the group worktree or group branch; complete the summary's review and wrap-up parts (role conclusions or N/A, applicable rounds, final commit, group branch and `develop` integration results), publish records through the controlled update entrance, then run the completion command from the dispatch prompt, `kander move <task-id> done --result completed --dispatch-id <id> --execution-epoch <epoch> --delivery-commit <final-full-SHA>` with the applicable `--disposition`, and the targeted `kander check <task-id>`, report per the applicable `KANDER-REPORTING-RULES.md` template or the user's format, end the current response turn, and keep the interactive agent CLI session waiting for the user to decide whether to dismiss.
- The orchestrator does not modify the delivery, acceptance, and verification records written before the card moved into `review/`.

**Orchestrator Wrap-Up on Behalf**

- The exception below is subject to the durable wrap-up authority gate. First reconcile the same dispatch; create a bound wrap-up intent when absent. Apply `kander dispatch authorize-wrap-up <request.json>` only after confirmed executor exit or an already-authorized reclaim followed by a valid stopped observation. A missing SESSION/WINDOW, a nonzero notify result, timeout or expired lease alone does not grant authority. Preserve the card and report when exit cannot be established.
- Keep the original OWNER and author conclusions. Record the actual on-behalf author/reason in the dedicated grant, accept its new epoch, and use only its cleanup/append-only record scope. Do not send a wrap-up-only token to another executor or upgrade it to ordinary code authority. Complete with the same dispatch and verified integration source; original completion and review gates remain in force.


- When the wrap-up does not close, the orchestrator may complete the full wrap-up of that card and report as the one who finished it, but only after the authority gate above has granted it. The situations that lead to the gate are:
  - The card has no usable `WINDOW`/`SESSION` record.
  - `notify` exited non-zero.
  - The executing agent exited after the dispatch-back and the card still has not entered `done/`.

  None of these is sufficient on its own. Each is a trigger to reconcile the dispatch and establish the executor's exit; the grant comes only from `kander dispatch authorize-wrap-up` once exit is confirmed. When exit cannot be established, preserve the card in its actual state and report; do not wrap up on behalf.

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

  When any card enters `archived/` with a result other than `completed`, or `trash/`, wait for the user to change the group contract or terminate the whole group.

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

## Machine Aggregation and Author Boundaries

Whether or not review applies, the orchestrator establishes the group's execution review plan
once every member is in `working/` or `review/` and before any batch runs, per
`KANDER-KANBAN-RULES.md` "Review Evidence Completion Gate"; without review it is the explicit
N/A plan, created before any member wraps up. Resolve role applicability through the existing
rules, recording explicit N/A reasons and rule bases. Use an unsealed plan for progressive
batching; append each later batch with the expected plan revision after the previous batch has
closed and before this batch's first run, then seal before wrap-up. Keep the fixed cycle
membership and existing batch evidence.

Attribute findings with the controlled assignment command. Shared findings name all affected
cards. Dispatch actual findings to their original executing owners. Each owner submits its own
immutable disposition originals and revisions. The orchestrator references these records and
may separately record its own verification opinion with its own author identity; it never
rewrites or impersonates execution-side conclusions.

Use `kander review aggregate <CWD> <batch-id>` to mechanically publish the complete disposition
to all members. A member without findings is not dispatched solely to copy a report. Aggregation
is not author verification and does not decide acceptance on an executing owner's behalf.

Receive in-batch fixes, CAS the same batch target, and perform required incremental reviews.
Mechanical-only fixes can advance the target without a new reviewer using the controlled
advance command, preserving the existing mechanical evidence rules. Close the batch only through
the review closure command with every required role, author disposition and final Git relation
satisfied. Release the next batch only from that exact closed target. During wrap-up, verify the
sealed plan and all closures in addition to actual develop integration; preserve unresolved
items and all original author records.

- Reclaiming a planned card may change its execution cycle; when `review progress` reports
  `requirements-needed`, rebind the plan per `KANDER-KANBAN-RULES.md` "Review Evidence Completion
  Gate" before continuing.
