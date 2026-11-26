# kander list

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Lists cards, optionally filtered, searched by title, or paginated. Used by card creators and the user; orchestrators monitor through `subscribe`, never by polling `list`. `--json` is the machine-readable board snapshot for external interfaces, such as an editor or a host view that polls the board.

## Syntax

```text
kander list [--agent-status] [backlog|todo|working|review|done|archived|trash]
```

## Additional Syntax

```text
kander list [--agent-status] [--assignee NAME] [--submitter NAME] [--group ID] [--search TEXT] [--limit N] [--cursor TOKEN] [backlog|todo|working|review|done|archived|trash]
```

## Arguments

- `state`: one of `backlog`, `todo`, `working`, `review`, `done`, `archived`, `trash`.
- `--assignee NAME`, `--submitter NAME`, `--group ID`: exact matches. Filters combine with AND.
- `--search TEXT`: literal title substring, with case-insensitive ASCII letters. Three or more Unicode characters use a trigram index. Shorter terms still match.
- `--limit N`: page size, an integer from 1 to 1000. Without it, return all matching cards.
- `--cursor TOKEN`: continue with the token from the previous page. Keep the same filters and supply `--limit`.
- `--agent-status`: observe the executing agent of every `working` or `review` card with a non-empty `WINDOW`. It works only with the global `--json`; without `--json` it has no effect.

## Examples

```text
# List every card
kander list

# List the cards in working
kander list working

# Search titles and print the first 20 matches
kander list --json --search login --limit 20

# Print the board snapshot with the agent status as JSON
kander list --json --agent-status
```

## Preconditions

- A located board.

## Effects

- Read-only. Without `--agent-status` the command runs no terminal probe. With it, the command runs the batch agent status observation of the TUI: one agent list per terminal backend, a pane facts probe as the fallback, and no session reverse lookup.

## Output

- Without `--json`: one row per card: state, task ID, title, and owner or current claim. When another page exists, stderr prints its cursor.
- With `--json`: one object, `schema_version` 1:
  - Top level: `schema_version` (integer), `change_token` (string), `generated_at` (string, RFC 3339, UTC), `root` (string, absolute board directory), `tasks` (array).
  - The server event position is `cursor`. With another page, `next_cursor` is the token for `--cursor`. It is not an event cursor.
  - Each task: `task_id`, `title`, `state`, `size`, `type`, `task_group`, `owner`, `submitter`, `assignee`, `session`, `window`, `launcher`, `created_at`, `started_at`, `finished_at`, `result` (strings; a missing value is `""`, never an omitted key), `revision` (integer, the revision of `kander show --json`), `agent` (object or `null`).
  - `owner` is the executing agent, `submitter` is the submission identity, and `assignee` is the person currently responsible.
  - `launcher` is the part of `window` before the first colon; `""` when `window` is empty.
  - `agent`: `status`, `runtime_state`, `reason`, `observed_at` (strings), `valid` (boolean). `status` is one of `alive`, `stopped`, `drifted`, `unknown`, `alive-orphaned`, `identity-mismatch`, `unprobeable`. `runtime_state` is one of `idle`, `working`, `blocked`, `done`, `unknown`. `agent` is `null` on every card without `--agent-status`, and on every card that is not in `working` or `review` or has an empty `window`.
  - `change_token` digests the snapshot without `generated_at` and every `observed_at`, plus the `spec.md` of every listed card. Any change of a listed card (added, removed, moved, body or header edited) or of an agent object changes it; an unchanged snapshot keeps it. With a state filter it covers only the listed cards.
  - Tasks are ordered by state in board order, then by newest sort time (`STARTED_AT` for `working` and `review`, `FINISHED_AT` for `done`, `CREATED_AT` otherwise), then by task ID descending. This is not the table order.
- The full contract is in the repository document `docs/board-snapshot.md`.

## Rejections

- An unknown state, more than one state, or an unknown option is a usage error.
- `--agent-status` without the global `--json` is a usage error.
- A cursor without a limit or a limit outside 1–1000 is a usage error. A cursor that does not match the project or filters is rejected by the service.
- If the service index is untrusted, the command stops with `index_rebuilding` and prints no list.
