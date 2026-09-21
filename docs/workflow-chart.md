# Workflow chart

The options panel's workflow page is a projection of the released rules in `rules/`. `internal/flow` derives a language-neutral `Chart` from the module switches and one task scale's configuration; `internal/tui` renders it. The chart performs nothing and reads nothing but the in-memory configuration.

This file is the mapping the chart must keep. When a rule changes, change the chart and this table together; when they disagree, the rules win.

## Model

| Type | Meaning |
| ---- | ------- |
| `Chart` | One task scale's workflow: `Scale` plus `Phases` in rule order |
| `Phase` | One block: a stable `Key`, an optional `Node`, annotation `Notes`, and the `Gates` that run after it |
| `Node` | The agent, model, and effort a phase runs with; review phases also carry `Role` and `Mode` |
| `Gate` | One decision with its `Exits` |
| `Exit` | One labeled way out: `Steps` are its actions, `Target` names the phase it leads to, `User` marks a decision the rules route to the user |

`Exit.Target` is what makes a loop a loop: a target naming an earlier phase is a back edge, a later one is a forward jump, and an empty target (or one naming the next phase) simply falls through. The renderer routes every such target through the side gutters; when the labels would no longer be readable it falls back to the compact form, which names each target after the exit instead of drawing it. The renderer never substitutes prose for an edge.

`Mode` is `required`, `auto`, `skip`, or `invalid`. `invalid` keeps a role whose policy cannot be resolved on the chart instead of dropping it.

## Phases

| Phase | Appears when | Rule |
| ----- | ------------ | ---- |
| `intake` | `rules.task_intake` | `KANDER-TASK-INTAKE-RULES.md` "Creation and Confirmation" |
| `create` | always | `KANDER-KANBAN-RULES.md` "Post-Creation Self-Review", "State Model" todo gate |
| `claim` | always | `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination" |
| `branch` | `rules.git` | `KANDER-GIT-RULES.md` "Task Branches", "Keeping a Task Branch Current" |
| `implement` | always | `KANDER-KANBAN-RULES.md` "Execution and Completion" |
| `self_check` | `rules.code` and (`rules.review` or task groups with Git) | `KANDER-CODE-RULES.md` "Delivery Self-Check"; `KANDER-KANBAN-RULES.md` "State Model" (`review/` is used only by task group cards) |
| `delivery_check` | `rules.review` without `rules.code` | `KANDER-REVIEW-RULES.md` "Preconditions and Execution" |
| `deliver_group` | task groups with Git | `KANDER-TASK-GROUP-RULES.md` "Delivering a Task Branch to the Group Branch" |
| `whitelist` | `rules.review` | `KANDER-REVIEW-RULES.md` "Preconditions and Execution" |
| `review_plan` | `rules.review` | `KANDER-REVIEW-RULES.md` "Controlled Plans, Author Dispositions, and Batch Closure" |
| `stage_primary` | `rules.review` | `KANDER-REVIEW-RULES.md` "Review Stages", "Review Profiles" |
| `stage_security` | `rules.review` | same |
| `batch_close` | `rules.review` | `KANDER-REVIEW-RULES.md` "Controlled Plans …" closure |
| `unresolved` | `rules.review` | `KANDER-REVIEW-RULES.md` "Unresolved Items Summary" |
| `integrate` | `rules.git` | `KANDER-GIT-RULES.md` "Integration and Cleanup" |
| `finish_own` | not `rules.git` | `KANDER-REVIEW-RULES.md` opening bullet; `KANDER-KANBAN-RULES.md` "Execution and Completion" |
| `wrap_up` | task groups with Git | `KANDER-TASK-GROUP-RULES.md` "Integration and Wrap-Up" |
| `complete` | always | `KANDER-KANBAN-RULES.md` "Execution and Completion", "Review Evidence Completion Gate" |
| `report` | `rules.reporting` | `KANDER-REPORTING-RULES.md` "Completion Report" |

`rules.collaboration` adds no phase. It constrains presentation only: the diagram stays within 100 ASCII columns per `KANDER-COLLABORATION-RULES.md` "Communication and Formatting", and the renderer additionally never exceeds the width the options panel gives it.

Task groups need Git, so `rules.task_groups` without `rules.git` produces the single-card chart.

## Gates and exits

| Gate | Exit | Target | Rule |
| ---- | ---- | ------ | ---- |
| `gate_intake` | the five execution options | options 1–3 fall through to `create`, option 4 jumps to `branch` (or `implement` without Git), option 5 loops to `intake` | `KANDER-TASK-INTAKE-RULES.md` "Presenting the options" |
| `gate_todo` | `todo_incomplete` | `create` | `KANDER-KANBAN-RULES.md` "State Model" todo gate |
| `gate_claim` | `claim_start`, `claim_self` | fall through | `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination" |
| `gate_delivery` | `delivery_fail`, `delivery_incomplete` | `implement` | `KANDER-CODE-RULES.md` "Delivery Self-Check" exit codes |
| `gate_delivery` | `delivery_review_required` | falls through with a recorded disposition | same |
| `gate_group_receive` | `group_ff_conflict` | `implement` | `KANDER-TASK-GROUP-RULES.md` "Delivering a Task Branch to the Group Branch" (`notify --kind sync`) |
| `gate_whitelist` | `whitelist_miss` | `integrate`, or `finish_own` without Git | `KANDER-REVIEW-RULES.md` "Preconditions and Execution": notify the change scope and continue without waiting |
| `gate_reviewer_ran` | `reviewer_unavailable` | user, four options | `KANDER-REVIEW-RULES.md` "Review Tool Unavailable" |
| `gate_reviewer_ran` | `reviewer_backend_failure` | user; stage one stops integration, stage two keeps its decision but a `required` role still needs a run | `KANDER-REVIEW-RULES.md` "Stage One Backend Failures" |
| `gate_must_fix` | `mf_none` | falls through, dispositions still required | `KANDER-REVIEW-RULES.md` must-fix threshold |
| `gate_must_fix` | `mf_unverifiable` | user, the stage is not released | `KANDER-REVIEW-RULES.md` "Main Agent Verification Duty", "Conclusions and Failure Handling" |
| `gate_must_fix` | `mf_attribution` | user | `KANDER-REVIEW-RULES.md` "Main Agent Verification Duty": a `blocking` or `high` item the main agent cannot classify as regression or new guarantee |
| `gate_must_fix` | `mf_mechanical` | falls through without re-running the reviewer | `KANDER-REVIEW-RULES.md` "Fixed archiving" and `review advance` |
| `gate_must_fix` | `mf_other` | `stage_primary` | `KANDER-REVIEW-RULES.md` stage diagram [1] and incremental re-review |
| `gate_must_fix` | `mf_round_cap` | user, three options | `KANDER-REVIEW-RULES.md` "Conclusions and Failure Handling" round cap |
| `gate_cross_security` | `cross_security_touched` | `stage_security` | `KANDER-REVIEW-RULES.md` "Carrying Conclusions Forward": a later PMQA fix that substantively changes security code |
| `gate_security_conditions` | present only for an `auto` security role | — | `KANDER-REVIEW-RULES.md` "Review Profiles" security trigger conditions |
| `gate_security_qualified` | `sec_none` | falls through, other tiers go to the unresolved list | `KANDER-REVIEW-RULES.md` stage two tiers |
| `gate_security_qualified` | `sec_fix` | `stage_security`, user decision 1 | `KANDER-REVIEW-RULES.md` stage diagram [2] |
| `gate_security_qualified` | `sec_cross_primary` | `stage_primary` | `KANDER-REVIEW-RULES.md` "Carrying Conclusions Forward": a security fix is re-reviewed by an enabled PMQA, unconditionally, for the fix scope |
| `gate_security_qualified` | `sec_accept` | user decision 2, recorded as accepted risk | same |
| `gate_security_qualified` | `sec_decision_stop` / `sec_decision_stop_delivery` | user decision 3 | same; without Git there is no integration to stop |
| `gate_security_qualified` | `sec_timeout` | falls through | `KANDER-REVIEW-RULES.md` security finding timeout: `medium` only, `blocking` and `high` always wait |
| `gate_security_qualified` | `sec_round_cap` | user, three options | round cap, as above |
| `gate_rebase` | `rebase_code_conflict` | `stage_primary` | `KANDER-GIT-RULES.md` "One-Time Review Gate": re-review only on a hand-resolved substantive code conflict |
| `gate_rebase` | `group_patch_changed` | `review_plan`, user | `KANDER-TASK-GROUP-RULES.md` "Merge-Back and Cleanup Preconditions": the patch must stay identical, any conflict integrates through a merge commit, and a hand-resolved code conflict is reviewed as a new batch |

A skipped stage keeps its box with the N/A note and its override note and has no gates; the cross edges to and from it disappear with it.

## Why stage one has no trigger-condition gate

`auto` means "run or not per the Review Profiles table and the security trigger conditions". Every whitelist entry's profile contains `PMQA` — entry 2 is classified by content and joins one of the others — so once review is triggered an `auto` `PMQA` always runs. Only the security role has trigger conditions of its own, so only stage two gets that gate.

## Rendering

- The spine is a single centered column of phase boxes. Notes sit under their box; gates hang off the spine.
- Back edges route through the left gutter, forward jumps through the right one. Lanes are assigned shortest span first so nested edges do not cross; several edges reaching the same box share one horizontal run.
- The chart is drawn with edges only while the label column stays readable. Below that the compact form drops the gutters, and the named target is protected from clipping: the label yields room to it.
