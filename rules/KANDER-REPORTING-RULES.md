# Completion Report Format

Loaded only when `rules.reporting=true`. This file defines the reporting format only; it adds no Git, review, or task group gates. Write N/A for steps that do not apply.

- When a non-kanban task ends, give one sentence with the goal, completion status, actual commit state, and branch; for non-Git tasks write "No commit, branch not applicable".

## Completion Report

- When a task card enters `done/`, `archived/`, or `trash/`, or stays blocked in `working/` or `review/` and is handed back to the user, report once with the template below. Never report while work is in progress, and never as free-form text or a one-line done/blocked message.
- Keep all 8 fields, in order, with `None` or `N/A` where there is no content. `Unresolved issues` lists one item per sub-line; each item follows "Report Wording". `Summary` carries two sub-lines, `Code branch` and `Final card state`; the last line is `Final card state`: `done`, `archived (<result>)`, `trash`, `working (blocked)`, or `review (blocked)`.
- If the card did not enter `done/`, `Task` gives the absolute path of the card under its current state directory. In that case, `Delivery`, `Acceptance`, `Verification`, `Review`, and `Wrap-up` list only what was actually completed and write `Not executed` with the reason for the rest. A blocked report lists under `Unresolved issues`, item by item, the remaining work, the blocking reason, and the unblocking condition, and states that the branch and worktree are preserved. A termination report adds the user-authorized conclusion (`cancelled`, `duplicate`, `wontfix`) and the reason; `duplicate` points to the replacement card.
- `Acceptance` counts the ticked `- [x]` items of `ACCEPTANCE_CRITERIA` as `<completed>` and all top-level items as `<total>`.
- Whoever ends the task reports; a session switch or takeover does not exempt this, and the orchestration summary does not replace the per-card report.
- Verification records actual results only; failures, unexecuted steps, and environment blocks are never written as passed. Use the full 40-character SHA for the final commit. Write "all completed" for wrap-up only when every applicable step succeeded; otherwise describe each exception.
- Unresolved issues list known defects, verification gaps, and follow-up tasks; count one root cause once. Only when `rules.review=true` or the user explicitly requested a full review this time, record review items by the categories in `KANDER-REVIEW-RULES.md`, without loading a disabled review module. For a security review timeout, attach the send time and the timeout time.

```markdown
# Kanban Task Completion Report

- Task: [<task-id> - <title>](<absolute path of the task entry under its final state directory>)
- Delivery: <user-observable outcome and key changes>
- Acceptance: <completed>/<total>; <per-item self-check conclusion or user-accepted exceptions>
- Verification: <actual commands and results; for failed or unexecuted items, the reason, impact, and substitute evidence>
- Review: <reviewer, status, and summary for PMQA (stage one) and Security (stage two); include N/A with the skip/exemption basis and fixes made during review>
- Wrap-up: <full SHA | N/A>; <integration result | N/A>; <main worktree sync, worktree, branch, temporary review files, `kander check` all completed or exceptions item by item>
- Unresolved issues (<N>): <None, or one item per line below>
  - `[source or category][tier or status] <issue>; impact: <...>; reason: <...>` <for a timed-out item, add the send time and the timeout time>
- Summary: <one-sentence summary>
  - Code branch: <branch where the code finally lives | N/A>
  - Final card state: <done | archived (<result>) | trash | working (blocked) | review (blocked)>
```

## Orchestration Summary

- When an orchestration ends (every planned card has entered `done/`, the user terminates the orchestration, or an integration blocker is handed back to the user), the orchestrator reports once with the template below. This applies to a task group orchestrator and to a `kander orchestrate` session alike. Never report while cards are still advancing.
- The summary is issued before the dismiss question of `snippets/orchestrate-integrate.md` "Cleanup Failures". The dismiss results follow as one line per card after the user's decision; they never change the summary already issued.
- Keep all 9 fields, in order, with `None` or `N/A` where there is no content. `Unresolved issues` lists one item per sub-line. `Card states` holds one sub-group per card: the task ID, then its `Summary`, `Code branch`, and `Final card state`. `Summary` carries the sub-line `Orchestration state`, which is the last line: `completed`, `terminated`, or `blocked`.
- The summary never replaces the per-card completion report: each card's executing agent (or the on-behalf finisher) reports with the completion report template. `Card states` lists each card once with its one-sentence summary, code branch, and final card state, and does not repeat the other per-card fields.
- `Unresolved issues` collects every item that stays open after the orchestration: known defects, verification gaps, follow-up tasks, and unresolved review items. Write each item on its own sub-line in the same form as the completion report's `Unresolved issues` field, prefixed with the task IDs it affects, and count one root cause once even when it hits several cards. A blocking item names its unblocking condition. Only when `rules.review=true`, classify unresolved review items by the categories in `KANDER-REVIEW-RULES.md` "Conclusions and Failure Handling". Every item follows "Report Wording": one fact per sentence, a fixed status word, the evidence as `path:line` or a full SHA, and no filled-in facts.
- `Self-resolutions` lists the blockers the orchestrator resolved on its own per `snippets/orchestrate-sessions.md` "Handling Blocked Executing Agents": task ID, cause, action, basis, and outcome with a fixed status word.
- `Integration` records, per group, the group HEAD before and after integration, the `develop` commit after merge-back, and the method. The method is `ff`, `rebase` with the patch-equality result, or `merge commit` with the conflicting paths and their verification per `snippets/orchestrate-integrate.md` "Merge-Back and Cleanup Preconditions". A standalone card integrated by its executing agent is listed with its final commit and `integrated by executing agent`.
- `Wrap-up` records, per group, the group worktree and branch cleanup, the untargeted `kander check` result, and every wrap-up completed on behalf with its reason; write "all completed" only when every applicable step succeeded.
- On termination, the unfinished cards are reported one by one with the completion report template first; the orchestration summary then follows with `Orchestration state: terminated` and the termination decision under `Summary`.

The template is the static snippet `snippets/report-orchestration.md`; its nine fields follow the rules above.

## Report Wording

These points apply to every report this file defines, the completion report and the orchestration summary, and to every item inside them, in any `agent_language`. They follow the writing principles of ASD-STE100 (Simplified Technical English); the standard itself is not required.

- Put the result first. The first sentence of a message states the outcome; process, details, and evidence follow it.
- Write one fact or one action per sentence. Split a sentence that carries two facts.
- Use one term for one thing. After choosing a word for a concept, keep it for the whole message and never rotate synonyms.
- Write a status with a fixed word. Where the template or another rule file defines a value (for example `None`, `N/A`, `Not executed`, `PASS`, `FAIL`, a final card state), use it. Elsewhere use only these words, or one fixed equivalent of each in the report language: completed, partially completed, in progress, verified, not verified, failed, skipped, blocked, unconfirmed. Completed never implies verified; write "completed, not verified" when no check ran.
- Give each verified claim its method: the command or check and its actual result. Quote the relevant failing lines verbatim; redact credentials and personal data.
- Mark a fact that was not checked as unconfirmed and name how to confirm it. Never hide uncertainty behind words such as "probably" or "should be".
- Never narrate the process ("first I ..., then I ..."). Give the results and the evidence the reader needs.
- Make evidence locatable: `path:line` for code, the full SHA for a commit, the full command line for a command.
- When the user must decide, give numbered options. Each option states one action and its consequence. Put the recommended option first, unless a rule file fixes the option list and its order.
- Take every fact from a source: the user's input, file contents, or tool output. List a missing fact under `Unresolved issues` or as a question; never fill it in.
