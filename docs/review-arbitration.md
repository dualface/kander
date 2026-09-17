# Review Dispute Arbitration

Kander's normal review roles remain `PMQA` and `Security`. Arbitration is not a third review stage and does not participate in role requirements or incremental review lineage.

It is narrow, immutable evidence for one disputed must-fix finding after the executing author has independently evaluated the finding and recorded the latest disposition as `unverifiable`.

## Why it is separate from review roles

A dispute arbiter answers a different question from PMQA or Security:

> Given this exact finding, the author's exact disagreement, and the directly relevant evidence, which material factual premise is established?

Making arbitration another normal review run would force it into batch role requirements, full-report schemas, incremental chains, and historical role migration. Keeping it attached to the disputed disposition preserves the existing review state machine and makes the evidence identity explicit.

## Fail-closed entry state

Arbitration only accepts an `unverifiable` latest author disposition. This is intentional: `unverifiable` already blocks batch closure.

If the author initially used `rejected` but the reviewer dispute remains live because the decisive fact is not established, the author first appends a new `unverifiable` disposition. The batch therefore cannot close merely because a third opinion is still pending.

## Verdicts

An arbitration record accepts exactly three verdicts:

- `sustain` — the reviewer's material premise is established. Keep closure blocked and return to the normal confirm/fix path.
- `overrule` — a material premise of the finding is falsified. The author may append a new `rejected` disposition citing the arbitration evidence.
- `inconclusive` — the evidence does not decide the factual dispute. Leave the item unverifiable and follow the existing user-decision/stop behavior.

The arbitration itself does not mutate author dispositions. This separation keeps the executing agent accountable for the final disposition and repair while preserving the independent arbiter's evidence.

## Independence gate

Kander rejects an arbitration when the named arbiter is the original reviewer agent or the author recorded on the disputed disposition. The record must bind the latest `unverifiable` disposition for the exact run/finding/task and must concern a `blocking`, `high`, or `medium` finding.

This is an agent-level independence check. Model-family independence is a routing policy and cannot be inferred reliably from arbitrary model identifiers, so it remains a workflow rule rather than a hard-coded parser rule.

## Recording evidence

The command is:

```text
kander review arbitrate <CWD> <absolute-arbitration.json>
```

Example input:

```json
{
  "schema": 1,
  "arbitration_id": "arb-pmqa-17",
  "run_id": "pmqa-run-17",
  "finding_id": "PMQA-3",
  "batch_id": "batch-17",
  "task_id": "20260918-example-task",
  "disposition_record_id": "record-pmqa-3-unverifiable",
  "arbiter": "swe2",
  "model": "swe-2",
  "effort": "max",
  "verdict": "sustain",
  "basis": "The reproducer reaches the claimed cancellation ordering and observes the leaked resource.",
  "report": "Exact arbiter output or normalized factual report used for the decision.",
  "report_hash": "<sha256 of the exact UTF-8 report bytes>"
}
```

Kander assigns `recorded_at` and stores immutable copies in both the review control archive and the assigned task's review archive. Reusing an arbitration ID with different content is rejected. If the author later submits a new disposition, an arbitration bound to the previous disposition becomes stale and cannot silently arbitrate the new position.

## Arbiter invocation

This PR deliberately separates **invocation** from **evidence admission**. Kander already supports several native/custom agent harnesses, and model-routing policy changes faster than the durable review schema. The orchestrator should invoke an independently configured agent using its normal/native harness, give it only the narrow dispute packet described in `KANDER-REVIEW-DISPOSITION-RULES.md`, then admit the resulting verdict through `kander review arbitrate`.

A later change can add convenience launcher glue or an Options setting for the arbiter without changing the evidence format above.
