# kander move

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract"

## Purpose and users

Moves a card between states and records claims, results, and dispatch completions. Used by the executing agent (claim, `review`, `done`), the orchestrator (`todo`), and the card creator; a dispatched executing agent uses the ID/epoch-bound form from its dispatch prompt.

## Syntax

```text
kander move <task-id> <backlog|todo|working|review|done|archived|trash> [--owner <agent> [--decision <reference>]] [--result <result>] [--reason <reason> --decision <reference>] [--duplicate-of <task-id>] [--expect-revision <revision>] [--dispatch-id <id> --execution-epoch <epoch>] [--delivery-commit <full-SHA>] [--disposition <card-relative-path>]
```

## Arguments

- `task-id`, `state`: the card and its new state (`backlog`, `todo`, `working`, `review`, `done`, `archived`, `trash`).
- `--owner <agent>`: manual claiming into `working/` writes `OWNER` and `STARTED_AT`; `--decision <reference>` records the user decision and is required for a reclaim from `review/`.
- `--result <result>`: `completed` for `done/`; `cancelled`, `duplicate`, `wontfix`, or `completed` for `archived/`; `trashed` for `trash/`.
- `--reason <reason> --decision <reference>`: required for `archived/` and `trash/`; they record the user decision.
- `--duplicate-of <task-id>`: the replacement card for `--result duplicate`.
- `--expect-revision <revision>`: compare-and-swap on the card revision.
- `--dispatch-id <id> --execution-epoch <epoch>`: the durable dispatch grant from the dispatch prompt (`snippets/commands/dispatch.md`).
- `--delivery-commit <full-SHA>`: the final delivery commit of a bound completion.
- `--disposition <card-relative-path>`: the disposition artifact of a bound completion.

## Preconditions

- The transition exists in `KANDER-KANBAN-RULES.md` "State Model".
- Into `todo/`: the `todo` gate (`SELF_REVIEW:`, and for large cards and group members `CARD_REVIEW:`, complete contract sections).
- Into `done/`: the completion gates of `snippets/review-evidence-gate.md` "Review Evidence Completion Gate" and the `SUMMARY` or `report.md` written through `update` first.
- Into `archived/` or `trash/`: a result, a reason, and a decision reference. An `archived/` card is final: every later write fails.
- A card that carries a `DISPATCH_ID` accepts only the ID/epoch-bound form.

## Effects

- Moves the whole entry; the task ID never changes. Only `kander` moves cards: `mv`, `cp`, or a file manager is forbidden.
- Manual claiming writes `OWNER` and `STARTED_AT`; `start` and `resume` own `SESSION` and `WINDOW`; dispatch-back moves to `working/` keep the existing owner.
- Completion writes `RESULT` and `FINISHED_AT` in one transaction. A bound completion commits its receipt atomically with the move.
- A move into `archived/` deletes the review evidence and control records that belong only to archived cards (`KANDER-KANBAN-RULES.md` "Archived Review Evidence").

**Reclaiming**

Source: moved from `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination".

- Reclaiming: when a `review/` card's executor has stopped and the user explicitly authorizes a new owner, that agent runs `kander move <task-id> working --owner <agent> --decision <user-decision-reference>`. The move rejects a missing or blank `--decision`, and active dispatch bindings, including a completed dispatch that was not released, still reject `--owner`. When `dispatch fail|cancel` released a binding in the current cycle, the move verifies the release. When the cycle never held a dispatch, no release exists and the move verifies that. In both cases it atomically records a protected `LIFECYCLE_DECISION` handoff binding the old cycle, new cycle, owner, decision reference, card revision, and the released dispatch (empty for a cycle that never held one); other working moves reject `--decision`. Every claim rewrites `OWNER` and `STARTED_AT` and adds a fresh claim identity, so same-minute reclaims also change the review cycle. Rebind the existing plan with the complete `rebind_cycles` map from `review progress`; an existing coordinator checkpoint consumes the release and handoff evidence on reconciliation. A `working/` card is never reclaimed this way: use the authorized `resume --agent` takeover, which preserves the cycle and `STARTED_AT`.

## Output

- `kander move` prints the new path after a successful move; when `rules.reporting=true` and the move enters `done/`, `archived/`, or `trash/` (and is not a dispatch replay), it also prints the prefilled completion report template from `KANDER-REPORTING-RULES.md` on standard error.
- A bound move prints JSON with `dispatch` and `replayed`; start work only for `replayed=false`.
- A move into `archived/` prints what cleanup removed and how much space it freed.
- When a move (not a dispatch replay) turns a GitHub issue's work set from not ready into ready and `issue.result_on_done` is not `off`, it prints a result handoff notice on standard error: the issue, the anchor card, the setting, and the `kander issue result ... --action inspect` command line. The session that ran the move follows `KANDER-ISSUE-RULES.md` "Completion Handoff". Standard output and the exit code are unchanged.

## Rejections

- A revision conflict: read and reconcile the current document before retrying.
- A missing gate record, an unknown state, a missing result or decision, or a plain move on a bound card.
- A missing task ID or state, an option without a value, or an unknown option is a usage error.
