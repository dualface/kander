# Architecture and Code Quality Rules

## Architecture and Boundaries

- Preserve the existing architecture and module boundaries. Dependencies follow the direction the project declares, otherwise stay one-way; no circular or reverse references. When a boundary change is unavoidable, explain the impact first and let the user decide.
- Cross-module access goes only through public APIs, DTOs, or events; never touch another module's internal state or private storage.
- A shared abstraction needs at least two stable call sites. Never bypass layers for convenience, duplicate domain logic, or add global mutable state.
- Before changing a public interface, a cross-module data model, or the dependency graph, list the affected modules and the compatibility plan. Before deleting or merging a module, confirm callers are retired, the migration path, and the rollback risk.

## Code Quality

- Correctness, readability, and maintainability first. Performance work needs a measurement or other evidence of a bottleneck.
- Functions and types have a single responsibility. Names express intent; no vague abbreviations or generic names.
- Validate input at boundaries. Never swallow errors, degrade silently, or mask failures with default values.
- Resources, concurrency, and cancellation have explicit ownership and lifecycle; no leaks, races, or unbounded retries and queues.
- Logs and error messages never leak credentials, personal data, or internal sensitive information.
- Tests cover the directly affected behavior, failure paths, and regression points.
- Follow the project's formatting, lint, error-handling, and logging conventions; never disable checks or suppress warnings without an explanation.
- Comment the reason behind non-obvious decisions, not the code line by line. Remove temporary code in this task or isolate it explicitly.

## Delivery Self-Check

Before a card moves to `review/`, or a single card requests review, run this checklist and record each result under `IMPLEMENTATION` with the command and its output. When the review module applies, a reviewer finding in any of these categories means the self-check was skipped: record that on the card as a self-check failure and fix it in the same fix round as the other findings. It is still an ordinary finding under `KANDER-REVIEW-RULES.md`, never an extra review round or a return outside the review flow.

1. `git diff --check` is clean: no conflict markers, trailing whitespace, or EOF drift.
2. No non-generated code file added by this delivery exceeds 1000 physical lines, and no touched file that was at or under 1000 lines at the base exceeds 1000 lines now. A file already above 1000 lines at the base is not measured. Never trim or reformat lines unrelated to the task to satisfy this item; that widens the diff into files other cards touch.
3. Comments and documents that describe changed behavior are updated in the same diff.
4. No dead code: nothing unreachable, uncalled, or unreferenced was added or left behind.
5. No redundant tests: no duplicated coverage of one behavior, no assertion unrelated to the behavior under test.
6. The touched modules compile, and the targeted tests for the changed behavior ran at the final delivery commit; cite that commit next to the result. Evidence produced before the last code change is stale and must be rerun.
7. A claim of "all tests pass" names the command, the commit, and the count; without all three it is treated as not executed.

Items 1, 2 and 6 come from commands, not from reading. Run them in the task worktree with `BASE` set to the source-branch SHA the branch is currently based on:

```bash
git diff --check BASE HEAD
git diff --name-only --diff-filter=A BASE HEAD | xargs -r wc -l | sort -n | tail -5
git diff --name-only --diff-filter=M BASE HEAD | while read -r f; do
  echo "$(git show "BASE:$f" | wc -l) $(wc -l < "$f") $f"; done | awk '$1 <= 1000 && $2 > 1000'
```

The first prints nothing when clean, the second lists the largest added files, the third prints only touched files that crossed 1000 lines. For item 6, record the build and test commands with the commit they ran at.

## Verification Records

- Run the minimal verification that directly proves the change.
- When a test or the environment fails, record the actual command and error; never mark it as passed, even for a pre-existing environment failure.
- Replace sensitive values with `[REDACTED]`; keep the rest of the error text verbatim.
