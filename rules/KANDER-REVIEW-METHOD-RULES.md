# Review Method Rules

Loaded together with `KANDER-REVIEW-RULES.md` whenever `rules.review=true`. This file defines how a reviewer investigates correctness. It does not add review stages, change reviewer selection, or expand task scope.

## PMQA Investigation Method

A first-round PMQA review is an independent attempt to falsify the implementation, not a summary of the author's approach.

1. Reconstruct the expected behavior from the task goal, expected outcome, acceptance criteria, existing project contracts, and the relevant pre-change behavior.
2. Inspect the complete review range and identify changed entry points, state transitions, external contracts, ownership or lifecycle boundaries, and callers or consumers whose behavior depends on the change.
3. Map every acceptance criterion to the implementing code path, the observable behavior it promises, and the verification or test evidence that exercises it.
4. For each changed behavior, actively try to construct a concrete counterexample. Check the applicable cases among invalid, empty, boundary, and unexpected inputs; error and partial-failure paths; retries and idempotency; cancellation and timeout; resource cleanup; lifecycle and state-transition ordering; concurrency; compatibility; and persisted-data behavior.
5. Do not restrict inspection to changed lines. Follow changed symbols into callers, callees, shared invariants, and adjacent module boundaries when required to establish whether the changed behavior is correct.
6. Treat tests as executable claims, not proof by existence. Determine what each relevant assertion actually proves and whether mocks, fixtures, skipped paths, or missing negative cases can hide the suspected defect.
7. Prefer concrete findings. A finding should identify the violated requirement, contract, or invariant; a realistic trigger; the observable impact; the supporting code evidence; and, when practical, a minimal reproducer or verification step.
8. PASS only after every acceptance criterion and every applicable review lens above has been examined. Absence of an obvious diff-level problem is not sufficient for PASS.

The reviewer is adversarial toward the implementation, not toward the author. Do not invent low-realism failures merely to avoid PASS; the reachability and scope limits in `KANDER-REVIEW-RULES.md` still apply.

## Cognitive Independence

- Author explanations are not correctness evidence. On a first-round review, form the correctness judgment from the task contract, repository, review range, existing contracts, and objective verification records.
- Do not adopt the author's confidence, implementation summary, rationale, or claimed correctness as premises. They may help locate code, but each claim still requires independent verification.
- A successful test run proves only that those tests passed under the recorded conditions. It does not establish correctness beyond the behavior those tests actually exercise.
- `Review Focus` and profile-generated focus are additive guidance, not an exhaustive checklist and not a scope restriction. Risks not named in the focus remain reportable when they are in scope under the task contract and `KANDER-REVIEW-RULES.md`.

## Finding Evidence

For a substantive correctness finding, the reviewer should make the report independently actionable by including, when applicable:

- the file, symbol, or other precise location;
- the violated requirement, existing contract, or invariant;
- the concrete trigger or precondition;
- the observable failure or regression;
- the code or behavioral evidence supporting the claim;
- why current tests or verification do not establish the correct behavior;
- a minimal verification or reproduction step when practical.

Do not require speculative fixes in order to report a valid finding. The finding establishes the defect; the executing agent remains responsible for choosing and verifying the repair.

## PASS Coverage Evidence

A PMQA PASS must include a concise coverage summary. At minimum it records:

- how each acceptance criterion was mapped to implementation and verification evidence;
- which changed entry points or externally visible behaviors were inspected;
- which realistic regression or failure-path classes were applicable and checked;
- whether lifecycle, concurrency, persistence, compatibility, or external-contract review was applicable, and the basis for N/A when it was not;
- any verification limitation that materially reduces confidence.

The coverage summary is evidence that the review procedure was performed; it is not an invitation to restate the patch or produce a long narrative.

## Incremental Reviews

The incremental-review rules in `KANDER-REVIEW-RULES.md` remain authoritative. In an incremental round, apply the same adversarial method only to the new material, the prior findings being closed, and requirements or invariants touched by the fix. Do not use this file to re-audit unchanged code when the main review protocol forbids it.
