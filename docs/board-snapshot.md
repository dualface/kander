# Board snapshot

`kander list --json` prints a machine-readable snapshot of the board. It is the interface an external view (first the Kander view of the dsh plugin) reads the board and the executing agents through, instead of parsing the table of `kander list` or reading `kanban/` directly. The released command contract is `rules/snippets/commands/list.md`; this document is the full contract.

## Command

```text
kander list [--mobile|--json [--agent-status]] [backlog|todo|working|review|done|archived|trash]
```

- `--json` prints one JSON object on stdout and exits 0. Without `--json` the table output is unchanged.
- `--agent-status` (only with `--json`) fills the agent objects. Without it the command runs no terminal probe at all.
- A state argument limits the snapshot to the cards of that state.
- `--mobile` with `--json`, `--agent-status` without `--json`, an unknown state, or an unknown option is a usage error (exit 2).
- Board read failures exit 1 with the same messages as the table form; journal and invalid-entry advisories go to stderr as before, never into the JSON.

## Schema version 1

### Top level

| Key              | Type    | Value                                              |
| ---------------- | ------- | -------------------------------------------------- |
| `schema_version` | integer | `1`                                                |
| `change_token`   | string  | 64 lowercase hex digits, see "Change token"        |
| `generated_at`   | string  | RFC 3339 time in UTC, for example `2026-11-03T08:23:20Z` |
| `root`           | string  | absolute path of the board directory               |
| `tasks`          | array   | one object per listed card                         |

### Task

Every key is always present. A string field without a value is `""`.

| Key           | Type           | Value                                                  |
| ------------- | -------------- | ------------------------------------------------------ |
| `task_id`     | string         | the task ID                                            |
| `title`       | string         | the card heading                                       |
| `state`       | string         | `backlog`, `todo`, `working`, `review`, `done`, `archived`, `trash` |
| `size`        | string         | `small` or `large`                                     |
| `type`        | string         | the `TYPE` field, for example `Feature`                |
| `task_group`  | string         | the `TASK_GROUP` field (legacy discussion line included) |
| `owner`       | string         | the `OWNER` field                                      |
| `session`     | string         | the `SESSION` field                                    |
| `window`      | string         | the `WINDOW` field                                     |
| `launcher`    | string         | the part of `window` before the first colon            |
| `created_at`  | string         | the `CREATED_AT` field                                 |
| `started_at`  | string         | the `STARTED_AT` field                                 |
| `finished_at` | string         | the `FINISHED_AT` field                                |
| `result`      | string         | the `RESULT` field                                     |
| `revision`    | integer        | the committed revision, equal to `kander show --json`  |
| `agent`       | object or null | see "Agent"                                            |

Tasks are ordered by state in board order, then by sort time descending, then by task ID descending. The sort time is `STARTED_AT` for `working` and `review`, `FINISHED_AT` (or the `spec.md` modification time when it is empty) for `done`, and `CREATED_AT` for every other state; a missing value is `-` and sorts after every time. This order is not the table order: the table sorts `backlog`, `todo`, `archived`, and `trash` cards by task ID only.

### Agent

`agent` is `null` on every card unless `--agent-status` is given. With it, every card in `working` or `review` with a non-empty `window` gets an object; every other card keeps `null`.

| Key             | Type    | Value                                                      |
| --------------- | ------- | ---------------------------------------------------------- |
| `status`        | string  | `alive`, `stopped`, `drifted`, `unknown`, `alive-orphaned`, `identity-mismatch`, `unprobeable` |
| `runtime_state` | string  | `idle`, `working`, `blocked`, `done`, `unknown`            |
| `reason`        | string  | the localized classification detail                        |
| `observed_at`   | string  | RFC 3339 time in UTC of the observation                    |
| `valid`         | boolean | `false` when `status` is `unknown`                         |

The value sets are the status constants of `internal/liveness` and the agent states a terminal reports. The observation is `liveness.ObserveAgentStatus`, the batch observation behind the TUI agent status marks: one agent list per terminal backend, a pane facts probe as the fallback, and no session reverse lookup (unlike `kander check`). A card on a process-pane terminal (`dsh`, `tmux`, `tmux-session`: the pane reports its foreground command and session marker, not the agent) is probed with pane facts and classified by the `kander check` rules for such panes, with `runtime_state` `unknown`; the TUI marks still cover only terminals that report agent identity. A card whose launcher has no probeable pane, such as `foreground` or `console`, gets `status` `unknown`. A consumer that needs to act on a lost agent tests for `stopped`.

## Change token

`change_token` is the SHA-256 of the snapshot content, excluding `generated_at` and every `observed_at`, together with the SHA-256 of the full `spec.md` of every listed card.

- Adding, removing, or moving a card, a `kander update`, and a body or header edit change it, also when an edit bypasses the revision.
- A change of any agent field other than `observed_at` changes it.
- Two snapshots of an unchanged board, with unchanged agent observations, have the same token.
- With a state filter the token covers only the listed cards, so a change to a card of another state does not change it.

A consumer polls the command and redraws only when the token differs from the last one it saw. The token is opaque: compare it for equality only.
