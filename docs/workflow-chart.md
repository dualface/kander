# Workflow chart

The options panel uses the workflow chart implementation from v0.7.12. It shows execution and review assignments for the selected task scale, rather than the complete card lifecycle. `internal/flow` reads only the in-memory options-session configuration; `internal/tui` renders the chart without changing configuration or running any actions.

## Model

- `Node` carries model and effort, plus role and policy for review nodes.
- `Stage` holds the ordered review stage name and its non-skipped role nodes.
- `Chart` carries the task scale, execution node, review-disabled flag, and stages.

With review enabled, the stages are PMQA followed by Security. A skipped role, or a role whose policy cannot be resolved, contributes no node; an empty stage remains visible as N/A. With review disabled, the chart shows execution, a review-disabled note, and completion.

Execution and reviewer models use the existing configuration resolution helpers, including per-scale overrides. Effort appears only for agents that support it. Empty models use the localized CLI-default label.

The chart reflects configured review policies. Repository-specific role exemptions and runtime review decisions are not inferred. Other module switches do not alter this restored chart, so it is not a complete projection of the released workflow rules.

## Rendering and interaction

With review enabled, the renderer draws execution, self-check, PMQA, Security, and completion. Nonempty review stages include gates with fix and incremental re-review loops; the Security loop also includes a user-decision step. Wide layouts draw return rails on the right. When those rails do not fit, compact layouts list the steps below each gate and name the return stage.

Labels come from the English, Chinese, and Japanese flow catalogs. The options panel retains task-scale switching, scope tabs, unsaved configuration previews, and report scrolling.
