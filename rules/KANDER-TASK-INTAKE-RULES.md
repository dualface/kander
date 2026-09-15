# Task Intake Guidance

## Creation and Confirmation

GitHub issues follow `KANDER-ISSUE-RULES.md` for the investigation, consent, and binding rules before this guidance applies.

Provide guidance only for new bug or feature requests that have not yet chosen an execution mode. Tasks continued via `start`, `resume`, or `notify`, and existing cards named by the user, continue on the original card; do not ask again or create another card. When the user has already explicitly chosen the kanban board or direct execution, follow the chosen flow without asking again. Pure Q&A, read-only investigation, minor documentation or configuration tweaks, releases, and merges do not trigger intake guidance.

When `rules.task_groups=true` and `rules.git=true`, read `KANDER-TASK-GROUP-RULES.md` "Task Splitting and Task Groups" before presenting the options, and state in the plan whether the work is one card, several independent single cards, or one or more task groups, with the reason. For a group, also list the member card titles, their dependency order, and the step that merges each group branch back into `develop` in dependency order once its gates pass. Card bodies are written after confirmation; the split and the merge-back step are confirmed together with the plan. When task groups are disabled, always plan a single card, whatever the number of goals, and do not load the disabled module.

After finishing the analysis and implementation plan, present the options once:

```text
- Confirm the plan and use the kanban board (create the cards; this session starts and advances them here)
- Confirm the plan and use the kanban board (create the cards; a separate orchestrator session advances and reports them in its own window)
- Confirm the plan and use the kanban board (create the cards only, leave them in backlog, start later on instruction)
- Confirm the plan, skip the board, implement directly in this session
- Adjust the plan (no card created or started)
```

Offer all five options for both single-card and multi-card plans, whether independent single cards, task groups, or both. Number these options from `1` and make them the only numbered question in that message, so the numbers cannot collide with another question.

- Choosing `Confirm the plan and use the kanban board (create the cards; this session starts and advances them here)` authorizes the plan, the development, and the kanban flow at once, including the merge-back steps the plan states; do not ask again before starting work or before integrating. This covers every standalone card and every task group named in the confirmed plan.

  For a standalone card with `rules.git=true`, this execution authorization also covers integration into `develop` and cleanup, with the conditions stated in `KANDER-GIT-RULES.md` "Commit and Push"; do not request a separate merge-back confirmation. The completion flow is defined in `KANDER-KANBAN-RULES.md` "Execution and Completion".

  For a single card, and for each of several independent single cards, run in order: `kander new`, fill in the complete contract according to the confirmed plan, complete the self-review and any applicable independent card review per `KANDER-KANBAN-RULES.md` "Post-Creation Self-Review" and fix the findings, `kander pick <task-id>` (defined in `KANDER-KANBAN-RULES.md` "Command Contract"), `kander start <task-id>`. Start and tracking responsibilities follow `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination"; the discussing agent no longer implements a card that has been delegated.

  For a task group, create every member card the same way, complete the group-level checks in `KANDER-TASK-GROUP-RULES.md` "Task Splitting and Task Groups", then orchestrate per that file. The confirmed plan is the orchestration plan; it already carries the integration authorization for each group it names.

- Choosing `Confirm the plan and use the kanban board (create the cards; a separate orchestrator session advances and reports them in its own window)` carries the same authorization as the first option, including every merge-back step the plan states, but hands the advancing to a separate session. Create every card the same way as the first option and complete its self-review and any applicable independent card review (and, for a task group, the group-level checks), leaving the cards in `backlog/`. Then write the confirmed plan into a notes file (card and group order, which cards may run in parallel, merge-back steps, and the user decisions) and run `kander orchestrate --message-file <notes> <task-id|task-group-id>...` in plan order per `KANDER-KANBAN-RULES.md` "Orchestrator Sessions". On success, tell the user where the orchestrator runs, that it advances every card and reports in its own window, and that this session no longer follows the plan; this session starts no card itself. When the launch fails, report the error; the cards stay in `backlog/`, and the user chooses to retry or to continue with the first option in this session.
- Choosing `Confirm the plan and use the kanban board (create the cards only, leave them in backlog, start later on instruction)` authorizes the plan and card creation only. Create the cards, complete the self-review and applicable independent card review, and leave them in `backlog/`; do not move them to `todo/` or start them. Starting later requires a further user instruction; that instruction enters the same flow as the first option from `kander pick` onward (after `pick`, `kander move <task-id> working --owner <agent>` replaces `kander start` only when the user explicitly asks this agent to execute the card itself, per `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination"), and it carries the integration authorization of the confirmed plan; do not ask for it again.
- Choosing `Confirm the plan, skip the board, implement directly in this session` implements directly per the project rules, without creating a card.
- Choosing `Adjust the plan (no card created or started)` modifies the plan; no card is created or started yet.
