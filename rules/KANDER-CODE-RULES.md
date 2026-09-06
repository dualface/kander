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

## Verification Records

- Run the minimal verification that directly proves the change.
- When a test or the environment fails, record the actual command and error; never mark it as passed. The same applies to pre-existing environment failures.
- Replace sensitive values with `[REDACTED]`; keep the rest of the error text verbatim.
