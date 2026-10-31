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

The `cwd` of a plan and the CWD argument of every evidence command name one worktree, not one string. Each evidence command resolves its CWD argument once like the `kander review` worktree root (`EvalSymlinks`, `Abs`, `Clean`) and uses it for Git checks and stored Git evidence; `review plan` accepts a plan whose resolved `cwd` names the same directory and stores the resolved form. Plan, run, closure, gate, and `kander check` comparisons go through `board.SameReviewCWD`, which is lexical: it cleans both paths and, on Windows, treats `\` and `/` alike and ignores case. It never touches the filesystem, so historical plans and closures stored with another spelling stay valid without being rewritten, and POSIX comparison stays case-sensitive.

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

When `kander move <id> working --owner <agent> --decision <reference>` changes `STARTED_AT`, the plan status changes to `requirements-needed`. A reclaim from `review/` requires `--decision`.
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

A rebind after every batch of the member closed starts a cycle no batch reviews. The plan records in `cycle_batch_from` the position of the first batch that can review the new cycle: the first open batch of the member, or the end of the plan. With no batch from that position on, `kander move <id> done` refuses, so dropping that open batch owes the batch again: `kander review progress` lists `cycle-batch:<task-id>` under `pending` and the action `append-cycle-batch` with `task_id`. Append one batch with `extend-plan` and `batch`. A sealed plan accepts this batch only when it names such a member, and the batch stays subject to the ordinary chain rules: `previous_batch_id` is the last batch, and `base` equals its closed target. The batch can run a review, or it can use the explicit N/A path with every role `N/A: <reason and rule basis>`. A rebind while a batch of the member is open owes no new batch as long as that batch stays in the plan, because it closes in the new cycle.

A member in `done/` stays as it was: a later batch of another member, an open batch that does not name it, and the rebind of another member do not change its `check`, its coordinator mirror, or its completed wrap-up, which stays bound to the plan end of its day.

### Cycle Binding of Runs

`run.json` and the sidecar record `member_cycles`: the execution cycle of each member when the run was created. A new closure needs a conclusion run of the current cycle of every member that remains. A run of another cycle cannot close an open batch, and it stays in the batch, so a plan still cannot hide it. This holds for a batch a plan adopted and for a batch that was open at a rebind. `kander review close` refuses with the run, role, member, and batch. `kander review progress` lists `stale-run:<batch>/<role>/<member>` under `pending` and the action `rerun-current-cycle`, and `kander move <id> done` refuses with the same item.

There are two ways out. Run the role on a new commit with `--advance-file` and `--previous-run-id`: the chain may start in the old cycle, and its head reviewed the current one. Or run the role on the same commit and retire the old run through `superseded_runs`. Findings of the old run stay binding on both paths.

A closed batch stays valid: its runs need no current binding. A run with no binding comes from an older binary. It counts as evidence of an open or adopted batch only while the cycle is unchanged, which means the execution chain of the member, with `emptyCycle` left out, names one cycle. This is a breaking change: a run of an old cycle no longer satisfies the gate, and a card that was reclaimed mid-review needs one more review after an upgrade.

### Dropped Required Batches

Dropping a batch with a `required` role does not end that review. The plan records it in `drop_obligations`: the batch, the roles, the members that were in `working/` or `review/`, and the position from which a batch counts. A later drop keeps earlier records. Dropping an N/A batch records nothing.

- A record ends for a member when a later batch names that member, requires the role, and closes. N/A does not count, and a batch without the member does not count for it. An excluded member owes nothing.
- `seal` refuses while a role has no later batch that requires it and names the member. Such a batch may arrive in the same request as the seal. Done still waits for it to close.
- `kander review progress` lists `dropped-review:<member>/<role>` under `pending` and the action `append-required-batch`. `kander move <id> done` refuses with the same item.
- `drop_batches` takes an optional `decision`, the user decision reference, and then records nothing. White space counts as no decision.
- `release_obligation` with `{"kind": "dropped", "task_ids": [...]}` and a `decision` ends a record later. A sealed plan accepts it, and it stays sealed. A member with no record is refused.
- Both `decision` values stay in `plan-history`. As with `--decision`, Kander cannot verify that the user gave them. Neither ends a recovery obligation or the batch a new cycle owes.

### Recovery With No Plan

`kander coordinator recover` on a member with no plan writes `recovery-reviews/<task-id>.json` with an empty `plan_id`. The record takes its roles from the plan created later, and it keeps the roles a later drop takes out of that plan, also with a `decision`.

- With at least one role, every role needs a closed batch that names the member and requires the role, and its conclusion run must be a run of the current cycle. N/A does not count, and `release_obligation` is refused.
- With no role, the plan is all N/A, and `kander move <id> done` refuses until `release_obligation` with `{"kind": "recovery", "task_ids": [...]}` and a `decision` records the user decision. That decision stays in the record and in `plan-history`. A required batch appended instead also discharges the record, and a sealed plan accepts that batch.

A reset of a member that has a plan keeps its existing rules: the record names the plan roles, and only a closed batch from its position on discharges it. `release_obligation` does not apply to it.

### Member Exclusion

A `kander move` into `archived/` or `trash/` of a plan or task group member writes `exclusions/<task-id>.json` in the same transaction: exclusion ID, plan, task group, source and target state, result, cycle, reason, and decision reference. Only that move writes the record, and neither state has a way back, so an exclusion cannot be forged to escape a review. A move from `done/` into `archived/` writes none. A move from `done/` into `trash/` does, because the card no longer counts as delivered.

- Plan, batch, closure, assignment, interpretation, and legacy-map writes skip the copies of excluded members and of every archived member, and the validators stop comparing those copies. A copy that exists stays as it is.
- Only an excluded member is exempt from the gate. Its cycle needs no rebind, and completion covers the other members. A member archived after `done/` keeps its facts and has no exemption.
- A new run names the batch members that remain. A run prepared before the exclusion takes no copy on the excluded member (`skipped_members`) and settles for the others; a same-ID retry keeps the members of that run. A batch with no member left neither closes nor blocks: the chain continues over it, and wrap-up evidence ends at the last batch with a member.
- An assigned finding of an excluded member blocks aggregation, advance, and closure until it has a `withdrawn` record (see "Status Lifecycle"). A new assignment refuses an excluded member.
- `kander coordinator reconcile` accepts a terminal member only from what the move wrote. A member archived with `RESULT: completed` needs `FINISHED_AT`, the fact of `done/`. A member with a stored execution chain needs an exclusion whose state, result, and cycle match the card. A member with no stored chain keeps the result check alone.

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

### Unplanned Batch Resolution

A run in a batch outside the plan of a member fails `kander check`, `kander review progress`, and `kander move <id> done` with `unplanned execution batch: <batch-id>`. Current binaries refuse such a run before the batch exists, but a run from an older binary stays on the board. The batch cannot join the chain, because a successor's base must equal the closed target of its predecessor. `extend-plan` with `resolve_batch` accepts it instead.

```json
{
  "plan_id": "implementation-cycle",
  "expected_revision": 3,
  "author": "coordinator",
  "basis": "batch-three reviewed the range of the old batch",
  "resolve_batch": {
    "batch_id": "old-batch",
    "covering_batch_id": "batch-three",
    "findings": [{
      "run_id": "old-run",
      "finding_id": "PMQA-001",
      "status": "fixed",
      "basis": "the next commit updates the document",
      "fix_commit": "<full SHA>",
      "verification": "batch-three reviewed the fix"
    }]
  }
}
```

The request is one operation, mutually exclusive with every other `extend-plan` operation. The CWD must be the plan's worktree. A sealed plan accepts it and stays sealed. The plan's `batches` do not change, and the unplanned run, its batch record, `reviews/<run_id>/`, and its `REVIEWS` line stay as they are.

The request is accepted only when all of these hold:

- The batch has a run and is outside the plan, and the plan holds at least one of its members.
- Every run is finalized and fully published, and no run waits for `review interpret`. A failed run with no report has no finding to list. A failed run that kept a readable report counts like any other run. A failed run with an unreadable report is refused, because nothing can interpret it and its findings stay unknown.
- `covering_batch_id` names one closed batch of this plan. Its base is the base of the unplanned batch or an ancestor, and its target is the target of the unplanned batch or a descendant.
- Every `required` role of the unplanned batch is `required` in the covering batch. Every plan member of the unplanned batch that was not excluded is a member of the covering batch.
- `findings` lists every finding of every run once, and nothing else. A must-fix finding is `fixed`, or `rejected` with `decision`, the user decision reference. Any other finding is `fixed`, `deferred`, or `rejected`. Every entry has a `basis`, and `fixed` also needs `fix_commit` and `verification`.
- A `fix_commit` is a strict descendant of the commit of its run, and it is the commit of the role's conclusion run in the covering batch or an ancestor of that commit.
- The plan has not resolved this batch before.

The limit: one covering batch only. A range that spans two closed batches has no covering batch, so the request is refused.

`internal/review` runs the Git checks and passes the edges to `internal/board`, which compares them with the edges it derives and runs no Git. The plan stores the record in `resolved_batches`, with the batch revision, every run ID and report hash, and the verified edges, and `plan-history` keeps the same record. `kander review plan` ignores that field. `kander review progress` prints the record, so each finding's result is readable there.

The gate reruns these conditions on the current records. A new run in the batch, a changed batch revision, or a changed report fails `kander check` and `kander move <id> done` again. Another unplanned batch fails them on its own.

Members in `done/` take the new plan copy like any other plan write, and archived members take none. An excluded member needs no place in the covering batch. A changed execution cycle does not block the resolution, and it must not: `review progress` can name `rebind_cycles` only after the batch is resolved. Rebind afterwards. A member of the batch outside this plan answers in its own plan. A board with a resolution needs a binary that knows `resolved_batches`; v1.0.0 rejects the plan as an unknown field. Archive cleanup removes the plan and `plan-history` with the rest of the evidence.

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
| `withdrawn` | Excluded member only | No | Written by the orchestrator. Requires `withdrawal.exclusion_id` and `withdrawal.decision`. Not a fix. |

A `withdrawn` record goes through `kander review disposition` with the revision of the excluded card. The orchestrator takes `exclusion_id` from `kander review progress <task-id>`, whose `pending` also lists each `withdrawal:<run>/<finding>/<task>` still owed. The original lives in the ledger, because an archived card takes no write. The aggregate view repeats these records in `runs[].withdrawn`, apart from the author dispositions.

---

## 5. Advance, Increment, and Batch Close

### 1. Advance Batch Target

When fixes are committed, advance the batch target:

```sh
kander review advance <absolute-CWD> <advance-request.json>
```

The request records `{batch_id, expected_revision, advance: {previous_target, target, reason, deliveries, foreign_commits}}`. `deliveries` maps batch-member commit SHAs to task IDs. Optional `foreign_commits` maps other commit SHAs in the range to nonempty reasons. Every commit in the range must appear in exactly one map. Git working tree must be clean.

A batch with a planned successor refuses both entrances, because the base of the successor is immutable and must equal the closed target of this batch. The error names the successors. Remove them with `extend-plan` and `drop_batches`, which accepts trailing batches with no run, advance, or closure, reopens a sealed plan, and leaves the batch records, so an ID cannot return with another base. After the close, append the next batch under a new ID, then seal again. A dropped `required` batch stays owed, so the batch you append must require the role and name the member, and the seal fails without it (see "Dropped Required Batches").

The command, and an incremental run with `--advance-file`, refuses while any assigned finding has no author disposition. Mechanical items count. The error lists each `task/run/finding`, and neither the batch nor the plan changes. An assigned finding without a record is a fix round that has not ended, and a target move would overtake it.

A fix dispatch normally binds a run at its base. A batch that an older binary advanced before a fix round ended stays recoverable: the dispatch base equals the current target, and a referenced run may sit on an earlier target of the batch's advance chain. The chain comes from `batch.advances` alone, from the initial target to the current target, with no Git access. A foreign batch, a run outside the chain, a superseded round, and a task outside the run stay rejected. Reconciling the completed fix accepts the same chain and does not depend on the target at reconciliation time.

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

### 5. Parallel Duplicate Runs and Retired Root Runs

Every successful run of a required role must sit on the conclusion chain (the conclusion run and its `previous_run_id` ancestors), with one exception: a run listed in the optional `superseded_runs` map, as a parallel duplicate or as a retired root run.

```json
"superseded_runs": {
  "pmqa-twin": {"replacement": "pmqa-fixed", "basis": "started in parallel with pmqa-first on the same commit"}
}
```

- The superseded run has `execution_status: ok` and the conclusion's role, and is not on the chain. A chain run has the same commit, reviewer, and `previous_run_id`; or the superseded run's predecessor is itself superseded and the run has the conclusion run's commit and reviewer. A full re-run after a fix or a reviewer switch qualifies under neither rule and stays rejected without a decision.
- With `decision`, a user decision reference, an entry also retires a root run that cannot join the chain, and a run whose predecessor has such an entry. Each such run needs its own entry. A run that branches off the conclusion chain does not qualify. Kander records the reference and does not verify it.
- `replacement` is a successful run on the chain; `basis` is nonempty. An entry that matches no unlinked successful run (another batch, a failed run, a chain run) is rejected.
- Each entry adds the edge `superseded.commit → replacement.commit` to the Git claims. The superseded run's findings keep the ordinary rules: they need assignment and author dispositions, and a fixed must-fix must be an ancestor of the conclusion run's commit. A fixed must-fix of the duplicate therefore needs a later incremental run as the conclusion.
- The entry stays in the closure. The originals, findings, and failures of a retired run stay, and they keep binding the close.
- An unlisted duplicate fails close with a localized next step naming the run, the conclusion, and the entry to add. `review progress` reports every role with two or more successful chain heads in an unclosed batch (candidates; only qualifying heads can be superseded) as `next_actions: [{"action": "supersede-duplicate-runs", "batch_id", "role", "run_ids"}]`.
- The field is `omitempty`: closures without it keep their stored bytes, so existing closed batches verify unchanged.

---

## 6. Completion Gate Verification

The `kander move <task-id> done` gate checks:
1. **Plan Sealed**: The task's review plan has `"sealed": true`.
2. **Batches Closed**: All batches are closed with valid role conclusions, except a batch with no member left. A new closure needs conclusions of the cycle current when it closes, and a closed batch stays valid after a later cycle change. A rebound member whose every earlier batch closed has a later batch. Drop obligations and recovery obligations are discharged.
3. **No Unresolved Blockers**: All blocking findings are resolved (`fixed` or validly `waived`).
4. **Git Ancestry**: All pass commits and fix commits are verified ancestors of the final delivery target.
5. **Artifact Integrity**: Local ledger matches hashes, without missing copies or broken links. Copies of excluded and archived members are not compared.
