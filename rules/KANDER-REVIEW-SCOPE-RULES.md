# Review Scope Rules

Loaded together with `KANDER-REVIEW-RULES.md` whenever `rules.review=true`. This file clarifies how acceptance criteria, existing contracts, regressions, and hardening interact. It does not expand review into unrelated cleanup.

## Existing Contracts Stay In Scope

Acceptance criteria define the requested change, but they do not replace pre-existing project contracts.

- A defect introduced, aggravated, or masked by the change remains in scope when it violates an existing supported behavior, invariant, lifecycle rule, compatibility promise, security boundary, persistence guarantee, or module contract, even if that guarantee is not repeated verbatim in `ACCEPTANCE_CRITERIA`.
- `OUT_OF_SCOPE` may exclude adding a new guarantee that the task never required. It must not exclude preserving a guarantee the system already had or a correctness property necessarily exercised by the changed behavior.
- A reviewer must distinguish "new hardening" from "regression prevention". Adding a new concurrency, platform, resilience, or security guarantee can be out of scope; repairing a race, lifecycle violation, cancellation bug, resource leak, supported-platform regression, persistence break, or trust-boundary regression caused by the current change is not hardening.

Examples:

- Adding Windows support to a Unix-only component may be out of scope. Breaking an already-supported Windows path is a regression and remains in scope.
- Adding retry semantics where none existed may be out of scope. Making an existing retry path non-idempotent is a correctness regression and remains in scope.
- Introducing new synchronization to protect a previously single-threaded component may be hardening. A race newly introduced in an already-concurrent path is a defect and remains in scope.
- Adding cancellation support may be out of scope. Leaking resources or corrupting state when the existing cancellation path is exercised after the change remains in scope.

## Realistic Trigger Requirement

Keeping an existing contract in scope does not relax the realism gate.

A reviewer still needs a realistic trigger tied to the changed behavior or touched boundary. Do not elevate hypothetical interleavings, unsupported environments, fabricated callers, or stacked edge conditions merely because they can be described as contract violations.

## Scope Decisions

When deciding whether a finding is in scope, apply this order:

1. Did the current task introduce, aggravate, or mask the behavior?
2. Does the behavior violate the explicit task contract or an existing supported contract/invariant?
3. Is the trigger realistically reachable in the task's operating context?
4. Would fixing it preserve an existing guarantee, or would it create a materially new product/business guarantee?

If 1-3 are yes and the fix preserves an existing guarantee, the finding is in scope. If the required fix would introduce a materially new business rule, product behavior, or external contract, follow the user-decision rules in `KANDER-REVIEW-RULES.md` rather than silently expanding scope.

## Acceptance Criteria Are Not Exhaustive Safety Clauses

Do not require task authors to restate universal project invariants in every card merely to keep them reviewable. Acceptance criteria should describe the task-specific outcome; existing project rules and supported contracts continue to bind the implementation.
