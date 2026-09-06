# Kander Kanban Command Protocol

## Applicability

- Read this file when actually using kanban commands. First read the configuration per `KANDER-AGENTS.md`, then load enabled modules as needed; this file does not by default require intake guidance, the Git workflow, review, or fixed reports.
- This file governs the kanban board managed by the `kander` command of the current scope. User instructions and target project rules take precedence; cards only store the task contract and execution records and never override user decisions, project rules, or security gates.
- Before operating on the board, an agent first reads this file under the rules root in full, then reads the target card.

  The command entry below is selected per `KANDER-AGENTS.md` "Scope".

## Storage and Location

- `kanban/` is machine-local shared data that is not committed to Git; its single instance lives in the main worktree root and is used only by agents on the same host and file system.

  Task worktrees create no copies, mirrors, or symbolic links.

  Remote agents cannot see it.

  On Windows, symbolic links, junctions, and other reparse points are always treated as unsafe entries; `kander` reads, writes, and moves through validated Win32 handles.

  POSIX keeps using no-follow file operations.

  Any failed safety check stops the operation; bypassing it with a file manager or plain path APIs is forbidden.

- The lookup order is `KANBAN_DIR` -> `kanban/` in the main worktree of the current Git repository -> search upward from the current directory for `kanban/`.

  `KANBAN_DIR` is only for tests, non-Git projects, or explicit overrides.

  A normal Git project locates it from any worktree like this:

```sh
MAIN_WORKTREE="$(git worktree list --porcelain | sed -n '1s/^worktree //p')"
KANBAN_DIR="$MAIN_WORKTREE/kanban"
```

- `kanban/` is not part of the Kander install payload; its location does not change between global and project installs.

  Board operations themselves create no branches, make no commits, do not push, and do not review.

  The code task a card represents still follows project rules.

  Committing `kanban/` or modifying the project `.gitignore` to propagate it is forbidden.

## Command Contract

Creating entries, querying, and moving between states use only `kander`; replacing them with `mv`, `cp`, or a file manager is forbidden. Body editing and same-state form upgrades follow "Entries and Documents" and "Task Scale and Grouping".

```text
kander init [project-path]
kander list [--mobile] [backlog|todo|working|review|done|archived|trash]
kander show <task-id>
kander new [--large] <feature|bug|chore|research> <slug> <title...>
kander move <task-id> <backlog|todo|working|review|done|archived|trash>
kander pick [task-id]
kander start [--agent codex|claude|grok|cursor] [--launcher auto|tmux|tmux-session|herdr|foreground|console] [task-id]
kander resume [--agent codex|claude|grok|cursor] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--launcher ...] <task-id>
kander notify [--pane HERDR-PANE-ID] [--timeout SECONDS] (--message TEXT | --message-file FILE) <task-id>
kander dismiss [--timeout SECONDS] <task-id>
kander check [--all] [task-id ...]
kander guard-write <path>
kander subscribe [--refresh SECONDS] [--heartbeat SECONDS] <task-group> <task-id>... [--watch <task-id|task-group-id>...]
kander            # open the terminal board
```

`kander show` prints the current state and absolute path before the card body, so the card can be relocated before writing; `kander move` prints the new path after a successful move. `kander guard-write` lets a host project's pre-write hook decide whether a target path would resurrect an old card in a state directory; it exits 0 to allow and non-zero to reject.

New writes use `@kander_session`, `@kander_project`, and `# kander-notify:`.

When reading, doing reverse lookup, or matching identity, `@kander_session` and `@onevoke_session` are both session markers, `@kander_project` and `@onevoke_project` are both project markers, and `# kander-notify:` and `# onevoke-notify:` are both notify instruction prefixes.

A match is when either side is non-empty and equals the card session; the marker counts as missing only when both sides are empty.

After the recovery/takeover channel of `resume` and `notify` succeeds, the new container address must be written back to the card `WINDOW`.

foreground/console are normalized to the launcher name.

When start or liveness validation fails, restore the pre-call original text and do not change the card state.

Neither `notify` nor `resume` moves the card: `review -> working` is performed by the notified or woken executing agent itself via `kander move <task-id> working` before handling the items; that state change is the "received and started" acknowledgement.

When `notify` probes the recorded `WINDOW`, it handles three classes. This classification takes precedence over the recovery overview later in this file.

- (1) **Busy state**: the pane exists and the agent/session match, but herdr is not `idle`/`done` or tmux is in copy-mode.
  - Retry the probe at a fixed interval within the same `--timeout` budget.
  - On timeout, return non-zero with "target agent busy, not delivered".
  - Do not start a recovery instance, do not create a body payload, do not change the card state or body.
  - A missing tmux session marker means identity cannot be proven; it counts as neither busy nor stale and goes through the recovery channel.
- (2) **Stale address**: the pane does not exist/has exited, the agent/session does not match, or the tmux foreground process does not match.
  - herdr does a reverse lookup from `pane list` by agent and session.
  - tmux uses `list-panes -a -F` with a triple filter: non-empty `@kander_session` or `@onevoke_session`, `pane_dead=0`, and the agent executable name.
  - After a unique hit, rerun the full target validation; only on success write back the compatible address and deliver directly.
  - `tmux` writes back the session id; `tmux-session` writes back the session name.
  - Old tmux cards with an empty `WINDOW` do not get a reverse lookup.
- (3) **Reverse lookup failed**: 0/multiple hits, reverse lookup unavailable, or the re-check of the new pane fails; recover through the existing chain.
  - Report both the reason the original address is stale and the reverse lookup reason.

An explicit `--pane` override does no stale-address reverse lookup.

**Start Parameters and Metadata**

- The agent, launcher, and model tier of `start` default to the Kander configuration; when initialization is incomplete, use the defaults.
- `--agent` and `--launcher` override only this invocation.
- Large tasks (directory cards containing `spec.md`) use `kanban_agents.large`; small tasks (single-file cards) use `kanban_agents.small`; both fall back to `kanban_agent`.
- On success, print the scale and the actual agent.
- `start` needs no confirmation by default and writes the session identifier into `SESSION`:
  - Claude/Grok: a UUID; Cursor: the chat id; Codex: only the agent name.
- The adjacent `WINDOW` field holds the delivery address:
  - herdr: `herdr:<tab-id>:<pane-id>`.
  - tmux/tmux-session: `<launcher>:<session-id>:<window-id>:<pane-id>`.
  - foreground/console: the launcher name.
- tmux/tmux-session first create a placeholder window/pane, persist `WINDOW`, then start the agent with `respawn-pane`, and write the pane marker with `tmux set-option -p -t <pane-id> @kander_session <session-id>`.
- Claude/Grok/Cursor use the card id; Codex reuses the `notify`/`resume` rollout resolution.
- Failure to write the address, start, or write the marker closes this invocation's window and rolls back the card.
- When an old card lacks the two fields, insert them in order after `OWNER`; do not batch-rewrite unstarted old cards.

**Resuming the Original Session**

- `resume` wakes up the original agent by the card `SESSION`, preserving context:
  - Claude/Grok use `--resume <uuid>`; Cursor uses `--resume <chat-id>`.
  - Codex uses `codex resume <session-id>`.
  - The Codex session id is looked up in the rollout records under `CODEX_HOME` (default `~/.codex`).
  - Only match user messages that begin with this task's start/resume prompt; do not match orchestrator sessions that merely mention the task ID.
  - Fail when not found.
- Only accepts cards in `review/` or `working/`; exactly one non-empty `--message` or `--message-file` must be given.
- `--timeout` must be a finite number of seconds greater than 60 and defaults to 120 seconds.
- `resume` does not move the card state: the prompt for a `review/` card asks the woken agent to run `kander move <task-id> working` itself before handling the items.
- When start or liveness validation fails, restore the original document; when restoring the document fails, the error states the directory where the card actually is.
- The launcher is the same as for `start`.
- After launch, reuse the same liveness criteria as the `notify` recovery branch: herdr and tmux/tmux-session validate an addressable terminal; foreground and console require the process not to exit during the full timeout observation window.
- herdr validates the pane directly returned by this invocation's `tab create`: `agent` must match and only the states `idle`, `working`, `blocked` are accepted.
- When the pane reports a non-empty `agent_session.value`, it must also exactly match the card session; when no valid identity is reported, the absence alone does not count as failure.
- What is judged here is whether the known new pane is alive, which differs from the duty of direct delivery and reverse lookup to determine the target by session identity.
- This relaxation is not a same-user security boundary.
- On an immediate exit, clean up the new instance and exit non-zero with whatever raw agent output can be obtained; only when validation passes, report `resumed` in the `start` output format.
- Cards without a `SESSION` record (not launched through `start`) cannot be `resume`d.
- `start`, `resume`, and a `notify` that needs to recover a process write the full prompt to a UTF-8 temporary task file on all platforms, containing the task ID, fixed requirements, and the message body.
- The agent command line receives only one instruction containing that absolute path.
- The task file asks the agent to try to delete it when done; failure to delete or leftover files do not affect the result.
- These files get no POSIX permission or Windows ACL check or tightening.
- Native Windows prefers the agent `.exe`.
- When Codex, Claude, Grok, or Cursor only has a `.cmd`/`.bat`, launch through an explicit `cmd.exe /d /s /v:off /c` with the agent adapter layer's argument encoding.

**Taking Over with a New Session**

- By default `resume` keeps the original agent and context per "Resuming the Original Session".
- An explicit `--agent <name>` means the user authorizes a takeover; even when the name equals the original agent, a brand-new session is allocated and the old CLI context is not migrated: the new agent first rebuilds progress from the card, task worktree, Git state, and implementation records, then handles the message.
- Cards that never went through `start` and have no original `SESSION` record still cannot be taken over.
- When new-session preparation such as Cursor `create-chat` fails, the card is unchanged.
- Before launch, overwrite `OWNER`/`SESSION`/`WINDOW` with the new agent, new session, and new launcher; do not change `STARTED_AT`.
- When start or liveness validation fails, restore the whole text and the original state.
- A Codex takeover on tmux/tmux-session discovers this invocation's exact session id, writes it as `codex <id>`, and sets the pane marker.
- herdr and process-type Codex lack a discovery channel; keep `codex` and resolve by rollout mtime in later recoveries.
- After the new agent passes liveness validation and before printing `taken over`, the command attempts a graceful exit and closes the original herdr/tmux container under the identity and single-pane topology gate of `dismiss`.
- When the original agent is already dead, close only when the container topology can be proven; when the original address is empty, foreground/console, or the same as the new address, record N/A.
- When validation, exit, or close fails, only print `original container kept` with the reason; do not force-kill, do not roll back the new agent, and the command still succeeds.
- Cleanup and liveness observation may each use one full `--timeout`; the worst case takes about 2x timeout.
- On success, print `cleaned up original container: ...` first, then `taken over: ...`.

**Notify and Recovery**

- `notify` is the single interface for the orchestrator to dispatch items back to the original executing agent.
- Like `resume`, it only accepts `review/` or `working/` cards, exactly one non-empty `--message` or `--message-file` must be given, and `--timeout` must be a finite number of seconds greater than 60, defaulting to 120 seconds.
- Address priority: explicit `--pane` override, the card `WINDOW` fast path, and when no window is recorded, scanning `herdr pane list` by agent and session id.
- Both the override and the reverse lookup keep using `pane get` to verify that the pane exists, the agent and `agent_session.value` match exactly, and the existing pane state is `idle` or `done`.
- Claude/Grok/Cursor cards with a recorded id are compared directly; only old Codex cards without an id reuse the rollout lookup of `resume`.
- Kander has no agent whitelist; herdr reverse lookup coverage depends on the current version and on whether each `source: herdr:<agent>` integration actually reports session identity.
- After a unique hit, write `herdr:<tab-id>:<pane-id>` back to `WINDOW`.
- 0 or multiple hits do not deliver.
- Direct delivery and reverse lookup are responsible for establishing the identity of an existing target, so when the pane reports no valid session identity, keep rejecting and enter the recovery chain; do not use the relaxed criteria of recovery liveness validation.
- Old tmux/tmux-session cards without a window still cannot be reverse looked up.
- When an address exists, also verify `pane_dead=0`, `pane_in_mode=0`, `pane_current_command` equal to the agent executable name, and require at least one of the pane's `@kander_session` or `@onevoke_session` user options to be non-empty, with that non-empty value exactly equal to the session id resolved from the card.
- When both are empty, identity cannot be proven; treat it as no direct delivery channel and fall back; do not relax to process-name-level approval.
- A mismatched option is a stale address; first do the reverse lookup per the three-class probe result of this section, and deliver directly only after a unique hit passes the re-check.
- tmux user options and herdr `agent_session` both sit within same-user permissions; they only avoid misdelivery and are not a security boundary against malicious same-user forgery.
- Create the body payload only after a direct delivery address exists and the probe passes.
- The Windows temporary root takes only the lexical path returned by GetTempPathW, and the no-follow boundary rejects reparse points component by component.
- The body is written to a `0700` temporary directory and `0600` file that are accessible only to the current user and tightened at creation; the foreground/console fallback creates no payload.
- The terminal receives only one line starting with `# kander-notify:` that contains the absolute path and the marker.
- When matching that instruction line, also recognize `# onevoke-notify:`.
- herdr must use `agent prompt <pane-id> <instruction>` to deliver to the agent TUI already running in the pane: it sends the body according to the pane's actual bracketed-paste mode, then sends an encoded Enter after a delay, and rejects up front when the agent is stopped at an approval or question UI rather than stuffing the body into that dialog.
- Do not switch to the shell-oriented `pane run` (the body and trailing CR are written in one batch, TUIs such as Cursor do not treat that as a submit, and the body stays in the input bar), and do not hand text to `pane send-keys`, which only accepts key names.
- tmux keeps using `send-keys -l` followed by a separate `Enter`.
- herdr uses `pane wait-output --match <marker> --source recent` for literal substring matching, allowing the TUI to add a rendering prefix before the marker.
- tmux uses a bounded `capture-pane` and confirms the marker by literal substring, likewise allowing a rendering prefix.
- A successful delivery action returns success; the command does not move the card. The message body for a `review/` card is prefixed with the requirement "run `kander move <task-id> working` first, then handle the items"; that state change is the start acknowledgement.
- A marker timeout only warns "delivered, not confirmed within timeout" and does not recover a second process.
- Only foreground/console, no direct delivery channel, or a failed probe or delivery action make the command recover the original session internally.
- A process-type recovery must stay alive during the full timeout observation window; after validation, foreground keeps occupying the current terminal until the agent exits.
- herdr recovery liveness follows the new-pane criteria of `resume`, where the state still only accepts `idle`, `working`, `blocked` and rejects `done` and `unknown`.
- When tab/window/process cleanup after a failed recovery validation fails, it must be reported together with the original error, noting that the new agent may still be alive.
- The orchestrator does not call `resume` separately.
- When both paths fail, exit non-zero and report both reasons; the card body and original state are unchanged.

**Dismissal and Terminal Cleanup**

- `dismiss` only accepts `done/` or `archived/` cards and does not change the card body or state.
- `--timeout` must be a finite number of seconds greater than 60 and defaults to 120 seconds.
- It locates the herdr pane or tmux/tmux-session pane by the card `WINDOW`.
- Old herdr cards without `WINDOW`, and stale addresses where the pane does not exist/is dead or the agent, session marker, or foreground process does not match, reuse the unique session reverse lookup of `notify`.
- herdr takes the tab the matched pane actually belongs to as the container; tmux reads the session/window the matched pane actually belongs to with `display-message`; the card `WINDOW` is not written back.
- 0 or multiple hits are both rejected.
- A busy state gets no reverse lookup and is still rejected.
- Before delivery, reuse the exact agent and session match of `notify`: herdr additionally requires `agent_status` to be `idle` or `done`; tmux additionally requires the pane to be alive, not in copy-mode, and with a matching foreground process.
- The current tab or session/window of the validated pane must exactly equal the located container, and the container may contain only that pane.
- When the pane has been moved or the container has other panes, reject before delivery; verify ownership again while waiting for exit and re-check the container topology before closing.
- Claude/Codex receive `/exit`; Grok/Cursor receive `/quit`.
- herdr uses `agent prompt`; tmux uses `send-keys -l` followed by a separate `Enter`.
- Close the herdr tab or tmux window only after confirming the agent process has exited.
- A tmux window that already disappeared with the agent counts as closed.
- Any identity, state, container ownership, delivery, exit confirmation, or close failure, as well as a timeout, returns non-zero and preserves the working state as is; do not force-kill and do not degrade to closing the container.
- `foreground`/`console` have no terminal container to close; report an error without performing any partial action.

**Initializing the Board**

- `init` idempotently creates the board and the 7 state directories (`backlog`, `todo`, `working`, `review`, `done`, `archived`, `trash`); rerunning on an existing board only creates missing directories, and Git projects only update the local `info/exclude`.
- On Windows, new directories must be created with `CREATE_NEW` relative to a pinned parent handle with a protected DACL exclusive to the current user applied at creation; a creation race failure must reject the operation.
- Existing directories only get their leaf directory ACL migrated.
- The parent chain of the Git exclude rejects reparse points component by component, existing ACLs are unchanged, and deduplicating read and append happen within the same pinned leaf handle and file lock.

**Launchers**

- Six launchers: `auto` is resolved at start time and the result is not written back to the configuration.
- auto selection order: when inside herdr (`HERDR_ENV=1`), start with `herdr`; otherwise when inside tmux, start with `tmux`; when inside both, herdr wins; when inside neither, fail without claiming, and do not fall back to `tmux-session`, `foreground`, or `console`.
- `tmux` creates the task window in the background of the starter's current session and requires `start` itself to run inside tmux.
- `tmux-session` determines a dedicated session by the main worktree path (`kb-<dir-name>-<path-digest>`):
  - Create it when absent; reuse it when it exists and `@kander_project` or `@onevoke_project` matches this project.
  - The same project shares one session, with one background window per card.
  - Does not require `start` to run inside tmux; does not switch the client after launch.
  - Prints the session name, window id, and an attach hint.
- `herdr` requires `HERDR_ENV=1` and herdr on PATH; it creates a new tab in the background of the current workspace (`--no-focus`, label reusing `window_name()`), first waits for the root pane to be ready, then runs the same agent command as tmux in that pane, without using `herdr agent start`.
- `foreground` runs in the foreground of the current terminal and waits for the agent to exit.
- `console` is supported only on native Windows; it starts the agent in a separate console window and returns the PID immediately.
- `console` has no session/window reuse, attach, or output capture capability and is not an equivalent implementation of tmux or `tmux-session`.
- POSIX defaults to `auto`; Windows defaults to `console`.
- Windows rejects `tmux` and `tmux-session`; herdr has a native Windows build, so `herdr` is available on Windows, and the options panel offers it when herdr is installed.
- The configuration likewise accepts `auto` on Windows, but there it only resolves to herdr; the options panel does not offer `auto`.
- The agent command sent into a terminal container is parsed once more by that container's shell: POSIX joins per sh; Windows assumes the herdr pane is PowerShell, always encodes argv into `%VAR%` variables restored by `cmd.exe /d /s /v:off /c`, and does not rely on PowerShell to pass arguments to native programs.
- When the agent command contains a newline or NUL, refuse to start; never send half a command to the container.

**herdr Session Reporting**

- When herdr `pane run` succeeds and the session reference is non-empty, `start` and `resume`/`notify` recovery share the session reporting path:
  - Call `pane.report_agent_session` through `HERDR_SOCKET_PATH`.
  - Pass this invocation's pane id, the card agent, the reference, `source=herdr:<agent>`, and a monotonically increasing `seq`.
  - Read back the same `agent_session.value` with `pane get` within a bounded budget.
- An empty reference (including newly started Codex whose id is not yet discovered) is not reported.
- Socket, response, or read-back failures only print one warning; they do not fail the start, roll back the card, or close the tab.
- herdr's own integration may still report the same identity.
- Kander's path exists to fill in launchers that do not trigger the integration hooks.

**Check and Liveness Classification**

- `check` by default checks invalid entries outside `done/` `archived/` and exits non-zero on errors.
- For `todo/`, `working/`, `review/` cards, it also checks contract completeness: missing required sections, leftover `<FILL_IN>` placeholders, or acceptance criteria without `- [ ]` items all count as invalid.
- `--all` includes those two columns.
- When task IDs are given, only the targets and cross-state/cross-form conflicts are checked; unrelated invalid entries do not affect the result, and targets in `done/` or `archived/` are checked too.
- In all modes, parse the `PREREQUISITES` of applicable cards and confirm that references exist and dependencies are acyclic.
- Targeted checks traverse reachable dependencies, including cross-group cycles.
- By default, prerequisite cards in `done/` or `archived/` are only confirmed to exist; only `--all` checks them and their full reachable graph.
- Unsatisfied dependencies do not fail `check`.
- No arguments and `--all` probe all `working/` cards.
- Targeted checks probe only the specified `working/` cards, never `review/`.
- Before the summary line, print a four-state liveness section: `alive` means the agent and available session identity match.
- `stopped` means the pane is gone or the agent/process mismatches and reverse lookup found nothing.
- `drifted` means the session was uniquely reverse looked up to a new pane, with the new address attached.
- `unknown` means an invalid session/window, foreground/console, program unavailable, state or probe failure.
- A herdr pane with missing identity but a valid agent and state is still `alive`, noted as not directly deliverable.
- A Codex empty reference gets no reverse lookup.
- Probe errors are counted as `unknown`; they do not write the card or affect the `check` exit code.
- `subscribe` requires an explicit group ID and non-empty member IDs, and validates member ownership.
- `--watch` may be repeated with external card or group IDs; a group is expanded to all its current members through the dependency resolution reader, without validating external target ownership.
- When an external target does not exist, expands to nothing, duplicates a member, or expansions duplicate each other, fail before subscribing.
- Bare `kander` displays the board read-only; it does not create, move, or start agents.

**Subscription Events**

- `subscribe` prints one JSON object per line containing `event`, `group_id`, and a `tasks` map from task ID to state.
- The initial `event` is `snapshot`.
- State changes are `state-change`, with a `changed` array whose items contain `task_id`, `from`, `to`.
- Heartbeats are `heartbeat`.
- When monitored `working/` cards exist, the heartbeat carries a `liveness` map whose items contain `agent`, `status`, `channel`, `detail`, reusing the four-state classifier of `check`.
- Probe failures count as `unknown` and do not end the subscription; fast refreshes do not probe.
- Without `working/` cards, `liveness` is omitted.
- With `--watch`, `tasks` contains the members and the expanded external tasks, and every event carries a `watched` array of external IDs.
- Without it, `watched` is not printed.
- External state changes also produce `state-change` and reset the heartbeat.
- `--refresh` is the scan interval in seconds, default 1.
- `--heartbeat` is the heartbeat interval in seconds after no state change, default 900.
- Both must be finite and greater than 0.

**Board Display and Interaction**

- Bare `kander` starts the TUI on the alt-screen; loading and errors stay inside the alternate screen, and exiting restores the terminal.
- Default 5 columns on screen; `-`/`=` decrease/increase and save.
- Column widths split the terminal evenly; when "configured column count x minimum width" does not fit, reduce columns, keeping at least one, and keep the selected column visible when switching.
- Columns are rounded panels with the name and task count embedded in the top border; arrows at both ends hint at more columns.
- The selected column's border is highlighted in the column color; the others are low contrast.
- Card titles use the column color; the selected card is fully inverted.
- Preferences such as theme, refresh, and single column are read from the `tui` section of `config.json` and changed in the options panel.
- The single-line top bar has the title and search box on the left, the column count and update time on the right, with one blank line below.
- The bottom status bar has the column count and card count on the left and two common keys on the right; a temporary copy result or error takes the whole bar.
- Arrow keys or `hjkl` switch columns/tasks.
- Single click focuses or selects a card, double click opens details, drag-selected text is copied to the system clipboard automatically.
- The mouse wheel scrolls cards or the body; PgUp/PgDn page.
- `/` or clicking the top-bar search area searches, `y` copies the task ID, Enter opens details, `a` toggles the archived column, `t` cycles auto/light/dark, `o` opens options, `?` opens the key overlay (any key closes it), `r` refreshes, `q` quits.
- Search covers title, task ID, task group, type, owner, and state.
- Details use the same panel style with an embedded-border title, and the body is rendered as Markdown.
- `hjkl`/arrow keys move the cursor, the wheel scrolls, Ctrl-d/u half page, Ctrl-f/b or PgUp/PgDn full page, `gg`/`G` to top/bottom, `/` searches the body, `n`/`N` jump between matches, `v`/`V` character/line selection then `y` copies, and drag selection also copies automatically.
- By default refresh in place by task ID every 30 seconds, preserving the selection and scroll position where possible.
- The scan ignores invalid entries and does not inject the CLI warning "run kander check to see".
- The Go TUI supports Windows.
- Library initialization failures must report the reason.

**Options Panel**

- `o` opens the options panel.
- Sections: interface preferences (theme, maximum columns on screen, minimum column width, auto refresh, single column, all columns, default language).
- Task execution and models (large/small task agent, model, reasoning effort, launcher).
- Review and models (four role reviewers, stage policy, model, reasoning effort).
- Rule modules (seven switches, individually, all on, all off; task groups depend on Git).
- The environment can be checked in place.
- Labels sit above or beside values; dim labels, bold values, and the focused value gets left/right arrows.
- No blank line between fields of the same agent/role; one blank line between different agents/roles.
- Model, reasoning effort, and review stages are indented one level; launcher stands alone at the end of the task execution screen.
- Large and small tasks and the four roles each have independent model and reasoning effort; sharing an agent/reviewer does not merge them.
- A role without a value gets the selected reviewer's default; changing the reviewer resets that role's default.
- After changing the agent/reviewer, the model fields on the same screen update accordingly.
- Configuration is written uniformly to `config.json`: `tui` preferences take effect and are saved immediately, without carrying other unsaved changes.
- The other sections submit with Enter; the root menu "Save and apply" saves the current configuration.
- The old `tui.json` is not read, migrated, or deleted.
- `Up`/`Down` move between fields, `Left`/`Right` change values, `Enter` submits the section and returns.
- Changed values are written to the in-memory session immediately; `Esc` returns and keeps the changed values.
- Environment side effects such as installing tmux run only after the whole section is confirmed with `Enter`.
- `q` or `o` again closes it.
- With unsaved changes, choose "Save and close / Discard changes and close / Continue editing".
- The title permanently shows an unsaved marker.
- Mouse click focuses a row, clicking again or double clicking confirms, the wheel moves between rows.
- The panel only reads and writes configuration; it does not create, move, or start task cards.

- Commands only do structural and mechanical validation; authorization, dependencies, and termination reasons are judged by the agent per this file.

## State Model

The directory is the single source of truth for state; the card body has no `status` field.

- `backlog/`: recorded but not yet committed to execution.
- `todo/`: confirmed by the user, contract complete, not yet claimed.
- `working/`: claimed and being implemented, verified, reviewed, or integrated; task group cards also return here during fix rounds and post-integration wrap-up.
- `review/`: used only by task group cards. Development, verification, and the task branch delivery record are complete, waiting for the orchestrator to ff that delivery onto the group branch and then arrange applicable review and final integration. This state itself does not guarantee the delivery is on the group branch; the orchestrator must verify before releasing in-group dependencies. After moving in, the executing agent ends this round of response and keeps the interactive CLI session; fixes, syncs, or post-integration wrap-up are dispatched back by the orchestrator via `notify`, and the original executing agent first runs `kander move <task-id> working` itself, then handles them.
- `done/`: recent tasks that have satisfied the completion gate.
- `archived/`: completed, cancelled, duplicate, or wontfix records that no longer occupy the active board.
- `trash/`: entries the user explicitly asked to delete but not yet permanently cleaned; not a task state.

```text
backlog <-> todo -> working -> done -> archived        (single-card flow)
                      |  ^
                      v  |  fix rounds and wrap-up move back to working
                    review -> done                     (task group flow; direct move only for orchestrator wrap-up on behalf)

todo -> backlog                                       withdraw commitment, back to scheduling
backlog, todo, working, review -> archived            only user-authorized termination
any state except trash -> trash                       only on explicit user request
```

- Entering `todo/` requires complete `GOAL`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA` (at least one top-level `- [ ]` item with content that can be judged) and `OUT_OF_SCOPE`, with no `<FILL_IN>` placeholders left in these four sections, plus the `SELF_REVIEW:` record line of "Post-Creation Self-Review" (large tasks and task group member cards additionally the `CARD_REVIEW:` line); entering `review/` requires `TASK_BRANCH` to be filled in; the gate for entering `done/` is in "Execution and Completion", the rest in "Termination and Cleanup".
- Older boards have no `review/`: when the other 6 state directories are all present, the first time any `kander` command locates the board it creates `review/` automatically, without requiring the user to rerun `init`.

  When other state directories are missing, stop normal board operations; the initialization command above can create them.

  When `review` is occupied by a file/symbolic link, always fail.

## Entries and Documents

### Invariants

- Every direct child of a state directory is a card: a small task is `YYYYMMDD-short-slug-task.md`, a large task is a directory of the same name that must contain a regular file `spec.md`.

  `short-slug` contains only lowercase ASCII letters, digits, and hyphens.

  The entry name without extension is the task ID.

- Task IDs are unique across the whole board; they must not repeat across states or exist as both file and directory at once. A move moves the whole entry; the entry name never changes after creation, is never copied then deleted, and leaves no copies. Large task directories use only relative links so they stay valid after a move.
- The card path changes with state moves. Before any write, relocate by task ID and take the state and path printed by `kander show <task-id>` as authoritative; when the file at the original path no longer exists, always treat the card as moved, never create a new one at the original path, and relocate before writing. New cards are created only through `kander new`. A host project can call `kander guard-write <path>` in a pre-write hook to intercept such mistaken writes.
- Cards must not contain tokens, credentials, sensitive service addresses, or personal data that should not stay on this machine.

### Small Task Template

```markdown
# <task title>

- TYPE: Feature | Bug | Chore | Research
- TASK_GROUP:
- CREATED_AT: YYYY-MM-DD HH:MM
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

<what to change and why>

## USER_DECISIONS

<directions and trade-offs the user has confirmed; write N/A if none>

## EXPECTED_OUTCOME

<observable, verifiable state after completion>

## ACCEPTANCE_CRITERIA

- [ ] <condition>

## THREAT_MODEL

<for security tasks: assets, trusted principals, and attacker capabilities; for non-security tasks write N/A>

## OUT_OF_SCOPE

- <state exclusion or inclusion for each of the four categories: existing issues, hardening, shared contracts and docs, adjacent features; give a reason for each>

## DISCUSSION

<key conclusions; task group cards also record PREREQUISITES at the beginning>

## IMPLEMENTATION

<plan, branch, commits, verification commands, results, environment gaps, and blockers>

## SUMMARY

<actual results, deviations, unresolved issues, and acceptance conclusion; leave empty before completion>
```

### Large Task Documents

- `spec.md` is required and contains the small task's metadata and contract sections: `GOAL`, `USER_DECISIONS`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, `THREAT_MODEL`, `OUT_OF_SCOPE`, `DISCUSSION`.
- `plan.md` is created as needed to record implementation steps, affected modules, verification, release and rollback plans; it must not modify the `spec.md` contract.
- `report.md` is created on completion to record actual changes, final commits, verification, deviations, unresolved issues, risks, and the acceptance conclusion; do not create it empty.

### Contract and Records

- After claiming, fill in `OWNER`, `STARTED_AT`, and `TASK_BRANCH`; write `N/A` when there is no branch.

  `start` also writes the adjacent `SESSION` and `WINDOW` fields, inserting them after `OWNER` when an old card lacks them; manually claimed cards leave them empty.

  The command fills in `FINISHED_AT` when moving into `done/`.

  The result is filled in only before entering `done/`, `archived/`, or `trash/`.

- Once a card enters `todo/`, `GOAL`, `USER_DECISIONS`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, `OUT_OF_SCOPE`, and task group relations are frozen. Changing any of them requires an explicit user decision first.
- `OUT_OF_SCOPE` defines the task boundary truthfully; unconfirmed extended goals are not written into `ACCEPTANCE_CRITERIA`. When the review module is enabled, refine the scope further per the review contract of `KANDER-REVIEW-RULES.md`.
- During implementation, append only key decisions, verification, environment gaps, commits, blockers, and next steps; do not copy the session transcript. Stable architecture, APIs, and long-term rules must still go into repository documentation or project rules.

## Task Scale and Grouping

- New cards default to small tasks; complex tasks may use directory cards.

  A form upgrade is done only by the current editor in backlog or the owner in working, keeps the same ID, moves the original content into spec.md, and keeps no copy.

  The form does not change in todo.

  A started card is not re-`start`ed or given a different agent because of it.

- Cards keep optional task group fields and dependency records. When task_groups is off, groups are not split automatically and independent single cards remain usable. When on, plan and execute per KANDER-TASK-GROUP-RULES.md; git must be on as well.
- Intake guidance belongs to KANDER-TASK-INTAKE-RULES.md and is read only when rules.task_intake=true; a user operating the board directly does not require it to be on.
- kander new creates the template in backlog; the caller fills in `GOAL`, `EXPECTED_OUTCOME`, and `ACCEPTANCE_CRITERIA` from confirmed content and does not write suggestions as user decisions.

### Post-Creation Self-Review

- After filling in the full contract, the creator must self-review before entering `todo/`; fix any problems found, then re-check; do not advance until it passes. This step is part of the card creation flow and does not depend on the `task_intake` or `review` switch.
- Check against the user's goal, the confirmed plan, and project rules:
  - Goal and outcome are consistent, no confirmed requirement is missing, and no suggestion or assumption is written as a user decision.
  - The task boundary is clear, this round's scope and exclusions do not conflict, and work necessary to reach the goal is not excluded.
  - Constraints are accurate and actionable, consistent with user decisions, project rules, and the actual interfaces and environment, with no contradictory requirements.
  - `ACCEPTANCE_CRITERIA` cover the goal and outcome, are actionable and decidable, and neither miss key conditions nor introduce out-of-scope requirements.
- Record the self-review conclusion and fixes in `DISCUSSION` as a separate line `SELF_REVIEW: <conclusion>` (ASCII colon, may be a list item); the gate for entering `todo/` checks that this line exists. Content that can be fixed from existing decisions is fixed directly; ambiguities that require new or changed user decisions are listed explicitly and wait for the user, not filled into the contract on one's own.
- Large task directory cards and task group member cards additionally require an independent card review beyond the self-review: an independent agent that does not share the card-creation session context (a new session or a subagent) reads only the card and the user's original requirement and issues a conclusion against the four points above; after fixing, the creator records the conclusion and the reviewer in `DISCUSSION` as a `CARD_REVIEW: <conclusion>` line, which the gate checks likewise. The tool only checks that the record line exists; the reviewer's independence and the conclusion quality are still guaranteed truthfully by the creator, and the card-creating agent must not write the `CARD_REVIEW:` line itself to satisfy the gate perfunctorily.

## Claiming, Starting, and Coordination

- When no task is specified and `todo/` has multiple cards, list the candidates for the user to choose; task groups are ordered by confirmed dependencies, not asked card by card. When start conditions are not met, only report the gap; do not claim or move back to `backlog/`.
- Before touching code, the unique entry in `working/` must be obtained first. The two claiming methods are mutually exclusive:

```sh
# Delegate to a new executing agent: start claims and launches atomically
kander start [--agent codex|claude|grok|cursor] [--launcher auto|tmux|tmux-session|herdr|foreground|console] <task-id>

# The user explicitly asks the current agent to execute an existing card: move only, then fill in `OWNER` and `STARTED_AT` manually
kander move <task-id> working
```

- `kander move <task-id> working` applies only when the user explicitly asks the current agent to execute an existing task card.

  Only when the enabled intake guidance is used, choosing "confirm the plan and go through the board" must use `start`.

  Do not `move ... working` first and then `start`.

  `start` only accepts `todo` cards.

  Moving the entry on the same file system is the claiming primitive; only the one whose move succeeds obtains the task.

  After a failure, re-check; do not create a replacement card, and add no lock service, database, or ID allocator.

**Start Checks and Rollback**

- `start` checks the agent, launcher, and TTY before launching.
- `auto` first resolves the current environment, then checks the actual launcher:
  - `tmux`: already inside a tmux session.
  - `tmux-session`: tmux available; the project session name is chosen at start.
  - `herdr`: `HERDR_ENV=1`, herdr on PATH, and `HERDR_WORKSPACE_ID` present.
  - `foreground`: all three standard streams are TTYs.
  - `console`: native Windows.
- A failed precondition check does not claim.
- When process creation, tmux session, tmux window, herdr tab, herdr pane readiness wait, or `pane run` fails, restore the document and move back to `todo/`.
- A failed herdr readiness wait or `pane run` must also close the newly created tab; a failed Codex session discovery or pane session marker write on tmux/tmux-session must also close the newly created window.
- Command text sent before the shell of a new tab takes over the terminal is discarded, so `pane run` must happen after the pane renders its first frame of output; the readiness wait has an upper bound, and a timeout is treated as failure.
- tmux/tmux-session count as started only after session discovery and pane marker write succeed.
- The herdr success condition is in the best-effort clause later in this section.
- foreground/console count as started once the process is created; a later exit does not roll back automatically.
- On success, `console` prints the PID and returns immediately.
- On success, print the actual launcher.
- `auto` must show the resolution result (`herdr` or `tmux`).

- herdr counts as started once `pane run` succeeds; the subsequent session identity report and read-back are best-effort, failures only warn and do not enter the `LaunchFailure` tab close and card rollback path.
- The temporary task file of `start` contains only the task ID and fixed requirements; the agent command line receives only one instruction to read that file.

  The executing agent first verifies the configuration through the entry, then reads this protocol, the card, and project rules, and prepares the actual working directory per the applicable flow.

  Fill in `TASK_BRANCH` when a task branch is actually used.

  In the start task file of a task group, "exit" means ending this round of response; it does not require actively exiting the interactive agent CLI or closing the terminal container. When a non-interactive invocation ends naturally, later dispatch-back is recovered by `notify`; an interactive session is kept until the user explicitly agrees to dismiss it.

- After claiming, only the executing owner may modify or move the `working/` entry; coordinating and orchestrating agents supervise read-only. After an explicit handover, the new owner takes over; no concurrent writes.
- Coordination responsibility after launch depends on the launcher:
  - foreground single card: the starter checks the result after the agent exits, until the task completes or is explicitly handed over.
  - tmux, tmux-session, or herdr single card: the executing agent reports to the user directly in its own window or tab; the starter does not patrol.

    Right after a successful start, tell the user that this session does not track the task's progress, the current session can end, and the next task gets a new session.

    `tmux-session` also gives the session name and attach command.

    `herdr` also gives the tab id and pane id.

    After `auto` resolves to `herdr` or `tmux`, coordinate per the corresponding single-card rule.

    When the user explicitly asks for tracking, coordinate as a foreground single card instead.

  - console single card: the executing agent reports to the user directly in a separate Windows console; the starter does not capture output.

    After a successful start, tell the user the PID and that this session does not track progress.

    That PID is only for read-only checks of whether the process still exists; it cannot be used to attach or recover output.

    When the user explicitly asks the starter to track, coordinate as a foreground single card instead.

  - task group: only when the module is enabled and dependencies are satisfied, orchestrate per `KANDER-TASK-GROUP-RULES.md`, performing applicable review, integration, and wrap-up; a successful start does not release that responsibility.

## Execution and Completion

- A single card is delivered per the task contract, user and project rules, and the currently enabled Kander modules.

  When the Git module is off, develop, worktrees, commits, push, or merge back are not required.

  When the review module is off, review is not required automatically.

  The user's own review, PR, or acceptance conditions must still be satisfied.

- Confirm the actual working directory from the card records; record implementation, verification, and unresolved issues.

  After completing the task contract and all applicable delivery steps, fill in `SUMMARY` or report.md, set `RESULT: completed`, run kander move <task-id> done and kander check.

- On failure or pause, keep the actual state and record the blocker and the condition to unblock. Write N/A for inapplicable Git or review steps; do not write unexecuted verification as passed.
- Task group members run review, integration, and wrap-up per KANDER-TASK-GROUP-RULES.md only when task_groups and git are enabled. These gates cannot be applied to independent single cards.
- The fixed report format for the user is read from KANDER-REPORTING-RULES.md only when rules.reporting=true. When off, use the user's own reporting form; the card result and necessary execution records are not omitted.
- A card that has entered done is not moved back or reused because a new problem is found afterwards; create a new card for the new problem and point it at the original card.

## Termination and Cleanup

- Only after the user explicitly cancels, judges it a duplicate, decides not to fix, or accepts an alternative direction may a `backlog/`, `todo/`, or `working/` card (as well as a review-state card) be archived directly.

  Implementation difficulty, failed verification, or a temporary blocker is not authorization.

  The result can only be `cancelled`, `duplicate`, or `wontfix`, each with a reason; `duplicate` must also point to the replacement card.

  `completed` is used only for `done -> archived`.

- After a card moves into `archived/` or `trash/`, the agent executing or operating on that card reports the result per the user's convention (using the `KANDER-REPORTING-RULES.md` template only when `rules.reporting=true`), with the last status line stating the actual destination and result.
- `done/` keeps recently completed items; archive them after the user confirms they need not be shown. Move a card into `trash/` only when the user explicitly asks to delete that specific card; before moving, write `RESULT: trashed`, the reason, and the time. Do not empty or permanently delete automatically; permanent deletion requires per-item authorization.

## Failure Recovery

- When a `working/` card is interrupted, has no owner, or makes no progress for a long time, the coordinating agent first notifies the original executing agent with `kander notify <task-id> --message <status and requirements>`.

  The command chooses direct delivery or recovery on its own; on a non-zero exit, the user decides on handover or termination.

  When the user decides to switch agents, use only `kander resume --agent <name> <task-id> --message <status and requirements>` to establish a takeover with a new session; do not migrate the session manually and do not `start` again.

  Other agents must not take over, move, or archive on their own.

  A process exit does not change `working/` and does not move back to `todo/`.

- On duplicate IDs, cross-state copies, a file and a directory with the same ID, a large task missing `spec.md`, conflicting targets, or missing or unwritable state directories, stop the affected operation and preserve the working state. Do not bypass the error by deleting, renaming, or moving.
- The board has no Git history; on accidental deletion, first check `trash/` and local backups; do not fabricate content.
