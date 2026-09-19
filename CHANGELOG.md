# Changelog

## Unreleased

- Agent overlay merge contract change: an `agents.<name>.session` overlay that omits `file` now inherits the embedded `session.file` declaration when its `mode` matches the embedded `mode`, instead of dropping it. This fixes `kander notify` failures and `kander check` liveness stuck at `unknown` (`agent pi declares no session file format for path identities`) for configs that overlaid `agents.pi.session` before pi gained `session.file` in v0.7.6. An explicit overlay `file` still wins, a different `mode` opts out, and every agent whose dialect resolves to the declaring embedded definition inherits — including custom wrappers with `dialect: pi`. `kander doctor` reports each relying overlay as an informational hint, and interactive repair can store the embedded declaration into the scope `config.json` (never into a project `.kander-config.json`).

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
