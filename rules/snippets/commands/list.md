# kander list

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Lists cards, optionally for one state. Used by card creators and the user; orchestrators monitor through `subscribe`, never by polling `list`. `--json` is the machine-readable board snapshot for external interfaces, such as an editor or a host view that polls the board.

## Syntax

```text
kander list [--mobile | --json [--agent-status]] [backlog|todo|working|review|done|archived|trash]
```

## Arguments

- `state`: one of `backlog`, `todo`, `working`, `review`, `done`, `archived`, `trash`.
- `--mobile`: narrow layout.
- `--json`: print the board snapshot below instead of the table.
- `--agent-status`: with `--json` only; observe the executing agent of every `working` or `review` card with a non-empty `WINDOW`.

## Preconditions

- A located board.

## Effects

- Read-only. Without `--agent-status` the command runs no terminal probe. With it, the command runs the batch agent status observation of the TUI: one agent list per terminal backend, a pane facts probe as the fallback, and no session reverse lookup.

## Output

- Without `--json`: one row per card: state, size, time, task ID, title.
- With `--json`: one JSON object on stdout, `schema_version` 1:
  - Top level: `schema_version` (integer), `change_token` (string), `generated_at` (string, RFC 3339, UTC), `root` (string, absolute board directory), `tasks` (array).
  - Each task: `task_id`, `title`, `state`, `size`, `type`, `task_group`, `owner`, `session`, `window`, `launcher`, `created_at`, `started_at`, `finished_at`, `result` (strings; a missing value is `""`, never an omitted key), `revision` (integer, the revision of `kander show --json`), `agent` (object or `null`).
  - `launcher` is the part of `window` before the first colon; `""` when `window` is empty.
  - `agent`: `status`, `runtime_state`, `reason`, `observed_at` (strings), `valid` (boolean). `status` is one of `alive`, `stopped`, `drifted`, `unknown`, `alive-orphaned`, `identity-mismatch`, `unprobeable`. `runtime_state` is one of `idle`, `working`, `blocked`, `done`, `unknown`. `agent` is `null` on every card without `--agent-status`, and on every card that is not in `working` or `review` or has an empty `window`.
  - `change_token` digests the snapshot without `generated_at` and every `observed_at`, plus the `spec.md` of every listed card. Any change of a listed card (added, removed, moved, body or header edited) or of an agent object changes it; an unchanged snapshot keeps it. With a state filter it covers only the listed cards.
  - Tasks are ordered as in the table.
- The full contract is in the repository document `docs/board-snapshot.md`.

## Rejections

- An unknown state, more than one state, or an unknown option is a usage error.
- `--mobile` with `--json`, and `--agent-status` without `--json`, are usage errors.
