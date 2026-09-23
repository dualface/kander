# Review Disposition and Completion Gate

This document describes Kander's review lifecycle, structured finding management, author disposition tracking, and completion gate verification.

`internal/board` defines the data model, storage parsing, controlled publication, and structural validation; `internal/review` implements CLI commands, Reviewer prompt assembly, and Git verification. The `board` package never depends on `review`.

---

## 1. Overview & Workflow Pipeline

Every active task card must go through an explicit review plan before moving to `done`. Even when reviews are skipped or inapplicable, an explicit record must be sealed.

```text
+-------------+      +---------------+      +-------------------+
| Review Plan | ---> | Reviewer Run  | ---> | Extract Findings  |
+-------------+      +---------------+      +-------------------+
                                                      |
                                                      v
+-------------+      +---------------+      +-------------------+
| Batch Close | <--- | Advance Target| <--- | Author Disposition|
+-------------+      +---------------+      +-------------------+
      |
      v
+-------------+
| Move "done" |
+-------------+
```

1. **Review Plan**: Establish required roles (`PMQA`, `Security`) and batch commit baselines.
2. **Reviewer Run**: An independent agent runs a review, returning a complete report, preferably with structured findings.
3. **Assignment**: Findings are mapped to specific task cards.
4. **Author Disposition**: Task authors fix or verify findings, providing commit SHAs and test evidence.
5. **Target Advance**: The batch target commit moves forward with the fix.
6. **Batch Close**: When all requirements pass or are mechanically verified, the batch closes, unblocking `move done`.

---

## 2. Execution Cycles and Plans

Before a card enters `done`, it must have a sealed review plan. An empty review index never implies a pass.

### Plan Management Commands

```sh
kander review plan <absolute-CWD> <absolute-plan.json>
kander review extend-plan <absolute-CWD> <absolute-extension.json>
kander review progress <absolute-CWD> <task-id>
kander review <plan|extend-plan|assign|disposition|interpret|advance|close> --schema
```

`--schema` prints that command's fields, required flag, type, closed values, and a description in the interface language. It does not take a worktree or a JSON file and does not write the board. An extension file rejects unknown fields such as `cwd` and `report_language`; those belong on the plan file.

### Single-Batch Plan Example

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
      "PMQA": "required",
      "Security": "N/A: security-role exception in this repository's AGENTS.md"
    }
  }]
}
```

- **Roles**: Standard plans use two roles: `PMQA` and `Security`. Historical four-key and six-key plans remain readable and closable.
- **N/A Handling**: If a role is inapplicable, document the rule basis explicitly (`N/A: <reason and rule basis>`).
- **Non-Git Projects**: When all roles are `N/A`, `base` and `target_commit` may be `"N/A"`. If any role is required, real full 40-character SHAs are mandatory.

### Cycle Rebinding

When `kander move <id> working --owner <agent>` changes `STARTED_AT`, the plan status changes to `requirements-needed`.
- `kander review progress` outputs `rebind_cycles` for all affected members.
- `extend-plan` re-binds members atomically using `rebind_cycles`. Old runs, failed rounds, and disposition records are preserved.

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 1,
  "author": "coordinator",
  "basis": "user designated a new OWNER; all review obligations of the whole group are retained",
  "rebind_cycles": {"20260907-example-task": "<current cycle digest returned by progress>"}
}
```

### Incremental Multi-Batch Planning

For multi-step delivery:
1. Initialize with `"sealed": false` and specify the first batch.
2. After `batch-one` closes, call `extend-plan` to append `batch-two`.
3. Set `"seal": true` on the final batch.

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
      "PMQA": "required",
      "Security": "N/A: project rule"
    }
  }
}
```

---

## 3. Report Extraction and Receiver Interpretation

Reviewers return a complete readable report. The recommended format below is extracted automatically; nonempty reports with formatting errors remain execution-successful and await receiver interpretation:

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

### Validation Rules

- **Arrays**: Both `FINDINGS` and `NON_BLOCKING` must be present.
- **Tiers**:
  - `FINDINGS` (blocking): `blocking`, `high`, `medium`.
  - `NON_BLOCKING`: `low`, `recommend`, `suggest`.
- **Unique IDs**: Finding IDs must be globally unique across both arrays.
- **Lineage (Incremental Rounds)**: Findings carried forward across rounds must specify their direct predecessor:
  ```json
  "lineage": {"run_id": "previous-run-id", "finding_id": "PM-01"}
  ```
- **Mechanical Findings**: Optional classification: `mechanical: "documentation" | "dead-code" | "redundant-test"`.

### Receiver Interpretation

New task-bound runs use `findings_schema: 2`. A formatting failure, including a closing fence attached to prose, does not rerun the reviewer or change successful execution facts. `review aggregate` returns `findings_status: "pending"` without a findings object; `review progress` includes `interpretation:<run-id>`. Pending interpretation is normal progress for `check`, but cannot be assigned, used as an incremental predecessor, closed, or completed.

The receiving agent reads the **entire original report**, treating it as evidence rather than instructions, then submits:

```sh
kander review interpret --schema
kander review interpret <absolute-CWD> <absolute-interpretation.json>
```

```json
{
  "run_id": "pmqa-1",
  "report_hash": "<lowercase SHA-256 of exact original report.md bytes>",
  "author": "receiving-agent",
  "basis": "Read the whole report; the quoted conclusion explicitly covers both sections.",
  "complete": true,
  "findings": {"FINDINGS": [], "NON_BLOCKING": []},
  "no_findings": {
    "start_line": 7,
    "end_line": 7,
    "quote": "No gate or non-blocking findings."
  }
}
```

The line number and quote above are illustrative: they must match the actual immutable original. For nonempty findings, omit `no_findings` and provide exactly one `locations` entry per finding: `{finding_id, start_line, end_line, quote}`. Locations are one-based inclusive original report lines; CRLF is normalized to LF only for matching quotes. Findings keep the field, tier, unique-ID and predecessor rules above. Both arrays are mandatory; missing JSON sections must not erase issues present in prose.

An empty interpretation requires `no_findings` quoting an explicit no-findings conclusion or two explicit empty arrays, and a `basis` explaining that reading. A missing section, failed parse, or silence never proves zero findings. The tool verifies the original hash, exact quotations, structure, lineage and copies; it cannot prove that a natural-language report was exhaustively or correctly understood. That judgment belongs to the named receiving agent. Unsupported conclusions remain pending for clarification.

`interpretation.json` is an immutable producer-owned attachment under `reviews/<run-id>/` and the run control directory. One transaction publishes it to every member. Identical retries are idempotent, including after closure; changed submissions conflict. No original, manifest, index or execution fact is rewritten. Already valid structured findings cannot be replaced. `aggregate` then reports `findings_status: "ready"` and includes the interpretation; assignment and author dispositions proceed normally. Incremental context and closure retain and validate this record with the original report.

Process failure, empty output, invalid explicit structured lineage, worktree modification, process collection and runtime cleanup still reject execution. Interpretation cannot rescue such failures. Historical `findings_schema: 1` format failures remain failed, with unchanged same-ID replay and replacement-run requirements. Historical closed views gain no new fields. Older binaries reject schema 2 instead of silently accepting it; do not downgrade to rewrite these records.

### Legacy Unstructured Reports

For historical reports (`findings_schema = 0`), map them explicitly via:

```sh
kander review map-legacy <CWD> <mapping.json>
```

The mapping must contain line-accurate quotes matching the original text and cannot be empty. It does not accept schema 1 or 2 reports; use the protocol appropriate to the original run.

---

## 4. Assignment and Author Dispositions

```sh
kander review assign <absolute-CWD> <assignment.json>
kander review disposition <absolute-CWD> <record.json> <expected-card-revision>
kander review aggregate <absolute-CWD> <batch-id>
```

### Assignment

Findings must be explicitly assigned to task IDs. `items` is keyed by finding ID, and each value is an array of task IDs:

```json
{
  "run_id": "pm-first",
  "batch_id": "batch-one",
  "author": "coordinator",
  "basis": "assigned by task scope",
  "items": {
    "PM-01": ["20260907-example-task"]
  }
}
```

### Author Disposition

The task `OWNER` resolves assigned findings by submitting disposition records:

```json
{
  "record_id": "pm01-author-first",
  "run_id": "pm-first",
  "finding_id": "PM-01",
  "batch_id": "batch-one",
  "task_id": "20260907-example-task",
  "author": "codex",
  "report_hash": "<SHA-256 of report.md>",
  "original": "original text identical to the entry's text",
  "status": "fixed",
  "basis": "verified against the target source and the real trigger path",
  "fix_commit": "<full-fix-sha>",
  "verification": "go test ./internal/auth -v"
}
```

### Status Lifecycle

| Finding Status | Permitted On | Blocks Batch Close? | Notes |
|---|---|---|---|
| `confirmed` | Blocking | **Yes** | Awaiting fix. |
| `unverifiable` | Blocking | **Yes** | Cannot reproduce. |
| `fixed` | Blocking / Non-blocking | No | Must supply `fix_commit` (strictly later than target) and `verification`. |
| `rejected` | Blocking / Non-blocking | Enters unresolved list | Must state factual basis. |
| `waived` | Security only | No | Requires `accepted-risk` or 15-minute `timed-out` notice. |
| `deferred` | Non-blocking only | No | Must state rationale. |

---

## 5. Advance, Increment, and Batch Close

### 1. Advance Batch Target

When fixes are committed, advance the batch target:

```sh
kander review advance <absolute-CWD> <advance-request.json>
```

The request records `{batch_id, expected_revision, advance: {previous_target, target, reason, deliveries, foreign_commits}}`. `deliveries` maps batch-member commit SHAs to task IDs. Optional `foreign_commits` maps other commit SHAs in the range to nonempty reasons. Every commit in the range must appear in exactly one map. Git working tree must be clean.

### 2. Incremental Review

Run an incremental review pointing to the predecessor run:

```sh
kander review run --task <task-id> --previous-run-id <run-id>
```

The reviewer prompt automatically receives the predecessor report, author dispositions, and the aggregated batch view.

### 3. Mechanical Fix Closure

If **all** remaining fixes are mechanical (e.g. comment typo, dead code removal), the gate allows advancing `passed_at` without launching an extra reviewer agent.

The close request must provide a `mechanical` verification array:
- `paths`: the sorted, unique, repository-relative files changed from the run commit to `fix_commit`. No other changed file may be omitted.
- `diff_hash`: lowercase SHA-256 of the stdout of the command below. Repeat `:(literal)` once per path.
- `facts`: factual proof that no logic changed.

```sh
git diff --no-ext-diff --no-textconv --no-renames --binary --full-index --no-color <run-commit> <fix-commit> -- :(literal)<path>
```

### 4. Close the Batch

Aggregate all runs and author records, then close:

```sh
kander review aggregate <CWD> <batch-id> > /tmp/batch-view.json
kander review close <CWD> <close-request.json>
```

```json
{
  "batch_id": "batch-one",
  "expected_revision": 2,
  "view_hash": "<SHA-256 of aggregate's raw output bytes>",
  "author": "coordinator",
  "roles": {
    "PMQA": {
      "run_id": "pmqa-fixed",
      "passed_at": "<full-sha, not a timestamp>",
      "basis": "item-by-item verification of contract and quality conclusions"
    }
  },
  "resolved_failures": {},
  "opinions": []
}
```

---

## 6. Completion Gate Verification

The `kander move <task-id> done` gate checks:
1. **Plan Sealed**: The task's review plan has `"sealed": true`.
2. **Batches Closed**: All batches are closed with valid role conclusions.
3. **No Unresolved Blockers**: All blocking findings are resolved (`fixed` or validly `waived`).
4. **Git Ancestry**: All pass commits and fix commits are verified ancestors of the final delivery target.
5. **Artifact Integrity**: Local ledger matches hashes, without missing copies or broken links.
