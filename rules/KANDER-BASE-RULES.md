# Kander Minimal Tool Protocol

This file constrains the tool and the language used with the user. It does not prescribe communication style, architecture, code verification, Git, or automatic review flows; those are the optional modules.

- Read the current scope's configuration first, as described in `KANDER-AGENTS.md`.
- Talk to the user, and write cards, records, and reports, in the `agent_language` from the configuration, or in the card's `LANGUAGE` when working on a task card.
- Read `KANDER-KANBAN-RULES.md` whenever kanban commands are used; no optional module needs to be enabled.
- Tool boundary failures must be reported. Bypassing them with ordinary file operations, or by controlling the agent directly, is forbidden. The module switches never change arguments, data structures, path validation, or process isolation.

## Single Review

- `kander review` is a single-review tool that can be invoked explicitly. Invoking it does not enable the full review or Git flow and does not require the target branch to be `develop`. Arguments: `[agent] [--task <id>]... [--run-id <id>] [--batch-id <id>] [--previous-run-id <id>] [--requirements-file <JSON>] [--advance-file <JSON>] <CWD> <base-commit> <commit> <role> <task-goal|absolute spec path> [review-context] [reviewed-commit]`.
- The target must be a clean Git worktree, and base must be an ancestor of commit.
- Without `--task`, no board is located. With `--task`, flags precede CWD, repeated task IDs are deduplicated, and the board is located from the target CWD. Cards must be working/review directory cards with one language and compatible task group membership.
- Task-bound review requires an explicit batch ID. A new batch also requires a JSON requirements file with exactly the keys `PMQA` and `Security`, each valued `required` or `N/A: <reason>`. Resolve these from user/project rules and stage policies; the file records that decision and grants no approval.
- A missing run ID is generated and printed to stderr. Reuse a run ID only to recover or finish publication of the same run; it never launches another reviewer, and changed inputs conflict. Use distinct run IDs for different roles on the same target. A retry after a process crash records interrupted evidence, never PASS.
- New runs and batches accept only the roles `PMQA` and `Security` (case-insensitive). Historical four-key (`PM`, `QA`, `CSA`, `Hacker`) and six-key batches stay readable, retryable, and closable with their original roles; never rewrite their requirements, role order, run IDs, originals, hashes, closures, or dispositions.
- Raw output, logs, input snapshots, sidecar, and manifest stay in each card's `reviews/<run_id>/`; the machine-owned REVIEWS section holds one JSON index line per run. Tool success is separate from semantic PASS. Never edit or delete these artifacts.
- Publication is atomic per card. A partial publication exits nonzero and preserves the cards that succeeded. After an interrupted board transaction, run `kander init` under its maintenance requirements, then retry the identical invocation with the same run ID. An incompletely published run cannot establish completion.
- Built-in reviewers are kept read-only through the isolation arguments on each agent definition, and their output is validated. A custom reviewer's read-only posture is its definition author's responsibility. Kander isolates the review-private directories for every reviewer and afterwards checks the Git-visible state of the target worktree; writes outside the worktree or to ignored paths are not detected.

## Installation and Task Files

- Automation must invoke the command root's `kander` through a process API argv array; do not assemble shell command strings.
- The executing agent and the reviewer read the complete task from a UTF-8 temporary file named by a one-line instruction on the command line. The file asks the agent to delete it when done; a failed deletion does not affect the result.

## Permissions and Boundaries

- Review-private directories and files are accessible only to the current user; the configuration, the kanban board, Git exclude, and the review runtime all reject symlinks, junctions, and other reparse points, and a failed safety check stops the operation.
- Bypassing the command to operate on these boundaries directly is forbidden.
