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
- `start` needs no confirmation and writes the session identifier into `SESSION` and the delivery address into `WINDOW` (herdr `herdr:<tab-id>:<pane-id>`; tmux/tmux-session `<launcher>:<session-id>:<window-id>:<pane-id>`; foreground/console the launcher name). A launch that cannot record the address or start the agent closes its own container and rolls the card back, unless "Late Failures" keeps it. Old cards missing the two fields get them inserted after `OWNER` on the next start; unstarted old cards are not batch-rewritten.
- New writes use `@kander_session`, `@kander_project`, and `# kander-notify:`; when reading or matching identity, the `@onevoke_*` markers and `# onevoke-notify:` are equivalent. A session marker matches when either side is non-empty and equals the card session; it counts as missing only when both are empty. foreground/console are normalized to the launcher name.
- When start or liveness validation fails, the pre-call text is restored only if the operation still owns the current revision; otherwise the newer text is preserved and the start ends as `started-unresolved` ("Late Failures").
- `start`, `resume`, and a `notify` that recovers a process write the full prompt to a UTF-8 temporary task file containing the task ID, fixed requirements, and the message body. The agent command line receives only one instruction containing that absolute path. When `prompt_delivery.mode` is `pane`, that instruction is not an argv element: it is delivered after the agent TUI is ready. The task file asks the agent to delete it when done; a failed deletion does not affect the result.

**Card Pins**

- A card may pin its executor and reviewers with optional header fields (lines before the first `## ` heading; the same spelling in a body section has no effect): `EXEC_AGENT`, `EXEC_MODEL`, `EXEC_EFFORT`, and `REVIEW_<ROLE>_AGENT`, `REVIEW_<ROLE>_MODEL`, `REVIEW_<ROLE>_EFFORT` for `PMQA` and `SECURITY`. Only the user decides a pin; never add one on your own. An empty value or `auto` leaves the item to the configuration. Cards without these fields behave exactly as before.
- A pinned value replaces the configuration for that card. A model or effort needs the agent of the same stage. An agent must be a loaded definition (a review pin needs a review template). An effort is accepted only when the agent's argv template takes `{effort}`, and an execution model only when its start template takes `{model}`. Values are one ASCII token that does not start with `-`. An explicit `--agent` (or `kander review` reviewer, or `<AGENT>_REVIEW_MODEL`/`<AGENT>_REVIEW_REASONING_EFFORT`) must equal the pin. A plain `resume` or `notify` recovery refuses when the card's agent differs from `EXEC_AGENT`. Every violation fails before any side effect; to change a pin after `todo/`, use a contract decision, since pins freeze with the contract.
- On a card that pins anything, Kander writes one record per stage after each successful launch (`start`, `resume`, takeover, `notify` recovery) or task-bound review run. The record is `EXEC_RESOLVED` / `REVIEW_<ROLE>_RESOLVED: agent=<v>(<src>) model=<v>(<src>) effort=<v>(<src>)`, where `<src>` is `forced`, `cli`, or `config:<large|small>`; `effort=N/A` marks an agent without an effort argument, and `model=N/A` an executor without a model argument. These records are managed: `update` cannot change them, and review task-context snapshots leave them out. Pins never enable a review role that the plan marks N/A; `kander review plan` prints a notice instead.

**Launchers**

- Seven built-in launchers plus those provided by loaded terminal definitions (JSON files in the global or project share directory `terminals/`; project replaces global, which replaces an embedded one of the same name). `auto` is resolved at start time among container launchers only, by the highest `auto_priority` whose required environment holds (built-in: `herdr` 200 needing `HERDR_ENV=1`, `luvus` 150 needing `LUVUS_ENV=1`, `tmux` 100 needing `TMUX`). It never falls back to `foreground` or `console`; when none applies, `start` fails without claiming. The resolved launcher is printed and never written back to the configuration.
- `tmux` creates the task window in the background of the starter's session and requires `start` to run inside tmux. `tmux-session` uses one dedicated session per main worktree with one background window per card, needs no enclosing tmux, and prints the session name, window id, and an attach hint. `herdr` requires `HERDR_ENV=1`, `HERDR_WORKSPACE_ID`, and herdr on PATH, and creates a background tab in the current workspace. `luvus` (POSIX only) requires `LUVUS_ENV=1`, `LUVUS_PANE_ID`, and luvus on PATH, opens the agent pane in a new luvus tab in the requested working directory, and fails the start when the installed luvus cannot honor that directory. `foreground` runs in the current terminal and waits for exit. `console` (native Windows only) starts the agent in a separate console window and returns the PID immediately, with no reuse, attach, or output capture.
- POSIX defaults to `auto`; Windows defaults to `console`, rejects `tmux`, `tmux-session`, and `luvus`, supports `herdr`, and resolves `auto` only to herdr.

**Choosing the Card and Coordinating After Launch**

Source: moved from `KANDER-KANBAN-RULES.md` "Claiming, Starting, and Coordination".

- When no task is specified and `todo/` has several cards, list the candidates for the user; task groups are ordered by confirmed dependencies, not asked card by card. When a specified card belongs to a task group, inspect the group and its prerequisites before claiming and use the member-card start entry in `snippets/orchestrate-start.md` "Task Orchestration". If that makes the current agent the orchestrator, advance the group in dependency order instead of claiming the requested card early. For other unmet start conditions, only report the gap; never claim or move back to `backlog/`.

**Late Failures**

A start has three results:

| Result | Exit status | Output | Card | Container |
| --- | --- | --- | --- | --- |
| Success | 0 | the started lines | stays in `working/` | kept |
| Failed, rolled back | 1 | `kander: <error>` | back in `todo/` | closed |
| Unresolved | 3 | `started-unresolved: ...` | keeps its column | kept |

- Before it closes anything, the starter judges executor evidence: the card moved past the starter's own last write, by revision or by column.
  - No evidence: it closes the container and rolls back.
  - Evidence: it keeps the container and the card, and writes the start attempt as `unresolved`.
- Without a container the same rule applies: with evidence the starter keeps the process and does not roll back. A foreground starter then waits for the agent to exit.
- When closing the container fails, the executor may still run. The starter does not roll back, and the result is unresolved.
- A write that lands after the judgement makes the rollback refuse. The result is then unresolved too, although the container is closed.
- The judgement has two limits. A dispatch or a manual `kander update` also counts as evidence, so the result is unresolved; that error is on the safe side. An executor that runs but has not written the card counts as no evidence, so the container closes and the executor ends with it.
- The output is one line, then two next steps:

```text
started-unresolved: <task-id> window=<address> reason=<reason>
  kander resume <task-id> --adopt --decision <reference>
  kander resume <task-id> --agent <agent> --decision <reference> --message <message>
```

- After `started-unresolved`, never retry `kander start`. The card is no longer in `todo/`, and a second executor could overwrite the first. Report to the user and let the user choose adoption or takeover.
- When a takeover or an adoption ended the attempt first, the starter prints `superseded: <task-id> ...` and exits 0. It closes nothing and rolls back nothing.

**Start Checks and Rollback**

- `start` checks the agent, launcher, and TTY before launching. `auto` resolves only among container launchers per `snippets/commands/start.md` "Launchers". The actual launcher's preconditions (its definition's `requires`; `HERDR_ENV=1`, herdr on PATH, and `HERDR_WORKSPACE_ID` for herdr; three TTY standard streams for `foreground`; native Windows for `console`) are checked first. A failed precondition does not claim.
- An agent whose `prompt_delivery.mode` is `pane` is rejected before claiming when the resolved launcher is `foreground` or `console`; the card stays in `todo/`.
- When process creation, the terminal container, or the agent launch fails, and "Late Failures" finds no evidence, the command closes the container it created, restores the document, and moves the card back to `todo/`.
- For `prompt_delivery.mode` `pane`, a second-stage TUI ready failure (`blocked` match, prompt-delivery rejection, or ready timeout) is the same class of failure. Capture the pane output, close this invocation's tab/window, restore the document, and move back to `todo/`, without keeping the container or writing `WINDOW`/`SESSION`. Failures that need a person to answer a dialog say to run that CLI once manually and retry `kander start`; a pure ready timeout reports the captured output without that instruction.
- tmux/tmux-session count as started only after session discovery and pane marker write succeed. When `prompt_delivery.mode` is `pane`, tmux/tmux-session count as started only after prompt delivery succeeds and session discovery and pane marker write succeed. herdr counts as started once `pane run` and any required exact-session discovery succeed; Codex discovers the unique task-bound rollout created after the launch began and persists its ID before best-effort reporting; an agent declaring `session.file` gets no proactive session identity report — its own integration reports the path-kind identity — while for other agents the subsequent session identity report and read-back are best-effort, failures only warn and do not enter the `LaunchFailure` tab close and card rollback path. When `prompt_delivery.mode` is `pane`, herdr also requires prompt delivery to succeed. foreground/console normally count as started once the process is created. Devin additionally requires exact-session discovery and persistence, and a discovery failure terminates the process started by this invocation before rollback. A later exit does not roll back. On success, print the actual launcher (`auto` shows the resolved one); `console` also prints the PID.
- Coordination after launch depends on the launcher:
  - foreground single card: the starter checks the result after the agent exits, until the task completes or is handed over.
  - tmux, tmux-session, or herdr single card: the executing agent reports to the user in its own window or tab; the starter does not patrol. Right after a successful start, tell the user that this session does not track the task, the session can end, and the next task gets a new session. `tmux-session` also gives the session name and attach command, `herdr` the tab and pane id. When the user explicitly asks for tracking, coordinate as a foreground single card.
  - console single card: the executing agent reports in a separate Windows console; the starter does not capture output. After a successful start, give the PID (usable only for read-only existence checks) and say that this session does not track progress; on an explicit tracking request, coordinate as a foreground single card.
  - orchestrator session (`kander orchestrate`): cards it starts are monitored per `snippets/orchestrate-sessions.md` "Orchestrator Sessions" whatever their launcher.
  - task group: only when the module is enabled and dependencies are satisfied, orchestrate per `KANDER-TASK-GROUP-RULES.md`, performing applicable review, integration, and wrap-up; a successful start does not release that responsibility.

## Output

- The scale, the actual agent, the resolved launcher, and the container address; `tmux-session` also prints the session name, window id, and an attach hint.

## Rejections

- A card outside `todo/`, a failed `todo` gate, an unavailable agent or launcher, a pin violation, or a failed launch (the container is closed and the card rolled back).
- More than one task ID, an option without a value, or an unknown option is a usage error.
