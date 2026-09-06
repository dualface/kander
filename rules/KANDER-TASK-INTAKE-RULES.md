# Task Intake Guidance

## Creation and Confirmation

Provide guidance only for new bug or feature requests that have not yet chosen an execution mode. Tasks continued via `start`, `resume`, or `notify`, and existing cards named by the user, continue on the original card; do not ask again or create another card. When the user has already explicitly chosen the kanban board or direct execution, follow the chosen flow without asking again. Pure Q&A, read-only investigation, minor documentation or configuration tweaks, releases, and merges do not trigger intake guidance.

After finishing the analysis and implementation plan, present the options once:

```text
- Confirm the plan and use the kanban board (create the card and start)
- Confirm the plan, skip the board, do it in this session
- Adjust the plan
```

Number these options, but the numbers must not collide with those of other questions.

- Choosing `Confirm the plan and use the kanban board (create the card and start)` authorizes the plan, the development, and the kanban flow at once; do not ask again before starting work.

  For a single card, run in order: `kander new`, fill in the complete contract according to the confirmed plan, complete the self-review and any applicable independent card review per `KANDER-KANBAN-RULES.md` "Post-Creation Self-Review" and fix the findings, `kander pick <task-id>`, `kander start <task-id>`. Start and tracking responsibilities follow `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination"; the discussing agent no longer implements a card that has been delegated.

  Only when `rules.task_groups=true` and `rules.git=true`, read `KANDER-TASK-GROUP-RULES.md` as needed for splitting and orchestration. When task groups are disabled, execute the confirmed standalone single-card contract; do not split into a group automatically or load the disabled module.

- Choosing `Confirm the plan, skip the board, do it in this session` implements directly per the project rules, without creating a card.
- Choosing `Adjust the plan` modifies the plan; no card is created or started yet.
