# Changelog

## v0.7.17 — 2026-09-22

- Feature: the options-panel workflow chart now covers the whole card lifecycle from the intake gate to the completion report. It is a Phase/Gate/Exit model: every rule loop is an `Exit.Target` back edge, and every user decision is marked. All seven module switches and that scale's agents, reviewers, and stage policies change the chart; an unresolvable stage policy stays visible as `invalid`. The renderer draws every target through side gutters, and falls back to a compact form that names the destination when labels would no longer be readable. `internal/flow` emits catalog keys, not text.
- Docs: `docs/workflow-chart.md` records the Phase/Gate/Exit model, which module switch owns which phase, and the rule clause behind every gate, exit, and back edge.
- Fix: a task-group merge-back no longer shows the single-card code-conflict re-review exit; it uses the Markdown-only conflict the merge-back rules name. The reduced delivery check keeps the review-required exit. Compact layout starts gate trees at the left edge and puts a clipped target on its own line. An auto stage still notes that a higher-precedence source can require or skip the role.

## v0.7.16 — 2026-09-22

- Feature: `kander review plan`, `extend-plan`, `assign`, `disposition`, `advance`, and `close` accept `--schema` and print every accepted JSON field, including nested fields, whether it is required, its type, closed values, and a description in the interface language. The command takes no working directory and no JSON file, does not locate a board, and writes nothing. `map-legacy`, `aggregate`, `progress`, and a normal review run have no schema output.
- Fix: assignment, disposition, `passed_at`, and same-run mechanical checks name the field or comparison that failed. Unknown-field errors are unchanged.
- Fix: advancing a review batch may record commits from outside the batch in `foreign_commits`, each with a nonempty reason. Every commit in the range appears in exactly one of `deliveries` or `foreign_commits`. A delivery still maps to a member task. An outside commit cannot be recorded as a batch fix. Advance files whose range contains only member deliveries stay valid without `foreign_commits`.
- Fix: Devin review auto-approves every tool call, so a non-interactive run no longer stops on a confirmation and returns an empty report. Devin's read-only posture is the review prompt plus the post-run Git check. The other built-in reviewers stay on their isolation arguments.
- Rules: reading `kander subscribe` is a background process whose standard output is a new UTF-8 file of JSON lines. The agent reads complete lines from a cursor, waits on that file, and treats unread lines as pending work. Waiting on the subscription file is allowed. Polling the board, the cards, and the executing agents is not.
- Rules: the review schema text matches stored validation. Verification may remain on a non-fixed disposition, duplicate task IDs are removed rather than rejected, and the rules name `authorization`, `previous_record_id`, and `opinions`.

## v0.7.15 — 2026-09-21

- Feature: the board stacks consecutive states into fewer visual columns when the terminal cannot fit the configured column count at `min_column_width`. Each visual column starts with one state and may take more while reserving one state for each remaining column and keeping every stacked panel's full content visible. Rendering, paging, scrolling, and mouse hit testing use that same geometry. The option is "Stack columns when space is limited" (`tui.compact`).
- Fix: `tui.compact` is stored with the other TUI fields when the scope section syncs, and changing the compact override in the options panel rebuilds that field when its presence changes, so the inherited value stays.

## v0.7.14 — 2026-09-21

- Fix: the auto theme follows a terminal background change while Kander is running. The probe is serialized on the TUI I/O boundary, applies only between frames, and ignores stale or invalid replies. Named themes do not probe. Unsupported terminals, timeouts, and Windows keep the startup palette. An open Huh form repaints in the same frame, so the form palette and the popup palette do not mix.
- Fix: an agent that declares `session.file` no longer receives an eager herdr id-kind session report. That report raced the agent's own path-kind report and could warn that the herdr session identity failed to report after a successful start. Agents without the declaration still get the report and a real failure warning.
- Rules: when task intake is on, a request expected to create, modify, delete, or regenerate code waits for an explicit execution choice before the first code write. Leaving Plan mode, or accepting its implement action, does not select that choice. Read-only work, and documentation that does not change executable code, stay exempt.

## v0.7.13 — 2026-09-20

- Fix: Homebrew self-updates now run `brew update` to refresh the tap metadata before `brew upgrade dualface/tap/kander`, so a stale local tap can no longer make the upgrade exit successfully while keeping the old binary. The upgrade targets the fully qualified formula name instead of the ambiguous short name, and the post-upgrade PATH version probe still fails the update when the installed binary is older than the release. Refresh failures, upgrade failures, and a stale post-upgrade version each return their own error with the bounded command output preserved for the TUI result.

## v0.7.12 — 2026-09-20

- Feature: interactive startup checks and `kander doctor` now offer one localized confirmation when installed rule files contain local edits. On confirmation, Kander creates a unique timestamped backup of each original file before atomically installing the embedded rule, reports every file result independently, and updates rule state only for successful replacements. Declining, ended input, backup or replacement failures, and non-interactive doctor paths preserve local edits; tests now resolve localized expectations through the configured catalog.

## v0.7.11 — 2026-09-20

- Feature: bare TUI startup now checks the latest stable GitHub release asynchronously and defers its update prompt until other overlays close. Confirmed Homebrew updates run `brew upgrade kander`; direct installs download the matching platform archive and checksums, enforce trusted hosts and size limits, verify SHA-256, safely extract and version-probe the binary, atomically replace the executable, then restart into the verified version. Failed checks remain non-blocking, and failed replacements preserve the installed binary.

## v0.7.10 — 2026-09-20

- Fix: a task card with broken task group ownership no longer blocks `kander orchestrate`, dependency expansion, or `kander subscribe` when it cannot belong to any referenced group. The board now records, per membership problem, the group values that card could still declare, and each caller checks completeness only for the groups it actually references. A card whose `TASK_GROUP` value is not a legal group ID, or whose duplicate `TASK_GROUP` lines all name other groups, is ruled out; unreadable cards and scan problems such as a duplicate task ID still block, because their ownership is unknown. Duplicate empty `TASK_GROUP` lines keep the legacy discussion-section group value in play, so a real member is never skipped silently.

## v0.7.9 — 2026-09-20

- Release: the release workflow reads the Homebrew tap credential from the `HOMEBREW_TAP_TOKEN` repository secret (formerly `TAP_TOKEN`), so the tap formula is synced automatically when a tag is pushed. No change to the binary or the rules.

## v0.7.8 — 2026-09-20

- Rules, behavior change: intake option 1 now means the current session claims each card with `kander move <task-id> working --owner <agent>` and executes it itself; it no longer launches a separate executing agent with `kander start`. Option 2 remains the hand-off: `kander start` for exactly one standalone card, `kander orchestrate` otherwise. When the option 2 launch fails, falling back to option 1 means this session executes every card.
- Rules, behavior change: a later start instruction for cards created under option 3 defaults to the option 1 self-execution flow. Option 3 records an `EXECUTION_MODE: self` line in each card's `DISCUSSION` so a session that did not create the cards still finds the mode; an explicit instruction to hand a card to a separate executing agent overrides it and uses `kander pick` and `kander start`. Option 3 authorizes the plan and card creation at once, and the plan's integration authorization takes effect when the start instruction arrives.
- Rules: new "Self-Executed Groups" section in `KANDER-TASK-GROUP-RULES.md` for a task group whose orchestrating session also executes the members. The session acts as each claimed member's `OWNER`, so the clauses that bar the orchestrator from implementing, fixing, moving, or editing a member card bind only a session that is not the `OWNER`. Members are never bound to a dispatch: claims, fix rounds, and wrap-up use plain moves, dispositions are submitted while the card is in `working/` and omit `authorization`, completion carries no dispatch, delivery-commit, or disposition flag, and reviewers stay independent. Only members that never carried a `DISPATCH_ID` qualify, and a `kander orchestrate` session never self-executes.
- Rules: a self-executed group reconciles the coordinator after each transition it performs and names `delivery_commit` only while the member is in `review/`; with no wrap-up evidence, the session itself compares the reviewed and integrated patches and records the `source_commit`/`rebased_base` mapping.
- Rules: a card executed by its claiming session has no `SESSION`, so `resume` cannot take it over. Under user authorization a successor session of the same configured agent continues it under the recorded `OWNER`; a different agent needs an explicit user decision.

## v0.7.7 — 2026-09-19

- Agent overlay merge contract change: an `agents.<name>.session` overlay that omits `file` now inherits the embedded `session.file` declaration when its `mode` matches the embedded `mode`, instead of dropping it. This fixes `kander notify` failures and `kander check` liveness stuck at `unknown` (`agent pi declares no session file format for path identities`) for configs that overlaid `agents.pi.session` before pi gained `session.file` in v0.7.4. An explicit overlay `file` still wins, a different `mode` opts out, and every agent whose dialect resolves to the declaring embedded definition inherits — including custom wrappers with `dialect: pi`. `kander doctor` reports each relying overlay as an informational hint, and interactive repair can store the embedded declaration into the scope `config.json` (never into a project `.kander-config.json`).

## v0.7.6 — 2026-09-19

- Review: the PM reviewer prompt no longer treats a regression of a guarantee the system already had as out of contract. A regression introduced, worsened, or concealed by the review range is a gate finding even when the task context does not restate the guarantee; adding a guarantee the system never had stays out of scope. This matches the rejection grounds in the v0.7.5 rules.

## v0.7.5 — 2026-09-19

- Rules: a plan with exactly one standalone card starts that card directly; `kander orchestrate` is for two or more cards or a task group.
- Rules: rejecting a `blocking`, `high`, or `medium` finding on factual grounds now needs evidence that falsifies a material premise; an unsettled factual dispute is `unverifiable`. Scope grounds (pre-existing, `OUT_OF_SCOPE`, beyond the contract, same root cause) stay separate and need no falsifying evidence, and a regression of a guarantee the system already had has no scope ground.
- Rules: review notices (whitelist not hit, the unresolved list) no longer read as questions; integration continues, and only the items sent for a user decision wait.
- Rules: the 15-minute Security finding timeout covers `medium` findings only. A confirmed `blocking` or `high` Security finding waits for the user. A decision that arrives after the batch closed never reopens it.
- Rules: the orchestrator's pre-plan sync is the single case where a group branch is updated with `--force-with-lease`; it is required only when `develop` touched files the group changes.
- Rules: when the group merge-back rebase conflicts or changes the patch, the orchestrator resolves the conflict in a merge commit, which keeps the reviewed history intact. Hand-resolved code conflicts are reviewed before `develop` moves.
- Rules: a user-authorized takeover of a stopped executing agent proceeds inside an open review batch.
- Rules: when a reviewer becomes permanently unavailable after a successful run with open must-fix findings, the agent reports the batch state and the user decides.
- Docs: new multi-model routing guide (`docs/multi-model-routing.md`).

## v0.7.4 — 2026-09-19

- Bare `kander` now runs doctor once per binary version per scope, recorded in `kander-startup-state.json` under the rules root; the report waits for Enter before the board opens.
- Doctor lists every distinct `kander` executable on PATH with its version. An outdated Homebrew copy offers `brew upgrade kander`; in an interactive terminal you can keep one executable and remove the other non-Homebrew copies.
- Doctor stops before any check or repair when PATH holds a newer `kander` than the running binary, so an old binary can no longer downgrade newer on-disk rules or config schema.
- Doctor cleans invalid and duplicate Kander rules references in agent instruction files (`@` import lines, the `## Kander Rules Entry` block, and bare-path lines) without touching prose, comments, or code fences. The non-repair path reports them; the repair path reports how many were removed.
- Rules integration checks now cover every agent the installer integrates for the scope, not only the configured execution agents. The Options wizard uses the same set.
- Doctor repair reports legacy rules migration results and names each rule file it restored or upgraded from the embedded copy; locally edited files are still only hinted, never overwritten.
- Interactive doctor asks which usable agent should replace a configured agent that is unavailable, once per affected field, instead of silently writing the first candidate. Non-interactive runs keep the automatic replacement; a field with no usable candidate keeps its value with a warning. A `chat_agent` that was never set follows the repaired large-scale kanban agent.
- An empty or whitespace-only `.kander-config.json` project overlay is deleted on load instead of failing configuration loading.
- The Options panel probes every agent concurrently when it opens, so one slow CLI no longer serializes the environment check.
- Docs: advanced documents were rewritten against the implementation and translated into Chinese and Japanese; the READMEs gained a FAQ section and a production retrospective.

## v0.7.3 — 2026-09-18

- Devin, OpenCode, and Kimi join the built-in agents; the roster is now eight, and all three support review. Devin resumes exact sessions through declarative session discovery, and the discovered session binding is persisted atomically with the start evidence.
- `kander check` is hardened: non-blob tree entries such as gitlinks are excluded from line counting, `--json=<value>` is rejected as a usage error, invalid-ref errors no longer echo the supplied ref, and diagnostics escape control characters.
- The Issues overlay import now fences stale board reads: an import result cancels the board read in flight, invalidates the affected summary, and queues a fresh snapshot.
- Launch no longer polls session discovery past its deadline.
- Rules: a single large delivery may open a review batch immediately only when no other independent delivery is ready to receive; the task-group and review rule files now use identical wording.
- CI: the redundant Windows cross-compile job was removed.

## v0.7.2 — 2026-09-16

- Global rules now install to `~/.agents/kander` instead of the old location. After upgrading, run the installer once so the rules entry points at the new path.
- New `kander check delivery` and `kander check overlap`. Both analyze Git directly, need no kanban board, and run in any ordinary worktree. Exit code `0` means nothing to do, `1` means findings, `2` means a bad argument.
- The TUI chat box has its own agent and model settings, independent from the kanban agent.
- Board and detail view refresh off the UI goroutine and read from an incremental summary cache. Large boards paint noticeably faster.
- An orchestrator session may now run a single card; it is no longer limited to multi-card plans.
- Rules: every booklet was trimmed to agent-facing clauses only. Tool mechanics moved into `docs/`. The entry file now only names what each session must do first and delegates the rest to `KANDER-LOADING-RULES.md`.

## v0.7.1 — 2026-09-15

- Installation copies the binary automatically. The Y/N question now appears at startup only, and is no longer confused with the install copy step.

## v0.7.0 — 2026-09-15

- Review roles are consolidated into **PMQA** and **Security**. Old four-key batches that are still open keep working with their historical roles, and the Options panel shows a migration hint for legacy review keys. Optional integrated roles are available for projects that want a single combined pass.
- A chat box in the terminal board starts an agent session without creating a card first.
- An empty board opens a welcome overlay, offers `kander init` when `kanban/` is missing, and opens Options when the scope configuration is missing.
- All confirmation dialogs share one key contract.
- The Markdown detail view accepts vim motions.

## v0.6.2 — 2026-09-14

- Copying the binary to a global location is opt-in. The installer explains what an optional global copy does and uses the entry you selected.

## v0.6.1 — 2026-09-14

- Installation resolves package manager executable links correctly.

## v0.6.0 — 2026-09-14

- **GitHub integration.** `kander issue repo` resolves the canonical repository identity. `kander issue list` / `show` read issues, and pressing `g` on the board opens the issue list as an overlay. Issues import as backlog cards, `kander issue triage` starts a takeover session, and finished results publish back to the issue. Kander never asks for, reads, or stores a token; it reuses the credentials `gh` already manages. `kander doctor` reports GitHub CLI state.
- **Pi** joins Codex, Claude, Grok, and Cursor as a built-in agent, with review support. Built-in agents are now embedded versioned JSON definitions that declare launch argv, pane delivery, exit commands, session hooks, and review invocation. A custom agent must declare its own review template.
- **Terminal definitions.** tmux and herdr run from declarative definitions behind one backend interface. A new terminal inventory command reports available definitions, checks conformance, and documents the backend selection order.
- **Themes.** Six named themes, including Tide, Dusk, and Slate, selectable in configuration and in the Options panel, pinned to truecolor values.
- **Options panel.** Edits the project overlay with inherit and restore, sets reviewers per card scale, switches tabs with Tab, applies the interface language live, and restores it on cancel. Operational commands now require a complete configuration.
- **Narrow terminals.** Columns become a tab strip so the board stays usable on a phone-sized window.
- `kander orchestrate` launches a multi-card plan. `kander subscribe --watch` monitors cards outside a task group.
- A state-aware task action popup opens with `m`; `g` is reserved for the issue list.
- Durable dispatch recovers accepted work with fenced epoch deadlines, so an interrupted handoff no longer loses its receipt.
- Tagged releases are automated and sync the Homebrew tap.
- Rules: card `SIZE` is defined by task difficulty, not by line count. A GitHub issue rules set was added. An orchestrator session must keep monitoring the cards it starts. Task card titles in this repository are English.

## v0.5.0 — 2026-09-09

- First tagged release of the Kander kanban collaboration toolchain.
