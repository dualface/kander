# Orchestration Summary Template

Source: moved from `KANDER-REPORTING-RULES.md` "Orchestration Summary".

```markdown
# Kanban Orchestration Summary

- Scope: <task group IDs and standalone task IDs in plan order; source of the plan notes>
- Order and parallelism: <actual start order; cards that ran in parallel | None>
- Review batches: <per batch: batch ID, base and target, roles with conclusions, fix rounds | N/A with the basis>
- Integration: <per group: group HEAD before and after, `develop` full SHA, method (ff | rebase, patch equal | merge commit, conflicting paths, verification); per standalone card: final full SHA, integrated by executing agent | N/A>
- Wrap-up: <per group: worktree and branches deleted, `kander check` result, wrap-ups done on behalf with reason; all completed or exceptions item by item>
- Self-resolutions (<N>): <None; or item by item `<task-id>: cause; action; basis; outcome`>
- Unresolved issues (<N>): <None; or item by item `<task-id>[, <task-id>] [source or category][tier or status] issue; impact: ...; reason: ...; unblocking condition: ... | N/A`>
- Card states: <item by item `<task-id> - <title>: <done | archived (<result>) | trash | working (blocked) | review (blocked)>`>
- Summary: <one-sentence summary; on termination, the termination decision and reason>; Orchestration state: <completed | terminated | blocked>
```
