# Review Disposition Rules

Loaded together with `KANDER-REVIEW-RULES.md` whenever `rules.review=true`. This file tightens how the main/executing agent may reject reviewer findings. It does not change the durable disposition schema or add a new review stage.

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
