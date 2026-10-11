# Kanban Task Completion Report

- Task: [{{.TaskID}} - {{.Title}}]({{.Path}})
- Delivery: <user-observable outcome and key changes>
- Acceptance: <completed>/<total>; <per-item self-check conclusion or user-accepted exceptions>
- Verification: <actual commands and results; for failed or unexecuted items, the reason, impact, and substitute evidence>
- Review: <reviewer, status, and summary for PMQA (stage one) and Security (stage two); include N/A with the skip/exemption basis and fixes made during review>
- Wrap-up: <full SHA | N/A>; <integration result | N/A>; <main worktree sync, worktree, branch, temporary review files, `kander check` all completed or exceptions item by item>
- Unresolved issues (<N>): <None, or one item per line below>
  - `[source or category][tier or status] <issue>; impact: <...>; reason: <...>` <for a timed-out item, add the send time and the timeout time>
- Summary: <one-sentence summary>
  - Code branch: <branch where the code finally lives | N/A>
  - Final card state: {{.FinalState}}
