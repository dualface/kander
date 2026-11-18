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

- `[agent]`: an optional configured agent with a review template.
- `--task <id>`: repeated; binds the run to cards.
- `--run-id <id>`, `--batch-id <id>`, `--previous-run-id <id>`: identify the run, its batch, and the predecessor of an incremental round.
- `--requirements-file <JSON>`: names the batch requirements for a new batch.
- `--advance-file <JSON>`: advances the batch target.
- `<CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]`: the positional arguments of a reviewer run, after its flags.
- `plan`, `extend-plan`, `assign`, `disposition`, `interpret`, `map-legacy`, `aggregate`, `advance`, `close`, `progress`: select an evidence subcommand; each takes an absolute CWD and a JSON file or an ID as shown in Syntax.
- `confirm`: the subcommand that confirms a prepared publication as its holder; see "Additional Syntax".
- `--schema`: prints the JSON schema of the input of a subcommand.
- `batch`: not a subcommand (no documented caller); batches are named in the plan by `plan` and `extend-plan`, advanced by `advance`, and closed by `close`.
- `resolve_batch`: a field of the `extend-plan` input that accepts an unplanned batch that already ran, on the evidence of one closed planned batch (`snippets/review-evidence-gate.md` "Review Evidence Completion Gate").

## Examples

```text
# Run one reviewer role on a commit range
kander review /tmp/worktree origin/develop HEAD PMQA "Review the example change"

# Publish a review plan
kander review plan /tmp/worktree /tmp/plan.json

# Print the input schema of the plan file
kander review plan --schema

# Extend a review plan
kander review extend-plan /tmp/worktree /tmp/extension.json

# Assign a reviewer to a batch
kander review assign /tmp/worktree /tmp/assignment.json

# Record the disposition of the findings
kander review disposition /tmp/worktree /tmp/record.json 3

# Record how the receiver reads a report
kander review interpret /tmp/worktree /tmp/interpretation.json

# Map a legacy batch to the plan
kander review map-legacy /tmp/worktree /tmp/map.json

# Print the disposition view of a batch
kander review aggregate /tmp/worktree 20261118-example-task-batch-1

# Advance the target of a batch
kander review advance /tmp/worktree /tmp/advance.json

# Close a batch
kander review close /tmp/worktree /tmp/close.json

# Print the progress of a card in its plan
kander review progress /tmp/worktree 20261118-example-task

# Confirm a prepared publication as its holder
kander review confirm /tmp/worktree publication-1 20261118-example-task
```

## Preconditions

- The target must be a clean Git worktree, and base must be an ancestor of commit.
- Without `--task`, no board is located. With `--task`, flags precede CWD, repeated task IDs are deduplicated, and the board is located from the target CWD. Cards must be working/review directory cards with one language and compatible task group membership.
- Task-bound review requires an explicit batch ID. A new batch also requires a JSON requirements file with exactly the keys `PMQA` and `Security`, each valued `required` or `N/A: <reason>`. Resolve these from user/project rules and stage policies; the file records that decision and grants no approval.

## Effects

- `kander review` is a single-review tool that can be invoked explicitly. Invoking it does not enable the full review or Git flow and does not require the target branch to be the mainline branch.
- A missing run ID is generated and printed to stderr. Run and batch IDs are board-wide, so a chosen ID starts with the task ID (or task-group ID), never a short generic name. Reuse a run ID only to recover or finish publication of the same run; it never launches another reviewer. A different Kander version is not a changed input, and the run keeps its original `kander_version`. Changed inputs conflict: restore the configuration and pass the frozen task context, or use a new run ID in the same batch. Use distinct run IDs for different roles on the same target. A retry after a process crash records interrupted evidence, never PASS.
- New runs and batches accept only the roles `PMQA` and `Security` (case-insensitive). Historical four-key (`PM`, `QA`, `CSA`, `Hacker`) and six-key batches stay readable, retryable, and closable with their original roles; never rewrite their requirements, role order, run IDs, originals, hashes, closures, or dispositions.
- Raw output, logs, input snapshots, sidecar, and manifest stay in each card's `reviews/<run_id>/`; the machine-owned REVIEWS section holds one JSON index line per run. Tool success is separate from semantic PASS. Never edit or delete these artifacts; only Kander removes them, after every card they belong to is archived.
- New readable review reports may await receiver interpretation when their findings format cannot be extracted. `kander review interpret <CWD> <absolute-JSON>` publishes an immutable, attributed reading with original hash and exact quotes; `--schema` describes its fields. Valid structured reports are extracted automatically. Zero findings need explicit original evidence; pending interpretation cannot establish completion, and failed executions cannot be converted to success.
- Publication is atomic per card. A partial publication exits nonzero and preserves the cards that succeeded. After an interrupted board transaction, run `kander init` under its maintenance requirements, then retry the identical invocation with the same run ID. An incompletely published run cannot establish completion.
- Built-in reviewers follow the isolation posture declared on each agent definition; reviewers whose arguments do not enforce read-only rely on the prompt and the post-run check, and their output is validated. A custom reviewer's read-only posture is its definition author's responsibility. Kander isolates the review-private directories for every reviewer and afterwards checks the Git-visible state of the target worktree; writes outside the worktree or to ignored paths are not detected.
- The evidence subcommands and their gates (plans, batches, assignments, author dispositions, aggregation, advance, closure, progress) are specified in `snippets/review-evidence-gate.md` "Review Evidence Completion Gate".

## Output

- The reviewer run prints the generated run ID on stderr and the report on stdout; task-bound runs publish into each card. `aggregate` prints the generated disposition view (preserve its exact bytes for `close`; with `--json` use the `view_hash` field, see below); `progress` prints the plan state, with `exclusion` for a member that left the plan, `resolved_batches` with the result of each finding of a resolved unplanned batch, and optional `next_actions` (`{action, batch_id, role, run_ids, task_id}`; `append-cycle-batch` names a rebound member with no batch; `supersede-duplicate-runs` lists two or more successful chain heads of one role in an unclosed batch; only heads that qualify as parallel duplicates, or root runs with a user decision reference and the runs that continue them, can go into `superseded_runs`); `--schema` prints the input schema.
- A new run prints a stderr warning when another run of its batch and role is outside its predecessor chain and is still running or a successful chain head; finalized failed runs never trigger it. The run continues and the exit code is unchanged. Resolve the duplicate with `--previous-run-id` or the close request's `superseded_runs`.
- With `--json`, the reviewer run is one object: `exit_code`, `report` (the report text), and for a task-bound run `run_id`, `batch_id`, `execution_status`, `failure_reason`, and `task_ids`. The run ID line, the card publication lines, and the other stderr remarks are notices (`review_archive_identity`, `review_card_published`, `review_interpretation_pending`, `review_process_evidence`, `review_output`, `review_error`). A run whose evidence reached every card, or that succeeded, is this object with the exit status of the run; any other failure is the error object with the code `review_failed`. A run that was prepared in the archive but not published (a failed finalize, a card that refused the publication, a joint confirmation still pending) is that error object too, and its message ends with the run ID and batch ID of the prepared run. `--schema` prints `command` and `schema` (the schema text). The evidence subcommands print the same object as without `--json`, on one line, with these fields:

  Field types: `string`, `integer`, `boolean`, `array of X`, `map X to Y` (a JSON object whose keys are of type X and values of type Y), `object`. A field in parentheses after `when present` is left out when it has no value.

  - `plan`, `extend-plan` (including `resolve_batch`): `plan_id` (string): the plan that was created or extended.
  - `advance`: `batch_id` (string): the batch whose target moved.
  - `assign`, `interpret`, `map-legacy`: `run_id` (string): the run that was assigned, interpreted, or mapped.
  - `disposition`: `record_id` (string): the author record that was stored.
  - `confirm`: `publication_id` (string): the confirmed publication; `task_id` (string): the card; `status` (string): always `confirmed`; `digest` (string): the lowercase SHA-256 of the confirmed publication.
  - `aggregate`: the disposition view. `schema` (integer): the view schema; `batch` (object): the batch (`batch_id` string, `plan_id` string when present, `previous_batch_id` string when present, `task_ids` array of string, `base` string and `target_commit` string, both full commits, `requirements` map role to string (`required` or `N/A: <reason>`), `advances` array of object, `report_language` string, `task_context_hash` string, `revision` integer: the value to put in `expected_revision`, `schema` integer); `runs` (array of object): one entry per run with `run` (object: the run record, `run_id`, `role`, `execution_status`, `commit`, and the other run fields), `findings_status` (string when present: `pending` while an interpretation is awaited), `findings` (object when present: `FINDINGS` and `NON_BLOCKING`, arrays of finding objects with `id`, `tier`, `text`, `evidence`), `assignment` (object when present: the `assign` record, `items` maps a finding ID to an array of task ID), `records` (array of object: the author dispositions, newest last), `interpretation` (object when present), and `withdrawn` (array of object when present: records whose finding was withdrawn with its member). With `--json` also `view_hash` (string): the lowercase SHA-256 of the indented text form of the view, see below.
  - `close`: the closure. `schema` (integer); `plan_id` (string); `batch_id` (string); `target_commit` (string): the final target, a full commit; `request` (object): the close request as stored (`batch_id`, `expected_revision` integer, `view_hash` string, `author` string, `roles` map role to object `run_id` `passed_at` `basis`, `resolved_failures` map failed run ID to replacement run ID, `superseded_runs` map run ID to object when present, `mechanical` array of object when present, `opinions` array of object); `git` (object): the Git evidence (`cwd` string, `head` string, `verified_at` string, `edges` array of object, `no_delivery` boolean when present, `observed_head` string when present, `not_applicable` string when present, `mechanical` array of object when present); `role_statuses` (map role to string): the status of each role: `PASS` for a role with a passing conclusion, the `N/A: <reason>` text of the plan for a role marked N/A, or the waiver policy of a waived role; `view` (object): the aggregate view the close bound; `closed_at` (string): the RFC 3339 time of the closure.
  - `progress`: the plan state of one card. `task_id` (string); `status` (string): `legacy-untracked`, `requirements-needed`, `plan-target-mismatch`, `pending`, `closed`, or `excluded`; `plan_id` (string when present); `exclusion` (object when present: why the card left the plan; `schema` integer, and strings `exclusion_id`, `task_id`, `plan_id` and `task_group` when present, `from_state`, `state`, `result`, `cycle` (the execution cycle of the card), `reason`, `decision`, `recorded_at`); `pending` (array of string): the open obligations, one identifier per entry: `unsealed-plan`, `unassigned-member`, `cycle-batch:<member>`, `recovery-review:<member>`, `recovery-role:<member>/<role>`, `dropped-review:<member>/<role>`, `stale-run:<batch>/<role>/<member>`, or the ID of a batch that is not closed; `rebind_cycles` (map task ID to string, when present): the complete map to send in `rebind_cycles` of `extend-plan`, each value the current execution cycle of that card; `plan_target` (string when present) and `batch_target` (string when present): full commits; `batch_id` (string when present); `next_actions` (array of object when present): `action` (string), `batch_id` (string), `role` (string), `run_ids` (array of string), `task_id` (string when present); `resolved_batches` (array of object when present): one entry per resolved unplanned batch with `request` (object), `author` (string), `basis` (string), `batch_revision` (integer), `runs` (array of object: the result of each finding), `git` (object), `recorded_at` (string).

  The close request binds the SHA-256 of the exact text form of the view: the two-space indented JSON with a final newline. The single line of `aggregate --json` is not that text, so its `view_hash` field is the digest to put in `view_hash` of the close request; never hash the line itself, and never reserialize it.

## Rejections

- A refusal over a value the request names (`expected_revision`, `view_hash`, `previous_target`, `previous_record_id`, `rebind_cycles`, `sync_targets`) keeps its label as the start of the message and then writes the value the request holds, the value the board holds (a request value that does not have the form of its field, such as a short SHA in `previous_target` or text in `view_hash`, is written as `<invalid>`), and the next step, which is to read the current state again (`kander review aggregate` or `kander review progress`, and for the revision of a plan `kander show --document reviews/plan.json <task-id>`) and to confirm that the decision still holds before the request is written again. A message never writes a token, a claim or lease proof, an execution credential, a report body, or a disposition reason. A close request with no `author` is refused as `close author required`, an advance whose `previous_target` is the batch target but whose `target` does not move, is no full commit, or has no `reason` is refused as `batch target invalid` (only a different `previous_target` is `batch target CAS conflict`), and a batch or plan that is not there is refused as `missing batch` or `missing plan` with its ID; neither is a CAS refusal. A review over a member that is not in `working/` or `review/` names the member and its state. For a member in `backlog/` or `todo/` it offers to wait until the member starts and, when the card has a `TASK_GROUP` and is still an unstarted member, `kander coordinator remove-member` after the user decides (see `coordinator.md`); it never offers `remove-member` for a card without a group or in `done/`, `archived/`, or `trash/`.
- A dirty worktree, a base that is not an ancestor, a reused run ID with changed inputs, a four- or six-key new batch, a batch outside the plan of a member (the error names `extend-plan`), a `resolve_batch` request that fails a condition of the gate snippet, a `close` on a conclusion run of another execution cycle, a `seal` over a dropped `required` batch with no planned replacement, a `release_obligation` with no `decision` or nothing to release, a `release_obligation` of kind `recovery` under a plan that requires a role, a target that differs from a frozen task context (the error names the frozen snapshot), a worktree other than the plan's `cwd`, conflicting review pins of one batch (change a pin with `kander update --contract-decision-file`), incomplete publication, or a gate failure of an evidence subcommand.
- Fewer than 5 or more than 7 positional arguments, a missing JSON path, or `--schema` on a subcommand without a schema is a usage error.

## Additional Syntax

```text
kander review confirm <absolute-CWD> <publication-id> <task-id>
```

## Distributed holder confirmation (v2)

With a server binding, a review operation over several claimed cards first prepares immutable material and each member's exact task revision and execution generation. Each holder independently confirms its member. `review confirm` reads the prepared publication and uses only the calling process's execution credential; it cannot borrow a successor's generation. Read the material before confirming. A changed task, expired lease or takeover invalidates the prepared authority and requires new coordination.

A single-card operation is proven directly by the card's holder. The process that coordinates the card's task group (the orchestrator session, which holds the group coordinator credential and no credential of the card's current execution generation) uses the same confirmation flow for a single card: the command ends with `evidence_incomplete` and names the publication ID, the card, and `kander review confirm <absolute-CWD> <publication-id> <task-id>`, and the client asks the holder to confirm. After the holder confirmed, run the same command again with the same arguments. For `kander review`, which waits this way during prepare or publish, the message also names the run ID; rerun it with that same `--run-id`, because without it the command generates a new run ID and the holder must confirm new material. Any other process without a credential for the card is refused with `held_by_other`, naming the holder's execution ID, owner, and generation, and `review disposition` is accepted only from the holder.

A coordinator may publish that exact material after all confirmations. The service checks the whole set inside the same redo transaction that publishes the member copies and completed operation receipt. A partial set publishes no card evidence and survives server restart. Retrying an already completed publication returns its saved result. The server neither runs Git nor launches a reviewer or agent.

## Distributed historical resolution and member changes

The `extend-plan` operation with `resolve_batch` reads a service snapshot of the plan, historical batch, report hashes, membership and required Git edges. The client fetches missing commits from the common remote and verifies ancestry locally. The service compares the exact inspected originals again in the resolution transaction, then publishes the plan copies, audit and receipt atomically under current-holder or joint authority. Historical CWD bytes remain unchanged; they identify old evidence rather than the client's checkout.

Joint retries preserve the original verified payload while it waits for confirmations. Rechecking the same immutable Git edges at another time does not create a new proposal solely because the verification timestamp changed. The client sends the exact material each holder confirmed. A substantive fact, revision or generation change requires a new proposal.

An audited terminal member exclusion lets runtime uploads continue for the remaining live generations. It does not exempt a member that was merely taken over, lost, or completed without an exclusion. Final publication still needs current remaining-holder authority and never rewrites an excluded card.

An already published operation replays its immutable result even if publication advanced the card versions or a member later left. Replay validates the original operation and material and performs no producer write. A still-prepared operation continues to require current confirmations; an old approval never authorizes another generation.
