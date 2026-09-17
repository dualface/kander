# Review Disposition Rules

Loaded together with `KANDER-REVIEW-RULES.md` whenever `rules.review=true`. This file tightens how the main/executing agent may reject reviewer findings. It does not change the author-disposition schema or add a new review stage; disputed must-fix findings may additionally carry immutable arbitration evidence as defined below.

## Independent Verification Before Rejection

The main or executing agent must independently verify each reviewer finding. Agreement is not automatic, but disagreement is not evidence.

For a `blocking`, `high`, or `medium` finding, `rejected` is valid only when the disposition falsifies a material premise of the finding with concrete evidence. A valid rejection basis should rely on one or more of:

- repository evidence showing the claimed path, state, caller, contract, or precondition does not exist;
- an executable verification or reproducer demonstrating that the claimed failure does not occur under the stated trigger;
- an existing test whose assertions directly cover the claimed failure mode and whose execution result is current for the target revision;
- an explicit task, project, compatibility, or support contract showing that the reviewer assumed a guarantee the system does not provide;
- a precise code-path argument that identifies the guard, invariant, ordering guarantee, or ownership rule that makes the trigger unreachable.

A response such as "the implementation is correct", "tests pass", "this is unlikely", "the reviewer misunderstood the code", or a restatement of the author's original rationale is not a sufficient rejection basis by itself.

## Rejected vs Unverifiable

Use `unverifiable` rather than `rejected` when the author and reviewer disagree about factual runtime behavior and neither side can establish the decisive fact with available repository or executable evidence.

Examples include:

- environment-dependent behavior that cannot be reproduced or inspected in the current environment;
- a concurrency or timing claim where no ordering guarantee can be established and no reliable reproducer is available;
- an external-system behavior for which neither the contract nor a trustworthy local substitute is available;
- a migration or compatibility claim that depends on unavailable historical data.

Uncertainty is not evidence that the reviewer is wrong.

## Dispute Arbitration

A `blocking`, `high`, or `medium` finding may enter narrow arbitration when the reviewer and author remain in factual disagreement after the author's independent verification. Arbitration starts only from an `unverifiable` author disposition.

If an author initially records `rejected` and the reviewer dispute remains live because the decisive fact is not actually established, append a new `unverifiable` disposition before arbitration. This keeps the existing closure gate fail-closed while the third opinion is pending.

Arbitration is not another full review round. The arbiter examines only the disputed finding and the evidence needed to decide its material factual premise. Do not ask the arbiter to re-review the entire task or generate unrelated findings.

The arbiter must be independent from both sides of the dispute:

- it must not be the original reviewer agent for the run;
- it must not be the executing/author agent that submitted the disputed disposition;
- when several independent agents are available, prefer a different model family or training lineage to reduce correlated failure modes;
- do not expose the author's private reasoning or the reviewer's private reasoning. Provide their recorded claims and evidence only.

Give the arbiter the minimum complete dispute packet:

1. the relevant task requirement, existing contract, or invariant;
2. the exact reviewer finding and its evidence;
3. the exact latest `unverifiable` author disposition and its basis;
4. the relevant target code or diff and directly related callers/callees;
5. the relevant test, reproducer, runtime, or contract evidence;
6. the precise factual question whose answer decides the dispute.

The arbiter returns exactly one semantic verdict:

- `sustain`: the finding's material premise is established. The author must keep closure blocked and continue through `confirmed` and repair/verification under the normal review loop.
- `overrule`: a material premise of the finding is falsified. The author may append a new `rejected` disposition and should cite the arbitration ID plus the decisive evidence in the basis.
- `inconclusive`: the available evidence cannot establish either side. Leave the item `unverifiable` and follow the existing user-decision/stop behavior; arbitration must not convert uncertainty into PASS.

Record the decision with:

`kander review arbitrate <CWD> <absolute-arbitration.json>`

The arbitration JSON is schema 1 and contains `arbitration_id`, `run_id`, `finding_id`, `batch_id`, `task_id`, `disposition_record_id`, `arbiter`, optional `model` / `effort`, `verdict`, `basis`, `report`, and `report_hash`. `report_hash` is the SHA-256 digest of the exact UTF-8 `report` bytes. Kander binds the record to the latest `unverifiable` disposition and stores immutable copies in the review control archive and the task review archive.

A later author disposition creates a new factual position. An old arbitration does not silently apply to that later disposition; arbitrate again only if the new position is again `unverifiable` and still disputed.

## Evidence Must Address the Actual Claim

The rejection basis must address the reviewer's concrete trigger and impact, not merely a nearby happy path.

- A unit test of one branch does not reject a finding about a different error or cancellation path.
- A passing integration test does not reject a race finding unless the test or an established invariant exercises or excludes the relevant interleaving.
- A type check or compile result does not reject a semantic correctness finding.
- A successful manual run does not reject a persistence, retry, migration, or compatibility finding unless it exercises the relevant state transition.

## Author Self-Defense Guard

When the executing agent is also the author of the implementation, it must reconstruct the finding from the reviewer's evidence before evaluating its own implementation rationale. The verification record should state what fact would make the finding true or false and what evidence was used to decide that fact.

The goal is not to privilege the reviewer over the author. The goal is to make `rejected` mean "falsified by evidence" rather than "the author remains unconvinced".

## Non-Blocking Findings

`low`, `recommend`, and `suggest` findings may still use the existing non-blocking disposition rules. When such an item is rejected, record a concrete basis proportional to the claim; do not require heavyweight reproduction when the item itself is advisory.
