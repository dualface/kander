# kander review

Source: moved from `KANDER-BASE-RULES.md` "Single Review"; the evidence subcommands are specified in `snippets/review-evidence-gate.md` "Review Evidence Completion Gate"

## Purpose and users

Runs one reviewer role on a commit range and publishes the evidence, and manages review plans, assignments, dispositions, and closures. The single review entry can be invoked explicitly by any agent; the full review workflow is `KANDER-REVIEW-RULES.md`.

## Syntax

```text
kander review [agent] [--task <id>]... [--run-id <id>] [--batch-id <id>] [--previous-run-id <id>] [--requirements-file <JSON>] [--advance-file <JSON>] <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]
kander review plan <absolute-CWD> <absolute-plan.json>
kander review extend-plan <absolute-CWD> <absolute-extension.json>
kander review assign <absolute-CWD> <absolute-assignment.json>
kander review disposition <absolute-CWD> <absolute-author-record.json> <expected-card-revision>
kander review interpret <absolute-CWD> <absolute-interpretation.json>
kander review map-legacy <absolute-CWD> <absolute-map.json>
kander review aggregate <absolute-CWD> <batch-id>
kander review advance <absolute-CWD> <absolute-advance-request.json>
kander review close <absolute-CWD> <absolute-close-request.json>
kander review progress <absolute-CWD> <task-id>
kander review <plan|extend-plan|assign|disposition|interpret|advance|close> --schema
```

## Arguments

- Reviewer run: `[agent]` an optional configured agent with a review template; `--task <id>` (repeated) binds the run to cards; `--run-id`, `--batch-id`, `--previous-run-id` identify the run, its batch, and the predecessor of an incremental round; `--requirements-file <JSON>` names the batch requirements for a new batch; `--advance-file <JSON>` advances the batch target; then `<CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]`.
- Evidence subcommands: the words `plan`, `extend-plan`, `assign`, `disposition`, `interpret`, `map-legacy`, `aggregate`, `advance`, `close`, and `progress` select an evidence subcommand; each takes an absolute CWD and a JSON file or an ID as shown in Syntax. `--schema` prints the JSON schema of a subcommand's input.
- `batch` is not a subcommand (no documented caller): batches are named in the plan by `plan` and `extend-plan`, advanced by `advance`, and closed by `close`.

## Preconditions

- The target must be a clean Git worktree, and base must be an ancestor of commit.
- Without `--task`, no board is located. With `--task`, flags precede CWD, repeated task IDs are deduplicated, and the board is located from the target CWD. Cards must be working/review directory cards with one language and compatible task group membership.
- Task-bound review requires an explicit batch ID. A new batch also requires a JSON requirements file with exactly the keys `PMQA` and `Security`, each valued `required` or `N/A: <reason>`. Resolve these from user/project rules and stage policies; the file records that decision and grants no approval.

## Effects

- `kander review` is a single-review tool that can be invoked explicitly. Invoking it does not enable the full review or Git flow and does not require the target branch to be `develop`.
- A missing run ID is generated and printed to stderr. Run and batch IDs are board-wide, so a chosen ID starts with the task ID (or task-group ID), never a short generic name. Reuse a run ID only to recover or finish publication of the same run; it never launches another reviewer, and changed inputs conflict: restore the configuration and pass the frozen task context, or use a new run ID in the same batch. Use distinct run IDs for different roles on the same target. A retry after a process crash records interrupted evidence, never PASS.
- New runs and batches accept only the roles `PMQA` and `Security` (case-insensitive). Historical four-key (`PM`, `QA`, `CSA`, `Hacker`) and six-key batches stay readable, retryable, and closable with their original roles; never rewrite their requirements, role order, run IDs, originals, hashes, closures, or dispositions.
- Raw output, logs, input snapshots, sidecar, and manifest stay in each card's `reviews/<run_id>/`; the machine-owned REVIEWS section holds one JSON index line per run. Tool success is separate from semantic PASS. Never edit or delete these artifacts; only Kander removes them, after every card they belong to is archived.
- New readable review reports may await receiver interpretation when their findings format cannot be extracted. `kander review interpret <CWD> <absolute-JSON>` publishes an immutable, attributed reading with original hash and exact quotes; `--schema` describes its fields. Valid structured reports are extracted automatically. Zero findings need explicit original evidence; pending interpretation cannot establish completion, and failed executions cannot be converted to success.
- Publication is atomic per card. A partial publication exits nonzero and preserves the cards that succeeded. After an interrupted board transaction, run `kander init` under its maintenance requirements, then retry the identical invocation with the same run ID. An incompletely published run cannot establish completion.
- Built-in reviewers follow the isolation posture declared on each agent definition; reviewers whose arguments do not enforce read-only rely on the prompt and the post-run check, and their output is validated. A custom reviewer's read-only posture is its definition author's responsibility. Kander isolates the review-private directories for every reviewer and afterwards checks the Git-visible state of the target worktree; writes outside the worktree or to ignored paths are not detected.
- The evidence subcommands and their gates (plans, batches, assignments, author dispositions, aggregation, advance, closure, progress) are specified in `snippets/review-evidence-gate.md` "Review Evidence Completion Gate".

## Output

- The reviewer run prints the generated run ID on stderr and the report on stdout; task-bound runs publish into each card. `aggregate` prints the generated disposition view (preserve its exact bytes for `close`); `progress` prints the plan state, with `exclusion` for a member that left the plan and optional `next_actions` (`{action, batch_id, role, run_ids, task_id}`; `append-cycle-batch` names a rebound member with no batch; `supersede-duplicate-runs` lists two or more successful chain heads of one role in an unclosed batch; only heads that qualify as parallel duplicates, or root runs with a user decision reference and the runs that continue them, can go into `superseded_runs`); `--schema` prints the input schema.
- A new run prints a stderr warning when another run of its batch and role is outside its predecessor chain and is still running or a successful chain head; finalized failed runs never trigger it. The run continues and the exit code is unchanged. Resolve the duplicate with `--previous-run-id` or the close request's `superseded_runs`.

## Rejections

- A dirty worktree, a base that is not an ancestor, a reused run ID with changed inputs, a four- or six-key new batch, a batch outside the plan of a member (the error names `extend-plan`), a target that differs from a frozen task context (the error names the frozen snapshot), a worktree other than the plan's `cwd`, conflicting review pins of one batch (change a pin with `kander update --contract-decision-file`), incomplete publication, or a gate failure of an evidence subcommand.
- Fewer than 5 or more than 7 positional arguments, a missing JSON path, or `--schema` on a subcommand without a schema is a usage error.
