# Changelog

## Unreleased

- Fix (review, #20): `kander review advance` and a review run with `--advance-file` now refuse to move a batch target while an assigned finding has no author disposition, and the error lists each task, run, and finding. Mechanical items count. An advance could previously overtake a member's fix round and leave its fix dispatch rejected at every base.
- Fix (dispatch, #20): a batch already advanced past a finding recovers without any board edit. A fix dispatch with `--base` set to the current batch target may reference a run on the batch's recorded advance chain, and `kander coordinator reconcile` accepts the completed round.
- Fix (coordinator, #20): reclaiming a `review/` card that never held a dispatch now records a handoff, so `kander coordinator reconcile` follows the cycle change instead of failing with `execution cycle changed without lifecycle handoff`.
- Breaking (`kander move`): `kander move <id> working --owner <agent>` from `review/` now requires `--decision <reference>`. Scripts that omit it are rejected with no change. A card that a plain reclaim already moved keeps its old failure; this release has no repair for an existing checkpoint.
- New: execution ledger. Every change of `OWNER`, `SESSION`, `WINDOW` or the execution cycle now writes a producer record beside the card. `kander coordinator reconcile` walks these records, so missed reconciles no longer strand a member, and a change no record carries fails.
- Fix (start, #22): a late launch failure no longer destroys an executor that already worked. Before it closes anything, `kander start` compares the card with its own last write. With executor evidence it keeps the container and the card, writes the start attempt as `unresolved`, prints `started-unresolved: <task-id> window=<address> reason=<reason>` with two next steps, and exits 3. Without evidence it closes the container and rolls back as before, and exits 1. A failed read of the card keeps the container too. Scripts must treat exit 3 as "do not retry `kander start`". The TUI start dialog shows the same result.
- Fix (start): when closing the container fails, the card no longer rolls back, because a second `kander start` would run a second executor. The start ends unresolved.
- New: `kander resume <task-id> --adopt --decision <reference>` keeps the running executor of a `pending` or `unresolved` start. It starts nothing and needs an executor confirmed `alive` or `alive-orphaned`. A tmux pane with no session marker reads `unknown`, so such a start needs a takeover.
- Breaking (`kander resume --agent`): a takeover of a `pending` or `unresolved` start now requires `--decision <reference>`. On a `running` record it stays optional. A takeover uses the same results as a start, including exit 3.
- New: a launcher that a takeover or an adoption overtook prints `superseded: <task-id> ...` and exits 0. It closes nothing and rolls nothing back.
- Breaking (`kander dispatch prepare`, `kander notify`): both refuse a card whose start attempt has not ended, and the error names the next step. This covers the short window while `kander start` still runs, so a script that notifies right after a start must wait for `kander start` to return. A start attempt an old binary left in `pending` ends the same way, through adoption or takeover.
- New: `kander dispatch settle <task-id> <dispatch-id> <revision>` ends the binding of a completed round. `kander move` into `archived/` or `trash/` settles a completed binding by itself, and a settlement counts as a release for a reclaim.
- Fix (`kander start`, `kander resume --agent`): an executor that writes the card early no longer fails session confirmation, the `WINDOW` write of a pane-delivery agent such as Kimi or Droid, or the session write-back of a takeover. A failed launch still restores nothing over newer executor work.
- Fix (coordinator): reconcile no longer fails after a dispatch was discarded between two reconciles, after a claim followed by `kander move ... trash`, after `trash` of a member that finished wrap-up, or after a plain `kander move <id> working` that followed a rollback. A member in `archived/` or `trash/` is checked only for its result and its execution records, and its Git facts are no longer verified again.
- Breaking (`kander update`): a `TASK_GROUP` change is refused while the old or the new group has a coordinator checkpoint.
- Breaking (`kander dispatch fail|cancel`): ending an accepted round now requires `--decision <reference>`.
- Breaking (`kander resume`, `kander notify`, `kander dismiss`): a card claimed or reclaimed with `--owner` is refused with a next step, before any dispatch binds. `kander resume --agent` still works, and now also replaces a claimed card that has no `SESSION`.
- Breaking (dispatch records): a dispatch is refused when its recorded execution cycle differs from the card's. Dispatches from an older version have no cycle and keep working.
- New (#23): `foreground` and `console` starts record a process identity beside the card: boot, agent PID, kernel start time, and executable. The identity is written once the agent reads its task file, because only then is its final executable known; until then the card reads `unknown`. On Windows it is written at once. A mount with `noatime` stays `unknown`. `kander check` and `kander subscribe` now answer for these cards instead of always saying `unknown`, on Linux, macOS, and Windows. Other platforms report `unprobeable`.
- New (#23): the liveness result set adds `alive-orphaned` (the agent runs, its foreground Kander process is gone), `identity-mismatch` (a process holds the recorded identity but is not this execution, or the card moved on), and `unprobeable` (the platform has no process identity or refused the probe). Scripts that read `status` from `kander subscribe` must accept these values. `unprobeable` and `unknown` never count as alive. A card started by an older binary reads `unknown` with the reason "old card has no process identity".
- New (#23): `kander resume --adopt` works on `foreground` and `console`. It uses the process identity and accepts `alive` and `alive-orphaned`.
- Breaking (`kander resume`, `kander notify`, #23): `foreground` and `console` have no native resume. Plain `kander resume` and the recovery channel of `kander notify` refuse a card that runs on either of them, and a relaunch through either of them, with the next step: adoption or a takeover. This also covers cards from older binaries. `kander resume --agent` is not refused.
- Upgrade: old and new binaries cannot share a board. A new binary reads old boards in place and writes schema 2 execution, dispatch, and checkpoint records, which an old binary rejects. Upgrade every session that touches a board together.
- Run `kander doctor` after upgrading to update the rules.

## v1.0.0 — 2026-10-29

- Microsoft Store: kander can ship as an MSIX package for Windows (x64 and arm64) with the `kander.exe` execution alias. A Start menu launch opens the board. The package turns off file and registry write virtualization so agents outside the package can read the task files kander writes under `%TEMP%`; it needs Windows 10 2004 (build 19041) or later. The Store's package version rules start the version line at 1.0.0, because a Store version cannot begin with 0.
- A kander installed from Microsoft Store leaves updates to the Store: the board's startup update check makes no network request and shows no update dialog, and a self-update is refused before anything is downloaded or written. It also never copies itself onto PATH: bare `kander` asks no copy question and `kander install` copies no binary, because the execution alias is already on PATH. Other installs are unchanged.
- Releases: for a stable tag the Release workflow also builds the Store upload bundle on a Windows runner and attaches it to the GitHub Release once the `KANDER_MSIX_IDENTITY_NAME`, `KANDER_MSIX_PUBLISHER`, and `KANDER_MSIX_PUBLISHER_DISPLAY_NAME` repository variables are set; until then it skips with a notice. The new MSIX workflow can also be run by hand. See `docs/windows-msix-store.md`.
- Fix (notify, #19): tmux and tmux-session delivery now waits 500ms between the typed instruction and the `Enter` that submits it. Codex treated an Enter that arrived right after the text as part of a paste, so the instruction stayed in its composer and notify reported the acknowledgement as unconfirmed. The wait applies to every agent and to every tmux delivery: direct notify, durable dispatch, pane-mode start prompts, `dismiss` exit commands, and `kander terminal test`. herdr and luvus keep their own `agent prompt` and are unchanged.
- Terminal definitions: a step may declare `delay`, a Go duration (at most 5s) that the backend waits before running the step's command. The wait is outside the step `timeout`, a skipped step does not wait, and a cancelled context ends the wait.

## v0.9.14 — 2026-10-28

- Fix (notify, #18): direct notify no longer reports `acknowledgement=confirmed` when the pane only shows the delivered instruction, for example in an agent composer that never submitted it. The `# kander-notify:` line no longer contains the acknowledgement marker. It names the private message file and asks the agent to output the acknowledgement line that file names. The message file now starts with a fixed request followed by `KANDER-NOTIFY-ACK:<32 hex>`, so the marker reaches the pane only after the agent read the file. This applies to tmux, tmux-session, and luvus polling and to herdr `wait-output`. Durable dispatch writes the same request and is still settled only by its accepted receipt. When no marker appears, notify waits for `--timeout`, prints the existing "delivered, not confirmed" warning, and reports the acknowledgement as unconfirmed.
- Run `kander doctor` after upgrading to update the rules.

## v0.9.13 — 2026-10-27

- Review recovery: when two runs of the same role ran in parallel by mistake, the agent can close the batch instead of being blocked. `kander review close` accepts an optional `superseded_runs` map that names each extra run, its replacement run, and the basis. A run qualifies only as a parallel twin of a run on the conclusion chain (same commit, same reviewer, same previous run), or as the successor of a superseded run whose commit and reviewer match the conclusion run. Each entry adds the Git edge from the superseded run's commit to the replacement's, and the superseded run's findings still need dispositions. A close that meets an unlisted run fails with a hint that names the run, the conclusion run, and the entry to add. Runs that used a different reviewer, or a full first-round rerun after a fix, still cannot be superseded; the rules tell the agent to report them to the user.
- `kander review progress` adds `next_actions`, which lists duplicate runs to supersede before close is tried. Starting a review run while another run of the same role is not linked into the chain still runs and prints a warning on stderr.
- `kander update` keeps its error codes and texts, and adds one line that says where the rejected content belongs and which command writes it: managed fields, managed sections, managed paths, duplicate headings, resolved pins, and frozen contract changes.
- Run `kander doctor` after upgrading to update the rules.

## v0.9.12 — 2026-10-25

- TUI: for a backlog card that cannot start, the `m` task actions menu shows "Talk about" in place of "Start task". The menu runs the same read-only checks as the TUI start path: the todo gate (complete contract, review records, card pins), the card form (valid SIZE, directory card), and the task-group switches. Choosing "Talk about" opens the chat box prefilled with a prompt that names the task, the card directory, and the problem, and asks the agent to explain why the card cannot start and how to fix it, without moving, starting, or claiming the card. The prompt replaces the kept chat draft and can be edited before Ctrl+S sends it. A startable card, a todo card, or a check that cannot finish keeps "Start task".

## v0.9.11 — 2026-10-23

- Fix (subscribe): a subscription read that exhausts its 2-second board lock wait no longer ends the subscription at once. It emits nothing, keeps the last snapshot, and retries after a full `--refresh` interval. Only lock wait timeouts that continue for 30 seconds emit the terminal read-error (`read_status=maintenance`). The initial snapshot follows the same rule.
- Fix (coordinator): `kander coordinator reconcile` no longer runs Git while it holds the exclusive board lock, so concurrent subscription reads no longer time out during a reconcile. Reconcile collects Git facts under shared locks, verifies them with no lock held, and commits only verified facts. A member, checkpoint, or Git fact that changed in between is reported as a conflict. An invalid member card is still reported before Git runs.
- Rules (Git, review): a reviewed single card now integrates the way a task group does. The integration rebase is used only when it applies cleanly and leaves the reviewed patch unchanged. On any conflict or a changed patch, the agent merges the latest `develop` into the task branch instead, and later advances are merged again. A merge commit whose code conflicts were resolved by hand, or one the user asks about, is reviewed in a new batch, so a single card keeps its review plan unsealed until integration succeeds and then seals it. A task group merge-back is also re-reviewed when the user asks.
- Run `kander doctor` after upgrading to update the rules.

## v0.9.10 — 2026-10-19

- Rules (review): the PMQA reviewer prompt now yields one report: one requirement table, one behavior table, and one merged findings list, with an incremental layout for re-reviews (prior finding statuses by ID first, then sections limited to the fix range). A bug fix without a reproducing automated test is a medium finding (the bug-fix review profile takes precedence over project-rule exceptions), and a test a project rule requires is a recommend item. A test counts as evidence only when it exercises the production path and would fail if the behavior were reverted; false coverage is a non-mechanical medium finding that needs re-review. Reviewers look for an existing equivalent before accepting a new function, type, or abstraction, report changes that bypass an established layer, and take architecture from architecture docs, decision records, and `AGENTS.md` / `CLAUDE.md`. An obvious complexity regression with a realistic trigger is reported as a non-blocking suggestion.
- Tests: the full test suite passes on Windows.
- Run `kander doctor` after upgrading to update the rules.

## v0.9.9 — 2026-10-16

- Language reminder. Every one-line instruction Kander delivers to an agent now ends with one sentence, written in the agent language itself, to communicate in that language (for example `用 zh-CN 语言交流。` or `ja 言語でやり取りしてください。`). It covers start, resume, takeover, notify recovery, orchestrate, issue triage, refinement and result, and chat sessions, the `# kander-notify:` line of direct and dispatch delivery, and the reviewer instruction. The value is the card `LANGUAGE` (or `agent_language`), the same one the existing English language directive uses. Sentences exist for English, simplified and traditional Chinese, Japanese, Korean, Spanish, French, and German, matched by primary subtag (`en`, `zh`, `ja`, `ko`, `es`, `fr`, `de`). Chinese uses the traditional sentence for script `Hant`, or for region `TW`, `HK`, or `MO` without script `Hans`, and the simplified sentence otherwise. Other languages get the English sentence, and an invalid value gets none. The reminder rides only on the delivered line: task files and notify message files are unchanged.
- Rules: option lists, report templates, and example questions that the rules give in English now fix only the meaning, numbering, and order. Agents present them, field labels included, in the `agent_language` (or the card `LANGUAGE`), keeping identifiers, command lines, paths, SHAs, card states, and fixed values such as `None` and `N/A` verbatim. This covers the task intake options, the completion report, the orchestration summary, and the orchestrator's integration and dismiss questions.
- Run `kander doctor` after upgrading to update the rules.

## v0.9.8 — 2026-10-16

- Fix (TUI): the Options "Execution and models" page separates the extended launch timeouts switch from the launcher above it by a blank line, and its description is shortened to "Raise launch timeouts to 120s".

## v0.9.7 — 2026-10-16

- Issue results for split work. An issue's anchor card plus every sibling card whose `DISCUSSION` holds a `SOURCE_ISSUE` line naming it now form one work set. `kander issue result` takes only the anchor (a sibling ID is rejected with the anchor's ID), and `start`, `inspect`, `apply`, and `decide` fail while any member is incomplete and name the pending cards. A member is complete in `done/` or archived as completed; a sibling in `trash/` or archived with another result is excluded and listed separately. `inspect` lists every member and the excluded siblings, and the proposal's `outcomes` cover every member's acceptance checkboxes in order. A single-card work set keeps its previous result identity, so existing records stay valid.
- Completion handoff. When a CLI `kander move` (not a dispatch replay) makes a work set ready, it prints a notice on stderr with the issue, the anchor, the setting, and the `--action inspect` command line, and the session that ran the move reconciles the result right away. The new `issue.result_on_done` setting (`auto` by default, `ask`, or `off`; the project overlay may override it, and the Options rules page edits it) decides whether that session publishes the result comment directly, asks first, or skips the handoff. Closing the issue always needs separate consent. The TUI Issues overlay shows work set progress (k/n), marks a ready work set without a reply, and offers `s` result reconciliation only for ready work sets.
- Acceptance checkboxes. A card in `working/` or `review/` may tick or untick top-level `- [ ]` / `- [x]` markers in `ACCEPTANCE_CRITERIA` through an ordinary `kander update`, without a contract decision file and without a new review batch. Any other frozen change still needs `--contract-decision-file`. The rules tell the executing agent to tick met criteria before SUMMARY or `report.md` (single cards) and before `move review` (group members), and the report Acceptance count follows the ticked items.
- Agent config directories. Install, `kander doctor`, and repair resolve every global agent rules target and the Pi extension below the agent's config directory: `agents.<name>.config_dir` in the scope config first, then the variable the agent reads (`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `GROK_HOME`, `KIMI_CODE_HOME`, `PI_CODING_AGENT_DIR`, `OPENCODE_CONFIG_DIR`, `XDG_CONFIG_HOME`), then the default. A project overlay cannot relocate global writes: its `config_dir` is ignored and `kander doctor` warns about it.
- Extended launch timeouts. The new optional `extended_launch_timeouts` setting (`false` by default; the project overlay may override it, and the Options "Execution and models" page edits it below the launcher) raises every launch-time wait shorter than 120 seconds to 120 seconds: the launcher pane readiness (15 seconds), session discovery (the agent's `session.discovery.timeout_ms`, default 10 seconds, and the Codex rollout wait), and pane prompt-delivery readiness (`prompt_delivery.ready.timeout_ms`). Longer waits are kept, and with the setting off every wait is unchanged. A terminal definition's `wait_ready` may read the readiness budget as `{timeout_ms}`, as the embedded herdr and luvus definitions now do; `kander terminal test` always uses 15 seconds.
- Fix (install): the previous rules entry is kept while any agent rules target cannot be resolved, so a legacy symlink is never left dangling.
- Fix (install): cursor-agent never reads `~/.cursor/AGENTS.md`, so the global cursor rules reference now lives in the always-applied rule `~/.cursor/rules/kander.mdc` (created with `alwaysApply: true` frontmatter; repair adds a missing frontmatter or reference, and leaves a rule with other frontmatter or a symlinked rule untouched). cursor's config directory is fixed: `agents.cursor.config_dir` is rejected.
- Run `kander doctor` after upgrading to update the rules.

## v0.9.6 — 2026-10-15

- Issue import with card refinement. In the TUI GitHub Issues overlay, `i` (`I` is now an alias) imports the selected issue with its comments and immediately starts the configured agent to complete the new backlog card. The refinement session reads the stored source attachments and the repository, completes the card contract, asks only for missing facts or real user decisions, and leaves the card in backlog; it never implements code, moves the card, or writes to GitHub. A launch failure keeps the imported card, and `s` on a bound backlog issue retries refinement. Importing an issue that already has a card does not launch a session or refresh its snapshot. `KANDER-ISSUE-RULES.md` adds the "Imported Card Refinement" section and the rules root adds `templates/prompt/refine.md`.
- Issue import rejects an issue with more than 50 comments instead of truncating to the latest 50.
- Fix (TUI): a refinement result or failure that returns after the overlay closed, or after the selection moved to another issue, keeps its receipt and binding, and the retry hint names the issue to select. A superseded issue index scan now fills missing card bindings instead of being dropped, so a just-committed import keeps its binding in the overlay.
- Fix (install): a nonempty `PI_CODING_AGENT_DIR` replaces `~/.pi/agent` for the global Pi rules reference and the `kander-rules.ts` extension, in install, `kander doctor`, and repair. `~` and relative values are expanded; project targets are unchanged.
- Run `kander doctor` after upgrading to update the rules.

## v0.9.5 — 2026-10-14

- Fix: `kander start` and takeover of a Codex card no longer fail with `context deadline exceeded` when the Codex sessions directory holds many rollouts. Discovery no longer reads any rollout before launch: after launch it reads only the date directories around the launch time, and a rollout counts as new when its UTC session timestamp is at or after the moment the launch began, so a Codex process in another time zone is still found. The task prompt prefixes are built once per lookup instead of once per rollout file.
- Fix: the embedded Devin definition gives session discovery a 30 second budget (`session.discovery.timeout_ms: 30000`), since one `devin list` call can take several seconds.
- Run `kander doctor` after upgrading to update the rules.

## v0.9.4 — 2026-10-12

- TUI agent status. Cards in `working` or `review` whose `WINDOW` names an agent-identity terminal (herdr, luvus) show the state of their executing agent: a mark before the card title (working, blocked, waiting for input, lost, unknown), the blocked count in the panel title or narrow-board state tab, and the status, observation time, and reason in the detail view. The board observes every 5 seconds (independent of `tui.refresh`) and at once on `r`, in the background, without blocking input; the help overlay lists the marks. The classification follows `kander check`.
- Terminal definitions accept an optional `agent_list` operation that reports every agent pane in one call, so the status observation runs one process per backend instead of one probe per card. The embedded herdr definition declares it over `herdr agent list`; backends without it fall back to per-pane `pane_facts`.

## v0.9.3 — 2026-10-12

- Rules split into phase snippets. The rules root now holds `snippets/` and `templates/` beside the `KANDER-*.md` files, and the installer installs, tracks, and repairs them (a locally modified template is backed up before replacement). The phase sections of the kanban, git, task group, and review rules move into snippets (`execute-single`, `execute-group-member`, `orchestrate-start`, `orchestrate-sessions`, `orchestrate-review`, `orchestrate-integrate`, `self-executed-group`, `fix-round`, `wrap-up`, `failure-recovery`, `coordinator-checkpoints`, `review-evidence-gate`, `git-sync`, `git-integrate`, `report-orchestration`), and `KANDER-LOADING-RULES.md` replaces the reading map by role with a phase snippet list. Start, resume, takeover, orchestrate, and dispatch prompts name the snippet to read for their phase. Run `kander doctor` after upgrading to update the rules.
- Command contracts. Each command's contract lives in `snippets/commands/<command>.md` with seven fixed sections, indexed by `snippets/commands/README.md`; the "Command Contract" text leaves `KANDER-KANBAN-RULES.md` and the "Single Review" text leaves `KANDER-BASE-RULES.md`. Every usage and `kander <command> --help` output prints the contract's Syntax block and the contract path, so usage is English in every locale. When the installed contract is missing, usage renders from the embedded copy and prints a one-time hint on stderr to run `kander doctor`.
- Prompt templates. Every agent prompt (start, resume, takeover, notify, orchestrate, chat, triage, result, task-file head and trailer, completion report prefill, reviewer bootstrap and contract) renders from `templates/` below the rules root, and an installed template overrides the embedded copy. Prompts are English in every interface language. A template that fails to parse or lacks a data key fails the command instead of launching an agent with a broken prompt. Codex session matching keeps the earlier localized heads, so rollouts from earlier releases still match.
- Reports. `KANDER-REPORTING-RULES.md` adds the "Orchestration Summary" template, issued once at the end of an orchestration ("group-level summary" is renamed throughout). The completion report and the orchestration summary put each unresolved issue on its own sub-line and split the summary tail into sub-lines.
- Fix (TUI): in the compact layout, states left of the focused state now show when they fit in the same stacked column, instead of the focused state being pinned to the left edge.

## v0.9.2 — 2026-10-10

- Rules wording follows ASD-STE100 (Simplified Technical English) principles. Imperative prohibitions consistently use "never", sentences over 40 words are split into one instruction or fact each with their conditions kept, and the blocked-card and self-resolution condition lists become sub-bullets. A new "Rule Wording" section in `KANDER-LOADING-RULES.md` fixes the modal words: in directive sentences `must` is an obligation, `never` a prohibition, and `may` a permission. The rules' meaning is unchanged.
- Report and card wording. `KANDER-REPORTING-RULES.md` adds "Report Wording": result first, one fact per sentence, fixed status words (values defined by the template or other rules take precedence), the verification method for each claim, marked uncertainty, locatable evidence, numbered options, and no invented facts. The card self-review checklist adds that each acceptance criterion states one checkable result, conditions come before actions, and one term names one thing. Run `kander doctor` after upgrading to update the rules.

## v0.9.1 — 2026-10-05

- Completion report reminder. With `rules.reporting` on, `kander move` into `done`, `archived`, or `trash` (not a dispatch replay) prints a localized reminder and the completion report template on stderr, prefilled with the task ID, title, absolute final path, final card state, and card language. Stdout and dispatch JSON are unchanged, and TUI moves print nothing. The start, resume, takeover, and notify prompts also carry a short report reminder while the switch is on. Run `kander doctor` after upgrading to update the rules.
- TUI: new optional `tui.theme_light` and `tui.theme_dark` settings (default `light` / `dark`) choose the concrete themes `auto` uses on a light or dark terminal background, at startup and when the background changes at runtime. The options panel adds two selectors that list the matching theme family and keep a hand-edited cross-family value.

## v0.9.0 — 2026-10-04

- Built-in `luvus` launcher. Embeds `definitions/luvus.json` so `kander start`, `notify`, `resume`, `focus`, and `dismiss` work from a luvus pane on POSIX without a user-placed definition (`LUVUS_ENV=1`, `LUVUS_PANE_ID`, and luvus on PATH). Container creation passes `--cwd` and validates that the new pane honors the requested directory (resolving symlinks with `cwd_resolved`), failing early with an upgrade hint on older luvus versions. Auto launcher resolution selects luvus at priority 150 (between herdr 200 and tmux 100); native Windows rejects luvus. Run `kander doctor` after upgrading to update the rules.
- TUI compact stacked panels: stacked panels fit their content height and divide spare column height evenly, so collapsing or expanding a task group immediately re-stacks the column. Up and down keys continue across stacked panels in the same visual column when reaching panel boundaries without crossing columns or wrapping around, and scrolling stops at the last content row.
- TUI: the board status bar adds a clickable `c chat` entry before `o options` and `? help`.
- TUI: pressing `y` on a focused task-group header copies the full task-group ID.
- Fix (liveness): missing terminal program diagnostics name the backend's own executable instead of always attributing it to herdr.

## v0.8.9 — 2026-10-02

- Archiving cleans up. After a card moves into `archived/`, Kander deletes the review evidence and control records that belong only to archived cards: review runs (inputs, originals, staging), batches, plans, closures, dispositions, task plans, start and dispatch records, task-group checkpoints, and the `reviews/` copies under those cards. Anything still shared with a card that is not archived, or belonging to a task group with a member outside `archived/` and `trash/`, stays until that changes (a task-group checkpoint also needs every member it lists to be archived); unfinished review runs of archived cards go too unless a review gate still holds them. Card bodies, `REVIEWS` index lines, reports, and `versions/`, `locks/`, and `issue-results/` are kept. `move` prints what was removed and how much space it freed; a failed cleanup never undoes the archive, and the next archive or `kander init` resumes it. `kander init` also cleans cards archived by earlier releases. Per-card receipts keep `kander check --all` quiet about removed evidence. The TUI archive form warns that archiving is final.
- Archived cards are final. `archived` no longer moves to `trash`, and every write to an archived card fails with a "final state" error before any side effect: `kander update` (body and attachments), `move`, review publication and interpretation, dispositions, dispatch records, and WINDOW/SESSION write-back. A review run shared with an archived card can therefore no longer be interpreted or closed. Moving into `archived/`, `init` recovery, legacy-card migration, read commands, and `dismiss` work as before, and `archived` with `RESULT: completed` still satisfies dependencies. Run `kander doctor` after upgrading to update the rules.
- Cards can pin their executor and reviewers regardless of the configuration with optional header fields: `EXEC_AGENT`, `EXEC_MODEL`, `EXEC_EFFORT`, and `REVIEW_PMQA_*` / `REVIEW_SECURITY_*` with the same three items. `start` (and the TUI start preview), `resume`, takeover, `notify` recovery, and task-bound `kander review` apply them; a conflicting `--agent`, reviewer argument, or review environment override, a model or effort without its agent, an unknown agent, or an effort the agent cannot take fails before any side effect. Pins freeze with the contract, and `kander check` and the `todo` gate validate them. Cards that pin anything get managed `EXEC_RESOLVED` / `REVIEW_<ROLE>_RESOLVED` records marking each value `forced`, `cli`, or `config:<scale>`; the records stay out of review task-context snapshots. Cards without these fields behave and are written exactly as before; older binaries ignore the fields. Run `kander doctor` after upgrading to update the rules.
- Notify (Codex on Linux, herdr): a card whose executor started but never recorded its Codex session ID is repaired before notification or an unaccepted durable dispatch. Kander proves the session from the pane's single foreground Codex process, the rollout file it holds open under `CODEX_HOME/sessions`, the physical launch directory, and this task's execution prompt, then reports and saves the ID and delivers normally; a retry after a partially saved report only completes the card binding instead of restarting the executor. Unprovable, conflicting, or unsupported cases keep the executor and pending dispatch and print a specific diagnostic. Run `kander doctor` after upgrading to update the rules.
- TUI: `Ctrl+J` inserts a newline in the chat box.

## v0.8.8 — 2026-09-30

- TUI: task-group rails start below the group header. The header row draws no rail, and member rows and in-group gaps indent the rail by one character with one space between it and the card content; group color, selection background, collapse state, scrolling, and mouse character selection follow the new layout.
- TUI: two new named themes. `matcha-zen` is a light theme ported from the livlign/posh-palette terminal theme and `bamboo-multiplex` is a dark theme ported from ribru17/bamboo.nvim; both use the selection-surface palette form and appear at the end of the `t` key cycle and the options Color theme list, and `tui.theme` accepts them.

## v0.8.7 — 2026-09-30

- TUI: cards of the same task group in one column cluster together and are joined by a continuous `│` rail drawn in the existing left pad column, so the column width and the card body offset stay unchanged. Each cluster takes one of eight colors for the active theme family (dark and light palettes, every color at least 3:1 against that family's backgrounds), chosen from the group ID's FNV-1a hash and shifted one slot when it would repeat the previous cluster's color; a selected card keeps its group color in the left pad.
- TUI: a clustered group can be collapsed and expanded. Its header row shows the rail color, `▾` or `▸`, the shortened group name, and the member count; `Enter`, `Space`, or a click toggles it, `z` toggles the focused card's group, and `m`, `y`, and `Enter` on a header no longer open the task menu, copy the ID, or open the detail view. Groups start expanded and keep their collapsed state for the process, a non-empty search temporarily expands them, and the column badge still counts hidden cards. Board rows now have variable heights, so `j`/`k`, paging, the wheel, and mouse hit testing follow visible rows, and the compact and archived columns use the same rows. Help gains `z`; the three catalogs add `tui.toggle_task_group`.

## v0.8.6 — 2026-09-29

- Fix (Windows): the private review runtime is usable by grok and codex. The runtime lease no longer holds DELETE access, so a reviewer can enter the runtime as its working directory; embedded agent definitions may declare `review_windows_read_groups` (Codex declares its sandbox group), and reviewers with read groups always get the spec snapshotted into the runtime. The claude review no longer injects `CLAUDE_CONFIG_DIR`.
- Fix (Windows): review evidence compares working directories as one directory, ignoring separator style and case, so a plan written with the `C:/...` spelling from Git no longer fails with `run/plan worktree mismatch`.
- Fix (Windows): herdr session reporting and pane focusing dial the herdr named pipe instead of a unix socket, so starts report the session identity and notify, dismiss, and focus work again.
- Fix (Windows): agent-facing paths in task files, rules, and notify messages use forward slashes, so claude and grok running commands through Git Bash no longer mangle the project-install `kander` path or the task file cleanup.
- Review: a later run of an existing batch whose member specs drifted only in task context now fails with a dedicated error naming the frozen `reviews/<run-id>/task-context.md` snapshot to pass, or asking for a new batch, instead of the generic `batch binding conflict`. The review rules describe the frozen snapshot; run `kander doctor` after upgrading to update the rules.

## v0.8.5 — 2026-09-28

- Notify: a herdr agent in the `working` state now receives `kander notify` and durable dispatch messages directly and handles them after its current turn, instead of being polled as busy until the timeout with nothing delivered. `blocked` and other states stay busy, since the pane may show a dialog that typed text would answer; tmux copy-mode stays busy, and `dismiss` still refuses a working agent. A durable dispatch the agent accepts only after its deadline is still rejected and follows the existing expiry handling.

## v0.8.4 — 2026-09-28

- Fix: retrying an interrupted review run whose advance records no `foreign_commits` no longer fails with `input conflict`. An empty `foreign_commits` map is normalized to its durable (omitted) form before the run identity is compared, so the identical retry is accepted and can finish publication.

## v0.8.3 — 2026-09-26

- Rules: review run IDs and batch IDs are unique across the whole board, not per card. Agents prefer the generated run ID; a chosen ID starts with the task ID (or the task-group ID for a batch), such as `<task-id>-pmqa-1`, instead of short generic names that collide with earlier cards and fail with `run identity conflict`.

## v0.8.2 — 2026-09-25

- TUI: the options panel and help accept the mouse. The status bar has clickable `o options | ? help` entries. In options, the wheel scrolls without changing values, clicking a menu item opens it, the selector arrows and Yes/No labels edit values, and clicking a text field gives it keyboard focus. Scope tabs and restore confirmations accept clicks.
- TUI: options sections have no mouse Save/Back buttons; Enter still saves. Clicking outside the frame on a section returns to the root menu and keeps every edit pending, including an interface language change. Clicking outside the root menu, or the visible Esc-close hint, closes options, and unsaved edits ask Save and close / Discard and close / Keep editing first.
- TUI: help scrolls with the wheel and closes on any key or a click outside its frame; clicks inside it keep it open.
- Fix: a click on an options menu item opens the item under the cursor. Menu rows were computed from a recalculated wrap that counted the colored selector's escape bytes as width, so in color terminals long labels took two rows and later clicks opened an item above the target. The unsaved-close confirmation and the menu's Esc-close hint are fixed the same way.

## v0.8.1 — 2026-09-23

- Review: accept complete readable reviewer reports for attributed receiver interpretation when structured findings cannot be parsed. `kander review interpret` binds findings to the original report hash and quoted evidence; unresolved interpretations cannot satisfy the review completion gate.
- Fix: Cursor reviewer invocations pass `--yolo` so tool calls do not wait for interactive approval.
- Review: Pi reviewer invocations use default tools and resource loading by removing the explicit tool allowlist and the switches disabling extensions, skills, prompt templates, and themes. Read-only inspection still relies on the prompt and post-run worktree verification.
- Fix: recognize the previous official review rules when the installation state file is missing, allowing doctor to update those rules without treating them as local edits.

## v0.8.0 — 2026-09-22

- TUI: simplify the options-panel workflow chart to the main path: confirm plan, execute and verify, PMQA, Security, deliver and integrate, and done. Each review role has its own fix-and-verification loop. The chart omits detailed gates, self-check, pause nodes, and integration rework; the underlying completion rules still apply.
- TUI: skipped review stages disappear and their surrounding nodes connect directly. Plan confirmation and integration follow their module switches. Wide layouts show return rails; narrow layouts name the role to re-review. Task-scale and scope switching, unsaved configuration previews, and model and effort labels remain available.
- Rules: fixing Security findings requires only Security incremental re-review and the necessary verification, without reopening PMQA. The mechanical-fix exception remains. Independently required PMQA fixes that change already-reviewed security code still require the relevant Security re-review.
- TUI: remove the configuration-preview footer from the workflow chart in English, Chinese, and Japanese. The review-disabled notice remains.

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
