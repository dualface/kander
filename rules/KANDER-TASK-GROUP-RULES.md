# Task Group Orchestration Rules

Task group orchestration is enabled only when `rules.task_groups=true` and `rules.git=true`; task groups always use `KANDER-GIT-RULES.md`, and a custom group integration contract with Git disabled is not supported.

- First read the enabled `KANDER-GIT-RULES.md` and the command protocol `KANDER-KANBAN-RULES.md`. If the configuration does not satisfy the dependencies, keep the cards and workspaces and report which switches need adjusting.
- Without `origin`, or when the user asks for local-only, skip every push and remote branch cleanup in this file and in its phase snippets (`snippets/execute-group-member.md`, `snippets/orchestrate-start.md`, `snippets/orchestrate-review.md`, `snippets/orchestrate-integrate.md`, `snippets/self-executed-group.md`, `snippets/fix-round.md`, `snippets/wrap-up.md`) and report that the remote is not synced.
- The orchestration plan must spell out the steps for delivering task branches to the group branch and for merging each group back into `develop` in dependency order. Integration authorization follows `snippets/git-integrate.md` "Integration and Cleanup": a plan confirmed through `KANDER-TASK-INTAKE-RULES.md` that names the group and its merge-back step is such authorization, whether the cards start immediately or later; merely starting cards is not. Never ask again when authorization exists.
- Three clauses apply to the whole file and to its phase snippets, whether or not review applies:
  - Every `notify` that dispatches a round carries the `--kind` of that round (`sync`, `fix`, `wrap-up`) and, for `fix` and `wrap-up`, the `--evidence-file`; a `--message-file` alone is the legacy spelling and is not sufficient. Status messages and takeovers for `working/` cards follow `snippets/failure-recovery.md` "Failure Recovery", which distinguishes unbound cards (a plain message without `--kind`) from bound cards (same-ID handling, or a disposed round and a new dispatch). "Once" in this file means one logical dispatch ID: same-ID reconciliation is allowed, exactly-once external side effects are not promised. Column transitions are scheduling hints; verify the dispatch receipt before treating a bound round as accepted or complete.
  - Wherever this file or a phase snippet requires `done/`, a card in `archived/` with `RESULT: completed` counts the same; wherever it requires `review/`, a card already in `done/` or in `archived/` with `RESULT: completed` counts the same.
  - Whenever this file or a phase snippet says the executing agent "moves back to `working/`", it means the ID/epoch-bound move command from the generated dispatch prompt (`snippets/commands/dispatch.md` "Durable Dispatch"); a plain `kander move <task-id> working` accepts no dispatch and authorizes no work. The only exception is a self-executed group (`snippets/self-executed-group.md` "Self-Executed Groups"), whose members are never bound to a dispatch.
- Review batches, dispatching findings back, and review gates apply only when `rules.review=true` or the user explicitly asks for a full review this time. Otherwise never load `KANDER-REVIEW-RULES.md`. In that case, after implementation and verification, enter the group integration flow under integration authorization and the project's delivery gates; record review as N/A, build no review base chain, and satisfy `snippets/review-evidence-gate.md` "Review Evidence Completion Gate" with the explicit N/A plan before any member completes.
- The fixed report format is read from `KANDER-REPORTING-RULES.md` only when `rules.reporting=true`; when off, report progress, deliveries, and unresolved items truthfully, and in-card records and state gates still apply.
- External dependencies are satisfied only per "Dependencies Between Task Cards", including its `develop` containment check. External work enters a group only through the group branch's anchor. The branch is created after every member's external dependencies are satisfied. It may be synced to `develop` per `snippets/orchestrate-start.md` "Creation and Reuse" before its review plan exists, and picks up no external work after that until final integration.

## Task IDs

- A task ID is `YYYYMMDD-short-slug-task`, unique across the board; it is the file card's entry name without `.md` or the directory card's directory name.
- `short-slug` is lowercase ASCII letters, digits, and single hyphens between them; no leading, trailing, or consecutive hyphens.

## Task Splitting and Task Groups

- Unless the user explicitly asks for a single card or a task group, the agent decides. Never put the choice to the user as a question (presenting the decision inside a plan per `KANDER-TASK-INTAKE-RULES.md` is not such a question). Judge by the number of goals: one goal is one card, however large; several goals are one card per goal. A goal is a result the user can accept on its own; a step that only makes sense as part of another goal gets no card. The only exception is the contract card below. Never split a goal to pad the count or merge goals to save cards. State the choice and its reason in one sentence, in the plan when intake guidance is on, otherwise when reporting the created cards.
- Each card focuses on one goal and spells out `EXPECTED_OUTCOME` and `ACCEPTANCE_CRITERIA`.
- When the request splits into several cards:
  - Cards form a task group only when they are related: one depends on another, or they share an integration contract that must be verified together. Unrelated goals from the same request become independent single cards.
  - A group has at most 3 cards by default; one above 5 is split into several groups along goals and dependencies. The goal count decides how many cards exist; these counts only decide how they are grouped. A group of 4 or 5 is acceptable only when its dependency chain is linear or its members share one integration contract, with the reason recorded in the `DISCUSSION` of the first card in dependency order; otherwise split it.
  - A task group ID is `YYYYMMDD-short-slug-group`, unique across the board. Each member's `- TASK_GROUP:` metadata holds its group ID; non-members leave it empty. A group is a relationship between cards and adds no board entry or state.
  - Members of one group share the same `LANGUAGE`; create them with the same `--language` or under the same configuration.
- Card count follows goals, not size; choose SIZE per `KANDER-KANBAN-RULES.md` "Task Scale and Grouping", assessing every member independently. Grouping grants no SIZE exemption, and a contract card is always `large`. Clarify shared interfaces or data contracts first; when they need separate delivery, create a contract card as a prerequisite.
- When creating a group, list all members and the dependency graph, and rule out missing references, dependency cycles, overlapping responsibilities, and overlapping files. List for every member the paths it is expected to change (directories are acceptable). Treat two members that share a path as an ordering dependency, not parallel work (see "Running Task Cards in Parallel"). Once in `todo/`, group relationships are frozen per the kanban protocol. The legacy Chinese `任务组: ...` line in `DISCUSSION` on old cards stays compatible as a literal marker.
- After creating the members, complete `KANDER-KANBAN-RULES.md` "Post-Creation Self-Review" card by card. Members must also obtain the `CARD_REVIEW:` record from an independent card review (one independent agent may review all members in one session, reading only those cards and the user's original requirement). Then check that the members fully cover the group's goals and the created cards fully cover the request, with no overlapping or missing boundaries, and that prerequisite deliveries satisfy the later cards' constraints and acceptance. Fix and re-check before advancing to `todo/`. `kander pick <task-id>` and `kander move <task-id> todo` are the same transition (`snippets/commands/pick.md`); the orchestrator performs it in `snippets/orchestrate-start.md` "Starting and Subscribing", or the creator performs it right after the checks when the user asked to start immediately.

## Dependencies Between Task Cards

- An in-group card's dependencies on cards or groups are written in a standalone code block at the start of its `DISCUSSION`, field name `PREREQUISITES`. IDs are separated by ASCII commas, and card and group IDs may be mixed. When there are none, write `PREREQUISITES: N/A` in the same position:

```text
PREREQUISITES: 20260905-contract-task,20260905-foundation-group
```

- An old card missing this line has no dependencies. A group reference expands to all current members of that group; referencing the card's own group or forming a cross-group cycle is not allowed.
- An in-group prerequisite is satisfied only when it has reached `review/` or `done/` and the orchestrator has confirmed its latest delivery commit is contained in the current group branch; the column alone does not release it.
- Out-of-group cards and all members expanded from group references must reach `done/` (or `archived/` with `RESULT: completed`) with their deliveries confirmed contained in the current `develop`. An `archived/` card with any other result, or a `trash/` card, never satisfies a dependency; hand the decision to the user. Non-group cards get no automatic dependency resolution; their executing agent verifies delivery preconditions against the task contract.
- Out-of-group references of any member block the whole group, because the group branch is not rebased once its review plan exists and the pre-plan sync of `snippets/orchestrate-start.md` "Creation and Reuse" is the only other point at which external work enters. When one late member would hold up the others for long, move it into a successor group that depends on the external target instead of starting the group early. This regrouping is a planning decision before the cards enter `todo/`; afterwards it is a frozen-relation change that needs an explicit user decision per `KANDER-KANBAN-RULES.md`.

## Running Task Cards in Parallel

- Note explicitly which cards can run in parallel.
- Run in parallel only cards that are ready at the same time and whose modified resources can be isolated at file level, not by module name. When the same resource cannot be isolated, record the dependency and run serially, and never share an execution worktree.
- Hotspot files, which many cards must touch to deliver anything (generated code, lock files, IDE project files, shared contract or schema files, per-locale string tables), are assigned to exactly one member, or the members that touch them run serially. Never let two parallel cards edit one hotspot file.
- Split a change that spans several endpoints or clients by delivery slice, not by layer. One slice carries one transport or contract change through every endpoint it affects and is verifiable on its own. The slices run serially in dependency order. Slicing by layer makes every card touch the same endpoint modules and test harnesses and guarantees conflicts.

## Phase Rules

Orchestration and member execution are read when the phase arrives; Kander names the file in the task file or dispatch that starts it.

- Executing a member card (branch from the group branch, delivery to it): `snippets/execute-group-member.md`.
- Orchestrating a group from the start entry to receiving deliveries: `snippets/orchestrate-start.md`.
- Review batches, dispatch-back, and evidence commands: `snippets/orchestrate-review.md`.
- Merge-back, wrap-up, and cleanup failures: `snippets/orchestrate-integrate.md`.
- A session that orchestrates and executes the members itself: `snippets/self-executed-group.md`.
- A fix round after a `notify --kind fix` dispatch: `snippets/fix-round.md`; the member's wrap-up after `--kind wrap-up`: `snippets/wrap-up.md`.
