# Review Disposition and Completion Gate

This protocol builds on the run/batch of the [Review evidence archive](review-evidence.md) and on [Card transactions](card-transactions.md). `internal/board` defines the shared model, parsing, controlled publication, and purely structural validation; `internal/review` provides the commands, Reviewer prompts, and Git verification. board does not depend on review.

## Execution Cycles and Plans

An active card must have an explicit review plan before entering done. Even when there is no REVIEWS index, no role has run, or the review rules are disabled, an empty index must not be taken to imply a pass. When review truly does not apply, each of the four roles is written as `N/A: <reason and rule basis>`, followed by an explicit batch close. The tool records the caller's applicability judgment; it does not substitute for user authorization or project-rule judgment.

```text
kander review plan <absolute-CWD> <absolute-plan.json>
kander review extend-plan <absolute-CWD> <absolute-extension.json>
kander review progress <absolute-CWD> <task-id>
```

Single-batch plan example; every SHA must be replaced with a real full commit:

```json
{
  "schema": 1,
  "sealed": true,
  "plan_id": "implementation-cycle",
  "author": "coordinator",
  "basis": "confirmed task and this repository's AGENTS.md",
  "cwd": "/absolute/group-worktree",
  "report_language": "zh-CN",
  "task_ids": ["20260907-example-task"],
  "batches": [{
    "batch_id": "batch-one",
    "task_ids": ["20260907-example-task"],
    "base": "<full-base-sha>",
    "target_commit": "<full-target-sha>",
    "requirements": {
      "PM": "required",
      "QA": "required",
      "CSA": "N/A: security-role exception in this repository's AGENTS.md",
      "Hacker": "N/A: security-role exception in this repository's AGENTS.md"
    }
  }]
}
```

In a non-Git project where all four roles are explicitly N/A, both base and target_commit may be written as `N/A`. In that case close stores a `git.not_applicable` basis, does not invoke Git, and does not claim that commits or ancestry have been verified. As soon as any role is required, real full SHAs must be used.

The tool generates revision, recorded_at, and each member's execution-cycle binding; the latter binds the task ID and STARTED_AT. The task plan pointer and tracked-cycles live in the stable control directory; the card stores the complete `reviews/plan.json`. A plan cannot be overwritten by a new plan, nor can a changed plan ID be used to discard a failed round. tracked-cycles must be consistent with the cycles stored in the plan; missing or inconsistent entries are structural errors.

After `move <id> working --owner <agent>` changes STARTED_AT, the whole plan returns `requirements-needed`, and progress also outputs `rebind_cycles` for all changed members. Calling `extend-plan` while explicitly carrying the current plan_id, expected_revision, author, basis, and this complete rebind_cycles atomically rebinds the new cycles of all members. This operation can only restore the original plan; it cannot simultaneously change batches or seal, nor replace role requirements; old runs, failed rounds, assignments, author records, batch-close evidence, and the state of other members are all preserved. The old complete plan and the request are stored in plan-history by revision; the current reviews/plan.json copy is updated in sync. An already sealed plan can also be rebound; a same-cycle request, a missing member, or a stale revision is refused. An existing failure still requires an explicit successful replacement, and an existing unresolved finding still requires disposition; rebinding cannot be used to skip them.

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "user designated a new OWNER; all review obligations of the whole group are retained",
  "rebind_cycles": {"20260907-example-task": "<current cycle digest returned by progress>"}
}
```

For batch-by-batch scheduling, first use `sealed: false`, listing the fixed membership of the whole cycle and the first batch. After the previous batch closes, append the next batch via extend-plan; existing batches or membership cannot be modified. Example:

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "previous batch is closed; accepting the next batch's delivery",
  "seal": true,
  "batch": {
    "batch_id": "batch-two",
    "previous_batch_id": "batch-one",
    "task_ids": ["20260907-example-task"],
    "base": "<previous-closed-target-sha>",
    "target_commit": "<next-target-sha>",
    "requirements": {
      "PM": "required", "QA": "required",
      "CSA": "N/A: project rule", "Hacker": "N/A: project rule"
    }
  }
}
```

The batch may also be omitted to seal only. seal requires every member to already belong to at least one batch; done is impossible while the plan is unsealed, a member is unbatched, or any batch is unclosed. Extension records are retained by revision; conclusions of old batches are not rewritten by plan appends. A static multi-batch plan is also accepted, but each later batch's base must equal the previous batch's actual closed target; if the previous batch may still receive fixes, use the append protocol instead of pre-filling a guessed final base.

A legacy done/archived card without a plan reads as `legacy-untracked`; no PASS is fabricated. Having tracked-cycles but a lost plan is an error. Legacy active cards need a plan to be established; launching, re-review, and wrap-up still follow the existing execution authorization. This plan is not the execution epoch of subsequent durable dispatch.

## Structured Findings and Manual Mapping

Every new Reviewer prompt requires one standalone fenced block:

````text
```kander-findings
{
  "FINDINGS": [{
    "id": "PM-01",
    "tier": "medium",
    "text": "full original text of the finding",
    "evidence": "file.go:42; concrete trigger and impact"
  }],
  "NON_BLOCKING": []
}
```
````

Both arrays must be present. FINDINGS accepts only blocking/high/medium; NON_BLOCKING accepts only low/recommend/suggest. IDs are unique across the two arrays; an ID casually mentioned in the report body does not constitute an entry. A missing ID, duplicate ID, duplicate JSON key, unknown field, missing array, duplicate fence, or otherwise invalid structure cannot participate in a valid batch close. A new run's sidecar has findings_schema 1; finalize validates the parsed structure and the complete lineage relations within the same transaction, and an invalid report is archived with execution_status=failed, a non-zero exit, and the concrete reason, with the original report/raw both preserved. execution_status=ok still does not mean semantic PASS.

A mechanical finding may carry `mechanical: documentation|dead-code|redundant-test`. A finding carried forward in an incremental round uses `lineage: {"run_id":"previous round's ID","finding_id":"previous round's entry ID"}` to point at the immediate predecessor; it cannot reference other batches or IDs merely mentioned in the body. New findings carry no lineage. The tool shares the identity-relation validation across finalize, assignment, and aggregation: lineage without a predecessor, multiple entries pointing at the same predecessor entry, a wrong predecessor/wrong entry, or reusing a predecessor ID while omitting lineage are all refused. A new report is marked failed before the immutable archive and cannot count as PASS. The tool does not claim to understand natural language in order to automatically recognize restatements.

A legacy findings_schema=0 unstructured report must be mapped explicitly via `review map-legacy <CWD> <JSON-file>`, preserving the original artifact without rewriting it. The mapping contains run_id, report_hash, author, basis, complete=true, the two findings arrays, and for each item finding_id/start_line/end_line/quote; line numbers start at 1 and the quote must match that source range verbatim. A manual mapping is also lineage-validated before immutable publication; an invalid request is refused outright, no mapping has been produced yet, and it can be corrected and resubmitted. An empty mapping must not auto-pass; a new-format report cannot use legacy mapping to patch missing structure — retry with a new run ID in the same batch. On a first-round failure, redo the full review; on an incremental failure, keep referencing the same last valid predecessor --previous-run-id (and its reviewed-commit), preserving the previously valid chain, neither referencing the failed run nor building a separate disconnected full chain. A target already advanced by a failed intent is not advanced again. In close's resolved_failures, explicitly point the original failed ID at the successful replacement ID on the valid chain. A retry with the same ID only recovers the original failure evidence and does not rerun the Reviewer.

## Assignment and Original Author Records

```text
kander review assign <absolute-CWD> <absolute-assignment.json>
kander review disposition <absolute-CWD> <absolute-record.json> <expected-card-revision>
kander review aggregate <absolute-CWD> <batch-id>
```

The assignment's `run_id/batch_id/author/basis/items` are required. Each key of items is an actual finding ID from the report, and each value is an explicit array of task IDs. A cross-card finding lists all assignments; the tool freezes those cards' OWNER at that moment. When a complete report has no findings, items explicitly writes `{}` and no author record is required.

```json
{
  "run_id": "pm-first", "batch_id": "batch-one",
  "author": "coordinator", "basis": "assigned by each task's modification scope",
  "items": {"PM-01": ["20260907-example-task"]}
}
```

Author record example:

```json
{
  "record_id": "pm01-author-first",
  "run_id": "pm-first", "finding_id": "PM-01", "batch_id": "batch-one",
  "task_id": "20260907-example-task", "author": "codex",
  "report_hash": "<SHA-256 of report.md>",
  "original": "original text identical to the entry's text",
  "status": "fixed", "basis": "verified against the target source and the real trigger path",
  "fix_commit": "<full-fix-sha>",
  "verification": "actual verification commands and results"
}
```

Records are submitted by the working card's current OWNER and only for the author's own explicit assignments. The assignment preserves the initial OWNER; after a legitimate takeover, the new OWNER may append their own conclusions but cannot overwrite old author records, and the old OWNER can no longer submit. The command verifies the current OWNER and the expected revision, and generates submitted_revision and the record time; a legacy record without submitted_revision is still validated against the original assignment's author binding. A later update creates a new record_id and points previous_record_id at the previous record for the same run/finding/task; old JSON is not overwritten. Controlled update cannot change these attachments. The author claim here is an identity record of a protocol-following local Agent, not a digital signature, and cannot prevent an arbitrary same-user process from submitting under a false name.

must-fix statuses are confirmed/fixed/rejected/unverifiable/waived. confirmed and unverifiable block batch close; rejected must state a factual basis and enters the unresolved list. fixed must have a fix SHA and a verification record; that commit should be strictly later than the finding's target commit. NON_BLOCKING accepts only fixed/deferred/rejected and must also record a basis.

waived is valid only for CSA/Hacker must-fix items. A waiver contains policy=accepted-risk with an explicit decision, or policy=timed-out with a fully documented notification basis decision, sent_at, and timeout_at; the two instants must be at least 15 minutes apart and timeout_at must not be in the future. It does not equal PASS. PM/QA, unverified items, and ordinary acceptance confirmations cannot use this exception. The actual notification, the user's decision, and the applicable rules must still be carried out and cited truthfully by the Agent; the presence of the fields grants no authorization.

Each author record is published only to its own `reviews/<run_id>/dispositions/<record_id>.json`; the tool-controlled ledger binds the original text and preserves the full chain. The orchestrator's verification opinions are separately marked with author/basis in the close request's opinions and may cite finding references, but cannot rewrite or submit a disposition on the author's behalf.

aggregate reads the whole batch's deduplicated runs, explicit assignments, and each author record, validates completeness, outputs JSON, and publishes to every member's `reviews/batches/<batch_id>/disposition.json` through the same recoverable transaction. A member with no findings need not be notified merely to transcribe the conclusion. When a multi-card aggregate publication is incomplete, the transaction recovery gate and copy verification block completion.

## Advance, Increment, and Close

Even a mechanical fix must first advance the batch target, without launching an extra Reviewer:

```text
kander review advance <absolute-CWD> <absolute-advance-request.json>
```

The request is `{batch_id, expected_revision, advance}`, where advance follows the archive protocol's previous_target/target/reason/deliveries. Both advance and extend-plan require the calling CWD to match the plan CWD exactly. A standalone advance against a legacy batch with no plan explicitly requires establishing the plan first, then advancing. review verifies a clean HEAD and the complete Git commit assignment range, and the board CASes inside the lock; any in-progress run, not-fully-published run, or already-closed batch state refuses the advance. The original `--advance-file` may still be combined with launching an incremental review.

An incremental review with `--task --previous-run-id` automatically loads the predecessor's original report, the non-overwritable author records, and the tool-generated batch view. Manual review-context/reviewed-commit may be omitted; an explicit reviewed-commit must match. A manual review-context is preserved verbatim as a separate supplement and does not replace the original artifacts. The tool uses explicit byte lengths to separate the automatic original artifacts from the supplementary text; before the intent commits, only the automatic sources are re-validated, while the overall input is still frozen by its original bytes. A retry passing different supplementary text is explicitly refused. The Reviewer prompt shows the automatic original artifacts and the manual supplement under readable headings, omitting the section when the supplement is empty; the frozen attachment keeps the original byte-length envelope and the internal counts are not shown in the prompt. A previous round that did not reach semantic PASS can be re-reviewed; a missing copy, missing author record, or wrong batch/member/commit/language is refused before launch. The context snapshot is validated again when the intent commits; a same-run retry uses the frozen input, avoiding mixing later state into the old invocation.

```text
kander review aggregate <CWD> <batch-id> > /tmp/batch-view.json
kander review close <CWD> <absolute-close-request.json>
```

close request example:

```json
{
  "batch_id": "batch-one",
  "expected_revision": 2,
  "view_hash": "<SHA-256 of aggregate's raw output bytes>",
  "author": "coordinator",
  "roles": {
    "PM": {"run_id":"pm-fixed", "passed_at":"<full-sha>", "basis":"item-by-item verification and cross-role impact check"},
    "QA": {"run_id":"qa-first", "passed_at":"<full-sha>", "basis":"verified structural and functional conclusions"}
  },
  "resolved_failures": {},
  "opinions": [{"author":"coordinator", "basis":"independent delivery verification, not a substitute for author dispositions"}]
}
```

Every required role must select a valid, complete, successful run and cover that role's entire explicit predecessor chain. An old role pass cannot be selected to skip a later round, and one role cannot substitute for another. Failed attempts explicitly point, via resolved_failures, at the same role's successful replacement run, with the commit relation verified; a batch with only failures cannot close.

A non-mechanical must-fix fix requires a subsequent review run covering the fix commit; with only mechanical must-fix items, that role's passed_at may be moved forward to the verified commit, keeping the mechanical classification, the actual fix, and the verification evidence. The mechanical disposition label by itself constitutes no waiver. The close request must also contain a separate `mechanical` array, each item containing record_id, finding (run_id/finding_id), task_id, author (same as the close author), category (matching the author disposition), reported_category (matching the Reviewer's original entry, an explicit empty string when the label is absent), report_hash, fix_commit, basis, facts, paths, diff_hash. facts must record the actual re-verification, such as sentence-by-sentence comparison, reference searches, or retained test coverage. paths are sorted, deduplicated repository-relative file paths; the tool verifies each path actually changed and verifies the listed range's diff digest from the originally reviewed commit to the fix SHA, saving the same binding in git.mechanical. The primary Agent may, by definition, additionally classify mechanical items the Reviewer missed labeling; labels or hashes must not be treated as semantic proof.

diff_hash is computed as the SHA-256 of the raw UTF-8 output bytes of the actual Git diff, with arguments `git diff --no-ext-diff --no-textconv --no-renames --binary --full-index --no-color <run-commit> <fix-commit> -- ':(literal)<path>' ...`. The supplementary facts must explain why these paths fully cover the mechanical fix of this finding; the tool performs no natural-language semantic lint and does not infer from path suffixes that code has no logic change. Without independent judgment, actual verification, or matching Git evidence, a subsequent review should be run.

Without mechanical items, passed_at must not be raised arbitrarily. All pass commits and fix commits must lie on the ancestor chain of the final target; a fix cannot point at the original finding commit or an unrelated branch.

close verifies, at the review layer, the current clean HEAD and all required Git objects/ancestry, then hands the CWD, HEAD, time, and the exact relation set, together with the current batch revision/view_hash, to the board. The board then aggregates the real original artifacts, CASes, and publishes closed.json and disposition.json. closed binds the final target; afterwards no run can be added, no author record modified, and no target advanced. A later batch must reference the previous batch's closed final target, not some old role PASS, and never infer by timestamp.

`check` and `move done` share the structural validation: a legitimately pending disposition shows pending/requirements-needed; existing but damaged original artifacts, structure, assignments, and copies are errors. `review progress` returns machine state. done additionally requires the plan sealed, all batches closed, and all copies and Git evidence bindings consistent. Purely structural validation does not run Git again and does not mean the code is integrated; final integration authorization, actual delivery, rebase rules, and Git verification continue under the existing workflow.

All evidence and control records are stored only in the local kanban board and do not enter Git. Native Windows behavior must be verified on Windows; cross-compilation does not equal native testing.
