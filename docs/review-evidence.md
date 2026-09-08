# Review Evidence Archive

A standalone review without `--task` keeps its original behavior and does not locate a kanban board. With that option, the main kanban board is located from the target CWD, and an explicit `KANBAN_DIR` override still takes precedence; the process CWD is not modified.

```text
kander review [agent] [--task <id>]...
  [--run-id <id>] --batch-id <id>
  [--previous-run-id <id>]
  [--requirements-file <absolute-json-path>]
  [--advance-file <absolute-json-path>]
  <CWD> <base> <commit> <role> <task-goal|absolute-spec-path>
  [review-context] [reviewed-commit]
```

All options come before the CWD. Repeated task IDs are normalized, sorted, and deduplicated; no other option may be repeated. run/batch IDs are 1–64 lowercase ASCII letters, digits, or hyphens, with the first character restricted to a letter or digit. The run ID may be omitted; it is then generated randomly and printed to stderr. The batch ID is supplied by the caller. Timestamps carry no identity and no causal ordering.

Both creating the intent and publishing to a not-yet-published card require a directory card in working/review; multiple cards must belong to the same non-empty task group and share the same language. Legacy file cards must be migrated per the init maintenance protocol. A long review does not hold the card lock; publication relocates cards by ID.

## Batches and Predecessors

The requirements file for a new batch must list all roles, each valued either required or N/A with a reason:

```json
{
  "PM": "required",
  "QA": "required",
  "CSA": "N/A: repository rule",
  "Hacker": "N/A: repository rule"
}
```

The caller resolves role requirements according to the user, project rules, and configuration; this file does not mean the tool can prove user authorization. Membership, base, requirements, the task-context hash, and language are fixed. PM/QA against the same target use the same batch with different runs. Later invocations may omit requirements; when provided, it must match the existing requirements.

Fixes do not open a new batch. Advancing the target requires an advance file:

```json
{
  "previous_target": "<old-full-sha>",
  "target": "<new-full-sha>",
  "reason": "fix delivery for this batch's tasks",
  "deliveries": {
    "<every-new-full-sha>": "20260907-example-task"
  }
}
```

review verifies that old is an ancestor of new and that every commit in old..new appears in the mapping and is assigned to a member of this batch; the board CASes the current target inside the group control lock, saving the old/new targets, the basis, and the version. Assignment is a fact supplied by the caller; there is no promise of deriving business assignment from arbitrary code, and deliveries from outside the group must not be misrepresented as fixes for this batch. Advancing is refused while any run of this batch is executing or not fully published.

An incremental round automatically reads the original report and author dispositions via previous-run-id; the caller's review-context is preserved verbatim as a separate supplement. reviewed-commit may be omitted; when passed explicitly it must match. The predecessor must share the same batch/base/role/reviewer and be fully published. For disposition, plans, and batch close, see [Review completion gate](review-disposition.md); this protocol does not auto-pass based on the word PASS appearing in a report.

## Original Artifacts and Schema

Each card stores:

```text
reviews/<run_id>/
  task-context.md
  review-context.md
  prompt.txt          # present when preparation succeeded
  evidence.txt        # present when preparation succeeded
  output.raw
  stdout.log
  error.log
  report.md           # present when the output decoded validly
  sidecar.json
  manifest.json
```

raw/log files are stored as-is, including invalid UTF-8. report.md stores the valid result text without rewriting line endings or content; a JSON Reviewer's original JSON is stored separately as output.raw. When there is no valid report, no report.md is fabricated and the index points to output.raw. Failure evidence may contain empty files; the sidecar states explicitly that the run did not start or failed, and an empty file does not indicate success.

sidecar schema 1 contains:

- run_id, batch_id, previous_run_id, task_ids, task_group, role.
- reviewer/model/effort, cwd/base/commit/reviewed_commit, the applicable advance.
- report_language, SHA-256 of the inputs and all original artifacts, kander_version.
- phase, launch_status, execution_status, semantic_status, exit_code, failure_reason.
- created_at, finished_at, duration_ms.

launch_status is not_started, unknown, or started. Before launching, launching/unknown is persisted first; on successful launch, running/started is written; on launch failure, not_started is recorded as final. execution_status is incomplete during preparation and finally ok, failed, not_started, or interrupted. semantic_status is always unassessed; ok only means the tool's execution verification succeeded and does not equal a semantic PASS.

manifest schema 1 stores the complete input identity, the sidecar hash, and the hashes of the original artifacts. In the body, each line of `## REVIEWS` is `- {JSON}`, with fields run_id, batch_id, role, execution_status, base, commit, previous_run_id, report. That section and the reviews attachments are managed by a dedicated publisher and cannot be modified via update. The manifest and index are not derived from the report body.

A card's LANGUAGE is resolved only when the intent is created; only when missing does it fall back to the configuration at that time. The frozen language does not drift with later configuration changes; a card with a LANGUAGE that disagrees with the frozen value reports a conflict.

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
```

This is a tool-reserved namespace, not a kanban task group, and no group card is created. run.json stores the per-card publication receipts; each card independently holds the complete original artifacts and a non-overwritable manifest. Hashes are for integrity detection and do not defend against arbitrary same-user tampering.

The per-run-ID OS execution lock lives in the stable locks directory, independent of the card lock. While held, it only briefly enters the board, group, and task transactions of card transactions; no code acquires the execution lock in the reverse direction, so no cycle forms. Different roles may run in parallel; a concurrent retry of the same run waits for the previous holder to release and then reads its result. Ordinary updates and moves between working and review may run during a long review. done is now checked by the review plan and disposition gate; an incomplete archive cannot legally enter done. If an external legacy program moves the card into done or another terminal state prematurely, the original artifacts remain in the control directory, publication fails, and the old path is not recreated; the default check skips deferred-check states, so `kander check <task-id>` or `kander check --all` must be used to locate incomplete publications. A done card cannot be moved back for reuse, nor patched around the controlled entry point; follow-up handling requires separate confirmation, and retrying with the same run must not promise to automatically repair the terminal state.

Inputs and the intent are committed to disk transactionally first; output is written to controlled staging. Only after process reaping, worktree checks, and runtime cleanup have all reached a conclusion are originals/sidecar frozen; then the original artifacts, manifest, index, and receipt are published atomically per card. Cross-card publication is not one big transaction: on partial failure, successful cards are kept, each card is reported individually, and the exit code is non-zero. A retry verifies successful cards without rewriting them and only fills in the missing ones.

The same run ID with different inputs is a hash conflict. A retry with the same inputs does not launch the Reviewer again; it can replay the original report even when the CLI has been removed or the worktree has new modifications. The first run still requires HEAD/clean/ancestry checks. A recovery call still requires the original Git objects, input bytes, and card bindings to be verifiable. If the original spec path moved with the card or the body later changed, the absolute path of the archived task-context.md may be passed instead; identity comparison is based on original-artifact bytes and does not require reusing a stale path. On a zero-card publication, the original inputs remain in the control directory inputs.

After the gate is killed, the OS execution lock is released; an identical invocation marks the not-yet-finalized intent as interrupted, keeps the staged output, and does not fabricate process reaping, cleanup, or a final report. Recovery does not confirm whether a leftover Reviewer is still running; it does not kill processes on its own based on an old PID, nor delete unknown runtimes. Abnormal runtime leftovers should be diagnosed and handled by the operator.

When an S multi-file transaction is interrupted, readers first report pending recovery. Pause writes, satisfy the maintenance conditions, run init, then retry with the same run ID. Do not manually patch the index, delete successful cards, recreate old paths, or switch IDs to bypass an incomplete publication.

All writes go through internal/fs, keeping POSIX private permissions and Windows creation-time protections (DACL/reparse denial). Binary original artifacts are recovered losslessly via the log's base64 byte fields; ordinary text keeps a compatible log format.

## Consumption Interfaces and Checks

- `board.PrepareReviewRun` / `UpdateReviewRun` / `FinalizeReviewRun`: intent and execution facts.
- `board.PublishReviewRun`: publishes per card and returns failures without erasing successful receipts.
- `board.ParseReviewIndexes` and ReviewInput/ReviewRun/ReviewBatch/ReviewManifest: shared types; board does not depend on review.
- `board.ReadReviewRun` / `ReadReviewOriginal`: read committed facts and verify original artifacts.
- `board.ReviewPublicationComplete`: verifies cross-card original artifacts, manifests, indexes, and predecessors; it does not judge semantic PASS.
- `kander check`: checks both intents and indexes; a successful zero-card publication does not disappear either. It reports incomplete publications, duplicate/missing/conflicting entries, and hash, language, membership, and predecessor errors, while keeping its original state scope.

Evidence lives in the local kanban board; it does not enter Git and is not deleted when temporary reports are cleaned up.
