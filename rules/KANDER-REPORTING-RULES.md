# Completion Report Format

Loaded only when `rules.reporting=true`. This file defines the reporting format only; it adds no Git, review, or task group gates. Write N/A for steps that do not apply.

- When a non-kanban task ends, give one sentence with the goal, completion status, actual commit state, and branch; for non-Git tasks write "No commit, branch not applicable".

## Completion Report

- When a task card enters `done/`, `archived/`, or `trash/`, or stays blocked in `working/` or `review/` and is handed back to the user, report once with the template below; never while work is in progress, and never as free-form text or a one-line done/blocked message.
- Keep all 8 fields, in order, with `None` or `N/A` where there is no content. The last line carries `Final card state`: `done`, `archived (<result>)`, `trash`, `working (blocked)`, or `review (blocked)`.
- If the card did not enter `done/`, `Task` gives the absolute path of the card under its current state directory; `Delivery`, `Acceptance`, `Verification`, `Review`, and `Wrap-up` list only what was actually completed and write `Not executed` with the reason for the rest. A blocked report lists under `Unresolved issues`, item by item, the remaining work, the blocking reason, and the unblocking condition, and states that the branch and worktree are preserved. A termination report adds the user-authorized conclusion (`cancelled`, `duplicate`, `wontfix`) and the reason; `duplicate` points to the replacement card.
- Whoever ends the task reports; a session switch or takeover does not exempt this, and a group-level summary does not replace the per-card report.
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
- Unresolved issues (<N>): <None; or item by item `[source or category][tier or status] issue; impact: ...; reason: ...`; attach send time and timeout time for timed-out items>
- Summary: <one-sentence summary>; Code branch: <branch where the code finally lives | N/A>; Final card state: <done | archived (<result>) | trash | working (blocked) | review (blocked)>
```
