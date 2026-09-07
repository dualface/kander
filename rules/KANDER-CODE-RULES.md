# Architecture and Code Quality Rules

## Architecture and Boundaries

- Preserve the existing architecture and module boundaries; no circular or reverse references. Dependencies follow the direction the project declares; if none is declared, keep them one-way. When a change is unavoidable, explain the impact first and let the user decide.
- Cross-module access goes only through public APIs, DTOs, or events; never touch another module's internal state, private storage, or implementation details.
- A shared abstraction needs at least two stable call sites; do not abstract ahead of a single use. Never bypass layers for convenience, duplicate domain logic, or create global mutable state.
- Before changing a public interface, a cross-module data model, or the dependency graph, list the affected modules and the compatibility plan.
- Before deleting or merging a module, confirm callers have been retired, the migration path, and the rollback risk.

## Code Quality

- Ensure correctness, readability, and maintainability.
- Performance optimization requires measurements or evidence of a bottleneck.
- Functions and types have a single responsibility.
- Names express business intent; no vague abbreviations or meaningless generic names.
- Validate input at boundaries; handle nulls and exceptions.
- Never swallow errors, degrade silently, or mask failures with default values.
- Resources, concurrency, and cancellation need explicit ownership and lifecycle; avoid leaks, races, and unbounded retries or queues.
- Logs and error messages must not leak credentials, personal data, or internal sensitive information.
- Tests cover directly affected behavior, failure paths, and regression points, and verify the contract.
- Follow the project's formatting, lint, error-handling, and logging conventions; never disable checks or suppress warnings without explanation.
- Comment complex decisions with the reason, not a line-by-line restatement. Remove temporary code in this task or isolate it explicitly.

## Delivery Self-Check

Before a card moves to `review/`, or a single card requests review, run this checklist and record
each result under `IMPLEMENTATION` with the command and its output. A reviewer finding in any of
these categories means the self-check was skipped, and the orchestrator may return the delivery
without a review round.

1. `git diff --check` is clean; no leftover conflict markers, trailing whitespace, or EOF drift.
2. Every non-generated code file touched by this delivery is at most 1000 physical lines, and a file
   that was already above 1000 lines has no net increase.
3. Comments and documents that describe changed behavior are updated in the same diff; no comment
   claims behavior the code no longer has.
4. No dead code: nothing unreachable, uncalled, or unreferenced was added or left behind.
5. No redundant tests: no duplicated coverage of one behavior, no assertion unrelated to the
   behavior under test.
6. The touched modules compile, and the targeted tests for the changed behavior were actually run
   at the final delivery commit; cite that commit next to the result. Evidence produced before the
   last code change is stale and must be rerun.
7. Claims of "all tests pass" name the command, the commit, and the count; a claim without those
   three is treated as not executed.

## Verification Records

- Run the minimal verification that directly proves the change.
- When a test or the environment fails, record the actual command and error; never mark it as passed. The same applies to pre-existing environment failures.
- Replace sensitive values with `[REDACTED]`; keep the rest of the error text verbatim.
