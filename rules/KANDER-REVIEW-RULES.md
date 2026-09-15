# Review Rules

Loaded automatically and used to judge triggering only when `rules.review=true`; may also be read when the user explicitly requests the full review flow for the current task. Arguments and the read-only gate for a single `kander review` are in `KANDER-BASE-RULES.md`; that does not automatically enable the full flow.

- This module does not depend on the Git workflow. When `rules.git=false`, keep the user's branch and delivery flow, determine the review base from the base recorded at task start or the range the user stated, pin the full SHA before the first round, do not guess `develop` or create branches, and get the user's authorization before committing the review target.
- The Git mechanisms for rebase, merge-back, and cleanup mentioned here are read from `KANDER-GIT-RULES.md` only when `rules.git=true`; task group clauses apply only when `rules.task_groups=true`; the fixed completion report only when `rules.reporting=true`. Do not load disabled modules through references.

## Reviewer Selection

- Reviewers are the five built-in agents plus any configured agent that declares a review template (`args.review` together with `review.*`). The public review entry on all platforms is `kander review` under the command root (per `KANDER-AGENTS.md` "Scope").
- Built-in isolation arguments live on each agent's definition. A custom reviewer's read-only posture is the definition author's responsibility; Kander still validates the result and isolates review-private directories. Apart from CLI and isolation arguments, every rule here is identical for all reviewers.

| reviewer | argument | CLI | isolation |
| -------- | -------- | --- | --------- |
| Codex | `codex` | `codex` | from the embedded definition |
| Claude | `claude` | `claude` | from the embedded definition |
| Grok | `grok` | `grok` | from the embedded definition |
| Cursor | `cursor` | `cursor-agent` | from the embedded definition |
| Pi | `pi` | `pi` | from the embedded definition |
| custom | the agent name | `review.path` or `path` | author's responsibility |

**Reviewer Isolation**

- Codex runs a sandbox-enforced read-only shell inside the target worktree; Grok exposes only read and search tools; Claude runs with its full toolset minus `Edit` and `Write`; Pi runs with a read-only tool allowlist; Cursor relies on the prompt and post-run verification. Kander validates every reviewer's output and afterwards checks the Git-visible state of the target worktree, which does not detect writes outside it or to ignored paths inside it. Leftover processes and runtime cleanup failures reject the result.
- A UTF-8 bootstrap task file in the protected review runtime names a separate `review-contract.md`; the reviewer reads both completely and does not modify either file. The reviewer receives only a short instruction naming the bootstrap file (on stdin by default, or through `{instruction}` in argv when the definition says `review.stdin: none`). Do not replace or loosen isolation arguments to unify implementations or accommodate Windows.
- `PMQA` and `Security` each select a reviewer from the first explicit source by precedence: the current user instruction; the nearest project `AGENTS.md` or `CLAUDE.md`; the user's global rules; the role configuration of the current scope (read by `kander review`; Codex when no configuration exists). Read rule files required for this judgment if not yet loaded; an unreadable one counts as unspecified.
- Different roles may use different reviewers. Fix re-runs and conclusion confirmation for one role keep the reviewer selected for that role in the current batch: the incremental chain requires the same reviewer, and closure rejects a successfully executed run not connected to the selected conclusion. A user-designated switch inside a batch is possible only while that role has no successfully executed run there (failed runs are bound through `resolved_failures`); otherwise it takes effect from the next batch.
- Below, "reviewer" means the one selected for the current role and "review entry" means `kander review` under the command root. "Main agent" is the agent that verifies findings and talks to the user: the executing agent for a single card; for a task group, the orchestrator for batch scheduling, aggregation, and user decisions, and each executing agent for its own assigned findings (see "Verification Split Between Orchestrator and Executing Agent"). "Delivery notes" means the card's `SUMMARY` (or `report.md` for a large card) and, for a non-kanban task, the final delivery message.

## Review Roles

The only runnable roles are `PMQA` (stage one) and `Security` (stage two), both defaulting to `auto`. `skip` means N/A unless a higher-precedence source requires the role; `required` means the role runs once review is triggered unless a higher-precedence source skips it. New invocations and two-key batches accept only these two names and their case aliases.

- `PMQA` first checks every acceptance criterion against the task contract, including explicit performance acceptance, then performs the architecture, correctness, regression, testing, and code-quality checks under "Goals and Boundaries". Explicit performance acceptance belongs to the contract portion; there is no general performance review. Finding IDs use the `PMQA-` prefix.
- `Security` first traces untrusted inputs across trust boundaries, then verifies realistic end-to-end exploit chains, reporting each root cause once and stating explicitly when no qualifying vulnerability was found. Finding IDs use the `Security-` prefix.
- Reviewer selection, models, large/small policies, incremental chains, verification, and failure handling apply equally to both roles. Never switch an existing run chain into a different role.
- New plans and batches use exactly the two keys `PMQA` and `Security`, each `required` or `N/A: <reason>`; a later batch appended to a historical four-role plan also uses the two-key form, and each batch freezes its own role set.

### Historical four-role evidence

`schema_version` stays 1 and existing plan, batch, run, index, manifest, closure, disposition, waiver, and recovery artifacts are never rewritten. Reading, same-ID retries, and close accept exact 2-key, 4-key (`PM`, `QA`, `CSA`, `Hacker`) and 6-key (those plus `PMQA` and `Security`) requirement objects; a run is admitted only when that batch's own requirements mark the role `required`, so an open four-key batch with `PM: required` may still complete a PM run and close. Waived stays valid for historical `CSA`/`Hacker` records and for `Security`, refused for `PMQA` and historical `PM`/`QA`.

## Goals and Boundaries

- Review goal: the plan or user goal is fully and correctly implemented, the flow is closed, and there are no directly related logic holes or regressions. Expanding the task scope is forbidden.
- `PMQA`'s quality portion first checks project architecture, module responsibilities, dependency direction, public boundaries, and integration patterns, then directly related correctness, regressions, testability, and code quality (single responsibility, readability, change locality, coupling and duplication, error and resource handling). It reports no performance findings; explicit performance acceptance is checked by the contract portion.
- `PMQA` limits non-generated code files to 1000 physical lines and reports a gate finding only when a file added this round exceeds 1000 lines or a touched file that was at or under 1000 lines at the base exceeds 1000 now; a file already above 1000 lines at the base is not measured, and trimming unrelated lines to shrink such a file is itself a finding. These are the conditions of `KANDER-CODE-RULES.md` "Delivery Self-Check" item 2. Do not sweep unrelated over-limit files.
- `PMQA` findings are limited to realistically reachable problems within the task context, existing contracts, or the code and module boundaries touched this round. Exhaustively stacked conditions, extreme edge cases, fabricated failures, or low-realism problems are forbidden; stop once task-related realistic risks are covered.
- Only handle problems introduced, aggravated, or masked by this task. Existing problems that are neither do not require fixes, do not block review or integration, and are not sent to the user, unless the user explicitly includes them.
- If a review result requires changing business logic, user flow, or external contracts beyond the user's explicit goal, explain the impact and let the user decide; implementing that change on a review conclusion alone is forbidden.
- For any issue needing the user's decision, the main agent explains each item in detail (problem, trigger condition, actual impact, suggested handling, the role that raised it) and gives numbered options; abstract options are forbidden, and source attribution must not be lost when aggregating.
- Runtime environment problems that affect verification credibility, delivery usability, or user data safety are reported explicitly. Unless the change directly introduces, aggravates, or masks them, or the user asks, record them only as a verification fact; judging them alone as blocking, high, or medium is forbidden.

## Preconditions and Execution

- Review triggering uses a whitelist: a task enters review only when it hits an entry below; no hit means no automatic review. An explicit user request enters unconditionally.
  1. An explicit feature development or bug fix task.
  2. Code changes beyond the small-change threshold: more than 1 code file changed relative to the review base, or added plus deleted lines exceeding 10. `git diff --numstat <base>..<commit>` is the source of truth; count only code files, excluding `*.md` and `*.markdown`; binary files are exempt.
  3. Security-sensitive changes, regardless of size: authentication and authorization, credentials, cryptography, permission checks, sandbox and isolation arguments, or other logic on a security boundary.
  4. External contract changes: adding, modifying, or removing CLI arguments, configuration schema, persisted data formats, network or plugin APIs.
  5. Irreversible or cross-data operations: data migration scripts, bulk rewriting or deleting of user data, anything a simple rollback cannot recover.
  6. Changes to build, install, release, CI, or the review gate flow itself.
  7. Dependency changes: adding, upgrading, or removing external dependencies.
  8. Test removal or loosening: deleting or skipping tests, relaxing assertions, reducing coverage.
  9. Deleting or renaming public modules, or cross-module refactoring.
- The whitelist is judged by the task's actual changes and the user's wording, not by title or labels; rephrasing a task to bypass review is forbidden.
- When review is skipped because nothing hit, tell the user before integration "this task did not trigger the review whitelist and did not go through the review loop" and state the change scope (files changed, lines added and deleted). Silent skipping is forbidden. If the user then requests a review, run the full flow.

**Review Tool Unavailable**

- When the selected reviewer's CLI or the review entry is unavailable on this machine, tell the user "the <reviewer> CLI is not installed on this machine; review cannot be executed" (and that Kander needs reinstalling when the entry itself is missing), keep the branch and worktree, and give numbered options `1. Retry after installing that CLI`, `2. Restart review with another reviewer`, `3. Skip this review and integrate directly (risk borne by the user; record "not reviewed" in the delivery notes)`, `4. Stop integration`. For a task-bound review, option 3 exists only before the review plan records that batch's role as `required`; after plan creation the requirement is frozen even when no run has started, closure needs a successful run, and the choice is between 1, 2, and 4. For an unbound review, option 3 exists only before its first run.
- Skipping on your own or switching reviewer without the user's explicit designation is forbidden.
- Before review, commit all task changes since the review base grouped by concern, keeping the worktree free of uncommitted or untracked files.
- Before review, the author completes `KANDER-CODE-RULES.md` "Delivery Self-Check" and records it when `rules.code=true`; when the code module is off, the author at least runs `kander check delivery --base <review-base>` (or records an equivalent `git diff --check` plus the line-count candidates), verifies the targeted tests ran at the final commit, and records that. Findings the self-check should have caught are ordinary findings: mechanical ones are `[mechanical]` items closed per "Fixed archiving", the rest follow the normal fix round. Record the self-check failure on the card; it is never an extra round or a return outside the review flow.

**Review Base**

- With the Git module enabled, the base of a dedicated task branch is the full `develop` SHA most recently used to create it from `develop` or rebase it onto `develop`. Fixes under the same base do not change it.
- The base is frozen once review starts; do not chase `develop` by rebasing before the loop ends. After the integration rebase, the base moves to the new base point, and whether to re-review follows the one-time gate in `KANDER-GIT-RULES.md` "Integration and Cleanup".

A standard review has two stages. Whether each role runs is resolved per "Review Stages" and its focus per "Review Profiles"; skipped roles are marked N/A. Stage one is `PMQA`; stage two is `Security` after stage one passes. Non-mechanical fixes receive incremental re-review, including the cross re-reviews below:

```text
[1] PMQA   First round. Contract portion: whether the implementation fully meets the task context;
                quality portion: functional correctness, regressions, tests and code quality; a skipped role is marked N/A and passes directly
     |  Must-fix findings --> fix and commit (same base, new HEAD)
     |                          --> incremental re-review --> back to [1] until PMQA passes
     |  Only mechanical must-fix items left --> fix and commit, main agent verifies mechanically --> PMQA passes on the new HEAD without re-running
     v  Enabled stage-one role passes
[2] Security  Decided by stage policy and trigger conditions; required overrides trigger conditions; when it does not run, mark N/A and pass directly
     |  Qualified finding --> user decision --> 1 fix and commit --> incremental re-review [2]; fix scope is incrementally re-reviewed by enabled PMQA
     |                                  Subsequent PMQA fixes substantively change security-related code --> Security incrementally re-reviews [2], until all parties pass
     |                           --> 2 confirm pass (record accepted risk and rationale in the delivery notes)
     |                           --> 3 stop integration (keep branch and worktree)
     |                           --> kanban task with no decision for 15 minutes: timed-out and ignored --> stage ends
     v  Stage ends
   Review complete --> show all unresolved items to the user --> enter the integration flow
```

- Incremental re-review: for the same role under the same base, the second round and later are always incremental, never full. `reviewed-commit` is the commit that role reviewed last round, strictly between base and new HEAD; task-bound invocations derive it from `--previous-run-id` (an explicit value must match), unbound ones supply it positionally. Only `reviewed-commit..commit` is new material: the reviewer verifies item by item that last round's findings are closed and reports only problems introduced, aggravated, or masked by the fix, or breakage of the touched requirements. Unchanged code is accepted; re-auditing it is forbidden.
- The review context includes every finding from the previous round with the main agent's handling conclusion. Task-bound runs get the original report, author records, and batch view automatically, with positional review-context as a delimited supplement; unbound runs get the full context from the caller (fix commit and verification for confirmed items, basis for rejected items, the user's decision for unverifiable ones). The reviewer only checks these facts; the main agent must not omit or rewrite last round's list.
- The first round passes no `reviewed-commit`. A role that PASSed is not re-run (see "Carrying Conclusions Forward"). A base or task context change means a new batch where that role restarts with a full first round. The main agent's verification duty is not reduced by incremental re-review.
- A fix round for a role that already reported on this base is invalid unless it runs in incremental mode. The tool does not refuse another full first round, but closure rejects a successfully executed run not connected to the selected conclusion chain, so the batch could no longer close. The only later full first rounds are the retry of a failed run (bound through `resolved_failures`) and the first round of a new batch.
- Severity tiers: every role labels its output with the six tiers below. A new-schema item without a tier fails output validation; only for a legacy unstructured report does the main agent assign the missing tier inside the `map-legacy` mapping.

| Tier        | Meaning                                                                | Must fix |
| ----------- | ---------------------------------------------------------------------- | -------- |
| `blocking`  | Goal not met, or data corruption, security failure, main flow unusable | Yes      |
| `high`      | Certain failure or regression on a common path, clear trigger          | Yes      |
| `medium`    | Fails under specific conditions, or real contract/boundary/error defect | Yes      |
| `low`       | A real defect, but rarely triggered with negligible consequences       | No       |
| `recommend` | Not a defect, but should change per project rules or conventions       | No       |
| `suggest`   | Optional improvement; the trade-off is up to the owner                 | No       |

- Fixed archiving: the following are never downgraded for rare triggering or minor consequences and are always at least `medium`, whichever role finds them: documentation or code comments inconsistent with the implementation; dead code (unreachable, or with no calls or references); redundant tests (duplicated coverage, or assertions unrelated to the behavior under test).
- Mechanical must-fix items are those three categories. In the structured item, the reviewer sets `mechanical` to exactly `documentation`, `dead-code`, or `redundant-test`; `[mechanical]` in human-readable prose is only a display tag and is not the machine binding. They are dispatched with the gate findings, fixed and committed, and closed by the main agent's mechanical evidence (sentence-by-sentence comment comparison, reference search output, or test lists). A round whose confirmed must-fix items are all mechanical never re-runs the reviewer; that role passes on the new HEAD after mechanical verification. When mixed with non-mechanical items, the incremental re-review covers only the non-mechanical ones and states that the mechanical ones were closed by the caller. When the reviewer omits the field, the main agent may independently classify the item per the definition and records the absent reviewer label; classifying non-mechanical defects as mechanical to skip re-review is forbidden.
- Tiers use the English identifiers above. `blocking`, `high`, `medium` go in the report's `FINDINGS` array; the rest in `NON_BLOCKING`. A new-schema report missing either array fails validation and is retried with a new run ID; only a legacy unstructured report may lack a non-blocking section, and then the mapping records "this role provided no non-blocking items" on the unresolved list.
- Must-fix threshold: only `blocking`, `high`, and `medium` verified and accepted by the main agent must be fixed. `low`, `recommend`, and `suggest` never block review or integration and never open a fix round, but closure still requires the assigned author's disposition (`fixed`, `deferred`, or `rejected`, each with a basis) for every one of them. They therefore travel to the author in a bound `--kind fix` dispatch whose evidence references every assigned `FINDINGS` and `NON_BLOCKING` item for that card (either set alone, or both) and whose message lists each item with role and tier, marking non-blocking ones "disposition only"; existing author originals on the lineage are included. Non-blocking references never make the round a reviewer re-run: the author completes a disposition-only round with `move review` on the unchanged delivery SHA. They stay on the card's unresolved list with the author's disposition for the next delivery or a follow-up card. A dispatch that asks the author to "triage" without listing items with role and tier is forbidden.
- In stage two, only `blocking`, `high`, and `medium` findings returned by an actually running `Security` (or historical `CSA`/`Hacker`) and confirmed by the main agent go to the user for decision. Each item lists the problem, impact, fix method, and at least `1. Fix and re-review`, `2. Confirm pass and accept the risk`, `3. Stop integration`. Other tiers and pure defense-in-depth advisories go straight to the unresolved list.
- The security finding timeout applies only to kanban tasks. Each item is timed independently from when its full information and options reach the user; items sent together share the send time, and a partial answer does not stop the other timers. With no explicit decision after 15 minutes, mark it "timed out and ignored", record role, tier, problem, impact, send time, timeout time, and rationale, and continue; this is not `PASS`, confirmation, or risk acceptance, and a decision that arrives before the card completes is honored. Non-kanban tasks, `PMQA` findings (and historical `PM`/`QA`), unverifiable items, contract or owner changes, and acceptance or integration confirmation requested by the user are never skipped by timeout.

**Invocation Arguments**

- With no reviewer specified: `kander review <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]`, dispatched by configuration; with one: `kander review <reviewer> <CWD> ...`. A global install uses `kander` on PATH or the absolute path of the selected executable (in PowerShell `& "C:\path\to\kander.exe" review ...`); never assume a copy in `~/.local/bin`. A project install uses the absolute entry under its command root.
- Automation launches the selected `kander` executable through a process API argv array, passing `review` and each argument separately. Batch files and Windows PowerShell 5 cannot guarantee lossless argv, so data containing `&|<>^%!`, quotes, or boundary backslashes is never passed through `.cmd` or concatenated shell strings. Bypassing Kander to call the reviewer CLI directly is forbidden.
- `CWD` is the absolute path of the target worktree; all commit arguments are full SHAs; the base is an ancestor of the commit, `HEAD` equals the commit, and there are no uncommitted or untracked files.
- Only incremental re-reviews use `reviewed-commit`, strictly between base and commit. Task-bound runs derive it from `--previous-run-id` and preserve the prior report and author conclusions automatically; unbound incremental runs require positional reviewed-commit and a nonempty review-context with the complete prior finding list and conclusions.

**Carrying Conclusions Forward**

- All roles of a full review share `CWD`, base, and task context; historical `CSA` and `Hacker` triggered together use the same commit. The task context is the authoritative requirement contract: a string for short tasks, a readable absolute spec path for long tasks.
- Under the same base, a role that passed is not re-run because of another role's fix, except two cross re-reviews: security fixes are re-reviewed by enabled `PMQA` (if `PMQA` is N/A in the frozen requirements, retain N/A; newly required coverage needs a new batch), and a subsequent `PMQA` fix that substantively changes security code is re-reviewed by the security role for the fix scope. On a historical four-role batch, the enabled QA-capability role (`QA` or `PMQA`, plus enabled `PM` when functional behavior changes) performs the cross re-review.
- Roles may pass on different commits; within the same base, the final commit must descend from every passing commit.
- Non-integration changes such as switching base, recreating the branch, or a forced rebase during review invalidate all conclusions. Because every batch with reviewer runs must still close, first close the open batch on its current target: the remaining required roles run there, and superseded findings are disposed as `rejected` with the user's quoted decision. A task context change on the same base chain then continues in a new batch of the same, still unsealed plan, starting at the previous closed target and reviewing only the work after it. When the user wants the closed range re-reviewed under the new contract, or when the base or the history changed, continue as new work under new cards and a new plan per "Group-Level Review for Task Groups". Report the wasted rounds either way.
- A rebase during integration caused by `develop` advancing carries conclusions forward per the one-time gate in `KANDER-GIT-RULES.md`; the pre-rewrite SHA need not remain an ancestor.

**Report Files and Stable Identity**

- Without task binding, save each role's stdout in a separate report in a private temporary directory outside the target worktree; clean it after the round passes, terminates, or the stage-two decision finishes, explaining first when retaining diagnostics.
- For kanban reviews, pass every reviewed card with repeated `--task <id>` before CWD, together with `--batch-id <id>`. Directory cards in working/review and a common report language are required; legacy file cards first follow the init maintenance protocol.
- A batch is named in the review plan before its first run (at plan creation, or by `extend-plan` afterwards) per `KANDER-KANBAN-RULES.md` "Review Evidence Completion Gate"; the first run supplies `--requirements-file`, an absolute JSON path naming exactly `PMQA` and `Security` with `required` or `N/A: <reason>`, resolved through the precedence and stage rules and identical to the plan entry. Requirements, members, base, task-context bytes, and report language are fixed for the batch. If the live spec changes or moves, later roles, fix rounds, and retries pass the absolute path to the archived `task-context.md` (available under `inputs/task-context.md` in the run control directory before publication) to retain the original bytes and batch ID.
- Use a distinct `--run-id` per actual reviewer invocation; omit it to get a random ID on stderr and record it. Different roles on one commit share the batch ID with different run IDs; timestamps establish no identity or order. A same-ID retry with identical inputs never launches the reviewer again: it verifies existing evidence and fills missing card publications, and if the original gate process ended before finalization, recovery marks the run interrupted and preserves partial raw output.
- A task-bound incremental invocation includes `--previous-run-id`; the predecessor must be fully published for the same batch, base, role, and reviewer. Caller context is a verbatim supplement, never a replacement.
- Keep the batch ID for fixes. To advance its target, pass an absolute `--advance-file` JSON with `previous_target`, `target`, a nonempty `reason`, and `deliveries` mapping every commit in that exact range to a member task ID; the command verifies range and membership and applies a compare-and-swap. Outstanding executions or incomplete publications settle first; include no outside deliveries.
- Each card retains complete immutable originals under `reviews/<run_id>/` (input context, raw output, logs, any valid report, sidecar, manifest), also for failed launches after intent creation, timeouts, invalid output, leftover processes, and cleanup failures; preflight failures invent no run. The report language freezes from card LANGUAGE, falling back to configuration only when absent. The tool appends one JSON index line per run in REVIEWS; never derive this index from prose, paste reports into the body, edit originals through update, or remove archived evidence.
- A structured report with missing or invalid findings or invalid lineage fails output validation at finalization: it is archived with `execution_status=failed`, a nonzero exit, and the reason. Retry in the same batch with a new run ID: a failed first round retries as a full invocation; a failed incremental round reuses its last valid `--previous-run-id` and reviewed-commit. Never select the failed run as predecessor or replace a valid chain with an unconnected full round, and do not repeat an advance the failed intent already committed. Bind each failed attempt through `resolved_failures`.
- A run whose reviewer process ended before producing a valid report (a signal exit such as 143, a launcher failure, a kill before output) is archived as failed or interrupted and retried once immediately in the same batch under the previous bullet, without reporting to the user first. Only when that retry also ends without a report is it a persistent backend failure per "Stage One Backend Failures".
- `execution_status=ok`, readable stdout, and a zero exit do not imply semantic PASS; interpret the report through this workflow. Failed or interrupted runs never establish a passing role.
- Pending reviews allow ordinary updates and moves between working and review; finish publication before any terminal move. Each card publication is atomic, and a cross-card failure preserves successful publications, reports each failed card, and exits nonzero. Retry the same run ID after resolving the error, obeying init's maintenance preconditions when init is required. An incomplete publication never counts toward batch completion, and `kander check <task-id>` still diagnoses it; do not move a done card back or bypass controlled writes to repair it.
- `kander check` validates intents, manifests, indexes, original hashes, language, membership, and predecessor relations; it infers no semantic PASS and closes no batch. Reviewer isolation and end-of-run worktree verification always apply.

## Group-Level Review for Task Groups

Kanban task groups (`KANDER-TASK-GROUP-RULES.md` "Task Orchestration") are not reviewed card by card; the orchestrator reviews them in batches on the group integration branch. Single-card tasks are reviewed card by card as above. This section changes only the review unit and role split; stage policy, incremental re-review, verification duty, and conclusion requirements carry over.

- The review unit is the batch: while no batch is open, the orchestrator receives ready `review/` deliveries onto the group branch per `KANDER-TASK-GROUP-RULES.md` "Group Integration Branch", verifies them, and, once every member has started, opens a batch over every delivery received since the previous closed target, choosing the moment by module, milestone, or dependency-chain step. Prefer small batches: when a delivery is large (many files, several endpoints or clients), open a batch as soon as it is received, so each fix round stays small and later deliveries do not queue. The batch diff should be readable in detail, and the batch contract covers all unreviewed deliveries from the batch base to HEAD; a received delivery is never left out of the batch that follows it. At least one batch per group; one per card is not mandatory. The whitelist is judged on the batch's aggregated changes, and a group containing feature development or bug fix cards always enters review.
- Each batch is an independent full review. `CWD` is the group worktree, the commit is the group branch HEAD. The first batch's base is the group's anchor at plan creation (the full `develop` SHA the group branch was created from, or the last pre-plan sync per `KANDER-TASK-GROUP-RULES.md` "Creation and Reuse"); a later batch's base is the closed target of the previous batch, so each batch reviews only its own changes. Other preconditions are unchanged. While a batch runs, new deliveries queue and the group branch is not updated; in-batch fixes wait for the reviewer to exit and are fast-forward received before re-review. Never change the review worktree or HEAD while the reviewer is running.
- The task context is a spec file merging the full contracts of all batch cards, in a temporary directory accessible only to the current user, passed by absolute path. Mark the task ID per card, keep all six contract fields from "task context and review context Templates" and each card's `DISCUSSION`, omitting no user trade-offs, acceptance criteria, or threat models. Passing only one card or the group name is forbidden. Each card's `OUT_OF_SCOPE` remains the boundary for judging overreach.
- Conclusions are not carried across batches and there is no cross-batch incremental re-review: each batch starts from a full first round without `reviewed-commit`. Only fix rounds within a batch are incremental (same base, same batch task context, `reviewed-commit` the group branch commit that role reviewed last round). The previous batch's unresolved items are written into later batches' exclusions and not reported again.
- When the user authorizes replaying deliveries onto a new base after a rebase or re-split, that replay is new work under new cards and a new plan; the previous batches are closed and stay history. In that batch's review context, list the files byte-identical to the previously closed target as already reviewed and focus the roles on files changed during conflict resolution and on the named integration seams; at tool level it is still a full first round.

**Verification Split Between Orchestrator and Executing Agent**

- The orchestrator triggers roles, stores reports, attributes findings by change scope through `review assign` (a finding goes to every batch card whose scope it hits; a card outside the batch cannot be assigned; attribution rules, including findings that hit no member's scope, are in `KANDER-TASK-GROUP-RULES.md` "Review Batches and Dispatch-Back"), and uses `notify` to dispatch the finding text, role, and tier back to the original executing agents. It does not call `kander resume <task-id> --message-file` separately; resumption is chosen inside notify. It aggregates the returned lists into the review context, triggers incremental re-review, and shows all unresolved items. It never judges confirmation on the executing agent's behalf, nor omits or rewrites conclusions.
- The executing agent bears the "Main Agent Verification Duty": verify item by item; fix confirmed items on the card's task branch, commit, rebase onto the latest group branch, re-verify, and update the task branch per the Git rule file; write the basis for rejected or unverifiable items; never update the group branch. Submit each conclusion through `review disposition`, write into `IMPLEMENTATION` one entry with the counts per status, the card-relative paths of the disposition records, and the full SHA of the latest delivery (never the finding list itself, per `KANDER-KANBAN-RULES.md` "Contract and Records"), then `move review` and end the turn. The orchestrator verifies and fast-forward receives the fix delivery, then triggers incremental re-review.
- Security roles run per "Review Stages" and trigger conditions after the whole batch passes stage one, on the same group branch commit; user decision and timeout rules are unchanged.
- A valid group-level review requires that the actual roles of every batch satisfy "Conclusions and Failure Handling" and that each later batch's base equals the previous closed target, forming a continuous chain from the group base to the final HEAD. Unresolved items are written into each card as one-line references per `KANDER-KANBAN-RULES.md` "Contract and Records". The orchestrator integrates per `KANDER-TASK-GROUP-RULES.md` "Group Integration Branch"; an integration rebase caused by `develop` advancing follows its "Merge-Back and Cleanup Preconditions", the reviewed patch must survive unchanged, and the orchestrator resolves no conflicts on the group branch.

## Review Stages

Beyond "Review Profiles" and the whitelist, whether each role runs is constrained by the stage policy. The configuration's `review_stages` specifies `auto`, `skip`, or `required` for `PMQA` and `Security`, per task scale as `review_stages.large` and `review_stages.small` (a legacy flat `{role: mode}` object applies to both scales); missing roles default to `auto`. View it with `kander config`.

Resolve each role from the first explicitly specified source by precedence:

1. The user instruction for the current task.
2. The project-level `AGENTS.md` or `CLAUDE.md` nearest to the target file; when unspecified, the user's own global rules.
3. The `review_stages` value for this card's `SIZE` scale; a task-group batch with mixed sizes uses `large`.
4. Review profiles and security trigger conditions (effective only when tier 3 is `auto`).

- `auto`: run or not per the "Review Profiles" table and the security trigger conditions; N/A when not triggered.
- `skip`: N/A by default unless a higher-precedence source explicitly requires running.
- `required`: run by default once review is triggered unless a higher-precedence source explicitly skips.

Example wording for tier (2): `Security is always N/A in this repository`, or `Skip PMQA for this task`. Skipped roles are recorded N/A in the delivery notes with the basis. When the user explicitly asks to run a skipped role, the user instruction prevails. Example policy requiring PMQA and leaving Security to auto at both scales:

```json
{
  "review_stages": {
    "large": {"PMQA": "required", "Security": "auto"},
    "small": {"PMQA": "required", "Security": "auto"}
  }
}
```

## Review Profiles

Review profiles decide which roles run in a round and the default content of each role's "Review Focus"; they change neither stage order nor the invocation and isolation of the review entry.

- The profile is derived from the whitelist entries the task's actual changes hit; the executing agent classifying or downgrading on its own is forbidden. With several hits take the union: each role starts one first round, fixes are incremental re-reviews, and the focus of each entry is merged into "Review Focus".
- The table only suggests role combinations; whether a role actually runs is decided by "Review Stages". Entries formerly annotated "PM exempt" still run `PMQA`.
- Security trigger conditions (in `auto` mode): `Security` runs when the change involves untrusted input, authentication and authorization, credentials, cryptography, network protocols, remote execution, file writes decided by untrusted input, install/update or release integrity; when the external attack surface is added or substantively changed; when a dedicated security review is being performed; or when the user requests it. `required` overrides these triggers and `skip` still means N/A.
- When the user explicitly requests a review that hits no whitelist entry, select the profile by the scope the user specified; when unspecified, use the feature development profile.

| Whitelist entry             | Roles                | Review focus                                                  |
| --------------------------- | -------------------- | ------------------------------------------------------------- |
| 1 Feature development       | PMQA                 | Contract: requirement completeness, flow closure; quality: regressions, test coverage |
| 1 Bug fix                   | PMQA                 | Contract: fix scope matches root cause, no out-of-scope changes; quality: repro path closed, regression tests |
| 2 Code change over threshold | Classified by content | Assign to the closest other entry; no clear type: feature development profile |
| 3 Security-sensitive        | PMQA + Security      | `THREAT_MODEL` mandatory, not N/A; isolation and permission boundaries, credential handling |
| 4 Contract change           | PMQA                 | Contract: impact surface, doc sync; quality: caller adaptation, compatibility handling |
| 5 Irreversible or cross-data | PMQA                 | Reversibility plan, dry-run or equivalent evidence, state after failed interruption |
| 6 Build/install/release gate | PMQA (+Security)     | Flow completeness, upgrade and rollback paths; Security when release integrity is involved |
| 7 Dependency change         | PMQA (+Security)     | Trusted source, version pinning, license, upstream breaking changes |
| 8 Test removal or loosening | PMQA                 | Check each removal or loosening reason; not masking a known failure |
| 9 Refactor or public module change | PMQA          | Behavioral equivalence: external behavior unchanged, all tests pass unchanged in meaning |

## Main Agent Verification Duty

- Accepting or rejecting a reviewer conclusion without verification is forbidden. Every finding and every `NON-BLOCKING` item gets a conclusion only after the main agent independently verifies it; the reviewer's wording, tone, confidence, and finding count are not evidence.
- Verification returns to the facts of the target commit: read the related code and contracts, walk the claimed trigger path, run commands and record output when needed. Concluding from impression or "sounds reasonable" is unverified.
- There are only three conclusions:
  - **Confirmed**: handle per tier; must-fix tiers are fixed and committed.
  - **Rejected**: not handled. Valid grounds: the basis does not match the actual code of the target commit; the problem existed at the base and was neither aggravated nor masked; it hits a specific `OUT_OF_SCOPE` item; it is beyond the contract, i.e. the demanded behavior is neither in `ACCEPTANCE_CRITERIA` nor a logical necessity of an existing contract (concurrency interleaving, cross-platform or security hardening, syncing shared contracts and architecture docs all belong here unless named by the acceptance criteria); the assumed trigger premise does not hold; same root cause as an accepted finding. Beyond-contract items are recorded as unresolved with a follow-up card suggested, but a real defect introduced, aggravated, or masked this round that reaches `blocking` or `high` still goes to the user to decide inclusion.
  - **Unverifiable**: state which evidence or environment is missing and send it to the user; treating it as rejected on your own is forbidden.
- Rejecting or changing tier (up or down) states the verification basis: file and line, commit SHA, commands run with output, the verbatim exclusion cited, or the user's quoted decision with its time. "Low impact", "later", "too large a change", or "the reviewer lacks context" are not a basis. A downgraded item still goes on the unresolved list.
- All rejected and unverifiable items go on the unresolved list for the user to re-check; the user may overturn any conclusion. An item the user wants handled becomes must-fix; one the user declines is recorded `rejected` with the quoted decision and stays on the list.
- Confirming a finding does not mean copying the reviewer's fix; apply the minimal correct fix. A fix that would change a user-set direction or an external contract goes to the user per "Goals and Boundaries".
- One root cause, one item in the dispatch summary and the user report: when several roles report the same root cause, present and count it once citing all source IDs. Machine identities are not merged: each `(run_id, finding_id)` keeps its own disposition per assigned task, and the duplicate cites the other item as its basis.

## task context and review context Templates

- `OUT_OF_SCOPE` is the risk-bounded stop boundary of the review, judged category by category with reasons, across four categories: problems that existed before the change; concurrency interleaving, cross-platform, and security hardening; syncing shared contracts, public APIs, and architecture docs; adjacent features and later phases. Categories not required by `ACCEPTANCE_CRITERIA` are stated as excluded; categories this card owns are stated as included. A single generic exclusion does not complete the contract; the card-creating agent completes it before `pick`. Reviewer and main agent both use this list to judge overreach; overstepping findings are recorded as unresolved with a follow-up card suggested.

The task context has six fields; keep all of them, write `N/A` when nothing applies, and never fabricate `USER_DECISIONS` or `ACCEPTANCE_CRITERIA`. When using an absolute spec, pass the full contract including any `DISCUSSION`:

```text
GOAL: <what to change and why>
USER_DECISIONS: <directions and trade-offs the user has explicitly decided>
EXPECTED_OUTCOME: <observable, verifiable state after completion>
ACCEPTANCE_CRITERIA: <keep each confirmed verifiable condition item by item>
THREAT_MODEL: <protected assets, trusted principals and realistic attacker capabilities; write N/A for non-security tasks>
OUT_OF_SCOPE: <see the list below; state each item with its reason>
```

The optional review context for roles in each stage is for evidence and navigation; changing the task context is forbidden:

```text
Fixes included this round: <fixes for findings accepted in earlier rounds; omit in the first round>
Review focus: <(1)(2)(3) itemized, pointing at where the real risk is this round>
Verification records: <commands run and their results, including items that could not be executed and why>
Environment gaps: <actual environment problems affecting verification and substitute evidence; omit if none>
```

Common `OUT_OF_SCOPE` items:

- Existing problems: `<specific item> existed before the change (see <commit>); fixing it would expand this task's scope.`
- User-decided directions: `This is the direction the user explicitly requested; do not oppose the direction itself on the grounds of <opposing claim>; the user is aware of the trade-off.`
- Hardening beyond the contract: `Concurrency interleaving, cross-platform or security hardening is not listed in ACCEPTANCE_CRITERIA; this card only guarantees <ACCEPTANCE_CRITERIA>, the rest is handled by <follow-up card>.`
- Theoretical defects with disproportionate cost, excludable only when all three hold (triggering needs an additional precondition already compromised; the consequence is minor or safer; fix complexity clearly exceeds the benefit): `<specific item> requires <premise> to trigger, has consequence <consequence>, and fixing it needs <cost>; not in this round's scope.`
- Environment verification gaps: `<command> could not be executed because of <reason>; covered by <substitute verification>; do not judge a problem on that basis.`
- Dropped backward compatibility: `The project has dropped all backward compatibility; do not raise compatibility, migration or fallback issues.`

Exclusions exclude only scope, never severity: "do not report high" or "report only doc issues" is forbidden. Exclusion reasons rest on facts (the introducing commit, the triggering premise, the user instruction); "not important" or "later" is forbidden. When other agents did the implementation, say so in the review context and point out the problems already fixed, so the reviewer re-checks similar half-changed states.

## Conclusions and Failure Handling

- A valid review requires: `PMQA`, as actually run per "Review Stages", has no open must-fix finding, every `blocking`, `high`, or `medium` item being `fixed` with verification or `rejected` with a factual basis or the user's quoted decision per "Main Agent Verification Duty"; and `Security`, as actually run, returned conclusions on the same commit with its findings decided by the user or recorded as timed out and ignored. Historical `PM`/`QA` and `CSA`/`Hacker` runs on open historical batches follow the same gates. A role exempt per profile or stage policy is recorded N/A in the delivery notes.
- A blocking, high, or medium finding judged "Unverifiable" counts as not passed: that stage is not released; send it to the user. A user decision not to handle it becomes a `rejected` disposition quoting the decision, kept on the unresolved list; a decision to handle it makes it confirmed.

**Unresolved Items Summary**

- When the loop ends (pass or termination), show the user every unresolved item of this round: unfixed `low`, `recommend`, and `suggest`; findings rejected or unverifiable; risks the user accepted and security findings timed out and ignored; roles not completed due to backend failure and roles that provided no `NON-BLOCKING` section. Each item states the source role, tier, problem, impact, and rationale. Reporting only "review passed" or "no blocking issues" is forbidden; when review was skipped for no whitelist hit, the notification rule in "Preconditions and Execution" suffices.
- The delivery notes and, for kanban tasks, the card carry the same items as one-line references per `KANDER-KANBAN-RULES.md` "Contract and Records": findings point at their disposition record; roles not completed, missing sections, and verification gaps point at the run's sidecar or error log with tier and status `N/A`, or, when no run exists, at the actual command log or `IMPLEMENTATION` record with the note "no run produced". The full text stays in the user report and the review artifacts.
- Round cap: when the same finding is still open after two fix rounds, or a batch has run three fix rounds, stop and report which finding is stuck, what was tried, and numbered options (`1. Accept the current state and record the item as unresolved`, `2. Change the contract`, `3. Continue with one more round`). Do not open a further round on your own.
- Review entry argument, authentication, precondition, or local environment errors are corrected first. Only when the same role invocation with correct arguments returns HTTP 5xx or an explicit service unavailable error 3 times in a row is it a persistent backend failure of that reviewer.

**Stage One Backend Failures**

- On a persistent `PMQA` backend failure, do not switch reviewer on your own: keep the branch and worktree, report the failed role, the actual error, and the completed stages, stop integration, and wait for the user to retry, reschedule, or explicitly designate another reviewer. When offering the switch, state its limit per "Reviewer Selection": with no successfully executed run of that role in the batch, only that role is re-run with the new reviewer; otherwise the switch takes effect from the next batch. Only when the user changes the whole reviewer set, the review base, or the task context are all stages voided and restarted from stage one.
- A persistent `Security` backend failure does not block the stage-two decision, but a role named `required` in the batch still needs a successful run before the batch can close: record it as "not completed due to backend failure" in the delivery notes and the report, let the enabled stage-one role decide the semantic conclusion, and wait for the user's retry, reschedule, or reviewer switch. Only a role recorded N/A needs no run. On a historical batch with several security roles, the remaining ones still produce their conclusions under the stage two rules.

## Controlled Plans, Author Dispositions, and Batch Closure

- Before completing an active card, establish a machine-readable execution review plan with `kander review plan <CWD> <absolute-plan.json>` naming the cycle, author and basis, worktree, language, members, ordered batch IDs, each batch base/target, and every role requirement in the exact two-key set (`required` or `N/A: <reason and applicable rule basis>`, resolved through the existing precedence). Disabled or inapplicable review needs explicit N/A records; empty indexes never imply exemption. Completed historical cards may remain `legacy-untracked` without an invented PASS.
- A single known batch may use a sealed plan. For incremental scheduling, create the plan unsealed once every member is in `working/` or `review/` and before the first batch runs, then `review extend-plan <CWD> <absolute-request.json>` with the expected plan revision appends each later batch after its predecessor closes and before its first run, or seals; it never adds or removes members, and a member already in `done/` or `archived/` cannot be extended. Seal only after every member is assigned. Unsealed plans and unclosed batches block done; never replace a plan to discard failed runs or reset the same cycle. Reclaim-induced cycle changes are rebound per `KANDER-KANBAN-RULES.md` "Review Evidence Completion Gate". Advance and extend-plan use the plan's exact CWD.
- New reports contain exactly one `kander-findings` fenced JSON object with mandatory `FINDINGS` and `NON_BLOCKING` arrays; each item has `id`, `tier`, `text`, and `evidence`, with IDs unique across both arrays, gate tiers only in FINDINGS and the rest only in NON_BLOCKING. A mechanical gate item may also contain `mechanical` with exactly one of `documentation`, `dead-code`, or `redundant-test`; other items omit it. Empty arrays explicitly mean no items; references in prose are not entries. The finding identity is `(run_id, finding_id)`; incremental carried items use explicit `lineage: {run_id, finding_id}` pointing to an actual item in the immediate predecessor.
- Old unstructured reports keep their originals. To use them, submit a complete human mapping through `review map-legacy <CWD> <absolute-map.json>` with author, basis, original report hash, structured items, and exact original line ranges and quotes; an invalid mapping is rejected without creating an immutable record. A missing ID search or empty mapping is not evidence of no findings, and new-schema reports cannot use this route.
- Attribute every item through `review assign <CWD> <absolute-assignment.json>` with run/batch, the assigning author and basis, and explicit item-to-task arrays; cross-card findings name every actual owner. An empty assignment is valid only for a parsed report with no items.
- Each executing agent verifies and submits only its own assigned findings through `review disposition <CWD> <absolute-record.json> <expected-card-revision>` while working, binding record/run/finding/batch/task IDs, author, original report hash and item text, status, factual basis, and the applicable fix SHA and verification. Revisions append a new record referencing the previous one. A successor OWNER may append an independently verified conclusion on the same task; former owners cannot submit after handoff, and the orchestrator never impersonates an author or overwrites records.
- Must-fix statuses are confirmed/fixed/rejected/unverifiable/waived (`deferred` is invalid). Confirmed and unverifiable block closure; rejected needs factual evidence or the user's quoted decision and stays on the unresolved list; fixed binds a later fix commit and actual verification. Non-blocking statuses are fixed/deferred/rejected, each with a basis, required for every assigned non-blocking item. Delegated placeholders are invalid.
- Waived is restricted to `Security` accepted-risk or kanban timeout rules (and historical `CSA`/`Hacker` records under the same rules), recording the policy and actual user decision, or the full notification basis with send and timeout times at least 15 minutes apart. A waiver is not PASS. `PMQA` must-fix items (and historical `PM`/`QA`), unverifiable items, and acceptance or integration authorization cannot be waived; a user decision on them is recorded as `rejected` with the quoted decision.
- `review aggregate <CWD> <batch-id>` validates all run publications and author originals and publishes a generated disposition view to every member, preserving author wording and attribution; members without findings need no notification merely to copy conclusions. The orchestrator may add separately attributed verification opinions to the closure request; they do not replace author dispositions.
- Mechanical-only fixes use `review advance <CWD> <absolute-request.json>` to CAS the batch target without a reviewer, with batch ID, expected revision, and the advance contract covering every in-batch delivery, at a clean worktree. The closing main agent supplies a separate `mechanical` assessment per exception, bound to the exact run/finding/task/author record, report hash, fix SHA, reviewer category (including an absent label), verification facts, and changed paths with their Git diff hash; a disposition's mechanical label alone cannot advance PASSED_AT, and existing labels are not proof. Non-mechanical must-fix items require a subsequent review covering the fix commit.
- Incremental `review --task ... --previous-run-id ...` loads the previous report, author records, and generated batch view automatically; manual context is a verbatim supplement. The prior semantic conclusion may be FAIL. Missing, inconsistent, foreign-member, wrong-language, or wrong-round evidence rejects before launch; retries retain the frozen input and reject changed supplemental text.
- Close through `review close <CWD> <absolute-request.json>` only after each required role has a valid conclusion, binding batch ID, expected revision, the SHA-256 of the exact aggregate JSON output, selected role run IDs, PASSED_AT, and verification basis, and linking failed attempts to their successful replacements. Failed execution is never PASS, and one successful role cannot stand in for missing roles. Closure verifies the final clean HEAD and the Git relationships among base, passing commits, fixes, and target, and publishes the final target with the complete disposition; partial publication is not closure. A closed batch accepts no new runs, target advances, or author changes, and the next batch uses its final target as base.
- `check` and `move done` share structural validation: pending author conclusions are legitimate progress, malformed existing evidence is an error, and structural validation never asserts integration. Integration authorization, delivery checks, rebase handling, and final Git verification remain mandatory. For a non-Git workflow with every role explicitly N/A, base and target_commit may both be N/A and the closure records Git as inapplicable; required roles always need real commit targets.
