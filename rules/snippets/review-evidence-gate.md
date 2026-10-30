# Review Evidence Completion Gate

Source: moved from `KANDER-KANBAN-RULES.md` "Review Evidence Completion Gate".

## Review Evidence Completion Gate

- An execution cycle is one claim of a card, identified by its task ID, `STARTED_AT`, and the producer-owned unique claim identity in `LIFECYCLE_DECISION`. Manual `move working --owner` and `start` each create a fresh identity, including a retry after start rollback; `resume --agent` preserves the identity and timestamp; cards without a claim identity keep the legacy task-ID plus `STARTED_AT` digest.
- New batch requirements contain exactly the two keys `PMQA` and `Security`; creating a four-key or six-key batch is rejected, while reading, same-ID retries, and close still accept exact two-, four-, and six-key objects. Existing originals, role order, hashes, runs, and closure evidence keep their identities; schema_version stays 1.
- Active execution cycles require an explicit review plan before `move done`, even when REVIEWS is empty or no reviewer ran, recording each role as required or N/A with an actual reason and rule basis. When review is disabled or nothing triggered it, the minimal sequence is: `review plan` with one sealed batch from the review base to the final delivery commit naming every role `N/A: <reason and rule basis>`, then `review aggregate` for that batch, then `review close` binding its view hash, then `move done`. This sequence loads no disabled module.

  For that explicit N/A path, replace every `<...>` value below and write the plan to a private absolute path. `base` and `target_commit` are real full SHAs for a Git delivery; both may be `N/A` only for a non-Git workflow. The `cwd` value must name the same directory as the absolute CWD passed to every evidence command; spellings that differ only as `snippets/orchestrate-review.md` "CWD equivalence" allows (on Windows `C:\...` versus `C:/...`, drive-letter case, a trailing separator) are the same directory.

  ```json
  {
    "schema": 1,
    "sealed": true,
    "plan_id": "<unique-plan-id>",
    "author": "<current-owner>",
    "basis": "review is disabled or did not trigger: <reason and rule basis>",
    "cwd": "<absolute-CWD>",
    "report_language": "<card-LANGUAGE>",
    "task_ids": ["<task-id>"],
    "batches": [{
      "batch_id": "<unique-batch-id>",
      "task_ids": ["<task-id>"],
      "base": "<full-base-SHA-or-N/A>",
      "target_commit": "<full-target-SHA-or-N/A>",
      "requirements": {
        "PMQA": "N/A: <reason and rule basis>",
        "Security": "N/A: <reason and rule basis>"
      }
    }]
  }
  ```

  Run `kander review plan <absolute-CWD> <absolute-plan.json>`, then preserve the exact stdout bytes of `kander review aggregate <absolute-CWD> <batch-id>` in a private file. Read `batch.revision` from that JSON and calculate the SHA-256 of the exact file bytes. Use those values in the close request:

  ```json
  {
    "batch_id": "<batch-id>",
    "expected_revision": 1,
    "view_hash": "<SHA-256-of-exact-aggregate-output>",
    "author": "<current-owner>",
    "roles": {},
    "resolved_failures": {},
    "opinions": []
  }
  ```

  Run `kander review close <absolute-CWD> <absolute-close-request.json>`. Never guess the revision, reserialize the aggregate before hashing, or treat the N/A closure as a semantic review PASS.
- A plan has at least one batch, its members are fixed at creation, and a card belongs to at most one plan per cycle. A plan can be created only while every member is in `working/` or `review/`. So a group plan is created after the last member started, and no batch runs before the plan exists. The plan names the first batch before its first run. Each later batch is appended with `extend-plan` after its predecessor closed and before its own first run (`extend-plan` cannot adopt a batch that already ran). A plan created with several batches must be continuous: each base equals the target of the batch before it. A run whose batch is outside the plan of a member is refused before the batch exists, and a plan is refused when a member already has a run outside it. A batch may not advance past a planned successor, because the base of a batch is immutable: `review advance` refuses, and `extend-plan` with `drop_batches` removes the trailing batches that have no run. That also unseals the plan, so the plan needs a new seal. Closing a batch requires it to be planned and the worktree clean at its final target; `review advance` also requires a planned batch. Fix rounds advance the batch's runtime target and, in the same transaction, its recorded plan target, plan revision, and every member's `reviews/plan.json` copy.
- Wrap-up evidence binds the base of the first planned batch and the closed final target of the last batch as its `source_commit` when history was not rewritten. When rewritten, `rebased_base` compares the complete patch of base..closed final target with the replayed range. A plan whose recorded target lags the runtime batch (legacy boards) fails `check` and `move done` with both SHAs; `review progress` reports `plan_target` and `batch_target`, and `review extend-plan` with `sync_targets` aligns the record. Completed historical cards without a plan stay readable as legacy-untracked, never as an invented PASS.

Controlled review evidence commands, all under the single review entry:

```text
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
```

- Plan, interpretation, assignment, author originals, generated disposition, and closure artifacts are producer-owned reviews attachments that ordinary update cannot replace. Authors submit only their own assigned items while working, with expected revision; revisions append, and the orchestrator cannot overwrite or impersonate an author's disposition. A generated complete batch view may be published to every member without notifying members that have no findings.
- New readable reports with formatting errors await `review interpret`: the receiver submits a complete structured reading with original report hash, identity, basis and exact quotes; zero findings require an explicit original statement. Pending interpretation is ordinary progress and blocks assignment, incremental consumption, batch closure and done. It never changes execution facts or historical failures.
- Check and completion use the same structural validator: a valid plan awaiting conclusions is pending, a legacy active card without one needs requirements, and malformed evidence, wrong identities, missing copies, or stale closure bindings are errors. Done additionally requires a sealed plan, coverage of every cycle member, all required successful role conclusions, author coverage, and every batch closed at its final target. Explicit N/A is valid; empty indexes, all failed runs, empty conclusions, an arbitrary old role PASS, and partial publication are not.
- Two or more successful runs of one role in one batch that do not chain to each other are chain heads; `review progress` lists them in `next_actions` with `action` `supersede-duplicate-runs`. Choose one chain as the conclusion. A head closes through the close request's `superseded_runs` only when it is a parallel duplicate (`snippets/orchestrate-review.md` "close"): a chain run has the same commit, reviewer, and predecessor, or its predecessor is superseded and it matches the conclusion's commit and reviewer. A head with another reviewer, or a full re-run after a fix, does not qualify as a duplicate. Report the batch ID, role, and run IDs to the user and wait for their decision. With that decision, the entry of each root run outside the chain, and of each run that continues such a root, also carries `decision`, the user decision reference. Findings of every head stay binding, and failed runs keep `resolved_failures`; never open a new batch or plan to discard them.
- Review closure validates Git in the review layer and stores evidence bound to the final target; the board revalidates structure and bindings without interpreting Git or claiming integration. Authorization, actual source-branch delivery, and final Git verification stay separate mandatory duties. Inspect progress through `review progress`; never fabricate a semantic PASS to make check succeed. For a non-Git project with every role explicitly N/A, base and target_commit may be `N/A` and closure records Git as not applicable; a required role still needs real commit targets.
- Reclaiming a planned card (see `snippets/commands/move.md` "Reclaiming") may change its execution cycle; `review progress` then reports `requirements-needed` and the complete `rebind_cycles` map. Use `review extend-plan` with the existing plan ID, expected revision, that map, author, and basis to restore the same requirements for the whole plan; this cannot change batches, sealing, role requirements, member states, or earlier evidence. When every batch of the rebound member had closed, the rebind alone does not pass `done`: `review progress` lists `cycle-batch:<task-id>` and the action `append-cycle-batch`, and `extend-plan` with `batch` appends one batch naming that member, even to a sealed plan. The batch may run a review or record every role as `N/A: <reason and rule basis>` per the explicit N/A path above. A sealed plan takes no other batch. Old failures and findings stay binding, and creating another plan to discard them is forbidden. A successor OWNER may append their own disposition to an assigned finding, preserving the old author's original and lineage. Advance and extend-plan run against the plan's worktree; their CWD must name the same directory as the plan's `cwd`. A plan never moves: after a worktree was moved, recreate it at the plan's `cwd`.
- A `kander move` into `archived/` or `trash/` of a plan or task group member writes a member exclusion record in the same transaction. A card that reached `done/` is not excluded when it is archived later; a move from `done/` into `trash/` is excluded. Only that move writes the record, and the card has no way back. Later plan, batch, and closure writes skip the copies of excluded members and of every archived member, but only an excluded member is exempt from the gate: its cycle needs no rebind, a run names the batch members that remain, and a batch with no member left neither closes nor blocks. An assigned finding of an excluded member still blocks aggregation, advance, and closure until the orchestrator writes a `withdrawn` record through `review disposition`, with the excluded card's revision, `withdrawal.exclusion_id` from `review progress`, and `withdrawal.decision`, the user decision reference. `withdrawn` exists only for an excluded member, never counts as a fix, and appears apart in `runs[].withdrawn` of the aggregate view.
- `review advance` and `--advance-file` refuse to move a batch target while an assigned finding has no author disposition; mechanical items need one too. The error names each task, run, and finding. Finish those fix rounds first. A batch that an older binary advanced too early recovers through the fix dispatch rule of `snippets/commands/dispatch.md`.
