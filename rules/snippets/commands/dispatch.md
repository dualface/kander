# kander dispatch

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Durable Dispatch")

## Purpose and users

Inspects and ends durable dispatches: the receipts behind `notify` and `resume` rounds. Used by the orchestrator to reconcile uncertainty (`show`), to prepare an explicit intent (`prepare`), to obtain the on-behalf wrap-up grant (`authorize-wrap-up`), and to record a terminal decision (`fail`, `cancel`). Executing agents consume dispatches through the ID/epoch-bound forms of `move` and `update`.

## Syntax

```text
kander dispatch prepare <absolute-UTF8-intent.json>
kander dispatch authorize-wrap-up <absolute-UTF8-request.json>
kander dispatch show <task-id> <dispatch-id>
kander dispatch fail|cancel <task-id> <dispatch-id> <dispatch-revision> <reason> [--decision <reference>]
```

## Arguments

- `prepare <absolute-UTF8-intent.json>`: create an intent without sending (not for `wrap-up`, which needs the Git-aware entrance of `notify`).
- `authorize-wrap-up <absolute-UTF8-request.json>`: the on-behalf wrap-up grant.
- `show <task-id> <dispatch-id>`: print the dispatch and its receipts.
- `fail|cancel <task-id> <dispatch-id> <dispatch-revision> <reason> [--decision <reference>]`: record the terminal decision and release the binding.

## Preconditions

- A located board. The rules of the "Durable Dispatch" section below.

## Effects

**Durable Dispatch**

- This section takes precedence over the legacy notification clauses. A `review/` card that belongs to a task group or already carries a `DISPATCH_ID` uses durable dispatch automatically; explicit `--kind fix|sync|wrap-up`, `--dispatch-id`, `--base <full-SHA>`, or `--evidence-file` selects it for `working/` cards and non-group cards. Without any of these, a message to a `working/` card is an unbound legacy message. Default kind is fix; select wrap-up explicitly for post-integration completion. A new request without base uses the current directory's Git HEAD.
- `notify` and `resume` accept these flags. A generated ID is printed before any send when omitted; record it and reuse it on retry. The same ID must retain task, message, kind, baseline, and references; preparation is idempotent and changed inputs conflict. Retries reuse the original baseline and deadline.
- An explicit UTF-8 intent for `dispatch prepare` contains `dispatch_id` (optional), `task_id`, `kind`, `message`, `base`, optional `references` (task ID plus card-relative `path`), `created_at`, and `confirm_by`. The default acceptance deadline is 120 seconds after creation (new notify/resume requests use their timeout); it never resets on restart or retry and is not a work-completion deadline.
- New fix intents require `evidence.fix`: a batch ID, nonempty run/finding references with exact `previous_run_id`, and references to all existing author originals on those findings with their lineage (author, run/finding ID, record ID, task ID, card-relative path). A disposition not yet written is not required. The current batch target must match base, the assignment must include the task, and all published originals must verify; superseded rounds, missing or foreign findings, and incomplete publication reject before sending. Legacy prose uses only the review producer's explicit mapping.
- Pass bindings with `--evidence-file <JSON>` to notify/resume, or as `evidence` in `dispatch prepare`. Same-ID retries retain the frozen binding (omit the evidence file); later author records never rewrite an earlier dispatch; generic `references` never substitute for evidence. Historical unbound intents stay readable but cannot be sent as new fix or wrap-up actions.
- New wrap-up intents require `evidence.wrap_up.git` with absolute CWD, source_commit, target_commit, target_ref (`refs/heads/develop` or `refs/remotes/origin/develop`), author, and basis.
  - The command binds the base of the first planned batch and the closed final target of the last batch (kept in sync by `review advance` and `--advance-file`, see `KANDER-KANBAN-RULES.md` "Review Evidence Completion Gate"). It verifies source and target ancestry into the selected develop reference and rereads that ref.
  - `source_commit` must equal the dispatch base, so pass `--base <source_commit>` whenever it differs from the HEAD of the directory the command runs in. By default source equals that closed final target; include no additional unreviewed commits.
  - After an authorized rebase, supply `rebased_base`: the command verifies base ancestry and compares the complete patch of the recorded range with the rebased range, normalizing only blob hashes and hunk offsets. A different patch rejects, and the orchestrator stops and reports per `KANDER-TASK-GROUP-RULES.md` "Merge-Back and Cleanup Preconditions".
  - Fetch and integration authorization stay the orchestrator's duty; an asserted PR merge never substitutes for this evidence. Integration evidence is stored at the dispatch's card-relative `dispatches/<id>/integration.json`; complete with that source_commit.
- On-behalf wrap-up requires `dispatch authorize-wrap-up <request.json>` with task_id, dispatch_id, expected_revision (dispatch revision), author, and reason, plus a complete wrap-up `intent` with the same IDs when no intent exists. The command creates the intent first, reconciles receipts under the delivery lease, then requires a fresh identity-valid stopped observation; a missing SESSION, an ordinary nonzero return, unknown delivery, active executors, confirmation timeout, or lease expiry proves no exit. An optional `reclaim_decision` cites prior user authorization for a completed reclaim; it performs none and replaces no observation.
- The grant fences the old epoch and issues a wrap-up-only epoch with its own 120-second acceptance deadline; reconcile an existing grant instead of issuing another. Accept it through the ordinary atomic working receipt. It permits only previously authorized cleanup and append-only wrap-up records (WRAP_UP_RECORDS, or the immutable `wrap-up/<dispatch-id>-<epoch>.md` record): no code changes, launch or delivery, ordinary takeover upgrade, runtime identity writes, or original-author conclusions.
- States are prepared, delivery-unknown, accepted, completed, failed, and cancelled. Delivery-unknown is persisted before the send or launch boundary; transport return values and marker echo never mean accepted. On an uncertain send failure, preserve the intent and payload and never immediately launch another executor.
- Query the same ID's receipt before retrying. Completed returns without sending. Accepted returns its receipt unless a same-ID `notify` without `--pane` proves the executor stopped and rotates the epoch before recovering the original session. An explicit `resume --agent` may likewise recover accepted work only with fresh stopped evidence, and without `--agent` an accepted resume is receipt-only. Unknown or missing identity never permits recovery. A known ready session can receive the same ID again, because acceptance is idempotent. Busy waits share the persisted deadline; expiry without a receipt is a nonzero pending result, not completion. A working executor may receive the message but accept it only after that deadline; the late acceptance is rejected and follows the same expiry handling.
- The executing agent first runs `kander move <task-id> working --dispatch-id <id> --execution-epoch <epoch>`; its JSON contains `dispatch` and `replayed`. Start work only for `replayed=false`; on replay or failure, report the receipt and never repeat work, even when the first round already returned to review before notify returned.
- Every bound author update carries the same ID/epoch and current expected revision; bound review disposition JSON also carries `authorization: {"dispatch_id":"...","epoch":1}`. Complete fix/sync with `move review`, or wrap-up with `move done --result completed`, carrying `--dispatch-id`, `--execution-epoch`, `--delivery-commit <final-full-SHA>`, and an applicable `--disposition <card-relative-artifact>`. Completion and its receipt commit atomically. Existing done/review evidence gates still apply, and a recorded SHA is not Git integration verification.
- Only one execution grant is active per card; a new intent replaces only a completed, failed, or cancelled one. Explicit `resume --agent` keeps the user-authorization requirement and rotates the epoch of an unfinished same-ID dispatch with its original payload and timestamps. A prepared or delivery-unknown takeover keeps the current deadline; an accepted takeover requires a stopped observation collected for this request, no older than 30 seconds, matching SESSION/WINDOW/OWNER/STARTED_AT and the card revision, and gets a new 120-second deadline. Old epochs cannot accept, update body or WINDOW, submit dispositions, or complete. No card lock spans an agent session.
- `dispatch fail|cancel` records the explicit terminal decision and releases the card's active `DISPATCH_ID`/`EXECUTION_EPOCH` binding in one transaction; the optional `--decision <reference>` records the user decision in the immutable termination original. It does not move or cancel the task, grant takeover, or discard history. For a pre-upgrade failed or cancelled round that still owns the binding, repeat its matching terminal command with `--decision <reference>` to record only the release. A nonmatching terminal state, a completed round, or a binding owned by a newer dispatch rejects. A changed or expired intent requires explicit disposition before another ID; uncertainty alone is not cancellation authorization.
- Ordinary unbound working messages and non-group legacy notifications create no receipt. Bound card writes cannot omit the execution grant. Dispatch originals and epoch receipts are producer-owned `dispatches/` attachments; guarantees cover controlled card operations, never exactly-once external side effects.

## Output

- One JSON object describing the dispatch and its receipts on standard output.

## Rejections

- A nonmatching terminal state, a completed round, a binding owned by a newer dispatch, a `wrap-up` intent passed to `prepare`, or malformed JSON.
- A missing subcommand, wrong argument count, an empty `--decision` value, or an unknown subcommand is a usage error.
