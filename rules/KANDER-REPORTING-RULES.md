# Completion Report Format

Loaded only when `rules.reporting=true`. See `KANDER-AGENTS.md` for precedence and the loading flow. This file only defines the reporting format; it adds no Git, review, or task group gates. Write N/A for steps that do not apply.

- When a non-kanban task ends, give one sentence with the goal, completion status, actual commit state, and branch; for non-Git tasks write "No commit, branch not applicable".

## Completion Report

- When a task card enters `done/`, `archived/`, or `trash/`, or stays blocked in `working/` / `review/` and is handed back to the user, report once using the template; do not report while work is in progress. Do not replace it with free-form text or a one-line done/blocked message.
- Do not omit or merge any of the 8 fields; write `None` or `N/A` when there is no content.

  Put the ending mode on the last line as `Final card state`, one of `done`, `archived (<result>)`, `trash`, `working (blocked)`, or `review (blocked)`.

- If the card did not enter `done/`, `Task` gives the absolute path of the card under its current state directory; `Delivery`, `Acceptance`, `Verification`, `Review`, and `Wrap-up` list only what was actually completed, and write `Not executed` with the reason for anything not done. Even when incomplete, never leave a field empty, drop a line, or change the format.
- A blocked report lists, item by item under `Unresolved issues`, the remaining work, the blocking reason, and the condition for unblocking, and states that the branch and worktree are preserved as they are.

  A termination report additionally gives the user-authorized conclusion (`cancelled`, `duplicate`, `wontfix`) and the reason; `duplicate` points to the replacement card.

- Whoever ends the task reports using the template; switching sessions or taking over does not exempt this, and a group-level summary does not replace the per-card report.
- Verification records only actual results; failures, unexecuted steps, and environment blocks must never be written as passed. Use the full 40-character SHA for the final commit. When every applicable wrap-up step succeeded, write "all completed"; otherwise describe each exception item by item.
- Unresolved issues list known defects, verification gaps, and follow-up tasks; count the same root cause only once.

  Only when `rules.review=true` or the user explicitly requested a full review this time, record review items additionally by the categories in `KANDER-REVIEW-RULES.md`; do not load a disabled review module because of this.

  When a security review timeout was applied, attach the send time and the timeout time.

```markdown
# Kanban Task Completion Report

- Task: [<task-id> - <title>](<absolute path of the task entry under done>)
- Delivery: <user-observable outcome and key changes>
- Acceptance: <completed>/<total>; <per-item self-check conclusion or user-accepted exceptions>
- Verification: <actual commands and results; for failed or unexecuted items, the reason, impact, and substitute evidence>
- Review: <reviewer, status, and summary for PM, CSA, Hacker, QA; fixes made during review>
- Wrap-up: <full SHA | N/A>; <integration result | N/A>; <main worktree sync, worktree, branch, temporary review files, `kander check` all completed or exceptions item by item>
- Unresolved issues (<N>): <None; or item by item `[source or category][tier or status] issue; impact: ...; reason: ...`; attach send time and timeout time for timed-out items>
- Summary: <one-sentence summary>; Code branch: <branch where the code finally lives | N/A>; Final card state: <done | archived (<result>) | trash | working (blocked) | review (blocked)>
```
