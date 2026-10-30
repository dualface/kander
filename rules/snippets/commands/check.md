# kander check

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Check and Liveness Classification")

## Purpose and users

Validates cards and probes executing agents (`check`), and analyzes a Git delivery (`check delivery`) or the overlap between two branches (`check overlap`). Used by every executing agent for its own card and delivery self-check, and by the orchestrator for the group.

## Syntax

```text
kander check [--all] [task-id ...]
kander check delivery --base <ref> [--commit <ref>] [--json]
kander check overlap --source <ref> [--head <ref>] [--json]
```

## Arguments

- `check [--all] [task-id ...]`: validate the board or the named cards; `--all` includes `done/` and `archived/`.
- `check delivery --base <ref> [--commit <ref>] [--json]`: `git diff --check` and line-count candidates of `base..commit`; `--commit` defaults to `HEAD`.
- `check overlap --source <ref> [--head <ref>] [--json]`: paths changed on both sides of the merge base; `--head` defaults to `HEAD`.
- `--json`: one schema-versioned object and a trailing newline on standard output.

## Preconditions

- `check` needs a located board. `delivery` and `overlap` are board-free and run in any Git worktree; `delivery` rejects a non-ancestor base.

## Effects

- `check` by default checks invalid entries outside `done/` and `archived/` and exits non-zero on errors; `--all` includes those two columns. For `todo/`, `working/`, `review/` cards it also checks contract completeness with the `todo/` gate rule: a missing or empty `GOAL`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, or `OUT_OF_SCOPE`, a leftover `<FILL_IN>` placeholder in those sections, or acceptance criteria without `- [ ]` items is invalid. With task IDs, only the targets and cross-state or cross-form conflicts are checked, including targets in `done/` or `archived/`.
- In all modes it parses `PREREQUISITES` of applicable cards and confirms references exist and dependencies are acyclic; targeted checks traverse reachable dependencies, including cross-group cycles. Prerequisites in `done/` or `archived/` are only confirmed to exist unless `--all`. Unsatisfied dependencies do not fail `check`.
- `check` also reports missing or conflicting review intents, incomplete publication, index/manifest/hash mismatches, language or member conflicts, and broken predecessor chains; it never infers PASS from prose and closes no batch.
- No arguments and `--all` probe all `working/` cards; targeted checks probe only the specified `working/` cards, never `review/`. Before the summary line, a liveness section is printed, with a reason for each card. `alive`: agent and available session identity match, or a foreground or console process identity matches completely; presence only, not readiness or progress. `alive-orphaned`: the agent of a `foreground` card runs, but the Kander process that started it is gone. `stopped`: pane gone or mismatched and a valid reverse lookup found nothing, or the recorded process exited, its ID was reused, or the system restarted. `drifted`: uniquely reverse looked up to a new pane, attached as the new address. `identity-mismatch`: a process holds the recorded ID and start time but runs another executable, or the card moved to another execution; never treat it as alive. `unprobeable`: the platform has no process identity, or it refused the probe. `unknown`: invalid session/window, a foreground or console card without a process identity, probe failure, lookup error, timeout, or multiple matches; it does not prove the session stopped. `unprobeable` and `unknown` never read as alive. Next steps: for `alive-orphaned`, adoption or a takeover; for `stopped`, a takeover; for `identity-mismatch`, `unprobeable`, and `unknown`, verify by hand. Probe errors count as `unknown` (a refused probe as `unprobeable`), write no card, and do not affect the exit code. Probes share a bounded per-card and batch budget; unfinished cards keep an `unknown` reason.
- `kander check delivery` and `kander check overlap` are board-free, read-only Git analyzers. They do not locate a kanban directory, do not fetch or write refs, and invoke Git through a process argv with no shell. `delivery` defaults `--commit` to `HEAD`, requires `--base`, rejects a non-ancestor base, and reports `git diff --check` plus physical line-count candidates. `overlap` defaults `--head` to `HEAD`, requires `--source`, and reports the path intersection of both sides of the merge base, including rename and copy old and new paths. Never substitute `comm`, `git diff --name-only` pipelines, or other shell pipelines for these commands.

## Output

- `check`: diagnostics per card, the liveness section, and a summary line.
- `delivery` and `overlap`: human-readable lines, or with `--json` one object (`status` `pass`, `fail`, `review-required`, or `error`; `delivery` lists `diff_check`, `added_over_limit`, `crossed_limit`; `overlap` lists `paths`). Recognized argument, Git, output-limit, and internal errors also use `status:error` JSON.
- Exit codes of `delivery` and `overlap`: 0 is a completed check with no action, 1 is a completed check with findings, 2 is a usage error, and 3 is an incomplete Git or execution error. Never record PASS after an incomplete check.

## Rejections

- An unknown option, a duplicate flag, a missing `--base` or `--source`, or an unexpected argument is a usage error (exit 2 for `delivery` and `overlap`).
