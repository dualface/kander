# Review Rules

Loaded automatically and used to judge triggering only when `rules.review=true`.

May also be read when the user explicitly requests the full review flow for the current task.

Precedence and loading flow are in `KANDER-AGENTS.md`.

Arguments and the read-only gate for a single `kander review` are in `KANDER-BASE-RULES.md`; that does not automatically enable the full flow.

- This module does not depend on the Git workflow.

  When `rules.git=false`, keep the user's branch and delivery flow, and determine the review base from the base recorded at task start or the range the user explicitly stated.

  Pin the full SHA before the first round; do not guess `develop` or create branches.

  Committing the review target requires the user's authorization.

- The Kander Git mechanisms for rebase, merge-back and cleanup mentioned in this file are read from `KANDER-GIT-RULES.md` only when `rules.git=true`; when it is off, return to the user's flow once the review completes.
- Task group clauses apply only when `rules.task_groups=true`; the fixed completion report applies only when `rules.reporting=true`; do not load disabled modules through references.

## Reviewer Selection

- Four reviewers are supported: Codex, Claude, Grok and Cursor.

  The public review entry on all platforms is `kander review` under the command root, which enters the single gate implementation.

  On Windows, prefer the reviewer `.exe`.

  When only `.cmd`/`.bat` exists, launch through an explicit `cmd.exe /d /s /v:off /c` and the argument encoding of the four reviewer adapter layers.

  This is not a general invocation contract for arbitrary batch scripts.

  Apart from the CLI and isolation arguments in the table below, every rule in this file is identical for all four.

  Select the command entry per `KANDER-AGENTS.md` "Scope".

| reviewer | agent argument | CLI            | Isolation arguments of the entry                                                                                                            |
| -------- | -------------- | -------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Codex    | `codex`        | `codex`        | `--sandbox read-only`, `--ephemeral`                                                                                                        |
| Claude   | `claude`       | `claude`       | `--permission-mode plan`, `--tools Read,Grep,Glob`, `--safe-mode`, `--no-session-persistence`                                               |
| Grok     | `grok`         | `grok`         | `--sandbox read-only`, `--no-memory`, `--no-subagents`                                                                                      |
| Cursor   | `cursor`       | `cursor-agent` | `--print --output-format json --trust`; `CURSOR_CONFIG_DIR` and `CURSOR_DATA_DIR` point to this round's isolated runtime; no `--sandbox` / `--mode ask` |

**Reviewer Isolation**

- Codex, Claude and Grok use read-only isolation: Codex runs a read-only shell inside the target worktree.
- Claude/Grok run in an out-of-tree runtime with only read and search tools exposed.
- Cursor only isolates configuration and session into the runtime; read-only relies on the prompt and post-run worktree verification, with no upfront blocking and no detection of out-of-tree writes.
- On all platforms the full prompt is written to a UTF-8 task file; the reviewer receives only a short instruction with the path.
- Grok keeps `--prompt-file`.
- The task file does not check or tighten POSIX permissions or Windows ACLs; when it lives in the review runtime it is still protected by that boundary.
- After the reviewer exits, the process group must be forcibly reaped; failure to do so is a review failure.
- Currently only Cursor ships helper processes that do not wait for wrap-up; for it, only detached descendants that left the parent chain count as leftovers and cause the result to be rejected; ordinary child processes do not cause rejection.
- For other reviewers, a non-empty process group at the moment of exit causes rejection.
- Worktree verification looks only at Git-visible state; writes to paths excluded by `.gitignore` do not fail the review.
- Claude's out-of-tree spec is first snapshotted into a runtime exclusive to the current user; the original parent directory is not authorized.
- The runtime follows the permission, pinned handle and bounded no-follow cleanup contract of the minimal tool protocol: POSIX `0700`.
- Windows `CREATE_NEW`, collision-safe retry with random names, and a protected DACL exclusive to the current user.
- The root handle is held continuously until worktree verification and cleanup finish.
- Cleanup failures such as reparse points, files in use or budget exhaustion all reject the result; silent leftovers are forbidden.
- Do not replace or loosen isolation arguments to unify implementations or accommodate Windows.

- `PM`, `CSA`, `Hacker` and `QA` each select a reviewer, taking the first explicit source by precedence:
  1. The current user instruction.
  2. The nearest project `AGENTS.md` or `CLAUDE.md`.
  3. The user's global rules.
  4. The role configuration of the current scope.

  When the first three tiers do not specify, `kander review` under the command root reads tier (4); when no configuration exists, Codex.

  Read rule files required for this judgment if not yet loaded; an unreadable one counts as unspecified.

- Different roles may use different reviewers. Fix re-runs and conclusion confirmation for the same role must keep using the reviewer selected for that role this round; switching midway voids that role's existing conclusions and re-runs that stage, while other roles that already passed are not voided.
- Below, "reviewer" and "reviewer CLI" both mean the one selected for the current review role; "review entry" means `kander review` under the command root.

## Goals and Boundaries

- Review goal: the plan or user goal is fully and correctly implemented, the flow is closed, and there are no directly related logic holes or regressions; expanding the task scope is forbidden.
- `QA` first checks project architecture, module responsibilities, dependency direction, public boundaries and integration patterns, then directly related correctness, regressions, testability and code quality (single responsibility, readability, change locality, coupling and duplication, error and resource handling).

  `QA` does not do general performance review or report performance findings.

  Explicit performance acceptance is checked by `PM` against the contract.

- `QA` limits non-generated code files to 1000 physical lines, and reports a gate finding only when a file added this round exceeds 1000 lines, the base was at most 1000 lines and this round exceeds the limit, or a task-related existing over-limit file has a net line increase.

  Do not sweep unrelated over-limit files.

  Not triggered when the final line count is not above the base; the same holds for patches that replace imports, adapters or glue code.

- `QA` findings are limited to realistically reachable problems within the task context, existing contracts, or the code and module boundaries touched this round. Exhaustively stacked conditions, extreme edge cases, fabricated failures or low-realism problems are forbidden; stop once task-related realistic risks are covered, and do not expand into a repository quality sweep.
- Only handle problems introduced, aggravated or masked by this task. Existing problems that are neither aggravated nor masked do not require fixes, do not block review or integration, and are not sent to the user for decision; unless the user explicitly includes them in this round.
- If a review result requires changing business logic, user flow or external contracts beyond the user's explicit goal, explain the impact to the user and let the user decide; choosing or implementing that change on the basis of a review conclusion alone is forbidden.
- For any issue needing the user's attention or decision, the main agent explains each item in detail: the problem, trigger condition, actual impact and suggested handling, marks the review role that raised it, then gives numbered options; giving only abstract options is forbidden, and source attribution must not be lost when aggregating results from multiple agents.
- Runtime environment problems that affect verification credibility, delivery usability or user data safety must be explicitly reported to the user.

  Unless the change directly introduces, aggravates or masks that problem, or the user explicitly asks to handle it, record it only as a verification fact; judging it alone as blocking, high or medium is forbidden, as is demanding unrelated fixes on that basis.

## Preconditions and Execution

- Review triggering uses a whitelist: a task enters review only when it hits any entry below; no hit means no automatic review. When the user explicitly requests a review, enter unconditionally, without whitelist restriction.
  1. An explicit feature development or bug fix task.
  2. Code changes beyond the small-change threshold: more than 1 code file changed relative to the review base, or added plus deleted lines exceeding 10. Use `git diff --numstat <base>..<commit>` as the source of truth; count only code files, excluding `*.md` and `*.markdown`; binary files are always exempt.
  3. Security-sensitive changes, regardless of size: authentication and authorization, credentials, cryptography, permission checks, sandbox and isolation arguments, or other logic directly affecting a security boundary.
  4. External contract changes: adding, modifying or removing CLI arguments, configuration schema, persisted data formats, network or plugin APIs.
  5. Irreversible or cross-data operations: data migration scripts, bulk rewriting or deleting of user data, and other operations that cannot be recovered by a simple rollback.
  6. Changes to build, install, release, CI or the review gate flow itself.
  7. Dependency changes: adding, upgrading or removing external dependencies.
  8. Test removal or loosening: deleting tests, skipping tests, relaxing assertions or reducing coverage.
  9. Deleting or renaming public modules, or cross-module refactoring.
- The whitelist is judged by the task's actual changes and the user's wording, not by the task title or labels; rephrasing a task that hits an entry into a non-hitting type to bypass review is forbidden.
- When review is skipped because no whitelist entry was hit, explicitly tell the user before integration "this task did not trigger the review whitelist and did not go through the review loop", and state the change scope (which files were changed, lines added and deleted). Silent skipping is forbidden. If the user then requests a review, run the full flow as usual.

**Review Tool Unavailable**

- When the selected reviewer's CLI or the review entry for the current platform is unavailable on this machine, review cannot run: explicitly tell the user "the <reviewer> CLI is not installed on this machine; review cannot be executed", keep the branch and worktree, and give numbered options `1. Retry after installing that CLI`, `2. Restart review with another reviewer`, `3. Skip this review and integrate directly (risk borne by the user; record "not reviewed" in the delivery notes)`, `4. Stop integration`.
- When the review entry is missing, also state that Kander needs to be reinstalled.
- Skipping on your own is forbidden; switching reviewer without the user's explicit designation is forbidden.

- Before review, commit all task changes since the review base grouped by concern, keeping the worktree free of uncommitted or untracked files.

**Review Base**

- When the Git module is enabled: the base of a dedicated task branch is the full `develop` SHA most recently used to create it from `develop` or rebase it onto `develop`.
- Fixes under the same base do not change the base.
- The base is frozen once review starts; do not chase `develop` by rebasing or update the base before the loop ends.
- Advancement of `develop` is left to integration.
- After the integration rebase, the base is updated to the new base point; whether to re-review follows the one-time gate in `KANDER-GIT-RULES.md` "Integration and Cleanup".

A standard review has two stages.

Whether each role runs is resolved first per "Review Stages"; skipped roles are marked N/A.

`PM` and `QA` run their first round in parallel in stage one on the same commit; the security roles enter stage two after both pass.

Review focus is still determined per "Review Profiles".

Stage transitions follow this diagram; each fix triggers an incremental re-review only for the roles that raised the must-fix items:

```text
[1] PM || QA    First round in parallel on the same commit. PM checks whether the implementation fully meets the task context;
                QA checks functional correctness, regressions, tests and code quality; exempt or skipped roles are marked N/A and pass directly
     |  Any role has must-fix findings --> merge both roles' must-fix items into one fix and commit (same base, new HEAD)
     |                          --> incremental re-review only for roles with must-fix items (in parallel if both) --> back to [1] until both pass
     |  A role has only mechanical must-fix items left --> fix and commit, main agent verifies mechanically --> that role passes on the new HEAD without re-running
     |  A role that already passed is not re-run because of another role's fix (see "Carrying Conclusions Forward")
     v  PM and QA both pass
[2] CSA/Hacker  Decided by stage policy and trigger conditions; required overrides trigger conditions; may run in parallel when both trigger; when neither runs, mark N/A and pass directly
     |  Qualified finding --> user decision --> 1 fix and commit --> incremental re-review [2]; fix scope is incrementally re-reviewed by QA, and by PM too when functional behavior changes
     |                                  Subsequent QA/PM fixes substantively change security-related code --> corresponding security role incrementally re-reviews [2], until all parties pass
     |                           --> 2 confirm pass (record accepted risk and rationale in the delivery notes)
     |                           --> 3 stop integration (keep branch and worktree)
     |                           --> kanban task with no decision for 15 minutes: timed-out and ignored --> stage ends
     v  Stage ends
   Review complete --> show all unresolved items to the user --> enter the integration flow
```

Incremental re-review: for the same role under the same base, the second round and later are always incremental re-reviews, never full re-reviews.

- The incremental invocation passes the commit that role reviewed last round as `reviewed-commit`, which must lie strictly between the base and the new HEAD.

  Only `reviewed-commit..commit` is new material.

  The reviewer verifies item by item that last round's findings are closed, and reports only problems introduced, aggravated or masked by the fix, or breakage of the touched requirements.

  Unchanged code is treated as accepted; re-auditing it or expanding the scope is forbidden.

- The review context must list every finding from the previous round together with the main agent's handling conclusion: for confirmed items, the fix commit and verification result; for rejected items, the verification basis; for unverifiable items, the user's decision.

  The reviewer only checks these facts; the main agent must not omit or rewrite last round's list.

- The first round does not pass `reviewed-commit`. Once that role has PASSed it is not re-run (see "Carrying Conclusions Forward"). A base change, a mid-way reviewer switch or a task context change invalidates the incremental chain, and that role restarts from a full first round.
- The main agent's verification duty is not reduced by incremental re-review: every conclusion of an incremental re-review is likewise verified item by item.

- Severity tiers: the output of every role is labeled with the six tiers in the table below; a missing tier makes the conclusion invalid, and the main agent assigns one.

| Tier        | Meaning                                                                | Must fix |
| ----------- | ---------------------------------------------------------------------- | -------- |
| `blocking`  | Goal not met, or data corruption, security failure, main flow unusable | Yes      |
| `high`      | Certain failure or regression on a common path, clear trigger          | Yes      |
| `medium`    | Fails under specific conditions, or real contract/boundary/error defect | Yes      |
| `low`       | A real defect, but rarely triggered with negligible consequences       | No       |
| `recommend` | Not a defect, but should change per project rules or conventions       | No       |
| `suggest`   | Optional improvement; the trade-off is up to the owner                 | No       |

- Fixed archiving: the following problems are not downgraded for rare triggering or minor consequences and are always judged at least `medium`, whichever role finds them:
  - Documentation or code comments inconsistent with the actual implementation.
  - Dead code (unreachable, or code with no calls or references at all).
  - Redundant tests (tests that duplicate coverage of the same behavior, or whose assertions are unrelated to the behavior under test).
- Mechanical must-fix items are the three categories under "Fixed Archiving", labeled `[mechanical]` in the report.

  Once verified as confirmed they must still be fixed and committed, with closure recorded by mechanical evidence such as sentence-by-sentence comment comparison, reference search output or test lists.

  When all confirmed must-fix items for a role this round are mechanical, that role passes on the new HEAD after the main agent verifies, without re-running.

  When mixed with non-mechanical items, they are incrementally re-reviewed together and do not count as a separate round.

  A missing `[mechanical]` label is assigned by the main agent per the definition; classifying non-mechanical defects as mechanical to skip re-review is forbidden.

- Report tiers use English identifiers: `blocking`, `high`, `medium`, `low`, `recommend`, `suggest`.

  The first three go in the report body; the last three are grouped in the report's `NON-BLOCKING` section.

  If that section is missing, do not re-run the role; record "this role provided no non-blocking items" and add it to the unresolved items list.

- Must-fix threshold: only `blocking`, `high` and `medium` verified and accepted by the main agent must be fixed. `low`, `recommend` and `suggest` never block review or integration; record their handling conclusion per "Conclusions and Failure Handling".
- In stage two, only `blocking`, `high` and `medium` findings returned by an actually running `CSA` or `Hacker` and confirmed by the main agent's verification are sent to the user for decision.

  Each item lists the problem, impact, fix method and at least three options: `1. Fix and re-review`, `2. Confirm pass and accept the risk`, `3. Stop integration`.

  Other tiers and pure defense-in-depth advisories go straight to the unresolved items list without triggering a decision.

- The security finding timeout applies only to kanban tasks.

  Each item is timed independently from when the full information and options reach the user.

  Items in the same batch share the send time; a partial answer does not stop the timers of the remaining items.

  With no explicit decision after 15 minutes, mark it "timed out and ignored", record the role, tier, problem, impact, send time, timeout time and rationale, then continue.

  "Timed out and ignored" is not equivalent to `PASS`, user confirmation or risk acceptance.

  A decision received before the card completes is handled per the latest instruction.

- Non-kanban tasks, `PM` and `QA` findings, unverifiable items, contract or owner changes, and acceptance or integration confirmation explicitly requested by the user must not be skipped by timeout.

**Invocation Arguments**

- Invocation: with no reviewer specified, use the current scope's `kander review <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]`, dispatched by configuration.
- With a reviewer specified, use `kander review <reviewer> <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]`.
- Example of manual plain arguments for a Windows global install: PowerShell `& "$env:USERPROFILE\.local\bin\kander" review ...`.
- Batch files and Windows PowerShell 5 cannot guarantee lossless arbitrary argv.
- Data containing `&|<>^%!`, quotes or boundary backslashes must not be passed by programmatically invoking `.cmd` or by concatenating shell strings.
- Automation must launch the command root `kander` directly through a process API argv array (globally `Path.home() / ".local/bin/kander"`), passing `review` and each argument separately.
- Bypassing Kander to call the reviewer CLI directly is forbidden.
- `CWD` is the absolute path of the target worktree.
- All commit arguments are full SHAs.
- The base is an ancestor of the commit, `HEAD` equals the commit, and there are no uncommitted or untracked files.
- Only incremental re-reviews pass `reviewed-commit`, which must lie strictly between the base and the commit; the review-context must contain a non-empty list of last round's findings.

**Carrying Conclusions Forward**

- Carrying conclusions forward: all roles of a full review share `CWD`, base and task context.
- `CSA` and `Hacker` triggered together use the same commit.
- Under the same base, a role that already passed is not re-run because of another role's fix, except for two cross re-reviews: security fixes are re-reviewed by `QA`, including `PM` when functional behavior changes.
- When a subsequent `PM` or `QA` fix substantively changes security code, the corresponding security role re-reviews the fix scope.
- Roles may pass on different commits; within the same base, the final commit must be a descendant of every passing commit.
- Non-integration changes such as the user switching base, recreating the branch, or a forced rebase during review invalidate all conclusions; restart from stage one.
- A rebase during integration caused by `develop` advancing carries conclusions forward per the one-time gate in `KANDER-GIT-RULES.md`; the pre-rewrite SHA is not required to remain an ancestor.
- The task context is the authoritative requirement contract: a string for short tasks, a readable absolute spec path for long tasks.

**Report Files**

- The stdout of every actually running role is saved as a separate report.
- The report directory must be outside the target worktree, in a temporary directory accessible only to the current user (POSIX `0700`, Windows protected DACL exclusive to the current user); mixing in logs, specs or other files is forbidden.
- Clean up that directory after this round's review passes, the user completes the stage two decision, or this round terminates; explain to the user first if diagnostics need to be kept.
- The review entry runs the reviewer with the isolation arguments from the "Reviewer Selection" table and verifies at the end that the worktree was not modified.
- Changing its sandbox, permission or tool arguments to bypass the gate is forbidden.

## Group-Level Review for Task Groups

Kanban task groups (see `KANDER-TASK-GROUP-RULES.md` "Task Orchestration") are not reviewed card by card; the orchestrator agent reviews them in batches on the group integration branch.

Single-card tasks are still reviewed card by card as above.

This section changes only the review unit and role split; stage policy, incremental re-review, verification duty and conclusion requirements all carry over.

- The review unit is the batch: the orchestrator selects cards already in `review/` by the same module, milestone or dependency chain to form a batch, fast-forwards the batch's latest deliveries onto the group branch and verifies them before starting review. A batch's diff should be readable in detail by the reviewer, and the batch contract must cover all unreviewed deliveries from the batch base to HEAD; do not receive the whole group's changes first and then review only some cards. The `review/` state does not replace delivery verification on the group branch.

  At least one batch for the whole group; one batch per card is not mandatory.

  The whitelist is judged on the batch's aggregated actual changes.

  A task group containing feature development or bug fix cards always enters review.

- Each batch is an independent full review.

  `CWD` is the absolute path of the group branch's dedicated worktree, and the commit is the current group branch HEAD.

  The first batch's base is the full `develop` SHA the group branch was created from; later batches' base is the group branch commit at which the previous batch completed review per "Conclusions and Failure Handling", so each batch reviews only its own newly introduced changes.

  Other preconditions are unchanged: `HEAD` equals the commit, the worktree has no uncommitted or untracked files, and all commit arguments are full SHAs.

  While a batch is in progress, new deliveries outside the batch queue up and the group branch is not updated. Fixes inside the batch must wait for the reviewer to exit, then be fast-forward received by the orchestrator before re-review; do not change the review worktree or HEAD while the reviewer is running.

- The task context is a spec file merging the full contracts of all cards in the batch, placed in a temporary directory accessible only to the current user and passed by absolute path. Mark the task ID per card, keep all six contract fields from "task context and review context Templates", and the cards' `DISCUSSION`, without omitting user trade-offs, acceptance criteria or threat models.

  Passing only one card or only the task group name is forbidden.

  Each card's `OUT_OF_SCOPE` remains the boundary for judging finding overreach.

- Conclusions are not carried across batches and there is no cross-batch incremental re-review: different batches have different task contexts, so per the invalidation conditions in "Incremental Re-review" each must start from a full first round, which does not pass `reviewed-commit`.

  Only fix rounds within a batch are incremental re-reviews: same base, same batch task context, `reviewed-commit` is the group branch commit that role reviewed last round, and the review context contains last round's finding list.

  The previous batch's unresolved items are written into the exclusions of later batches' task contexts and not reported again.

**Verification Split Between Orchestrator and Executing Agent**

- The orchestrator triggers roles, stores reports, attributes findings by change scope (cross-card findings go to the card that hit them; with no owner, create a small fix card), and uses notify to dispatch the finding text, role and tier back to the original executing agent.
- Do not separately call `kander resume <task-id> --message-file`; resumption is chosen internally by notify.
- The orchestrator aggregates the returned lists into the review context, triggers incremental re-review, and shows all unresolved items.
- The executing agent bears the "Main Agent Verification Duty": verify item by item; for confirmed items, fix on the card's task branch, commit, rebase onto the latest group branch and re-verify, updating the task branch per the Git rule file; for rejected or unverifiable items, write the basis. The executing agent does not update the group branch.
- Write the list, conclusions and the full SHA of the latest delivery into `IMPLEMENTATION`, then `move review` and end this round's response. The orchestrator first verifies and fast-forward receives the fix delivery, then triggers incremental re-review.
- The orchestrator must not judge confirmation on the executing agent's behalf, nor omit or rewrite conclusions.

- Security roles run per "Review Stages" and trigger conditions after the whole batch passes stage one, with `CSA` and `Hacker` on the same group branch commit; user decision and timeout rules are unchanged.
- A valid group-level review requires that the actual roles of every batch satisfy "Conclusions and Failure Handling", and that each later batch's base equals the commit at which the previous batch completed review, forming a continuous chain from the group base to the final HEAD.

  Unresolved items are aggregated and written into each card.

  The orchestrator integrates per `KANDER-TASK-GROUP-RULES.md` "Group Integration Branch".

  An integration rebase caused by `develop` advancing is handled by the one-time gate; substantive manual code conflicts still require re-review.

## Review Stages

Beyond "Review Profiles" and the whitelist trigger conditions, whether each role runs is also constrained by the default stage policy.

The configuration file's `review_stages` specifies `auto`, `skip` or `required` for each of the four roles; the default is `auto`.

View it with `kander config` under the command root.

When resolving each role, take the first explicitly specified source by precedence:

1. The user instruction for the current task.
2. The project-level `AGENTS.md` or `CLAUDE.md` nearest to the target file; when unspecified, the user's own global rules.
3. The role's `review_stages` value in the configuration file.
4. Review profiles and security role trigger conditions (effective only when tier 3 is `auto`).

Semantics of each value:

- `auto` - run or not per the "Review Profiles" table and the security role trigger conditions; mark N/A when not triggered.
- `skip` - do not run by default and mark N/A, unless a higher-precedence source explicitly requires running.
- `required` - run by default once review is triggered, unless a higher-precedence source explicitly requires skipping.

Example wording for tier (2): `CSA and Hacker are always N/A in this repository`, or `Skip QA for this task`. Skipped roles are recorded as N/A in the delivery notes with the basis stated. When the user explicitly asks to run a skipped role, the user instruction prevails.

## Review Profiles

Review profiles decide which roles run in a review round and the default content of each role's review context "Review Focus". Profiles do not change stage order, nor the invocation method and isolation arguments of the current platform's review entry.

- The profile is derived from the whitelist entries hit, based on the task's actual changes.

  The executing agent classifying or downgrading on its own is forbidden.

  When multiple entries hit, take the union: each role starts only one first round, and fixes are handled as incremental re-reviews.

  The focus of each entry is merged into the review context's "Review Focus".

- `PM` and `QA` are always the stage one parallel pair in review profiles.

  Whether they actually run is still constrained by the "Review Stages" precedence chain; the profile table only expresses suggested role combinations.

  `PM` is exempt in `auto` mode only when every hit entry is annotated "PM exempt"; record the exemption as N/A and write it into the delivery notes.

- Security role trigger conditions (in `auto` mode): `CSA` runs only when the change involves untrusted input, authentication and authorization, credentials, cryptography, network protocols, remote execution, file writes decided by untrusted input, install/update or release integrity.

  `Hacker` runs only when the external attack surface is added or substantively changed, a dedicated security review is being performed, or the user explicitly requests it.

  Annotations in the profile table are hints only; whether a role actually runs is judged jointly by the security role trigger conditions and "Review Stages".

- When the user explicitly requests a review that hits no whitelist entry, select the profile by the scope the user specified; when unspecified, run the full flow with the feature development profile.

| Whitelist entry             | Roles                | Review focus                                                  |
| --------------------------- | -------------------- | ------------------------------------------------------------- |
| 1 Feature development       | PM + QA              | PM: requirement completeness, flow closure; QA: regressions, test coverage |
| 1 Bug fix                   | PM + QA              | PM: fix scope matches root cause, no out-of-scope changes; QA: repro path closed, regression tests |
| 2 Code change over threshold | Classified by content | Assign to the closest other entry; no clear type: feature development profile |
| 3 Security-sensitive        | PM + CSA/Hacker + QA | `THREAT_MODEL` mandatory, not N/A; isolation and permission boundaries, credential handling |
| 4 Contract change           | PM + QA              | PM: impact surface, doc sync; QA: caller adaptation, compatibility handling |
| 5 Irreversible or cross-data | PM + QA              | Reversibility plan, dry-run or equivalent evidence, state after failed interruption |
| 6 Build/install/release gate | PM + QA (+CSA)       | Flow completeness, upgrade and rollback paths; CSA when release integrity is involved |
| 7 Dependency change         | QA (+CSA); PM exempt | Trusted source, version pinning, license, upstream breaking changes |
| 8 Test removal or loosening | QA; PM exempt        | Check each removal or loosening reason; not masking a known failure |
| 9 Refactor or public module change | QA; PM exempt | Behavioral equivalence: external behavior unchanged, all tests pass unchanged in meaning |

## Main Agent Verification Duty

- Accepting a reviewer conclusion without verification is forbidden; so is rejecting one without verification.

  Every finding and every `NON-BLOCKING` item gets a handling conclusion only after the main agent independently verifies it.

  The reviewer's wording, tone, confidence and number of findings are not evidence.

- Verification must return to the facts of the target commit: read the related code and contracts, walk the trigger path the finding claims, and run commands and record output when needed. Concluding from impression, the reviewer's own statement or "sounds reasonable" counts as unverified.
- There are only three verification conclusions:
  - **Confirmed** - handle per tier; must-fix tiers are fixed and committed.
  - **Rejected** - not handled, not fixed.

    Common types: the basis does not match the actual code of the target commit.

    The problem already existed at the base and was not aggravated or masked this round.

    It hits a specific item in the task context `OUT_OF_SCOPE`.

    Beyond the contract, i.e. the behavior the finding demands is neither in `ACCEPTANCE_CRITERIA` nor a logical necessity of an existing contract (concurrency interleaving, cross-platform or security hardening, and syncing shared contracts and architecture docs all belong here, unless named by the acceptance criteria).

    The trigger premise the finding assumes does not hold in this task.

    Same root cause as another finding already accepted.

    Beyond-contract items are recorded as unresolved items with a follow-up card suggested, and are not fixed this round.

    But a real defect introduced, aggravated or masked this round that reaches `blocking` or `high` is still sent to the user to decide whether to include it.

    Existing problems unaffected by this round are still excluded per "Goals and Boundaries".

  - **Unverifiable** - state which evidence or environment is missing and send it to the user for decision; treating it as rejected on your own is forbidden.
- Judging as rejected or changing tier (upgrading and downgrading alike) must state the verification basis: file and line number, `commit` SHA, commands actually run with output, or the verbatim exclusion item cited.

  Evidence-free judgments such as "low impact", "later", "too large a change" or "the reviewer lacks context" do not count as a basis.

  A downgraded item still goes into the unresolved items list.

- All rejected and unverifiable items go into the unresolved items list at the end of the loop for the user to re-check; the user may overturn any conclusion, and overturned items are handled as must-fix.
- Confirming a finding does not mean copying the reviewer's fix: the main agent applies the minimal correct fix. If the reviewer's fix would change a direction the user has explicitly set or an external contract, send it to the user for decision per "Goals and Boundaries".

## task context and review context Templates

- `OUT_OF_SCOPE` is the risk-bounded stop boundary of the review and must be judged category by category with reasons stated, across four categories:
  1. Problems that existed before the change.
  2. Concurrency interleaving, cross-platform and security hardening.
  3. Syncing shared contracts, public APIs and architecture docs.
  4. Adjacent features and later phases.

  Categories not required by `ACCEPTANCE_CRITERIA` are stated as excluded; categories this card is responsible for are stated as included.

  A single generic exclusion does not make the contract complete; the card-creating agent completes it before `pick`.

  During review, both the reviewer and the main agent use this list to judge whether a finding oversteps the contract; overstepping ones are recorded as unresolved items with a follow-up card suggested.

The task context is organized by the following six fields; keep all of them, write `N/A` when nothing applies, and do not fabricate `USER_DECISIONS` or `ACCEPTANCE_CRITERIA`. When using an absolute spec, pass the full contract, including any existing `DISCUSSION`:

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

The concrete boundary of `OUT_OF_SCOPE` makes each role's risk-bounded stop effective, avoiding misreporting known trade-offs or existing problems as this round's findings. Common items:

- Existing problems not introduced this round: `<specific item> existed before the change (see <commit>); fixing it would expand this task's scope.`
- Directions the user has explicitly decided or areas reserved for the user's own decision: `This is the direction the user explicitly requested; do not oppose the direction itself on the grounds of <opposing claim>; the user is aware of the trade-off.`
- Hardening beyond the contract: `Concurrency interleaving, cross-platform or security hardening is not listed in ACCEPTANCE_CRITERIA; this card only guarantees <ACCEPTANCE_CRITERIA>, the rest is handled by <follow-up card>.`
- Theoretical defects with disproportionate cost versus benefit, excludable only when all three hold (triggering requires an additional precondition already compromised; the consequence is minor or even safer; fix complexity clearly exceeds the benefit): `<specific item> requires <premise> to trigger, has consequence <consequence>, and fixing it needs <cost>; not in this round's scope.`
- Verification gaps caused by the environment: `<command> could not be executed because of <reason>; covered by <substitute verification>; do not judge a problem on that basis.`
- When the project has dropped backward compatibility: `The project has dropped all backward compatibility; do not raise compatibility, migration or fallback issues.`

Exclusions may exclude only "scope", never "severity": writing "do not report high" or "report only doc issues" is forbidden; real blocking, high and medium must still be reportable.

Exclusion reasons must rest on facts (which commit introduced it, what premise triggers it, which user instruction), and empty phrases such as "not important" or "later" are forbidden.

When the implementation was done by other agents, state that in the review context and point out the problems you already fixed, so the reviewer focuses on re-checking similar half-changed states.

## Conclusions and Failure Handling

- A valid review requires: `QA` and `PM`, as actually run per "Review Stages", both have no blocking, high or medium findings accepted by the main agent.

  `CSA` and `Hacker`, as actually run per trigger conditions, returned conclusions on the same commit, and their security findings have been decided by the user or recorded as timed out and ignored per the timeout rule.

  When `PM` is exempt per profile or stage policy, record N/A in the delivery notes.

- A blocking, high or medium finding judged "Unverifiable" counts as not passed: that stage must not be released; send it to the user for decision and continue only after the user judges it need not be handled.

**Unresolved Items Summary**

- When the review loop ends (pass or mid-way termination), show the user every unresolved item of this round, omitting none:
  - Unfixed `low`, `recommend` and `suggest`.
  - Findings judged rejected or unverifiable.
  - Risks the user has confirmed accepting, and security findings timed out and ignored.
  - Roles not completed due to backend failure, and roles that provided no `NON-BLOCKING` section.
- Each item states the source role, tier, problem, impact and rationale.
- The same content is written into the delivery notes.
- Kanban tasks also write it into the task card.
- Reporting only "review passed" or "no blocking issues" is forbidden.
- When review is skipped because no whitelist entry was hit, following the notification rule in "Preconditions and Execution" is sufficient.

- There is no cap on the number of restarted rounds. But when the same finding still fails after several consecutive rounds of fixes, stop spinning, explain to the user where it is stuck, what has been tried and the next options, and let the user decide.
- Review entry argument, authentication, precondition or local environment errors must be corrected first.

  Only when the same role invocation with correct arguments and preconditions returns HTTP 5xx or an explicit service unavailable error 3 times in a row is it treated as a persistent backend failure of that reviewer.

**Stage One Backend Failures**

- When `PM` or `QA` has a persistent backend failure, do not switch reviewer on your own: keep the branch and worktree, report the failed role, the actual error and the completed stages, stop integration, and wait for the user to decide to retry, reschedule or explicitly designate another reviewer.
- When offering the reviewer switch option, state the cost: only that role's existing conclusions are voided and that stage is re-run; conclusions of other roles that already passed remain valid.
- Only when the user changes the whole reviewer set, the review base or the task context are all stages voided per the corresponding rules and restarted from stage one (`PM` and `QA` first round in parallel on the same commit).

- A persistent backend failure of `CSA` or `Hacker` does not block review: record that role as "not completed due to backend failure", state it in the delivery notes and the report, and let `PM` and `QA` decide the review conclusion.

  When both security roles trigger and one fails, the other still produces its conclusion and is handled per the stage two rules.
