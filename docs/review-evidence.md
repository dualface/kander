# Review Evidence Archive

A standalone review without `--task` keeps its original behavior and does not locate a kanban board. With that option, the main kanban board is located from the target CWD, and an explicit `KANBAN_DIR` override still takes precedence; the process CWD is not modified.

```text
kander review [agent] [--task <id>]...
  [--run-id <id>] --batch-id <id>
  [--previous-run-id <id>]
  [--requirements-file <absolute-json-path>]
  [--requirement-decision <ROLE>=<decision-id>]...
  [--advance-file <absolute-json-path>]
  <CWD> <base> <commit> <role> <task-goal|absolute-spec-path>
  [review-context] [reviewed-commit]
```

All options come before the CWD. Repeated task IDs are normalized, sorted, and deduplicated; `--requirement-decision` may be repeated once per role; no other option may be repeated. run/batch IDs are 1–64 lowercase ASCII letters, digits, or hyphens, with the first character restricted to a letter or digit. The run ID may be omitted; it is then generated randomly and printed to stderr. The batch ID is supplied by the caller. Timestamps carry no identity and no causal ordering.

Both creating the intent and publishing to a not-yet-published card require a directory card in working/review; multiple cards must belong to the same non-empty task group and share the same language. Legacy file cards must be migrated per the init maintenance protocol. A review over a member in another state is refused as `<task-id>: working/review`; the message names the member and its state, and for a `backlog/` or `todo/` card of a task group it also names `kander coordinator remove-member` as the user's option while the card is an unstarted member. A long review does not hold the card lock; publication relocates cards by ID.

A refusal over an expected value keeps its label as the start of the message and writes the value of the request next to the current value: `close target/revision/view CAS` names the batch, both revisions, and both view hashes; `batch revision CAS conflict`, `plan extension CAS/sealed conflict`, `batch target CAS conflict`, `cycle rebind CAS requires exactly all changed members`, `sync target CAS requires the current batch target`, and `disposition predecessor CAS conflict` name the values of their fields. A request that is no CAS refusal has its own label: `close author required`, `batch target invalid`, `missing batch`, `missing plan`, and for a kernel refusal with no cause found in the request `batch close refused` or `plan extension refused` with the false facts or the state condition that does not hold.

## Batches and Predecessors

The requirements file for a new batch must list exactly `PMQA` and `Security`. Each value is required or N/A with a reason:

```json
{
  "PMQA": "required",
  "Security": "N/A: repository rule"
}
```

The caller resolves role requirements according to the user, project rules, and configuration; this file does not mean the tool can prove user authorization. Membership, base, requirements, the task-context hash, and language are fixed. Publication appends the review index to each member spec, so a later run that passes the live spec always drifts from the frozen hash. When only the task context differs, the run is rejected before launch with a dedicated error naming the batch. That error also names the absolute path of the snapshot to pass: `reviews/<run-id>/task-context.md` in the first member, by task ID, that published the chosen run. The chosen run is the given previous run when it is finalized and carries the frozen hash; otherwise it is the earliest such run by creation time. With no such run, the error names the run's `inputs/task-context.md` in the control directory, and a new batch is no way out while the plan names this one. Any other binding mismatch still reports `batch binding conflict`. Later invocations may omit requirements; when provided, it must match the existing requirements.

A run names every batch member except members that left the plan (see [Member exclusion](review-disposition.md#member-exclusion)). A batch with no member left never closes. An assigned finding of it blocks the other members of the plan while it has no record, or its latest status is `confirmed` or `unverifiable`, until a `withdrawn` record ends that. A batch outside the plan of a member is refused before it exists, with an error that names `kander review extend-plan`. A plan is refused when a member already has a run in a batch the plan does not name, and several planned batches must be continuous. A run an older binary left outside the plan fails the gate until `extend-plan` with `resolve_batch` accepts its batch; see [Unplanned batch resolution](review-disposition.md#unplanned-batch-resolution). The record binds the batch revision, every run ID, and every report hash, and it writes nothing into `reviews/<run_id>/` or the `REVIEWS` index.

The task context of a later run must equal the frozen one. The error names the published snapshot, or the run's `inputs/task-context.md` in the control directory before publication. A replay with changed inputs conflicts, and the error names both ways out: restore the inputs, or use a new run ID. A worktree other than the plan's `cwd` is refused, and the only way out is to recreate the worktree at that path.

Fixes do not open a new batch. Advancing the target requires an advance file (the board decides the advance through the generated plan kernel; see [Formal verification](formal-verification.md)):

```json
{
  "previous_target": "<old-full-sha>",
  "target": "<new-full-sha>",
  "reason": "fix delivery for this batch's tasks",
  "deliveries": {
    "<member-delivery-full-sha>": "20260907-example-task"
  },
  "foreign_commits": {
    "<outside-delivery-full-sha>": "concurrent delivery outside this batch"
  }
}
```

The advance command file wraps this object: `{batch_id, expected_revision, advance}`. `kander review advance --schema` prints those fields. `passed_at` on close is a full commit SHA, not a timestamp. Review verifies that old is an ancestor of new and that every commit in old..new appears exactly once in either `deliveries` or `foreign_commits`. A delivery must be assigned to a member of this batch. Every foreign commit needs a nonempty reason. Both mappings reject commits outside the range, and the same commit cannot appear in both. The board CASes the current target inside the group control lock, saving the old/new targets, attribution evidence, basis, and version. Attribution is a fact supplied by the caller; there is no promise of deriving business assignment from arbitrary code. Outside deliveries must be recorded as foreign and must not be misrepresented as fixes for this batch. Advancing is refused while any run of this batch is executing or not fully published. Existing advance files whose range contains only member deliveries remain valid without `foreign_commits`.

A fresh run never refuses to start because of other runs of its role. After its intent is committed, `kander review` prints a localized stderr warning when the batch holds a run of the same role outside the new run's predecessor chain that is not finalized or is a successful chain head (no other successful run names it as `previous_run_id`). Finalized failures are left out, because a failed run is retried as a fresh run and bound through `resolved_failures`. A failure to read the other runs prints a notice and the run continues. The duplicate is resolved at close through `superseded_runs`; see [Parallel duplicate runs](review-disposition.md#5-parallel-duplicate-runs).

An incremental round automatically reads the original report and author dispositions via previous-run-id; the caller's review-context is preserved verbatim as a separate supplement. reviewed-commit may be omitted; when passed explicitly it must match. The predecessor must share the same batch/base/role/reviewer and be fully published. For disposition, plans, and batch close, see [Review completion gate](review-disposition.md); this protocol does not auto-pass based on the word PASS appearing in a report.

## Roles and Historical Compatibility

New `kander review` invocations and two-key batches accept only `PMQA` and `Security` (case-insensitive). Explicit `PM`, `QA`, `CSA`, `Hacker` and `CodeSecurityAnalyst` are rejected unless the target batch's frozen requirements mark that historical role `required`, or the same-ID replay is already a run of that role. PMQA checks the task contract first, then quality criteria; Security performs trust-boundary analysis before exploit-chain analysis, reporting each root cause once. Both roles default to `auto`.

New batches use the exact two-key requirements form shown above. Creating a four-key or six-key batch is rejected, including when appending a batch to a historical four-role plan. Unknown keys and incomplete sets are rejected.

Reading, same-ID retries and close still accept exact two-key, four-key (`PM`, `QA`, `CSA`, `Hacker`) and six-key objects. Every batch keeps its frozen role set. A run is admitted only when that batch's own requirements mark the role `required`, so an open four-role batch can still finish its original roles; no conversion or prior closure is required on upgrade. Historical requirements, role order, run IDs, indexes, manifests, closures, dispositions, waivers, recovery artifacts and their hashes retain their identities. schema_version remains 1. Security supports the existing verified-finding waiver rules, as do historical CSA and Hacker records; PMQA and historical PM/QA do not. See the [completion gate](review-disposition.md) for the executing agent's dispositions and closure requirements.

## Project Policy Check

On a server whose project has a policy, a new batch (created by `kander review --requirements-file`, `kander review plan`, or `kander review extend-plan`) is checked against it under the project lock before any receipt is written; the rules are in [Project policy](project-policy.md#review-batches). The request carries the client's `policy_revision`: a stale one is `policy_revision_stale` (HTTP 409), and a role the policy requires that the requirements mark N/A without a decision record on a member is `policy_requirement_unmet` (HTTP 400); either writes no batch, run, staging file, or receipt. The decisions come from `--requirement-decision <ROLE>=<decision-id>` or from `requirement_decisions` of a batch of `plan` and of the `batch` of `extend-plan`.

The batch record of a new batch stores `policy_revision` (omitted when 0) and `requirement_decisions` (the decisions the check used, omitted when none). The fields are not part of the run identity or of any idempotency receipt. `kander review aggregate` shows them in `batch`; `kander review progress` lists them in `batch_policies`, keyed by the planned batches that have them, and omits the field when none has.

Existing batches are never checked again. A batch created before the check, including every historical four-role and six-role batch, has neither field, and reading it, retrying a run, an incremental run, and closing it behave as before.

## Original Artifacts and Schema

Each card stores:

```text
reviews/<run_id>/
  task-context.md
  review-context.md
  prompt.txt          # present when preparation succeeded
  review-contract.md  # rendered runtime contract; present when preparation succeeded
  evidence.txt        # present when preparation succeeded
  output.raw
  stdout.log
  error.log
  report.md           # present when the output decoded validly
  process-evidence.json # POSIX only; present when a reviewer process group member was observed
  sidecar.json
  manifest.json
  interpretation.json # optional receiver reading, separately published after the run
```

`evidence.txt` lists the review range, its commit log and changed-file ledger.
An incremental run also lists the fix range and its own log and ledger. The
original Git log and file-ledger commands do not disable Git colors, so explicit
settings such as `color.ui=always` can leave ANSI escapes in the evidence.

New evidence has no diffs or commit tree. The reviewer uses read-only Git in
the worktree named in the bootstrap and follows the contract's guidance for
bounded diff inspection. Existing run IDs can still be retried for recovery,
and archived evidence is not rewritten.

Before launching the reviewer in distributed mode, Kander checks each of the
three artifact request bodies against the service limit. If one is too
large, the localized error identifies the artifact, its size and the request
limit, the largest evidence section when applicable, and how to retry. If
the evidence is too large, narrow the review range and retry with a new run ID.

process-evidence.json holds the bounded process group member records and the cleanup result, in the format of [Process Group Member Records](review-runtime-isolation.md#process-group-member-records). It is written to staging when the process tree is collected, frozen with the other originals, and covered by the manifest hashes. It is optional: a run without it, such as any run of an older binary, stays valid. A run with it is rejected by older binaries, which do not know the file.

raw/log files are stored as-is, including invalid UTF-8. report.md stores the valid result text without rewriting line endings or content; a JSON Reviewer's original JSON is stored separately as output.raw. When there is no valid report, no report.md is fabricated and the index points to output.raw. Failure evidence may contain empty files; the sidecar states explicitly that the run did not start or failed, and an empty file does not indicate success.

sidecar schema 1 contains:

- run_id, batch_id, previous_run_id, task_ids, task_group, role.
- reviewer/model/effort, cwd/base/commit/reviewed_commit, the applicable advance. `cwd` is the resolved worktree root; a planned run must name the plan's directory under `board.SameReviewCWD` (see [Review disposition](review-disposition.md)).
- findings_schema (new runs: 2; historical 0/1 retain their behavior).
- report_language, SHA-256 of the inputs and all original artifacts, kander_version.
- phase, launch_status, execution_status, semantic_status, exit_code, failure_reason.
- created_at, finished_at, duration_ms.
- member_cycles: the execution cycle of each member when the run was created. A run of an older binary has none. A new closure accepts only a conclusion run of the current cycle; a closed batch keeps its runs. See [cycle binding of runs](review-disposition.md#cycle-binding-of-runs).

launch_status is not_started, unknown, or started. Before launching, launching/unknown is persisted first; on successful launch, running/started is written; on launch failure, not_started is recorded as final. execution_status is incomplete during preparation and finally ok, failed, not_started, or interrupted. semantic_status is always unassessed; ok only means the tool's execution verification succeeded and does not equal a semantic PASS.

manifest schema 1 stores the complete input identity, the sidecar hash, and the hashes of the original artifacts. In the body, each line of `## REVIEWS` is `- {JSON}`, with fields run_id, batch_id, role, execution_status, base, commit, previous_run_id, report. That section and the reviews attachments are managed by a dedicated publisher and cannot be modified via update. The manifest and index are not derived from the report body.

A card's LANGUAGE is resolved only when the intent is created; only when missing does it fall back to the configuration at that time. The frozen language does not drift with later configuration changes; a card with a LANGUAGE that disagrees with the frozen value reports a conflict.

New schema 2 runs separate format extraction from execution. A nonempty readable report whose findings cannot be extracted stays `execution_status: ok`; aggregation shows `findings_status: pending` and progress names `interpretation:<run-id>`. A complete receiver interpretation with original hash, attribution and exact quotes resolves it through `review interpret`; valid structured reports need no interpretation. This record is published in one transaction to all members and checked on consumption; it does not become part of the frozen original manifest. The closed batch view binds its full content. See [receiver interpretation](review-disposition.md#receiver-interpretation) for the zero-findings and completion gates. Invalid explicit structured lineage and execution failures remain failed. Historical failures, hashes, same-ID retries and closed views are unchanged.

## Persistence and Recovery

The board reuses the transactions, revisions, locks, and recovery log of [Card transactions](card-transactions.md). The stable control records live at:

```text
kanban/.kander/groups/00000000-review-archive-group/
  batches/<batch_id>.json
  runs/<run_id>/
    run.json
    inputs/
    staging/
    originals/
    sidecar.json
    interpretation.json # optional immutable receiver reading
```

This is a tool-reserved namespace, not a kanban task group, and no group card is created. run.json stores the per-card publication receipts; each card independently holds the complete original artifacts and a non-overwritable manifest. Hashes are for integrity detection and do not defend against arbitrary same-user tampering.

The per-run-ID OS execution lock lives in the stable locks directory, independent of the card lock. While held, it only briefly enters the board, group, and task transactions of card transactions; no code acquires the execution lock in the reverse direction, so no cycle forms. Different roles within a stage may run in parallel; stage one (PMQA) precedes stage two (Security); a concurrent retry of the same run waits for the previous holder to release and then reads its result. Ordinary updates and moves between working and review may run during a long review. done is now checked by the review plan and disposition gate; an incomplete archive cannot legally enter done. If an external legacy program moves the card into done or another terminal state prematurely, the original artifacts remain in the control directory, publication fails, and the old path is not recreated; the default check skips deferred-check states, so `kander check <task-id>` or `kander check --all` must be used to locate incomplete publications. A done card cannot be moved back for reuse, nor patched around the controlled entry point; follow-up handling requires separate confirmation, and retrying with the same run must not promise to automatically repair the terminal state.

Inputs and the intent are committed to disk transactionally first; output is written to controlled staging. Only after process reaping, worktree checks, and runtime cleanup have all reached a conclusion are originals/sidecar frozen; then the original artifacts, manifest, index, and receipt are published atomically per card. Cross-card publication is not one big transaction: on partial failure, successful cards are kept, each card is reported individually, and the exit code is non-zero. A retry verifies successful cards without rewriting them and only fills in the missing ones.

The same run ID with different inputs is a hash conflict. A retry with the same inputs does not launch the Reviewer again, and the Kander version that prepared the run is not an input: a retry from another version is a recovery, and the run keeps its original `kander_version`; it can replay the original report even when the CLI has been removed or the worktree has new modifications. The first run still requires HEAD/clean/ancestry checks. A recovery call still requires the original Git objects, input bytes, and card bindings to be verifiable. If the original spec path moved with the card or the body later changed, the absolute path of the archived task-context.md may be passed instead; identity comparison is based on original-artifact bytes and does not require reusing a stale path. On a zero-card publication, the original inputs remain in the control directory inputs.

After the gate is killed, the OS execution lock is released; an identical invocation marks the not-yet-finalized intent as interrupted, keeps the staged output, and does not fabricate process reaping, cleanup, or a final report. Recovery does not confirm whether a leftover Reviewer is still running; it does not kill processes on its own based on an old PID, nor delete unknown runtimes. Abnormal runtime leftovers should be diagnosed and handled by the operator.

When an S multi-file transaction is interrupted, readers first report pending recovery. Pause writes, satisfy the maintenance conditions, run init, then retry with the same run ID. Do not manually patch the index, delete successful cards, recreate old paths, or switch IDs to bypass an incomplete publication.

All writes go through internal/fs, keeping POSIX private permissions and Windows creation-time protections (DACL/reparse denial). Binary original artifacts are recovered losslessly via the log's base64 byte fields; ordinary text keeps a compatible log format.

## Abandoning a run that cannot be published

A finalized run counts only when every member holds its copy or was skipped. In server mode only the run's writer can publish it, and it needs the writer secret that lives in the private directory of the client that prepared the run, the same client ID, and a claim of every remaining member on the generation the writer recorded. A run can lose all three: the claim is taken over (the generation moves on), the run is looked at from another host, or another client holds the card. Such a run blocks aggregation, `kander check`, and the target advance of its batch for good. `kander review abandon <absolute-CWD> <absolute-abandon.json>` with `{"run_id", "decision", "reason"}` retires it. `kander review abandon --schema` prints the fields.

**Meaning.** The abandoned run is taken out of its batch.

- Nothing is written into a member card: no copy, no manifest, no `REVIEWS` line. The run record gets an `abandonment` object (`decision`, `reason`, `abandoned_at`), and every member becomes published and skipped, so the run counts as settled. The originals stay on the server until the archive cleanup removes them.
- The run is no run of the batch. The batch view lists it in `abandoned_runs` (run ID, decision, reason) and not in `runs`, so its findings bind nothing and nobody has to assign or dispose them. The closure keeps the view and so the audit line. A batch without an abandoned run keeps the bytes of its view.
- It cannot be the predecessor of another run, cannot satisfy a role, and is not published, prepared, or consumed again; each of these is refused with an error that says the run is abandoned. A role of the batch still needs a passing, published conclusion run, so a new run satisfies it.
- A batch with a run abandoned still counts as touched: it cannot be retargeted for that reason.
- The admission of a plan still counts it: a card with no plan whose run is abandoned is refused by a new plan and by `add_members` as before the abandon. [Unplanned Batch Resolution](review-disposition.md#unplanned-batch-resolution) names the way out.

The decision is a reference to a user decision. Kander records it and does not verify it, like the decision of `superseded_runs`, `coordinator release`, and `coordinator remove-member`.

**Guard.** The kernel accepts the abandon (`abandon` of the review run machine) only when all of these hold: the run is finalized; no member holds a copy (every member is unpublished, or published and skipped); the run is not abandoned yet; the decision and the reason are non-empty; the batch is not closed; no run names it as `previous_run_id`; at least one member is in working or review and has not left the batch; and the run read in the transaction is the requested run, with its group bound. A run with no such member needs one member moved back to working first, because without one nobody can confirm the abandon. A run whose members all left the batch is settled by the skip of the publication path instead. The same request again returns the first result and writes nothing, even after the batch closed or a member left; another decision or reason is a conflict.

**Authorization.** The abandon is a review write on the member cards, so it uses the authority of the other review writes (`reviewWrite`, operation `review-abandon`, the members of the run) and no writer credential. With one live member, the holder of its current generation proves it directly. A process that coordinates the group writes the abandon itself, in one command and without confirmation, when the conditions of the coordinator write authority hold for every live member (all in `review/` and in the group, none with a dispatch in progress, the declared revisions and generations current; see [Distributed server](distributed-server.md#review-gate)). When they do not hold, a coordinator asks the holder through the joint publication for one live member: the command ends with `evidence_incomplete` and a review request on the card, and the same command with the same arguments succeeds once the holder confirmed. With two or more live members and no coordinator write, every holder confirms the same material. Any other process is refused with `held_by_other`, and a run with no live member is refused for every caller. A confirmation binds the operation, the request (run ID, decision, reason), the member revisions, and the generations, so it cannot be used for another request. A publish confirmed before the abandon is refused by the kernel afterwards.

**Next step in the errors.** For a finalized run, the `incomplete publication` error of `kander check` and of aggregation keeps its prefix and names the two ways out: publish it again with the same run ID, or retire it with `kander review abandon` and a decision reference. A run that is not finalized has neither way, so its error stays as it was.

**Difference from `superseded_runs`.** `superseded_runs` of the close request retires a successful run that is already published and is outside the chain of its role; its findings stay binding and the run stays in the closure's view. An abandon retires a run that was never published, before any record depends on it, and removes it from the view.

An older server rejects the `abandon` action (`unsupported review action`), and an older client ignores the new fields: it neither shows `abandoned_runs` nor the `abandonment` of a run, and the view it prints and hashes for a batch with an abandoned run lacks the audit line, so a close request built from that output carries a view hash the server refuses. Upgrade the server first, then every client that handles a batch with an abandoned run.

## Consumption Interfaces and Checks

- `board.InterpretReview` / `ReviewNeedsInterpretation`: immutable receiver interpretations and pending-format status; `check` treats a missing interpretation as progress and conflicting copies as damage.
- `board.PrepareReviewRun` / `UpdateReviewRun` / `FinalizeReviewRun`: intent and execution facts.
- `board.PublishReviewRun`: publishes per card and returns failures without erasing successful receipts.
- `board.AbandonReviewRunWithTransaction`: retires a finalized run that cannot be published (see "Abandoning a run that cannot be published").
- `board.ParseReviewIndexes` and ReviewInput/ReviewRun/ReviewBatch/ReviewManifest: shared types; board does not depend on review.
- `board.ReadReviewRun` / `ReadReviewOriginal`: read committed facts and verify original artifacts.
- `board.UnlinkedReviewRuns` / `UnlinkedReviewRunsWarning`: the advisory start-time lookup of same-role runs outside a new run's predecessor chain.
- `board.ReviewPublicationComplete`: verifies cross-card original artifacts, manifests, indexes, and predecessors; it does not judge semantic PASS.
- `kander check`: checks both intents and indexes; a successful zero-card publication does not disappear either. It reports incomplete publications, duplicate/missing/conflicting entries, and hash, language, membership, and predecessor errors, while keeping its original state scope.
- `board.CompactReviewRuns` / `ReviewRunCompacted` / `CompactedReviewRuns`: compaction of settled runs and its state (see "Review Run Compaction").

Evidence lives in the local kanban board; it does not enter Git and is not deleted when temporary reports are cleaned up.

## Pruning on Archive

Archived cards are final (see [Card transactions](card-transactions.md)), so their evidence is removed once nothing can read it again. `board.PruneArchived` runs after every committed move into `archived/` and inside `kander init`; it holds the exclusive board lock for the whole round, so it cannot race a publication, a reader, or a concurrent archive.

- A card is settled when it is in `archived/` and either has no task group or every current member of its group is in `archived/` or `trash/`, so no member can move again. Until then the group's coordinator may still reconcile its archived members, which reads their start, dispatch, and review records, so those stay. A card that is missing, unreadable, in `trash/`, or in any other state is never settled, and an unreadable card keeps every group unsettled because its group is unknown.
- A plan is removed when all its `task_ids` are settled; a batch when all its `task_ids` are settled and its plan is removed or absent; a run when all its `task_ids` are settled and its batch is removed or absent, whether or not it finished or was fully published, since nothing can complete it for final cards. A run whose execution lock another process holds (a review gate still running) is kept with a warning; the cleanup takes each run's lock without waiting, because it already holds the board lock that gates take after theirs. Records that a kept record names as `previous_run_id`, `previous_batch_id`, `batch_id`, or `plan_id` are kept; this is iterated to a fixed point.
- Closures and dispositions go with their batch, `plan-history/<plan>/` with its plan, and `task-plans/<task>.json`, `tracked-cycles/<task>.json`, and `exclusions/<task>.json` with a settled task whose plan is removed or absent. Card copies `reviews/<run>/`, `reviews/batches/<batch>/` and `reviews/plan.json` go with the matching record, and an emptied `reviews/` directory is removed. `start-group/<task>/`, dispatch registry entries naming a settled `task_id`, and the checkpoint directory of a task group are removed too; the checkpoint additionally needs every member it lists to be in `archived/` (a listed member in `trash/` keeps it), while the group's current board members only need to be in `archived/` or `trash/`. No other record type, and nothing outside the review archive, start, dispatch, and task-group directories, is touched; `versions/`, `locks/`, and `issue-results/` stay.
- IDs come only from validated directory names and record fields (`ValidReviewID`, task IDs, group IDs); paths are built from the control root and the card directory, never taken from a record. A record that cannot be read or validated, an invalid ID, an unexpected directory entry, a link, or a special file keeps the item and is reported.
- Order: the archive move stages an intent `00000000-prune-group/pending/<task>.json` in its own transaction; the cleanup merges `receipts/<task>.json` (removed run, batch, and plan IDs) for every affected card first, then deletes card copies, then control records, and removes the intents last. An interrupted or failed round leaves the intents; the next archive or `kander init` recomputes the same set and finishes it, and a repeated round removes nothing. A failure never undoes the archive: `move` still exits 0 and prints a warning.
- `kander check` skips `REVIEWS` index entries and runs listed in the card's receipt; evidence of cards that are not archived is checked as strictly as before. The card body, `REVIEWS` index lines, `plan.md`, `report.md`, and other attachments are never removed.

## Review Run Compaction

A settled run keeps its conclusion, its records, and the hashes of every original, but its large files (the evidence text, the reviewer session log, the prompt, the raw output, and the frozen contexts) have no further reader. `board.CompactReviewRuns` removes them and records the removal in a compaction receipt. It has no command-line entry of its own; `kander server gc` calls it.

**Entry.** `CompactReviewRuns(ctx, root, CompactionOptions{OlderThan, Now, DryRun, KanderVersion}) (CompactionReport, error)`. `OlderThan` is the retention period and must be positive; a zero `Now` means the current time. A dry run only plans: it writes no receipt and removes nothing, and only lock files may be created. `CompactionReport` lists `Runs` (`RunID`, `Files`, `Bytes`) with the files removed, or in a dry run the files that would be removed, their totals `Files` and `Bytes`, `Skipped` (`RunID`, `Conditions`, `Reason`) for every run that was kept with each condition that failed, `Warnings`, and `Err`. On unchanged data a dry run reports the same runs, files, and bytes as the run that follows it.

**Settlement conditions.** A run is compacted only when all of these hold; `Skipped` names each failed one with its `CompactionCondition` number:

1. `run.json` has `phase` `finalized`, or the run has an `abandonment`.
2. `published` and `skipped_members` together cover every `task_ids` member.
3. The batch is closed (`closures/<batch>.json` exists), or the run is abandoned.
4. Every member card is in `done/`, `archived/`, or `trash/`; a missing or unreadable card fails it.
5. The task group of every member that has one is settled, with the meaning of `board.GroupEvidenceSettled`: every member is final and, for a group with a checkpoint, every completed wrap-up of a done member has its wrap-up Git evidence record. Membership is read with the problems of the whole scan (`Board.GroupMembership().ErrFor(group)`): a card the scan could not list, such as one without `spec.md`, an unreadable card, or a card with several `TASK_GROUP` lines fails it for every group it may belong to, and a member whose own membership is ambiguous fails it too.
6. `finished_at` (for an abandoned run without it, `abandonment.abandoned_at`) is earlier than `Now` minus `OlderThan`; a run with neither fails it.
7. The run's execution lock `locks/review-run-<run>.lock` is free. It is taken without waiting; a busy run is kept and reported.
8. No unsettled run names the run as its `previous_run_id`. A run kept for this keeps its own predecessors as well. The locks of every successor of a run are taken too, since a gate working on a successor reads its predecessors.

Condition number 9 (`CompactionRemoval`) marks a run that met every condition but kept files: an invalid receipt, a listed name that is a link or a special file, a tree that holds one, or a failed removal. Such a run is kept whole until the cause is fixed; a failed removal is finished by the next call. An entry of `originals/` or of a card copy that no review producer writes (anything but the originals, `report.md`, `process-evidence.json`, `manifest.json`, `sidecar.json`, `assignment.json`, `interpretation.json`, `legacy-map.json`, and `dispositions/`) is never removed and is listed in `Warnings`, and so is a known name whose entry has another type than its producer writes (`dispositions/` is a directory, every other name a regular file), such as a link or a FIFO; the run is still compacted.

**Removed and kept files.**

| Location | Removed | Kept |
| --- | --- | --- |
| `runs/<run>/inputs/` | the whole directory | — |
| `runs/<run>/staging/` | the whole directory | — |
| `runs/<run>/originals/` | `evidence.txt`, `error.log`, `prompt.txt`, `output.raw`, `stdout.log`, `review-context.md`, `task-context.md`, `review-contract.md` | `report.md` and every other file |
| `runs/<run>/` | — | `run.json`, `sidecar.json`, `dispositions.json`, and every other file |
| member card `reviews/<run>/` | the same eight names as `originals/` | `report.md`, `manifest.json`, `sidecar.json`, `assignment.json`, `dispositions/`, and every other file |

The hashes of removed files stay in `run.json` and in the card manifests; neither file is rewritten. The card body and its `REVIEWS` index are never touched.

**Receipt.** `.kander/groups/00000000-prune-group/compactions/<run>.json`:

```json
{
  "schema": 1,
  "run_id": "<run>",
  "task_ids": ["<task>"],
  "files": [
    {"location": "inputs", "name": "*", "bytes": 6700000},
    {"location": "originals", "name": "evidence.txt", "bytes": 1864640},
    {"location": "card:<task>", "name": "evidence.txt", "bytes": 1864640}
  ],
  "compacted_at": "<RFC 3339 UTC>",
  "kander_version": "<version>"
}
```

`location` is `inputs`, `staging`, `originals`, or `card:<task>`; `inputs` and `staging` are removed as trees and recorded with the name `*`, which covers every file below them. The receipt carries no hash. A receipt is valid only when `run_id` equals its file name, `task_ids` equals the sorted `task_ids` of `run.json`, and every entry uses a known location and name. Only `CompactReviewRuns` writes it; `kander update` and the other commands cannot.

**Order and locks.** Each round takes the exclusive board lock, refuses a pending transaction, plans from one snapshot, and handles at most 50 runs, so a large backlog never holds the board for long; the next round takes the lock again. Within a round it writes every receipt first (merging into an existing one, which a repeated round leaves unchanged), then removes the card copies, then the control files. Removal never goes through a board transaction, so the removed bytes never enter the journal. A failure undoes nothing: an interrupted round is finished by the next call on the same set, and a call on a fully compacted board removes nothing. A run counts in `Runs` once one of its removals completes, also when that removal only took an emptied directory left from an interrupted round, so a dry run and the call that follows list the same runs. A run whose removals all failed is listed only in `Skipped`. The exclusive board lock is taken with the caller's context, and the context is checked again before the first receipt is written: a call cancelled while it waits for a board writer writes and removes nothing.

**Checks.** Every integrity check relaxes only for the files a valid receipt lists: a listed file that is missing passes, a listed file that exists is still compared with its hash, and an unlisted file that is missing or changed is still damage. An invalid receipt is damage. Runs without a receipt are checked as strictly as before. `kander check` prints, for each checked card, one information line that names its compacted runs; it reports no problem for them. A prune receipt still takes precedence: when an archived card's runs are pruned, check skips them as before.

**Reads and retries.** `board.ReadReviewOriginal`, `board.ReadReviewInput`, and `GET /api/v1/projects/{project}/reviews/evidence/{run}/original|input/{name}` refuse a listed file that is gone with the board code `board.review_compacted` (API code `review_compacted`, HTTP 410), never with a hash mismatch; `report.md` and the other kept files stay readable. Any other name below `inputs/` is still never readable. A name that is one plain path segment (no separator, no leading dot, no colon, no trailing space or dot) answers `board.review_compacted` once the `inputs` receipt entry covers it and the file is gone; the check only looks whether the file exists and never reads it. An unsafe name, a run without a receipt, or a file that still exists keeps the name refusal. `GET /api/v1/projects/{project}/reviews/runs/{run}` adds `"compacted": true` to the run of a compacted run; a server that predates compaction never sends the field, so a client reads its runs as not compacted and needs no new route. A receipt that cannot be validated never hides the run: the response still carries the run, with `compaction_error` set to the reason, and `kander review` then refuses a retry of the run ID with that reason instead of reading it as not compacted. Reusing the run ID of a compacted run with `kander review` is refused before any authority, pin, or Git check, with `board.review_run_compacted`, and no reviewer starts; the board refuses a prepare or a new card copy of a compacted run the same way. A replay is not offered because it compares the frozen inputs that compaction removed. A new review takes a new run ID.

**Publications.** `GET /api/v1/projects/{project}/publications/{id}` adds `compacted_runs` to its response: the runs the material names that are compacted now. A run is named by the operation `review-publish:<run>`, and at any depth of the material by a `run_id` or `previous_run_id` field (the prepare input and its predecessor, assign, disposition, interpret, map-legacy, abandon, the role conclusions and mechanical assessments of `review-close:<batch>`, the findings and runs of `review-resolve-batch`), by a `run_ids` list, by a key or `replacement` of `superseded_runs`, or by a key (the failed run) or value (its replacement) of `resolved_failures`. The field is computed per request, is never stored, and is absent when no named run is compacted. A named run whose compaction state cannot be verified, such as one with a damaged receipt, is listed in `compaction_errors` with the reason instead; the material is still returned. `kander review publication` prints it as `compacted_runs`.

**Archive pruning.** When `PruneArchived` removes a compacted run, it removes its receipt first; the card's prune receipt already covers the run, so an interruption between the two leaves no receipt without a run. The compaction receipts of the planned runs are removed right after the prune receipts and before any copy or control record. Each receipt is read through a no-follow, non-blocking handle whose type is checked first, so a FIFO in its place fails the round instead of holding the board lock, and is copied to `00000000-prune-group/compaction-backups/<run>.json` before it is removed. Readers take that copy for a missing receipt, so no interruption leaves a kept run without one; `kander check` also prints the compaction line for a run that has only its copy, as it does when the receipt exists, and a receipt that exists still wins over its copy. Check reads each run's receipt and copy by path, so a receipt directory it cannot list costs only the lines of the runs whose receipts cannot be read. When `runs/` cannot be listed, check takes the runs named in the receipt directories instead, reports the listing error as one more problem, and still prints its summary and the compaction lines it found. When a receipt cannot be copied or removed, the cleanup plans again with that run kept, so everything the kept run reads (its batch, closure, plan, predecessors, and the other runs of its batch) is kept with it. Every run that planning again keeps gets its receipt back and its copy removed; the copy of a deleted run goes after the run. The cleanup reports the failure. Every round starts by settling what an interrupted round left there: a run that still exists gets its receipt back, and the copy of a run that is gone is removed. A copy that cannot be read, restored, or removed keeps only its own run, when that run still exists, together with what the run reads; the other copies are still settled, unrelated batches are still removed in the same round, and the round reports each failure. When the run of such a copy exists but its `run.json` cannot be read, planning cannot tell what the run reads, so the round removes nothing. A FIFO or another special file in place of a copy is never opened or removed: the round fails at once, keeps its run, and frees the board lock, and the atomic writes and removals of `internal/fs` refuse such a target without blocking. The listing of `compaction-backups/` does not follow links and reports the type of each entry, so a symlink or another reparse point in place of a copy is listed as a link instead of failing the listing. Like any other entry that is not a regular file, a link is never opened or removed: only its run stays, when that run still exists, the link and its target are left alone, and the round reports the error. Since such an entry is never removed, its run counts as existing while `runs/<run>/` is there, even without its `run.json`. A link whose name is no copy is reported as well and keeps no run. The other copies, including the copy of a run that is gone, are settled as usual in the same round. A `compaction-backups/` that cannot be opened at all still fails the round. The next round removes them all. A missing `inputs/` or `staging/` is normal there.

**Import.** A v1 board has no compaction receipts, and `ImportRecord` returns no key for the `compactions/` directory, so the import path is unchanged.
