# Review Rules

Loaded automatically and used to judge triggering only when `rules.review=true`; may also be read when the user explicitly requests the full review flow for the current task. Arguments and the read-only gate for a single `kander review` are in `KANDER-BASE-RULES.md`; that does not automatically enable the full flow.

- This module does not depend on the Git workflow. When `rules.git=false`, keep the user's branch and delivery flow. In that mode, determine the review base from the base recorded at task start or the range the user stated, and pin the full SHA before the first round. Never guess `develop` or create branches, and get the user's authorization before committing the review target.
- The Git mechanisms for rebase, merge-back, and cleanup mentioned here are read from `KANDER-GIT-RULES.md` only when `rules.git=true`; task group clauses apply only when `rules.task_groups=true`; the fixed completion report only when `rules.reporting=true`. Never load disabled modules through references.

## Reviewer Selection

- Reviewers are the eight built-in agents plus any configured agent that declares a review template (`args.review` together with `review.*`). The public review entry on all platforms is `kander review` under the command root (per `KANDER-LOADING-RULES.md` "Scope").
- Built-in isolation arguments live on each agent's definition. A custom reviewer's read-only posture is the definition author's responsibility; Kander still validates the result and isolates review-private directories. Apart from CLI and isolation arguments, every rule here is identical for all reviewers.

| reviewer | argument | CLI | isolation |
| -------- | -------- | --- | --------- |
| Codex | `codex` | `codex` | from the embedded definition |
| Claude | `claude` | `claude` | from the embedded definition |
| Grok | `grok` | `grok` | from the embedded definition |
| Cursor | `cursor` | `cursor-agent` | from the embedded definition |
| Pi | `pi` | `pi` | from the embedded definition |
| Devin | `devin` | `devin` | from the embedded definition |
| OpenCode | `opencode` | `opencode` | from the embedded definition |
| Kimi | `kimi` | `kimi` | from the embedded definition |
| custom | the agent name | `review.path` or `path` | author's responsibility |

**Reviewer Isolation**

- Codex runs a sandbox-enforced read-only shell inside the target worktree. Grok exposes only read and search tools. Claude runs with its full toolset minus `Edit` and `Write`. Pi uses its default tools and resource loading and relies on the prompt and post-run verification. Cursor approves tool calls without an interactive prompt and relies on the prompt and post-run verification. Devin auto-approves every tool under `--permission-mode dangerous` and has no enforced read-only sandbox, so read-only relies on the prompt and post-run verification. OpenCode runs under an `OPENCODE_PERMISSION` policy that denies every action except `read`/`glob`/`grep`/`list`/`external_directory`, plus the prompt and post-run verification. Kimi runs under a `--agent-file` profile that allowlists only `Read`/`Grep`/`Glob` and denies shell, write, subagent, and interaction tools, plus the prompt and post-run verification. Kander validates every reviewer's output and afterwards checks the Git-visible state of the target worktree, which does not detect writes outside it or to ignored paths inside it. Leftover processes and runtime cleanup failures reject the result.
- A UTF-8 bootstrap task file in the protected review runtime names a separate `review-contract.md`; the reviewer reads both completely and does not modify either file. The reviewer receives only a short instruction naming the bootstrap file (on stdin by default, or through `{instruction}` in argv when the definition says `review.stdin: none`). Never replace or loosen isolation arguments to unify implementations or accommodate Windows.
- `PMQA` and `Security` each select a reviewer from the first explicit source by precedence. The sources, highest first, are: the current user instruction; the nearest project `AGENTS.md` or `CLAUDE.md`; the user's global rules; the role configuration of the current scope (read by `kander review`; Codex when no configuration exists). Read rule files required for this judgment if not yet loaded; an unreadable one counts as unspecified.
- A bound card's `REVIEW_<ROLE>_AGENT`/`_MODEL`/`_EFFORT` pins (`snippets/commands/start.md` "Card Pins") are the user's recorded choice for that card. Task-bound `kander review` applies them over the configuration. Every card of one run must pin the role identically (pinned on one and unset on another is a conflict), and a different explicit reviewer fails. To use another reviewer, change the pin through a contract decision first.
- Different roles may use different reviewers. Fix re-runs and conclusion confirmation for one role keep the reviewer selected for that role in the current batch: the incremental chain requires the same reviewer, and closure rejects a successfully executed run not connected to the selected conclusion. A user-designated switch inside a batch is possible only while that role has no successfully executed run there (failed runs are bound through `resolved_failures`); otherwise it takes effect from the next batch.
- Below, "reviewer" means the one selected for the current role and "review entry" means `kander review` under the command root. "Main agent" is the agent that verifies findings and talks to the user. For a single card, it is the executing agent. For a task group, it is the orchestrator for batch scheduling, aggregation, and user decisions, and each executing agent for its own assigned findings (see `snippets/orchestrate-review.md` "Verification Split Between Orchestrator and Executing Agent"). "Delivery notes" means the card's `SUMMARY` (or `report.md` for a large card) and, for a non-kanban task, the final delivery message.

## Review Roles

The only runnable roles are `PMQA` (stage one) and `Security` (stage two), both defaulting to `auto`. `skip` means N/A unless a higher-precedence source requires the role; `required` means the role runs once review is triggered unless a higher-precedence source skips it. New invocations and two-key batches accept only these two names and their case aliases.

- `PMQA` first checks every acceptance criterion against the task contract, including explicit performance acceptance, then performs the architecture, correctness, regression, testing, and code-quality checks under "Goals and Boundaries". Explicit performance acceptance belongs to the contract portion; there is no general performance review. Finding IDs use the `PMQA-` prefix.
- `Security` first traces untrusted inputs across trust boundaries, then verifies realistic end-to-end exploit chains, reporting each root cause once and stating explicitly when no qualifying vulnerability was found. Finding IDs use the `Security-` prefix.
- Reviewer selection, models, large/small policies, incremental chains, verification, and failure handling apply equally to both roles. Never switch an existing run chain into a different role.
- New plans and batches use exactly the two keys `PMQA` and `Security`, each `required` or `N/A: <reason>`; a later batch appended to a historical four-role plan also uses the two-key form, and each batch freezes its own role set.

### Historical four-role evidence

`schema_version` stays 1 and existing plan, batch, run, index, manifest, closure, disposition, waiver, and recovery artifacts are never rewritten. Reading, same-ID retries, and close accept exact 2-key, 4-key (`PM`, `QA`, `CSA`, `Hacker`) and 6-key (those plus `PMQA` and `Security`) requirement objects. A run is admitted only when that batch's own requirements mark the role `required`, so an open four-key batch with `PM: required` may still complete a PM run and close. Waived stays valid for historical `CSA`/`Hacker` records and for `Security`, refused for `PMQA` and historical `PM`/`QA`.

## Goals and Boundaries

- Review goal: the plan or user goal is fully and correctly implemented, the flow is closed, and there are no directly related logic holes or regressions. Expanding the task scope is forbidden.
- `PMQA`'s quality portion first checks project architecture, module responsibilities, dependency direction, public boundaries, and integration patterns, then directly related correctness, regressions, testability, and code quality (single responsibility, readability, change locality, coupling and duplication, error and resource handling). It reports no performance findings; explicit performance acceptance is checked by the contract portion.
- `PMQA` limits non-generated code files to 1000 physical lines. It reports a gate finding only when a file added this round exceeds 1000 lines or a touched file that was at or under 1000 lines at the base exceeds 1000 now. A file already above 1000 lines at the base is not measured, and trimming unrelated lines to shrink such a file is itself a finding. These are the conditions of `KANDER-CODE-RULES.md` "Delivery Self-Check" item 2. Never sweep unrelated over-limit files.
- `PMQA` findings are limited to realistically reachable problems within the task context, existing contracts, or the code and module boundaries touched this round. Exhaustively stacked conditions, extreme edge cases, fabricated failures, or low-realism problems are forbidden; stop once task-related realistic risks are covered.
- Only handle problems introduced, aggravated, or masked by this task. Existing problems that are neither do not require fixes, do not block review or integration, and are not sent to the user, unless the user explicitly includes them.
- If a review result requires changing business logic, user flow, or external contracts beyond the user's explicit goal, explain the impact and let the user decide; implementing that change on a review conclusion alone is forbidden.
- For any issue needing the user's decision, the main agent explains each item in detail (problem, trigger condition, actual impact, suggested handling, the role that raised it) and gives numbered options. Abstract options are forbidden. Never lose source attribution when aggregating.
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
- When review is skipped because nothing hit, tell the user before integration "this task did not trigger the review whitelist and did not go through the review loop" and state the change scope (files changed, lines added and deleted). Silent skipping is forbidden. This is a notice, not a question: send it and continue into integration without waiting for a reply. If the user requests a review before the card completes, run the full flow; a later request is new work.

**Review Tool Unavailable**

- When the selected reviewer's CLI or the review entry is unavailable on this machine, tell the user "the <reviewer> CLI is not installed on this machine; review cannot be executed" (and that Kander needs reinstalling when the entry itself is missing). Keep the branch and worktree, and give numbered options `1. Retry after installing that CLI`, `2. Restart review with another reviewer`, `3. Skip this review and integrate directly (risk borne by the user; record "not reviewed" in the delivery notes)`, `4. Stop integration`. For a task-bound review, option 3 exists only before the review plan records that batch's role as `required`. After plan creation the requirement is frozen even when no run has started, closure needs a successful run, and the choice is between 1, 2, and 4. For an unbound review, option 3 exists only before its first run.
- Skipping on your own or switching reviewer without the user's explicit designation is forbidden.
- Before review, commit all task changes since the review base grouped by concern, keeping the worktree free of uncommitted or untracked files.
- Before review, the author completes `KANDER-CODE-RULES.md` "Delivery Self-Check" and records it when `rules.code=true`. When the code module is off, the author at least runs `kander check delivery --base <review-base>` (or records an equivalent `git diff --check` plus the line-count candidates), verifies the targeted tests ran at the final commit, and records that. Findings the self-check should have caught are ordinary findings: mechanical ones are `[mechanical]` items closed per "Fixed archiving", the rest follow the normal fix round. Record the self-check failure on the card; it is never an extra round or a return outside the review flow.

**Review Base**

- With the Git module enabled, the base of a dedicated task branch is the full `develop` SHA most recently used to create it from `develop` or rebase it onto `develop`. Fixes under the same base do not change it.
- The base is frozen once review starts; never chase `develop` by rebasing before the loop ends. After the integration rebase, the base moves to the new base point, and whether to re-review follows the one-time gate in `snippets/git-integrate.md` "Integration and Cleanup".

A standard review has two stages. Whether each role runs is resolved per "Review Stages" and its focus per "Review Profiles"; skipped roles are marked N/A. Stage one is `PMQA`; stage two is `Security` after stage one passes. Non-mechanical fixes receive incremental re-review by the role that reported them. Security fixes do not reopen PMQA:

```text
[1] PMQA   First round. Contract portion: whether the implementation fully meets the task context;
                quality portion: functional correctness, regressions, tests and code quality; a skipped role is marked N/A and passes directly
     |  Must-fix findings --> fix and commit (same base, new HEAD)
     |                          --> incremental re-review --> back to [1] until PMQA passes
     |  Only mechanical must-fix items left --> fix and commit, main agent verifies mechanically --> PMQA passes on the new HEAD without re-running
     v  Enabled stage-one role passes
[2] Security  Decided by stage policy and trigger conditions; required overrides trigger conditions; when it does not run, mark N/A and pass directly
     |  Qualified finding --> user decision --> 1 fix and commit --> Security incremental re-review [2]; do not return to PMQA
     |                           --> 2 confirm pass (record accepted risk and rationale in the delivery notes)
     |                           --> 3 stop integration (keep branch and worktree)
     |                           --> kanban task with no decision for 15 minutes: timed-out and ignored --> stage ends
     v  Stage ends
   Review complete --> show all unresolved items to the user --> enter the integration flow
```

- Incremental re-review: for the same role under the same base, the second round and later are always incremental, never full. `reviewed-commit` is the commit that role reviewed last round, strictly between base and new HEAD; task-bound invocations derive it from `--previous-run-id` (an explicit value must match), unbound ones supply it positionally. Only `reviewed-commit..commit` is new material: the reviewer verifies item by item that last round's findings are closed and reports only problems introduced, aggravated, or masked by the fix, or breakage of the touched requirements. Unchanged code is accepted; re-auditing it is forbidden.
- The review context includes every finding from the previous round with the main agent's handling conclusion. Task-bound runs get the original report, author records, and batch view automatically, with positional review-context as a delimited supplement. Unbound runs get the full context from the caller (fix commit and verification for confirmed items, basis for rejected items, the user's decision for unverifiable ones). The reviewer only checks these facts; the main agent must never omit or rewrite last round's list.
- The first round passes no `reviewed-commit`. A role that PASSed is not re-run (see "Carrying Conclusions Forward"). A base or task context change means a new batch where that role restarts with a full first round. The main agent's verification duty is not reduced by incremental re-review.
- A fix round for a role that already reported on this base is invalid unless it runs in incremental mode. The tool does not refuse another full first round, but closure rejects a successfully executed run not connected to the selected conclusion chain, so the batch could no longer close. The only later full first rounds are the retry of a failed run (bound through `resolved_failures`) and the first round of a new batch.
- Severity tiers: every role labels its output with the six tiers below. Structured findings require a tier. For a readable report awaiting interpretation, the receiving main agent records the supported tier and its basis through `review interpret`; legacy reports retain `map-legacy`. Never infer no findings from a missing tier or malformed format.

| Tier        | Meaning                                                                | Must fix |
| ----------- | ---------------------------------------------------------------------- | -------- |
| `blocking`  | Goal not met, or data corruption, security failure, main flow unusable | Yes      |
| `high`      | Certain failure or regression on a common path, clear trigger          | Yes      |
| `medium`    | Fails under specific conditions, or real contract/boundary/error defect | Yes      |
| `low`       | A real defect, but rarely triggered with negligible consequences       | No       |
| `recommend` | Not a defect, but should change per project rules or conventions       | No       |
| `suggest`   | Optional improvement; the trade-off is up to the owner                 | No       |

- Fixed archiving: the following are never downgraded for rare triggering or minor consequences and are always at least `medium`, whichever role finds them. They are: documentation or code comments inconsistent with the implementation; dead code (unreachable, or with no calls or references); redundant tests (duplicated coverage, or assertions unrelated to the behavior under test).
- Mechanical must-fix items are those three categories. In the structured item, the reviewer sets `mechanical` to exactly `documentation`, `dead-code`, or `redundant-test`; `[mechanical]` in human-readable prose is only a display tag and is not the machine binding. They are dispatched with the gate findings, fixed and committed, and closed by the main agent's mechanical evidence (sentence-by-sentence comment comparison, reference search output, or test lists). A round whose confirmed must-fix items are all mechanical never re-runs the reviewer; that role passes on the new HEAD after mechanical verification. When mixed with non-mechanical items, the incremental re-review covers only the non-mechanical ones and states that the mechanical ones were closed by the caller. When the reviewer omits the field, the main agent may independently classify the item per the definition and records the absent reviewer label; classifying non-mechanical defects as mechanical to skip re-review is forbidden.
- Tiers use the English identifiers above. `blocking`, `high`, `medium` go in the report's `FINDINGS` array; the rest in `NON_BLOCKING`. Both arrays are required for automatic extraction and for a submitted interpretation. A readable report missing the machine format awaits `review interpret` instead of a reviewer rerun. Read the entire report; never treat a missing section as an explicit zero-finding conclusion. Record any missing non-blocking section on the unresolved list.
- Must-fix threshold: only `blocking`, `high`, and `medium` verified and accepted by the main agent must be fixed. `low`, `recommend`, and `suggest` never block review or integration and never open a fix round, but closure still requires the assigned author's disposition (`fixed`, `deferred`, or `rejected`, each with a basis) for every one of them. They therefore travel to the author in a bound `--kind fix` dispatch whose evidence references every assigned `FINDINGS` and `NON_BLOCKING` item for that card (either set alone, or both). The dispatch message lists each item with role and tier, marking non-blocking ones "disposition only"; existing author originals on the lineage are included. Non-blocking references never make the round a reviewer re-run: the author completes a disposition-only round with `move review` on the unchanged delivery SHA. They stay on the card's unresolved list with the author's disposition for the next delivery or a follow-up card. A dispatch that asks the author to "triage" without listing items with role and tier is forbidden.
- In stage two, only `blocking`, `high`, and `medium` findings returned by an actually running `Security` (or historical `CSA`/`Hacker`) and confirmed by the main agent go to the user for decision. Each item lists the problem, impact, fix method, and at least `1. Fix and re-review`, `2. Confirm pass and accept the risk`, `3. Stop integration`. Other tiers and pure defense-in-depth advisories go straight to the unresolved list.
- The security finding timeout applies only to kanban tasks. Each item is timed independently from when its full information and options reach the user; items sent together share the send time, and a partial answer does not stop the other timers. The timeout covers `medium` findings only; a confirmed `blocking` or `high` finding waits for the user's decision, and the card stays in `review/` with the question recorded until it arrives. For a `medium` finding with no explicit decision after 15 minutes, mark it "timed out and ignored", record role, tier, problem, impact, send time, timeout time, and rationale, and continue; this is not `PASS`, confirmation, or risk acceptance. A decision that arrives while the batch is still open is honored in that batch. One that arrives after the batch closed never reopens it. Before integration, stop and handle it as the user directs, in a new batch when the plan is unsealed and otherwise as new work under a new card. After integration it is new work, and nothing already on `develop` is reverted without the user's instruction. Non-kanban tasks, `PMQA` findings (and historical `PM`/`QA`), unverifiable items, contract or owner changes, and acceptance or integration confirmation requested by the user are never skipped by timeout.

**Invocation Arguments**

- With no reviewer specified: `kander review <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]`, dispatched by configuration; with one: `kander review <reviewer> <CWD> ...`. A global install uses `kander` on PATH or the absolute path of the selected executable (in PowerShell `& "C:\path\to\kander.exe" review ...`); never assume a copy in `~/.local/bin`. A project install uses the absolute entry under its command root.
- Automation launches the selected `kander` executable through a process API argv array, passing `review` and each argument separately. Batch files and Windows PowerShell 5 cannot guarantee lossless argv, so data containing `&|<>^%!`, quotes, or boundary backslashes is never passed through `.cmd` or concatenated shell strings. Bypassing Kander to call the reviewer CLI directly is forbidden.
- `CWD` is the absolute path of the target worktree; all commit arguments are full SHAs; the base is an ancestor of the commit, `HEAD` equals the commit, and there are no uncommitted or untracked files.
- Only incremental re-reviews use `reviewed-commit`, strictly between base and commit. Task-bound runs derive it from `--previous-run-id` and preserve the prior report and author conclusions automatically; unbound incremental runs require positional reviewed-commit and a nonempty review-context with the complete prior finding list and conclusions.

**Carrying Conclusions Forward**

- All roles of a full review share `CWD`, base, and task context; historical `CSA` and `Hacker` triggered together use the same commit. The task context is the authoritative requirement contract: a string for short tasks, a readable absolute spec path for long tasks. The first run of a batch freezes the task context. Publishing a run appends the review index to the live `spec.md`, so every later run of the same batch, whether another role or an incremental round, passes the frozen snapshot `reviews/<run-id>/task-context.md` from a member card instead of the live spec. Put only execution and fix evidence in the supplemental review context. A change to `GOAL`, `USER_DECISIONS`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, `THREAT_MODEL`, or `OUT_OF_SCOPE` needs a new batch.
- Under the same base, Security fixes receive only Security incremental re-review; they do not reopen a passed `PMQA` or require a skipped `PMQA` to run. Necessary verification and the mechanical-only exception still apply. If a later, independently required `PMQA` fix substantively changes already-reviewed security code, the enabled security role re-reviews that changed scope. Historical four-role batches likewise do not reopen passed `PM` or `QA` solely because of a security fix; their frozen role requirements and evidence remain unchanged.
- Roles may pass on different commits; within the same base, the final commit must descend from every passing commit.
- Non-integration changes such as switching base, recreating the branch, or a forced rebase during review invalidate all conclusions. Because every batch with reviewer runs must still close, first close the open batch on its current target: the remaining required roles run there, and superseded findings are disposed as `rejected` with the user's quoted decision. A task context change on the same base chain then continues in a new batch of the same, still unsealed plan, starting at the previous closed target and reviewing only the work after it. When the user wants the closed range re-reviewed under the new contract, or when the base or the history changed, continue as new work under new cards and a new plan per `snippets/orchestrate-review.md` "Group-Level Review for Task Groups". Report the wasted rounds either way.
- A rebase during integration caused by `develop` advancing carries conclusions forward per the one-time gate in `KANDER-GIT-RULES.md`; the pre-rewrite SHA need not remain an ancestor.

**Report Files and Stable Identity**

- Without task binding, save each role's stdout in a separate report in a private temporary directory outside the target worktree; clean it after the round passes, terminates, or the stage-two decision finishes, explaining first when retaining diagnostics.
- For kanban reviews, pass every reviewed card with repeated `--task <id>` before CWD, together with `--batch-id <id>`. Directory cards in working/review and a common report language are required; legacy file cards first follow the init maintenance protocol.
- A batch is named in the review plan before its first run (at plan creation, or by `extend-plan` afterwards) per `snippets/review-evidence-gate.md` "Review Evidence Completion Gate". The first run supplies `--requirements-file`, an absolute JSON path naming exactly `PMQA` and `Security` with `required` or `N/A: <reason>`, resolved through the precedence and stage rules and identical to the plan entry. Requirements, members, base, task-context bytes, and report language are fixed for the batch. If the live spec changes or moves, later roles, fix rounds, and retries pass the absolute path to the archived `task-context.md` (available under `inputs/task-context.md` in the run control directory before publication) to retain the original bytes and batch ID.
- Use a distinct `--run-id` per actual reviewer invocation; omit it to get a random ID on stderr and record it. Run and batch IDs are unique across the whole board, not per card, and an ID reused with a different batch or member set is rejected before the reviewer starts. Prefer the generated run ID; when you must choose an ID yourself, prefix it with the task ID (or the task-group ID for a batch), for example `<task-id>-pmqa-1`, and never use short generic names such as `pmqa-1` or `s2-pmqa-1`. Different roles on one commit share the batch ID with different run IDs; timestamps establish no identity or order. A same-ID retry with identical inputs never launches the reviewer again: it verifies existing evidence and fills missing card publications, and if the original gate process ended before finalization, recovery marks the run interrupted and preserves partial raw output.
- A task-bound incremental invocation includes `--previous-run-id`; the predecessor must be fully published for the same batch, base, role, and reviewer. Caller context is a verbatim supplement, never a replacement.
- Keep the batch ID for fixes. To advance its target, pass an absolute `--advance-file` JSON with `previous_target`, `target`, a nonempty `reason`, `deliveries`, and optional `foreign_commits`. `deliveries` maps each batch-member commit in that exact range to its task ID. `foreign_commits` maps every outside commit in the range to a nonempty reason. Every commit in the range must appear in exactly one map; the command verifies range, membership, reasons, and uniqueness before applying a compare-and-swap. Never misrepresent an outside delivery as a batch task fix. Outstanding executions or incomplete publications settle first.
- Each card retains complete immutable originals under `reviews/<run_id>/` (input context, raw output, logs, any valid report, sidecar, manifest), also for failed launches after intent creation, timeouts, invalid output, leftover processes, and cleanup failures; preflight failures invent no run. The report language freezes from card LANGUAGE, falling back to configuration only when absent. The tool appends one JSON index line per run in REVIEWS. Never derive this index from prose, paste reports into the body, edit originals through update, or remove archived evidence. Only Kander's own cleanup after a move into `archived/` removes it (`KANDER-KANBAN-RULES.md` "Archived Review Evidence").
- A new, successfully executed review with nonempty readable text but an invalid findings format remains execution-successful and awaits interpretation. The receiver reads the complete original and submits `review interpret` with the run ID, exact report hash, author, basis, completeness attestation, structured findings, and exact original line ranges/quotes. Keep the original report and execution facts unchanged; never rerun a reviewer merely to repair Markdown. `aggregate` shows `findings_status: pending`, `progress` lists `interpretation:<run-id>`, and assignment, incremental consumption, close, and done remain blocked until the interpretation is valid.
- Valid structured findings retain automatic extraction. Invalid explicit predecessor relationships still fail validation; interpreter submissions validate those same relationships. Execution failures (including empty output, timeout, process collection, worktree changes, and runtime cleanup), interrupted runs, and historical format failures never become PASS through interpretation. Retry an actual failed run in the same batch with a new run ID: a failed first round retries as a full invocation; a failed incremental round reuses the last valid `--previous-run-id`. Never select failed evidence as predecessor, repeat an already committed advance, or rewrite original failures; bind failed attempts through `resolved_failures`.
- A run whose reviewer process ended before producing a valid report (a signal exit such as 143, a launcher failure, a kill before output) is archived as failed or interrupted. Retry it once immediately in the same batch under the previous bullet, without reporting to the user first. Only when that retry also ends without a report is it a persistent backend failure per "Stage One Backend Failures".
- `execution_status=ok`, readable stdout, and a zero exit do not imply semantic PASS; interpret the report through this workflow. Failed or interrupted runs never establish a passing role.
- Pending reviews allow ordinary updates and moves between working and review; finish publication before any terminal move. Each card publication is atomic, and a cross-card failure preserves successful publications, reports each failed card, and exits nonzero. Retry the same run ID after resolving the error, obeying init's maintenance preconditions when init is required. An incomplete publication never counts toward batch completion, and `kander check <task-id>` still diagnoses it; never move a done card back or bypass controlled writes to repair it.
- `kander check` validates intents, manifests, indexes, original hashes, language, membership, and predecessor relations; it infers no semantic PASS and closes no batch. Reviewer isolation and end-of-run worktree verification always apply.

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
- Security trigger conditions (in `auto` mode): `Security` runs when the change involves untrusted input, authentication and authorization, credentials, cryptography, network protocols, remote execution, file writes decided by untrusted input, install/update or release integrity. `Security` also runs when the external attack surface is added or substantively changed, when a dedicated security review is being performed, or when the user requests it. `required` overrides these triggers and `skip` still means N/A.
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

## task context and review context Templates

- `OUT_OF_SCOPE` is the risk-bounded stop boundary of the review, judged category by category with reasons. It has four categories: problems that existed before the change; concurrency interleaving, cross-platform, and security hardening; syncing shared contracts, public APIs, and architecture docs; adjacent features and later phases. Categories not required by `ACCEPTANCE_CRITERIA` are stated as excluded; categories this card owns are stated as included. A single generic exclusion does not complete the contract; the card-creating agent completes it before `pick`. Reviewer and main agent both use this list to judge overreach; overstepping findings are recorded as unresolved with a follow-up card suggested.

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

- A valid review requires two conditions. First, `PMQA`, as actually run per "Review Stages", has no open must-fix finding: every `blocking`, `high`, or `medium` item is `fixed` with verification or `rejected` with a factual basis or the user's quoted decision per `snippets/fix-round.md` "Main Agent Verification Duty". Second, `Security`, as actually run, has a valid conclusion with its findings decided by the user or recorded as timed out and ignored. Roles may pass on different commits per "Carrying Conclusions Forward"; Security-only fixes retain the earlier valid PMQA conclusion, and the final target must descend from every passing commit. Historical `PM`/`QA` and `CSA`/`Hacker` runs on open historical batches follow the same gates. A role exempt per profile or stage policy is recorded N/A in the delivery notes.
- A blocking, high, or medium finding judged "Unverifiable" counts as not passed: that stage is not released; send it to the user. A user decision not to handle it becomes a `rejected` disposition quoting the decision, kept on the unresolved list; a decision to handle it makes it confirmed.

**Unresolved Items Summary**

- When the loop ends (pass or termination), show the user every unresolved item of this round. The items are: unfixed `low`, `recommend`, and `suggest`; findings rejected or unverifiable; risks the user accepted and security findings timed out and ignored; roles not completed due to backend failure and roles that provided no `NON-BLOCKING` section. Each item states the source role, tier, problem, impact, and rationale. Reporting only "review passed" or "no blocking issues" is forbidden; when review was skipped for no whitelist hit, the notification rule in "Preconditions and Execution" suffices.
- The delivery notes and, for kanban tasks, the card carry the same items as one-line references per `KANDER-KANBAN-RULES.md` "Contract and Records". Findings point at their disposition record. Roles not completed, missing sections, and verification gaps point at the run's sidecar or error log with tier and status `N/A`, or, when no run exists, at the actual command log or `IMPLEMENTATION` record with the note "no run produced". The full text stays in the user report and the review artifacts.
- Round cap: when the same finding is still open after two fix rounds, or a batch has run three fix rounds, stop and report which finding is stuck, what was tried, and numbered options (`1. Accept the current state and record the item as unresolved`, `2. Change the contract`, `3. Continue with one more round`). Never open a further round on your own.
- Review entry argument, authentication, precondition, or local environment errors are corrected first. Only when the same role invocation with correct arguments returns HTTP 5xx or an explicit service unavailable error 3 times in a row is it a persistent backend failure of that reviewer.

**Stage One Backend Failures**

- On a persistent `PMQA` backend failure, never switch reviewer on your own. Keep the branch and worktree, report the failed role, the actual error, and the completed stages, stop integration, and wait for the user to retry, reschedule, or explicitly designate another reviewer. When offering the switch, state its limit per "Reviewer Selection": with no successfully executed run of that role in the batch, only that role is re-run with the new reviewer; otherwise the switch takes effect from the next batch. When that role already has a successfully executed run with open must-fix findings and the reviewer cannot come back, the batch can neither switch reviewer nor close. Say exactly that to the user, with the batch ID, the role, the open finding IDs, and the fix commits awaiting re-review, and wait. How to restore the reviewer or settle those findings is the user's decision, never inferred or worked around. Only when the user changes the whole reviewer set, the review base, or the task context are all stages voided and restarted from stage one.
- A persistent `Security` backend failure does not block the stage-two decision, but a role named `required` in the batch still needs a successful run before the batch can close. Record it as "not completed due to backend failure" in the delivery notes and the report, let the enabled stage-one role decide the semantic conclusion, and wait for the user's retry, reschedule, or reviewer switch. Only a role recorded N/A needs no run. On a historical batch with several security roles, the remaining ones still produce their conclusions under the stage two rules.

## Phase Rules

- Verifying and disposing assigned findings (the main agent's duty, fix rounds): `snippets/fix-round.md`.
- Group-level review, controlled plans and batch closure, and the evidence command JSON: `snippets/orchestrate-review.md`.
