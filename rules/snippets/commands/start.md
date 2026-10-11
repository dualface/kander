# kander start

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Configured Execution Agents", "Start Parameters and Metadata", "Card Pins", "Launchers")

## Purpose and users

Launches an executing agent on a `todo/` card in its own container and writes the session identity to the card. Used by the card creator (intake option 2, single card) and by the orchestrator; a session that executes a card itself claims with `kander move <task-id> working --owner <agent>` instead.

## Syntax

```text
kander start [--agent <configured-agent>] [--launcher <launcher>] [task-id]
```

## Arguments

- `task-id`: the card; without it, the command lists the `todo/` candidates.
- `--agent <configured-agent>`: override the configured execution agent for this invocation only.
- `--launcher <launcher>`: a built-in launcher name (`auto`, `tmux`, `tmux-session`, `herdr`, `luvus`, `foreground`, `console`) or one provided by a loaded terminal definition (see "Launchers" below).

## Preconditions

- The card is in `todo/` with its gates passed; `start` accepts only `todo` cards. Never `move ... working` first and then `start`.
- For a task group member, the start entry of the task group rules applies first.
- The launcher's environment holds (see "Launchers").

## Effects

**Configured Execution Agents**

- Execution agents come from the configuration (`agents`); a custom agent may also be a reviewer when it declares `args.review` and `review.*`. Templates replace `{model}`, `{effort}`, `{session}` within argv elements; Kander appends the prompt last. No shell interpolation is used. When `prompt_delivery.mode` is `pane`, the prompt is not appended to argv: it is delivered into the agent TUI after it is ready, with the same primitives as notify.
- An agent whose session mode is `none` cannot be resumed; `notify` then uses fresh-process recovery without direct delivery, under the existing stopped-observation and receipt gates.

**Start Parameters and Metadata**

- The agent, launcher, and model tier of `start` default to the configuration; `--agent` and `--launcher` override only this invocation. `SIZE: large` uses `kanban_agents.large`, `SIZE: small` uses `kanban_agents.small`, both falling back to `kanban_agent`. On success, print the scale and the actual agent.
- `start` needs no confirmation and writes the session identifier into `SESSION` and the delivery address into `WINDOW` (herdr `herdr:<tab-id>:<pane-id>`; tmux/tmux-session `<launcher>:<session-id>:<window-id>:<pane-id>`; foreground/console the launcher name). A launch that cannot record the address or start the agent closes its own container and rolls the card back. Old cards missing the two fields get them inserted after `OWNER` on the next start; unstarted old cards are not batch-rewritten.
- New writes use `@kander_session`, `@kander_project`, and `# kander-notify:`; when reading or matching identity, the `@onevoke_*` markers and `# onevoke-notify:` are equivalent. A session marker matches when either side is non-empty and equals the card session; it counts as missing only when both are empty. foreground/console are normalized to the launcher name.
- When start or liveness validation fails, the pre-call text is restored only if the operation still owns the current revision; otherwise the newer text is preserved and a conflict reported.
- `start`, `resume`, and a `notify` that recovers a process write the full prompt to a UTF-8 temporary task file containing the task ID, fixed requirements, and the message body. The agent command line receives only one instruction containing that absolute path. When `prompt_delivery.mode` is `pane`, that instruction is not an argv element: it is delivered after the agent TUI is ready. The task file asks the agent to delete it when done; a failed deletion does not affect the result.

**Card Pins**

- A card may pin its executor and reviewers with optional header fields (lines before the first `## ` heading; the same spelling in a body section has no effect): `EXEC_AGENT`, `EXEC_MODEL`, `EXEC_EFFORT`, and `REVIEW_<ROLE>_AGENT`, `REVIEW_<ROLE>_MODEL`, `REVIEW_<ROLE>_EFFORT` for `PMQA` and `SECURITY`. Only the user decides a pin; never add one on your own. An empty value or `auto` leaves the item to the configuration. Cards without these fields behave exactly as before.
- A pinned value replaces the configuration for that card. A model or effort needs the agent of the same stage. An agent must be a loaded definition (a review pin needs a review template). An effort is accepted only when the agent's argv template takes `{effort}`. Values are one ASCII token that does not start with `-`. An explicit `--agent` (or `kander review` reviewer, or `<AGENT>_REVIEW_MODEL`/`<AGENT>_REVIEW_REASONING_EFFORT`) must equal the pin. A plain `resume` or `notify` recovery refuses when the card's agent differs from `EXEC_AGENT`. Every violation fails before any side effect; to change a pin after `todo/`, use a contract decision, since pins freeze with the contract.
- On a card that pins anything, Kander writes one record per stage after each successful launch (`start`, `resume`, takeover, `notify` recovery) or task-bound review run. The record is `EXEC_RESOLVED` / `REVIEW_<ROLE>_RESOLVED: agent=<v>(<src>) model=<v>(<src>) effort=<v>(<src>)`, where `<src>` is `forced`, `cli`, or `config:<large|small>` and `effort=N/A` marks an agent without an effort argument. These records are managed: `update` cannot change them, and review task-context snapshots leave them out. Pins never enable a review role that the plan marks N/A; `kander review plan` prints a notice instead.

**Launchers**

- Seven built-in launchers plus those provided by loaded terminal definitions (JSON files in the global or project share directory `terminals/`; project replaces global, which replaces an embedded one of the same name). `auto` is resolved at start time among container launchers only, by the highest `auto_priority` whose required environment holds (built-in: `herdr` 200 needing `HERDR_ENV=1`, `luvus` 150 needing `LUVUS_ENV=1`, `tmux` 100 needing `TMUX`). It never falls back to `foreground` or `console`; when none applies, `start` fails without claiming. The resolved launcher is printed and never written back to the configuration.
- `tmux` creates the task window in the background of the starter's session and requires `start` to run inside tmux. `tmux-session` uses one dedicated session per main worktree with one background window per card, needs no enclosing tmux, and prints the session name, window id, and an attach hint. `herdr` requires `HERDR_ENV=1`, `HERDR_WORKSPACE_ID`, and herdr on PATH, and creates a background tab in the current workspace. `luvus` (POSIX only) requires `LUVUS_ENV=1`, `LUVUS_PANE_ID`, and luvus on PATH, opens the agent pane in a new luvus tab in the requested working directory, and fails the start when the installed luvus cannot honor that directory. `foreground` runs in the current terminal and waits for exit. `console` (native Windows only) starts the agent in a separate console window and returns the PID immediately, with no reuse, attach, or output capture.
- POSIX defaults to `auto`; Windows defaults to `console`, rejects `tmux`, `tmux-session`, and `luvus`, supports `herdr`, and resolves `auto` only to herdr.

## Output

- The scale, the actual agent, the resolved launcher, and the container address; `tmux-session` also prints the session name, window id, and an attach hint.

## Rejections

- A card outside `todo/`, a failed `todo` gate, an unavailable agent or launcher, a pin violation, or a failed launch (the container is closed and the card rolled back).
- More than one task ID, an option without a value, or an unknown option is a usage error.
